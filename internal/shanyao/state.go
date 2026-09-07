package shanyao

import "strconv"

// Tracker 跟踪一个账号「当前这一局」的战局状态。
//
// 非并发安全:与管线其余状态一致,只在 pipeline 的单消费 goroutine 内读写
// (见 internal/pipeline/pipeline.go 的 Pipeline 注释)。
//
// 生命周期:0x1316 开局 → 0x131a 每回合补阵容 → 0x1324 刷血量 → 0x132c 收尾。
// 各 On* 方法返回「状态是否发生了变化」,供调用方决定要不要广播 —— 一局里 0x1324
// 有近百条,但大多数只改一两件事,整份重发会让前端无谓重渲染。
type Tracker struct {
	battleID uint64
	mode     uint32
	round    uint32
	active   bool
	finished bool
	result   uint32
	self     sideState
	foe      sideState
	dirty    bool
}

// sideState 是一方的持久化状态:pets 按 gid 归并,order 保留首次出现顺序。
//
// gid 缺失时(对手未出场的宠物)退而用 pet_id 归并:同一方内 pet_id 唯一。
type sideState struct {
	info  Side
	order []string          // 归并键序列
	pets  map[string]Pet    // 归并键 -> 宠物
	byID  map[uint32]string // pet_id -> 归并键(0x1324 只给 pet_id)
}

// NewTracker 创建空状态机。
func NewTracker() *Tracker { return &Tracker{} }

// BattleID 返回当前战局 id;0 表示尚未开局。
func (t *Tracker) BattleID() uint64 { return t.battleID }

// Active 报告是否有一局正在进行(已进战、未结算)。
func (t *Tracker) Active() bool { return t.active }

// OnEnter 应用 0x1316 进战通知。
func (t *Tracker) OnEnter(e Enter) bool {
	if e.BattleID == 0 {
		return false
	}
	if e.BattleID != t.battleID { // 换局:整份重置
		t.reset()
		t.battleID = e.BattleID
		t.dirty = true
	}
	if e.Mode != 0 && e.Mode != t.mode {
		t.mode, t.dirty = e.Mode, true
	}
	t.setRound(e.Round)
	if !t.active {
		t.active, t.finished, t.dirty = true, false, true
	}
	t.mergeSide(&t.self, e.Self)
	t.mergeSide(&t.foe, e.Foe)
	// 开局时双方都是满血:进战包给的 battle_attr[1] 就是上限(实测与结算 max_hp 一致),
	// 而当前血量要等第一次伤害同步(0x1324)才下发 —— 不先顶上,出场宠物的血条会长时间
	// 显示「—」。**只在进战包上这么做**:换上场的宠物可能带着旧伤,那时不能假设满血。
	for _, s := range []*sideState{&t.self, &t.foe} {
		for key, p := range s.pets {
			if !p.HasHP && p.HPMax > 0 {
				p.HP, p.HasHP = p.HPMax, true
				s.pets[key] = p
				t.dirty = true
			}
		}
	}
	return t.takeDirty()
}

// OnRound 应用 0x131a 回合开始通知(补齐对手阵容、更新在场标记)。
func (t *Tracker) OnRound(r Round) bool {
	if !t.active {
		return false // 没在局内的回合包(漏了进战包)不认,避免凭空造一局
	}
	if r.BattleID != 0 && r.BattleID != t.battleID {
		return false
	}
	t.setRound(r.Round)
	t.mergeSide(&t.self, r.Self)
	t.mergeSide(&t.foe, r.Foe)
	t.mergeSkills(r.Skills)
	return t.takeDirty()
}

// mergeSkills 把 pet_id -> 技能 的映射补到已有宠物上。
//
// 单独一步是因为 data_update.pet_skill 只给战斗编号、不给 gid,无法按宠物归并键定位;
// 而技能是「补齐用」的 —— 已经解析出技能的宠物不覆盖(进战包那批更全)。
func (t *Tracker) mergeSkills(skills map[uint32][]Skill) {
	for id, ids := range skills {
		for _, s := range []*sideState{&t.self, &t.foe} {
			key, ok := s.byID[id]
			if !ok {
				continue
			}
			p := s.pets[key]
			if len(p.Skills) > 0 {
				continue
			}
			p.Skills = ids
			s.pets[key] = p
			t.dirty = true
		}
	}
}

