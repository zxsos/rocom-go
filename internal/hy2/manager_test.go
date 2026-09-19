package hy2

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/zxsos/rocom-go/internal/dialout"
)

// 本文件守 Manager 的核心不变量,尤其**顺序**:先起新的、成功后再停旧的。
// 这条顺序错了,改配置失败就会把代理整个弄丢 —— 而它常是手机游戏流量的唯一通道。
//
// 与被它取代的 internal/socks5 那份同源,差别只在监听的是 UDP:同一 UDP 地址重复 bind
// 会报 address already in use(已实测),故「是否真的在监听」用它来判。

// testCert 生成一张一次性自签证书。hy2 必须跑在 TLS 上,没有证书连 New 都过不去。
func testCert(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "rocom-go test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:     []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// freeUDPAddr 取一个当前空闲的 UDP 端口(绑定后立刻关闭,供测试用)。
func freeUDPAddr(t *testing.T) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := pc.LocalAddr().String()
	pc.Close()
	return addr
}

// occupied 占住一个 UDP 端口并返回其地址,用于制造「新配置必然 bind 失败」。
func occupied(t *testing.T) (string, func()) {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return pc.LocalAddr().String(), func() { pc.Close() }
}

// listening 判断 addr 是否已被占用(即有人真的在监听)。
func listening(addr string) bool {
	pc, err := net.ListenPacket("udp", addr)
	if err != nil {
		return true
	}
	pc.Close()
	return false
}

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	m := NewManager()
	m.SetCert(testCert(t))
	return m
}

