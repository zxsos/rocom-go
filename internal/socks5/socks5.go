// Package socks5 提供极简的 RFC 1928 SOCKS5 代理服务器(仅 TCP CONNECT,无认证)。
// 面向"云服务器当网关抓自己进程出站流量"的部署场景:手机把游戏流量代理到本机,
// rocom-capture 整网卡抓包即可看到代理进程以本机 IP 出站的连接(须配合 -skip-self-ip=false)。
package socks5

import (
	"errors"
	"io"
	"log"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

const (
	verSocks5      = 0x05
	methodNone     = 0x00 // 无认证(裸代理,部署方自行决定安全边界)
	methodUserPass = 0x02 // RFC 1929 用户名/密码认证
	authVersion    = 0x01 // RFC 1929 认证子协商版本
	cmdConnect     = 0x01 // 仅支持 CONNECT,TCP 游戏流量用不到 BIND/UDP
	atypIPv4       = 0x01
	atypDomain     = 0x03
	atypIPv6       = 0x04
	repSuccess     = 0x00
	repConnRefused = 0x05
)

// ListenAndServe 在 addr 上监听并处理 SOCKS5 连接,阻塞直至监听器关闭。
// allow 非空时仅允许匹配的客户端 IP 接入(支持 IP 或 CIDR),用于挡住公网扫描器;
// maxConns > 0 时限制同时处理的连接数,超限直接拒绝,防止连接风暴拖垮同进程的 Web 服务;
// user 非空时启用 RFC 1929 用户名/密码认证,pass 为对应密码。
func ListenAndServe(addr string, allow []netip.Prefix, maxConns int, user, pass string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	if user == "" {
		log.Printf("socks5 代理已启动: %s (无认证)", addr)
	} else {
		log.Printf("socks5 代理已启动: %s (用户名/密码认证,用户=%s)", addr, user)
	}
	if len(allow) > 0 {
		log.Printf("socks5 客户端白名单: %v", allow)
	}
	var sem chan struct{}
	if maxConns > 0 {
		sem = make(chan struct{}, maxConns)
		log.Printf("socks5 并发连接上限: %d", maxConns)
	}
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		if !allowed(conn.RemoteAddr(), allow) {
			conn.Close()
			log.Printf("socks5: 拒绝未授权客户端 %s", conn.RemoteAddr())
			continue
		}
		if sem != nil {
			select {
			case sem <- struct{}{}:
			default:
				conn.Close()
				log.Printf("socks5: 并发连接数已达上限(%d),拒绝 %s", maxConns, conn.RemoteAddr())
				continue
			}
		}
		go func(c net.Conn) {
			if sem != nil {
				defer func() { <-sem }()
			}
			handle(c, user, pass)
		}(conn)
	}
}

// ParseAllow 解析逗号分隔的客户端 IP 白名单,支持 IP 或 CIDR 网段。
func ParseAllow(s string) ([]netip.Prefix, error) {
	var prefs []netip.Prefix
	for part := range strings.SplitSeq(s, ",") {
		if part = strings.TrimSpace(part); part == "" {
			continue
		}
		if strings.Contains(part, "/") {
			p, err := netip.ParsePrefix(part)
			if err != nil {
				return nil, err
			}
			prefs = append(prefs, p)
			continue
		}
		a, err := netip.ParseAddr(part)
		if err != nil {
			return nil, err
		}
		prefs = append(prefs, netip.PrefixFrom(a, a.BitLen()))
	}
	return prefs, nil
}

