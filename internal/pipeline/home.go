package pipeline

import (
	"log"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/zxsos/rocom-go/internal/pet"
	"github.com/zxsos/rocom-go/internal/scene"
	"github.com/zxsos/rocom-go/internal/server"
	"github.com/zxsos/rocom-go/internal/store"
)

// ---- 实时地图的家园小窝图层(见 docs/data.md 3.6)----
//
// 进入家园时服务器一次性给全:home_info(家具布局 + 下蛋配对)与 other_actors(住在窝里的宠物、
// 趴在窝上还没收的蛋)。之后的变化走 AOI 通知(收走一颗蛋 = 那个蛋实体离开)。
//
// 小窝取自**家具列表**而不是实体,因为空窝没有任何实体,只有家具那一行——「小窝可能为空」
// 正是要显示的状态之一。窝与宠物靠 furniture_guid 对应,窝与蛋靠蛋实体的 attach_item_id 对应。
//
// 这里还顺手维护一件**跨场景的全局状态**:学院小窝里住着谁(见 syncAcademyNest)—— 它是
// 「子代性格 100% 遗传」的依据,而那是游戏事实、不是玩家设定,故跟着家园快照一起进。

// homeEgg 是趴在某个窝上、还没收的蛋。
type homeEgg struct {
	actorID   uint64
	npcCfgID  uint32
	itemID    uint32 // 由 npc_cfg_id 反查(gamedata.EggNPCItem)
	furniture uint64 // 所在小窝
}

// homeState 是一次家园停留期间的状态(离开家园即整体作废)。
type homeState struct {
	res       int32
	level     uint32
	roomLevel uint32
	// ownerID 这个家园的主人(见 scene.HomeInfo.OwnerID):0 = 快照没带归属。
	// 与 ownHome 一起判「这个家是不是自己的」—— 好友家园里也有学院小窝,不能拿来写自己的真值
	// (见 syncAcademyNest)。
	ownerID uint64
	// ownHome 这份快照是不是自己的家:ownerID 与当前账号的 uid 相等。
	// ownerID 为 0(判不了)时这里恒为 false,调用方据此走弱判据。
	ownHome   bool
	nests     []scene.Nest              // 只留小窝家具,按 guid 稳定排序
	pets      map[uint64]*scene.HomePet // actor_id -> 入住宠物
	eggs      map[uint64]*homeEgg       // actor_id -> 窝上的蛋
	couples   map[uint64][]uint64       // 母本 actor -> 候选父本 actor(服务器下发)
	// couplesStale:进场景之后又有宠物进/出小窝。配对(lay_egg_couple)**只在进场景快照里下发一次**,
	// 之后哪怕新住进一只、凑成了新的一对,服务器也不再重发(2026-08-15 第五份 pcap 实测:新住户的
	// actor 只出现在 AOI 通知与喂食请求里,没有任何消息重发配对)。故此时手上的配对已可能不全,
	// 据此标记并告知前端,别让人以为「这窝没配上」。重进一次家园即可刷新。
	couplesStale bool
	// pendingEgg 是最近一次交互的蛋实体(c2s 0x0137 的 npc_id):随后到来的收蛋奖励通知
	// 据此知道这颗蛋来自哪个窝,进而记下双亲。
	pendingEgg   *homeEgg
	pendingSince time.Time
}

// pendingEggTTL 是「刚点了窝上的蛋」到「服务器下发那颗蛋」之间的容忍窗口。
// 实测同一秒内即返回,给足余量即可;超时作废,免得把后来别处得到的蛋错记双亲。
const pendingEggTTL = 30 * time.Second

// petAt 返回住在某个窝里的宠物;空窝返回 nil。
func (h *homeState) petAt(guid uint64) (uint64, *scene.HomePet) {
	for actor, p := range h.pets {
		if p.Furniture == guid {
			return actor, p
		}
	}
	return 0, nil
}

// eggAt 返回趴在某个窝上的蛋;没有返回 nil。
func (h *homeState) eggAt(guid uint64) *homeEgg {
	for _, e := range h.eggs {
		if e.furniture == guid {
			return e
		}
	}
	return nil
}

// onHomeSnapshot 处理进场景快照里的家园部分;非家园(无 home_info)返回 false。
func (p *Pipeline) onHomeSnapshot(conn, acc string, body []byte, res int32) bool {
	hi, ok := scene.ParseHomeInfo(body)
	if !ok {
		return false
	}
	h := &homeState{
		res: res, level: hi.Level, roomLevel: hi.RoomLevel,
		ownerID: hi.OwnerID, ownHome: hi.OwnerID != 0 && hi.OwnerID == accountUID(acc),
		pets: map[uint64]*scene.HomePet{}, eggs: map[uint64]*homeEgg{},
		couples: map[uint64][]uint64{},
	}
	for _, n := range hi.Nests {
		if _, isNest := p.db.NestFurniture(n.ConfigID); isNest {
			h.nests = append(h.nests, n)
		}
	}
	sort.Slice(h.nests, func(i, j int) bool { return h.nests[i].GUID < h.nests[j].GUID })
	for _, c := range hi.Couples {
		h.couples[c.FemaleActor] = c.MaleActors
	}
	for _, a := range scene.ParseSceneActors(body) {
		p.addHomeActor(h, a, true)
	}
	p.conn(conn).home = h
	// 学院小窝里是谁 = 游戏事实,先同步再推送(见 syncAcademyNest)。
	p.syncAcademyNest(conn, acc)
	p.pushHome(conn, acc)
	return true
}

