package hy2

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestSubscriptionClashIsValidYAML 把生成的配置真解一遍。
//
// 断言字符串拼对了不够:YAML 是**缩进敏感**的,一处缩进错了,客户端只会说
// 「配置格式错误」,而人眼盯着文本看不出是哪一行。密码那一处尤其危险 ——
// 一个没转义的引号就能把后面的结构改掉,而它不会让任何一处断言失败。
// 故这里按真实解析器走一遍,并确认密码原样取回。
func TestSubscriptionClashIsValidYAML(t *testing.T) {
	const pass = `a"b:\ c#d`
	out, err := SubscriptionClash(SubParams{Host: "example.com", Port: 11443, Password: pass, GamePort: 8195})
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	var doc struct {
		Proxies []struct {
			Name     string `yaml:"name"`
			Type     string `yaml:"type"`
			Server   string `yaml:"server"`
			Port     int    `yaml:"port"`
			Password string `yaml:"password"`
			UDP      bool   `yaml:"udp"`
		} `yaml:"proxies"`
		ProxyGroups []struct {
			Name string `yaml:"name"`
		} `yaml:"proxy-groups"`
		Rules []string `yaml:"rules"`
	}
	if err := yaml.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("生成的 YAML 解不开: %v\n%s", err, out)
	}
	if len(doc.Proxies) != 1 {
		t.Fatalf("节点数应为 1,得到 %d", len(doc.Proxies))
	}
	p := doc.Proxies[0]
	if p.Type != "hysteria2" || p.Server != "example.com" || p.Port != 11443 {
		t.Fatalf("节点字段不符: %+v", p)
	}
	// 关键:含引号/冒号/反斜杠/井号的密码必须原样取回 —— 转义漏一处就会在这里暴露。
	if p.Password != pass {
		t.Fatalf("密码没原样保留: 期望 %q,得到 %q", pass, p.Password)
	}
	if p.UDP {
		t.Fatal("udp 必须为 false:服务端 DisableUDP=true,开着会连不上")
	}
	if len(doc.ProxyGroups) != 1 || doc.ProxyGroups[0].Name != "抓包通道" {
		t.Fatalf("策略组不符: %+v", doc.ProxyGroups)
	}
	// 规则顺序:游戏端口那条必须排在 GEOIP,CN,DIRECT 之前(理由见 sub.go 顶部)。
	if len(doc.Rules) == 0 || doc.Rules[0] != "DST-PORT,8195,抓包通道" {
		t.Fatalf("首条规则应为游戏端口分流,得到 %v", doc.Rules)
	}
	last := doc.Rules[len(doc.Rules)-1]
	if last != "MATCH,DIRECT" {
		t.Fatalf("末条规则应为 MATCH,DIRECT,得到 %q", last)
	}
}

func TestSubToken(t *testing.T) {
	// 空密码没有可下发的配置,也就没有令牌 —— 调用方据此拒绝提供订阅。
	if got := SubToken(""); got != "" {
		t.Fatalf("空密码应得到空令牌,得到 %q", got)
	}
	a, b := SubToken("secret"), SubToken("secret")
	if a != b {
		t.Fatalf("同一密码应得到同一令牌(订阅地址要稳定): %q vs %q", a, b)
	}
	if len(a) != 32 {
		t.Fatalf("令牌长度应为 32 个十六进制字符,得到 %d: %q", len(a), a)
	}
	// 换密码必须换令牌:否则吊销一台设备还得去清理别处的残留地址。
	if c := SubToken("secret2"); c == a {
		t.Fatalf("换密码后令牌没变: %q", c)
	}
	// 令牌是可公开的(它就在订阅地址里),故不能等于密码本身。
	if strings.Contains(a, "secret") {
		t.Fatalf("令牌不该包含密码原文: %q", a)
	}
}

func TestSubscriptionClashRuleOrder(t *testing.T) {
	out, err := SubscriptionClash(SubParams{Host: "example.com", Port: 11443, Password: "p", Secure: true})
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	// 这条顺序断言是本次改动的**核心**:游戏服务器是国内 IP,
	// 一旦 GEOIP,CN,DIRECT(或末尾的 MATCH)排在前面,流量就走直连,
	// 表现为「手机连上了、游戏也能玩、面板上一条数据都没有」。
	// 取最后一次出现:模板头部注释里也写着 GEOIP,CN,DIRECT(就是为了提醒顺序),
	// 用 Index 会匹配到注释那一处,断言本身就成了假的。
	rule := strings.LastIndex(out, "DST-PORT,8195,抓包通道")
	geo := strings.LastIndex(out, "GEOIP,CN,DIRECT")
	if rule < 0 {
		t.Fatalf("缺少游戏端口的分流规则:\n%s", out)
	}
	if geo < 0 {
		t.Fatalf("缺少 GEOIP,CN,DIRECT 兜底规则:\n%s", out)
	}
	if rule > geo {
		t.Fatalf("分流规则排在 GEOIP,CN,DIRECT 之后,游戏流量会被放回直连")
	}
	// MATCH 必须直连:这台机器是抓包机,不是日常出口。
	if !strings.Contains(out, "MATCH,DIRECT") {
		t.Fatalf("MATCH 应为 DIRECT:\n%s", out)
	}
}

