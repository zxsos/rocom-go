package server

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/whoisnian/rocom-capture/internal/envfile"
	"github.com/whoisnian/rocom-capture/internal/socks5"
)

// 管理面板的运行期配置:改邮箱 SMTP / 图鉴令牌 / 内置 SOCKS5 代理,不必重启服务。
//
// ## 为什么分三档,而不是「都改完重启」
//
// 重启会打断正在解密的游戏连接(见 store/merchant_src.go 文件头的说明),代价实打实。
// 而这几项配置的生效代价并不一样,一律重启纯属浪费:
//
//	T1 邮箱 SMTP、图鉴令牌   —— 完全热更:每次使用时读内存,换掉即可
//	T2 SOCKS5 代理           —— 独立重启:它是独立 goroutine + 独立 listener,
//	                            重开它不影响 Web 服务、不影响抓包
//	T3 Web 监听地址 / TLS    —— 必须重启进程,**本轮不做**(改它是「让正在处理你
//	                            请求的服务器当场消失」,失败即远端失联,须另配防变砖保护)
//
// ## 落盘策略
//
// /etc/rocom.env 是配置的**唯一**落盘位置:它由 deploy.sh 生成、systemd 通过
// EnvironmentFile= 读取,/opt/rocom/run.sh 把每项组装成启动 flag。改它 = 改启动参数,
// 故「重启后配置还在」是天然成立的。内存里的值只是「不必重启的加速」。
//
// 顺序必须是**先落盘、再改内存**:反过来若进程在改完内存后、落盘前挂掉,
// 重启后配置就丢了 —— 而管理员以为自己已经改好了。
//
// ## 敏感项
//
// SMTP 授权码、代理密码、图鉴令牌的 GET **不返回原文**,只给「是否已设置」。
// 前端把留空当作「不修改」。这与 -egg-api-key「只在服务端持有、不下发前端」
// 的既有约定一致。

const (
	// envSMTPUser / 等:配置项在 /etc/rocom.env 里的键名,与 deploy.sh 生成的模板一致。
	// 新增键要同时改 scripts/deploy.sh 的 write_env(否则重启后这项不生效)。
	envSMTPUser    = "ROCOM_SMTP_USER"
	envSMTPPass    = "ROCOM_SMTP_PASS"
	envEggAPIKey   = "ROCOM_EGG_API_KEY"
	envSocks5Addr  = "ROCOM_SOCKS5_ADDR"
	envSocks5Allow = "ROCOM_SOCKS5_ALLOW"
	envSocks5Block = "ROCOM_SOCKS5_BLOCK"
	envSocks5Max   = "ROCOM_SOCKS5_MAX_CONNS"
	envSocks5User  = "ROCOM_SOCKS5_USER"
	envSocks5Pass  = "ROCOM_SOCKS5_PASS"
)

// defaultEnvPath 是配置的落盘位置(由 scripts/deploy.sh 写入,systemd 读取)。
const defaultEnvPath = "/etc/rocom.env"

// setEnvPath 指定配置文件位置。默认 /etc/rocom.env;测试与手动部署时改成临时文件。
func (s *Server) setEnvPath(path string) { s.envPath = path }

// configWritable 报告配置文件是否可写。
// 不可写(手动跑的进程、非 root、文件不存在)时面板降级为只读 ——
// 与其让人在面板上改了却存不下,不如一开始就说明「请改 /etc/rocom.env 后重启」。
func (s *Server) configWritable() bool {
	return s.envPath != "" && envfile.Writable(s.envPath)
}

// configJSON 是 GET 的响应。敏感字段只给是否设置,不给原文。
type configJSON struct {
	Writable bool   `json:"writable"` // 配置文件可写?false 时面板应只读
	Path     string `json:"path"`     // 配置文件路径(供面板提示)

	SMTPUser    string `json:"smtpUser"`    // 发件邮箱(非敏感,可回显)
	SMTPPassSet bool   `json:"smtpPassSet"` // 授权码:只给是否已设置
	EggKeySet   bool   `json:"eggKeySet"`   // 图鉴令牌:只给是否已设置

	Socks5 socks5JSON `json:"socks5"`
	Web    webJSON    `json:"web"`
}

// webJSON 是 Web 监听地址的回显。地址本身不敏感,照实给。
type webJSON struct {
	Addr     string          `json:"addr"`              // 当前监听地址(请求时的原文)
	RealAddr string          `json:"realAddr"`          // 内核解析后的实际地址
	Pending  *webPendingJSON `json:"pending,omitempty"` // 待确认的试运行;无则省略
}

