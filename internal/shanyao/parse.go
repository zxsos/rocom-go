package shanyao

import (
	"google.golang.org/protobuf/encoding/protowire"

	"github.com/whoisnian/rocom-capture/internal/wire"
)

// 字段号一览(括号内是 pcapdump 按游戏描述符渲染出的字段名)。
//
// 换游戏版本后必须重新核对:
//
//	go run ./cmd/pcapdump -pcap <文件> -op 0x1316,0x131a,0x1324,0x132c
//
// 渲染结果给出字段名,字段号可临时加一个读 internal/pbdesc 的小程序打印
// (db.FindOp(op) → 遍历 Fields())。
//
// 只收录本项目需要的字段,未列出的字段一概跳过 —— 少解析一个字段就少一处要跟版本的地方。
const (
	// 0x1316 ZoneBattleEnterNotify
	fEnterMode  = 1 // battle_mode
	fEnterRound = 2 // round
	fEnterInit  = 6 // init_info

	// init_info
	fInitBattleID = 1 // battle_id
	fInitSelfTeam = 5 // player_team
	fInitFoeTeam  = 6 // enemy_team

	// team(player_team / enemy_team 同构)
	fTeamBase  = 1  // base
	fTeamPets  = 2  // pets
	fBaseUIN   = 1  // role_uin
	fBaseName  = 2  // name
	fBaseLevel = 14 // role_level
	fBaseHP    = 18 // hp(剩余体力)
	fBaseHPMax = 17 // raw_hp(体力上限)

	// pets(每只一个 pets 条目,内含 inside 与 common)
	fPetInside = 1 // battle_inside_pet_info
	fPetCommon = 2 // battle_common_pet_info

	// battle_inside_pet_info
	fInPetID   = 1  // pet_id
	fInAttr    = 6  // battle_attr(repeated int32)
	fInSkillRd = 8  // skill_round_data(repeated)
	fInConfID  = 21 // conf_id
	fInBase    = 22 // base_conf_id
	fInName    = 23 // name
	fInDeadRnd = 55 // dead_round(>0 表示已倒下过)
	fInEntered = 70 // is_entered(是否已在场上)

	// skill_round_data(与 pet_skill.skills 同构,字段号一致)
	fSkillRdID      = 39 // skill_id(skills.json 口径的 7 位 id)
	fSkillRdHits    = 4  // cast_cnt(连击段数)
	fSkillRdCost    = 9  // cost_energy(能耗)
	fSkillRdExPow   = 18 // ex_damage_param(额外威力参数)
	fSkillRdRulePow = 22 // rule_damage_param(规则威力参数)

	// battle_common_pet_info.attribute_info(14):hp=1 attack=2 special_attack=3
	// defense=4 special_defense=5 speed=6;每项的 total_race=1 talent=2
	// base_value=3 effort_add=6。
	fCmAttr = 14
	fAtHP   = 1
	fAtAtk  = 2
	fAtSAtk = 3
	fAtDef  = 4
	fAtSDef = 5
	fAtSpd  = 6
	fAtRace = 1
	fAtTal  = 2
	fAtBase = 3
	fAtEff  = 6

	// —— buff / 印记层数(印记规则的输入,见 roco 的 marks.js)——
	// battle_inside_pet_info.buffs(5,repeated):buff_id(2) stack(4) buff_type(6) from_skill_id(15)
	// battle_inside_pet_info.remain_buff_infos(39,repeated):buff_id(1) stack(2)
	// 0x1324 perform_info.buff_change(4):target_id(2) buff_id(3) type(4) buff_info(8){buff_id(2) stack(4)}
	// 0x1324 sync_data.pet_sync_info(3):buff_id(14) buff_stack_change(15) buff_stack_result(16)
	fInBuffs      = 5
	fInRemainBuff = 39
	fBuffID       = 2
	fBuffStack    = 4
	fBuffType     = 6
	fBuffSkill    = 15
	fRemainID     = 1
	fRemainStack  = 2
	fPIBuffChange = 4
	fBCTarget     = 2
	fBCBuffID     = 3
	fBCInfo       = 8
	fSyncBuffID   = 14
	fSyncStackRes = 16

	// battle_common_pet_info.energy = 33:当前能量。
	//
	// 只在为「魔能爆」(ruleId=mana_burst,威力随消耗的能量变)这类动态威力技能准备的:
	// 能量 0 是有意义的值(能量耗尽很常见),故用 HasEnergy 区分「0 能量」与「没给」。
	fCmEnergy = 33

	// 0x1324 perform_cmd.perform_info.sync_data.skill_sync_info(3)
	fSyncSkill  = 3
	fSSPetID    = 1
	fSSSkillID  = 2
	fSSParamRes = 4  // damage_param_result:对局内**实时威力**(含融合/被动修正)
	fSSHitsRes  = 6  // cast_cnt_result
	fSSCostRes  = 10 // cost_energy_result

	// battle_common_pet_info
	fCmGID     = 1  // gid
	fCmConfID  = 2  // conf_id
	fCmName    = 3  // name
	fCmDamType = 6  // skill_dam_type(repeated enum)
	fCmNature  = 7  // nature
	fCmGender  = 8  // gender
	fCmLevel   = 10 // level
	fCmSkill   = 12 // skill
	fCmBase    = 15 // base_conf_id
	fCmMutType = 45 // mutation_type
	fCmBlood   = 47 // blood_id
	fCmGlass   = 86 // glass_info
	fCmType    = 92 // type

	fGlassType  = 1 // glass_type
	fGlassValue = 2 // glass_value
	fSkillData  = 1 // skill.skill_data(repeated)
	fSkillID    = 1 // skill_data.id(战斗内为 skills.json 口径 ×100)

	// 0x131a ZoneBattleRoundStartNotify
	fRoundState   = 2  // state_info
	fStBattleID   = 1  // battle_id
	fStRound      = 2  // round
	fStSelfTeam   = 7  // player_team
	fStFoeTeam    = 8  // enemy_team
	fRoundPerform = 3  // perform_cmd
	fPIDataUpdate = 44 // perform_info.data_update
	fDUUIN        = 1  // data_update.uin
	fDUPet        = 3  // data_update.pet
	fDUOther      = 8  // data_update.other
	fDUPetSkill   = 7  // data_update.pet_skill
	fOtherUIN     = 1  // data_update.other.role_uin
	fOtherPets    = 3  // data_update.other.pets
	fPSKPetID     = 1  // pet_skill.pet_id
	fPSKSkills    = 2  // pet_skill.skills
	fPSKSkillID   = 39 // pet_skill.skills.skill_id

	// 0x1324 ZoneBattlePerformStartNotify
	fPerformCmd  = 1  // perform_cmd
	fPerformInfo = 2  // perform_info(repeated)
	fPerfRound   = 3  // perform_cmd.round
	fPISyncData  = 12 // perform_info.sync_data
	fSyncPet     = 2  // sync_data.pet_sync_info(repeated)
	fSyncPetID   = 1  // pet_sync_info.pet_id
	fSyncHPRes   = 3  // pet_sync_info.hp_result

	// 0x132c ZoneBattleFinishNotify
	fSettle      = 1  // settle_info
	fStlResult   = 6  // result
	fStlMonster  = 8  // monster_info(repeated)
	fStlBattler  = 16 // battler_info(repeated)
	fStlBattleID = 19 // battle_id

	fMonState    = 2  // state
	fMonGID      = 4  // pet_gid
	fMonPetID    = 39 // pet_id
	fMonConfID   = 11 // conf_id
	fMonBase     = 12 // petbase_id
	fMonUIN      = 13 // uin
	fMonRemainHP = 19 // remain_hp
	fMonMaxHP    = 20 // max_hp
	fMonName     = 27 // pet_name
	fMonMutType  = 28 // mutation_type
	fMonSide     = 32 // side(1=我方,0=对手)
	fMonGlass    = 38 // glass_info

	fBatUIN   = 1 // battler_info.id(=uin)
	fBatHPMax = 4 // original_hp
	fBatHP    = 5 // hp
)

