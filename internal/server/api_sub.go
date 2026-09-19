package server

import (
	"crypto/subtle"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/zxsos/rocom-go/internal/envfile"
	"github.com/zxsos/rocom-go/internal/hy2"
)

// 配置订阅:GET /sub/{token} 下发**整份**客户端配置(节点 + 分流规则)。
//
// 与 /api/admin/hy2/link 的分工:
//   - link 给一条 node 链接,适合「已经有配置、只想换个节点」的人;
//   - sub  给一份完整配置,适合「只想复制一个 URL 就完事」的人。
//
// 后者不是前者的便利版,而是**必要**的:抓包成立的前提是游戏那条 TCP 流量被送进代理,
// 而那取决于客户端的分流规则 —— 规则不在 node 链接里。只导入 node 的人得到的结果是
// 「手机连上了、游戏也能玩、面板上一条数据都没有」(游戏服是国内 IP,默认分流会放它直连),
// 从表象上几乎无法与「配置完全没生效」区分。故这里把规则一并下发。
//
// 鉴权走路径里的令牌而非管理员会话:订阅是**客户端**定时回来拉的,它没有、
// 也不该有管理员令牌。令牌由当前密码派生(见 hy2.SubToken)——
// 换密码即吊销所有旧订阅地址,这正是想要的语义。
func (s *Server) handleSub(w http.ResponseWriter, r *http.Request) {
	pass := s.hy2Mgr.Password()
	want := hy2.SubToken(pass)
	// 一律 404,不区分「代理没设密码」与「令牌不对」:给探测者一个可区分的信号没有好处。
	// 常数时间比较是为了不让令牌被逐字节试探出来。
	if want == "" || subtle.ConstantTimeCompare([]byte(r.PathValue("token")), []byte(want)) != 1 {
		// 记一笔:排障时「客户端到底拉到没有」是第一个要回答的问题,而这条请求
		// 不进管理员会话、也没有别的痕迹。只记来源与结论,不记令牌本身。
		log.Printf("配置订阅: 令牌不匹配,已拒绝(%s)", r.RemoteAddr)
		http.NotFound(w, r)
		return
	}

	addr, ok := s.hy2Mgr.Running()
	if !ok {
		http.Error(w, "代理未运行:先在面板启用 hy2,才有可下发的配置", http.StatusConflict)
		return
	}
	_, listenPort, err := net.SplitHostPort(addr)
	if err != nil {
		http.Error(w, "解析监听地址失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	port, err := strconv.Atoi(listenPort)
	if err != nil {
		http.Error(w, "监听端口异常: "+addr, http.StatusInternalServerError)
		return
	}

	// 主机:与 link 同一条优先级 —— 「对外地址」> ?host= > 请求的 Host 头。
	// 最后那一项让订阅地址可以**不带任何参数**直接用:客户端去拉的地址本身就是
	// 面板地址,而大多数部署下面板地址就是手机打得通的那个。
	f, _ := envfile.Load(s.envPath)
	adv, err := hy2.ParseAdvertise(advOrEmpty(f))
	if err != nil {
		http.Error(w, "对外地址配置有误: "+err.Error(), http.StatusBadRequest)
		return
	}
	host := adv.Host
	if host == "" {
		host = strings.TrimSpace(r.URL.Query().Get("host"))
	}
	if host == "" {
		host = hostOnly(r.Host)
	}
	if host == "" {
		http.Error(w, "无法确定主机:请在「对外地址」填 host,或给订阅地址加 ?host=你的域名", http.StatusBadRequest)
		return
	}
	if adv.Port > 0 {
		port = adv.Port
	}

	// 密码取**内存里正在跑的那个**:面板上手动改过 env 但没重启时,env 是新值、
	// 而手机此刻连得上的是旧值 —— 订阅必须与「真在运行的进程」一致。
	params := hy2.SubParams{
		Host:     host,
		Port:     port,
		Password: pass,
		Secure:   s.hy2Mgr.SecureFor(host),
		GamePort: s.gamePort,
	}

	// 按 User-Agent 选格式:三种客户端的配置格式互不通用(Clash 系 YAML、
	// 小火箭 .conf、sing-box 内核用原生 JSON),而订阅地址是同一个 ——
	// 让服务端按来访者挑,用户就只需记一条 URL。
	//
	// 单开 sing-box 那一路不是为了好看:这类客户端不采用 Clash 配置里的 rules,
	// 用 YAML 喂它等于把分流规则丢掉,抓包一定不成立(见 hy2.SubscriptionSingbox)。
	var body, ctype, format string
	switch ua := r.UserAgent(); {
	case isShadowrocket(ua):
		body, err = hy2.SubscriptionShadowrocket(params)
		ctype, format = "text/plain; charset=utf-8", "shadowrocket"
	case isSingbox(ua):
		body, err = hy2.SubscriptionSingbox(params)
		ctype, format = "application/json; charset=utf-8", "sing-box"
	default:
		body, err = hy2.SubscriptionClash(params)
		ctype, format = "text/yaml; charset=utf-8", "clash"
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", ctype)
	// 正文含明文密码:不让代理/CDN 留副本。
	w.Header().Set("Cache-Control", "no-store")
	// Clash 系客户端按这个头决定多久回来刷新一次(单位小时);数值大小无所谓,
	// 目的是让「面板改了端口/密码」能自动传导到客户端,不必让人重新导入。
	w.Header().Set("Profile-Update-Interval", "24")
	// 下发成功也记一笔:「客户端拉到了但规则没生效」与「客户端根本没来拉」是两种
	// 完全不同的问题,而它们在下游的表现一模一样(面板就是没有数据)。
	log.Printf("配置订阅已下发: %s:%d 格式=%s 来源=%s UA=%q", host, port, format, r.RemoteAddr, r.UserAgent())
	_, _ = io.WriteString(w, body)
}

// isShadowrocket 判断来访者是不是小火箭。它拉订阅时会带自己的 UA;
// 认不出来的一律给 Clash YAML —— 那是覆盖面最广的一种(Clash Meta / FlClash / Mihomo / Stash)。
func isShadowrocket(ua string) bool {
	return strings.Contains(strings.ToLower(ua), "shadowrocket")
}

// isSingbox 判断来访者是不是 sing-box 内核的客户端(Hiddify 是主要的那个)。
//
// Hiddify 的 UA 形如 "HiddifyNext/4.1.1 (android) like ClashMeta v2ray sing-box" ——
// 它自称 like ClashMeta,但那是「能读 Clash 的节点列表」,分流仍由它自己的路由表说了算。
// 故这里的判断必须**排在 Clash 兜底之前**:否则它会拿到一份规则根本不生效的 YAML,
// 而症状(游戏能玩、面板没数据)与没配置一模一样。
func isSingbox(ua string) bool {
	ua = strings.ToLower(ua)
	return strings.Contains(ua, "hiddify") || strings.Contains(ua, "sing-box") || strings.Contains(ua, "singbox")
}

// hostOnly 从 Host 头里剥掉端口,得到能写进配置的主机名。
// 纯 IPv6 字面量在 [ ] 里,也在这里去掉方括号(配置里两种客户端都写裸地址)。
func hostOnly(h string) string {
	h = strings.TrimSpace(h)
	if host, _, err := net.SplitHostPort(h); err == nil {
		return strings.Trim(host, "[]")
	}
	return strings.Trim(h, "[]")
}
