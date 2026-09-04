package pipeline

import (
	"sort"
	"strconv"
	"time"

	"github.com/whoisnian/rocom-capture/internal/gamedata"
	"github.com/whoisnian/rocom-capture/internal/pet"
	"github.com/whoisnian/rocom-capture/internal/scene"
	"github.com/whoisnian/rocom-capture/internal/server"
)

// ---- 实时地图的野生宠物图层(见 docs/data.md 3.5)----
//
// 大世界的野生宠物是普通 NPC 实体(ActorInfo),个体属性就挂在 npc_base 上:身高、体重、嗓音、
// 变异(mutation_type)、炫彩(glass_info)——**捕捉后这些原样进 PetData**(2026-08-02 用捕捉
// 珀尔鼬/友爱天天的 pcap 逐字段核对过),所以丢球之前就能筛。实体来源与星星同两处:进场景/传送后的周边快照(0x014a)
// 与移动中随 AOI 补发的动作通知(0x0413/0x0414 的 actor_enter)。
//
// **下发范围**:普通野生宠是水平 80m 的圆(高度不计,走到 88-90m 才收到 actor_leave),首领 150/200m;
// 跨进边界到实体真正下发另有滞后(中位 6.3s、最大 47s),故「附近没标记」不等于「附近没有」。
// 见 docs/data.md 3.7。
//
// 只跟踪**值得跑一趟**的几类(其余野生宠满地都是,全画出来只会糊住地图):
//   - 炫彩(glass_info.glass_type != GT_NULL,等价于 mutation_type 的 MDT_GLASS 位);
//   - 异色(mutation_type 的 MDT_SHINING 位);
//   - 污染(mutation_type 的 MDT_CHAOS 家族);这类丢球即进战斗,打完才解除污染。
//   - 体型:体重百分位 >= 98「大块头」、<= 2「小不点」(MEDAL_TASK_CONF 的奖牌判定,
//     与宠物列表/事件页的「W xx%」同一口径,见 pet.SizePercentile)。
//   - 声音:voice >= 96 婉转声、<= -96 粗嗓门(对应奖牌百分位 [98,100]/[0,2] 的边界)。
//
// 炫彩不另按 mutation 位判:两者严格等价(全部 pcap 363 只变异宠物零反例),
// 用 glass_info 还能顺带说出是哪一种炫彩。MDT_VACANT(空缺态)客户端自己都不出变异标,忽略。
//
// **标记位置是实体下发时的位置,之后不再更新**:野生宠的 AI 跑在客户端(NpcBase.is_server_ai
// 为假),它在刷新点附近的溜达根本不过网——16 份 pcap 里 server_move 只出现 1 次、client_move
// 全是玩家 avatar,没有一条属于野生宠。故位置≈刷新点,误差是它自己绕的那几米。
const (
	// 声音阈值(PET_GLOBAL_CONFIG.pet_voice_low/high = ±100):voice >= 96 婉转声、
	// <= -96 粗嗓门(对应奖牌百分位 [98,100]/[0,2] 的边界)。
	wildVoiceHigh = 96
	wildVoiceLow  = -96
	// 出 AOI 后「最后所见」的灰点还留多久(超时由 pushWilds 顺手丢弃)。取 4 小时是为了
	// 让灰点当作「本次上线在这一带见过什么」的备忘:野生宠刷新周期远长于几分钟,隔一阵回来
	// 多半还在。灰点不会无限堆积——TTL 到了自己过期,自己捉走的当场撤,**换场景整份作废**
	// 是兜住上限的那道闸(否则跑一天会攒下几百个属于别的场景的灰点,见 resetWilds)。
	wildStaleTTL = 4 * time.Hour
)

// wildPet 是一只被跟踪的野生宠物(当前场景会话内)。
type wildPet struct {
	actorID    uint64
	cfgID      int32
	lv         int32
	height     int32
	weight     int32
	voice      int32
	mutation   int32
	glassType  int32
	glassValue int32
	pos        scene.Position
	seenAt     time.Time // 最近一次确认它还在 AOI 里的时刻
	left       bool      // 已离开 AOI:标记转为「最后所见」,置灰显示,wildStaleTTL 后丢弃
}