// Skill 是一个技能槽。
//
// 数值有**两个来源**,语义不同,不要混:
//   - 对局内(skill_round_data / 0x1324 的 skill_sync_info):带上融合、被动与层数
//     修正后的真实值,只在有数据时才填(Cost/Hits/Power 的零值 + 对应 Has 标记区分);
//   - 静态表(gamedata 的 calc_skills.json):资料站整理的基准威力/能耗,兜底用。
type Skill struct {
	ID       uint32 // skills.json 口径的 7 位 id
	Cost     uint32 // 能耗
	Hits     uint32 // 连击段数(0 视为 1)
	Power    int32  // 威力参数
	HasPower bool
}

// StatLine 是一项六维的详细构成(服务端只给我方)。
//
// 面板值 = round(base_value × 性格系数) + effort_add,与 roco-calculator 的
// calculatePanelStat 完全对应(base_value 就是它公式里的 scaled + 70/10 那一段),
// 故我方六维无需从种族值反推。
type StatLine struct {
	Race      uint32
	Talent    uint32 // 个体(displayIv,默认 60)
	BaseValue uint32
	EffortAdd uint32
}

// Buff 是一个挂在宠物身上的 buff / 印记。
//
// 印记层数(蓄势/风起/星陨…)就是靠它带出来的:roco 的印记模型是「每方一个槽 +
// 层数」,而协议给的是 buff 列表,故这里原样保留 id 与层数,翻译交给前端的印记表。
type Buff struct {
	BuffID uint32
	Stacks uint32
	Type   uint32
	Skill  uint32 // 来自哪个技能(部分 buff 带)
}

