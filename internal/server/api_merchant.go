package server

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/mail"
	"net/smtp"
	"net/url"
	"strings"
	"time"
)

// 远行商人:第三方 API(https://apii.xianyuw.cn/api/v1/rocom-merchant)的本地缓存代理。
//
// 业务模型(按游戏活动节奏):
//   - 每天 8:00 开张、0:00(24 点)收摊,8/12/16/20 四个整点各上架一轮新货,0 点后到次日
//     8:00 前打烊休市没有在售(页面显示刚结束营业日的全天回顾);
//   - 查询槽按 4h 对齐 8 点:8/12/16/20 四轮都回源第三方,次日 0/4 两个槽休市不查;
//   - 结果按槽缓存进 SQLite(store 的 merchant_slots),命中缓存不再回源,防止反复烧第三方 token;
//     缓存保留 2 天,写入时顺手清理更早记录;
//   - 触发回源两条路径:merchantLoop 每 15 分钟检查当前槽(覆盖「早上 8 点自动查第一次」),
//     以及玩家打开页面时 handleMerchant 按当前时间补查缺失的槽;
//   - 订阅提醒:有货槽写入后对比本营业日更早轮找出「新增商品」,对订阅者(邮箱+关键词,空关键词=
//     全部)发 QQ 邮箱邮件;每槽每邮箱只发一次(merchant_notified 去重)。SMTP 未配置时静默跳过。
const (
	merchantFetchURL = "https://apii.xianyuw.cn/api/v1/rocom-merchant"
	merchantOpenHour = 8                // 每天 8 点开张(8 点前休市)
	merchantSlotStep = 4 * time.Hour    // 查询槽跨度(8/12/16/20 四轮)
	merchantCheck    = 15 * time.Minute // 定时器检查间隔
	merchantSmtpHost = "smtp.qq.com"    // QQ 邮箱 SMTP(465 SSL)
)

// merchantSlotJSON 单个 4h 槽。性质分两种:
//   - 上架轮(8/12/16/20):该时段在售卖对应点位上架的商品,empty=查过但无货(不算休市);
//   - 打烊休市(次日 0/4,off=true):00:00~08:00 收摊打烊,没有在售,也不查询。
// merchant 是该槽第三方原始 JSON(仅上架轮有货时带)。
type merchantSlotJSON struct {
	Start    int64           `json:"start"`
	End      int64           `json:"end"`
	Label    string          `json:"label"`
	Empty    bool            `json:"empty"`
	Off      bool            `json:"off"` // true=打烊休市时段(0~8 点),不是商品轮
	Merchant json.RawMessage `json:"merchant,omitempty"`
}

// merchantRespJSON handleMerchant 的响应:status 供前端选择「营业中 / 昨日回顾」。
type merchantRespJSON struct {
	Now    int64              `json:"now"`
	Day    string             `json:"day"` // 当前展示的营业日(YYYY-MM-DD;休市时指刚结束的营业日)
	Status string             `json:"status"`
	Today  []merchantSlotJSON `json:"today"` // 当天 6 个槽(升序,empty 标注休市;8 点前为空,看 prev)
	Prev   []merchantSlotJSON `json:"prev"`  // 仅 status=idle 时填充:昨日的 6 个槽(回顾用)
}

