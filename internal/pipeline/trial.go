package pipeline

import (
	"log"
	"sort"
	"time"

	"github.com/whoisnian/rocom-capture/internal/capture"
	"github.com/whoisnian/rocom-capture/internal/gamedata"
	"github.com/whoisnian/rocom-capture/internal/gcp"
	"github.com/whoisnian/rocom-capture/internal/server"
	"github.com/whoisnian/rocom-capture/internal/trial"
)

// ---- 草系徽章试炼:实时同步游戏内的一局 ----
//
// 玩法与全部 opcode 的语义见 docs/pcap-20260831-grass-trial.md。一句话:
// 服务器把「一局」的完整状态(试炼宠物副本、当前章节/节点、节点选项、待处理奖励、商店、
// 金币)反复全量下发,客户端的操作则是 c2s 的「推进节点 / 选事件 / 刷新 / 处理奖励 /
// 选祝福 / 买东西」。本文件把两侧拼成一份状态,经 SSE 推给草系试炼页。
//
// 状态挂在**账号**而非连接上:一局跨传送/换场景,断线重连时服务器还会用 0x1960
// (ResumeChallengeRsp)把整局续回来;按连接存会在这两处丢掉。

// trialIdleTimeout 是「一局已结束」的兜底判定:这么久没有任何试炼消息即视为已离场。
// 正常一局不会静默这么久(战斗/心跳都会带 0x195c 全量同步)。
const trialIdleTimeout = 30 * time.Minute

// trialLogMax 是保留的操作流水条数(前端只展示最近若干条,不必无限增长)。
const trialLogMax = 40

// trialReviewKeep 是历史战绩里下发给前端的最近条数(服务器保留 250 条,全量下发太重)。
const trialReviewKeep = 20

// trialTopPets 是「常用形态」榜单的条目数。
const trialTopPets = 8

// trialState 是某账号的试炼状态(进行中的一局 + 账号档案)。
type trialState struct {
	run     *trialRun             // 当前/上一局;nil=从未开始
	history *trialHist            // 账号档案(0x1975)
	query   *trial.QueryChallenge // 最近一次开局前查询(可选章节/词条/初始金币)
	updated time.Time             // 最近一条试炼消息的到达时刻(墙钟),兜底判定离场
	pushed  bool                  // 是否已广播过(避免没数据时也推)
}

// openRun 用一份全量快照开启/刷新一局。
//
// 服务器**每次都发全量**,且入口有四个(0x1951 开局 / 0x1959 打开面板 / 0x195c 换章 /
// 0x1960 断线重连)。故不能见到一份快照就整个重建 —— 实测 0x1959 会在一局中途再下发
// (打 BOSS 时玩家又开了一次面板),整份重建会把已累积的操作流水与「已进 BOSS 战」
// 这类**只存在于 c2s 侧**的信息抹掉。同一局同一章就地更新;换局/换章才重建,
// 且流水与开始时刻照旧延续(玩家要看的是「这一趟」做了什么)。
// openRun 用一份全量快照开启/刷新一局。t 是这份快照的**消息时刻**(不是墙钟):
// 离线回放历史 pcap 时二者相差数天,结算时刻的比对只能用消息时刻。
func (st *trialState) openRun(c *trial.Challenge, t time.Time) {
	if st.run != nil && st.run.trialConfID == c.TrialConfID && st.run.chapterID == c.ChapterID {
		st.run.merge(c)
		return
	}
	old := st.run
	st.run = newRun(c, t)
	if old != nil {
		st.run.log = old.log
		st.run.startedAt = old.startedAt
	}
	// 开局前的查询(可选章节/初始金币)补进这一局:0x197d 早于开局,那时还没有 run 可挂。
	if st.query != nil {
		if len(st.query.Chapters) > 0 {
			st.run.chapters = st.query.Chapters
		}
		if st.run.coin == 0 && st.query.InitCoin > 0 {
			st.run.coin = st.query.InitCoin
		}
	}
}

// trialRun 是一局的实时镜像。
type trialRun struct {
	trialConfID uint32
	slotID      uint32
	chapterID   uint32
	nodeIndex   uint32
	coin        uint32
	chapters    []uint32
	effects     []uint32
	pet         *trial.Pet
	// initialFeatures 是局级的天生特性(#33),用于把宠物的特性拆成
	// 「天生」与「试炼获得」两组(见 trial.InitialFeatures)。
	initialFeatures trial.InitialFeatures
	selection       *trial.Selection
	bless           *trial.BlessSelection
	pending         *trial.PendingStep // 祝福的下一步(候选技能等)
	reward          *trial.Reward      // 待处理的节点奖励
	shop            []trial.ShopItem
	boss            bool          // 已进 BOSS 战
	active          bool          // 一局进行中
	result          *trial.Settle // 上一局结算(active=false 时才有)
	log             []trialLogEntry
	startedAt       time.Time
}

// trialLogEntry 是操作流水里的一条。
type trialLogEntry struct {
	ts     int64
	kind   string // node / refresh / battle / bless / reward / shop / boss / settle
	label  string
	ids    []uint32
	action uint32
	coin   uint32
}

// trialHist 是账号档案(0x1975)的精简聚合。
type trialHist struct {
	progress  *trial.Progress
	updatedAt time.Time
}

