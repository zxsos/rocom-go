package pipeline

import (
	"sort"
	"strconv"
	"time"

	"github.com/whoisnian/rocom-capture/internal/scene"
	"github.com/whoisnian/rocom-capture/internal/server"
)

// ---- 实时地图的采集物图层(花/草/菌/矿/果树)----
//
// 采集物是**服务器按刷新规则刷出来的 NPC 实体**,与眠枭之星同源同路:进场景快照(0x014a)
// 与 AOI 动作通知(0x0413/0x0414 的 actor_enter / actor_leave)。实体带 npc_content_cfg_id,
// 与 POI 表里采集物点位的 R 是同一个 id,据此查出品种名与图标(见 gamedata.GatherByRefresh)。
//
// 关键实测结论(2026-09,两份独立 pcap:57s/64 实体 与 55s/20 实体):
//   - 服务器确实只下发「此刻刷着」的那一批:**刷出率约三到四成**(玩家 87m 圈内平均
//     13~19 个候选点里只有 4~6 个有实体)。这正是本图层存在的理由 —— POI 图层画的是
//     全部候选点,七成是空的,实时层替玩家滤掉它们。
//   - 采完就消失,且**能与「走出 AOI」区分**:被采走时玩家在 6~17m 内且同秒有奖励通知
//     (0x0243),出 AOI 时玩家在 63~92m 外。两者不必区分也能做对 —— 采走与走远对
//     「此刻有没有」是同一个答案:没有。
//
// **与野生宠物图层的语义相反,不保留「最后所见」**:
//   - 野生宠标记离开 AOI 后要置灰留 4 小时(见 wildStaleTTL)—— 它刷得慢、走回去多半还在,
//     灰点是「这一带见过什么」的备忘。
//   - 采集物采完会**按刷新规则再刷**(刷新行全部带 refresh_rule),留灰点等于告诉玩家
//     「那儿还有」,他走过去却扑空。而它再刷时服务器会重新发 enter,标记自然回来。
//
// 只跟踪**条目内已知**的品种(即 names.json 的 gather 点位表里有的那些,当前 41 个品种
// / 3552 个点位):品种名与图标全靠点位表给,表外品种认出来也说不出叫什么。官方没登记
// 进点位表的品种(实测发现 npc_cfg_id 50047 就在刷,点位表一条都没有)目前认不出来,
// 那是生成脚本的缺口,该修的是表(见 scripts/gen_gamedata.py 的 GATHER_GENRES)。

// gatherTracker 是一个连接的采集物观测态。
// 与 starTracker 一样**按场景会话**跟踪:换场景/传送即整份作废(旧实体必然已不在 AOI)。
type gatherTracker struct {
	marks map[uint64]*gatherMark // actor_id -> 标记
	res   int32                  // 当前场景 res(投影要用)
}

// gatherMark 是当前视野里的一个采集物实体。
type gatherMark struct {
	actorID   uint64
	refreshID int32 // = POI.R,查品种名/图标用
	pos       scene.Position
}

func newGatherTracker(res int32) *gatherTracker {
	return &gatherTracker{marks: map[uint64]*gatherMark{}, res: res}
}

// observeGathers 收下一个实体快照/AOI 通知里的采集物:进入即跟踪,离开/被采走即撤掉。
// snapshot=true 表示来自进场景快照(0x014a),否则是 AOI 动作通知(0x0413/0x0414)。
//
// 与 observeWilds 同处一条调用链,但**不共用** tracker:两者的失效规则相反
// (见本文件顶部),共用一个结构迟早要在「留不留灰点」上二选一,选哪个都是错的。
func (p *Pipeline) observeGathers(conn, acc string, body []byte, now time.Time, snapshot bool) {
	cs := p.conns[conn]
	if cs == nil || cs.gathers == nil {
		return
	}
	ts := cs.gathers
	changed := false

	actors := scene.ParseActorEnter(body)
	if snapshot {
		actors = scene.ParseSceneActors(body)
	}
	for _, a := range actors {
		// 判据是刷新点 id 命中采集物点位表,不认 npc_cfg_id:一个品种可能有多个
		// npc_cfg_id(实测黄石榴石有 50900/50901/50902 三个),而品种名与图标本来
		// 就得从点位表里取,对上了就一次拿全。详见 gamedata.GatherByRefresh。
		if _, ok := p.db.GatherByRefresh(a.RefreshID); !ok {
			continue
		}
		// 已在视野里(服务器重复下发):只更新位置,不动身份。
		// 野生宠那份还要清 stale,这里没有 stale,记位置即可。
		if old, ok := ts.marks[a.ActorID]; ok {
			if old.pos != a.Pos {
				old.pos = a.Pos
				changed = true
			}
			continue
		}
		ts.marks[a.ActorID] = &gatherMark{actorID: a.ActorID, refreshID: a.RefreshID, pos: a.Pos}
		changed = true
	}

	// 离开 AOI 与被采走在此同归一处:两者都意味着「此刻没有了」,标记当场撤掉。
	//
	// 不像星星那样要按玩家距离区分「被收走」与「走远出 AOI」(见 starCollectRadius)——
	// 星星收走了是永久状态,要落库;采集物采完还会再刷,「没了」只是这一刻的事实,
	// 不需要也不该记成持久状态。
	for _, id := range scene.ParseActorLeave(body) {
		if _, ok := ts.marks[id]; ok {
			delete(ts.marks, id)
			changed = true
		}
	}

	if changed {
		p.markGathersDirty(conn, now)
	}
}

