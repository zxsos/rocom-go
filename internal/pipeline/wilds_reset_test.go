package pipeline

import (
	"context"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protowire"

	"github.com/zxsos/roco-go/internal/gcp"
	"github.com/zxsos/roco-go/internal/pet"
	"github.com/zxsos/roco-go/internal/scene"
	"github.com/zxsos/roco-go/internal/server"
)

// 本文件锁住 resetWilds 的语义:**换场景与传送一律整份作废**。
//
//   - **真换场景(res 变了)整份作废** —— 那些坐标属于另一张图,留着只会越攒越多;
//   - **同场景内传送(res 不变)同样整份作废** —— 这一条**与上游 deed645 相反**(上游判定
//     res 没变就只置灰留着),是有意为之:每个灰点都是一个带 grayscale 滤镜的 DOM 节点,
//     而大地图传送点 ↔ 营地魔力之源这类同场景传送在跑图时高频发生,攒起来会直接拖慢地图
//     (完整理由见 resetWilds 的注释)。若有人照着上游把置灰改回来,这里的用例会立刻炸。
//
// 实测依据(2026-09-01,PCAPdroid_01_9月_19_12_52.pcap 的 0x015c):
// 同场景内传送(from_res 10003 → to_res 10003),传送后 3 秒内 actor_enter 8 条、
// **actor_leave 0 条** —— 服务器传送时不为旧实体补发离开事件,故这一步必须由我们作废,
// 没有 leave 可等。

// wildCount 统计当前观测态里的标记数(含已置灰的)。
//
// 注意:手工构造 wildPet 时**必须给 seenAt**,否则 pushWilds 会按
// 「now - seenAt(零值) > 4h TTL」把它当过期灰点删掉(见 pushWilds 的清理逻辑)。
func wildCount(p *Pipeline) (n int, stale int) {
	ts := p.conn(testSess).wilds
	if ts == nil {
		return 0, 0
	}
	for _, w := range ts.pets {
		n++
		if w.left {
			stale++
		}
	}
	for _, w := range ts.all {
		n++
		if w.left {
			stale++
		}
	}
	return n, stale
}

// TestWildsDroppedOnSceneChange 真换场景(res 变了)时整份作废 —— 那些坐标属于另一张图,
// 留着只会越攒越多(灰点 4h 才过期),且投影到新底图上毫无意义。
func TestWildsDroppedOnSceneChange(t *testing.T) {
	p, _ := newTestPipeline(t)
	login(t, p, 1)
	p.handle(msg(gcp.S2C, scene.OpEnterSceneRsp, enterSceneBody(1001, testRes, 0)))
	// 造两只标记(一只稀有、一只普通)
	now := time.Now()
	p.conn(testSess).wilds.pets[111] = &wildPet{actorID: 111, cfgID: int32(testNpcCfgID), seenAt: now}
	p.conn(testSess).wilds.all[222] = &wildPet{actorID: 222, cfgID: int32(testNpcCfgID), seenAt: now}
	if n, _ := wildCount(p); n != 2 {
		t.Fatalf("前置:应有 2 只标记,实际 %d", n)
	}

	// 换到另一个场景
	p.handle(msg(gcp.S2C, scene.OpEnterSceneRsp, enterSceneBody(1002, 20001, 0)))

	// 稀有与普通两个通道都要清:只清一个会出现「稀有没了、普通还挂着」的不一致。
	if n, _ := wildCount(p); n != 0 {
		t.Errorf("换场景后应一只不剩(属于别的图的坐标),实际 %d", n)
	}
	// 观测态要建在新 res 名下,否则新场景看到的宠会投到旧图上
	if got := p.conn(testSess).wilds.res; got != 20001 {
		t.Errorf("换场景后 res 应更新为 20001,实际 %d", got)
	}
	_ = pet.OpLoginRsp
}

// TestWildsDroppedOnCrossSceneTeleport 跨场景传送同样整份作废(与换场景同理)。
//
// 与 TestWildsDroppedOnSameSceneTeleport 是同一条 0x015c 的两种落点:两种都整份作废。
func TestWildsDroppedOnCrossSceneTeleport(t *testing.T) {
	p, _ := newTestPipeline(t)
	login(t, p, 1)
	p.handle(msg(gcp.S2C, scene.OpEnterSceneRsp, enterSceneBody(1001, testRes, 0)))
	p.conn(testSess).wilds.pets[777] = &wildPet{actorID: 777, cfgID: int32(testNpcCfgID), seenAt: time.Now()}
	p.conn(testSess).wilds.all[888] = &wildPet{actorID: 888, cfgID: int32(testNpcCfgID), seenAt: time.Now()}

	// 跨场景传送(落地 res 与当前 res 不同)
	p.handle(msg(gcp.S2C, scene.OpTeleportNotify, teleportBody(testRes, 20001)))

	if n, _ := wildCount(p); n != 0 {
		t.Errorf("跨场景传送后应一只不剩,实际 %d", n)
	}
	if got := p.conn(testSess).wilds.res; got != 20001 {
		t.Errorf("跨场景传送后 res 应为 20001,实际 %d", got)
	}
}