// applySkillSync 把一次「技能实时威力/段数/能耗」回填到某只宠物上。
//
// 命中不了就丢弃:技能同步是按 pet_id 定位的,而镜像对局里对手的 pet_id 可能与
// 我方重复,宁可少一次更新,也不要改错边。
func (t *Tracker) applySkillSync(sync SkillSync) {
	for _, s := range []*sideState{&t.self, &t.foe} {
		key, ok := s.byID[sync.PetID]
		if !ok {
			continue
		}
		p := s.pets[key]
		found := false
		for i, sk := range p.Skills {
			if sk.ID != sync.SkillID {
				continue
			}
			found = true
			if sync.HasPower && (p.Skills[i].Power != sync.Power || !p.Skills[i].HasPower) {
				p.Skills[i].Power, p.Skills[i].HasPower = sync.Power, true
				t.dirty = true
			}
			if sync.Hits > 0 && p.Skills[i].Hits != sync.Hits {
				p.Skills[i].Hits = sync.Hits
				t.dirty = true
			}
			if sync.HasCost && p.Skills[i].Cost != sync.Cost {
				p.Skills[i].Cost = sync.Cost
				t.dirty = true
			}
			break
		}
		// 没见过的技能(临时获得/技能石)也记下来:后续公式要用得到。
		if !found && sync.SkillID != 0 {
			p.Skills = append(p.Skills, Skill{
				ID:       sync.SkillID,
				Power:    sync.Power,
				HasPower: sync.HasPower,
				Hits:     sync.Hits,
				Cost:     sync.Cost,
			})
			t.dirty = true
		}
		s.pets[key] = p
		return
	}
}

// OnPerform 应用 0x1324 的血量同步。
func (t *Tracker) OnPerform(p Perform) bool {
	if !t.active || p.Round == 0 && len(p.Updates) == 0 {
		return false
	}
	t.setRound(p.Round)
	for _, u := range p.Updates {
		if !u.HasHP {
			continue
		}
		// 先我方后对手:两边的 pet_id 空间本不重叠(我方 401..406、对手小编号),
		// 但镜像对局里出现过对手编号与我方相同的包,按我方优先不会串。
		if !t.applyHP(&t.self, u) {
			t.applyHP(&t.foe, u)
		}
	}
	for _, sync := range p.SkillSyncs {
		t.applySkillSync(sync)
	}
	for _, ch := range p.BuffChanges {
		t.applyBuffChange(ch)
	}
	return t.takeDirty()
}

// applyBuffChange 把一次 buff 层数变化写到对应宠物上(先我方后对手,同 applyHP)。
func (t *Tracker) applyBuffChange(ch BuffChange) {
	for _, s := range []*sideState{&t.self, &t.foe} {
		if _, ok := s.byID[ch.PetID]; !ok {
			continue
		}
		key := s.byID[ch.PetID]
		p := s.pets[key]
		found := false
		for i := range p.Buffs {
			if p.Buffs[i].BuffID != ch.BuffID {
				continue
			}
			found = true
			if ch.HasStack && p.Buffs[i].Stacks != ch.Stacks {
				p.Buffs[i].Stacks = ch.Stacks
				t.dirty = true
			}
			break
		}
		if !found {
			p.Buffs = append(p.Buffs, Buff{BuffID: ch.BuffID, Stacks: ch.Stacks})
			t.dirty = true
		}
		s.pets[key] = p
		return
	}
}

// OnFinish 应用 0x132c 结算通知。
func (t *Tracker) OnFinish(f Finish) bool {
	if !t.active {
		return false
	}
	if f.BattleID != 0 && f.BattleID != t.battleID {
		return false
	}
	if f.Result != 0 && f.Result != t.result {
		t.result, t.dirty = f.Result, true
	}
	for _, m := range f.Monsters {
		t.sideOfMonster(m).applyMonster(m, &t.dirty)
	}
	for _, b := range f.Battlers {
		for _, s := range []*sideState{&t.self, &t.foe} {
			if b.UIN != 0 && s.info.UIN == b.UIN {
				if b.HPMax > 0 && s.info.HPMax != b.HPMax {
					s.info.HPMax, t.dirty = b.HPMax, true
				}
				if !s.info.HasHP || s.info.HP != b.HP {
					s.info.HP, s.info.HasHP, t.dirty = b.HP, true, true
				}
			}
		}
	}
	if !t.finished {
		t.finished, t.dirty = true, true
	}
	return t.takeDirty()
}

