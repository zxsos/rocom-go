package main

import (
	"crypto/tls"
	"flag"
	"log"
	"net/http"
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
	probe := flag.String("merchant-probe", "", "远行商人整点抢单(临时测试模式):auto=对准下一个 8/12/16/20 整点每 10s 回源直到拿到本轮货单,now=立即开始;空=不启用")
	slots := flag.Bool("slots-capture", true, "远行商人档期观察(临时验证模式):对准下一个 8/12/16/20 整点,前 2 分钟起每 30s 抓一次 onebiji 页面到整点后 7 分钟,只打日志不改业务;false=不启用")
	flag.Parse()

	db, err := gamedata.Load()
	if err != nil {
		log.Fatalf("加载名称库失败: %v", err)
	}
	st, err := store.New(*dbPath, db)
	if err != nil {
		log.Fatalf("打开数据库失败: %v", err)
	}
	srv := server.New(st, server.NewHub(), db, *eggAPIKey, *smtpUser, *smtpPass)
	if *probe != "" {
		srv.StartMerchantProbe(*probe) // 临时测试模式,见 AI_merchant_probe.md
	}
	if *slots {
		srv.StartSlotsCapture() // 临时验证模式,见 docs/merchant-onebiji-probe.md
	}
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

	pl := pipeline.New(st, db, srv)
	go pl.Run(eng)
	go serveWeb(*addr, srv.Handler(), *useTLS, *certPath, *keyPath)
	if *socks5Addr != "" {
		if *skipSelf && *iface != "" {
			log.Printf("提示: -socks5-addr 已启用但未设 -skip-self-ip=false,代理进程以本机 IP 出站的游戏流量会被丢弃")
		}
		go serveSocks5(*socks5Addr, *socks5Allow, *socks5Block, *socks5Max, *socks5User, *socks5Pass)
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

// serveWeb 启动 Web 服务(-tls 时用自签证书起 HTTPS,证书不存在则生成,见 tls.go)。
func serveWeb(addr string, h http.Handler, useTLS bool, certPath, keyPath string) {
	if useTLS {
		cert, err := loadOrCreateCert(certPath, keyPath)
		if err != nil {
			log.Fatalf("准备 TLS 证书失败: %v", err)
		}
		hs := &http.Server{
			Addr:      addr,
			Handler:   h,
			TLSConfig: &tls.Config{Certificates: []tls.Certificate{cert}},
		}
		log.Printf("Web 界面: https://localhost%s (自签证书,浏览器首次访问需手动信任)", addr)
		if err := hs.ListenAndServeTLS("", ""); err != nil {
			log.Fatalf("HTTPS 服务失败: %v", err)
		}
		return
	}
	log.Printf("Web 界面: http://localhost%s", addr)
	// ReadHeaderTimeout: 防慢速 header 攻击(Slowloris),不影响正常请求。
	// IdleTimeout: 空闲连接最大存活时间,清理断开未检测的残留连接,避免 goroutine 堆积。
	// 注意:不设 WriteTimeout/ReadTimeout —— SSE 长连接(/api/stream)需要无写超时,
	// 设了会中断流式推送。ReadHeaderTimeout 只影响 header 读取阶段,不影响 body/SSE。
	hs := &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	if err := hs.ListenAndServe(); err != nil {
		log.Fatalf("HTTP 服务失败: %v", err)
	}
}

// serveSocks5 启动内置 SOCKS5 代理(仅 TCP CONNECT),供手机把游戏流量代理到本机,
// 整网卡抓包即可看到代理进程以本机 IP 出站的连接(须配合 -skip-self-ip=false,见 main)。
func serveSocks5(addr, allow, block string, maxConns int, user, pass string) {
	prefs, err := socks5.ParseAllow(allow)
	if err != nil {
		log.Fatalf("解析 -socks5-allow 失败: %v", err)
	}
	if user != "" && pass == "" {
		log.Fatal("-socks5-user 已设置但 -socks5-pass 为空")
	}
	blocked := []string{}
	for part := range strings.SplitSeq(block, ",") {
		if part = strings.TrimSpace(part); part != "" {
			blocked = append(blocked, part)
		}
	}
	if err := socks5.ListenAndServe(addr, prefs, blocked, maxConns, user, pass); err != nil {
		log.Fatalf("SOCKS5 服务失败: %v", err)
	}
}
