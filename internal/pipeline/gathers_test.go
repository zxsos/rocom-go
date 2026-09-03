package pipeline

import (
	"testing"
	"time"

	"github.com/whoisnian/rocom-capture/internal/capture"
	"github.com/whoisnian/rocom-capture/internal/gamedata"
	"github.com/whoisnian/rocom-capture/internal/gcp"
	"github.com/whoisnian/rocom-capture/internal/scene"
	"google.golang.org/protobuf/encoding/protowire"
)

// 本文件锁住三件**错了也不报错、页面却完全正常**的事:
//
//   1. 采集物实体要能被认出来并推给前端(认不出来时页面只是「这一层一直是空的」,
//      与「附近真没有采集物」无从分辨);
//   2. 实体离开后必须撤掉标记(不撤会留一屏指向别处的假标记,玩家走过去扑空 ——
//      采集物采完会再刷,留着「最后所见」比不留更糟,这与野生宠的灰点语义相反);
//   3. 合并窗口要生效(实体进出是突发,逐条广播会把整份列表重发几十遍)。
//
// 走**真实解析路径**(构造 0x0414 消息 → handle),不直接调 observeGathers 手搓结构体:
// 实体解析、场景归属、合并窗口这三环任意一环断了,手搓一遍都测不出来。
//
// 样本点位取自 names.json 的真实采集物(见 gatherSamples),不写死假的刷新点 id ——
// 那样即使 GatherByRefresh 整个失灵(比如索引没建)测试也照样过。

