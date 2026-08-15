package scene

// 家园(精灵小窝)相关解析,供实时地图的小窝图层与精灵蛋的双亲推断(见 docs/data.md 3.6)。
//
// 三样东西都在进场景快照 ZoneSceneClientEnterSceneFinishNtyAck(0x014a)里:
//   - home_info(22):房屋等级 + 家具布局(小窝也是家具)+ **下蛋配对** lay_egg_couple;
//   - other_actors(7) 里 detail_type=10 的实体:入住小窝的宠物(home_pet_info);
//   - other_actors(7) 里 detail_type=13 的实体:趴在窝上、还没收的蛋(npc_cfg_id 形如 930xxx,
//     attach_item_info.attach_item_id 即所属小窝的 furniture_guid)。
//
// 配对不必自己按距离猜:服务器直接下发 female → male 列表(male 是 repeated,几个窝挨太近
// 「串窝」时会有多个候选父本,此时父本无法唯一确定)。宠物实体的 base.actor_id 就是配对里的
// obj_id,据此把配对翻成 pet_gid。
//
// 坐标:home_pet_info.pos 与实体 base.pt.pos 同为家园场景世界坐标(与玩家移动包一套,可直接
// 投影到家园底图);家具 position.pos 是它的 100 倍(实测逐项 ×100),换算后一致。

import (
	"bytes"

	"google.golang.org/protobuf/encoding/protowire"
)

// FurniturePosScale 是家具坐标相对场景世界坐标的倍数(实测家具 19800 ↔ 宠物/玩家 198)。
const FurniturePosScale = 100

// HomePet 是入住某个小窝的一只宠物(ActorInfo.npc.home_pet.home_pet_info)。
type HomePet struct {
	PetGid    uint32 // 宠物 gid(与宠物列表同一套)
	ConfID    uint32 // 形态 conf_id
	Furniture uint64 // 所住小窝的 furniture_guid
	Name      string // 昵称(服务器按 bytes 下发)
	Status    uint32 // 家园状态(1701/1702/1406…语义未细究)
	FeedRound uint32 // 已喂食轮数
	Pos       Position
}

// Nest 是家园里的一件小窝家具(RoomFurnitureDetails)。是否为小窝由调用方按
// config_id 查 gamedata.NestFurniture 判定——本包不引配置表。
type Nest struct {
	GUID     uint64 // furniture_guid;入住宠物的 home_pet_info.furniture_guid 与之对应
	ConfigID uint32 // 家具配置 id(1001071 精灵小窝)
	ItemGid  uint32 // 家具物品 gid
	Pos      Position
}

// Couple 是一对下蛋配对(HomeLayEggCoupleOne):母本一只、候选父本一至多只。
// 父本多于一只即「串窝」,谁是实际父本无从确定(协议里没有这个记录)。
type Couple struct {
	FemaleActor uint64
	MaleActors  []uint64
}

// HomeInfo 是一次进入家园时下发的家园快照(不含实体,实体在 other_actors 里)。
type HomeInfo struct {
	Level     uint32
	RoomLevel uint32
	Nests     []Nest
	Couples   []Couple
}

// ParseHomeInfo 从 0x014a 的 AppBody 取 home_info(22);非家园场景返回 ok=false。
func ParseHomeInfo(body []byte) (HomeInfo, bool) {
	hi := subMsg(body, 22)
	if hi == nil {
		return HomeInfo{}, false
	}
	var out HomeInfo
	scanFields(hi, func(num protowire.Number, typ protowire.Type, val []byte, v uint64) {
		switch {
		case num == 4 && typ == protowire.VarintType:
			out.Level = uint32(v)
		case num == 5 && typ == protowire.VarintType:
			out.RoomLevel = uint32(v)
		case num == 20 && typ == protowire.BytesType: // room_layout(RoomLayoutInfo)
			out.Nests = append(out.Nests, parseFurniture(val)...)
		case num == 22 && typ == protowire.BytesType: // lay_egg_couple(HomeLayEggCoupleInfo)
			out.Couples = append(out.Couples, parseCouples(val)...)
		}
	})
	return out, true
}

// parseFurniture 展开 room_layout: rooms(1) → room_plane_list(20) → furniture_list(20)。
func parseFurniture(layout []byte) []Nest {
	var out []Nest
	scanFields(layout, func(n1 protowire.Number, t1 protowire.Type, room []byte, _ uint64) {
		if n1 != 1 || t1 != protowire.BytesType { // rooms
			return
		}
		scanFields(room, func(n2 protowire.Number, t2 protowire.Type, plane []byte, _ uint64) {
			if n2 != 20 || t2 != protowire.BytesType { // room_plane_list
				return
			}
			scanFields(plane, func(n3 protowire.Number, t3 protowire.Type, fur []byte, _ uint64) {
				if n3 != 20 || t3 != protowire.BytesType { // furniture_list
					return
				}
				out = append(out, parseNest(fur))
			})
		})
	})
	return out
}

