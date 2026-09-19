package main

import (
	"crypto/tls"
	"flag"
	"log"
	"net/netip"
	"strings"
	"time"

	"github.com/zxsos/rocom-go/internal/capture"
	"github.com/zxsos/rocom-go/internal/gamedata"
	"github.com/zxsos/rocom-go/internal/hy2"
	"github.com/zxsos/rocom-go/internal/pipeline"
	"github.com/zxsos/rocom-go/internal/server"
	"github.com/zxsos/rocom-go/internal/store"
)

func main() {
	pcapPath := flag.String("pcap", "", "离线 pcap 文件路径(回放模式)")
	iface := flag.String("iface", "", "实时抓包网卡名;填 auto 则自动选默认路由所在的那张(容器里也能算,推荐)")
	ignoreIPs := flag.String("ignore-ip", "", "额外忽略的 IP(逗号分隔;两端命中即丢包)。实时抓包已自动忽略网卡自身 IP,此项用于离线回放或多网关等场景")
	skipSelf := flag.Bool("skip-self-ip", true, "忽略网卡自身 IP(单臂网关去重)。hy2/云代理模式下本机进程出站的游戏流量以本机 IP 为源,须设 false 才抓得到(启用 -hy2-addr 且未显式指定本项时会自动用 false)")
	port := flag.Int("port", 8195, "游戏服务器端口")
	addr := flag.String("addr", ":4939", "Web 服务监听地址")
	dbPath := flag.String("db", "rocom.db", "SQLite 数据库路径")
	useTLS := flag.Bool("tls", false, "启用 HTTPS(自签证书;手机经局域网访问以满足屏幕常亮等需 secure context 的 API)")
	certPath := flag.String("cert", "rocom-cert.pem", "TLS 证书路径(-tls 时不存在则自动生成自签证书)")
	keyPath := flag.String("key", "rocom-key.pem", "TLS 私钥路径(-tls 时不存在则自动生成)")
	hy2Addr := flag.String("hy2-addr", "", "内嵌 hysteria2 代理的 UDP 监听地址(如 :11443;空=不启用)。手机端须用支持 hy2 的客户端(Clash Meta / 小火箭 / sing-box);防火墙与云安全组放行的是 **UDP** 而非 TCP。流量到本机后整网卡抓包即可见代理进程出站连接,须配合 -skip-self-ip=false")
	hy2Pass := flag.String("hy2-pass", "", "hysteria2 认证密码(隧道内传输,非明文)。公网部署时请与 -hy2-allow 白名单搭配使用")
	hy2Allow := flag.String("hy2-allow", "", "hysteria2 客户端 IP 白名单(逗号分隔,支持 IP 或 CIDR 网段;空=不限制)。带公网 IP 部署时必填:密码挡不住扫描器打满 UDP 端口")
	hy2Max := flag.Int("hy2-max-conns", 128, "hysteria2 同时处理的最大连接数(超限直接拒绝;0=不限制),防连接风暴拖垮同进程 Web 服务")
	hy2Block := flag.String("hy2-block", "google.com,example.com", "hysteria2 屏蔽的目标域名(逗号分隔,精确或子域匹配;默认含手机系统连通性探测常用域名 google.com/example.com,可覆盖。空=不屏蔽)")
	hy2Up := flag.Int("hy2-up", hy2.DefaultUpMbps, "hysteria2 上行带宽(Mbps),供拥塞控制用;**重启进程才生效**(详见 internal/hy2 的注释)")
	hy2Down := flag.Int("hy2-down", hy2.DefaultDownMbps, "hysteria2 下行带宽(Mbps),供拥塞控制用;**重启进程才生效**")
	// 以下 -socks5-* 已废弃:内置 SOCKS5 被 hysteria2 取代(理由见 internal/hy2 的包注释)。
	// 仍然**保留定义**只有一个原因:Go 的 flag 包遇到未定义参数会直接 os.Exit(2),
	// 而 deploy.sh 生成的 /etc/rocom.env 不会自动删键 —— 老机器升级后残留的
	// ROCOM_SOCKS5_* 会被组装成启动参数,届时进程起不来,配合 systemd 的
	// Restart=on-failure 就是崩溃循环。故这里照单收下、不使用、只告警。
	// 返回值一律丢弃:它们只读进 flag 表里占个位,任何地方都不该再使用。
	flag.String("socks5-addr", "", "已废弃(SOCKS5 已被 hysteria2 取代):仍可解析但不生效,请改用 -hy2-addr")
	flag.String("socks5-allow", "", "已废弃:请改用 -hy2-allow")
	flag.Int("socks5-max-conns", 128, "已废弃:请改用 -hy2-max-conns")
	flag.String("socks5-user", "", "已废弃:hysteria2 只需密码,不再需要用户名")
	flag.String("socks5-pass", "", "已废弃:请改用 -hy2-pass")
	flag.String("socks5-block", "", "已废弃:请改用 -hy2-block")
	smtpUser := flag.String("merchant-smtp-user", "", "远行商人订阅提醒的发件 QQ 邮箱地址(需开启 SMTP 并配合 -merchant-smtp-pass 授权码;空=订阅提醒不可用)")
	smtpPass := flag.String("merchant-smtp-pass", "", "远行商人订阅提醒的发件 QQ 邮箱 SMTP 授权码(QQ 邮箱设置里生成,非登录密码;空=订阅提醒不可用)")
	flag.Parse()

	// -skip-self-ip 是否被**显式**指定过:flag.Visit 只遍历命令行里真正出现过的 flag。
	// 用于下面的「hy2 自动置 false」—— 显式传了就尊重用户的选择,不再自作主张。
	skipSelfSet := false
	// 同时收集被显式设置的**废弃** flag,好给出明确的迁移提示。
	var deprecated []string
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "skip-self-ip" {
			skipSelfSet = true
		}
		if len(f.Name) >= 6 && f.Name[:6] == "socks5" {
			deprecated = append(deprecated, "-"+f.Name)
		}
	})
	if len(deprecated) > 0 {
		log.Printf("警告: %v 已废弃(内置 SOCKS5 被 hysteria2 取代),这些参数**不生效**;"+
			"请改用 -hy2-addr/-hy2-pass/-hy2-allow,并从 /etc/rocom.env 删掉 ROCOM_SOCKS5_*",
			deprecated)
	}

	db, err := gamedata.Load()
	if err != nil {
		log.Fatalf("加载名称库失败: %v", err)
	}
	st, err := store.New(*dbPath, db)
	if err != nil {
		log.Fatalf("打开数据库失败: %v", err)
	}
	// 代理交给 Manager 管理生命周期:面板改代理配置时能只重启它,不必重启整个进程
	// (重启会打断正在解密的游戏连接)。校验与启动逻辑都在 hy2.Config / Manager.Start 里。
	hy2Mgr := hy2.NewManager()
	srv := server.New(st, server.NewHub(), db, *smtpUser, *smtpPass, hy2Mgr)
	eng := capture.NewEngine(*port)
	eng.Keys = st // 会话密钥持久化:抓包服务重启后继续解密仍存活的连接
	for s := range strings.SplitSeq(*ignoreIPs, ",") {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		ip, err := netip.ParseAddr(s)
		if err != nil {
			log.Fatalf("-ignore-ip 无效地址 %q: %v", s, err)
		}
		eng.AddSkipIP(ip)
	}

	// Web 服务交给 server 的监听器托管(而非这里直接 ListenAndServe):
	// 管理面板要在运行期改监听地址,必须能「先起新的、成功后再停旧的」——
	// 那要求有人持有并管理监听器,见 internal/server/web_listen.go。
	// 证书只在这里准备一次,换地址时复用同一份(它是 -tls 的产物,与监听地址无关)。
	// hy2 是它的第二个使用方:hy2 必须跑在 TLS 上,故启用代理时也必须有证书 ——
	// 复用同一份自签证书(客户端 skip-cert-verify 即可),不必再多维护一套。
	// 但「有证书」不等于「Web 走 HTTPS」:Web 的协议只由 -tls 决定。经 nginx 反代
	// 的部署里 TLS 在边缘终结,后端这一跳走明文反而少一份自签证书的信任问题。
	var tlsCfg *tls.Config
	if *useTLS || *hy2Addr != "" {
		cert, err := loadOrCreateCert(*certPath, *keyPath)
		if err != nil {
			log.Fatalf("准备 TLS 证书失败: %v", err)
		}
		hy2Mgr.SetCert(cert)
		if *useTLS {
			tlsCfg = &tls.Config{Certificates: []tls.Certificate{cert}}
		}
	}
	web := server.NewWebServer(srv.Handler(), tlsCfg)
	srv.SetWebServer(web)
	// 后台循环必须在 SetWebServer 之后再起:循环里会读 s.web,提前启动会撞上 nil(见 Server.Start)。
	srv.Start()

	pl := pipeline.New(st, db, srv)
	go pl.Run(eng)
	if err := web.Listen(*addr); err != nil {
		log.Fatalf("Web 服务失败: %v", err)
	}
	if *hy2Addr != "" {
		if !skipSelfSet && *skipSelf {
			// 启用代理却留着 skip-self-ip=true:代理进程以本机 IP 出站的流量会**两个
			// 方向全被丢**,表现是手机能玩、包数在涨,却一条数据都解析不出来(极难自查)。
			// 这种组合几乎必然是配置疏忽,故在用户没显式指定时直接替他改掉并说明。
			log.Printf("已启用 hysteria2 代理,自动改用 -skip-self-ip=false(代理以本机 IP 出站,设 true 会一个包都抓不到);" +
				"如确要保留请显式传 -skip-self-ip=true")
			*skipSelf = false
		}
		if err := hy2Mgr.Start(hy2.Config{
			Addr:     *hy2Addr,
			Password: *hy2Pass,
			Allow:    *hy2Allow,
			Block:    *hy2Block,
			MaxConns: *hy2Max,
			UpMbps:   *hy2Up,
			DownMbps: *hy2Down,
		}); err != nil {
			log.Fatalf("hysteria2 服务启动失败: %v", err)
		}
	}

	switch {
	case *pcapPath != "":
		log.Printf("离线回放: %s", *pcapPath)
		if err := eng.RunOffline(*pcapPath); err != nil {
			log.Fatalf("回放失败: %v", err)
		}
		// 等管线消费完 Out 缓冲里的剩余消息再统计:RunOffline 的 close(Out) 只表示
		// 「不再有新消息」,已缓冲的(最多 4096 条)还在被 handle 消费,不等待的话
		// 下面这行会拿到偏小的数字(实测打印「共宠物 0 只」而实际 730 只)。
		// 涂地同理 —— 它也是管线写的,故等完再补刷盘。
		<-pl.Done()
		// 涂地是攒批落盘的(见 server/paint.go),而回放几秒就跑完一整份 pcap、一次都攒不到,
		// 故这里补一次落盘,否则回放出来的覆盖图重启就没了。
		srv.FlushPaint()
		log.Printf("回放完成，%d 个账号共宠物 %d 只。Web 服务保持运行(Ctrl-C 退出)", pl.AccountCount(), pl.PetTotal())
		if d := eng.NoKeyDropped(); d > 0 {
			log.Printf("提示: %d 个数据包因尚无会话密钥被丢弃(抓包晚于密钥协商时属正常)", d)
		}
		if d := eng.BadKeyDropped(); d > 0 {
			log.Printf("提示: %d 个数据包因密钥错误(明文校验失败)被丢弃(缓存密钥失效时会出现)", d)
		}
		select {}
	case *iface != "":
		// 网卡名解析(自动选 / 候选提示 / 桥接检测)必须赶在 RunLive 之前做完:它既决定
		// 横幅里显示什么,也决定失败时给出的错误信息 —— 容器部署下用户只有 docker logs 可看。
		info, err := capture.ResolveIface(*iface)
		if err != nil {
			log.Fatalf("抓包失败: %v", err)
		}
		if info.Warn != "" {
			log.Printf("警告: %s", info.Warn)
		}
		printBanner(*port, *addr, *dbPath, *hy2Addr, *skipSelf, *useTLS, info)
		// 定期摘要:RunLive 阻塞,故在它之前起。丢包由 capture 包在采样到增量时
		// 立即告警,这里只做周期性汇总 —— 让人不查日志也知道当前是否在丢。
		go func() {
			tick := time.NewTicker(5 * time.Minute)
			defer tick.Stop()
			for range tick.C {
				log.Printf("抓包统计: 收到 %d 个包,丢弃 %d 个,无密钥丢弃 %d 个,密钥错误丢弃 %d 个",
					capture.PacketSeen(), capture.PacketDropped(),
					eng.NoKeyDropped(), eng.BadKeyDropped())
			}
		}()
		if err := eng.RunLive(info.Name, *skipSelf); err != nil {
			log.Fatalf("抓包失败(需 root): %v", err)
		}
	default:
		log.Println("用法: -pcap <文件> 或 -iface <网卡|auto>(不确定网卡名就填 auto,会自动选默认路由那张)")
	}
}

