package pipeline

import (
	"sort"
	"strconv"
	"time"

	"github.com/whoisnian/rocom-capture/internal/pet"
	"github.com/whoisnian/rocom-capture/internal/scene"
	"github.com/whoisnian/rocom-capture/internal/store"
)

// ---- 实时地图的家园小窝图层(见 docs/data.md 3.6)----
//
// 进入家园时服务器一次性给全:home_info(家具布局 + 下蛋配对)与 other_actors(住在窝里的宠物、
// 趴在窝上还没收的蛋)。之后的变化走 AOI 通知(收走一颗蛋 = 那个蛋实体离开)。
//
// 小窝取自**家具列表**而不是实体,因为空窝没有任何实体,只有家具那一行——「小窝可能为空」
// 正是要显示的状态之一。窝与宠物靠 furniture_guid 对应,窝与蛋靠蛋实体的 attach_item_id 对应。

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
		p.pushHome(conn, acc)
	}
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
type nestMark struct {
	ID   string  `json:"id"` // furniture_guid(uint64 超出 JS 安全整数,用字符串)
	U    float64 `json:"u"`
	V    float64 `json:"v"`
	X    int32   `json:"x"`
	Y    int32   `json:"y"`
	Name string  `json:"name"` // 家具名(精灵小窝)
	// Pet 为空即空窝。
	Pet *nestPet `json:"pet,omitempty"`
	Egg *nestEgg `json:"egg,omitempty"`
}

// nestPet 是窝里那只宠物的简要信息(悬浮显示;点击看详情走 /api/pets/{gid})。
type nestPet struct {
	Gid       uint32   `json:"gid"`
	Name      string   `json:"name"`
	Species   string   `json:"species,omitempty"`
	Img       string   `json:"img,omitempty"`
	Gender    string   `json:"gender,omitempty"`
	Level     uint32   `json:"level,omitempty"`
	HeightM   float64  `json:"heightM,omitempty"`
	WeightKg  float64  `json:"weightKg,omitempty"`
	HeightPct *float64 `json:"heightPct,omitempty"`
	WeightPct *float64 `json:"weightPct,omitempty"`
	Voice     int32    `json:"voice"`
	Nature    string   `json:"nature,omitempty"`
	Talent    string   `json:"talentRank,omitempty"`
	FeedRound uint32   `json:"feedRound,omitempty"`
	// Mates 是与它配对的另一半(母本看到候选父本,父本看到它配的母本);多于一个即串窝。
	Mates []nestMate `json:"mates,omitempty"`
}

// nestMate 是配对里的另一半(只给名字与 gid,详情点开另一个窝即可)。
type nestMate struct {
	Gid  uint32 `json:"gid"`
	Name string `json:"name"`
}

// nestEgg 是趴在窝上、还没收的蛋。
type nestEgg struct {
	ItemID uint32 `json:"itemId"`
	Name   string `json:"name"`
	Icon   string `json:"icon,omitempty"`
}

// pushHome 缓存并广播当前家园的小窝图层。
func (p *Pipeline) pushHome(conn, acc string) {
	cs := p.conns[conn]
	payload := map[string]any{"account": acc, "nests": []nestMark{}}
	if cs == nil || cs.home == nil {
		p.srv.SetLastHome(acc, payload)
		p.srv.Hub().Broadcast("home", acc, payload)
		return
	}
	h := cs.home
	sc := p.st.For(acc)
	marks := make([]nestMark, 0, len(h.nests))
	for _, n := range h.nests {
		u, v, _ := p.db.Project(uint32(h.res), n.Pos.X, n.Pos.Y)
		name, _ := p.db.NestFurniture(n.ConfigID)
		m := nestMark{ID: strconv.FormatUint(n.GUID, 10), U: u, V: v, X: n.Pos.X, Y: n.Pos.Y, Name: name}
		if actor, hp := h.petAt(n.GUID); hp != nil {
			m.Pet = p.nestPetOf(sc, h, actor, hp)
		}
		if e := h.eggAt(n.GUID); e != nil {
			ne := nestEgg{ItemID: e.itemID, Icon: p.db.EggIcon(e.itemID)}
			if it, ok := p.db.EggItemInfo(e.itemID); ok {
				ne.Name = it.Name
			}
			m.Egg = &ne
		}
		marks = append(marks, m)
	}
	payload["nests"] = marks
	payload["sceneResId"] = h.res
	payload["level"] = h.level
	payload["roomLevel"] = h.roomLevel
	payload["couplesStale"] = h.couplesStale
	p.srv.SetLastHome(acc, payload)
	p.srv.Hub().Broadcast("home", acc, payload)
}

// nestPetOf 组一只入住宠物的简要信息:名字/位置来自场景实体,个体属性回库里取(宠物列表已存)。
func (p *Pipeline) nestPetOf(sc *store.Scoped, h *homeState, actor uint64, hp *scene.HomePet) *nestPet {
	np := &nestPet{Gid: hp.PetGid, Name: hp.Name, FeedRound: hp.FeedRound}
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
			np.Mates = append(np.Mates, nestMate{Gid: mp.PetGid, Name: mp.Name})
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