// addHomeActor 收下一个可能与小窝有关的实体(入住宠物 / 窝上的蛋)。
// snapshot=false(AOI 增量)时新来的住户会让配对信息过期,见 homeState.couplesStale。
func (p *Pipeline) addHomeActor(h *homeState, a scene.NpcActor, snapshot bool) bool {
	if a.HomePet != nil {
		if _, known := h.pets[a.ActorID]; !known && !snapshot {
			h.couplesStale = true
		}
		hp := *a.HomePet
		h.pets[a.ActorID] = &hp
		return true
	}
	if item := p.db.EggNPCItem(uint32(a.CfgID)); item != 0 && a.AttachItem != 0 {
		h.eggs[a.ActorID] = &homeEgg{actorID: a.ActorID, npcCfgID: uint32(a.CfgID),
			itemID: item, furniture: a.AttachItem}
		return true
	}
	return false
}

// observeHome 处理 AOI 动作通知里与家园有关的变化(新下的蛋进场、收走的蛋离场、宠物进出窝)。
func (p *Pipeline) observeHome(conn, acc string, body []byte) {
	cs := p.conns[conn]
	if cs == nil || cs.home == nil {
		return
	}
	changed := false
	for _, a := range scene.ParseActorEnter(body) {
		if p.addHomeActor(cs.home, a, false) {
			changed = true
		}
	}
	for _, id := range scene.ParseActorLeave(body) {
		if _, ok := cs.home.pets[id]; ok {
			delete(cs.home.pets, id)
			cs.home.couplesStale = true // 住户搬走同样让配对过期
			changed = true
		}
		if _, ok := cs.home.eggs[id]; ok {
			delete(cs.home.eggs, id)
			changed = true
		}
	}
	if changed {
		// 住户进出小窝最常见的就是这一刻(玩家在自己家里把宠物放进/抱出学院小窝),
		// 故这里也要同步真值,而不是只在进场景那一次。
		p.syncAcademyNest(conn, acc)
		p.pushHome(conn, acc)
	}
}

// syncAcademyNest 把「学院小窝里住着谁」同步成全局真值(培育页按它算性格 100% 遗传,见 data.md 3.6)。
//
// 为什么要自动做:小窝里那只参与孵蛋时子代性格 100% 随它,而「谁在小窝里」是游戏里的一个
// **事实**(家具的住户),不该让玩家再到页面上手工勾一遍 —— 管线本来就已经解出了住户(见 pushHome
// 里那个 petAt),这里只是把同一个值写进全局设置。玩家自己「打算怎么放」仍由培育线里的计划值表达。
//
// 三条纪律:
//   - **只认自己的家**:好友家园里也有学院小窝(config 1001072),那是对方的宠物。归属在
//     0x014a 的 home_info.home_owner_id 里,与登录 uid 同口径(见 scene.HomeInfo.OwnerID);
//     判不了归属时只按「住户确实在本账号宠物库里」这条弱判据,且**只增不清**。
//   - **只在值真变时写库 + 广播**:家园消息极高频(一次进场景 0x0414 就有上百条),每次都写盘、
//     每次都广播会让培育页反复重拉。稳态下这里是一次点查、零写入。
//   - **「没有这件家具」不动已有值**:那多半是还没解锁这个玩法,不该顺手把玩家手工设的值清掉;
//     只有「家具在、但窝是空的」才算事实上的空窝。
func (p *Pipeline) syncAcademyNest(conn, acc string) {
	cs := p.conns[conn]
	if cs == nil || cs.home == nil {
		return
	}
	h := cs.home
	// 归属明确、但不是自己的家:什么都不做(别人家的窝与自己的设置无关,更不能清)。
	if h.ownerID != 0 && !h.ownHome {
		return
	}
	var guid uint64
	for _, n := range h.nests {
		if p.db.IsAcademyNest(n.ConfigID) {
			guid = n.GUID
			break
		}
	}
	if guid == 0 {
		return // 自己家里没有学院小窝(还没解锁/这件家具没放):不动既有值
	}
	var gid uint32
	if _, hp := h.petAt(guid); hp != nil {
		gid = hp.PetGid
	}
	if !h.ownHome {
		// 弱判据(这条快照没带归属):空窝不动、住户必须在自己的库里。
		if gid == 0 {
			return
		}
		if pp, err := p.st.For(acc).GetPet(gid); err != nil || pp == nil {
			return
		}
	}
	if p.st.AcademyGid() == gid {
		return
	}
	if err := p.st.SetAcademyGid(gid); err != nil {
		log.Printf("学院小窝: 同步真值 %d 失败: %v", gid, err)
		return
	}
	// 培育页的性格命中率是按真值算的,改完得让它重拉(与培育的其它写操作同一套路)。
	p.srv.Hub().Broadcast("breeding", acc, map[string]any{"account": acc})
}

