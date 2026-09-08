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

//go:embed data/calc_marks.json
var calcMarksJSON []byte

//go:embed data/calc_traits.json
var calcTraitsJSON []byte

//go:embed data/calc_mark_ids.json
var calcMarkIDsJSON []byte

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
	DamType   string  `json:"damType"`
	Cost      *int    `json:"cost"`
	BasePower *int    `json:"basePower"`
	RuleID    *string `json:"ruleId"`
	RocoID    string  `json:"rocoId"`
	// Source: official-client(官方表按 id 直取) / roco-snapshot(按名桥接兜底)。
	Source string `json:"source"`
	// DynamicPower=true 表示威力随条件变化(官方表 dam_para 多段取值,如魔能爆按能量),
	// **不能**拿单一威力当数;见 scripts/gen_calcdata.py。
	DynamicPower bool `json:"dynamicPower"`
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
	// Floor 是单次伤害的下限,取自官方客户端 `ATTR_GLOBAL_CONFIG` 的
	// phy_dam_floor / spe_dam_floor(实测同为 2)。
	//
	// nil = 官方数据缺失。此时**不**兜底一个默认值,由调用方按「无下限」处理 ——
	// 没有依据时宁可不设,也不要自己编一个。
	//
	// ⚠️ 官方只给了数值,没说明它作用在「每段伤害」还是「总伤害」;当前前端按**每段**
	// 兜底(见 web/src/pages/shanyao/calc.js),口径尚未用真实对局验证。
	Floor *int `json:"floor"`
}

// CalcMark 是一个印记的定义与效果。
//
// AffectsThisHit=false 表示「结算时机不在本次技能」(回合末/入场/特殊伤害类型),
// **不参与主公式** —— 这是 roco 的口径,别把「中毒」这类算进本次伤害。
type CalcMark struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Polarity       string   `json:"polarity"`
	Summary        string   `json:"summary"`
	Affects        string   `json:"affects"`
	Per            int      `json:"per"`
	Unit           string   `json:"unit"`
	Needs          []string `json:"needs"`
	AffectsThisHit bool     `json:"affectsThisHit"`
	Note           string   `json:"note"`
}

// CalcTrait 是一个特性的规则(roco 的 TRAIT_NAME_TO_RULE)。
type CalcTrait struct {
	Name        string         `json:"name"`
	Rule        string         `json:"rule"`
	Side        string         `json:"side"`
	Effect      string         `json:"effect"`
	Params      map[string]any `json:"params"`
	Needs       []string       `json:"needs"`
	Note        string         `json:"note"`
	Implemented bool           `json:"implemented"`
	// BuffIds:战斗里该特性挂的 buff(官方 BUFF_CONF type=3),用于**从宠物 buff 自动识别特性**。
	// 同名特性可能有多个 buff 变体(不同数值档)。
	BuffIds  []uint32 `json:"buffIds"`
	Desc     string   `json:"desc"`
	TalentID string   `json:"talentId"`
	Source   string   `json:"source"`
}

// CalcWeather 是一种天气及其在战斗里挂的 buff。
type CalcWeather struct {
	Name  string   `json:"name"`
	Buffs []uint32 `json:"buffs"`
}

