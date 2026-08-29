package server

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"io/fs"
	"log"
	"mime"
	"net"
	"net/http"
	"net/smtp"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/whoisnian/rocom-capture/internal/gamedata"
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

// merchantLoc 固定北京时间(UTC+8):游戏按北京时间 8 点开张,第三方时间戳也是北京时区语义
// (fetched_at 为 UTC 的 8 点 = 北京 8 点)。不依赖服务器本地时区——云服务器常默认 UTC,
// 会导致 slot 与营业状态整体错位 8 小时(UTC 凌晨被误判「打烊」,永远不回源)。
var merchantLoc = time.FixedZone("CST", 8*3600)

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

// merchantDayStart 返回 t 所在营业日的 0 点(按北京时间计算)。
func merchantDayStart(t time.Time) time.Time {
	t = t.In(merchantLoc)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, merchantLoc)
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

// merchantDayStatus 返回当前时刻的营业状态(按北京时间判定):
// open=营业中(8 点至次日 0 点前,显示今日已上架轮次),idle=打烊休市(0 点后到次日 8 点前,显示昨日回顾)。
func merchantDayStatus(now time.Time) string {
	if now.In(merchantLoc).Hour() < merchantOpenHour {
		return "idle"
	}
	return "open"
}

// merchantLoop 定时补查:每 15 分钟检查一次当前槽,未缓存则回源(8 点开张后自动完成首次查询);
// 随后补扫当日有货槽重发未投递的订阅提醒(兜底首次发信失败/事后补发)。
func (s *Server) merchantLoop() {
	s.merchantEnsure(time.Now())
	t := time.NewTicker(merchantCheck)
	defer t.Stop()
	for range t.C {
		now := time.Now()
		s.merchantEnsure(now)
		s.merchantResend(now)
	}
}

