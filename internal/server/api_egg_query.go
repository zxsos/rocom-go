package server

import (
	"fmt"
	"net/http"
	"strconv"
)

// 随机蛋「猜猜孵出谁」的数据源:**只有本地源**。
//
// 本地源按 PET_EGG_CONF 反推(见 gamedata.MatchRandomEgg 与 docs/data.md
// 「随机蛋的区间藏在哪」):零外部依赖、无限流、**离线可用**,且会用蛋的孵化时长
// maxSecs 硬筛 —— 这是唯一有实测支撑的一维。没有系别(系别只在协议
// GetSkillDamType 里,配置表没有)。
//
// 曾经还有过一个代理第三方图鉴的源(需要令牌、限流 10 次/分钟、**不做时长筛选**,
// 故候选里会混入时长根本对不上的物种:实测同一颗蛋本地 4 条、第三方 12 条,其中
// 8 条孵化时长与这颗蛋不符)。它已于 v4.2.3 移除 —— 移除的理由与实测时间线见
// docs/data.md 的历史段落。
const eggSrcLocal = "local"

// eggMatchOut 是查蛋的响应契约。
type eggMatchOut struct {
	Source  string          `json:"source"`  // 恒为 "local"(字段保留供前端标注数据来源)
	Total   int             `json:"total"`   // 候选条数(= len(matches))
	Matches []eggMatchEntry `json:"matches"` // 按匹配度降序
}

// eggMatchEntry 是单条候选。
//
// img 是**可直接赋给 <img src> 的完整值**,不是相对路径:本地给 /img/ 开头的站内路径。
// 候选列表是本契约唯一的消费方,统一成"拿来即用"省掉前端分支。
type eggMatchEntry struct {
	Name      string  `json:"name"`                // 物种名
	Img       string  `json:"img,omitempty"`       // 头像;无图时为空
	HatchSecs int32   `json:"hatchSecs"`           // 孵化时长(秒)
	Score     float64 `json:"score"`               // 匹配度 0-100(仅用于排序,不是概率)
	HeightPct float64 `json:"heightPct,omitempty"` // 蛋身高在该物种区间内的百分位
	WeightPct float64 `json:"weightPct,omitempty"` // 同上,体重
	ConfID    uint32  `json:"confId,omitempty"`    // 物种 conf_id
	Note      string  `json:"note,omitempty"`      // 补充说明:本地给孵化时长文案
}

// handleEggQuery 查随机蛋可能孵出的物种。
//
// 参数:
//
//	height=  蛋身高(米)      weight=  蛋体重(千克)      maxSecs= 孵满所需秒数
//
// 三者都来自前端 EggView,可省略(缺 maxSecs 时退化成纯尺寸匹配,会宽得多)。
//
// **不接 src 参数**:查到的结果与它无关,留着只会让人以为还能选源。
func (s *Server) handleEggQuery(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	height := parseFloat(q.Get("height"))
	weight := parseFloat(q.Get("weight"))
	maxSecs := parseInt32(q.Get("maxSecs"))

	cands := s.db.MatchRandomEgg(height, weight, maxSecs)
	matches := make([]eggMatchEntry, 0, len(cands))
	for _, c := range cands {
		img := ""
		if c.Img != "" {
			img = "/img/" + c.Img
		}
		matches = append(matches, eggMatchEntry{
			Name:      c.Name,
			Img:       img,
			HatchSecs: c.HatchSecs,
			Score:     c.Score,
			HeightPct: c.HeightPct,
			WeightPct: c.WeightPct,
			ConfID:    c.ConfID,
			Note:      hatchNote(c.HatchSecs),
		})
	}
	writeJSON(w, eggMatchOut{
		Source:  eggSrcLocal,
		Total:   len(matches),
		Matches: matches,
	})
}

// hatchNote 把孵化秒数说成人话;0 或异常值不给文案(宁可不写,也别写错)。
func hatchNote(secs int32) string {
	if secs <= 0 {
		return ""
	}
	if secs < 3600 {
		return fmt.Sprintf("孵化 %d 分钟", secs/60)
	}
	h, m := secs/3600, (secs%3600)/60
	if m == 0 {
		return fmt.Sprintf("孵化 %d 小时", h)
	}
	return fmt.Sprintf("孵化 %d 小时 %d 分", h, m)
}

// parseFloat 解析可选的浮点查询参数;空或非法都当 0(MatchRandomEgg 对 0 有定义)。
func parseFloat(s string) float64 {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return v
}

// parseInt32 解析可选的整数查询参数;空或非法都当 0。
func parseInt32(s string) int32 {
	v, err := strconv.ParseInt(s, 10, 32)
	if err != nil {
		return 0
	}
	return int32(v)
}
