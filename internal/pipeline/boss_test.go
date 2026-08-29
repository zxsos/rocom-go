package pipeline

import (
	"testing"
	"time"

	"github.com/whoisnian/rocom-capture/internal/server"
)

// 本文件覆盖花种(花灵 BOSS)相关的逻辑。
//
// 选这两个层次是有意的:
//   - 纯函数(cloneWorlds / slotFlowers / trimFriendWorlds / mergeFlowerDetail 等):
//     直接构造数据调用,不碰 protobuf,便宜且能钉住真实逻辑(如好友槽淘汰、详情继承)。
//   - setFlowers:花种写回的主路径,直接调用即可(不需构造 0x0375 消息),
//     能验证「当前列表」与「世界槽」的同步 —— 这是回访好友世界时标记不丢的关键。
//
// 不做的事:构造 0x0375 / 0x0338 的深层嵌套消息。那些解析由 scene 包负责,
// 构造成本高且易写错,收益不如上面这些。

func flower(id uint32, owner uint64) flowerItem {
	return flowerItem{ID: id, NpcLogicID: uint64(id) * 10, OwnerUserID: owner}
}

// TestFlowerWorldOwnerAndKey 验证世界归属判定与槽键生成。
// 0 = 自己世界;非 0 即世界归属者 user_id,槽键 "owner:<uid>" 需稳定唯一(回访要命中)。
func TestFlowerWorldOwnerAndKey(t *testing.T) {
	if got := flowerWorldOwner(nil); got != 0 {
		t.Errorf("空列表归属 = %d, 期望 0", got)
	}
	if got := flowerWorldOwner([]flowerItem{flower(1, 0), flower(2, 10001)}); got != 10001 {
		t.Errorf("归属 = %d, 期望 10001(取第一个非 0)", got)
	}
	if got := flowerOwnerKey(10001); got != "owner:10001" {
		t.Errorf("槽键 = %q, 期望 owner:10001", got)
	}
}

// TestCloneWorlds 验证浅复制:增删槽不污染原表(原表是 server 缓存里的共享数据)。
func TestCloneWorlds(t *testing.T) {
	src := server.FlowerWorlds{
		"self": &server.FlowerWorld{TS: 1, Flowers: []flowerItem{flower(1, 0)}},
	}
	cp := cloneWorlds(src)
	cp["owner:1"] = &server.FlowerWorld{TS: 2}
	delete(cp, "self")

	if len(src) != 1 {
		t.Errorf("原表被污染: 长度 %d, 期望 1", len(src))
	}
	if _, ok := src["self"]; !ok {
		t.Error("原表的 self 槽被删掉了")
	}
}

// TestSlotFlowers 验证读槽:槽不存在或为 nil 时返回 false(调用方据此决定是否新建)。
func TestSlotFlowers(t *testing.T) {
	worlds := server.FlowerWorlds{
		"self":    &server.FlowerWorld{TS: 100, Flowers: []flowerItem{flower(1, 0)}},
		"owner:1": nil, // 空槽(防御:不该 panic)
	}
	if items, ts, ok := slotFlowers(worlds, "self"); !ok || ts != 100 || len(items) != 1 {
		t.Errorf("self 槽 = (%v,%d,%v), 期望 (1项,100,true)", items, ts, ok)
	}
	if _, _, ok := slotFlowers(worlds, "owner:1"); ok {
		t.Error("nil 槽应返回 false")
	}
	if _, _, ok := slotFlowers(worlds, "owner:999"); ok {
		t.Error("不存在的槽应返回 false")
	}
}

// TestTrimFriendWorlds 验证好友槽超上限时淘汰最老的,且 self 槽永不被淘汰。
func TestTrimFriendWorlds(t *testing.T) {
	worlds := server.FlowerWorlds{"self": &server.FlowerWorld{TS: 1}}
	// 造 maxFriendWorlds+2 个好友槽,ts 从 10 递增
	for i := 0; i < maxFriendWorlds+2; i++ {
		k := flowerOwnerKey(uint64(100 + i))
		worlds[k] = &server.FlowerWorld{TS: int64(10 + i)}
	}
	trimFriendWorlds(worlds)

	if _, ok := worlds["self"]; !ok {
		t.Error("self 槽不该被淘汰")
	}
	friend := 0
	for k := range worlds {
		if k != "self" {
			friend++
		}
	}
	if friend != maxFriendWorlds {
		t.Errorf("好友槽剩 %d 个, 期望 %d", friend, maxFriendWorlds)
	}
	// 淘汰的是最老的:ts=10 与 ts=11 的那两个应已消失
	for _, old := range []string{flowerOwnerKey(100), flowerOwnerKey(101)} {
		if _, ok := worlds[old]; ok {
			t.Errorf("最老的槽 %q 应已被淘汰", old)
		}
	}
	// 最新的应保留
	if _, ok := worlds[flowerOwnerKey(100+maxFriendWorlds+1)]; !ok {
		t.Error("最新的好友槽不该被淘汰")
	}
}

