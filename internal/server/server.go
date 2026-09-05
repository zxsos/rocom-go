// Package server 提供 REST API、SSE 实时推送,并 embed 前端静态资源。
//
// 文件划分(路由集中注册在 server.go 的 routes):
//   - api_pets / api_map / api_eggs / api_handbook:业务接口(宠物·事件·筛选 / 实时地图 / 精灵蛋 / 图鉴炫彩)
//   - api_account / api_rank / api_debug:账号 PIN 与删除 / 排行榜 / 调试解析
//   - admin / admin_manage / admin_inject:管理员认证会话 / 统计与黑白名单 / 注入精灵投放与生命周期
//   - merchant / merchant_notify / merchant_mail / merchant_smtp / merchant_sub:
//     远行商人(槽缓存与回源 / 新货订阅匹配 / 邮件排版 / SMTP 投递 / 订阅管理)
//   - flowers / paint:花种世界存档槽 / 涂地覆盖位图
//   - stream / hub:SSE 端点 / 广播中心
package server

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/whoisnian/rocom-capture/internal/gamedata"
	"github.com/whoisnian/rocom-capture/internal/socks5"
	"github.com/whoisnian/rocom-capture/internal/store"
)

//go:embed all:web
var webFS embed.FS

// Server 聚合存储、广播中心与路由。
type Server struct {
	store       *store.Store
	hub         *Hub
	mux         *http.ServeMux
	db          *gamedata.DB
	opcodeNames map[uint16]string
	medals      []gamedata.MedalEntry
	medalIDs    map[string][]uint32 // 奖牌名 -> id 列表(同名多枚时全含),用于把筛选名解析为 id
	icons       iconMeta

	snap *snapshotStore // 各账号「最近一次」实时快照,四类数据各自加锁(见 state.go)

	online   *onlineTracker // 账号在线状态(自带锁,见 state.go)
	accounts *acctResolver  // 请求→账号推导(自带锁与 5s 缓存,见 state.go)

	adminMu    sync.Mutex
	adminToken string // 管理员会话令牌;服务重启后失效需重新登录

	// eggAPIKey:第三方图鉴 API 令牌(-egg-api-key),查随机蛋可能物种用;空=不启用。
	// 现在可被管理面板在运行期修改,而读取方有 HTTP 请求与 merchantLoop 两个 goroutine,
	// 故经 eggAPIKey()/setEggAPIKey() 访问 —— 直接读写字段会构成数据竞争。
	// 对外只暴露「是否已设置」(keySet),令牌原文不下发前端(见 New 的注释)。
	eggAPIKey   string
	eggAPIKeyMu sync.Mutex

	merchantMu sync.Mutex // 远行商人回源互斥:并发请求/定时任务同时缺缓存时,只放行一次回源(见 merchant.go)

	// 当前生效的远行商人数据源(见 merchant.go 的源常量):启动时从库载入,
	// 切源时更新。它几乎不变而被频繁读取(每次请求与每个 tick),故存内存镜像。
	merchantSrc   string
	merchantSrcMu sync.Mutex

	// 当前生效的查蛋数据源(见 api_egg_query.go 的源常量):启动时从库载入,
	// 切源时更新。同上,存内存镜像。
	eggSrc   string
	eggSrcMu sync.Mutex

	// 远行商人订阅提醒的进程内认领:槽开始时间戳 → 上一次认领时刻(见 merchant_notify.go)。
	// 用途:同一槽被并发触发时只放行一个调用去发信,避免订阅者收到两份一模一样的邮件。
	merchantClaimMu sync.Mutex
	merchantClaimed map[int64]time.Time

	// 远行商人本轮的回源尝试次数:槽开始时间戳 → 已尝试几次(见 merchant.go)。
	// 用途:日志里给出「第几次才拿到货单」—— 整点后第三方滞后切换时,没有序号就
	// 分不出「第 4 次才拿到」与「一次命中」,而那正是判断第三方是否异常的依据。
	merchantTryMu sync.Mutex
	merchantTries map[int64]int

	// 远行商人订阅邮件提醒:发件 QQ 邮箱与 SMTP 授权码(-merchant-smtp-user/-merchant-smtp-pass),
	// 空=订阅提醒不可用(前端提示,商家数据仍正常)。一批订阅者共用一个 SMTP 会话串行发信,
	// 既省掉重复的握手与认证,又不增加并发连接(QQ 邮箱对并发连接敏感,见 state.go)。
	// 凭据可被管理面板在运行期改(见 smtpSender.setCredentials)。
	smtp *smtpSender

	// 运行期配置(管理面板可改,见 api_admin_config.go)。
	// envPath 是配置**唯一**的落盘位置(systemd 的 EnvironmentFile,由 deploy.sh 生成);
	// 内存里的值只是「不必重启的加速」—— 故任何改动都必须先落盘再改内存,
	// 两者顺序反了就会「重启后配置丢失」。
	envPath string
	// socks5Mgr 管理内置代理的启停。改代理配置不必重启进程,也就不打断抓包。
	socks5Mgr *socks5.Manager
	// web 托管 Web 服务的监听,使监听地址也能在运行期改(试运行→确认,见 web_listen.go)。
	// main 启动时注入;为空时改地址的端点返回 503(单元测试与只用到 Handler 的场景)。
	web *webServer

	injectMu sync.Mutex
	injects  map[string][]*injectEntry // 账号 -> 已注入精灵(管理员投放,有生命周期,见 admin_inject.go)

	paint paintState // 涂地覆盖位图(自带锁,见 paint.go)
}

