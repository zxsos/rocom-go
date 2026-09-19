package server

import (
	"net/url"
	"testing"
)

// 这些用例守护的是**优先级**,不是「能不能解析出某个名字」。
// 优先级错了的代价很具体:朋友点的是 Clash 的一键按钮,却拿到一份小火箭配置,
// 而两种格式在客户端上的报错都是「配置格式错误」—— 从表象上查不出是服务端发错了。
// 改优先级顺序时这里必须红一次(变异:把 UA 提到 format 之前,第一条用例就该失败)。
func TestResolveFormat(t *testing.T) {
	cases := []struct {
		name       string
		query      url.Values
		pathFormat string
		ua         string
		accept     string
		want       string
	}{
		// ?format= 必须压过一切猜测
		{name: "format压过UA", query: url.Values{"format": []string{"clash"}}, ua: "Shadowrocket/2.2.67", want: "clash"},
		{name: "format压过路径后缀", query: url.Values{"format": []string{"sr"}}, pathFormat: "clash", want: "shadowrocket"},
		{name: "客户端名当格式名", query: url.Values{"format": []string{"hiddify"}}, want: "clash"},
		{name: "小火箭别名", query: url.Values{"format": []string{"shadowrocket"}}, want: "shadowrocket"},
		// 路径后缀(二维码里写死的那一支)
		{name: "路径后缀", pathFormat: "clash", ua: "Shadowrocket/2.2.67", want: "clash"},
		{name: "路径sr后缀", pathFormat: "sr", want: "shadowrocket"},
		// UA
		{name: "UA小火箭", ua: "Shadowrocket/2.2.67 (iPhone)", want: "shadowrocket"},
		{name: "UA小写小火箭", ua: "shadowrocket", want: "shadowrocket"},
		{name: "UAClash", ua: "Clash Meta for Android/2.11.0", want: "clash"},
		{name: "UAHiddify", ua: "Hiddify/1.0", want: "clash"},
		{name: "UA无信息量", ua: "okhttp/4.9.0", want: "clash"},
		{name: "UA空", ua: "", want: "clash"},
		// Accept 只作最后一层
		{name: "Accept yaml", ua: "curl/8.0", accept: "application/yaml", want: "clash"},
		{name: "Accept 通配", accept: "*/*", want: "clash"},
		// 非法值不该被当成「显式指定」
		{name: "format未知值回落UA", query: url.Values{"format": []string{"whatever"}}, ua: "Shadowrocket/1", want: "shadowrocket"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			q := c.query
			if q == nil {
				q = url.Values{}
			}
			if got := resolveFormat(q, c.pathFormat, c.ua, c.accept); got.ID != c.want {
				t.Fatalf("resolveFormat() = %q, want %q", got.ID, c.want)
			}
		})
	}
}

// TestIsBrowser 守护「人」与「客户端」的分流:分错了,朋友点开链接只会看到一坨 YAML
// 文本(当浏览器处理成了客户端)或者客户端拉到一张 HTML 页(反过来)。
func TestIsBrowser(t *testing.T) {
	cases := []struct {
		ua   string
		want bool
	}{
		{"Mozilla/5.0 (iPhone; CPU iPhone OS 17_0) Safari", true},
		{"", true},                 // curl / 微信内置:给人看说明页
		{"Clash Meta for Android/2.11.0", false},
		{"Shadowrocket/2.2.67", false},
		{"Hiddify/1.0", false},
	}
	for _, c := range cases {
		if got := isBrowser(c.ua); got != c.want {
			t.Fatalf("isBrowser(%q) = %v, want %v", c.ua, got, c.want)
		}
	}
}
