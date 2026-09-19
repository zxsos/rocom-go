//go:build linux

package capture

import (
	"fmt"
	"log"
	"net"
	"net/netip"
	"os"
	"strings"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/afpacket"
	"github.com/google/gopacket/layers"
)

// dockerDefaultBridge 是 `docker run` 不加 --network 时的默认网桥网段(见 dockerBridgeWarn)。
var dockerDefaultBridge = netip.MustParsePrefix("172.17.0.0/16")

// 环形缓冲区大小。默认约 8MB(blockSize 64KB × 128 块),在开了 NoCopy 的串行
// 处理下不够:处理慢 → 帧无法释放 → 新包写不进来被丢弃。多人同时挂 hy2 代理时
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

// IfaceInfo 是网卡解析的结果:实际要抓的网卡名、它的非回环 IP(供启动横幅显示),
// 以及一条需要注意的提示(Warn 非空时调用方应打出来)。
type IfaceInfo struct {
	Name string
	IPs  []netip.Addr
	Auto bool   // 是否为程序自动选中(用户没显式指定)
	Warn string // 需要提醒的情况(Docker 桥接模式等)
}

// ResolveIface 把 -iface 的取值解析成实际要抓包的网卡。
//
// 这个函数只为一件事而存在:**部署时最高频的坑就是网卡名填错**。默认表现是二进制直接
// 退出、容器不停重启,而报错只有 gopacket 的 "no such device" —— 用户看不出该填什么。
// 故这里做三件事:
//
//  1. 空串与 "auto" 都自动选**默认路由**所在的那张网卡。Docker 用 --network host 时
//     容器与宿主机共享路由表,算出来的就是宿主机那张物理网卡 —— VPS 的网卡很少叫 eth0,
//     用户不必再 `ip route get 8.8.8.8` 去查。
//  2. 名字填错时,错误信息里列出全部候选网卡并指出默认路由在哪张:容器部署下只能从
//     docker logs 里找答案,得直接告诉他该填什么。
//  3. 选中的像是容器自带网桥时给警告(多半是漏了 --network host)。那种情况的表现是
//     「容器起来了、包数却是 0」,同样难查。
func ResolveIface(name string) (IfaceInfo, error) {
	explicit := name != "" && name != "auto"
	if !explicit {
		if n, err := defaultRouteIface(); err == nil {
			name = n
		} else if n, ok := firstUsableIface(); ok {
			// 没有默认路由(纯内网、旁路镜像):退回第一张可用网卡,并说清这是猜的。
			name = n
			log.Printf("提示: 未找到默认路由,自动选了网卡 %q;若不对请显式指定 -iface", n)
		} else {
			return IfaceInfo{}, fmt.Errorf("自动选择网卡失败: 没有可用的非回环网卡,请用 -iface <网卡名> 显式指定")
		}
	} else if _, err := net.InterfaceByName(name); err != nil {
		return IfaceInfo{}, fmt.Errorf("网卡 %q 不存在。%s", name, ifaceCandidates())
	}
	ips := ifaceAddrs(name)
	if len(ips) == 0 {
		// 网卡存在但没有 IP:旁路镜像/透明网桥下属正常(只收不发),故只提醒不拦。
		log.Printf("提示: 网卡 %q 没有非回环 IP —— 旁路镜像/透明网桥下属正常,单臂网关下则可能选错了", name)
	}
	return IfaceInfo{Name: name, IPs: ips, Auto: !explicit, Warn: dockerBridgeWarn(name, ips)}, nil
}

// defaultRouteIface 读 /proc/net/route 找默认路由(Destination 全 0)所在的网卡。
// 只认已启用(FlagUp)的网卡,免得选到一张刚被禁用的。
func defaultRouteIface() (string, error) {
	b, err := os.ReadFile("/proc/net/route")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(b), "\n")[1:] { // 首行是表头
		f := strings.Fields(line)
		if len(f) < 8 || f[1] != "00000000" {
			continue
		}
		ifi, err := net.InterfaceByName(f[0])
		if err != nil || ifi.Flags&net.FlagUp == 0 {
			continue
		}
		return f[0], nil
	}
	return "", fmt.Errorf("没有默认路由")
}

