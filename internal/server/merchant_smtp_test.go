package server

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// 本文件守住「一批订阅者共用一个 SMTP 会话」。
//
// 存在理由:复用连接省的是握手 + 认证这类**固定开销**,而这一点在功能上完全不可见
// —— 每人建一条连接照样发得出去、内容一字不差、既有测试全绿。故必须有直接断言
// 「建了几次会话」的测试:否则哪天有人把 sendBatch 改回逐封 dial,除了变慢之外
// 没有任何信号(正是那种永远绿灯的假安全感)。
//
// 真连 QQ SMTP 测不了这些,故 smtpSession 是接口、sendBatch 经 m.dialFn 建会话,
// 下面的假会话只记操作序列,一个字节也不发。

// fakeSession 是一条假会话,记录落在它上面的每一次操作。
type fakeSession struct {
	sends    []smtpMail // 按发出顺序
	resetN   int
	closed   bool
	discardN int

	// sendErr 返回本会话第 i 次 send 的错误(nil = 成功)。下标是**本会话内**的
	// 序号而非全批序号 —— 重连后新会话从第 0 封重新计数,才对得上真实语义。
	sendErr func(i int) error
	// resetErr 非 nil 时 reset 失败,模拟「连接已废,只能丢掉重连」。
	resetErr error
}

func (f *fakeSession) send(m *smtpSender, msg smtpMail) (int, error) {
	f.sends = append(f.sends, msg)
	if f.sendErr != nil {
		if err := f.sendErr(len(f.sends) - 1); err != nil {
			return 0, err
		}
	}
	return len(msg.html), nil
}

// TestSMTPMailImageIsExternalURL 商品图必须是 http(s) 直链,并按原样输出。
//
// 这是本次删除的**回归闸门**:原先还有一条「本地相对路径 → 读 embed 的 webp →
// 内嵌成 CID 附件」的分支,已确认是死代码 —— 两个数据源的图都是
// patchwiki.biligame.com 的 https 直链(2026-09-03 用咸鱼源真实响应核对过)。
// 若哪天第三方改回相对路径,邮件里会静默没图(不报错、发信照成功),
// 故这里把「非直链就不显示」钉住,免得将来有人放宽而失去这个信号。
func TestSMTPMailImageIsExternalURL(t *testing.T) {
	const url = "https://patchwiki.biligame.com/images/rocom/thumb/c/c5/abc.png/100px-1.png"
	content := merchantMailContent("远行商人「云上仙岛」", "2026-08-30", "08:00 ~ 12:00",
		time.Time{}, time.Time{},
		[]merchantItem{{Name: "蓝晶碧玺", Kind: "prop", Price: 1000, Limit: 100,
			TimeLabel: "08:00-12:00", Image: url}})

	if !strings.Contains(content, `src="`+url+`"`) {
		t.Errorf("外链图未原样输出:\n%s", content)
	}
	// 正文里不该再有任何内嵌痕迹 —— 附件机制已经删掉了。
	if strings.Contains(content, "cid:") {
		t.Errorf("正文仍含 cid: 引用(附件已删,只会是裂图):\n%s", content)
	}
	// 也不该有本站路径:那正是被删掉的死分支产物,收件端访问不到。
	if strings.Contains(content, "/img/") {
		t.Errorf("正文出现本站路径(收件端不可达):\n%s", content)
	}
}

// TestSMTPMailMessageNoAttachment 邮件原文是单段 text/html,不再有附件。
//
// 与上面一条互补:上面看正文内容,这里看 MIME 结构 —— 内嵌那套若被悄悄加回来,
// 这里会先红(multipart / base64 都会重新出现)。
func TestSMTPMailMessageNoAttachment(t *testing.T) {
	content := merchantMailContent("远行商人「云上仙岛」", "2026-08-30", "08:00 ~ 12:00",
		time.Time{}, time.Time{},
		[]merchantItem{{Name: "蓝晶碧玺", Kind: "prop", Price: 1000, Limit: 100,
			TimeLabel: "08:00-12:00",
			Image:     "https://patchwiki.biligame.com/images/rocom/thumb/c/c5/abc.png/100px-1.png"}})
	msg := merchantMailMessage("远哥来了 <a@qq.com>", "b@qq.com", "新货上架", content)

	for _, bad := range []string{"multipart/related", "base64", "cid:", "boundary"} {
		if strings.Contains(msg, bad) {
			t.Errorf("邮件原文含 %q —— 内嵌附件已删除,不该再出现:\n%s", bad, msg)
		}
	}
	if !strings.Contains(msg, "Content-Type: text/html; charset=UTF-8") {
		t.Error("邮件不是单段 text/html")
	}
}

