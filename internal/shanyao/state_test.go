package shanyao

import "testing"

// enterOf 造一份进战数据:我方两只(401/402)、对手一只(1)。
func enterOf() Enter {
	return Enter{
		BattleID: 100,
		Mode:     15,
		Round:    1,
		Self: Side{UIN: 100000002, Name: "示例玩家", Level: 68, HP: 6, HPMax: 6, Pets: []Pet{
			{GID: 1, PetID: 401, BaseConfID: 3141, Name: "小花仙", Level: 60, HPMax: 494, HP: 494, HasHP: true, OnField: true},
			{GID: 2, PetID: 402, BaseConfID: 3179, Name: "恶魔虫", Level: 60, HPMax: 391, HP: 391, HasHP: true},
		}},
		Foe: Side{UIN: 142792, Name: "林恩", Level: 68, HP: 6, HPMax: 6, Pets: []Pet{
			{GID: 11, PetID: 1, BaseConfID: 3735, Name: "机幕方舟", Level: 60, HPMax: 458, HP: 458, HasHP: true, OnField: true},
		}},
	}
}

func TestTrackerEnterRoundFinish(t *testing.T) {
	tr := NewTracker()
	if !tr.OnEnter(enterOf()) {
		t.Fatal("开局应算变化")
	}
	snap := tr.Snapshot()
	if !snap.Active || snap.Finished || snap.BattleID != 100 || snap.Round != 1 {
		t.Fatalf("开局快照 = %+v", snap)
	}
	if len(snap.Self.Pets) != 2 || len(snap.Foe.Pets) != 1 {
		t.Fatalf("双方宠物数 = %d/%d", len(snap.Self.Pets), len(snap.Foe.Pets))
	}

	// 回合包补齐对手第二只,并把我方 401 标为在场。
	r := Round{BattleID: 100, Round: 5, Self: Side{UIN: 100000002, Pets: []Pet{
		{GID: 1, PetID: 401, OnField: true},
	}}, Foe: Side{UIN: 142792, Pets: []Pet{
		{GID: 11, PetID: 1, OnField: true},
		{GID: 12, PetID: 3, BaseConfID: 3719, Name: "离心舞者", Level: 60, HPMax: 415, HP: 415, HasHP: true, OnField: true, Mutation: 8},
	}}}
	if !tr.OnRound(r) {
		t.Fatal("补齐对手应算变化")
	}
	snap = tr.Snapshot()
	if snap.Round != 5 {
		t.Errorf("回合号 = %d", snap.Round)
	}
	if len(snap.Foe.Pets) != 2 || snap.Foe.Pets[1].Name != "离心舞者" {
		t.Fatalf("对手 = %+v", snap.Foe.Pets)
	}
	if !snap.Self.Pets[0].OnField {
		t.Error("我方 401 应在场")
	}
	// 已存在的宠物不因回合包丢字段:名字/等级/上限都还在。
	if snap.Self.Pets[0].Name != "小花仙" || snap.Self.Pets[0].HPMax != 494 {
		t.Errorf("回合包覆盖了既有字段: %+v", snap.Self.Pets[0])
	}

	// 血量变化:401 → 120,并推进回合。
	if !tr.OnPerform(Perform{Round: 6, Updates: []HPUpdate{{PetID: 401, HP: 120, HasHP: true}}}) {
		t.Fatal("掉血应算变化")
	}
	snap = tr.Snapshot()
	if snap.Self.Pets[0].HP != 120 || snap.Self.Pets[0].Dead {
		t.Errorf("掉血后 = %+v", snap.Self.Pets[0])
	}
	// 同样的血量再来一次:不脏(一局近百条 0x1324,重复值不该触发广播)。
	if tr.OnPerform(Perform{Round: 6, Updates: []HPUpdate{{PetID: 401, HP: 120, HasHP: true}}}) {
		t.Error("血量未变的同步不该算变化")
	}
	// 打到 0 判倒下。
	if !tr.OnPerform(Perform{Round: 7, Updates: []HPUpdate{{PetID: 401, HP: 0, HasHP: true}}}) {
		t.Fatal("归零应算变化")
	}
	if p := tr.Snapshot().Self.Pets[0]; !p.Dead || p.HP != 0 {
		t.Errorf("归零后 = %+v", p)
	}

	// 结算:补最终血量、标已结束、写回双方体力。
	f := Finish{BattleID: 100, Result: 68, Monsters: []Monster{
		{GID: 1, PetID: 401, UIN: 100000002, Side: 1, State: MonsterDefeated, RemainHP: 0, MaxHP: 494},
		{GID: 11, PetID: 1, UIN: 142792, Side: 0, State: 3, RemainHP: 458, MaxHP: 458, Name: "机幕方舟"},
	}, Battlers: []Battler{{UIN: 100000002, HP: 0, HPMax: 6}, {UIN: 142792, HP: 4, HPMax: 6}}}
	if !tr.OnFinish(f) {
		t.Fatal("结算应算变化")
	}
	snap = tr.Snapshot()
	if !snap.Finished || snap.Result != 68 {
		t.Errorf("结算后 = %+v", snap)
	}
	if snap.Self.HP != 0 || snap.Foe.HP != 4 {
		t.Errorf("双方体力 = %d/%d", snap.Self.HP, snap.Foe.HP)
	}
	// side=1 归我方、side=0 归对手。写反时结算项会按 gid 落到**另一边**并新建一只
	// (gid 在本方查不到),故除了血量还要看双方宠物数有没有被塞进外来户。
	if len(snap.Self.Pets) != 2 || len(snap.Foe.Pets) != 2 {
		t.Fatalf("结算后双方宠物数 = %d/%d(应为 2/2)", len(snap.Self.Pets), len(snap.Foe.Pets))
	}
	if p := snap.Self.Pets[0]; p.HP != 0 || !p.Dead {
		t.Errorf("我方结算血量 = %+v", p)
	}
	if p := snap.Foe.Pets[0]; p.HP != 458 || p.Dead {
		t.Errorf("对手结算血量 = %+v", p)
	}
}