// wildTracker 是一个连接在**当前场景会话**内的野生宠物观测态。**换场景即重建**(res 变了,
// 那些坐标属于另一张图,见 resetWilds);同场景内传送只把标记置灰、不重建。
//   - pets:稀有类别(异色/炫彩/污染/奖牌四件套)的个体,前端按图层描边、可点资料卡;
//   - all:其余普通野生宠,仅作「全部野生」图层的小头像点,不描边、不弹卡、不参与通知。
//
// 两者键不重叠:同一 actor_id 若命中 wildKinds 进 pets,否则进 all。
type wildTracker struct {
	pets map[uint64]*wildPet
	all  map[uint64]*wildPet
	res  int32
}

func newWildTracker(res int32) *wildTracker {
	return &wildTracker{pets: map[uint64]*wildPet{}, all: map[uint64]*wildPet{}, res: res}
}

// wildKinds 返回该实体命中的类别键;一只可同时命中多类。前端 WILD_LAYERS 的每个开关
// 覆盖其中一个或多个(异色/炫彩合成一个开关),悬浮提示则仍按这里的细粒度分开说。
func (p *Pipeline) wildKinds(a scene.NpcActor) []string {
	var out []string
	if a.GlassType != gamedata.GlassNull {
		out = append(out, "colorful")
	}
	if a.IsShiny() {
		out = append(out, "shiny")
	}
	if a.IsPolluted() {
		out = append(out, "pollution")
	}
	// 体重百分位:>=98 大块头、<=2 小不点(MEDAL_TASK_CONF 的奖牌边界;注意配置那栏
	// 写的是「身高」,实测按**体重**判,见 docs/data.md 3.5)。形态范围缺失时判不了,跳过。
	if base, ok := p.db.NpcPetBase(uint32(a.CfgID)); ok {
		if info, ok := p.db.PetBase(base); ok {
			if pct := pet.SizePercentile(float64(a.Weight)/1000,
				float64(info.WeightLow)/1000, float64(info.WeightHigh)/1000); pct != nil {
				switch {
				case *pct >= 98:
					out = append(out, "big")
				case *pct <= 2:
					out = append(out, "small")
				}
			}
		}
	}
	// 声音:>=96 婉转声、<=-96 粗嗓门(对应奖牌百分位 [98,100]/[0,2] 的边界)。
	switch {
	case a.Voice >= wildVoiceHigh:
		out = append(out, "high")
	case a.Voice <= wildVoiceLow:
		out = append(out, "low")
	}
	return out
}