// resetGathers 换场景/传送时整份作废(并推空列表,前端立刻清屏)。
//
// 与 resetWilds 不同:采集物是「此刻有」的实时态,**同场景内传送也整份作废**(野生宠那样
// 置灰留着会让人白跑一趟 —— 采完会按刷新规则再刷,「那儿还有」是假的)。换场景同理。
// 实测传送时服务器不为旧实体补发 leave,故这一步只能由我们代劳(见 resetWilds 的注释)。
func (p *Pipeline) resetGathers(conn, acc string, res int32, now time.Time) {
	cs := p.conns[conn]
	if cs == nil {
		return
	}
	cs.gathers = newGatherTracker(res)
	cs.gathersDirtyAt = time.Time{}
	p.pushGathers(conn, acc, now)
}

// gathersDebounce 是采集物图层广播的合并窗口,取 wildsDebounce(150ms)同一量级。
//
// 采集物进出同样突发:实测一份 57s pcap 里采集物 enter 53 条、leave 30 条,
// 常在几秒内成片出现(玩家跑进一片采集区)。窗口沿用野生宠那套已经验证过的取值,
// 不再单独实测一遍 —— 两者共用同一条 0x0414 通道,行为一致。
const gathersDebounce = wildsDebounce

// markGathersDirty 记下「该连接的采集物有变更,待广播」。语义同 markWildsDirty:
// 保留**最早**那个变更时刻,否则持续突发会不断把窗口推后、永远发不出去(饿死)。
func (p *Pipeline) markGathersDirty(conn string, now time.Time) {
	cs := p.conn(conn)
	if cs == nil {
		return
	}
	if cs.gathersDirtyAt.IsZero() {
		cs.gathersDirtyAt = now
	}
}

// flushDirtyGathers 广播所有「窗口已到」的待发变更。由 handle 在每条消息后调用,
// 与 flushDirtyWilds 同处一条路径(两者共用同一批 0x0414 消息,故一并推进)。
func (p *Pipeline) flushDirtyGathers(now time.Time) {
	for conn, cs := range p.conns {
		if cs.gathersDirtyAt.IsZero() || now.Sub(cs.gathersDirtyAt) < gathersDebounce {
			continue
		}
		cs.gathersDirtyAt = time.Time{}
		acc := p.connAccount[conn]
		if acc == "" {
			continue
		}
		p.pushGathers(conn, acc, now)
	}
}

// flushAllDirtyGathers 无条件补发所有待发的采集物变更(不看窗口)。
// 只在离线回放结束时用一次:此后不会再有消息推进窗口,不补发就永远丢了。
func (p *Pipeline) flushAllDirtyGathers() {
	now := p.lastMsgAt
	if now.IsZero() {
		now = time.Now()
	}
	for conn, cs := range p.conns {
		if cs.gathersDirtyAt.IsZero() {
			continue
		}
		cs.gathersDirtyAt = time.Time{}
		acc := p.connAccount[conn]
		if acc == "" {
			continue
		}
		p.pushGathers(conn, acc, now)
	}
}

// pushGathers 缓存并广播当前场景视野内的采集物。
//
// 同样不逐条广播:实体进出是突发(见 gathersDebounce),逐条发会把整份列表重发几十遍。
// 视野内的采集物个位数到二十来个(实测 87m 圈内 4~6 个),单份载荷很小,重发的代价
// 主要在前端重建标记而非带宽 —— 但仍按窗口合并,与野生宠一致。
func (p *Pipeline) pushGathers(conn, acc string, now time.Time) {
	cs := p.conns[conn]
	if cs == nil || cs.gathers == nil {
		return
	}
	ts := cs.gathers
	marks := make([]server.GatherMark, 0, len(ts.marks))
	for id, g := range ts.marks {
		u, v, ok := p.db.Project(uint32(ts.res), g.pos.X, g.pos.Y)
		if !ok { // 该场景无底图:投影无从谈起,标记也就无处可画
			continue
		}
		m := server.GatherMark{
			ID: strconv.FormatUint(id, 10), R: g.refreshID,
			U: u, V: v, X: g.pos.X, Y: g.pos.Y, Z: g.pos.Z,
		}
		// 品种名与图标:实体不带,靠刷新点 id 回查点位表。查不到时不跳过 ——
		// 位置是真的,只是说不出它叫什么,前端按无名标记画。
		if poi, ok := p.db.GatherByRefresh(g.refreshID); ok {
			m.N = poi.N
			m.Icon = p.db.POIIconOf(poi.I)
		}
		marks = append(marks, m)
	}
	// 顺序稳定(前端按 id 作 key,免得每次推送都重排 DOM)。
	sort.Slice(marks, func(i, j int) bool { return marks[i].ID < marks[j].ID })

	payload := server.GatherPayload{Account: acc, SceneResID: ts.res, Gathers: marks}
	p.srv.SetLastGathers(acc, &payload)
	p.srv.Hub().Broadcast("gathers", acc, payload)
}