func TestTrackerOnFieldFollowsLatest(t *testing.T) {
	tr := NewTracker()
	e := enterOf()
	e.Self.Pets[0].OnField = false
	tr.OnEnter(e)

	// 回合包把它标为在场 → 应跟上;再下一回合不在场 → 应回落。
	// 「在场」是每回合重发的战斗态,不能沿用旧值。
	tr.OnRound(Round{BattleID: 100, Round: 2, Self: Side{UIN: 100000002, Pets: []Pet{
		{GID: 1, PetID: 401, OnField: true},
	}}})
	if p := tr.Snapshot().Self.Pets[0]; !p.OnField {
		t.Errorf("应已在场: %+v", p)
	}
	tr.OnRound(Round{BattleID: 100, Round: 3, Self: Side{UIN: 100000002, Pets: []Pet{
		{GID: 1, PetID: 401, OnField: false},
	}}})
	if p := tr.Snapshot().Self.Pets[0]; p.OnField {
		t.Errorf("应已离场: %+v", p)
	}
}

func TestTrackerFillsMissingFieldsLater(t *testing.T) {
	tr := NewTracker()
	e := enterOf()
	// 开局这只只有编号(对手未出场的宠物就是这样),名字/等级靠回合包补。
	e.Foe.Pets = []Pet{{GID: 30, PetID: 7}}
	tr.OnEnter(e)
	tr.OnRound(Round{BattleID: 100, Round: 4, Foe: Side{UIN: 142792, Pets: []Pet{
		{GID: 30, PetID: 7, BaseConfID: 3753, Name: "飞飞钥", Level: 60, HPMax: 543, HP: 543, HasHP: true, OnField: true},
	}}})
	p := tr.Snapshot().Foe.Pets[0]
	if p.Name != "飞飞钥" || p.BaseConfID != 3753 || p.Level != 60 || p.HPMax != 543 {
		t.Errorf("回合包补字段失败: %+v", p)
	}
}