// TestSMTPMailExternalImgSrcEscaped 第三方外链图的 URL 必须转义后再进 src 属性。
//
// 不是洁癖:好游快爆源的图地址是**从 HTML 页面里刮出来的**,里面带个双引号就能
// 截断 src 属性、往邮件正文里塞任意标签。邮件客户端的 HTML 沙箱各异,不该把
// 这份输入当成可信内容。
func TestSMTPMailExternalImgSrcEscaped(t *testing.T) {
	const evil = `https://x.example.com/a"onerror="alert(1)`
	content := merchantMailContent("远行商人", "2026-08-30", "08:00 ~ 12:00",
		time.Time{}, time.Time{},
		[]merchantItem{{Name: "残缺魔镜", Image: evil}})

	if strings.Contains(content, `<img src="`+evil+`"`) {
		t.Error("src 未转义:双引号会截断属性")
	}
	if !strings.Contains(content, "&#34;") {
		t.Errorf("src 里的双引号未转成 &#34;:\n%s", content)
	}
}

// TestSMTPMailNonURLImageSkipped 非 http(s) 的 image 值一律不显示,
// 商品行本身照常输出 —— 缺图不该让整行消失。
//
// 这类取值现在是「第三方改版」的信号(见 merchantMailItemImg 的注释),
// 不显示是刻意的:拼成一个收件端访问不到的链接只会是裂图。
func TestSMTPMailNonURLImageSkipped(t *testing.T) {
	content := merchantMailContent("远行商人「云上仙岛」", "2026-08-30", "08:00 ~ 12:00",
		time.Time{}, time.Time{},
		[]merchantItem{
			{Name: "残缺魔镜", Kind: "prop", Price: 120, Limit: 2,
				TimeLabel: "08:00-12:00", Image: "HeadIcon/3001.webp"}, // 相对路径
			{Name: "适格钥匙", Kind: "prop", Price: 60, Limit: 1,
				TimeLabel: "08:00-12:00"}, // 无图
		})
	if strings.Contains(content, "<img") {
		t.Errorf("非直链图却输出了图片标签:\n%s", content)
	}
	// 两种「没图」都不能让商品行消失 —— 没图只是没图,商品信息才是主体。
	for _, want := range []string{"残缺魔镜", "适格钥匙"} {
		if !strings.Contains(content, want) {
			t.Errorf("图缺失时商品行不该消失, 缺 %q:\n%s", want, content)
		}
	}
}

func (f *fakeSession) reset() error {
	f.resetN++
	return f.resetErr
}

func (f *fakeSession) close() { f.closed = true }

func (f *fakeSession) discard() { f.discardN++ }

// newFakeDialer 给 m 装上建会话的假实现,返回「已成功建起的会话」列表的取值函数。
//
// failDialAt:第几次 dial 应失败(1 起,0 = 从不失败);失败的那次不计入列表。
// setup:新会话建好时回调,用来安排失败点(会话是在 sendBatch 内部按需建的,
// 事先拿不到它,只能在建的那一刻配置)。
func newFakeDialer(m *smtpSender, failDialAt int, setup func(*fakeSession)) func() []*fakeSession {
	var mu sync.Mutex
	var made []*fakeSession
	n := 0
	m.dialFn = func() (smtpSession, error) {
		mu.Lock()
		defer mu.Unlock()
		n++
		if failDialAt > 0 && n == failDialAt {
			return nil, errors.New("模拟拨号失败")
		}
		f := &fakeSession{}
		if setup != nil {
			setup(f)
		}
		made = append(made, f)
		return f, nil
	}
	return func() []*fakeSession {
		mu.Lock()
		defer mu.Unlock()
		return append([]*fakeSession(nil), made...)
	}
}