// Snapshot 导出当前战局的只读快照。
func (t *Tracker) Snapshot() Snapshot {
	return Snapshot{
		BattleID: t.battleID,
		Mode:     t.mode,
		Round:    t.round,
		Active:   t.active,
		Finished: t.finished,
		Result:   t.result,
		Self:     t.self.snapshot(),
		Foe:      t.foe.snapshot(),
	}
}

// Snapshot 是一份对外只读的战局快照。
type Snapshot struct {
	BattleID uint64
	Mode     uint32
	Round    uint32
	Active   bool
	Finished bool
	Result   uint32
	Self     Side
	Foe      Side
}

// reset 清空到未开局。
func (t *Tracker) reset() {
	*t = Tracker{}
}

// setRound 推进回合号(只增不减:回合包可能乱序/重发)。
func (t *Tracker) setRound(r uint32) {
	if r > t.round {
		t.round, t.dirty = r, true
	}
}

// takeDirty 取出并清除变更标记。
func (t *Tracker) takeDirty() bool {
	d := t.dirty
	t.dirty = false
	return d
}

// mergeSide 把一份队伍信息并入一方的持久状态(由 Tracker 调用,回写 dirty)。
//
// 归并键优先 gid;对手未出场的宠物 gid 为 0,退化为 pet_id(此时按方内唯一处理)。
// 已存在的宠物只补空缺字段(名字/等级/性格/系别/技能…),不覆盖既有值 ——
// 0x131a 每回合都重发同一批宠物,但它们只带「在场」这类战斗态信息。
// sideOfMonster 判定结算里一只宠物属于哪一边。
//
// ⚠️ **不能只看 monster_info.side**:实测两局 pcap 的 side 语义是相反的
// (一局我方 side=1、对手 side=0,另一局我方 side=0、对手 side=1)—— 它是
// 「进战包里的阵营编号」,不是「我方=1」。按 side 归类会把自己的宠物记进对手栏
// (对手栏出现一堆自己人)。故**以 uin 为准**,side 只作 uin 缺失时的兜底。
func (t *Tracker) sideOfMonster(m Monster) *sideState {
	if m.UIN != 0 {
		if t.self.info.UIN != 0 && uint64(m.UIN) == t.self.info.UIN {
			return &t.self
		}
		if t.foe.info.UIN != 0 && uint64(m.UIN) == t.foe.info.UIN {
			return &t.foe
		}
		// uin 两边都对不上(观战/第三方):退回 side,按旧口径
	}
	if m.Side == 1 {
		return &t.self
	}
	return &t.foe
}

// ownsGID 报告某只宠物(按 gid)是否已经属于**另一方**。
//
// 存在的理由:实测一局里 0x131a 的 data_update.other 会带上**我方**的宠物
// (对方视角的「另一方」就是我方),而 other.role_uin 有时为 0 —— 这时按
// 「不是本方 uin 就算对手」的兜底会把自己的宠物记到对手那边,页面上对手栏
// 出现一堆自己人(同一 gid 两边各一份)。故以**先到为准**:进战包已经给出
// 权威阵营,后面再见到同一只就保持原阵营,不重复登记。
func (t *Tracker) ownsGID(other *sideState, p Pet) bool {
	if p.GID == 0 {
		return false
	}
	_, ok := other.pets[petKey(p)]
	return ok
}

