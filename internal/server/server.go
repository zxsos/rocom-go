// Package server 提供 REST API、SSE 实时推送,并 embed 前端静态资源。
// 文件划分:api_pets(宠物/事件/筛选)/ api_map(实时地图)/ stream(SSE)/ hub(广播中心)。
package server

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/whoisnian/rocom-capture/internal/gamedata"
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

	posMu       sync.Mutex                // 保护 lastPos / lastWild / lastHome / lastFlowers
	lastPos     map[string]map[string]any // 账号 -> 最近一次位置(实时地图页加载时即时回显,不必等下一次移动)
	lastWild    map[string]any            // 账号 -> 最近一次野生宠物标记(同上,免得进页面要等下一条 AOI 通知)
	lastHome    map[string]any            // 账号 -> 最近一次家园小窝图层(同上;不在家园时为空列表)
	lastFlowers map[string]any            // 账号 -> 最近一次花种 BOSS 分组(花种页加载时即时回显,见 0x0375)

	onlineMu sync.Mutex       // 保护 lastSeen
	lastSeen map[string]int64 // 账号 -> 最近活跃 Unix 秒(pipeline 上报,/api/accounts 据此标在线)

	// acctCache 缓存最近活跃账号,避免 acct() 每次请求都跑 ListAccounts 全表 JOIN。
	// 前端首次加载(currentAccount 为空)时并行发多个 API,每个都调 acct() → ListAccounts(),
	// 该查询 LEFT JOIN pets + GROUP BY + ORDER BY,多账号时是全表扫描,N 个并发请求 = N 次全表扫。
	// 用 lastSeen 推导最近活跃账号,5 秒内复用,避免重复查库。
	acctCacheMu   sync.Mutex
	acctCacheVal  string
	acctCacheTime time.Time

	adminMu    sync.Mutex
	adminToken string // 管理员会话令牌;服务重启后失效需重新登录

	eggAPIKey string // 第三方图鉴 API 令牌(-egg-api-key),查随机蛋可能物种用;空=不启用

	merchantMu sync.Mutex // 远行商人回源互斥:并发请求/定时任务同时缺缓存时,只放行一次回源(见 api_merchant.go)

	// 远行商人订阅邮件提醒:发件 QQ 邮箱与 SMTP 授权码(-merchant-smtp-user/-merchant-smtp-pass),
	// 空=订阅提醒不可用(前端提示,商家数据仍正常)。smtpMu 串行发信,避免 QQ 邮箱并发触发限流。
	smtpUser string
	smtpPass string
	smtpMu   sync.Mutex

	injectMu sync.Mutex
	injects  map[string][]*injectEntry // 账号 -> 已注入精灵(管理员投放,有生命周期,见 admin.go)

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
func New(st *store.Store, hub *Hub, db *gamedata.DB, eggAPIKey, smtpUser, smtpPass string) *Server {
	s := &Server{store: st, hub: hub, mux: http.NewServeMux(), db: db, opcodeNames: db.OpcodeNames(), medals: db.AllMedals(), eggAPIKey: eggAPIKey, smtpUser: smtpUser, smtpPass: smtpPass}
	s.lastPos = map[string]map[string]any{}
	s.lastWild = map[string]any{}
	s.lastHome = map[string]any{}
	s.lastFlowers = map[string]any{}
	s.lastSeen = map[string]int64{}
	s.medalIDs = map[string][]uint32{}
	s.injects = map[string][]*injectEntry{}
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
	s.mux.HandleFunc("GET /api/admin/placeholder", s.handleAdminPlaceholder)
	s.mux.HandleFunc("GET /api/position", s.handlePosition)
	s.mux.HandleFunc("GET /api/pois", s.handlePois)
	s.mux.HandleFunc("GET /api/wildpets", s.handleWildPets)
	s.mux.HandleFunc("GET /api/paint", s.handlePaint)
	s.mux.HandleFunc("DELETE /api/paint", s.handlePaintReset)
	s.mux.HandleFunc("GET /api/home", s.handleHome)
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
	s.mux.HandleFunc("GET /api/admin/egg-stats", s.handleAdminEggStats)
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
//
// 回退路径优化:原来每次都调 ListAccounts()(LEFT JOIN pets + GROUP BY + ORDER BY 全表扫),
// 前端首次加载时并行发 5-8 个 API、每个都触发一次,多账号场景下是明显的延迟来源。
// 现改为优先从 lastSeen 内存表推导最近活跃账号(5s 缓存),仅在 lastSeen 为空(无流量/刚启动)
// 时才回退到 ListAccounts() 查库。
func (s *Server) acct(r *http.Request) string {
	if a := r.URL.Query().Get("account"); a != "" {
		return a
	}
	// 快路径:5s 内复用缓存结果,避免并发请求重复推导
	s.acctCacheMu.Lock()
	if s.acctCacheVal != "" && time.Since(s.acctCacheTime) < 5*time.Second {
		v := s.acctCacheVal
		s.acctCacheMu.Unlock()
		return v
	}
	s.acctCacheMu.Unlock()

	// 从 lastSeen 找最近活跃账号(内存,无查库)
	s.onlineMu.Lock()
	var bestAcc string
	var bestTs int64
	for acc, ts := range s.lastSeen {
		if ts > bestTs {
			bestTs = ts
			bestAcc = acc
		}
	}
	s.onlineMu.Unlock()
	if bestAcc != "" {
		s.acctCacheMu.Lock()
		s.acctCacheVal = bestAcc
		s.acctCacheTime = time.Now()
		s.acctCacheMu.Unlock()
		return bestAcc
	}

	// lastSeen 为空(刚启动/纯离线回放):回退到查库
	if accs, err := s.store.ListAccounts(); err == nil && len(accs) > 0 {
		s.acctCacheMu.Lock()
		s.acctCacheVal = accs[0].Account
		s.acctCacheTime = time.Now()
		s.acctCacheMu.Unlock()
		return accs[0].Account // ListAccounts 按 updated_at 倒序,取最近
	}
	return ""
}

// onlineWindow 是「在线」判定窗口(秒):账号最近这段秒数内有流量即算在线。
// 游戏客户端保持连接时约 1.6s 一条心跳,断线/关游戏后不再有消息,30s 足够区分。
const onlineWindow = 30

// TouchAccount 记录账号最近活跃时刻(Unix 秒)。pipeline 对每条可归属消息调用;
// 离线回放时消息自带历史时间戳,账号如实显示离线。
func (s *Server) TouchAccount(acc string, ts int64) {
	s.onlineMu.Lock()
	if s.lastSeen == nil {
		s.lastSeen = map[string]int64{}
	}
	s.lastSeen[acc] = ts
	s.onlineMu.Unlock()
}

// AccountOnline 判定账号是否在线(最近 onlineWindow 秒内有流量),handleAccounts 逐账号合并。
func (s *Server) AccountOnline(acc string) bool {
	s.onlineMu.Lock()
	ts, ok := s.lastSeen[acc]
	s.onlineMu.Unlock()
	return ok && time.Now().Unix()-ts <= onlineWindow
}

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
		http.ServeFileFS(w, r, sub, path)
		return
	}
	// SPA fallback
	http.ServeFileFS(w, r, sub, "index.html")
}
