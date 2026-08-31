package pipeline

import (
	"sort"
	"time"

	"github.com/whoisnian/rocom-capture/internal/capture"
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
	selection   *trial.Selection
	bless       *trial.BlessSelection
	pending     *trial.PendingStep // 祝福的下一步(候选技能等)
	reward      *trial.Reward      // 待处理的节点奖励
	shop        []trial.ShopItem
	boss        bool          // 已进 BOSS 战
	active      bool          // 一局进行中
	result      *trial.Settle // 上一局结算(active=false 时才有)
	log         []trialLogEntry
	startedAt   time.Time
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
	// 第几章:按服务器给的可选章节次序;列表为空时退回 chapter_id 末三位(3000→第1章)
	for i, c := range r.chapters {
		if c == r.chapterID {
			out.ChapterIdx = uint32(i) + 1
		}
	}
	if out.ChapterIdx == 0 && r.chapterID >= 3000 {
		out.ChapterIdx = r.chapterID - 3000 + 1
	}
	out.Chapters = r.chapters
	out.Effects = r.effects
	if r.pet != nil {
		out.Pet = p.trialPetPayload(r.pet)
	}
	if r.selection != nil {
		for _, e := range r.selection.Events {
			out.Options = append(out.Options, server.TrialOption{
				Slot: e.SlotIndex, Event: e.EventConfID, Reward: e.RewardID,
				Level: e.Level, EventCost: e.EventCost, RewardCost: e.RewardCost,
				Extra: e.ExtraRewards,
			})
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

// trialPetPayload 把试炼宠物副本转成对外载荷:名称/头像走 gamedata 按 base_conf_id 查。
func (p *Pipeline) trialPetPayload(tp *trial.Pet) *server.TrialPet {
	out := &server.TrialPet{
		Gid: tp.Gid, Name: tp.Name, Level: tp.Level,
		HP: tp.HP, MaxHP: tp.MaxHP, Energy: tp.EnergyCeil, Growth: tp.Growth,
		Features: tp.Features, Shards: tp.Shards, Equipped: tp.Equipped,
	}
	if info, ok := p.db.PetBase(tp.BaseConfID); ok {
		out.Species = info.Name
		if img := p.db.PetImageByBase(tp.BaseConfID, false); img.Head != "" {
			out.Img = img.Head
		}
	}
	if out.Species == "" && tp.ConfID != 0 {
		out.Species = p.db.Species(tp.ConfID)
		if img := p.db.PetImage(tp.ConfID, false); img.Head != "" {
			out.Img = img.Head
		}
	}
	if out.Name == "" {
		out.Name = out.Species
	}
	for _, s := range tp.Skills {
		out.Skills = append(out.Skills, server.TrialSkill{
			ID: s.BaseID, Power: s.Power, Cost: s.EnergyCost,
			Fusion: s.FusionCount, Slot: s.SlotPos, Merged: s.Merged,
		})
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