func (t *Tracker) mergeSide(s *sideState, info Side) {
	if s.pets == nil {
		s.pets = map[string]Pet{}
		s.byID = map[uint32]string{}
	}
	if info.UIN != 0 && s.info.UIN != info.UIN {
		s.info.UIN, t.dirty = info.UIN, true
	}
	if info.Name != "" && s.info.Name != info.Name {
		s.info.Name, t.dirty = info.Name, true
	}
	if info.Level != 0 && s.info.Level != info.Level {
		s.info.Level, t.dirty = info.Level, true
	}
	if info.HPMax > 0 && s.info.HPMax != info.HPMax {
		s.info.HPMax, t.dirty = info.HPMax, true
	}
	// 体力**可以是 0**(打输就是 0),故看 HasHP 而不是值是否为正。
	if info.HasHP && (!s.info.HasHP || s.info.HP != info.HP) {
		s.info.HP, s.info.HasHP, t.dirty = info.HP, true, true
	}
	for _, p := range info.Pets {
		key := petKey(p)
		if key == "" {
			continue
		}
		// 同一只宠物只归一边:已在对方阵营的(进战包先登记过)就不往这边加。
		if t.ownsGID(&t.foe, p) && s == &t.self {
			continue
		}
		if t.ownsGID(&t.self, p) && s == &t.foe {
			continue
		}
		// 同一只宠物在本方只能有一个键。实测踩到过:补发包先给 gid=0 的版本
		// (键 p<pet_id>),随后又给带 gid 的版本(键 g<gid>)—— 两个键并存就在
		// 页面上变成两只同名同血的宠物。故带 gid 时先看本方有没有同 pet_id 的记录。
		if _, ok := s.pets[key]; !ok {
			if k2, ok2 := s.byID[p.PetID]; ok2 {
				key = k2
			}
		}
		old, ok := s.pets[key]
		if !ok {
			s.pets[key] = p
			s.order = append(s.order, key)
			if p.PetID != 0 {
				s.byID[p.PetID] = key
			}
			t.dirty = true
			continue
		}
		merged := mergePet(old, p)
		if !samePet(old, merged) {
			s.pets[key] = merged
			if p.PetID != 0 {
				s.byID[p.PetID] = key
			}
			t.dirty = true
		}
	}
}

// applyHP 把一次血量同步应用到某方;命中返回 true。
func (t *Tracker) applyHP(s *sideState, u HPUpdate) bool {
	key, ok := s.byID[u.PetID]
	if !ok {
		return false
	}
	p := s.pets[key]
	if p.HP == u.HP && p.HasHP {
		return true // 命中但没变化,不算脏
	}
	p.HP, p.HasHP = u.HP, true
	if u.HP <= 0 {
		p.Dead = true
	}
	s.pets[key] = p
	t.dirty = true
	return true
}

// applyMonster 把结算里一只宠物的最终状态写回。
func (s *sideState) applyMonster(m Monster, dirty *bool) {
	if s.pets == nil {
		s.pets = map[string]Pet{}
		s.byID = map[uint32]string{}
	}
	var key string
	if m.GID != 0 {
		key = "g" + uitoa(m.GID)
	} else if m.PetID != 0 {
		key = "p" + uitoa(m.PetID)
	} else {
		return
	}
	p, ok := s.pets[key]
	if !ok {
		p = Pet{GID: m.GID, PetID: m.PetID, BaseConfID: m.PetbaseID, ConfID: m.ConfID, Name: m.Name}
		s.pets[key] = p
		s.order = append(s.order, key)
		if p.PetID != 0 {
			s.byID[p.PetID] = key
		}
		*dirty = true
	}
	if m.Name != "" && p.Name != m.Name {
		p.Name, *dirty = m.Name, true
	}
	if m.PetbaseID != 0 && p.BaseConfID == 0 {
		p.BaseConfID, *dirty = m.PetbaseID, true
	}
	if m.MaxHP > 0 && p.HPMax != m.MaxHP {
		p.HPMax, *dirty = m.MaxHP, true
	}
	if p.HP != m.RemainHP || !p.HasHP {
		p.HP, p.HasHP, *dirty = m.RemainHP, true, true
	}
	dead := m.State == MonsterDefeated || m.State == MonsterCatched
	if dead != p.Dead {
		p.Dead, *dirty = dead, true
	}
	if m.Mutation != 0 && p.Mutation == 0 {
		p.Mutation, *dirty = m.Mutation, true
	}
	if m.GlassType != 0 && p.GlassType == 0 {
		p.GlassType, p.GlassValue, *dirty = m.GlassType, m.GlassVal, true
	}
	s.pets[key] = p
}

// snapshot 按首次出现顺序导出一方。
func (s *sideState) snapshot() Side {
	out := s.info
	for _, k := range s.order {
		out.Pets = append(out.Pets, s.pets[k])
	}
	return out
}

