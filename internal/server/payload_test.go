package server

import (
	"encoding/json"
	"sort"
	"testing"
)

// 本文件钉住四个实时载荷的 **JSON 键名**。
//
// 存在理由:阶段 3 把这些载荷从 map[string]any 收成 struct,动机正是「键名是字符串字面量,
// 拼错一个前端读到 undefined 而 Go 编译照样通过」。但结构体化只防住了**读**的一侧
// (pos.U 写错编译报错),**写**的一侧 —— json tag —— 依然是无保护的字符串字面量。
//
// golden 测试能覆盖 REST 路径的键名,但广播专属字段(inject / injectRevoke / layerOnly)
// 不出现在任何 golden 里。故这里直接断言键集,四类载荷全覆盖,包括那些广播专属字段。
//
// 注意:必须把所有字段填成**非零值**,否则 omitempty 会省掉它们,断言就成了空转。

// keys 返回 JSON 对象的顶层键名(排序后)。
func keys(t *testing.T, v any) []string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func eq(t *testing.T, name string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s 键集不符\n  实际: %v\n  期望: %v", name, got, want)
		return
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("%s 键集不符\n  实际: %v\n  期望: %v", name, got, want)
			return
		}
	}
}

// TestPositionPayloadKeys 钉住位置载荷的键名。
// u/v/vu/vv 必须不缺席 —— 它们是前端定位与外推的依据,少一个箭头就不动或不跟手。
func TestPositionPayloadKeys(t *testing.T) {
	u, v, vu, vv := 0.5, 0.5, 0.1, 0.2
	p := PositionPayload{
		Account: "UID:1", SceneResID: 10003, SceneCfgID: 1001,
		SceneName: "卡洛西亚大陆", Img: "bigmap/10003.webp",
		X: 1, Y: 2, Z: 3, Heading: 1, Stop: true, Paintable: true,
		Ts: 1, TsMs: 1000,
		U: &u, V: &v, VU: &vu, VV: &vv,
		Path:      []PositionPoint{{U: 1, V: 2}},
		LayerOnly: true, Layer: map[string]any{"k": 1},
	}
	eq(t, "PositionPayload", keys(t, p), []string{
		"account", "heading", "img", "layer", "layerOnly", "paintable",
		"path", "sceneCfgId", "sceneName", "sceneResId", "stop",
		"ts", "tsMs", "u", "v", "vu", "vv", "x", "y", "z",
	})
}

// TestHomePayloadKeys 钉住家园载荷的键名。
// 元信息四字段(sceneResId/level/roomLevel/couplesStale)由内嵌 *HomeMeta 保证同进同退。
func TestHomePayloadKeys(t *testing.T) {
	pct := 1.0
	h := HomePayload{
		Account: "UID:1",
		Nests: []NestMark{{
			ID: "1", U: 1, V: 2, X: 3, Y: 4, Name: "精灵小窝",
			Pet: &NestPet{Gid: 1, Name: "小火", Species: "火神", Img: "a.webp",
				Gender: "♂", Level: 60, HeightM: 1.8, WeightKg: 92.5,
				HeightPct: &pct, WeightPct: &pct, Voice: 12, Nature: "固执",
				Talent: "S", FeedRound: 2, Mates: []NestMate{{Gid: 2, Name: "小水"}}},
			Egg: &NestEgg{ItemID: 5001, Name: "友爱天天的蛋", Icon: "egg/5001.webp"},
		}},
		HomeMeta: &HomeMeta{SceneResID: 10003, Level: 5, RoomLevel: 2, CouplesStale: true},
	}
	eq(t, "HomePayload", keys(t, h), []string{
		"account", "couplesStale", "level", "nests", "roomLevel", "sceneResId",
	})
}

// TestHomePayloadNoMeta 验证不在家园时元信息整体缺席 ——
// 若将来误把 HomeMeta 填成零值而非 nil,这四个键会凭空出现。
func TestHomePayloadNoMeta(t *testing.T) {
	h := HomePayload{Account: "UID:1", Nests: []NestMark{}}
	eq(t, "HomePayload(不在家园)", keys(t, h), []string{"account", "nests"})
}

// TestWildPayloadKeys 钉住野生宠载荷的键名,含广播专属的 inject / injectRevoke。
// allPets 与 pets 只差大小写,是最容易拼错的一对(JSON 里没有编译期保护)。
func TestWildPayloadKeys(t *testing.T) {
	pct := 98.5
	w := WildPayload{
		Account: "UID:1", SceneResID: 10003,
		Pets: []WildMark{{ID: "1", Name: "珀尔鼬", Img: "a.webp",
			Kinds: []string{"shiny"}, U: 1, V: 2, X: 3, Y: 4, Z: 5,
			Lv: 45, Voice: 96, Height: 120, Weight: 8800, WeightPct: &pct,
			GlassType: 1, Glass: "暗夜拾光", GlassValue: 131073,
			Mutation: 1, Stale: true, Inject: true}},
		AllPets:      []WildAllMark{{ID: "2", Name: "鸭吉吉", Img: "b.webp", U: 3, V: 4, Stale: true}},
		Inject:       true,
		InjectRevoke: "id-1",
	}
	eq(t, "WildPayload", keys(t, w), []string{
		"account", "allPets", "inject", "injectRevoke", "pets", "sceneResId",
	})
}

// TestFlowerPayloadKeys 钉住花种载荷的键名。
// cur / worlds 是内部字段:它们出现在 FlowerPayload(内部流转)上是对的,
// 但绝不能出现在 /api/flowers 的输出里 —— 那条由 flowerView 与
// TestContractFlowersHiddenFields 双重保证。
func TestFlowerPayloadKeys(t *testing.T) {
	f := FlowerPayload{
		Account: "UID:1",
		Flowers: []FlowerItem{{ID: 7001, Name: "火神", Star: 7}},
		Cur:     "self",
		Worlds:  FlowerWorlds{"self": &FlowerWorld{TS: 1, Flowers: []FlowerItem{{ID: 7001}}}},
	}
	eq(t, "FlowerPayload", keys(t, f), []string{"account", "cur", "flowers", "worlds"})

	// 对外输出只有 account/flowers
	v := flowerView{Account: "UID:1", Flowers: f.Flowers}
	eq(t, "flowerView", keys(t, v), []string{"account", "flowers"})
}