// BuffChange 是 0x1324 里一次 buff 变化(层数以**结果值**为准,同 hp_result 的口径)。
type BuffChange struct {
	PetID    uint32
	BuffID   uint32
	Stacks   uint32
	HasStack bool
}

// PetStats 是六维。Has 为 false 表示服务端没下发(对手即如此)。
type PetStats struct {
	Has                                                                       bool
	HP, PhysicalAttack, MagicalAttack, PhysicalDefense, MagicalDefense, Speed StatLine
}

// SkillSync 是 0x1324 里一条技能同步:某只宠物的某个技能的实时威力/段数/能耗。
type SkillSync struct {
	PetID    uint32
	SkillID  uint32
	Power    int32
	HasPower bool
	Hits     uint32
	Cost     uint32
	HasCost  bool
}

// Pet 是一只参战精灵。
//
// HP/HPMax 用 int32 且 0 表示「服务端没给」:战斗包里宠物血量只在发生变化时才下发,
// 缺省值必须与「真的 0 血(倒下)」区分开,故每个 HP 都带 HasHP 标记。
type Pet struct {
	GID        uint32 // 宠物唯一 id(battle_common_pet_info.gid);0 表示缺失
	PetID      uint32 // 战斗内编号(我方 401..406,对手小编号;HP 同步只认它)
	ConfID     uint32
	BaseConfID uint32 // petbase_id(形态)
	Name       string
	Level      uint32
	Gender     uint32
	Nature     uint32
	Blood      uint32 // 血脉 id
	DamTypes   []int32
	Stats      PetStats // 六维(仅我方下发)
	// Energy 是当前能量(0..10 量级)。HasEnergy 区分「0 能量」与「没给」—— 0 是常见状态。
	Energy    uint32
	HasEnergy bool
	// Buffs 是当前挂在身上的 buff / 印记层数。进战与回合包会整份重发,故 merge 时整份替换。
	Buffs      []Buff
	Skills     []Skill
	Mutation   uint32 // 炫彩判据:Mutation & MutationShinyBit
	GlassType  uint32
	GlassValue uint32
	HPMax      int32 // 上限(battle_attr[1] 实测即最大 HP)
	HP         int32 // 当前 HP
	HasHP      bool  // HP 是否已知
	OnField    bool  // 是否当前在场
	Dead       bool
}

// Side 是一方(玩家或对手)的信息。
//
// HP 带 HasHP:剩余体力**可以是 0**(打输了就是 0),与「服务端没给」必须分开,
// 否则前端会把「体力耗尽」显示成占位符。
type Side struct {
	UIN   uint64
	Name  string
	Level uint32
	HP    int32 // 剩余体力(角色血,不是宠物血)
	HPMax int32
	HasHP bool
	Pets  []Pet
}

// Enter 是 0x1316 进战通知里用到的部分。
type Enter struct {
	BattleID uint64
	Mode     uint32 // battle_mode
	Round    uint32
	Self     Side
	Foe      Side
}

// Round 是 0x131a 回合开始里用到的部分。
//
// Skills 是 pet_id -> 技能列表,来自 data_update.pet_skill:那份只给编号不给形态,
// 无法按 gid 归并,故单独带回由状态机按 pet_id 补到对应宠物上。
type Round struct {
	BattleID uint64
	Round    uint32
	Self     Side
	Foe      Side
	Skills   map[uint32][]Skill
}

// HPUpdate 是一次「某只宠物血量变成多少」的同步(0x1324)。
type HPUpdate struct {
	PetID uint32
	HP    int32
	HasHP bool // hp_result 是否出现(出现即为真值,含 0)
}