// handleTrial 分发试炼相关消息。与 handleEgg 一样不参与「消费即返回」的主分发,
// 单独过一遍 —— 试炼的 opcode 与宠物/场景两路都不重叠(唯一共享的 0x132c 战斗结束
// 通知双方各取所需,已在 handle 里对宠物路放行)。
func (p *Pipeline) handleTrial(m capture.Message, acc string) {
	st := p.acct(acc).trial()
	now := time.Now()
	s2c := m.Direction == gcp.S2C
	changed := false

	// 战斗进入通知(0x1316)**不属于试炼专属 opcode**,试炼外的战斗(野外/PVP)也走它。
	// 是否属于试炼由消息内的 grass_trial_battle_info 判定,故在这里先看一眼再放行:
	// 命中试炼就记下「遇到谁」,不命中则完全不动(让宠物路等其它消费者照常处理)。
	if m.Opcode == trial.OpBattleEnterNotify {
		p.recordTrialEncounter(m, acc, st)
	}

	switch {
	// —— 一局的全量快照 ——
	case s2c && (m.Opcode == trial.OpStartChallengeRsp || m.Opcode == trial.OpResumeRsp):
		// 开局/续局:流水重开(这是新的一趟);续局(断线重连)保留流水,那一趟还在打。
		if c := trial.ParseChallengeRsp(m.AppBody); c != nil {
			old := st.run
			st.run = newRun(c, m.Time)
			st.run.log = append(st.run.log, trialLogEntry{ts: m.Time.Unix(), kind: "start", label: "开始挑战"})
			if m.Opcode == trial.OpResumeRsp && old != nil {
				st.run.log = append(old.log, st.run.log...)
				st.run.startedAt = old.startedAt
			}
			changed = true
		}
	case s2c && m.Opcode == trial.OpChallengeDataSync:
		if c := trial.ParseChallengeSync(m.AppBody); c != nil {
			st.openRun(c, m.Time)
			changed = true
		}
	case s2c && m.Opcode == trial.OpGetInfoRsp:
		c, prog := trial.ParseGetInfoRsp(m.AppBody)
		if c != nil {
			st.openRun(c, m.Time)
			changed = true
		} else if st.run != nil && st.run.active {
			// 打开面板却没有进行中的一局:上一趟已经结束(BOSS 打完/放弃了)。
			// 这一步不能省 —— 只等 0x196a 结算通知的话,抓包漏掉它就一直显示「进行中」。
			st.run.active = false
			changed = true
		}
		if prog != nil {
			st.history = &trialHist{progress: prog, updatedAt: now}
			st.finishFromReview(prog)
			// 与 0x1975 同源,同样要补录见闻录 —— 少了这行,「登录进去」图鉴不会刷新。
			//
			// 0x1959 是**打开试炼面板**的响应(0x1958 触发),而 0x1975 要等真打一场
			// 试炼战斗才下发。实测本 pcap:登录 14:55:06 → 0x1959 在 14:55:10(4 秒后,
			// 带 167/127/98 只)→ 首条 0x1975 在 15:04:45(10 分钟后)。
			// 只挂 0x1975 的话,用户打开面板看到的是空白三张图,得打完一场才补上 ——
			// 而档案其实在登录几秒内就已经送到门口了。
			p.syncTrialEncounters(prog, acc, m.Time.Unix())
			changed = true
		}

	// —— 节点推进(c2s)与节点内容(s2c)——
	case !s2c && m.Opcode == trial.OpNextNodeReq:
		if n, ok := trial.ParseNextNodeReq(m.AppBody); ok && st.run != nil {
			st.run.chapterID, st.run.nodeIndex = n.ChapterID, n.NodeIndex
			// 推进到新节点:上一节点的选项/奖励/商店一并作废
			st.run.selection, st.run.bless, st.run.reward, st.run.pending = nil, nil, nil, nil
			st.run.shop = nil
			st.run.active = true
			st.run.log = append(st.run.log, trialLogEntry{ts: m.Time.Unix(), kind: "node",
				label: "推进节点", ids: []uint32{n.ChapterID, n.NodeIndex}})
			changed = true
		}
	case s2c && m.Opcode == trial.OpNextNodeRsp:
		nc := trial.ParseNextNodeRsp(m.AppBody)
		if st.run != nil && (nc.Selection != nil || nc.Bless != nil) {
			st.run.selection, st.run.bless = nc.Selection, nc.Bless
			changed = true
		}
	case s2c && m.Opcode == trial.OpSelectEventRsp:
		nc := trial.ParseSelectEventRsp(m.AppBody)
		if st.run != nil && (nc.Bless != nil || len(nc.Shop) > 0) {
			if nc.Bless != nil {
				st.run.bless = nc.Bless
				st.run.selection = nil
			}
			if len(nc.Shop) > 0 {
				st.run.shop = nc.Shop
			}
			changed = true
		}
	case s2c && m.Opcode == trial.OpNodeRefreshRsp:
		nr := trial.ParseNodeRefreshRsp(m.AppBody)
		if st.run != nil && nr.Selection != nil {
			st.run.selection = nr.Selection
			st.run.coin = nr.Coin
			st.run.log = append(st.run.log, trialLogEntry{ts: m.Time.Unix(), kind: "refresh",
				label: "刷新节点", ids: []uint32{nr.RefreshType}, coin: nr.Coin})
			changed = true
		}

	// —— 奖励:服务器通知(0x1967)→ 玩家处理(c2s 0x1968)→ 回包(0x1969)——
	case s2c && m.Opcode == trial.OpRewardNotify:
		if r := trial.ParseRewardNotify(m.AppBody); r != nil && st.run != nil {
			st.run.reward, st.run.coin = r, r.Coin
			changed = true
		}
	case !s2c && m.Opcode == trial.OpRewardReq:
		a := trial.ParseRewardReq(m.AppBody)
		if st.run != nil {
			st.run.log = append(st.run.log, trialLogEntry{ts: m.Time.Unix(), kind: "reward",
				label: rewardActionLabel(a.Action), ids: []uint32{a.RewardID}, action: a.Action})
			changed = true
		}
	case s2c && m.Opcode == trial.OpRewardRsp:
		u := trial.ParseRewardRsp(m.AppBody)
		if st.run != nil && (u.Pet != nil || u.Coin != 0) {
			st.run.apply(u)
			changed = true
		}

	// —— 祝福:选选项(0x1970)→ 确认(0x1972)——
	case !s2c && m.Opcode == trial.OpBlessSelectReq:
		if st.run != nil {
			st.run.log = append(st.run.log, trialLogEntry{ts: m.Time.Unix(), kind: "bless", label: "选择祝福"})
			changed = true
		}
	case s2c && m.Opcode == trial.OpBlessSelectRsp:
		u := trial.ParseBlessSelectRsp(m.AppBody)
		if st.run != nil && (u.Pending != nil || u.Pet != nil) {
			st.run.apply(u)
			changed = true
		}
	case !s2c && m.Opcode == trial.OpBlessConfirmReq:
		c := trial.ParseBlessConfirmReq(m.AppBody)
		if st.run != nil {
			ids := []uint32{c.Effect}
			if c.ChosenSkillID != 0 {
				ids = append(ids, c.ChosenSkillID)
			}
			st.run.log = append(st.run.log, trialLogEntry{ts: m.Time.Unix(), kind: "bless",
				label: blessEffectLabel(c.Effect), ids: ids})
			changed = true
		}
	case s2c && m.Opcode == trial.OpBlessConfirmRsp:
		u := trial.ParseBlessConfirmRsp(m.AppBody)
		if st.run != nil && (u.Pet != nil || u.Coin != 0 || u.Finished) {
			st.run.apply(u)
			if u.Finished {
				st.run.bless, st.run.pending = nil, nil
			}
			changed = true
		}

	// —— 商店(c2s 1978 / s2c 1979)——
	case s2c && m.Opcode == trial.OpShopBuyRsp:
		u := trial.ParseShopBuyRsp(m.AppBody)
		if st.run != nil && (u.Pet != nil || u.ShopItem != nil) {
			st.run.apply(u)
			if u.ShopItem != nil {
				for i := range st.run.shop {
					if st.run.shop[i].Index == u.ShopItem.Index {
						st.run.shop[i] = *u.ShopItem
					}
				}
				st.run.log = append(st.run.log, trialLogEntry{ts: m.Time.Unix(), kind: "shop",
					label: "购买", ids: []uint32{u.ShopItem.ItemID}, coin: u.Coin})
			}
			changed = true
		}

	// —— 换出战技能(0x197b)——
	case s2c && m.Opcode == trial.OpChangeSkillRsp:
		if u := trial.ParseChangeSkillRsp(m.AppBody); u.Pet != nil && st.run != nil {
			st.run.pet = u.Pet
			changed = true
		}

	// —— 开局前的查询(可选章节/本周词条/初始金币)——
	case s2c && m.Opcode == trial.OpQueryChallengeRsp:
		q := trial.ParseQueryChallengeRsp(m.AppBody)
		if len(q.Chapters) > 0 {
			st.query = &q
			if st.run != nil {
				st.run.chapters = q.Chapters
				if st.run.coin == 0 && q.InitCoin > 0 {
					st.run.coin = q.InitCoin
				}
			}
			changed = st.run != nil
		}

	// —— BOSS 战(c2s 0x196d)与一局结算(s2c 0x196a)——
	case !s2c && m.Opcode == trial.OpBossBattleEnter:
		if st.run != nil {
			st.run.boss = true
			st.run.log = append(st.run.log, trialLogEntry{ts: m.Time.Unix(), kind: "boss", label: "进入 BOSS 战"})
			changed = true
		}
	case s2c && m.Opcode == trial.OpSettleNotify:
		// 结算通知与「档案里最新战绩」(finishFromReview)可能都到,谁先谁后不定;
		// 已有结果就不再重复收尾,免得流水里出现两条。
		if s := trial.ParseSettleNotify(m.AppBody); s != nil && (st.run == nil || st.run.result == nil) {
			if st.run == nil {
				st.run = &trialRun{startedAt: now}
			}
			st.run.active = false
			st.run.boss = false
			st.run.result = s
			st.run.log = append(st.run.log, trialLogEntry{ts: m.Time.Unix(), kind: "settle",
				label: map[bool]string{true: "通关", false: "失败"}[s.Review.Victory],
				ids:   []uint32{s.Review.PetBaseID, s.Review.Duration}})
			changed = true
		}
	case s2c && m.Opcode == trial.OpAbandonRsp:
		if st.run != nil && st.run.active {
			st.run.active = false
			st.run.log = append(st.run.log, trialLogEntry{ts: m.Time.Unix(), kind: "settle", label: "放弃挑战"})
			changed = true
		}

	// —— 账号档案(0x1975,55KB 全量)——
	case s2c && m.Opcode == trial.OpProgressDataSync:
		if prog := trial.ParseProgressSync(m.AppBody); prog != nil {
			st.history = &trialHist{progress: prog, updatedAt: now}
			st.finishFromReview(prog)
			p.syncTrialEncounters(prog, acc, m.Time.Unix())
			changed = true
		}

	default:
		return
	}

	st.updated = now
	if changed {
		p.pushTrial(acc, st)
	}
}

