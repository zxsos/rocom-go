package trial

import (
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
)

// 以下字段号经 `go run ./cmd/fielddump GrassTrial` 从游戏描述符 all.pb 逐条核对,
// 与 docs/pcap-20260831-grass-trial.md 的报文时序一致。

// 用 protowire 手工拼一份 GrassTrialChallengeData,喂给 ParseChallengeData 断言字段号。
// 不依赖任何真实抓包,故测试可离线、可复现;字段号错了测试会红,即变异守卫。
// TestParseChallengeDataInitialFeatures 守局级 #33(initial_feature_ids)。
//
// 它存在的意义是**区分宠物的天生特性与试炼中获得的特性**:
// acquired(#11,宠物级)是累积追加的流水,initial(#33,局级)整局不变,
// 两者之差就是本局拿到的。实测 17 场战斗的特性序列印证了这点 ——
// 288135 从头到尾都在(天生),其余逐个累积(获得)。
// 抓错字段号会解成空,两组拆分随之失效(且**不报错**,只是静默变成「不区分」)。
func TestParseChallengeDataInitialFeatures(t *testing.T) {
	var body []byte
	body = protowire.AppendTag(body, 1, protowire.VarintType) // state
	body = protowire.AppendVarint(body, 2)
	for _, id := range []uint64{288135} { // initial_feature_ids(非 packed)
		body = protowire.AppendTag(body, 33, protowire.VarintType)
		body = protowire.AppendVarint(body, id)
	}
	c := ParseChallengeData(body)
	if c == nil {
		t.Fatal("ParseChallengeData 返回 nil")
	}
	if len(c.InitialFeatures) != 1 || c.InitialFeatures[0] != 288135 {
		t.Fatalf("InitialFeatures = %v, 期望 [288135](字段号是 33 吗?)", c.InitialFeatures)
	}
	if !c.InitialFeatures.Has(288135) {
		t.Error("Has(288135) 应为 true")
	}
	if c.InitialFeatures.Has(288001) {
		t.Error("Has(288001) 应为 false —— 它不在天生列表里")
	}

	// packed 编码也要认(协议允许两种,repeated uint32 很常见)
	var pk []byte
	var packed []byte
	packed = protowire.AppendVarint(packed, 288135)
	packed = protowire.AppendVarint(packed, 288001)
	pk = protowire.AppendTag(pk, 33, protowire.BytesType)
	pk = protowire.AppendBytes(pk, packed)
	c2 := ParseChallengeData(pk)
	if len(c2.InitialFeatures) != 2 {
		t.Errorf("packed 解出 %d 个, 期望 2: %v", len(c2.InitialFeatures), c2.InitialFeatures)
	}
}

// TestIsFeatureID 锁住三类 id 的区间判据。
//
// 协议不下发类型字段,类别全靠区间分(逐条对照 updated_pet 的落点得出,
// 见 docs/pcap-20260831-grass-trial.md 2.1)。判据写宽一点点,技能就会被当成
// 特性、拿精灵的特性名去绑 —— 名字绑在错误的 id 上,比不绑糟糕得多。
// 前端 web/src/pages/trial/Trial.jsx 的 isFeatureID 是同一套判据,改一处要改两处。
func TestIsFeatureID(t *testing.T) {
	yes := []uint32{288000, 288001, 288135, 288999}
	no := []uint32{0, 1999, 2016, 3005, 7020500, 7880058, 289000, 4000000}
	for _, id := range yes {
		if !IsFeatureID(id) {
			t.Errorf("IsFeatureID(%d) 应为 true", id)
		}
	}
	for _, id := range no {
		if IsFeatureID(id) {
			t.Errorf("IsFeatureID(%d) 应为 false", id)
		}
	}
}

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

