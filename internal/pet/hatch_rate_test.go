package pet

import (
	"math"
	"testing"
	"time"
)

// TestHatchActivityRateWindow 守「孵蛋加速日」的时间表边界。
//
// 窗口 = 北京时间周五 04:00 ~ 周一 04:00。边界一旦写错(尤其差 1 秒、或按本地时区
// 而非北京时间算),整周的预计完成时间都会在活动开始前/结束后多报或少报 5 倍 ——
// 而这类错误在开发环境很难撞见(抓包主机时区未必是 UTC+8)。故逐个边界钉死。
func TestHatchActivityRateWindow(t *testing.T) {
	// at 按北京时间组装一个时刻(本地时区无关)。
	at := func(y int, mo time.Month, d, h, mi, s int) int64 {
		return time.Date(y, mo, d, h, mi, s, 0, cstZone).Unix()
	}

	cases := []struct {
		name string
		ts   int64
		want float64
	}{
		// 周四:窗口尚未开始
		{"周四 03:59:59", at(2026, 9, 3, 3, 59, 59), 1},
		{"周四 23:00", at(2026, 9, 3, 23, 0, 0), 1},
		// 周五:04:00 整点开始(边界各差 1 秒)
		{"周五 03:59:59", at(2026, 9, 4, 3, 59, 59), 1},
		{"周五 04:00:00", at(2026, 9, 4, 4, 0, 0), hatchActivityRate},
		{"周五 12:00", at(2026, 9, 4, 12, 0, 0), hatchActivityRate},
		{"周五 23:59:59", at(2026, 9, 4, 23, 59, 59), hatchActivityRate},
		// 周末:全天
		{"周六 00:00", at(2026, 9, 5, 0, 0, 0), hatchActivityRate},
		{"周六 13:26", at(2026, 9, 5, 13, 26, 0), hatchActivityRate}, // 实测 pcap 时刻
		{"周日 23:59:59", at(2026, 9, 6, 23, 59, 59), hatchActivityRate},
		// 周一:04:00 整点结束(边界各差 1 秒)
		{"周一 00:00", at(2026, 9, 7, 0, 0, 0), hatchActivityRate},
		{"周一 03:59:59", at(2026, 9, 7, 3, 59, 59), hatchActivityRate},
		{"周一 04:00:00", at(2026, 9, 7, 4, 0, 0), 1},
		{"周一 12:00", at(2026, 9, 7, 12, 0, 0), 1},
		// 周二~周三:窗口外
		{"周二 12:00", at(2026, 9, 8, 12, 0, 0), 1},
		{"周三 12:00", at(2026, 9, 9, 12, 0, 0), 1},
	}
	for _, c := range cases {
		if got := HatchActivityRate(c.ts); got != c.want {
			t.Errorf("%s: 倍率 = %v,期望 %v", c.name, got, c.want)
		}
	}
}

// TestHatchActivityRateIgnoresHostZone 守「窗口按北京时间判定,与抓包主机时区无关」。
//
// 抓包主机常是 UTC 的服务器/容器。若按本地时区算,周五 04:00(北京)= 周四 20:00(UTC),
// 会被判到周四 → 整段窗口偏移 8 小时,恰好跨过 04:00 这个边界。
func TestHatchActivityRateIgnoresHostZone(t *testing.T) {
	// 保存并强制改掉进程时区,结束后还原(Go 的 time.Local 是全局的,必须还原以免影响他例)
	old := time.Local
	defer func() { time.Local = old }()

	for _, zone := range []*time.Location{
		time.UTC,
		time.FixedZone("UTC+9", 9*60*60),
		time.FixedZone("UTC-7", -7*60*60),
	} {
		time.Local = zone
		// 北京时间周六 12:00 —— 在任何时区下都该是加速中
		sat := time.Date(2026, 9, 5, 12, 0, 0, 0, cstZone).Unix()
		if got := HatchActivityRate(sat); got != hatchActivityRate {
			t.Errorf("时区 %s 下周六 12:00 应为 %v,实得 %v(窗口不该跟主机时区走)",
				zone, hatchActivityRate, got)
		}
		// 北京时间周二 12:00 —— 任何时区下都该是 1 倍
		tue := time.Date(2026, 9, 8, 12, 0, 0, 0, cstZone).Unix()
		if got := HatchActivityRate(tue); got != 1 {
			t.Errorf("时区 %s 下周二 12:00 应为 1,实得 %v", zone, got)
		}
	}
}

// TestHatchRateWithMoving 守「总倍率 = 活动倍率 × 移动增益」。
//
// 前端按同一口径自己算(见 hatch.js 的 hatchRateNow),两边必须一致 —— 故这里把
// 期望值写死成数字,任一侧改了算法都会对不上。
func TestHatchRateWithMoving(t *testing.T) {
	at := func(y int, mo time.Month, d, h int) int64 {
		return time.Date(y, mo, d, h, 0, 0, 0, cstZone).Unix()
	}
	// 加速日窗口内(周六):静止 5、移动 5×4.2=21
	if got := HatchRate(at(2026, 9, 5, 12), false); got != 5 {
		t.Errorf("加速日静止应为 5,实得 %v", got)
	}
	if got := HatchRate(at(2026, 9, 5, 12), true); math.Abs(got-21) > 0.01 {
		t.Errorf("加速日移动应为 21(5 × 4.2),实得 %v", got)
	}
	// 非加速日(周二):静止 1、移动 1×4.2=4.2
	// ⚠️ 后者是**推算值**(六份 pcap 全抓在加速日,没有对照样本),若实测不符,
	// 改 hatchMoveGain 时同步改这里与前端常数。
	if got := HatchRate(at(2026, 9, 8, 12), false); got != 1 {
		t.Errorf("非加速日静止应为 1,实得 %v", got)
	}
	if got := HatchRate(at(2026, 9, 8, 12), true); math.Abs(got-4.2) > 0.01 {
		t.Errorf("非加速日移动应为 4.2(1 × 4.2),实得 %v", got)
	}
	// 异常时刻不该崩,也不该被算成某周的某天
	if got := HatchRate(0, true); got <= 0 {
		t.Errorf("ts=0 时应退回安全的正倍率,实得 %v", got)
	}
}

// TestHatchActivityRateZeroTS 守异常输入:0 / 负数不该被算成「很久以前的周四」。
func TestHatchActivityRateZeroTS(t *testing.T) {
	for _, ts := range []int64{0, -1, -1e9} {
		if got := HatchActivityRate(ts); got != 1 {
			t.Errorf("ts=%d 应返回 1(异常输入),实得 %v", ts, got)
		}
	}
}
