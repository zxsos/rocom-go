package pipeline

import (
	"testing"

	"google.golang.org/protobuf/encoding/protowire"

	"github.com/whoisnian/rocom-capture/internal/gcp"
	"github.com/whoisnian/rocom-capture/internal/pet"
	"github.com/whoisnian/rocom-capture/internal/store"
)


// —— 孵蛋器在孵标记:权威口径 egg_gid 的时序 ——
//
// 背景(见 internal/pet/egg.go 的 Hatching):蛋从孵蛋器取出后服务器**不清
// start_hatch_time**,只把进度清零。故判定必须走权威的 egg_gid 列表,而不能用蛋自己的字段。
//
// 权威列表有两个下发点:登录数据 0x0102(PetBackpackInfo.egg_gid)与开孵蛋器 0x0312。
// 但登录包**先于**背包分页到达 —— 那时库里还没有蛋,对账改不动任何行。
// 故 connState.hatchGids 把列表记住,等蛋从 0x1344 入库时逐颗判定。
// 本测试锁住这个时序:没有它,首次运行(空库)会全部判成"不在孵"。

// loginWithHatchBody 构造带孵蛋器占用列表的登录回包:body.field2 = LoginData,
// 其 player_info(2).pet_info(4).backpack_info(9) = PetBackpackInfo{egg_gid(1)…, boxes(3)}。
// boxes 需凑够 5 个非零 pet_gid,否则 bestBackpack 判为误解析。
func loginWithHatchBody(userID uint64, name string, eggGids []uint64) []byte {
	base := protowire.AppendTag(nil, 1, protowire.VarintType)
	base = protowire.AppendVarint(base, userID)
	base = protowire.AppendTag(base, 3, protowire.BytesType)
	base = protowire.AppendString(base, name)
	login := protowire.AppendTag(nil, 1, protowire.BytesType)
	login = protowire.AppendBytes(login, base)

	// PetBackpackInfo: egg_gid(1) 重复; boxes(3) 至少 5 只宠物
	box := protowire.AppendTag(nil, 1, protowire.VarintType)
	box = protowire.AppendVarint(box, 1) // box_id
	for _, g := range []uint64{6476, 12335, 11291, 18471, 266, 1503} {
		box = protowire.AppendTag(box, 3, protowire.VarintType)
		box = protowire.AppendVarint(box, g)
	}
	bp := protowire.AppendTag(nil, 3, protowire.BytesType)
	bp = protowire.AppendBytes(bp, box)
	for _, g := range eggGids {
		bp = protowire.AppendTag(bp, 1, protowire.VarintType)
		bp = protowire.AppendVarint(bp, g)
	}
	pi := protowire.AppendTag(nil, 9, protowire.BytesType)
	pi = protowire.AppendBytes(pi, bp)
	petInfo := protowire.AppendTag(nil, 4, protowire.BytesType)
	petInfo = protowire.AppendBytes(petInfo, pi)
	login = protowire.AppendTag(login, 2, protowire.BytesType)
	login = protowire.AppendBytes(login, petInfo)

	b := protowire.AppendTag(nil, 2, protowire.BytesType)
	return protowire.AppendBytes(b, login)
}

// TestHatchSlotsLoginBeforeBackpack 锁住「登录包先到、蛋后入库」的时序:
// 空库时收到登录包(带 egg_gid),随后背包分页送来蛋 —— 在孵标记应当立刻判对,
// 而不是全部为 0(那样页面会显示「孵蛋器 0/N」,与实际不符)。
func TestHatchSlotsLoginBeforeBackpack(t *testing.T) {
	p, _ := newTestPipeline(t)

	// 1) 登录先到:库里还没有蛋,但对账列表要记住
	p.handle(msg(gcp.S2C, pet.OpLoginRsp, loginWithHatchBody(1, "测试", []uint64{3259, 3262, 3264})))
	if got := p.conn(testSess).hatchGids; len(got) != 3 || !got[3259] || !got[3262] || !got[3264] {
		t.Fatalf("登录未记住孵蛋器列表: %v", got)
	}

	// 2) 背包分页随后到:三颗在孵的蛋 + 两颗不在孵的,全部首次入库
	p.handle(msg(gcp.S2C, pet.OpGetBagItemInfoByPageRsp,
		bagEggPageBody(1, 1, []uint32{3259, 3262, 3264, 4001, 4002})))

	eggs, err := p.st.For(testAcc).ListEggs(store.EggFilter{})
	if err != nil {
		t.Fatalf("读蛋: %v", err)
	}
	hatching := map[uint32]bool{}
	for _, e := range eggs {
		if e.Hatching {
			hatching[e.Gid] = true
		}
	}
	if len(eggs) != 5 {
		t.Fatalf("蛋数 = %d, want 5", len(eggs))
	}
	for _, g := range []uint32{3259, 3262, 3264} {
		if !hatching[g] {
			t.Errorf("gid %d 在登录的 egg_gid 里,应判为在孵", g)
		}
	}
	for _, g := range []uint32{4001, 4002} {
		if hatching[g] {
			t.Errorf("gid %d 不在 egg_gid 里,不该判为在孵", g)
		}
	}
}

// bagEggPageBody 构造 0x1344 背包分页回包:total_page(2)=page, req_page(3)=page,
// bag_info(4).item_list(3) 一组,type(1)=EggItemType, items(2) 为各蛋。
func bagEggPageBody(page uint32, total uint32, gids []uint32) []byte {
	var items []byte
	for _, g := range gids {
		item := protowire.AppendTag(nil, 1, protowire.VarintType)
		item = protowire.AppendVarint(item, uint64(g))
		item = protowire.AppendTag(item, 15, protowire.BytesType)
		item = protowire.AppendBytes(item, eggBriefBytes(g))
		items = protowire.AppendTag(items, 2, protowire.BytesType)
		items = protowire.AppendBytes(items, item)
	}
	list := protowire.AppendTag(nil, 1, protowire.VarintType)
	list = protowire.AppendVarint(list, pet.EggItemType)
	list = append(list, items...)

	bag := protowire.AppendTag(nil, 3, protowire.BytesType)
	bag = protowire.AppendBytes(bag, list)

	b := protowire.AppendTag(nil, 2, protowire.VarintType)
	b = protowire.AppendVarint(b, uint64(total))
	b = protowire.AppendTag(b, 3, protowire.VarintType)
	b = protowire.AppendVarint(b, uint64(page))
	b = protowire.AppendTag(b, 4, protowire.BytesType)
	return protowire.AppendBytes(b, bag)
}

// eggBriefBytes 拼一颗蛋的 PetEggBrief:conf_id(1)=3062001(小独角兽),max(6)=57600,
// 其余给非零值以通过 ParseBagEggs 的合法性检查。start_hatch(9) 故意都给 0 ——
// 真实场景里取出过的蛋反而带着非零的 start_hatch,那正是要靠 egg_gid 纠正的。
func eggBriefBytes(gid uint32) []byte {
	b := protowire.AppendTag(nil, 1, protowire.VarintType)
	b = protowire.AppendVarint(b, 3062001)
	b = protowire.AppendTag(b, 2, protowire.VarintType)
	b = protowire.AppendVarint(b, 42) // height
	b = protowire.AppendTag(b, 3, protowire.VarintType)
	b = protowire.AppendVarint(b, 14525) // weight
	b = protowire.AppendTag(b, 6, protowire.VarintType)
	b = protowire.AppendVarint(b, 57600) // max_secs
	return b
}