// merchantResend 补扫本营业日已开始的有货槽,对「未通知且关键词命中」的订阅重发提醒。
// 用于兜底首次发信失败(SMTP 瞬断/授权码过期/被限流)与事后补发(如服务 8 点后才启动、
// 当天漏看)。幂等:merchantNotify 内部按 merchant_notified 去重,已发过的槽自动跳过,
// 不会重复打扰;SMTP 未配置或打烊(0-8 点)时直接返回。
func (s *Server) merchantResend(now time.Time) {
	if s.smtpUser == "" || s.smtpPass == "" {
		return
	}
	if merchantDayStatus(now) == "idle" {
		return
	}
	for _, st := range merchantDaySlots(merchantDayStart(now)) {
		if st.After(now) {
			break // 只扫已开始的槽(8/12/16/20 中开始时刻 ≤ now 的)
		}
		if empty, _, ok := s.store.GetMerchantSlot(st.Unix()); !ok || empty {
			continue // 未回源或无货,无提醒可发
		}
		s.merchantNotify(st)
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

	// 订阅邮件在后台 goroutine 发:SMTP 偶发慢/挂连接,同步发会阻塞「强制刷新」的 HTTP
	// 响应(前端 fetch 一直等)。sendMerchantMail 自带整体 deadline,这里异步双保险。
	go func() {
		for _, st := range notify {
			s.merchantNotify(st)
		}
	}()
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
		log.Printf("merchantFetch 构造请求失败: %v", err)
		return false, false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("merchantFetch 请求失败: %v", err)
		return false, false
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 上限 1MB
	if err != nil {
		log.Printf("merchantFetch 读响应失败: %v", err)
		return false, false
	}
	if resp.StatusCode != http.StatusOK {
		log.Printf("merchantFetch HTTP %d, 响应前 200 字节: %q", resp.StatusCode, truncateBytes(body, 200))
		return false, false
	}
	// 校验并判定有货/无货:第三方成功码不统一,实测 code=0 与 code=200 都表示成功,
	// 故 code∈{0,200} 且 data.items 非空视为有货,其余(无货/业务错误)记空。
	var out struct {
		Code int `json:"code"`
		Data struct {
			Items []json.RawMessage `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		log.Printf("merchantFetch JSON 解析失败: %v, 响应前 200 字节: %q", err, truncateBytes(body, 200))
		return false, false
	}
	empty := !((out.Code == 0 || out.Code == 200) && len(out.Data.Items) > 0)
	if empty {
		log.Printf("merchantFetch 第三方返回无货: code=%d items=%d", out.Code, len(out.Data.Items))
	}
	if err := s.store.PutMerchantSlot(slotStart.Unix(), empty, string(body)); err != nil {
		log.Printf("merchantFetch 写槽缓存失败: %v", err)
		return false, false
	}
	return true, empty
}

// truncateBytes 截断字节串用于日志(避免刷屏),超长时加省略号。
func truncateBytes(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
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
				// 自动修正历史误判:此前 code==200 被当失败写成 empty,读缓存时按原始 body 重新判定有货。
				if empty && merchantBodyHasItems(data) {
					empty = false
				}
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

// merchantBodyHasItems 判断缓存的第三方原始 JSON 是否实际有货:
// code∈{0,200}(第三方成功码不统一)且 data.items 非空。用于读取缓存时修正历史误判的空标记。
func merchantBodyHasItems(data string) bool {
	var out struct {
		Code int `json:"code"`
		Data struct {
			Items []json.RawMessage `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(data), &out); err != nil {
		return false
	}
	return (out.Code == 0 || out.Code == 200) && len(out.Data.Items) > 0
}

// merchantItem 第三方 items 中订阅邮件需要的字段。
type merchantItem struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Price     int    `json:"price"`
	Limit     int    `json:"limit"`
	TimeLabel string `json:"time_label"`
	StartTime int64  `json:"start_time"` // 毫秒(北京时间语义),time_label 缺失时推断时段用
	EndTime   int64  `json:"end_time"`
	Image     string `json:"image"` // 商品图:http(s) 外链原样;否则为本站 /img/ 相对路径(邮件里 CID 内嵌)
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
		// merchant_name 第三方已含完整显示名(如「远行商人「云上仙岛」」),这里直接
		// 使用不再硬编码前缀,避免出现「远行商人 远行商人 上架了新商品」的重复。
		name := out.Data.MerchantName
		if name == "" {
			name = "远行商人"
		}
		var imgs []merchantMailImg
		content := merchantMailContent(name,
			merchantDayStart(slotStart).Format("2006-01-02"),
			slotStart.Format("15:04")+" ~ "+slotStart.Add(merchantSlotStep).Format("15:04"),
			news, &imgs)
		// 退订签名只在 HTML 模板尾部保留一份(见 merchantMailHTMLTpl),正文不再重复。
		subject := "远行商人新货上架(" + slotStart.Format("15:04") + " 轮)"
		if err := s.sendMerchantMailHTML(sub.Email, subject, content, imgs); err == nil {
			s.store.MarkMerchantNotified(slotStart.Unix(), sub.Email)
		} else {
			// 发信失败不 Mark:补扫(merchantResend)或下次触发仍会重试。
			// 之前这里是静默吞错,查无可查(邮件没到 = 授权码过期/被限流/网络瞬断都无痕迹)。
			log.Printf("merchantNotify 发信失败 slot=%s to=%s: %v", slotStart.Format("15:04"), sub.Email, err)
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

// 发件人显示名(收件端显示「远哥来了 <sender@example.com>」),RFC 2047 编码支持中文。
const merchantMailFromName = "远哥来了"

// merchantMailHTMLTpl 邮件正文模板:纯白背景 + 浅色卡片 + 金色标题栏。
const merchantMailHTMLTpl = `<!DOCTYPE html>
<html lang="zh-CN"><body style="margin:0;padding:0;background:#ffffff;">
<div style="background:#ffffff;padding:36px 16px;font-family:-apple-system,'PingFang SC','Microsoft YaHei',sans-serif;">
  <div style="max-width:560px;margin:0 auto;background:#fffaf0;border-radius:18px;overflow:hidden;box-shadow:0 12px 40px rgba(0,0,0,.4);">
    <div style="background:linear-gradient(135deg,#f0b429,#d99a1e);padding:22px 28px;">
      <div style="font-size:20px;font-weight:800;color:#3a2505;">远行商人</div>
      <div style="font-size:12px;color:#7a5a15;margin-top:4px;">新货上架提醒</div>
    </div>
    <div style="padding:26px 28px;color:#3a2a14;font-size:14px;line-height:1.9;">%s</div>
    <div style="background:#f3e7cc;padding:14px 28px;font-size:12px;color:#8a6d3b;text-align:center;line-height:1.7;">
      本邮件由「远行商人」新货提醒自动发送<br>如需退订,请到站点「远行商人」页取消订阅
    </div>
  </div>
</div>
</body></html>`

// merchantMailFrom 生成带中文显示名的 From 头。
func (s *Server) merchantMailFrom() string {
	return mime.QEncoding.Encode("utf-8", merchantMailFromName) + " <" + s.smtpUser + ">"
}

// merchantMailImg 邮件内嵌图片附件(cid 引用 + 原始 webp 字节)。
type merchantMailImg struct {
	cid  string
	data []byte
}

// merchantMailBody 把纯文本正文转成模板包裹的 HTML(保留换行与前导空格,列表行转 •;
// 行内 @img:<path> 商品图标记渲染为 <img src="cid:...">,图片字节收集到 imgs,
// 由 sendMerchantMail 以 multipart/related 附件发送)。
func merchantMailBody(body string, imgs *[]merchantMailImg) string {
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " ")
		lead := len(line) - len(trimmed)
		if strings.HasPrefix(trimmed, "- ") {
			trimmed = "• " + strings.TrimPrefix(trimmed, "- ")
		}
		esc := merchantMailEscaped(trimmed, imgs)
		if lead > 0 {
			esc = strings.Repeat("&nbsp;", lead) + esc
		}
		// 单行断到 ≤850 字节:防止某一行本身(商品名/描述极端长)超限。
		lines[i] = merchantMailWrap(esc, 850)
	}
	// 行间用 <br>\r\n 连接而非裸 <br>:HTML 里 CRLF 渲染为空白,不产生可见换行
	// (可见换行由 <br> 负责),但能让每条商品独占一行,避免商品一多整段正文
	// 拼成一个超 998 字节的巨型单行触发 RFC 5321 拒绝。
	// 用 Replace 而非 Sprintf:模板背景渐变色里有裸 %(0%,55%,100%),
	// Sprintf 会把它当格式 verb 误解析导致正文占位符拿不到参数(%!s(MISSING))。
	return strings.Replace(merchantMailHTMLTpl, "%s", strings.Join(lines, "<br>\r\n"), 1)
}

// merchantMailWrap 把转义后的单行 HTML 在累计字节 ≥ maxBytes 处插入 CRLF 断行,
// 防止极端长行(超长商品名/描述)突破 RFC 5321 的 998 字节单行限制。
// 断点落在 rune 边界,不断开 UTF-8 字符;HTML 里裸 CRLF 渲染为空白,
// 不产生可见换行(可见换行由 <br> 负责),对收件端视觉无影响。
func merchantMailWrap(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	var b strings.Builder
	var n int
	for _, r := range s {
		size := len(string(r))
		if n > 0 && n+size > maxBytes {
			b.WriteString("\r\n")
			n = 0
		}
		b.WriteRune(r)
		n += size
	}
	return b.String()
}

// merchantMailEscaped 转义一行文本,并把行内的 @img:<path> 商品图标记替换成 <img>(不转义)。
func merchantMailEscaped(s string, imgs *[]merchantMailImg) string {
	if !strings.Contains(s, "@img:") {
		return html.EscapeString(s)
	}
	var b strings.Builder
	rest := s
	for {
		i := strings.Index(rest, "@img:")
		if i < 0 {
			b.WriteString(html.EscapeString(rest))
			break
		}
		b.WriteString(html.EscapeString(rest[:i]))
		rest = rest[i+len("@img:"):]
		j := strings.IndexAny(rest, " \t")
		path := rest
		if j >= 0 {
			path, rest = rest[:j], rest[j:]
		} else {
			rest = ""
		}
		if src := merchantImgHTML(path, imgs); src != "" {
			b.WriteString(src)
		}
	}
	return b.String()
}

// merchantImgHTML 把商品图路径渲染为邮件 <img>:http(s) 外链直接引用;
// 本地相对路径(本站 /img/ 前缀)读 embed 的 webp,以 CID 引用收集到 imgs
// (收件端无需访问本站即可显示,且不受 SMTP 单行 998 字节限制)。
// 读不到图片时返回空串(不显示)。
func merchantImgHTML(src string, imgs *[]merchantMailImg) string {
	if src == "" {
		return ""
	}
	if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") {
		return merchantImgTag(src)
	}
	if b, err := fs.ReadFile(gamedata.ImageFS(), src); err == nil && len(b) > 0 {
		cid := fmt.Sprintf("merchant%d", len(*imgs)+1)
		*imgs = append(*imgs, merchantMailImg{cid: cid, data: b})
		return merchantImgTag("cid:" + cid)
	}
	return ""
}

