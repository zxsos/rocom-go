package shanyao

import (
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
)

// 构造 wire 字节的辅助。
//
// 这里**刻意写死协议里的字段号字面量**,不引用 parse.go 的 f* 常量:
// 用常量构造用例的话,常量被改错时测试会跟着一起错,等于没测(变异测试实测踩过:
// 把 fInBase 从 22 改成 24,用常量造包的用例照样全绿)。字面量的唯一代价是
// 协议字段号变动时要改两处 —— 那正是我们希望被提醒的时刻。

func vField(num protowire.Number, v uint64) []byte {
	return protowire.AppendVarint(protowire.AppendTag(nil, num, protowire.VarintType), v)
}

func sField(num protowire.Number, s string) []byte {
	return protowire.AppendBytes(protowire.AppendTag(nil, num, protowire.BytesType), []byte(s))
}

// mField 把若干段拼成一个 length-delimited 字段(便于一行写完嵌套消息)。
func mField(num protowire.Number, parts ...[]byte) []byte {
	var body []byte
	for _, p := range parts {
		body = append(body, p...)
	}
	return protowire.AppendBytes(protowire.AppendTag(nil, num, protowire.BytesType), body)
}

// petBody 构造一只宠物(battle_inside + battle_common),字段号一律字面量。
func petBody(gid, petID, base uint32, name string, level, hpMax uint32, mutation uint32, skills ...uint32) []byte {
	// battle_inside_pet_info: pet_id=1 battle_attr=6(repeated) skill_round_data=8
	// conf_id=21 base_conf_id=22 name=23 is_entered=70
	inside := vField(1, uint64(petID))
	inside = append(inside, vField(6, 0)...) // battle_attr[0]
	inside = append(inside, vField(6, uint64(hpMax))...)
	inside = append(inside, vField(22, uint64(base))...)
	inside = append(inside, sField(23, name)...)
	inside = append(inside, vField(70, 1)...)
	for _, sk := range skills {
		// skill_round_data.skill_id = 39
		inside = append(inside, mField(8, vField(39, uint64(sk)))...)
	}

	// battle_common_pet_info: gid=1 conf_id=2 name=3 skill_dam_type=6 nature=7
	// gender=8 level=10 skill=12 base_conf_id=15 mutation_type=45 glass_info=86
	common := vField(1, uint64(gid))
	common = append(common, vField(15, uint64(base))...)
	common = append(common, sField(3, name)...)
	common = append(common, vField(10, uint64(level))...)
	common = append(common, vField(7, 30)...) // 踏实
	common = append(common, vField(8, 2)...)  // ♀
	common = append(common, vField(6, 13)...) // 虫
	common = append(common, vField(6, 3)...)  // 草
	common = append(common, vField(45, uint64(mutation))...)
	glass := append(vField(1, 1), vField(2, 3145748)...) // GT_COMMON / 粒子3+配色20
	common = append(common, mField(86, glass)...)
	// skill.skill_data.id = 1(战斗内为 skills.json 口径 ×100)
	common = append(common, mField(12, mField(1, vField(1, 713032000)))...)

	return append(mField(1, inside), mField(2, common)...) // pets.inside=1 / pets.common=2
}

// teamBody 构造一支队伍:base=1 / pets=2;base 内 role_uin=1 name=2 role_level=14 hp=18 raw_hp=17
func teamBody(uin uint64, name string, level, hp, hpMax uint32, pets ...[]byte) []byte {
	base := vField(1, uin)
	base = append(base, sField(2, name)...)
	base = append(base, vField(14, uint64(level))...)
	base = append(base, vField(18, uint64(hp))...)
	base = append(base, vField(17, uint64(hpMax))...)
	out := mField(1, base)
	for _, p := range pets {
		out = append(out, mField(2, p)...)
	}
	return out
}