// Perform 是 0x1324 演出通知里用到的部分。
type Perform struct {
	Round       uint32
	Updates     []HPUpdate
	SkillSyncs  []SkillSync  // 技能实时威力/段数/能耗
	BuffChanges []BuffChange // buff / 印记层数变化
}

// Monster 是 0x132c 结算里一只宠物的最终状态。
type Monster struct {
	GID       uint32
	PetID     uint32
	UIN       uint32
	Side      int32 // 1=我方,0=对手
	PetbaseID uint32
	ConfID    uint32
	Name      string
	Level     uint32
	State     uint32
	Mutation  uint32
	GlassType uint32
	GlassVal  uint32
	RemainHP  int32
	MaxHP     int32
}

// Battler 是 0x132c 结算里一方的剩余体力。
type Battler struct {
	UIN   uint64
	HP    int32
	HPMax int32
}

// Finish 是 0x132c 结算通知里用到的部分。
type Finish struct {
	BattleID uint64
	Result   uint32
	Monsters []Monster
	Battlers []Battler
}

// ParseEnter 解析 0x1316 进战通知。
//
// 注意**对手只给首发一只**的完整信息,其余在队宠物的 base_conf_id 为 0:
// 剩下的靠 0x131a 每回合下发补齐(见 Tracker.OnRound)。
func ParseEnter(body []byte) (Enter, bool) {
	init := wire.SubMsg(body, fEnterInit)
	if init == nil {
		return Enter{}, false
	}
	bid, _ := wire.Varint(init, fInitBattleID)
	if bid == 0 {
		return Enter{}, false // 没有 battle_id 的进战包无从配对,宁可不开局
	}
	var out Enter
	out.BattleID = bid
	if v, ok := wire.Varint(body, fEnterMode); ok {
		out.Mode = uint32(v)
	}
	if v, ok := wire.Varint(body, fEnterRound); ok {
		out.Round = uint32(v)
	}
	out.Self = parseTeam(wire.SubMsg(init, fInitSelfTeam))
	out.Foe = parseTeam(wire.SubMsg(init, fInitFoeTeam))
	return out, true
}

// ParseRound 解析 0x131a 回合开始通知。
//
// selfUIN 是本方 uin(登录时记下的 user_id),**必填**:
// 当前版本的 0x131a 把双方宠物拆在 perform_cmd.perform_info[].data_update 里下发,
// 而 data_update 只有 uin、没有「我方/对手」标记 —— 不带上本方 uin 就分不清边。
// (旧版本在 state_info.player_team / enemy_team 里给过整队,那段解析保留着兜底。)
func ParseRound(body []byte, selfUIN uint64) (Round, bool) {
	st := wire.SubMsg(body, fRoundState)
	cmd := wire.SubMsg(body, fRoundPerform)
	if st == nil && cmd == nil {
		return Round{}, false
	}
	var out Round
	if st != nil {
		out.BattleID, _ = wire.Varint(st, fStBattleID)
		if v, ok := wire.Varint(st, fStRound); ok {
			out.Round = uint32(v)
		}
		out.Self = parseTeam(wire.SubMsg(st, fStSelfTeam))
		out.Foe = parseTeam(wire.SubMsg(st, fStFoeTeam))
	}
	if cmd != nil {
		if v, ok := wire.Varint(cmd, fPerfRound); ok && v > uint64(out.Round) {
			out.Round = uint32(v)
		}
	}
	out.Skills = map[uint32][]Skill{}
	collectDataUpdates(cmd, selfUIN, &out.Self, &out.Foe, out.Skills)
	if len(out.Skills) == 0 {
		out.Skills = nil
	}
	if st == nil && len(out.Self.Pets) == 0 && len(out.Foe.Pets) == 0 {
		return Round{}, false // 既没回合状态也没宠物,无从认这一包
	}
	return out, true
}

