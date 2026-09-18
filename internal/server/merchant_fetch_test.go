package server

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// 本文件守住远行商人的**回源时机**:什么时候该回源、什么时候必须不回源,以及源站
// 瞬时返回空时不能把已有货单清掉。
//
// 存在理由(2026-08-30 线上故障):原实现是「每槽只回源一次」,而源站自己有缓存,
// 轮次开始后新上架的商品要滞后才出现在它的页面里 —— 当晚 20:00 开轮,那份快照
// 到 20:56 才补全魔力果/火系粉尘/萌系粉尘,页面整整一轮只显示了 4 件全天货,
// 只有管理员点「强制刷新」才补得回来。
//
// 但反过来也不能放开重查:merchantFetch 是按「现在时刻」问源站的,拿回来的是当前货单,
// 往已结束的历史槽里写一发就是**伪造历史数据**。所以这里同时守住两个方向的约束。
//
// 假桩统一走 fakeHaoyouAPI(见 merchant_haoyou_test.go):商人现在只有一个源,
// 它抓的是 HTML 页面而非 JSON 接口,故这里喂的也是页面。

// testDay 造一个营业日(0 = 今天,-1 = 昨天)。
//
// 不能写死日历日期:store 的写入路径会顺手删掉 48 小时前的槽(见 merchantRetain),
// 写死的日期过两天就被清掉,用例会随机失败。故相对「今天」取,永远落在保留窗口内。
// 各用例靠 (off, 槽下标) 错开,互不干扰 —— 注意 merchant_notify_test.go 已占用今天的
// 8:00 槽(seedMerchantNotify),本文件一律避开它。
func testDay(off int) time.Time {
	return merchantDayStart(time.Now()).AddDate(0, 0, off)
}

// haoyouPageForSlot 造一份「该槽有这些货」的页面。
//
// 必须把 block 的 end 设成 slotStart + 4h:fetchHaoyou 是按 start 相等挑槽的
// (见 merchant_haoyou.go),而页面只标结束时刻。end 给错的话页面里就没有这个槽,
// 回源会「成功但空货」——看着像被测逻辑坏了,其实是夹具对不上。
func haoyouPageForSlot(slotStart time.Time, names ...string) string {
	goods := make([]haoyouGood, 0, len(names))
	for _, n := range names {
		goods = append(goods, haoyouGood{name: n, image: "a.png"})
	}
	return haoyouPage(haoyouBlock{end: slotStart.Add(merchantSlotStep).Unix(), goods: goods})
}

