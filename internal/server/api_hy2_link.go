package server

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/zxsos/rocom-go/internal/envfile"
	"github.com/zxsos/rocom-go/internal/hy2"
)

// hy2LinkJSON 是 GET /api/admin/hy2/link 的响应。
//
// 带 link 就等于带明文密码(链接本身含它),所以这条路由必须挂管理员鉴权,且回
// no-store —— 别让代理或浏览器缓存把密码留在中间设备上。
type hy2LinkJSON struct {
	Link   string `json:"link"`   // 可直接导入客户端的 hysteria2:// 链接
	Sub    string `json:"sub"`    // 整份配置的订阅地址(节点 + 8195 规则);密码为空时为空串
	Host   string `json:"host"`   // 实际用的主机
	Port   int    `json:"port"`   // 实际用的端口
	Secure bool   `json:"secure"` // true = 链接不带 insecure(客户端可验签)
}

// handleHy2Link 生成手机可直接导入的代理链接。
//
// ?host=<可选>:浏览器把自己地址栏的主机传来当默认值。优先级是
// 「配置里的对外地址主机 > 请求带来的主机」—— 配过的人要的正是固定值(比如 NAT 后
// 的公网域名),没配过的人才需要「从哪打开就用哪」这个便利。
//
// 端口取**运行中实例**的实际监听地址,不取 env:面板改端口是热重启,env 与内存同步
// 落盘,但改失败时(端口被占)内存里仍是旧值 —— 而旧值才是手机此刻真连得上的那个。
func (s *Server) handleHy2Link(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	addr, ok := s.hy2Mgr.Running()
	if !ok {
		http.Error(w, "代理未运行:先在面板填好端口启用,才有可导入的链接", http.StatusConflict)
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

	// 配置文件可能压根不存在(手动跑二进制、离线回放)—— 那不该拦住生成链接:
	// 密码走内存里那份,对外地址当作未设置。
	f, _ := envfile.Load(s.envPath)
	// 坏值要报出来:链接生成不出来是小事,「配置里存着解析不了的东西」才是根因。
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
		http.Error(w, "无法确定主机:请在「对外地址」填 host,或用一个手机也能访问的地址打开本面板", http.StatusBadRequest)
		return
	}
	if adv.Port > 0 {
		port = adv.Port
	}
	// 密码:内存(当前真在跑的)优先,env 兜底 —— 与 configGet 同一套取值顺序。
	// 链接要和「手机此刻连得上的那个进程」一致,而 env 可能是手改过还没重启的值。
	pass := s.hy2Mgr.Password()
	if pass == "" && f != nil {
		pass, _ = f.Get(envHy2Pass)
	}
	// 主机能在证书 SAN 里验上就不带 insecure —— 带它等于让客户端放弃校验,中间人可接管隧道。
	secure := s.hy2Mgr.SecureFor(host)
	link, err := hy2.Link(hy2.LinkParams{
		Host:     host,
		Port:     port,
		Password: pass,
		Secure:   secure,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, hy2LinkJSON{Link: link, Sub: subURL(r, pass, host), Host: host, Port: port, Secure: secure})
}

// subURL 拼出整份配置的订阅地址(见 api_sub.go)。返回空串表示当前没有可用的订阅
// (密码为空 → 没有令牌),前端据此不显示那一行。
//
// 用意:让「一次导入就配好」成为可能 —— 只给 node 链接的话,导入方得到一个没有分流
// 规则的客户端,而抓包恰恰依赖那条规则。
//
// 地址里带的 host 参数是**给客户端的节点地址**用的,不是订阅地址自己的主机:
// 订阅地址取 r.Host(管理员此刻打开面板用的那个地址,换成手机打开也大多能通),
// 而节点主机沿用同一条优先级(对外地址 > 浏览器地址栏),故把算好的 host 透传过去,
// 免得客户端自己去猜 —— 它猜不到「对外地址」这一项。
func subURL(r *http.Request, pass, host string) string {
	token := hy2.SubToken(pass)
	if token == "" || r.Host == "" {
		return ""
	}
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s/sub/%s?host=%s", scheme, r.Host, token, url.QueryEscape(host))
}

// advOrEmpty 读「对外地址」;配置文件里没有这项时视作未设置。
func advOrEmpty(f *envfile.File) string {
	if f == nil {
		return ""
	}
	v, _ := f.Get(envHy2Advertise)
	return v
}
