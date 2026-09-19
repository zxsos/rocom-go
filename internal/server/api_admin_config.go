package server

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/zxsos/rocom-go/internal/envfile"
	"github.com/zxsos/rocom-go/internal/hy2"
)

// 管理面板的运行期配置:改邮箱 SMTP / 内嵌 hysteria2 代理,不必重启服务。
//
// ## 为什么分三档,而不是「都改完重启」
//
// 重启会打断正在解密的游戏连接(见 store/merchant_src.go 文件头的说明),代价实打实。
// 而这几项配置的生效代价并不一样,一律重启纯属浪费:
//
//	T1 邮箱 SMTP           —— 完全热更:每次使用时读内存,换掉即可
//	T2 hysteria2 代理      —— 独立重启:它是独立 goroutine + 独立 UDP socket,
//	                          重开它不影响 Web 服务、不影响抓包
//	                          (改密码/白名单/屏蔽/并发上限连重启都不用,直接换参数)
//	T3 Web 监听地址 / TLS  —— 必须重启进程,**本轮不做**(改它是「让正在处理你
//	                          请求的服务器当场消失」,失败即远端失联,须另配防变砖保护)
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
// SMTP 授权码、代理密码的 GET **不返回原文**,只给「是否已设置」。
// 前端把留空当作「不修改」。

const (
	// envSMTPUser / 等:配置项在 /etc/rocom.env 里的键名,与 deploy.sh 生成的模板一致。
	// 新增键要同时改 scripts/deploy.sh 的 write_env(否则重启后这项不生效)。
	envSMTPUser = "ROCOM_SMTP_USER"
	envSMTPPass = "ROCOM_SMTP_PASS"
	envHy2Addr  = "ROCOM_HY2_ADDR"
	envHy2Allow = "ROCOM_HY2_ALLOW"
	envHy2Block = "ROCOM_HY2_BLOCK"
	envHy2Max   = "ROCOM_HY2_MAX_CONNS"
	envHy2Pass  = "ROCOM_HY2_PASS"
)

// 已废弃的 SOCKS5 键名。SOCKS5 已被 hysteria2 取代(理由见 internal/hy2 的包注释),
// 这些键只用于**识别旧配置并提示迁移**,不再写入、也不再生效。
// 之所以要认得它们:deploy.sh 的 write_env 不覆盖已存在的 env 文件,
// 老机器升级后 ROCOM_SOCKS5_* 会一直留在那儿,不提示的话管理员永远不知道该删。
var deprecatedSocks5Keys = []string{
	"ROCOM_SOCKS5_ADDR", "ROCOM_SOCKS5_ALLOW", "ROCOM_SOCKS5_BLOCK",
	"ROCOM_SOCKS5_MAX_CONNS", "ROCOM_SOCKS5_USER", "ROCOM_SOCKS5_PASS",
}

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

	Hy2 hy2JSON `json:"hy2"`
	Web webJSON `json:"web"`
}

// webJSON 是 Web 监听地址的回显。地址本身不敏感,照实给。
type webJSON struct {
	Addr     string          `json:"addr"`              // 当前监听地址(请求时的原文)
	RealAddr string          `json:"realAddr"`          // 内核解析后的实际地址
	Pending  *webPendingJSON `json:"pending,omitempty"` // 待确认的试运行;无则省略
}

// hy2JSON 是代理配置的回显。密码只给是否已设置。
//
// 不暴露带宽(UpMbps/DownMbps):它是建连时就写进 QUIC 参数的,改它必须重建监听,
// 而同一端口「先起新后停旧」必然撞 address already in use —— 给一个点了不生效的
// 开关不如不给(详见 internal/hy2/manager.go 的 Start 注释)。
type hy2JSON struct {
	Addr     string `json:"addr"`
	Allow    string `json:"allow"`
	Block    string `json:"block"`
	MaxConns int    `json:"maxConns"`
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
	out.SMTPUser = user
	out.SMTPPassSet = pass != ""

	out.Hy2 = s.hy2CfgFromEnv()
	if s.hy2Mgr != nil {
		if addr, ok := s.hy2Mgr.Running(); ok {
			out.Hy2.Running = true
			out.Hy2.RealAddr = addr
		}
	}
	warnDeprecatedSocks5Keys(f)
	// Web 地址以**正在监听的**为准(它永远是确定的),不读 env:
	// 与 SMTP/令牌不同,这里没有「内存空而 env 有值」的情形 —— 进程一旦起来就必定在
	// 某个地址上监听,而那个地址才是管理员真正连着的东西。env 若被人手改过但没重启,
	// 显示它只会误导(面板说 5000,实际连着 4939)。
	if s.web != nil {
		out.Web.Addr, out.Web.RealAddr, out.Web.Pending = s.web.Status()
	}
	writeJSON(w, out)
}