func TestParseEnter(t *testing.T) {
	self := teamBody(100000002, "示例玩家", 68, 4, 6,
		petBody(2181144456, 401, 3141, "小花仙", 60, 494, 9, 7130320, 7130200),
		petBody(2181144457, 402, 3179, "恶魔虫", 60, 391, 8),
	)
	foe := teamBody(142792, "林恩", 68, 4, 6,
		petBody(18876, 1, 3735, "机幕方舟", 60, 458, 0),
	)
	// init_info = 6:battle_id=1 player_team=5 enemy_team=6
	init := vField(1, 613286970137184)
	init = append(init, mField(5, self)...)
	init = append(init, mField(6, foe)...)
	body := vField(1, 15) // battle_mode
	body = append(body, vField(2, 1)...)
	body = append(body, mField(6, init)...)

	e, ok := ParseEnter(body)
	if !ok {
		t.Fatal("ParseEnter 未解析出进战信息")
	}
	if e.BattleID != 613286970137184 {
		t.Errorf("battle_id = %d", e.BattleID)
	}
	if e.Mode != 15 || e.Round != 1 {
		t.Errorf("mode/round = %d/%d", e.Mode, e.Round)
	}
	if e.Self.Name != "示例玩家" || e.Self.UIN != 100000002 || e.Self.HP != 4 || e.Self.HPMax != 6 {
		t.Errorf("我方 = %+v", e.Self)
	}
	if len(e.Self.Pets) != 2 {
		t.Fatalf("我方宠物数 = %d", len(e.Self.Pets))
	}
	p0 := e.Self.Pets[0]
	if p0.GID != 2181144456 || p0.PetID != 401 || p0.BaseConfID != 3141 || p0.Name != "小花仙" {
		t.Errorf("小花仙 = %+v", p0)
	}
	if p0.Level != 60 || p0.Nature != 30 || p0.Gender != 2 || p0.HPMax != 494 {
		t.Errorf("小花仙属性 = %+v", p0)
	}
	if p0.Mutation != 9 || p0.Mutation&MutationShinyBit == 0 {
		t.Errorf("小花仙炫彩位 = %d", p0.Mutation)
	}
	if p0.GlassType != 1 || p0.GlassValue != 3145748 {
		t.Errorf("小花仙炫彩 = %d/%d", p0.GlassType, p0.GlassValue)
	}
	if len(p0.DamTypes) != 2 || p0.DamTypes[0] != 13 || p0.DamTypes[1] != 3 {
		t.Errorf("小花仙系别 = %v", p0.DamTypes)
	}
	// 技能优先取 skill_round_data(7 位 id);common 那份是 ×100 口径,只在没有前者时用。
	if len(p0.Skills) != 2 || p0.Skills[0].ID != 7130320 || p0.Skills[1].ID != 7130200 {
		t.Errorf("小花仙技能 = %v", p0.Skills)
	}
	if !p0.OnField {
		t.Error("小花仙应在场")
	}
	// 对手只给首发一只(协议如此),其余靠回合包补齐。
	if len(e.Foe.Pets) != 1 || e.Foe.Pets[0].Name != "机幕方舟" {
		t.Errorf("对手 = %+v", e.Foe.Pets)
	}
	if e.Foe.Pets[0].Mutation != 0 {
		t.Errorf("机幕方舟炫彩位应为 0,实际 %d", e.Foe.Pets[0].Mutation)
	}
}

func TestParseEnterNoBattleID(t *testing.T) {
	body := mField(6, mField(5, teamBody(1, "x", 1, 1, 1)))
	if _, ok := ParseEnter(body); ok {
		t.Fatal("缺 battle_id 的进战包必须判为无效(否则无从配对回合/结算)")
	}
}

