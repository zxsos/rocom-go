package server

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"
)

// webServer 托管 Web 服务的监听,使管理面板能在运行期改监听地址(「Web 服务」卡片)。
//
// ## 为什么改它比改代理危险得多
//
// 改 SOCKS5 代理,最坏是手机的游戏流量断一下,面板还在这里、随时能改回来。
// 改 Web 监听地址,改的是**管理员正用来改它的那条连接本身**:一旦新地址起不来
// (端口被占)或起得来却连不上(IP 填成 127.0.0.1、Docker 没映射新端口),
// 面板当场失联 —— 而它部署在网关上,人往往不在那台机器旁边。
//
// 故这里不做「保存即生效」,而是**试运行 → 确认**两步:
//
//	试运行   bind 新地址成功 → 新旧**同时**服务 → 配置文件一个字节都不动
//	确认     管理员在新地址上打开过面板 → 才落盘 + 停掉旧监听
//	超时     没人确认 → 自动回滚到旧地址,配置从未被改过
//
// 关键顺序是 **先起新的、成功后再停旧的**(与 socks5.Manager 同理):反过来
// 若先停旧的再起新的,新地址一旦 bind 失败就变成「新的起不来、旧的也没了」。
// 又因为配置文件只在确认时写,试运行期间无论进程怎么挂、地址多离谱,
// 重启后都还是旧地址 —— 改错的代价恒为零。
//
// ## 与 /etc/rocom.env 的关系
//
// 落盘由 api_web_addr.go 负责(webServer 不关心文件)。顺序仍守
// 「先落盘、再改内存」:ApplyPending 在切换之前调用传入的 save。
// 反过来的话,落盘失败而监听已换,界面显示新端口能用、重启后却回到旧端口。

const (
	// webReadHeaderTimeout / webIdleTimeout 与原先 main.go 里 serveWeb 的取值一致。
	// 不设 WriteTimeout/ReadTimeout:SSE 长连接(/api/stream)需要无写超时,
	// 设了会中断流式推送。
	webReadHeaderTimeout = 10 * time.Second
	webIdleTimeout       = 120 * time.Second

	// webOldShutdownGrace 是确认后留给旧监听的收尾时间:先关监听不再接入,
	// 已建立的连接(可能还有在途响应)这么久之后才强制关。
	webOldShutdownGrace = 3 * time.Second
)

// webTrialTimeout 是试运行的存活时长:到点无人确认即自动回滚。
//
// 是 var 而非 const:测试要把它压到毫秒级(见 api_web_addr_test.go),
// 否则「超时自动回滚」这条只能靠睡 90 秒去验。
var webTrialTimeout = 90 * time.Second

// errNoPending 表示当前没有待确认的试运行(确认/回滚时返回 409)。
// errBadHandoff 表示交接码不对或已被用过(返回 403)。
var (
	errNoPending  = errors.New("没有待确认的新地址")
	errBadHandoff = errors.New("交接码无效或已被使用")
)

// webServer 持有当前监听与可选的试运行监听。
type webServer struct {
	handler http.Handler
	tlsCfg  *tls.Config // nil = 明文 HTTP

	mu      sync.Mutex
	cur     *webInst // 当前提供服务的监听(唯一)
	pending *webInst // 试运行中的新监听,与 cur 并存
	timer   *time.Timer
}

// webInst 是一个在监听的实例。
type webInst struct {
	addr     string    // 请求监听时的地址原文(不是内核解析后的形态,理由见 socks5/manager.go)
	deadline time.Time // 试运行的自动回滚时刻;非试运行实例为零值
	handoff  string    // 试运行实例特有:一次性交接码(见 ApplyPendingWithHandoff)
	ln       net.Listener
	srv      *http.Server
}

// NewWebServer 创建 Web 服务托管。tlsCfg 非 nil 时各监听都走 HTTPS。
func NewWebServer(h http.Handler, tlsCfg *tls.Config) *webServer {
	return &webServer{handler: h, tlsCfg: tlsCfg}
}

// Listen 在 addr 上开始监听,**立即返回**(服务在后台 goroutine 里跑)。
// 只在 bind 失败时返回错误 —— 那通常是端口被占,由调用方决定是否致命。
//
// 刻意非阻塞:调用方(main 与测试)拿到 nil 就意味着「已经在监听了」,
// 不必再去 sleep 等它起来,也不用自己再包一层 goroutine。
func (w *webServer) Listen(addr string) error {
	it, err := w.newInst(addr, time.Time{})
	if err != nil {
		return err
	}
	w.mu.Lock()
	w.cur = it
	w.mu.Unlock()
	scheme := "http"
	extra := ""
	if w.tlsCfg != nil {
		scheme = "https"
		extra = " (自签证书,浏览器首次访问需手动信任)"
	}
	log.Printf("Web 界面: %s://localhost%s%s", scheme, showAddr(it.ln.Addr()), extra)
	go it.serve(w.tlsCfg != nil)
	return nil
}

