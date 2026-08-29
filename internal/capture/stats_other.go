//go:build !linux

package capture

// 丢包统计依赖 AF_PACKET 的 TPACKET_STATISTICS,Linux 专属;
// 其它平台(本项目实际只跑 Linux)一律返回 0,保持 API 一致。

// PacketDropped 返回被内核丢弃的包数。非 Linux 恒为 0。
func PacketDropped() uint64 { return 0 }

// PacketSeen 返回内核收到的包数。非 Linux 恒为 0。
func PacketSeen() uint64 { return 0 }
