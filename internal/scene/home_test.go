package scene

import (
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
)

// 构造工具:按字段号拼 protobuf(与 star_test.go 同风格)。
func fVar(num protowire.Number, v uint64) []byte {
	return protowire.AppendVarint(protowire.AppendTag(nil, num, protowire.VarintType), v)
}

func fMsg(num protowire.Number, sub []byte) []byte {
	return protowire.AppendBytes(protowire.AppendTag(nil, num, protowire.BytesType), sub)
}

func xyz(x, y, z int32) []byte {
	b := fVar(1, uint64(uint32(x)))
	b = append(b, fVar(2, uint64(uint32(y)))...)
	return append(b, fVar(3, uint64(uint32(z)))...)
}

// homeInfoMsg 拼一个 ZoneSceneClientEnterSceneFinishNtyAck:home_info(22) 里含
// home_level(4)/room_level(5)/room_layout(20)/lay_egg_couple(22)。
func homeInfoMsg(nests [][]byte, couples []byte) []byte {
	var plane []byte
	for _, n := range nests {
		plane = append(plane, fMsg(20, n)...) // furniture_list
	}
	room := fMsg(20, plane)               // room_plane_list
	layout := fMsg(1, room)               // rooms
	hi := fVar(4, 25)                     // home_level
	hi = append(hi, fVar(5, 5)...)        // room_level
	hi = append(hi, fMsg(20, layout)...)  // room_layout
	hi = append(hi, fMsg(22, couples)...) // lay_egg_couple
	return fMsg(22, hi)
}

// furniture 拼一件家具:guid(1)/item_gid(4)/config_id(5)/position(6).pos(1)。
// 家具坐标是场景坐标的 FurniturePosScale 倍。
func furniture(guid uint64, itemGid, cfg uint32, x, y int32) []byte {
	b := fVar(1, guid)
	b = append(b, fVar(4, uint64(itemGid))...)
	b = append(b, fVar(5, uint64(cfg))...)
	return append(b, fMsg(6, fMsg(1, xyz(x*FurniturePosScale, y*FurniturePosScale, 100)))...)
}

func TestParseHomeInfo(t *testing.T) {
	// 两个小窝(1001071)+ 一件别的家具;一对配对(母 100 ← 父 200、300 两个候选=串窝)。
	nests := [][]byte{
		furniture(11, 827, 1001071, 198, 45),
		furniture(22, 1047, 1001071, -1042, 485),
		furniture(33, 1, 1002149, 0, 0),
	}
	couple := fVar(1, 100)
	couple = append(couple, fVar(2, 200)...)
	couple = append(couple, fVar(2, 300)...)
	body := homeInfoMsg(nests, fMsg(1, couple))

	hi, ok := ParseHomeInfo(body)
	if !ok {
		t.Fatal("应解出 home_info")
	}
	if hi.Level != 25 || hi.RoomLevel != 5 {
		t.Errorf("等级 = %d/%d, want 25/5", hi.Level, hi.RoomLevel)
	}
	if len(hi.Nests) != 3 { // 本包不认小窝,家具全给出,由调用方按 config_id 过滤
		t.Fatalf("家具数 = %d, want 3", len(hi.Nests))
	}
	n := hi.Nests[1]
	if n.GUID != 22 || n.ItemGid != 1047 || n.ConfigID != 1001071 {
		t.Errorf("家具 = %+v", n)
	}
	if n.Pos.X != -1042 || n.Pos.Y != 485 { // 已按 FurniturePosScale 换算回场景坐标
		t.Errorf("家具坐标 = %v, want (-1042,485)", n.Pos)
	}
	if len(hi.Couples) != 1 || hi.Couples[0].FemaleActor != 100 || len(hi.Couples[0].MaleActors) != 2 {
		t.Errorf("配对 = %+v", hi.Couples)
	}
	if _, ok := ParseHomeInfo(fVar(4, 1)); ok {
		t.Error("非家园消息不该解出 home_info")
	}
}

func TestParseHomePetActor(t *testing.T) {
	// ActorInfo{npc(11){base(1){actor_id(2), pt(8).pos(1)}, home_pet(22).home_pet_info(1), attach_item_info(23)}}
	hp := fVar(1, 37610)                            // pet_gid
	hp = append(hp, fVar(3, 726147329154784719)...) // furniture_guid
	hp = append(hp, fVar(8, 1701)...)               // status
	hp = append(hp, fMsg(11, []byte("小独角兽"))...)    // name(bytes)
	hp = append(hp, fVar(12, 4)...)                 // feed_round
	hp = append(hp, fMsg(13, xyz(-1042, 485, 61))...)
	hp = append(hp, fVar(14, 3062001)...) // conf_id

	base := fVar(2, 9567953371313215810)
	base = append(base, fMsg(8, fMsg(1, xyz(-1042, 485, 61)))...)
	npc := fMsg(1, base)
	npc = append(npc, fMsg(22, fMsg(1, hp))...)
	npc = append(npc, fMsg(23, fVar(2, 999))...) // attach_item_info.attach_item_id

	actors := ParseSceneActors(fMsg(7, fMsg(11, npc)))
	if len(actors) != 1 {
		t.Fatalf("实体数 = %d", len(actors))
	}
	a := actors[0]
	if a.HomePet == nil {
		t.Fatal("应解出 home_pet")
	}
	if a.HomePet.PetGid != 37610 || a.HomePet.ConfID != 3062001 ||
		a.HomePet.Furniture != 726147329154784719 || a.HomePet.Name != "小独角兽" ||
		a.HomePet.FeedRound != 4 || a.HomePet.Status != 1701 {
		t.Errorf("home_pet = %+v", *a.HomePet)
	}
	if a.HomePet.Pos.X != -1042 || a.HomePet.Pos.Y != 485 {
		t.Errorf("宠物坐标 = %v", a.HomePet.Pos)
	}
	if a.AttachItem != 999 {
		t.Errorf("attach_item = %d, want 999", a.AttachItem)
	}
	if a.ActorID != 9567953371313215810 { // 配对里的 obj_id 即此
		t.Errorf("actor_id = %d", a.ActorID)
	}
}

func TestParseNpcNextAct(t *testing.T) {
	// c2s:6 字节子头 + protobuf + tsf4g 尾
	body := append([]byte{0xc0, 0x50, 0, 0, 0, 0x08}, fVar(1, 9567953371316361703)...)
	body = append(body, fVar(2, 830000028)...)
	body = append(body, []byte("tsf4g\x0b")...)
	id, opt, ok := ParseNpcNextAct(body)
	if !ok || id != 9567953371316361703 || opt != 830000028 {
		t.Errorf("ParseNpcNextAct = %d, %d, %v", id, opt, ok)
	}
	if _, _, ok := ParseNpcNextAct([]byte{1, 2, 3}); ok {
		t.Error("过短的包不该解出")
	}
}