// TestParseReviewSkills 断言历史战绩里带出了技能明细(字段 9 review_skills)。
//
// 这个字段是「试炼专属 id(788 段)」的**唯一已知来源**:当前局只见过 7880058,
// 而 7880000~7880071 共 27 个 id 只在历史战绩里出现。早先没解析它,才把它们
// 当成「资料站没收录」而漏掉 —— 有了字段 9,现在能拿到每个技能的融合后威力/
// 能耗/类型/槽位,足以人工在游戏里核对出真身。
func TestParseReviewSkills(t *testing.T) {
	// 一个攻击技能(威力 85 能耗 3 type 1 槽 5)+ 一个状态技能(威力 0 能耗 4 type 2 槽 7)
	var atk, buf []byte
	atk = protowire.AppendTag(atk, 1, protowire.VarintType) // base_skill_id
	atk = protowire.AppendVarint(atk, 7880056)
	atk = protowire.AppendTag(atk, 2, protowire.VarintType) // fused_power
	atk = protowire.AppendVarint(atk, 85)
	atk = protowire.AppendTag(atk, 3, protowire.VarintType) // fused_energy_cost
	atk = protowire.AppendVarint(atk, 3)
	atk = protowire.AppendTag(atk, 5, protowire.VarintType) // skill_type
	atk = protowire.AppendVarint(atk, 1)
	atk = protowire.AppendTag(atk, 7, protowire.VarintType) // slot_pos
	atk = protowire.AppendVarint(atk, 5)

	var st []byte
	st = protowire.AppendTag(st, 1, protowire.VarintType)
	st = protowire.AppendVarint(st, 7880062)
	st = protowire.AppendTag(st, 3, protowire.VarintType)
	st = protowire.AppendVarint(st, 4)
	st = protowire.AppendTag(st, 5, protowire.VarintType)
	st = protowire.AppendVarint(st, 2)
	st = protowire.AppendTag(st, 7, protowire.VarintType)
	st = protowire.AppendVarint(st, 7)

	buf = protowire.AppendTag(buf, 2, protowire.VarintType) // petbase_conf_id
	buf = protowire.AppendVarint(buf, 3569)
	buf = protowire.AppendTag(buf, 9, protowire.BytesType) // review_skills
	buf = protowire.AppendBytes(buf, atk)
	buf = protowire.AppendTag(buf, 9, protowire.BytesType)
	buf = protowire.AppendBytes(buf, st)

	r := ParseReview(buf)
	if r.PetBaseID != 3569 {
		t.Fatalf("PetBaseID = %d, 期望 3569", r.PetBaseID)
	}
	if len(r.Skills) != 2 {
		t.Fatalf("解析出 %d 个技能, 期望 2", len(r.Skills))
	}
	want := []Skill{
		{BaseID: 7880056, Power: 85, EnergyCost: 3, SkillType: 1, SlotPos: 5},
		{BaseID: 7880062, Power: 0, EnergyCost: 4, SkillType: 2, SlotPos: 7},
	}
	for i, w := range want {
		got := r.Skills[i]
		// Skill 含 slice(Merged),不能直接用 == 比较
		if got.BaseID != w.BaseID || got.Power != w.Power ||
			got.EnergyCost != w.EnergyCost || got.SkillType != w.SkillType || got.SlotPos != w.SlotPos {
			t.Errorf("Skills[%d] = %+v, 期望 %+v", i, got, w)
		}
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
	if len(l2.DiscoveredIDs) != 3 || l2.DiscoveredIDs[0] != 1 ||
		l2.DiscoveredIDs[1] != 2 || l2.DiscoveredIDs[2] != 3 {
		t.Errorf("packed 的 id 列表 = %v, 期望 [1 2 3]", l2.DiscoveredIDs)
	}
}

// TestParseLogRecordIDs 守「见闻录必须吐出**具体 id** 而非只有数量」。
//
// 这条曾真的坏过:原实现只 `Discovered++` 计数、把 id 全丢了。后果是登录后
// 无法补录历史 —— 已遇见的几百只精灵全靠重新打一遍才会显示,而服务器明明
// 在 0x1975 里下发了完整清单(实测 292 只 vs 抓包 17 场)。
// 计数对、列表空,这种情况光看 Discovered 字段发现不了,故单独断言。
func TestParseLogRecordIDs(t *testing.T) {
	var b []byte
	b = protowire.AppendTag(b, 1, protowire.VarintType) // log_conf_id = 101
	b = protowire.AppendVarint(b, 101)
	for _, id := range []uint64{3001, 3005, 8101, 3001} { // 故意重复一只,测不去重
		b = protowire.AppendTag(b, 3, protowire.VarintType)
		b = protowire.AppendVarint(b, id)
	}
	b = protowire.AppendTag(b, 6, protowire.VarintType) // total
	b = protowire.AppendVarint(b, 337)

	l := ParseLogRecord(b)
	if l.LogConfID != 101 {
		t.Fatalf("LogConfID = %d, 期望 101", l.LogConfID)
	}
	// 关键:具体 id 必须保留(顺序与重复都保留,去重交给 store 层)
	want := []uint32{3001, 3005, 8101, 3001}
	if len(l.DiscoveredIDs) != len(want) {
		t.Fatalf("DiscoveredIDs = %v, 期望 %v(只拿到 %d 个,是否又退化成只计数了?)",
			l.DiscoveredIDs, want, len(l.DiscoveredIDs))
	}
	for i := range want {
		if l.DiscoveredIDs[i] != want[i] {
			t.Errorf("DiscoveredIDs[%d] = %d, 期望 %d", i, l.DiscoveredIDs[i], want[i])
		}
	}
	if l.Discovered != uint32(len(l.DiscoveredIDs)) {
		t.Errorf("Discovered = %d, 应等于 len(DiscoveredIDs) = %d",
			l.Discovered, len(l.DiscoveredIDs))
	}
	// 章节映射:认不出来的册号必须返回 0,让调用方丢弃而非硬套
	if got := l.ChapterOf(); got != 2 {
		t.Errorf("log_conf_id=101 应映射到第 2 章, 实际 %d", got)
	}
	for cid, want := range map[uint32]uint32{100: 1, 101: 2, 102: 3} {
		r := &LogRecord{LogConfID: cid}
		if got := r.ChapterOf(); got != want {
			t.Errorf("log_conf_id=%d 应映射到第 %d 章, 实际 %d", cid, want, got)
		}
	}
	for _, cid := range []uint32{0, 99, 103, 200, 999} {
		r := &LogRecord{LogConfID: cid}
		if got := r.ChapterOf(); got != 0 {
			t.Errorf("log_conf_id=%d 认不出来应返回 0, 实际 %d(会污染别的章)", cid, got)
		}
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
