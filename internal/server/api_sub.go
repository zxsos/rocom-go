package server

import (
	"crypto/subtle"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/zxsos/rocom-go/internal/envfile"
	"github.com/zxsos/rocom-go/internal/hy2"
)

// 配置订阅:下发**整份**客户端配置(节点 + 分流规则),而不是一条 node 链接。
//
// 与 /api/admin/hy2/link 的分工:
//   - link 给一条 node 链接,适合「已经有配置、只想换个节点」的人;
//   - sub  给一份完整配置,适合「只想打开一个链接就完事」的人。
//
// 后者不是前者的便利版,而是**必要**的:抓包成立的前提是游戏那条 TCP 流量被送进代理,
// 而那取决于客户端的分流规则 —— 规则不在 node 链接里。只导入 node 的人得到的结果是
// 「手机连上了、游戏也能玩、面板上一条数据都没有」(游戏服是国内 IP,默认分流会放它直连),
// 从表象上几乎无法与「配置完全没生效」区分。故这里把规则一并下发。
//
// 鉴权走路径里的令牌而非管理员会话:订阅是**客户端**定时回来拉的,它没有、
// 也不该有管理员令牌。令牌有两种:
//   - 设备令牌(sub_tokens,见 store/sub_token.go):一台设备一枚,可单独吊销;
//   - 旧的地址令牌(由 hy2 密码派生,见 hy2.SubToken):给早期已导入的地址留的退路。

// subFormat 一种客户端配置格式。
//
// 格式只决定**容器语法**,不决定规则语义:两份配置的规则段必须同源同义,否则又要
// 栽进「连上了但抓不到」那个坑里(见 docs/deploy.md)。
type subFormat struct {
	ID     string                              // 日志与面板里显示的名字
	File   string                              // 下载文件名(进 Content-Disposition)
	CType  string                              // 响应 Content-Type
	Render func(p hy2.SubParams) (string, error) // 渲染函数
}

var (
	subFormatClash = subFormat{
		ID: "clash", File: "rocom-clash.yaml", CType: "text/yaml; charset=utf-8",
		Render: hy2.SubscriptionClash,
	}
	subFormatShadowrocket = subFormat{
		ID: "shadowrocket", File: "rocom.conf", CType: "text/plain; charset=utf-8",
		Render: hy2.SubscriptionShadowrocket,
	}
)

// canonicalFormat 把人写的格式名归一成一种格式。
//
// 别名是给「一键链接」用的:引导页上按钮写的是客户端名(小火箭 / Clash Meta / Hiddify),
// 而它们要的都是同一份 Clash YAML。让人填客户端名而不是格式名,少一次心智转换。
func canonicalFormat(v string) (subFormat, bool) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "clash", "meta", "mihomo", "verge", "clash-verge", "flclash", "stash", "hiddify", "singbox", "sing-box":
		// sing-box 也先给 Clash YAML:本轮没做 sing-box 原生 JSON(见 handleSub 里的
		// 说明),而 Hiddify 明确支持直接导入 clash 订阅链接,这条路是通的。
		return subFormatClash, true
	case "sr", "rocket", "shadowrocket":
		return subFormatShadowrocket, true
	}
	return subFormatClash, false
}

// resolveFormat 定下这次发哪种格式。优先级自左向右,命中即停:
// ?format= > 路径后缀 > User-Agent > Accept > 默认 Clash YAML。
//
// 显式优先是刻意的:UA 这东西既不可靠(Clash 系的 UA 常常是 okhttp / Dart / Go-http-client
// 之类没信息量的串)又会被改,而「手动指定」必须能压过一切猜测 —— 否则排障时没法
// 复现「某某客户端到底拿到了什么」。路径后缀是给二维码用的:码里写死后缀,UA 就骗不了人。
func resolveFormat(q url.Values, pathFormat, ua, accept string) subFormat {
	if f, ok := canonicalFormat(q.Get("format")); ok {
		return f
	}
	if f, ok := canonicalFormat(pathFormat); ok {
		return f
	}
	if f, ok := formatFromUA(ua); ok {
		return f
	}
	// Accept 协商:真发 application/yaml 的客户端不多,但发了就得认 —— 这是唯一一个
	// 由客户端主动声明、且不会骗人的信号。
	if a := strings.ToLower(accept); strings.Contains(a, "yaml") {
		return subFormatClash
	}
	return subFormatClash
}

