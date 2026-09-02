package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// 本文件锁住好游快爆源的解析与归一化。
//
// 为什么值得单独测:这个源的形态与咸鱼源完全不同(HTML 页面 vs JSON 接口),
// 而**下游四处**(有货判定 / 订阅邮件 / 前端 unwrap / 分栏)都只认咸鱼源那一种壳。
// 归一化错一点,症状不是报错而是「页面显示成奇怪的样子」或「商品掉进其他时段栏」
// —— 编译照样过,只有断言抓得住。

// haoyouDay 造一个固定营业日(2026-09-02,北京时间)。
//
// 写死日期是安全的:这里不落库(slots 不写 merchant_slots),不触发
// merchantRetain 的清理,故不会像 merchant_fetch_test.go 那样随日期淘汰。
func haoyouDay() time.Time {
	return time.Date(2026, 9, 2, 0, 0, 0, 0, haoyouLoc)
}

// haoyouPage 拼一段页面 HTML:blocks 按给定顺序输出,end 是该档结束时间(Unix 秒)。
//
// 顺序由调用方显式给定而非按 map 遍历,是为了能造出「HTML 里档期倒序出现」的样本
// —— 解析必须按时间重排,否则同档商品会散在不同位置。
//
// 刻意**照抄真实页面结构**(2026-09-02 抓的样本),尤其是那两处陷阱:
//   - 块内有**两个** <img>:商品图在 gitem 里,价格后面那枚是洛克贝币种图标。
//     限定在 gitem 内取才稳 —— 只按「块内第一个 img」取在当前顺序下也对,但页面
//     一旦调换顺序就翻车。夹具还原这个布局,才能拦住「取成图标」的实现。
//   - 类别只在行内 onclick 的 showShopinfo 第三个参数里,正文里没有。
func haoyouPage(blocks ...haoyouBlock) string {
	out := ""
	for _, b := range blocks {
		for _, g := range b.goods {
			out += `<li class="all_show li_show on  show_1  " style="display:" data-time="` + itoa(b.end) + `"  onclick="showShopinfo('` + g.image + `','` + g.name + `','` + g.kind + `','描述文字')">
                                              <div class="tp2"></div>
                                            <div class="t3"></div>

                      <div class="gitem">
                        <img src="` + g.image + `" alt="">
                        <em>` + g.limit + `</em>
                      </div>
                      <div class="sp-text">
                        <p><em class="shop_name">` + g.name + `</em></p>
                        <div><em class="shop_price">` + g.price + ` </em><img src="//res.3839pic.com/onebiji/hykb_tools/comm/lkwgmerchant/static/images/icon.png" alt=""></div>
                      </div>
                      <div class="datetime_show">
                                                <div class="sp-time">
                          <i></i>
                          <em></em>
                        </div>
                                               </div>
                    </li>
`
		}
	}
	return out
}

// haoyouBlock 页面上的一个档期(测试夹具用):结束时间 + 该档商品。
type haoyouBlock struct {
	end   int64
	goods []haoyouGood
}

// fakeHaoyouAPI 把页面地址换成 httptest 假服务,返回请求次数。
//
// 必须挡住真实请求:fetchHaoyou 打的是公开第三方站点,单测跑一次就访问一次,
// 且返回什么取决于它当时的货单,断言根本没法写。
func fakeHaoyouAPI(t *testing.T, body string, status int) *int {
	t.Helper()
	hits := new(int)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*hits++
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	old := haoyouURL
	haoyouURL = srv.URL
	t.Cleanup(func() { haoyouURL = old })
	return hits
}

