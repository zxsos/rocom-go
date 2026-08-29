package pipeline

import (
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protowire"

	"github.com/whoisnian/rocom-capture/internal/capture"
	"github.com/whoisnian/rocom-capture/internal/gamedata"
	"github.com/whoisnian/rocom-capture/internal/gcp"
	"github.com/whoisnian/rocom-capture/internal/pet"
	"github.com/whoisnian/rocom-capture/internal/scene"
	"github.com/whoisnian/rocom-capture/internal/server"
	"github.com/whoisnian/rocom-capture/internal/store"
)

// 本文件给管线补测试。
//
// 背景:本包原是 0% 覆盖(约 2700 行),而阶段 3 把 position / wildpets / home / flowers
// 四个载荷从 map[string]any 改成了 struct 时,改的正是这里。那次改动的验证只有一次手工
// 对比(跑 pcap、抓 SSE、与 HEAD 基线比),既不在 CI 里、也没留在代码里 —— 下次谁再改
// 这里,拿不到那次的结论。
//
// 测试策略:直接构造 capture.Message 喂给 handle()。Message 是纯数据结构,AppBody 是
// protobuf 字节,故用 protowire 手工拼装即可,不需要 pcap 文件、不需要真实网络。
// 断言落在「推给前端的载荷」上 —— 那正是对外契约,也是阶段 3 改坏的地方。
//
// 参照坐标:卡洛西亚大陆 res=10003,ox=306000 oy=408000 side=408000(见 names.json 的 maps),
// 地块正中即 (510000, 612000),投影后 u=v=0.5。

const (
	testRes  = 10003
	testAcc  = "UID:1"
	testX    = 510000 // 306000 + 408000/2
	testY    = 612000 // 408000 + 408000/2
	testSess = "sess-1"
)

func newTestPipeline(t *testing.T) (*Pipeline, *server.Server) {
	t.Helper()
	db, err := gamedata.Load()
	if err != nil {
		t.Fatalf("加载名称库: %v", err)
	}
	st, err := store.New(filepath.Join(t.TempDir(), "t.db"), db)
	if err != nil {
		t.Fatalf("打开数据库: %v", err)
	}
	srv := server.New(st, server.NewHub(), db, "", "", "")
	return New(st, db, srv), srv
}

// —— 消息构造 ——

func msg(dir gcp.Direction, op uint16, body []byte) capture.Message {
	return capture.Message{
		Time:      time.Now(),
		Direction: dir,
		Opcode:    op,
		Session:   testSess,
		AppBody:   body,
	}
}

// loginBody 构造 0x0102 登录回包:field2=LoginData{field1=base{field1=user_id, field3=name}}。
// ParseLoginAccount 按裸 wire 解析,故手工拼装即可,不必依赖 pb 生成物。
func loginBody(userID uint64, name string) []byte {
	base := protowire.AppendTag(nil, 1, protowire.VarintType)
	base = protowire.AppendVarint(base, userID)
	if name != "" {
		base = protowire.AppendTag(base, 3, protowire.BytesType)
		base = protowire.AppendString(base, name)
	}
	login := protowire.AppendTag(nil, 1, protowire.BytesType)
	login = protowire.AppendBytes(login, base)
	b := protowire.AppendTag(nil, 2, protowire.BytesType) // body.field2 = LoginData
	return protowire.AppendBytes(b, login)
}

// enterSceneBody 构造 0x0152 进入场景回包:field2=scene_cfg_id, field3=scene_res_cfg_id, field5=room。
func enterSceneBody(cfg, res, room int32) []byte {
	b := protowire.AppendTag(nil, 2, protowire.VarintType)
	b = protowire.AppendVarint(b, uint64(cfg))
	b = protowire.AppendTag(b, 3, protowire.VarintType)
	b = protowire.AppendVarint(b, uint64(res))
	b = protowire.AppendTag(b, 5, protowire.VarintType)
	b = protowire.AppendVarint(b, uint64(room))
	return b
}

// posBody 构造一个 Position 子消息(field1=x, field2=y, field3=z)。
func posBody(x, y, z int32) []byte {
	b := protowire.AppendTag(nil, 1, protowire.VarintType)
	b = protowire.AppendVarint(b, uint64(x))
	b = protowire.AppendTag(b, 2, protowire.VarintType)
	b = protowire.AppendVarint(b, uint64(y))
	b = protowire.AppendTag(b, 3, protowire.VarintType)
	b = protowire.AppendVarint(b, uint64(z))
	return b
}

