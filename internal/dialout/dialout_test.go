package dialout

import (
	"context"
	"net"
	"net/netip"
	"strconv"
	"testing"
	"time"
)

func TestSplitList(t *testing.T) {
	got := SplitList(" a.com , ,b.com,, ")
	if len(got) != 2 || got[0] != "a.com" || got[1] != "b.com" {
		t.Errorf("SplitList = %q,期望 [a.com b.com](去空与空白)", got)
	}
	if n := len(SplitList("")); n != 0 {
		t.Errorf("空串应得空切片,实际 %d", n)
	}
}

func TestParseAllow(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantLen int
		wantErr bool
	}{
		{"空串", "", 0, false},
		{"单个 IP", "1.2.3.4", 1, false},
		{"IP 与 CIDR 混排", "1.2.3.4, 10.0.0.0/8 ,", 2, false},
		{"非法 IP", "not-an-ip", 0, true},
		{"非法 CIDR", "10.0.0.0/33", 0, true},
		{"IPv6", "2001:db8::1", 1, false},
	}
	for _, c := range cases {
		got, err := ParseAllow(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("%s: err=%v, wantErr=%v", c.name, err, c.wantErr)
			continue
		}
		if len(got) != c.wantLen {
			t.Errorf("%s: 得到 %d 条,期望 %d", c.name, len(got), c.wantLen)
		}
	}
}

// TestAllowed 守两件事:白名单为空放行一切;认不出的地址一律**拒绝**而非放行。
func TestAllowed(t *testing.T) {
	allow, err := ParseAllow("127.0.0.1,10.0.0.0/8")
	if err != nil {
		t.Fatal(err)
	}
	udp := func(ip string) net.Addr { return &net.UDPAddr{IP: net.ParseIP(ip), Port: 1} }
	tcp := func(ip string) net.Addr { return &net.TCPAddr{IP: net.ParseIP(ip), Port: 1} }

	cases := []struct {
		name string
		addr net.Addr
		want bool
	}{
		{"UDP 命中", udp("127.0.0.1"), true},
		{"TCP 命中", tcp("127.0.0.1"), true},
		{"CIDR 命中", udp("10.9.9.9"), true},
		{"未命中", udp("203.0.113.9"), false},
		{"认不出的地址类型", &net.UnixAddr{Name: "/tmp/x", Net: "unix"}, false},
		{"IPv4 映射的 IPv6 也认", &net.TCPAddr{IP: net.ParseIP("::ffff:127.0.0.1"), Port: 1}, true},
	}
	for _, c := range cases {
		if got := Allowed(c.addr, allow); got != c.want {
			t.Errorf("%s: Allowed = %v,期望 %v", c.name, got, c.want)
		}
	}
	// 空白名单 = 不限制
	if !Allowed(udp("203.0.113.9"), nil) {
		t.Error("白名单为空时应放行一切")
	}
}

func TestBlockedHost(t *testing.T) {
	block := []string{"google.com", "example.com"}
	cases := []struct {
		host string
		want bool
	}{
		{"google.com", true},
		{"Google.Com", true},     // 大小写不敏感
		{"www.google.com", true}, // 子域
		{"google.com.", true},    // 容忍末尾点
		{"notgoogle.com", false}, // 不能只按后缀匹配
		{"example.org", false},
		{"8.8.8.8", false}, // IP 不参与域名屏蔽
	}
	for _, c := range cases {
		if got := BlockedHost(c.host, block); got != c.want {
			t.Errorf("BlockedHost(%q) = %v,期望 %v", c.host, got, c.want)
		}
	}
	if BlockedHost("google.com", nil) {
		t.Error("屏蔽名单为空时不应屏蔽任何目标")
	}
}

// TestDialAddrsSuccess 正常路径:唯一候选可达时直接返回该连接。
func TestDialAddrsSuccess(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err == nil {
			c.Close()
		}
	}()
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	conn, err := DialAddrs(context.Background(), []netip.Addr{netip.MustParseAddr("127.0.0.1")}, port)
	if err != nil {
		t.Fatalf("DialAddrs: %v", err)
	}
	conn.Close()
}

// TestDialAddrsPicksReachable 守 happy eyeballs 的关键路径:**首个地址快速失败时要落到后面的地址**,
// 而不是把首个的错误直接返回 —— 跨地域云服务器上「某运营商黑洞掉部分 IP」正是这个场景,
// 串行逐个尝试会让客户端空等到拨号超时。
//
// 用 127.0.0.2 作为必然失败的候选(Linux 下整个 127/8 都是回环,但那里没有监听,
// 会立即 RST),127.0.0.1 才是真能连上的那个。
func TestDialAddrsPicksReachable(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err == nil {
			c.Close()
		}
	}()
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := DialAddrs(ctx, []netip.Addr{
		netip.MustParseAddr("127.0.0.2"), // 无监听:快速失败
		netip.MustParseAddr("127.0.0.1"), // 可达
	}, port)
	if err != nil {
		t.Fatalf("首个候选失败后应回退到可达地址: %v", err)
	}
	defer conn.Close()
	if got := conn.RemoteAddr().(*net.TCPAddr).IP.String(); got != "127.0.0.1" {
		t.Errorf("连到了 %s,期望回退到 127.0.0.1", got)
	}
}

func TestDialAddrsErrors(t *testing.T) {
	ctx := context.Background()
	if _, err := DialAddrs(ctx, nil, "80"); err == nil {
		t.Error("空地址列表应报错")
	}
	// 全部不可达:返回错误而不是 panic 或挂住
	if _, err := DialAddrs(ctx, []netip.Addr{netip.MustParseAddr("127.0.0.1")}, "1"); err == nil {
		t.Error("全部地址不可达时应返回错误")
	}
}

// TestDialTargetBadTarget 守入站侧脏数据:目标不是 host:port 时直接报错,不能 panic。
func TestDialTargetBadTarget(t *testing.T) {
	if _, err := DialTarget(context.Background(), "no-port-here"); err == nil {
		t.Error("缺少端口的目标应报错")
	}
}

func TestTuneTCP(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err == nil {
			time.Sleep(50 * time.Millisecond)
			c.Close()
		}
	}()
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	// 非 TCP 连接与非 Linux 平台都不应 panic
	TuneTCP(c)
	TuneTCP(&net.UDPConn{})
}