// gatherSamples 从测试库的真实点位里挑几个品种各取一点。
// 写死 id 的风险:names.json 随版本重生成后 id 会变,那时测试就开始骗人了。
func gatherSamples(t *testing.T, db *gamedata.DB) []gamedata.POI {
	t.Helper()
	want := []string{"可可果树", "喵喵草", "无花果树"}
	got := make([]gamedata.POI, 0, len(want))
	for _, name := range want {
		found := false
		for _, p := range db.POIs(uint32(testRes)) {
			if p.K == "gather" && p.N == name && p.I != "" {
				got = append(got, p)
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("测试库里没有品种「%s」的采集物点位,换个品种或检查 names.json", name)
		}
	}
	return got
}

// gatherNpcBase 构造一个采集物 NPC 的 ActorInfo_NpcBase:只有 npc_cfg_id(1)
// 与 npc_content_cfg_id(10),**没有身高体重** —— 静态 NPC 的 npc_base 里就没有
// 那两个字段(见 scene.IsWildPet)。带上它们会被误判成野生宠物。
func gatherNpcBase(cfgID, refreshID int32) []byte {
	b := protowire.AppendTag(nil, 1, protowire.VarintType)
	b = protowire.AppendVarint(b, uint64(uint32(cfgID)))
	b = protowire.AppendTag(b, 10, protowire.VarintType)
	b = protowire.AppendVarint(b, uint64(uint32(refreshID)))
	return b
}

// actsBody 把若干 ActorInfo 包成 0x0414 的 actor_enter:
// acts(1) → actor_enter(1) → actors(1, 重复 ActorInfo)。
func actsBody(actors [][]byte) []byte {
	var enter []byte
	for _, a := range actors {
		enter = protowire.AppendTag(enter, 1, protowire.BytesType)
		enter = protowire.AppendBytes(enter, a)
	}
	acts := protowire.AppendTag(nil, 1, protowire.BytesType)
	acts = protowire.AppendBytes(acts, enter)
	body := protowire.AppendTag(nil, 1, protowire.BytesType)
	return protowire.AppendBytes(body, acts)
}

// leaveBody 包成 0x0414 的 actor_leave:acts(1) → actor_leave(2) → actor_ids(1, 重复)。
func leaveBody(ids ...uint64) []byte {
	var leave []byte
	for _, id := range ids {
		leave = protowire.AppendTag(leave, 1, protowire.VarintType)
		leave = protowire.AppendVarint(leave, id)
	}
	acts := protowire.AppendTag(nil, 2, protowire.BytesType)
	acts = protowire.AppendBytes(acts, leave)
	body := protowire.AppendTag(nil, 1, protowire.BytesType)
	return protowire.AppendBytes(body, acts)
}

// gatherActor 构造一条采集物实体的 ActorInfo(结构见 scene 包的 npcActorInfo)。
func gatherActor(actorID uint64, cfgID, refreshID, x, y, z int32) []byte {
	var base []byte
	base = protowire.AppendTag(base, 2, protowire.VarintType)
	base = protowire.AppendVarint(base, actorID)
	var pt []byte
	pt = protowire.AppendTag(pt, 1, protowire.BytesType)
	pt = protowire.AppendBytes(pt, posBody(x, y, z))
	base = protowire.AppendTag(base, 8, protowire.BytesType)
	base = protowire.AppendBytes(base, pt)

	npc := protowire.AppendTag(nil, 1, protowire.BytesType)
	npc = protowire.AppendBytes(npc, base)
	npc = protowire.AppendTag(npc, 3, protowire.BytesType)
	npc = protowire.AppendBytes(npc, gatherNpcBase(cfgID, refreshID))

	actor := protowire.AppendTag(nil, 11, protowire.BytesType)
	return protowire.AppendBytes(actor, npc)
}

// pushNow 把合并窗口推过:广播按 150ms 窗口攒着,不推进就永远发不出来。
// 窗口按**消息时间轴**判定(见 markGathersDirty),故再喂一条更晚的消息即可。
func pushNow(p *Pipeline) {
	p.handle(capture.Message{
		Time:      time.Now().Add(time.Second),
		Direction: gcp.C2S,
		Opcode:    scene.OpSceneMoveReq,
		Session:   testSess,
		AppBody:   moveBody(testX, testY, 0, 0, 0, true, 1001, nil),
	})
}

// actAt 在指定时刻喂一条 0x0414 实体通知。时刻可控才能测合并窗口 ——
// msg() 用 time.Now(),三条消息几乎同刻,构造不出「持续突发超过窗口」的情形。
func actAt(p *Pipeline, at time.Time, actors [][]byte) {
	p.handle(capture.Message{
		Time:      at,
		Direction: gcp.S2C,
		Opcode:    scene.OpPlayActsNotify,
		Session:   testSess,
		AppBody:   actsBody(actors),
	})
}

// —— 测试 ——

// TestGathersTrackedAndPushed 采集物实体进入视野后要出现在推送里,且带品种名与图标。
//
// 品种名/图标**不能**由前端猜(实体只给刷新点 id),故这里钉死「对得上」:
// 若 GatherByRefresh 失灵,标记还在但 n 是空串,前端只会画一个说不出是什么的点。
func TestGathersTrackedAndPushed(t *testing.T) {
	p, srv := newTestPipeline(t)
	samples := gatherSamples(t, p.db)
	login(t, p, 1)
	p.handle(msg(gcp.S2C, scene.OpEnterSceneRsp, enterSceneBody(1001, testRes, 0)))

	actors := make([][]byte, 0, len(samples))
	for i, s := range samples {
		// 实体坐标**故意与点位错开 3 米**:真实流量里实体是刷在候选点附近的,
		// 并不精确落在 AREA_CONF 的圆心上(实测不影响 20m 内的采走判定,但画出来
		// 应以实体为准)。坐标若写成与点位相同,「用实体坐标」与「用点位坐标」
		// 两种实现就无从分辨了。
		actors = append(actors, gatherActor(uint64(9000+i), 50041, s.R, s.X+300, s.Y-200, s.Z+50))
	}
	p.handle(msg(gcp.S2C, scene.OpPlayActsNotify, actsBody(actors)))
	pushNow(p)

	got := srv.GetLastGathers(testAcc)
	if got == nil {
		t.Fatal("没有 gathers 快照(实体没被认出来?)")
	}
	if len(got.Gathers) != len(samples) {
		t.Fatalf("标记数 = %d, 期望 %d", len(got.Gathers), len(samples))
	}
	if got.SceneResID != testRes {
		t.Errorf("sceneResId = %d, 期望 %d", got.SceneResID, testRes)
	}
	// 推送按 id 排序,与样本顺序无关,按 r 找回样本再逐个比对。
	byR := map[int32]gamedata.POI{}
	for _, s := range samples {
		byR[s.R] = s
	}
	for _, m := range got.Gathers {
		s, ok := byR[m.R]
		if !ok {
			t.Errorf("多出一个不认识的标记: r=%d n=%q", m.R, m.N)
			continue
		}
		if m.N != s.N {
			t.Errorf("r=%d 的品种名 = %q, 期望 %q —— 名与图标都来自点位表,对不上说明索引或取字段错了",
				m.R, m.N, s.N)
		}
		// 图标必须是**拼好的路径**而非原始文件名:点位表的 i 只是 BagItem 编号
		// (如 100211),直接透给前端会拼出不存在的 URL,图上就是一个破图。
		if want := p.db.POIIconOf(s.I); m.Icon != want {
			t.Errorf("r=%d 的图标 = %q, 期望 %q —— 原始文件名须经 POIIconOf 拼成 worldmap/<原名>.webp",
				m.R, m.Icon, want)
		}
		// 期望值是**实体坐标**(点位 +3m/-2m/+0.5m 那个),不是点位坐标:
		// 实体刷在候选点附近而非圆心上,画出来要以实体为准。
		if m.X != s.X+300 || m.Y != s.Y-200 || m.Z != s.Z+50 {
			t.Errorf("r=%d 坐标 = (%d,%d,%d), 期望 (%d,%d,%d) —— 应用实体自己的坐标,而非候选点圆心",
				m.R, m.X, m.Y, m.Z, s.X+300, s.Y-200, s.Z+50)
		}
		if m.U < 0 || m.U > 1 || m.V < 0 || m.V > 1 {
			t.Errorf("r=%d 投影 (u,v) = (%f,%f) 越界", m.R, m.U, m.V)
		}
	}
}

// TestGathersDroppedOnLeave 实体离开后标记必须当场撤掉,**不留灰点**。
//
// 这是本图层与野生宠最关键的差别:野生宠离开后要置灰保留 4 小时(刷得慢,走回去
// 多半还在),采集物采完会按刷新规则再刷,留灰点等于骗玩家「那儿还有」。
// 若哪天有人把 leave 改成「置灰」以图省事,这条会立刻炸。
func TestGathersDroppedOnLeave(t *testing.T) {
	p, srv := newTestPipeline(t)
	samples := gatherSamples(t, p.db)
	login(t, p, 1)
	p.handle(msg(gcp.S2C, scene.OpEnterSceneRsp, enterSceneBody(1001, testRes, 0)))

	s := samples[0]
	p.handle(msg(gcp.S2C, scene.OpPlayActsNotify, actsBody([][]byte{
		gatherActor(9001, 50041, s.R, s.X, s.Y, s.Z),
	})))
	pushNow(p)
	if got := srv.GetLastGathers(testAcc); got == nil || len(got.Gathers) != 1 {
		t.Fatalf("前置失败:进入后应有 1 个标记, 实际 %v", got)
	}

	p.handle(msg(gcp.S2C, scene.OpPlayActsNotify, leaveBody(9001)))
	pushNow(p)
	got := srv.GetLastGathers(testAcc)
	if got == nil {
		t.Fatal("离开后没有快照")
	}
	if len(got.Gathers) != 0 {
		t.Errorf("离开后仍有 %d 个标记(应全部撤掉): %+v", len(got.Gathers), got.Gathers)
	}

	// 再次下发同 id 的 enter 要能重新出现(采集物会再刷,撤掉不是永久拉黑)。
	p.handle(msg(gcp.S2C, scene.OpPlayActsNotify, actsBody([][]byte{
		gatherActor(9001, 50041, s.R, s.X, s.Y, s.Z),
	})))
	pushNow(p)
	got = srv.GetLastGathers(testAcc)
	if got == nil || len(got.Gathers) != 1 {
		t.Errorf("再次进入后标记数 = %d, 期望 1", len(got.Gathers))
	}
}

// TestGathersResetOnSceneChange 换场景必须整份作废,而不是留着等 leave。
//
// 传送时服务器**不**为旧实体补发离开事件(见 resetWilds 的实测),指望 leave 来清
// 就会留下一屏指向别处的标记 —— 它们投影到新底图上毫无意义。
func TestGathersResetOnSceneChange(t *testing.T) {
	p, srv := newTestPipeline(t)
	samples := gatherSamples(t, p.db)
	login(t, p, 1)
	p.handle(msg(gcp.S2C, scene.OpEnterSceneRsp, enterSceneBody(1001, testRes, 0)))

	s := samples[0]
	p.handle(msg(gcp.S2C, scene.OpPlayActsNotify, actsBody([][]byte{
		gatherActor(9001, 50041, s.R, s.X, s.Y, s.Z),
	})))
	pushNow(p)
	if got := srv.GetLastGathers(testAcc); got == nil || len(got.Gathers) != 1 {
		t.Fatalf("前置失败:进入后应有 1 个标记, 实际 %v", got)
	}

	// 换到魔法学院(10018,同样有底图):旧标记应立刻清空并推一条空的给前端清屏。
	p.handle(msg(gcp.S2C, scene.OpEnterSceneRsp, enterSceneBody(1002, 10018, 0)))
	got := srv.GetLastGathers(testAcc)
	if got == nil {
		t.Fatal("换场景后没有快照(应推一条空的把前端清屏)")
	}
	if len(got.Gathers) != 0 {
		t.Errorf("换场景后仍留着 %d 个标记: %+v", len(got.Gathers), got.Gathers)
	}
	if got.SceneResID != 10018 {
		t.Errorf("sceneResId = %d, 期望 10018", got.SceneResID)
	}
}

// TestGathersIgnoresUnknownRefresh 非采集物的实体不该被当成采集物。
//
// 刷新点 id 对不上采集物点位表的一律忽略 —— 星/零件/野生宠/玩家 avatar 都走同一条
// 0x0414 通道,认错的话这一层会瞬间被无关实体塞满。
func TestGathersIgnoresUnknownRefresh(t *testing.T) {
	p, srv := newTestPipeline(t)
	login(t, p, 1)
	p.handle(msg(gcp.S2C, scene.OpEnterSceneRsp, enterSceneBody(1001, testRes, 0)))

	p.handle(msg(gcp.S2C, scene.OpPlayActsNotify, actsBody([][]byte{
		gatherActor(9001, 50041, 1234567, testX, testY, 0), // 编造的刷新点 id
		gatherActor(9002, 55162, 8888888, testX, testY, 0), // 星星的 npc_cfg_id
	})))
	pushNow(p)

	got := srv.GetLastGathers(testAcc)
	if got == nil {
		t.Fatal("没有快照")
	}
	if len(got.Gathers) != 0 {
		t.Errorf("认出了 %d 个不该认的实体: %+v", len(got.Gathers), got.Gathers)
	}
}

// TestGathersDebounce 合并窗口:同一窗口内的连续变更只推一次。
//
// 实体进出是突发(实测一份 57s pcap 里采集物 enter 53 条、leave 30 条),逐条广播
// 会把整份列表重发几十遍,前端每秒重建几百个标记 —— 地图卡顿的根因。
//
// 用**同一时刻**的三批:窗口没到之前一次都不该推。若哪天把窗口去掉改成逐条推,
// 这里会立刻失败(而页面看起来完全正常,只是更卡)。
func TestGathersDebounce(t *testing.T) {
	p, srv := newTestPipeline(t)
	samples := gatherSamples(t, p.db)
	login(t, p, 1)
	p.handle(msg(gcp.S2C, scene.OpEnterSceneRsp, enterSceneBody(1001, testRes, 0)))

	t0 := time.Now()
	for i := 0; i < 3; i++ {
		s := samples[i%len(samples)]
		actAt(p, t0, [][]byte{gatherActor(uint64(9100+i), 50041, s.R, s.X, s.Y, s.Z)})
	}
	// 快照本身不是 nil —— 进场景时 resetGathers 已推过一条空的(给前端清屏)。
	// 故看标记数:窗口未到时它应仍是 0。
	if got := srv.GetLastGathers(testAcc); got != nil && len(got.Gathers) != 0 {
		t.Errorf("窗口未到就推了 %d 个标记(应攒到窗口结束再发)", len(got.Gathers))
	}

	// 推进窗口:三批应合并成一次全量发出。
	pushNow(p)
	got := srv.GetLastGathers(testAcc)
	if got == nil || len(got.Gathers) != 3 {
		t.Errorf("窗口到点后标记数 = %d, 期望 3(三批合并成一次全量)", len(got.Gathers))
	}
}

// TestGathersDebounceNotStarved 持续突发时窗口**不能饿死**:脏标记要记「最早」那次
// 变更的时刻,而不是每来一批就往后推。
//
// 这是窗口实现最容易写错的一处(见 markGathersDirty 的注释):若每批都把时刻刷新成
// 最新,而突发持续不断(实测实体进出约 4.8 次/秒、间隔常小于 150ms),窗口就永远
// 不会到点 —— 表现为**实时采集物这层一直是空的**,页面没有任何报错,极难排查。
//
// 构造方式:三批间隔 100ms(每批间隔小于窗口、总跨度大于窗口)。记「最早」时,
// 第三批到达时距首次变更已 200ms > 150ms,应当推出去。
func TestGathersDebounceNotStarved(t *testing.T) {
	p, srv := newTestPipeline(t)
	samples := gatherSamples(t, p.db)
	login(t, p, 1)
	p.handle(msg(gcp.S2C, scene.OpEnterSceneRsp, enterSceneBody(1001, testRes, 0)))

	t0 := time.Now()
	for i := 0; i < 3; i++ {
		s := samples[i%len(samples)]
		actAt(p, t0.Add(time.Duration(i)*100*time.Millisecond),
			[][]byte{gatherActor(uint64(9200+i), 50041, s.R, s.X, s.Y, s.Z)})
	}
	// 不再额外推进:三批自身的时刻跨度(200ms)已超过窗口,此刻就该已经推过了。
	got := srv.GetLastGathers(testAcc)
	if got == nil || len(got.Gathers) != 3 {
		t.Errorf("持续突发后标记数 = %d, 期望 3 —— 窗口被无限推后(饿死)了", len(got.Gathers))
	}
}
