package gamedata

import (
	_ "embed" // go:embed 需要(即使本文件不直接用 embed.FS)
	"encoding/json"
	"sort"
	"strconv"
)

// 技能表(由 scripts/gen_skills.py 生成),含两块互补的数据:
//
//  1. **skill_id → 技能中文名**(names):协议里的 base_skill_id(7020500 这类)。
//     草系试炼等「只给 id」的场景靠它显示中文名。
//  2. **形态 → 三类技能**(innate/stone/blood):天生、技能石可学、血脉。
//     宠物详情页用。注意都是**可换的配置**,不是某只宠物当前携带的技能 ——
//     后者见 git 0762eb6 移除 Pet.SkillIDs 的理由(技能可经技能石等途径更换)。
//
// 数据来源与 names.json 不同:names.json 全部出自游戏解包 Bin 配置,而这份来自两个
// 第三方资料站(aismile.dev + arkmeng.cn),是**过渡方案** —— 解包出 SKILL_CONF
// (id→中文名,与协议同源)后应改走解包数据。独立成文件正是为了让「这份数据从哪来」说得清。
//
//go:embed data/skills.json
var skillsJSON []byte

// Skill 是一个技能的定义(名/属性/威力/能耗/效果/协议 id)。
//
// json tag 是**必须的**:本结构直接进 /api/pets/{gid} 的响应(见 server 的 petDetailView),
// 而全站 API 一律 camelCase;不加 tag 会输出 Name/Cost 这类大写键,与其余字段
// (weightKg/glassType/…)不一致,前端取值也容易写错。
// Power/Cost 用字符串而非数值:无威力的变化类技能是 "—"(如「防御」),整数表达不了。
type Skill struct {
	Name    string `json:"name"`
	Elem    string `json:"elem"`
	Power   string `json:"power"`
	Cost    string `json:"cost"`
	Effect  string `json:"effect"`
	SkillID uint32 `json:"skillId,omitempty"` // 协议里的 base_skill_id;重名技能为 0(见 gen_skills.py)
}

// InnateSkill 是天生技能:Skill 的定义 + 学会等级。
// 内嵌 Skill 让 JSON 字段平铺(name/elem/…/level),与改造前的响应形状一致。
type InnateSkill struct {
	Skill
	Level uint32 `json:"level"`
}

// skillRaw 是 JSON 里技能表的紧凑数组形式:
// [名, 属性, 威力, 能耗, 效果下标, skill_id]。
// 用数组而非对象是为压体积(见 gen_skills.py 的说明)。
type skillRaw [6]any

// skillsDB 是解析后的技能表。
type skillsDB struct {
	effects []string               // 共享效果描述池
	names   map[uint32]string      // skill_id -> 技能名
	descs   map[uint32]string      // skill_id -> 官方效果文案(SKILL_CONF.desc,纯文本)
	skills  []skillRaw             // 全局技能表(每个技能只存一份)
	innate  map[uint32][][2]uint32 // petbase_id -> [技能下标, 学会等级]
	stone   map[uint32][]uint32    // petbase_id -> 技能下标(技能石可学)
	blood   map[uint32][]uint32    // petbase_id -> 技能下标(血脉)

	// 懒解析缓存:展开一个形态的三类技能要解 40+ 条 JSON 数组,而宠物列表页
	// 一次可能查几十个形态。展开结果不变,缓存后重复查询是纯内存命中。
	innateByID map[uint32][]InnateSkill
	stoneByID  map[uint32][]Skill
	bloodByID  map[uint32][]Skill
}

