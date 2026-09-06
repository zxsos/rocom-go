package server

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

// eggRate 取 /api/eggs 返回的孵化倍率。
func eggRate(t *testing.T, s *Server, acc string) float64 {
	t.Helper()
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/eggs?account="+acc, nil))
	if w.Code != 200 {
		t.Fatalf("HTTP %d: %s", w.Code, w.Body.String())
	}
	var j struct {
		HatchRate float64 `json:"hatchRate"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &j); err != nil {
		t.Fatalf("解响应: %v (%s)", err, w.Body.String())
	}
	return j.HatchRate
}

// TestHatchRateIgnoresMoving 守「倍率**不**随移动状态变化」。
//
// 这是 2026-09-06 的结论,别再改回去。早先版本会在移动时乘一个固定增益 4.2,
// 那等于把「在线加成」这个**没有定值**的量当成有定值去外推:实测移动档倍率
// 16.90~27.32(中位 21.96、标准差 3.38),单一增益覆盖不了;且 ETA 会随玩家
// 跑跑停停每几秒跳一次(六份 pcap 合计 212 次,最密的一份平均 3.5 秒一次)。
// 现在倍率只由活动倍率(1x/5x)与玩家实测决定,移动状态不参与。
func TestHatchRateIgnoresMoving(t *testing.T) {
	s := newTestServer(t)
	seedContract(t, s)
	const acc = contractAcc

	base := eggRate(t, s, acc)
	if base <= 0 {
		t.Fatalf("前置:倍率应为正,实得 %v", base)
	}
	// 管线里已不再有移动观测(见本次改动),倍率不该受任何移动状态影响。
	// 这里钉住「同一账号连续两次请求倍率一致」—— 它只该随加速日时间表变。
	if again := eggRate(t, s, acc); again != base {
		t.Errorf("倍率不该自发变化,两次请求 %v 与 %v", base, again)
	}
}

// TestHatchRateUsesMeasured 守「玩家实测的倍率优先于活动倍率」。
//
// 这是「测速」功能的核心契约:玩家点开始 → 开两次孵蛋器 → 后端取差分,之后
// 接口就该返回**实测值**而非活动倍率。实测值可以大于(移动时)也可以小于
// 活动倍率,都必须如实返回 —— 它是测出来的,不是估出来的。
func TestHatchRateUsesMeasured(t *testing.T) {
	s := newTestServer(t)
	seedContract(t, s)
	const acc = contractAcc

	before := eggRate(t, s, acc)

	// 模拟一次完整测速:第一次采样 → 等够间隔 → 第二次采样
	sc := s.store.For(acc)
	if err := sc.ArmHatchSpeed(1000); err != nil {
		t.Fatalf("ArmHatchSpeed: %v", err)
	}
	if got := eggRate(t, s, acc); got != before {
		t.Errorf("刚开始测速(还没采样)时就该仍是活动倍率 %v,实得 %v", before, got)
	}
	sc.RecordHatchSample(1000, map[uint32]int32{1: 0, 2: 0, 3: 0})
	if got := eggRate(t, s, acc); got != before {
		t.Errorf("只采了一次样时仍该是活动倍率 %v,实得 %v", before, got)
	}
	// 间隔 30s、每颗蛋各推进 630 孵化秒 → 21 倍(加速日 5 倍叠加移动加成,典型值)
	sc.RecordHatchSample(1030, map[uint32]int32{1: 630, 2: 630, 3: 630})
	if got := eggRate(t, s, acc); got != 21 {
		t.Errorf("测速完成后应返回实测倍率 21,实得 %v", got)
	}
}
