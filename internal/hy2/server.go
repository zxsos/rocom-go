// Package hy2 提供**内嵌**的 hysteria2 服务端,取代原先的内置 SOCKS5 代理。
//
// 为什么换:公网上的 SOCKS5 有两个硬伤 —— RFC 1929 的用户名/密码是**明文**传输,
// 且握手特征明显,扫描器一眼就能认出这是开放代理并拿去滥用(白名单只能挡住不抓包的
// 那批)。hysteria2 的认证发生在 QUIC 隧道**内部**,链路上看不到凭据,也没有可指纹的
// 明文握手。
//
// 对抓包的影响:**没有**。两种入站的终点都一样 —— 流量在 VPS 上被解开后以本机 IP
// 出站,工具在物理网卡上照原样抓到(故 `-skip-self-ip=false` 仍是必需的)。
// 差别只在手机到 VPS 这一段怎么走:原来是 TCP 承载 TCP(弱网上有 TCP-over-TCP 问题),
// 现在是 QUIC(UDP)承载,丢包恢复更快。
package hy2

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	hyserver "github.com/apernet/hysteria/core/v2/server"

	"github.com/zxsos/rocom-go/internal/dialout"
)

const (
	// DefaultUpMbps / DefaultDownMbps 是给 hysteria 拥塞控制用的带宽声明。
	// 游戏是低带宽小包流(实测几十 KB/s 量级),20 Mbps 足够宽松又不至于让
	// 拥塞窗口失控。填 0 表示不设上限,但那样就少了 pacing,故默认给确定值。
	DefaultUpMbps   = 20
	DefaultDownMbps = 20

	// maxIdleTimeout 是 QUIC 的空闲超时(hy2 允许 4s~120s)。取上限 120s:
	// 手机切后台/锁屏时游戏心跳会暂停(Android 省电策略下可达数分钟),
	// 超时太短会让「只是暂时没流量」的连接被判死,玩家回到前台时游戏被迫重连,
	// 表现为一次明显卡顿 —— 被本包取代的原 SOCKS5 代理也有同样的空闲处理判断。
	maxIdleTimeout = 120 * time.Second

	dialTimeout = 10 * time.Second
)

// Params 是代理的**运行期可变**参数:监听地址之外的一切都在这里,
// 换它们不需要重新 bind(见 Server.SetParams)。
type Params struct {
	Allow    []netip.Prefix // 客户端白名单;空=不限制
	Block    []string       // 屏蔽的目标域名
	MaxConns int            // 并发上限;0=不限制
	Password string         // hy2 协议内认证密码
}

// Server 是一个在跑的 hysteria2 服务端。
//
// 它同时充当 hy2 的两个回调角色:Authenticator(认证 + 源 IP 白名单)与
// Outbound(出站拨号)。两者都从同一份 params 快照取值,避免一次连接中途
// 参数被换掉导致「按旧白名单放行、按新密码认证」这类半新半旧的组合。
type Server struct {
	pc   net.PacketConn
	srv  hyserver.Server
	cert *tls.Certificate

	params   atomic.Pointer[Params]
	inflight atomic.Int32 // 当前在处理的 TCP 连接数(配合 MaxConns)
}

// New 在 addr(UDP)上监听并返回已就绪的服务端(尚未开始 Serve)。
// cert 是服务端证书 —— hy2 必须跑在 TLS 上,这里复用 Web 那份自签证书。
func New(addr string, cert *tls.Certificate, p Params, upMbps, downMbps int) (*Server, error) {
	if cert == nil {
		return nil, errors.New("hy2: 未提供 TLS 证书(hy2 必须跑在 TLS 上)")
	}
	var lc net.ListenConfig
	pc, err := lc.ListenPacket(context.Background(), "udp", addr)
	if err != nil {
		return nil, err
	}
	s := &Server{pc: pc, cert: cert}
	s.params.Store(&p)

	cfg := &hyserver.Config{
		TLSConfig:     hyserver.TLSConfig{Certificates: []tls.Certificate{*cert}},
		Conn:          pc,
		Authenticator: s,
		Outbound:      s,
		EventLogger:   s,
		BandwidthConfig: hyserver.BandwidthConfig{
			MaxTx: mbpsToBytes(upMbps),
			MaxRx: mbpsToBytes(downMbps),
		},
		// 游戏是 TCP 8195,用不到 UDP 转发;关掉可少一条攻击面
		// (否则等于给任何人发 UDP 包的通道)。
		DisableUDP: true,
		QUICConfig: hyserver.QUICConfig{MaxIdleTimeout: maxIdleTimeout},
	}
	srv, err := hyserver.NewServer(cfg)
	if err != nil {
		_ = pc.Close()
		return nil, err
	}
	s.srv = srv
	return s, nil
}

// mbpsToBytes 把 Mbps 换算成 hy2 要求的字节/秒。
// hy2 规定非 0 值不得小于 65536,故小值一律抬到 65536 而不是报错。
func mbpsToBytes(mbps int) uint64 {
	if mbps <= 0 {
		return 0
	}
	b := uint64(mbps) * 1024 * 1024 / 8
	if b < 65536 {
		b = 65536
	}
	return b
}

// SetParams 换掉运行期参数,**立即对新连接生效**,不中断监听、不影响已建立的连接。
// 这是改密码/白名单/并发上限的通路(改地址走 Manager 重启监听)。
func (s *Server) SetParams(p Params) {
	s.params.Store(&p)
}