// collectDataUpdates 把 perform_cmd.perform_info[].data_update 里的宠物与技能分边归入。
//
// 归属判定:data_update.uin 是**接收方**(自己);data_update.other 是对手那一侧
// (other.role_uin 给出对手 uin)。故:
//
//	data_update.pet   → 归 uin
//	data_update.other → 归 other.role_uin
//	pet_skill        → 归 uin(技能按 pet_id 记,由状态机补到对应宠物上)
func collectDataUpdates(cmd []byte, selfUIN uint64, self, foe *Side, skills map[uint32][]Skill) {
	if cmd == nil || selfUIN == 0 {
		return // 不知道本方 uin 时不猜:分错边比少认几只糟
	}
	sideOf := func(uin uint64) *Side {
		if uin == selfUIN {
			return self
		}
		return foe
	}
	for _, pi := range wire.Subs(cmd, fPerformInfo) {
		for _, du := range wire.Subs(pi, fPIDataUpdate) {
			owner, _ := wire.Varint(du, fDUUIN)
			if pet := wire.SubMsg(du, fDUPet); pet != nil && owner != 0 {
				sideOf(owner).Pets = append(sideOf(owner).Pets,
					parsePet(wire.SubMsg(pet, fPetInside), wire.SubMsg(pet, fPetCommon)))
			}
			if oth := wire.SubMsg(du, fDUOther); oth != nil {
				ru, _ := wire.Varint(oth, fOtherUIN)
				if ru != 0 {
					s := sideOf(ru)
					for _, p := range wire.Subs(oth, fOtherPets) {
						s.Pets = append(s.Pets, parsePet(wire.SubMsg(p, fPetInside), wire.SubMsg(p, fPetCommon)))
					}
				}
			}
			for _, ps := range wire.Subs(du, fDUPetSkill) {
				id, ok := wire.Varint(ps, fPSKPetID)
				if !ok {
					continue
				}
				var ids []Skill
				for _, sk := range wire.Subs(ps, fPSKSkills) {
					ids = appendSkill(ids, parseSkill(sk))
				}
				if len(ids) > 0 {
					skills[uint32(id)] = ids
				}
			}
		}
	}
}

// ParsePerform 解析 0x1324 演出通知里的血量同步。
//
// 血量只在 sync_data.pet_sync_info 里以**结果值**(hp_result)下发,没有增量累加一说;
// 一条 perform 里可能有多段(多段伤害/多只宠),故全部收下由状态机按顺序应用。
func ParsePerform(body []byte) (Perform, bool) {
	cmd := wire.SubMsg(body, fPerformCmd)
	if cmd == nil {
		return Perform{}, false
	}
	var out Perform
	if v, ok := wire.Varint(cmd, fPerfRound); ok {
		out.Round = uint32(v)
	}
	for _, pi := range wire.Subs(cmd, fPerformInfo) {
		// buff_change(4):target_id 是**战斗编号**(pet_id);层数取 buff_info.stack(结果值),
		// 顶层 buff_id 也可作为兜底(部分包只在 info 里给)。
		for _, bc := range wire.Subs(pi, fPIBuffChange) {
			var ch BuffChange
			if v, ok := wire.Varint(bc, fBCTarget); ok {
				ch.PetID = uint32(v)
			}
			if v, ok := wire.Varint(bc, fBCBuffID); ok {
				ch.BuffID = uint32(v)
			}
			if info := wire.SubMsg(bc, fBCInfo); info != nil {
				if v, ok := wire.Varint(info, fBuffStack); ok {
					ch.Stacks, ch.HasStack = uint32(v), true
				}
				if ch.BuffID == 0 {
					if v, ok := wire.Varint(info, fBuffID); ok {
						ch.BuffID = uint32(v)
					}
				}
			}
			if ch.PetID != 0 && ch.BuffID != 0 {
				out.BuffChanges = append(out.BuffChanges, ch)
			}
		}
		sd := wire.SubMsg(pi, fPISyncData)
		if sd == nil {
			continue
		}
		for _, ps := range wire.Subs(sd, fSyncPet) {
			id, ok := wire.Varint(ps, fSyncPetID)
			if !ok {
				continue
			}
			// 只收带 hp_result 的项:演出包里大量同步只是能量/层数变化,
			// 收进来会让「有更新」永远为真,每包都触发一次广播。
			v, ok := wire.Varint(ps, fSyncHPRes)
			if !ok {
				continue
			}
			out.Updates = append(out.Updates, HPUpdate{PetID: uint32(id), HP: int32(v), HasHP: true})
		}
		// 技能同步:damage_param_result 是**这一刻**的真实威力(含融合与被动修正),
		// 比静态表的基准威力更贴近实际,故单独收下回填到对应宠物的技能上。
		for _, ss := range wire.Subs(sd, fSyncSkill) {
			sync := SkillSync{}
			if v, ok := wire.Varint(ss, fSSPetID); ok {
				sync.PetID = uint32(v)
			}
			if v, ok := wire.Varint(ss, fSSSkillID); ok {
				sync.SkillID = normSkillID(v)
			}
			if v, ok := wire.Varint(ss, fSSParamRes); ok {
				sync.Power, sync.HasPower = int32(v), true
			}
			if v, ok := wire.Varint(ss, fSSHitsRes); ok {
				sync.Hits = uint32(v)
			}
			if v, ok := wire.Varint(ss, fSSCostRes); ok {
				sync.Cost, sync.HasCost = uint32(v), true
			}
			if sync.PetID != 0 && (sync.HasPower || sync.HasCost || sync.Hits > 0) {
				out.SkillSyncs = append(out.SkillSyncs, sync)
			}
			// 同一次同步里也可能只带 buff 层数:单独收一条 BuffChange(pet_id 在本条里)。
			var bc BuffChange
			if v, ok := wire.Varint(ss, fSyncBuffID); ok {
				bc.BuffID = uint32(v)
			}
			if v, ok := wire.Varint(ss, fSyncStackRes); ok {
				bc.Stacks, bc.HasStack = uint32(v), true
			}
			bc.PetID = sync.PetID
			if bc.PetID != 0 && bc.BuffID != 0 && bc.HasStack {
				out.BuffChanges = append(out.BuffChanges, bc)
			}
		}
	}
	return out, len(out.Updates) > 0 || len(out.SkillSyncs) > 0 || len(out.BuffChanges) > 0
}