// showAddr 把监听地址变成能照着点开的形态。
//
// 监听所有网卡时内核给回的是 [::]:4939(填 :4939 或 0.0.0.0:4939 都是它),
// 原样拼进 URL 就成了 "localhost[::]:4939" —— 而这一行日志正是让人照着去打开的。
func showAddr(a net.Addr) string {
	s := a.String()
	host, port, err := net.SplitHostPort(s)
	if err != nil {
		return s
	}
	if ip, err := netip.ParseAddr(host); err == nil && ip.IsUnspecified() {
		return ":" + port
	}
	return s
}

// Trial 试运行新地址:bind 成功后新旧**同时**服务,配置文件不动。
// 返回新地址的实际监听地址、端口号、自动回滚时刻(Unix 秒)与一次性交接码。
//
// bind 失败时 cur 一个字节都没动 —— 这是「不会变砖」的全部依据。
func (w *webServer) Trial(addr string) (real string, port int, deadline int64, handoff string, err error) {
	addr = strings.TrimSpace(addr)
	if err := validateListenAddr(addr); err != nil {
		return "", 0, 0, "", err
	}

	w.mu.Lock()
	if w.cur != nil && w.cur.addr == addr {
		w.mu.Unlock()
		return "", 0, 0, "", errors.New("新地址与当前监听地址相同")
	}
	// 连续改端口时,上一次尚未确认的试运行没有保留价值,先收掉它。
	// 但**只收 pending,绝不碰 cur**:此刻还没 bind 成功,收掉 cur 就没退路了。
	stale := w.pending
	w.pending = nil
	w.stopTimerLocked()
	w.mu.Unlock()
	if stale != nil {
		stale.close(0)
	}

	it, err := w.newInst(addr, time.Now().Add(webTrialTimeout))
	if err != nil {
		// 端口被占、地址写错都落在这里:返回错误,cur 照常服务
		return "", 0, 0, "", fmt.Errorf("监听 %s 失败: %w", addr, err)
	}
	it.handoff, err = newHandoffCode()
	if err != nil {
		it.close(0)
		return "", 0, 0, "", err
	}
	go it.serve(w.tlsCfg != nil)

	w.mu.Lock()
	w.pending = it
	// 用闭包捕获实例而非读 w.pending:定时器已在跑时若又开了新的试运行,
	// Stop 未必赶得上前者的 goroutine,按实例比对才不会误收新的那份。
	w.timer = time.AfterFunc(webTrialTimeout, func() { w.expire(it) })
	w.mu.Unlock()

	log.Printf("Web 地址试运行: %s(与现有 %s 并存,等待确认)", it.ln.Addr().String(), w.curAddr())
	return it.ln.Addr().String(), portOf(it.ln.Addr()), it.deadline.Unix(), it.handoff, nil
}

// ApplyPending 确认试运行:先经 save 落盘,成功后新监听转正、旧监听收尾。
func (w *webServer) ApplyPending(save func(addr string) error) (addr, real string, err error) {
	return w.applyPending("", save)
}

// ApplyPendingWithHandoff 用一次性交接码确认,**不要求管理员令牌**。
//
// 为什么需要它:管理员令牌存在浏览器的 localStorage 里,而它是**按源**(协议+主机+端口)
// 隔离的。换端口 = 换源,令牌不会跟过去 —— 管理员兴冲冲打开新地址,看到的却是登录页,
// 于是「打开新地址」这个动作根本没法作为确认的证据。
//
// 交接码把这个环补上:试运行时生成一个随机码,管理员点的链接里带着它,新源凭此
// 一次性完成确认并接上会话。它比把令牌直接放进 URL 安全得多 —— 用完即废、
// 随试运行一起过期,即便被截图留存也已失效(这个仓库对截图泄露是有要求的,
// 见前端的隐私遮罩)。
func (w *webServer) ApplyPendingWithHandoff(code string, save func(addr string) error) (addr, real string, err error) {
	return w.applyPending(code, save)
}

// applyPending 确认的公共实现。code 非空时按交接码校验,否则按管理员已鉴权处理
// (鉴权在 handler 里做,见 api_web_addr.go)。
//
// save 在持锁期间调用(它只写文件、不回调 webServer,故不会自锁):
// 「读待确认地址 → 落盘 → 切换」必须是一个原子过程,否则定时器可能恰好在
// 落盘前把试运行收掉,于是写进配置文件的是一个已经不存在的监听。
func (w *webServer) applyPending(code string, save func(addr string) error) (addr, real string, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	next := w.pending
	if next == nil {
		return "", "", errNoPending
	}
	if code != "" {
		if subtle.ConstantTimeCompare([]byte(code), []byte(next.handoff)) != 1 {
			return "", "", errBadHandoff
		}
		next.handoff = "" // 一次性
	}
	if err := save(next.addr); err != nil {
		return "", "", err // 落盘失败:保留待确认状态,管理员可以重试
	}
	old := w.cur
	w.cur = next
	w.pending = nil
	w.stopTimerLocked()
	if old != nil {
		// 异步收尾:管理员可能是从**旧地址**点的「保留」,那条响应还在旧连接上。
		// 同步关会把响应一起掐掉,而 close 内部又要等活跃连接 —— 于是自己等自己。
		go old.close(webOldShutdownGrace)
	}
	log.Printf("Web 地址已切换: %s(原 %s 停止服务)", next.ln.Addr().String(), addrOf(old))
	return next.addr, next.ln.Addr().String(), nil
}