// Authenticate 是 hy2 的认证回调。auth 是客户端在**隧道内部**送来的凭据,
// 链路上不可见 —— 这正是它优于 RFC 1929 的地方。白名单也在这一步判:
// 判定发生在握手阶段,未授权的源连 QUIC 连接都建立不起来。
func (s *Server) Authenticate(addr net.Addr, auth string, tx uint64) (bool, string) {
	p := *s.params.Load()
	if !dialout.Allowed(addr, p.Allow) {
		log.Printf("hy2: 拒绝未授权客户端 %s", addr)
		return false, ""
	}
	if subtle.ConstantTimeCompare([]byte(auth), []byte(p.Password)) != 1 {
		log.Printf("hy2: 客户端 %s 认证失败", addr)
		return false, ""
	}
	return true, "rocom"
}

// TCP 是 hy2 的 TCP 出站回调:屏蔽名单检查 → 并发上限 → happy-eyeballs 拨号。
func (s *Server) TCP(reqAddr string) (net.Conn, error) {
	p := *s.params.Load()
	if host, _, err := net.SplitHostPort(reqAddr); err == nil && dialout.BlockedHost(host, p.Block) {
		return nil, fmt.Errorf("hy2: 目标域名在屏蔽名单内: %s", host)
	}
	// 并发上限用计数而非 channel 信号量:上限可在运行期改,
	// 而 channel 的容量改不了(重建会让已在跑的连接在错误的 channel 上归还令牌)。
	counted := p.MaxConns > 0
	if counted && int(s.inflight.Add(1)) > p.MaxConns {
		s.inflight.Add(-1)
		return nil, fmt.Errorf("hy2: 并发连接数已达上限(%d)", p.MaxConns)
	}
	ctx, cancel := context.WithTimeout(context.Background(), dialTimeout)
	defer cancel()
	conn, err := dialout.DialTarget(ctx, reqAddr)
	if err != nil {
		if counted {
			s.inflight.Add(-1)
		}
		return nil, err
	}
	dialout.TuneTCP(conn)
	if !counted {
		return conn, nil
	}
	return &countedConn{Conn: conn, n: &s.inflight}, nil
}

// UDP 与 CheckUDP 只为满足 hyserver.Outbound 接口:已在 Config 里 DisableUDP,
// 正常情况下不会被调用,真被调用时一律拒绝(不留一条对外发 UDP 的通道)。
func (s *Server) UDP(reqAddr string) (hyserver.UDPConn, error) {
	return nil, errors.New("hy2: 已禁用 UDP 转发")
}

// CheckUDP 同 UDP:UDP 转发整体关闭。
func (s *Server) CheckUDP(reqAddr string) error {
	return errors.New("hy2: 已禁用 UDP 转发")
}

// countedConn 在 Close 时归还并发计数的令牌,只归还一次。
type countedConn struct {
	net.Conn
	n    *atomic.Int32
	once sync.Once
}

func (c *countedConn) Close() error {
	c.once.Do(func() { c.n.Add(-1) })
	return c.Conn.Close()
}

// Serve 运行服务端,阻塞直至 Close 或监听出错。
func (s *Server) Serve() error {
	p := *s.params.Load()
	if len(p.Allow) > 0 {
		log.Printf("hy2 客户端白名单: %v", p.Allow)
	}
	if p.MaxConns > 0 {
		log.Printf("hy2 并发连接上限: %d", p.MaxConns)
	}
	return s.srv.Serve()
}

// Close 关停监听(hy2 内部会一并关掉 PacketConn)。
// 已在处理的连接**不受影响**(它们会在自己结束时退出),只是不再接受新连接。
func (s *Server) Close() error { return s.srv.Close() }

// Addr 返回实际监听地址(端口填 0 时由内核分配,此处给出真实端口)。
func (s *Server) Addr() net.Addr { return s.pc.LocalAddr() }

// ---- hyserver.EventLogger:只挑对排障有用的两条,其余留空实现 ----

// Connect 在客户端认证通过后被调用,记录来源便于核对「谁在用这条通道」。
func (s *Server) Connect(addr net.Addr, id string, tx uint64) {
	log.Printf("hy2: 客户端连接 %s (tx=%d)", addr, tx)
}

// Disconnect 在连接断开时被调用。异常断开(err 非 nil)才记,正常断开不刷日志。
func (s *Server) Disconnect(addr net.Addr, id string, err error) {
	if err != nil {
		log.Printf("hy2: 客户端断开 %s: %v", addr, err)
	}
}

// TCPRequest 记录每次 TCP 转发请求。游戏只连少数几个域名,量不大,直接记。
func (s *Server) TCPRequest(addr net.Addr, id, reqAddr string) {}

// TCPError 记录转发失败(拨号失败、被屏蔽、超限等),是排障的主要入口。
func (s *Server) TCPError(addr net.Addr, id, reqAddr string, err error) {
	log.Printf("hy2: 转发失败 %s → %s: %v", addr, reqAddr, err)
}

// UDPRequest / UDPError 在 DisableUDP 下不会被调用。
func (s *Server) UDPRequest(addr net.Addr, id string, sessionID uint32, reqAddr string) {}
func (s *Server) UDPError(addr net.Addr, id string, sessionID uint32, err error)        {}
