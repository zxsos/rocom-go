package server

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"
)

// TestHatchRateReflectsMoving 守「API 返回的倍率随移动状态实时变化」。
//
// 这是「实时显示孵化时间」的核心保障:玩家跑起来,接口就该给出更快的倍率;
// 站住或超时,就该回到静止那档。回放覆盖不到(历史包时刻与 time.Now() 差几小时,
// 必然判定为静止),故在这里用可控的时刻钉住。
func TestHatchRateReflectsMoving(t *testing.T) {
	s := newTestServer(t)
	seedContract(t, s)
	const acc = contractAcc

	rate := func() float64 {
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/eggs?account="+acc, nil))
		var j struct{ HatchRate float64 `json:"hatchRate"` }
		if err := json.Unmarshal(w.Body.Bytes(), &j); err != nil {
			t.Fatalf("解响应: %v (%s)", err, w.Body.String())
		}
		return j.HatchRate
	}
	base := rate()
	if base <= 0 {
		t.Fatalf("前置:倍率应为正,实得 %v", base)
	}

	// 玩家开始移动 → 倍率应变大
	s.SetHatchMoving(acc, time.Now())
	mv := rate()
	if !(mv > base) {
		t.Errorf("移动后倍率应大于静止(%v),实得 %v —— 实时没生效", base, mv)
	}

	// 明确停止(零值) → 立刻回到静止倍率
	s.SetHatchMoving(acc, time.Time{})
	if got := rate(); got != base {
		t.Errorf("停止后应回到静止倍率 %v,实得 %v", base, got)
	}

	// 超时:不再有移动包(切后台/站住不补发),TTL 后自动翻回
	s.SetHatchMoving(acc, time.Now().Add(-hatchMoveTTL-time.Second))
	if got := rate(); got != base {
		t.Errorf("超过 TTL 应自动翻回静止倍率 %v,实得 %v", base, got)
	}
}

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
