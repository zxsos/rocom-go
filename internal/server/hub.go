package server

import (
	"encoding/json"
	"sync"
)

// envelope 是推送给前端的统一消息封装。
type envelope struct {
	Type    string `json:"type"`              // pet | event | position | stars | starzones | wildpets | debug
	Account string `json:"account,omitempty"` // 所属账号("" = 全局/调试,前端不按账号过滤)
	Data    any    `json:"data"`
}

// streamMsg 是投递给订阅者的一条消息:预先序列化的 JSON,外带 type/account 供订阅端过滤,
// 免得每个连接都重新反序列化。
type streamMsg struct {
	typ     string
	account string
	data    []byte
}

// Hub 管理 SSE 订阅者并广播实时消息。
type Hub struct {
	mu   sync.Mutex
	subs map[chan streamMsg]struct{}
}

// NewHub 创建广播中心。
func NewHub() *Hub {
	return &Hub{subs: make(map[chan streamMsg]struct{})}
}

func (h *Hub) subscribe() chan streamMsg {
	ch := make(chan streamMsg, 64)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *Hub) unsubscribe(ch chan streamMsg) {
	h.mu.Lock()
	if _, ok := h.subs[ch]; ok {
		delete(h.subs, ch)
		close(ch)
	}
	h.mu.Unlock()
}

// Broadcast 把一条消息广播给所有订阅者(满则丢弃，避免阻塞)。account 为消息所属账号,
// 传 "" 表示全局消息(所有连接都收);订阅端按 account/type 决定是否转发(见 handleStream)。
// 无订阅者(没有页面连着)时直接返回,省去 json.Marshal——实时抓包对每条消息都发 debug 广播、
// 页同步时每只宠物再发一次,常态下无人订阅,这层早退把该开销清零。
// 序列化(json.Marshal)放在锁外:移动包 8 条/秒、每只宠物/每条消息都要 marshal,若在全局锁内
// 做,所有广播会串行排队——一个慢订阅/大 payload 的 marshal 会拖住后续所有广播,进而拖住
// 消费循环(见 pipeline)。故只在锁内取订阅者快照与判空,marshal 在锁外,投递时再短暂加锁。
func (h *Hub) Broadcast(typ, account string, data any) {
	h.mu.Lock()
	subs := make([]chan streamMsg, 0, len(h.subs))
	for ch := range h.subs {
		subs = append(subs, ch)
	}
	h.mu.Unlock()
	if len(subs) == 0 {
		return
	}
	msg, err := json.Marshal(envelope{Type: typ, Account: account, Data: data})
	if err != nil {
		return
	}
	m := streamMsg{typ: typ, account: account, data: msg}
	for _, ch := range subs {
		select {
		case ch <- m:
		default:
		}
	}
}
