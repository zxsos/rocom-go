package server

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// 本文件守住「同一槽里,同一件商品对同一收件人只提醒一次」。
//
// 「同一槽」这个范围要盯紧:去重**不跨档**(merchant_notified 按 槽+邮箱+商品名 记)。
// 远行商人一天四档(8/12/16/20),同一批货会隔档重新上架 —— 那时**必须再提醒一次**,
// 见 TestMerchantNotifyRestockInLaterSlot;而同一槽内的多次回源只发没发过的那几件,
// 见 TestMerchantNotifyLateArrivingItems。两条合起来才是完整契约 —— 缺任何一条,
// 改动都会往错误的方向收敛(要么漏发、要么重复打扰),而另一条不会报错。
//
// 存在理由(两条,缺一不可):
//  1. merchant_notified 只在**发信成功之后**才 Mark(发信失败要留给 merchantResend 补扫
//     重试,见 merchant.go),而一次 SMTP 往返要几秒。于是两个触发源在同一 tick 撞到同一个槽时
//     (merchantEnsure 回源后异步发信 + merchantResend 补扫,8/12/16/20 每个整点档首次回源必撞),
//     后到者读到的必然是「未 Mark」,于是各发一封 —— 表现为订阅者收到两份一模一样的邮件。
//     该竞态只在**并发**下出现,顺序调用测不出来,故用 goroutine + 模拟 SMTP 延迟复现。
//  2. 去重粒度必须是**商品**而非整槽。第三方滞后补货,同一轮要回源多次(见 merchantShouldFetch),
//     每次都可能带来新商品;只按槽去重的话,轮次开始那次已发过信就把后续补上的商品永久挡住了
//     —— 2026-08-30 实测:20:0x 首查只有 4 件全天货,20:56 补上的 3 件专属货再没发出去。

// seedMerchantNotify 造一个有货的 8 点槽 + 一条订阅,返回槽开始时刻。
// 关键词留空=订阅全部;造两个商品以便区分「命中」与「未命中」。
func seedMerchantNotify(t *testing.T, s *Server, keywords string) time.Time {
	t.Helper()
	// 取当前营业日的 8 点槽:merchantNotify 不判营业状态,故测试在 0-8 点跑也成立。
	slot := merchantDaySlots(merchantDayStart(time.Now()))[0]
	const body = `{"code":0,"data":{"merchant_name":"远行商人「云上仙岛」","items":[` +
		`{"name":"残缺魔镜","kind":"prop","price":120,"limit":2,"time_label":"08:00-12:00"},` +
		`{"name":"幽系血脉秘药","kind":"prop","price":80,"limit":1,"time_label":"08:00-12:00"}]}}`
	if err := s.store.PutMerchantSlot(slot.Unix(), false, body); err != nil {
		t.Fatalf("写槽缓存: %v", err)
	}
	if err := s.store.UpsertMerchantSub("UID:1", "player@qq.com", keywords); err != nil {
		t.Fatalf("写订阅: %v", err)
	}
	return slot
}

// fakeSMTP 把 SMTP 换成计数器:每发一封记一次收件人与正文,并睡一小会儿模拟真实往返。
// 返回两个取值函数:收件人列表、正文列表。
//
// 正文也要记是因为本次 fix 的关键契约是「第二封只含新到的那几件」 —— 只数封数的话,
// 第二封把四件全重发一遍照样通过(正是「永远绿灯的测试」那种假安全感)。
func fakeSMTP(t *testing.T, s *Server, delay time.Duration) (func() []string, func() []string) {
	t.Helper()
	s.smtp = newSMTPSender("from@qq.com", "pass")
	var mu sync.Mutex
	var sent, htmls []string
	s.smtp.sendFn = func(to, subject, html string) error {
		mu.Lock()
		sent = append(sent, to)
		htmls = append(htmls, html)
		mu.Unlock()
		time.Sleep(delay) // 真实 SMTP 往返是秒级,Mark 发生在返回之后
		return nil
	}
	to := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), sent...)
	}
	body := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), htmls...)
	}
	return to, body
}