func TestParseEnterInsideOnly(t *testing.T) {
	// 只有 battle_inside_pet_info(没有 common):形态/名字/上限/技能都得从 inside 取。
	// 这条同时锁住 inside 各字段的号码 —— common 那份会给出同样的值,只有 inside
	// 单独出现时,号码写错才会暴露。
	inside := vField(1, 401) // pet_id
	inside = append(inside, vField(6, 0)...)
	inside = append(inside, vField(6, 494)...)
	inside = append(inside, vField(22, 3141)...)
	inside = append(inside, sField(23, "小花仙")...)
	inside = append(inside, mField(8, vField(39, 7130320))...)
	inside = append(inside, vField(55, 9)...) // dead_round

	init := append(vField(1, 5), mField(5, mField(2, mField(1, inside)))...)
	e, ok := ParseEnter(mField(6, init))
	if !ok {
		t.Fatal("解析失败")
	}
	p := e.Self.Pets[0]
	if p.BaseConfID != 3141 || p.Name != "小花仙" || p.HPMax != 494 || p.PetID != 401 {
		t.Errorf("inside 解析 = %+v", p)
	}
	if len(p.Skills) != 1 || p.Skills[0].ID != 7130320 {
		t.Errorf("技能 = %v", p.Skills)
	}
	if !p.Dead {
		t.Error("dead_round>0 应判为倒下")
	}
}

func TestParseEnterCommonSkillFallback(t *testing.T) {
	// 只带 battle_common_pet_info:技能走 ×100 折算分支。
	common := vField(1, 7) // gid
	common = append(common, mField(12, mField(1, vField(1, 713032000)))...)
	init := append(vField(1, 9), mField(5, mField(2, mField(2, common)))...)
	e, ok := ParseEnter(mField(6, init))
	if !ok {
		t.Fatal("解析失败")
	}
	if len(e.Self.Pets) != 1 || len(e.Self.Pets[0].Skills) != 1 || e.Self.Pets[0].Skills[0].ID != 7130320 {
		t.Errorf("技能折算 = %v", e.Self.Pets[0].Skills)
	}
}

func TestParseRound(t *testing.T) {
	// state_info = 2:battle_id=1 round=2 player_team=7 enemy_team=8
	self := teamBody(100000002, "示例玩家", 68, 4, 6, petBody(2181144458, 403, 3400, "扑克大王", 60, 359, 8))
	foe := teamBody(142792, "林恩", 68, 4, 6, petBody(21057, 2, 3256, "流浪鼠", 60, 455, 8))
	st := vField(1, 42)
	st = append(st, vField(2, 7)...)
	st = append(st, mField(7, self)...)
	st = append(st, mField(8, foe)...)

	r, ok := ParseRound(mField(2, st), 100000002)
	if !ok {
		t.Fatal("ParseRound 失败")
	}
	if r.BattleID != 42 || r.Round != 7 {
		t.Errorf("battle/round = %d/%d", r.BattleID, r.Round)
	}
	if len(r.Self.Pets) != 1 || r.Self.Pets[0].Name != "扑克大王" {
		t.Errorf("我方 = %+v", r.Self.Pets)
	}
	if len(r.Foe.Pets) != 1 || r.Foe.Pets[0].Name != "流浪鼠" || r.Foe.Pets[0].PetID != 2 {
		t.Errorf("对手 = %+v", r.Foe.Pets)
	}
}

// TestParseRoundDataUpdate 锁定「对手阵容从 data_update 里捞」这条路。
//
// 存在的理由:当前版本的 0x131a 把双方宠物拆在 perform_cmd.perform_info[].data_update
// 下发(state_info 里没有整队),而 data_update **只有 uin、没有我方/对手标记** ——
// 分边全靠 ParseRound 的 selfUIN 参数。这条路一断,对手就永远只剩进战包那只首发。
func TestParseRoundDataUpdate(t *testing.T) {
	const selfUIN, foeUIN = 100000002, 142792
	// data_update:uin=1 pet=3 other=8 pet_skill=7
	//              other.role_uin=1 other.pets=3
	//              pet_skill.pet_id=1 pet_skill.skills=2 skills.skill_id=39
	selfPet := mField(3, petBody(2181144457, 402, 3179, "恶魔虫", 60, 391, 8))
	foeDU := mField(8, vField(1, foeUIN), mField(3, petBody(18876, 1, 3735, "机幕方舟", 60, 458, 0)))
	du := mField(44, vField(1, selfUIN), selfPet, foeDU,
		mField(7, vField(1, 402), mField(2, vField(39, 7130150))))
	body := mField(3, mField(2, du)) // perform_cmd=1? —— 0x131a 里 perform_cmd 是 3

	r, ok := ParseRound(body, selfUIN)
	if !ok {
		t.Fatal("ParseRound(data_update) 失败")
	}
	if len(r.Self.Pets) != 1 || r.Self.Pets[0].Name != "恶魔虫" || r.Self.Pets[0].PetID != 402 {
		t.Errorf("我方 = %+v", r.Self.Pets)
	}
	if len(r.Foe.Pets) != 1 || r.Foe.Pets[0].Name != "机幕方舟" || r.Foe.Pets[0].GID != 18876 {
		t.Fatalf("对手 = %+v", r.Foe.Pets)
	}
	// 分边错了这里会红:两边的宠物信息都完整,靠名字/编号都区分得出来。
	if r.Foe.Pets[0].Level != 60 || r.Foe.Pets[0].Nature != 30 || r.Foe.Pets[0].DamTypes[0] != 13 {
		t.Errorf("对手详情 = %+v", r.Foe.Pets[0])
	}
	if got := r.Skills[402]; len(got) != 1 || got[0].ID != 7130150 {
		t.Errorf("技能 = %v", r.Skills)
	}
}