// allowed 判断远端地址是否命中白名单;白名单为空时放行一切。
func allowed(addr net.Addr, allow []netip.Prefix) bool {
	if len(allow) == 0 {
		return true
	}
	a, ok := addr.(*net.TCPAddr)
	if !ok {
		return false
	}
	ip, ok := netip.AddrFromSlice(a.IP)
	if !ok {
		return false
	}
	ip = ip.Unmap()
	for _, p := range allow {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// handle 处理单个客户端连接:握手(含可选认证) → 拨号上游 → 双向转发。
func handle(c net.Conn, user, pass string) {
	defer c.Close()
	target, err := handshake(c, user, pass)
	if err != nil {
		log.Printf("socks5: %s 握手失败: %v", c.RemoteAddr(), err)
		return
	}
	up, err := net.DialTimeout("tcp", target, 10*time.Second)
	if err != nil {
		writeReply(c, repConnRefused) // 回复失败,客户端据此断开
		log.Printf("socks5: 连接 %s 失败: %v", target, err)
		return
	}
	defer up.Close()
	if err := writeReply(c, repSuccess); err != nil {
		return
	}
	log.Printf("socks5: %s → %s", c.RemoteAddr(), target)
	relay(c, up)
	log.Printf("socks5: 断开 %s → %s", c.RemoteAddr(), target)
}

// handshake 完成版本协商、认证(可选)与 CONNECT 请求解析,返回目标 "host:port"。
// user 非空时要求 RFC 1929 用户名/密码认证,否则按无认证协商。
func handshake(c net.Conn, user, pass string) (string, error) {
	c.SetReadDeadline(time.Now().Add(10 * time.Second))
	defer c.SetReadDeadline(time.Time{})
	var hdr [2]byte
	if _, err := io.ReadFull(c, hdr[:]); err != nil {
		return "", err
	}
	if hdr[0] != verSocks5 {
		return "", errors.New("非 socks5 版本")
	}
	methods := make([]byte, int(hdr[1]))
	if _, err := io.ReadFull(c, methods); err != nil {
		return "", err
	}
	if user != "" {
		// 要求认证:客户端须声明支持 username/password 方法
		if !hasMethod(methods, methodUserPass) {
			if _, err := c.Write([]byte{verSocks5, 0xFF}); err != nil {
				return "", err
			}
			return "", errors.New("客户端不支持 username/password 认证")
		}
		if _, err := c.Write([]byte{verSocks5, methodUserPass}); err != nil {
			return "", err
		}
		if err := authUserPass(c, user, pass); err != nil {
			return "", err
		}
	} else if _, err := c.Write([]byte{verSocks5, methodNone}); err != nil {
		return "", err
	}
	var req [4]byte // VER CMD RSV ATYP
	if _, err := io.ReadFull(c, req[:]); err != nil {
		return "", err
	}
	if req[0] != verSocks5 || req[2] != 0 {
		return "", errors.New("非法请求头")
	}
	if req[1] != cmdConnect {
		return "", errors.New("仅支持 CONNECT")
	}
	var host string
	switch req[3] {
	case atypIPv4:
		var ip [4]byte
		if _, err := io.ReadFull(c, ip[:]); err != nil {
			return "", err
		}
		host = net.IP(ip[:]).String()
	case atypDomain:
		var ln [1]byte
		if _, err := io.ReadFull(c, ln[:]); err != nil {
			return "", err
		}
		dm := make([]byte, int(ln[0]))
		if _, err := io.ReadFull(c, dm); err != nil {
			return "", err
		}
		host = string(dm)
	case atypIPv6:
		var ip [16]byte
		if _, err := io.ReadFull(c, ip[:]); err != nil {
			return "", err
		}
		host = net.IP(ip[:]).String()
	default:
		return "", errors.New("非法地址类型")
	}
	var port [2]byte
	if _, err := io.ReadFull(c, port[:]); err != nil {
		return "", err
	}
	return net.JoinHostPort(host, strconv.Itoa(int(port[0])<<8|int(port[1]))), nil
}

// hasMethod 判断 methods 列表中是否含指定认证方法。
func hasMethod(methods []byte, m byte) bool {
	for _, x := range methods {
		if x == m {
			return true
		}
	}
	return false
}

// authUserPass 完成 RFC 1929 用户名/密码认证子协商,校验失败返回错误。
func authUserPass(c net.Conn, wantUser, wantPass string) error {
	var hdr [2]byte // VER ULEN
	if _, err := io.ReadFull(c, hdr[:]); err != nil {
		return err
	}
	if hdr[0] != authVersion {
		return errors.New("非法认证版本")
	}
	uname := make([]byte, int(hdr[1]))
	if _, err := io.ReadFull(c, uname); err != nil {
		return err
	}
	var plen [1]byte
	if _, err := io.ReadFull(c, plen[:]); err != nil {
		return err
	}
	passwd := make([]byte, int(plen[0]))
	if _, err := io.ReadFull(c, passwd); err != nil {
		return err
	}
	if string(uname) == wantUser && string(passwd) == wantPass {
		_, err := c.Write([]byte{authVersion, 0x00}) // 认证成功
		return err
	}
	_, _ = c.Write([]byte{authVersion, 0x01}) // 认证失败
	return errors.New("认证失败")
}

// writeReply 回 CONNECT 应答(BND.ADDR 固定 0.0.0.0:0,客户端不关心)。
func writeReply(c net.Conn, rep byte) error {
	_, err := c.Write([]byte{verSocks5, rep, 0, atypIPv4, 0, 0, 0, 0, 0, 0})
	return err
}

// relay 双向转发,任一端结束即关闭双方连接。
func relay(a, b net.Conn) {
	done := make(chan struct{}, 1)
	go func() { io.Copy(b, a); done <- struct{}{} }()
	go func() { io.Copy(a, b); done <- struct{}{} }()
	<-done
	a.Close()
	b.Close()
}