// ParseFinish 解析 0x132c 结算通知。
func ParseFinish(body []byte) (Finish, bool) {
	stl := wire.SubMsg(body, fSettle)
	if stl == nil {
		return Finish{}, false
	}
	var out Finish
	out.BattleID, _ = wire.Varint(stl, fStlBattleID)
	if v, ok := wire.Varint(stl, fStlResult); ok {
		out.Result = uint32(v)
	}
	for _, m := range wire.Subs(stl, fStlMonster) {
		var mo Monster
		if v, ok := wire.Varint(m, fMonGID); ok {
			mo.GID = uint32(v)
		}
		if v, ok := wire.Varint(m, fMonPetID); ok {
			mo.PetID = uint32(v)
		}
		if v, ok := wire.Varint(m, fMonUIN); ok {
			mo.UIN = uint32(v)
		}
		if v, ok := wire.Varint(m, fMonSide); ok {
			mo.Side = int32(v)
		}
		if v, ok := wire.Varint(m, fMonBase); ok {
			mo.PetbaseID = uint32(v)
		}
		if v, ok := wire.Varint(m, fMonConfID); ok {
			mo.ConfID = uint32(v)
		}
		if v, ok := wire.Varint(m, fMonState); ok {
			mo.State = uint32(v)
		}
		if v, ok := wire.Varint(m, fMonMutType); ok {
			mo.Mutation = uint32(v)
		}
		if v, ok := wire.Varint(m, fMonRemainHP); ok {
			mo.RemainHP = int32(v)
		}
		if v, ok := wire.Varint(m, fMonMaxHP); ok {
			mo.MaxHP = int32(v)
		}
		if b, ok := wire.Bytes(m, fMonName); ok {
			mo.Name = string(b)
		}
		if g := wire.SubMsg(m, fMonGlass); g != nil {
			if v, ok := wire.Varint(g, fGlassType); ok {
				mo.GlassType = uint32(v)
			}
			if v, ok := wire.Varint(g, fGlassValue); ok {
				mo.GlassVal = uint32(v)
			}
		}
		out.Monsters = append(out.Monsters, mo)
	}
	for _, b := range wire.Subs(stl, fStlBattler) {
		var bt Battler
		bt.UIN, _ = wire.Varint(b, fBatUIN)
		if v, ok := wire.Varint(b, fBatHP); ok {
			bt.HP = int32(v)
		}
		if v, ok := wire.Varint(b, fBatHPMax); ok {
			bt.HPMax = int32(v)
		}
		out.Battlers = append(out.Battlers, bt)
	}
	return out, true
}

// parseTeam 解析一支队伍(base + pets)。
func parseTeam(team []byte) Side {
	var s Side
	if team == nil {
		return s
	}
	if base := wire.SubMsg(team, fTeamBase); base != nil {
		s.UIN, _ = wire.Varint(base, fBaseUIN)
		if b, ok := wire.Bytes(base, fBaseName); ok {
			s.Name = string(b)
		}
		if v, ok := wire.Varint(base, fBaseLevel); ok {
			s.Level = uint32(v)
		}
		if v, ok := wire.Varint(base, fBaseHP); ok {
			s.HP, s.HasHP = int32(v), true
		}
		if v, ok := wire.Varint(base, fBaseHPMax); ok {
			s.HPMax = int32(v)
		}
	}
	for _, p := range wire.Subs(team, fTeamPets) {
		inside := wire.SubMsg(p, fPetInside)
		common := wire.SubMsg(p, fPetCommon)
		if inside == nil && common == nil {
			continue
		}
		s.Pets = append(s.Pets, parsePet(inside, common))
	}
	return s
}

