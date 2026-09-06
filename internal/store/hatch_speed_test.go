package store

import (
	"testing"
)

// 复用 store_test.go 里的 newTestStore(带 t.TempDir + 收尾关闭),避免重复造轮子。
func newHatchSpeedScope(t *testing.T) *Scoped {
	t.Helper()
	return newTestStore(t).For("acc1")
}

func TestHatchSpeedStateMachine(t *testing.T) {
	sc := newHatchSpeedScope(t)

	if st, err := sc.GetHatchSpeed(); err != nil || st.State != HatchSpeedIdle {
		t.Fatalf("初始应为 idle,实得 %v err=%v", st.State, err)
	}

	// 未 armed 时采样不该有任何动作(玩家只是开面板看进度,不是要测速)
	if _, err := sc.RecordHatchSample(1000, map[uint32]int32{1: 100}); err != nil {
		t.Fatalf("未 armed 时采样应为空操作: %v", err)
	}
	if st, _ := sc.GetHatchSpeed(); st.State != HatchSpeedIdle {
		t.Fatalf("未 armed 采样后应仍是 idle,实得 %v", st.State)
	}

	// 开始测速 → armed
	if err := sc.ArmHatchSpeed(1000); err != nil {
		t.Fatalf("ArmHatchSpeed 失败: %v", err)
	}
	if st, _ := sc.GetHatchSpeed(); st.State != HatchSpeedArmed {
		t.Fatalf("点开始后应为 armed,实得 %v", st.State)
	}

	// 第一次采样 → first
	if _, err := sc.RecordHatchSample(1010, map[uint32]int32{1: 100, 2: 200}); err != nil {
		t.Fatalf("第一次采样失败: %v", err)
	}
	if st, _ := sc.GetHatchSpeed(); st.State != HatchSpeedFirst || st.T1 != 1010 {
		t.Fatalf("第一次采样后应为 first 且 t1=1010,实得 %v t1=%d", st.State, st.T1)
	}

	// 第二次采样(间隔 20s,两颗蛋各 +400 / +800 孵化秒 → 20 倍与 40 倍,取中位 30)
	// 故意让两颗蛋不一致,验证取中位数而非平均值((20+40)/2 也是 30,换个数更好):
	// 用 +400 / +600 → 20 与 30 → 中位数 25(平均也是 25)… 换成三颗:20/30/100 → 中位 30,平均 50
	secs2 := map[uint32]int32{1: 100 + 400, 2: 200 + 600, 3: 3000 + 2000}
	// 第三颗第一次采样里没有,应被忽略(只对两批都存在的蛋差分)
	if _, err := sc.RecordHatchSample(1030, secs2); err != nil {
		t.Fatalf("第二次采样失败: %v", err)
	}
	st, err := sc.GetHatchSpeed()
	if err != nil {
		t.Fatalf("读状态失败: %v", err)
	}
	if st.State != HatchSpeedDone {
		t.Fatalf("第二次采样后应为 done,实得 %v", st.State)
	}
	// (100+400-100)/20 = 20;(200+600-200)/20 = 30 → 两数取中位 = 25
	if st.Rate != 25 {
		t.Errorf("倍率应为 25,实得 %v", st.Rate)
	}
	if st.MeasuredAt != 1030 {
		t.Errorf("measured_at 应为 1030,实得 %d", st.MeasuredAt)
	}
}

func TestHatchSpeedTooCloseResetsStart(t *testing.T) {
	sc := newHatchSpeedScope(t)
	sc.ArmHatchSpeed(1000)
	sc.RecordHatchSample(1000, map[uint32]int32{1: 100})

	// 间隔不足(5s):不当作第二次,而是重置起点 —— 否则会算出 (105-100)/5=1 这种假值
	// (实测 1s 区间会算出 5.00 或 67.50,见 docs/pcap-2026-09-05-hatch-rate.md §5)
	if _, err := sc.RecordHatchSample(1005, map[uint32]int32{1: 105}); err != nil {
		t.Fatalf("采样失败: %v", err)
	}
	st, _ := sc.GetHatchSpeed()
	if st.State != HatchSpeedFirst {
		t.Fatalf("间隔不足应保持 first,实得 %v", st.State)
	}
	if st.T1 != 1005 {
		t.Errorf("间隔不足应把这次当新的第一次(t1=1005),实得 %d", st.T1)
	}

	// 重置后再等够,就能正常出结果:(305-105)/20 = 10
	if _, err := sc.RecordHatchSample(1025, map[uint32]int32{1: 305}); err != nil {
		t.Fatalf("采样失败: %v", err)
	}
	st, _ = sc.GetHatchSpeed()
	if st.State != HatchSpeedDone || st.Rate != 10 {
		t.Errorf("重置后应正常完成,期望 rate=10,实得 state=%v rate=%v", st.State, st.Rate)
	}
}