// resetWilds 换场景或传送时结算野生宠标记:同场景内传送只置灰,**真换场景则整份作废**。
//
// 关键事实(2026-09-01 实测 PCAPdroid_01_9月_19_12_52.pcap 的 0x015c):
// 传送后 3 秒内 actor_enter 8 条、**actor_leave 0 条** —— 服务器传送时不为旧实体
// 补发离开事件(客户端自己清空 AOI)。这一步只能由我们代劳,没有 leave 可等。
//
// 两种情形分得很清,依据是**场景 res 变没变**:
//   - 同场景内传送(`0x015c` 的 `to_scene_res_cfg_id` 与当前 res 相同:大地图传送点、
//     营地魔力之源之间跳):旧标记的世界坐标仍属这张底图,玩家走回去还能再遇上,只是
//     此刻不在 AOI 里了 —— 这与「走远了出 AOI」是同一回事,故转成「最后所见」置灰保留。
//   - 真换场景:那些坐标属于另一张图,投影到新底图上毫无意义。留着只会越攒越多
//     (灰点 4 小时才过期,跑一天能攒几百个),故整份作废,前端随即清屏。
//
// 置灰时**不动 seenAt**:4 小时 TTL 仍从「最后一次确认它在」算起,不因传送续命,
// 灰点最终自己过期(见 wildStaleTTL 与 pushWilds 的清理)。
//
// 只管野生宠物标记。涂地跟踪的实体(connState.wildSeen)由调用方各自处理,本函数不碰
// —— 两者失效规则不同(涂地是「扫过」的凭据,不随传送恢复)。
func (p *Pipeline) resetWilds(conn, acc string, res int32, now time.Time) {
	cs := p.conn(conn)
	if ts := cs.wilds; ts != nil && ts.res == res {
		// 同场景内传送:只置灰。稀有(pets)与普通(all)两个通道都要置灰,
		// 免得出现「稀有还留着、普通没了」的不一致。
		for _, w := range ts.pets {
			w.left = true
		}
		for _, w := range ts.all {
			w.left = true
		}
	} else { // 真换了场景(或首次进场景):上个场景的实体与坐标一律作废
		cs.wilds = newWildTracker(res)
	}
	// 换场景/传送必须**立即**广播(标记要当场转为置灰态或清屏,不能等合并窗口),
	// 顺带把待发的变更一并消费掉 —— 否则紧接着还会再补发一次,白多一份全量。
	cs.wildsDirtyAt = time.Time{}
	p.pushWilds(conn, acc, now)
}

// observeWilds 收下一个实体快照/AOI 通知里的野生宠物:匹配的进入即跟踪,离开转为「最后所见」。
// snapshot=true 表示来自进场景快照(0x014a),否则是 AOI 动作通知(0x0413/0x0414)。
func (p *Pipeline) observeWilds(conn, acc string, body []byte, now time.Time, snapshot bool) {
	cs := p.conns[conn]
	if cs == nil || cs.wilds == nil {
		return
	}
	ts := cs.wilds
	changed := false

	actors := scene.ParseActorEnter(body)
	if snapshot {
		actors = scene.ParseSceneActors(body)
	}
	for _, a := range actors {
		// 涂地跟的是**全部**野生宠实体(见 paintSeen):任何一只下发,都证明玩家到它之间
		// 这条线上的东西已经到手了,与它稀不稀有无关。下面的稀有/普通双通道才是地图标记那一套。
		// **首领除外**:它的下发距离是 150/200m 那两档(普通野生宠 80m,见 docs/data.md 3.7),
		// 拿它当凭据会把中间那段其实没下发过普通野生宠的地方也涂上。
		// 同理只认**可丢球捕捉**的(同稀有通道的第二道闸):家园宠、剧情/活动 NPC 也带身高体重,
		// 但它们不是野外刷出来的,下发规则未知,不该拿来当「这条线扫过了」的凭据。
		catchable := false
		if _, ok := p.db.NpcPetBase(uint32(a.CfgID)); ok {
			catchable = true
		}
		boss := p.db.IsNpcBoss(uint32(a.CfgID))
		// 涂地凭据:可捕捉且非首领(首领下发距离不同,见上注)。
		if a.IsWildPet() && catchable && !boss {
			if cs.wildSeen == nil {
				cs.wildSeen = map[uint64]scene.Position{}
			}
			cs.wildSeen[a.ActorID] = a.Pos
		}
		// 地图标记分两通道(与涂地凭据不同:稀有通道不排除首领——首领若是稀有个体照样标,
		// 见 IsNpcBoss 注释「地图标记不受影响」):
		//   - pets(稀有):IsWildPet && catchable && wildKinds 非空;描边/资料卡/通知。
		//   - all(普通):上一条不成立、但 IsWildPet && catchable && !boss;只作小头像点。
		// 首领若不是稀有(无 wildKinds)则两通道都不进——它不是「周围野生精灵」该列的。
		rare := a.IsWildPet() && catchable && len(p.wildKinds(a)) > 0
		var bucket map[uint64]*wildPet
		switch {
		case rare:
			bucket = ts.pets
		case a.IsWildPet() && catchable && !boss:
			bucket = ts.all
		default:
			continue
		}
		if old, ok := bucket[a.ActorID]; ok { // 重新进入 AOI:复活标记
			old.pos, old.seenAt, old.left = a.Pos, now, false
			changed = true
			continue
		}
		bucket[a.ActorID] = &wildPet{
			actorID: a.ActorID, cfgID: a.CfgID, lv: a.Lv,
			height: a.Height, weight: a.Weight, voice: a.Voice,
			mutation: a.Mutation, glassType: a.GlassType, glassValue: a.GlassValue,
			pos: a.Pos, seenAt: now,
		}
		changed = true
	}

	// 离开 AOI:不立刻抹掉,置灰保留一段时间,免得刚瞥见一只稀有的、一转身标记就没了;
	// 超过 wildStaleTTL 由 pushWilds 清理。**自己捉走的另算**(见下),那种要当场撤。
	for _, id := range scene.ParseActorLeave(body) {
		delete(cs.wildSeen, id) // 出了视野就不再从它身上涂(走廊只代表「此刻确实看得见」)
		if w, ok := ts.pets[id]; ok && !w.left {
			w.left = true
			changed = true
		}
		if w, ok := ts.all[id]; ok && !w.left {
			w.left = true
			changed = true
		}
	}

	// 自己丢球捉走的:实体不是「走远了」而是真没了,标记当场撤掉,不留灰点。
	// (捕捉失败的 act 同样会来,带 is_catch_success=false,ParseCaughtByThrow 已挡掉。)
	for _, id := range scene.ParseCaughtByThrow(body) {
		delete(cs.wildSeen, id)
		if _, ok := ts.pets[id]; ok {
			delete(ts.pets, id)
			changed = true
		}
		if _, ok := ts.all[id]; ok {
			delete(ts.all, id)
			changed = true
		}
	}

	// 新看到的宠物 = 新的方向,当场涂一次(玩家可能站着不动,等不到下一个移动包)。
	p.paintSeen(conn, acc, cs.res, nil)

	if changed {
		p.markWildsDirty(conn, now)
	}
}

