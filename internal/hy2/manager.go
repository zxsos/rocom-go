package hy2

import (
	"crypto/tls"
	"fmt"
	"log"
	"sync"
	"sync/atomic"

	"github.com/zxsos/rocom-go/internal/dialout"
)

// Config 是一份完整的代理配置(管理面板编辑的就是这份)。
// 字段与启动 flag 一一对应,空 Addr 表示「不启用代理」。
type Config struct {
	Addr     string // UDP 监听地址,如 :11443;空=不启用
	Password string // hy2 协议内认证密码(隧道内传输,非明文;空=任何人可连)
	Allow    string // 客户端 IP/CIDR 白名单(逗号分隔);空=不限制
	Block    string // 屏蔽的目标域名(逗号分隔);空=不屏蔽
	UpMbps   int    // 上行带宽(Mbps),供 hy2 拥塞控制用;<=0 取 DefaultUpMbps
	DownMbps int    // 下行带宽(Mbps);<=0 取 DefaultDownMbps
	MaxConns int    // 并发上限;0=不限制
}

// Validate 校验配置是否可用:白名单可解析、并发上限非负、带宽非负。
// 面板保存前先验一次,避免把起不来的配置写进 env —— 那会让服务**下次重启**直接失败,
// 而失败发生在重启之后,管理员已经连不上面板了。
//
// 监听地址能否 bind 不在这里验:那要真去占端口才知道,由 Start 负责。
// 密码允许为空(等价于「不认证」),但公网部署时必须与 Allow 搭配使用。
func (c Config) Validate() error {
	if c.Addr == "" {
		return nil // 不启用:其余字段无意义,不校验
	}
	if _, err := dialout.ParseAllow(c.Allow); err != nil {
		return fmt.Errorf("hy2: 白名单格式错误: %w", err)
	}
	if c.MaxConns < 0 {
		return fmt.Errorf("hy2: 并发上限不能为负")
	}
	if c.UpMbps < 0 || c.DownMbps < 0 {
		return fmt.Errorf("hy2: 带宽不能为负")
	}
	return nil
}

// Manager 管理代理实例的生命周期,支持在进程内启停与换配置重启。
//
// 为什么要它:hy2 是独立 goroutine + 独立 UDP socket,与 Web 服务和抓包互不干扰。
// 故改端口/密码时可以**只重启它** —— 不必重启整个进程,也就不打断正在解密的游戏连接。
type Manager struct {
	mu   sync.Mutex
	cur  *inst
	cert atomic.Pointer[tls.Certificate]
}

// inst 是一个在跑的实例。stopped 在调用 Close() **之前**关闭,
// 好让 Serve 收尾时区分「我们主动关的」与「监听真的出事了」。
type inst struct {
	srv *Server
	// addr 是**请求监听时的地址原文**,不是 srv.Addr().String()。
	// 后者是内核解析后的形态:请求 ":11443" 时它返回 "[::]:11443",两者字符串不等;
	// 拿它和面板提交的地址比会误判成「改了地址」,进而走重新 bind 的老路,
	// 又撞上 address already in use。故比原文。
	addr  string
	up    int
	down  int
	stopC chan struct{}
	done  chan struct{} // Serve 已返回
}

// NewManager 创建管理器。证书随后由 SetCert 提供(hy2 必须跑在 TLS 上)。
func NewManager() *Manager { return &Manager{} }

// SetCert 设置/更新服务端证书。复用 Web 那份自签证书:同一台机器上
// 「证书与监听地址无关」这一点对两者都成立(见 main.go 里准备 tlsCfg 处的注释)。
func (m *Manager) SetCert(c tls.Certificate) { m.cert.Store(&c) }

