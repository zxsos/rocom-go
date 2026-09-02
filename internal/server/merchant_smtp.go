package server

import (
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"net/smtp"
	"strings"
	"time"
)

const (
	// smtpDialTimeout 是 TCP + TLS 握手的超时。与邮件体积无关,故单独设且比数据段
	// 短 —— 握手卡住几乎总是出网或 465 被封,重试也没用,早点放弃更好。
	smtpDialTimeout = 10 * time.Second
	// smtpGreetTimeout 覆盖服务器问候与 AUTH,同样是固定开销。
	smtpGreetTimeout = 15 * time.Second
	// smtpMailTimeout 是**单封**邮件的 I/O 窗口(MAIL FROM → DATA 写完)。
	//
	// 复用连接后必须每封刷新:SetDeadline 设的是**绝对时刻**而非空闲超时,只在建
	// 连接时设一次的话,批里第 N 封剩下的窗口 = 20s − 前 N−1 封已耗时间 —— 人多时
	// 后面的必然撞 deadline,表现为「只有前几封发得出去」,且极容易被误读成限流。
	smtpMailTimeout = 20 * time.Second
)

// smtpMail 是一封待发邮件:html 是最终正文(模板已套)。
//
// 邮件正文是单段 text/html:商品图一律是第三方 https 直链,不内嵌(见
// merchantMailItemImg),故没有附件、正文里也不该出现 cid: 引用。
type smtpMail struct {
	to      string
	subject string
	html    string
}

// merchantMail 是一封待发的新货提醒。content 是内容区 HTML,发出前统一套模板 ——
// 与单封路径共用 merchantMailHTMLTpl,免得两处排版漂移。
type merchantMail struct {
	to      string
	subject string
	content string
}

// smtpSession 是一条已拨号并认证的 SMTP 连接,可连发多封。
//
// 定义成接口只为**可测**:真连 QQ SMTP 无法在单测里断言「这一批只建了一条连接」
// 与「某封失败后连接是复位还是重连」,而那正是复用改造的全部风险所在。生产实现
// 只有 realSession 一个,不因这个接口多出任何一层。
type smtpSession interface {
	// send 发一封,返回正文字节数。
	send(m *smtpSender, msg smtpMail) (int, error)
	// reset 中止当前事务让连接能接着发下一封;返回错误表示连接已不可用。
	reset() error
	// close 正常收尾:QUIT 再关连接。
	close()
	// discard 连接已废:直接关,**不发 QUIT** —— QUIT 必然跟着失败,还会打一条
	// 「邮件应已投递」的误导日志(那封恰恰没投递)。
	discard()
}

// realSession 是 smtpSession 的生产实现:一条到 QQ SMTP(465 SSL)的 TLS 连接。
type realSession struct {
	conn net.Conn
	c    *smtp.Client
}

// dial 建立一条可连发多封的会话。调用方负责 close / discard;出错时本函数自行收尾。
//
// 握手与认证是**固定开销**,与邮件体积无关 —— 这也是复用连接的全部理由:
// 一批人只付一次,而不是每人一遍(见 sendBatch)。
func (m *smtpSender) dial() (smtpSession, error) {
	dialer := &net.Dialer{Timeout: smtpDialTimeout}
	t0 := time.Now()
	conn, err := tls.DialWithDialer(dialer, "tcp", merchantSmtpHost+":465",
		&tls.Config{ServerName: merchantSmtpHost})
	tDial := time.Since(t0)
	if err != nil {
		log.Printf("smtp 拨号失败 耗时=%.2fs: %v", tDial.Seconds(), err)
		return nil, fmt.Errorf("拨号: %w", err)
	}
	// 问候与 AUTH 也要兜底:QQ SMTP 偶发挂连接,不设 deadline 会一直等到 TCP 层超时。
	conn.SetDeadline(time.Now().Add(smtpGreetTimeout))
	t0 = time.Now()
	c, err := smtp.NewClient(conn, merchantSmtpHost)
	if err != nil {
		conn.Close()
		log.Printf("smtp 读服务问候失败 拨号已耗=%.2fs: %v", tDial.Seconds(), err)
		return nil, fmt.Errorf("读服务问候: %w", err)
	}
	if err := c.Auth(smtp.PlainAuth("", m.user, m.pass, merchantSmtpHost)); err != nil {
		c.Close()
		log.Printf("smtp 认证失败 拨号=%.2fs 认证=%.2fs: %v(授权码过期或被限流?)",
			tDial.Seconds(), time.Since(t0).Seconds(), err)
		return nil, fmt.Errorf("认证: %w", err)
	}
	if tAuth := time.Since(t0); tDial+tAuth >= time.Second {
		log.Printf("smtp 建会话偏慢 拨号=%.2fs 认证=%.2fs", tDial.Seconds(), tAuth.Seconds())
	}
	return &realSession{conn: conn, c: c}, nil
}