// TestParseHaoyou 用一段固定 HTML 验证解析:档期换算、商品提取、排序。
//
// 为什么单独测:整个源的结论(「哪一档有什么货」)完全建立在 data-time 的语义上。
// 若把「结束时间」误当成「开始时间」,所有档期会整体偏移 4 小时,而日志看起来
// 仍然完全正常 —— 时间、商品、数量都有,只是**错的**。这类错误没有任何症状。
func TestParseHaoyou(t *testing.T) {
	day := haoyouDay()
	end1 := day.Add(12 * time.Hour).Unix() // 12:00 结束 → 档期 08:00-12:00
	end2 := day.Add(24 * time.Hour).Unix() // 次日 00:00 结束 → 档期 20:00-24:00
	// HTML 里**故意倒序**出现,用来验证输出按时间升序。
	html := haoyouPage(
		haoyouBlock{end: end2, goods: []haoyouGood{{name: "神奇的蛋"}}},
		haoyouBlock{end: end1, goods: []haoyouGood{{name: "幽系粉尘"}, {name: "毒系粉尘"}}},
	)

	got := parseHaoyou([]byte(html))
	if len(got) != 2 {
		t.Fatalf("档期数 = %d, 期望 2", len(got))
	}
	if got[0].start.Format("15:04") != "08:00" {
		t.Errorf("第一个档期 start = %s, 期望 08:00 —— 排序失效?", got[0].start.Format("15:04"))
	}
	if got[1].start.Format("15:04") != "20:00" {
		t.Errorf("第二个档期 start = %s, 期望 20:00", got[1].start.Format("15:04"))
	}
	// data-time 是**结束**时间:start = end - 4h
	if want := time.Unix(end1, 0).In(haoyouLoc); !got[0].end.Equal(want) {
		t.Errorf("第一个档期 end = %s, 期望 %s", got[0].end.Format("15:04"), want.Format("15:04"))
	}
	if d := got[0].end.Sub(got[0].start); d != 4*time.Hour {
		t.Errorf("档期时长 = %v, 期望 4h —— data-time 被当成开始时间了?", d)
	}
	// 同档期的商品归到一起
	if len(got[0].goods) != 2 {
		t.Errorf("08:00 档商品数 = %d, 期望 2", len(got[0].goods))
	}
	if len(got[1].goods) != 1 || got[1].goods[0].name != "神奇的蛋" {
		t.Errorf("20:00 档商品 = %+v, 期望 [神奇的蛋]", got[1].goods)
	}
}

// itoa 是测试里的小工具(避免为拼 HTML 引 strconv 两套用法)。
func itoa(v int64) string {
	return strconv.FormatInt(v, 10)
}

// TestParseHaoyouImageAndKind 商品图与类别的提取。
//
// 单独一条是因为这里有个**没有症状**的陷阱:块内有两枚 <img>,商品图在 gitem 里,
// 价格后面那枚是洛克贝币种图标。取错了不报错、不缺字段 —— 只是页面上每件商品的图
// 都变成同一个金币图标。名字、价格、限购全对,看不出哪里坏了。
func TestParseHaoyouImageAndKind(t *testing.T) {
	day := haoyouDay()
	end := day.Add(12 * time.Hour).Unix()
	const img = "https://patchwiki.biligame.com/images/rocom/thumb/c/c5/abcdef.png/100px-123.png"
	html := `<li class="all_show" data-time="` + itoa(end) + `" onclick="showShopinfo('` + img + `','蓝晶碧玺','炼金材料','描述')">
	  <div class="gitem">
	    <img src="` + img + `" alt="">
	    <em>限购100</em>
	  </div>
	  <div class="sp-text">
	    <p><em class="shop_name">蓝晶碧玺</em></p>
	    <div><em class="shop_price">价格：1000 </em><img src="//res.3839pic.com/.../icon.png" alt=""></div>
	  </div>
	</li>`
	got := parseHaoyou([]byte(html))
	if len(got) != 1 || len(got[0].goods) != 1 {
		t.Fatalf("解析出 %d 个档期, 期望 1 档 1 件", len(got))
	}
	g := got[0].goods[0]
	if g.image != img {
		t.Errorf("image = %q, 期望 %q —— 取到币种图标的话所有商品图都一样", g.image, img)
	}
	if strings.Contains(g.image, "icon.png") {
		t.Errorf("image 取成了洛克贝币种图标: %q", g.image)
	}
	if g.kind != "炼金材料" {
		t.Errorf("kind = %q, 期望「炼金材料」", g.kind)
	}

	// 没有 gitem / 没有 onclick 时:图与类别为空,但商品本身不该丢
	// (页面若改版去掉这两处,至少要保住名字价格限购,而不是整条商品消失)
	plain := `<li class="all_show" data-time="` + itoa(end) + `">
	  <div class="gitem"><em>限购100</em></div>
	  <p><em class="shop_name">蓝晶碧玺</em></p>
	</li>`
	got2 := parseHaoyou([]byte(plain))
	if len(got2) != 1 || len(got2[0].goods) != 1 {
		t.Fatalf("缺图缺类别时应仍解析出商品, 实际 %d 档", len(got2))
	}
	if got2[0].goods[0].image != "" || got2[0].goods[0].kind != "" {
		t.Errorf("缺图缺类别时应为空: image=%q kind=%q", got2[0].goods[0].image, got2[0].goods[0].kind)
	}
}

