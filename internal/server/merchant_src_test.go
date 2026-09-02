package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/whoisnian/rocom-capture/internal/gamedata"
	"github.com/whoisnian/rocom-capture/internal/store"
)

// newTestServerFrom 用既有的库另起一个 Server(模拟「服务重启」)。
//
// 与 newTestServer 的差别只有一点:复用传入的 store,这样上一个 Server 写进库里的
// 配置能被读出来 —— 「重启后源是否还在」只有这样才能测。
func newTestServerFrom(t *testing.T, st *store.Store) *Server {
	t.Helper()
	db, err := gamedata.Load()
	if err != nil {
		t.Fatalf("加载名称库: %v", err)
	}
	return New(st, NewHub(), db, "", "", "") // 测试不涉及查蛋/邮件,三个令牌留空
}

// 本文件锁住「数据源可切换」这条链路,尤其是三件只在这条链路上成立的事:
//
//  1. **切源必须清缓存** —— 不清的话,另一源格式的旧货单会被当成新源的数据显示,
//     而页面顶部的来源标注在那一刻就开始说谎了。这种错不会报错,只会让人看着
//     一份来路不明的货单以为它是新的。
//  2. **好游快爆源无需令牌** —— 这是它相对咸鱼源的全部价值。若哪天「未配令牌直接
//     返回 503」的短路被加回来(或漏了这个源),这条链路上最关键的用途就没了,
//     而编译、契约全都照样通过。
//  3. **源的持久性** —— 落库而非只改内存,否则服务一重启就悄悄回到默认源,
//     管理员会以为自己切过的源还生效着。

// stubHaoyou 把好游快爆页面换成 httptest 假服务。
//
// 必须挡住真实请求:切源会异步按新源重抓当前轮,不换掉就会真去打第三方站点
// (既脏了对方的访问统计,也让断言取决于它当时的货单)。
func stubHaoyou(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `<li class="all_show_x" data-time="0"></li>`)
	}))
	t.Cleanup(srv.Close)
	old := haoyouURL
	haoyouURL = srv.URL
	t.Cleanup(func() { haoyouURL = old })
}