// send 用本会话发一封,返回正文字节数。deadline **每封刷新**(理由见 smtpMailTimeout)。
func (s *realSession) send(m *smtpSender, msg smtpMail) (int, error) {
	if err := s.conn.SetDeadline(time.Now().Add(smtpMailTimeout)); err != nil {
		return 0, fmt.Errorf("设 deadline: %w", err)
	}
	if err := s.c.Mail(m.user); err != nil {
		return 0, fmt.Errorf("MAIL FROM: %w", err)
	}
	if err := s.c.Rcpt(msg.to); err != nil {
		return 0, fmt.Errorf("RCPT TO: %w", err)
	}
	w, err := s.c.Data()
	if err != nil {
		return 0, fmt.Errorf("DATA: %w", err)
	}
	body := merchantMailMessage(m.from(), msg.to, mime.QEncoding.Encode("utf-8", msg.subject), msg.html)
	if _, err := io.WriteString(w, body); err != nil {
		// 正文写到一半断了:仍要 Close 收掉这次 DATA,服务端才回到命令态,
		// 否则后续的 RSET / 下一封都无从谈起(连接已被半截数据污染)。
		w.Close()
		return 0, fmt.Errorf("写正文: %w", err)
	}
	if err := w.Close(); err != nil {
		// 走到这里服务端已收下正文并给了最终应答(多半是拒收),事务已结束、
		// 连接仍可用,故不把连接判废 —— 下一封照发。
		return len(body), fmt.Errorf("服务端应答: %w", err)
	}
	return len(body), nil
}

func (s *realSession) reset() error { return s.c.Reset() }

// close 优雅退出。QUIT 失败只记不返 —— 走到这里该发的都已投递。
func (s *realSession) close() {
	if err := s.c.Quit(); err != nil {
		log.Printf("smtp QUIT 异常(邮件应已投递): %v", err)
	}
	s.c.Close()
}

func (s *realSession) discard() { s.c.Close() }

