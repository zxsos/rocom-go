package scene

import (
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
)

// 下面各用例的**数值**取自 PCAPdroid_01_9月_15_22_36.pcap 里第一条 0x02e6
// (ZONE_SCENE_ONLINE_VISITOR_INFO_NOTIFY)。那一场是「好友甲(100000003)在示例玩家(100000002)
// 的世界里,两人牵手/双人骑乘」,包里两人各一条:
//
//	visitor_info { uin: 100000002  network: 102  pos { pos {x:454226 y:626990 z:2017}
//	                                                   dir {x:0 y:0 z:-1523} }
//	              zone_inst_id: 147119408711 }
//	visitor_info { uin: 100000003  network: 97   pos { pos {x:454423 y:627174 z:1986}
//	                                                   dir {x:0 y:0 z:-128} }
//	              zone_inst_id: 147119410402 }
//
// 字节是照结构拼的(未嵌原文),但坐标/朝向/uin/network 全是真实值 —— wire 布局就这么两种
// (repeated 子消息 + varint),真正需要真实值的是**字段号与坐标口径**,那正是这里锚定的。

// pointMsg 拼一个 Point{pos(1), dir(2)},两者都是 Position 形状。
func pointMsg(p, dir []byte) []byte {
	b := fMsg(1, p)
	return append(b, fMsg(2, dir)...)
}

// visitorInfoMsg 拼一个 VisitorInfo{uin(1), network(2), pos(3), zone_inst_id(6)}。
func visitorInfoMsg(uin uint32, network int32, pt []byte) []byte {
	b := fVar(1, uint64(uin))
	b = append(b, fVar(2, uint64(uint32(network)))...)
	b = append(b, fMsg(3, pt)...)
	return append(b, fVar(6, 147119408711)...)
}

func TestParseOnlineVisitors(t *testing.T) {
	// 两人:自己(100000002)与好友甲(100000003),都带朝向 dir.z。
	body := fMsg(1, visitorInfoMsg(100000002, 102, pointMsg(xyz(454226, 626990, 2017), xyz(0, 0, -1523))))
	body = append(body, fMsg(1, visitorInfoMsg(100000003, 97, pointMsg(xyz(454423, 627174, 1986), xyz(0, 0, -128))))...)

	got := ParseOnlineVisitors(body)
	if len(got) != 2 {
		t.Fatalf("解析出 %d 个访客,期望 2", len(got))
	}
	want := []Visitor{
		{Uin: 100000002, Pos: Position{X: 454226, Y: 626990, Z: 2017}, Dir: Position{Z: -1523}},
		{Uin: 100000003, Pos: Position{X: 454423, Y: 627174, Z: 1986}, Dir: Position{Z: -128}},
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("访客%d = %+v, 期望 %+v", i, got[i], want[i])
		}
	}
}

func TestParseOnlineVisitorsSkipsJunk(t *testing.T) {
	// pos 字段缺失的条目不该被收下(误命中会让 pipeline 把箭头挪到 {0,0,0});
	// 顶层非 field 1 的子消息同样跳过。
	body := fMsg(1, visitorInfoMsg(0, 97, pointMsg(xyz(1, 2, 3), xyz(0, 0, 0)))) // uin=0 → 丢弃
	body = append(body, fMsg(7, fVar(1, 12345))...)                              // 非 visitor_info
	body = append(body, fMsg(1, visitorInfoMsg(100000002, 102, pointMsg(xyz(4, 5, 6), xyz(0, 0, 7))))...)

	got := ParseOnlineVisitors(body)
	if len(got) != 1 {
		t.Fatalf("解析出 %d 个访客,期望 1", len(got))
	}
	if got[0].Uin != 100000002 || got[0].Pos != (Position{X: 4, Y: 5, Z: 6}) {
		t.Errorf("访客 = %+v, 期望 uin=100000002 pos={4 5 6}", got[0])
	}
}

func TestParseOnlineVisitorsEmpty(t *testing.T) {
	if got := ParseOnlineVisitors(nil); len(got) != 0 {
		t.Errorf("空输入解析出 %d 个访客,期望 0", len(got))
	}
	// 只有 uin 没有 pos:不该panic,坐标保持零值
	if got := ParseOnlineVisitors(fMsg(1, fVar(1, 100000002))); len(got) != 1 || got[0].Pos != (Position{}) {
		t.Errorf("缺 pos 时 = %+v, 期望 uin 有效、Pos 零值", got)
	}
}

// selfInfoMsg 拼一段 ZoneEnterSceneRsp:scene_cfg_id(2)/scene_res_cfg_id(3)/
// home_room_level(5) + self_info(11)=ActorInfo{avatar(12){base(1){logic_id(3),lv(11),name(12)}}}。
func selfInfoMsg(field protowire.Number, logicID uint64) []byte {
	base := fVar(3, logicID) // logic_id:玩家的 uin
	base = append(base, fVar(11, 68)...)
	avatar := fMsg(1, base)
	self := fMsg(12, avatar)
	return fMsg(field, self)
}

func TestParseSelfUin(t *testing.T) {
	// 真实值:示例玩家(自己)的 uin。进场景回包里 self_info 在 field 11、传送通知里在 field 21。
	for _, tc := range []struct {
		name  string
		field protowire.Number
	}{
		{"进入场景回包", SelfInfoInEnterSceneRsp},
		{"传送通知", SelfInfoInTeleport},
	} {
		body := fVar(2, 103) // scene_cfg_id
		body = append(body, fVar(3, 10003)...)
		body = append(body, selfInfoMsg(tc.field, 100000002)...)

		if got := ParseSelfUin(body, tc.field); got != 100000002 {
			t.Errorf("%s: uin = %d, 期望 100000002", tc.name, got)
		}
	}
}

func TestParseSelfUinNotFound(t *testing.T) {
	// 常见的「拿不到」:整包没有 self_info、self_info 里是 npc 而非 avatar(实测 NPC 走
	// field 11)、base 缺 logic_id。一律返回 0 —— pipeline 据此不做补位,宁可让箭头冻着
	// 也不能拿别人的坐标当自己的。
	cases := map[string][]byte{
		"无 self_info":  fVar(3, 10003),
		"npc 分支":       fMsg(11, fMsg(11, fVar(3, 555))),
		"base 无 logic": selfInfoMsg(SelfInfoInEnterSceneRsp, 0),
		"空包":           nil,
	}
	for name, body := range cases {
		if got := ParseSelfUin(body, SelfInfoInEnterSceneRsp); got != 0 {
			t.Errorf("%s: uin = %d, 期望 0", name, got)
		}
	}
}

// TestParseSelfUinIgnoresOtherAvatar 守一条易错的语义:self_info 里只可能是自己。
// 但访客流里别人也是 avatar,若哪天把解析接到 0x014a 的 other_actors 上,取到的是别人的
// uin —— 故这里明确 self_info 路径只按给定字段号取,不做任何「挑一个 avatar」的兜底。
func TestParseSelfUinIgnoresOtherAvatar(t *testing.T) {
	// field 20(非 self_info)里塞一个 avatar,不该被取到
	body := append(fVar(3, 10003), selfInfoMsg(20, 100000003)...)
	if got := ParseSelfUin(body, SelfInfoInEnterSceneRsp); got != 0 {
		t.Errorf("uin = %d, 期望 0(非 self_info 字段不该被取用)", got)
	}
}
