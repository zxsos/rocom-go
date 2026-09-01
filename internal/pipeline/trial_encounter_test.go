package pipeline

import (
	"testing"

	"google.golang.org/protobuf/encoding/protowire"

	"github.com/whoisnian/rocom-capture/internal/capture"
	"github.com/whoisnian/rocom-capture/internal/gcp"
	"github.com/whoisnian/rocom-capture/internal/trial"
)

// 本文件锁住「见闻录补录」的**两条入口**。
//
// 背景:补录只挂在 0x1975(档案同步)上,而 0x1975 要真打一场试炼战斗才下发;
// 0x1959(打开试炼面板)在**登录几秒后**就带回了同一份档案,却漏了补录 ——
// 用户打开面板看到的是空白三张图,得打完一场才填上。实测 pcap:
//   登录 14:55:06 → 0x1959 14:55:10(带 167/127/98 只)→ 首条 0x1975 15:04:45
//
// 这条路径光靠 golden 契约测试锁不住(它只比对 /api/* 的响应结构,而漏调补录
// 的响应结构完全正常,只是**数据少了一大半**),故在此直接断言库里的记录数。

// logRecordBody 拼一条见闻录(GrassTrialLogSceneRecord):
// field1=log_conf_id(100/101/102 → 第1/2/3章), field3=discovered_petbase_ids(非 packed)。
func logRecordBody(confID uint32, ids ...uint32) []byte {
	b := protowire.AppendTag(nil, 1, protowire.VarintType)
	b = protowire.AppendVarint(b, uint64(confID))
	for _, id := range ids {
		b = protowire.AppendTag(b, 3, protowire.VarintType)
		b = protowire.AppendVarint(b, uint64(id))
	}
	b = protowire.AppendTag(b, 6, protowire.VarintType) // total
	return protowire.AppendVarint(b, uint64(len(ids)))
}

// progressBody 拼 GrassTrialProgressData:field4 是 logs(见闻录,每册一条子消息)。
func progressBody(logs ...[]byte) []byte {
	var b []byte
	for _, l := range logs {
		b = protowire.AppendTag(b, 4, protowire.BytesType)
		b = protowire.AppendBytes(b, l)
	}
	return b
}

// getInfoRspBody 拼 0x1959 的响应:field2=trial_data{field2=progress}。
func getInfoRspBody(progress []byte) []byte {
	td := protowire.AppendTag(nil, 2, protowire.BytesType)
	td = protowire.AppendBytes(td, progress)
	b := protowire.AppendTag(nil, 2, protowire.BytesType)
	return protowire.AppendBytes(b, td)
}

// progressSyncBody 拼 0x1975 的响应:field1=progress。
func progressSyncBody(progress []byte) []byte {
	b := protowire.AppendTag(nil, 1, protowire.BytesType)
	return protowire.AppendBytes(b, progress)
}

// TestSyncTrialEncountersOnGetInfo 锁住最容易被漏掉的那条入口:
// **打开面板(0x1959)就要补录**,不必等打完一场(0x1975)。
//
// 这条曾经是坏的 —— 补录只挂在 0x1975 上,而 0x1959 在登录 4 秒后就带着同一份
// 档案来了。用户视角:登录进去图鉴不刷新,打一场才突然出现一堆。
func TestSyncTrialEncountersOnGetInfo(t *testing.T) {
	p, _ := newTestPipeline(t)
	prog := progressBody(
		logRecordBody(100, 3001, 3002, 3003), // 第 1 章
		logRecordBody(101, 4001, 4002),       // 第 2 章
		logRecordBody(102, 5001),             // 第 3 章
	)
	p.handleTrial(capture.Message{
		Direction: gcp.S2C, Opcode: trial.OpGetInfoRsp,
		AppBody: getInfoRspBody(prog),
	}, testAcc)

	want := map[uint32]int{1: 3, 2: 2, 3: 1}
	for ch, n := range want {
		if got := len(p.st.TrialEncounters(testAcc, ch)); got != n {
			t.Errorf("打开面板后第 %d 章应有 %d 条遇见记录,实际 %d", ch, n, got)
		}
	}
}

// TestSyncTrialEncountersOnProgressSync 锁住 0x1975 入口(打完一场后的档案同步):
// 面板入口修好之后,这条更不能丢 —— 战斗中新遇到的精灵只走这条路补进来。
func TestSyncTrialEncountersOnProgressSync(t *testing.T) {
	p, _ := newTestPipeline(t)
	prog := progressBody(logRecordBody(100, 3001, 3002))
	p.handleTrial(capture.Message{
		Direction: gcp.S2C, Opcode: trial.OpProgressDataSync,
		AppBody: progressSyncBody(prog),
	}, testAcc)
	if got := len(p.st.TrialEncounters(testAcc, 1)); got != 2 {
		t.Errorf("档案同步后第 1 章应有 2 条,实际 %d", got)
	}
}

// TestSyncTrialEncountersIdempotent 锁住幂等:档案是**每次都全量重发**的,
// 反复收到同一份不能把 times 反复累加、也不能把 last_seen 一直推到当下 ——
// 否则「首次遇到」这个时间语义就废了。
func TestSyncTrialEncountersIdempotent(t *testing.T) {
	p, _ := newTestPipeline(t)
	prog := progressBody(logRecordBody(100, 3001, 3002))
	m := capture.Message{Direction: gcp.S2C, Opcode: trial.OpGetInfoRsp, AppBody: getInfoRspBody(prog)}
	p.handleTrial(m, testAcc)
	p.handleTrial(m, testAcc) // 同一份档案再来一次
	p.handleTrial(m, testAcc)

	got := p.st.TrialEncounters(testAcc, 1)
	if len(got) != 2 {
		t.Fatalf("重复补录后应仍是 2 条,实际 %d", len(got))
	}
	for base, e := range got {
		if e.Times != 1 {
			t.Errorf("petbase %d 的 times 应为 1(补录未累加),实际 %d", base, e.Times)
		}
	}
}

// TestSyncTrialEncountersSkipsUnknownChapter 锁住「认不出章就丢弃」:
// log_conf_id 不在 100/101/102 里时不能硬套默认值 —— 记错章会把一章的进度
// 污染到另一章,而用户很难察觉。
func TestSyncTrialEncountersSkipsUnknownChapter(t *testing.T) {
	p, _ := newTestPipeline(t)
	prog := progressBody(logRecordBody(999, 3001, 3002)) // 认不出的 conf id
	p.handleTrial(capture.Message{
		Direction: gcp.S2C, Opcode: trial.OpGetInfoRsp, AppBody: getInfoRspBody(prog),
	}, testAcc)
	// 三章都不该有记录(ChapterOf() 返回 0 被丢弃),而不是落进某一章
	for ch := uint32(1); ch <= 3; ch++ {
		if got := len(p.st.TrialEncounters(testAcc, ch)); got != 0 {
			t.Errorf("未知册不该写入任何章节,第 %d 章却有 %d 条", ch, got)
		}
	}
}