// onBattleFinish 处理战斗结算(0x132c):被污染的野生宠丢球只是开战,结果要等这里
// (见 docs/data.md 3.5)。捉走与打死对标记是一回事——那儿已经没这只了,当场撤掉;
// 战斗期间它早已离开 AOI 被置灰,这一步把灰点也抹掉。打输/它逃跑则不在此列,灰点照旧。
func (p *Pipeline) onBattleFinish(conn, acc string, body []byte, now time.Time) {
	cs := p.conns[conn]
	if cs == nil || cs.wilds == nil {
		return
	}
	changed := false
	for _, id := range scene.ParseBattleGoneNpcs(body) {
		if _, ok := cs.wilds.pets[id]; ok {
			delete(cs.wilds.pets, id)
			changed = true
		}
		if _, ok := cs.wilds.all[id]; ok {
			delete(cs.wilds.all, id)
			changed = true
		}
	}
	// 新看到的宠物 = 新的方向,当场涂一次(玩家可能站着不动,等不到下一个移动包)。
	p.paintSeen(conn, acc, cs.res, nil)

	if changed {
		p.markWildsDirty(conn, now)
	}
}

// wildsDebounce 是野生宠物图层广播的合并窗口。
//
// 取值依据(实测一份 604 秒 pcap,0x0414 共 11282 条、其中 2878 条带实体进出):
//
//	 50ms → 407 次(14.1%)   200ms → 260 次(9.0%)
//	100ms → 356 次(12.4%)   500ms → 199 次(6.9%)
//	150ms ≈ 300 次          1s    → 146 次(5.1%)
//
// 边际收益递减明显,50ms 就已经吃掉大部分抖动。取 150ms 是**兼顾提醒及时性**:
// 稀有宠出现提醒(见 web/src/pages/map/wildNotify.js)走的就是这条推送,窗口开太大
// 会「玩家都跑过去了才响」。150ms 人眼无感,却已合并掉约九成突发。
const wildsDebounce = 150 * time.Millisecond

