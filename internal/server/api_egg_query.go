package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// 随机蛋「猜猜孵出谁」的数据源:同一时刻只有一个生效,管理员在管理面板切换
// (见 handleAdminEggSource,范式同远行商人)。
//
// 两个源的性质差别很大,不是同一站的两条路:
//
//   - eggSrcLocal 本地源:按 PET_EGG_CONF 反推(见 gamedata.MatchRandomEgg 与
//     docs/data.md「随机蛋的区间藏在哪」)。零外部依赖、无限流、**离线可用**,
//     且会用蛋的孵化时长 maxSecs 硬筛 —— 这是唯一有实测支撑的一维。
//     没有系别(系别只在协议 GetSkillDamType 里,配置表没有)。
//   - eggSrcXianyu 咸鱼源:代理第三方图鉴。需要 -egg-api-key,限流 10 次/分钟。
//     比本地多给系别;但**不做时长筛选**,只按身高体重匹配、把 hatch_data 当展示
//     字段返回,故候选里会混入时长根本对不上的物种(实测同一颗蛋:本地 4 条、
//     咸鱼源 12 条,其中 8 条的孵化时长与这颗蛋不符)。
//
// 默认本地源:它不是「退而求其次」,而是更准的那个 —— 第三方读的是同一张表
// (它返回的 hatch_data / height_range / weight_range 正是 PET_EGG_CONF 的列),
// 却没用上时长这一维。保留咸鱼源是为了对照与兜底(本地数据随游戏版本走,
// 第三方若先更新,可临时切过去)。
const (
	eggSrcLocal  = "local"
	eggSrcXianyu = "xianyu"
)

// eggSrcDefault 库里没配置时生效的源。
const eggSrcDefault = eggSrcLocal

// eggSourceValid 判断源标识是否合法(管理端点写入前的校验入口)。
func eggSourceValid(src string) bool {
	return src == eggSrcLocal || src == eggSrcXianyu
}

// eggSourceNeedKey 该源是否必须配置第三方令牌。
func eggSourceNeedKey(src string) bool { return src == eggSrcXianyu }

// eggSource 返回当前生效的数据源标识。
//
// 内存镜像而非每次读库:每次查蛋都要问它一次,而它只在切源时才变 ——
// 没必要为一次几乎不变的读取给每条请求加一条 SQL(与远行商人同一取舍)。
func (s *Server) eggSource() string {
	s.eggSrcMu.Lock()
	defer s.eggSrcMu.Unlock()
	return s.eggSrc
}

// eggAPIKeyGet 返回当前生效的第三方图鉴令牌;空=未配置。
//
// 加锁而非直接读字段:管理面板可在运行期改它(见 eggAPIKeySet),而读取方横跨
// HTTP 请求与 merchantLoop 两个 goroutine —— 裸读是数据竞争。
func (s *Server) eggAPIKeyGet() string {
	s.eggAPIKeyMu.Lock()
	defer s.eggAPIKeyMu.Unlock()
	return s.eggAPIKey
}

// eggAPIKeySet 更新令牌(管理面板改配置后立即生效,不必重启)。
func (s *Server) eggAPIKeySet(key string) {
	s.eggAPIKeyMu.Lock()
	defer s.eggAPIKeyMu.Unlock()
	s.eggAPIKey = key
}

// eggAPIKeySetOn 返回令牌是否已配置。面板回显只给这个布尔,
// 令牌原文从不下发前端(见 server.New 的注释)。
func (s *Server) eggAPIKeySetOn() bool { return s.eggAPIKeyGet() != "" }

// eggSetSource 切换数据源:落库 → 更新内存镜像。
//
// 与远行商人不同,这里**不需要清缓存**:两个源都是「每次请求实时算」
// (本地查内存里的 egg_conf、咸鱼源实时代理第三方),没有任何跨源复用的缓存,
// 故切源立即对下一次查询生效,也没有「切源当天数据为空」这类代价。
func (s *Server) eggSetSource(src string) error {
	if !eggSourceValid(src) {
		return fmt.Errorf("未知的数据源 %q", src)
	}
	if err := s.store.SetEggSource(src); err != nil {
		return err
	}
	s.eggSrcMu.Lock()
	s.eggSrc = src
	s.eggSrcMu.Unlock()
	log.Printf("查蛋数据源已切换为 %s", src)
	return nil
}

// eggSourceName 源的中文展示名。
//
// 映射放在后端:前端切换时要 POST 标识,若这份映射只存在于前端,两边迟早漂移
// (改了一处忘了另一处,表现是「面板显示未知源」这类没人能一眼看懂的错)。
func eggSourceName(src string) string {
	switch src {
	case eggSrcLocal:
		return "本地源"
	case eggSrcXianyu:
		return "咸鱼源"
	}
	return src
}

var eggMatchAPI = "https://apii.xianyuw.cn/api/v1/rocom-incubate"

// eggMatchOut 是两个源共用的响应契约。
type eggMatchOut struct {
	Source  string          `json:"source"`  // "local" 或 "xianyu"
	Total   int             `json:"total"`   // 候选条数(= len(matches))
	Matches []eggMatchEntry `json:"matches"` // 按匹配度降序
}