// loadSkills 解析内嵌的技能表;文件缺失或格式不符时返回空表(不影响其余功能)。
func loadSkills() *skillsDB {
	db := &skillsDB{
		names: map[uint32]string{},
	}
	var raw struct {
		Effects []string               `json:"effects"`
		Names   map[string]string      `json:"names"`
		Descs   map[string]string      `json:"descs"`
		Skills  []skillRaw             `json:"skills"`
		Innate  map[string][][2]uint32 `json:"innate"`
		Stone   map[string][]uint32    `json:"stone"`
		Blood   map[string][]uint32    `json:"blood"`
	}
	if err := json.Unmarshal(skillsJSON, &raw); err != nil {
		return db
	}
	db.effects = raw.Effects
	db.skills = raw.Skills
	for k, v := range raw.Names {
		if id, err := strconv.ParseUint(k, 10, 32); err == nil {
			db.names[uint32(id)] = v
		}
	}
	for k, v := range raw.Descs {
		if id, err := strconv.ParseUint(k, 10, 32); err == nil {
			if db.descs == nil {
				db.descs = map[uint32]string{}
			}
			db.descs[uint32(id)] = v
		}
	}
	db.innate = parseIdxPairs(raw.Innate)
	db.stone = parseIdxList(raw.Stone)
	db.blood = parseIdxList(raw.Blood)
	return db
}

// parseIdxPairs 解析 {形态id: [[技能下标, 等级], …]}。
func parseIdxPairs(m map[string][][2]uint32) map[uint32][][2]uint32 {
	out := make(map[uint32][][2]uint32, len(m))
	for k, v := range m {
		if id, err := strconv.ParseUint(k, 10, 32); err == nil {
			out[uint32(id)] = v
		}
	}
	return out
}

// parseIdxList 解析 {形态id: [技能下标, …]}。
func parseIdxList(m map[string][]uint32) map[uint32][]uint32 {
	out := make(map[uint32][]uint32, len(m))
	for k, v := range m {
		if id, err := strconv.ParseUint(k, 10, 32); err == nil {
			out[uint32(id)] = v
		}
	}
	return out
}

// expand 把技能表里的一个下标展开成 Skill。越界返回零值(生成物与代码版本
// 不一致时会发生,宁可少显示一条也不要 panic)。
func (sdb *skillsDB) expand(idx uint32) Skill {
	if int(idx) >= len(sdb.skills) {
		return Skill{}
	}
	r := sdb.skills[idx]
	s := Skill{}
	if v, ok := r[0].(string); ok {
		s.Name = v
	}
	if v, ok := r[1].(string); ok {
		s.Elem = v
	}
	if v, ok := r[2].(string); ok {
		s.Power = v
	}
	if v, ok := r[3].(string); ok {
		s.Cost = v
	}
	if v, ok := r[4].(float64); ok {
		if i := int(v); i >= 0 && i < len(sdb.effects) {
			s.Effect = sdb.effects[i]
		}
	}
	if v, ok := r[5].(float64); ok {
		s.SkillID = uint32(v)
	}
	return s
}

// SkillName 把协议里的技能 id(base_skill_id,如 7020500)翻成中文名;
// 查不到返回空串(资料站只覆盖 605 条,未收录的 id 仍可能出现在协议里)。
//
// 关于「试炼专属 id」:试炼会给部分技能换一个 id(如魔能爆的 7020550 在试炼里
// 变成 7880058),这些 id 资料站没有,已在 gen_skills.py 里按抓包实证登记
// (EXTRA_SKILL_IDS)。再遇到查不到的 id,通常是资料站尚未收录的新技能,
// 调用方回退显示原始 id 即可,不是错误。
//
// 注意**融合不会改变 base_skill_id**:融合只改 fused_power 与 fusion_count
// (实测 7880058 融合 2 次:威力 20 -> 150, id 不变)。故不需要为融合态
// 单独维护映射 —— 这点早先判断错了,特此留注避免再犯。
func (db *DB) SkillName(skillID uint32) string {
	if db.skills == nil {
		return ""
	}
	return db.skills.names[skillID]
}

// SkillDesc 返回某技能 id 的官方效果文案(SKILL_CONF.desc,已剥成纯文本),
// 供技能名旁边展示「这个技能是干嘛的」。查不到返回空串 —— 调用方回退 title。
//
// 技能 id 与 descs 的键同一编号体系(与协议 base_skill_id 同源),所以只要
// SkillName 能命中,desc 也一定能命中(官方表每个技能都有 desc,1918/1918 非空)。
func (db *DB) SkillDesc(skillID uint32) string {
	if db.skills == nil {
		return ""
	}
	return db.skills.descs[skillID]
}

