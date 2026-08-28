// Package socks5 提供极简的 RFC 1928 SOCKS5 代理服务器(仅 TCP CONNECT,无认证)。
// 面向"云服务器当网关抓自己进程出站流量"的部署场景:手机把游戏流量代理到本机,
// rocom-capture 整网卡抓包即可看到代理进程以本机 IP 出站的连接(须配合 -skip-self-ip=false)。
package socks5

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync"
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
	repNotAllowed  = 0x02 // 规则集禁止连接(域名屏蔽命中时回复,客户端据此放弃)
	repConnRefused = 0x05
)

// ListenAndServe 在 addr 上监听并处理 SOCKS5 连接,阻塞直至监听器关闭。
// allow 非空时仅允许匹配的客户端 IP 接入(支持 IP 或 CIDR),用于挡住公网扫描器;
// block 非空时屏蔽匹配的目标域名(精确或子域),在拨号前直接拒绝——手机系统的
// 连通性探测(google.com/example.com 等)不属于游戏流量,拦下可避免反复拨号失败刷日志;
// maxConns > 0 时限制同时处理的连接数,超限直接拒绝,防止连接风暴拖垮同进程的 Web 服务;
// user 非空时启用 RFC 1929 用户名/密码认证,pass 为对应密码。
func ListenAndServe(addr string, allow []netip.Prefix, block []string, maxConns int, user, pass string) error {
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
			handle(c, user, pass, block)
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

// blockedHost 判断目标 host(域名或 IP)是否命中屏蔽名单。
// 域名匹配精确或子域(.前缀),大小写不敏感、容忍末尾点;IP 不参与域名屏蔽。
func blockedHost(host string, block []string) bool {
	if len(block) == 0 || net.ParseIP(host) != nil {
		return false
	}
	h := strings.ToLower(strings.TrimSuffix(host, "."))
	for _, b := range block {
		b = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(b), "."))
		if b == "" {
			continue
		}
		if h == b || strings.HasSuffix(h, "."+b) {
			return true
		}
	}
	return false
}

// dialer 复用，拨号超时与 keep-alive 在此统一控制。
var dialer = func() *net.Dialer {
	d := &net.Dialer{
		Timeout: 10 * time.Second,
		// 保持 TCP keepalive，游戏长连接空闲断开更早被发现。
		KeepAlive: 30 * time.Second,
	}
	return d
}()

// dnsCache 带 TTL 缓存的域名→IP 解析:游戏反复连接同一批域名(登录服/网关/
// 大区服),跨地域云服务器上每次 DNS 解析可能增加几十~几百毫秒延迟。
// 缓存解析结果,TTL 内直连 IP,消除重复 DNS 往返。
//
// 关键设计:滑动 TTL + 后台异步刷新(singleflight 去重)。
// 早期实现用固定 5 分钟硬过期,多域名在同一时间窗建立后会在几乎同一时刻集体过期,
// 下一次请求命中过期缓存时触发批量同步 DNS 解析,在跨地域云服务器上表现为
// 「隔几分钟延迟飙升到几百 ms、持续数秒」的周期性抖动。
// 现在改为:TTL 到期后不立即同步重解析,而是返回旧值(最多容忍 staleTTL 时长),
// 同时用 singleflight 在后台异步刷新——首个触发的请求发起一次解析,
// 后续并发请求直接复用旧值,解析完成后更新缓存并重置 TTL。
// dnsCall 是一次同步解析的执行记录:同一 host 的并发请求合并为一次解析,
// 后到者等待前者的结果,避免登录等场景多连接同时新建时对同一域名重复
// getaddrinfo(同步解析阻塞在 libc 里,重复只会放大首次连接的延迟)。
type dnsCall struct {
	done chan struct{}
	ips  []netip.Addr
	err  error
}

type dnsCache struct {
	mu       sync.Mutex
	ttl      time.Duration
	stale    time.Duration // 过期后仍可容忍返回旧值的时长(滑动窗口)
	m        map[string][]netip.Addr
	t        map[string]time.Time
	refresh  map[string]struct{} // 正在后台刷新的 host(去重,避免惊群)
	inflight map[string]*dnsCall  // 正在同步解析的 host(singleflight 去重)
}