type calcDB struct {
	race    map[uint32]CalcRace
	skills  map[uint32]CalcSkill
	types   *CalcTypeChart
	marks   []CalcMark
	traits  []CalcTrait
	buffIDs map[uint32]string
	weather map[uint32]CalcWeather
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
	var marks struct {
		Marks []CalcMark `json:"marks"`
	}
	if json.Unmarshal(calcMarksJSON, &marks) == nil {
		db.marks = marks.Marks
	}
	var traits struct {
		Traits []CalcTrait `json:"traits"`
	}
	if json.Unmarshal(calcTraitsJSON, &traits) == nil {
		db.traits = traits.Traits
	}
	// calc_mark_ids.json 的每条是 {name, source, confidence, evidence} —— 带来源与可信度,
	// 便于人工复核(对拍推断来的只是候选,不是事实)。
	var w struct {
		Weather map[string]CalcWeather `json:"weather"`
	}
	if json.Unmarshal(calcMarkIDsJSON, &w) == nil && len(w.Weather) > 0 {
		db.weather = make(map[uint32]CalcWeather, len(w.Weather))
		for k, v := range w.Weather {
			if id, err := strconv.ParseUint(k, 10, 32); err == nil {
				db.weather[uint32(id)] = v
			}
		}
	}
	var ids struct {
		Buffs map[string]struct {
			Name string `json:"name"`
		} `json:"buffs"`
	}
	if json.Unmarshal(calcMarkIDsJSON, &ids) == nil && len(ids.Buffs) > 0 {
		db.buffIDs = make(map[uint32]string, len(ids.Buffs))
		for k, v := range ids.Buffs {
			if id, err := strconv.ParseUint(k, 10, 32); err == nil && v.Name != "" {
				db.buffIDs[uint32(id)] = v.Name
			}
		}
	}
	if db.race == nil && db.skills == nil && db.types == nil && len(db.marks) == 0 && len(db.traits) == 0 {
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

// CalcMarks 返回全部印记定义;没数据返回 nil。
func (db *DB) CalcMarks() []CalcMark {
	if db.calc == nil {
		return nil
	}
	return db.calc.marks
}

// CalcMarkByName 按**中文名**查印记(我们与 roco 之间只有名字是共用的)。
func (db *DB) CalcMarkByName(name string) (*CalcMark, bool) {
	if db.calc == nil {
		return nil, false
	}
	for i := range db.calc.marks {
		if db.calc.marks[i].Name == name {
			return &db.calc.marks[i], true
		}
	}
	return nil, false
}

// CalcTraits 返回特性规则;没数据返回 nil。
func (db *DB) CalcTraits() []CalcTrait {
	if db.calc == nil {
		return nil
	}
	return db.calc.traits
}

// CalcTraitByName 按中文名查特性规则(形态→特性名来自 features.json 的 petbase_feature)。
func (db *DB) CalcTraitByName(name string) (*CalcTrait, bool) {
	if db.calc == nil {
		return nil, false
	}
	for i := range db.calc.traits {
		if db.calc.traits[i].Name == name {
			return &db.calc.traits[i], true
		}
	}
	return nil, false
}

// MarkNameOfBuff 把协议的 buff_id 翻成印记名;未收录返回 false。
//
// 调用方拿到 false 时应显示原始 id,**不要**拿相近名字顶替 —— 这张表本来就稀有,
// 猜错的代价比「显示 buff #20010090」大得多。
func (db *DB) MarkNameOfBuff(buffID uint32) (string, bool) {
	if db.calc == nil {
		return "", false
	}
	v, ok := db.calc.buffIDs[buffID]
	return v, ok
}

// WeatherOf 返回某 weather_type 的天气名与其 buff;未收录返回 false。
func (db *DB) WeatherOf(weatherType uint32) (CalcWeather, bool) {
	if db.calc == nil {
		return CalcWeather{}, false
	}
	v, ok := db.calc.weather[weatherType]
	return v, ok
}

// WeatherFromBuff 反查:某个 buff_id 出现在哪种天气里(暴风雪的 buff 20170910/20171230)。
//
// ⚠️ **反查不唯一**:官方 `WEATHER_CONF` 里多个天气**共享**同一批 buff —— 实测
// 暴风雪(5)与小雪(9)的 buffs 完全相同,共有 6 个 buff 被 2~3 个天气引用。
// 故这里返回 **weather_type 最小的那个**(并如实给出它的名字);要知道全部候选,
// 调用方得自己遍历全部天气比对。
//
// 为什么固定取最小:map 的遍历顺序是随机的,不排序的话同一份数据每次会返回不同
// 的天气 —— 函数行为不确定,测试也随之随机红。
func (db *DB) WeatherFromBuff(buffID uint32) (uint32, CalcWeather, bool) {
	if db.calc == nil {
		return 0, CalcWeather{}, false
	}
	best, bestW, ok := uint32(0), CalcWeather{}, false
	for t, w := range db.calc.weather {
		hit := false
		for _, b := range w.Buffs {
			if b == buffID {
				hit = true
				break
			}
		}
		if hit && (!ok || t < best) {
			best, bestW, ok = t, w, true
		}
	}
	return best, bestW, ok
}

// TraitByBuff 按 buff_id 反查特性;用于「宠物身上有哪个 buff 就带哪个特性」。
func (db *DB) TraitByBuff(buffID uint32) (*CalcTrait, bool) {
	if db.calc == nil {
		return nil, false
	}
	for i := range db.calc.traits {
		for _, b := range db.calc.traits[i].BuffIds {
			if b == buffID {
				return &db.calc.traits[i], true
			}
		}
	}
	return nil, false
}

// CalcWeatherAll 返回全部天气(weather_type → 天气);没数据返回 nil。
func (db *DB) CalcWeatherAll() map[uint32]CalcWeather {
	if db.calc == nil {
		return nil
	}
	return db.calc.weather
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