// TestHaoyouImageURL 协议相对 URL 要补成 https:。
//
// 页面上洛克贝图标用的就是 `//res.3839pic.com/...` 这种写法。商品图目前是完整 https,
// 但站点若改成协议相对,不补协议的话前端 imgSrc 的 /^https?:\/\// 判不出来,
// 会被当成本地相对路径拼成 /img///res... —— 图片全裂且不易定位。
func TestHaoyouImageURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://a.b/c.png", "https://a.b/c.png"},
		{"http://a.b/c.png", "http://a.b/c.png"},
		{"//res.3839pic.com/c.png", "https://res.3839pic.com/c.png"},
		{"  https://a.b/c.png  ", "https://a.b/c.png"},
		{"", ""},
	}
	for _, c := range cases {
		if got := haoyouImageURL(c.in); got != c.want {
			t.Errorf("haoyouImageURL(%q) = %q, 期望 %q", c.in, got, c.want)
		}
	}
}

// TestParseHaoyouEmpty 确认异常输入不会 panic、也不会凭空造出档期。
func TestParseHaoyouEmpty(t *testing.T) {
	for name, in := range map[string][]byte{
		"空输入":                      {},
		"无关 HTML":                  []byte(`<html><body>nothing here</body></html>`),
		"只有 all_show 没有 data-time": []byte(`<li class="all_show">x</li>`),
		"有 data-time 但没有商品名":       []byte(`<li class="all_show" data-time="1788235200">x</li>`),
	} {
		if got := parseHaoyou(in); len(got) != 0 {
			t.Errorf("%s: 返回 %d 个档期, 期望 0", name, len(got))
		}
	}
}

// TestHaoyouPrice 价格文案归一化:"16w" 必须变成 160000。
//
// 这是真实踩过的坑:页面为显示紧凑把大数写成 16w,而前端直接渲染 `{it.price} 洛克贝`
// —— 不归一化的话页面上会赫然写着「16w 洛克贝」。
func TestHaoyouPrice(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"16w", 160000},       // 页面实测:地系血脉秘药 16w
		{"1.5w", 15000},       // 带小数的万
		{"16W", 160000},       // 大写单位
		{"500", 500},          // 纯数字(幽系粉尘)
		{"价格：160000", 160000}, // 带前缀文案
		{"1,000", 1000},       // 千分位
		{" 800 ", 800},        // 首尾空白
		{"", 0},               // 缺失
		{"abc", 0},            // 非法
		{"w", 0},              // 只有单位
	}
	for _, c := range cases {
		if got := haoyouPrice(c.in); got != c.want {
			t.Errorf("haoyouPrice(%q) = %d, 期望 %d", c.in, got, c.want)
		}
	}
}

// TestHaoyouLimit 限购文案只取数字。
func TestHaoyouLimit(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"限购100", 100},
		{"限购3", 3},
		{"限购 5 ", 5},
		{"", 0},
		{"无限购", 0},
	}
	for _, c := range cases {
		if got := haoyouLimit(c.in); got != c.want {
			t.Errorf("haoyouLimit(%q) = %d, 期望 %d", c.in, got, c.want)
		}
	}
}