var dns = &dnsCache{
	ttl:      5 * time.Minute,
	stale:    30 * time.Second, // 过期后 30s 内仍返回旧值,后台异步刷新
	m:        map[string][]netip.Addr{},
	t:        map[string]time.Time{},
	refresh:  map[string]struct{}{},
	inflight: map[string]*dnsCall{},
}

// lookup 返回 host 的解析结果。缓存命中(含 stale 期内)直接返回;
// 过期且超出 stale 窗口时同步解析;过期但在 stale 窗口内时返回旧值并触发后台异步刷新。
func (dc *dnsCache) lookup(ctx context.Context, host string) ([]netip.Addr, error) {
	if ip, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{ip}, nil // 已是 IP,无需解析
	}

	dc.mu.Lock()
	until, ok := dc.t[host]
	now := time.Now()
	if ok {
		addrs := dc.m[host]
		if now.Before(until) {
			// 未过期,直接返回
			dc.mu.Unlock()
			return addrs, nil
		}
		staleUntil := until.Add(dc.stale)
		if now.Before(staleUntil) && len(addrs) > 0 {
			// 过期但在 stale 窗口内:返回旧值,后台异步刷新(去重)
			if _, refreshing := dc.refresh[host]; !refreshing {
				dc.refresh[host] = struct{}{}
				dc.mu.Unlock()
				go dc.refreshHost(host)
			} else {
				dc.mu.Unlock()
			}
			return addrs, nil
		}
	}
	dc.mu.Unlock()

	// 未命中或超出 stale 窗口:同步解析(同一 host 的并发请求合并为一次)
	ips, err := dc.resolveSync(ctx, host)
	if err != nil {
		return nil, err
	}
	return ips, nil
}

// resolveSync 同步解析并更新缓存,同时把同一 host 的并发请求合并为一次解析。
func (dc *dnsCache) resolveSync(ctx context.Context, host string) ([]netip.Addr, error) {
	dc.mu.Lock()
	if c, ok := dc.inflight[host]; ok {
		// 已有请求在解析,等它的结果
		dc.mu.Unlock()
		<-c.done
		return c.ips, c.err
	}
	c := &dnsCall{done: make(chan struct{})}
	dc.inflight[host] = c
	dc.mu.Unlock()

	ips, err := dc.resolve(ctx, host)

	dc.mu.Lock()
	c.ips, c.err = ips, err
	close(c.done)
	delete(dc.inflight, host)
	dc.mu.Unlock()
	return ips, err
}

// resolve 执行实际 DNS 解析并更新缓存。用默认 Resolver(PreferGo=false 走系统 libc,
// 兼顾 /etc/hosts 与 mDNS),结果缓存到 TTL。
func (dc *dnsCache) resolve(ctx context.Context, host string) ([]netip.Addr, error) {
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	dc.mu.Lock()
	dc.m[host] = ips
	dc.t[host] = time.Now().Add(dc.ttl)
	dc.mu.Unlock()
	return ips, nil
}

// refreshHost 在后台异步刷新单个 host 的 DNS 记录。
// 使用独立 context(不受请求生命周期影响),解析失败时保留旧值不动,
// 避免短暂 DNS 故障导致缓存被清空。
func (dc *dnsCache) refreshHost(host string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	dc.mu.Lock()
	delete(dc.refresh, host)
	if err == nil && len(ips) > 0 {
		dc.m[host] = ips
		dc.t[host] = time.Now().Add(dc.ttl)
	}
	// 解析失败:保留旧值,等下次 stale 窗口再重试
	dc.mu.Unlock()
}

// fallbackDelay 是 happy eyeballs 的第二批拨号延迟:首个 IP 立即拨号,
// 若 fallbackDelay 内未成功则并发拨其余全部 IP,取最先连通的连接。
// 跨地域云服务器上某运营商路由可能黑洞掉部分 IP,串行逐个尝试会让客户端
// 空等到拨号超时;并发探测把「发现首个 IP 不可达」的代价压到 fallbackDelay。
// 首个 IP 快速失败(RST/拒绝)时 select 会立即拿到结果,不受此值影响;
// 此值只影响「首个 IP 挂起无响应」的等待时长,云服务器常见 RTT 下 200ms 足够,
// 再大只会让黑洞场景的连接建立白白多等。
const fallbackDelay = 200 * time.Millisecond