// TestParseRoundNeedsSelfUIN:不知道本方 uin 时不猜边(data_update 无从归属)。
func TestParseRoundNeedsSelfUIN(t *testing.T) {
	du := mField(44, vField(1, 100000002), mField(3, petBody(1, 402, 3179, "恶魔虫", 60, 391, 8)))
	body := mField(3, mField(2, du))
	r, ok := ParseRound(body, 0)
	if ok {
		t.Fatalf("本方 uin 未知、又没有 state_info 整队时不该认这一包: %+v", r)
	}
	if len(r.Self.Pets) != 0 || len(r.Foe.Pets) != 0 {
		t.Errorf("本方 uin 未知时不该分边: self=%v foe=%v", r.Self.Pets, r.Foe.Pets)
	}
}

func TestParsePerform(t *testing.T) {
	// perform_cmd=1:perform_info=2 round=3
	// perform_info.sync_data=12;sync_data.pet_sync_info=2;pet_sync_info:pet_id=1 hp_result=3
	// hp_result=0 也是真值(倒下),不能当作「没给」。
	sync := mField(2, vField(1, 402), vField(3, 0))
	sync = append(sync, mField(2, vField(1, 1), vField(3, 118))...)
	// 没有 hp_result 的同步项(只有能量/层数变化)不该产出更新。
	sync = append(sync, mField(2, vField(1, 403))...)

	cmd := vField(3, 12)
	cmd = append(cmd, mField(2, mField(12, sync))...)

	p, ok := ParsePerform(mField(1, cmd))
	if !ok {
		t.Fatal("ParsePerform 失败")
	}
	if p.Round != 12 {
		t.Errorf("round = %d", p.Round)
	}
	if len(p.Updates) != 2 {
		t.Fatalf("更新条数 = %d", len(p.Updates))
	}
	if p.Updates[0].PetID != 402 || !p.Updates[0].HasHP || p.Updates[0].HP != 0 {
		t.Errorf("更新0 = %+v(hp_result=0 必须保留)", p.Updates[0])
	}
	if p.Updates[1].PetID != 1 || p.Updates[1].HP != 118 {
		t.Errorf("更新1 = %+v", p.Updates[1])
	}
}