// TestHaoyouTimeLabel 末档(20:00 ~ 次日 00:00)的标签必须写成 20:00-24:00。
//
// 写成 20:00-00:00 也能过格式校验(^\d{2}:\d{2}-\d{2}:\d{2}$),却匹配不上前后端共用的
// 标准时段键 "20:00-24:00" —— 商品会掉进「其他时段」栏,看着像数据错了实则标签写错。
func TestHaoyouTimeLabel(t *testing.T) {
	day := haoyouDay()
	last := day.Add(20 * time.Hour)
	if got, want := haoyouTimeLabel(last, last.Add(4*time.Hour)), "20:00-24:00"; got != want {
		t.Errorf("末档标签 = %q, 期望 %q(写成 20:00-00:00 会掉进「其他时段」栏)", got, want)
	}
	noon := day.Add(8 * time.Hour)
	if got, want := haoyouTimeLabel(noon, noon.Add(4*time.Hour)), "08:00-12:00"; got != want {
		t.Errorf("首档标签 = %q, 期望 %q", got, want)
	}
}

// TestFetchHaoyouPicksRequestedSlot 一次只取请求的那一档,且归一化成咸鱼源同形。
//
// 「只取当前轮」是本次定下的口径(见 docs/data.md):页面会给出多个档,但我们一次
// 只填一个 4h 槽 —— 若把别的档也写进来,就与「已结束的槽永不回源」那条硬规则冲突了
// (拿回来的不是该槽当时的货单)。
func TestFetchHaoyouPicksRequestedSlot(t *testing.T) {
	s := newTestServer(t)
	day := haoyouDay()
	// 页面上两档:08:00-12:00(2 件)与 20:00-24:00(1 件)
	page := haoyouPage(
		haoyouBlock{end: day.Add(12 * time.Hour).Unix(), goods: []haoyouGood{
			{name: "蓝晶碧玺", price: "1000", limit: "限购100", kind: "炼金材料",
				image: "https://patchwiki.biligame.com/images/rocom/thumb/c/c5/k8wzdzechd88qs9bxuvcnyg0pv4tzl4.png/100px-100681.png"},
			{name: "地系血脉秘药", price: "16w", limit: "限购3", kind: "血脉修改道具",
				image: "https://patchwiki.biligame.com/images/rocom/thumb/1/17/808i7ggttfttqex.png/100px-999999.png"},
		}},
		haoyouBlock{end: day.Add(24 * time.Hour).Unix(), goods: []haoyouGood{
			{name: "神奇的蛋", price: "36000", limit: "限购5", kind: "精灵蛋",
				image: "https://patchwiki.biligame.com/images/rocom/thumb/2/28/abcdef.png/100px-111111.png"},
		}},
	)
	fakeHaoyouAPI(t, page, http.StatusOK)

	body, ok, _ := s.fetchHaoyou(day.Add(8*time.Hour), true)
	if !ok {
		t.Fatal("fetchHaoyou 失败(应拿到正常响应)")
	}
	// 下游判定必须能认出这是有货的响应 —— 归一化是否同形,这一句就是判据。
	if !merchantBodyHasItems(body) {
		t.Fatalf("归一化结果未被判成有货: %s", body)
	}
	var env struct {
		Code int `json:"code"`
		Data struct {
			ItemCount    int            `json:"item_count"`
			MerchantName string         `json:"merchant_name"`
			Items        []merchantItem `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("归一化结果不是合法 JSON: %v\n%s", err, body)
	}
	if len(env.Data.Items) != 2 {
		t.Fatalf("取到 %d 件商品, 期望只取 08:00 那档的 2 件: %+v", len(env.Data.Items), env.Data.Items)
	}
	if env.Data.ItemCount != 2 {
		t.Errorf("item_count = %d, 期望 2", env.Data.ItemCount)
	}
	if env.Data.MerchantName == "" {
		t.Error("merchant_name 为空(邮件抬头会缺商人名)")
	}
	byName := map[string]merchantItem{}
	for _, it := range env.Data.Items {
		byName[it.Name] = it
	}
	gem, ok := byName["蓝晶碧玺"]
	if !ok {
		t.Fatalf("缺商品「蓝晶碧玺」: %+v", env.Data.Items)
	}
	if gem.Price != 1000 || gem.Limit != 100 {
		t.Errorf("蓝晶碧玺 = 价格%d 限购%d, 期望 1000/100", gem.Price, gem.Limit)
	}
	if gem.TimeLabel != "08:00-12:00" {
		t.Errorf("蓝晶碧玺 time_label = %q, 期望 08:00-12:00", gem.TimeLabel)
	}
	med, ok := byName["地系血脉秘药"]
	if !ok {
		t.Fatalf("缺商品「地系血脉秘药」: %+v", env.Data.Items)
	}
	// 页面原文是 16w —— 不归一化的话前端会显示「16w 洛克贝」
	if med.Price != 160000 {
		t.Errorf("地系血脉秘药价格 = %d, 期望 160000(页面原文 16w)", med.Price)
	}
	if med.Limit != 3 {
		t.Errorf("地系血脉秘药限购 = %d, 期望 3", med.Limit)
	}
	// 商品图:必须是 gitem 里那张,不是价格后面那枚洛克贝币种图标
	wantImg := "https://patchwiki.biligame.com/images/rocom/thumb/c/c5/k8wzdzechd88qs9bxuvcnyg0pv4tzl4.png/100px-100681.png"
	if gem.Image != wantImg {
		t.Errorf("蓝晶碧玺 image = %q, 期望 %q(取到币种图标的话每件商品图都一样)", gem.Image, wantImg)
	}
	if strings.Contains(gem.Image, "icon.png") || strings.HasPrefix(gem.Image, "/img/") {
		t.Errorf("商品图取错了: %q —— 块内第二个 img 是洛克贝币种图标,不是商品图", gem.Image)
	}
	if med.Image == "" {
		t.Error("地系血脉秘药 image 为空(该源是提供商品图的)")
	}
	// 类别:页面给的就是中文,原样入库(前端 kindText 与邮件映射对未收录值原样返回)
	if gem.Kind != "炼金材料" {
		t.Errorf("蓝晶碧玺 kind = %q, 期望「炼金材料」", gem.Kind)
	}
	if med.Kind != "血脉修改道具" {
		t.Errorf("地系血脉秘药 kind = %q, 期望「血脉修改道具」", med.Kind)
	}
	// start_time/end_time 是毫秒,且按北京时间读出来正好是该档(与咸鱼源口径一致)
	if got := time.UnixMilli(gem.StartTime).In(merchantLoc).Format("15:04"); got != "08:00" {
		t.Errorf("start_time 读出 %s, 期望 08:00", got)
	}
	if got := time.UnixMilli(gem.EndTime).In(merchantLoc).Format("15:04"); got != "12:00" {
		t.Errorf("end_time 读出 %s, 期望 12:00", got)
	}

	// 反向再取一次另一档:只是「挑到了某一档」还不够 —— 直接取页面第一档的实现
	// 也能过上面的断言。必须证明**请求的那一档**才被取出来。
	body2, ok, _ := s.fetchHaoyou(day.Add(20*time.Hour), true)
	if !ok {
		t.Fatal("取 20:00 档失败")
	}
	var env2 struct {
		Data struct {
			Items []merchantItem `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body2), &env2); err != nil {
		t.Fatalf("解析: %v", err)
	}
	if len(env2.Data.Items) != 1 || env2.Data.Items[0].Name != "神奇的蛋" {
		t.Errorf("取 20:00 档得到 %+v, 期望只有「神奇的蛋」一件", env2.Data.Items)
	}
}