// TestWildsDroppedOnSameSceneTeleport 同场景内传送同样整份作废。
//
// 这是与上游 deed645 分道扬镳的地方:上游判定 res 没变就只置灰留着,本仓库不采纳
// (理由见 resetWilds 的注释与文件头)。用例与 TestWildsDroppedOnCrossSceneTeleport
// 是同一条 0x015c 的两种落点,两种都作废。
func TestWildsDroppedOnSameSceneTeleport(t *testing.T) {
	p, srv := newTestPipeline(t)
	login(t, p, 1)
	p.handle(msg(gcp.S2C, scene.OpEnterSceneRsp, enterSceneBody(1001, testRes, 0)))
	// 稀有与普通各一只:作废要两个通道都覆盖,不能只清稀有的
	p.conn(testSess).wilds.pets[333] = &wildPet{actorID: 333, cfgID: int32(testNpcCfgID), seenAt: time.Now()}
	p.conn(testSess).wilds.all[444] = &wildPet{actorID: 444, cfgID: int32(testNpcCfgID), seenAt: time.Now()}

	// 订阅广播,确认传送后确实推了一次(前端据此清屏)
	pop, cancel := srv.Hub().SubscribeForTest()
	defer cancel()
	typed := make(chan string, 32)
	go func() {
		for {
			typ, ok := pop(context.Background())
			if !ok {
				return
			}
			typed <- typ
		}
	}()

	// 同场景传送(teleportBody 为真实 0x015c 的结构:from/to 的 res 相同)
	p.handle(msg(gcp.S2C, scene.OpTeleportNotify, teleportBody(testRes, testRes)))

	// 连灰点都不该剩:传送后真回到那一带遇上了,服务器会重新下发实体(0x014a/0x0413),
	// 不需要旧坐标留着 —— 而灰点每个都是带 grayscale 滤镜的 DOM 节点,攒起来拖慢地图。
	if n, stale := wildCount(p); n != 0 || stale != 0 {
		t.Errorf("同场景传送后应一只不剩(含灰点),实际 %d 只 / %d 灰", n, stale)
	}
	if !sawTyped(typed, "wildpets") {
		t.Error("传送后应广播 wildpets,让前端清屏")
	}
	_ = server.WildPayload{}
}

// teleportBody 构造 0x015c 传送通知(字段号见 scene.ParseTeleport):
// cfg_id(11)、res_id(12)、room(31)、to_pt(14){pos(1), dir(2)}。
func teleportBody(fromRes, toRes int32) []byte {
	var b []byte
	b = protowire.AppendTag(b, 11, protowire.VarintType) // scene_cfg_id
	b = protowire.AppendVarint(b, 1001)
	b = protowire.AppendTag(b, 12, protowire.VarintType) // scene_res_cfg_id(落点)
	b = protowire.AppendVarint(b, uint64(toRes))
	b = protowire.AppendTag(b, 31, protowire.VarintType) // room
	b = protowire.AppendVarint(b, 0)

	var dir []byte
	dir = protowire.AppendTag(dir, 3, protowire.VarintType)
	dir = protowire.AppendVarint(dir, 0)
	var pos []byte
	pos = protowire.AppendTag(pos, 1, protowire.VarintType)
	pos = protowire.AppendVarint(pos, 510000)
	pos = protowire.AppendTag(pos, 2, protowire.VarintType)
	pos = protowire.AppendVarint(pos, 612000)
	pos = protowire.AppendTag(pos, 3, protowire.VarintType)
	pos = protowire.AppendVarint(pos, 1200)
	var pt []byte
	pt = protowire.AppendTag(pt, 1, protowire.BytesType) // to_pt.pos
	pt = protowire.AppendBytes(pt, pos)
	pt = protowire.AppendTag(pt, 2, protowire.BytesType) // to_pt.dir
	pt = protowire.AppendBytes(pt, dir)
	b = protowire.AppendTag(b, 14, protowire.BytesType) // to_pt
	return protowire.AppendBytes(b, pt)
}
