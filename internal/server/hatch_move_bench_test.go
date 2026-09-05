package server

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/whoisnian/rocom-capture/internal/gamedata"
	"github.com/whoisnian/rocom-capture/internal/store"
)

// 本文件回答一个运维问题:「移动实时检测会不会拖慢抓包?」
//
// 移动包峰值约 8 条/秒,observeHatchMove 对**每一条**都调 SetHatchMoving,而它跑在
// 抓包管线的消费循环上。若每次有毫秒级开销、或最坏情况会阻塞(等锁、等 SSE 客户端),
// 就会抬高抓包延迟甚至丢包 —— 那不是「页面多刷一次」的小事。故把开销测出来。
//
// ⚠️ 关键:SetHatchMoving 只在**状态翻转**时才 Broadcast。稳态(一直在跑)下
// changed=false,压根不走广播 —— 这点决定了「常态开销」与「最坏开销」差一个量级,
// 必须分开测。第一版把「有订阅者」当成重路径,其实它稳态下也不广播,与无订阅者
// 测的是同一条路(数值几乎一样),是无效区分。故按「是否翻转」而非「有无订阅者」分。

// newBenchServer 是 newTestServer 的基准版。
//
// 不复用 newTestServer 只因它收 *testing.T 且要用 t.TempDir()/t.Cleanup();
// 逻辑保持一致 —— 令牌同样留空,同样在结束时关库。
func newBenchServer(b *testing.B) *Server {
	b.Helper()
	db, err := gamedata.Load()
	if err != nil {
		b.Fatalf("加载名称库: %v", err)
	}
	st, err := store.New(filepath.Join(b.TempDir(), "bench.db"), db)
	if err != nil {
		b.Fatalf("打开数据库: %v", err)
	}
	b.Cleanup(func() { _ = st.Close() })
	return New(st, NewHub(), db, "", "", "", nil)
}

// startConsumer 起一个 SSE 消费者,免得队列堆到 128 触发「丢旧保新」(那会改变被测路径)。
func startConsumer(b *testing.B, s *Server) {
	b.Helper()
	pop, cancel := s.hub.SubscribeForTest()
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if _, ok := pop(ctx); !ok {
				return
			}
		}
	}()
	b.Cleanup(func() { stop(); <-done; cancel() })
}

// BenchmarkSetHatchMoving_Steady 常态:状态不翻转(玩家持续在跑)。
//
// 这是移动包 8 条/秒里的绝大多数 —— 每次只做「加锁 + 写时间戳 + 判翻转」,不广播。
func BenchmarkSetHatchMoving_Steady(b *testing.B) {
	s := newBenchServer(b)
	startConsumer(b, s)
	now := time.Now()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.SetHatchMoving("UID:bench", now)
	}
}

// BenchmarkSetHatchMoving_FlappingNoSubscriber 翻转,但没人开着页面。
//
// 守住 Broadcast 的**早退优化**:无订阅者时直接返回、不做 json.Marshal。
// 去掉该优化后本项会从约 200ns 涨到约 770ns(实测),是这条优化的直接证据。
func BenchmarkSetHatchMoving_FlappingNoSubscriber(b *testing.B) {
	s := newBenchServer(b)
	now := time.Now()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i%2 == 0 {
			s.SetHatchMoving("UID:bench", now)
		} else {
			s.SetHatchMoving("UID:bench", time.Time{})
		}
	}
}

// BenchmarkSetHatchMoving_FlappingWithSubscriber 翻转且有人开着页面(完整广播路径)。
//
// 玩家推住摇杆在动/停边界抖动时就是这个形状。走完整路径:取订阅者快照 → marshal → push。
func BenchmarkSetHatchMoving_FlappingWithSubscriber(b *testing.B) {
	s := newBenchServer(b)
	startConsumer(b, s)
	now := time.Now()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i%2 == 0 {
			s.SetHatchMoving("UID:bench", now)
		} else {
			s.SetHatchMoving("UID:bench", time.Time{})
		}
	}
}

// BenchmarkSetHatchMoving_StuckSubscriber 下游完全不消费(页面卡死/网络断了但连接还在)。
//
// 这才是「会不会提高延迟」的真正考点:若 push 是阻塞的,队列堆满后每次调用都会等,
// 抓包循环就被一个卡死的页面拖住 —— 那会丢包。sub.push 用 select+default 且队列
// 上限 128(满则丢最旧保最新),故**不该**变慢。本基准守住这一点:
// 它的耗时必须与有消费者时(FlappingWithSubscriber)同量级。
func BenchmarkSetHatchMoving_StuckSubscriber(b *testing.B) {
	s := newBenchServer(b)
	_, cancel := s.hub.SubscribeForTest() // 订阅但**不消费**
	b.Cleanup(cancel)

	now := time.Now()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i%2 == 0 {
			s.SetHatchMoving("UID:bench", now)
		} else {
			s.SetHatchMoving("UID:bench", time.Time{})
		}
	}
}
