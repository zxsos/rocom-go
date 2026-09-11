package pipeline

import (
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protowire"

	"github.com/zxsos/rocom-go/internal/capture"
	"github.com/zxsos/rocom-go/internal/gcp"
	"github.com/zxsos/rocom-go/internal/scene"
)

// 本文件测「被牵着走/双人骑乘时的位置补位」(position.go 的 onVisitorPos)。
//
// 为什么需要它:被其他玩家牵着走时客户端**不发移动包(0x0133)** —— 位置由领队带动,
// 自己这一侧没有输入可报。此时箭头冻在原地,而涂地因为吃的是另一条数据(野生宠 AOI 通知)
// 仍在往前长,看上去就是「人在动、箭头不动」。修法是拿 s2c 访客流(0x02e6)里自己那一条
// 续上。判定逻辑的**边界**才是这里要钉住的:什么时候该接管、什么时候必须让位。

// 坐标口径同 pipeline_test.go:res=10003(卡洛西亚大陆)ox=306000 oy=408000 side=408000。
const (
	riderUin     = uint32(100000002) // 自己(pcap 里的「示例玩家」)
	otherUin     = uint32(100000003) // 同场另一位玩家(「好友甲」)
	riderX       = int32(454226)
	riderY       = int32(626990)
	riderZ       = int32(2017)
	riderYaw     = int32(-1523) // 服务器在 dir.z 里给的朝向(度×10)
	otherX       = int32(454423)
	otherY       = int32(627174)
	riderNetwork = int32(102)
)

// visitorBody 拼一条 0x02e6:visitor_info(1) 若干个,每个 {uin(1),network(2),pos(3)};
// pos = Point{pos(1), dir(2)},dir 里的 z 即朝向角×10。
func visitorBody(vs ...[]byte) []byte {
	var b []byte
	for _, v := range vs {
		b = protowire.AppendTag(b, 1, protowire.BytesType)
		b = protowire.AppendBytes(b, v)
	}
	return b
}

func visitorInfo(uin uint32, x, y, z, yaw int32) []byte {
	b := protowire.AppendTag(nil, 1, protowire.VarintType)
	b = protowire.AppendVarint(b, uint64(uin))
	b = protowire.AppendTag(b, 2, protowire.VarintType)
	b = protowire.AppendVarint(b, uint64(riderNetwork))

	point := protowire.AppendTag(nil, 1, protowire.BytesType)
	point = protowire.AppendBytes(point, posBody(x, y, z))
	point = protowire.AppendTag(point, 2, protowire.BytesType)
	point = protowire.AppendBytes(point, posBody(0, 0, yaw))

	b = protowire.AppendTag(b, 3, protowire.BytesType)
	return protowire.AppendBytes(b, point)
}

// enterSceneWithSelf 拼一条带 self_info 的进场景回包,顺便把 res 与 selfUin 都设上。
func enterSceneWithSelf(cfg, res int32, uin uint32) []byte {
	b := enterSceneBody(cfg, res, 0)
	base := protowire.AppendTag(nil, 3, protowire.VarintType)
	base = protowire.AppendVarint(base, uint64(uin))
	self := protowire.AppendTag(nil, 12, protowire.BytesType) // ActorInfo.avatar
	self = protowire.AppendBytes(self, protowire.AppendBytes(
		protowire.AppendTag(nil, 1, protowire.BytesType), base)) // avatar.base
	b = protowire.AppendTag(b, 11, protowire.BytesType) // ZoneEnterSceneRsp.self_info
	return protowire.AppendBytes(b, self)
}

// msgAt 构造指定时刻的消息(接管判定依赖包内时刻,必须可控)。
func msgAt(dir gcp.Direction, op uint16, body []byte, at time.Time) capture.Message {
	m := msg(dir, op, body)
	m.Time = at
	return m
}

// —— 测试 ——

// TestRiderTakesOverWhenMoveStops 是修复的核心:移动包停发超过 riderGap 后,
// 访客流应顶上,把箭头推到真实位置。
func TestRiderTakesOverWhenMoveStops(t *testing.T) {
	p, srv := newTestPipeline(t)
	login(t, p, 1)
	t0 := time.Now()

	// 进场景(带 self_info):这一条同时给出 res 与自己的 uin
	p.handle(msgAt(gcp.S2C, scene.OpEnterSceneRsp, enterSceneWithSelf(1001, testRes, riderUin), t0))
	if got := p.conn(testSess).selfUin; got != uint64(riderUin) {
		t.Fatalf("selfUin = %d, 期望 %d(进场景回包应拿到自己 uin)", got, riderUin)
	}

	// 玩家自己走了一下(移动包在发)
	p.handle(msgAt(gcp.C2S, scene.OpSceneMoveReq, moveBody(riderX, riderY, riderZ, 0, 0, true, 1001, nil), t0))

	// 停发 10 秒后,访客流带着新位置到达 —— 此时该接管
	later := t0.Add(10 * time.Second)
	p.handle(msgAt(gcp.S2C, scene.OpOnlineVisitorInfoNotify,
		visitorBody(
			visitorInfo(otherUin, otherX, otherY, 1986, -128),
			visitorInfo(riderUin, riderX+5000, riderY+3000, riderZ, riderYaw),
		), later))

	pos := srv.GetLastPosition(testAcc)
	if pos == nil {
		t.Fatal("停发后仍无位置推送")
	}
	if pos.X != riderX+5000 || pos.Y != riderY+3000 {
		t.Errorf("补位坐标 = (%d,%d), 期望 (%d,%d) —— 没跟上访客流",
			pos.X, pos.Y, riderX+5000, riderY+3000)
	}
	// 朝向应直接用服务器给的 dir.z,而不是 0(掰回 0 度会让箭头乱转)
	if got := int32(pos.Heading * 10); got != riderYaw {
		t.Errorf("heading = %d(度×10), 期望 %d(应取 pos.dir.z)", got, riderYaw)
	}
}