// merchantImgTag 生成商品图 <img> 标签(src 是最终可用的 URL 或 cid: 引用)。
func merchantImgTag(src string) string {
	return `<img src="` + src + `" alt="" style="width:56px;height:56px;object-fit:contain;border-radius:10px;vertical-align:middle;margin:0 10px 0 2px;">`
}

// merchantKindTextMap 第三方商品 kind(英文) → 中文显示;未收录的未知值原样返回,
// 宁可不译不错译(邮件里出现新值可再补表)。
var merchantKindTextMap = map[string]string{
	"prop": "道具", "pet": "宠物", "egg": "精灵蛋", "fragment": "碎片",
	"skin": "皮肤", "cloth": "装扮", "material": "材料", "seed": "种子",
	"fruit": "果实", "food": "食物", "gem": "宝石", "diamond": "钻石",
	"ticket": "票券", "tool": "工具", "equip": "装备", "consumable": "消耗品",
	"furniture": "家具", "card": "卡片", "scroll": "卷轴", "key": "钥匙",
	"medal": "奖牌", "suit": "套装", "decoration": "装饰", "coin": "洛克贝",
}

func merchantKindText(kind string) string {
	if t, ok := merchantKindTextMap[kind]; ok {
		return t
	}
	return kind
}

// merchantMailSlots 标准售卖时段(北京时间),与前端 Merchant.jsx 的 SLOTS 一致。
var merchantMailSlots = []string{"08:00-12:00", "12:00-16:00", "16:00-20:00", "20:00-24:00"}