func TestManagerStartStop(t *testing.T) {
	m := newTestManager(t)
	defer m.Stop()

	addr := freeUDPAddr(t)
	if err := m.Start(Config{Addr: addr, Password: "p1", MaxConns: 8}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	got, ok := m.Running()
	if !ok || got != addr {
		t.Errorf("Running() = %q,%v; 期望 %q,true", got, ok, addr)
	}
	if !listening(addr) {
		t.Error("Start 返回成功但端口并未被占用(代理没有真的在监听)")
	}

	m.Stop()
	if _, ok := m.Running(); ok {
		t.Error("Stop 后仍在运行")
	}
	if listening(addr) {
		t.Error("Stop 后端口仍未释放")
	}
}

// TestManagerRestartKeepsOldOnFailure 守最关键的一条:新配置起不来时,旧实例必须还在服务。
func TestManagerRestartKeepsOldOnFailure(t *testing.T) {
	m := newTestManager(t)
	defer m.Stop()

	first := freeUDPAddr(t)
	if err := m.Start(Config{Addr: first, Password: "p1", MaxConns: 8}); err != nil {
		t.Fatal(err)
	}
	// 占住第二个端口,让新配置必然 bind 失败
	blocked, release := occupied(t)
	defer release()

	if err := m.Start(Config{Addr: blocked, Password: "p1", MaxConns: 8}); err == nil {
		t.Fatal("端口被占时 Start 应报错")
	}
	// 旧实例必须还在跑
	got, ok := m.Running()
	if !ok || got != first {
		t.Errorf("新配置失败后 Running() = %q,%v; 期望旧实例 %q 仍在运行", got, ok, first)
	}
	if !listening(first) {
		t.Error("旧实例已不再监听")
	}
}

// TestManagerRestartSwitchesAddr 验证正常换端口:旧的关掉、新的生效。
func TestManagerRestartSwitchesAddr(t *testing.T) {
	m := newTestManager(t)
	defer m.Stop()

	first, second := freeUDPAddr(t), freeUDPAddr(t)
	if err := m.Start(Config{Addr: first, Password: "p1"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(Config{Addr: second, Password: "p1"}); err != nil {
		t.Fatal(err)
	}
	got, _ := m.Running()
	if got != second {
		t.Errorf("换端口后 Running() = %q,期望 %q", got, second)
	}
	if listening(first) {
		t.Errorf("旧端口 %s 未释放", first)
	}
}

// TestManagerDisableStopsProxy 验证「不启用」(Addr 为空)会停掉代理。
func TestManagerDisableStopsProxy(t *testing.T) {
	m := newTestManager(t)
	defer m.Stop()
	addr := freeUDPAddr(t)
	if err := m.Start(Config{Addr: addr, Password: "p1"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(Config{}); err != nil { // Addr 空 = 不启用
		t.Fatalf("配成不启用不应报错: %v", err)
	}
	if _, ok := m.Running(); ok {
		t.Error("Addr 为空时代理应已停止")
	}
	if listening(addr) {
		t.Error("Stop 后端口仍未释放")
	}
}

// TestManagerStartTwiceDoesNotLeak 连续换配置不应残留 goroutine / 端口。
func TestManagerStartTwiceDoesNotLeak(t *testing.T) {
	m := newTestManager(t)
	defer m.Stop()
	addr := freeUDPAddr(t)
	for i := 0; i < 3; i++ {
		if err := m.Start(Config{Addr: addr, Password: "p1", MaxConns: 4}); err != nil {
			t.Fatalf("第 %d 次 Start: %v", i+1, err)
		}
	}
	if _, ok := m.Running(); !ok {
		t.Error("三次重启后应仍在运行")
	}
}

// TestManagerSameAddrChangeParams 守一个很容易写错、且错了就只能重启服务的场景:
// **只改密码、不改端口**。
//
// UDP 与 TCP 一样不允许两个 socket 同时 bind 同一地址。若换参数也走「重新起监听」
// 的老路,同端口改密码必然撞上 address already in use —— 面板上一个再普通不过的
// 操作直接失败。故地址没变时必须只换参数、不碰监听器。
//
// 这里直接比对实例指针:只要还是同一个 *Server,就证明监听器没被重建。
func TestManagerSameAddrChangeParams(t *testing.T) {
	m := newTestManager(t)
	defer m.Stop()

	addr := freeUDPAddr(t)
	if err := m.Start(Config{Addr: addr, Password: "p1", MaxConns: 4}); err != nil {
		t.Fatal(err)
	}
	before := m.cur.srv
	// 只改密码与并发上限,地址一字不变
	if err := m.Start(Config{Addr: addr, Password: "p2", MaxConns: 9}); err != nil {
		t.Fatalf("同端口改参数不应失败: %v", err)
	}
	if m.cur.srv != before {
		t.Error("地址未变却重建了监听实例(同端口改密码会撞 address already in use)")
	}
	if !listening(addr) {
		t.Error("改参数后不再监听")
	}
}

// TestManagerBandwidthIsStartupOnly 守一条被实测逼出来的约束:**带宽是启动项,不是运行期项**。
//
// 带宽在建连时就写进 QUIC 参数,改它必须重建监听;而同一端口「先起新后停旧」必然撞
// address already in use(先停旧再起新又会短暂丢掉代理)。两条路都不成立,故改为:
// 运行期改带宽**不重建实例**,只沿用旧值并告警 —— 这里就守「实例指针不变、仍在监听」。
// 若哪天有人把它改回「重建监听」,这个用例会立刻红。
func TestManagerBandwidthIsStartupOnly(t *testing.T) {
	m := newTestManager(t)
	defer m.Stop()

	addr := freeUDPAddr(t)
	if err := m.Start(Config{Addr: addr, Password: "p1", UpMbps: 10}); err != nil {
		t.Fatal(err)
	}
	before := m.cur.srv
	if err := m.Start(Config{Addr: addr, Password: "p1", UpMbps: 40}); err != nil {
		t.Fatalf("改带宽不应让 Start 失败(应沿用旧值并告警): %v", err)
	}
	if m.cur.srv != before {
		t.Error("改带宽不应重建监听实例(同端口重建必然 address already in use)")
	}
	if !listening(addr) {
		t.Error("改带宽后不再监听")
	}
}

// TestManagerWildcardAddrChangeParams 守变异 C 想抓的那条:**通配地址**。
//
// 请求 ":11443" 时,内核解析后的 srv.Addr().String() 是 "[::]:11443",与请求原文
// 字符串不等。若拿它去和面板提交的地址比,会永远判成「地址变了」→ 走重新 bind →
// 同端口改密码必然被 address already in use 打回。
// 上面那个用例用的是 127.0.0.1 具体地址(解析后与原文一致),覆盖不到这里。
func TestManagerWildcardAddrChangeParams(t *testing.T) {
	m := newTestManager(t)
	defer m.Stop()

	// 通配地址无法让内核代选端口,只能自己挑一个空闲的
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := pc.LocalAddr().(*net.UDPAddr).Port
	pc.Close()

	addr := fmt.Sprintf(":%d", port)
	if err := m.Start(Config{Addr: addr, Password: "p1"}); err != nil {
		t.Fatal(err)
	}
	if parsed := m.parsedAddr(); parsed != addr {
		t.Logf("提示: 请求 %q 解析后为 %q(两者不等,故只能比原文)", addr, parsed)
	}
	before := m.cur.srv
	if err := m.Start(Config{Addr: addr, Password: "p2"}); err != nil {
		t.Fatalf("通配地址下改参数不应失败(应识别为同一地址): %v", err)
	}
	if m.cur.srv != before {
		t.Error("通配地址被误判为「地址变了」而重建监听")
	}
}

func TestConfigValidate(t *testing.T) {
	cases := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{"不启用时其余字段不校验", Config{}, false},
		{"合法", Config{Addr: ":11443", Password: "p", MaxConns: 8}, false},
		{"白名单合法", Config{Addr: ":11443", Allow: "1.2.3.4,10.0.0.0/8"}, false},
		{"白名单非法", Config{Addr: ":11443", Allow: "not-an-ip"}, true},
		{"负并发上限", Config{Addr: ":11443", MaxConns: -1}, true},
		{"负带宽", Config{Addr: ":11443", UpMbps: -1}, true},
		{"空密码(不认证)允许", Config{Addr: ":11443"}, false},
	}
	for _, c := range cases {
		err := c.cfg.Validate()
		if (err != nil) != c.wantErr {
			t.Errorf("%s: err=%v, wantErr=%v", c.name, err, c.wantErr)
		}
	}
}

// TestAuthenticate 守两道防线:源 IP 白名单与密码。
// 白名单外的来源即使密码正确也必须拒绝 —— 那正是「公网扫描器碰巧拿到密码」的场景。
func TestAuthenticate(t *testing.T) {
	cert := testCert(t)
	s, err := New("127.0.0.1:0", &cert, Params{
		Allow:    mustAllow(t, "127.0.0.1,10.0.0.0/8"),
		Password: "secret",
	}, DefaultUpMbps, DefaultDownMbps)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	udp := func(ip string) net.Addr { return &net.UDPAddr{IP: net.ParseIP(ip), Port: 12345} }
	cases := []struct {
		name   string
		addr   net.Addr
		auth   string
		wantOK bool
	}{
		{"白名单内且密码正确", udp("127.0.0.1"), "secret", true},
		{"白名单内但密码错误", udp("127.0.0.1"), "wrong", false},
		{"白名单外(密码也错)", udp("203.0.113.9"), "wrong", false},
		{"白名单外(密码正确)", udp("203.0.113.9"), "secret", false},
		{"CIDR 网段内", udp("10.1.2.3"), "secret", true},
	}
	for _, c := range cases {
		ok, _ := s.Authenticate(c.addr, c.auth, 0)
		if ok != c.wantOK {
			t.Errorf("%s: Authenticate = %v,期望 %v", c.name, ok, c.wantOK)
		}
	}
}

// TestOutboundBlockedAndCounted 守 Outbound.TCP 的两件事:
// 屏蔽名单命中时在拨号前拒绝;拨号失败时并发计数必须归还(否则上限会被「泄漏」吃掉)。
func TestOutboundBlockedAndCounted(t *testing.T) {
	cert := testCert(t)
	s, err := New("127.0.0.1:0", &cert, Params{
		Block:    []string{"google.com", "example.com"},
		MaxConns: 1,
	}, DefaultUpMbps, DefaultDownMbps)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if _, err := s.TCP("google.com:443"); err == nil {
		t.Error("屏蔽名单内的域名应被拒绝(不发起注定失败的拨号)")
	}
	if n := s.inflight.Load(); n != 0 {
		t.Errorf("被拒绝的请求不应占用并发计数,实际 %d", n)
	}

	// 拨号失败(对端无人)同样要把计数还回去
	if _, err := s.TCP("127.0.0.1:1"); err == nil {
		t.Error("对端无监听时应报错")
	}
	if n := s.inflight.Load(); n != 0 {
		t.Errorf("拨号失败后并发计数应归还,实际 %d", n)
	}

	// 成功路径:连上本地一个 listener,Close 后计数归零
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
	conn, err := s.TCP(ln.Addr().String())
	if err != nil {
		t.Fatalf("拨号本地 listener: %v", err)
	}
	if n := s.inflight.Load(); n != 1 {
		t.Errorf("成功后并发计数应为 1,实际 %d", n)
	}
	conn.Close()
	if n := s.inflight.Load(); n != 0 {
		t.Errorf("Close 后并发计数应归零,实际 %d", n)
	}
	// 重复 Close 不应把计数减成负数
	conn.Close()
	if n := s.inflight.Load(); n != 0 {
		t.Errorf("重复 Close 不应重复归还,实际 %d", n)
	}
}

func TestOutboundUDPDisabled(t *testing.T) {
	cert := testCert(t)
	s, err := New("127.0.0.1:0", &cert, Params{}, DefaultUpMbps, DefaultDownMbps)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.UDP("8.8.8.8:53"); err == nil {
		t.Error("UDP 转发应被禁用")
	}
	if err := s.CheckUDP("8.8.8.8:53"); err == nil {
		t.Error("CheckUDP 在禁用 UDP 时应报错")
	}
}

// TestNoCertFails 守一条很容易漏的:hy2 必须跑在 TLS 上,没证书不能「先起来再说」。
func TestNoCertFails(t *testing.T) {
	if _, err := New("127.0.0.1:0", nil, Params{}, DefaultUpMbps, DefaultDownMbps); err == nil {
		t.Error("未提供证书时 New 应报错")
	}
	m := NewManager()
	if err := m.Start(Config{Addr: freeUDPAddr(t), Password: "p"}); err == nil {
		t.Error("未提供证书时 Start 应报错")
	}
}

func TestMBpsToBytes(t *testing.T) {
	cases := []struct {
		mbps int
		want uint64
	}{
		{0, 0},        // 不设上限
		{1, 131072},   // 1 Mbps = 128 KiB/s
		{20, 2621440}, // 20 Mbps
		{-5, 0},       // 非法值退回「不设上限」
	}
	for _, c := range cases {
		if got := mbpsToBytes(c.mbps); got != c.want {
			t.Errorf("mbpsToBytes(%d) = %d,期望 %d", c.mbps, got, c.want)
		}
	}
	// hy2 规定非 0 值不得小于 65536,极小值必须被抬上去而不是原样传出
	if got := mbpsToBytes(1); got < 65536 {
		t.Errorf("带宽过小未抬到 hy2 的下限 65536: %d", got)
	}
}

func mustAllow(t *testing.T, s string) []netip.Prefix {
	t.Helper()
	p, err := dialout.ParseAllow(s)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
