package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

// handleAdminStats 返回全部成员抓捕情况的图表数据(近30天时间轴,按账号聚合)。
func (s *Server) handleAdminStats(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	st, err := s.store.AdminStats()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, st)
}

// handleAdminPlaySessions 返回游玩记录(管理后台「游玩记录」):会话明细列表 + 汇总
// (当前在线/今日/近14天每日)。明细分页:limit=每页条数(默认50,上限200)、
// offset=跳过条数(默认0);total 是同一筛选下的总条数,供前端算总页数。
// 汇总始终按全量计算,不随分页变化。
func (s *Server) handleAdminPlaySessions(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	acc := strings.TrimSpace(r.URL.Query().Get("account"))
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	offset := 0
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			offset = n
		}
	}
	sessions, err := s.store.ListPlaySessions(acc, limit, offset)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	summary, err := s.store.PlaySessionSummary()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	total, err := s.store.CountPlaySessions(acc)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, map[string]any{"sessions": sessions, "summary": summary, "total": total})
}

// handleAdminEggStats 返回查蛋 API(第三方图鉴)使用统计:累计/今日次数、成功率、
// 近 14 天每日、按账号排行、最近明细。keySet 告知服务端是否配置 -egg-api-key。
func (s *Server) handleAdminEggStats(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	st, err := s.store.EggQueryStats()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	st.KeySet = s.eggAPIKey != ""
	writeJSON(w, st)
}

// handleAdminRules 列出全部黑白名单规则。
func (s *Server) handleAdminRules(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	rules, err := s.store.ListRules()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, map[string]any{"rules": rules})
}

// handleAdminRuleSet 新增/更新一条黑白名单规则。
func (s *Server) handleAdminRuleSet(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var req struct {
		Account string `json:"account"`
		Mode    string `json:"mode"`
		Note    string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", 400)
		return
	}
	req.Account = strings.TrimSpace(req.Account)
	if req.Account == "" {
		http.Error(w, "account required", 400)
		return
	}
	if req.Mode != "black" && req.Mode != "white" {
		http.Error(w, "mode must be black or white", 400)
		return
	}
	if err := s.store.SetRule(req.Account, req.Mode, req.Note); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// handleAdminMerchantSource 远行商人数据源:查询与切换。
//
//	GET  → {source, keySet, sources:[{id, name, needKey}]}
//	POST {source} → 切换生效(清槽缓存 + 按新源重抓当前轮,见 merchantSetSource)
//
// 切换为什么要清缓存:两个源的货单格式不同,留着另一份会被当成新源的数据显示,
// 页面顶部的来源标注也在说谎。代价是切源当天「昨日回顾」为空,直到下一个营业日
// 的档被缓存 —— 前端卡片里写明了这一点。
func (s *Server) handleAdminMerchantSource(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		type srcJSON struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			NeedKey bool   `json:"needKey"`
		}
		// 源的清单从后端下发而非前端硬编码:合法标识只有后端能校验,
		// 让前端自己列一份迟早与校验逻辑漂移。
		sources := []srcJSON{}
		for _, id := range []string{merchantSrcXianyu, merchantSrcHaoyou} {
			sources = append(sources, srcJSON{
				ID: id, Name: merchantSourceName(id), NeedKey: merchantNeedKey(id),
			})
		}
		writeJSON(w, map[string]any{
			"source":  s.merchantSource(),
			"keySet":  s.eggAPIKey != "",
			"sources": sources,
		})
	case http.MethodPost:
		var req struct {
			Source string `json:"source"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "参数解析失败", http.StatusBadRequest)
			return
		}
		req.Source = strings.TrimSpace(req.Source)
		if !merchantSourceValid(req.Source) {
			http.Error(w, "未知的数据源:"+req.Source, http.StatusBadRequest)
			return
		}
		if err := s.merchantSetSource(req.Source); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	default:
		http.Error(w, "不支持的请求方法", http.StatusMethodNotAllowed)
	}
}

// handleAdminRuleDelete 删除一条黑白名单规则(?account=xxx)。
func (s *Server) handleAdminRuleDelete(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	acc := strings.TrimSpace(r.URL.Query().Get("account"))
	if acc == "" {
		http.Error(w, "account required", 400)
		return
	}
	if err := s.store.DeleteRule(acc); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}
