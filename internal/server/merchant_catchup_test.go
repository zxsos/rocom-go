package server

import (
	"net/http"
	"testing"
	"time"
)

// 本文件锁住「整点后尽快拿到新货单」这条链路。
//
// 为什么值得单独测:两个源都在整点后约 1 分钟才切到新一轮(08:00 档实测 32~62 秒,
// 12:00 换档同样约 1 分钟)。于是**整点后的第一次回源几乎必然拿到空货单**,而判据里
// 若只有「10 分钟冷却」一条,那次空回源就会把新货单堵到整点后 10 分钟才进库 ——
// 比不轮询还慢。这种错误没有任何症状:数据最终会到,只是每次都晚十分钟。

// TestMerchantFetchEmptyThenCatchup 真实走一遍「整点后第一次回源拿到空」。
//
// 用 merchantFetch 真写库(而非手工播种),确保**落库的 empty 标记**确实能让后续
// 判据认出「还没切换」—— 中间任何一环(写错 empty、判据读错字段)都会让密集重试
// 失效,退化成 10 分钟冷却。
//
// 时间这样构造才自洽:切换窗口是相对**槽开始**算的(merchantCatchupWin),而
// merchantFetch 按真实时间写 fetched_at,故 slotStart 必须接近现在 —— 取「1 分钟前
// 开始的槽」,正好是整点后第一次回源的场景。
func TestMerchantFetchEmptyThenCatchup(t *testing.T) {
	s := newTestServer(t)
	slotStart := time.Now().Add(-time.Minute)
	fakeMerchantAPI(t, `{"code":200,"data":{"item_count":0,"items":[]}}`, http.StatusOK)

	ok, empty := s.merchantFetch(slotStart, true)
	if !ok {
		t.Fatal("merchantFetch 失败(假第三方应正常响应)")
	}
	if !empty {
		t.Fatal("第三方返回空时应判定 empty")
	}
	gotEmpty, _, fetchedAt, ok := s.store.GetMerchantSlot(slotStart.Unix())
	if !ok {
		t.Fatal("空货单没落库")
	}
	if !gotEmpty {
		t.Fatal("空货单没标记 empty —— 判据认不出「还没切换」,会退化成 10 分钟冷却")
	}

	// 第三方切好了:过了密集间隔就必须允许重试
	now := time.Unix(fetchedAt, 0).Add(merchantCatchupEvery + time.Second)
	if !s.merchantShouldFetch(slotStart, now) {
		t.Errorf("空货单过了 %v 仍不允许重试 —— 新货单会被拖到 %v 冷却之后,正是本次要修的问题",
			merchantCatchupEvery, merchantRefetch)
	}
	// 对照:一旦过了切换窗口,即便仍是空的也要回到 10 分钟冷却,不能一直刷
	past := slotStart.Add(merchantCatchupWin + time.Minute)
	if s.merchantShouldFetch(slotStart, past) {
		t.Errorf("过了切换窗口(%v)仍按密集间隔回源 —— 源异常时会一直刷第三方", merchantCatchupWin)
	}
}

// TestMerchantShouldFetchCatchup 切换窗口内的空货单按密集间隔重试。
//
// 这是本次重构最核心的一条断言:窗口内「空」必须走 merchantCatchupEvery(30 秒),
// 而不是 merchantRefetch(10 分钟)。
func TestMerchantShouldFetchCatchup(t *testing.T) {
	s := newTestServer(t)
	// slotStart 显式取「1 分钟前开始的槽」,而不是 merchantDaySlots(...)[N]:
	// 后者取出来的槽可能尚未开始(如 12 点取到 16:00 那档),导致用例结果随运行时
	// 刻变化 —— 那正是 flaky 的来源。切换窗口是相对**槽开始**算的,故必须自洽。
	slotStart := time.Now().Add(-time.Minute)

	// 槽刚开始时回源:此刻第三方还没切换,拿到空货单(真实场景:整点后 15 秒)
	firstFetch := slotStart.Add(15 * time.Second)
	if err := s.store.PutMerchantSlotAt(slotStart.Unix(), true,
		`{"code":200,"data":{"item_count":0,"items":[]}}`, firstFetch.Unix()); err != nil {
		t.Fatalf("播种空货单: %v", err)
	}

	cases := []struct {
		name string
		at   time.Time
		want bool
		why  string
	}{
		{"刚回源完立刻再问", firstFetch.Add(5 * time.Second), false,
			"30 秒未到,不能回源(否则就是在刷第三方)"},
		{"过了密集间隔仍在窗口内", firstFetch.Add(31 * time.Second), true,
			"第三方此刻多半已切换,必须重试 —— 这条不成立的话新货单要等 10 分钟"},
		{"窗口内多次重试", firstFetch.Add(2 * time.Minute), true,
			"窗口内始终允许按密集间隔重试"},
	}
	for _, c := range cases {
		if got := s.merchantShouldFetch(slotStart, c.at); got != c.want {
			t.Errorf("%s: merchantShouldFetch = %v, 期望 %v —— %s", c.name, got, c.want, c.why)
		}
	}

	// 对照:同样是「空」,但已过切换窗口 → 回到 10 分钟冷却(不再刷第三方)
	late := slotStart.Add(merchantCatchupWin + time.Minute)
	if err := s.store.PutMerchantSlotAt(slotStart.Unix(), true,
		`{"code":200,"data":{"item_count":0,"items":[]}}`, late.Add(-time.Minute).Unix()); err != nil {
		t.Fatalf("播种: %v", err)
	}
	if got := s.merchantShouldFetch(slotStart, late); got {
		t.Errorf("过了切换窗口的空槽在 1 分钟后就回源了 —— 窗口外应回到 %v 冷却,否则会一直刷", merchantRefetch)
	}
}