// trial 返回(必要时创建)某账号的试炼状态。
func (a *acctState) trial() *trialState {
	if a.tr == nil {
		a.tr = &trialState{}
	}
	return a.tr
}

// newRun 从服务器的一局快照建一份镜像。t 是该快照的消息时刻(见 openRun 的说明)。
func newRun(c *trial.Challenge, t time.Time) *trialRun {
	return &trialRun{
		trialConfID: c.TrialConfID, slotID: c.SlotID, chapterID: c.ChapterID,
		nodeIndex: c.NodeIndex, coin: c.Coin, chapters: c.Chapters,
		effects: c.Effects, pet: c.Pet, selection: c.Selection,
		active: true, startedAt: t,
	}
}

// merge 用全量快照就地更新(服务器每次同步给的都是整局,不增量)。
func (r *trialRun) merge(c *trial.Challenge) {
	if c.TrialConfID != 0 {
		r.trialConfID = c.TrialConfID
	}
	if c.SlotID != 0 {
		r.slotID = c.SlotID
	}
	if c.ChapterID != 0 {
		r.chapterID = c.ChapterID
	}
	r.nodeIndex = c.NodeIndex
	r.coin = c.Coin
	if len(c.Chapters) > 0 {
		r.chapters = c.Chapters
	}
	if len(c.Effects) > 0 {
		r.effects = c.Effects
	}
	if c.Pet != nil {
		r.pet = c.Pet
	}
	// 天生特性整局不变,只在建局/恢复时取一次(#33 是局级字段,
	// apply 类的增量回包里没有它,故不在这里更新,免得被空值覆盖)。
	if len(c.InitialFeatures) > 0 {
		r.initialFeatures = c.InitialFeatures
	}
	if c.Selection != nil {
		r.selection = c.Selection
	}
	r.active = true
}