// TestFetchHaoyouLastSlotLabel 末档商品的时段标签必须是 20:00-24:00。
//
// 单独一条是因为它太容易写错又太不容易被发现:标签照写 20:00-00:00 时,JSON 结构、
// 商品、价格全对,只有前端分栏会把它丢进「其他时段」。
func TestFetchHaoyouLastSlotLabel(t *testing.T) {
	s := newTestServer(t)
	day := haoyouDay()
	last := day.Add(20 * time.Hour)
	page := haoyouPage(haoyouBlock{
		end:   last.Add(4 * time.Hour).Unix(),
		goods: []haoyouGood{{name: "神奇的蛋", price: "36000", limit: "限购5"}},
	})
	fakeHaoyouAPI(t, page, http.StatusOK)

	body, ok, _ := s.fetchHaoyou(last, true)
	if !ok {
		t.Fatal("fetchHaoyou 失败")
	}
	var env struct {
		Data struct {
			Items []merchantItem `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("解析: %v", err)
	}
	if len(env.Data.Items) != 1 {
		t.Fatalf("商品数 = %d, 期望 1", len(env.Data.Items))
	}
	if got := env.Data.Items[0].TimeLabel; got != "20:00-24:00" {
		t.Errorf("末档 time_label = %q, 期望 20:00-24:00", got)
	}
	// 后端邮件分组与前端共用同一套标准段,这里直接断言它确实被分进了 20:00-24:00 组
	allDay, groups, other := merchantGroupItems(env.Data.Items)
	if len(allDay) != 0 || len(other) != 0 {
		t.Errorf("末档商品不该落进全天/其他栏: allDay=%d other=%d", len(allDay), len(other))
	}
	if len(groups) != 1 || groups[0].Title != "20:00-24:00" {
		t.Errorf("分组 = %+v, 期望只有 20:00-24:00 一组", groups)
	}
}

// TestFetchHaoyouMissingSlotIsEmpty 页面上没有请求的那一档时,按「查过但无货」返回
// (ok=true),而不是当成故障(ok=false)。
//
// 休市时段(0-8 点)页面只显示昨天全天,当天档要等开市后才出现 —— 那是正常的
// 「此刻无货」,不是抓取失败。判成失败会让 merchantShouldFetch 认「从未查过」,
// 下一 tick 立刻再抓,白白多打第三方。
func TestFetchHaoyouMissingSlotIsEmpty(t *testing.T) {
	s := newTestServer(t)
	day := haoyouDay()
	page := haoyouPage(haoyouBlock{
		end:   day.Add(12 * time.Hour).Unix(),
		goods: []haoyouGood{{name: "蓝晶碧玺", price: "1000", limit: "限购100"}},
	})
	fakeHaoyouAPI(t, page, http.StatusOK)

	body, ok, _ := s.fetchHaoyou(day.Add(16*time.Hour), true) // 页面没有 16:00 这一档
	if !ok {
		t.Fatal("页面缺该档应判为「无货」(ok=true),而不是失败")
	}
	if merchantBodyHasItems(body) {
		t.Errorf("无货时 items 应为空: %s", body)
	}
}

// TestFetchHaoyouHTTPError 抓取失败必须报 ok=false,让调用方不写库
// (写空货单会把已有数据盖掉,写失败响应更糟)。
func TestFetchHaoyouHTTPError(t *testing.T) {
	s := newTestServer(t)
	day := haoyouDay()
	fakeHaoyouAPI(t, "", http.StatusInternalServerError)

	if body, ok, _ := s.fetchHaoyou(day.Add(8*time.Hour), true); ok {
		t.Errorf("HTTP 500 时应返回 ok=false, 实际 ok=true body=%q", body)
	}
}

// TestFetchHaoyouSendsUA 抓取必须带 User-Agent。
//
// 空 UA 会被站点 403,而「抓取失败」与「页面没数据」在日志里长得一模一样 ——
// 不查根因就会误判成站点改版,然后去改根本没问题的正则。
func TestFetchHaoyouSendsUA(t *testing.T) {
	s := newTestServer(t)
	day := haoyouDay()
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("User-Agent")
		_, _ = io.WriteString(w, haoyouPage(haoyouBlock{
			end:   day.Add(12 * time.Hour).Unix(),
			goods: []haoyouGood{{name: "蓝晶碧玺", price: "1000", limit: "限购100"}},
		}))
	}))
	t.Cleanup(srv.Close)
	old := haoyouURL
	haoyouURL = srv.URL
	t.Cleanup(func() { haoyouURL = old })

	if _, ok, _ := s.fetchHaoyou(day.Add(8*time.Hour), true); !ok {
		t.Fatal("fetchHaoyou 失败")
	}
	if got == "" {
		t.Error("抓取未带 User-Agent —— 站点会返回 403,且症状与「页面没数据」相同")
	}
}
