package server

import (
	"os"
	"strings"
	"testing"
	"time"
)

// TestMerchantSMTPLive 真实发信冒烟测试:验证 QQ 邮箱 SMTP 配置(授权码)是否可用。
//
// 默认 skip —— 它会真的发出一封邮件,不能混进日常 go test ./...(否则每次跑测试
// 都给收件人发垃圾信,还容易被 QQ 判异常限流)。显式开启:
//
//	set -a; source .env.local; set +a
//	go test ./internal/server/ -run TestMerchantSMTPLive -v -count=1
//
// 环境变量:ROCOM_SMTP_LIVE=1(开关)、ROCOM_SMTP_USER、ROCOM_SMTP_PASS、
// ROCOM_SMTP_TO(可选,缺省=自发自收,够验证链路)。
//
// 断言的是「真发成功 + 正文合规」,不只是 err == nil:
// SMTP 有个坑是服务端收下却不投递(仅在回信里报错),这里抓不到,
// 故失败时看日志里的分阶段耗时(拨号/认证/数据)定位,别只看这一行红。
func TestMerchantSMTPLive(t *testing.T) {
	if os.Getenv("ROCOM_SMTP_LIVE") != "1" {
		t.Skip("未开启:需 ROCOM_SMTP_LIVE=1(会真发邮件);见 .env.local")
	}
	user := os.Getenv("ROCOM_SMTP_USER")
	pass := os.Getenv("ROCOM_SMTP_PASS")
	if user == "" || pass == "" {
		t.Fatal("缺 ROCOM_SMTP_USER / ROCOM_SMTP_PASS")
	}
	to := os.Getenv("ROCOM_SMTP_TO")
	if to == "" {
		to = user
	}

	sender := newSMTPSender(user, pass)
	if !sender.configured() {
		t.Fatal("smtpSender.configured() 为 false,账号或授权码为空")
	}

	body := "这是一封 SMTP 冒烟测试邮件,用于验证发信链路与授权码是否有效。\n\n" +
		"- 发送时间:" + time.Now().Format("2006-01-02 15:04:05") + "\n" +
		"- 收件:" + to + "\n\n——rocom-capture merchant_smtp_live_test"

	start := time.Now()
	err := sender.sendMerchantMail(to, "【测试】rocom SMTP 冒烟", body)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("发信失败(耗时 %.2fs): %v\n"+
			"排障:拨号失败=出网/465 被封;认证失败=授权码过期或没开 SMTP 服务;数据失败=正文超限。",
			elapsed.Seconds(), err)
	}
	t.Logf("发信成功 to=%s 耗时=%.2fs", to, elapsed.Seconds())

	// 正文合规:交给 SMTP 的 HTML 不能含字面 \r\n(与 TestMerchantMailDeliveredNoLiteralCRLF 同一条不变量,
	// 这里额外覆盖真实发信路径,因为注入 sendFn 的测试不会走到 merchantMailMessage 组装)。
	html := merchantMailBody(body)
	msg := merchantMailMessage(sender.from(), to, "【测试】rocom SMTP 冒烟", html)
	if strings.Contains(msg, `\r\n`) {
		t.Error("邮件原文含字面 \\r\\n")
	}
	for _, line := range strings.Split(msg, "\r\n") {
		if len(line) > 998 {
			t.Errorf("单行 %d 字节,超过 RFC 5321 的 998 上限", len(line))
		}
	}
}