// accountUID 取 "UID:<uid>" 里的 uid;取不出返回 0(此时家园归属判不了,走弱判据)。
func accountUID(acc string) uint64 {
	v, err := strconv.ParseUint(strings.TrimPrefix(acc, "UID:"), 10, 64)
	if err != nil {
		return 0
	}
	return v
}

// leaveHome 在换场景/传送时作废家园状态并推空(前端随即撤掉小窝图层)。
func (p *Pipeline) leaveHome(conn, acc string, res int32) {
	cs := p.conn(conn)
	if cs.home == nil || cs.home.res == res {
		return
	}
	cs.home = nil
	p.pushHome(conn, acc)
}

// onNpcInteract 记下「刚点了窝上的蛋」,供随后的收蛋通知认领双亲(见 applyHomeEggParents)。
func (p *Pipeline) onNpcInteract(conn string, body []byte, now time.Time) {
	cs := p.conns[conn]
	if cs == nil || cs.home == nil {
		return
	}
	id, _, ok := scene.ParseNpcNextAct(body)
	if !ok {
		return
	}
	if e, isEgg := cs.home.eggs[id]; isEgg {
		cs.home.pendingEgg, cs.home.pendingSince = e, now
	}
}

// ---- 推送 ----

// nestMark 是推送给前端的一个小窝(u/v 已按底图投影,与玩家位置同一套)。
// pushHome 缓存并广播当前家园的小窝图层。
func (p *Pipeline) pushHome(conn, acc string) {
	cs := p.conns[conn]
	payload := &server.HomePayload{Account: acc, Nests: []server.NestMark{}}
	if cs == nil || cs.home == nil {
		p.srv.SetLastHome(acc, payload)
		p.srv.Hub().Broadcast("home", acc, payload)
		return
	}
	h := cs.home
	sc := p.st.For(acc)
	marks := make([]server.NestMark, 0, len(h.nests))
	for _, n := range h.nests {
		u, v, _ := p.db.Project(uint32(h.res), n.Pos.X, n.Pos.Y)
		name, _ := p.db.NestFurniture(n.ConfigID)
		m := server.NestMark{ID: strconv.FormatUint(n.GUID, 10), U: u, V: v, X: n.Pos.X, Y: n.Pos.Y, Name: name}
		if actor, hp := h.petAt(n.GUID); hp != nil {
			m.Pet = p.nestPetOf(sc, h, actor, hp)
		}
		if e := h.eggAt(n.GUID); e != nil {
			ne := server.NestEgg{ItemID: e.itemID, Icon: p.db.EggIcon(e.itemID)}
			if it, ok := p.db.EggItemInfo(e.itemID); ok {
				ne.Name = it.Name
			}
			m.Egg = &ne
		}
		marks = append(marks, m)
	}
	payload.Nests = marks
	// 四个元信息字段同进同退:在家园时整体下发(值即使为 0/false 也带),
	// 不在家园时整体缺席 —— 由 HomePayload 的内嵌指针保证,与改造前的 map 行为一致。
	payload.HomeMeta = &server.HomeMeta{
		SceneResID:   h.res,
		Level:        h.level,
		RoomLevel:    h.roomLevel,
		CouplesStale: h.couplesStale,
	}
	p.srv.SetLastHome(acc, payload)
	p.srv.Hub().Broadcast("home", acc, payload)
}

// nestPetOf 组一只入住宠物的简要信息:名字/位置来自场景实体,个体属性回库里取(宠物列表已存)。
func (p *Pipeline) nestPetOf(sc *store.Scoped, h *homeState, actor uint64, hp *scene.HomePet) *server.NestPet {
	np := &server.NestPet{Gid: hp.PetGid, Name: hp.Name, FeedRound: hp.FeedRound}
	if pp, err := sc.GetPet(hp.PetGid); err == nil && pp != nil {
		pet.FillSizePercentile(p.db, pp)
		np.Species, np.Img, np.Gender, np.Level = pp.Species, pp.Image.Head, pp.Gender, pp.Level
		np.HeightM, np.WeightKg = pp.HeightM, pp.WeightKg
		np.HeightPct, np.WeightPct = pp.HeightPct, pp.WeightPct
		np.Voice, np.Nature, np.Talent = pp.Voice, pp.Nature, pp.TalentRank
		if np.Name == "" {
			np.Name = pp.Name
		}
	}
	for _, mate := range h.matesOf(actor) {
		if mp := h.pets[mate]; mp != nil {
			np.Mates = append(np.Mates, server.NestMate{Gid: mp.PetGid, Name: mp.Name})
		}
	}
	return np
}

// matesOf 返回与某只宠物配对的另一半 actor 列表:母本给候选父本,父本给它配的母本。
func (h *homeState) matesOf(actor uint64) []uint64 {
	if males, ok := h.couples[actor]; ok {
		return males
	}
	var out []uint64
	for female, males := range h.couples {
		for _, m := range males {
			if m == actor {
				out = append(out, female)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
