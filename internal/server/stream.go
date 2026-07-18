package server

import (
	"fmt"
	"net/http"
)

// handleStream 是 SSE 端点:把 Hub 广播的实时消息转发给前端。
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "stream unsupported", 500)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// 该连接只接收当前账号(?account=,缺省回退最近活跃账号)的消息;account 为空的全局消息始终放行。
	account := s.acct(r)
	// 高频的 debug(逐条 opcode)流量默认不推送,仅调试页显式 ?debug=1 时才发,避免其它页面白拉。
	wantDebug := r.URL.Query().Get("debug") == "1"

	ch := s.hub.subscribe()
	defer s.hub.unsubscribe(ch)
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				return
			}
			if msg.account != "" && msg.account != account {
				continue // 非当前账号
			}
			if msg.typ == "debug" && !wantDebug {
				continue // 未订阅调试流
			}
			fmt.Fprintf(w, "data: %s\n\n", msg.data)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}