// apply 把「宠物被更新」类回包的结果并进镜像。
func (r *trialRun) apply(u trial.Update) {
	if u.Pet != nil {
		r.pet = u.Pet
	}
	if u.Coin != 0 {
		r.coin = u.Coin
	}
	if u.Pending != nil {
		r.pending = u.Pending
	}
	// 奖励处理完即清空待处理标记:服务器不再重发 0x1967
	if r.reward != nil && u.Pet != nil {
		r.reward = nil
	}
}

// rewardActionLabel 把 c2s 0x1968 的 action 翻成中文(取值经 26 条请求逐条对照推出)。
func rewardActionLabel(action uint32) string {
	switch action {
	case 0:
		return "融合到技能"
	case 1:
		return "学习新技能"
	case 2:
		return "直接收下"
	case 3:
		return "折算金币"
	case 4:
		return "刷新"
	default:
		return "处理奖励"
	}
}

// blessEffectLabel 把祝福的 effect 翻成中文(0=学技能、9=合并技能槽,其余待考)。
func blessEffectLabel(effect uint32) string {
	switch effect {
	case 0:
		return "选择技能"
	case 9:
		return "合并技能"
	default:
		return "确认祝福"
	}
}

// finishFromReview 用档案里最新一条战绩补上「一局已结束」。
//
// 为什么需要它:服务器**未必**发 0x196a 结算通知 —— 本样本打到抓包截断都没等到,
// 而 0x1959(最后一包 15:19:52,与整局结束同刻)的 progress_data 里已经多出这一局的
// 战绩。没有这一步,页面会一直显示「进行中」,要等 30 分钟的兜底才收尾。
//
// 判据:结算时刻不早于本局开始时刻,且战绩里的宠物形态与本局一致(同账号可能连开数局,
// 形态不同即不是这一趟)。
func (st *trialState) finishFromReview(prog *trial.Progress) bool {
	r := st.run
	if r == nil || !r.active || r.pet == nil || prog == nil || len(prog.Reviews) == 0 {
		return false
	}
	var newest *trial.ReviewRecord
	for i := range prog.Reviews {
		if newest == nil || prog.Reviews[i].SettleAt > newest.SettleAt {
			newest = &prog.Reviews[i]
		}
	}
	if int64(newest.SettleAt) < r.startedAt.Unix() || newest.PetBaseID != r.pet.BaseConfID {
		return false
	}
	r.active = false
	r.boss = false
	r.result = &trial.Settle{Review: *newest}
	r.log = append(r.log, trialLogEntry{ts: int64(newest.SettleAt), kind: "settle",
		label: map[bool]string{true: "通关", false: "失败"}[newest.Victory],
		ids:   []uint32{newest.PetBaseID, newest.Duration}})
	return true
}

// sweepTrial 兜底:长时间没有任何试炼消息即视为已离场(一局不会静默这么久)。
func (p *Pipeline) sweepTrial(now time.Time) {
	for acc, as := range p.accts {
		st := as.tr
		if st == nil || st.run == nil || !st.run.active {
			continue
		}
		if !st.updated.IsZero() && now.Sub(st.updated) > trialIdleTimeout {
			st.run.active = false
			p.pushTrial(acc, st)
		}
	}
}

// ---- 遇见记录 ----