// TestParseSkillRoundData 锁住「对局内技能参数」:能耗 / 段数 / 威力。
//
// 这三个数是伤害估算的输入,且**对局内值优先于静态表**(融合与被动会改威力)。
// 字段号写错这里立刻红 —— 与宠物本体字段一样,靠断言而不是靠眼力。
func TestParseSkillRoundData(t *testing.T) {
	inside := vField(1, 402) // pet_id
	// skill_round_data: skill_id=39 cast_cnt=4 cost_energy=9 rule_damage_param=22
	sd := vField(39, 7130150)
	sd = append(sd, vField(4, 3)...)
	sd = append(sd, vField(9, 4)...)
	sd = append(sd, vField(22, 112)...)
	inside = append(inside, mField(8, sd)...)
	// 只有 rule 段位没给时才取 ex 段位(18)
	sd2 := vField(39, 7130130)
	sd2 = append(sd2, vField(18, 20)...)
	inside = append(inside, mField(8, sd2)...)

	team := mField(2, mField(1, inside))
	init := append(vField(1, 7), mField(5, team)...)
	e, ok := ParseEnter(mField(6, init))
	if !ok {
		t.Fatal("解析失败")
	}
	got := e.Self.Pets[0].Skills
	if len(got) != 2 {
		t.Fatalf("技能数 = %d", len(got))
	}
	if got[0].ID != 7130150 || got[0].Hits != 3 || got[0].Cost != 4 || !got[0].HasPower || got[0].Power != 112 {
		t.Errorf("虫击 = %+v", got[0])
	}
	if got[1].ID != 7130130 || !got[1].HasPower || got[1].Power != 20 {
		t.Errorf("虫群 = %+v", got[1])
	}
}

// TestParseStats 锁住我方六维(对手不下发,故「没给」必须保持 Has=false)。
func TestParseStats(t *testing.T) {
	// attribute_info=14: hp=1{total_race=1, talent=2, base_value=3, effort_add=6}
	hp := mField(1, vField(1, 122), vField(2, 60), vField(3, 328), vField(6, 100))
	atk := mField(2, vField(1, 67), vField(2, 60), vField(3, 205), vField(6, 50))
	common := vField(1, 2181144456) // gid
	common = append(common, mField(14, hp, atk)...)
	team := mField(2, mField(2, common))
	init := append(vField(1, 8), mField(5, team)...)
	e, ok := ParseEnter(mField(6, init))
	if !ok {
		t.Fatal("解析失败")
	}
	st := e.Self.Pets[0].Stats
	if !st.Has {
		t.Fatal("给了 attribute_info 就应有六维")
	}
	if st.HP.Race != 122 || st.HP.Talent != 60 || st.HP.BaseValue != 328 || st.HP.EffortAdd != 100 {
		t.Errorf("HP = %+v", st.HP)
	}
	if st.PhysicalAttack.Race != 67 || st.PhysicalAttack.BaseValue != 205 || st.PhysicalAttack.EffortAdd != 50 {
		t.Errorf("物攻 = %+v", st.PhysicalAttack)
	}
	// 没给 attribute_info 的宠物(对手):Has 必须为 false,不能拿零值充数。
	common2 := vField(1, 99)
	team2 := mField(2, mField(2, common2))
	init2 := append(vField(1, 9), mField(5, team2)...)
	e2, _ := ParseEnter(mField(6, init2))
	if e2.Self.Pets[0].Stats.Has {
		t.Error("没下发六维时不该置 Has")
	}
}

// TestParsePerformSkillSync 锁住 0x1324 的技能同步(damage_param_result)。
//
// 这是**对局内真实威力**的唯一来源(静态表只有基准威力),字段号写错会让
// 融合/被动修正后的威力静默失踪 —— 算出来的伤害看着对、实则偏低。
func TestParsePerformSkillSync(t *testing.T) {
	// skill_sync_info: pet_id=1 skill_id=2 damage_param_result=4
	//                  cast_cnt_result=6 cost_energy_result=10
	ss := mField(3, vField(1, 402), vField(2, 7130150), vField(4, 112), vField(6, 3), vField(10, 4))
	// 嵌套:body → perform_cmd(1) → perform_info(2) → sync_data(12) → skill_sync_info(3)
	pi := mField(12, ss)
	cmd := append(vField(3, 2), mField(2, pi)...)
	p, ok := ParsePerform(mField(1, cmd))
	if !ok {
		t.Fatal("ParsePerform 失败")
	}
	if len(p.SkillSyncs) != 1 {
		t.Fatalf("技能同步数 = %d", len(p.SkillSyncs))
	}
	got := p.SkillSyncs[0]
	if got.PetID != 402 || got.SkillID != 7130150 || !got.HasPower || got.Power != 112 ||
		got.Hits != 3 || !got.HasCost || got.Cost != 4 {
		t.Errorf("技能同步 = %+v", got)
	}
}

