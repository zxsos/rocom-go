package server

import (
	"context"
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

// sub 是一个 SSE 订阅者的队列。
// 不用裸 channel 而用「锁 + 切片 + 通知」,是为了让 position(覆盖式状态,最多保留最新一条)
// 与事件型消息(wildpets/stars/paint,丢一条就永久丢)分而治之:
//   - position 高频(移动时 ~8条/秒)但不值钱:来新的覆盖旧的,队列里永远最多一条,
//     既保证订阅端拿到的是最新位置,又不让高频位置把队列塞满、挤掉事件消息。
//   - 事件消息照常排队;真慢到队列满(订阅端严重落后),丢最旧的保最新。
type sub struct {
	mu     sync.Mutex
	q      []streamMsg
	wait   chan struct{} // 容量 1 的「有货」信号,push 后发,pop 阻塞时收
	closed bool
}

func (s *sub) push(m streamMsg) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	if m.typ == "position" {
		// 覆盖式:队里若有 position 直接替换,保持「最多一条最新位置」。
		for i := len(s.q) - 1; i >= 0; i-- {
			if s.q[i].typ == "position" {
				s.q[i] = m
				s.mu.Unlock()
				return
			}
		}
	}
	if len(s.q) >= 128 { // 队列满(消费端严重落后):丢最旧的,保最新
		copy(s.q, s.q[1:])
		s.q[len(s.q)-1] = m
	} else {
		s.q = append(s.q, m)
	}
	s.mu.Unlock()
	select {
	case s.wait <- struct{}{}: // 有货信号;已在等待队列里则跳过(信号只用于唤醒,不计数)
	default:
	}
}

// pop 取出一条消息;队列空时阻塞等 push 的信号,ctx 取消或订阅关闭后返回 false。
func (s *sub) pop(ctx context.Context) (streamMsg, bool) {
	for {
		s.mu.Lock()
		if len(s.q) > 0 {
			m := s.q[0]
			s.q = s.q[1:]
			s.mu.Unlock()
			return m, true
		}
		closed := s.closed
		s.mu.Unlock()
		if closed {
			return streamMsg{}, false
		}
		select {
		case <-s.wait:
		case <-ctx.Done():
			return streamMsg{}, false
		}
	}
}

// tryPop 非阻塞取一条;队列空立即返回 false。供测试断言「没有广播」。
func (s *sub) tryPop() (streamMsg, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.q) == 0 {
		return streamMsg{}, false
	}
	m := s.q[0]
	s.q = s.q[1:]
	return m, true
}

func (s *sub) close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	select {
	case s.wait <- struct{}{}: // 唤醒阻塞中的 pop,让它循环后看到 closed 返回 false
	default:
	}
}

// Hub 管理 SSE 订阅者并广播实时消息。
type Hub struct {
	mu   sync.Mutex
	subs map[*sub]struct{}
}

// NewHub 创建广播中心。
func NewHub() *Hub {
	return &Hub{subs: make(map[*sub]struct{})}
}

func (h *Hub) subscribe() *sub {
	s := &sub{wait: make(chan struct{}, 1)}
	h.mu.Lock()
	h.subs[s] = struct{}{}
	h.mu.Unlock()
	return s
}

func (h *Hub) unsubscribe(s *sub) {
	h.mu.Lock()
	if _, ok := h.subs[s]; ok {
		delete(h.subs, s)
		s.close()
	}
	h.mu.Unlock()
}

// SubscribeForTest 是**仅供测试**的订阅入口:让其它包(如 pipeline)能数「某类消息
// 广播了几次」。生产路径是 SSE(见 handleStream),用不到它。
//
// 存在的理由:广播**次数**是一类只能测不能看的行为 —— 数据始终是对的,只有把整份
// 列表重发几十遍才会让前端卡住,而接口响应、golden 快照全都看不出差别。要锁住这类
// 退化就得数次数,故开放这个口子(返回取一条的函数与取消函数)。
func (h *Hub) SubscribeForTest() (pop func(ctx context.Context) (string, bool), cancel func()) {
	s := h.subscribe()
	return func(ctx context.Context) (string, bool) {
			m, ok := s.pop(ctx)
			return m.typ, ok
		}, func() {
			h.unsubscribe(s)
		}
}

// Broadcast 把一条消息广播给所有订阅者(满则丢旧保新,见 sub.push)。account 为消息所属账号,
// 传 "" 表示全局消息(所有连接都收);订阅端按 account/type 决定是否转发(见 handleStream)。
// 无订阅者(没有页面连着)时直接返回,省去 json.Marshal——实时抓包对每条消息都发 debug 广播、
// 页同步时每只宠物再发一次,常态下无人订阅,这层早退把该开销清零。
// 序列化(json.Marshal)放在锁外:移动包 8 条/秒、每只宠物/每条消息都要 marshal,若在全局锁内
// 做,所有广播会串行排队——一个慢订阅/大 payload 的 marshal 会拖住后续所有广播,进而拖住
// 消费循环(见 pipeline)。故只在锁内取订阅者快照与判空,marshal 在锁外,投递时再短暂加锁。
func (h *Hub) Broadcast(typ, account string, data any) {
	h.mu.Lock()
	subs := make([]*sub, 0, len(h.subs))
	for s := range h.subs {
		subs = append(subs, s)
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
	for _, s := range subs {
		s.push(m)
	}
}
