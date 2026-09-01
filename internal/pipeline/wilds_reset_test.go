package pipeline

import (
	"context"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protowire"

	"github.com/whoisnian/rocom-capture/internal/gcp"
	"github.com/whoisnian/rocom-capture/internal/pet"
	"github.com/whoisnian/rocom-capture/internal/scene"
	"github.com/whoisnian/rocom-capture/internal/server"
)

// 本文件锁住「换场景/传送不删标记,只有用户主动清空才删」这条语义。
//
// 实测依据(2026-09-01,PCAPdroid_01_9月_19_12_52.pcap 的 0x015c):
// 同场景内传送(from_res 10003 → to_res 10003),传送后 3 秒内 actor_enter 8 条、
// **actor_leave 0 条** —— 服务器传送时不为旧实体补发离开事件。此刻玩家眼里标记
// 还在图上,是我们自己抹掉的(resetWilds 原先无条件 newWildTracker)。
//
// 标记是「本次上线在这一带见过什么」的备忘,是否抹平应由用户决定,不该由系统代劳。

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

// TestWildsSurviveSceneChange 换场景后标记**仍在**(只置灰),不被删除。
func TestWildsSurviveSceneChange(t *testing.T) {
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

	n, stale := wildCount(p)
	if n != 2 {
		t.Errorf("换场景后标记应仍保留 2 只(只置灰),实际 %d", n)
	}
	if stale != 2 {
		t.Errorf("换场景后两只都应置灰,实际 %d", stale)
	}
	// 当前场景要跟到新 res,否则新场景看到的宠会投到旧图上
	if got := p.conn(testSess).wilds.res; got != 20001 {
		t.Errorf("换场景后 res 应更新为 20001,实际 %d", got)
	}
	_ = pet.OpLoginRsp
}

// TestWildsSurviveTeleport 同场景内传送后标记仍在且置灰 —— 用户报的场景。
func TestWildsSurviveTeleport(t *testing.T) {
	p, srv := newTestPipeline(t)
	login(t, p, 1)
	p.handle(msg(gcp.S2C, scene.OpEnterSceneRsp, enterSceneBody(1001, testRes, 0)))
	p.conn(testSess).wilds.pets[333] = &wildPet{actorID: 333, cfgID: int32(testNpcCfgID), seenAt: time.Now()}

	// 订阅广播,确认传送后确实推了一次(前端据此把标记转为灰点)
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

	if n, stale := wildCount(p); n != 1 || stale != 1 {
		t.Errorf("传送后应保留 1 只且置灰,实际 %d 只 / %d 灰", n, stale)
	}
	if !sawTyped(typed, "wildpets") {
		t.Error("传送后应广播 wildpets,让前端把标记转为灰点")
	}
	_ = server.WildPayload{}
}

// TestWildsClearRemovesAll 用户主动清空:标记(含灰点)全部删除。
func TestWildsClearRemovesAll(t *testing.T) {
	p, _ := newTestPipeline(t)
	login(t, p, 1)
	p.handle(msg(gcp.S2C, scene.OpEnterSceneRsp, enterSceneBody(1001, testRes, 0)))
	now := time.Now()
	p.conn(testSess).wilds.pets[444] = &wildPet{actorID: 444, cfgID: int32(testNpcCfgID), seenAt: now}
	p.conn(testSess).wilds.all[555] = &wildPet{actorID: 555, cfgID: int32(testNpcCfgID), seenAt: now}
	// 先让它们都置灰(模拟换过场景),清空应连灰点一起删
	p.resetWilds(testSess, testAcc, testRes, time.Now())
	if n, _ := wildCount(p); n != 2 {
		t.Fatalf("前置:应有 2 只,实际 %d", n)
	}

	p.clearWildsForAccount(testAcc)

	if n, _ := wildCount(p); n != 0 {
		t.Errorf("主动清空后应一只不剩(含灰点),实际 %d", n)
	}
}

// TestWildsResetKeepsSeenAt 置灰不重置 seenAt:4h TTL 仍从最后一次确认算起。
func TestWildsResetKeepsSeenAt(t *testing.T) {
	p, _ := newTestPipeline(t)
	login(t, p, 1)
	p.handle(msg(gcp.S2C, scene.OpEnterSceneRsp, enterSceneBody(1001, testRes, 0)))
	first := time.Now().Add(-time.Hour)
	w := &wildPet{actorID: 666, cfgID: int32(testNpcCfgID), seenAt: first}
	p.conn(testSess).wilds.pets[666] = w

	p.resetWilds(testSess, testAcc, testRes, time.Now())

	if !w.seenAt.Equal(first) {
		t.Errorf("置灰不该改 seenAt: 原本 %v, 现在 %v", first, w.seenAt)
	}
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