// recordTrialEncounter 把一场试炼战斗里遇到的精灵记进数据库。
//
// 三道判据,缺一不记(宁可少记也别记错 —— 记错的进度比没有进度更误导):
//  1. 必须是试炼战斗(消息带 grass_trial_battle_info);
//  2. 必须有正在进行的试炼局(否则拿不到章节);
//  3. 必须推得出第几章 —— 归不到某张图上的记录毫无用处。
//
// 章节取自**当前局**而非战斗消息:后者不带章节信息。这也意味着若是战斗拖到
// 退出试炼之后才结束,可能因 active=false 而漏记 —— 可接受,遇到概率极低,
// 且漏记只是少一格,记错章节才会污染整张图的进度。
func (p *Pipeline) recordTrialEncounter(m capture.Message, acc string, st *trialState) {
	e := trial.ParseBattleEnter(m.AppBody)
	if e == nil || !e.Type.IsTrial() || len(e.PetBases) == 0 {
		return
	}
	if st.run == nil || !st.run.active {
		return
	}
	ch := chapterIdxOf(st.run)
	if ch == 0 {
		return
	}
	// 时间戳用**报文自带的时间**而非当前时刻:离线回放历史 pcap 补录时,
	// 写成入库时刻会让补出来的记录全挤在回放那一下(见 AddTrialEncounters)。
	if err := p.st.AddTrialEncounters(acc, ch, uint32(e.Type), e.PetBases, m.Time.Unix()); err != nil {
		log.Printf("记录试炼遇见失败(第%d章 %v): %v", ch, e.PetBases, err)
		return
	}
	for _, b := range e.PetBases {
		log.Printf("试炼遇见: 第%d章 %s战 petbase=%d", ch, e.Type.Label(), b)
	}
	// 通知前端重拉「遇见记录」。
	//
	// 只发信号、不带数据:一张图 786 只精灵,整份塞进 SSE 太重,而前端那头本来
	// 就有完整拉取逻辑(GET /api/trial/encounters),让它自己去取即可。
	// 与 eggs 频道同一路数(见 eggs.go:69)。
	//
	// 这条不加的话:遇见记录是读库的累积历史、不走 trial 快照,打完一局页面
	// 不会有任何变化,只有重进页面才看得到 —— 用过的人只会以为没记录上。
	p.srv.Hub().Broadcast("trial_enc", acc, map[string]any{"account": acc})
}

// syncTrialEncounters 用账号档案(0x1975)里的见闻录补录「已经遇见过」的精灵。
//
// 为什么需要它:0x1316 战斗通知只能记到**抓包期间发生的那几场**,而见闻录是
// 服务器保存的账号完整历史 —— 实测同一账号:抓包 17 场 vs 见闻录 292 只。
// 没有这条,用户装好之后看到的是近乎空白的三张图,得重新打一遍才填得满,
// 而实际上游戏里早就遇见过了。登录后档案一同步就能补齐(故不必回放历史 pcap)。
//
// 只补**缺的**:0x1975 是账号档案的全量推送,每次登录/同步都会重发一遍,
// 若逐条 AddTrialEncounters 会把 times 反复累加、last_seen 一直被推到当下,
// 「首次遇到」这个时间语义就废了。故先查已有记录,只写没见过的那些。
func (p *Pipeline) syncTrialEncounters(prog *trial.Progress, acc string, ts int64) {
	var added int
	for i := range prog.Logs {
		rec := &prog.Logs[i]
		ch := rec.ChapterOf()
		if ch == 0 || len(rec.DiscoveredIDs) == 0 {
			continue
		}
		have := p.st.TrialEncounters(acc, ch)
		var fresh []uint32
		for _, id := range rec.DiscoveredIDs {
			if _, ok := have[id]; !ok {
				fresh = append(fresh, id)
			}
		}
		if len(fresh) == 0 {
			continue
		}
		// 见闻录不带战斗类型,这里记 BattleNormal(0):
		// 它只保证「遇到过」,无法区分普通/首领/NPC。若之后再从战斗通知拿到
		// 更具体的类型,AddTrialEncounters 的 kind 取最大规则会把它升上去,
		// 不会被这里的 0 覆盖(见该函数的 SQL)。
		if err := p.st.AddTrialEncounters(acc, ch, uint32(trial.BattleNormal), fresh, ts); err != nil {
			log.Printf("补录试炼遇见失败(第%d章 %d 只): %v", ch, len(fresh), err)
			continue
		}
		added += len(fresh)
	}
	if added > 0 {
		log.Printf("见闻录补录: 新增 %d 只已遇见的精灵", added)
		p.srv.Hub().Broadcast("trial_enc", acc, map[string]any{"account": acc})
	}
}

// ---- 推送 ----

// pushTrial 组一份试炼快照,缓存并广播。
func (p *Pipeline) pushTrial(acc string, st *trialState) {
	payload := &server.TrialPayload{Account: acc, Ts: time.Now().Unix(), Active: false}
	// slot_id → 属性系:优先用档案里各槽位自带的 dam_type。
	// 不能按「slot_id - 1000 + 2」硬推:属性系枚举里**缺 7**(2..6 之后直接到 8),
	// 从 1005 起偏移量就错位了。
	damOf := map[uint32]int32{}
	if st.history != nil {
		for _, s := range st.history.progress.Slots {
			damOf[s.SlotID] = int32(s.DamType)
		}
	}
	if st.run != nil {
		payload.Active = st.run.active
		payload.Run = p.trialRunPayload(st.run, damOf)
	}
	if st.history != nil {
		payload.History = p.trialHistoryPayload(st.history.progress)
	}
	st.pushed = true
	p.srv.SetLastTrial(acc, payload)
	p.srv.Hub().Broadcast("trial", acc, payload)
}

