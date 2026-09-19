package server

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/zxsos/rocom-go/internal/store"
)

// 订阅设备管理(管理员):一人一枚令牌,各带一条短链。
//
// 为什么要有它:早先只有一枚由密码派生的令牌,「想踢掉某个人」和「换密码」是同一件事,
// 而换密码会让所有人的订阅一起失效。拆成每设备一枚之后,吊销就只是这一行的事 ——
// 换密码退居为兜底手段(见 docs/deploy.md 的三层吊销)。
//
// 列表里的近况几列(末次拉取 / UA / 实发格式 / 命中数)是排障用的:面板上要能一眼
// 分开「根本没来拉」与「拉到了但规则没生效」,而这两种故障的表现一模一样。
type subDeviceJSON struct {
	Token      string `json:"token"`
	Label      string `json:"label"`
	Code       string `json:"code"`    // 短码;空=没生成过短链
	SubURL     string `json:"subUrl"`  // 可直接发给朋友的地址(短链)
	CreatedAt  int64  `json:"createdAt"`
	ExpiresAt  int64  `json:"expiresAt"`  // 0=长期
	RevokedAt  int64  `json:"revokedAt"`  // >0=已吊销
	LastSeenAt int64  `json:"lastSeenAt"` // 0=从未拉取
	LastUA     string `json:"lastUa"`
	LastFormat string `json:"lastFormat"`
	Hits       int64  `json:"hits"`
	Usable     bool   `json:"usable"`
}

// handleAdminSubDevices 订阅设备的列表 / 新建 / 吊销。
func (s *Server) handleAdminSubDevices(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.listSubDevices(w, r)
	case http.MethodPost:
		s.createSubDevice(w, r)
	case http.MethodDelete:
		s.revokeSubDevice(w, r)
	default:
		http.Error(w, "不支持的方法", http.StatusMethodNotAllowed)
	}
}

func (s *Server) listSubDevices(w http.ResponseWriter, r *http.Request) {
	devs, err := s.store.ListSubDevices()
	if err != nil {
		http.Error(w, "读取订阅设备失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	out := make([]subDeviceJSON, 0, len(devs))
	for _, d := range devs {
		out = append(out, deviceJSON(r, d))
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, map[string]any{"devices": out})
}

func (s *Server) createSubDevice(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Label    string `json:"label"`
		TTLHours int    `json:"ttlHours"` // <=0 = 长期有效
	}
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "请求体解析失败: "+err.Error(), http.StatusBadRequest)
			return
		}
	}
	var ttl int64
	if req.TTLHours > 0 {
		ttl = int64(req.TTLHours) * 3600
	}
	d, err := s.store.CreateSubDevice(req.Label, ttl)
	if err != nil {
		http.Error(w, "创建订阅设备失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	// 回完整对象:前端拿到就能直接画二维码,不必再拉一次列表。
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, map[string]any{"device": deviceJSON(r, *d)})
}

func (s *Server) revokeSubDevice(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "缺少 token", http.StatusBadRequest)
		return
	}
	if err := s.store.RevokeSubDevice(token); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	log.Printf("订阅设备已吊销: 来源=%s", r.RemoteAddr)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, map[string]any{"ok": true})
}

// handleAdminSubDeviceRename 改备注名。
func (s *Server) handleAdminSubDeviceRename(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	var req struct {
		Token string `json:"token"`
		Label string `json:"label"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "请求体解析失败: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Token == "" {
		http.Error(w, "缺少 token", http.StatusBadRequest)
		return
	}
	if err := s.store.RenameSubDevice(req.Token, req.Label); err != nil {
		http.Error(w, "改名失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, map[string]any{"ok": true})
}

// deviceJSON 转出给前端。SubURL 用**请求的 Host** 拼,不落库:
// 同一个面板今天用内网地址打开、明天用公网域名打开,发出去的链接应该跟着变 ——
// 存死一个值只会让某一天发出去的链接指向打不通的地址。
func deviceJSON(r *http.Request, d store.SubDevice) subDeviceJSON {
	return subDeviceJSON{
		Token:      d.Token,
		Label:      d.Label,
		Code:       d.Code,
		SubURL:     landingURL(r, d.Code),
		CreatedAt:  d.CreatedAt,
		ExpiresAt:  d.ExpiresAt,
		RevokedAt:  d.RevokedAt,
		LastSeenAt: d.LastSeenAt,
		LastUA:     d.LastUA,
		LastFormat: d.LastFormat,
		Hits:       d.Hits,
		Usable:     d.Usable(),
	}
}
