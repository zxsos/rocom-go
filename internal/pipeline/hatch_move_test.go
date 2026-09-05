package pipeline

import (
	"testing"
	"time"

	"github.com/whoisnian/rocom-capture/internal/scene"
)

// TestObserveHatchMove 守「玩家是否在移动」的判定(孵化倍率的在线加成依据)。
//
// 移动会在活动倍率之上再加孵化进度(实测静止 5.00、移动 16.9~25.8),而协议不会给
// 倍率,只能从这个前提观测。判定错有两种方向:该算移动时没算(少提示)、停下后
// 仍算移动(一直挂着「移动中」误导)—— 两边都要钉住。
func TestObserveHatchMove(t *testing.T) {
	p, srv := newTestPipeline(t)
	const acc = "UID:test-hatch-move"
	now := time.Now()

	// 在移动:速度非零 → 记下当前时刻
	p.observeHatchMove(acc, scene.MoveReq{
		Speed: scene.Position{X: 400, Y: 300, Z: 0},
	}, now)
	if !srv.IsHatchMoving(acc) {
		t.Error("速度非零时应算在移动")
	}

	// 客户端上报 stop_move → 立即翻回,不等 TTL
	p.observeHatchMove(acc, scene.MoveReq{
		Speed:    scene.Position{X: 400, Y: 300, Z: 0},
		StopMove: true,
	}, now.Add(time.Second))
	if srv.IsHatchMoving(acc) {
		t.Error("收到 stop_move 后应立即不算在移动")
	}

	// 没按 stop 但速度归零(站着 / 上了静止的坐骑)同样算停
	p.observeHatchMove(acc, scene.MoveReq{
		Speed: scene.Position{X: 400, Y: 300, Z: 0},
	}, now.Add(2*time.Second))
	if !srv.IsHatchMoving(acc) {
		t.Fatal("前置:应重新算在移动")
	}
	p.observeHatchMove(acc, scene.MoveReq{}, now.Add(3*time.Second)) // 零速度
	if srv.IsHatchMoving(acc) {
		t.Error("速度归零(即使没带 stop_move)也应算停下")
	}
}
