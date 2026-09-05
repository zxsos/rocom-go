package main

import (
	"crypto/tls"
	"flag"
	"log"
	"net/netip"
	"strings"
	"time"

	"github.com/whoisnian/rocom-capture/internal/capture"
	"github.com/whoisnian/rocom-capture/internal/gamedata"
	"github.com/whoisnian/rocom-capture/internal/pipeline"
	"github.com/whoisnian/rocom-capture/internal/server"
	"github.com/whoisnian/rocom-capture/internal/socks5"
	"github.com/whoisnian/rocom-capture/internal/store"
)

func main() {
	pcapPath := flag.String("pcap", "", "离线 pcap 文件路径(回放模式)")
	iface := flag.String("iface", "", "实时抓包网卡名")
	ignoreIPs := flag.String("ignore-ip", "", "额外忽略的 IP(逗号分隔;两端命中即丢包)。实时抓包已自动忽略网卡自身 IP,此项用于离线回放或多网关等场景")
	skipSelf := flag.Bool("skip-self-ip", true, "忽略网卡自身 IP(单臂网关去重)。socks5/云代理模式下本机进程出站的游戏流量以本机 IP 为源,须设 false 才抓得到")
	port := flag.Int("port", 8195, "游戏服务器端口")
	addr := flag.String("addr", ":4939", "Web 服务监听地址")
	dbPath := flag.String("db", "rocom.db", "SQLite 数据库路径")
	useTLS := flag.Bool("tls", false, "启用 HTTPS(自签证书;手机经局域网访问以满足屏幕常亮等需 secure context 的 API)")
	certPath := flag.String("cert", "rocom-cert.pem", "TLS 证书路径(-tls 时不存在则自动生成自签证书)")
	keyPath := flag.String("key", "rocom-key.pem", "TLS 私钥路径(-tls 时不存在则自动生成)")
	socks5Addr := flag.String("socks5-addr", "", "内置 SOCKS5 代理监听地址(如 :1080;空=不启用)。手机把游戏流量代理到本机后,整网卡抓包即可见代理进程出站连接,须配合 -skip-self-ip=false")
	socks5Allow := flag.String("socks5-allow", "", "SOCKS5 客户端 IP 白名单(逗号分隔,支持 IP 或 CIDR 网段;空=不限制)。带公网 IP 部署时必填,否则几分钟内会被全网扫描器滥用")
	socks5Max := flag.Int("socks5-max-conns", 128, "SOCKS5 同时处理的最大连接数(超限直接拒绝;0=不限制),防连接风暴拖垮同进程 Web 服务。多人共用或手机配了全局代理(其它 App 流量也走这里)时按需调大")
	socks5User := flag.String("socks5-user", "", "SOCKS5 认证用户名(空=无认证)。建议配合 -socks5-allow 白名单使用;RFC 1929 密码为明文传输,公网直连时配合加密隧道更稳")
	socks5Pass := flag.String("socks5-pass", "", "SOCKS5 认证密码(空=无认证;-socks5-user 非空时必填)")
	socks5Block := flag.String("socks5-block", "google.com,example.com", "SOCKS5 屏蔽的目标域名(逗号分隔,精确或子域匹配;默认含手机系统连通性探测常用域名 google.com/example.com,可覆盖。空=不屏蔽)")
	eggAPIKey := flag.String("egg-api-key", "", "查询随机蛋(神奇的蛋)可能物种的第三方图鉴 API 令牌(只在服务端持有,不下发前端;空=孵蛋页不提供查询)")
	smtpUser := flag.String("merchant-smtp-user", "", "远行商人订阅提醒的发件 QQ 邮箱地址(需开启 SMTP 并配合 -merchant-smtp-pass 授权码;空=订阅提醒不可用)")
	smtpPass := flag.String("merchant-smtp-pass", "", "远行商人订阅提醒的发件 QQ 邮箱 SMTP 授权码(QQ 邮箱设置里生成,非登录密码;空=订阅提醒不可用)")
	flag.Parse()

	db, err := gamedata.Load()
	if err != nil {
		log.Fatalf("加载名称库失败: %v", err)
	}
	st, err := store.New(*dbPath, db)
	if err != nil {
		log.Fatalf("打开数据库失败: %v", err)
	}
	// 代理交给 Manager 管理生命周期:面板改代理配置时能只重启它,不必重启整个进程
	// (重启会打断正在解密的游戏连接)。原 serveSocks5 的校验与拆分逻辑已并入
	// socks5.Config.Validate / Manager.Start。
	socks5Mgr := socks5.NewManager()
	srv := server.New(st, server.NewHub(), db, *eggAPIKey, *smtpUser, *smtpPass, socks5Mgr)
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
	var tlsCfg *tls.Config
	if *useTLS {
		cert, err := loadOrCreateCert(*certPath, *keyPath)
		if err != nil {
			log.Fatalf("准备 TLS 证书失败: %v", err)
		}
		tlsCfg = &tls.Config{Certificates: []tls.Certificate{cert}}
	}
	web := server.NewWebServer(srv.Handler(), tlsCfg)
	srv.SetWebServer(web)

	pl := pipeline.New(st, db, srv)
	go pl.Run(eng)
	if err := web.Listen(*addr); err != nil {
		log.Fatalf("Web 服务失败: %v", err)
	}
	if *socks5Addr != "" {
		if *skipSelf && *iface != "" {
			log.Printf("提示: -socks5-addr 已启用但未设 -skip-self-ip=false,代理进程以本机 IP 出站的游戏流量会被丢弃")
		}
		if err := socks5Mgr.Start(socks5.Config{
			Addr:     *socks5Addr,
			Allow:    *socks5Allow,
			Block:    *socks5Block,
			MaxConns: *socks5Max,
			User:     *socks5User,
			Pass:     *socks5Pass,
		}); err != nil {
			log.Fatalf("SOCKS5 服务启动失败: %v", err)
		}
	}

	switch {
	case *pcapPath != "":
		log.Printf("离线回放: %s", *pcapPath)
		if err := eng.RunOffline(*pcapPath); err != nil {
			log.Fatalf("回放失败: %v", err)
		}
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
		log.Printf("实时抓包: 网卡=%s 端口=%d", *iface, *port)
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
		if err := eng.RunLive(*iface, *skipSelf); err != nil {
			log.Fatalf("抓包失败(需 root): %v", err)
		}
	default:
		log.Println("用法: -pcap <文件> 或 -iface <网卡>")
	}
}

// 内置 SOCKS5 代理(仅 TCP CONNECT)供手机把游戏流量代理到本机,整网卡抓包即可看到
// 代理进程以本机 IP 出站的连接(须配合 -skip-self-ip=false)。
// 启停与参数变更走 socks5.Manager(见 main 里的 socks5Mgr),管理面板可在运行期改。
