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
// (当前在线/今日/近14天每日)。查询参数:account=账号过滤、limit=条数(默认200,上限1000)。
func (s *Server) handleAdminPlaySessions(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	acc := strings.TrimSpace(r.URL.Query().Get("account"))
	limit := 200
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
			limit = n
		}
	}
	sessions, err := s.store.ListPlaySessions(acc, limit)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	summary, err := s.store.PlaySessionSummary()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, map[string]any{"sessions": sessions, "summary": summary})
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