func TestSubscriptionClashGamePort(t *testing.T) {
	// 端口跟着 -port 走,而不是写死 8195:写死的话改了端口就再也抓不到。
	out, err := SubscriptionClash(SubParams{Host: "h", Port: 1, Password: "p", GamePort: 9999})
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	if !strings.Contains(out, "DST-PORT,9999,抓包通道") {
		t.Fatalf("规则没跟着 GamePort 走:\n%s", out)
	}
	// 非法 GamePort 回落到默认值,而不是生成一条谁也不匹配的规则。
	out, err = SubscriptionClash(SubParams{Host: "h", Port: 1, Password: "p", GamePort: 0})
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	if !strings.Contains(out, "DST-PORT,8195,抓包通道") {
		t.Fatalf("GamePort 未回落到默认值:\n%s", out)
	}
}

func TestSubscriptionClashSecure(t *testing.T) {
	secure, err := SubscriptionClash(SubParams{Host: "example.com", Port: 1, Password: "p", Secure: true})
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	if strings.Contains(secure, "skip-cert-verify") {
		t.Fatalf("主机能验签时不该跳过证书校验:\n%s", secure)
	}
	insecure, err := SubscriptionClash(SubParams{Host: "1.2.3.4", Port: 1, Password: "p"})
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	if !strings.Contains(insecure, "skip-cert-verify: true") {
		t.Fatalf("主机不在证书 SAN 里时必须跳过校验,否则客户端连不上:\n%s", insecure)
	}
}

func TestSubscriptionClashPasswordEscaping(t *testing.T) {
	// 密码里一个引号就能把 YAML 后续结构改掉,必须转义。
	// 这里断言的是转义后的确切形态,而不是「能跑」。
	out, err := SubscriptionClash(SubParams{Host: "h", Port: 1, Password: `a"b: c#d`})
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	want := `    password: "a\"b: c#d"`
	if !strings.Contains(out, want) {
		t.Fatalf("密码未按 YAML 双引号标量转义,期望含 %s:\n%s", want, out)
	}
}

func TestSubscriptionClashRejectsBadParams(t *testing.T) {
	cases := []struct {
		name string
		p    SubParams
	}{
		{"空密码(没有可用的节点)", SubParams{Host: "h", Port: 1}},
		{"空主机", SubParams{Port: 1, Password: "p"}},
		{"主机含空格", SubParams{Host: "a b", Port: 1, Password: "p"}},
		{"主机含斜杠", SubParams{Host: "a/b", Port: 1, Password: "p"}},
		{"端口越界", SubParams{Host: "h", Port: 70000, Password: "p"}},
		{"端口为零", SubParams{Host: "h", Port: 0, Password: "p"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if out, err := SubscriptionClash(c.p); err == nil {
				t.Fatalf("应当报错却生成了:\n%s", out)
			}
		})
	}
}

func TestSubscriptionShadowrocket(t *testing.T) {
	out, err := SubscriptionShadowrocket(SubParams{Host: "example.com", Port: 11443, Password: "p", Secure: true})
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	// .conf 的 [Proxy] 行是逗号分隔的裸值,格式错了客户端只会说「配置格式错误」。
	want := "[Proxy]\nROCOM hy2 = hysteria2, example.com, 11443, password=p\n"
	if !strings.Contains(out, want) {
		t.Fatalf("Proxy 行格式不符,期望含:\n%s\n实际:\n%s", want, out)
	}
	// 策略组:规则指向组而非节点。小火箭两侧都收,但组这一层能当场看出
	// 「现在选的是谁」——抓包失败最常见的原因就是这儿被选成了 DIRECT。
	group := "[Proxy Group]\n抓包通道 = select, ROCOM hy2, DIRECT\n"
	if !strings.Contains(out, group) {
		t.Fatalf("策略组格式不符,期望含:\n%s\n实际:\n%s", group, out)
	}
	if !strings.Contains(out, "DST-PORT,8195,抓包通道") {
		t.Fatalf("缺少把游戏端口送进代理的规则:\n%s", out)
	}
	// 同 Clash 那条:注释里也有一处 GEOIP,CN,DIRECT,故取最后一次出现(即规则行)。
	rule := strings.LastIndex(out, "DST-PORT,8195,抓包通道")
	geo := strings.LastIndex(out, "GEOIP,CN,DIRECT")
	if rule < 0 || geo < 0 || rule > geo {
		t.Fatalf("规则顺序不对,游戏流量会被放回直连:\n%s", out)
	}
	if !strings.Contains(out, "FINAL,DIRECT") {
		t.Fatalf("FINAL 应为 DIRECT:\n%s", out)
	}
}

func TestSubscriptionShadowrocketRejectsComma(t *testing.T) {
	// 裸值里出现逗号会被当成下一个字段,而客户端不会指出是哪个字段 —— 故直接拦掉。
	if _, err := SubscriptionShadowrocket(SubParams{Host: "h", Port: 1, Password: "a,b"}); err == nil {
		t.Fatal("密码含逗号时应报错")
	}
	if _, err := SubscriptionShadowrocket(SubParams{Host: "h", Port: 1, Password: "p\nx"}); err == nil {
		t.Fatal("密码含换行时应报错")
	}
	// 反过来,Clash 那边靠引号转义,同样的密码可以正常下发。
	if _, err := SubscriptionClash(SubParams{Host: "h", Port: 1, Password: "a,b"}); err != nil {
		t.Fatalf("Clash 版应能处理含逗号的密码: %v", err)
	}
}