// TestRiderYieldsToMove 守另一半边界:自己正常操作时,1Hz 的访客流不该抢戏 ——
// 移动包峰值约 8 条/秒,比访客流细得多,让高频那侧为准。
func TestRiderYieldsToMove(t *testing.T) {
	p, srv := newTestPipeline(t)
	login(t, p, 1)
	t0 := time.Now()
	p.handle(msgAt(gcp.S2C, scene.OpEnterSceneRsp, enterSceneWithSelf(1001, testRes, riderUin), t0))
	p.handle(msgAt(gcp.C2S, scene.OpSceneMoveReq, moveBody(testX, testY, 0, 0, 0, true, 1001, nil), t0))

	// 移动包刚发过 1 秒(远小于 riderGap),此刻访客流报到别处:不该接管
	p.handle(msgAt(gcp.S2C, scene.OpOnlineVisitorInfoNotify,
		visitorBody(visitorInfo(riderUin, riderX, riderY, riderZ, riderYaw)),
		t0.Add(time.Second)))

	pos := srv.GetLastPosition(testAcc)
	if pos == nil {
		t.Fatal("无位置推送")
	}
	if pos.X != testX || pos.Y != testY {
		t.Errorf("坐标 = (%d,%d), 期望 (%d,%d) —— 移动包仍活跃,访客流不该接管",
			pos.X, pos.Y, testX, testY)
	}
}

// TestRiderNeedsSelfUin 守安全边界:不知道自己是谁时,绝不能拿别人的坐标当自己的
// (现实中会表现为箭头瞬移到别人身上,比冻着还糟)。
func TestRiderNeedsSelfUin(t *testing.T) {
	p, srv := newTestPipeline(t)
	login(t, p, 1)
	t0 := time.Now()
	// 进场景但**不带** self_info:selfUin 保持 0
	p.handle(msgAt(gcp.S2C, scene.OpEnterSceneRsp, enterSceneBody(1001, testRes, 0), t0))
	p.handle(msgAt(gcp.C2S, scene.OpSceneMoveReq, moveBody(testX, testY, 0, 0, 0, true, 1001, nil), t0))

	p.handle(msgAt(gcp.S2C, scene.OpOnlineVisitorInfoNotify,
		visitorBody(visitorInfo(otherUin, otherX, otherY, 1986, -128)),
		t0.Add(10*time.Second)))

	pos := srv.GetLastPosition(testAcc)
	if pos == nil {
		t.Fatal("无位置推送")
	}
	if pos.X != testX || pos.Y != testY {
		t.Errorf("坐标 = (%d,%d), 期望 (%d,%d) —— 不知自己 uin 时不得补位",
			pos.X, pos.Y, testX, testY)
	}
}

// TestRiderIgnoresOtherPlayers 守同一个边界的另一种情形:认得自己,但包里**没有**自己
// (比如只在别人的世界里才有访客流)。同样不该动。
func TestRiderIgnoresOtherPlayers(t *testing.T) {
	p, srv := newTestPipeline(t)
	login(t, p, 1)
	t0 := time.Now()
	p.handle(msgAt(gcp.S2C, scene.OpEnterSceneRsp, enterSceneWithSelf(1001, testRes, riderUin), t0))
	p.handle(msgAt(gcp.C2S, scene.OpSceneMoveReq, moveBody(testX, testY, 0, 0, 0, true, 1001, nil), t0))

	// 只有别人,没有自己
	p.handle(msgAt(gcp.S2C, scene.OpOnlineVisitorInfoNotify,
		visitorBody(visitorInfo(otherUin, otherX, otherY, 1986, -128)),
		t0.Add(10*time.Second)))

	pos := srv.GetLastPosition(testAcc)
	if pos == nil || pos.X != testX || pos.Y != testY {
		t.Errorf("坐标 = %+v, 期望停在 (%d,%d) —— 访客流里没有自己时不该动", pos, testX, testY)
	}
}