// socks5JSON 是代理配置的回显。密码只给是否已设置。
type socks5JSON struct {
	Addr     string `json:"addr"`
	Allow    string `json:"allow"`
	Block    string `json:"block"`
	MaxConns int    `json:"maxConns"`
	User     string `json:"user"`
	PassSet  bool   `json:"passSet"`
	Running  bool   `json:"running"`  // 当前是否在运行
	RealAddr string `json:"realAddr"` // 实际监听地址(端口填 0 时为内核分配的真实端口)
}

// handleAdminConfig 配置读取与保存。
//
//	GET  → configJSON(敏感项脱敏)
//	POST → 校验 → 写 /etc/rocom.env → 内存热更(T1)或重启代理(T2)
func (s *Server) handleAdminConfig(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.configGet(w)
	case http.MethodPost:
		s.configPost(w, r)
	default:
		http.Error(w, "不支持的请求方法", http.StatusMethodNotAllowed)
	}
}

func (s *Server) configGet(w http.ResponseWriter) {
	out := configJSON{
		Writable: s.configWritable(),
		Path:     s.envPath,
	}
	// 敏感项:内存(当前真正生效的)优先,env 兜底。
	//
	// 兜底不是多余的:env 是「下次重启后生效」的值。若进程没经 run.sh 启动
	// (手动跑二进制、离线回放),内存里的 flag 初值是空的而 env 里其实有配置 ——
	// 只看内存会让管理员以为「从没配过」,进而重复填写;只看 env 又会在面板
	// 改过之后(内存已热更、env 也已同步)显示不出刚改的结果。
	// 与 POST 的「留空=不修改」用同一套取值顺序,两者必须一致。
	f, _ := envfile.Load(s.envPath)
	user, pass := s.smtp.credentials()
	if user == "" && f != nil {
		user, _ = f.Get(envSMTPUser)
	}
	if pass == "" && f != nil {
		pass, _ = f.Get(envSMTPPass)
	}
	eggKey := s.eggAPIKeyGet()
	if eggKey == "" && f != nil {
		eggKey, _ = f.Get(envEggAPIKey)
	}
	out.SMTPUser = user
	out.SMTPPassSet = pass != ""
	out.EggKeySet = eggKey != ""

	out.Socks5 = s.socks5CfgFromEnv()
	if s.socks5Mgr != nil {
		if addr, ok := s.socks5Mgr.Running(); ok {
			out.Socks5.Running = true
			out.Socks5.RealAddr = addr
		}
	}
	// Web 地址以**正在监听的**为准(它永远是确定的),不读 env:
	// 与 SMTP/令牌不同,这里没有「内存空而 env 有值」的情形 —— 进程一旦起来就必定在
	// 某个地址上监听,而那个地址才是管理员真正连着的东西。env 若被人手改过但没重启,
	// 显示它只会误导(面板说 5000,实际连着 4939)。
	if s.web != nil {
		out.Web.Addr, out.Web.RealAddr, out.Web.Pending = s.web.Status()
	}
	writeJSON(w, out)
}

// socks5CfgFromEnv 从 env 文件读出代理配置(没有则用内存里正在跑的兜底)。
func (s *Server) socks5CfgFromEnv() socks5JSON {
	out := socks5JSON{}
	f, err := envfile.Load(s.envPath)
	if err != nil {
		return out
	}
	out.Addr, _ = f.Get(envSocks5Addr)
	out.Allow, _ = f.Get(envSocks5Allow)
	out.Block, _ = f.Get(envSocks5Block)
	out.User, _ = f.Get(envSocks5User)
	if p, ok := f.Get(envSocks5Pass); ok {
		out.PassSet = p != ""
	}
	if v, ok := f.Get(envSocks5Max); ok {
		if n, err := strconv.Atoi(v); err == nil {
			out.MaxConns = n
		}
	}
	return out
}

// configReq 是 POST 的入参。敏感项留空 = 不修改(前端约定)。
type configReq struct {
	SMTPUser string `json:"smtpUser"`
	SMTPPass string `json:"smtpPass"` // 留空=不改
	EggKey   string `json:"eggKey"`   // 留空=不改

	Socks5 *socks5Req `json:"socks5"` // 不传=不改代理
}

type socks5Req struct {
	Addr     string `json:"addr"` // 空=不启用
	Allow    string `json:"allow"`
	Block    string `json:"block"`
	MaxConns int    `json:"maxConns"`
	User     string `json:"user"`
	Pass     string `json:"pass"` // 留空=不改
}

