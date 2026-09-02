package pipeline

import (
	"strconv"
	"strings"
	"testing"

	"github.com/whoisnian/rocom-capture/internal/gamedata"
	"github.com/whoisnian/rocom-capture/internal/gcp"
	"github.com/whoisnian/rocom-capture/internal/scene"
	"google.golang.org/protobuf/encoding/protowire"
)

// 本文件锁住一件事:花种必须带上守护宠物的形态编号 baseConfId。
//
// 为什么单独测:花种卡片的色卡点开后,「在 rkpet 看 3D 效果」外链靠它拼 URL
// (rkpetURL 要求 p.baseConfId 非空),缺了这个字段按钮就不显示。此前 FlowerItem
// 压根没有这个字段 —— 花种页的色卡**从来**就没有过那条按钮,而页面看着完全正常。
//
// 这里走**真实解析路径**(构造 0x0375 消息 → onBossNpcInfo),而不是直接调
// onBossNpcInfo 手搓结构体:漏设字段正是本次的 bug,手搓一遍就测试不出来。
// 本文件打破了 boss_test.go 顶部「不构造 0x0375 消息」的约定 —— 那条约定是为了
// 省事,但这次被测的 bug 就在那一段里,不构造等于没测。

// bossNpcInfoBody 构造一条 s2c 0x0375 消息体:
// flower_npcs(2) → boss_npcs(1, repeated) → BossNpcInfo{npc_cfg_id(1),
// star(2), blood(3), battle_petbase_id(5), npc_logic_id(6)}。
func bossNpcInfoBody(npcCfgID, petBaseID uint32) []byte {
	n := protowire.AppendTag(nil, 1, protowire.VarintType)
	n = protowire.AppendVarint(n, uint64(npcCfgID))
	n = protowire.AppendTag(n, 2, protowire.VarintType)
	n = protowire.AppendVarint(n, 5) // star: 普通花灵
	n = protowire.AppendTag(n, 3, protowire.VarintType)
	n = protowire.AppendVarint(n, 1) // blood
	n = protowire.AppendTag(n, 5, protowire.VarintType)
	n = protowire.AppendVarint(n, uint64(petBaseID))
	n = protowire.AppendTag(n, 6, protowire.VarintType)
	n = protowire.AppendVarint(n, 7788) // npc_logic_id

	group := protowire.AppendTag(nil, 1, protowire.BytesType)
	group = protowire.AppendBytes(group, n)

	body := protowire.AppendTag(nil, 2, protowire.BytesType)
	body = protowire.AppendBytes(body, group)
	return body
}

// pickBaseWithMismatchedHead 从名称库里挑一个「头像文件名 != 形态编号」的形态。
//
// 存在的理由:头像只是**图片文件名**,多个形态常共用同一张素材 —— 实测 names.json
// 的 images 里 1112 个形态中 461 个的 h 与形态编号不同(如 3242 的图是 3012)。
// 只有拿这类形态做样本,才能验证编号是**查表得来**而非从 Img 反推。
// 找不到就返回 0(调用方 Skip)—— 这是数据特征,不是缺陷。
func pickBaseWithMismatchedHead(t *testing.T, db *gamedata.DB) uint32 {
	t.Helper()
	for base := uint32(3001); base < 4200; base++ {
		if _, ok := db.PetBase(base); !ok {
			continue
		}
		head := db.PetImageByBase(base, false).Head
		// HeadIcon/<n>.webp → 取 <n> 与形态编号比
		n := head
		if i := strings.LastIndexByte(n, '/'); i >= 0 {
			n = n[i+1:]
		}
		n = strings.TrimSuffix(n, ".webp")
		if n != "" && n != strconv.FormatUint(uint64(base), 10) {
			return base
		}
	}
	return 0
}

// TestFlowerCarriesBaseConfID 花种下发时必须带守护宠物的形态编号。
func TestFlowerCarriesBaseConfID(t *testing.T) {
	// 故意选一个「头像文件名 != 形态编号」的形态(见 payload.go 的注释),
	// 让「从 Img 反推」的取巧实现在下面这条测试里也一并露馅。
	const petBase = uint32(3242)

	p, srv := newTestPipeline(t)
	if head := p.db.PetImageByBase(petBase, false).Head; head == "" {
		t.Fatalf("前置失败:形态 %d 在测试库里没有头像,换个形态", petBase)
	}
	login(t, p, 1)
	p.handle(msg(gcp.S2C, scene.OpQueryBossNpcInfoRsp, bossNpcInfoBody(20129, petBase)))

	payload := srv.GetLastFlowers(testAcc)
	if payload == nil {
		t.Fatal("没有 flowers 快照(0x0375 没广播?)")
	}
	if len(payload.Flowers) == 0 {
		t.Fatal("花种列表为空,无法验证")
	}
	for _, f := range payload.Flowers {
		if f.BaseConfID == 0 {
			t.Errorf("花种 %s(id=%d) 缺 baseConfId —— 色卡点开后不会有「看 3D 效果」按钮",
				f.Name, f.ID)
			continue
		}
		// 必须是**形态编号**而非 npc_cfg_id:塞错会让外链指向另一只宠物,
		// 页面却一切正常(与 wilds_glass_link_test.go 里那个坑同源)。
		if f.BaseConfID != petBase {
			t.Errorf("花种 %s 的 baseConfId = %d, 期望形态编号 %d(npc_cfg_id 是 20129,别塞错)",
				f.Name, f.BaseConfID, petBase)
		}
	}
}

// TestFlowerBaseConfIDNotDerivedFromImg 形态编号不得从头像路径反推。
//
// 这条防的是「解析头像文件名拿编号」的取巧实现。头像只是**图片文件名**,多个形态常
// 共用同一张素材:实测 names.json 的 images 里 1112 个形态中 461 个的 h 与形态编号
// 不同。故这里选一个「头像名 != 形态编号」的形态做样本 —— 若实现去解析 Img,
// 得到的值就会与真实编号不同,断言立刻失败。
func TestFlowerBaseConfIDNotDerivedFromImg(t *testing.T) {
	p, srv := newTestPipeline(t)
	// 找一个「头像文件名与形态编号不同」的形态做样本:只有在这种样本上,
	// 「从 Img 反推编号」的实现才会露出破绽。
	petBase := pickBaseWithMismatchedHead(t, p.db)
	if petBase == 0 {
		t.Skip("测试库里没有「头像名 != 形态编号」的形态,本用例无样本")
	}

	login(t, p, 1)
	p.handle(msg(gcp.S2C, scene.OpQueryBossNpcInfoRsp, bossNpcInfoBody(20129, petBase)))

	payload := srv.GetLastFlowers(testAcc)
	if payload == nil || len(payload.Flowers) == 0 {
		t.Fatal("没有花种快照")
	}
	f := payload.Flowers[0]
	if f.BaseConfID != petBase {
		t.Errorf("baseConfId = %d, 期望 %d —— 若等于头像里的编号(%s),说明是从 Img 反推的",
			f.BaseConfID, petBase, f.Img)
	}
	// 花种没有异色(两个面板都不带 mutation),故不应给出 shiny 字段
	if f.Img == "" {
		t.Error("形态没有头像,本用例失去意义")
	}
}
