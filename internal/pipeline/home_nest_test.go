package pipeline

// —— 学院小窝「真值」自动维护的验收(实现见 home.go 的 syncAcademyNest)——
//
// 为什么值得单测:这套玩法在页面上只表现为**一个百分比**。真值写错时(认了好友家园的窝、拿别人的
// 宠物当自己的、每次家园消息都刷一遍库、或误把精灵小窝当学院小窝)不报错、不红任何编译,只会让
// 那个数字悄悄不对 —— 正是那种「没人会发现」的错,故这里把六种情形钉死:
//   自家 → 写;换窝 → 更新;空窝 → 清零;好友家 → 不写不动;归属判不了 → 只增不清;非学院小窝 → 不管。
//
// 报文按 scene.ParseHomeInfo / ParseSceneActors / ParseActorEnter 的字段号手拼(与
// scene/home_test.go、wilds_debounce_test.go 同一套路,不引 pcap 也不用 pb 生成物)。

import (
	"testing"

	"github.com/zxsos/rocom-go-parse/gcp"
	"github.com/zxsos/rocom-go/internal/pet"
	"github.com/zxsos/rocom-go/internal/scene"
	"google.golang.org/protobuf/encoding/protowire"
)

const (
	// testAcademyCfg / testFairyCfg 对应 gamedata.AcademyNestConfigID 与精灵小窝 —— 后者用来确认
	// 「只认学院小窝」:两者都是家具、都是小窝,只有前者才是性格 100% 遗传的那只。
	testAcademyCfg = 1001072
	testFairyCfg   = 1001071

	// testAcademyGUID 是学院小窝那件家具的 guid;精灵小窝另给一个,避免撞上。
	testAcademyGUID = 944829128083705640
	testFairyGUID   = 706055450669263513

	// nestAccountUID 是 newTestPipeline 里 login(t, p, 1) 的 uid(testAcc == "UID:1")。
	nestAccountUID = 1
)

func nestVar(num protowire.Number, v uint64) []byte {
	return protowire.AppendVarint(protowire.AppendTag(nil, num, protowire.VarintType), v)
}

func nestMsg(num protowire.Number, sub []byte) []byte {
	return protowire.AppendBytes(protowire.AppendTag(nil, num, protowire.BytesType), sub)
}

func nestXYZ(x, y int32) []byte {
	return append(nestVar(1, uint64(uint32(x))), nestVar(2, uint64(uint32(y)))...)
}

// nestFurniture 一件家具:guid(1)/item_gid(4)/config_id(5)/position(6).pos(1)。
func nestFurniture(guid uint64, cfg uint32) []byte {
	b := nestVar(1, guid)
	b = append(b, nestVar(4, 1)...)
	b = append(b, nestVar(5, uint64(cfg))...)
	return append(b, nestMsg(6, nestMsg(1, nestXYZ(0, 0)))...)
}

// nestSnapshotPet 进场景快照里的实体:other_actors(7) → ActorInfo{npc(11){base(1){actor_id(2),
// pt(8).pos(1)}, home_pet(22).home_pet_info(1){pet_gid(1), furniture_guid(3)}}}。
// 字段号照 scene.ParseSceneActors 与 parseHomePet 的口径 —— furniture_guid 对上哪件家具,
// 就表示它住在哪个窝里。
func nestSnapshotPet(actorID uint64, petGid uint32, guid uint64) []byte {
	base := nestVar(2, actorID)
	base = append(base, nestMsg(8, nestMsg(1, nestXYZ(0, 0)))...)
	hp := nestVar(1, uint64(petGid))
	hp = append(hp, nestVar(3, guid)...)
	npc := nestMsg(1, base)
	npc = append(npc, nestMsg(22, nestMsg(1, hp))...)
	return nestMsg(11, npc)
}

// nestSnapshotBody 拼 0x014a 的 AppBody:home_info(22){home_name(1), home_owner_id(2),
// home_level(4), room_level(5), room_layout(20)→rooms(1)→room_plane_list(20)→furniture_list(20)}
// + 若干 other_actors(7)。owner 传 0 表示这条快照**没带归属**(走弱判据的那一支)。
func nestSnapshotBody(owner uint64, nests [][]byte, actors [][]byte) []byte {
	var plane []byte
	for _, n := range nests {
		plane = append(plane, nestMsg(20, n)...)
	}
	hi := nestMsg(1, []byte("示例玩家")) // home_name
	if owner != 0 {
		hi = append(hi, nestVar(2, owner)...) // home_owner_id:判「是不是自己的家」
	}
	hi = append(hi, nestVar(4, 25)...) // home_level
	hi = append(hi, nestVar(5, 5)...)  // room_level
	hi = append(hi, nestMsg(20, nestMsg(1, nestMsg(20, plane)))...)
	body := nestMsg(22, hi)
	for _, a := range actors {
		body = append(body, nestMsg(7, a)...)
	}
	return body
}

