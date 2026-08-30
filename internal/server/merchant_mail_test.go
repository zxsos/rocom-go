package server

import (
	"strings"
	"testing"
)

// 本文件守住「邮件正文不出现字面 \r\n」。
//
// 存在理由:Go 的 raw string(反引号)不做转义,写在里面的 \r\n 是四个**字面字符**
// 而不是 CRLF。merchantMailGroup 的分组标题行正是这么写的 —— 于是每封邮件的每个
// 分组标题后面都跟着一串收件端可见的「\r\n」文本。同文件其余行都写成
// `...` + "\r\n" 拼接(双引号里的才是真 CRLF),故只有这一处中招。
//
// 断行本身是必要的(RFC 5321 单行 998 字节上限),不能为了消掉字面文本就删断行,
// 故下面两个方向都断言:不许有字面 \r\n,但必须有真实 CRLF。

// mailTestItems 覆盖三个分组:全天售卖 / 标准时段 / 其他时段。
// 三者都要覆盖到,否则断言可能压根没跑到出问题的那一行。
var mailTestItems = []merchantItem{
	{Name: "残缺魔镜", Kind: "prop", Price: 120, Limit: 2, TimeLabel: "08:00-12:00 / 12:00-16:00 / 16:00-20:00 / 20:00-24:00"},
	{Name: "适格钥匙", Kind: "prop", Price: 60, Limit: 1, TimeLabel: "08:00-12:00"},
	{Name: "淘沙球", Kind: "prop", Price: 30, Limit: 5, TimeLabel: "09:30-11:30"},
}

// TestMerchantMailContentNoLiteralCRLF 内容区 HTML:分组标题后不能有字面 \r\n。
func TestMerchantMailContentNoLiteralCRLF(t *testing.T) {
	var imgs []merchantMailImg
	body := merchantMailContent("远行商人「云上仙岛」", "2026-08-30", "08:00 ~ 12:00", mailTestItems, &imgs)

	for _, want := range []string{"全天售卖", "08:00-12:00", "其他时段"} {
		if !strings.Contains(body, want) {
			t.Errorf("正文缺少分组标题 %q", want)
		}
	}
	if strings.Contains(body, `\r\n`) { // 反引号内是字面四个字符,与真 CRLF 不冲突
		t.Errorf("正文含字面 \\r\\n:\n%s", body)
	}
	if !strings.Contains(body, "\r\n") {
		t.Error("正文缺少真实 CRLF 断行,单行可能突破 RFC 5321 的 998 字节上限")
	}
}

// TestMerchantMailDeliveredNoLiteralCRLF 端到端:两条发信路径最终交给 SMTP 的完整
// HTML 都不含字面 \r\n(内容区 + 模板拼接后的产物,即收件端真正看到的东西)。
func TestMerchantMailDeliveredNoLiteralCRLF(t *testing.T) {
	s := newTestServer(t)
	for _, tc := range []struct {
		name string
		send func() string
	}{
		{"新货提醒", func() string {
			var imgs []merchantMailImg
			content := merchantMailContent("远行商人「云上仙岛」", "2026-08-30", "08:00 ~ 12:00", mailTestItems, &imgs)
			var got string
			s.smtp = newSMTPSender("from@qq.com", "pass")
			s.smtp.sendFn = func(to, subject, html string, imgs []merchantMailImg) error {
				got = html
				return nil
			}
			if err := s.smtp.sendMerchantMailHTML("player@qq.com", "远行商人新货上架(08:00 轮)", content, imgs); err != nil {
				t.Fatalf("发信: %v", err)
			}
			return got
		}},
		{"订阅验证", func() string {
			var got string
			s.smtp = newSMTPSender("from@qq.com", "pass")
			s.smtp.sendFn = func(to, subject, html string, imgs []merchantMailImg) error {
				got = html
				return nil
			}
			if err := s.smtp.sendMerchantMail("player@qq.com", "【远哥来了】订阅成功验证",
				"你已成功订阅「远行商人」新货提醒!\n\n——远哥来了"); err != nil {
				t.Fatalf("发信: %v", err)
			}
			return got
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			html := tc.send()
			if strings.Contains(html, `\r\n`) {
				t.Errorf("最终 HTML 含字面 \\r\\n:\n%s", html)
			}
		})
	}
}
