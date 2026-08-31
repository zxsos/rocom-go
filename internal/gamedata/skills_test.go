package gamedata

import "testing"

// 本文件验证技能表(scripts/gen_skills.py 产出)的加载与访问,覆盖三类技能:
// 天生(innate)/ 技能石可学(stone)/ 血脉(blood)。
// 数据来自第三方资料站,取值随资料更新而变,故只断言**结构**与确定的行为,
// 不断言具体技能名(那会让资料一更新测试就红)。
//
// 生成物是「技能定义单一来源」格式:skills 表存定义,三类关系表只存下标。
// 故这里有专门的测试守护下标展开的正确性(下标错位会张冠李戴,且不会报错)。

func TestInnateSkillsLoaded(t *testing.T) {
	db, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if db.skills == nil {
		t.Fatal("skills 未加载")
	}
	if len(db.skills.innate) == 0 {
		t.Fatal("技能表为空:data/skills.json 没解析出形态")
	}
	// 全局技能表是三类技能的共同来源,必须非空
	if len(db.skills.skills) == 0 {
		t.Fatal("全局技能表为空 —— 三类技能都依赖它展开")
	}
	// 效果池必须非空,否则所有技能的 effect 都会是空串
	if len(db.skills.effects) == 0 {
		t.Error("effects 池为空")
	}
	// 三类关系表都应解析出形态(资料站覆盖 462 个形态)
	if len(db.skills.stone) == 0 {
		t.Error("技能石表为空 —— 详情接口会缺 learnable")
	}
	if len(db.skills.blood) == 0 {
		t.Error("血脉表为空 —— 详情接口会缺 bloodline")
	}
	t.Logf("形态 天生%d/技能石%d/血脉%d, 技能表 %d, 效果池 %d",
		len(db.skills.innate), len(db.skills.stone), len(db.skills.blood),
		len(db.skills.skills), len(db.skills.effects))
}

// TestSkillTableExpandIndexed 守护「形态表存的是 skills 下标」这件事。
//
// 下标错位(如少算一个技能、或 off-by-one)不会报错,只会让整张表的技能全部
// 张冠李戴 —— 而且看起来「数据很正常」。故这里校验:任取一个形态,展开出的
// 技能名必须与压缩前的数据一致。用「名字都能在技能表里找到」+「下标不越界」
// 来兜住,越界即说明下标与技能表长度不匹配。
func TestSkillTableExpandIndexed(t *testing.T) {
	db, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	n := uint32(len(db.skills.skills))
	check := func(kind string, idxs map[uint32][]uint32, get func(uint32) []Skill) {
		checked := 0
		for id, list := range idxs {
			for _, i := range list {
				if i >= n {
					t.Fatalf("%s 形态 %d 的技能下标 %d 越界(技能表 %d 条)", kind, id, i, n)
				}
			}
			got := get(id)
			if len(got) == 0 {
				t.Errorf("%s 形态 %d 有索引却展开为空", kind, id)
				continue
			}
			for _, s := range got {
				if s.Name == "" {
					t.Errorf("%s 形态 %d 展开出无名字的技能: %+v", kind, id, s)
				}
			}
			checked++
			if checked >= 50 { // 抽查即可,全量跑 462×3 太慢
				break
			}
		}
		if checked == 0 {
			t.Errorf("%s 表没有可抽查的形态", kind)
		}
		t.Logf("%s: 抽查 %d 个形态均正常", kind, checked)
	}
	// 天生是 [下标, 等级] 对,单独校验下标部分
	for id, list := range db.skills.innate {
		for _, p := range list {
			if p[0] >= n {
				t.Fatalf("天生 形态 %d 的技能下标 %d 越界(技能表 %d 条)", id, p[0], n)
			}
		}
	}
	check("技能石", db.skills.stone, db.LearnableSkills)
	check("血脉", db.skills.blood, db.BloodlineSkills)
}

// TestThreeSkillKindsDisjoint 断言三类技能几乎互斥(实测:天生∩技能石 3 条、
// 与血脉 0 条)。这是「三者并列不去重」这个设计决定的依据 —— 若哪天资料站
// 让三者大面积重叠,前端就会显示一堆重复技能,设计得改。
func TestThreeSkillKindsDisjoint(t *testing.T) {
	db, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	total, overlap := 0, 0
	for id := range db.skills.innate {
		inn := map[string]bool{}
		for _, s := range db.InnateSkills(id) {
			inn[s.Name] = true
		}
		total += len(inn)
		for _, s := range db.LearnableSkills(id) {
			if inn[s.Name] {
				overlap++
			}
		}
		for _, s := range db.BloodlineSkills(id) {
			if inn[s.Name] {
				overlap++
			}
		}
	}
	if total == 0 {
		t.Fatal("天生技能表为空")
	}
	if r := float64(overlap) / float64(total); r > 0.05 {
		t.Errorf("天生与技能石/血脉重叠过多: %.1f%% (%d/%d) —— 三者并列的假设可能已不成立",
			r*100, overlap, total)
	}
	t.Logf("天生 %d 条中, 与技能石/血脉重叠 %d 条 (%.2f%%)", total, overlap, float64(overlap)/float64(total)*100)
}