// iconMeta 是全局固定图标(每只宠物都一样,不随宠物下发):六维属性小图 + 异色/炫彩/污染标记图。
// 前端一次性拉取(GET /api/icons),供六维栏与标记徽标渲染。
type iconMeta struct {
	Stat          map[string]string `json:"stat"` // hp/attack/spAttack/defense/spDefense/speed -> 相对路径
	Type          map[string]string `json:"type"` // 系别中文名 -> 图标路径(筛选按钮用)
	Shiny         string            `json:"shiny,omitempty"`
	Colorful      string            `json:"colorful,omitempty"`
	ShinyColorful string            `json:"shinyColorful,omitempty"`
	Pollution     string            `json:"pollution,omitempty"`
	PartnerFrame  string            `json:"partnerFrame,omitempty"` // 搭档标记徽章橙色外框底(img_collect)
}

// New 创建 HTTP 服务。eggAPIKey 是查询随机蛋(神奇的蛋)可能物种的第三方图鉴 API 令牌,
// 只在服务端持有;空字符串 = 孵蛋页不提供查询(前端会提示未配置)。
// smtpUser/smtpPass 是远行商人订阅邮件提醒的发件 QQ 邮箱与授权码,空 = 订阅提醒不可用。
// New 创建 HTTP 服务。socks5Mgr 为 nil 时自建一个(测试与纯 Web 场景)。
func New(st *store.Store, hub *Hub, db *gamedata.DB, eggAPIKey, smtpUser, smtpPass string, socks5Mgr *socks5.Manager) *Server {
	s := &Server{store: st, hub: hub, mux: http.NewServeMux(), db: db, opcodeNames: db.OpcodeNames(), medals: db.AllMedals(), eggAPIKey: eggAPIKey}
	s.snap = newSnapshotStore()
	s.medalIDs = map[string][]uint32{}
	s.injects = map[string][]*injectEntry{}
	s.online = newOnlineTracker()
	s.accounts = newAcctResolver(s.online, st)
	s.smtp = newSMTPSender(smtpUser, smtpPass)
	s.envPath = configEnvPath()
	if socks5Mgr != nil {
		s.socks5Mgr = socks5Mgr
	} else {
		s.socks5Mgr = socks5.NewManager()
	}
	// 远行商人数据源:库里没配置(老库/首次)或值非法时回退默认源。
	// 读取失败按「未配置」处理(表是后加的,老库没有这一行属正常),同样回退。
	s.merchantSrc = merchantSrcDefault
	if v := st.MerchantSource(); merchantSourceValid(v) {
		s.merchantSrc = v
	}
	// 查蛋数据源:同上,库里没配置或值非法时回退默认(本地)源。
	s.eggSrc = eggSrcDefault
	if v := st.EggSource(); eggSourceValid(v) {
		s.eggSrc = v
	}
	for _, m := range s.medals {
		s.medalIDs[m.Name] = append(s.medalIDs[m.Name], m.ID)
	}
	// 六维编号 1-6:1生命 2物攻 3魔攻 4物防 5魔防 6速度(与 pet.ToPet 六维顺序一致)。
	s.icons = iconMeta{
		Stat: map[string]string{
			"hp":        db.AttributeTypeIcon(1),
			"attack":    db.AttributeTypeIcon(2),
			"spAttack":  db.AttributeTypeIcon(3),
			"defense":   db.AttributeTypeIcon(4),
			"spDefense": db.AttributeTypeIcon(5),
			"speed":     db.AttributeTypeIcon(6),
		},
		Type:          db.SkillDamTypeIcons(),
		Shiny:         db.StaticIcon("shiny"),
		Colorful:      db.StaticIcon("colorful"),
		ShinyColorful: db.StaticIcon("shiny_colorful"),
		Pollution:     db.StaticIcon("pollution"),
		PartnerFrame:  db.StaticIcon("partner_frame"),
	}
	s.routes()
	go s.sweepInjects()        // 注入精灵生命周期:玩家靠近 10 秒后自动消失
	go s.merchantLoop()        // 远行商人:按 4h 槽定时回源第三方并缓存(见 api_merchant.go)
	go s.startRankSettlement() // 排行榜称号:每晚 00:05 结算,启动时补结算(见 api_rank.go)
	return s
}

