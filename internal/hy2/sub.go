package hy2

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"text/template"
)

// 配置订阅(整份客户端配置,而不是一条节点链接)的生成。
//
// 为什么必须有它:node 链接只解决「连得上」,解决不了「抓得到」。
// 抓包成立的前提是**游戏那一条 TCP 流量被送进代理**,而那取决于客户端的分流规则
// (deploy/rocom-clash.yaml 里那条 DST-PORT 规则),不在节点里。只导入节点的人
// 会得到一个「手机连上了、游戏也能玩、可面板上一条数据都没有」的结果 —— 因为
// 游戏服务器是国内的,默认分流(GEOIP,CN,DIRECT / MATCH,DIRECT)会把它放回直连,
// 流量根本不经过这台机器。这个故障的表象与「配置完全没生效」几乎一样,极难自查。
//
// 所以订阅把节点**和规则**一起下发:一次导入,能连也能抓。

// DefaultGamePort 是规则里默认要送进代理的游戏端口(与 -port 的默认值一致)。
const DefaultGamePort = 8195

// SubToken 由当前 hy2 密码派生出订阅令牌。
//
// 为什么不直接用密码当令牌:订阅 URL 会被客户端长期保存,还可能出现在客户端的
// 日志/分享截图里。用哈希则「拿到 URL 反推不出密码」,而泄露本身也不构成新的暴露面 ——
// 能拿到这个令牌的人本来就被假定已经能从面板看到配置。
//
// 为什么由密码派生而不是另存一个随机串:密码一换,旧订阅 URL 立即失效。
// 这正是换密码时想要的语义(否则吊销一台设备还要去清理别处的残留)。
// 密码为空(不认证)时返回空串,调用方据此拒绝提供订阅。
func SubToken(password string) string {
	if password == "" {
		return ""
	}
	sum := sha256.Sum256([]byte("rocom-go/sub/v1|" + password))
	return hex.EncodeToString(sum[:16])
}

// SubParams 是渲染一份完整配置所需的全部信息。
//
// 与 LinkParams 的区别只在多一个 GamePort:节点链接描述「怎么连」,
// 订阅还要描述「哪些流量该走它」。
type SubParams struct {
	Host     string // 必填:手机要打的域名 / IPv4 / IPv6 字面量
	Port     int    // 必填:hy2 对外端口
	Password string // 必填:客户端没有密码就没有可用的节点
	Secure   bool   // true = 主机能在证书 SAN 里验签,故不跳过校验
	GamePort int    // 要送进代理的游戏端口;<=0 取 DefaultGamePort
}

// normalize 校验并补全参数,返回渲染时可直接用的值。
func (p SubParams) normalize() (SubParams, error) {
	p.Host = strings.TrimSpace(p.Host)
	if !validLinkHost(p.Host) {
		return p, fmt.Errorf("hy2: 主机非法: %q(不能含空格与 @ / ? # 等分隔符)", p.Host)
	}
	if p.Port < 1 || p.Port > 65535 {
		return p, fmt.Errorf("hy2: 端口非法: %d", p.Port)
	}
	if p.Password == "" {
		return p, fmt.Errorf("hy2: 未设密码,无法生成配置(先在上方填一个密码并保存)")
	}
	if p.GamePort < 1 || p.GamePort > 65535 {
		p.GamePort = DefaultGamePort
	}
	return p, nil
}

// clashView / srView 是模板的取值视图。
//
// 单独一层是为了把「已经转义好的字面量」交给模板:模板里再拼引号容易漏掉某个字段,
// 而漏掉的后果是 YAML 被注入(密码里一个引号就能把后面的结构改掉),
// 比少一个字段严重得多。
type clashView struct {
	Host      string // YAML 双引号标量
	Port      int
	Password  string // YAML 双引号标量
	SkipCert  bool
	GamePort  int
	ProxyName string
}