// chapterIdxOf 推出「当前是第几章」(1 起)。
//
// 按服务器给的可选章节次序定位;列表为空时退回 chapter_id 末三位
// (协议恒为 3000/3001/3002,故 3000→第1章)。返回 0 表示推不出来。
//
// 单独抽成函数是因为两处要用:载荷的 chapterIdx,以及遇见记录要按章归档 ——
// 两处口径必须一致,否则记录会记到别的章上去。
func chapterIdxOf(r *trialRun) uint32 {
	for i, c := range r.chapters {
		if c == r.chapterID {
			return uint32(i) + 1
		}
	}
	if r.chapterID >= 3000 {
		return r.chapterID - 3000 + 1
	}
	return 0
}

// trialRunPayload 把一局镜像转成对外载荷。damOf 是 slot_id → 属性系的查表(见 pushTrial)。
func (p *Pipeline) trialRunPayload(r *trialRun, damOf map[uint32]int32) *server.TrialRun {
	out := &server.TrialRun{
		TrialID:   r.trialConfID,
		SlotID:    r.slotID,
		ChapterID: r.chapterID,
		NodeIndex: r.nodeIndex,
		Coin:      r.coin,
		Boss:      r.boss,
	}
	if d, ok := damOf[r.slotID]; ok {
		out.SlotName = p.db.SkillDamType(d) + "系"
	} else if r.slotID >= 1000 { // 档案还没到时的兜底:属性系枚举缺 7,只在 1000-1004 上成立
		if n := p.db.SkillDamType(int32(r.slotID-1000) + 2); n != "" {
			out.SlotName = n + "系"
		}
	}
	out.ChapterIdx = chapterIdxOf(r)
	// 静态配置(wiki):层类型 / 章节名 / 第 7 层候选阵容。
	// 协议只给编号,这些「这一层是什么、对面可能是谁」得查静态表才知道。
	// 缺数据时留空 —— 静态配置是可选的,不该因为它缺失就让整个接口失败。
	if f := p.db.TrialFloor(r.nodeIndex); f != gamedata.FloorUnknown {
		out.Floor, out.FloorLabel = string(f), f.Label()
		if f == gamedata.FloorNPC {
			for _, o := range p.db.TrialNPCOpponents(r.trialConfID, out.ChapterIdx) {
				op := server.TrialOpponent{ID: o.ID, Name: o.Name}
				for _, base := range o.Pets {
					pet := server.TrialOppPet{Base: base}
					if info, ok := p.db.PetBase(base); ok {
						pet.Name = info.Name
						if form := info.Form; form != "" {
							pet.Name += "_" + form
						}
					}
					// 头像按 petbase 查;不是每个形态都有图,缺图时留空由前端占位
					if im := p.db.PetImageByBase(base, false); im.Head != "" {
						pet.Img = im.Head
					}
					op.Pets = append(op.Pets, pet)
				}
				out.Opponents = append(out.Opponents, op)
			}
		}
	}
	out.ChapterName = p.db.TrialChapterName(out.ChapterIdx)
	out.Chapters = r.chapters
	out.Effects = r.effects
	if r.pet != nil {
		out.Pet = p.trialPetPayload(r.pet, r.initialFeatures)
	}
	if r.selection != nil {
		for _, e := range r.selection.Events {
			o := server.TrialOption{
				Slot: e.SlotIndex, Event: e.EventConfID, Reward: e.RewardID,
				Level: e.Level, EventCost: e.EventCost, RewardCost: e.RewardCost,
				Extra: e.ExtraRewards, Pool: e.RandomSkills, Used: e.UsedRewards,
			}
			// 事件对应哪只精灵协议不说,靠标注补(见 trialEventPet)。
			o.Pet = p.trialEventPet(e.EventConfID)
			p.trialOptionNames(&o)
			out.Options = append(out.Options, o)
		}
		out.RefreshCost = r.selection.TotalRefreshCost
	}
	if r.bless != nil {
		b := &server.TrialBless{Event: r.bless.EventConfID}
		for _, o := range r.bless.Options {
			b.Options = append(b.Options, o.OptionConfID)
		}
		if r.pending != nil {
			b.Effect = r.pending.Effect
			b.Candidates = r.pending.CandidateSkill
		}
		out.Bless = b
	}
	if r.reward != nil {
		out.Reward = &server.TrialReward{Event: r.reward.EventConfID, ID: r.reward.RewardID,
			Extra: r.reward.ExtraIDs, Coin: r.reward.Coin}
	}
	for _, it := range r.shop {
		s := server.TrialShopItem{Type: it.ItemType, ID: it.ItemID, Price: it.Price,
			Index: it.Index, Bought: it.IsPurchased}
		out.Shop = append(out.Shop, s)
	}
	if r.result != nil {
		out.Result = &server.TrialResult{
			Victory: r.result.Review.Victory, Duration: r.result.Review.Duration,
			PetBaseID: r.result.Review.PetBaseID, PetLevel: r.result.Review.PetLevel,
			SettleAt: int64(r.result.Review.SettleAt), Score: r.result.TotalScore,
		}
	}
	// 流水:只给最近若干条,按时间倒序(最新在上)
	logs := r.log
	if len(logs) > trialLogMax {
		logs = logs[len(logs)-trialLogMax:]
	}
	for i := len(logs) - 1; i >= 0; i-- {
		e := logs[i]
		out.Log = append(out.Log, server.TrialLogEntry{Ts: e.ts, Kind: e.kind,
			Label: e.label, IDs: e.ids, Action: e.action, Coin: e.coin})
	}
	return out
}

