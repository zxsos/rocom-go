package gamedata

import "testing"

// TestCalcRaceAnchors 用**抓包实测**的种族值钉住桥接结果。
//
// 这四个形态的种族值来自本机解析的 PVP 战局(2026-09-07,早于 S4 前瞻调整),
// 是「桥接没接错」的唯一可信锚点:roco 快照里用的是自有哈希 id,桥接错了
// (比如名字撞车)只会静默给出另一个形态的数值。
func TestCalcRaceAnchors(t *testing.T) {
	db, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := map[uint32]map[string]int{
		3141: {"hp": 122, "physicalAttack": 67, "magicalAttack": 72, "physicalDefense": 84, "magicalDefense": 94, "speed": 100},
		3179: {"hp": 100, "physicalAttack": 147, "magicalAttack": 137, "physicalDefense": 108, "magicalDefense": 89, "speed": 115},
		3069: {"hp": 128, "physicalAttack": 77, "magicalAttack": 75, "physicalDefense": 101, "magicalDefense": 81, "speed": 105},
		3400: {"hp": 81, "physicalAttack": 78, "magicalAttack": 78, "physicalDefense": 76, "magicalDefense": 96, "speed": 100},
	}
	for id, w := range want {
		got, ok := db.CalcRaceOf(id)
		if !ok {
			t.Fatalf("petbase %d 未收录(桥接失败)", id)
		}
		for k, v := range w {
			if got.Race[k] != v {
				t.Errorf("petbase %d(%s).%s = %d, 期望 %d", id, got.Name, k, got.Race[k], v)
			}
		}
	}
}

func TestCalcRaceTypes(t *testing.T) {
	db, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got, ok := db.CalcRaceOf(3141) // 花衣蝶:虫 + 草
	if !ok {
		t.Fatal("未收录 3141")
	}
	if len(got.Types) != 2 || got.Types[0] != "虫" || got.Types[1] != "草" {
		t.Errorf("系别 = %v", got.Types)
	}
}

func TestCalcSkillPower(t *testing.T) {
	db, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// 虫击:静态威力 90。这里锁的是「技能侧的桥接」——按名字接到 roco 的技能表。
	got, ok := db.CalcSkillOf(7130150)
	if !ok {
		t.Fatal("未收录 7130150(虫击)")
	}
	if got.Name != "虫击" || got.BasePower == nil || *got.BasePower != 90 {
		t.Errorf("虫击 = %+v", got)
	}
	if got.Cost == nil || *got.Cost != 3 {
		t.Errorf("虫击能耗 = %v", got.Cost)
	}
	// 变化技能没有威力:不能拿 0 冒充。
	if s, ok := db.CalcSkillOf(7130320); !ok || s.BasePower != nil {
		t.Errorf("虫结阵应为无威力技能, 实得 %+v (ok=%v)", s, ok)
	}
}

func TestCalcMarks(t *testing.T) {
	db, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	marks := db.CalcMarks()
	if len(marks) == 0 {
		t.Fatal("缺印记表")
	}
	// 蓄势:每层攻击技能威力 +30%,参与本次伤害
	m, ok := db.CalcMarkByName("蓄势")
	if !ok {
		t.Fatal("未收录「蓄势」")
	}
	if !m.AffectsThisHit || m.Per != 30 || m.Unit != "percent" || m.Affects != "power" {
		t.Errorf("蓄势 = %+v", m)
	}
	// 中毒:结算在回合末,**不能**算进本次伤害(roco 明确「本次技能不追加伤害」)
	p, ok := db.CalcMarkByName("中毒")
	if !ok {
		t.Fatal("未收录「中毒」")
	}
	if p.AffectsThisHit {
		t.Errorf("中毒不该参与本次伤害: %+v", p)
	}
	// 风起需要「先手」:条件丢了就会无条件加成,伤害凭空变高
	w, ok := db.CalcMarkByName("风起")
	if !ok || len(w.Needs) == 0 || w.Needs[0] != "先手" {
		t.Errorf("风起的前置条件缺失: %+v", w)
	}
}

func TestCalcTraits(t *testing.T) {
	db, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	traits := db.CalcTraits()
	if len(traits) == 0 {
		t.Fatal("缺特性规则表")
	}
	// 专注力是快照里唯一带 ruleId 的特性,参数 multiplier=2(有据可查)
	f, ok := db.CalcTraitByName("专注力")
	if !ok {
		t.Fatal("未收录「专注力」")
	}
	if f.Rule != "physical_power_first_turn" || !f.Implemented {
		t.Errorf("专注力 = %+v", f)
	}
	if got := f.Params["multiplier"]; got != float64(2) {
		t.Errorf("专注力 multiplier = %v, 期望 2(快照 ruleParams 给的)", got)
	}
	if len(f.Needs) == 0 || f.Needs[0] != "首回合" {
		t.Errorf("专注力的触发条件缺失: %+v", f)
	}
	// 未实现的规则必须显式标出来,不能悄悄当「无效果」参与计算
	unknown := 0
	for _, tr := range traits {
		if !tr.Implemented {
			unknown++
		}
	}
	if unknown > 0 {
		t.Logf("未实现的特性规则 %d 条(应显式标未支持,不参与计算)", unknown)
	}
}

func TestMarkNameOfBuffUnmapped(t *testing.T) {
	db, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// 未收录的 buff 必须返回 false —— 调用方据此显示原始 id,不猜名字。
	// (calc_mark_ids.json 需在有解包数据的机器上跑 gen_markids.py 才有内容)
	if _, ok := db.MarkNameOfBuff(20010090); ok {
		t.Log("该 buff 已收录(说明跑过 gen_markids.py);若未跑过则应返回 false")
	}
	if _, ok := db.MarkNameOfBuff(99999999); ok {
		t.Error("不存在的 buff_id 不该命中")
	}
}

func TestCalcTypeChart(t *testing.T) {
	db, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	tc := db.CalcTypeChart()
	if tc == nil {
		t.Fatal("缺克制表")
	}
	if len(tc.Types) != 18 {
		t.Fatalf("系别数 = %d", len(tc.Types))
	}
	if len(tc.Matrix) != len(tc.Types) {
		t.Fatalf("矩阵行数 = %d", len(tc.Matrix))
	}
	if tc.Clamp.Max != 3 || tc.Clamp.Min != 0.25 {
		t.Errorf("钳制 = %+v", tc.Clamp)
	}
	// 火 → 草 应为 2:矩阵行列顺序写反了这里会红(攻在前、守在后)。
	idx := func(name string) int {
		for i, n := range tc.Types {
			if n == name {
				return i
			}
		}
		t.Fatalf("缺系别 %s", name)
		return 0
	}
	if got := tc.Matrix[idx("火")][idx("草")]; got != 2 {
		t.Errorf("火→草 = %v, 期望 2", got)
	}
	if got := tc.Matrix[idx("草")][idx("火")]; got != 0.5 {
		t.Errorf("草→火 = %v, 期望 0.5", got)
	}
}