// petKey 给出宠物的归并键;无从归并时返回空串。
func petKey(p Pet) string {
	if p.GID != 0 {
		return "g" + uitoa(p.GID)
	}
	if p.PetID != 0 {
		return "p" + uitoa(p.PetID)
	}
	return ""
}

// mergePet 用新信息补齐旧记录的空缺字段(旧值优先)。
func mergePet(old, add Pet) Pet {
	out := old
	if out.GID == 0 {
		out.GID = add.GID
	}
	if out.PetID == 0 {
		out.PetID = add.PetID
	}
	if out.ConfID == 0 {
		out.ConfID = add.ConfID
	}
	if out.BaseConfID == 0 {
		out.BaseConfID = add.BaseConfID
	}
	if out.Name == "" {
		out.Name = add.Name
	}
	if out.Level == 0 {
		out.Level = add.Level
	}
	if out.Gender == 0 {
		out.Gender = add.Gender
	}
	if out.Nature == 0 {
		out.Nature = add.Nature
	}
	if out.Blood == 0 {
		out.Blood = add.Blood
	}
	if out.Mutation == 0 {
		out.Mutation = add.Mutation
	}
	if out.GlassType == 0 {
		out.GlassType, out.GlassValue = add.GlassType, add.GlassValue
	}
	if out.HPMax == 0 {
		out.HPMax = add.HPMax
	}
	if len(out.DamTypes) == 0 {
		out.DamTypes = add.DamTypes
	}
	if len(out.Skills) == 0 {
		out.Skills = add.Skills
	}
	// 六维只在少数包里(进战/回合的首发)下发,给了就以它为准。
	if add.Stats.Has {
		out.Stats = add.Stats
	}
	// 能量每回合都在变,给了就以新值为准。
	if add.HasEnergy {
		out.Energy, out.HasEnergy = add.Energy, true
	}
	// buff 是整份快照(进战/回合包重发),故整份替换;没有 buff 的包不改动(避免误清)。
	if len(add.Buffs) > 0 {
		out.Buffs = add.Buffs
	}
	// 在场/倒下/当前血量是**战斗态**,给了就以新值为准(不给就保留旧值)。
	// 前提是「给了」必须有 HasHP 标记:0 血(倒下)与「没给」不能混淆。
	out.OnField = add.OnField
	if add.Dead {
		out.Dead = true
	}
	if add.HasHP {
		out.HP, out.HasHP = add.HP, true
		if add.HP <= 0 {
			out.Dead = true
		}
	}
	return out
}

// samePet 比较两只宠物是否完全一致(用于判断合并后是否真的变了)。
func samePet(a, b Pet) bool {
	if a.GID != b.GID || a.PetID != b.PetID || a.ConfID != b.ConfID ||
		a.BaseConfID != b.BaseConfID || a.Name != b.Name || a.Level != b.Level ||
		a.Gender != b.Gender || a.Nature != b.Nature || a.Blood != b.Blood ||
		a.Mutation != b.Mutation || a.GlassType != b.GlassType || a.GlassValue != b.GlassValue ||
		a.HPMax != b.HPMax || a.HP != b.HP || a.HasHP != b.HasHP ||
		a.OnField != b.OnField || a.Dead != b.Dead {
		return false
	}
	if len(a.DamTypes) != len(b.DamTypes) || len(a.Skills) != len(b.Skills) {
		return false
	}
	for i := range a.DamTypes {
		if a.DamTypes[i] != b.DamTypes[i] {
			return false
		}
	}
	for i := range a.Skills {
		if a.Skills[i] != b.Skills[i] {
			return false
		}
	}
	if a.Stats.Has != b.Stats.Has || a.Energy != b.Energy || a.HasEnergy != b.HasEnergy {
		return false
	}
	if len(a.Buffs) != len(b.Buffs) {
		return false
	}
	for i := range a.Buffs {
		if a.Buffs[i] != b.Buffs[i] {
			return false
		}
	}
	return !a.Stats.Has || a.Stats == b.Stats // 都有了才逐项比(StatLine 是可比较的结构体)
}

// uitoa 拼归并键用的十进制转换(键只在本包内比较,不进协议也不落库)。
func uitoa(v uint32) string { return strconv.FormatUint(uint64(v), 10) }
