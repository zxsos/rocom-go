package server

import "net/http"

// 草系徽章试炼接口(见 internal/trial 与 docs/pcap-20260831-grass-trial.md)。
//
// 与 wildpets / home / flowers 同一路数:管线把试炼状态推给 server 缓存,页面加载时
// 经本接口即时回显,之后由 SSE 的 trial 消息实时覆盖。

// SetLastTrial 缓存某账号最近一次试炼状态(由消费管线在广播 trial 时调用)。
func (s *Server) SetLastTrial(account string, payload *TrialPayload) {
	if account == "" {
		return
	}
	s.snap.setTrial(account, payload)
}

// handleTrial 返回当前账号最近一次草系试炼状态;无记录返回 null。
func (s *Server) handleTrial(w http.ResponseWriter, r *http.Request) {
	v := s.snap.getTrial(s.acct(r))
	writeJSON(w, v)
}
