package store

import (
	"testing"

	"github.com/whoisnian/rocom-capture/internal/pet"
)

// TestEggHatchUpdateSingleClock 守「孵化进度的采样时刻只有一个钟」。
//
// 前端估孵化倍率靠相邻两次采样的差分 Δv/Δt(见 docs/data.md 3.6 与
// web/src/pages/eggs/hatch.js):两个 t 一旦不同源,钟差就直接落进分母。
// 网关时钟若与游戏服务器差 60 秒,相隔 10 秒的两次采样会被算成 70 秒 →
// 5 倍速算出 0.7,再被钳到下限 1,进度条与预计时间一起失真。
//
// 协议里恰好有两个候选钟,而它们会在同一颗蛋上交替出现:
//   - 背包快照(0x1344 / 登录)给的是**服务器**的 last_hatch_update_sec
//   - 0x0312 顶层的 hatched_secs[] 不带时刻,只能配**抓包主机**的消息时刻
//
// 这里钉死:落库的一律是后者(now),服务器那个值不采信 —— now 是唯一处处可得的钟。
func TestEggHatchUpdateSingleClock(t *testing.T) {
	st := newTestStore(t)
	if err := st.UpsertAccount(testAcc, "测试账号"); err != nil {
		t.Fatalf("建账号: %v", err)
	}
	sc := st.For(testAcc)

	// 服务器时钟 1700000000,抓包主机时钟比它快 300 秒 —— 两者差得越明显,
	// 混用越藏不住。
	const serverTS, hostTS = int64(1700000000), int64(1700000300)

	// 背包快照路径:view 里带着**服务器**的时刻,落库后必须变成 now
	if err := sc.UpsertEggs([]*pet.EggView{
		{Gid: 90001, ItemID: 107028, Hatching: true, HatchedSecs: 600, MaxSecs: 28800, HatchUpdate: serverTS},
	}, hostTS, nil); err != nil {
		t.Fatalf("UpsertEggs: %v", err)
	}
	got := eggByGid(t, sc, 90001)
	if got.HatchUpdate != hostTS {
		t.Errorf("背包快照应把 HatchUpdate 记成抓包主机时刻 %d,实得 %d("+
			"采信了服务器的 %d,差分就跨钟了)", hostTS, got.HatchUpdate, serverTS)
	}
	if got.HatchedSecs != 600 {
		t.Fatalf("进度本身不该被动:HatchedSecs = %d,期望 600", got.HatchedSecs)
	}

	// 0x0312 顶层 hatched_secs[] 路径:它本来就只能配 now,两次采样的钟必须同源
	if _, err := sc.ReconcileHatching([]uint32{90001}, []int32{900}, nil, hostTS+10); err != nil {
		t.Fatalf("ReconcileHatching: %v", err)
	}
	got = eggByGid(t, sc, 90001)
	if got.HatchUpdate != hostTS+10 {
		t.Errorf("对账应把 HatchUpdate 记成抓包主机时刻 %d,实得 %d", hostTS+10, got.HatchUpdate)
	}
	// 这才是差分真正要的那对:(Δv=300) / (Δt=10) = 30 倍/秒 → 前端会钳到上限,
	// 但至少是「同一把尺子量出来的」。若两次采样的钟不同源,分母是 300+10。
	if got.HatchedSecs != 900 {
		t.Fatalf("HatchedSecs = %d,期望 900", got.HatchedSecs)
	}
}

// TestEggHatchUpdateZeroProgress 守「进度为 0 时不留时刻」。
//
// 不在孵蛋器里的蛋 hatched_secs 恒为 0。若给它们也盖上观测时刻,入孵后第一次采样的
// 差分就退化成「从 0 到现在」—— 那正是被实测否掉的单点法(玩家跑动过后会虚报成
// 8~9 倍,真实倍率 5)。留 0,前端即按「进度未知」处理,不外推。
func TestEggHatchUpdateZeroProgress(t *testing.T) {
	st := newTestStore(t)
	if err := st.UpsertAccount(testAcc, "测试账号"); err != nil {
		t.Fatalf("建账号: %v", err)
	}
	sc := st.For(testAcc)
	const hostTS = int64(1700000300)

	// 背包快照:没进孵蛋器 → 时刻清零,哪怕服务器给了个时刻
	if err := sc.UpsertEggs([]*pet.EggView{
		{Gid: 90002, ItemID: 107028, HatchedSecs: 0, MaxSecs: 28800, HatchUpdate: 1700000000},
	}, hostTS, nil); err != nil {
		t.Fatalf("UpsertEggs: %v", err)
	}
	if got := eggByGid(t, sc, 90002); got.HatchUpdate != 0 {
		t.Errorf("零进度时 HatchUpdate 应为 0,实得 %d", got.HatchUpdate)
	}

	// 0x0312 对账:刚入孵(进度 0),同样不留时刻
	if _, err := sc.ReconcileHatching([]uint32{90002}, []int32{0}, nil, hostTS+10); err != nil {
		t.Fatalf("ReconcileHatching(sec=0): %v", err)
	}
	if got := eggByGid(t, sc, 90002); got.HatchUpdate != 0 {
		t.Errorf("对账到 sec=0 时 HatchUpdate 应为 0,实得 %d", got.HatchUpdate)
	}
	if got := eggByGid(t, sc, 90002); !got.Hatching {
		t.Errorf("蛋在孵蛋器名单里,hatching 应已置位")
	}
}

// eggByGid 取回一颗蛋(读取路径会按名称库重算,故只断言与时钟/进度相关的字段)。
func eggByGid(t *testing.T, sc *Scoped, gid uint32) *pet.EggView {
	t.Helper()
	eggs, err := sc.ListEggs(EggFilter{})
	if err != nil {
		t.Fatalf("ListEggs: %v", err)
	}
	for _, e := range eggs {
		if e.Gid == gid {
			return e
		}
	}
	t.Fatalf("gid %d 不在列表里(共 %d 颗)", gid, len(eggs))
	return nil
}