// TestTrackerEnterFillsFullHP:开局双方满血,血条不能等到第一次伤害同步才显示。
//
// 只在进战包上这么推断(换上场的宠物可能带旧伤),故这里顺便锁住「回合包不补满血」。
func TestTrackerEnterFillsFullHP(t *testing.T) {
	tr := NewTracker()
	e := enterOf()
	e.Self.Pets[0].HP, e.Self.Pets[0].HasHP = 0, false
	tr.OnEnter(e)
	p := tr.Snapshot().Self.Pets[0]
	if !p.HasHP || p.HP != 494 {
		t.Errorf("开局应按上限补满血: %+v", p)
	}

	// 回合里新上场的宠物:血量未知就保持未知(不能假设满血)。
	tr.OnRound(Round{BattleID: 100, Round: 2, Foe: Side{UIN: 142792, Pets: []Pet{
		{GID: 99, PetID: 7, BaseConfID: 3753, Name: "飞飞钥", HPMax: 543},
	}}})
	fresh := tr.Snapshot().Foe.Pets[len(tr.Snapshot().Foe.Pets)-1]
	if fresh.HasHP {
		t.Errorf("回合里新上场的宠物不该被假设为满血: %+v", fresh)
	}
}

// TestTrackerSkillSync:0x1324 的技能同步给出**这一刻**的真实威力(含融合/被动),
// 要回填到对应宠物;重复值不该重复触发广播。
func TestTrackerSkillSync(t *testing.T) {
	tr := NewTracker()
	e := enterOf()
	e.Self.Pets[1].Skills = []Skill{{ID: 7130150, Cost: 3}}
	tr.OnEnter(e)

	sync := SkillSync{PetID: 402, SkillID: 7130150, Power: 112, HasPower: true, Hits: 3, Cost: 4, HasCost: true}
	if !tr.OnPerform(Perform{Round: 2, SkillSyncs: []SkillSync{sync}}) {
		t.Fatal("技能同步应算变化")
	}
	got := tr.Snapshot().Self.Pets[1].Skills
	if len(got) != 1 || !got[0].HasPower || got[0].Power != 112 || got[0].Hits != 3 || got[0].Cost != 4 {
		t.Fatalf("技能 = %+v", got)
	}
	if tr.OnPerform(Perform{Round: 2, SkillSyncs: []SkillSync{sync}}) {
		t.Error("同样的技能同步再来一次不该算变化")
	}
	// 没见过的技能也要记下来(临时获得/技能石),不能因为查不到就丢。
	if !tr.OnPerform(Perform{Round: 3, SkillSyncs: []SkillSync{{PetID: 402, SkillID: 7000010, Power: 10, HasPower: true}}}) {
		t.Fatal("新技能应算变化")
	}
	if got := tr.Snapshot().Self.Pets[1].Skills; len(got) != 2 {
		t.Errorf("新技能未记入: %+v", got)
	}
}

func TestTrackerStatsMerge(t *testing.T) {
	tr := NewTracker()
	e := enterOf()
	e.Self.Pets[0].Stats = PetStats{} // 进战包没给六维
	tr.OnEnter(e)
	if tr.Snapshot().Self.Pets[0].Stats.Has {
		t.Fatal("没下发时不该有六维")
	}
	// 回合包补上六维:给了就以它为准(否则伤害估算没有输入)。
	st := PetStats{Has: true, HP: StatLine{Race: 122, Talent: 60, BaseValue: 328, EffortAdd: 100}}
	tr.OnRound(Round{BattleID: 100, Round: 2, Self: Side{UIN: 100000002, Pets: []Pet{
		{GID: 1, PetID: 401, Stats: st},
	}}})
	if got := tr.Snapshot().Self.Pets[0].Stats; got != st {
		t.Errorf("六维未合并: %+v", got)
	}
}

// TestTrackerNoCrossSideDuplicate:同一只宠物(gid 相同)不能同时出现在两边。
//
// 实测踩到过:0x131a 的 data_update.other 会带上**我方**的宠物(对方视角的另一
// 方就是我方),而 other.role_uin 有时为 0 —— 按「不是本方 uin 就归对手」的兜底
// 会把自己的宠物记进对手栏,页面上对手栏出现一堆自己人。修法是「先到为准」:
// 进战包先登记了阵营,后续再见到同一 gid 就跳过。
// hasGID 供断言用:只看这批宠物里有没有这只 gid。
func hasGID(pets []Pet, gid uint32) bool {
	for _, p := range pets {
		if p.GID == gid {
			return true
		}
	}
	return false
}