// TestMerchantNotifyConcurrentSameSlot 同一槽被两个触发源并发调用,只应发出一封。
func TestMerchantNotifyConcurrentSameSlot(t *testing.T) {
	s := newTestServer(t)
	slot := seedMerchantNotify(t, s, "魔镜,钥匙")
	sent, _ := fakeSMTP(t, s, 150*time.Millisecond)

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.merchantNotify(slot)
		}()
	}
	wg.Wait()

	if got := sent(); len(got) != 1 {
		t.Fatalf("同一槽并发触发发出 %d 封, 期望 1 封: %v", len(got), got)
	}
}

// TestMerchantNotifyRepeatedSameSlot 补扫每 15 分钟跑一遍(顺序重复):只发一封。
func TestMerchantNotifyRepeatedSameSlot(t *testing.T) {
	s := newTestServer(t)
	slot := seedMerchantNotify(t, s, "魔镜,钥匙")
	sent, _ := fakeSMTP(t, s, 0)

	for i := 0; i < 3; i++ {
		s.merchantNotify(slot)
	}

	if got := sent(); len(got) != 1 {
		t.Fatalf("同一槽重复触发发出 %d 封, 期望 1 封: %v", len(got), got)
	}
}

// TestMerchantNotifyDifferentSlots 不同槽互不干扰:各发一封,共两封。
func TestMerchantNotifyDifferentSlots(t *testing.T) {
	s := newTestServer(t)
	slot := seedMerchantNotify(t, s, "魔镜,钥匙")
	next := slot.Add(merchantSlotStep)
	if err := s.store.PutMerchantSlot(next.Unix(), false,
		`{"code":0,"data":{"merchant_name":"远行商人「云上仙岛」","items":`+
			`[{"name":"适格钥匙","kind":"prop","price":60,"limit":1,"time_label":"12:00-16:00"}]}}`); err != nil {
		t.Fatalf("写下一轮槽缓存: %v", err)
	}
	sent, _ := fakeSMTP(t, s, 0)

	s.merchantNotify(slot)
	s.merchantNotify(next)

	if got := sent(); len(got) != 2 {
		t.Fatalf("两个槽共发出 %d 封, 期望 2 封: %v", len(got), got)
	}
}

// TestMerchantNotifyRestockInLaterSlot 隔档补货:同一批货在更晚的档重新上架时,
// **必须再提醒一次** —— 对每个订阅者那都是一次新的购买机会。
//
// 2026-09-03 线上故障:16:00 档的 蓝晶碧玺/魔力果/神奇的蛋 与 08:00 档一字不差,而
// merchantNotify 原先按「本营业日更早槽已出现过的商品名」去重,把整档静默吞掉 —— 日志里
// 连一行都没有(该出口只在「有货但关键词没命中」时才记),排查时完全无从下手。
// 去重改为**每槽每商品**后,这一档照常发信。
//
// 断言**正文**而不只数封数:只数封数的话,第二封发成空正文照样通过 —— 那正是
// 「永远绿灯的测试」给人的虚假安全感。
func TestMerchantNotifyRestockInLaterSlot(t *testing.T) {
	s := newTestServer(t)
	slot := seedMerchantNotify(t, s, "") // 空关键词 = 订阅全部
	next := slot.Add(merchantSlotStep)
	// 更晚的一档:商品与上一档**完全相同**(隔档补货),只有时段标签不同。
	if err := s.store.PutMerchantSlot(next.Unix(), false,
		`{"code":0,"data":{"merchant_name":"远行商人「云上仙岛」","items":[`+
			`{"name":"残缺魔镜","kind":"prop","price":120,"limit":2,"time_label":"12:00-16:00"},`+
			`{"name":"幽系血脉秘药","kind":"prop","price":80,"limit":1,"time_label":"12:00-16:00"}]}}`); err != nil {
		t.Fatalf("写下一档槽缓存: %v", err)
	}
	sent, bodies := fakeSMTP(t, s, 0)

	s.merchantNotify(slot)
	s.merchantNotify(next)

	if got := sent(); len(got) != 2 {
		t.Fatalf("隔档补货共发出 %d 封, 期望 2(每档各一封): %v", len(got), got)
	}
	bs := bodies()
	if len(bs) != 2 {
		t.Fatalf("正文 %d 封, 期望 2 封", len(bs))
	}
	// 第二封必须真的带着这批货 —— 空正文也算「发了」,那等于没提醒。
	for _, want := range []string{"残缺魔镜", "幽系血脉秘药"} {
		if !strings.Contains(bs[1], want) {
			t.Errorf("第二封缺 %q —— 隔档补货没提醒到", want)
		}
	}
	// 下一档的已通知清单要独立记录,否则补扫会再发一封
	if !s.store.MerchantNotifiedItems(next.Unix(), "player@qq.com")["残缺魔镜"] {
		t.Error("下一档未记录已通知商品: 补扫会重复发一封")
	}
}