// threeMails 三封内容各不相同的邮件 —— 内容必须不同,否则「合并发送把正文串位」
// 这类错误测不出来(三封长得一样的话,怎么发都对)。
func threeMails() []smtpMail {
	return []smtpMail{
		{to: "a@qq.com", subject: "s", html: "A"},
		{to: "b@qq.com", subject: "s", html: "B"},
		{to: "c@qq.com", subject: "s", html: "C"},
	}
}

// TestSMTPBatchReusesOneSession 三条邮件 = 一条会话。这是复用改造的核心断言。
func TestSMTPBatchReusesOneSession(t *testing.T) {
	m := newSMTPSender("from@qq.com", "pass")
	sessions := newFakeDialer(m, 0, nil)

	errs := m.sendBatch(threeMails())
	for i, err := range errs {
		if err != nil {
			t.Fatalf("第 %d 封失败: %v", i+1, err)
		}
	}
	got := sessions()
	if len(got) != 1 {
		t.Fatalf("建会话 %d 次, 期望 1 —— 复用连接是本改造的全部意义", len(got))
	}
	if len(got[0].sends) != 3 {
		t.Fatalf("会话上发了 %d 封, 期望 3", len(got[0].sends))
	}
	// 每人拿到的是**自己**的正文:这条保证「一个会话连发多封」没有把内容串位。
	if got[0].sends[1].to != "b@qq.com" || got[0].sends[1].html != "B" {
		t.Errorf("第 2 封串位: to=%q html=%q", got[0].sends[1].to, got[0].sends[1].html)
	}
	if !got[0].closed {
		t.Error("批结束后会话未关闭 —— 连接泄漏")
	}
	if got[0].discardN != 0 {
		t.Error("正常结束却走了 discard(应为 close)—— QUIT 没发出去")
	}
}

// TestSMTPBatchResetAfterFailure 中间一封失败但 RSET 成功:同一条会话继续发完,
// 只有那一封报错 —— 一个人撞上 550 不能连累其他人收不到。
func TestSMTPBatchResetAfterFailure(t *testing.T) {
	m := newSMTPSender("from@qq.com", "pass")
	sessions := newFakeDialer(m, 0, func(f *fakeSession) {
		f.sendErr = func(i int) error {
			if i == 0 {
				return errors.New("550 被限流")
			}
			return nil
		}
	})

	errs := m.sendBatch(threeMails())

	if errs[0] == nil {
		t.Error("第 1 封应失败")
	}
	for i := 1; i < len(errs); i++ {
		if errs[i] != nil {
			t.Errorf("第 %d 封被前一封的失败连累: %v", i+1, errs[i])
		}
	}
	got := sessions()
	if len(got) != 1 {
		t.Fatalf("建会话 %d 次, 期望 1(RSET 成功后应继续复用)", len(got))
	}
	if got[0].resetN != 1 {
		t.Errorf("RSET %d 次, 期望 1 —— 不复位的话下一封会从半截事务开始", got[0].resetN)
	}
	if len(got[0].sends) != 3 {
		t.Errorf("会话上发了 %d 封, 期望 3(失败的那一封也算一次尝试)", len(got[0].sends))
	}
}

