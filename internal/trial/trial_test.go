package trial

import (
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
)

// 以下字段号经 `go run ./cmd/fielddump GrassTrial` 从游戏描述符 all.pb 逐条核对,
// 与 docs/pcap-20260831-grass-trial.md 的报文时序一致。

// 用 protowire 手工拼一份 GrassTrialChallengeData,喂给 ParseChallengeData 断言字段号。
// 不依赖任何真实抓包,故测试可离线、可复现;字段号错了测试会红,即变异守卫。
func TestParseChallengeData(t *testing.T) {
	var body []byte
	body = protowire.AppendTag(body, 1, protowire.VarintType) // state
	body = protowire.AppendVarint(body, 2)
	body = protowire.AppendTag(body, 2, protowire.VarintType) // trial_conf_id
	body = protowire.AppendVarint(body, 10002)
	body = protowire.AppendTag(body, 3, protowire.VarintType) // current_chapter_id
	body = protowire.AppendVarint(body, 3001)
	body = protowire.AppendTag(body, 4, protowire.VarintType) // current_node_index
	body = protowire.AppendVarint(body, 5)
	body = protowire.AppendTag(body, 7, protowire.VarintType) // remaining_coin
	body = protowire.AppendVarint(body, 18)
	body = protowire.AppendTag(body, 26, protowire.VarintType) // slot_id
	body = protowire.AppendVarint(body, 1000)

	c := ParseChallengeData(body)
	if c == nil {
		t.Fatal("ParseChallengeData 返回 nil")
	}
	if c.State != 2 || c.TrialConfID != 10002 || c.ChapterID != 3001 ||
		c.NodeIndex != 5 || c.Coin != 18 || c.SlotID != 1000 {
		t.Errorf("字段对不上: %+v", c)
	}
}

func TestParsePet(t *testing.T) {
	var pet []byte
	pet = protowire.AppendTag(pet, 1, protowire.VarintType) // pet_gid
	pet = protowire.AppendVarint(pet, 133)
	pet = protowire.AppendTag(pet, 2, protowire.VarintType) // base_conf_id
	pet = protowire.AppendVarint(pet, 3569)
	pet = protowire.AppendTag(pet, 3, protowire.VarintType) // current_hp
	pet = protowire.AppendVarint(pet, 264)
	pet = protowire.AppendTag(pet, 4, protowire.VarintType) // max_hp
	pet = protowire.AppendVarint(pet, 389)

	var skill []byte
	skill = protowire.AppendTag(skill, 1, protowire.VarintType) // base_skill_id
	skill = protowire.AppendVarint(skill, 7020500)
	skill = protowire.AppendTag(skill, 2, protowire.VarintType) // fused_power
	skill = protowire.AppendVarint(skill, 25)
	skill = protowire.AppendTag(skill, 4, protowire.VarintType) // fusion_count
	skill = protowire.AppendVarint(skill, 1)
	skill = protowire.AppendTag(skill, 7, protowire.VarintType) // slot_pos
	skill = protowire.AppendVarint(skill, 2)

	pet = protowire.AppendTag(pet, 10, protowire.BytesType) // skills
	pet = protowire.AppendBytes(pet, skill)

	p := ParsePet(pet)
	if p == nil {
		t.Fatal("ParsePet 返回 nil")
	}
	if p.Gid != 133 || p.BaseConfID != 3569 || p.HP != 264 || p.MaxHP != 389 {
		t.Errorf("基础字段对不上: %+v", p)
	}
	if len(p.Skills) != 1 {
		t.Fatalf("技能数 = %d, 期望 1", len(p.Skills))
	}
	s := p.Skills[0]
	if s.BaseID != 7020500 || s.Power != 25 || s.FusionCount != 1 || s.SlotPos != 2 {
		t.Errorf("技能字段对不上: %+v", s)
	}
}

// TestParsePetInnerData 断言内嵌 PetData(字段 13)里的 conf_id(2)与 name(3)被取到。
func TestParsePetInnerData(t *testing.T) {
	var pd []byte
	pd = protowire.AppendTag(pd, 2, protowire.VarintType) // conf_id
	pd = protowire.AppendVarint(pd, 3568003)
	pd = protowire.AppendTag(pd, 3, protowire.BytesType) // name
	pd = protowire.AppendBytes(pd, []byte("黑猫巫师"))

	var pet []byte
	pet = protowire.AppendTag(pet, 13, protowire.BytesType) // pet_data
	pet = protowire.AppendBytes(pet, pd)

	p := ParsePet(pet)
	if p == nil || p.ConfID != 3568003 || p.Name != "黑猫巫师" {
		t.Errorf("内嵌 PetData 没取到: %+v", p)
	}
}