// TestParseEnergy:当前能量是动态威力规则 mana_burst(魔能爆)的**唯一输入**。
// 0 是合法值(能量耗尽),故必须能区分「0 能量」与「没给」—— 用 HasEnergy。
func TestParseEnergy(t *testing.T) {
	// energy = 33
	common := vField(1, 2181144456)
	common = append(common, vField(33, 0)...)
	team := mField(2, mField(2, common))
	e, ok := ParseEnter(mField(6, append(vField(1, 11), mField(5, team)...)))
	if !ok {
		t.Fatal("解析失败")
	}
	if !e.Self.Pets[0].HasEnergy || e.Self.Pets[0].Energy != 0 {
		t.Errorf("能量 = %d(HasEnergy=%v), 期望 0 且已知", e.Self.Pets[0].Energy, e.Self.Pets[0].HasEnergy)
	}
	// 没给 energy 的宠物:HasEnergy 必须为 false。
	common2 := vField(1, 2181144457)
	team2 := mField(2, mField(2, common2))
	e2, _ := ParseEnter(mField(6, append(vField(1, 12), mField(5, team2)...)))
	if e2.Self.Pets[0].HasEnergy {
		t.Error("没下发能量时不该置 HasEnergy")
	}
}

func TestParsePerformNoUpdate(t *testing.T) {
	if _, ok := ParsePerform(mField(1, vField(3, 3))); ok {
		t.Fatal("没有血量同步的演出包不该算有效(否则每个演出都触发一次广播)")
	}
}

func TestParseFinish(t *testing.T) {
	// settle_info=1:result=6 monster_info=8 battler_info=16 battle_id=19
	// monster_info: state=2 pet_gid=4 conf_id=11 petbase_id=12 uin=13
	//               remain_hp=19 max_hp=20 pet_name=27 mutation_type=28 side=32 pet_id=39
	mon := func(gid, petID, uin, base, side, state, remain, max uint32, name string) []byte {
		return mField(8, vField(4, uint64(gid)), vField(39, uint64(petID)), vField(13, uint64(uin)),
			vField(12, uint64(base)), vField(32, uint64(side)), vField(2, uint64(state)),
			vField(19, uint64(remain)), vField(20, uint64(max)), sField(27, name))
	}
	// battler_info: id=1 original_hp=4 hp=5
	bat := func(uin, hp, hpMax uint64) []byte {
		return mField(16, vField(1, uin), vField(5, hp), vField(4, hpMax))
	}
	stl := vField(19, 42)
	stl = append(stl, vField(6, 68)...)
	stl = append(stl, mon(2181144456, 401, 100000002, 3141, 1, MonsterDefeated, 0, 494, "小花仙")...)
	stl = append(stl, mon(18876, 1, 142792, 3735, 0, 3, 458, 458, "机幕方舟")...)
	stl = append(stl, bat(100000002, 0, 6)...)
	stl = append(stl, bat(142792, 4, 6)...)

	f, ok := ParseFinish(mField(1, stl))
	if !ok {
		t.Fatal("ParseFinish 失败")
	}
	if f.BattleID != 42 || f.Result != 68 {
		t.Errorf("battle/result = %d/%d", f.BattleID, f.Result)
	}
	if len(f.Monsters) != 2 {
		t.Fatalf("结算宠物数 = %d", len(f.Monsters))
	}
	if f.Monsters[0].Side != 1 || f.Monsters[0].State != MonsterDefeated ||
		f.Monsters[0].RemainHP != 0 || f.Monsters[0].MaxHP != 494 {
		t.Errorf("我方结算 = %+v", f.Monsters[0])
	}
	if f.Monsters[1].Side != 0 || f.Monsters[1].RemainHP != 458 || f.Monsters[1].Name != "机幕方舟" {
		t.Errorf("对手结算 = %+v", f.Monsters[1])
	}
	if len(f.Battlers) != 2 || f.Battlers[0].HP != 0 || f.Battlers[1].HP != 4 {
		t.Errorf("双方体力 = %+v", f.Battlers)
	}
}