// Start 应用 cfg。
//
// 两条通路,取决于监听地址有没有变:
//
//  1. **地址没变**:只换运行期参数(密码/白名单/屏蔽/并发上限),不碰监听器。
//     零中断 —— 已建立的连接不受影响,也不会撞上 bind 冲突。改密码走的就是这条。
//  2. **地址变了**:必须先起新的、成功后再停旧的。反过来(先停旧的再起新的)的话,
//     新配置一旦有问题(端口被占、地址写错)就变成「新的起不来、旧的也没了」,
//     管理员只能干瞪眼 —— 而代理常是手机游戏流量的唯一通道,断掉等于抓包全停。
//     先起新的则失败时旧实例照常服务,配置改动只是没生效而已。
//
// ⚠️ **带宽不在运行期可改之列**:它是**建连时**就写进 QUIC 参数的,改它必须重建监听,
// 而同一端口「先起新后停旧」必然撞 address already in use(已实测)。故 UpMbps/DownMbps
// 只作为**启动项**:运行期改了不生效,只打一条日志说明要重启进程。管理面板因此
// 不暴露这两项,避免给出一个「点了却没效果」的开关。
func (m *Manager) Start(cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	allow, err := dialout.ParseAllow(cfg.Allow)
	if err != nil {
		return fmt.Errorf("hy2: 白名单解析失败: %w", err)
	}
	p := Params{
		Allow:    allow,
		Block:    dialout.SplitList(cfg.Block),
		MaxConns: cfg.MaxConns,
		Password: cfg.Password,
	}
	up, down := cfg.UpMbps, cfg.DownMbps
	if up <= 0 {
		up = DefaultUpMbps
	}
	if down <= 0 {
		down = DefaultDownMbps
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if cfg.Addr == "" { // 配成「不启用」:停掉即可
		m.stopLocked()
		return nil
	}
	cert := m.cert.Load()
	if cert == nil {
		return fmt.Errorf("hy2: 未提供 TLS 证书")
	}
	// 通路 1:地址没变且已有实例 → 只换参数(带宽差异只告警,见 Start 注释)
	if m.cur != nil && m.cur.addr == cfg.Addr {
		if m.cur.up != up || m.cur.down != down {
			log.Printf("hy2: 带宽变更(%d/%d → %d/%d Mbps)需重启进程才生效,本次继续沿用 %d/%d",
				m.cur.up, m.cur.down, up, down, m.cur.up, m.cur.down)
			up, down = m.cur.up, m.cur.down
		}
		m.cur.srv.SetParams(p)
		log.Printf("hy2 配置已更新(监听地址不变): %s", cfg.Addr)
		return nil
	}

	// 通路 2:换地址/换带宽(或首次启动)—— 先起新的,成功后才停旧的
	next, err := New(cfg.Addr, cert, p, up, down)
	if err != nil {
		// 直接返回,**旧的还在跑**(见函数注释)
		return fmt.Errorf("hy2: 启动失败,旧实例仍在运行: %w", err)
	}
	it := &inst{srv: next, addr: cfg.Addr, up: up, down: down,
		stopC: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(it.done)
		if err := next.Serve(); err != nil {
			select {
			case <-it.stopC: // 我们主动关的:Accept 报错属正常退出,不必记
			default:
				log.Printf("hy2: 监听异常退出: %v", err)
			}
		}
	}()

	old := m.cur
	m.cur = it
	if old != nil {
		old.shutdown()
	}
	log.Printf("hy2 已监听(UDP): %s", next.Addr())
	return nil
}

// Stop 停掉代理(配置改成不启用时调用)。
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopLocked()
}

// stopLocked 停掉当前实例(调用方须持有 mu)。
func (m *Manager) stopLocked() {
	if m.cur == nil {
		return
	}
	m.cur.shutdown()
	m.cur = nil
	log.Print("hy2 代理已停止")
}

// shutdown 关监听并等 Serve 返回。等是必要的:Close 只是让 Accept 不再阻塞,
// 若立刻启动新实例去 bind 同一端口,可能有极短窗口内旧监听尚未完全释放。
func (it *inst) shutdown() {
	close(it.stopC)
	if err := it.srv.Close(); err != nil {
		log.Printf("hy2: 关闭监听失败: %v", err)
	}
	<-it.done
}

// Running 报告当前是否有实例在跑,及其实际监听地址。
func (m *Manager) Running() (addr string, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cur == nil {
		return "", false
	}
	return m.cur.srv.Addr().String(), true
}

// parsedAddr 返回当前实例的**内核解析后**地址(仅测试用:它与请求原文常常不等,
// 如请求 ":11443" 得到 "[::]:11443",测试据此确认「只能比原文」这条约束)。
func (m *Manager) parsedAddr() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cur == nil {
		return ""
	}
	return m.cur.srv.Addr().String()
}
