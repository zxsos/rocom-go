package gamedata

import (
	_ "embed" // go:embed 需要的空导入(见下方 //go:embed 指令)

	"encoding/json"
	"sort"
	"strconv"
)

// 伤害估算所需的静态数据(独立于 names.json,见 scripts/gen_calcdata.py)。
//
// 为什么单独成文件:这三份表的**数据源不是游戏解包**,而是 roco-calculator 的赛季
// 快照(原始数据来自 BWIKI,CC BY-NC-SA 4.0)。与「解包出来的 names.json」混在一起
// 会让「这份数据从哪来、什么许可、怎么更新」说不清 —— 现有 skills/features/trial
// 三份第三方数据也是各自独立,此处同例。
//
// 用途:
//   - 我方宠物的六维协议里就有(attribute_info),**不需要**这里的数据;
//   - 对手的六维服务端不下发,只能按「种族值 + 默认个体 + 已知性格」推算 —— 这份
//     表就是推算的输入。推算结果在 UI 上必须标注「推算」,不能冒充精确值。

//go:embed data/calc_race.json
var calcRaceJSON []byte

//go:embed data/calc_skills.json
var calcSkillsJSON []byte

//go:embed data/calc_types.json
var calcTypesJSON []byte

// 六维键顺序与 roco-calculator 一致,便于前端直接按序取用。
var calcStatKeys = []string{"hp", "physicalAttack", "magicalAttack", "physicalDefense", "magicalDefense", "speed"}

// CalcRace 是一个形态的种族值与系别。
type CalcRace struct {
	Name   string         `json:"name"`
	DexNo  string         `json:"dexNo"`
	Types  []string       `json:"types"`
	Race   map[string]int `json:"race"`
	Total  int            `json:"total"`
	RocoID string         `json:"rocoId"`
}

// CalcSkill 是一个技能的静态数值。
//
// BasePower 为 nil 表示「变化/无威力」技能(与 roco 的 status 类别一致);
// 对局内的真实威力由协议下发(融合、被动都会改),优先于这里的值。
type CalcSkill struct {
	Name      string  `json:"name"`
	Type      string  `json:"type"`
	Category  string  `json:"category"`
	Cost      *int    `json:"cost"`
	BasePower *int    `json:"basePower"`
	RuleID    *string `json:"ruleId"`
	RocoID    string  `json:"rocoId"`
}

// CalcTypeChart 是 18 系克制关系。
//
// Matrix[攻方系别下标][守方系别下标] = 单系倍率(1 / 2 / 0.5);双系相乘后按 Clamp
// 钳制(roco: raw>=4 记 3,raw<=0.25 记 0.25)。
type CalcTypeChart struct {
	Types  []string    `json:"types"`
	Matrix [][]float64 `json:"matrix"`
	Clamp  struct {
		Max float64 `json:"max"`
		Min float64 `json:"min"`
	} `json:"clamp"`
}

type calcDB struct {
	race   map[uint32]CalcRace
	skills map[uint32]CalcSkill
	types  *CalcTypeChart
}

// loadCalc 解析三份表;任何一份缺失都返回 nil,调用方一律按「缺数据」处理 ——
// 伤害估算缺数据不是错误,硬造一个默认值才是。
func loadCalc() *calcDB {
	db := &calcDB{}
	var race struct {
		Forms map[string]CalcRace `json:"forms"`
	}
	if json.Unmarshal(calcRaceJSON, &race) == nil && len(race.Forms) > 0 {
		db.race = make(map[uint32]CalcRace, len(race.Forms))
		for k, v := range race.Forms {
			if id, err := strconv.ParseUint(k, 10, 32); err == nil {
				db.race[uint32(id)] = v
			}
		}
	}
	var sk struct {
		Skills map[string]CalcSkill `json:"skills"`
	}
	if json.Unmarshal(calcSkillsJSON, &sk) == nil && len(sk.Skills) > 0 {
		db.skills = make(map[uint32]CalcSkill, len(sk.Skills))
		for k, v := range sk.Skills {
			if id, err := strconv.ParseUint(k, 10, 32); err == nil {
				db.skills[uint32(id)] = v
			}
		}
	}
	var tc CalcTypeChart
	if json.Unmarshal(calcTypesJSON, &tc) == nil && len(tc.Types) > 0 {
		db.types = &tc
	}
	if db.race == nil && db.skills == nil && db.types == nil {
		return nil
	}
	return db
}

// CalcRaceOf 返回某形态的种族值与系别;没收录返回 nil。
//
// 覆盖率约 54%(611/1136),未命中的多为未进化小形态;PVP 里登场的形态基本都能命中。
func (db *DB) CalcRaceOf(petbaseID uint32) (*CalcRace, bool) {
	if db.calc == nil {
		return nil, false
	}
	v, ok := db.calc.race[petbaseID]
	if !ok {
		return nil, false
	}
	return &v, true
}

// CalcSkillOf 返回某技能的静态数值;没收录返回 nil。
func (db *DB) CalcSkillOf(skillID uint32) (*CalcSkill, bool) {
	if db.calc == nil {
		return nil, false
	}
	v, ok := db.calc.skills[skillID]
	if !ok {
		return nil, false
	}
	return &v, true
}

// CalcTypeChart 返回属性克制表;缺数据时返回 nil(前端按「无克制」处理并提示)。
func (db *DB) CalcTypeChart() *CalcTypeChart {
	if db.calc == nil {
		return nil
	}
	return db.calc.types
}

// CalcStatKeys 返回六维键的固定顺序(与 roco 一致),供前端按序取用。
func CalcStatKeys() []string {
	out := make([]string, len(calcStatKeys))
	copy(out, calcStatKeys)
	return out
}

// NatureMultipliers 把性格 id 展开成六维系数(顺序见 CalcStatKeys)。
//
// 系数是 **+1.2 / −0.9**(不是常见同类游戏的 1.1/0.9),取自 roco-calculator 的
// natures.js;names.json.nature_effect 的 pos/neg 是**六维序号 1..6**,恰好与
// CalcStatKeys 的下标+1 对应(已核:1 大胆 = 物攻↑/物防↓,与 roco 一致)。
// 未知性格返回 nil(前端按「无修正」处理)。
func (db *DB) NatureMultipliers(natureID uint32) []float64 {
	if natureID == 0 {
		return nil
	}
	e, ok := db.natureEffect[key(natureID)]
	if !ok || (e.Pos == 0 && e.Neg == 0) {
		return nil
	}
	out := make([]float64, len(calcStatKeys))
	for i := range out {
		out[i] = 1
	}
	if e.Pos >= 1 && int(e.Pos) <= len(out) {
		out[e.Pos-1] = 1.2
	}
	if e.Neg >= 1 && int(e.Neg) <= len(out) {
		out[e.Neg-1] = 0.9
	}
	return out
}

// CalcElements 返回全部系别名(按克制表的顺序);缺数据时返回 nil。
func (db *DB) CalcElements() []string {
	tc := db.CalcTypeChart()
	if tc == nil {
		return nil
	}
	out := make([]string, len(tc.Types))
	copy(out, tc.Types)
	sort.Strings(out)
	return out
}