// printBanner 在开始抓包前打一段固定的配置摘要。
//
// 为什么要它:容器部署下用户只看 `docker logs`,而「网卡选错」与「skip-self-ip 设错」
// 这两类问题的表现都很安静(前者容器反复重启、后者数据永远为空),不看 README 就不知道该
// 核对什么。把决定行为的几项一次打全,`docker logs | tail` 一眼就能确认。
func printBanner(port int, addr, dbPath, hy2Addr string, skipSelf, useTLS bool, en capture.IfaceInfo) {
	mode := "单臂网关 / 旁路镜像"
	if hy2Addr != "" {
		mode = "hysteria2 代理(监听 UDP " + hy2Addr + ")"
	}
	via := "显式指定"
	if en.Auto {
		via = "自动选中(默认路由)"
	}
	ips := "(该网卡无 IP)"
	if len(en.IPs) > 0 {
		ss := make([]string, len(en.IPs))
		for i, ip := range en.IPs {
			ss[i] = ip.String()
		}
		ips = strings.Join(ss, ", ")
	}
	scheme := "http"
	if useTLS {
		scheme = "https"
	}
	log.Printf("==================== 抓包配置 ====================")
	log.Printf("网卡      %s (%s)  %s", en.Name, ips, via)
	log.Printf("模式      %s   -skip-self-ip=%v", mode, skipSelf)
	log.Printf("端口      游戏 %d    Web %s://<本机IP>%s", port, scheme, addr)
	log.Printf("数据库    %s", dbPath)
	log.Printf("=================================================")
}

// 内嵌 hysteria2 代理供手机把游戏流量代理到本机,整网卡抓包即可看到代理进程以本机 IP
// 出站的连接(须配合 -skip-self-ip=false)。它取代了原先的内置 SOCKS5 —— 后者在公网上的
// 明文认证与开放代理特征无法靠配置弥补(理由见 internal/hy2 的包注释)。
// 启停与参数变更走 hy2.Manager(见 main 里的 hy2Mgr),管理面板可在运行期改。
