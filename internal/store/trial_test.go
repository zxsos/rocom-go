package store

import (
	"testing"
	"time"
)

// 本文件测草系试炼的遇见记录(见 trialEncounter 表与 internal/trial/battle.go)。
//
// 三条口径各有一个测试守着,都是**改错了不报错、只会让进度慢慢失真**的那类:
//  1. 每章独立计算 —— 记错章等于污染另一张图的进度;
//  2. kind 取最大值 —— 同一只可能在普通池与首领池都遇到过;
//  3. first_seen 保持首次、last_seen/times 累加 —— 进度条的「首次遇到」意义在此。

// seedEnc 写入一条遇见记录。ts 传 0 表示用当前时间。
// 补录历史时 ts 应传报文自带的时间(见 AddTrialEncounters)。
func seedEnc(t *testing.T, st *Store, acc string, ch, kind uint32, bases ...uint32) {
	t.Helper()
	if err := st.AddTrialEncounters(acc, ch, kind, bases, 0); err != nil {
		t.Fatalf("写遇见记录(第%d章 kind=%d %v): %v", ch, kind, bases, err)
	}
}

func TestAddTrialEncounters(t *testing.T) {
	st := newTestStore(t)

	// 第 1 章普通战遇到 3001
	seedEnc(t, st, testAcc, 1, 0, 3001)
	got := st.TrialEncounters(testAcc, 1)
	e, ok := got[3001]
	if !ok {
		t.Fatal("第1章应能查到 3001")
	}
	if e.Chapter != 1 || e.Kind != 0 || e.Times != 1 {
		t.Errorf("3001 = %+v, 期望 chapter=1 kind=0 times=1", e)
	}
	if e.FirstSeen != e.LastSeen {
		t.Errorf("首次与末次应相同: first=%d last=%d", e.FirstSeen, e.LastSeen)
	}

	// 再遇一次:times 累加、last_seen 前进、first_seen **不动**
	first := e.FirstSeen
	time.Sleep(1100 * time.Millisecond) // 秒级时间戳,不足 1 秒看不出差别
	seedEnc(t, st, testAcc, 1, 0, 3001)
	got = st.TrialEncounters(testAcc, 1)
	e = got[3001]
	if e.Times != 2 {
		t.Errorf("再遇一次 times 应为 2, 实际 %d", e.Times)
	}
	if e.FirstSeen != first {
		t.Errorf("first_seen 应保持首次 %d, 实际 %d", first, e.FirstSeen)
	}
	if e.LastSeen <= first {
		t.Errorf("last_seen 应前进到 %d 之后, 实际 %d", first, e.LastSeen)
	}
}

// TestTrialEncountersPerChapter 守「每章独立计算」—— 与 wiki 口径一致。
// 3005 同时在第 2、3 章的池里,只在第 2 章打过照面 → 第 3 章仍算未遇见。
func TestTrialEncountersPerChapter(t *testing.T) {
	st := newTestStore(t)
	seedEnc(t, st, testAcc, 2, 0, 3005)

	if _, ok := st.TrialEncounters(testAcc, 2)[3005]; !ok {
		t.Error("第2章应有 3005")
	}
	if _, ok := st.TrialEncounters(testAcc, 3)[3005]; ok {
		t.Error("第3章不该有 3005 —— 每章独立,漏了这条会污染另一张图的进度")
	}
	// chapter=0 查全部:应能查到,且章节信息正确
	all := st.TrialEncounters(testAcc, 0)
	if e, ok := all[3005]; !ok || e.Chapter != 2 {
		t.Errorf("全章查询 3005 = %+v (ok=%v), 期望 chapter=2", e, ok)
	}
}

