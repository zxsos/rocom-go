//go:build linux

package capture

import (
	"sync/atomic"
)

// packetStats 是 AF_PACKET 环形缓冲区的丢包统计。
//
// 为什么需要它:抓包是**单 goroutine 串行**(见 process),且开了 NoCopy —— 包数据
// 直接引用环形缓冲区、不复制。处理一慢,缓冲区里的帧就无法释放,新来的包写不进去
// 直接丢弃。多人同时挂 socks5 时包量成倍增长,这个瓶颈会被放大。
//
// 内核自己就在数丢了多少(TPACKET_STATISTICS),但 gopacket 默认不开,
// 故必须显式 InitSocketStats 才能读到。没有它,"是不是丢包"只能靠猜。
type packetStats struct {
	drops   atomic.Uint64 // 因环形缓冲区满被内核丢弃的包数(累计)
	packets atomic.Uint64 // 内核收到的包数(累计)
}

// captureStats 是实时抓包的统计句柄;离线回放时为 nil。
var captureStats packetStats

// PacketDropped 返回被内核丢弃的包数(环形缓冲区满)。仅实时抓包有效。
func PacketDropped() uint64 { return captureStats.drops.Load() }

// PacketSeen 返回内核收到的包数。仅实时抓包有效。
func PacketSeen() uint64 { return captureStats.packets.Load() }

// recordStats 累加一次采样到的统计。由 RunLive 的采集 goroutine 调用。
func recordStats(packets, drops uint) {
	if drops > 0 {
		captureStats.drops.Add(uint64(drops))
	}
	captureStats.packets.Add(uint64(packets))
}