// Hub 返回广播中心。
func (s *Server) Hub() *Hub { return s.hub }

// SetWebServer 注入 Web 监听托管,使管理面板能在运行期改监听地址。
// main 在开始监听之前调用(见 cmd/rocom-capture/main.go)。
func (s *Server) SetWebServer(w *webServer) { s.web = w }

// OpcodeName 返回 opcode 的可读名称。
func (s *Server) OpcodeName(op uint16) string {
	if n, ok := s.opcodeNames[op]; ok {
		return n
	}
	return fmt.Sprintf("UNKNOWN_0x%04X", op)
}

// Handler 返回 http.Handler。
func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/pets", s.handlePets)
	s.mux.HandleFunc("GET /api/pets/{gid}", s.handlePet)
	s.mux.HandleFunc("GET /api/events", s.handleEvents)
	s.mux.HandleFunc("GET /api/events/count", s.handleEventCount)
	s.mux.HandleFunc("GET /api/events/stats", s.handleEventStats)
	s.mux.HandleFunc("DELETE /api/events", s.handleClearEvents)
	s.mux.HandleFunc("GET /api/filter-options", s.handleFilterOptions)
	s.mux.HandleFunc("GET /api/stats", s.handleStats)
	s.mux.HandleFunc("GET /api/medals", s.handleMedals)
	s.mux.HandleFunc("GET /api/name-options", s.handleNameOptions)
	s.mux.HandleFunc("GET /api/icons", s.handleIcons)
	s.mux.HandleFunc("GET /api/boxes", s.handleBoxes)
	s.mux.HandleFunc("GET /api/teams", s.handleTeams)
	s.mux.HandleFunc("GET /api/evolution", s.handleEvolution)
	s.mux.HandleFunc("GET /api/pet-page", s.handlePetPage)
	s.mux.HandleFunc("GET /api/accounts", s.handleAccounts)
	// 账号 PIN 保护 + 账号删除(隐私:切账号需 PIN,删账号需 PIN 或管理员)
	s.mux.HandleFunc("POST /api/account/verify", s.handleAccountVerify)
	s.mux.HandleFunc("POST /api/account/pin", s.handleAccountPin)
	s.mux.HandleFunc("DELETE /api/account", s.handleAccountDelete)
	// 排行榜:福布斯(洛克贝) / 盈亏排行;参与开关(默认参加,可在洛克贝旁一键退出)。
	s.mux.HandleFunc("GET /api/leaderboard", s.handleLeaderboard)
	s.mux.HandleFunc("POST /api/account/rank", s.handleAccountRank)
	// 管理员(隐式面板,前端导航不显示):首启设置密码 → 登录签发内存令牌 → 校验后使用。
	s.mux.HandleFunc("GET /api/admin/status", s.handleAdminStatus)
	s.mux.HandleFunc("POST /api/admin/setup", s.handleAdminSetup)
	s.mux.HandleFunc("POST /api/admin/login", s.handleAdminLogin)
	s.mux.HandleFunc("POST /api/admin/logout", s.handleAdminLogout)
	s.mux.HandleFunc("GET /api/admin/rules", s.handleAdminRules)
	s.mux.HandleFunc("POST /api/admin/rules", s.handleAdminRuleSet)
	s.mux.HandleFunc("DELETE /api/admin/rules", s.handleAdminRuleDelete)
	s.mux.HandleFunc("GET /api/admin/stats", s.handleAdminStats)
	s.mux.HandleFunc("GET /api/admin/play-sessions", s.handleAdminPlaySessions)
	s.mux.HandleFunc("GET /api/admin/wild-pets", s.handleAdminWildPetOptions)
	s.mux.HandleFunc("GET /api/admin/injects", s.handleAdminListInjects)
	s.mux.HandleFunc("POST /api/admin/inject-wild", s.handleAdminInjectWild)
	s.mux.HandleFunc("DELETE /api/admin/inject-wild", s.handleAdminRevokeInject)
	s.mux.HandleFunc("POST /api/admin/inject-flower", s.handleAdminInjectFlower)
	s.mux.HandleFunc("GET /api/position", s.handlePosition)
	s.mux.HandleFunc("GET /api/pois", s.handlePois)
	s.mux.HandleFunc("GET /api/gathers", s.handleGathers)
	s.mux.HandleFunc("GET /api/wildpets", s.handleWildPets)
	// 曾有 DELETE /api/wildpets(清空野生宠标记),已删除:换场景本就整份作废
	// (见 pipeline.resetWilds),同场景内的灰点又有 4 小时 TTL 兜着 —— 手动清空只剩
	// 「不想看这些灰点了」这一种用处,不值为它留一个删数据的入口。
	s.mux.HandleFunc("GET /api/paint", s.handlePaint)
	s.mux.HandleFunc("DELETE /api/paint", s.handlePaintReset)
	s.mux.HandleFunc("GET /api/home", s.handleHome)
	s.mux.HandleFunc("GET /api/trial", s.handleTrial)
	s.mux.HandleFunc("GET /api/trial/encounters", s.handleTrialEncounters)
	// 曾有 DELETE /api/trial/encounters(清空遇见记录),已删除:见闻录是权威来源、
	// 清空后会被立刻补回,该接口不可能生效。理由见 api_trial.go 里的说明。
	s.mux.HandleFunc("GET /api/flowers", s.handleFlowers)
	s.mux.HandleFunc("GET /api/flowers/slots", s.handleFlowerSlots)
	s.mux.HandleFunc("DELETE /api/flowers/slots", s.handleDeleteFlowerSlot)
	s.mux.HandleFunc("GET /api/eggs", s.handleEggs)
	s.mux.HandleFunc("GET /api/eggs/query", s.handleEggQuery)
	s.mux.HandleFunc("GET /api/handbook-glasses", s.handleHandbookGlasses)
	s.mux.HandleFunc("GET /api/merchant", s.handleMerchant)
	s.mux.HandleFunc("GET /api/merchant/sub", s.handleMerchantSub)
	s.mux.HandleFunc("POST /api/merchant/sub", s.handleMerchantSub)
	s.mux.HandleFunc("DELETE /api/merchant/sub", s.handleMerchantSub)
	s.mux.HandleFunc("GET /api/admin/merchant-subs", s.handleAdminMerchantSubs)
	s.mux.HandleFunc("DELETE /api/admin/merchant-subs", s.handleAdminMerchantSubs)
	s.mux.HandleFunc("POST /api/admin/merchant-test-mail", s.handleAdminMerchantTestMail)
	s.mux.HandleFunc("GET /api/admin/merchant-source", s.handleAdminMerchantSource)
	s.mux.HandleFunc("POST /api/admin/merchant-source", s.handleAdminMerchantSource)
	s.mux.HandleFunc("GET /api/admin/egg-stats", s.handleAdminEggStats)
	s.mux.HandleFunc("GET /api/admin/egg-source", s.handleAdminEggSource)
	s.mux.HandleFunc("POST /api/admin/egg-source", s.handleAdminEggSource)
	s.mux.HandleFunc("GET /api/admin/config", s.handleAdminConfig)
	s.mux.HandleFunc("POST /api/admin/config", s.handleAdminConfig)
	// Web 监听地址(改它要试运行 + 确认,见 api_web_addr.go)
	s.mux.HandleFunc("POST /api/admin/web-addr", s.handleAdminWebAddr)
	s.mux.HandleFunc("POST /api/admin/web-addr/confirm", s.handleAdminWebAddrConfirm)
	s.mux.HandleFunc("POST /api/admin/web-addr/revert", s.handleAdminWebAddrRevert)
	s.mux.HandleFunc("GET /api/stream", s.handleStream)
	s.mux.HandleFunc("POST /api/debug/parse", s.handleDebugParse)
	// 宠物图片(embed 的 webp,路径如 /img/HeadIcon/3001.webp);长缓存,内容随版本变更。
	imgFS := http.FileServerFS(gamedata.ImageFS())
	s.mux.Handle("GET /img/", http.StripPrefix("/img/", cacheControl(imgFS, "public, max-age=86400")))
	s.mux.HandleFunc("/", s.handleStatic)
}