// TestMerchantFetchLogsEveryAttempt 同一轮内连续回源,日志必须逐次给出递增的尝试
// 序号,且每个出口(空 / 有货)都留下一条。
//
// 存在理由:整点后源站滞后约 1 分钟才切到新一轮,于是切换窗口内前几次回源**必然
// 拿到空**。日志里若没有递增序号,「第 4 次才拿到」与「一次命中」长得一模一样 ——
// 而那正是区分「源站慢」与「我们压根没去查」的唯一依据。序号不递增,这条线索就没了。
//
// 断言日志文本而非返回值:回源结果本身(ok/empty)已被其它用例覆盖,这里守的是
// 「日志能否把一整轮的获取过程还原出来」—— 光有返回值正确、日志看不出过程,照样排查不了。
func TestMerchantFetchLogsEveryAttempt(t *testing.T) {
	s := newTestServer(t)
	slot := merchantDaySlots(testDay(-1))[1]
	emptyPage := haoyouPageForSlot(slot)
	goodsPage := haoyouPageForSlot(slot, "残缺魔镜", "适格钥匙")

	// 按调用次序返回:前两次空,第三次起有货(模拟整点后源站滞后切换)。
	var mu sync.Mutex
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		n++
		body := emptyPage
		if n > 2 {
			body = goodsPage
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	old := haoyouURL
	haoyouURL = srv.URL
	t.Cleanup(func() { haoyouURL = old })

	// 接管 log 输出以断言文本内容(不改动全局 logger 之外的状态)。
	var buf bytes.Buffer
	oldOut, oldFlags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	defer func() { log.SetOutput(oldOut); log.SetFlags(oldFlags) }()

	for i := 1; i <= 2; i++ {
		if ok, empty := s.merchantFetch(slot); !ok || !empty {
			t.Fatalf("第 %d 次应回源成功且为空, 实际 ok=%v empty=%v", i, ok, empty)
		}
	}
	if ok, empty := s.merchantFetch(slot); !ok || empty {
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
	// 源站固定是好游快爆:日志里的来源标注要能被 grep 出来,否则多实例排查时分不清谁抓的。
	if !strings.Contains(got, "源="+merchantSrcHaoyou) {
		t.Errorf("日志缺少来源标注 源=%s:\n%s", merchantSrcHaoyou, got)
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

// TestMerchantFetchKeepsGoodsOnEmptyResponse 重查撞上源站瞬时返回空时,
// 库里已有的好货单必须**保留** —— 覆盖成空会让页面上明明还有的货单整片消失。
//
// 同时要求把回源时刻推到当前:不推的话重查冷却立刻失效,下一 tick 又判定该重查,
// 于是一路回源到窗口结束,白打源站。
//
// 这条比其它回源用例更好写:断言的是**播种进去的那份**原样留在库里(保护分支根本不
// 碰数据),所以可以逐字节精确比对,不必管归一化产物的字段顺序。
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
	fakeHaoyouAPI(t, haoyouPageForSlot(slot), http.StatusOK) // 该槽无货

	ok, empty := s.merchantFetch(slot)

	if !ok || !empty {
		t.Fatalf("merchantFetch = (%v, %v), 期望 (true, true)", ok, empty)
	}
	gotEmpty, gotData, after, ok2 := s.store.GetMerchantSlot(slot.Unix())
	if !ok2 {
		t.Fatal("槽缓存记录消失了")
	}
	if gotEmpty {
		t.Error("源站返回空却把槽标记成 empty, 页面货单会整片消失")
	}
	if gotData != goods {
		t.Errorf("源站返回空却覆盖了既有货单:\n got = %s\nwant = %s", gotData, goods)
	}
	if after <= before {
		t.Errorf("保留旧货单后未把回源时刻推前(%d → %d): 冷却失效会导致反复回源", before, after)
	}
}

// TestMerchantFetchStillWritesEmptyWhenNoGoods 保护逻辑不能过头:库里本来就没货时,
// 空响应必须照常写成 empty,否则「该轮确实无货」永远记不下来。
//
// 不比对存储原文:落库的是归一化后的响应体,字段集合由归一化层决定,逐字节比会把
// 断言绑在实现细节上。这里要守的只有两件事 —— 记了 empty,且确实没有商品。
func TestMerchantFetchStillWritesEmptyWhenNoGoods(t *testing.T) {
	s := newTestServer(t)
	slot := merchantDaySlots(testDay(0))[2]
	fakeHaoyouAPI(t, haoyouPageForSlot(slot), http.StatusOK)

	ok, empty := s.merchantFetch(slot)

	if !ok || !empty {
		t.Fatalf("merchantFetch = (%v, %v), 期望 (true, true)", ok, empty)
	}
	gotEmpty, gotData, _, ok2 := s.store.GetMerchantSlot(slot.Unix())
	if !ok2 || !gotEmpty {
		t.Errorf("无货时未写成 empty: ok=%v empty=%v", ok2, gotEmpty)
	}
	if merchantBodyHasItems(gotData) {
		t.Errorf("无货却存进了带商品的响应: %s", gotData)
	}
}

// TestMerchantFetchRefetchUpdatesGoods 重查拿到更全的货单时要**覆盖**写库 ——
// 这正是修的那个故障:20:56 那份快照多出 3 件,必须能盖掉 20:0x 那份不完整的。
//
// 比对解析后的商品而非原文:库里存的是归一化产物,与播种进去的那份字符串本来就不同形。
func TestMerchantFetchRefetchUpdatesGoods(t *testing.T) {
	s := newTestServer(t)
	slot := merchantDaySlots(testDay(0))[3]
	const first = `{"code":200,"data":{"item_count":4,"items":[{"name":"残缺魔镜"}]}}`
	if err := s.store.PutMerchantSlotAt(slot.Unix(), false, first, time.Now().Add(-time.Hour).Unix()); err != nil {
		t.Fatalf("播种首查货单: %v", err)
	}
	hits := fakeHaoyouAPI(t, haoyouPageForSlot(slot, "残缺魔镜", "魔力果"), http.StatusOK)

	ok, empty := s.merchantFetch(slot)

	if !ok || empty {
		t.Fatalf("merchantFetch = (%v, %v), 期望 (true, false)", ok, empty)
	}
	if *hits != 1 {
		t.Errorf("回源 %d 次, 期望 1 次", *hits)
	}
	_, got, _, _ := s.store.GetMerchantSlot(slot.Unix())
	if n := merchantBodyItemCount(got); n != 2 {
		t.Errorf("重查后库里有 %d 件商品, 期望 2 件(未覆盖旧货单?): %s", n, got)
	}
	if !strings.Contains(got, "魔力果") {
		t.Errorf("重查拿到的新商品「魔力果」没进库(旧货单未被覆盖): %s", got)
	}
}

// TestMerchantCurrentSlot 当前轮 = 开始时刻 ≤ now 的最后一轮。
//
// 与 TestMerchantShouldFetch 的「已结束的槽永不回源」合起来,才完整守住「只回源当前轮」:
// 更早的轮连被评估的机会都没有(cur 之前的下标一律不看),而即便被评估,它们也已结束、
// merchantShouldFetch 必然返回 false。两条独立成立,互为兜底。
//
// 抽成纯函数测而不是走 merchantEnsure:回源要打到源站,那需要额外的假桩,而这里想验的
// 只是「哪个下标是当前轮」这一条时间换算 —— 纯函数能精确地只测它。
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

// TestMerchantEnsureFetchesCurrentSlot 走**真实调用链**验证回源确实发生:
// merchantEnsure → merchantShouldFetch / force → merchantFetch → fetchHaoyou。
//
// 为什么要有它:直接调 merchantFetch 的用例绕过了 merchantEnsure 里的判定与选槽逻辑,
// 那条路径坏了(比如 force 参数没透传、当前槽算错)照样全绿。这里钉的是「玩家点刷新
// 或 merchantEnsure 判定该查时,请求真的打到源站,且写进了对应槽」。
func TestMerchantEnsureFetchesCurrentSlot(t *testing.T) {
	s := newTestServer(t)
	// 用昨天的一个进行中槽(避开别处占用的):把 now 设在槽开始后 30 分钟
	slot := merchantDaySlots(testDay(-1))[1]
	now := slot.Add(30 * time.Minute)

	var mu sync.Mutex
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, haoyouPageForSlot(slot, "残缺魔镜"))
	}))
	t.Cleanup(srv.Close)
	old := haoyouURL
	haoyouURL = srv.URL
	t.Cleanup(func() { haoyouURL = old })

	// ① force=true:前端「强制刷新」路径,必须立即回源当前轮
	s.merchantEnsure(now, true)
	mu.Lock()
	forced := hits
	mu.Unlock()
	if forced == 0 {
		t.Fatal("force=true 时 merchantEnsure 没有发起回源")
	}
	if _, _, _, ok := s.store.GetMerchantSlot(slot.Unix()); !ok {
		t.Error("force=true 回源后当前槽没有缓存")
	}

	// ② 常规路径:一个没查过的进行中槽,shouldFetch 判定要查,同样得真打到源站
	slot2 := merchantDaySlots(testDay(-1))[2]
	s.merchantEnsure(slot2.Add(30 * time.Minute))
	mu.Lock()
	total := hits
	mu.Unlock()
	if total <= forced {
		t.Errorf("常规首查没有回源(累计 %d 次,force 后已是 %d 次)", total, forced)
	}
	if _, _, _, ok := s.store.GetMerchantSlot(slot2.Unix()); !ok {
		t.Error("常规首查后该槽没有缓存")
	}
}