// sendBatch 用一个 SMTP 会话发完整批,返回与 msgs 一一对应的错误。
//
// 收益是**固定开销只付一次**(N 封 → 1 次握手 + 1 次认证);数据段仍要每人一份 ——
// 各封正文与内嵌图本就不同(见 merchantNotify 的按人去重),无从合并,想省那一半
// 只能按内容分组 + 密送,而密送要求内容逐字节相同,这里不成立。
//
// 并发连接数仍是 1(整批持有 m.mu),故不触碰「QQ 邮箱对并发连接敏感」那条顾虑。
//
// 失败互不影响:某封失败后 RSET 复位继续下一封;RSET 也失败说明连接已废,丢弃
// 重连再发剩下的 —— 一个人撞上 550 不能连累其他人收不到。连接级错误(拨号/问候/
// 认证)绕不过去,余下各封返回同一个错误,交给上层按「失败不 Mark」去重试。
func (m *smtpSender) sendBatch(msgs []smtpMail) []error {
	m.mu.Lock()
	defer m.mu.Unlock()
	errs := make([]error, len(msgs))
	if len(msgs) == 0 {
		return errs
	}
	if m.sendFn != nil { // 测试注入,不真发信(见 merchant_notify_test.go)
		for i, msg := range msgs {
			errs[i] = m.sendFn(msg.to, msg.subject, msg.html)
		}
		return errs
	}

	start := time.Now()
	var sess smtpSession
	dials, sent, size := 0, 0, 0
	for i := range msgs {
		if sess == nil {
			var err error
			dials++
			if sess, err = m.dialSession(); err != nil {
				log.Printf("smtp 建会话失败(余下 %d 封都发不出): %v", len(msgs)-i, err)
				for ; i < len(msgs); i++ {
					errs[i] = err
				}
				break
			}
		}
		n, err := sess.send(m, msgs[i])
		size += n
		if err == nil {
			sent++
			continue
		}
		errs[i] = err
		log.Printf("smtp 发信失败 to=%s: %v", msgs[i].to, err)
		if rerr := sess.reset(); rerr != nil {
			log.Printf("smtp RSET 失败,丢弃连接(余下 %d 封将重连): %v", len(msgs)-i-1, rerr)
			sess.discard()
			sess = nil
		}
	}
	if sess != nil {
		sess.close()
	}
	if failed := countErrs(errs); failed > 0 || dials > 1 || time.Since(start) >= time.Second {
		log.Printf("smtp 批量发信 封数=%d 成功=%d 失败=%d 建会话=%d 次 总计=%.2fs 体积=%dKB",
			len(msgs), sent, failed, dials, time.Since(start).Seconds(), size/1024)
	}
	return errs
}

// dialSession 建会话:dialFn 非 nil 时走它(测试注入),否则真连 QQ SMTP。
func (m *smtpSender) dialSession() (smtpSession, error) {
	if m.dialFn != nil {
		return m.dialFn()
	}
	return m.dial()
}

// countErrs 数一下这批里失败了几封(用于汇总日志)。
func countErrs(errs []error) int {
	n := 0
	for _, e := range errs {
		if e != nil {
			n++
		}
	}
	return n
}

// sendMerchantMailBatch 用一个会话发完整批新货提醒(merchantNotify 用)。
func (m *smtpSender) sendMerchantMailBatch(msgs []merchantMail) []error {
	batch := make([]smtpMail, len(msgs))
	for i, msg := range msgs {
		batch[i] = smtpMail{to: msg.to, subject: msg.subject,
			html: strings.Replace(merchantMailHTMLTpl, "%s", msg.content, 1)}
	}
	return m.sendBatch(batch)
}

// sendMerchantMail 通过 QQ 邮箱 SMTP(465 SSL)发送纯文本正文邮件(订阅验证 / 管理员测试),
// 内部转成模板 HTML。新货提醒的排版走 sendMerchantMailHTML / sendMerchantMailBatch。
func (m *smtpSender) sendMerchantMail(to, subject, body string) error {
	html := merchantMailBody(body)
	return m.sendBatch([]smtpMail{{to: to, subject: subject, html: html}})[0]
}

// sendMerchantMailHTML 发送已构造好的内容区 HTML。单封路径;
// 批量走 sendMerchantMailBatch,两者共用 merchantMailHTMLTpl。
func (m *smtpSender) sendMerchantMailHTML(to, subject, htmlBody string) error {
	html := strings.Replace(merchantMailHTMLTpl, "%s", htmlBody, 1)
	return m.sendBatch([]smtpMail{{to: to, subject: subject, html: html}})[0]
}

// merchantMailMessage 组装完整邮件文本(头部 + 正文)。
//
// 只有一段 text/html:两个数据源的商品图都是 https 直链,邮件里不内嵌任何图片,
// 故没有附件、没有 multipart(见 merchantMailItemImg 的说明)。正文自身的断行
// 仍受 RFC 5321 单行 998 字节限制 —— 那是正文的问题,由 merchantMailWrap 与
// 分组输出处的 CRLF 保证,与 MIME 结构无关。
func merchantMailMessage(from, to, subject, html string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\n", from, to, subject)
	b.WriteString("Content-Type: text/html; charset=UTF-8\r\n\r\n")
	b.WriteString(html)
	return b.String()
}