// parsePet 合并一只宠物的两份信息。
//
// inside 给战斗内状态(编号/最大 HP/在场/倒下),common 给宠物本体(形态/名字/性格/
// 性别/系别/技能/炫彩)。两份都可能缺,故逐字段「取先拿到的非空值」。
func parsePet(inside, common []byte) Pet {
	var p Pet
	if inside != nil {
		if v, ok := wire.Varint(inside, fInPetID); ok {
			p.PetID = uint32(v)
		}
		if v, ok := wire.Varint(inside, fInConfID); ok {
			p.ConfID = uint32(v)
		}
		if v, ok := wire.Varint(inside, fInBase); ok {
			p.BaseConfID = uint32(v)
		}
		if b, ok := wire.Bytes(inside, fInName); ok {
			p.Name = string(b)
		}
		if v, ok := wire.Varint(inside, fInEntered); ok {
			p.OnField = v != 0
		}
		if v, ok := wire.Varint(inside, fInDeadRnd); ok && v != 0 {
			p.Dead = true
		}
		// battle_attr[1] 实测为该宠的**最大** HP(进战与回合包里都是满值,
		// 不随伤害变化 —— 当前血量只认 0x1324 的 hp_result)。
		if attrs := wire.FieldVarints(inside, fInAttr); len(attrs) > 1 {
			if v := int32(uint32(attrs[1])); v > 0 {
				p.HPMax = v
			}
		}
		for _, sd := range wire.Subs(inside, fInSkillRd) {
			p.Skills = appendSkill(p.Skills, parseSkill(sd))
		}
	}
	if common != nil {
		if v, ok := wire.Varint(common, fCmGID); ok {
			p.GID = uint32(v)
		}
		if p.ConfID == 0 {
			if v, ok := wire.Varint(common, fCmConfID); ok {
				p.ConfID = uint32(v)
			}
		}
		if p.BaseConfID == 0 {
			if v, ok := wire.Varint(common, fCmBase); ok {
				p.BaseConfID = uint32(v)
			}
		}
		if p.Name == "" {
			if b, ok := wire.Bytes(common, fCmName); ok {
				p.Name = string(b)
			}
		}
		if v, ok := wire.Varint(common, fCmLevel); ok {
			p.Level = uint32(v)
		}
		if v, ok := wire.Varint(common, fCmGender); ok {
			p.Gender = uint32(v)
		}
		if v, ok := wire.Varint(common, fCmNature); ok {
			p.Nature = uint32(v)
		}
		if v, ok := wire.Varint(common, fCmBlood); ok {
			p.Blood = uint32(v)
		}
		if v, ok := wire.Varint(common, fCmMutType); ok {
			p.Mutation = uint32(v)
		}
		for _, v := range wire.FieldVarints(common, fCmDamType) {
			p.DamTypes = append(p.DamTypes, int32(v))
		}
		if g := wire.SubMsg(common, fCmGlass); g != nil {
			if v, ok := wire.Varint(g, fGlassType); ok {
				p.GlassType = uint32(v)
			}
			if v, ok := wire.Varint(g, fGlassValue); ok {
				p.GlassValue = uint32(v)
			}
		}
		if len(p.Skills) == 0 {
			if sk := wire.SubMsg(common, fCmSkill); sk != nil {
				for _, sd := range wire.Subs(sk, fSkillData) {
					if v, ok := wire.Varint(sd, fSkillID); ok {
						p.Skills = appendSkill(p.Skills, Skill{ID: normSkillID(v)})
					}
				}
			}
		}
		// 六维:协议只给我方(对手的 attribute_info 为空),故没给就是没给。
		if attr := wire.SubMsg(common, fCmAttr); attr != nil {
			p.Stats = parseStats(attr)
		}
		if v, ok := wire.Varint(common, fCmEnergy); ok {
			p.Energy, p.HasEnergy = uint32(v), true
		}
		p.Buffs = parseBuffs(inside)
	}
	return p
}

