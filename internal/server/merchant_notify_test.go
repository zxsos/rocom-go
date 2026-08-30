package server

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// 本文件守住「同一营业日槽对同一收件人只发一封提醒」。
//
// 存在理由:merchant_notified 只在**发信成功之后**才 Mark(发信失败要留给 merchantResend 补扫
// 重试,见 merchant.go),而一次 SMTP 往返要几秒。于是两个触发源在同一 tick 撞到同一个槽时
// (merchantEnsure 回源后异步发信 + merchantResend 补扫,8/12/16/20 每个整点档首次回源必撞),
// 后到者读到的必然是「未 Mark」,于是各发一封 —— 表现为订阅者收到两份一模一样的邮件。
// 该竞态只在**并发**下出现,顺序调用测不出来,故用 goroutine + 模拟 SMTP 延迟复现。

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

// fakeSMTP 把 SMTP 换成计数器:每发一封记一次收件人,并睡一小会儿模拟真实往返。
func fakeSMTP(t *testing.T, s *Server, delay time.Duration) func() []string {
	t.Helper()
	s.smtp = newSMTPSender("from@qq.com", "pass")
	var mu sync.Mutex
	var sent []string
	s.smtp.sendFn = func(to, subject, html string, imgs []merchantMailImg) error {
		mu.Lock()
		sent = append(sent, to)
		mu.Unlock()
		time.Sleep(delay) // 真实 SMTP 往返是秒级,Mark 发生在返回之后
		return nil
	}
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), sent...)
	}
}

// TestMerchantNotifyConcurrentSameSlot 同一槽被两个触发源并发调用,只应发出一封。
func TestMerchantNotifyConcurrentSameSlot(t *testing.T) {
	s := newTestServer(t)
	slot := seedMerchantNotify(t, s, "魔镜,钥匙")
	sent := fakeSMTP(t, s, 150*time.Millisecond)

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
	sent := fakeSMTP(t, s, 0)

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
	sent := fakeSMTP(t, s, 0)

	s.merchantNotify(slot)
	s.merchantNotify(next)

	if got := sent(); len(got) != 2 {
		t.Fatalf("两个槽共发出 %d 封, 期望 2 封: %v", len(got), got)
	}
}

// TestMerchantNotifyKeywordMiss 关键词未命中任何新增商品时一封都不发。
func TestMerchantNotifyKeywordMiss(t *testing.T) {
	s := newTestServer(t)
	slot := seedMerchantNotify(t, s, "相框,国王,棱镜,项链")
	sent := fakeSMTP(t, s, 0)

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
	s.smtp.sendFn = func(to, subject, html string, imgs []merchantMailImg) error {
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
	if !s.store.MerchantNotified(slot.Unix(), "player@qq.com") {
		t.Fatal("重试成功却未 Mark, 下次补扫会再发一封")
	}
}
