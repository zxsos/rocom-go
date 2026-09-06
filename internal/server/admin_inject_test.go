package server

import (
	"encoding/json"
	"testing"
)

// TestMergeWildAccumulates 守「连投多只注入精灵时快照会累积」。
//
// 这是 2026-09-06 修的 bug:投放时广播只带新注入的那一只,而前端收到 wildpets
// 是**整表替换**(useWildPets.js 的 setData(d)),于是连投几只地图上始终只剩
// 最后一只,连真实野生宠也一起被清掉(因为 allPets 也推了空)。
//
// 修法是广播改用 mergeWild 返回的合并结果(它本来就返回了全量快照,只是被丢掉了)。
// 这里钉住 mergeWild 的累积语义 —— 广播那侧的正确性依赖它返回**全量**。
func TestMergeWildAccumulates(t *testing.T) {
	sn := newSnapshotStore()
	const acc = "UID:1"

	// 先有 2 只真实野生宠
	cur := sn.mergeWild(acc, 10003, []WildMark{
		{ID: "real-1", Name: "A"},
		{ID: "real-2", Name: "B"},
	})
	if len(cur.Pets) != 2 {
		t.Fatalf("前置:应有 2 只,实得 %d", len(cur.Pets))
	}
	// 普通野生宠图层也要在(注入不该把它清掉)
	sn.wild[acc].AllPets = []WildAllMark{{ID: "all-1"}, {ID: "all-2"}}

	// 投第一只
	cur = sn.mergeWild(acc, 10003, []WildMark{{ID: "inj-1", Name: "X", Inject: true}})
	if len(cur.Pets) != 3 {
		t.Errorf("投第 1 只后应有 3 只,实得 %d", len(cur.Pets))
	}
	if len(cur.AllPets) != 2 {
		t.Errorf("allPets 应保留 2 只,实得 %d", len(cur.AllPets))
	}

	// 投第二只:关键 —— 第一只必须还在
	cur = sn.mergeWild(acc, 10003, []WildMark{{ID: "inj-2", Name: "Y", Inject: true}})
	if len(cur.Pets) != 4 {
		t.Errorf("投第 2 只后应有 4 只(两只注入都在),实得 %d", len(cur.Pets))
	}
	ids := map[string]bool{}
	for _, p := range cur.Pets {
		ids[p.ID] = true
	}
	for _, want := range []string{"real-1", "real-2", "inj-1", "inj-2"} {
		if !ids[want] {
			t.Errorf("缺少 %s:实际 %v", want, ids)
		}
	}
	if len(cur.AllPets) != 2 {
		t.Errorf("allPets 仍应是 2 只,实得 %d", len(cur.AllPets))
	}
}

// TestMergeWildSnapshotReflectsAll 补一层:HTTP 快照接口读到的也是全量 ——
// 页面刷新后要能看到全部注入精灵,而不是只剩最后一只。
func TestMergeWildSnapshotReflectsAll(t *testing.T) {
	sn := newSnapshotStore()
	const acc = "UID:2"
	sn.mergeWild(acc, 10003, []WildMark{{ID: "i1"}})
	sn.mergeWild(acc, 10003, []WildMark{{ID: "i2"}})
	sn.mergeWild(acc, 10003, []WildMark{{ID: "i3"}})

	got := sn.getWild(acc)
	if got == nil || len(got.Pets) != 3 {
		t.Fatalf("快照应有 3 只,实得 %v", got)
	}
}

// TestDropWildKeepsOthers 守撤销只删指定的那只,其余(含真实野生宠)不受影响。
func TestDropWildKeepsOthers(t *testing.T) {
	sn := newSnapshotStore()
	const acc = "UID:3"
	sn.mergeWild(acc, 10003, []WildMark{{ID: "real"}, {ID: "inj-1"}, {ID: "inj-2"}})

	if !sn.dropWild(acc, "inj-1") {
		t.Fatal("dropWild 应返回 true")
	}
	got := sn.getWild(acc)
	if len(got.Pets) != 2 {
		t.Fatalf("撤销一只后应剩 2 只,实得 %d", len(got.Pets))
	}
	for _, p := range got.Pets {
		if p.ID == "inj-1" {
			t.Error("被撤销的那只不该还在")
		}
	}
	// 撤销不存在的 id 返回 false(前端据此判断 404)
	if sn.dropWild(acc, "nope") {
		t.Error("撤销不存在的 id 应返回 false")
	}
}

// TestWildPayloadInjectFieldsJSON 守广播字段的 JSON 名不变。
// 前端按 inject / injectRevoke 做增量处理,改名会静默失效
// (表现为撤销后标记不消失、或投放后又丢一批)。
func TestWildPayloadInjectFieldsJSON(t *testing.T) {
	b, err := json.Marshal(WildPayload{Inject: true, InjectRevoke: "x"})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["inject"]; !ok {
		t.Error("缺少 inject 字段")
	}
	if _, ok := m["injectRevoke"]; !ok {
		t.Error("缺少 injectRevoke 字段")
	}
}
