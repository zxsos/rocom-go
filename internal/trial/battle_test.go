package trial

import (
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
)

// 本文件用**手工构造**的消息验证战斗解析,不依赖 pcap 文件 ——
// 构造字节能精确覆盖边界(无 grass_trial_battle_info 的、pets 为空的、
// 字段号写错的),而回放 pcap 只能验证「这一份样本是这样」。

// appendVarint 追加一个 varint 字段。
func appendVarint(b []byte, num protowire.Number, v uint64) []byte {
	b = protowire.AppendTag(b, num, protowire.VarintType)
	return protowire.AppendVarint(b, v)
}

// appendMsg 追加一个嵌套消息字段。
func appendMsg(b []byte, num protowire.Number, body []byte) []byte {
	b = protowire.AppendTag(b, num, protowire.BytesType)
	return protowire.AppendBytes(b, body)
}

// buildBattleEnter 构造一条 0x1316。
//
// 字段路径(从 pbdesc 的 ZoneBattleEnterNotify 描述符逐个核对过):
//
//	#6 init_info
//	     #6  enemy_team:  #1 base(#25 npc_id) / #2 pets(#2 battle_common_pet_info(#15 base_conf_id))
//	     #25 grass_trial_battle_info: #3 event_type
//
// 注意 repeated 字段的编码:**每个元素一个独立 tag**,不是「一个 tag 装一个数组」。
// (早先我把 pets 包成一层 [#2: [#2:pet1, #2:pet2]],多了一层,结果解析全空 ——
//
//	这种错误不会报错,只会静默取到 nil,故在此留注。)
func buildBattleEnter(npcID uint64, petBases ...uint64) []byte {
	var team []byte
	for _, p := range petBases {
		info := appendVarint(nil, 15, p)                   // battle_common_pet_info.base_conf_id
		team = appendMsg(team, 2, appendMsg(nil, 2, info)) // enemy_team.pets(每个一只)
	}
	team = appendMsg(team, 1, appendVarint(nil, 25, npcID)) // enemy_team.base.npc_id

	initInfo := appendMsg(nil, 6, team) // enemy_team
	gtbi := appendVarint(nil, 3, 1)     // event_type = 1(首领)
	initInfo = appendMsg(initInfo, 25, gtbi)

	return appendMsg(nil, 6, initInfo) // init_info
}

func TestParseBattleEnter(t *testing.T) {
	e := ParseBattleEnter(buildBattleEnter(86023, 8107, 8107)) // 故意重复一只,测去重
	if e == nil {
		t.Fatal("解析失败")
	}
	if e.Type != BattleBoss {
		t.Errorf("Type = %d, 期望 BattleBoss(1)", e.Type)
	}
	if !e.Type.IsTrial() {
		t.Error("带 grass_trial_battle_info 的应判定为试炼战斗")
	}
	if e.NPCObject != 86023 {
		t.Errorf("NPCObject = %d, 期望 86023", e.NPCObject)
	}
	if len(e.PetBases) != 1 || e.PetBases[0] != 8107 {
		t.Errorf("PetBases = %v, 期望 [8107](去重后)", e.PetBases)
	}
}

// TestParseBattleEnterNotTrial 守护「试炼外战斗不进遇见记录」。
//
// 0x1316 不是试炼专属 opcode,野外与 PVP 战斗也走它。判定依据是消息里的
// grass_trial_battle_info —— 没有它就不是试炼战斗,调用方据此丢弃。
// 这条要是失效,野生宠物战斗会被误记进试炼的图里,进度全错。
func TestParseBattleEnterNotTrial(t *testing.T) {
	// 只有 init_info + enemy_team,没有 #25 grass_trial_battle_info
	team := appendMsg(nil, 2, appendMsg(nil, 2, appendVarint(nil, 15, 3001)))
	body := appendMsg(nil, 6, appendMsg(nil, 6, team))

	e := ParseBattleEnter(body)
	if e == nil {
		t.Fatal("解析失败")
	}
	if e.Type != BattleUnknown {
		t.Errorf("Type = %d, 期望 BattleUnknown(没有 grass_trial_battle_info)", e.Type)
	}
	if e.Type.IsTrial() {
		t.Error("不带 grass_trial_battle_info 的不该判定为试炼战斗")
	}
	// 宠物仍解析出来了,但调用方会因 IsTrial()==false 丢弃
	if len(e.PetBases) != 1 {
		t.Errorf("PetBases = %v, 期望能解出 [3001]", e.PetBases)
	}
}

// TestBattleTypeLabels 守护战斗类型的中文名与试炼判定。
func TestBattleTypeLabels(t *testing.T) {
	for bt, want := range map[BattleType]string{
		BattleNormal: "普通", BattleBoss: "首领",
		BattleNPC: "NPC", BattleFinal: "最终BOSS",
	} {
		if got := bt.Label(); got != want {
			t.Errorf("BattleType(%d).Label() = %q, 期望 %q", bt, got, want)
		}
		if !bt.IsTrial() {
			t.Errorf("BattleType(%d) 应判定为试炼战斗", bt)
		}
	}
	if BattleUnknown.IsTrial() {
		t.Error("BattleUnknown 不该判定为试炼战斗")
	}
	if BattleUnknown.Label() != "" {
		t.Error("BattleUnknown 的 Label 应为空串")
	}
}

// TestParseBattleEnterEdge 覆盖空输入与空 pets。
func TestParseBattleEnterEdge(t *testing.T) {
	// 空输入返回 nil:调用方写 `e == nil || !e.Type.IsTrial()` 即可统一处理,
	// 不必再区分「解不出来」与「解出来但不是试炼」。
	if e := ParseBattleEnter(nil); e != nil {
		t.Errorf("空输入应返回 nil, 得到 %+v", e)
	}
	// 只有 init_info 没有 enemy_team
	body := appendMsg(nil, 6, appendMsg(nil, 25, appendVarint(nil, 3, 2)))
	e := ParseBattleEnter(body)
	if e == nil || e.Type != BattleNPC {
		t.Errorf("无 enemy_team 时 Type = %v, 期望 BattleNPC(2)", e)
	}
	if len(e.PetBases) != 0 {
		t.Errorf("无 enemy_team 时 PetBases = %v, 期望空", e.PetBases)
	}
}