// cacheControl 给静态资源加 Cache-Control 头。
func cacheControl(h http.Handler, v string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", v)
		h.ServeHTTP(w, r)
	})
}

// acct 返回请求指向的账号:优先 ?account=,缺省回退最近活跃账号(库空则空串)。
// 实现见 state.go 的 acctResolver。
func (s *Server) acct(r *http.Request) string { return s.accounts.resolve(r) }

// TouchAccount 记录账号最近活跃时刻(Unix 秒)。pipeline 对每条可归属消息调用;
// 离线回放时消息自带历史时间戳,账号如实显示离线。
func (s *Server) TouchAccount(acc string, ts int64) { s.online.touch(acc, ts) }

// AccountOnline 判定账号是否在线(最近 onlineWindow 秒内有流量),handleAccounts 逐账号合并。
func (s *Server) AccountOnline(acc string) bool { return s.online.online(acc) }

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	sub, _ := fs.Sub(webFS, "web")
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}
	if f, err := sub.Open(path); err == nil {
		f.Close()
		// 静态资源缓存策略(三档):
		//   assets/  vite 构建产物,文件名带内容 hash,内容不变则文件名不变 → 可安全长缓存 immutable;
		//   fonts/   public 原样复制的字体文件,**无 hash**,给短缓存(13KB,每天重验一次成本极低,
		//            又能防止「改了字体文件但浏览器一直用旧的」);
		//   其余      index.html / logo.svg / route-map 数据等,无 hash 且可能随发布更新 → no-cache。
		switch {
		case strings.HasPrefix(path, "assets/"):
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		case strings.HasPrefix(path, "fonts/"):
			w.Header().Set("Cache-Control", "public, max-age=86400")
		default:
			w.Header().Set("Cache-Control", "no-cache")
		}
		http.ServeFileFS(w, r, sub, path)
		return
	}
	// SPA fallback
	//
	// assets/ 走到这里是**故障而非正常路由**:这些文件名带内容 hash,index.html 引用
	// 了就一定该存在。缺失意味着构建产物不完整 —— 常见于 vite 的 emptyOutDir 清空
	// 产物目录后 npm run build 中途失败,而 `//go:embed all:web` 对缺失文件**不报错**,
	// 于是二进制带着缺件编出来。后果极隐蔽:这里返回 200 + text/html,浏览器按 HTML
	// 规范拒绝把 HTML 当 module script 执行,React 从不挂载 —— 页面全黑,而服务端
	// 日志一条异常都没有(2026-09-04 线上事故)。故必须显式告警,让它在日志里可见。
	//
	// 部署期的防线在 scripts/deploy.sh 的 check_frontend_assets(编译前校验,拦在根因);
	// 这里是兜底,覆盖手工替换二进制等绕过脚本的路径。
	if strings.HasPrefix(path, "assets/") {
		log.Printf("警告: 静态资源缺件,已回退到 index.html: %s — 前端产物不完整(重新 npm run build 后再编译)", path)
	}
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFileFS(w, r, sub, "index.html")
}
