//go:build !linux

package dialout

import "net"

// TuneTCP 在非 Linux 平台上为空实现:Tcp_QUICKACK 与 TCP_NOTSENT_LOWAT 都是 Linux 专属选项。
func TuneTCP(c net.Conn) {}
