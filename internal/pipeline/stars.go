package pipeline

import (
	"math"
	"strings"
	"time"

	"github.com/whoisnian/rocom-capture/internal/capture"
	"github.com/whoisnian/rocom-capture/internal/scene"
	"github.com/whoisnian/rocom-capture/internal/store"
)

// ---- 眠枭之星收集判定(见 docs/data.md 3.4)----
//
// 星/光点:已收集的服务器**根本不刷**,只有未收集的才作为 NPC 实体下发(实体带刷新点 id)。故:
//
//	收到某点的实体          ⇒ 未收集
//	走到某点附近却没有实体  ⇒ 已收集
//
// 石像**不同**:本体收集后不消失、实体一直下发,「出现/消失」不携带收集信息;它的星是实体上的
// 挂件,状态就在实体里(scene.NpcActor.Pendant),触碰收集时客户端发挂件交互(0x0272,带刷新行
// id)。故石像看挂件定状态、只在挂件交互成功时判「刚收走」,不参与「实体离开=被收走」的判定;
// 而 seen 的语义(true ⇒ 未收集)对石像同样成立(挂件已收的石像不置 seen),扫描逻辑无需分叉。
//
// AOI 是**按格子**下发的(配置里有「跨aoi拆分」的区域),不是圆形半径:实测多次出现「更远的实体
// 下发了、更近的没下发」。故不能拿单一半径当 AOI 边界,只能取一个**保守判定半径**——4 份 pcap 里
// 凡距玩家轨迹 ≤100m 的固定 POI(必定存在那些)全部下发,无一例外,故 80m 留足余量。
//
// 但「进圈时刻」不能立即结账:实体按跨格触发下发,可以晚于进圈 4-31s、晚到时玩家已近至 21-59m
// (12 份 pcap 共 5 例),圈边缘徘徊时延迟无上界——进圈即判会闪烁(先判已收集隐藏图标,实体
// 随后到达又翻回)。空间邻近也推不出「该格已下发」(实测有星点 20m 内他者实体早到、星实体晚
// 31s,格边界贴着点过)。故只在两种**实体必已下发**的时机结账:贴脸(≤starCommitNear,实测
// 最早晚到距离 21m 的一半),或已过最近点回撤(≥minD+starCommitBack,即接近段结束——实体
// 要来早来了)。回撤结账在圈外也生效(擦圈边而过的点,回撤 15m 时往往已出圈)。代价只是
// 结账推迟到走过之后几秒,12 份 pcap 复演:零闪烁、无误判、无漏判(仅 pcap 截断处未及结账)。
const (
	starSweepRadius   = 8000                    // 判定半径(厘米):玩家进到此距离内仍无实体 ⇒ 该点已收集(结账另看时机)
	starCommitNear    = 1000                    // 贴脸结账距离:实体最早在 21m 处必已下发,10m 留足余量
	starCommitBack    = 1500                    // 回撤结账迟滞:距离回升超过最近点这么多 ⇒ 接近段结束
	starCollectRadius = 3000                    // 实体离开 AOI 时,玩家在此距离内 ⇒ 是被收走了(而非走远出 AOI)
	starSettle        = 1500 * time.Millisecond // 进场景后等快照到齐再判定,免得把还没下发的当成已收集
)

// starTracker 是一个连接在**当前场景会话**内的星星观测态(换场景/传送即重置)。
type starTracker struct {
	seen   map[int32]bool    // 本场景收到过实体的刷新点 id ⇒ 未收集
	actor  map[uint64]int32  // 实体 actor_id -> 刷新点 id(实体离开时只给 actor_id)
	minD   map[int32]float64 // 刷新点 id -> 本场景内玩家距它的最近距离(只记进过判定圈的,结账时机用)
	snapAt time.Time         // 周边实体快照(0x014a)到达时刻;零值 = 还没到,不做已收集判定
	res    int32             // 当前场景 res(星点按场景取)
}

func newStarTracker(res int32) *starTracker {
	return &starTracker{seen: map[int32]bool{}, actor: map[uint64]int32{}, minD: map[int32]float64{}, res: res}
}

// posXY 从位置推送里取玩家世界坐标。
func posXY(pos map[string]any) (int32, int32, bool) {
	x, ok1 := pos["x"].(int32)
	y, ok2 := pos["y"].(int32)
	return x, y, ok1 && ok2
}

// starPos 查某刷新点的世界坐标(星点来自 gamedata 的 POI 表)。
func (p *Pipeline) starPos(res int32, refreshID int32) [2]int32 {
	for _, poi := range p.db.POIs(uint32(res)) {
		if poi.R == refreshID {
			return [2]int32{poi.X, poi.Y}
		}
	}
	return [2]int32{}
}

// near 报告 (x,y) 是否在点 p 的 r 厘米内(平面距离;星星有同 xy 叠放的,z 不参与)。
func near(x, y int32, p [2]int32, r int32) bool {
	if p == ([2]int32{}) {
		return false
	}
	dx, dy := float64(x-p[0]), float64(y-p[1])
	return math.Hypot(dx, dy) <= float64(r)
}