// TestMerchantNotifyKeywordMiss 关键词未命中任何新增商品时一封都不发。
func TestMerchantNotifyKeywordMiss(t *testing.T) {
	s := newTestServer(t)
	slot := seedMerchantNotify(t, s, "相框,国王,棱镜,项链")
	sent, _ := fakeSMTP(t, s, 0)

	s.merchantNotify(slot)

	if got := sent(); len(got) != 0 {
		t.Fatalf("关键词未命中却发出 %d 封: %v", len(got), got)
	}
}

// TestMerchantNotifyRetryAfterFailure 认领不能把发信失败的槽锁死:
// 冷却内立刻重试应被拦(SMTP 抽风时不连发),冷却过后(补扫 tick)必须能重试成功。
func TestMerchantNotifyRetryAfterFailure(t *testing.T) {
	s := newTestServer(t)
	slot := seedMerchantNotify(t, s, "魔镜,钥匙")
	s.smtp = newSMTPSender("from@qq.com", "pass")
	var mu sync.Mutex
	var attempts int
	s.smtp.sendFn = func(to, subject, html string) error {
		mu.Lock()
		attempts++
		n := attempts
		mu.Unlock()
		if n == 1 { // 首次发信失败(授权码过期/限流一类)
			return errors.New("smtp 550 被限流")
		}
		return nil
	}

	s.merchantNotify(slot) // 第 1 次尝试:失败
	s.merchantNotify(slot) // 冷却内:应被认领拦住,不再尝试
	mu.Lock()
	got1 := attempts
	mu.Unlock()
	if got1 != 1 {
		t.Fatalf("冷却内重试未被拦住, 尝试次数 = %d, 期望 1", got1)
	}

	// 手动让认领过期,模拟 15 分钟后的补扫 tick
	s.merchantClaimMu.Lock()
	s.merchantClaimed[slot.Unix()] = time.Now().Add(-merchantClaimCooldown)
	s.merchantClaimMu.Unlock()
	s.merchantNotify(slot) // 冷却后:应重试并成功

	mu.Lock()
	got2 := attempts
	mu.Unlock()
	if got2 != 2 {
		t.Fatalf("冷却后未重试, 尝试次数 = %d, 期望 2", got2)
	}
	notified := s.store.MerchantNotifiedItems(slot.Unix(), "player@qq.com")
	if !notified["残缺魔镜"] {
		t.Fatalf("重试成功却未记录已通知商品, 下次补扫会再发一封: %v", notified)
	}
}