// TestParseSlotProgress 断言只收 is_cleared=true 的 trial_conf_id。
func TestParseSlotProgress(t *testing.T) {
	// 一个已通关、一个未通关
	var reward1 []byte
	reward1 = protowire.AppendTag(reward1, 1, protowire.VarintType) // trial_conf_id
	reward1 = protowire.AppendVarint(reward1, 10002)
	reward1 = protowire.AppendTag(reward1, 3, protowire.VarintType) // is_cleared
	reward1 = protowire.AppendVarint(reward1, 1)

	var reward2 []byte
	reward2 = protowire.AppendTag(reward2, 1, protowire.VarintType)
	reward2 = protowire.AppendVarint(reward2, 10001)
	// 无 is_cleared(或为 0)= 未通关

	var slot []byte
	slot = protowire.AppendTag(slot, 1, protowire.VarintType) // slot_id
	slot = protowire.AppendVarint(slot, 1000)
	slot = protowire.AppendTag(slot, 5, protowire.VarintType) // dam_type
	slot = protowire.AppendVarint(slot, 2)
	slot = protowire.AppendTag(slot, 4, protowire.BytesType) // rewards
	slot = protowire.AppendBytes(slot, reward1)
	slot = protowire.AppendTag(slot, 4, protowire.BytesType)
	slot = protowire.AppendBytes(slot, reward2)

	s := ParseSlotProgress(slot)
	if s.SlotID != 1000 || s.DamType != 2 {
		t.Errorf("槽位字段对不上: %+v", s)
	}
	if len(s.ClearedIDs) != 1 || s.ClearedIDs[0] != 10002 {
		t.Errorf("ClearedIDs = %v, 期望只含 10002", s.ClearedIDs)
	}
}

// TestParseLogRecord 断言见闻录的 discovered 计数兼容 packed 与非 packed 两种编码。
// 实测字段 3 是非 packed(392 个独立 tag),但协议允许 packed,两种都要对。
func TestParseLogRecord(t *testing.T) {
	// 非 packed:三个独立 varint
	var np []byte
	np = protowire.AppendTag(np, 1, protowire.VarintType) // log_conf_id
	np = protowire.AppendVarint(np, 100)
	np = protowire.AppendTag(np, 3, protowire.VarintType) // discovered(非 packed 的第 1 个)
	np = protowire.AppendVarint(np, 3001)
	np = protowire.AppendTag(np, 3, protowire.VarintType)
	np = protowire.AppendVarint(np, 3002)
	np = protowire.AppendTag(np, 3, protowire.VarintType)
	np = protowire.AppendVarint(np, 3003)
	np = protowire.AppendTag(np, 6, protowire.VarintType) // total
	np = protowire.AppendVarint(np, 210)

	l := ParseLogRecord(np)
	if l.Discovered != 3 || l.Total != 210 || l.LogConfID != 100 {
		t.Errorf("非 packed 见闻录对不上: %+v", l)
	}

	// packed:一个 bytes 内含三个 varint
	var pk []byte
	pk = protowire.AppendTag(pk, 3, protowire.BytesType)
	var packed []byte
	packed = protowire.AppendVarint(packed, 1)
	packed = protowire.AppendVarint(packed, 2)
	packed = protowire.AppendVarint(packed, 3)
	pk = protowire.AppendBytes(pk, packed)

	l2 := ParseLogRecord(pk)
	if l2.Discovered != 3 {
		t.Errorf("packed 见闻录 discovered = %d, 期望 3", l2.Discovered)
	}
}

// TestParseRewardNotify 断言奖励通知的关键字段,以及「空通知」返回 nil。
func TestParseRewardNotify(t *testing.T) {
	var b []byte
	b = protowire.AppendTag(b, 1, protowire.VarintType) // event_conf_id
	b = protowire.AppendVarint(b, 110005)
	b = protowire.AppendTag(b, 2, protowire.VarintType) // reward_id
	b = protowire.AppendVarint(b, 288001)
	b = protowire.AppendTag(b, 3, protowire.VarintType) // cur_coin
	b = protowire.AppendVarint(b, 10)

	r := ParseRewardNotify(b)
	if r == nil || r.EventConfID != 110005 || r.RewardID != 288001 || r.Coin != 10 {
		t.Errorf("奖励通知对不上: %+v", r)
	}

	if ParseRewardNotify([]byte("tsf4g")) != nil {
		t.Error("空通知应返回 nil")
	}
}

// TestParseNextNodeReq 断言 c2s 子头(6 字节)被正确跳过。
func TestParseNextNodeReq(t *testing.T) {
	var body []byte
	body = append(body, 0xde, 0xad, 0xbe, 0xef, 0x00, 0x08) // 6 字节子头
	body = protowire.AppendTag(body, 1, protowire.VarintType)
	body = protowire.AppendVarint(body, 3002)
	body = protowire.AppendTag(body, 2, protowire.VarintType)
	body = protowire.AppendVarint(body, 7)
	body = append(body, "tsf4g\x00\x01"...)

	n, ok := ParseNextNodeReq(body)
	if !ok || n.ChapterID != 3002 || n.NodeIndex != 7 {
		t.Errorf("节点推进请求对不上: %+v ok=%v", n, ok)
	}
}

// TestParseRewardAction 断言 action 字段(处理奖励的方式)。
func TestParseRewardReq(t *testing.T) {
	var body []byte
	body = append(body, 0xde, 0xad, 0xbe, 0xef, 0x00, 0x08)
	body = protowire.AppendTag(body, 1, protowire.VarintType) // action
	body = protowire.AppendVarint(body, 1)                    // 作为新技能
	body = protowire.AppendTag(body, 2, protowire.VarintType) // reward_id
	body = protowire.AppendVarint(body, 7130130)

	a := ParseRewardReq(body)
	if a.Action != 1 || a.RewardID != 7130130 {
		t.Errorf("奖励处理请求对不上: %+v", a)
	}
}
