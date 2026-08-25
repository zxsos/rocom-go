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