// firstUsableIface 返回第一张已启用、且带非回环 IP 的网卡(自动选择的兜底)。
func firstUsableIface() (string, bool) {
	ifs, err := net.Interfaces()
	if err != nil {
		return "", false
	}
	for _, ifi := range ifs {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagLoopback != 0 {
			continue
		}
		if len(ifaceAddrs(ifi.Name)) > 0 {
			return ifi.Name, true
		}
	}
	return "", false
}

// ifaceAddrs 返回某网卡的非回环单播 IP(已 Unmap,IPv4 的 4-in-6 形式会被折叠)。
func ifaceAddrs(name string) []netip.Addr {
	ifi, err := net.InterfaceByName(name)
	if err != nil {
		return nil
	}
	addrs, err := ifi.Addrs()
	if err != nil {
		return nil
	}
	var out []netip.Addr
	for _, a := range addrs {
		var raw net.IP
		switch v := a.(type) {
		case *net.IPNet:
			raw = v.IP
		case *net.IPAddr:
			raw = v.IP
		}
		if ip, ok := netip.AddrFromSlice(raw); ok && !ip.IsLoopback() {
			out = append(out, ip.Unmap())
		}
	}
	return out
}

// ifaceCandidates 列出候选网卡(名 + IP)并指出默认路由在哪张,供「网卡名填错」的错误信息用。
func ifaceCandidates() string {
	def, _ := defaultRouteIface()
	ifs, err := net.Interfaces()
	if err != nil {
		return "读取网卡列表失败: " + err.Error()
	}
	var b strings.Builder
	n := 0
	for _, ifi := range ifs {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagLoopback != 0 {
			continue
		}
		ips := ifaceAddrs(ifi.Name)
		if len(ips) == 0 {
			continue
		}
		mark := ""
		if ifi.Name == def {
			mark = " ← 默认路由所在,多半就是它"
		}
		fmt.Fprintf(&b, "\n  %s (%s)%s", ifi.Name, joinAddrs(ips), mark)
		n++
	}
	if n == 0 {
		return "当前没有可用的非回环网卡 —— 容器里若未加 --network host,是看不到宿主机网卡的"
	}
	return "当前可用网卡:" + b.String()
}

// dockerBridgeWarn 在「跑在容器里 + 网卡地址落在 Docker 默认网桥网段」时返回一条警告。
//
// 为什么只认 172.17.0.0/16:那是 `docker run` 不加 --network 时的默认网段。自定义网段
// 这里会漏(不提示),但宁可漏也不误报 —— --network host 下网卡是宿主机的真实地址,
// 不会落在这个段里。
func dockerBridgeWarn(iface string, ips []netip.Addr) string {
	if _, err := os.Stat("/.dockerenv"); err != nil {
		return "" // 不在容器里
	}
	for _, ip := range ips {
		if ip.Is4() && dockerDefaultBridge.Contains(ip) {
			return fmt.Sprintf("网卡 %s (%s) 是 Docker 默认网桥:桥接模式下容器看不到宿主机物理网卡,"+
				"抓不到任何游戏流量。请给 docker run 加 --network host(compose 里写 network_mode: host)后重启", iface, ip)
		}
	}
	return ""
}

// joinAddrs 把 IP 列表拼成 "a, b" 形式(日志与错误信息用)。
func joinAddrs(ips []netip.Addr) string {
	ss := make([]string, len(ips))
	for i, ip := range ips {
		ss[i] = ip.String()
	}
	return strings.Join(ss, ", ")
}