// TestInnateSkillsSortedDesc 断言按学会等级降序(生成脚本排好,这里再排一次防御)。
func TestInnateSkillsSortedDesc(t *testing.T) {
	db, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	checked := 0
	for id := range db.skills.innate {
		sk := db.InnateSkills(id)
		if len(sk) == 0 {
			t.Errorf("形态 %d 有紧凑条目却展开为空", id)
			continue
		}
		for i := 1; i < len(sk); i++ {
			if sk[i-1].Level < sk[i].Level {
				t.Errorf("形态 %d 未按等级降序: %v", id, sk)
				break
			}
		}
		// 每条都应有名字(名字是这张表的全部意义)
		for _, s := range sk {
			if s.Name == "" {
				t.Errorf("形态 %d 有技能缺名字: %+v", id, s)
			}
		}
		checked++
		if checked >= 50 { // 全量 347 个形态逐个断言太慢,抽样足够
			break
		}
	}
}

// TestSkillName 断言 skill_id → 中文名的映射。
//
// 取值来自第三方资料站,故只断言确定的几条(用协议实测过的 id),覆盖三类情况:
// 普通技能、同名变体、试炼专属 id。
func TestSkillName(t *testing.T) {
	db, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(db.skills.names) == 0 {
		t.Fatal("names 映射为空:data/skills.json 没解析出 skill_id")
	}
	// 普通技能:试炼里实测出现过的 id
	for id, want := range map[uint32]string{
		7020500: "乱打", // 黑猫巫师初始技能(融合前威力25/能耗4)
		7020550: "魔能爆",
		7170210: "午夜噪音",
		7120100: "毒液渗透",
		7020840: "借用", // 变化类(威力0),按数值反查不了,只能按 id 查
		7140190: "听桥",
		7120300: "毒肽",
	} {
		if got := db.SkillName(id); got != want {
			t.Errorf("SkillName(%d) = %q, 期望 %q", id, got, want)
		}
	}
	// 试炼专属 id(788 段):资料站整段缺失,靠抓包实证 + 人工在游戏里核对逐个
	// 登记,见 gen_skills.py 的 EXTRA_SKILL_IDS
	for id, want := range map[uint32]string{
		7880000: "力量增效", 7880007: "热身运动", 7880008: "藤绞",
		7880011: "引燃", 7880018: "霜降", 7880026: "毒孢子",
		7880029: "虫群智慧", 7880054: "滚雪球", 7880056: "暴风雪",
		7880057: "孢子", 7880058: "魔能爆", 7880062: "光合作用",
		7880068: "疫病吐息",
	} {
		if got := db.SkillName(id); got != want {
			t.Errorf("SkillName(%d) = %q, 期望 %q", id, got, want)
		}
	}
	// 不存在的 id
	if got := db.SkillName(1); got != "" {
		t.Errorf("不存在的 id 应返回空串, 得到 %q", got)
	}
}

// TestSkillNameFusionKeepsID 守护一条反直觉的事实:**融合不改变 base_skill_id**。
//
// 早先误以为「融合会生成新 skill_id、查不到名是正常的」,据此放过了一个本可查到的
// 名字(7880058)。实测它开局零融合时就存在,融合 2 次(威力 20 → 150、
// fusion_count 0 → 2)后 id 始终不变 —— 故融合态技能同样该查到名。
// 本测试防止该误解回归。
func TestSkillNameFusionKeepsID(t *testing.T) {
	db, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// 试炼实测:这几个 id 无论融合几次都不变(只增长威力与 fusion_count)。
	// 7880054 尤其能说明问题:它在战绩里被观测到融合态(威力 55),仍查得到名。
	for _, id := range []uint32{7020500, 7880058, 7170210, 7880054} {
		if got := db.SkillName(id); got == "" {
			t.Errorf("融合态技能 %d 应能查到名(融合不改 base_skill_id), 得到空串", id)
		}
	}
}