// parseNest 解一件家具:furniture_guid(1)/item_gid(4)/config_id(5)/position(6).pos(1)。
// 家具坐标是场景世界坐标的 FurniturePosScale 倍,这里换算回来,与实体/玩家同一套。
func parseNest(b []byte) Nest {
	var n Nest
	scanFields(b, func(num protowire.Number, typ protowire.Type, val []byte, v uint64) {
		switch {
		case num == 1 && typ == protowire.VarintType:
			n.GUID = v
		case num == 4 && typ == protowire.VarintType:
			n.ItemGid = uint32(v)
		case num == 5 && typ == protowire.VarintType:
			n.ConfigID = uint32(v)
		case num == 6 && typ == protowire.BytesType: // position(Point)
			if pos := subMsg(val, 1); pos != nil {
				n.Pos = parseXYZ(pos)
				n.Pos.X /= FurniturePosScale
				n.Pos.Y /= FurniturePosScale
				n.Pos.Z /= FurniturePosScale
			}
		}
	})
	return n
}

// parseCouples 解 lay_egg_couple: female_couple(1){female_obj_id(1), male_obj_id(2,repeated)}。
func parseCouples(b []byte) []Couple {
	var out []Couple
	scanFields(b, func(num protowire.Number, typ protowire.Type, one []byte, _ uint64) {
		if num != 1 || typ != protowire.BytesType {
			return
		}
		var c Couple
		scanFields(one, func(n protowire.Number, t protowire.Type, _ []byte, v uint64) {
			if t != protowire.VarintType {
				return
			}
			switch n {
			case 1:
				c.FemaleActor = v
			case 2:
				c.MaleActors = append(c.MaleActors, v)
			}
		})
		if c.FemaleActor != 0 {
			out = append(out, c)
		}
	})
	return out
}

// parseHomePet 解 npc.home_pet(22).home_pet_info(1);非家园宠物实体返回 nil。
func parseHomePet(npc []byte) *HomePet {
	hp := subMsg(subMsg(npc, 22), 1)
	if hp == nil {
		return nil
	}
	var p HomePet
	scanFields(hp, func(num protowire.Number, typ protowire.Type, val []byte, v uint64) {
		switch {
		case num == 1 && typ == protowire.VarintType:
			p.PetGid = uint32(v)
		case num == 3 && typ == protowire.VarintType:
			p.Furniture = v
		case num == 8 && typ == protowire.VarintType:
			p.Status = uint32(v)
		case num == 11 && typ == protowire.BytesType:
			p.Name = string(val)
		case num == 12 && typ == protowire.VarintType:
			p.FeedRound = uint32(v)
		case num == 13 && typ == protowire.BytesType:
			p.Pos = parseXYZ(val)
		case num == 14 && typ == protowire.VarintType:
			p.ConfID = uint32(v)
		}
	})
	return &p
}

// parseXYZ 解 Position{x(1),y(2),z(3)}(varint,int32 语义)。
func parseXYZ(b []byte) Position {
	var p Position
	scanFields(b, func(num protowire.Number, typ protowire.Type, _ []byte, v uint64) {
		if typ != protowire.VarintType {
			return
		}
		switch num {
		case 1:
			p.X = int32(v)
		case 2:
			p.Y = int32(v)
		case 3:
			p.Z = int32(v)
		}
	})
	return p
}

// OpNpcNextActReq 是 c2s 的 NPC 交互请求(ZONE_SCENE_NPC_NEXT_ACT_REQ)。家园里从小窝上
// **收蛋**走的就是它:npc_id 即那颗蛋 NPC 的 actor_id,据此能把随后下发的新蛋对回它所在的窝。
const OpNpcNextActReq = 0x0137

// ParseNpcNextAct 取 c2s NPC 交互请求里的 npc_id(1)/option_id(2)。
// c2s AppBody 前有 6 字节子头(见 docs/protocol.md),故从固定偏移起解;解不出返回 ok=false。
func ParseNpcNextAct(appBody []byte) (npcID uint64, optionID int32, ok bool) {
	body := appBody
	if i := bytes.Index(body, tsf4gMark); i >= 0 {
		body = body[:i]
	}
	if len(body) <= c2sSubHeaderLen {
		return 0, 0, false
	}
	body = body[c2sSubHeaderLen:]
	scanFields(body, func(num protowire.Number, typ protowire.Type, _ []byte, v uint64) {
		if typ != protowire.VarintType {
			return
		}
		switch num {
		case 1:
			npcID = v
		case 2:
			optionID = int32(v)
		}
	})
	return npcID, optionID, npcID != 0
}

// c2sSubHeaderLen 是 c2s AppBody 里 protobuf 之前的子头长度(实测恒为 6)。
const c2sSubHeaderLen = 6
