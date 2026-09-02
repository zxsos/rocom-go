package server

import (
	"encoding/json"
	"log"
	"strings"
	"sync"
	"time"
)

// smtpUnconfiguredOnce 让「未配置 SMTP 发件邮箱」只提示一次(理由见 merchantNotify)。
var smtpUnconfiguredOnce sync.Once

type merchantItem struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Price     int    `json:"price"`
	Limit     int    `json:"limit"`
	TimeLabel string `json:"time_label"`
	StartTime int64  `json:"start_time"` // 毫秒(北京时间语义),time_label 缺失时推断时段用
	EndTime   int64  `json:"end_time"`
	Image     string `json:"image"` // 商品图:两源都是 patchwiki.biligame.com 的 https 直链;非 http(s) 一律不显示
}

// merchantClaimCooldown 是同一槽两次认领之间的最小间隔。
//
// 取 10 分钟(小于 merchantResend 的 15 分钟 tick):并发撞到同一槽的两个触发源
// (回源后的异步发信 + 同 tick 补扫)间隔只有毫秒级,必然被拦住;而发信失败的槽
// 下一次补扫(≥15 分钟后)仍能重试,补扫兜底能力不受影响。
const merchantClaimCooldown = 10 * time.Minute

// merchantClaim 认领某槽的「本轮发信权」,已被认领且在冷却内时返回 false(调用方应直接返回)。
//
// 存在理由:merchant_notified 只在**发信成功之后**才 Mark(发信失败要留给 merchantResend
// 补扫重试),而一次 SMTP 往返是秒级。于是两个触发源撞到同一槽时,后到者读到的必然是
// 「未 Mark」,于是各发一封 —— 表现为订阅者收到两份一模一样的邮件。8/12/16/20 每档首次
// 回源必撞:merchantEnsure 回源后在 goroutine 里异步发信,merchantLoop 同一次 tick 紧接着
// 又跑 merchantResend 补扫,补扫读缓存命中「有货且未 Mark」便再发一轮。
//
// 认领带冷却而非「认领后永久占用」:永久占用会把发信失败的槽一并锁死,补扫重试就失效了;
// 冷却内拦重、冷却后放行,两个诉求都满足。跨进程/重启由 merchant_notified 兜底(已发成功
// 的槽在库里有记录,重启后也不会重发)。
// 表只按当前营业日使用,认领时顺手清掉往日与过冷却的条目,不会长驻增长。
func (s *Server) merchantClaim(slotStart time.Time) bool {
	now := time.Now()
	s.merchantClaimMu.Lock()
	defer s.merchantClaimMu.Unlock()
	if s.merchantClaimed == nil {
		s.merchantClaimed = map[int64]time.Time{}
	}
	yesterday := merchantDayStart(now).AddDate(0, 0, -1).Unix()
	for slot, at := range s.merchantClaimed {
		if slot < yesterday || now.Sub(at) >= merchantClaimCooldown {
			delete(s.merchantClaimed, slot)
		}
	}
	key := slotStart.Unix()
	if at, ok := s.merchantClaimed[key]; ok && now.Sub(at) < merchantClaimCooldown {
		return false
	}
	s.merchantClaimed[key] = now
	return true
}