// SubscriptionClash 渲染一份 Clash Meta / FlClash / Mihomo 可直接导入的完整配置。
//
// 规则刻意只放行游戏端口:这台机器是抓包机,不是日常出口。
// 把 MATCH 指向代理会让手机的所有流量绕一圈(还可能因 VPS 出境受限而变慢),
// 而抓包所需的一条 8195 已经足够 —— 少揽事就少出故障。
func SubscriptionClash(p SubParams) (string, error) {
	p, err := p.normalize()
	if err != nil {
		return "", err
	}
	v := clashView{
		Host:      strconv.Quote(p.Host),
		Port:      p.Port,
		Password:  strconv.Quote(p.Password),
		SkipCert:  !p.Secure,
		GamePort:  p.GamePort,
		ProxyName: "ROCOM hy2",
	}
	var b strings.Builder
	if err := clashTmpl.Execute(&b, v); err != nil {
		return "", fmt.Errorf("hy2: 渲染 Clash 配置失败: %w", err)
	}
	return b.String(), nil
}

// srView 是 Shadowrocket .conf 的取值视图。
//
// .conf 的 [Proxy] 行是**逗号分隔的裸值**,没有引号可用,故这里的转义只能靠
// 「先拒绝再拼」:含逗号/换行的值拼进去会多切出一个字段,而客户端只会报
// 「配置格式错误」,不会指出是哪个字段 —— 故在 validate 里直接拦掉。
type srView struct {
	Host     string
	Port     int
	Password string
	SkipCert bool
	GamePort int
}

// SubscriptionShadowrocket 渲染一份小火箭(Shadowrocket)可导入的完整配置。
//
// 与 Clash 版同源同义:节点 + 「只把游戏端口送进代理」的规则。
// .conf 的 [Proxy] 语法是 `名字 = 类型,服务器,端口,key=value...`,
// 其中跳过证书校验写作 skip-cert-verify=true(与 Shadowrocket 其它协议一致)。
func SubscriptionShadowrocket(p SubParams) (string, error) {
	p, err := p.normalize()
	if err != nil {
		return "", err
	}
	// 裸值不能含逗号与换行:前者会被当成下一个字段,后者会截断整行。
	for _, f := range []struct{ name, val string }{
		{"主机", p.Host}, {"密码", p.Password},
	} {
		if strings.ContainsAny(f.val, ",\r\n") {
			return "", fmt.Errorf("hy2: %s 含逗号或换行,无法写进小火箭的 .conf(请换一个值)", f.name)
		}
	}
	v := srView{
		Host:     p.Host,
		Port:     p.Port,
		Password: p.Password,
		SkipCert: !p.Secure,
		GamePort: p.GamePort,
	}
	var b strings.Builder
	if err := srTmpl.Execute(&b, v); err != nil {
		return "", fmt.Errorf("hy2: 渲染小火箭配置失败: %w", err)
	}
	return b.String(), nil
}

// clashTmpl 是 Clash Meta / FlClash 的配置模板。
//
// tun 与 dns 两段照抄 deploy/rocom-clash.yaml:游戏流量要在 TUN 模式下才拦得到,
// 而 fake-ip 是为了让「域名规则」与「IP 规则」都还能命中(见那份模板里的说明)。
var clashTmpl = template.Must(template.New("clash").Parse(clashTmplText))