var merchantSlotRe = regexp.MustCompile(`^\d{2}:\d{2}-\d{2}:\d{2}$`)

// merchantItemSlots 解析商品售卖时段(与前端 parseSlots 一致):优先 time_label
//("08:00-12:00 / …" 用 / 分割 + 正则校验),为空/格式不符时按 start_time/end_time
// (毫秒,北京时间语义)推断为单个时段串。
func merchantItemSlots(it merchantItem) []string {
	if raw := strings.TrimSpace(it.TimeLabel); raw != "" {
		slots := []string{}
		for _, s := range strings.Split(raw, "/") {
			if s = strings.TrimSpace(s); merchantSlotRe.MatchString(s) {
				slots = append(slots, s)
			}
		}
		if len(slots) > 0 {
			return slots
		}
	}
	if it.StartTime > 0 && it.EndTime > 0 {
		st := time.UnixMilli(it.StartTime).In(merchantLoc)
		et := time.UnixMilli(it.EndTime).In(merchantLoc)
		s, e := st.Format("15:04"), et.Format("15:04")
		if e == "00:00" {
			e = "24:00"
		}
		if s == e {
			return nil
		}
		return []string{s + "-" + e}
	}
	return nil
}

// merchantAllDay 是否覆盖全部四个标准时段(全天售卖)。
func merchantAllDay(slots []string) bool {
	for _, s := range merchantMailSlots {
		found := false
		for _, x := range slots {
			if x == s {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// merchantGroup 邮件正文的时段分组:全天 / 标准时段 / 其他。
type merchantGroup struct {
	Title string
	Items []merchantItem
}

// merchantGroupItems 把新增商品按时段分组(与前端 groupBySlot 一致):
// 覆盖全部四段的归「全天」,其余按各自时段归组,不在标准四段内的归「其他」。
func merchantGroupItems(items []merchantItem) (allDay []merchantItem, groups []merchantGroup, other []merchantItem) {
	slotGroups := make([][]merchantItem, len(merchantMailSlots))
	slotIdx := make(map[string]int, len(merchantMailSlots))
	for i, s := range merchantMailSlots {
		slotIdx[s] = i
	}
	for _, it := range items {
		slots := merchantItemSlots(it)
		if len(slots) > 0 && merchantAllDay(slots) {
			allDay = append(allDay, it)
			continue
		}
		hit := false
		for _, s := range slots {
			if i, ok := slotIdx[s]; ok {
				slotGroups[i] = append(slotGroups[i], it)
				hit = true
			}
		}
		if !hit {
			other = append(other, it)
		}
	}
	for i, s := range merchantMailSlots {
		if len(slotGroups[i]) > 0 {
			groups = append(groups, merchantGroup{Title: s, Items: slotGroups[i]})
		}
	}
	return allDay, groups, other
}

// merchantMailContent 构造新货提醒的内容区 HTML(嵌入 merchantMailHTMLTpl 的 %s):
// 商人名大标题 + 营业日/本轮,商品按「全天 / 标准时段 / 其他」分组展示。
// 全天商品不打具体时间点(组标题已表达),商品图以 CID 引用收集到 imgs。
func merchantMailContent(name, day, slot string, items []merchantItem, imgs *[]merchantMailImg) string {
	var b strings.Builder
	// 每行以 CRLF 结尾:HTML 里裸 CRLF 渲染为空白,不产生可见换行,但保证
	// 任何单行(组标题/商品行)都不超过 RFC 5321 的 998 字节限制。
	b.WriteString(`<div style="text-align:center;padding:2px 0 0;">` + "\r\n")
	b.WriteString(`<span style="font-size:22px;font-weight:800;color:#3a2505;">` + html.EscapeString(name) + `</span></div>` + "\r\n")
	b.WriteString(`<div style="text-align:center;font-size:12px;color:#8a6d3b;margin:4px 0 2px;">营业日 ` + html.EscapeString(day) + ` · 本轮 ` + html.EscapeString(slot) + `</div>` + "\r\n")
	allDay, groups, other := merchantGroupItems(items)
	merchantMailGroup(&b, "全天售卖", allDay, imgs)
	for _, g := range groups {
		merchantMailGroup(&b, g.Title, g.Items, imgs)
	}
	merchantMailGroup(&b, "其他时段", other, imgs)
	return b.String()
}

// merchantMailGroup 输出一个时段分组:金色小标题 + 商品行列表。
func merchantMailGroup(b *strings.Builder, title string, items []merchantItem, imgs *[]merchantMailImg) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(b, `<div style="font-size:13px;font-weight:700;color:#a06a10;margin:16px 0 8px;padding-left:8px;border-left:3px solid #f0b429;">%s</div>\r\n`, html.EscapeString(title))
	for _, it := range items {
		merchantMailItemRow(b, it, imgs)
	}
}

// merchantMailItemRow 输出单个商品行:图 + 名称 + (类型 · 价格 · 限购)。
func merchantMailItemRow(b *strings.Builder, it merchantItem, imgs *[]merchantMailImg) {
	imgTag := ""
	if src := merchantMailItemImg(it, imgs); src != "" {
		imgTag = `<img src="` + src + `" alt="" style="width:44px;height:44px;border-radius:8px;object-fit:cover;flex:none;background:#f7efdb;">`
	}
	var meta []string
	if k := merchantKindText(it.Kind); k != "" {
		meta = append(meta, k)
	}
	meta = append(meta, fmt.Sprintf("%d 洛克贝", it.Price))
	if it.Limit > 0 {
		meta = append(meta, fmt.Sprintf("限购 %d", it.Limit))
	}
	b.WriteString(`<div style="display:flex;align-items:center;gap:10px;background:#fff;border:1px solid #f2e6c8;border-radius:10px;padding:9px 12px;margin:7px 0;">` + "\r\n")
	if imgTag != "" {
		b.WriteString(imgTag + "\r\n")
	}
	b.WriteString(`<div style="flex:1;min-width:0;">` + "\r\n")
	b.WriteString(`<div style="font-size:14px;font-weight:700;color:#3a2a14;">` + html.EscapeString(it.Name) + `</div>` + "\r\n")
	if len(meta) > 0 {
		b.WriteString(`<div style="font-size:12px;color:#8a6d3b;margin-top:2px;">` + html.EscapeString(strings.Join(meta, " · ")) + `</div>` + "\r\n")
	}
	b.WriteString(`</div></div>` + "\r\n")
}

// merchantMailItemImg 商品图 URL:http(s) 外链原样;本地 embed 路径读 webp 以 CID 收集。
func merchantMailItemImg(it merchantItem, imgs *[]merchantMailImg) string {
	if it.Image == "" {
		return ""
	}
	if strings.HasPrefix(it.Image, "http://") || strings.HasPrefix(it.Image, "https://") {
		return html.EscapeString(it.Image)
	}
	if data, err := fs.ReadFile(gamedata.ImageFS(), it.Image); err == nil && len(data) > 0 {
		cid := fmt.Sprintf("merchant%d", len(*imgs)+1)
		*imgs = append(*imgs, merchantMailImg{cid: cid, data: data})
		return "cid:" + cid
	}
	return ""
}

// sendMerchantMail 通过 QQ 邮箱 SMTP(465 SSL)发送纯文本正文邮件(订阅验证 / 管理员测试),
// 内部转成模板 HTML。新货提醒的排版走 sendMerchantMailHTML(直接发结构化 HTML)。
func (s *Server) sendMerchantMail(to, subject, body string) error {
	var imgs []merchantMailImg
	html := merchantMailBody(body, &imgs)
	return s.smtpSendMail(to, subject, html, imgs)
}

// sendMerchantMailHTML 发送已构造好的内容区 HTML(含 CID 内嵌图),merchantNotify 新货提醒用。
// htmlBody 是嵌入 merchantMailHTMLTpl 的 %s 内容区(分组卡片 + 商品行)。
func (s *Server) sendMerchantMailHTML(to, subject, htmlBody string, imgs []merchantMailImg) error {
	return s.smtpSendMail(to, subject, strings.Replace(merchantMailHTMLTpl, "%s", htmlBody, 1), imgs)
}

// smtpSendMail 底层 SMTP 发送:正文以 multipart/related + CID 内嵌(HTML 引用 cid:,
// 附件 base64 每 76 字符分行),避免单行超过 RFC 5321 的 998 字节限制导致 500 拒收。
// 串行发信(smtpMu),避免并发连接被 QQ 邮箱判为异常触发限流。
// 整体 deadline 兜底:QQ SMTP 偶发挂连接(网络波动/被限流),TLS 拨号限 10s,
// 连接建立后全程 I/O 限 20s,避免调用方(管理页强制刷新等)无限等待。
func (s *Server) smtpSendMail(to, subject, html string, imgs []merchantMailImg) error {
	s.smtpMu.Lock()
	defer s.smtpMu.Unlock()

	dialer := &net.Dialer{Timeout: 10 * time.Second}
	conn, err := tls.DialWithDialer(dialer, "tcp", merchantSmtpHost+":465", &tls.Config{ServerName: merchantSmtpHost})
	if err != nil {
		return err
	}
	conn.SetDeadline(time.Now().Add(20 * time.Second))
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
	msg := merchantMailMessage(s.merchantMailFrom(), to, mime.QEncoding.Encode("utf-8", subject), html, imgs)
	if _, err := io.WriteString(w, msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// merchantMailMessage 组装完整邮件文本(头部 + 正文)。有内嵌图片时用
// multipart/related:HTML 引用 cid:,附件 base64 每 76 字符一行(CRLF),
// 满足 RFC 5321 单行 998 字节限制;无图片时保持单一 text/html。
func merchantMailMessage(from, to, subject, html string, imgs []merchantMailImg) string {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\n", from, to, subject)
	if len(imgs) == 0 {
		b.WriteString("Content-Type: text/html; charset=UTF-8\r\n\r\n")
		b.WriteString(html)
		return b.String()
	}
	boundary := fmt.Sprintf("----=_rocom_%x", time.Now().UnixNano())
	fmt.Fprintf(&b, "Content-Type: multipart/related; boundary=%s\r\n\r\n", boundary)
	fmt.Fprintf(&b, "--%s\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n%s\r\n", boundary, html)
	for _, img := range imgs {
		fmt.Fprintf(&b, "--%s\r\nContent-Type: image/webp\r\nContent-Transfer-Encoding: base64\r\nContent-ID: <%s>\r\n\r\n", boundary, img.cid)
		enc := base64.StdEncoding.EncodeToString(img.data)
		for len(enc) > 76 {
			b.WriteString(enc[:76] + "\r\n")
			enc = enc[76:]
		}
		b.WriteString(enc + "\r\n")
	}
	b.WriteString("--" + boundary + "--\r\n")
	return b.String()
}

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
		if err := s.store.UpsertMerchantSub(account, email, strings.TrimSpace(req.Keywords)); err != nil {
			http.Error(w, "保存失败", http.StatusInternalServerError)
			return
		}
		// 保存成功:自动发送验证邮件,确认收件邮箱可达。发信失败不阻塞订阅(仅提示)。
		out := map[string]any{"ok": true, "mail_sent": false}
		if s.smtpUser != "" && s.smtpPass != "" {
			subject := "【远哥来了】订阅成功验证"
			body := "你已成功订阅「远行商人」新货提醒!\n\n" +
				"本邮件用于验证收件邮箱可正常接收提醒,无需回复。\n" +
				"此后每轮(8/12/16/20 点)有新增商品上架时,会第一时间发邮件通知你。\n\n" +
				"——远哥来了"
			if err := s.sendMerchantMail(email, subject, body); err != nil {
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
			"configured": s.smtpUser != "" && s.smtpPass != "",
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
	if s.smtpUser == "" || s.smtpPass == "" {
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
	if err := s.sendMerchantMail(email, subject, body); err != nil {
		http.Error(w, "发送失败:"+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}