// trialEventPet 返回某节点事件(event_conf_id)对应的精灵对手;查不到返回 nil(前端占位)。
//
// 两层解析:
//  1. **官方 GRASS_TRIAL_EVENT_CONF**(gen_trial_official.py 落表,见
//     gamedata.TrialEventPetBase):普通遭遇/首领事件直接给出精灵 —— 与协议同源,
//     免人工标注。NPC 整队(300xxx+)/祝福/商人等事件不在表里(返回 0),落到下一步。
//  2. 众包标注兜底:官方表外的遭遇(新版本/漏解包),玩家照游戏画面标 kind=event,
//     名字是精灵形态全名,再经 PetByName 反查成形态。标注在 DB 里,审核通过即生效;
//     每次组载荷查一次库(一个节点 3 条而已),不做缓存。官方表内的事件被标注了
//     **不覆盖官方** —— 官方与协议同源,玩家标注只在官方缺失时才有意义。
func (p *Pipeline) trialEventPet(eventConfID uint32) *server.TrialOppPet {
	if base := p.db.TrialEventPetBase(eventConfID); base != 0 {
		return trialOppPetOf(p.db, base)
	}
	a, ok := p.st.ApprovedAnnotation("event", int64(eventConfID))
	if !ok {
		return nil
	}
	base, _, ok := p.db.PetByName(a.Name)
	if !ok {
		return nil // 标注的名字对不上任何形态(多半是 wiki 别名),宁缺勿错
	}
	return trialOppPetOf(p.db, base)
}

// trialOppPetOf 按 petbase 组装事件对手精灵(形态全名 + 头像);查不到元数据返回 nil。
func trialOppPetOf(db *gamedata.DB, base uint32) *server.TrialOppPet {
	info, ok := db.PetBase(base)
	if !ok {
		return nil
	}
	pet := &server.TrialOppPet{Base: base, Name: info.Name}
	if info.Form != "" {
		pet.Name += "_" + info.Form
	}
	if im := db.PetImageByBase(base, false); im.Head != "" {
		pet.Img = im.Head
	}
	return pet
}

// trialOptionNames 给一个事件卡片里出现的 id 补中文名(技能 + 能确定的那条特性)。
//
// 技能:按 id 查 skills.json,融合不改 base_skill_id 故融合态同样查得到。
//
// 特性:没有内置名表,但**标出精灵之后就有了** —— 池里那条 288xxx 就是这只精灵
// 自身的特性,拿形态去查「精灵 → 特性」表即得(见 gamedata.FeatureNameOfBase)。
// 只在池里**恰好一个**特性 id 时才绑:一只精灵只有一个自身特性,出现多条说明
// 我们对池的理解是错的,这时宁可都不给名字。
func (p *Pipeline) trialOptionNames(o *server.TrialOption) {
	ids := make([]uint32, 0, len(o.Pool)+len(o.Extra)+1)
	ids = append(ids, o.Reward)
	ids = append(ids, o.Pool...)
	ids = append(ids, o.Extra...)
	var feats []uint32
	for _, id := range ids {
		if id == 0 {
			continue
		}
		if trial.IsFeatureID(id) {
			feats = append(feats, id)
			continue
		}
		if n := p.db.SkillName(id); n != "" {
			if o.Names == nil {
				o.Names = map[uint32]string{}
			}
			o.Names[id] = n
		}
	}
	if len(feats) != 1 || o.Pet == nil {
		return
	}
	if n := p.db.FeatureNameOfBase(o.Pet.Base); n != "" {
		if o.Names == nil {
			o.Names = map[uint32]string{}
		}
		o.Names[feats[0]] = n
	}
}

