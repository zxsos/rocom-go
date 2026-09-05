package server

import (
	"testing"
	"time"
)

// TestHatchMovingFlip 守「玩家是否在移动」的状态翻转(孵化倍率的在线加成依据)。
//
// 回放覆盖不到这条逻辑(历史数据的包内时刻与 time.Now() 差几小时,必然超时 ——
// 见 isHatchMoving 的注释),故在这里用可控的时刻钉住三种情形。
func TestHatchMovingFlip(t *testing.T) {
	s := newTestServer(t)
	const acc = "UID:test-moving"

	// 从没收到过移动包 → 不在移动
	if s.isHatchMoving(acc) {
		t.Error("未收到移动包时应为 false")
	}

	// 收到一个在移动的包 → 在移动
	s.SetHatchMoving(acc, time.Now())
	if !s.isHatchMoving(acc) {
		t.Error("刚收到移动包时应为 true")
	}

	// 客户端上报 stop_move(记零值) → 立即翻回,不等 TTL
	s.SetHatchMoving(acc, time.Time{})
	if s.isHatchMoving(acc) {
		t.Error("收到 stop_move(零值)后应立即为 false")
	}

	// 切后台/断线:不会再有 stop 包,只剩 TTL 兜底
	s.SetHatchMoving(acc, time.Now().Add(-hatchMoveTTL+time.Second))
	if !s.isHatchMoving(acc) {
		t.Error("TTL 内仍应算在移动")
	}
	s.SetHatchMoving(acc, time.Now().Add(-hatchMoveTTL-time.Second))
	if s.isHatchMoving(acc) {
		t.Error("超过 TTL 应翻回 false(切后台/断线时不会有 stop 包)")
	}
}

// TestHatchMovingEmptyAccount 守空账号不该被记录(防御:未归属的消息不该污染状态)。
func TestHatchMovingEmptyAccount(t *testing.T) {
	s := newTestServer(t)
	s.SetHatchMoving("", time.Now())
	if s.isHatchMoving("") {
		t.Error("空账号不该被记成在移动")
	}
}
