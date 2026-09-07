package server

import (
	"net/http"
)

// 闪耀大赛(隐藏模块 #/shanyao)接口。
//
// 与 trial / wildpets / flowers 同一路数:管线把战局状态推给 server 缓存,页面加载时
// 经本接口即时回显,之后由 SSE 的 shanyao 消息实时覆盖。
//
// 战局的解析与状态机在 internal/shanyao(0x1316 进战 / 0x131a 回合 / 0x1324 演出 /
// 0x132c 结算);本文件只管缓存与出口。

// SetLastShanyao 缓存某账号最近一次战局状态(由消费管线在广播 shanyao 时调用)。
func (s *Server) SetLastShanyao(account string, payload *ShanyaoPayload) {
	if account == "" {
		return
	}
	s.snap.setShanyao(account, payload)
}

// handleShanyao 返回当前账号最近一次战局状态;无记录返回 null。
func (s *Server) handleShanyao(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.snap.getShanyao(s.acct(r)))
}
