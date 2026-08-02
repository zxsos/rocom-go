package scene

import (
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
)

// throwActs 构造一条 0x0414 body:acts(1) → throw_catch_notify(173) =
// {npc_id(3), is_catch_success(4), npc_catch_infos(11){npc_id(1), is_catch_success(2)}}。
// withInfos=false 时只带顶层那对(older 包体形状)。
func throwActs(npcID uint64, success, withInfos bool) []byte {
	var nty []byte
	nty = protowire.AppendTag(nty, 3, protowire.VarintType)
	nty = protowire.AppendVarint(nty, npcID)
	if success {
		nty = protowire.AppendTag(nty, 4, protowire.VarintType)
		nty = protowire.AppendVarint(nty, 1)
	}
	if withInfos {
		var info []byte
		info = protowire.AppendTag(info, 1, protowire.VarintType)
		info = protowire.AppendVarint(info, npcID)
		if success {
			info = protowire.AppendTag(info, 2, protowire.VarintType)
			info = protowire.AppendVarint(info, 1)
		}
		nty = protowire.AppendTag(nty, 11, protowire.BytesType)
		nty = protowire.AppendBytes(nty, info)
	}
	var acts []byte
	acts = protowire.AppendTag(acts, 173, protowire.BytesType)
	acts = protowire.AppendBytes(acts, nty)
	var body []byte
	body = protowire.AppendTag(body, 1, protowire.BytesType)
	body = protowire.AppendBytes(body, acts)
	return body
}

// TestParseCaughtByThrow 用 2026-08-02 那两只被丢球捉走的实体 id(珀尔鼬/友爱天天)。
func TestParseCaughtByThrow(t *testing.T) {
	got := ParseCaughtByThrow(throwActs(9279722995167884497, true, true))
	if len(got) != 1 || got[0] != 9279722995167884497 {
		t.Fatalf("成功捕捉应回一个 id, got %v", got)
	}
	// 顶层与 npc_catch_infos 指同一只,不能重复计。
	if len(ParseCaughtByThrow(throwActs(9279722995167884492, true, true))) != 1 {
		t.Error("同一 id 出现在两处时应去重")
	}
	// 只有顶层那对(无 npc_catch_infos)也要认。
	if got := ParseCaughtByThrow(throwActs(123, true, false)); len(got) != 1 || got[0] != 123 {
		t.Errorf("仅顶层字段时应回该 id, got %v", got)
	}
}

// TestParseCaughtByThrowFail:捕捉失败的 act 同样下发(is_catch_success 缺省为 false),
// 见到 act 就当捉到会把还在的标记误撤——16 份 pcap 里失败比成功还多。
func TestParseCaughtByThrowFail(t *testing.T) {
	if got := ParseCaughtByThrow(throwActs(9257243989227111044, false, true)); len(got) != 0 {
		t.Errorf("捕捉失败不该回 id, got %v", got)
	}
	// 扔道具/魔法的投掷物销毁:不带 npc_id,更不该命中。
	var acts []byte
	acts = protowire.AppendTag(acts, 173, protowire.BytesType)
	acts = protowire.AppendBytes(acts, nil)
	var body []byte
	body = protowire.AppendTag(body, 1, protowire.BytesType)
	body = protowire.AppendBytes(body, acts)
	if got := ParseCaughtByThrow(body); len(got) != 0 {
		t.Errorf("非捕捉的投掷不该回 id, got %v", got)
	}
}

// TestParseCaughtInBattle 用 2026-08-02 污染爬爬的战斗结算:
// settle_info(1) → monster_info(8){state(2), npc_obj_id(16)}。
func TestParseCaughtInBattle(t *testing.T) {
	monster := func(state int, npcObjID uint64) []byte {
		var m []byte
		m = protowire.AppendTag(m, 2, protowire.VarintType)
		m = protowire.AppendVarint(m, uint64(state))
		m = protowire.AppendTag(m, 16, protowire.VarintType)
		m = protowire.AppendVarint(m, npcObjID)
		return m
	}
	var si []byte
	si = protowire.AppendTag(si, 8, protowire.BytesType) // 我方队伍成员:存活、npc_obj_id 为 0
	si = protowire.AppendBytes(si, monster(3, 0))
	si = protowire.AppendTag(si, 8, protowire.BytesType) // 被捉走的野怪
	si = protowire.AppendBytes(si, monster(battleMonsterCatched, 9279722995167795223))
	var body []byte
	body = protowire.AppendTag(body, 1, protowire.BytesType)
	body = protowire.AppendBytes(body, si)

	got := ParseCaughtInBattle(body)
	if len(got) != 1 || got[0] != 9279722995167795223 {
		t.Fatalf("应只回被捉走的那只, got %v", got)
	}
	// 打败/逃跑的不算捉到。
	for _, st := range []int{0, 2, 3} {
		var si2 []byte
		si2 = protowire.AppendTag(si2, 8, protowire.BytesType)
		si2 = protowire.AppendBytes(si2, monster(st, 999))
		var b2 []byte
		b2 = protowire.AppendTag(b2, 1, protowire.BytesType)
		b2 = protowire.AppendBytes(b2, si2)
		if got := ParseCaughtInBattle(b2); len(got) != 0 {
			t.Errorf("state=%d 不该算捉到, got %v", st, got)
		}
	}
}