// TestMerchantShouldFetchAfterGotGoods 拿到货单后回到 10 分钟冷却。
//
// 「空」与「有货」语义不同:空是等第三方切换(几十秒),有货是等它补货(十分钟)。
// 拿到货单后若继续 30 秒一次,单轮会多打上百次第三方,token 直接烧穿。
func TestMerchantShouldFetchAfterGotGoods(t *testing.T) {
	s := newTestServer(t)
	slotStart := time.Now().Add(-time.Minute)
	fetchedAt := slotStart.Add(time.Minute) // 槽开始 1 分钟后拿到货单
	if err := s.store.PutMerchantSlotAt(slotStart.Unix(), false,
		`{"code":200,"data":{"item_count":2,"items":[{"name":"蓝晶碧玺"}]}}`, fetchedAt.Unix()); err != nil {
		t.Fatalf("播种货单: %v", err)
	}

	if got := s.merchantShouldFetch(slotStart, fetchedAt.Add(merchantCatchupEvery+time.Second)); got {
		t.Errorf("已拿到货单却仍按 %v 密集重查 —— 单轮会多打上百次第三方", merchantCatchupEvery)
	}
	if got := s.merchantShouldFetch(slotStart, fetchedAt.Add(merchantRefetch+time.Second)); !got {
		t.Errorf("过了 %v 冷却应重查(追第三方滞后补上的新货)", merchantRefetch)
	}
}

// TestMerchantShouldFetchNeverAfterWindow 窗口外一律不回源(避免无限期烧 token)。
//
// 这条在改前就存在,但重构动了判据顺序,必须确认没被绕过 —— 尤其是「空 + 窗口外」
// 这个组合:新增的密集重试分支不能越过窗口闸门。
func TestMerchantShouldFetchNeverAfterWindow(t *testing.T) {
	s := newTestServer(t)
	slotStart := time.Now().Add(-time.Minute)
	if err := s.store.PutMerchantSlotAt(slotStart.Unix(), true,
		`{"code":200,"data":{"item_count":0,"items":[]}}`, slotStart.Add(89*time.Minute).Unix()); err != nil {
		t.Fatalf("播种: %v", err)
	}
	for _, at := range []time.Time{
		slotStart.Add(merchantRefetchWin),             // 刚过窗口
		slotStart.Add(merchantRefetchWin + time.Hour), // 远过窗口
	} {
		if got := s.merchantShouldFetch(slotStart, at); got {
			t.Errorf("槽开始后 %v 仍判定回源 —— 窗口 %v 之外必须一律不查", at.Sub(slotStart), merchantRefetchWin)
		}
	}
}

// TestMerchantPollFinerThanCatchup 轮询必须明显细于密集重试间隔。
//
// 这条防的是配套改动走偏:merchantCatchupEvery 再短也没用 —— 若 merchantLoop 的
// ticker 比它还粗,实际重试间隔就是轮询间隔,「30 秒重试」会被拖成 15 分钟。
// 单独断言常量关系,比事后调 ticker 省事得多。
func TestMerchantPollFinerThanCatchup(t *testing.T) {
	if merchantPoll > merchantCatchupEvery/2 {
		t.Errorf("merchantPoll(%v) 应 ≤ merchantCatchupEvery 的一半(%v), 否则密集重试会被轮询粒度拖慢",
			merchantPoll, merchantCatchupEvery/2)
	}
	if merchantCatchupEvery >= merchantRefetch {
		t.Errorf("merchantCatchupEvery(%v) 必须小于 merchantRefetch(%v), 否则密集重试没有意义",
			merchantCatchupEvery, merchantRefetch)
	}
	if merchantCatchupWin >= merchantRefetchWin {
		t.Errorf("merchantCatchupWin(%v) 必须小于 merchantRefetchWin(%v), 否则密集重试会越过窗口闸门",
			merchantCatchupWin, merchantRefetchWin)
	}
}