// hy2CfgFromEnv 从 env 文件读出代理配置,供面板回显(是否在跑另由 hy2Mgr.Running 补)。
func (s *Server) hy2CfgFromEnv() hy2JSON {
	out := hy2JSON{}
	f, err := envfile.Load(s.envPath)
	if err != nil {
		return out
	}
	out.Addr, _ = f.Get(envHy2Addr)
	out.Allow, _ = f.Get(envHy2Allow)
	out.Block, _ = f.Get(envHy2Block)
	if p, ok := f.Get(envHy2Pass); ok {
		out.PassSet = p != ""
	}
	if v, ok := f.Get(envHy2Max); ok {
		if n, err := strconv.Atoi(v); err == nil {
			out.MaxConns = n
		}
	}
	return out
}

// warnDeprecatedSocks5Keys 在读到旧 SOCKS5 配置时提示迁移。
//
// 只在**有人看面板**时提示,而不是每次请求都刷 —— 管理员唯一能收到这个信息的场合
// 就是打开配置页;写进启动日志则会被淹没在抓包日志里。
func warnDeprecatedSocks5Keys(f *envfile.File) {
	if f == nil {
		return
	}
	var found []string
	for _, k := range deprecatedSocks5Keys {
		if v, ok := f.Get(k); ok && v != "" {
			found = append(found, k)
		}
	}
	if len(found) == 0 {
		return
	}
	log.Printf("警告: 配置文件里仍有已废弃的 SOCKS5 项 %v —— 内置 SOCKS5 已被 hysteria2 取代,这些项不再生效,请改成 ROCOM_HY2_ADDR / ROCOM_HY2_PASS / ROCOM_HY2_ALLOW 后删除旧键", found)
}

// configReq 是 POST 的入参。敏感项留空 = 不修改(前端约定)。
type configReq struct {
	SMTPUser string `json:"smtpUser"`
	SMTPPass string `json:"smtpPass"` // 留空=不改

	Hy2 *hy2Req `json:"hy2"` // 不传=不改代理
}

type hy2Req struct {
	Addr     string `json:"addr"` // 空=不启用
	Allow    string `json:"allow"`
	Block    string `json:"block"`
	MaxConns int    `json:"maxConns"`
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

	// —— T1:邮箱 ——
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

	// —— T2:代理 ——
	var nextHy2 *hy2.Config
	if req.Hy2 != nil {
		cfg := hy2.Config{
			Addr:     strings.TrimSpace(req.Hy2.Addr),
			Allow:    req.Hy2.Allow,
			Block:    req.Hy2.Block,
			MaxConns: req.Hy2.MaxConns,
			Password: req.Hy2.Pass,
		}
		if cfg.Password == "" { // 留空=不修改,沿用 env 里的现有值
			if v, _ := f.Get(envHy2Pass); v != "" {
				cfg.Password = v
			}
		}
		// 校验必须在落盘**之前**:写了起不来的配置,服务下次重启就起不来,
		// 而那会儿管理员已经连不上面板了。
		if err := cfg.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		nextHy2 = &cfg
	}

	// —— 落盘(先写文件,再改内存)——
	put := func(k, v string) {
		if err == nil {
			err = f.Set(k, v)
		}
	}
	put(envSMTPUser, smtpUser)
	put(envSMTPPass, smtpPass)
	if nextHy2 != nil {
		put(envHy2Addr, nextHy2.Addr)
		put(envHy2Allow, nextHy2.Allow)
		put(envHy2Block, nextHy2.Block)
		put(envHy2Pass, nextHy2.Password)
		put(envHy2Max, strconv.Itoa(nextHy2.MaxConns))
		// 顺手清掉废弃的 SOCKS5 键:面板这次保存已经把代理整体换成 hy2,
		// 留着旧键只会让下次启动继续打废弃告警。
		for _, k := range deprecatedSocks5Keys {
			if err == nil {
				err = f.Unset(k)
			}
		}
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

	restarted := false
	if nextHy2 != nil && s.hy2Mgr != nil {
		if err := s.hy2Mgr.Start(*nextHy2); err != nil {
			// 代理没起来不影响其余配置(已落盘 + 已热更),如实回报
			writeJSON(w, map[string]any{
				"ok": true, "hy2Error": err.Error(),
			})
			return
		}
		restarted = true
	}
	log.Printf("管理面板更新配置: smtp=%v hy2重启=%v", smtpUser != "", restarted)
	writeJSON(w, map[string]any{"ok": true, "hy2Restarted": restarted})
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