func TestTrackerNoCrossSideDuplicate(t *testing.T) {
	tr := NewTracker()
	tr.OnEnter(enterOf()) // 我方 gid 1 / 2 先进场
	// 回合包把「我方两只 + 对手一只」一股脑塞进对方(other.role_uin 缺失时的形态)
	tr.OnRound(Round{BattleID: 100, Round: 2, Foe: Side{UIN: 142792, Pets: []Pet{
		{GID: 1, PetID: 401},   // 与我方 gid 1 同一只 → 必须被忽略
		{GID: 2, PetID: 402},   // 同上
		{GID: 999, PetID: 403}, // 真对手
	}}})
	snap := tr.Snapshot()
	// 对手栏:进战包那一只 + 本次的真对手,**不能**出现我方 gid 1 / 2
	for _, p := range snap.Foe.Pets {
		if p.GID == 1 || p.GID == 2 {
			t.Fatalf("我方宠物被记进了对手栏: %+v", snap.Foe.Pets)
		}
	}
	if !hasGID(snap.Foe.Pets, 999) {
		t.Errorf("真对手 gid 999 未登记: %+v", snap.Foe.Pets)
	}
	if len(snap.Self.Pets) != 2 {
		t.Errorf("我方不该被对手栏的同 gid 条目影响, 实得 %+v", snap.Self.Pets)
	}
}

// TestTrackerFinishSideByUIN:结算归属**按 uin**,不看 side —— 两局 pcap 的 side
// 语义相反,按 side 归类会把自己人记进对手栏。
func TestTrackerFinishSideByUIN(t *testing.T) {
	tr := NewTracker()
	tr.OnEnter(enterOf()) // self UIN 100000002 / foe UIN 142792
	// 我方宠物(100000002)但 side=0:按旧口径会归对手,按 uin 应归我方
	tr.OnFinish(Finish{BattleID: 100, Monsters: []Monster{
		{GID: 1, UIN: 100000002, Side: 0, State: MonsterDefeated, RemainHP: 0},
		{GID: 11, UIN: 142792, Side: 1, State: 3},
	}})
	snap := tr.Snapshot()
	if !hasGID(snap.Self.Pets, 1) || hasGID(snap.Foe.Pets, 1) {
		t.Errorf("我方宠物(side=0)应归我方: self=%v foe=%v", snap.Self.Pets, snap.Foe.Pets)
	}
	if !hasGID(snap.Foe.Pets, 11) || hasGID(snap.Self.Pets, 11) {
		t.Errorf("对手宠物(side=1)应归对手: self=%v foe=%v", snap.Self.Pets, snap.Foe.Pets)
	}
}

func TestTrackerIgnoresBeforeEnter(t *testing.T) {
	tr := NewTracker()
	if tr.OnRound(Round{BattleID: 1, Round: 1}) {
		t.Error("未开局就来的回合包不该生效(漏了进战包时也不凭空造局)")
	}
	if tr.OnPerform(Perform{Round: 1, Updates: []HPUpdate{{PetID: 401, HP: 1, HasHP: true}}}) {
		t.Error("未开局就来的演出包不该生效")
	}
	if tr.OnFinish(Finish{BattleID: 1}) {
		t.Error("未开局就来的结算包不该生效")
	}
	if tr.Snapshot().Active {
		t.Error("不应有局在进行")
	}
}

func TestTrackerIgnoresOtherBattle(t *testing.T) {
	tr := NewTracker()
	tr.OnEnter(enterOf())
	if tr.OnRound(Round{BattleID: 999, Round: 2}) {
		t.Error("别的战局的回合包不该并入")
	}
	if tr.OnFinish(Finish{BattleID: 999}) {
		t.Error("别的战局的结算包不该并入")
	}
}

func TestTrackerNewBattleResets(t *testing.T) {
	tr := NewTracker()
	tr.OnEnter(enterOf())
	tr.OnPerform(Perform{Round: 3, Updates: []HPUpdate{{PetID: 401, HP: 10, HasHP: true}}})

	e2 := enterOf()
	e2.BattleID = 200
	e2.Round = 1
	e2.Foe.Pets = []Pet{{GID: 21, PetID: 1, BaseConfID: 9999, Name: "新对手", Level: 60, HPMax: 100, HP: 100, HasHP: true}}
	if !tr.OnEnter(e2) {
		t.Fatal("换局应算变化")
	}
	snap := tr.Snapshot()
	if snap.BattleID != 200 || snap.Round != 1 {
		t.Errorf("换局后 = %+v", snap)
	}
	if len(snap.Foe.Pets) != 1 || snap.Foe.Pets[0].Name != "新对手" {
		t.Errorf("换局后对手 = %+v", snap.Foe.Pets)
	}
	if snap.Self.Pets[0].HP != 494 {
		t.Errorf("换局后不该残留上一局血量: %+v", snap.Self.Pets[0])
	}
}