// TestRiderHeadingFallsBackToDelta 守朝向的兜底分支:服务器没给 dir(全零)但人确实在动时,
// 用位移方向兜底,不能掰回 0 度。
func TestRiderHeadingFallsBackToDelta(t *testing.T) {
	p, srv := newTestPipeline(t)
	login(t, p, 1)
	t0 := time.Now()
	p.handle(msgAt(gcp.S2C, scene.OpEnterSceneRsp, enterSceneWithSelf(1001, testRes, riderUin), t0))
	p.handle(msgAt(gcp.C2S, scene.OpSceneMoveReq, moveBody(testX, testY, 0, 0, 0, true, 1001, nil), t0))

	// dir 全零,但向东(+X)移动了 10 米 —— 朝向应为 0 度(世界 +X)
	p.handle(msgAt(gcp.S2C, scene.OpOnlineVisitorInfoNotify,
		visitorBody(visitorInfo(riderUin, testX+1000, testY, 0, 0)),
		t0.Add(10*time.Second)))

	pos := srv.GetLastPosition(testAcc)
	if pos == nil {
		t.Fatal("无位置推送")
	}
	if got := int32(pos.Heading * 10); got != 0 {
		t.Errorf("heading = %d(度×10), 期望 0(纯 +X 位移,方向即世界 +X)", got)
	}
}

// TestRiderKeepsHeadingWhenStill 守最后一条:站着不动且服务器没给朝向时,沿用上一次朝向,
// 别把箭头掰回 0 度(那会让箭头在停下的一瞬间乱转)。
func TestRiderKeepsHeadingWhenStill(t *testing.T) {
	p, srv := newTestPipeline(t)
	login(t, p, 1)
	t0 := time.Now()
	p.handle(msgAt(gcp.S2C, scene.OpEnterSceneRsp, enterSceneWithSelf(1001, testRes, riderUin), t0))
	p.handle(msgAt(gcp.C2S, scene.OpSceneMoveReq, moveBody(testX, testY, 0, 0, 0, true, 1001, nil), t0))

	// 先动一下,服务器给朝向 -1523
	p.handle(msgAt(gcp.S2C, scene.OpOnlineVisitorInfoNotify,
		visitorBody(visitorInfo(riderUin, testX+5000, testY, 0, riderYaw)),
		t0.Add(10*time.Second)))
	if pos := srv.GetLastPosition(testAcc); int32(pos.Heading*10) != riderYaw {
		t.Fatalf("前置:heading = %d, 期望 %d", int32(pos.Heading*10), riderYaw)
	}

	// 再停住:位置没变、服务器也没给朝向 —— 应沿用 -1523
	p.handle(msgAt(gcp.S2C, scene.OpOnlineVisitorInfoNotify,
		visitorBody(visitorInfo(riderUin, testX+5000, testY, 0, 0)),
		t0.Add(11*time.Second)))

	pos := srv.GetLastPosition(testAcc)
	if got := int32(pos.Heading * 10); got != riderYaw {
		t.Errorf("停下后 heading = %d(度×10), 期望沿用 %d", got, riderYaw)
	}
	if !pos.Stop {
		t.Error("停下时 stop 应为 true(不该再给速度让前端外推)")
	}
	if pos.VU != nil || pos.VV != nil {
		t.Error("停下时 vu/vv 应缺席")
	}
}

// TestRiderResumesAfterMoveReturns 验证接管是可逆的:移动包恢复后,访客流立刻让位
// (否则玩家夺回控制权后会有两个源在打架)。
func TestRiderResumesAfterMoveReturns(t *testing.T) {
	p, srv := newTestPipeline(t)
	login(t, p, 1)
	t0 := time.Now()
	p.handle(msgAt(gcp.S2C, scene.OpEnterSceneRsp, enterSceneWithSelf(1001, testRes, riderUin), t0))
	p.handle(msgAt(gcp.C2S, scene.OpSceneMoveReq, moveBody(testX, testY, 0, 0, 0, true, 1001, nil), t0))

	// 停发 10 秒 → 访客流接管
	p.handle(msgAt(gcp.S2C, scene.OpOnlineVisitorInfoNotify,
		visitorBody(visitorInfo(riderUin, riderX, riderY, riderZ, riderYaw)),
		t0.Add(10*time.Second)))
	if pos := srv.GetLastPosition(testAcc); pos.X != riderX {
		t.Fatalf("前置:接管后 x = %d, 期望 %d", pos.X, riderX)
	}

	// 玩家自己开始操作(移动包恢复)→ 之后紧跟着的访客流必须让位
	p.handle(msgAt(gcp.C2S, scene.OpSceneMoveReq, moveBody(testX, testY, 0, 0, 0, true, 1001, nil),
		t0.Add(11*time.Second)))
	p.handle(msgAt(gcp.S2C, scene.OpOnlineVisitorInfoNotify,
		visitorBody(visitorInfo(riderUin, riderX, riderY, riderZ, riderYaw)),
		t0.Add(12*time.Second)))

	pos := srv.GetLastPosition(testAcc)
	if pos.X != testX || pos.Y != testY {
		t.Errorf("移动包恢复后坐标 = (%d,%d), 期望 (%d,%d) —— 访客流应让位",
			pos.X, pos.Y, testX, testY)
	}
}