// TestTrialEncountersKindMax 守冲突时 kind 取**最大值**。
// 同一只精灵可能先在普通池遇到(kind=0)、后在首领池遇到(kind=1),
// 记录里应留 1 —— 首领是更「高」的遭遇形态。
func TestTrialEncountersKindMax(t *testing.T) {
	st := newTestStore(t)
	seedEnc(t, st, testAcc, 1, 1, 8101) // 先首领
	seedEnc(t, st, testAcc, 1, 0, 8101) // 再普通:不该把 1 覆盖成 0
	if got := st.TrialEncounters(testAcc, 1)[8101].Kind; got != 1 {
		t.Errorf("kind 应保持最大值 1, 实际 %d(被低值覆盖了)", got)
	}

	seedEnc(t, st, testAcc, 2, 0, 8102) // 先普通
	seedEnc(t, st, testAcc, 2, 1, 8102) // 再首领:应升到 1
	if got := st.TrialEncounters(testAcc, 2)[8102].Kind; got != 1 {
		t.Errorf("kind 应升为 1, 实际 %d", got)
	}
}

// TestAddTrialEncountersSkips 守三道跳过条件,任一失效都会写进脏数据。
func TestAddTrialEncountersSkips(t *testing.T) {
	st := newTestStore(t)
	// chapter=0 跳过:归不到某张图上的记录毫无用处
	if err := st.AddTrialEncounters(testAcc, 0, 0, []uint32{3001}, 0); err != nil {
		t.Fatalf("chapter=0 不该报错: %v", err)
	}
	// petbase=0 跳过:协议里 0 是「没解析出来」,不是真编号
	if err := st.AddTrialEncounters(testAcc, 1, 0, []uint32{0, 3002}, 0); err != nil {
		t.Fatalf("含 0 不该报错: %v", err)
	}
	got := st.TrialEncounters(testAcc, 0)
	if _, ok := got[3001]; ok {
		t.Error("chapter=0 的记录不该入库")
	}
	if _, ok := got[0]; ok {
		t.Error("petbase=0 不该入库")
	}
	if e, ok := got[3002]; !ok || e.Chapter != 1 {
		t.Errorf("同一批里的 3002 应正常入库, 得到 %+v (ok=%v)", e, ok)
	}
	// 空列表:不写也不报错
	if err := st.AddTrialEncounters(testAcc, 1, 0, nil, 0); err != nil {
		t.Fatalf("空列表不该报错: %v", err)
	}
}

// TestAddTrialEncountersTS 守「时间戳取战斗时刻而非入库时刻」。
//
// 这是为**离线回放补录**服务的:回放一份上周的 pcap,记录的 first_seen 就该是
// 上周那场战斗的时间。若写成回放此刻,补出来的记录全挤在同一秒,
// 「首次遇到」这个字段便彻底失去意义。
func TestAddTrialEncountersTS(t *testing.T) {
	st := newTestStore(t)
	const battleTS = int64(1700000000) // 2023-11-14,刻意取一个远离当下的时刻

	if err := st.AddTrialEncounters(testAcc, 1, 0, []uint32{3001}, battleTS); err != nil {
		t.Fatalf("写入: %v", err)
	}
	e := st.TrialEncounters(testAcc, 1)[3001]
	if e.FirstSeen != battleTS {
		t.Errorf("first_seen 应取战斗时刻 %d, 实际 %d", battleTS, e.FirstSeen)
	}
	if e.LastSeen != battleTS {
		t.Errorf("last_seen 应取战斗时刻 %d, 实际 %d", battleTS, e.LastSeen)
	}

	// ts<=0 是「拿不到时间」的情形:退回当前时刻,不能记成 1970
	if err := st.AddTrialEncounters(testAcc, 2, 0, []uint32{3005}, 0); err != nil {
		t.Fatalf("写入: %v", err)
	}
	now := time.Now().Unix()
	if got := st.TrialEncounters(testAcc, 2)[3005].FirstSeen; got <= 0 || got > now+5 {
		t.Errorf("ts<=0 应退回当前时刻(约 %d), 实际 %d", now, got)
	}
}

