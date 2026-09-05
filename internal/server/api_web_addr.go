package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/whoisnian/rocom-capture/internal/envfile"
)

// 管理面板改 Web 监听地址(ROCOM_ADDR)。
//
// ## 为什么是「试运行 → 确认」,而不是保存即生效
//
// 其它配置改错了无非是功能不可用,面板还在,随时能改回来。Web 监听地址不同:
// 它正是管理员用来改它的那条连接的另一端。若保存即生效,下面每一种情况都会
// 让管理员当场失去面板,而服务通常跑在网关上、人不在机器旁:
//
//	- 新端口已被别的进程占用(bind 失败)
//	- 新端口在容器/防火墙外没有映射或放通(bind 成功,但连不上)
//	- 监听 IP 填成 127.0.0.1(只有本机连得上)
//
// 故拆成三步,配置文件只在**确认**时写:
//
//	POST /api/admin/web-addr          试运行:新地址开始监听,新旧并存,不落盘
//	POST /api/admin/web-addr/confirm  确认:先落盘,再停旧监听
//	POST /api/admin/web-addr/revert   回滚:关掉新监听,配置文件不动
//
// 试运行有 90 秒时限(webTrialTimeout),无人确认自动回滚。于是「改错」这件事
// 的代价恒为零:最坏是试运行到期,一切回到原样,连配置文件都没被碰过。
// 状态机本身在 web_listen.go,这里只做鉴权、入参与落盘。
//
// 前端的配合方式见 web/src/pages/admin/WebAddrCard.jsx:确认这一步由「管理员
// 在新地址上打开了面板」这一事实自动触发 —— 打不开就不会有确认,也就不该落盘。

// envWebAddr 是 Web 监听地址在 /etc/rocom.env 里的键名,与 deploy.sh 的模板一致
// (run.sh 把它组装成 -addr)。
const envWebAddr = "ROCOM_ADDR"

// webPendingJSON 是待确认的试运行信息,GET /api/admin/config 用它在刷新后把倒计时接上。
//
// 刻意**不含**交接码:它是一次性凭据,若进了 GET 响应,每刷一次页面就能再取一遍,
// 而它本该只在试运行那一刻发放一次。前端因此只在本地留着它(见 WebAddrCard),
// 刷新后跳转链接消失 —— 那也意味着管理员还没去过新地址,重新试运行一次即可。
type webPendingJSON struct {
	Addr     string `json:"addr"`     // 试运行的地址原文
	RealAddr string `json:"realAddr"` // 内核解析后的实际地址(端口填 0 时为真实端口)
	Port     int    `json:"port"`     // 实际端口号:前端据此拼出跳转 URL
	Deadline int64  `json:"deadline"` // Unix 秒;到点自动回滚
}

func (s *Server) handleAdminWebAddr(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "不支持的请求方法", http.StatusMethodNotAllowed)
		return
	}
	if !s.configWritable() {
		http.Error(w, "配置文件不可写("+s.envPath+"),无法保存新的监听地址", http.StatusServiceUnavailable)
		return
	}
	if s.web == nil {
		http.Error(w, "Web 服务未被托管,无法在运行期改地址", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		Addr string `json:"addr"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "参数解析失败", http.StatusBadRequest)
		return
	}
	real, port, deadline, handoff, err := s.web.Trial(req.Addr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]any{
		"ok":       true,
		"addr":     req.Addr,
		"realAddr": real,
		"port":     port,
		"deadline": deadline,
		"handoff":  handoff,
	})
}

func (s *Server) handleAdminWebAddrConfirm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "不支持的请求方法", http.StatusMethodNotAllowed)
		return
	}
	// 入参可有可无(空体也合法),故解析错误一律忽略:按「无交接码」处理。
	var req struct {
		Handoff string `json:"handoff"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	// 两条并存的路径:
	//   - 带交接码:来自**新地址**那一次访问,它是「新地址可达」的证据,无需令牌
	//     (新源上还没有管理员会话,令牌按源隔离 —— 见 ApplyPendingWithHandoff)
	//   - 不带:管理员仍在旧地址上操作,必须已登录
	if req.Handoff == "" && !s.requireAdmin(w, r) {
		return
	}
	if s.web == nil {
		http.Error(w, "Web 服务未被托管,无法在运行期改地址", http.StatusServiceUnavailable)
		return
	}

	save := func(addr string) error { return s.saveEnvAddr(addr) }
	var addr, real string
	var err error
	if req.Handoff != "" {
		addr, real, err = s.web.ApplyPendingWithHandoff(req.Handoff, save)
	} else {
		addr, real, err = s.web.ApplyPending(save)
	}
	switch {
	case errors.Is(err, errNoPending):
		http.Error(w, "没有待确认的新地址", http.StatusConflict)
		return
	case errors.Is(err, errBadHandoff):
		http.Error(w, "交接码无效或已被使用", http.StatusForbidden)
		return
	case err != nil:
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	out := map[string]any{"ok": true, "addr": addr, "realAddr": real}
	if req.Handoff != "" {
		// 顺带把会话接过去:否则管理员刚确认完就被踢回登录页,而这一步正是
		// 「在新地址上能用」的证据收集环节,不该以登出收场。
		s.adminMu.Lock()
		out["token"] = s.adminToken
		s.adminMu.Unlock()
	}
	writeJSON(w, out)
}

func (s *Server) handleAdminWebAddrRevert(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "不支持的请求方法", http.StatusMethodNotAllowed)
		return
	}
	if s.web == nil {
		http.Error(w, "Web 服务未被托管,无法在运行期改地址", http.StatusServiceUnavailable)
		return
	}
	if err := s.web.Revert(); errors.Is(err, errNoPending) {
		http.Error(w, "没有待确认的新地址", http.StatusConflict)
		return
	} else if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// saveEnvAddr 把监听地址写进配置文件(确认时调用)。
//
// 只改这一个键:envfile 按行处理,未知键与注释原样保留(见 envfile 的包注释),
// 故不会动到 ROCOM_IFACE 之类不归面板管的东西。
func (s *Server) saveEnvAddr(addr string) error {
	f, err := envfile.Load(s.envPath)
	if err != nil {
		return errors.New("读取配置文件失败: " + err.Error())
	}
	if err := f.Set(envWebAddr, addr); err != nil {
		return err
	}
	if err := f.Save(); err != nil {
		return errors.New("写入配置文件失败: " + err.Error())
	}
	return nil
}
