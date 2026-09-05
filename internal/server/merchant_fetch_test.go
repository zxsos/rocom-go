package server

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// 本文件守住远行商人的**回源时机**:什么时候该回源、什么时候必须不回源,以及第三方
// 瞬时返回空时不能把已有货单清掉。
//
// 存在理由(2026-08-30 线上故障):原实现是「每槽只回源一次」,而第三方自己有缓存,
// 轮次开始后新上架的商品要滞后才出现在它的响应里 —— 当晚 20:00 开轮,第三方那份快照
// 到 20:56 才补全魔力果/火系粉尘/萌系粉尘,页面整整一轮只显示了 4 件全天货,
// 只有管理员点「强制刷新」才补得回来。
//
// 但反过来也不能放开重查:merchantFetch 是按「现在时刻」问第三方的,拿回来的是当前货单,
// 往已结束的历史槽里写一发就是**伪造历史数据**。所以这里同时守住两个方向的约束。

// testDay 造一个营业日(0 = 今天,-1 = 昨天)。
//
// 不能写死日历日期:store 的写入路径会顺手删掉 48 小时前的槽(见 merchantRetain),
// 写死的日期过两天就被清掉,用例会随机失败。故相对「今天」取,永远落在保留窗口内。
// 各用例靠 (off, 槽下标) 错开,互不干扰 —— 注意 merchant_notify_test.go 已占用今天的
// 8:00 槽(seedMerchantNotify),本文件一律避开它。
func testDay(off int) time.Time {
	return merchantDayStart(time.Now()).AddDate(0, 0, off)
}

// fakeMerchantAPI 把第三方接口换成 httptest 假服务,返回请求次数。
//
// 必须挡住真实请求:merchantFetch 打的是线上地址,单元测试跑一次就烧一次 token,
// 而且返回什么取决于第三方当时的货单,断言根本没法写。
func fakeMerchantAPI(t *testing.T, body string, status int) *int {
	t.Helper()
	hits := new(int)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*hits++
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	old := merchantFetchURL
	merchantFetchURL = srv.URL
	t.Cleanup(func() { merchantFetchURL = old })
	return hits
}

// TestMerchantFetchLogsEveryAttempt 同一轮内连续回源,日志必须逐次给出递增的尝试
// 序号,且每个出口(空 / 有货)都留下一条。
//
// 存在理由:整点后第三方滞后约 1 分钟才切到新一轮,于是切换窗口内前几次回源**必然
// 拿到空**。日志里若没有递增序号,「第 4 次才拿到」与「一次命中」长得一模一样 ——
// 而那正是区分「第三方慢」与「我们压根没去查」的唯一依据。序号不递增,这条线索就没了。
//
// 断言日志文本而非返回值:回源结果本身(ok/empty)已被其它用例覆盖,这里守的是
// 「日志能否把一整轮的获取过程还原出来」—— 光有返回值正确、日志看不出过程,照样排查不了。
func TestMerchantFetchLogsEveryAttempt(t *testing.T) {
	const emptyBody = `{"code":200,"data":{"item_count":0,"items":[]}}`
	const goodsBody = `{"code":200,"data":{"item_count":2,"items":` +
		`[{"name":"残缺魔镜"},{"name":"适格钥匙"}]}}`

	s := newTestServer(t)
	// 按调用次序返回:前两次空,第三次起有货(模拟整点后第三方滞后切换)。
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		w.Header().Set("Content-Type", "application/json")
		if n <= 2 {
			_, _ = io.WriteString(w, emptyBody)
			return
		}
		_, _ = io.WriteString(w, goodsBody)
	}))
	defer srv.Close()
	oldURL := merchantFetchURL
	merchantFetchURL = srv.URL
	defer func() { merchantFetchURL = oldURL }()

	// 接管 log 输出以断言文本内容(不改动全局 logger 之外的状态)。
	var buf bytes.Buffer
	oldOut, oldFlags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	defer func() { log.SetOutput(oldOut); log.SetFlags(oldFlags) }()

	// 槽取昨天且避开今天 8:00(已让给订阅测试,见 testDay 的说明)。
	slot := merchantDaySlots(testDay(-1))[1]
	if ok, empty := s.merchantFetch(slot, true); !ok || !empty {
		t.Fatalf("第 1 次应回源成功且为空, 实际 ok=%v empty=%v", ok, empty)
	}
	if ok, empty := s.merchantFetch(slot, true); !ok || !empty {
		t.Fatalf("第 2 次应回源成功且为空, 实际 ok=%v empty=%v", ok, empty)
	}
	if ok, empty := s.merchantFetch(slot, true); !ok || empty {
		t.Fatalf("第 3 次应回源成功且有货, 实际 ok=%v empty=%v", ok, empty)
	}

	got := buf.String()
	for i := 1; i <= 3; i++ {
		if !strings.Contains(got, fmt.Sprintf("尝试#%d", i)) {
			t.Errorf("日志缺少第 %d 次尝试的序号:\n%s", i, got)
		}
	}
	if c := strings.Count(got, "结果=空货单"); c != 2 {
		t.Errorf("应有 2 条空货单结果, 实际 %d 条:\n%s", c, got)
	}
	if !strings.Contains(got, "结果=有货 2 件") {
		t.Errorf("日志缺少有货结果:\n%s", got)
	}
	// 阶段耗时必须真的打出来:只有序号没有耗时,还是看不出慢在哪一段。
	if !strings.Contains(got, "总计=") {
		t.Errorf("日志缺少各阶段耗时:\n%s", got)
	}
}

