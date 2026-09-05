package socks5

import (
	"fmt"
	"net"
	"testing"
	"time"
)

// 本文件守 Manager 的核心不变量,尤其**顺序**:先起新的、成功后再停旧的。
// 这条顺序错了,改配置失败就会把代理整个弄丢 —— 而它常是手机游戏流量的唯一通道。

// freeAddr 取一个当前空闲的端口(监听后立刻关闭,供测试用)。
func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	return addr
}

func TestManagerStartStop(t *testing.T) {
	m := NewManager()
	defer m.Stop()

	addr := freeAddr(t)
	if err := m.Start(Config{Addr: addr, MaxConns: 8}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	got, ok := m.Running()
	if !ok || got != addr {
		t.Errorf("Running() = %q,%v; 期望 %q,true", got, ok, addr)
	}
	// 真的在监听:能连上
	c, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		t.Fatalf("代理未真正监听: %v", err)
	}
	c.Close()

	m.Stop()
	if _, ok := m.Running(); ok {
		t.Error("Stop 后仍在运行")
	}
}

// TestManagerRestartKeepsOldOnFailure 守最关键的一条:新配置起不来时,旧实例必须还在服务。
func TestManagerRestartKeepsOldOnFailure(t *testing.T) {
	m := NewManager()
	defer m.Stop()

	first := freeAddr(t)
	if err := m.Start(Config{Addr: first, MaxConns: 8}); err != nil {
		t.Fatal(err)
	}
	// 占住第二个端口,让新配置必然 bind 失败
	blocker, err := net.Listen("tcp", freeAddr(t))
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Close()

	err = m.Start(Config{Addr: blocker.Addr().String(), MaxConns: 8})
	if err == nil {
		t.Fatal("端口被占时 Start 应报错")
	}
	// 旧实例必须还在跑
	got, ok := m.Running()
	if !ok || got != first {
		t.Errorf("新配置失败后 Running() = %q,%v; 期望旧实例 %q 仍在运行", got, ok, first)
	}
	c, err := net.DialTimeout("tcp", first, time.Second)
	if err != nil {
		t.Errorf("旧实例已不再服务: %v", err)
	} else {
		c.Close()
	}
}

// TestManagerRestartSwitchesAddr 验证正常换端口:旧的关掉、新的生效。
func TestManagerRestartSwitchesAddr(t *testing.T) {
	m := NewManager()
	defer m.Stop()

	first, second := freeAddr(t), freeAddr(t)
	if err := m.Start(Config{Addr: first}); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(Config{Addr: second}); err != nil {
		t.Fatal(err)
	}
	got, _ := m.Running()
	if got != second {
		t.Errorf("换端口后 Running() = %q,期望 %q", got, second)
	}
	// 旧端口应已释放:能重新 bind 成功
	ln, err := net.Listen("tcp", first)
	if err != nil {
		t.Errorf("旧端口 %s 未释放: %v", first, err)
	} else {
		ln.Close()
	}
}

// TestManagerDisableStopsProxy 验证「不启用」(Addr 为空)会停掉代理。
func TestManagerDisableStopsProxy(t *testing.T) {
	m := NewManager()
	defer m.Stop()
	if err := m.Start(Config{Addr: freeAddr(t)}); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(Config{}); err != nil { // Addr 空 = 不启用
		t.Fatalf("配成不启用不应报错: %v", err)
	}
	if _, ok := m.Running(); ok {
		t.Error("Addr 为空时代理应已停止")
	}
}

// TestManagerStartTwiceDoesNotLeak 连续换配置不应残留 goroutine / 端口。
func TestManagerStartTwiceDoesNotLeak(t *testing.T) {
	m := NewManager()
	defer m.Stop()
	addr := freeAddr(t)
	for i := 0; i < 3; i++ {
		if err := m.Start(Config{Addr: addr, MaxConns: 4}); err != nil {
			t.Fatalf("第 %d 次 Start: %v", i+1, err)
		}
	}
	// 同一端口连起三次,说明每次都干净地释放了上一个监听
	if _, ok := m.Running(); !ok {
		t.Error("三次重启后应仍在运行")
	}
}