// parseSkill 解析一条 skill_round_data。
func parseSkill(sd []byte) Skill {
	var s Skill
	if v, ok := wire.Varint(sd, fSkillRdID); ok {
		s.ID = normSkillID(v)
	}
	if v, ok := wire.Varint(sd, fSkillRdCost); ok {
		s.Cost = uint32(v)
	}
	if v, ok := wire.Varint(sd, fSkillRdHits); ok {
		s.Hits = uint32(v)
	}
	// 威力参数:规则段位优先,其次额外段位;两者都是「对局内」口径。
	if v, ok := wire.Varint(sd, fSkillRdRulePow); ok && v != 0 {
		s.Power, s.HasPower = int32(v), true
	} else if v, ok := wire.Varint(sd, fSkillRdExPow); ok && v != 0 {
		s.Power, s.HasPower = int32(v), true
	}
	return s
}

// parseBuffs 解析 battle_inside_pet_info 的 buff 列表(buffs 优先,其次 remain_buff_infos)。
func parseBuffs(inside []byte) []Buff {
	if inside == nil {
		return nil
	}
	var out []Buff
	for _, b := range wire.Subs(inside, fInBuffs) {
		var bf Buff
		if v, ok := wire.Varint(b, fBuffID); ok {
			bf.BuffID = uint32(v)
		}
		if v, ok := wire.Varint(b, fBuffStack); ok {
			bf.Stacks = uint32(v)
		}
		if v, ok := wire.Varint(b, fBuffType); ok {
			bf.Type = uint32(v)
		}
		if v, ok := wire.Varint(b, fBuffSkill); ok {
			bf.Skill = uint32(v)
		}
		if bf.BuffID != 0 {
			out = append(out, bf)
		}
	}
	if len(out) > 0 {
		return out
	}
	for _, b := range wire.Subs(inside, fInRemainBuff) {
		var bf Buff
		if v, ok := wire.Varint(b, fRemainID); ok {
			bf.BuffID = uint32(v)
		}
		if v, ok := wire.Varint(b, fRemainStack); ok {
			bf.Stacks = uint32(v)
		}
		if bf.BuffID != 0 {
			out = append(out, bf)
		}
	}
	return out
}

// parseStats 解析 attribute_info 的六维。
func parseStats(attr []byte) PetStats {
	var st PetStats
	one := func(num protowire.Number) StatLine {
		var l StatLine
		sub := wire.SubMsg(attr, num)
		if sub == nil {
			return l
		}
		if v, ok := wire.Varint(sub, fAtRace); ok {
			l.Race = uint32(v)
		}
		if v, ok := wire.Varint(sub, fAtTal); ok {
			l.Talent = uint32(v)
		}
		if v, ok := wire.Varint(sub, fAtBase); ok {
			l.BaseValue = uint32(v)
		}
		if v, ok := wire.Varint(sub, fAtEff); ok {
			l.EffortAdd = uint32(v)
		}
		return l
	}
	st.HP = one(fAtHP)
	st.PhysicalAttack = one(fAtAtk)
	st.MagicalAttack = one(fAtSAtk)
	st.PhysicalDefense = one(fAtDef)
	st.MagicalDefense = one(fAtSDef)
	st.Speed = one(fAtSpd)
	// 至少有一项给了值才算「有六维」:全 0 与「没下发」不是一回事。
	st.Has = st.HP.BaseValue > 0 || st.PhysicalAttack.BaseValue > 0 || st.Speed.BaseValue > 0
	return st
}

// appendSkill 去重追加技能(同 id 合并,后者补齐前者的空缺)。
func appendSkill(list []Skill, s Skill) []Skill {
	if s.ID == 0 {
		return list
	}
	for i, v := range list {
		if v.ID == s.ID {
			if list[i].Cost == 0 {
				list[i].Cost = s.Cost
			}
			if list[i].Hits == 0 {
				list[i].Hits = s.Hits
			}
			if !list[i].HasPower && s.HasPower {
				list[i].Power, list[i].HasPower = s.Power, true
			}
			return list
		}
	}
	return append(list, s)
}

// normSkillID 把战斗内的技能 id 归一到 skills.json 的口径。
//
// battle_common_pet_info 里给的是 skills.json 口径 ×100(如 713032000 → 虫结阵
// 7130320),skill_round_data 里直接就是 7 位 id;两种都见过,故统一折算。
func normSkillID(v uint64) uint32 {
	if v >= 10_000_000 && v%100 == 0 {
		return uint32(v / 100)
	}
	return uint32(v)
}