func TestTrackerNoDuplicateWhenGIDMissing(t *testing.T) {
	tr := NewTracker()
	tr.OnEnter(enterOf())
	// 0x131a 的补发包常常只带 pet_id(gid 为 0):必须归到既有宠物上,
	// 否则同一只会出现两次(一次有名字有技能、一次只有编号和血量)。
	tr.OnRound(Round{BattleID: 100, Round: 2, Self: Side{UIN: 100000002, Pets: []Pet{
		{PetID: 401, HP: 300, HasHP: true, OnField: true},
		{PetID: 402, HP: 391, HasHP: true},
	}}})
	snap := tr.Snapshot()
	if len(snap.Self.Pets) != 2 {
		t.Fatalf("我方宠物数 = %d(应为 2,不该出现空壳重复)", len(snap.Self.Pets))
	}
	for _, p := range snap.Self.Pets {
		if p.Name == "" {
			t.Errorf("出现无名空壳: %+v", p)
		}
	}
	if snap.Self.Pets[0].HP != 300 || !snap.Self.Pets[0].OnField {
		t.Errorf("血量/在场未合并进既有宠物: %+v", snap.Self.Pets[0])
	}
}

func TestTrackerSkillsByPetID(t *testing.T) {
	tr := NewTracker()
	e := enterOf()
	// 开局时对手那只没有技能(data_update.pet_skill 稍后才到)。
	e.Foe.Pets = []Pet{{GID: 11, PetID: 1, BaseConfID: 3735, Name: "机幕方舟", Level: 60}}
	e.Self.Pets[0].Skills = []Skill{{ID: 7130320}} // 进战包已给技能
	tr.OnEnter(e)
	tr.OnRound(Round{BattleID: 100, Round: 2, Foe: Side{UIN: 142792},
		Skills: map[uint32][]Skill{1: {{ID: 200286}, {ID: 7070290}}}})
	p := tr.Snapshot().Foe.Pets[0]
	if len(p.Skills) != 2 || p.Skills[0].ID != 200286 {
		t.Errorf("按 pet_id 补技能失败: %+v", p)
	}
	// 已有技能的宠物不被覆盖(进战包那批更全)。
	tr.OnRound(Round{BattleID: 100, Round: 3, Self: Side{UIN: 100000002},
		Skills: map[uint32][]Skill{401: {{ID: 999}}}})
	if got := tr.Snapshot().Self.Pets[0].Skills; len(got) != 1 || got[0].ID != 7130320 {
		t.Errorf("不该用 pet_skill 覆盖已有技能: %v", got)
	}
}

func TestTrackerSideHPZeroIsKnown(t *testing.T) {
	tr := NewTracker()
	e := enterOf()
	e.Self.HP, e.Self.HasHP = 6, true
	tr.OnEnter(e)
	// 结算:体力打到 0。0 是**已知**值,不能显示成占位符。
	tr.OnFinish(Finish{BattleID: 100, Battlers: []Battler{{UIN: 100000002, HP: 0, HPMax: 6}}})
	s := tr.Snapshot()
	if !s.Self.HasHP || s.Self.HP != 0 {
		t.Errorf("结算后体力 = %d(HasHP=%v)", s.Self.HP, s.Self.HasHP)
	}
}

func TestTrackerSkillMergeKeepsFirst(t *testing.T) {
	tr := NewTracker()
	tr.OnEnter(enterOf())
	// 回合包里只带战斗态,不带技能:已解析出的技能不能被空列表顶掉。
	tr.OnRound(Round{BattleID: 100, Round: 2, Self: Side{UIN: 100000002, Pets: []Pet{
		{GID: 1, PetID: 401, OnField: true},
	}}})
	if got := tr.Snapshot().Self.Pets[0]; got.Name != "小花仙" || got.HPMax != 494 || got.HP != 494 {
		t.Errorf("技能/属性被回合包冲掉: %+v", got)
	}
}