// TestMerchantShouldFetch 当前槽的重查窗口与冷却,以及「已结束的槽永不回源」。
//
// 最后两条是最重要的:已结束的槽拿回来的是「现在」的货单,写进去等于伪造历史
// —— 服务 16 点才启动时,旧实现会把 16 点的货单同时填进 8/12/16 三个槽。
func TestMerchantShouldFetch(t *testing.T) {
	const goods = `{"code":200,"data":{"item_count":2,"items":` +
		`[{"name":"残缺魔镜"},{"name":"适格钥匙"}]}}`
	s := newTestServer(t)

	cases := []struct {
		name    string
		idx     int           // 槽下标(见 testDay 关于错开的说明;今天 8:00 已让给订阅测试)
		seed    bool          // 是否先播种一条缓存记录
		ago     time.Duration // 播种记录的回源时刻在「现在」之前多久
		elapsed time.Duration // 「现在」距槽开始多久
		want    bool
	}{
		{"未查过的进行中槽要补查", 0, false, 0, 30 * time.Minute, true},
		{"刚回源过在冷却内不再查", 1, true, 2 * time.Minute, 30 * time.Minute, false},
		{"过冷却仍在进行中则重查", 2, true, 20 * time.Minute, 30 * time.Minute, true},
		{"过窗口即使过了冷却也不查", 3, true, 20 * time.Minute, 2 * time.Hour, false},
		{"已结束的槽永不回源_无记录", 4, false, 0, 5 * time.Hour, false},
		{"已结束的槽永不回源_有记录", 5, true, 20 * time.Minute, 5 * time.Hour, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			slot := merchantDaySlots(testDay(-1))[c.idx]
			now := slot.Add(c.elapsed)
			if c.seed {
				if err := s.store.PutMerchantSlotAt(slot.Unix(), false, goods, now.Add(-c.ago).Unix()); err != nil {
					t.Fatalf("播种槽缓存: %v", err)
				}
			}
			if got := s.merchantShouldFetch(slot, now); got != c.want {
				t.Errorf("merchantShouldFetch = %v, 期望 %v(槽 %s, now = 槽开始 + %v)",
					got, c.want, slot.Format("15:04"), c.elapsed)
			}
		})
	}
}