// moveBody 构造 0x0133 移动包。segs 为轨迹点(会一并写 time_stamp 供 SegSpan 计算)。
func moveBody(x, y, z, yaw int32, speedX int32, stop bool, cfgID int32, segs []int32) []byte {
	b := protowire.AppendTag(nil, 1, protowire.VarintType) // time_stamp
	b = protowire.AppendVarint(b, 1000)

	b = protowire.AppendTag(b, 2, protowire.BytesType) // to_pos
	b = protowire.AppendBytes(b, posBody(x, y, z))

	b = protowire.AppendTag(b, 3, protowire.BytesType) // to_rot(z=yaw)
	b = protowire.AppendBytes(b, posBody(0, 0, yaw))

	if !stop {
		b = protowire.AppendTag(b, 4, protowire.BytesType) // speed
		b = protowire.AppendBytes(b, posBody(speedX, 0, 0))
	} else {
		b = protowire.AppendTag(b, 8, protowire.VarintType) // stop_move
		b = protowire.AppendVarint(b, 1)
	}

	for i, sx := range segs { // move_seg_list
		seg := protowire.AppendTag(nil, 1, protowire.BytesType) // seg.pos
		seg = protowire.AppendBytes(seg, posBody(sx, testY, z))
		seg = protowire.AppendTag(seg, 2, protowire.VarintType) // seg.time_stamp
		seg = protowire.AppendVarint(seg, uint64(1000+i*2500))
		b = protowire.AppendTag(b, 12, protowire.BytesType)
		b = protowire.AppendBytes(b, seg)
	}

	b = protowire.AppendTag(b, 17, protowire.VarintType) // scene_cfg_id
	b = protowire.AppendVarint(b, uint64(cfgID))
	return b
}

// login 登记账号归属:管线要求先见到登录回包才处理后续消息。
func login(t *testing.T, p *Pipeline, userID uint64) {
	t.Helper()
	p.handle(msg(gcp.S2C, pet.OpLoginRsp, loginBody(userID, "测试")))
	if got := p.connAccount[testSess]; got != testAcc {
		t.Fatalf("登录登记失败: connAccount=%q, 期望 %q", got, testAcc)
	}
}

// —— 测试 ——

// TestRegisterLogin 验证登录回包建立「连接 → 账号」映射,且未登录的消息被丢弃。
func TestRegisterLogin(t *testing.T) {
	p, srv := newTestPipeline(t)

	// 未登录时的消息不该产生任何推送
	p.handle(msg(gcp.C2S, scene.OpSceneMoveReq, moveBody(testX, testY, 0, 0, 0, true, 1001, nil)))
	if got := srv.GetLastPosition(testAcc); got != nil {
		t.Errorf("未登录却有位置推送: %+v", got)
	}

	login(t, p, 1)

	// 登录后同一条消息应被接受
	p.conn(testSess).res = testRes // 进入场景由 0x0152 设置,这里直接给
	p.handle(msg(gcp.C2S, scene.OpSceneMoveReq, moveBody(testX, testY, 0, 0, 0, true, 1001, nil)))
	if got := srv.GetLastPosition(testAcc); got == nil {
		t.Error("登录后仍无位置推送")
	}
}

// TestPositionFields 验证移动包推出去的载荷字段 —— 阶段 3 改的就是这里,
// 键名写错(如 allPets 写成 allpets)前端读到 undefined 而 Go 编译照样过,
// 故字段名必须由测试钉住。
func TestPositionFields(t *testing.T) {
	p, srv := newTestPipeline(t)
	login(t, p, 1)
	p.handle(msg(gcp.S2C, scene.OpEnterSceneRsp, enterSceneBody(1001, testRes, 0)))

	p.handle(msg(gcp.C2S, scene.OpSceneMoveReq, moveBody(testX, testY, 1200, 1235, 0, true, 1001, nil)))
	pos := srv.GetLastPosition(testAcc)
	if pos == nil {
		t.Fatal("无位置推送")
	}
	if pos.Account != testAcc {
		t.Errorf("account = %q, 期望 %q", pos.Account, testAcc)
	}
	if pos.SceneResID != testRes {
		t.Errorf("sceneResId = %d, 期望 %d", pos.SceneResID, testRes)
	}
	if pos.X != testX || pos.Y != testY || pos.Z != 1200 {
		t.Errorf("坐标 = (%d,%d,%d), 期望 (%d,%d,1200)", pos.X, pos.Y, pos.Z, testX, testY)
	}
	if pos.U == nil || pos.V == nil {
		t.Fatal("u/v 缺失(该场景有底图,应有投影坐标)")
	}
	// 地块正中 → u=v=0.5
	if abs(*pos.U-0.5) > 1e-9 || abs(*pos.V-0.5) > 1e-9 {
		t.Errorf("u,v = (%v,%v), 期望 (0.5,0.5)", *pos.U, *pos.V)
	}
	if pos.Heading != 123.5 { // yaw=1235 → 123.5 度
		t.Errorf("heading = %v, 期望 123.5", pos.Heading)
	}
	if !pos.Stop {
		t.Error("stop 应为 true")
	}
	if !pos.Paintable {
		t.Error("paintable 应为 true(卡洛西亚大陆可涂地)")
	}
}

