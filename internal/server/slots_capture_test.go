package server

import (
	"testing"
	"time"
)

// 本文件锁住 onebiji 页面解析的正确性 —— 尤其是**档期时间的换算**。
//
// 为什么单独测:整个观察的结论(「下一档几点出现」)完全建立在 data-time 的
// 语义上。若把「结束时间」误当成「开始时间」,所有档期会整体偏移 4 小时,
// 而日志看起来仍然完全正常 —— 时间、商品、数量都有,只是**错的**。
// 这类错误没有任何症状,只有断言能抓住。

// TestParseOnebiji 用一段固定 HTML 验证解析:档期换算、商品提取、排序。
//
// data-time=1788235200 = 2026-09-01 12:00:00 UTC+8,是 08:00-12:00 这一档的
// **结束时间**;故解析出的 start 应为 08:00、end 应为 12:00。
func TestParseOnebiji(t *testing.T) {
	// 两个档期:08-12(2 件)与 20-24(1 件),且 HTML 里**故意倒序**出现,
	// 用来验证输出按时间升序(日志里档期顺序必须稳定,否则人工比对会看串)。
	end1 := int64(1788235200) // 2026-09-01 12:00 +0800 → 档期 08:00-12:00
	end2 := int64(1788278400) // 2026-09-02 00:00 +0800 → 档期 20:00-24:00
	html := []byte(`
		<li class="all_show_x" data-time="` + itoa(end2) + `">
			<span class="shop_name">神奇的蛋</span>
			<span class="shop_price">36000</span><em>限购5</em>
		</li>
		<li class="all_show_x" data-time="` + itoa(end1) + `">
			<span class="shop_name">幽系粉尘</span>
			<span class="shop_price">500</span><em>限购100</em>
		</li>
		<li class="all_show_x" data-time="` + itoa(end1) + `">
			<span class="shop_name">毒系粉尘</span>
			<span class="shop_price">500</span><em>限购100</em>
		</li>
	`)
	got := parseOnebiji(html)
	if len(got) != 2 {
		t.Fatalf("档期数 = %d, 期望 2", len(got))
	}

	loc := time.FixedZone("UTC+8", 8*3600)
	// 1. 排序:先出现的应是 08:00 那档(HTML 里它在后面)
	if got[0].start.Format("15:04") != "08:00" {
		t.Errorf("第一个档期 start = %s, 期望 08:00 —— 排序失效?", got[0].start.Format("15:04"))
	}
	if got[1].start.Format("15:04") != "20:00" {
		t.Errorf("第二个档期 start = %s, 期望 20:00", got[1].start.Format("15:04"))
	}
	// 2. data-time 是**结束**时间:start = end - 4h
	if want := time.Unix(end1, 0).In(loc); !got[0].end.Equal(want) {
		t.Errorf("第一个档期 end = %s, 期望 %s", got[0].end.Format("15:04"), want.Format("15:04"))
	}
	if d := got[0].end.Sub(got[0].start); d != 4*time.Hour {
		t.Errorf("档期时长 = %v, 期望 4h —— data-time 被当成开始时间了?", d)
	}
	// 3. 同档期的商品归到一起
	if len(got[0].goods) != 2 {
		t.Errorf("08:00 档商品数 = %d, 期望 2", len(got[0].goods))
	}
	if len(got[1].goods) != 1 {
		t.Errorf("20:00 档商品数 = %d, 期望 1", len(got[1].goods))
	}
	// 4. 商品字段
	if got[0].goods[0].name != "幽系粉尘" {
		t.Errorf("商品名 = %q, 期望 %q", got[0].goods[0].name, "幽系粉尘")
	}
	if got[1].goods[0].name != "神奇的蛋" {
		t.Errorf("商品名 = %q, 期望 %q", got[1].goods[0].name, "神奇的蛋")
	}
}

// TestParseOnebijiEmpty 确认异常输入不会 panic、也不会凭空造出档期。
// 页面结构变了(选择器失效)时返回空,由调用方打印「解析到 0 件」——
// 这比 panic 崩溃或返回半截数据都好。
func TestParseOnebijiEmpty(t *testing.T) {
	for name, in := range map[string][]byte{
		"空输入":                      {},
		"无关 HTML":                  []byte(`<html><body>nothing here</body></html>`),
		"只有 all_show 没有 data-time": []byte(`<li class="all_show">x</li>`),
		"有 data-time 但没有商品名":       []byte(`<li class="all_show" data-time="1788235200">x</li>`),
	} {
		if got := parseOnebiji(in); len(got) != 0 {
			t.Errorf("%s: 返回 %d 个档期, 期望 0", name, len(got))
		}
	}
}

// itoa 是测试里的小工具(避免为拼 HTML 引 strconv 两套用法)。
func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	var buf []byte
	for v > 0 {
		buf = append([]byte{byte('0' + v%10)}, buf...)
		v /= 10
	}
	return string(buf)
}
