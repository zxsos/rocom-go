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

// battleMonster 构造一条 BattleMonsterInfo{state(2), npc_obj_id(16)}。
func battleMonster(state int, npcObjID uint64) []byte {
	var m []byte
	m = protowire.AppendTag(m, 2, protowire.VarintType)
	m = protowire.AppendVarint(m, uint64(state))
	m = protowire.AppendTag(m, 16, protowire.VarintType)
	m = protowire.AppendVarint(m, npcObjID)
	return m
}

// battleFinish 把若干 BattleMonsterInfo 包成 ZoneBattleFinishNotify:settle_info(1).monster_info(8)。
func battleFinish(monsters ...[]byte) []byte {
	var si []byte
	for _, m := range monsters {
		si = protowire.AppendTag(si, 8, protowire.BytesType)
		si = protowire.AppendBytes(si, m)
	}
	var body []byte
	body = protowire.AppendTag(body, 1, protowire.BytesType)
	body = protowire.AppendBytes(body, si)
	return body
}

// TestParseBattleGoneNpcs 用两份真实结算:2026-08-02 污染爬爬被**捉走**、
// 同日污染矿晶虫被**打死**。两者对地图标记是一回事——那儿已经没这只了。
func TestParseBattleGoneNpcs(t *testing.T) {
	got := ParseBattleGoneNpcs(battleFinish(
		battleMonster(3, 0), // 我方队伍成员:存活、npc_obj_id 为 0,应被过滤
		battleMonster(battleMonsterCatched, 9279722995167795223), // 爬爬,捉走
	))
	if len(got) != 1 || got[0] != 9279722995167795223 {
		t.Fatalf("应只回被捉走的那只, got %v", got)
	}
	got = ParseBattleGoneNpcs(battleFinish(
		battleMonster(3, 0),
		battleMonster(battleMonsterDefeated, 9279722995167807470), // 矿晶虫,打死
	))
	if len(got) != 1 || got[0] != 9279722995167807470 {
		t.Fatalf("应只回被打死的那只, got %v", got)
	}
}

// TestParseBattleGoneNpcsStay:逃跑(2)/存活(3)不算消失——打输或它跑了,走回去还能再遇上,
// 标记该继续按「离开 AOI」置灰,不能当场撤。
func TestParseBattleGoneNpcsStay(t *testing.T) {
	for _, st := range []int{2, 3} {
		if got := ParseBattleGoneNpcs(battleFinish(battleMonster(st, 999))); len(got) != 0 {
			t.Errorf("state=%d 不该算消失, got %v", st, got)
		}
	}
	// state 字段缺省时不采信(DEFEATED 恰是枚举 0,宁可少撤也不误撤,见 ParseBattleGoneNpcs)。
	var m []byte
	m = protowire.AppendTag(m, 16, protowire.VarintType)
	m = protowire.AppendVarint(m, 999)
	if got := ParseBattleGoneNpcs(battleFinish(m)); len(got) != 0 {
		t.Errorf("state 缺省不该算消失, got %v", got)
	}
}
