package server

import (
	"encoding/hex"
	"encoding/json"
	"net/http"

	"github.com/whoisnian/rocom-capture/internal/pbdesc"
)

// handleDebugParse 把调试页某条消息的原始数据解析成可读树:
// 按 opcode 精确解码(内嵌游戏描述符),失败自动退回通用 wire 级。仅调试用,无副作用。
func (s *Server) handleDebugParse(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Opcode int    `json:"opcode"` // 十进制 opcode(0x824 -> 2084),与调试流 opcode 字段一致
		Dir    string `json:"dir"`    // "c2s"/"s2c",决定起始偏移偏好
		Hex    string `json:"hex"`    // AppBody 十六进制(调试流 hex 字段)
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, map[string]any{"error": "参数解析失败"})
		return
	}
	body, err := hex.DecodeString(req.Hex)
	if err != nil {
		writeJSON(w, map[string]any{"error": "hex 解码失败"})
		return
	}
	if len(body) > 1<<20 { // 防滥用,单条最大 1MB
		writeJSON(w, map[string]any{"error": "数据过大"})
		return
	}
	text := pbdesc.Render(uint16(req.Opcode), req.Dir, body)
	writeJSON(w, map[string]any{
		"opcode": req.Opcode,
		"dir":    req.Dir,
		"name":   s.OpcodeName(uint16(req.Opcode)),
		"text":   text,
	})
}
