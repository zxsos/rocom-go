package server

import (
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"net/smtp"
	"strings"
	"time"
)

// sendMerchantMail 通过 QQ 邮箱 SMTP(465 SSL)发送纯文本正文邮件(订阅验证 / 管理员测试),
// 内部转成模板 HTML。新货提醒的排版走 sendMerchantMailHTML(直接发结构化 HTML)。
func (m *smtpSender) sendMerchantMail(to, subject, body string) error {
	var imgs []merchantMailImg
	html := merchantMailBody(body, &imgs)
	return m.smtpSendMail(to, subject, html, imgs)
}

// sendMerchantMailHTML 发送已构造好的内容区 HTML(含 CID 内嵌图),merchantNotify 新货提醒用。
// htmlBody 是嵌入 merchantMailHTMLTpl 的 %s 内容区(分组卡片 + 商品行)。
func (m *smtpSender) sendMerchantMailHTML(to, subject, htmlBody string, imgs []merchantMailImg) error {
	return m.smtpSendMail(to, subject, strings.Replace(merchantMailHTMLTpl, "%s", htmlBody, 1), imgs)
}

// smtpSendMail 底层 SMTP 发送:正文以 multipart/related + CID 内嵌(HTML 引用 cid:,
// 附件 base64 每 76 字符分行),避免单行超过 RFC 5321 的 998 字节限制导致 500 拒收。
// 串行发信(m.mu),避免并发连接被 QQ 邮箱判为异常触发限流。
// 整体 deadline 兜底:QQ SMTP 偶发挂连接(网络波动/被限流),TLS 拨号限 10s,
// 连接建立后全程 I/O 限 20s,避免调用方(管理页强制刷新等)无限等待。
func (m *smtpSender) smtpSendMail(to, subject, html string, imgs []merchantMailImg) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sendFn != nil { // 测试注入,不真发信
		return m.sendFn(to, subject, html, imgs)
	}

	// 分阶段计时:发一封慢的时候,只有拆开看才知道卡在握手、认证还是传数据
	// (这三者的优化手段完全不同 —— 复用连接 / 换授权码 / 压缩正文)。
	var tDial, tAuth, tData, tQuit time.Duration
	start := time.Now()

	dialer := &net.Dialer{Timeout: 10 * time.Second}
	t0 := time.Now()
	conn, err := tls.DialWithDialer(dialer, "tcp", merchantSmtpHost+":465", &tls.Config{ServerName: merchantSmtpHost})
	tDial = time.Since(t0)
	if err != nil {
		log.Printf("smtp 拨号失败 to=%s 耗时=%.2fs: %v", to, tDial.Seconds(), err)
		return err
	}
	conn.SetDeadline(time.Now().Add(20 * time.Second))
	c, err := smtp.NewClient(conn, merchantSmtpHost)
	if err != nil {
		conn.Close()
		log.Printf("smtp 建客户端失败 to=%s 拨号已耗=%.2fs: %v", to, tDial.Seconds(), err)
		return err
	}
	defer c.Close()

	t0 = time.Now()
	if err := c.Auth(smtp.PlainAuth("", m.user, m.pass, merchantSmtpHost)); err != nil {
		tAuth = time.Since(t0)
		log.Printf("smtp 认证失败 to=%s 拨号=%.2fs 认证=%.2fs: %v(授权码过期或被限流?)",
			to, tDial.Seconds(), tAuth.Seconds(), err)
		return err
	}
	tAuth = time.Since(t0)

	// 数据段:MAIL FROM / RCPT TO / DATA 与正文写入都算进去 —— 邮件体积大时,
	// 这一段会随内嵌图片的张数与体积线性增长。
	var msgsize int
	t0 = time.Now()
	dataErr := func() error {
		if err := c.Mail(m.user); err != nil {
			return err
		}
		if err := c.Rcpt(to); err != nil {
			return err
		}
		w, err := c.Data()
		if err != nil {
			return err
		}
		msg := merchantMailMessage(m.from(), to, mime.QEncoding.Encode("utf-8", subject), html, imgs)
		if _, err := io.WriteString(w, msg); err != nil {
			return err
		}
		if err := w.Close(); err != nil {
			return err
		}
		msgsize = len(msg) // 供日志输出(下面统一打)
		return nil
	}()
	tData = time.Since(t0)

	t0 = time.Now()
	quitErr := c.Quit()
	tQuit = time.Since(t0)

	total := time.Since(start)
	if dataErr != nil {
		log.Printf("smtp 发送数据失败 to=%s 拨号=%.2fs 认证=%.2fs 数据=%.2fs 总计=%.2fs: %v",
			to, tDial.Seconds(), tAuth.Seconds(), tData.Seconds(), total.Seconds(), dataErr)
		return dataErr
	}
	if quitErr != nil {
		// Quit 失败通常无害(邮件已投递),故只记不返回错误。
		log.Printf("smtp QUIT 异常(邮件应已投递) to=%s 退出=%.2fs: %v", to, tQuit.Seconds(), quitErr)
	}
	// 慢的时候才给明细:正常发信不刷屏,但凡超过 1 秒就把各阶段摊开,便于定位。
	if total >= time.Second {
		log.Printf("smtp 发信偏慢 to=%s 拨号=%.2fs 认证=%.2fs 数据=%.2fs 退出=%.2fs 总计=%.2fs 体积=%dKB 内嵌图=%d张",
			to, tDial.Seconds(), tAuth.Seconds(), tData.Seconds(), tQuit.Seconds(), total.Seconds(), msgsize/1024, len(imgs))
	}
	return nil
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