const clashTmplText = `# rocom-go 配置订阅 —— 由服务端按当前运行状态生成,别手改:改了下次刷新就被覆盖。
#
# Clash Meta / FlClash / Mihomo:配置 → 新建 → 从 URL 导入(填本页那条订阅地址)。
# 导入后确认「抓包通道」这个策略组选中的是 {{.ProxyName}}(不是 DIRECT)。
#
# 这份配置只把游戏端口 {{.GamePort}} 的流量送进代理,其余一律直连 ——
# 那台机器是抓包机,不是日常出口;反正要抓的也只有游戏那一条连接。
#
# 密码 / 端口 / 要不要跳过证书校验三样已由服务端算好:它们分别散落在配置文件、
# 运行中的监听地址与证书 SAN 里,手拼迟早拼错,而拼错的表象是「连不上」,看不出哪儿错。

port: 7890
socks-port: 7891
allow-lan: false
mode: rule
log-level: warning
ipv6: false
external-controller: 127.0.0.1:9090

tun:
  enable: true
  stack: system
  dns-hijack:
    - 198.18.0.2:53
  auto-route: true
  auto-detect-interface: true

dns:
  enable: true
  listen: 127.0.0.1:1053
  enhanced-mode: fake-ip
  fake-ip-range: 198.18.0.1/16
  default-nameserver:
    - 223.5.5.5
    - 119.29.29.29
  nameserver:
    - https://doh.pub/dns-query
    - https://dns.alidns.com/dns-query

proxies:
  - name: "{{.ProxyName}}"
    type: hysteria2
    server: {{.Host}}
    port: {{.Port}}
    password: {{.Password}}
    udp: false{{if .SkipCert}}
    # 服务端证书的 SAN 里没有这个主机(多半是按 IP 连),只能不校验。
    # 换成证书里的域名访问即可去掉这一行。
    skip-cert-verify: true{{end}}

proxy-groups:
  - name: "抓包通道"
    type: select
    proxies:
      - "{{.ProxyName}}"
      - DIRECT

rules:
  # 命门:必须排在 GEOIP,CN,DIRECT 之前。游戏服务器是国内 IP,
  # 一旦被那条规则(或末尾的 MATCH)先放行,流量就走了直连 ——
  # 游戏照样能玩,但这台机器一个包都收不到,面板上自然没有数据。
  - DST-PORT,{{.GamePort}},抓包通道
  - IP-CIDR,192.168.0.0/16,DIRECT,no-resolve
  - IP-CIDR,10.0.0.0/8,DIRECT,no-resolve
  - IP-CIDR,172.16.0.0/12,DIRECT,no-resolve
  - IP-CIDR,100.64.0.0/10,DIRECT,no-resolve
  - IP-CIDR,127.0.0.0/8,DIRECT,no-resolve
  - IP-CIDR,224.0.0.0/4,DIRECT,no-resolve
  - GEOIP,CN,DIRECT
  - MATCH,DIRECT
`

// 曾有第三路:sing-box 原生 JSON(给 Hiddify),2026-09-19 实测无效后删除。
// 理由写在 internal/server/api_sub.go 分发的那一处 —— 别急着加回来:Clash YAML 与
// sing-box JSON 都试过、都没通,说明瓶颈在客户端侧而不是格式。

// srTmpl 是小火箭的 .conf 模板。段落结构与键名取自小火箭导出的配置,
// 只保留这份用途真正需要的最小集合。
//
// 规则指向 [Proxy Group] 而不是直接指向节点:小火箭两侧都收,但能查到的官方与
// 社区示例一律走策略组这一层,且组里能一眼看出「当前选的是哪个」——
// 抓包失败的典型原因之一就是这里被选成了 DIRECT,留着这一层便于当场核对。
var srTmpl = template.Must(template.New("shadowrocket").Parse(srTmplText))

const srTmplText = `# rocom-go 配置订阅 —— 由服务端按当前运行状态生成,别手改:改了下次刷新就被覆盖。
#
# 小火箭(Shadowrocket):配置 → 添加配置 → 填本页那条订阅地址。
# 导入后确认「抓包通道」这个策略组选中的是 ROCOM hy2(不是 DIRECT)。
#
# 只把游戏端口 {{.GamePort}} 的流量送进代理,其余直连。
# 游戏服务器是国内 IP:一旦被 GEOIP,CN,DIRECT 或 FINAL 先放行,流量就走了直连,
# 游戏能玩但这台机器收不到包 —— 规则顺序是这份配置里唯一要紧的东西。

[General]
bypass-system = true
ipv6 = false
dns-server = https://doh.pub/dns-query, https://dns.alidns.com/dns-query
fallback-dns-server = 223.5.5.5, 119.29.29.29
skip-proxy = 127.0.0.1, localhost, *.local, 192.168.0.0/16, 10.0.0.0/8, 172.16.0.0/12

[Proxy]
ROCOM hy2 = hysteria2, {{.Host}}, {{.Port}}, password={{.Password}}{{if .SkipCert}}, skip-cert-verify=true{{end}}

[Proxy Group]
抓包通道 = select, ROCOM hy2, DIRECT

[Rule]
DST-PORT,{{.GamePort}},抓包通道
IP-CIDR,192.168.0.0/16,DIRECT,no-resolve
IP-CIDR,10.0.0.0/8,DIRECT,no-resolve
IP-CIDR,172.16.0.0/12,DIRECT,no-resolve
IP-CIDR,127.0.0.0/8,DIRECT,no-resolve
GEOIP,CN,DIRECT
FINAL,DIRECT
`