// eggMatchEntry 是统一后的单条候选。
//
// img 是**可直接赋给 <img src> 的完整值**,不是相对路径:本地给 /img/ 开头的站内路径,
// 第三方给外链 URL。候选列表是本契约唯一的消费方,统一成"拿来即用"省掉前端分支。
type eggMatchEntry struct {
	Name      string  `json:"name"`                // 物种名
	Img       string  `json:"img,omitempty"`       // 头像;无图时为空
	HatchSecs int32   `json:"hatchSecs"`           // 孵化时长(秒)
	Score     float64 `json:"score"`               // 匹配度 0-100(仅用于排序,不是概率)
	HeightPct float64 `json:"heightPct,omitempty"` // 蛋身高在该物种区间内的百分位(第三方不提供)
	WeightPct float64 `json:"weightPct,omitempty"` // 同上,体重
	ConfID    uint32  `json:"confId,omitempty"`    // 物种 conf_id(第三方的 pet_id 口径未必相同,勿跨源比较)
	Note      string  `json:"note,omitempty"`      // 补充说明:本地给孵化时长文案,第三方给 hatch_label
}

// handleEggQuery 查随机蛋可能孵出的物种。
//
// 参数:
//
//	height=  蛋身高(米)      weight=  蛋体重(千克)      maxSecs= 孵满所需秒数
//
// 三者都来自前端 EggView,可省略(缺 maxSecs 时本地退化成纯尺寸匹配,会宽得多)。
//
// **用哪个源由服务端配置决定,不接 src 参数**:数据源是对全服生效的运维选项,
// 若能让请求参数覆盖,任何玩家都能夹带 src=xianyu 去烧第三方额度(10 次/分钟)。
// 想换源只能走管理面板。
func (s *Server) handleEggQuery(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	height := parseFloat(q.Get("height"))
	weight := parseFloat(q.Get("weight"))
	maxSecs := parseInt32(q.Get("maxSecs"))

	if s.eggSource() == eggSrcXianyu {
		s.handleEggQueryXianyu(w, r, q.Get("height"), q.Get("weight"))
		return
	}

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

// handleEggQueryXianyu 代理第三方图鉴(咸鱼源),把对方的结构翻成统一契约。
// 统计:只要「发起了第三方请求」就记一条(未配 key 或请求没发出去不记),
// 见 store.LogEggQuery 与管理面板「查蛋 API 统计」。
func (s *Server) handleEggQueryXianyu(w http.ResponseWriter, r *http.Request, height, weight string) {
	// 统一埋点:ok 表示拿到第三方正常 JSON,matches 仅在成功时有效。
	start := time.Now()
	sent := false
	ok := false
	matches := 0
	defer func() {
		if !sent {
			return
		}
		s.store.LogEggQuery(s.acct(r), ok, int(time.Since(start).Milliseconds()), matches, height, weight)
	}()

	if s.eggAPIKeyGet() == "" {
		http.Error(w, "服务端未配置查询令牌(启动时加 -egg-api-key);可到管理面板把数据源切回本地源",
			http.StatusServiceUnavailable)
		return
	}
	params := url.Values{}
	params.Add("key", s.eggAPIKeyGet())
	params.Add("format", "json")
	if height != "" {
		params.Add("height", height)
	}
	if weight != "" {
		params.Add("weight", weight)
	}
	params.Add("implemented", "true")

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, eggMatchAPI+"?"+params.Encode(), nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, "查询第三方失败: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	sent = true                                               // 已发起第三方请求,之后的成败都要计入统计
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 上限 1MB,防第三方异常大响应
	if err != nil {
		http.Error(w, "读取第三方响应失败", http.StatusBadGateway)
		return
	}
	if resp.StatusCode != http.StatusOK {
		http.Error(w, fmt.Sprintf("第三方返回 %d: %s", resp.StatusCode, body), resp.StatusCode)
		return
	}
	// 校验 JSON 结构,别把错误页透传给前端。
	var out eggMatchResp
	if err := json.Unmarshal(body, &out); err != nil {
		http.Error(w, "第三方响应解析失败: "+err.Error(), http.StatusBadGateway)
		return
	}
	ok = true
	matches = out.Data.Total

	entries := make([]eggMatchEntry, 0, len(out.Data.Matches))
	for _, m := range out.Data.Matches {
		e := eggMatchEntry{
			Name:      m.PetName,
			Img:       m.ImgName,
			HatchSecs: int32(m.HatchData),
			Score:     m.Score,
			Note:      m.HatchLabel,
		}
		if m.PetID > 0 && m.PetID <= 1<<32-1 {
			e.ConfID = uint32(m.PetID)
		}
		entries = append(entries, e)
	}
	writeJSON(w, eggMatchOut{
		Source:  eggSrcXianyu,
		Total:   len(entries),
		Matches: entries,
	})
}

// eggMatchResp 第三方响应的外层结构(仅用于解析,不透传给前端)。
type eggMatchResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		Matches []eggMatchPet `json:"matches"`
		Total   int           `json:"total"`
		Source  string        `json:"source"`
	} `json:"data"`
}

// eggMatchPet 第三方单条匹配结果(字段与第三方约定一致)。
type eggMatchPet struct {
	PetID              int64   `json:"pet_id"`
	PetName            string  `json:"pet_name"`
	ImgName            string  `json:"img_name"`
	SourcePetName      string  `json:"source_pet_name"`
	ChainRootName      string  `json:"chain_root_name"`
	HeightRange        string  `json:"height_range"`
	WeightRange        string  `json:"weight_range"`
	HatchData          int64   `json:"hatch_data"`
	HatchLabel         string  `json:"hatch_label"`
	MainType           string  `json:"main_type"`
	SubType            *string `json:"sub_type"`
	Score              float64 `json:"score"`
	MatchedMetricCount int     `json:"matched_metric_count"`
	IsImplemented      bool    `json:"is_implemented"`
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
