package pipeline

import (
	"context"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protowire"

	"github.com/whoisnian/rocom-capture/internal/capture"
	"github.com/whoisnian/rocom-capture/internal/gcp"
	"github.com/whoisnian/rocom-capture/internal/scene"
	"github.com/whoisnian/rocom-capture/internal/server"
)

// 本文件锁住「野生宠物广播的合并窗口」——实时地图卡顿的根因修复。
//
// 背景(实测一份 604 秒 pcap):0x0414 共 11282 条,其中 2878 条带实体进出,约
// 4.8 次/秒;而每次 pushWilds 都把整份视野列表(平均 73 只宠、约 8.7KB)重发一遍,
// 合计 41.5 KB/s 下行、前端每秒重建数百个标记节点。更糟的是同一只宠反复进出 AOI
// 占 53.6%(1441 次进入只涉及 669 个实体)—— 大部分广播是白发的。
// 攒 150ms 再发可降到约 10%(窗口取值见 wildsDebounce 的实测表格)。
//
// 这类退化光看接口响应看不出来(数据始终是对的),只有广播**次数**能体现,故本
// 测试直接数 Hub 广播了几条 wildpets。
//
// **窗口如何推进**:没有独立定时器,完全由消息驱动(见 flushDirtyWilds)。故「变更
// 发生」与「广播出去」之间必然还隔着一条消息 —— 实时抓包下消息每秒十几条,延迟就
// 是十几毫秒;测试里必须自己补一条推进消息,否则永远发不出去。

// testNpcCfgID 是一个真实存在的 NPC_CONF.id(实测 gamedata.NpcPetBase 命中),
// observeWilds 的 catchable 判据要查这张表,随便编一个 id 会让 changed 恒为 false。
const testNpcCfgID = 10009

// actorEnterBody 构造一条 0x0414:acts(1) → actor_enter(1) → actors(1, 重复 ActorInfo)。
// ActorInfo = npc(11) → {base(1).pt(8).pos(1), npc_base(3)}。
// 必须带 npc_cfg_id 与**身高体重**:observeWilds 要 IsWildPet()(Height>0 && Weight>0)
// 且 CfgID 能在 NpcPetBase 查到,才判定 changed —— 少任一项都触发不了广播。
func actorEnterBody(actorID uint64, x, y int32) []byte {
	// base(1):actor_id(2)、pt(8)→pos(1);lv(11)
	var base []byte
	base = protowire.AppendTag(base, 2, protowire.VarintType)
	base = protowire.AppendVarint(base, actorID)
	var pt []byte
	pt = protowire.AppendTag(pt, 1, protowire.BytesType)
	pt = protowire.AppendBytes(pt, posBody(x, y, 0))
	base = protowire.AppendTag(base, 8, protowire.BytesType)
	base = protowire.AppendBytes(base, pt)

	// npc_base(3):npc_cfg_id(1)、height(11)、weight(12) —— 后两项是 IsWildPet 的判据
	var npcBase []byte
	npcBase = protowire.AppendTag(npcBase, 1, protowire.VarintType) // npc_cfg_id
	npcBase = protowire.AppendVarint(npcBase, testNpcCfgID)
	npcBase = protowire.AppendTag(npcBase, 11, protowire.VarintType) // height(÷100=米)
	npcBase = protowire.AppendVarint(npcBase, 80)
	npcBase = protowire.AppendTag(npcBase, 12, protowire.VarintType) // weight(÷1000=千克)
	npcBase = protowire.AppendVarint(npcBase, 12000)

	// npc(11) = {base(1), npc_base(3)}
	var npc []byte
	npc = protowire.AppendTag(npc, 1, protowire.BytesType)
	npc = protowire.AppendBytes(npc, base)
	npc = protowire.AppendTag(npc, 3, protowire.BytesType)
	npc = protowire.AppendBytes(npc, npcBase)

	var actor []byte
	actor = protowire.AppendTag(actor, 11, protowire.BytesType)
	actor = protowire.AppendBytes(actor, npc)

	var enter []byte
	enter = protowire.AppendTag(enter, 1, protowire.BytesType)
	enter = protowire.AppendBytes(enter, actor)

	var acts []byte
	acts = protowire.AppendTag(acts, 1, protowire.BytesType)
	acts = protowire.AppendBytes(acts, enter)

	b := protowire.AppendTag(nil, 1, protowire.BytesType)
	return protowire.AppendBytes(b, acts)
}

// wildCounter 订阅 Hub,持续在后台收 wildpets 广播并计数。
// 必须后台持续收:sub 的队列满了会丢旧保新(见 server.sub.push),不取走就数不准。
type wildCounter struct {
	pop  func(ctx context.Context) (string, bool)
	mu   sync.Mutex
	n    int
}

func newWildCounter(t *testing.T, srv *server.Server) *wildCounter {
	t.Helper()
	pop, cancel := srv.Hub().SubscribeForTest()
	c := &wildCounter{pop: pop}
	go func() {
		for {
			typ, ok := c.pop(context.Background()) // 阻塞直到取消
			if !ok {
				return
			}
			if typ == "wildpets" {
				c.mu.Lock()
				c.n++
				c.mu.Unlock()
			}
		}
	}()
	t.Cleanup(cancel)
	return c
}