// dialTarget 拨号到 target("host:port"),域名经 dns 缓存解析后对候选 IP
// 做 happy eyeballs 并发探测(首个立即,其余 fallbackDelay 后并发),取最先连通。
func dialTarget(ctx context.Context, target string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(target)
	if err != nil {
		return nil, err
	}
	addrs, err := dns.lookup(ctx, host)
	if err != nil {
		return nil, err
	}
	return dialAddrs(ctx, addrs, port)
}

// dialAddrs 对多个候选 IP 并发拨号。首个 IP 立即拨号;若 fallbackDelay 内未成功,
// 把其余 IP 全部并发拨号。任一连接成功即返回并取消其余拨号(避免连接风暴),
// 其余已建立的连接随即关闭,不留泄漏。
func dialAddrs(ctx context.Context, addrs []netip.Addr, port string) (net.Conn, error) {
	if len(addrs) == 0 {
		return nil, errors.New("无可连接地址")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	type res struct {
		conn net.Conn
		err  error
	}
	results := make(chan res, len(addrs))
	var wg sync.WaitGroup
	dialOne := func(a netip.Addr) {
		defer wg.Done()
		conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(a.String(), port))
		if err != nil && ctx.Err() != nil {
			return // 已被更快成功的连接取消,忽略结果
		}
		results <- res{conn, err}
	}

	var firstErr error
	wg.Add(1)
	go dialOne(addrs[0])
	timer := time.NewTimer(fallbackDelay)
	defer timer.Stop()
	select {
	case r := <-results:
		if r.conn != nil {
			cancel()
			wg.Wait()
			return r.conn, nil
		}
		firstErr = r.err
	case <-timer.C:
	}
	// 首个未成功:并发拨其余全部 IP
	for _, a := range addrs[1:] {
		wg.Add(1)
		go dialOne(a)
	}
	wg.Wait()
	close(results)
	var lastErr = firstErr
	for r := range results {
		if r.conn != nil {
			cancel()
			// 关闭并发探测中已建立的其他连接,避免泄漏
			for extra := range results {
				if extra.conn != nil && extra.conn != r.conn {
					extra.conn.Close()
				}
			}
			return r.conn, nil
		}
		if r.err != nil {
			lastErr = r.err
		}
	}
	if lastErr == nil {
		lastErr = errors.New("无可连接地址")
	}
	return nil, lastErr
}