// TestAdminMerchantSourceSwitch 走 HTTP 入口验证切源的完整后果。
func TestAdminMerchantSourceSwitch(t *testing.T) {
	s := newTestServer(t)
	stubHaoyou(t)
	token := testAdminToken(t, s)

	adminGet := func() map[string]any {
		t.Helper()
		rr := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/admin/merchant-source", nil)
		req.Header.Set("X-Admin-Token", token)
		s.Handler().ServeHTTP(rr, req)
		if rr.Code != 200 {
			t.Fatalf("GET 数据源 = %d: %s", rr.Code, rr.Body)
		}
		var got map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
			t.Fatalf("解析: %v", err)
		}
		return got
	}
	adminPost := func(src string) int {
		t.Helper()
		rr := httptest.NewRecorder()
		req := httptest.NewRequest("POST", "/api/admin/merchant-source",
			strings.NewReader(`{"source":"`+src+`"}`))
		req.Header.Set("X-Admin-Token", token)
		s.Handler().ServeHTTP(rr, req)
		return rr.Code
	}

	// ① 默认源是咸鱼源,且未配令牌(keySet=false)
	got := adminGet()
	if got["source"] != merchantSrcXianyu {
		t.Errorf("默认源 = %v, 期望 %s", got["source"], merchantSrcXianyu)
	}
	if got["keySet"] != false {
		t.Errorf("keySet = %v, 期望 false(newTestServer 不带 -egg-api-key)", got["keySet"])
	}
	sources, _ := got["sources"].([]any)
	if len(sources) != 2 {
		t.Errorf("源清单 = %d 个, 期望 2", len(sources))
	}

	// ② 未配令牌 + 咸鱼源:商人接口应当不可用(503)—— 这是切源的动机所在
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest("GET", "/api/merchant", nil))
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("咸鱼源未配令牌时 GET /api/merchant = %d, 期望 503", rr.Code)
	}

	// ③ 播种一份槽缓存,切源后它必须消失。
	//
	// 故意用**昨天**的槽而非今天的:切源会异步重抓当前轮并写回今天的槽,
	// 用今天的槽会被那个 goroutine 重新创建出来,断言就随机失败(取决于
	// 测试跑在哪个时段)。昨天的槽不会被 merchantEnsure 触碰(它只看当前轮)。
	slot := merchantDaySlots(merchantDayStart(time.Now()).AddDate(0, 0, -1))[0]
	const goods = `{"code":200,"data":{"item_count":1,"items":[{"name":"残缺魔镜"}]}}`
	if err := s.store.PutMerchantSlot(slot.Unix(), false, goods); err != nil {
		t.Fatalf("播种槽缓存: %v", err)
	}

	// 非法标识必须被拒,且**不能**改动当前配置
	if code := adminPost("nope"); code != http.StatusBadRequest {
		t.Errorf("切到未知源 = %d, 期望 400", code)
	}
	if s.merchantSource() != merchantSrcXianyu {
		t.Errorf("被拒的切换仍改了当前源: %s", s.merchantSource())
	}

	if code := adminPost(merchantSrcHaoyou); code != http.StatusOK {
		t.Fatalf("切到好游快爆源 = %d: 期望 200", code)
	}
	if got := s.merchantSource(); got != merchantSrcHaoyou {
		t.Fatalf("切换后当前源 = %s, 期望 %s", got, merchantSrcHaoyou)
	}
	if _, _, _, ok := s.store.GetMerchantSlot(slot.Unix()); ok {
		t.Error("切源后旧槽缓存仍在 —— 另一源格式的货单会被当成新源数据显示,来源标注也在说谎")
	}

	// ④ 切到好游快爆后,未配令牌也应当能查(这正是它的价值)
	rr = httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest("GET", "/api/merchant", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("好游快爆源未配令牌时 GET /api/merchant = %d, 期望 200(该源无需令牌)", rr.Code)
	}
	var out struct {
		Source string `json:"source"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("解析: %v", err)
	}
	if out.Source != merchantSrcHaoyou {
		t.Errorf("响应 source = %q, 期望 %q(前端据此标注来源,错了就是标注说谎)",
			out.Source, merchantSrcHaoyou)
	}

	// ⑤ 切回咸鱼源:未配令牌时又该不可用,证明切换是双向生效的
	if code := adminPost(merchantSrcXianyu); code != http.StatusOK {
		t.Fatalf("切回咸鱼源 = %d, 期望 200", code)
	}
	rr = httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest("GET", "/api/merchant", nil))
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("切回咸鱼源后 = %d, 期望 503", rr.Code)
	}
}

// TestMerchantSourcePersists 源要落库:新起一个 Server 读同一个库,应拿到切过的源。
//
// 只改内存镜像的实现能过上面那条测试(同一进程内看不出来),却会在重启后悄悄回到
// 默认源 —— 管理员以为自己切过的源还生效着,看到的是另一份数据。
func TestMerchantSourcePersists(t *testing.T) {
	s := newTestServer(t)
	stubHaoyou(t)
	if err := s.store.SetMerchantSource(merchantSrcHaoyou); err != nil {
		t.Fatalf("写入源配置: %v", err)
	}
	if got := s.store.MerchantSource(); got != merchantSrcHaoyou {
		t.Fatalf("库里读回 = %q, 期望 %q", got, merchantSrcHaoyou)
	}
	// 另起一个 Server(模拟重启),应载入库里的源而非默认源
	s2 := newTestServerFrom(t, s.store)
	if got := s2.merchantSource(); got != merchantSrcHaoyou {
		t.Errorf("重启后源 = %s, 期望 %s —— 未落库的话重启会悄悄回到默认源", got, merchantSrcHaoyou)
	}
}

// TestMerchantSourceValid 标识合法性与默认回退。
func TestMerchantSourceValid(t *testing.T) {
	for _, ok := range []string{merchantSrcXianyu, merchantSrcHaoyou} {
		if !merchantSourceValid(ok) {
			t.Errorf("merchantSourceValid(%q) = false, 期望 true", ok)
		}
	}
	for _, bad := range []string{"", "nope", "XIANYU", "haoyou "} {
		if merchantSourceValid(bad) {
			t.Errorf("merchantSourceValid(%q) = true, 期望 false", bad)
		}
	}
	// 库里的非法值(老数据/手改过)必须回退默认源,而不是原样生效
	s := newTestServer(t)
	if err := s.store.SetMerchantSource("garbage"); err != nil {
		t.Fatalf("写入: %v", err)
	}
	s2 := newTestServerFrom(t, s.store)
	if got := s2.merchantSource(); got != merchantSrcDefault {
		t.Errorf("库里是非法值时源 = %s, 期望回退默认 %s", got, merchantSrcDefault)
	}
	// 展示名:未知标识原样返回(面板显示原始值,便于排查)
	if got := merchantSourceName("garbage"); got != "garbage" {
		t.Errorf("merchantSourceName(未知) = %q, 期望原样返回", got)
	}
}
