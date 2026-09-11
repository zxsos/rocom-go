//go:build linux

package capture

import (
	"net"
	"net/netip"
	"os"
	"strings"
	"testing"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

// 本文件守部署时最容易踩的两个坑:**网卡名填错**与 **-skip-self-ip 设错**。
// 两者的现场表现都很安静(前者二进制退出、容器反复重启;后者包数在涨、却永远没有数据),
// 用户只能从 docker logs 里找线索 —— 故这里钉住「错误信息必须给出该填什么」,
// 以及「运行期自检依赖的计数真的在涨」。

// firstUpIface 返回本机第一张可用的非回环网卡;没有则跳过(CI 容器里很常见)。
func firstUpIface(t *testing.T) string {
	t.Helper()
	n, ok := firstUsableIface()
	if !ok {
		t.Skip("本机没有可用的非回环网卡")
	}
	return n
}

// TestResolveIfaceExplicit 显式给的网卡名应被原样采用,且不标记为自动。
func TestResolveIfaceExplicit(t *testing.T) {
	name := firstUpIface(t)
	info, err := ResolveIface(name)
	if err != nil {
		t.Fatalf("ResolveIface(%q): %v", name, err)
	}
	if info.Name != name {
		t.Errorf("应原样返回 %q,实得 %q", name, info.Name)
	}
	if info.Auto {
		t.Error("显式指定时 Auto 应为 false")
	}
}

// TestResolveIfaceAuto 空串与 "auto" 都应自动选中并标记 Auto。
func TestResolveIfaceAuto(t *testing.T) {
	firstUpIface(t) // 没有可用网卡时跳过
	for _, in := range []string{"", "auto"} {
		info, err := ResolveIface(in)
		if err != nil {
			t.Fatalf("ResolveIface(%q): %v", in, err)
		}
		if !info.Auto {
			t.Errorf("ResolveIface(%q) 应标记为自动选中", in)
		}
		if info.Name == "" {
			t.Errorf("ResolveIface(%q) 应给出网卡名", in)
		}
	}
}

// TestResolveIfaceBadNameListsCandidates 填错时必须给出候选与默认路由提示。
//
// 这是 ResolveIface 存在的主要理由:容器部署下用户看不到 README,只会看 docker logs ——
// 一句 "no such device" 对他毫无帮助,得直接告诉他该填什么。
func TestResolveIfaceBadNameListsCandidates(t *testing.T) {
	const bad = "definitely-not-an-iface"
	_, err := ResolveIface(bad)
	if err == nil {
		t.Fatal("不存在的网卡名应报错")
	}
	msg := err.Error()
	if !strings.Contains(msg, bad) {
		t.Errorf("错误信息应带上填错的名字,实得: %s", msg)
	}
	// 关键:要有「该填什么」的线索,不能只是「你填错了」。
	if !strings.Contains(msg, "当前可用网卡") && !strings.Contains(msg, "非回环网卡") {
		t.Errorf("错误信息应列出候选网卡(或说明一张都没有),实得: %s", msg)
	}
}

// TestDockerBridgeWarn 守两条:容器里选中默认网桥要报警,真实网络地址不能误报。
// 误报会让宿主部署的用户以为自己漏了 --network host。
func TestDockerBridgeWarn(t *testing.T) {
	bridge := []netip.Addr{netip.MustParseAddr("172.17.0.2")}
	real := []netip.Addr{netip.MustParseAddr("172.16.0.29")} // 常见 VPS 段,不是 Docker 默认网桥

	if _, err := os.Stat("/.dockerenv"); err == nil {
		if dockerBridgeWarn("eth0", bridge) == "" {
			t.Error("容器里选中 172.17 段网卡应给出桥接警告")
		}
	} else if got := dockerBridgeWarn("eth0", bridge); got != "" {
		t.Errorf("不在容器里不该给桥接警告,实得: %s", got)
	}
	if got := dockerBridgeWarn("ens17", real); got != "" {
		t.Errorf("172.16 段不是 Docker 默认网桥,不该报警,实得: %s", got)
	}
}

// TestDroppedBySelfIPCounts 守「被本机 IP 丢弃的包有记账」。
//
// 运行期自检(见 pollStats)完全依赖这个计数:计数不涨,那条「-skip-self-ip 可能设错了」
// 的警告就永远不会出现 —— 而设错时**其它症状一个都没有**。
func TestDroppedBySelfIPCounts(t *testing.T) {
	e := NewEngine(8195)
	flow := func(src, dst string) gopacket.Flow {
		ip := &layers.IPv4{SrcIP: net.ParseIP(src).To4(), DstIP: net.ParseIP(dst).To4()}
		return ip.NetworkFlow()
	}

	// 未登记忽略集时一个都不该丢(否则会把正常流量全滤掉)
	if e.droppedBySelfIP(flow("10.0.0.1", "1.2.3.4")) {
		t.Error("未登记忽略集时不该丢包")
	}
	if e.SkipDropped() != 0 {
		t.Errorf("未丢包时计数应为 0,实得 %d", e.SkipDropped())
	}

	e.AddSkipIP(netip.MustParseAddr("10.0.0.1"))
	if !e.droppedBySelfIP(flow("10.0.0.1", "1.2.3.4")) {
		t.Error("源命中忽略集应丢弃")
	}
	if !e.droppedBySelfIP(flow("1.2.3.4", "10.0.0.1")) {
		t.Error("目的命中忽略集也应丢弃 —— 两个方向都要查,漏一侧等于半聋")
	}
	if got := e.SkipDropped(); got != 2 {
		t.Errorf("两次命中应记 2,实得 %d", got)
	}
	// 不命中的不该被丢、也不该记账
	if e.droppedBySelfIP(flow("10.0.0.2", "1.2.3.4")) {
		t.Error("不在忽略集里的 IP 不该被丢")
	}
	if got := e.SkipDropped(); got != 2 {
		t.Errorf("不命中的包不该记账,实得 %d", got)
	}
}

// TestEmittedCounts 守「产出的消息被记账」——运行期自检的另一半前提。
// 它要是恒为 0,那条警告在**正常部署**上也会乱报。
func TestEmittedCounts(t *testing.T) {
	e := NewEngine(8195)
	if e.Emitted() != 0 {
		t.Fatalf("新引擎计数应为 0,实得 %d", e.Emitted())
	}
	e.emit(Message{Opcode: 0x0102}) // Out 有 4096 缓冲,不会阻塞
	e.emit(Message{Opcode: 0x0102})
	e.emit(Message{Opcode: 0x0102})
	if got := e.Emitted(); got != 3 {
		t.Errorf("Emitted 应为 3,实得 %d", got)
	}
}