// TestMergeFlowerDetail 验证已点过的 0x0338 详情会按 npc_logic_id 继承到新列表。
// 这是「退回拜访过的世界时标记不丢」的关键,也是整组重发时不被冲掉的保证。
func TestMergeFlowerDetail(t *testing.T) {
	prev := []flowerItem{
		{ID: 7001, NpcLogicID: 70010, Detail: true, Lv: 60,
			GlassType: 1, Glass: "暗夜拾光", GlassValue: 131073,
			BindName: "火神", MedalName: "大块头"},
		{ID: 7002, NpcLogicID: 0, Blood: 3, Detail: true, Lv: 50}, // 无 logic_id,走兜底键
	}
	next := []flowerItem{
		{ID: 7001, NpcLogicID: 70010},       // 应继承上一条
		{ID: 7002, NpcLogicID: 0, Blood: 3}, // 应走兜底键继承
		{ID: 7003, NpcLogicID: 70030},       // 新的,无详情
	}
	got := mergeFlowerDetail(next, prev)

	if !got[0].Detail || got[0].Lv != 60 || got[0].Glass != "暗夜拾光" ||
		got[0].GlassValue != 131073 || got[0].BindName != "火神" || got[0].MedalName != "大块头" {
		t.Errorf("详情未按 npc_logic_id 继承: %+v", got[0])
	}
	if !got[1].Detail || got[1].Lv != 50 {
		t.Errorf("详情未按 (id,blood) 兜底继承: %+v", got[1])
	}
	if got[2].Detail {
		t.Errorf("新花种不该凭空有详情: %+v", got[2])
	}

	// 旧列表里没有详情的,不继承
	noDetail := mergeFlowerDetail([]flowerItem{{ID: 7001, NpcLogicID: 70010}},
		[]flowerItem{{ID: 7001, NpcLogicID: 70010, Detail: false}})
	if noDetail[0].Detail {
		t.Error("旧值无详情时不该继承出详情")
	}
}

// TestSetFlowersSyncsSlot 验证 setFlowers 同时更新「当前列表」与「当前世界槽」,
// 这是回访该世界时恢复最新详情的前提。
//
// 注意 setFlowers 只**更新已存在的**槽,不新建 —— 建槽由 onBossNpcInfo 负责
// (它才知道世界归属)。这里先备好 self 槽再验证同步。
func TestSetFlowersSyncsSlot(t *testing.T) {
	p, srv := newTestPipeline(t)
	login(t, p, 1)

	srv.SetLastFlowers(testAcc, &server.FlowerPayload{
		Account: testAcc, Cur: "self",
		Flowers: []flowerItem{flower(7001, 0)},
		Worlds: server.FlowerWorlds{
			"self": &server.FlowerWorld{TS: 100, Flowers: []flowerItem{flower(7001, 0)}},
		},
	})
	p.setFlowers(testAcc, []flowerItem{flower(7002, 0)})

	got := srv.GetLastFlowers(testAcc)
	slot, _, ok := slotFlowers(got.Worlds, "self")
	if !ok {
		t.Fatal("self 槽不存在")
	}
	if len(slot) != 1 || slot[0].ID != 7002 {
		t.Errorf("self 槽内容 = %+v, 期望含 7002", slot)
	}
	if len(got.Flowers) != 1 || got.Flowers[0].ID != 7002 {
		t.Errorf("当前列表 = %+v, 期望含 7002", got.Flowers)
	}
	// 其它槽不受影响
	if _, _, ok := slotFlowers(got.Worlds, "owner:1"); ok {
		t.Error("不该凭空多出 owner:1 槽")
	}
}

// TestSetFlowersDoesNotMutateSharedSlot 验证 setFlowers 不原地改已发布的共享槽。
// 原地改会触发 Go map/slice 并发读写 fatal error(整个进程崩溃,不可 recover)。
func TestSetFlowersDoesNotMutateSharedSlot(t *testing.T) {
	p, srv := newTestPipeline(t)
	login(t, p, 1)

	srv.SetLastFlowers(testAcc, &server.FlowerPayload{
		Account: testAcc, Cur: "self",
		Flowers: []flowerItem{flower(7001, 0)},
		Worlds: server.FlowerWorlds{
			"self": &server.FlowerWorld{TS: time.Now().Unix(), Flowers: []flowerItem{flower(7001, 0)}},
		},
	})
	before := srv.GetLastFlowers(testAcc).Worlds["self"]

	p.setFlowers(testAcc, []flowerItem{flower(7002, 0)})

	// 旧槽对象不应被改写(它可能被 HTTP 读取方持有)
	if len(before.Flowers) != 1 || before.Flowers[0].ID != 7001 {
		t.Errorf("已发布的槽被原地改写: %+v", before.Flowers)
	}
	// 新值应在新对象上
	after := srv.GetLastFlowers(testAcc).Worlds["self"]
	if len(after.Flowers) != 1 || after.Flowers[0].ID != 7002 {
		t.Errorf("self 槽未更新: %+v", after.Flowers)
	}
}