// TestSMTPBatchReconnectsWhenResetFails RSET 也失败 = 连接已废:丢弃重连,
// 余下的照发。这是「连接断了之后还能不能发完」的兜底。
func TestSMTPBatchReconnectsWhenResetFails(t *testing.T) {
	m := newSMTPSender("from@qq.com", "pass")
	// 只让**第一条**会话坏掉:它发第一封时断线且 RSET 不响应,重连后的新会话
	// 必须一切正常。若 setup 对每条会话都生效,重连后还会再断一次,测的就成了
	// 「连续重连」而不是「断线后能接着发完」。
	first := true
	sessions := newFakeDialer(m, 0, func(f *fakeSession) {
		if !first {
			return
		}
		first = false
		f.sendErr = func(i int) error { return errors.New("写正文: 连接被重置") }
		f.resetErr = errors.New("RSET 无响应")
	})

	errs := m.sendBatch(threeMails())

	if errs[0] == nil {
		t.Error("第 1 封应失败")
	}
	for i := 1; i < len(errs); i++ {
		if errs[i] != nil {
			t.Errorf("第 %d 封在重连后仍失败: %v", i+1, errs[i])
		}
	}
	got := sessions()
	if len(got) != 2 {
		t.Fatalf("建会话 %d 次, 期望 2(连接判废后重连一次)", len(got))
	}
	if got[0].discardN != 1 {
		t.Error("废连接未 discard —— 泄漏")
	}
	if got[0].closed {
		t.Error("废连接走了 close(会发 QUIT),应走 discard")
	}
	if len(got[1].sends) != 2 {
		t.Errorf("新会话上发了 %d 封, 期望 2(余下两封)", len(got[1].sends))
	}
	if !got[1].closed {
		t.Error("新会话未关闭 —— 连接泄漏")
	}
}

// TestSMTPBatchDialFailureMarksRest 重连时拨号也失败:余下各封都要带上错误,
// 不能悄悄吞掉 —— 吞掉就等于「明明没发出去,上层却以为成功并 Mark 已通知」。
func TestSMTPBatchDialFailureMarksRest(t *testing.T) {
	m := newSMTPSender("from@qq.com", "pass")
	sessions := newFakeDialer(m, 2, func(f *fakeSession) {
		f.sendErr = func(i int) error { return errors.New("550 被限流") }
		f.resetErr = errors.New("RSET 无响应")
	})

	errs := m.sendBatch(threeMails())

	for i, err := range errs {
		if err == nil {
			t.Errorf("第 %d 封不该成功(1 封撞 550、后 2 封连不上)", i+1)
		}
	}
	got := sessions()
	if len(got) != 1 {
		t.Errorf("成功建会话 %d 次, 期望 1(第 2 次拨号失败,不计入)", len(got))
	}
}

// TestSMTPBatchEmpty 空批次不建连接、不 panic。
func TestSMTPBatchEmpty(t *testing.T) {
	m := newSMTPSender("from@qq.com", "pass")
	sessions := newFakeDialer(m, 0, nil)

	if errs := m.sendBatch(nil); len(errs) != 0 {
		t.Errorf("空批次返回 %d 个错误, 期望 0", len(errs))
	}
	if got := sessions(); len(got) != 0 {
		t.Errorf("空批次却建了 %d 条会话", len(got))
	}
}

// TestMerchantNotifyOneSessionForAll 端到端:merchantNotify 给两个订阅者发信,
// 全程只建一条会话。
//
// 与上面几条不重复:**上面测的是 sendBatch 本身,这条测的是调用点真的走了批量**。
// 若 merchantNotify 改回在循环里逐人调用 sendMerchantMailHTML,上面几条全绿,
// 而复用在这里已经失效 —— 只有这条会红。
func TestMerchantNotifyOneSessionForAll(t *testing.T) {
	s := newTestServer(t)
	slot := seedMerchantNotify(t, s, "") // 空关键词 = 订阅全部
	if err := s.store.UpsertMerchantSub("UID:2", "other@qq.com", ""); err != nil {
		t.Fatalf("写第二个订阅: %v", err)
	}
	s.smtp = newSMTPSender("from@qq.com", "pass")
	sessions := newFakeDialer(s.smtp, 0, nil)

	s.merchantNotify(slot)

	got := sessions()
	if len(got) != 1 {
		t.Fatalf("两个订阅者共用了 %d 条会话, 期望 1(整批复用的落点就在 merchantNotify)", len(got))
	}
	if len(got[0].sends) != 2 {
		t.Fatalf("会话上发了 %d 封, 期望 2", len(got[0].sends))
	}
	tos := map[string]bool{}
	for _, msg := range got[0].sends {
		tos[msg.to] = true
	}
	for _, want := range []string{"player@qq.com", "other@qq.com"} {
		if !tos[want] {
			t.Errorf("会话上没发给 %s: %v", want, tos)
		}
	}
}
