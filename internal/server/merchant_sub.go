package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// handleMerchantSub 订阅/退订远行商人邮件提醒(按当前登录账号绑定,一个账号一个邮箱):
//
//	GET    /api/merchant/sub → {configured, subscribed, email, keywords}
//	POST   /api/merchant/sub {email, keywords} → 订阅/更新(关键词逗号分隔,空=全部)
//	DELETE /api/merchant/sub → 退订
func (s *Server) handleMerchantSub(w http.ResponseWriter, r *http.Request) {
	account := s.acct(r)
	if account == "" {
		http.Error(w, "缺少账号信息", http.StatusBadRequest)
		return
	}
	switch r.Method {
	case http.MethodGet:
		email, keywords, ok := s.store.GetMerchantSub(account)
		writeJSON(w, map[string]any{
			"configured": s.smtp.configured(),
			"subscribed": ok,
			"email":      email,
			"keywords":   keywords,
		})
	case http.MethodPost:
		var req struct {
			Email    string `json:"email"`
			Keywords string `json:"keywords"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "参数解析失败", http.StatusBadRequest)
			return
		}
		email := strings.ToLower(strings.TrimSpace(req.Email))
		if !strings.Contains(email, "@") || len(email) < 5 {
			http.Error(w, "邮箱格式不正确", http.StatusBadRequest)
			return
		}
		if err := s.store.UpsertMerchantSub(account, email, strings.TrimSpace(req.Keywords)); err != nil {
			http.Error(w, "保存失败", http.StatusInternalServerError)
			return
		}
		// 保存成功:自动发送验证邮件,确认收件邮箱可达。发信失败不阻塞订阅(仅提示)。
		out := map[string]any{"ok": true, "mail_sent": false}
		if s.smtp.configured() {
			subject := "【远哥来了】订阅成功验证"
			body := "你已成功订阅「远行商人」新货提醒!\n\n" +
				"本邮件用于验证收件邮箱可正常接收提醒,无需回复。\n" +
				"此后每轮(8/12/16/20 点)有新增商品上架时,会第一时间发邮件通知你。\n\n" +
				"——远哥来了"
			if err := s.smtp.sendMerchantMail(email, subject, body); err != nil {
				out["mail_error"] = err.Error()
			} else {
				out["mail_sent"] = true
			}
		}
		writeJSON(w, out)
	case http.MethodDelete:
		if err := s.store.DeleteMerchantSub(account); err != nil {
			http.Error(w, "退订失败", http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	default:
		http.Error(w, "不支持的请求方法", http.StatusMethodNotAllowed)
	}
}

// handleAdminMerchantSubs 管理接口:邮箱推送名单。
//
//	GET    /api/admin/merchant-subs → {configured, subs:[{email, account, keywords, created_at}]}
//	DELETE /api/admin/merchant-subs?email=xxx → 按邮箱删除全部关联订阅
func (s *Server) handleAdminMerchantSubs(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		subs, err := s.store.ListMerchantSubs()
		if err != nil {
			http.Error(w, "拉取订阅名单失败", http.StatusInternalServerError)
			return
		}
		type subJSON struct {
			Email     string `json:"email"`
			Account   string `json:"account"`
			Keywords  string `json:"keywords"`
			CreatedAt int64  `json:"created_at"`
		}
		out := make([]subJSON, 0, len(subs))
		for _, sub := range subs {
			out = append(out, subJSON{Email: sub.Email, Account: sub.Account, Keywords: sub.Keywords, CreatedAt: sub.CreatedAt})
		}
		writeJSON(w, map[string]any{
			"configured": s.smtp.configured(),
			"subs":       out,
		})
	case http.MethodDelete:
		email := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("email")))
		if email == "" {
			http.Error(w, "缺少 email 参数", http.StatusBadRequest)
			return
		}
		if err := s.store.DeleteMerchantSubByEmail(email); err != nil {
			http.Error(w, "删除订阅失败", http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	default:
		http.Error(w, "不支持的请求方法", http.StatusMethodNotAllowed)
	}
}

// handleAdminMerchantTestMail 管理接口:发送测试邮件,验证 SMTP 配置是否可用。
// POST {email} → 向指定邮箱发一封测试信;错误信息透传 SMTP 具体报错,便于排障。
func (s *Server) handleAdminMerchantTestMail(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var req struct {
		Email   string `json:"email"`
		Subject string `json:"subject"`
		Body    string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "参数解析失败", http.StatusBadRequest)
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if !strings.Contains(email, "@") || len(email) < 5 {
		http.Error(w, "邮箱格式不正确", http.StatusBadRequest)
		return
	}
	if !s.smtp.configured() {
		http.Error(w, "服务端未配置发件邮箱(-merchant-smtp-user / -merchant-smtp-pass)", http.StatusBadRequest)
		return
	}
	subject := strings.TrimSpace(req.Subject)
	if subject == "" {
		subject = "【测试】远行商人订阅邮件"
	}
	body := strings.TrimSpace(req.Body)
	if body == "" {
		body = "这是一封测试邮件,说明 QQ 邮箱 SMTP 配置正常,新货提醒可以正常投递。\n\n" +
			"发送时间:" + time.Now().Format("2006-01-02 15:04:05") + "\n\n——远行商人订阅自动发送"
	}
	if err := s.smtp.sendMerchantMail(email, subject, body); err != nil {
		http.Error(w, "发送失败:"+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}