// formatFromUA 按 User-Agent 猜客户端。认不出来就返回 false,交给后面的规则兜底。
//
// 只认得出来的才认:宁可回落覆盖面最广的 Clash YAML,也不要靠半截关键字瞎猜 ——
// 猜错的代价是对方拿到一份完全用不了的配置,而猜不出的代价只是拿了 Clash YAML。
func formatFromUA(ua string) (subFormat, bool) {
	l := strings.ToLower(ua)
	if l == "" {
		return subFormatClash, false
	}
	if isShadowrocket(ua) {
		return subFormatShadowrocket, true
	}
	for _, k := range []string{"clash", "mihomo", "flclash", "stash", "verge", "hiddify"} {
		if strings.Contains(l, k) {
			return subFormatClash, true
		}
	}
	return subFormatClash, false
}

// handleSub 旧形态的订阅地址:GET /sub/{token}(也吃 /sub/{token}/{format})。
func (s *Server) handleSub(w http.ResponseWriter, r *http.Request) {
	s.serveSub(w, r, r.PathValue("token"), r.PathValue("format"))
}

// handleSubCode 短链:GET /i/{code}。
//
// 人来和客户端来给两种东西:
//   - 浏览器(或空 UA,比如 curl)→ 302 到前端引导页,让人看见二维码和一键按钮;
//   - 代理客户端 → 直接下发配置,不绕那一跳。
//
// 不统一成 302 是因为客户端对「302 之后要不要改写自己保存的 URL」行为不一,
// 而多一跳就多一处说不清的故障。短码本身的统计照记(见 store.NoteSubFetch)。
func (s *Server) handleSubCode(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	token, ok := s.store.ResolveSubCode(code)
	if !ok {
		log.Printf("订阅短链: 短码无效(%s) 来源=%s", code, r.RemoteAddr)
		http.NotFound(w, r)
		return
	}
	if isBrowser(r.UserAgent()) {
		// 前端是 HashRouter,SPA 路由写在 # 后面。
		http.Redirect(w, r, "/#/i/"+url.PathEscape(code), http.StatusFound)
		return
	}
	s.serveSub(w, r, token, "")
}