// merchantDayStart 返回 t 所在营业日的 0 点。
func merchantDayStart(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// merchantDaySlots 返回营业日 6 个槽的开始时刻(对齐 8 点:8/12/16/20/0/4)。
func merchantDaySlots(day time.Time) []time.Time {
	return []time.Time{
		day.Add(8 * time.Hour),
		day.Add(12 * time.Hour),
		day.Add(16 * time.Hour),
		day.Add(20 * time.Hour),
		day.Add(24 * time.Hour),
		day.Add(28 * time.Hour), // 次日 4 点
	}
}

// merchantDayStatus 返回当前时刻的营业状态:
// open=营业中(8 点至次日 0 点前,显示今日已上架轮次),idle=打烊休市(0 点后到次日 8 点前,显示昨日回顾)。
func merchantDayStatus(now time.Time) string {
	if now.Hour() < merchantOpenHour {
		return "idle"
	}
	return "open"
}

// merchantLoop 定时补查:每 15 分钟检查一次当前槽,未缓存则回源(8 点开张后自动完成首次查询)。
func (s *Server) merchantLoop() {
	s.merchantEnsure(time.Now())
	t := time.NewTicker(merchantCheck)
	defer t.Stop()
	for range t.C {
		s.merchantEnsure(time.Now())
	}
}

// merchantEnsure 按当前时间补齐缓存:营业中(8-24 点)把今天已开始的轮次(8/12/16/20 中
// 开始时刻 ≤ 现在的)逐个补查缺失的槽;打烊(0-8 点)不查,回顾数据看库。force=true 时跳过
// 缓存直接回源「当前轮」(最后一个已开始的槽),供前端「强制刷新」用。
func (s *Server) merchantEnsure(now time.Time, force ...bool) {
	if s.eggAPIKey == "" {
		return
	}
	if merchantDayStatus(now) == "idle" {
		return
	}
	slots := merchantDaySlots(merchantDayStart(now))

	// 当前轮 = 最后一个开始时刻 ≤ now 的可查槽(前 4 个);8 点前不会有。
	cur := -1
	for i := 0; i < 4; i++ {
		if !slots[i].After(now) {
			cur = i
		}
	}
	if cur < 0 {
		return
	}

	s.merchantMu.Lock()
	// 有货的新回源槽收集起来,锁外再补发订阅邮件(发信慢,别占锁)。
	var notify []time.Time
	if len(force) > 0 && force[0] {
		if ok, empty := s.merchantFetch(slots[cur]); ok && !empty {
			notify = append(notify, slots[cur])
		}
	} else {
		// 普通路径:从 8 点槽补到当前轮,缺失的逐个回源(命中缓存或空标记则跳过)。
		for i := 0; i <= cur; i++ {
			if !s.merchantCached(slots[i].Unix()) {
				if ok, empty := s.merchantFetch(slots[i]); ok && !empty {
					notify = append(notify, slots[i])
				}
			}
		}
	}
	s.merchantMu.Unlock()

	for _, st := range notify {
		s.merchantNotify(st)
	}
}

// merchantCached 判断某槽是否已有缓存记录(empty 也算,避免反复查空)。
func (s *Server) merchantCached(slot int64) bool {
	_, _, ok := s.store.GetMerchantSlot(slot)
	return ok
}

// merchantFetch 回源第三方并写入槽缓存,顺带清理 2 天前的过期记录。
// 返回 (ok, empty):ok=拿到「第三方正常响应」(有货无货都算,仅网络/HTTP 层失败返回 false,
// 不写库);empty=该槽查过但无货。ok && !empty 时调用方应在锁外触发 merchantNotify。
func (s *Server) merchantFetch(slotStart time.Time) (bool, bool) {
	params := url.Values{}
	params.Add("key", s.eggAPIKey)
	params.Add("format", "json")
	params.Add("refresh", "false")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, merchantFetchURL+"?"+params.Encode(), nil)
	if err != nil {
		return false, false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, false
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 上限 1MB
	if err != nil || resp.StatusCode != http.StatusOK {
		return false, false
	}
	// 校验并判定有货/无货:code==0 且 data.items 非空视为有货,其余(无货/业务错误)记空。
	var out struct {
		Code int `json:"code"`
		Data struct {
			Items []json.RawMessage `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return false, false
	}
	empty := out.Code != 0 || len(out.Data.Items) == 0
	if err := s.store.PutMerchantSlot(slotStart.Unix(), empty, string(body)); err != nil {
		return false, false
	}
	return true, empty
}

// handleMerchant 返回当前营业日的槽缓存与状态,玩家打开页面时按当前时间补查缺失槽。
// 参数:force=1 强制回源当前可查槽(烧第三方 token,前端「强制刷新」用)。
// 响应结构见 merchantRespJSON。
func (s *Server) handleMerchant(w http.ResponseWriter, r *http.Request) {
	if s.eggAPIKey == "" {
		http.Error(w, "服务端未配置查询令牌(启动时加 -egg-api-key)", http.StatusServiceUnavailable)
		return
	}
	now := time.Now()
	s.merchantEnsure(now, r.URL.Query().Get("force") == "1")

	day := merchantDayStart(now)
	out := merchantRespJSON{
		Now:    now.Unix(),
		Status: merchantDayStatus(now),
	}
	if out.Status == "idle" {
		// 0-8 点打烊:展示刚结束的营业日(昨天 8 点开张那一轮)的全天回顾。
		day = day.AddDate(0, 0, -1)
		out.Prev = s.merchantSlotsOfDay(day)
	} else {
		out.Today = s.merchantSlotsOfDay(day)
	}
	out.Day = day.Format("2006-01-02")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(out)
}

// merchantSlotsOfDay 组装某营业日的 6 个槽:8/12/16/20 四轮读缓存(有货则带 merchant),
// 次日 0/4 两个槽是打烊休市(固定 empty,不查库)。
func (s *Server) merchantSlotsOfDay(day time.Time) []merchantSlotJSON {
	starts := merchantDaySlots(day)
	out := make([]merchantSlotJSON, 0, len(starts))
	for i, st := range starts {
		js := merchantSlotJSON{
			Start: st.Unix(),
			End:   st.Add(merchantSlotStep).Unix(),
			Label: st.Format("15:04") + "~" + st.Add(merchantSlotStep).Format("15:04"),
			Empty: true,
		}
		if i < 4 { // 8/12/16/20 四轮读缓存
			if empty, data, ok := s.store.GetMerchantSlot(st.Unix()); ok {
				js.Empty = empty
				if !empty {
					js.Merchant = json.RawMessage(data)
				}
			}
		} else { // 次日 0/4 槽:00:00~08:00 打烊休市
			js.Off = true
		}
		out = append(out, js)
	}
	return out
}

// merchantItem 第三方 items 中订阅邮件需要的字段。
type merchantItem struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Price     int    `json:"price"`
	Limit     int    `json:"limit"`
	TimeLabel string `json:"time_label"`
}

// merchantNotify 槽缓存写好且判定有货后调用:对比本营业日更早轮的商品,找出「新增」部分,
// 对关键词命中的订阅者发邮件;同一槽对同一邮箱只发一次(merchant_notified 去重)。
// SMTP 未配置(发件邮箱为空)时静默返回,不影响商家数据本身。
func (s *Server) merchantNotify(slotStart time.Time) {
	if s.smtpUser == "" || s.smtpPass == "" {
		return
	}
	empty, data, ok := s.store.GetMerchantSlot(slotStart.Unix())
	if !ok || empty {
		return
	}
	var out struct {
		Data struct {
			MerchantName string         `json:"merchant_name"`
			Items        []merchantItem `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(data), &out); err != nil {
		return
	}
	// 本营业日更早槽已出现过的商品名(8 点轮无更早槽 → 全部算新增)。
	seen := map[string]bool{}
	for _, st := range merchantDaySlots(merchantDayStart(slotStart)) {
		if !st.Before(slotStart) {
			break
		}
		if e, d, ok2 := s.store.GetMerchantSlot(st.Unix()); ok2 && !e {
			var o struct {
				Data struct {
					Items []struct{ Name string `json:"name"` } `json:"items"`
				} `json:"data"`
			}
			if json.Unmarshal([]byte(d), &o) == nil {
				for _, it := range o.Data.Items {
					if it.Name != "" {
						seen[it.Name] = true
					}
				}
			}
		}
	}
	var news []merchantItem
	for _, it := range out.Data.Items {
		if !seen[it.Name] {
			news = append(news, it)
		}
	}
	if len(news) == 0 {
		return // 本轮与更早轮商品相同,不打扰订阅者
	}

	subs, err := s.store.ListMerchantSubs()
	if err != nil {
		return
	}
	for _, sub := range subs {
		if s.store.MerchantNotified(slotStart.Unix(), sub.Email) {
			continue
		}
		if !merchantSubMatch(sub.Keywords, news) {
			continue
		}
		var body strings.Builder
		fmt.Fprintf(&body, "远行商人「%s」上架了新商品!\n\n", out.Data.MerchantName)
		fmt.Fprintf(&body, "营业日:%s\n", merchantDayStart(slotStart).Format("2006-01-02"))
		fmt.Fprintf(&body, "本轮:%s ~ %s\n\n", slotStart.Format("15:04"), slotStart.Add(merchantSlotStep).Format("15:04"))
		body.WriteString("新增商品:\n")
		for _, it := range news {
			body.WriteString("- ")
			body.WriteString(it.Name)
			if it.Kind != "" {
				fmt.Fprintf(&body, "(%s)", it.Kind)
			}
			fmt.Fprintf(&body, " %d 金币", it.Price)
			if it.Limit > 0 {
				fmt.Fprintf(&body, " 限购 %d", it.Limit)
			}
			body.WriteByte('\n')
			if it.TimeLabel != "" {
				fmt.Fprintf(&body, "  售卖时间:%s\n", it.TimeLabel)
			}
		}
		body.WriteString("\n——\n本邮件由远行商人订阅自动发送;如需退订,请在站点「远行商人」页取消订阅。\n")
		subject := "远行商人新货上架(" + slotStart.Format("15:04") + " 轮)"
		if err := s.sendMerchantMail(sub.Email, subject, body.String()); err == nil {
			s.store.MarkMerchantNotified(slotStart.Unix(), sub.Email)
		}
	}
}

// merchantSubMatch 判断订阅关键词是否命中新增商品:空关键词 = 订阅全部(大小写不敏感子串匹配)。
func merchantSubMatch(keywords string, news []merchantItem) bool {
	if strings.TrimSpace(keywords) == "" {
		return true
	}
	for _, kw := range strings.Split(keywords, ",") {
		kw = strings.ToLower(strings.TrimSpace(kw))
		if kw == "" {
			continue
		}
		for _, it := range news {
			if strings.Contains(strings.ToLower(it.Name), kw) {
				return true
			}
		}
	}
	return false
}

// sendMerchantMail 通过 QQ 邮箱 SMTP(465 SSL)发送邮件。subject/body 由调用方拼好
// (订阅新货提醒 / 管理员测试)。串行发信(smtpMu),避免并发连接被 QQ 邮箱判为异常触发限流。
func (s *Server) sendMerchantMail(to, subject, body string) error {
	s.smtpMu.Lock()
	defer s.smtpMu.Unlock()

	conn, err := tls.Dial("tcp", merchantSmtpHost+":465", &tls.Config{ServerName: merchantSmtpHost})
	if err != nil {
		return err
	}
	c, err := smtp.NewClient(conn, merchantSmtpHost)
	if err != nil {
		conn.Close()
		return err
	}
	defer c.Close()
	if err := c.Auth(smtp.PlainAuth("", s.smtpUser, s.smtpPass, merchantSmtpHost)); err != nil {
		return err
	}
	if err := c.Mail(s.smtpUser); err != nil {
		return err
	}
	if err := c.Rcpt(to); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	h := mail.Header{}
	h.Set("From", s.smtpUser)
	h.Set("To", to)
	h.Set("Subject", mime.QEncoding.Encode("utf-8", subject))
	h.Set("MIME-Version", "1.0")
	h.Set("Content-Type", "text/plain; charset=UTF-8")
	if _, err := w.Write([]byte(h.Encode() + "\r\n" + body)); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// handleMerchantSub 订阅/退订远行商人邮件提醒(玩家侧登记收件 QQ 邮箱与关键词):
//
//	GET    /api/merchant/sub?email=xxx → {configured, subscribed, email, keywords}
//	POST   /api/merchant/sub {email, keywords} → 订阅/更新(关键词逗号分隔,空=全部)
//	DELETE /api/merchant/sub?email=xxx → 退订
func (s *Server) handleMerchantSub(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		email := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("email")))
		keywords, ok := s.store.GetMerchantSub(email)
		writeJSON(w, map[string]any{
			"configured": s.smtpUser != "" && s.smtpPass != "",
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
		if err := s.store.UpsertMerchantSub(email, strings.TrimSpace(req.Keywords)); err != nil {
			http.Error(w, "保存失败", http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	case http.MethodDelete:
		email := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("email")))
		if err := s.store.DeleteMerchantSub(email); err != nil {
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
//	GET    /api/admin/merchant-subs → {configured, subs:[{email, keywords, created_at}]}
//	DELETE /api/admin/merchant-subs?email=xxx → 删除订阅
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
			Keywords  string `json:"keywords"`
			CreatedAt int64  `json:"created_at"`
		}
		out := make([]subJSON, 0, len(subs))
		for _, sub := range subs {
			out = append(out, subJSON{Email: sub.Email, Keywords: sub.Keywords, CreatedAt: sub.CreatedAt})
		}
		writeJSON(w, map[string]any{
			"configured": s.smtpUser != "" && s.smtpPass != "",
			"subs":       out,
		})
	case http.MethodDelete:
		email := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("email")))
		if email == "" {
			http.Error(w, "缺少 email 参数", http.StatusBadRequest)
			return
		}
		if err := s.store.DeleteMerchantSub(email); err != nil {
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
		Email string `json:"email"`
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
	if s.smtpUser == "" || s.smtpPass == "" {
		http.Error(w, "服务端未配置发件邮箱(-merchant-smtp-user / -merchant-smtp-pass)", http.StatusBadRequest)
		return
	}
	subject := "【测试】远行商人订阅邮件"
	body := "这是一封测试邮件,说明 QQ 邮箱 SMTP 配置正常,新货提醒可以正常投递。\n\n" +
		"发送时间:" + time.Now().Format("2006-01-02 15:04:05") + "\n\n——远行商人订阅自动发送"
	if err := s.sendMerchantMail(email, subject, body); err != nil {
		http.Error(w, "发送失败:"+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}