// InnateSkills 返回某形态天生会的技能,按学会等级降序(同级按名);
// 无记录返回 nil —— 第三方资料只覆盖部分形态,缺数据不是错误。
func (db *DB) InnateSkills(petbaseID uint32) []InnateSkill {
	sdb := db.skills
	if sdb == nil {
		return nil
	}
	if got, ok := sdb.innateByID[petbaseID]; ok {
		return got
	}
	raws, ok := sdb.innate[petbaseID]
	if !ok {
		return nil
	}
	out := make([]InnateSkill, 0, len(raws))
	for _, r := range raws {
		out = append(out, InnateSkill{Skill: sdb.expand(r[0]), Level: r[1]})
	}
	// 生成脚本已排好序,这里再排一次只为防御(展开顺序与紧凑条目一致,行为可预期)。
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Level != out[j].Level {
			return out[i].Level > out[j].Level
		}
		return out[i].Name < out[j].Name
	})
	if sdb.innateByID == nil {
		sdb.innateByID = map[uint32][]InnateSkill{}
	}
	sdb.innateByID[petbaseID] = out
	return out
}

// LearnableSkills 返回某形态**能用技能石学会**的技能(按名排序)。
//
// 与天生技能的关系:实测两者只重叠 3 条(462 个形态、7376 条条目),几乎完全互斥
// —— 技能石提供的是「升级学不到、得另外花技能石」的那批。重叠那几条是两种途径
// 都能拿到,两边都列才符合玩家查图鉴的预期,故不去重。
func (db *DB) LearnableSkills(petbaseID uint32) []Skill {
	return db.expandIdxList(petbaseID, db.skills.stone, &db.skills.stoneByID)
}

// BloodlineSkills 返回某形态**能通过血脉获得**的技能(按名排序)。
//
// 血脉条目在资料站里没有等级、也没有解锁条件(只有精灵与进化链),故这里只返回
// 技能本身 —— 不要想当然地给它补一个等级出来。
func (db *DB) BloodlineSkills(petbaseID uint32) []Skill {
	return db.expandIdxList(petbaseID, db.skills.blood, &db.skills.bloodByID)
}

// expandIdxList 是 LearnableSkills / BloodlineSkills 的公共实现:
// 按 petbaseID 查索引表、展开成 Skill 列表并缓存。cache 传指针是因为
// 首次查询要初始化这个 map(见 InnateSkills 里的同款写法)。
func (db *DB) expandIdxList(petbaseID uint32, idxOf map[uint32][]uint32, cache *map[uint32][]Skill) []Skill {
	sdb := db.skills
	if sdb == nil {
		return nil
	}
	if got, ok := (*cache)[petbaseID]; ok {
		return got
	}
	idxs, ok := idxOf[petbaseID]
	if !ok {
		return nil
	}
	out := make([]Skill, 0, len(idxs))
	for _, i := range idxs {
		if s := sdb.expand(i); s.Name != "" {
			out = append(out, s)
		}
	}
	if *cache == nil {
		*cache = map[uint32][]Skill{}
	}
	(*cache)[petbaseID] = out
	return out
}

// HasInnateSkills 判断某形态是否有天生技能数据(前端据此决定要不要显示技能区块)。
func (db *DB) HasInnateSkills(petbaseID uint32) bool {
	return db.skills != nil && len(db.skills.innate[petbaseID]) > 0
}

// SkillCatalogEntry 是技能目录里的一条。
type SkillCatalogEntry struct {
	ID   uint32 `json:"id"`
	Name string `json:"name"`
}

// SkillCatalog 返回全部技能(含重名技能)的 id -> 名目录,按 id 升序。
// 供宠物详情等场景做「全量技能名表」展示。
func (db *DB) SkillCatalog() []SkillCatalogEntry {
	sdb := db.skills
	if sdb == nil {
		return nil
	}
	out := make([]SkillCatalogEntry, 0, len(sdb.names))
	for id, name := range sdb.names {
		out = append(out, SkillCatalogEntry{ID: id, Name: name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