// markWildsDirty 记下「该连接的野生宠有变更,待广播」。真正的广播由 flushDirtyWilds
// 在窗口到点后补发 —— 突发时一连几十条实体进出只发一次。
// now 是消息时刻(非墙钟),离线回放才不会把窗口判成早已超时。
func (p *Pipeline) markWildsDirty(conn string, now time.Time) {
	cs := p.conn(conn)
	if cs == nil {
		return
	}
	// 已有待广播的,保留**最早**那个时刻:窗口从第一次变更起算,
	// 否则持续突发会不断把时刻推后,永远发不出去(饿死)。
	if cs.wildsDirtyAt.IsZero() {
		cs.wildsDirtyAt = now
	}
}

// flushDirtyWilds 广播所有「窗口已到」的待发变更。由 handle 在每条消息后调用:
// 没有独立的定时器,故完全跟着消息走 —— 离线回放几秒跑完一份 pcap 时,窗口按包内
// 时间戳判定,行为与实时抓包一致(不会因为墙钟只过了几秒就一次都不发)。
func (p *Pipeline) flushDirtyWilds(now time.Time) {
	for conn, cs := range p.conns {
		if cs.wildsDirtyAt.IsZero() || now.Sub(cs.wildsDirtyAt) < wildsDebounce {
			continue
		}
		cs.wildsDirtyAt = time.Time{}
		acc := p.connAccount[conn]
		if acc == "" {
			continue
		}
		p.pushWilds(conn, acc, now)
	}
}

// flushAllDirtyWilds 无条件补发所有待发的野生宠变更(不看窗口)。
// 只在离线回放结束时用一次:此后不会再有消息推进窗口,不补发就永远丢了。
func (p *Pipeline) flushAllDirtyWilds() {
	// now 必须取**消息时间轴**上的时刻(最后一条消息),不能用 time.Now():
	// 离线回放的包时间是几小时前,用墙钟会让 pushWilds 的 TTL 判定(now - seenAt)
	// 算出几小时,把整份列表当成过期灰点全删掉 —— 补发反而清空了地图。
	now := p.lastMsgAt
	if now.IsZero() {
		now = time.Now()
	}
	for conn, cs := range p.conns {
		if cs.wildsDirtyAt.IsZero() {
			continue
		}
		cs.wildsDirtyAt = time.Time{}
		acc := p.connAccount[conn]
		if acc == "" {
			continue
		}
		p.pushWilds(conn, acc, now)
	}
}