// serveSub 查令牌 → 算参数 → 选格式 → 下发。两条入口共用这一段。
func (s *Server) serveSub(w http.ResponseWriter, r *http.Request, token, pathFormat string) {
	dev, isDevice := s.store.GetSubDevice(token)
	if isDevice && !dev.Usable() {
		// 403 而不是 404:小火箭把 403 直接显示成「订阅被重置或令牌错误」,
		// 正是想让被吊销的人看见的话。正文是给人看的 —— 他多半会在浏览器里打开这条链接。
		log.Printf("配置订阅: 令牌已吊销或过期,已拒绝(来源=%s)", r.RemoteAddr)
		http.Error(w, "这条订阅已被吊销或过期:找发链接的人要一条新的", http.StatusForbidden)
		return
	}
	pass := s.hy2Mgr.Password()
	if !isDevice {
		// 旧的地址令牌:由当前密码派生,换密码即全部失效(那是当年刻意要的语义)。
		// 常数时间比较是不让令牌被逐字节试探出来。
		want := hy2.SubToken(pass)
		if want == "" || subtle.ConstantTimeCompare([]byte(token), []byte(want)) != 1 {
			// 一律 404,不区分「代理没设密码」与「令牌不对」:给探测者一个可区分的信号没有好处。
			// 记一笔:排障时「客户端到底拉到没有」是第一个要回答的问题,而这条请求
			// 不进管理员会话、也没有别的痕迹。只记来源与结论,不记令牌本身。
			log.Printf("配置订阅: 令牌不匹配,已拒绝(%s)", r.RemoteAddr)
			http.NotFound(w, r)
			return
		}
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

	format := resolveFormat(r.URL.Query(), pathFormat, r.UserAgent(), r.Header.Get("Accept"))
	body, err := format.Render(params)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", format.CType)
	// 正文含明文密码:不让代理/CDN 留副本。
	w.Header().Set("Cache-Control", "no-store")
	// Clash 系客户端按这个头决定多久回来刷新一次(单位小时);数值大小无所谓,
	// 目的是让「面板改了端口/密码」能自动传导到客户端,不必让人重新导入。
	w.Header().Set("Profile-Update-Interval", "24")
	// 文件名与标题:各家客户端在订阅卡片上显示的名字取自这里,不设的话会显示一截 URL,
	// 朋友在自己的客户端里认不出哪条是你给的。
	w.Header().Set("Content-Disposition", `attachment; filename="`+format.File+`"`)
	w.Header().Set("Profile-Title", "ROCOM")
	if u := landingURL(r, dev.Code); u != "" {
		// 客户端订阅卡片上会多出一个「打开网页」入口 —— 朋友下次想看说明或换设备时
		// 不必再翻聊天记录,这是把门槛降下来的那一步。
		w.Header().Set("Profile-Web-Page-Url", u)
		w.Header().Set("Support-Url", u)
	}
	// 到期信息只在真设了有效期时给:全 0 的流量统计显示出来只会让人以为配额用完了。
	// Clash Verge Rev 的文档写明「一般要求 UA 含 clash 才回这个头」,故按它来。
	if isDevice && dev.ExpiresAt > 0 && strings.Contains(strings.ToLower(r.UserAgent()), "clash") {
		w.Header().Set("Subscription-Userinfo",
			"upload=0; download=0; total=0; expire="+strconv.FormatInt(dev.ExpiresAt, 10))
	}

	// 下发成功也记一笔:「客户端拉到了但规则没生效」与「客户端根本没来拉」是两种
	// 完全不同的问题,而它们在下游的表现一模一样(面板就是没有数据)。
	log.Printf("配置订阅已下发: %s:%d 格式=%s 来源=%s UA=%q", host, port, format.ID, r.RemoteAddr, r.UserAgent())
	if isDevice {
		// 记不进去不该让订阅失败(那会变成一个「偶发拉不到」的玄学故障),故不从严。
		if err := s.store.NoteSubFetch(token, r.UserAgent(), format.ID); err != nil {
			log.Printf("配置订阅: 记录命中失败: %v", err)
		}
	}
	_, _ = io.WriteString(w, body)
}

// subIntroJSON 是引导页 GET /api/sub/intro 的响应:只给「这条链接现在是什么状态」。
//
// 刻意不含任何密钥:引导页会在朋友的手机上打开,而那台设备并不该因此得到密码。
type subIntroJSON struct {
	OK       bool   `json:"ok"`
	Label    string `json:"label"`    // 备注名,让朋友知道这是谁给的
	Revoked  bool   `json:"revoked"`
	Expired  bool   `json:"expired"`
	GamePort int    `json:"gamePort"` // 抓包认的端口,引导页要拿它写进说明
}

// handleSubIntro 引导页的状态查询。短码本身就是秘密(它等于订阅令牌),故这里不再验会话。
func (s *Server) handleSubIntro(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	token, ok := s.store.ResolveSubCode(code)
	if !ok {
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, subIntroJSON{OK: false})
		return
	}
	dev, ok := s.store.GetSubDevice(token)
	if !ok {
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, subIntroJSON{OK: false})
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, subIntroJSON{
		OK:       true,
		Label:    dev.Label,
		Revoked:  dev.Revoked(),
		Expired:  dev.Expired(),
		GamePort: s.gamePort,
	})
}

// isShadowrocket 判断来访者是不是小火箭。它拉订阅时会带自己的 UA。
func isShadowrocket(ua string) bool {
	return strings.Contains(strings.ToLower(ua), "shadowrocket")
}

// isBrowser 判断这次来访是不是「人在浏览器里打开」。
//
// 代理客户端的 UA 里没有 Mozilla(Clash 系是 Clash/…、小火箭是 Shadowrocket/…),
// 而所有浏览器都带 —— 用这一条把「人」和「客户端」分开,短链才能既给人看页、
// 又给客户端发配置。空 UA 按人处理:curl / 微信内置打开都该看到说明页。
func isBrowser(ua string) bool {
	l := strings.ToLower(strings.TrimSpace(ua))
	if l == "" {
		return true
	}
	return strings.Contains(l, "mozilla")
}

// landingURL 拼出引导页地址;没有短码(旧的地址令牌)时返回空串。
func landingURL(r *http.Request, code string) string {
	if code == "" || r.Host == "" {
		return ""
	}
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s/i/%s", scheme, r.Host, url.PathEscape(code))
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