// trialPetPayload 把试炼宠物副本转成对外载荷:名称/头像走 gamedata 按 base_conf_id 查。
// initial 是局级的天生特性(#33),用来把 Features 拆成「天生」与「试炼获得」两组。
func (p *Pipeline) trialPetPayload(tp *trial.Pet, initial trial.InitialFeatures) *server.TrialPet {
	// 异色:试炼带的是玩家自己的精灵,异色/炫彩原样带进试炼。
	// 判据与宠物主流程一致(bit0=异色、bit3=炫彩),见 internal/pet/model.go。
	shiny := tp.Mutation&1 != 0
	out := &server.TrialPet{
		Gid: tp.Gid, Name: tp.Name, Level: tp.Level,
		HP: tp.HP, MaxHP: tp.MaxHP, Energy: tp.EnergyCeil, Growth: tp.Growth,
		Features: tp.Features, Shards: tp.Shards, Equipped: tp.Equipped,
		// 外观:异色/炫彩是玩家自己精灵的属性,进试炼原样带着 ——
		// 前端据此显示异色标记并渲染炫彩色卡(见 web/src/components/badges.jsx)。
		Shiny: shiny, Colorful: tp.Mutation&8 != 0,
		GlassType: tp.GlassType, GlassValue: tp.GlassValue,
	}
	// 天生 vs 试炼获得:两边都判,不按下标切片(见 InitialFeatures 的说明)。
	// initial 缺失时(老快照/字段不存在)两组都留空 —— **不猜**,
	// 标错比不标更糟,用户会把「天生」当成确定的事实。
	if len(initial) > 0 {
		// 去重保序:acquired 是累积追加的,同一个 id 可能出现多次
		// (实测 features 里 288001 就有两个 —— 它是「已获得」的流水而非集合)。
		seen := map[uint32]bool{}
		for _, f := range tp.Features {
			if f == 0 || seen[f] {
				continue
			}
			seen[f] = true
			if initial.Has(f) {
				out.InnateFeatures = append(out.InnateFeatures, f)
			} else {
				out.GainedFeatures = append(out.GainedFeatures, f)
			}
		}
		// 天生特性的名字:用 wiki 的「精灵 → 特性」表桥接(见 gamedata.FeatureNameOfBase)。
		// **只有恰好一条天生特性时才绑** —— 表是「一只精灵 → 一个特性名」的口径,
		// 出现多条天生时无从判断该把名字给谁,猜一个不如都不给。
		if len(out.InnateFeatures) == 1 {
			if n := p.db.FeatureNameOfBase(tp.BaseConfID); n != "" {
				out.FeatureNames = map[uint32]string{out.InnateFeatures[0]: n}
			}
		}
	}
	// 取图时按异色取(判据见上):外观标志只存在于内嵌 PetData 里,
	// 取图时不看,异色精灵就一律显示普通头像。
	if info, ok := p.db.PetBase(tp.BaseConfID); ok {
		out.Species = info.Name
		if img := p.db.PetImageByBase(tp.BaseConfID, shiny); img.Head != "" {
			out.Img = img.Head
		}
	}
	if out.Species == "" && tp.ConfID != 0 {
		out.Species = p.db.Species(tp.ConfID)
		if img := p.db.PetImage(tp.ConfID, shiny); img.Head != "" {
			out.Img = img.Head
		}
	}
	if out.Name == "" {
		out.Name = out.Species
	}
	for _, s := range tp.Skills {
		sk := server.TrialSkill{
			ID: s.BaseID, Power: s.Power, Cost: s.EnergyCost,
			Fusion: s.FusionCount, Slot: s.SlotPos, Merged: s.Merged,
		}
		// 技能名按 base_skill_id 查。融合**不会**改变 base_skill_id(只改威力与
		// fusion_count),故融合态技能同样能查到名。查不到即资料站未收录,
		// name 缺失、前端回退显示 id。
		if n := p.db.SkillName(s.BaseID); n != "" {
			sk.Name = n
		}
		out.Skills = append(out.Skills, sk)
	}
	return out
}

// trialHistoryPayload 把账号档案聚合成对外载荷。
//
// 聚合而非透传:progress_data 一次 55KB(250 条战绩 + 1586 个节点记录),每次同步都整包
// 广播太重,而页面真正要的是「胜率 / 常用形态 / 各系进度 / 见闻录」这几个汇总视角。
func (p *Pipeline) trialHistoryPayload(prog *trial.Progress) *server.TrialHistory {
	out := &server.TrialHistory{ChallengeInc: prog.ChallengeInc, Cleared: prog.ClearedTrials}
	for _, r := range prog.Reviews {
		out.Total++
		if r.Victory {
			out.Wins++
		}
	}
	// 最近战绩:按结算时间倒序取前 N 条
	recent := make([]trial.ReviewRecord, len(prog.Reviews))
	copy(recent, prog.Reviews)
	sort.Slice(recent, func(i, j int) bool { return recent[i].SettleAt > recent[j].SettleAt })
	if len(recent) > trialReviewKeep {
		recent = recent[:trialReviewKeep]
	}
	for _, r := range recent {
		name := ""
		if info, ok := p.db.PetBase(r.PetBaseID); ok {
			name = info.Name
		}
		out.Recent = append(out.Recent, server.TrialReview{
			SettleAt: int64(r.SettleAt), PetBaseID: r.PetBaseID, PetName: name,
			PetLevel: r.PetLevel, TrialID: r.TrialID, Victory: r.Victory,
			Duration: r.Duration, SlotID: r.SlotID, Mutation: r.Mutation,
		})
	}
	// 常用形态:按 petbase 计数取前 N
	counts := map[uint32]int{}
	for _, r := range prog.Reviews {
		counts[r.PetBaseID]++
	}
	type kv struct {
		id uint32
		n  int
	}
	var list []kv
	for id, n := range counts {
		list = append(list, kv{id, n})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].n != list[j].n {
			return list[i].n > list[j].n
		}
		return list[i].id < list[j].id
	})
	if len(list) > trialTopPets {
		list = list[:trialTopPets]
	}
	for _, e := range list {
		name := ""
		img := ""
		if info, ok := p.db.PetBase(e.id); ok {
			name = info.Name
			if im := p.db.PetImageByBase(e.id, false); im.Head != "" {
				img = im.Head
			}
		}
		out.TopPets = append(out.TopPets, server.TrialTopPet{PetBaseID: e.id, Name: name, Img: img, Count: uint32(e.n)})
	}
	// 各系槽位进度(18 个属性系,每个 3 个难度)
	for _, s := range prog.Slots {
		out.Slots = append(out.Slots, server.TrialSlot{
			SlotID: s.SlotID, DamType: s.DamType,
			DamName: p.db.SkillDamType(int32(s.DamType)),
			Cleared: uint32(len(s.ClearedIDs)),
		})
	}
	for _, l := range prog.Logs {
		out.Logs = append(out.Logs, server.TrialLogBook{
			LogConfID: l.LogConfID, Discovered: l.Discovered,
			Total: l.Total, Unlocked: l.Unlocked,
		})
	}
	return out
}