// handle 处理单个客户端连接:握手(含可选认证) → 域名屏蔽检查 → 拨号上游 → 双向转发。
func handle(c net.Conn, user, pass string, block []string) {
	defer c.Close()
	target, err := handshake(c, user, pass)
	if err != nil {
		log.Printf("socks5: %s 握手失败: %v", c.RemoteAddr(), err)
		return
	}
	// 域名屏蔽:命中名单(如手机系统连通性探测 google.com/example.com)在拨号前拒绝,
	// 免去一次注定失败的 DNS+拨号,也不再刷「连接失败」日志。
	if host, _, err := net.SplitHostPort(target); err == nil && blockedHost(host, block) {
		writeReply(c, repNotAllowed)
		log.Printf("socks5: 屏蔽 %s → %s", c.RemoteAddr(), target)
		return
	}
	// 先写成功回复再拨号会让客户端空等一个 RTT；这里先拨号，成功后才回复。
	// 拨号用 context 以便后续可扩展取消。
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	up, err := dialTarget(ctx, target)
	if err != nil {
		writeReply(c, repConnRefused) // 回复失败,客户端据此断开
		log.Printf("socks5: 连接 %s 失败: %v", target, err)
		return
	}
	defer up.Close()

	// 禁用 Nagle 算法:游戏流量多为小包(指令/心跳),Nagle 会攒包增加延迟。
	// 同时增大 socket 缓冲区,减少 syscall 次数。
	// 客户端连接(Accept 来的)默认不带 keepalive,手机 WiFi 抖动/休眠会导致半开连接,
	// relay 中 io.Copy 永久阻塞、goroutine 与信号量 slot 泄露,表现为周期性延迟飙升。
	// 显式开启 keepalive + 较短探测间隔,让半开连接在 ~75s 内被发现并回收。
	if tc, ok := up.(*net.TCPConn); ok {
		_ = tc.SetNoDelay(true)
		_ = tc.SetReadBuffer(64 * 1024)
		_ = tc.SetWriteBuffer(64 * 1024)
		_ = tc.SetKeepAlive(true)
		_ = tc.SetKeepAlivePeriod(30 * time.Second) // dialer 已设 30s,这里显式兜底
		setQuickAck(tc)
	}
	if tc, ok := c.(*net.TCPConn); ok {
		_ = tc.SetNoDelay(true)
		_ = tc.SetReadBuffer(64 * 1024)
		_ = tc.SetWriteBuffer(64 * 1024)
		_ = tc.SetKeepAlive(true)
		_ = tc.SetKeepAlivePeriod(30 * time.Second)
		setQuickAck(tc)
	}

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

// bufPool 复用转发缓冲区:每个方向的 io.Copy 各需一块 32KB 缓冲,
// 高并发连接下复用可避免频繁分配/GC,降低转发路径上的停顿。
var bufPool = sync.Pool{
	New: func() any { b := make([]byte, 32*1024); return &b },
}

// relayIdleTimeout 是中继方向的最大空闲时长:若某方向在此期间无数据可读,
// 则认为连接已半开/死掉,主动关闭双方连接。
// 游戏心跳间隔通常远小于此值,正常流量不会触发;仅用于清理静默断开的连接,
// 防止 io.Copy 永久阻塞 → goroutine 与信号量泄露 → 周期性延迟飙升。
const relayIdleTimeout = 2 * time.Minute

// idleReader 在读取前设置滚动 ReadDeadline:每次读成功后顺延 deadline,
// 若 relayIdleTimeout 内无数据则读返回超时错误,relay 据此关闭连接。
type idleReader struct {
	r net.Conn
	d time.Duration
	q *net.TCPConn // 仅 TCP 连接非 nil,用于每次读前重设 TCP_QUICKACK
}

func newIdleReader(r net.Conn, d time.Duration) *idleReader {
	ir := &idleReader{r: r, d: d}
	if tc, ok := r.(*net.TCPConn); ok {
		ir.q = tc
	}
	return ir
}

func (ir *idleReader) Read(p []byte) (int, error) {
	// 每次读之前刷新 deadline,实现「空闲计时器」效果
	_ = ir.r.SetReadDeadline(time.Now().Add(ir.d))
	// TCP_QUICKACK 是一次性开关:内核在首次生效后自动关闭,回到 delayed ACK
	// (约 40ms)。游戏是高频小包的双向交互,这里每次读前重设,保证每个数据包
	// 都立即回 ACK,避免每轮交互白白多等一个 delayed ACK 周期。
	// 非 TCP 连接(如测试用内存 conn)跳过。
	if ir.q != nil {
		setQuickAck(ir.q)
	}
	return ir.r.Read(p)
}

// copyWithIdle 带空闲超时的双向拷贝。
// 任一方向结束(含 idle 超时)立即关闭对端,触发其 io.Copy 也立即返回。
func copyWithIdle(dst, src net.Conn, buf []byte) {
	ir := newIdleReader(src, relayIdleTimeout)
	io.CopyBuffer(dst, ir, buf)
	// 读结束后清除 deadline,避免影响后续可能的 close 逻辑
	_ = src.SetReadDeadline(time.Time{})
}

// relay 双向转发,任一端结束即关闭双方连接。
// 用 sync.WaitGroup 确保两个 goroutine 都退出后再返回;
// 任一方向 io.Copy 返回立即 Close 对端,触发其 io.Copy 也立即返回,
// 避免一端已断开另一端仍阻塞在读上增加尾延迟。
// 每个方向带 relayIdleTimeout 空闲超时,清理静默断开的半开连接。
func relay(a, b net.Conn) {
	// 只关闭一次,且先关闭写端再读端:关闭 a 让「b→a」方向立即 EOF,
	// 关闭 b 让「a→b」方向立即 EOF,两个 io.Copy 随即返回,无额外等待。
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		buf := bufPool.Get().(*[]byte)
		defer bufPool.Put(buf)
		copyWithIdle(b, a, *buf)
		b.Close()
	}()
	go func() {
		defer wg.Done()
		buf := bufPool.Get().(*[]byte)
		defer bufPool.Put(buf)
		copyWithIdle(a, b, *buf)
		a.Close()
	}()
	wg.Wait()
}
