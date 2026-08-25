//go:build !linux

package socks5

import "net"

// setQuickAck 在非 Linux 平台上为空实现(TCP_QUICKACK 是 Linux 专属选项)。
func setQuickAck(_ *net.TCPConn) {}