// TestMerchantFetchKeepsGoodsOnEmptyResponse 重查撞上第三方瞬时返回空时,
// 库里已有的好货单必须**保留** —— 覆盖成空会让页面上明明还有的货单整片消失。
//
// 同时要求把回源时刻推到当前:不推的话重查冷却立刻失效,下一 tick 又判定该重查,
// 于是一路回源到窗口结束,白烧 token。
func TestMerchantFetchKeepsGoodsOnEmptyResponse(t *testing.T) {
	s := newTestServer(t)
	slot := merchantDaySlots(testDay(0))[1]
	const goods = `{"code":200,"data":{"item_count":7,"items":[{"name":"残缺魔镜"}]}}`
	if err := s.store.PutMerchantSlotAt(slot.Unix(), false, goods, time.Now().Add(-time.Hour).Unix()); err != nil {
		t.Fatalf("播种货单: %v", err)
	}
	_, _, before, ok := s.store.GetMerchantSlot(slot.Unix())
	if !ok {
		t.Fatal("播种失败: 读不到刚写的槽缓存")
	}
	fakeMerchantAPI(t, `{"code":200,"data":{"item_count":0,"items":[]}}`, http.StatusOK)

	ok, empty := s.merchantFetch(slot, true)

	if !ok || !empty {
		t.Fatalf("merchantFetch = (%v, %v), 期望 (true, true)", ok, empty)
	}
	gotEmpty, gotData, after, ok2 := s.store.GetMerchantSlot(slot.Unix())
	if !ok2 {
		t.Fatal("槽缓存记录消失了")
	}
	if gotEmpty {
		t.Error("第三方返回空却把槽标记成 empty, 页面货单会整片消失")
	}
	if gotData != goods {
		t.Errorf("第三方返回空却覆盖了既有货单:\n got = %s\nwant = %s", gotData, goods)
	}
	if after <= before {
		t.Errorf("保留旧货单后未把回源时刻推前(%d → %d): 冷却失效会导致反复回源, 白烧 token", before, after)
	}
}

// TestMerchantFetchStillWritesEmptyWhenNoGoods 保护逻辑不能过头:库里本来就没货时,
// 空响应必须照常写成 empty,否则「该轮确实无货」永远记不下来。
func TestMerchantFetchStillWritesEmptyWhenNoGoods(t *testing.T) {
	s := newTestServer(t)
	slot := merchantDaySlots(testDay(0))[2]
	const none = `{"code":200,"data":{"item_count":0,"items":[]}}`
	fakeMerchantAPI(t, none, http.StatusOK)

	ok, empty := s.merchantFetch(slot, true)

	if !ok || !empty {
		t.Fatalf("merchantFetch = (%v, %v), 期望 (true, true)", ok, empty)
	}
	gotEmpty, gotData, _, ok2 := s.store.GetMerchantSlot(slot.Unix())
	if !ok2 || !gotEmpty {
		t.Errorf("无货时未写成 empty: ok=%v empty=%v", ok2, gotEmpty)
	}
	if gotData != none {
		t.Errorf("无货时应把第三方原始响应一并存下(供事后排查):\n got = %s\nwant = %s", gotData, none)
	}
}