// knownStars 返回该账号已确认的星星状态(首次访问从库预热)。
func (p *Pipeline) knownStars(acc string) map[int32]int {
	as := p.acct(acc)
	if as.starKnown == nil {
		as.starKnown = p.st.StarStates(acc)
	}
	return as.starKnown
}

// zoneGot 返回该账号 camp → 已收集数合计(首次访问从库预热)。
// key 的**存在性**也有语义:map 里有 camp = 服务器给过该区计数行;根本没行的区(月兔暗港)
// 不注册任何星,不参与守卫(见 sweepStars)。
func (p *Pipeline) zoneGot(acc string) map[int32]int32 {
	as := p.acct(acc)
	if as.zoneGot == nil {
		g := map[int32]int32{} // 抓包服务重启后从库预热
		for _, r := range p.st.StarZones(acc) {
			g[r.Camp] += r.Got
		}
		as.zoneGot = g
	}
	return as.zoneGot
}

// saveStars 落盘并广播星星状态增量(只写与库内快照不同的)。
func (p *Pipeline) saveStars(acc string, states map[int32]int) {
	known := p.knownStars(acc)
	diff := map[int32]int{}
	for rid, s := range states {
		if known[rid] != s {
			known[rid], diff[rid] = s, s
		}
	}
	if len(diff) == 0 {
		return
	}
	p.st.SetStarStates(acc, diff)
	p.srv.Hub().Broadcast("stars", acc, diff)
}

// bumpStarZone 把一次**新收集**计入本地分区进度并广播。服务器只在进场景包(0x0152)里给
// 全量进度、收集当下不推增量(全部 pcap 核实:0x01DC/0x01DF worldmap 通知从未出现,收集时刻
// 只有 AOI + 奖励通知)——不自己记,刚收完某区最后一颗时,该区其他星(抓包前收掉、没走近过的)
// 要等下次传送/重登才整片隐藏。只在两条「刚收走」路径上调用(实体离开+在旁、挂件交互成功),
// 且须在 saveStars 写入该点状态**之前**——按「库内尚未是已收集」防重(光点交互存在光点离开→
// 星出现→星离开的同 rid 两次 leave 路径,只能计一次);sweepStars 补判的历史收集**不算**
// (服务器 got 早已计入,再加会重复)。候选区域唯一才能归因,双候选点(全图 16 个)与检测
// 错漏等下次 0x0152 全量校准。前端按 camp 聚合求和判收满,加在该区哪一行(npc 形态)无所谓,
// 取首行(个别行会 got>tot,聚合和不受影响)。
func (p *Pipeline) bumpStarZone(acc string, res int32, rid int32) {
	if p.knownStars(acc)[rid] == store.StarCollected {
		return // 本次会话或历史上已计过:不重复
	}
	var zs []int32
	for _, poi := range p.db.POIs(uint32(res)) {
		if poi.R == rid && strings.HasPrefix(poi.K, "star") {
			zs = poi.Z
			break
		}
	}
	if len(zs) != 1 {
		return
	}
	rows := p.st.StarZones(acc)
	for i := range rows {
		if rows[i].Camp == zs[0] {
			rows[i].Got++
			p.st.SetStarZones(acc, rows)
			if g := p.acct(acc).zoneGot; g != nil {
				g[zs[0]]++
			}
			p.srv.Hub().Broadcast("starzones", acc, rows)
			return
		}
	}
}

// starSee 收录一个星星系实体:星/光点按「出现 ⇒ 未收集」;石像按挂件状态定收集与否
// (本体常驻,出现不代表未收集),且不进 ts.actor——「实体离开 = 被收走」对石像不成立。
func starSee(ts *starTracker, a scene.NpcActor, states map[int32]int) {
	if a.IsStatue() {
		if a.Pendant == scene.PendantCollected {
			delete(ts.seen, a.RefreshID)
			states[a.RefreshID] = store.StarCollected
		} else { // 挂件未收集;个别实体缺挂件字段时也保守视作未收集(宁可多显示)
			ts.seen[a.RefreshID] = true
			states[a.RefreshID] = store.StarUncollected
		}
		return
	}
	ts.seen[a.RefreshID] = true
	ts.actor[a.ActorID] = a.RefreshID
	states[a.RefreshID] = store.StarUncollected
}

// observeStars 收下一个 AOI 通知里的星星实体进/离(0x0413/0x0414)。
// 实体进入 ⇒ 见 starSee;实体离开且玩家就在旁边 ⇒ 刚被收走(走远出 AOI 的离开不算,故看距离)。
func (p *Pipeline) observeStars(conn, acc string, body []byte) {
	cs := p.conns[conn]
	if cs == nil || cs.stars == nil {
		return
	}
	ts := cs.stars
	states := map[int32]int{}
	for _, a := range scene.ParseActorEnter(body) {
		if a.IsStar() && a.RefreshID != 0 {
			starSee(ts, a, states)
		}
	}
	pos := p.acct(acc).lastPos
	for _, id := range scene.ParseActorLeave(body) {
		rid, ok := ts.actor[id]
		if !ok {
			continue
		}
		delete(ts.actor, id)
		// 玩家不可能隔着几十米收集:只有他就在旁边时,实体消失才是「被收走」。
		if px, py, ok := posXY(pos); ok && near(px, py, p.starPos(ts.res, rid), starCollectRadius) {
			delete(ts.seen, rid)
			states[rid] = store.StarCollected
			p.bumpStarZone(acc, ts.res, rid) // 刚收走:分区进度本地 +1(凑满即触发前端整区隐藏)
		}
	}
	p.saveStars(acc, states)
}

