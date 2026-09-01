package store

import "testing"

// TestApprovedAnnotation 锁住「按 (kind, code) 精确取一条已通过标注」。
//
// 这条查询是试炼组载荷时的热路径:一个节点 3 个事件,每个都要问一次
// 「这个 event_conf_id 标了没有」。它必须**只认 approved** —— 玩家提交的
// 未审猜测若被当成答案,页面会把某人的猜测显示成事实。
//
// 同时验证 event 这一新 kind 走通了整条链路(提交 → 审核 → 按 kind 取回):
// kind 是个字符串列,新增取值不需要改表结构,但若哪天有人给它加校验白名单,
// 忘了带 event,试炼页就会静默退回「无标注」状态。
func TestApprovedAnnotation(t *testing.T) {
	st := newTestStore(t)
	const code = int64(130056)

	// 1. 空库:不该因为「没查到」而报错(试炼页会频繁遇到未标注的事件)
	if _, ok := st.ApprovedAnnotation("event", code); ok {
		t.Error("空库时 ApprovedAnnotation 应返回 false")
	}

	// 2. 提交了但未审:不算数
	if _, err := st.SubmitAnnotation(Annotation{
		Kind: "event", Code: code, Name: "奇丽花", Submitter: "UID:1",
	}); err != nil {
		t.Fatalf("提交标注: %v", err)
	}
	if _, ok := st.ApprovedAnnotation("event", code); ok {
		t.Fatal("待审标注不该被 ApprovedAnnotation 取到")
	}

	// 3. 审核通过:取得到,且字段完整
	pending, err := st.PendingAnnotations("event")
	if err != nil || len(pending) != 1 {
		t.Fatalf("待审列表 = %d 条, 期望 1 (err=%v)", len(pending), err)
	}
	if err := st.ReviewAnnotation(pending[0].ID, true, "admin"); err != nil {
		t.Fatalf("审核: %v", err)
	}
	got, ok := st.ApprovedAnnotation("event", code)
	if !ok {
		t.Fatal("审核通过後 ApprovedAnnotation 仍查不到")
	}
	if got.Name != "奇丽花" {
		t.Errorf("name = %q, 期望 %q", got.Name, "奇丽花")
	}
	if got.Kind != "event" || got.Code != code {
		t.Errorf("kind/code = %q/%d, 期望 event/%d", got.Kind, got.Code, code)
	}

	// 4. 按 kind 隔离:同 code 的 skill 标注不该被串到
	if _, ok := st.ApprovedAnnotation("skill", code); ok {
		t.Error("kind 未隔离 —— event 的标注被 skill 取到了")
	}
}
