package server

import (
	"net/http"

	"github.com/whoisnian/rocom-capture/internal/gamedata"
)

// 伤害估算的**规则常量**(隐藏模块 #/shanyao 用)。
//
// 与 /api/icons、/api/medals 同路数:不随账号、不随战局,前端取一次即可。
// 为什么不写死在前端:属性克制表与六维键序来自 roco-calculator 的赛季快照
// (见 scripts/gen_calcdata.py),换赛季时要跟着变 —— 写成 JS 常量就会出现
// 「后端换了、前端没换」这种最难查的口径不一致。
//
// 缺数据时返回 null(而不是编一套默认值):前端据此显示「缺数据」,好过拿错的
// 克制表算出一个看着合理的数。

// CalcRulesPayload 是伤害估算的规则常量。
type CalcRulesPayload struct {
	StatKeys []string       `json:"statKeys"` // 六维键的固定顺序(race/… 数组按它展开)
	Types    []string       `json:"types"`    // 18 个系别名
	Matrix   [][]float64    `json:"matrix"`   // matrix[攻][守] = 单系倍率
	Clamp    CalcRulesClamp `json:"clamp"`    // 双系相乘后的钳制
	// Floor:单次伤害下限(官方客户端 `ATTR_GLOBAL_CONFIG`)。nil = 官方数据缺失,
	// 前端按「无下限」处理 —— 没有依据时不兜底默认值。
	// ⚠️ 它作用在「每段」还是「总伤害」尚未用真实对局验证,前端当前按每段兜底。
	Floor *int `json:"floor,omitempty"`
	// 印记与特性的规则:两者都是「名字 → 效果」(我们与 roco 之间只有名字是共用的),
	// 与克制表同属规则常量,故放同一份里一次取回。
	Marks  []gamedata.CalcMark  `json:"marks"`  // 印记定义与效果
	Traits []gamedata.CalcTrait `json:"traits"` // 特性规则(roco 的 TRAIT_NAME_TO_RULE + 官方表)
	// Weather:weather_type → 天气名 + 该天气挂的 buff —— 有了它才能从场上 buff 反推当前天气。
	Weather map[uint32]gamedata.CalcWeather `json:"weather,omitempty"`
	Source  string                          `json:"source,omitempty"` // 数据来源说明(快照 id + 许可)
}

// CalcRulesClamp 是克制倍率的钳制规则(roco: raw>=4 记 3,raw<=0.25 记 0.25)。
type CalcRulesClamp struct {
	Max float64 `json:"max"`
	Min float64 `json:"min"`
}

// handleCalcRules 返回伤害估算规则;缺数据时返回 null。
func (s *Server) handleCalcRules(w http.ResponseWriter, r *http.Request) {
	tc := s.db.CalcTypeChart()
	if tc == nil {
		writeJSON(w, nil)
		return
	}
	// 一句话的出处说明:够人工核对即可,不把整个 _source 搬进响应(它很长且只对
	// 生成器有意义)。
	const source = "roco-calculator 赛季快照(原始: BWIKI, CC BY-NC-SA 4.0)"
	writeJSON(w, &CalcRulesPayload{
		StatKeys: gamedata.CalcStatKeys(),
		Types:    tc.Types,
		Matrix:   tc.Matrix,
		Clamp:    CalcRulesClamp{Max: tc.Clamp.Max, Min: tc.Clamp.Min},
		Floor:    tc.Floor,
		Marks:    s.db.CalcMarks(),
		Traits:   s.db.CalcTraits(),
		Weather:  s.db.CalcWeatherAll(),
		Source:   source,
	})
}