// TestPositionSpeedAndPath 验证移动中带速度与轨迹、停下时两者都缺席。
func TestPositionSpeedAndPath(t *testing.T) {
	p, srv := newTestPipeline(t)
	login(t, p, 1)
	p.handle(msg(gcp.S2C, scene.OpEnterSceneRsp, enterSceneBody(1001, testRes, 0)))

	// 移动中:speed 非零 → 应有 vu/vv
	p.handle(msg(gcp.C2S, scene.OpSceneMoveReq, moveBody(testX, testY, 0, 0, 4080, false, 1001, nil)))
	pos := srv.GetLastPosition(testAcc)
	if pos.VU == nil || pos.VV == nil {
		t.Error("移动中缺 vu/vv")
	}
	if pos.Stop {
		t.Error("stop 应为 false")
	}

	// 停下:无 speed → vu/vv 应缺席(指针为 nil,不是 0)
	p.handle(msg(gcp.C2S, scene.OpSceneMoveReq, moveBody(testX, testY, 0, 0, 0, true, 1001, nil)))
	pos = srv.GetLastPosition(testAcc)
	if pos.VU != nil || pos.VV != nil {
		t.Error("停下时 vu/vv 应缺席")
	}
	if pos.Path != nil {
		t.Error("停下时 path 应缺席")
	}
}

// TestPositionNoMap 验证无底图场景只回坐标、不带 u/v —— 这是 buildPos 的早退分支,
// 漏掉它前端会拿不到坐标而整个地图页空白。
func TestPositionNoMap(t *testing.T) {
	p, srv := newTestPipeline(t)
	login(t, p, 1)

	// res=999999 在 names.json 里没有底图
	p.handle(msg(gcp.S2C, scene.OpEnterSceneRsp, enterSceneBody(1001, 999999, 0)))
	p.handle(msg(gcp.C2S, scene.OpSceneMoveReq, moveBody(testX, testY, 0, 0, 0, true, 1001, nil)))

	pos := srv.GetLastPosition(testAcc)
	if pos == nil {
		t.Fatal("无位置推送")
	}
	if pos.X != testX {
		t.Errorf("无底图时 x = %d, 期望 %d(坐标仍应回)", pos.X, testX)
	}
	if pos.U != nil || pos.V != nil {
		t.Error("无底图场景不应有 u/v")
	}
}

// TestPositionPathReplay 验证沉默后补报的轨迹会被下发(buildPos 的 path 分支)。
// 轨迹点需 ≥2 个才发,且末点会补上当前位置以与外推衔接。
func TestPositionPathReplay(t *testing.T) {
	p, srv := newTestPipeline(t)
	login(t, p, 1)
	p.handle(msg(gcp.S2C, scene.OpEnterSceneRsp, enterSceneBody(1001, testRes, 0)))

	// 两个轨迹点、间隔 2.5s(超过 SegSpan 阈值,应回放)
	p.handle(msg(gcp.C2S, scene.OpSceneMoveReq,
		moveBody(testX, testY, 0, 0, 0, true, 1001, []int32{testX - 2000, testX - 1000})))

	pos := srv.GetLastPosition(testAcc)
	if len(pos.Path) < 2 {
		t.Fatalf("path 点数 = %d, 期望 ≥2", len(pos.Path))
	}
	// 末点应补成当前位置(u=0.5),与外推无缝衔接
	last := pos.Path[len(pos.Path)-1]
	if abs(last.U-0.5) > 1e-9 {
		t.Errorf("path 末点 u = %v, 期望 0.5(当前位置)", last.U)
	}
}

// TestWhitelistBlocks 验证黑名单账号的消息被完全丢弃(不推送、不统计)。
func TestWhitelistBlocks(t *testing.T) {
	p, srv := newTestPipeline(t)
	login(t, p, 1)
	if err := p.st.SetRule(testAcc, "black", ""); err != nil {
		t.Fatalf("设黑名单: %v", err)
	}
	p.conn(testSess).res = testRes
	p.handle(msg(gcp.C2S, scene.OpSceneMoveReq, moveBody(testX, testY, 0, 0, 0, true, 1001, nil)))
	if got := srv.GetLastPosition(testAcc); got != nil {
		t.Errorf("黑名单账号仍有推送: %+v", got)
	}
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