// RunLive 在指定网卡上用 AF_PACKET 被动抓包(无需 libpcap)。阻塞运行。
// skipSelf 为 true 时忽略网卡自身 IP(单臂网关去重);hy2/云代理模式下本机进程
// 出站的游戏流量正是以本机 IP 为源,必须传 false 才抓得到(见 cmd/rocom-go -skip-self-ip)。
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
		go pollStats(tp, e)
	}

	src := gopacket.NewPacketSource(tp, layers.LayerTypeEthernet)
	src.NoCopy = true
	e.process(src)
	return nil
}

// selfCheckMin 是运行期自检的起判包数:玩家一连上几秒就能到这个量,故不必再等时间窗口。
const selfCheckMin = 100

// pollStats 定期采样内核的丢包计数并累计。
// 计数是**累计值**而非差值,故这里自己算增量:间隔内的丢包数才是判断依据
// (总丢包数里可能混着启动初期的一次性抖动)。丢包时立即打日志,便于定位时间段。
//
// 顺带承担一个运行期自检:包在进来、却全被「本机 IP」挡掉、且一条业务消息都没解析出来
// —— 那是 -skip-self-ip 设错的指纹(见下),这是唯一能兜住「设错了但表面正常」的手段。
func pollStats(tp *afpacket.TPacket, e *Engine) {
	tick := time.NewTicker(statsInterval)
	defer tick.Stop()
	var lastPackets, lastDrops uint
	selfCheckWarned := false
	for range tick.C {
		s, s3, err := tp.SocketStats()
		if err != nil {
			continue
		}
		// 两个版本的内核计数都要读:带 OptBlockSize/OptNumBlocks 时 gopacket 以
		// **TPACKET_V3** 激活环,包数落在 V3 结构里,而 V1 那个会永远是 0。只读 V1 的
		// 后果是「收到 0 个包」长挂零 —— 而依赖 PacketSeen 的 -skip-self-ip 自检
		// (下面 selfCheckMin)因此永不触发,等于把唯一的兜底也弄丢了。
		pkts, drops := sumStats(s, s3)
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

		// 运行期自检:-skip-self-ip 设错时**没有任何其它症状**(手机能玩、包数在涨、
		// 日志无异常),只是永远没有宠物数据。正常部署即使有 SNAT 副本,也一定会有未被
		// 丢弃的流量并解析出消息,故「丢了很多 + 一条消息都没有」足以判定。
		// 只在首次命中时打一次:这是个部署期问题,重启才能解决,刷屏没用。
		if !selfCheckWarned && e.SkipDropped() > selfCheckMin && e.Emitted() == 0 {
			selfCheckWarned = true
			log.Printf("警告: 已有 %d 个包因「本机 IP」被丢弃,且尚未解析出任何游戏消息 —— "+
				"若你是 hy2/云代理部署(手机把游戏流量代理到本机),请设 -skip-self-ip=false 后重启;"+
				"若确为单臂网关,请回头确认网卡是否选错(见启动时的网卡日志)", e.SkipDropped())
		}
	}
}

// sumStats 合并 TPacket V1 与 V3 两份内核计数。
//
// gopacket 只把 getsockopt(PACKET_STATISTICS) 的结果累加进**与激活版本相符**的那个
// 结构,另一个保持零值 —— 而调用方事先不该关心用的是哪个版本(它由选项组合决定,
// 带 OptBlockSize/OptNumBlocks 就是 V3)。两边都读再相加,V1/V3 下都对。
func sumStats(s afpacket.SocketStats, s3 afpacket.SocketStatsV3) (packets, drops uint) {
	return s.Packets() + s3.Packets(), s.Drops() + s3.Drops()
}

// ignoreSelfIPs 把网卡自身的单播 IP 登记进忽略集(单臂 NAT 去重,见 RunLive)。
func ignoreSelfIPs(e *Engine, iface string) {
	ips := ifaceAddrs(iface)
	for _, ip := range ips {
		e.AddSkipIP(ip)
	}
	if len(ips) > 0 {
		log.Printf("单臂网关去重: 忽略本机 %s 的 IP %v", iface, ips)
	}
}
