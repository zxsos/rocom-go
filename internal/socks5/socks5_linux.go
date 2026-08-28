//go:build linux

package socks5

import (
	"net"
	"syscall"
)

// setQuickAck 为 TCP 连接启用 TCP_QUICKACK:收到数据立即回 ACK,跳过 delayed ACK
// 的 40ms 等待。游戏流量是高频小包的请求-响应式(指令/心跳/回包),代理两端
// (客户端连接与上游连接)都启用可削减每个交互周期内的感知延迟。
func setQuickAck(c *net.TCPConn) {
	rc, err := c.SyscallConn()
	if err != nil {
		return
	}
	_ = rc.Control(func(fd uintptr) {
		_ = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_TCP, syscall.TCP_QUICKACK, 1)
	})
}

// tcpNotsentLowat 是 TCP_NOTSENT_LOWAT 选项值:发送队列中未发出数据的阈值,
// 超过该值内核才允许积压更多。调低后发送小包(游戏指令/心跳)不会在 socket
// 发送队列里排队等攒批,弱网丢包恢复后也能更早把新包发出去。
// Linux 的 TCP_NOTSENT_LOWAT 无标准 syscall 常量,直接使用 <netinet/tcp.h> 的 0x201。
const tcpNotsentLowat = 0x201

// setNotsentLowat 收紧发送队列积压阈值:游戏是低带宽小包流,16KB 足够覆盖
// 正常排队,同时保证包不被压在队列尾部等待。
func setNotsentLowat(c *net.TCPConn) {
	rc, err := c.SyscallConn()
	if err != nil {
		return
	}
	_ = rc.Control(func(fd uintptr) {
		_ = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_TCP, tcpNotsentLowat, 16*1024)
	})
}