func (s *Server) configPost(w http.ResponseWriter, r *http.Request) {
	if !s.configWritable() {
		http.Error(w, "配置文件不可写("+s.envPath+"),请在服务器上直接编辑后重启服务", http.StatusServiceUnavailable)
		return
	}
	var req configReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "参数解析失败", http.StatusBadRequest)
		return
	}
	f, err := envfile.Load(s.envPath)
	if err != nil {
		http.Error(w, "读取配置文件失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// —— T1:邮箱与令牌 ——
	smtpUser := strings.TrimSpace(req.SMTPUser)
	smtpPass := req.SMTPPass
	if smtpPass == "" { // 留空=不修改:沿用现有授权码
		if _, cur := s.smtp.credentials(); cur != "" {
			smtpPass = cur
		} else if v, _ := f.Get(envSMTPPass); v != "" {
			smtpPass = v
		}
	}
	if smtpUser != "" && smtpPass == "" {
		http.Error(w, "已填发件邮箱但未填授权码", http.StatusBadRequest)
		return
	}

	eggKey := req.EggKey
	if eggKey == "" { // 留空=不修改
		if cur := s.eggAPIKeyGet(); cur != "" {
			eggKey = cur
		} else if v, _ := f.Get(envEggAPIKey); v != "" {
			eggKey = v
		}
	}

	// —— T2:代理 ——
	var nextSocks *socks5.Config
	if req.Socks5 != nil {
		cfg := socks5.Config{
			Addr:     strings.TrimSpace(req.Socks5.Addr),
			Allow:    req.Socks5.Allow,
			Block:    req.Socks5.Block,
			MaxConns: req.Socks5.MaxConns,
			User:     req.Socks5.User,
			Pass:     req.Socks5.Pass,
		}
		if cfg.Pass == "" { // 留空=不修改,沿用 env 里的现有值
			if v, _ := f.Get(envSocks5Pass); v != "" {
				cfg.Pass = v
			}
		}
		// 校验必须在落盘**之前**:写了起不来的配置,服务下次重启就起不来,
		// 而那会儿管理员已经连不上面板了。
		if err := cfg.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		nextSocks = &cfg
	}

	// —— 落盘(先写文件,再改内存)——
	put := func(k, v string) {
		if err == nil {
			err = f.Set(k, v)
		}
	}
	put(envSMTPUser, smtpUser)
	put(envSMTPPass, smtpPass)
	put(envEggAPIKey, eggKey)
	if nextSocks != nil {
		put(envSocks5Addr, nextSocks.Addr)
		put(envSocks5Allow, nextSocks.Allow)
		put(envSocks5Block, nextSocks.Block)
		put(envSocks5User, nextSocks.User)
		put(envSocks5Pass, nextSocks.Pass)
		put(envSocks5Max, strconv.Itoa(nextSocks.MaxConns))
	}
	if err != nil {
		http.Error(w, "组装配置失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	// 落盘失败就**不**改内存:否则界面显示已生效、重启后却回到旧值
	if err := f.Save(); err != nil {
		http.Error(w, "写入配置文件失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// —— 内存热更 ——
	s.smtp.setCredentials(smtpUser, smtpPass)
	s.eggAPIKeySet(eggKey)

	restarted := false
	if nextSocks != nil && s.socks5Mgr != nil {
		if err := s.socks5Mgr.Start(*nextSocks); err != nil {
			// 代理没起来不影响其余配置(已落盘 + 已热更),如实回报
			writeJSON(w, map[string]any{
				"ok": true, "socks5Error": err.Error(),
			})
			return
		}
		restarted = true
	}
	log.Printf("管理面板更新配置: smtp=%v eggKey=%v socks5重启=%v", smtpUser != "", eggKey != "", restarted)
	writeJSON(w, map[string]any{"ok": true, "socks5Restarted": restarted})
}

// configEnvPath 决定配置文件位置,并返回是否可写。
//
// 非 systemd 部署(手动跑二进制、-pcap 回放)通常没有 /etc/rocom.env;
// 此时面板只读,不做任何假装能改的事。
func configEnvPath() string {
	if p := os.Getenv("ROCOM_ENV_FILE"); p != "" {
		return p
	}
	return defaultEnvPath
}

// ErrConfigNotWritable 供上层判断是否要把配置面板标为只读。
var ErrConfigNotWritable = errors.New("config: 配置文件不可写")