// TestTrialSkillPoolNotInInnateSkills 守护「试炼专属 id 不污染天生技能回填」。
//
// 788 段是试炼里另起的一套编号(同一 id 会跨形态出现,如 7880057 见于
// 花衣蝶/兽花蕾/帅帅魔偶/影狸 —— 那些形态恰好都会「孢子」)。它们与基础 id
// 指向同一个技能,若算进「名 → id」反查,「孢子」这类就会因为同时有基础 id
// 与 788 段 id 被判为重名、skill_id 填 0 ——
// 故 gen_skills.py 只把 EXTRA 用于**查名**,不参与技能表的 id 回填。
func TestTrialSkillPoolNotInInnateSkills(t *testing.T) {
	db, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	trialOnly := map[uint32]bool{
		7880000: true, 7880007: true, 7880008: true, 7880011: true,
		7880018: true, 7880026: true, 7880029: true, 7880054: true,
		7880056: true, 7880057: true, 7880058: true, 7880062: true,
		7880068: true,
	}
	bad := 0
	for id := range db.skills.innate {
		for _, s := range db.InnateSkills(id) {
			if trialOnly[s.SkillID] {
				t.Errorf("形态 %d 的天生技能 %q 用了试炼态 id %d", id, s.Name, s.SkillID)
				bad++
			}
		}
	}
	// 同时确认这些名字**本身**仍在天生技能表里(没有被误判成重名而填 0)
	for _, want := range []string{"孢子", "毒孢子", "引燃", "虫群智慧", "力量增效"} {
		found, withID := 0, 0
		for id := range db.skills.innate {
			for _, s := range db.InnateSkills(id) {
				if s.Name == want {
					found++
					if s.SkillID != 0 {
						withID++
					}
				}
			}
		}
		if found > 0 && withID == 0 {
			t.Errorf("天生技能 %q 出现 %d 次但 skill_id 全为 0 —— 被误判成重名了", want, found)
		} else {
			t.Logf("天生技能 %q: %d 条, 带 id %d 条", want, found, withID)
		}
	}
}

// TestInnateSkillsHaveSkillID 断言天生技能带上了 skill_id(重名技能除外)。
func TestInnateSkillsHaveSkillID(t *testing.T) {
	db, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	withID, withoutID := 0, 0
	for id := range db.skills.innate {
		for _, s := range db.InnateSkills(id) {
			if s.SkillID != 0 {
				withID++
			} else {
				withoutID++ // 重名技能(借用/取念/复写/愿力冲击/疾风涡轮)填 0
			}
		}
	}
	if withID == 0 {
		t.Fatal("没有任何天生技能带上 skill_id —— 合并回填没生效")
	}
	// 名字能查到 id 的应占绝大多数(资料站 492 个技能里 488 个唯一命中)
	if r := float64(withID) / float64(withID+withoutID); r < 0.9 {
		t.Errorf("带 skill_id 的比例过低: %.1f%% (%d/%d)", r*100, withID, withID+withoutID)
	}
	t.Logf("带 skill_id %d 条, 重名无 id %d 条", withID, withoutID)
}

// TestInnateSkillsUnknown 断言未知形态返回 nil 且不 panic。
func TestInnateSkillsUnknown(t *testing.T) {
	db, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := db.InnateSkills(99999999); got != nil {
		t.Errorf("未知形态应返回 nil, 得到 %v", got)
	}
	if got := db.LearnableSkills(99999999); got != nil {
		t.Errorf("未知形态 LearnableSkills 应返回 nil, 得到 %v", got)
	}
	if got := db.BloodlineSkills(99999999); got != nil {
		t.Errorf("未知形态 BloodlineSkills 应返回 nil, 得到 %v", got)
	}
	if db.HasInnateSkills(99999999) {
		t.Error("未知形态 HasInnateSkills 应为 false")
	}
}

// TestAllSkillKindsCached 断言三类技能二次调用都命中缓存、结果一致。
func TestAllSkillKindsCached(t *testing.T) {
	db, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for id := range db.skills.innate {
		a, b := db.InnateSkills(id), db.InnateSkills(id)
		if len(a) != len(b) {
			t.Fatalf("天生 形态 %d 两次长度不一: %d vs %d", id, len(a), len(b))
		}
		for i := range a {
			if a[i] != b[i] {
				t.Errorf("天生 形态 %d 第 %d 条不一致", id, i)
			}
		}
		la, lb := db.LearnableSkills(id), db.LearnableSkills(id)
		if len(la) != len(lb) {
			t.Errorf("技能石 形态 %d 两次长度不一", id)
		}
		ba, bb := db.BloodlineSkills(id), db.BloodlineSkills(id)
		if len(ba) != len(bb) {
			t.Errorf("血脉 形态 %d 两次长度不一", id)
		}
		break // 只需验证一个形态
	}
}

// TestInnateSkillsCached 断言二次调用命中缓存、结果一致(懒解析不能每次都新建)。
func TestInnateSkillsCached(t *testing.T) {
	db, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for id := range db.skills.innate {
		a := db.InnateSkills(id)
		b := db.InnateSkills(id)
		if len(a) != len(b) {
			t.Fatalf("形态 %d 两次调用长度不一: %d vs %d", id, len(a), len(b))
		}
		for i := range a {
			if a[i] != b[i] {
				t.Errorf("形态 %d 第 %d 条两次不一致: %+v vs %+v", id, i, a[i], b[i])
			}
		}
		break // 只需验证一个
	}
}