// newHandoffCode 生成一个一次性交接码。
func newHandoffCode() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("生成交接码失败: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// Revert 放弃试运行:关掉新监听,旧地址与配置文件都不受影响。
func (w *webServer) Revert() error {
	w.mu.Lock()
	it := w.pending
	w.pending = nil
	w.stopTimerLocked()
	w.mu.Unlock()
	if it == nil {
		return errNoPending
	}
	log.Printf("Web 地址试运行已回滚,继续监听 %s", w.curAddr())
	it.close(0)
	return nil
}

// Status 返回当前监听地址与待确认的试运行信息(pending 为空表示没有)。
func (w *webServer) Status() (addr, real string, pend *webPendingJSON) {
	w.mu.Lock()
	defer w.mu.Unlock()
	var a, r string
	if w.cur != nil {
		a, r = w.cur.addr, w.cur.ln.Addr().String()
	}
	var p *webPendingJSON
	if w.pending != nil {
		p = &webPendingJSON{
			Addr:     w.pending.addr,
			RealAddr: w.pending.ln.Addr().String(),
			Port:     portOf(w.pending.ln.Addr()),
			Deadline: w.pending.deadline.Unix(),
		}
	}
	return a, r, p
}

// expire 定时回滚:仅当待确认的仍是触发定时器的那一个实例时才收。
func (w *webServer) expire(it *webInst) {
	w.mu.Lock()
	if w.pending != it {
		w.mu.Unlock()
		return
	}
	w.pending = nil
	w.timer = nil
	w.mu.Unlock()
	log.Printf("Web 地址试运行超时未确认,已自动回滚(继续监听 %s)", w.curAddr())
	it.close(0)
}

// stopTimerLocked 停掉定时器(调用方须持有 mu)。
func (w *webServer) stopTimerLocked() {
	if w.timer != nil {
		w.timer.Stop()
		w.timer = nil
	}
}

func (w *webServer) curAddr() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return addrOf(w.cur)
}

// newInst 建立监听并配好 http.Server,**不开始服务**。
func (w *webServer) newInst(addr string, deadline time.Time) (*webInst, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	return &webInst{
		addr:     addr,
		deadline: deadline,
		ln:       ln,
		srv: &http.Server{
			Handler:           w.handler,
			TLSConfig:         w.tlsCfg,
			ReadHeaderTimeout: webReadHeaderTimeout,
			IdleTimeout:       webIdleTimeout,
		},
	}, nil
}

// serve 服务这个监听,直到它被关闭。
func (it *webInst) serve(useTLS bool) {
	var err error
	if useTLS {
		err = it.srv.ServeTLS(it.ln, "", "")
	} else {
		err = it.srv.Serve(it.ln)
	}
	// Serve 只在监听器被关时返回;关监听是我们自己收尾的正常路径,不必记。
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Printf("Web 监听 %s 异常退出: %v", it.addr, err)
	}
}

// close 停掉这个监听。
//
// grace > 0 时走 Shutdown:先关监听不再接受新连接,已建立的给这么久时间收尾。
// grace == 0 时直接 Close(试运行的实例上通常没有任何连接,没必要等)。
// 用 Shutdown 而非 Close 是为了在途响应 —— 见 ApplyPending 里的注释。
func (it *webInst) close(grace time.Duration) {
	if grace <= 0 {
		it.srv.Close()
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()
	if err := it.srv.Shutdown(ctx); err != nil {
		it.srv.Close() // 超时后强制关掉剩余连接(SSE 长连接不会自己结束)
	}
}

func addrOf(it *webInst) string {
	if it == nil {
		return ""
	}
	return it.addr
}

// portOf 取监听地址里的端口号,取不到(非 TCP)返回 0。
func portOf(a net.Addr) int {
	ta, ok := a.(*net.TCPAddr)
	if !ok {
		return 0
	}
	return ta.Port
}

// validateListenAddr 校验监听地址:形如 host:port,端口 0~65535。
// 比让 net.Listen 自己去撞错误更早给出可读提示(它只报 "invalid port")。
// host 可为空(监听所有网卡);IPv6 须带方括号,由前端 joinAddr 保证。
func validateListenAddr(addr string) error {
	if addr == "" {
		return errors.New("监听地址不能为空")
	}
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("监听地址须为 IP:端口 形式(%q): %w", addr, err)
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 0 || n > 65535 {
		return fmt.Errorf("端口须为 0~65535 的数字(%q)", port)
	}
	return nil
}
