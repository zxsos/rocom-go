package server

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// 管理员认证:隐式面板(前端导航不显示,需手动输入 #/admin)。
// 首次进入引导设置密码(存 PBKDF2-SHA256 哈希),之后凭密码登录,成功签发内存令牌(服务重启即失效)。

const (
	adminIter    = 600_000 // PBKDF2 迭代次数
	adminSaltN   = 16      // 盐字节数
	adminTokenN  = 32      // 令牌字节数
	adminMinPass = 4       // 密码最短长度
)

// hashAdminPassword 用 PBKDF2-SHA256 派生密码哈希,输出 "pbkdf2$iter$saltB64$hashB64"。
func hashAdminPassword(pw string) (string, error) {
	salt := make([]byte, adminSaltN)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	sum, err := pbkdf2.Key(sha256.New, pw, salt, adminIter, 32)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("pbkdf2$%d$%s$%s", adminIter,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(sum)), nil
}

// verifyAdminPassword 校验密码与存储哈希是否一致。
func verifyAdminPassword(pw, stored string) bool {
	parts := strings.Split(stored, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2" {
		return false
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter <= 0 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, pw, salt, iter, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

// authed 校验请求是否携带当前有效管理员令牌。
func (s *Server) authed(r *http.Request) bool {
	s.adminMu.Lock()
	defer s.adminMu.Unlock()
	return s.adminToken != "" && subtle.ConstantTimeCompare(
		[]byte(s.adminToken), []byte(r.Header.Get("X-Admin-Token"))) == 1
}

func newAdminToken() string {
	b := make([]byte, adminTokenN)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// handleAdminStatus 返回密码是否已配置、当前是否已登录。
func (s *Server) handleAdminStatus(w http.ResponseWriter, r *http.Request) {
	configured, err := s.store.AdminConfigured()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, map[string]any{"configured": configured, "authed": s.authed(r)})
}

// handleAdminSetup 首次设置管理员密码(已配置则拒绝)。
func (s *Server) handleAdminSetup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", 400)
		return
	}
	configured, err := s.store.AdminConfigured()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if configured {
		http.Error(w, "already configured", 409)
		return
	}
	if len(req.Password) < adminMinPass {
		http.Error(w, "password too short", 400)
		return
	}
	hash, err := hashAdminPassword(req.Password)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if err := s.store.SetAdminPassword(hash); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	token := newAdminToken()
	s.adminMu.Lock()
	s.adminToken = token
	s.adminMu.Unlock()
	writeJSON(w, map[string]any{"token": token})
}

// handleAdminLogin 密码登录,签发内存令牌。
func (s *Server) handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", 400)
		return
	}
	stored := s.store.AdminPassHash()
	if stored == "" || !verifyAdminPassword(req.Password, stored) {
		http.Error(w, "wrong password", 401)
		return
	}
	token := newAdminToken()
	s.adminMu.Lock()
	s.adminToken = token
	s.adminMu.Unlock()
	writeJSON(w, map[string]any{"token": token})
}

// handleAdminLogout 注销管理员会话。
func (s *Server) handleAdminLogout(w http.ResponseWriter, r *http.Request) {
	if !s.authed(r) {
		http.Error(w, "unauthorized", 401)
		return
	}
	s.adminMu.Lock()
	s.adminToken = ""
	s.adminMu.Unlock()
	writeJSON(w, map[string]any{"ok": true})
}

// requireAdmin 校验管理员会话,未登录则回 401 并返回 false。
func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	if !s.authed(r) {
		http.Error(w, "unauthorized", 401)
		return false
	}
	return true
}