// sweepStars 按玩家当前位置判定周围的星星:走到判定半径内却没收到实体 ⇒ 已收集。
func (p *Pipeline) sweepStars(conn, acc string, res int32, x, y int32, now time.Time) {
	cs := p.conns[conn]
	if cs == nil {
		return
	}
	ts := cs.stars
	// 快照没到齐就判,会把「还没下发」当成「已收集」。
	if ts == nil || ts.res != res || ts.snapAt.IsZero() || now.Sub(ts.snapAt) < starSettle {
		return
	}
	states := map[int32]int{}
	got := p.zoneGot(acc)
	// 一条区域计数都没有(本会话与库里都没见过进场景包)⇒ 守卫无从工作,全部不判。
	// (2026-07-17 实测:新区紫星配置/计数就位但实体未开放刷出,无 got=0 守卫会把玩家
	// 路过的点全误判成已收集。)
	if len(got) == 0 {
		return
	}
	for _, poi := range p.db.POIs(uint32(res)) {
		if !strings.HasPrefix(poi.K, "star") {
			continue
		}
		d := math.Hypot(float64(x-poi.X), float64(y-poi.Y))
		md, entered := ts.minD[poi.R]
		// 没进过判定圈的点不关心;进过的出圈后仍要评估(回撤结账常发生在圈外)。
		if d > starSweepRadius && !entered {
			continue
		}
		if ts.seen[poi.R] {
			if d <= starSweepRadius {
				states[poi.R] = store.StarUncollected
			}
			continue
		}
		if d <= starSweepRadius && (!entered || d < md) {
			ts.minD[poi.R], md, entered = d, d, true
		}
		// 守卫:候选区域(见 POI.Z)只要有一个**有计数行且 got=0**,「已收集」就不可能成立
		//(真实归属区必在候选之中),多半是该点还没开放刷出——不判,保持显示。
		// 服务器**根本没给计数行**的候选(如月兔暗港,该区不注册任何星)不可能是真归属区,
		// 跳过不挡——否则重叠带上的点会被永远卡住(2026-07-17 pcap 实测:望风半岛 3 点
		// 已收集却因候选含月兔暗港而永不隐藏)。
		ok := true
		for _, c := range poi.Z {
			if g, tracked := got[c]; tracked && g == 0 {
				ok = false
				break
			}
		}
		// 结账时机:贴脸,或已过最近点回撤(实体若在早就该到了,见 starCommitNear/Back 注释)。
		if ok && (d <= starCommitNear || (entered && d >= md+starCommitBack)) {
			states[poi.R] = store.StarCollected
		}
	}
	p.saveStars(acc, states)
}

// onSceneSnapshot 处理进场景/传送后的周边实体快照(0x014a):一次性给出 AOI 内的实体。
// 星/光点实体 ⇒ 那些点未收集;石像实体按挂件状态直接定收集与否(见 starSee)。
func (p *Pipeline) onSceneSnapshot(m capture.Message, acc string) {
	cs := p.conns[m.Session]
	if cs == nil || cs.stars == nil {
		return
	}
	ts := cs.stars
	states := map[int32]int{}
	for _, a := range scene.ParseSceneActors(m.AppBody) {
		if a.IsStar() && a.RefreshID != 0 {
			starSee(ts, a, states)
		}
	}
	ts.snapAt = m.Time
	p.saveStars(acc, states)
}

// onPendantReq 记录 c2s 挂件交互(触碰石像上浮现的星):请求直接带石像刷新行 id,
// 等回包(0x0273)确认后判已收集。
func (p *Pipeline) onPendantReq(m capture.Message) {
	if rid, ok := scene.ParsePendantInteract(m.AppBody); ok {
		p.conn(m.Session).pendantRid = rid
	}
}

// onPendantRsp 处理挂件交互回包:成功即判该石像的星已收集。
func (p *Pipeline) onPendantRsp(m capture.Message, acc string) {
	cs := p.conns[m.Session]
	if cs == nil {
		return
	}
	rid := cs.pendantRid
	cs.pendantRid = 0
	ts := cs.stars
	if rid == 0 || ts == nil || !scene.ParsePendantInteractRsp(m.AppBody) {
		return
	}
	// 只认当前场景确有该刷新点的 POI(其它 NPC 的挂件交互对不上星点,自然被滤掉)。
	if p.starPos(ts.res, rid) == ([2]int32{}) {
		return
	}
	delete(ts.seen, rid)
	p.bumpStarZone(acc, ts.res, rid) // 石像的星刚收走:计入分区进度(须在写状态前,防重闸看库内旧值)
	p.saveStars(acc, map[int32]int{rid: store.StarCollected})
}