// merchantNotify 槽缓存写好且判定有货后调用:对比本营业日更早轮的商品与本槽**已通知过**的
// 商品,找出「新增」部分,对关键词命中的订阅者发邮件;每个商品对同一邮箱只提醒一次。
//
// 去重两层:库表 merchant_notified.items 挡跨进程/重启与**同一轮内的多次回源**(发信成功后
// 才 Mark,失败留给补扫重试),merchantClaim 挡同进程内并发触发(见该函数注释)。
//
// 按商品而非按槽去重的理由:第三方滞后补货,同一轮会回源多次(见 merchantShouldFetch),
// 每次都可能带来新商品。只按槽去重的话,轮次开始那次若已发过信,后续补上的商品就被永久挡住
// —— 2026-08-30 就是这样:20:0x 首查只有 4 件全天货,20:56 补上的 3 件专属货再没发出去。
// 现在第二次通知只会包含「上一次没发过的」那几件。
//
// SMTP 未配置(发件邮箱为空)时静默返回,不影响商家数据本身。
func (s *Server) merchantNotify(slotStart time.Time) {
	if !s.smtp.configured() {
		// 只提示**一次**:这条对排查有价值(没配 SMTP 就永远不会有邮件),但
		// merchantNotify 每轮回源都会走一遍 —— 不收敛的话,一个不打算用邮件功能的
		// 部署每天会打几十条同样的话,把真正要紧的发信耗时日志淹掉。
		// (merchantResend 开头已过滤掉这种情况,故这里只在回源成功后触发。)
		smtpUnconfiguredOnce.Do(func() {
			log.Printf("merchantNotify 跳过: 未配置 SMTP 发件邮箱(需 -merchant-smtp-user/-pass),订阅提醒不会发送")
		})
		return
	}
	empty, data, fetchedAt, ok := s.store.GetMerchantSlot(slotStart.Unix())
	if !ok {
		log.Printf("merchantNotify 跳过 slot=%s: 该槽还没有缓存记录", slotStart.Format("01-02 15:04"))
		return
	}
	if empty {
		log.Printf("merchantNotify 跳过 slot=%s: 该槽查过但无货", slotStart.Format("01-02 15:04"))
		return
	}
	var out struct {
		Data struct {
			MerchantName string         `json:"merchant_name"`
			Items        []merchantItem `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(data), &out); err != nil {
		// 换源后这里最值得盯:库里存的是**第三方原始 JSON**,若新源的输出与
		// 约定不符(比如归一化漏字段、站点改版直接吐 HTML),此前是静默返回 ——
		// 表现就是「数据看着有,但邮件永远不发」,且日志里什么都没有。
		log.Printf("merchantNotify 货单解析失败 slot=%s: %v(响应前 200 字节: %q)",
			slotStart.Format("01-02 15:04"), err, truncateBytes([]byte(data), 200))
		return
	}
	// 本营业日更早槽已出现过的商品名(8 点轮无更早槽 → 全部算新增)。
	seen := map[string]bool{}
	for _, st := range merchantDaySlots(merchantDayStart(slotStart)) {
		if !st.Before(slotStart) {
			break
		}
		if e, d, _, ok2 := s.store.GetMerchantSlot(st.Unix()); ok2 && !e {
			var o struct {
				Data struct {
					Items []struct {
						Name string `json:"name"`
					} `json:"items"`
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
	// 每个订阅者的「本轮新增」不同 —— 已通知过的商品按邮箱分别去重,故逐个算。
	// 先算出全部待办再统一认领发信权,避免「第一个人没新货」就把整槽的发信权占掉。
	subs, err := s.store.ListMerchantSubs()
	if err != nil {
		log.Printf("merchantNotify 读订阅列表失败 slot=%s: %v", slotStart.Format("01-02 15:04"), err)
		return
	}
	type pending struct {
		email string
		news  []merchantItem
	}
	var pend []pending
	// unnotified 记「所有订阅者里最多的未通知新货数」,用来区分下面两种「pend 为空」:
	//   - 为 0:这些都通知过了 —— 正常;
	//   - > 0:有新货却没人收到 —— 反常(多半是订阅关键词配置与预期不符),值得记一笔。
	unnotified := 0
	for _, sub := range subs {
		notified := s.store.MerchantNotifiedItems(slotStart.Unix(), sub.Email)
		var news []merchantItem
		for _, it := range out.Data.Items {
			if !seen[it.Name] && !notified[it.Name] {
				news = append(news, it)
			}
		}
		if len(news) > unnotified {
			unnotified = len(news)
		}
		if len(news) == 0 || !merchantSubMatch(sub.Keywords, news) {
			continue // 没有他没见过的商品,或关键词没命中,不打扰
		}
		pend = append(pend, pending{sub.Email, news})
	}
	if len(pend) == 0 {
		// 只在**有新货却没人收到**时记。若只是「都已通知过」则不记 —— merchantResend
		// 每 15 分钟把每个有货槽都过一遍,那种情况记下来纯属刷屏,反而把要紧的
		// 发信耗时日志淹掉。
		if unnotified > 0 {
			log.Printf("merchantNotify 跳过 slot=%s: %d 件未通知新货,但无订阅者关键词命中(订阅数=%d)",
				slotStart.Format("01-02 15:04"), unnotified, len(subs))
		}
		return
	}
	// 认领本槽发信权:拦住并发触发的重复发信(同一槽每档都会被回源与补扫各触发一次)。
	if !s.merchantClaim(slotStart) {
		// 这里被挡通常是对的(刚发过,10 分钟内不再发)。但**换源**时容易踩到:
		// merchantSetSource 会立刻异步回源当前轮并触发通知,若此前 10 分钟内已经发过
		// 这一轮,切源那次就被静默挡掉 —— 表现同样是「切了源却没邮件」。故记下来。
		log.Printf("merchantNotify 本槽在冷却期内已发过,跳过 slot=%s(待发 %d 人)",
			slotStart.Format("01-02 15:04"), len(pend))
		return
	}

	// ---------------------------------------------------------------- 第一趟:构造
	//
	// 先把所有人的正文都构造完再发信,**而不是边构造边发**。构造要读本地图片文件
	// (毫秒级但非零),而 SMTP 会话一旦建好就该连续发完 —— 让一条已认证的连接在
	// 构造正文时空转,容易被服务端判空闲踢掉,那样复用反而更糟。
	//
	// 代价是这批正文与内嵌图要同时在内存里:订阅者是十来人的量级、图也就几张,
	// 完全可接受;真到了几百人再改回边构造边发也不迟。
	notifyStart := time.Now()
	buildCosts := make([]time.Duration, len(pend))
	mails := make([]merchantMail, len(pend))
	for i, p := range pend {
		// merchant_name 第三方已含完整显示名(如「远行商人「云上仙岛」」),这里直接
		// 使用不再硬编码前缀,避免出现「远行商人 远行商人 上架了新商品」的重复。
		name := out.Data.MerchantName
		if name == "" {
			name = "远行商人"
		}
		t0 := time.Now()
		// fetchedAt 是这批货单的回源时刻(槽缓存里的最后一次成功回源)。探测模式下
		// 它就是要测的东西:与档期整点一减,即是第三方的滞后。邮件里显式写出,
		// 便于人工核对(探测日志另有逐次的时间线,两者互为印证)。
		content := merchantMailContent(name,
			merchantDayStart(slotStart).Format("2006-01-02"),
			slotStart.Format("15:04")+" ~ "+slotStart.Add(merchantSlotStep).Format("15:04"),
			slotStart, time.Unix(fetchedAt, 0),
			p.news)
		buildCosts[i] = time.Since(t0)
		// 退订签名只在 HTML 模板尾部保留一份(见 merchantMailHTMLTpl),正文不再重复。
		mails[i] = merchantMail{
			to:      p.email,
			subject: "远行商人新货上架(" + slotStart.Format("15:04") + " 轮)",
			content: content,
		}
	}

	// ---------------------------------------------------------------- 第二趟:发信
	//
	// 整批共用一个 SMTP 会话(见 smtpSender.sendBatch):握手与认证这类固定开销
	// 只付一次,而不是每人一遍。返回的 errs 与 mails 一一对应 —— 一个人的失败
	// 不影响其他人,也不影响自己之后的重试。
	errs := s.smtp.sendMerchantMailBatch(mails)
	sent, failed := 0, 0
	for i, err := range errs {
		p := pend[i]
		if err == nil {
			sent++
			names := make([]string, 0, len(p.news))
			for _, it := range p.news {
				names = append(names, it.Name)
			}
			// 记录的是「本批商品已通知」而非「本槽已通知」:下次重查带着新货再来时,
			// 只有新货会再发一封,老商品不会重复打扰。
			if err := s.store.MarkMerchantNotified(slotStart.Unix(), p.email, names); err != nil {
				log.Printf("merchantNotify 记录已通知商品失败 slot=%s to=%s: %v", slotStart.Format("15:04"), p.email, err)
			}
		} else {
			failed++
			// 发信失败不 Mark:补扫(merchantResend)或下次触发仍会重试。
			// 之前这里是静默吞错,查无可查(邮件没到 = 授权码过期/被限流/网络瞬断都无痕迹)。
			log.Printf("merchantNotify 发信失败 slot=%s to=%s: %v", slotStart.Format("15:04"), p.email, err)
		}
		// 逐人一行:订阅者通常不多,而这行是判断「慢在个别人还是普遍现象」的依据
		// (个别人慢 = 他那封的图多/网络抖动;普遍慢 = 建会话或邮件体积的问题)。
		// 「发信」那一列没了 —— 整批只有一次网络往返,逐人的数字不再有意义,
		// 耗时一律看下面那行汇总(以及 smtp 批量发信那行)。
		log.Printf("merchantNotify 发信 #%d/%d slot=%s to=%s 构造=%.2fs 商品=%d %s",
			i+1, len(pend), slotStart.Format("15:04"), p.email,
			buildCosts[i].Seconds(), len(p.news),
			map[bool]string{true: "失败", false: "成功"}[err != nil])
	}
	log.Printf("merchantNotify 完成 slot=%s 待发=%d 成功=%d 失败=%d 总耗时=%.2fs",
		slotStart.Format("15:04"), len(pend), sent, failed, time.Since(notifyStart).Seconds())
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
