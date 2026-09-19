package hy2

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"strings"
	"testing"
	"time"
)

func TestParseAdvertise(t *testing.T) {
	cases := []struct {
		in   string
		host string
		port int
		bad  bool
	}{
		{in: "", host: "", port: 0},
		{in: "  ", host: "", port: 0},
		{in: "rocom.example", host: "rocom.example", port: 0},
		{in: "rocom.example:50000", host: "rocom.example", port: 50000},
		{in: ":50000", host: "", port: 50000},         // 只固定端口(NAT 映射)
		{in: "[::1]:11443", host: "::1", port: 11443}, // IPv6 带端口
		{in: "::1", host: "::1", port: 0},             // IPv6 无端口
		{in: "1.2.3.4", host: "1.2.3.4", port: 0},
		{in: "host:0", bad: true}, // 0 只在监听端口里表示随机,对外地址里没意义
		{in: "host:99999", bad: true},
		{in: "host:abc", bad: true},
	}
	for _, c := range cases {
		got, err := ParseAdvertise(c.in)
		if c.bad {
			if err == nil {
				t.Errorf("ParseAdvertise(%q) 应报错,得到 %+v", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseAdvertise(%q): %v", c.in, err)
			continue
		}
		if got.Host != c.host || got.Port != c.port {
			t.Errorf("ParseAdvertise(%q) = %+v, want host=%q port=%d", c.in, got, c.host, c.port)
		}
	}
}

func TestLink(t *testing.T) {
	got, err := Link(LinkParams{Host: "203.0.113.10", Port: 11443, Password: "pw123"})
	if err != nil {
		t.Fatal(err)
	}
	// 主机不是证书里的名字 → 必须带 insecure=1,否则客户端验签必失败、导入即连不上
	want := "hysteria2://pw123@203.0.113.10:11443/?insecure=1#rocom"
	if got != want {
		t.Errorf("链接 = %q, want %q", got, want)
	}

	if got, err = Link(LinkParams{Host: "rocom.example", Port: 443, Password: "pw", Secure: true}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "insecure") {
		t.Errorf("可验签时不该带 insecure: %q", got)
	}

	// 密码含保留字符:不编码就会截断主机名(表现是「连到半个域上」,极难倒推)
	if got, err = Link(LinkParams{Host: "h.example", Port: 1, Password: "p@ss/w?d"}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "hysteria2://p%40ss%2Fw%3Fd@h.example:1") {
		t.Errorf("密码未编码: %q", got)
	}

	if got, err = Link(LinkParams{Host: "h.example", Port: 1}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "@") {
		t.Errorf("无密码时不应出现认证段: %q", got)
	}

	// IPv6 字面量必须加方括号,否则端口分隔符与地址里的冒号混在一起
	if got, err = Link(LinkParams{Host: "2001:db8::1", Port: 11443, Password: "p"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "@[2001:db8::1]:11443") {
		t.Errorf("IPv6 未加方括号: %q", got)
	}

	for _, bad := range []LinkParams{
		{Host: "", Port: 1},
		{Host: "a b.example", Port: 1}, // 空格会截断 URI
		{Host: "u@h.example", Port: 1}, // 会被当成认证段
		{Host: "h.example/path", Port: 1},
		{Host: "h.example", Port: 0},
		{Host: "h.example", Port: 70000},
	} {
		if _, err := Link(bad); err == nil {
			t.Errorf("Link(%+v) 应报错", bad)
		}
	}
}

func TestLinkLabel(t *testing.T) {
	got, err := Link(LinkParams{Host: "h.example", Port: 1, Password: "p", Label: "我的 代理#1"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(got, "#我的-代理-1") {
		t.Errorf("显示名未净化: %q", got)
	}
	if strings.Count(got, "#") != 1 {
		t.Errorf("片段里不应再有 #: %q", got)
	}
}

// TestSecureFor 守「要不要带 insecure」这个判断:它是链接里唯一的安全开关。
func TestSecureFor(t *testing.T) {
	m := NewManager()
	if m.SecureFor("rocom.example") {
		t.Error("没有证书时应判为不可验签(必须带 insecure)")
	}
	m.SetCert(testCertWithNames(t, []string{"rocom.example", "*.wild.example"}))
	for host, want := range map[string]bool{
		"rocom.example":    true,  // SAN 里有
		"sub.wild.example": true,  // 通配符命中
		"a.b.wild.example": false, // 通配符只吃一级
		"other.example":    false,
		"127.0.0.1":        false, // 证书没有 IP SAN
	} {
		if got := m.SecureFor(host); got != want {
			t.Errorf("SecureFor(%q) = %v, want %v", host, got, want)
		}
	}
}

func testCertWithNames(t *testing.T, dns []string) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(7),
		Subject:      pkix.Name{CommonName: dns[0]},
		DNSNames:     dns,
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}
