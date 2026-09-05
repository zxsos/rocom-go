package socks5

import (
	"fmt"
	"log"
	"strings"
	"sync"
)

// Config 是一份完整的代理配置(管理面板编辑的就是这份)。
// 字段与启动 flag 一一对应,空 Addr 表示「不启用代理」。
type Config struct {
	Addr     string // 监听地址,如 :1080;空=不启用
	Allow    string // 客户端 IP/CIDR 白名单(逗号分隔);空=不限制
	Block    string // 屏蔽的目标域名(逗号分隔);空=不屏蔽
	MaxConns int    // 并发上限;0=不限制
	User     string // 认证用户名;空=无认证
	Pass     string // 认证密码
}

// Validate 校验配置是否可用:白名单可解析、用户名与密码成对、并发上限非负。
// 面板保存前先验一次,避免把起不来的配置写进 env —— 那会让服务**下次重启**直接失败,
// 而失败发生在重启之后,管理员已经连不上面板了。
//
// 监听地址能否 bind 不在这里验:那要真去占端口才知道,由 Start 负责。
func (c Config) Validate() error {
	if c.Addr == "" {
		return nil // 不启用:其余字段无意义,不校验
	}
	if strings.TrimSpace(c.Addr) == "" {
		return fmt.Errorf("socks5: 监听地址为空")
	}
	if _, err := ParseAllow(c.Allow); err != nil {
		return fmt.Errorf("socks5: 白名单格式错误: %w", err)
	}
	if c.User != "" && c.Pass == "" {
		return fmt.Errorf("socks5: 已设置用户名但密码为空")
	}
	if c.MaxConns < 0 {
		return fmt.Errorf("socks5: 并发上限不能为负")
	}
	return nil
}

// Manager 管理代理实例的生命周期,支持在进程内启停与换配置重启。
//
// 为什么要它:socks5 是独立 goroutine + 独立 listener,与 Web 服务和抓包互不干扰。
// 故改端口/密码时可以**只重启它** —— 不必重启整个进程,也就不打断正在解密的游戏连接
// (那条顾虑见 store/merchant_src.go 文件头的说明)。
type Manager struct {
	mu  sync.Mutex
	cur *inst
}

// inst 是一个在跑的实例。stopped 在调用 Close() **之前**关闭,
// 好让 Serve 收尾时区分「我们主动关的」与「监听真的出事了」。
type inst struct {
	srv *Server
	// addr 是**请求监听时的地址原文**,不是 srv.Addr().String()。
	// 后者是内核解析后的形态:请求 ":1080" 时它返回 "[::]:1080",两者字符串不等;
	// 拿它和面板提交的地址比会误判成「改了地址」,进而走重新 bind 的老路,
	// 又撞上 address already in use。故比原文。
	addr    string
	stopped chan struct{}
	done    chan struct{} // Serve 已返回
}

// NewManager 创建管理器。
func NewManager() *Manager { return &Manager{} }

// Start 应用 cfg。
//
// 两条通路,取决于监听地址有没有变:
//
//  1. **地址没变**:只换运行期参数(认证/白名单/屏蔽/并发上限),不碰监听器。
//     零中断 —— 已建立的连接不受影响,也不会撞上 bind 冲突。改密码走的就是这条。
//  2. **地址变了**:必须先起新的、成功后再停旧的。反过来(先停旧的再起新的)的话,
//     新配置一旦有问题(端口被占、地址写错)就变成「新的起不来、旧的也没了」,
//     管理员只能干瞪眼 —— 而代理常是手机游戏流量的唯一通道,断掉等于抓包全停。
//     先起新的则失败时旧实例照常服务,配置改动只是没生效而已。
func (m *Manager) Start(cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	allow, err := ParseAllow(cfg.Allow)
	if err != nil {
		return fmt.Errorf("socks5: 白名单解析失败: %w", err)
	}
	p := Params{Allow: allow, Block: splitList(cfg.Block), MaxConns: cfg.MaxConns, User: cfg.User, Pass: cfg.Pass}

	m.mu.Lock()
	defer m.mu.Unlock()

	if cfg.Addr == "" { // 配成「不启用」:停掉即可
		m.stopLocked()
		return nil
	}
	// 通路 1:地址没变且已有实例 → 只换参数
	if m.cur != nil && m.cur.addr == cfg.Addr {
		m.cur.srv.SetParams(p)
		log.Printf("socks5 配置已更新(监听地址不变): %s", cfg.Addr)
		return nil
	}

	// 通路 2:换地址(或首次启动)—— 先起新的,成功后才停旧的
	next, err := New(cfg.Addr, p)
	if err != nil {
		// 直接返回,**旧的还在跑**(见函数注释)
		return fmt.Errorf("socks5: 启动失败,旧实例仍在运行: %w", err)
	}
	it := &inst{srv: next, addr: cfg.Addr, stopped: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(it.done)
		if err := next.Serve(); err != nil {
			select {
			case <-it.stopped: // 我们主动关的:Accept 报错属正常退出,不必记
			default:
				log.Printf("socks5: 监听异常退出: %v", err)
			}
		}
	}()

	old := m.cur
	m.cur = it
	if old != nil {
		old.shutdown()
	}
	log.Printf("socks5 已监听: %s", next.Addr())
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
	log.Print("socks5 代理已停止")
}

// shutdown 关监听并等 Serve 返回。等是必要的:Close 只是让 Accept 不再阻塞,
// 若立刻启动新实例去 bind 同一端口,可能有极短窗口内旧监听尚未完全释放。
func (it *inst) shutdown() {
	close(it.stopped)
	if err := it.srv.Close(); err != nil {
		log.Printf("socks5: 关闭监听失败: %v", err)
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
// 如请求 ":1080" 得到 "[::]:1080",测试据此确认「只能比原文」这条约束)。
func (m *Manager) parsedAddr() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cur == nil {
		return ""
	}
	return m.cur.srv.Addr().String()
}

// splitList 拆分逗号分隔的列表,去空与首尾空白。
func splitList(s string) []string {
	var out []string
	for part := range strings.SplitSeq(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
