// Package dialout 提供代理**出站侧**的共用能力:客户端白名单判定、目标域名屏蔽、
// 带滑动 TTL 缓存的 DNS 解析,以及 happy-eyeballs 并发拨号。
//
// 它从原 internal/socks5(已被 internal/hy2 取代)里提取出来,让出站实现与入站协议解耦 ——
// 这些能力是游戏场景的真实痛点,若跟着入站协议走,换协议(如 SOCKS5 → hysteria2)就得重抄
// 一遍,迟早漂移:跨地域云服务器上的 DNS 抖动、某运营商黑洞掉部分 IP 的拨号等待,都是踩过的坑。
package dialout

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"
)

// SplitList 拆分逗号分隔的列表,去空与首尾空白。
func SplitList(s string) []string {
	var out []string
	for part := range strings.SplitSeq(s, ",") {
		if part = strings.TrimSpace(part); part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
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

// AddrOf 从 net.Addr 取出 IP。支持 TCP/UDP/IP 三种地址形态,
// 取不到时返回 false —— 白名单判定对「认不出的地址」一律拒绝,不做放行。
func AddrOf(a net.Addr) (netip.Addr, bool) {
	switch v := a.(type) {
	case *net.TCPAddr:
		return unmap(v.IP)
	case *net.UDPAddr:
		return unmap(v.IP)
	case *net.IPAddr:
		return unmap(v.IP)
	}
	return netip.Addr{}, false
}

func unmap(ip net.IP) (netip.Addr, bool) {
	a, ok := netip.AddrFromSlice(ip)
	if !ok {
		return netip.Addr{}, false
	}
	return a.Unmap(), true
}

// Allowed 判断远端地址是否命中白名单;白名单为空时放行一切。
func Allowed(addr net.Addr, allow []netip.Prefix) bool {
	if len(allow) == 0 {
		return true
	}
	ip, ok := AddrOf(addr)
	if !ok {
		return false
	}
	for _, p := range allow {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// BlockedHost 判断目标 host(域名或 IP)是否命中屏蔽名单。
// 域名匹配精确或子域(.前缀),大小写不敏感、容忍末尾点;IP 不参与域名屏蔽。
func BlockedHost(host string, block []string) bool {
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

// dialer 复用,拨号超时与 keep-alive 在此统一控制。
var dialer = func() *net.Dialer {
	d := &net.Dialer{
		Timeout: 10 * time.Second,
		// 保持 TCP keepalive,游戏长连接空闲断开更早被发现。
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
	inflight map[string]*dnsCall // 正在同步解析的 host(singleflight 去重)
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
	return dc.resolveSync(ctx, host)
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

// DialTarget 拨号到 target("host:port"),域名经 dns 缓存解析后对候选 IP
// 做 happy eyeballs 并发探测(首个立即,其余 fallbackDelay 后并发),取最先连通。
func DialTarget(ctx context.Context, target string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(target)
	if err != nil {
		return nil, err
	}
	addrs, err := dns.lookup(ctx, host)
	if err != nil {
		return nil, err
	}
	return DialAddrs(ctx, addrs, port)
}

// DialAddrs 对多个候选 IP 并发拨号。首个 IP 立即拨号;若 fallbackDelay 内未成功,
// 把其余 IP 全部并发拨号。任一连接成功即返回并取消其余拨号(避免连接风暴),
// 其余已建立的连接随即关闭,不留泄漏。
func DialAddrs(ctx context.Context, addrs []netip.Addr, port string) (net.Conn, error) {
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
	lastErr := firstErr
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