// TestMerchantNotifyLateArrivingItems 第三方滞后补货:同一轮第二次回源带来新商品时,
// 第二封邮件**只含新到的那几件**,不能把上一封发过的再发一遍。
//
// 对应 2026-08-30 那次故障:20:0x 首查拿到 4 件全天货并发信,20:56 补上的魔力果/火系粉尘/
// 萌系粉尘因「本槽已通知过」被挡 —— 订阅者一整轮都没收到这三件的提醒。
func TestMerchantNotifyLateArrivingItems(t *testing.T) {
	s := newTestServer(t)
	slot := seedMerchantNotify(t, s, "") // 空关键词 = 订阅全部
	sent, bodies := fakeSMTP(t, s, 0)

	s.merchantNotify(slot) // 第 1 次:2 件(残缺魔镜 / 幽系血脉秘药)
	if got := sent(); len(got) != 1 {
		t.Fatalf("首次通知发出 %d 封, 期望 1 封", len(got))
	}

	// 第三方滞后补货:同一轮重查后货单从 2 件变成 4 件。
	later := `{"code":0,"data":{"merchant_name":"远行商人「云上仙岛」","items":[` +
		`{"name":"残缺魔镜","kind":"prop","price":120,"limit":2,"time_label":"20:00-24:00"},` +
		`{"name":"幽系血脉秘药","kind":"prop","price":80,"limit":1,"time_label":"20:00-24:00"},` +
		`{"name":"魔力果","kind":"prop","price":6000,"limit":20,"time_label":"20:00-24:00"},` +
		`{"name":"火系粉尘","kind":"prop","price":500,"limit":100,"time_label":"20:00-24:00"}]}}`
	if err := s.store.PutMerchantSlot(slot.Unix(), false, later); err != nil {
		t.Fatalf("覆盖槽缓存: %v", err)
	}
	// 认领冷却(10 分钟)会拦住紧接着的第二次通知,这里手动放行 —— 真实路径里它是
	// 10 分钟后的另一次回源(见 merchantRefetch),测试关心的是商品级去重而非冷却。
	s.merchantClaimMu.Lock()
	s.merchantClaimed[slot.Unix()] = time.Now().Add(-merchantClaimCooldown)
	s.merchantClaimMu.Unlock()

	s.merchantNotify(slot) // 第 2 次:应只补发 2 件新货

	if got := sent(); len(got) != 2 {
		t.Fatalf("补货后共发出 %d 封, 期望 2 封(首次 1 + 补发 1)", len(got))
	}
	// 关键断言:第二封只含新到的 2 件 —— 多了是重复打扰,少了是漏发。
	bs := bodies()
	if len(bs) != 2 {
		t.Fatalf("正文 %d 封, 期望 2 封", len(bs))
	}
	for _, want := range []string{"魔力果", "火系粉尘"} {
		if !strings.Contains(bs[1], want) {
			t.Errorf("第二封缺新货 %q", want)
		}
	}
	for _, old := range []string{"残缺魔镜", "幽系血脉秘药"} {
		if strings.Contains(bs[1], old) {
			t.Errorf("第二封重复发了上一封已发过的 %q", old)
		}
	}
	notified := s.store.MerchantNotifiedItems(slot.Unix(), "player@qq.com")
	for _, want := range []string{"残缺魔镜", "幽系血脉秘药", "魔力果", "火系粉尘"} {
		if !notified[want] {
			t.Errorf("已通知清单缺 %q: %v", want, notified)
		}
	}
}

// TestMerchantNotifyNoNewItemsNoMail 重查后一件新货都没有时,一封都不该发。
// 这是 TestMerchantNotifyLateArrivingItems 的另一半:补货能补发,但没补货时不能骚扰。
func TestMerchantNotifyNoNewItemsNoMail(t *testing.T) {
	s := newTestServer(t)
	slot := seedMerchantNotify(t, s, "")
	sent, _ := fakeSMTP(t, s, 0)

	s.merchantNotify(slot)
	s.merchantClaimMu.Lock()
	s.merchantClaimed[slot.Unix()] = time.Now().Add(-merchantClaimCooldown)
	s.merchantClaimMu.Unlock()
	s.merchantNotify(slot) // 货单没变:不应再发

	if got := sent(); len(got) != 1 {
		t.Fatalf("货单未变却发出 %d 封, 期望 1 封", len(got))
	}
}

// TestMerchantNotifyPerSubscriberDedup 已通知清单**按邮箱分别记**:给 A 发过的商品
// 不能挡住 B(两人各自订阅、各收各的)。
func TestMerchantNotifyPerSubscriberDedup(t *testing.T) {
	s := newTestServer(t)
	slot := seedMerchantNotify(t, s, "")
	if err := s.store.UpsertMerchantSub("UID:2", "other@qq.com", ""); err != nil {
		t.Fatalf("写第二个订阅: %v", err)
	}
	sent, _ := fakeSMTP(t, s, 0)

	s.merchantNotify(slot) // 两个订阅者各收一封

	got := sent()
	if len(got) != 2 {
		t.Fatalf("两个订阅者共收到 %d 封, 期望 2 封: %v", len(got), got)
	}
	if !s.store.MerchantNotifiedItems(slot.Unix(), "player@qq.com")["残缺魔镜"] ||
		!s.store.MerchantNotifiedItems(slot.Unix(), "other@qq.com")["残缺魔镜"] {
		t.Fatal("两个订阅者的已通知清单应各自独立记录")
	}
}