// TestMerchantFetchRefetchUpdatesGoods 重查拿到更全的货单时要**覆盖**写库 ——
// 这正是修的那个故障:20:56 那份快照多出 3 件,必须能盖掉 20:0x 那份不完整的。
func TestMerchantFetchRefetchUpdatesGoods(t *testing.T) {
	s := newTestServer(t)
	slot := merchantDaySlots(testDay(0))[3]
	const first = `{"code":200,"data":{"item_count":4,"items":[{"name":"残缺魔镜"}]}}`
	const later = `{"code":200,"data":{"item_count":7,"items":[{"name":"残缺魔镜"},{"name":"魔力果"}]}}`
	if err := s.store.PutMerchantSlotAt(slot.Unix(), false, first, time.Now().Add(-time.Hour).Unix()); err != nil {
		t.Fatalf("播种首查货单: %v", err)
	}
	hits := fakeMerchantAPI(t, later, http.StatusOK)

	ok, empty := s.merchantFetch(slot, true)

	if !ok || empty {
		t.Fatalf("merchantFetch = (%v, %v), 期望 (true, false)", ok, empty)
	}
	if *hits != 1 {
		t.Errorf("回源 %d 次, 期望 1 次", *hits)
	}
	if _, got, _, _ := s.store.GetMerchantSlot(slot.Unix()); got != later {
		t.Errorf("重查未覆盖旧货单:\n got = %s\nwant = %s", got, later)
	}
}

// TestMerchantCurrentSlot 当前轮 = 开始时刻 ≤ now 的最后一轮。
//
// 与 TestMerchantShouldFetch 的「已结束的槽永不回源」合起来,才完整守住「只回源当前轮」:
// 更早的轮连被评估的机会都没有(cur 之前的下标一律不看),而即便被评估,它们也已结束、
// merchantShouldFetch 必然返回 false。两条独立成立,互为兜底。
//
// 抽成纯函数测而不是走 merchantEnsure,是为了避开数据竞争:merchantEnsure 要求
// eggAPIKey 非空才回源,而 New() 起的 merchantLoop goroutine 会读同一个字段,
// 测试再写就与那次读取无序(-race 必报)。纯函数没有这个负担。
func TestMerchantCurrentSlot(t *testing.T) {
	day := testDay(-1)
	slots := merchantDaySlots(day)
	cases := []struct {
		elapsed time.Duration
		want    int
	}{
		{0, -1},                            // 0:00 打烊(merchantEnsure 在此之前就返回了)
		{7*time.Hour + 59*time.Minute, -1}, // 8 点前
		{8 * time.Hour, 0},
		{13 * time.Hour, 1},
		{19*time.Hour + 59*time.Minute, 2},
		{20 * time.Hour, 3},
		{23*time.Hour + 59*time.Minute, 3},
	}
	for _, c := range cases {
		now := day.Add(c.elapsed)
		if got := merchantCurrentSlot(day, now); got != c.want {
			t.Errorf("merchantCurrentSlot(now=%s) = %d, 期望 %d", now.Format("15:04"), got, c.want)
		}
	}
	// 不变式:当前轮(若存在)必定仍在进行中 —— 这是「只回源当前轮」安全的前提。
	// 若哪天它不成立,merchantEnsure 就会去回源一个已结束的槽(拿到的是现在的货单)。
	for _, c := range cases {
		if c.want < 0 {
			continue
		}
		if !merchantSlotLive(slots[c.want], day.Add(c.elapsed)) {
			t.Errorf("当前轮 %s 在进行中判定上不成立(now = 0 点 + %v)", slots[c.want].Format("15:04"), c.elapsed)
		}
	}
}