// TestManagerSameAddrChangeParams 守一个很容易写错、且错了就只能重启服务的场景:
// **只改密码、不改端口**。
//
// TCP 不允许两个 socket 同时 bind 同一地址。若换参数也走「重新起监听」的老路,
// 同端口改密码必然撞上 address already in use —— 面板上一个再普通不过的操作
// 直接失败。故地址没变时必须只换参数、不碰监听器。
//
// 另有一层陷阱:请求 ":1080" 时内核解析后的 srv.Addr().String() 是 "[::]:1080",
// 与原文不等。拿它比会误判成「改了地址」,又绕回重新 bind。故比的是请求原文。
func TestManagerSameAddrChangeParams(t *testing.T) {
	m := NewManager()
	defer m.Stop()

	addr := freeAddr(t)
	if err := m.Start(Config{Addr: addr, User: "u1", Pass: "p1", MaxConns: 4}); err != nil {
		t.Fatal(err)
	}
	// 只改密码与并发上限,地址一字不变
	if err := m.Start(Config{Addr: addr, User: "u1", Pass: "p2", MaxConns: 9}); err != nil {
		t.Fatalf("同端口改参数不应失败: %v", err)
	}
	got, ok := m.Running()
	if !ok {
		t.Fatal("改参数后应仍在运行")
	}
	_ = got
	// 监听器没被重建:端口始终可用,且仍监听在原地址
	c, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		t.Errorf("改参数后不再服务: %v", err)
	} else {
		c.Close()
	}
}

// TestManagerWildcardAddrChangeParams 守变异 C 想抓的那条:**通配地址**。
//
// 请求 ":0"(或 ":1080")时,内核解析后的 srv.Addr().String() 是 "[::]:port",
// 与请求原文字符串不等。若拿它去和面板提交的地址比,会永远判成「地址变了」→
// 走重新 bind → 同端口改密码必然被 address already in use 打回。
// 上面那个用例用的是 127.0.0.1 具体地址(解析后与原文一致),覆盖不到这里。
func TestManagerWildcardAddrChangeParams(t *testing.T) {
	m := NewManager()
	defer m.Stop()

	// 先占一个端口,再放掉,拿到一个空闲端口号(通配地址无法让内核代选,只能自己挑)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	addr := fmt.Sprintf(":%d", port)
	if err := m.Start(Config{Addr: addr, User: "u1", Pass: "p1"}); err != nil {
		t.Fatal(err)
	}
	// 内核解析后的形态与请求原文不同,但**请求原文**应判定为同一地址
	if parsed := m.parsedAddr(); parsed != addr {
		t.Logf("提示: 请求 %q 解析后为 %q(两者不等,故只能比原文)", addr, parsed)
	}
	if err := m.Start(Config{Addr: addr, User: "u1", Pass: "p2"}); err != nil {
		t.Fatalf("通配地址下改参数不应失败(应识别为同一地址): %v", err)
	}
	if _, ok := m.Running(); !ok {
		t.Error("改参数后应仍在运行")
	}
}

func TestConfigValidate(t *testing.T) {
	cases := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{"不启用时其余字段不校验", Config{}, false},
		{"合法", Config{Addr: ":1080", MaxConns: 8}, false},
		{"白名单合法", Config{Addr: ":1080", Allow: "1.2.3.4,10.0.0.0/8"}, false},
		{"白名单非法", Config{Addr: ":1080", Allow: "not-an-ip"}, true},
		{"有用户名无密码", Config{Addr: ":1080", User: "u"}, true},
		{"用户名密码成对", Config{Addr: ":1080", User: "u", Pass: "p"}, false},
		{"负并发上限", Config{Addr: ":1080", MaxConns: -1}, true},
		{"纯空白地址", Config{Addr: "   "}, true},
	}
	for _, c := range cases {
		err := c.cfg.Validate()
		if (err != nil) != c.wantErr {
			t.Errorf("%s: err=%v, wantErr=%v", c.name, err, c.wantErr)
		}
	}
}

func TestSplitList(t *testing.T) {
	got := splitList(" a.com , ,b.com,, ")
	if len(got) != 2 || got[0] != "a.com" || got[1] != "b.com" {
		t.Errorf("splitList = %q,期望 [a.com b.com](去空与空白)", got)
	}
	if n := len(splitList("")); n != 0 {
		t.Errorf("空串应得空切片,实际 %d", n)
	}
}