func TestHatchSpeedIgnoresEmptyAndRearm(t *testing.T) {
	sc := newHatchSpeedScope(t)
	sc.ArmHatchSpeed(1000)

	// 空采样(下发的蛋都不在孵)不该推进状态机
	if _, err := sc.RecordHatchSample(1000, nil); err != nil {
		t.Fatalf("空采样应为空操作: %v", err)
	}
	if st, _ := sc.GetHatchSpeed(); st.State != HatchSpeedArmed {
		t.Fatalf("空采样后应仍是 armed,实得 %v", st.State)
	}

	// 走到 done
	sc.RecordHatchSample(1000, map[uint32]int32{1: 0})
	sc.RecordHatchSample(1020, map[uint32]int32{1: 100})
	if st, _ := sc.GetHatchSpeed(); st.State != HatchSpeedDone {
		t.Fatalf("应已完成,实得 %v", st.State)
	}

	// 完成后再来采样不该把结果冲掉(玩家开着面板时会持续下发)
	if _, err := sc.RecordHatchSample(1040, map[uint32]int32{1: 300}); err != nil {
		t.Fatalf("采样失败: %v", err)
	}
	if st, _ := sc.GetHatchSpeed(); st.Rate != 5 {
		t.Errorf("完成后新采样不应改结果,期望保持 5,实得 %v", st.Rate)
	}

	// 重新测速应清空旧结果
	if err := sc.ArmHatchSpeed(2000); err != nil {
		t.Fatalf("重新测速失败: %v", err)
	}
	if st, _ := sc.GetHatchSpeed(); st.State != HatchSpeedArmed || st.Rate != 0 {
		t.Errorf("重新测速应回到 armed 且 rate=0,实得 state=%v rate=%v", st.State, st.Rate)
	}

	// 取消
	if err := sc.CancelHatchSpeed(); err != nil {
		t.Fatalf("取消失败: %v", err)
	}
	if st, _ := sc.GetHatchSpeed(); st.State != HatchSpeedIdle {
		t.Errorf("取消后应回到 idle,实得 %v", st.State)
	}
}

func TestClearStaleHatchSpeed(t *testing.T) {
	s := newTestStore(t)

	s.For("online").ArmHatchSpeed(1000)
	s.For("offline").ArmHatchSpeed(1000)
	s.For("offline").RecordHatchSample(1000, map[uint32]int32{1: 0})
	s.For("offline").RecordHatchSample(1030, map[uint32]int32{1: 210})

	// 只有 online 还在线:offline 的结果必须清掉(倍率依赖玩家此刻的状态,
	// 下线后再上线状态已变,旧结果留着会让新会话按上一轮的倍率外推 —— 那是虚报)
	if err := s.ClearStaleHatchSpeed([]string{"online"}); err != nil {
		t.Fatalf("ClearStaleHatchSpeed 失败: %v", err)
	}
	if st, _ := s.For("offline").GetHatchSpeed(); st.State != HatchSpeedIdle {
		t.Errorf("下线账号的测速结果应被清除,实得 %v", st.State)
	}
	if st, _ := s.For("online").GetHatchSpeed(); st.State != HatchSpeedArmed {
		t.Errorf("在线账号的结果应保留,实得 %v", st.State)
	}

	// active 为空(无人在线)应清全部:服务重启后内存连接表已空,库里的结果无人认领
	if err := s.ClearStaleHatchSpeed(nil); err != nil {
		t.Fatalf("ClearStaleHatchSpeed(nil) 失败: %v", err)
	}
	if st, _ := s.For("online").GetHatchSpeed(); st.State != HatchSpeedIdle {
		t.Errorf("无人在线时应清除全部,实得 %v", st.State)
	}
}

func TestHatchSpeedMedianPrefersMiddle(t *testing.T) {
	sc := newHatchSpeedScope(t)
	sc.ArmHatchSpeed(1000)
	// 三颗蛋各算出 10 / 20 / 60 倍(中位数 20;最小 10、最大 60、平均 30)。
	// 取中位数是为了抗跳变:实测服务器偶发批量补齐会让**单颗**蛋的差分暴涨
	// (docs/pcap-2026-09-05-hatch-rate.md §4:22 个差分里混着 16.9 / 130.0),
	// 取最小/最大/平均都会被它带歪。用奇数颗才会走到中位数那条分支。
	dt := int64(20)
	sc.RecordHatchSample(1000, map[uint32]int32{1: 0, 2: 0, 3: 0})
	sc.RecordHatchSample(1000+dt, map[uint32]int32{
		1: 10 * int32(dt),
		2: 20 * int32(dt),
		3: 60 * int32(dt),
	})
	st, _ := sc.GetHatchSpeed()
	if st.State != HatchSpeedDone {
		t.Fatalf("应已完成,实得 %v", st.State)
	}
	if st.Rate != 20 {
		t.Errorf("三颗蛋应取中位数 20(最小 10、最大 60、平均 30),实得 %v", st.Rate)
	}
}

func TestHatchSpeedSkipsRegressedEggs(t *testing.T) {
	sc := newHatchSpeedScope(t)
	sc.ArmHatchSpeed(1000)
	// 蛋 2 的进度**倒退**了(中途被取出、换进别的蛋),它的差分必须跳过:
	// 混进来会算出负增量,把整次测速的倍率拉低甚至弄成负数。
	sc.RecordHatchSample(1000, map[uint32]int32{1: 100, 2: 500})
	sc.RecordHatchSample(1020, map[uint32]int32{1: 200, 2: 300})
	st, _ := sc.GetHatchSpeed()
	if st.State != HatchSpeedDone {
		t.Fatalf("应已完成,实得 %v", st.State)
	}
	// 只剩蛋 1 可用:(200-100)/20 = 5;若没跳过蛋 2 会得到 (5 + -10)/2 = -2.5
	if st.Rate != 5 {
		t.Errorf("倒退的蛋应被跳过,期望 5,实得 %v", st.Rate)
	}
}