// take 等一小会儿让异步计数落地,再取走并清零(返回自上次 take 以来的广播次数)。
// 计数在后台 goroutine 累加,handle 返回时它未必已记账,故取前先让一让。
func (c *wildCounter) take() int {
	time.Sleep(30 * time.Millisecond)
	c.mu.Lock()
	defer c.mu.Unlock()
	n := c.n
	c.n = 0
	return n
}

// pushAct 在 t 时刻送进一条带实体进出的 0x0414。
func pushAct(p *Pipeline, t time.Time, actorID uint64) {
	p.handle(capture.Message{
		Time: t, Direction: gcp.S2C, Opcode: scene.OpPlayActsNotify,
		Session: testSess, AppBody: actorEnterBody(actorID, testX, testY),
	})
}

// pushMove 在 t 时刻送进一条移动包(用它推进窗口:玩家跑动时它源源不断)。
func pushMove(p *Pipeline, t time.Time) {
	p.handle(capture.Message{
		Time: t, Direction: gcp.C2S, Opcode: scene.OpSceneMoveReq, Session: testSess,
		AppBody: moveBody(testX, testY, 0, 0, 4080, false, 1001, nil),
	})
}

// TestWildsDebounceMergesBurst 核心用例:一瞬间涌入多条实体进出,只广播一次。
//
// 这是「同一只宠反复进出」的极端情形(实测占 53.6%):窗口内 20 只宠依次进入,
// 若不合并就是 20 次全量广播。
func TestWildsDebounceMergesBurst(t *testing.T) {
	p, srv := newTestPipeline(t)
	login(t, p, 1)
	c := newWildCounter(t, srv)
	p.handle(msg(gcp.S2C, scene.OpEnterSceneRsp, enterSceneBody(1001, testRes, 0)))
	c.take() // 丢掉进场景那次立即广播(基线)

	base := time.Now()
	// 同一时刻涌入 20 条实体进出(全在窗口内)
	for i := 0; i < 20; i++ {
		pushAct(p, base, uint64(1000+i))
	}
	if n := c.take(); n != 0 {
		t.Fatalf("窗口内不该广播,实际 %d 次", n)
	}

	// 200ms 后推进 —— 用**绝对时间**而非 wildsDebounce 的倍数:若用倍数,窗口被改成
	// 30 秒时期望会跟着一起放大,这个测试就永远绿了(变异测试就是这么漏网的)。
	pushMove(p, base.Add(200*time.Millisecond))
	if n := c.take(); n != 1 {
		t.Errorf("窗口到点后应只广播 1 次(合并掉突发),实际 %d 次", n)
	}
}

// TestWildsDebounceKeepsSeparateBursts 两次相隔较远的突发应各发一次 ——
// 合并不能把「间隔许久的两件事」也吞掉,否则地图会长时间不更新。
func TestWildsDebounceKeepsSeparateBursts(t *testing.T) {
	p, srv := newTestPipeline(t)
	login(t, p, 1)
	c := newWildCounter(t, srv)
	p.handle(msg(gcp.S2C, scene.OpEnterSceneRsp, enterSceneBody(1001, testRes, 0)))
	c.take()

	// 间隔取**绝对 1 秒**(模拟玩家跑到下一片区域、那边又刷了一波宠)。
	// 用绝对值是刻意的:窗口 150ms 时 1 秒足以区分;若哪天窗口被调到 1 秒以上,
	// 两次突发就会被吞成一次 —— 地图更新延迟陡增,此测试必须变红。
	base := time.Now()
	pushAct(p, base, 2001)
	pushMove(p, base.Add(time.Second))
	if n := c.take(); n != 1 {
		t.Fatalf("第一次突发应发出 1 次,实际 %d 次", n)
	}
	pushAct(p, base.Add(2*time.Second), 2002)
	pushMove(p, base.Add(3*time.Second))
	if n := c.take(); n != 1 {
		t.Errorf("第二次突发应再发 1 次,实际 %d 次", n)
	}
}

// TestWildsResetBroadcastsImmediately 换场景/传送必须**立即**广播,不能等窗口:
// 玩家已经到了新场景,旧标记要当场清掉,否则会看到上个场景的宠残留几秒。
func TestWildsResetBroadcastsImmediately(t *testing.T) {
	p, srv := newTestPipeline(t)
	login(t, p, 1)
	c := newWildCounter(t, srv)
	p.handle(msg(gcp.S2C, scene.OpEnterSceneRsp, enterSceneBody(1001, testRes, 0)))
	c.take()

	base := time.Now()
	pushAct(p, base, 3001)
	// 紧接着换场景(仍在窗口内):必须当场广播
	p.handle(capture.Message{
		Time: base.Add(10 * time.Millisecond), Direction: gcp.S2C,
		Opcode: scene.OpEnterSceneRsp, Session: testSess,
		AppBody: enterSceneBody(1001, testRes, 0),
	})
	if n := c.take(); n < 1 {
		t.Errorf("换场景应立刻广播(清旧标记),实际 %d 次", n)
	}
	// 且换场景后不该再残留待发状态(否则紧接着还会白补一次全量)
	if !p.conns[testSess].wildsDirtyAt.IsZero() {
		t.Error("换场景后应清掉待发标记,否则会多补一次全量广播")
	}
}
