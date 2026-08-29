//go:build linux

package capture

import (
	"log"
	"net"
	"net/netip"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/afpacket"
	"github.com/google/gopacket/layers"
)

// 环形缓冲区大小。默认约 8MB(blockSize 64KB × 128 块),在开了 NoCopy 的串行
// 处理下不够:处理慢 → 帧无法释放 → 新包写不进来被丢弃。多人同时挂 socks5 时
// 包量成倍增长,故显式放大到 32MB。
//
// 约束:blockSize 必须是 frameSize 的整数倍,且是页大小的整数倍;numBlocks ×
// blockSize 即总缓冲。frameSize 取 4096 覆盖常见 MTU(含 VLAN/GRE 封装余量)。
const (
	frameSize = 4096
	blockSize = frameSize * 32 // 128KB/块
	numBlocks = 256            // 总 32MB
)

// statsInterval 是丢包统计的采样间隔。太频繁 syscall 开销大,太稀疏则丢包
// 发现得晚;30s 兼顾(日志也按此节奏,不至于刷屏)。
const statsInterval = 30 * time.Second

// RunLive 在指定网卡上用 AF_PACKET 被动抓包(无需 libpcap)。阻塞运行。
// skipSelf 为 true 时忽略网卡自身 IP(单臂网关去重);socks5/云代理模式下本机进程
// 出站的游戏流量正是以本机 IP 为源,必须传 false 才抓得到(见 cmd/rocom-capture -skip-self-ip)。
func (e *Engine) RunLive(iface string, skipSelf bool) error {
	if skipSelf {
		// 单臂网关去重:抓包网卡在做 SNAT 转发时,会把游戏流的一个副本(源改为本机 IP)
		// 再次从同一网卡发出并被捕获。登记本机 IP 到忽略集,只保留 NAT 前的真实客户端会话。
		ignoreSelfIPs(e, iface)
	}

	tp, err := afpacket.NewTPacket(
		afpacket.OptInterface(iface),
		afpacket.OptPollTimeout(time.Second),
		afpacket.OptFrameSize(frameSize),
		afpacket.OptBlockSize(blockSize),
		afpacket.OptNumBlocks(numBlocks),
	)
	if err != nil {
		return err
	}
	defer tp.Close()

	// 内核的 TPACKET_STATISTICS 默认关闭,不开就读不到丢包数。
	// 失败不影响抓包,只意味着丢包不可见,故仅提示不返回。
	if err := tp.InitSocketStats(); err != nil {
		log.Printf("提示: 无法开启抓包丢包统计(%v),丢包数将不可用", err)
	} else {
		go pollStats(tp)
	}

	src := gopacket.NewPacketSource(tp, layers.LayerTypeEthernet)
	src.NoCopy = true
	e.process(src)
	return nil
}

// pollStats 定期采样内核的丢包计数并累计。
// 计数是**累计值**而非差值,故这里自己算增量:间隔内的丢包数才是判断依据
// (总丢包数里可能混着启动初期的一次性抖动)。丢包时立即打日志,便于定位时间段。
func pollStats(tp *afpacket.TPacket) {
	tick := time.NewTicker(statsInterval)
	defer tick.Stop()
	var lastPackets, lastDrops uint
	for range tick.C {
		s, _, err := tp.SocketStats()
		if err != nil {
			continue
		}
		pkts, drops := s.Packets(), s.Drops()
		// 计数器溢出回绕时差值会异常大,跳过这次采样免得记出巨数
		if pkts >= lastPackets && drops >= lastDrops {
			recordStats(pkts-lastPackets, drops-lastDrops)
			if drops > lastDrops {
				log.Printf("警告: 抓包丢包 %d 个(近 %v 内收到 %d 个)—— 环形缓冲区满、处理跟不上,"+
					"可增大 blockSize/numBlocks 或降低单包处理耗时",
					drops-lastDrops, statsInterval, pkts-lastPackets)
			}
		}
		lastPackets, lastDrops = pkts, drops
	}
}

// ignoreSelfIPs 把网卡自身的单播 IP 登记进忽略集(单臂 NAT 去重,见 RunLive)。
func ignoreSelfIPs(e *Engine, iface string) {
	ifi, err := net.InterfaceByName(iface)
	if err != nil {
		return
	}
	addrs, err := ifi.Addrs()
	if err != nil {
		return
	}
	var ips []netip.Addr
	for _, a := range addrs {
		var raw net.IP
		switch v := a.(type) {
		case *net.IPNet:
			raw = v.IP
		case *net.IPAddr:
			raw = v.IP
		}
		if ip, ok := netip.AddrFromSlice(raw); ok && !ip.IsLoopback() {
			ip = ip.Unmap()
			e.AddSkipIP(ip)
			ips = append(ips, ip)
		}
	}
	if len(ips) > 0 {
		log.Printf("单臂网关去重: 忽略本机 %s 的 IP %v", iface, ips)
	}
}