// TestMerchantFetchSendsRefresh 回源必须带 refresh=true —— 这条光读代码看不出来
// (参数拼在 URL 查询串里),但它是「准点拿到新货单」的关键。
//
// 为什么必须强制第三方回源:它自己有缓存,带 refresh=false 时返回的可能是上一轮
// 甚至更早的**旧快照** —— 那时无论我们重查多少次,拿回的始终是同一份陈旧数据,
// 「轮次开始后滞后补全」这件事永远追不上(2026-08-30 实测滞后 56 分钟)。
// 带 refresh=true 才会真正回源,准点后约 1 分钟即可拿到新货单。
//
// 同时也钉住其余必填参数:key 与 format=json 缺一不可。
func TestMerchantFetchSendsRefresh(t *testing.T) {
	s := newTestServer(t)
	s.eggAPIKeySet("test-key")              // merchantFetch 的必填查询参数;真 token 不在测试里出现
	slot := merchantDaySlots(testDay(0))[5] // 避开别处占用的槽
	const goods = `{"code":200,"data":{"item_count":1,"items":[{"name":"残缺魔镜"}]}}`

	// 收集**所有**请求而非只记最后一次:New() 起的 merchantLoop 后台 goroutine 也会
	// 回源打到这个假服务(见 server.go),只记最后一次的话可能读到后台的请求 ——
	// 那会让断言「碰巧正确」(假绿灯)。改成收集全部并断言全部,后台请求反而
	// 变成额外的验证样本。
	var mu sync.Mutex
	var queries []url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		queries = append(queries, r.URL.Query())
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, goods)
	}))
	t.Cleanup(srv.Close)
	old := merchantFetchURL
	merchantFetchURL = srv.URL
	t.Cleanup(func() { merchantFetchURL = old })

	if ok, _ := s.merchantFetch(slot, true); !ok {
		t.Fatal("merchantFetch 失败(应拿到正常响应)")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(queries) == 0 {
		t.Fatal("没有捕到任何回源请求")
	}
	for i, q := range queries {
		if got := q.Get("refresh"); got != "true" {
			t.Errorf("第 %d 次请求 refresh = %q, 期望 \"true\"(不强制回源会拿到第三方陈旧快照,重查等同空转)", i+1, got)
		}
		if got := q.Get("format"); got != "json" {
			t.Errorf("第 %d 次请求 format = %q, 期望 \"json\"", i+1, got)
		}
		if got := q.Get("key"); got == "" {
			t.Errorf("第 %d 次请求 key 缺失(第三方必填)", i+1)
		}
	}
}

// TestMerchantEnsureForcesRefresh 走**真实调用链**验证强制刷新生效:
// merchantEnsure → merchantShouldForceRefresh → merchantFetch。
//
// 为什么要有它:TestMerchantFetchSendsRefresh 直接调 merchantFetch(slot, true),
// 绕过了策略函数 —— 变异测试证明,把 merchantShouldForceRefresh 改成恒返回 false
// 时那条用例**照样通过**(假绿灯)。真正决定线上行为的是 merchantEnsure 里的调用,
// 故必须在这里钉死。
func TestMerchantEnsureForcesRefresh(t *testing.T) {
	s := newTestServer(t)
	s.eggAPIKeySet("test-key")
	// 用昨天的一个进行中槽(避开别处占用的):把 now 设在槽开始后 30 分钟
	slot := merchantDaySlots(testDay(-1))[1]
	now := slot.Add(30 * time.Minute)

	var mu sync.Mutex
	var queries []url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		queries = append(queries, r.URL.Query())
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":200,"data":{"item_count":1,"items":[{"name":"残缺魔镜"}]}}`)
	}))
	t.Cleanup(srv.Close)
	old := merchantFetchURL
	merchantFetchURL = srv.URL
	t.Cleanup(func() { merchantFetchURL = old })

	// force=true:前端「强制刷新」路径
	s.merchantEnsure(now, true)

	// 常规路径(shouldFetch 判定要回源):同样必须带 refresh=true —— 重查的目的
	// 就是追第三方滞后,拿它的缓存快照等于空转。
	s.merchantEnsure(now) // 此时该槽刚回源过(冷却内),换个槽触发常规首查
	slot2 := merchantDaySlots(testDay(-1))[2]
	s.merchantEnsure(slot2.Add(30 * time.Minute))

	mu.Lock()
	defer mu.Unlock()
	if len(queries) == 0 {
		t.Fatal("merchantEnsure 没有发起回源")
	}
	for i, q := range queries {
		if got := q.Get("refresh"); got != "true" {
			t.Errorf("第 %d 次回源 refresh = %q, 期望 \"true\"(拿缓存快照的重查等同空转)", i+1, got)
		}
	}
}
