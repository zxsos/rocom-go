package server

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// 账号 PIN 保护:每个账号可设 4-6 位数字 PIN,切到该账号时前端弹框输入。
// 管理员代设,用户可自助改/删(需旧 PIN 或已解锁会话);删账号需 PIN 或管理员权限。
// 限频:同一 IP+账号每分钟最多 10 次校验,超限 429。

const (
	pinIter    = 100_000 // PIN 用较低迭代(PIN 短+限频,场景非对抗性)
	pinSaltN   = 16
	pinMinLen  = 4
	pinMaxLen  = 6
	pinRateWin = 60 * time.Second
	pinRateMax = 10
)

// hashPin 用 PBKDF2-SHA256 派生 PIN 哈希,输出 "pbkdf2$iter$saltB64$hashB64"。
func hashPin(pin string) (string, error) {
	salt := make([]byte, pinSaltN)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	sum, err := pbkdf2.Key(sha256.New, pin, salt, pinIter, 32)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("pbkdf2$%d$%s$%s", pinIter,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(sum)), nil
}

// verifyPin 校验 PIN 与存储哈希是否一致。
func verifyPin(pin, stored string) bool {
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
	got, err := pbkdf2.Key(sha256.New, pin, salt, iter, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

// 限频:pinRateMu 保护 pinRate(map[ip+account]→时间戳滑窗)。
var pinRateMu sync.Mutex
var pinRate = map[string][]time.Time{}

// pinRateAllow 滑窗限频:同一 ip+account 在 pinRateWin 内不超过 pinRateMax 次。
func pinRateAllow(key string) bool {
	pinRateMu.Lock()
	defer pinRateMu.Unlock()
	now := time.Now()
	cutoff := now.Add(-pinRateWin)
	times := pinRate[key]
	out := times[:0]
	for _, t := range times {
		if t.After(cutoff) {
			out = append(out, t)
		}
	}
	if len(out) >= pinRateMax {
		pinRate[key] = out
		return false
	}
	out = append(out, now)
	pinRate[key] = out
	return true
}

// clientIP 提取请求来源 IP(优先 X-Forwarded-For,兜底 RemoteAddr)。
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	// RemoteAddr 形如 "1.2.3.4:5678",去端口
	if i := strings.LastIndex(r.RemoteAddr, ":"); i > 0 {
		return r.RemoteAddr[:i]
	}
	return r.RemoteAddr
}

// validPin 检查 PIN 格式:4-6 位纯数字。
func validPin(pin string) bool {
	if len(pin) < pinMinLen || len(pin) > pinMaxLen {
		return false
	}
	for _, c := range pin {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// handleAccountVerify 校验账号 PIN。无 PIN 时返回 hasPin:false(前端不弹框)。
func (s *Server) handleAccountVerify(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Account string `json:"account"`
		Pin     string `json:"pin"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", 400)
		return
	}
	req.Account = strings.TrimSpace(req.Account)
	if req.Account == "" {
		http.Error(w, "account required", 400)
		return
	}
	hash := s.store.AccountPinHash(req.Account)
	if hash == "" {
		writeJSON(w, map[string]any{"ok": true, "hasPin": false})
		return
	}
	rateKey := clientIP(r) + "|" + req.Account
	if !pinRateAllow(rateKey) {
		http.Error(w, "too many attempts", 429)
		return
	}
	if !verifyPin(req.Pin, hash) {
		http.Error(w, "wrong pin", 401)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "hasPin": true})
}

// handleAccountPin 设置/修改/清除账号 PIN。
// 鉴权:管理员令牌可直接操作;否则需提供旧 PIN 且与存储一致(首次设 PIN 仅管理员可做)。
func (s *Server) handleAccountPin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Account string `json:"account"`
		OldPin  string `json:"oldPin"`
		NewPin  string `json:"newPin"` // 空=清除 PIN
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", 400)
		return
	}
	req.Account = strings.TrimSpace(req.Account)
	if req.Account == "" {
		http.Error(w, "account required", 400)
		return
	}
	isAdmin := s.authed(r)
	existing := s.store.AccountPinHash(req.Account)
	if !isAdmin {
		// 非管理员:必须输对旧 PIN(已设 PIN 时)才能操作
		if existing != "" && !verifyPin(req.OldPin, existing) {
			http.Error(w, "wrong old pin", 401)
			return
		}
	}
	// 清除 PIN
	if req.NewPin == "" {
		if err := s.store.SetAccountPin(req.Account, ""); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
		return
	}
	// 设置/修改 PIN
	if !validPin(req.NewPin) {
		http.Error(w, "pin must be 4-6 digits", 400)
		return
	}
	hash, err := hashPin(req.NewPin)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if err := s.store.SetAccountPin(req.Account, hash); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// handleAccountDelete 删除账号及其全部数据。
// 鉴权:管理员令牌可直接删;否则需输对该账号 PIN(已设 PIN 时)。无 PIN 账号仅管理员可删。
func (s *Server) handleAccountDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Account string `json:"account"`
		Pin     string `json:"pin"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", 400)
		return
	}
	req.Account = strings.TrimSpace(req.Account)
	if req.Account == "" {
		http.Error(w, "account required", 400)
		return
	}
	isAdmin := s.authed(r)
	if !isAdmin {
		hash := s.store.AccountPinHash(req.Account)
		if hash == "" {
			http.Error(w, "no pin set; admin required to delete", 403)
			return
		}
		rateKey := clientIP(r) + "|" + req.Account
		if !pinRateAllow(rateKey) {
			http.Error(w, "too many attempts", 429)
			return
		}
		if !verifyPin(req.Pin, hash) {
			http.Error(w, "wrong pin", 401)
			return
		}
	}
	// 清理内存态
	s.posMu.Lock()
	delete(s.lastPos, req.Account)
	delete(s.lastWild, req.Account)
	delete(s.lastHome, req.Account)
	s.posMu.Unlock()
	s.onlineMu.Lock()
	delete(s.lastSeen, req.Account)
	s.onlineMu.Unlock()
	s.injectMu.Lock()
	delete(s.injects, req.Account)
	s.injectMu.Unlock()
	// 删库
	if err := s.store.DeleteAccount(req.Account); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	// 广播账号列表刷新
	s.hub.Broadcast("accounts", "", map[string]any{"deleted": req.Account})
	writeJSON(w, map[string]any{"ok": true})
}
