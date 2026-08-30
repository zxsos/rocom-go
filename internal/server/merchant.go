package server

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
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

// merchantLoc 固定北京时间(UTC+8):游戏按北京时间 8 点开张,第三方时间戳也是北京时区语义
// (fetched_at 为 UTC 的 8 点 = 北京 8 点)。不依赖服务器本地时区——云服务器常默认 UTC,
// 会导致 slot 与营业状态整体错位 8 小时(UTC 凌晨被误判「打烊」,永远不回源)。
var merchantLoc = time.FixedZone("CST", 8*3600)

// merchantSlotJSON 单个 4h 槽。性质分两种:
//   - 上架轮(8/12/16/20):该时段在售卖对应点位上架的商品,empty=查过但无货(不算休市);
//   - 打烊休市(次日 0/4,off=true):00:00~08:00 收摊打烊,没有在售,也不查询。
//
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
// 当天漏看)。幂等:merchantNotify 内部按 merchant_notified 去重(已发成功的槽自动跳过),
// 且同一槽在冷却内只认领一次(见 merchant_notify.go 的 merchantClaim),故本函数与
// merchantEnsure 的异步发信撞到同一槽也不会重复打扰;SMTP 未配置或打烊(0-8 点)时直接返回。
func (s *Server) merchantResend(now time.Time) {
	if !s.smtp.configured() {
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
