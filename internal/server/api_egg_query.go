package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// 第三方图鉴 API:按蛋的身高/体重匹配可能孵出的物种(神奇的蛋/随机蛋用)。
// 令牌只在服务端(-egg-api-key),前端只传 height/weight,避免令牌进浏览器代码。
const eggMatchAPI = "https://apii.xianyuw.cn/api/v1/rocom-incubate"

// eggMatchResp 第三方响应的外层结构(透传给前端时用,仅做格式校验)。
type eggMatchResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		Matches []eggMatchPet `json:"matches"`
		Total   int           `json:"total"`
		Source  string        `json:"source"`
	} `json:"data"`
}

// eggMatchPet 单条匹配结果(字段与第三方约定一致,前端直接消费)。
type eggMatchPet struct {
	PetID           int64   `json:"pet_id"`
	PetName         string  `json:"pet_name"`
	ImgName         string  `json:"img_name"`
	SourcePetName   string  `json:"source_pet_name"`
	ChainRootName   string  `json:"chain_root_name"`
	HeightRange     string  `json:"height_range"`
	WeightRange     string  `json:"weight_range"`
	HatchData       int64   `json:"hatch_data"`
	HatchLabel      string  `json:"hatch_label"`
	MainType        string  `json:"main_type"`
	SubType         *string `json:"sub_type"`
	Score               float64 `json:"score"`
	MatchedMetricCount  int     `json:"matched_metric_count"`
	IsImplemented       bool    `json:"is_implemented"`
}

// handleEggQuery 代理查询随机蛋可能孵出的物种。
// 请求参数:height(米)、weight(千克),均可省略(缺省交给第三方按空处理)。
// 响应为第三方原始 JSON(含 matches 列表),HTTP 状态与错误信息原样转述。
func (s *Server) handleEggQuery(w http.ResponseWriter, r *http.Request) {
	if s.eggAPIKey == "" {
		http.Error(w, "服务端未配置查询令牌(启动时加 -egg-api-key)", http.StatusServiceUnavailable)
		return
	}
	q := r.URL.Query()
	params := url.Values{}
	params.Add("key", s.eggAPIKey)
	params.Add("format", "json")
	if v := q.Get("height"); v != "" {
		params.Add("height", v)
	}
	if v := q.Get("weight"); v != "" {
		params.Add("weight", v)
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
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Write(body)
}