// nestEnterBody AOI(0x0414)的进场部分:acts(1) → actor_enter(1) → actors(1, 重复 ActorInfo)。
func nestEnterBody(actors ...[]byte) []byte {
	var enter []byte
	for _, a := range actors {
		enter = append(enter, nestMsg(1, a)...)
	}
	return nestMsg(1, nestMsg(1, enter))
}

// nestLeaveBody AOI 的离场部分:acts(1) → actor_leave(2) → actor_ids(1, 重复)。
func nestLeaveBody(actorIDs ...uint64) []byte {
	var ids []byte
	for _, id := range actorIDs {
		ids = append(ids, nestVar(1, id)...)
	}
	return nestMsg(1, nestMsg(2, ids))
}

// nestAddPet 往库里放一只宠物:弱判据(归属未知)要求住户确实在本账号宠物库里。
func nestAddPet(t *testing.T, p *Pipeline, gid uint32) {
	t.Helper()
	if _, err := p.st.For(testAcc).UpsertPet(&pet.Pet{
		Gid: gid, ConfID: 3006, BaseConfID: 3006, Species: "火神", Name: "小窝住户", Level: 10,
	}); err != nil {
		t.Fatalf("写宠物 %d: %v", gid, err)
	}
}

// TestAcademyNestAutoTruth 自家家园:住户被写成真值,换窝/空窝都跟着变。
func TestAcademyNestAutoTruth(t *testing.T) {
	p, _ := newTestPipeline(t)
	login(t, p, nestAccountUID)
	enter := func(body []byte) {
		t.Helper()
		p.handle(msg(gcp.S2C, scene.OpEnterSceneFinishAck, body))
	}
	nests := [][]byte{nestFurniture(testAcademyGUID, testAcademyCfg), nestFurniture(testFairyGUID, testFairyCfg)}

	// 自己家:学院小窝住 41991,精灵小窝住 8867 —— 只有前者进真值。
	enter(nestSnapshotBody(nestAccountUID, nests, [][]byte{
		nestSnapshotPet(11, 41991, testAcademyGUID),
		nestSnapshotPet(12, 8867, testFairyGUID),
	}))
	if got := p.st.AcademyGid(); got != 41991 {
		t.Fatalf("真值 = %d, 期望 41991(自家学院小窝的住户)", got)
	}

	// 换窝:同一件家具改成住 55555(精灵小窝那只不动)→ 跟着更新。
	enter(nestSnapshotBody(nestAccountUID, nests, [][]byte{
		nestSnapshotPet(11, 55555, testAcademyGUID),
		nestSnapshotPet(12, 8867, testFairyGUID),
	}))
	if got := p.st.AcademyGid(); got != 55555 {
		t.Errorf("换窝后真值 = %d, 期望 55555", got)
	}

	// 空窝:家具还在、住户没了 → 清零(「家具在但空着」才算空窝,见 syncAcademyNest 的注释)。
	enter(nestSnapshotBody(nestAccountUID, nests, [][]byte{nestSnapshotPet(12, 8867, testFairyGUID)}))
	if got := p.st.AcademyGid(); got != 0 {
		t.Errorf("空窝后真值 = %d, 期望 0", got)
	}

	// AOI 增量才是常态:站在自己家里把宠物抱进/抱出小窝,不会重发进场景快照。
	p.handle(msg(gcp.S2C, scene.OpPlayActsNotify, nestEnterBody(nestSnapshotPet(11, 41991, testAcademyGUID))))
	if got := p.st.AcademyGid(); got != 41991 {
		t.Errorf("AOI 放进小窝后真值 = %d, 期望 41991", got)
	}
	p.handle(msg(gcp.S2C, scene.OpPlayActsNotify, nestLeaveBody(11)))
	if got := p.st.AcademyGid(); got != 0 {
		t.Errorf("AOI 抱走后真值 = %d, 期望 0", got)
	}
}

// TestAcademyNestOnlyAcademyConfig 只认学院小窝:自家家园里只有精灵小窝时不写。
//
// 这条是防「按 NestFurniture 命中就写」那种写法 —— 精灵小窝也有住户,但它没有性格 100% 的加成,
// 认错了会让页面上凭空多出几个必中的建议。
func TestAcademyNestOnlyAcademyConfig(t *testing.T) {
	p, _ := newTestPipeline(t)
	login(t, p, nestAccountUID)

	p.handle(msg(gcp.S2C, scene.OpEnterSceneFinishAck, nestSnapshotBody(nestAccountUID,
		[][]byte{nestFurniture(testFairyGUID, testFairyCfg)},
		[][]byte{nestSnapshotPet(12, 8867, testFairyGUID)})))
	if got := p.st.AcademyGid(); got != 0 {
		t.Fatalf("真值 = %d, 期望 0(只有精灵小窝,不该写)", got)
	}
}