// pushWilds 缓存并广播当前场景的野生宠物标记(顺带清理过期的「最后所见」)。
//
// 调用方只有两处:markWildsDirty 攒够窗口后的补发(常规路径),以及换场景/传送时的
// resetWilds —— 后者必须**立即**广播(玩家已经到了新场景,旧标记要么当场清掉、要么
// 转为灰点,不能等窗口)。延迟一律走 markWildsDirty,别在这里节流。
//
// 为什么不逐条广播:实体进出 AOI 在跑动时是**突发**而非低频 —— 实测一份 604 秒的
// pcap 里 0x0414 有 11282 条,其中 2878 条带实体进出(约 4.8 次/秒);而同一只宠
// 反复进出占 53.6%(1441 次进入只涉及 669 个实体)。每次都把整份视野列表(平均 73
// 只宠、约 8.7KB)重发一遍,就是 41.5 KB/s 的下行与每秒数百个标记的重建 —— 前端
// 地图卡顿的根因。攒 150ms 再发可降到约 10%(见 wildsDebounce 的实测数据)。
//
// now 取**消息时刻**而非 time.Now():离线回放的包时间是几小时前的,用挂钟一比就全过期了。
func (p *Pipeline) pushWilds(conn, acc string, now time.Time) {
	cs := p.conns[conn]
	if cs == nil || cs.wilds == nil {
		return
	}
	ts := cs.wilds
	marks := []server.WildMark{}
	for id, w := range ts.pets {
		if w.left && now.Sub(w.seenAt) > wildStaleTTL {
			delete(ts.pets, id)
			continue
		}
		u, v, ok := p.db.Project(uint32(ts.res), w.pos.X, w.pos.Y)
		if !ok { // 该场景无底图:投影无从谈起,标记也就无处可画
			continue
		}
		m := server.WildMark{
			ID: strconv.FormatUint(w.actorID, 10), U: u, V: v,
			X: w.pos.X, Y: w.pos.Y, Z: w.pos.Z,
			Lv: w.lv, Voice: w.voice, Height: w.height, Weight: w.weight,
			Mutation: w.mutation, Stale: w.left,
			Kinds: p.wildKinds(scene.NpcActor{
				CfgID: w.cfgID, Weight: w.weight,
				Voice: w.voice, Mutation: w.mutation, GlassType: w.glassType,
			}),
		}
		if w.glassType != gamedata.GlassNull {
			m.GlassType = w.glassType
			m.Glass = p.db.GlassDesc(w.glassType, w.glassValue)
			if m.Glass == "" { // 配置里查不到(新赛季款/新色号)时至少标出是炫彩
				m.Glass = "炫彩"
			}
			m.GlassValue = w.glassValue
		}
		if base, ok := p.db.NpcPetBase(uint32(w.cfgID)); ok {
			// 形态编号与是否异色:色卡的「在 rkpet 看 3D 效果」外链靠这两项拼 URL
			// (缺编号没链接、缺 shiny 则 3D 是普通配色)。反查不到形态时不给编号,
			// 前端按无按钮处理 —— 比给个错号强。
			m.BaseConfID = base
			m.Shiny = w.mutation&scene.MutationShiny != 0
			if info, ok := p.db.PetBase(base); ok {
				m.Name = info.Name
				// 体重单位与 PetData 一致(÷1000 千克),百分位口径同宠物列表。
				m.WeightPct = pet.SizePercentile(float64(w.weight)/1000,
					float64(info.WeightLow)/1000, float64(info.WeightHigh)/1000)
			}
			// 异色个体有专属头像的就用异色版(无则 PetImageByBase 自动回退普通)。
			m.Img = p.db.PetImageByBase(base, w.mutation&scene.MutationShiny != 0).Head
		}
		marks = append(marks, m)
	}
	// 顺序稳定(前端按 id 作 key,免得每次推送都重排 DOM)。
	sort.Slice(marks, func(i, j int) bool { return marks[i].ID < marks[j].ID })

	// 「全部野生」图层:普通野生宠(未命中稀有类别的可捕捉个体)。与稀有通道同一次推送,
	// 前端一个订阅即收齐两组。普通宠只查名/头像/投影,不带稀有的全量字段。
	allMarks := []server.WildAllMark{}
	for id, w := range ts.all {
		if w.left && now.Sub(w.seenAt) > wildStaleTTL {
			delete(ts.all, id)
			continue
		}
		u, v, ok := p.db.Project(uint32(ts.res), w.pos.X, w.pos.Y)
		if !ok {
			continue
		}
		m := server.WildAllMark{
			ID: strconv.FormatUint(w.actorID, 10), U: u, V: v, Stale: w.left,
		}
		if base, ok := p.db.NpcPetBase(uint32(w.cfgID)); ok {
			if info, ok := p.db.PetBase(base); ok {
				m.Name = info.Name
			}
			m.Img = p.db.PetImageByBase(base, false).Head
		}
		allMarks = append(allMarks, m)
	}
	sort.Slice(allMarks, func(i, j int) bool { return allMarks[i].ID < allMarks[j].ID })

	payload := server.WildPayload{Account: acc, SceneResID: ts.res, Pets: marks, AllPets: allMarks}
	p.srv.SetLastWildPets(acc, &payload)
	p.srv.Hub().Broadcast("wildpets", acc, payload)
}
