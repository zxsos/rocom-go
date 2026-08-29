package server

import (
	"encoding/json"
	"log"
	"strings"
	"time"
)

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
	if !s.smtp.configured() {
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
		if err := s.smtp.sendMerchantMailHTML(sub.Email, subject, content, imgs); err == nil {
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