// TestAcademyNestIgnoresFriendHome 访问好友家园不能污染真值:那里也有学院小窝,但住的是对方的宠物。
func TestAcademyNestIgnoresFriendHome(t *testing.T) {
	p, _ := newTestPipeline(t)
	login(t, p, nestAccountUID)

	// 先在自己的家设好
	p.handle(msg(gcp.S2C, scene.OpEnterSceneFinishAck, nestSnapshotBody(nestAccountUID,
		[][]byte{nestFurniture(testAcademyGUID, testAcademyCfg)},
		[][]byte{nestSnapshotPet(11, 41991, testAcademyGUID)})))
	if got := p.st.AcademyGid(); got != 41991 {
		t.Fatalf("真值 = %d, 期望 41991", got)
	}

	// 站在好友家:owner 是对方 → 什么都不做(既不写也不清)
	const friendUID = 906803708
	p.handle(msg(gcp.S2C, scene.OpEnterSceneFinishAck, nestSnapshotBody(friendUID,
		[][]byte{nestFurniture(1234567890123456789, testAcademyCfg)},
		[][]byte{nestSnapshotPet(21, 77777, 1234567890123456789)})))
	if got := p.st.AcademyGid(); got != 41991 {
		t.Errorf("真值 = %d, 期望仍是 41991(好友家园里那只不能算自己的)", got)
	}

	// 好友家空窝同理:连清空都不该做(自己那只其实还在家里)
	p.handle(msg(gcp.S2C, scene.OpEnterSceneFinishAck, nestSnapshotBody(friendUID,
		[][]byte{nestFurniture(1234567890123456789, testAcademyCfg)}, nil)))
	if got := p.st.AcademyGid(); got != 41991 {
		t.Errorf("真值 = %d, 期望仍是 41991(别人的空窝与自己的设置无关)", got)
	}
}

// TestAcademyNestWeakFallback 快照没带归属(老包/字段缺失)时的弱判据:**只增不清**。
//
// 判不了归属时宁可留着旧值,也不能拿不确定的数据把真值抹掉或覆盖成好友家的宠物 —— 那两件事
// 都是静默的,玩家只会看到百分比莫名其妙地变了。
func TestAcademyNestWeakFallback(t *testing.T) {
	p, _ := newTestPipeline(t)
	login(t, p, nestAccountUID)
	enter := func(actors [][]byte) {
		t.Helper()
		p.handle(msg(gcp.S2C, scene.OpEnterSceneFinishAck, nestSnapshotBody(0,
			[][]byte{nestFurniture(testAcademyGUID, testAcademyCfg)}, actors)))
	}

	// 住户不在自己库里 → 不写(多半是好友家的宠物)
	enter([][]byte{nestSnapshotPet(11, 77777, testAcademyGUID)})
	if got := p.st.AcademyGid(); got != 0 {
		t.Fatalf("真值 = %d, 期望 0(住户不在自己库里,不敢写)", got)
	}

	// 住户确实在自己库里 → 写
	nestAddPet(t, p, 41991)
	enter([][]byte{nestSnapshotPet(11, 41991, testAcademyGUID)})
	if got := p.st.AcademyGid(); got != 41991 {
		t.Errorf("真值 = %d, 期望 41991(住户在自己库里)", got)
	}

	// 空窝 → **不清**(归属判不了,宁可信旧值)
	enter(nil)
	if got := p.st.AcademyGid(); got != 41991 {
		t.Errorf("真值 = %d, 期望仍是 41991(判不了归属时只增不清)", got)
	}
}

// TestAcademyNestNoFurnitureKeepsValue 自己家里没有学院小窝这件家具时不动已有值。
//
// 与「家具在但空着」是两回事:前者多半是还没解锁这个玩法(甚至只是这一份快照没带全),
// 顺手清掉会把玩家手工设的值一起抹了。
func TestAcademyNestNoFurnitureKeepsValue(t *testing.T) {
	p, _ := newTestPipeline(t)
	login(t, p, nestAccountUID)

	p.handle(msg(gcp.S2C, scene.OpEnterSceneFinishAck, nestSnapshotBody(nestAccountUID,
		[][]byte{nestFurniture(testAcademyGUID, testAcademyCfg)},
		[][]byte{nestSnapshotPet(11, 41991, testAcademyGUID)})))
	if got := p.st.AcademyGid(); got != 41991 {
		t.Fatalf("真值 = %d, 期望 41991", got)
	}

	// 同一账号再进一次家(这一次家具列表里没有学院小窝了)→ 保持原值
	p.handle(msg(gcp.S2C, scene.OpEnterSceneFinishAck, nestSnapshotBody(nestAccountUID,
		[][]byte{nestFurniture(testFairyGUID, testFairyCfg)},
		[][]byte{nestSnapshotPet(12, 8867, testFairyGUID)})))
	if got := p.st.AcademyGid(); got != 41991 {
		t.Errorf("真值 = %d, 期望仍是 41991(没有这件家具时不动既有值)", got)
	}
}
