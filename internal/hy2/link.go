package hy2

import (
	"crypto/x509"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// 导入链接(hysteria2://…)的生成。
//
// 为什么要服务端生成而不是让人照文档手拼:链接里同时有**密码、端口、要不要跳过证书
// 校验**三样容易填错的东西,而这三样恰好分别散落在配置文件、运行中的监听地址、证书
// SAN 里。手机客户端导入失败时,人很难看出是端口填错还是 insecure 没加 —— 让持有全部
// 信息的服务端一次算对,比让人对着模板猜省事得多。

// Advertise 是「对外可达地址」:手机真正打的 host:port。
//
// 不能拿监听地址直接充当它,有两个原因:
//  1. 监听地址通常是 ":11443" 这种「本机所有网卡」形态,根本没有主机;
//  2. 端口可能被 NAT / 云映射改过 —— 本机 Web 就是内网 57124、对外 39443,
//     若代理也如此,链接里写监听端口就连不通。
//
// 故每一部分都可单独省略,省略就回落到更「活」的来源:
//
//	host            只固定主机,端口跟实际监听走
//	host:port       两者都固定
//	:port           只固定端口,主机由请求方(浏览器地址栏)给
//	空              全不固定
type Advertise struct {
	Host string
	Port int // 0 = 未指定
}

// ParseAdvertise 解析 Advertise 的三种形态。空串是合法输入(全不固定)。
func ParseAdvertise(s string) (Advertise, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Advertise{}, nil
	}
	// ":11443" 是「只固定端口」。注意不能见冒号就切:裸 IPv6 "::1" 也以冒号开头,
	// 它剩下的部分里还有冒号 —— 那种是主机,不是端口。
	if strings.HasPrefix(s, ":") && !strings.Contains(s[1:], ":") {
		return advertisePortOnly(s[1:])
	}
	if host, port, err := net.SplitHostPort(s); err == nil {
		a, perr := advertisePortOnly(port)
		if perr != nil {
			return Advertise{}, perr
		}
		a.Host = host
		return a, nil
	}
	// 无端口:整体当主机。非法字符留给 Link 校验,这里不重复一套。
	return Advertise{Host: s}, nil
}

func advertisePortOnly(p string) (Advertise, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return Advertise{}, nil
	}
	n, err := strconv.Atoi(p)
	if err != nil || n < 1 || n > 65535 {
		return Advertise{}, fmt.Errorf("hy2: 对外端口非法: %q(应为 1-65535)", p)
	}
	return Advertise{Port: n}, nil
}

// LinkParams 是生成链接所需的全部信息。
type LinkParams struct {
	Host     string // 必填:域名、IPv4 或 IPv6 字面量
	Port     int    // 必填:1-65535
	Password string // 空 = 服务端未设密码(链接不带认证段)
	Label    string // 客户端里显示的名字;空 = "rocom"
	Secure   bool   // true = 主机能在证书里验签,故不加 insecure=1
}

// Link 拼出可直接导入的 hysteria2 链接。
//
// 形态:`hysteria2://密码@主机:端口/?insecure=1#名字` —— 认证串占 username 位,
// 详见下面 u.User 那处注释。百分号编码由 url.URL 负责:密码里有 `@` 或 `/` 时不编码
// 会把主机名截断,那又是另一种「连到半个域名上」的诡异现象。
func Link(p LinkParams) (string, error) {
	host := strings.TrimSpace(p.Host)
	if !validLinkHost(host) {
		return "", fmt.Errorf("hy2: 主机非法: %q(不能含空格与 @ / ? # 等分隔符)", p.Host)
	}
	if p.Port < 1 || p.Port > 65535 {
		return "", fmt.Errorf("hy2: 端口非法: %d", p.Port)
	}
	u := &url.URL{
		Scheme: "hysteria2",
		// Path 显式给 "/":查询参数要挂在 "/?" 后面,与官方文档里的示例形态一致。
		// 留空时 url.String() 会输出 "host:port?insecure=1",个别客户端按第一个
		// '?' 之前切 host,少这一斜杠就多了点被误解析的可能。
		Path: "/",
		Host: net.JoinHostPort(host, strconv.Itoa(p.Port)),
	}
	if p.Password != "" {
		// 认证串在 userinfo 的 **username** 位:官方 URI 规范是
		// `hysteria2://[auth@]hostname[:port]/`,auth 即 username,只有服务器的 userpass
		// 模式才写成 `user:pass`。写成 `:pass@` 时客户端按规范取 username 拿到空串,
		// 表现是「导入成功、连不上」,服务端只报认证失败 —— 链接文本上看不出差别。
		u.User = url.User(p.Password) // 百分号编码由 url.URL 负责,含 @ / # 时才不会截断主机名
	}
	if !p.Secure {
		u.RawQuery = "insecure=1"
	}
	return u.String() + "#" + linkLabel(p.Label), nil
}

// validLinkHost 拒绝会破坏链接结构的字符。不校验「是不是真的能解析」—— 那是客户端的事,
// 服务端拦不住内网域名,也不该拦。
func validLinkHost(h string) bool {
	if h == "" {
		return false
	}
	return !strings.ContainsAny(h, " \t@/?#%[]")
}

// linkLabel 把显示名压成 URI 安全的片段。
//
// 名字是给人看的,客户端会百分号解码回来,故中文可以留;但空格与 `#` 会把后面的内容
// 截断或另起片段,统一换成 `-`。
func linkLabel(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		s = "rocom"
	}
	r := strings.NewReplacer(" ", "-", "\t", "-", "#", "-", "?", "-", "&", "-", "=", "-", ":", "-")
	return r.Replace(s)
}

// SecureFor 报告「以 host 连接时客户端能否验签通过」,决定链接要不要带 insecure=1。
//
// 带 insecure=1 不是无害的:它让客户端不再校验证书,中间人就能接管整条隧道。而经
// Let's Encrypt 域名访问时完全可以验 —— 证书 SAN 里有那个域名。故按 SAN 逐次判断,
// 而不是无脑加上。
func (m *Manager) SecureFor(host string) bool {
	c := m.cert.Load()
	if c == nil || len(c.Certificate) == 0 {
		return false
	}
	leaf := c.Leaf
	if leaf == nil {
		var err error
		if leaf, err = x509.ParseCertificate(c.Certificate[0]); err != nil {
			return false
		}
	}
	return leaf.VerifyHostname(host) == nil
}
