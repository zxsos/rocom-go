package pipeline

import (
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"

	"github.com/zxsos/roco-go/internal/gamedata"
	"github.com/zxsos/roco-go/internal/gcp"
	"github.com/zxsos/roco-go/internal/pb"
	"github.com/zxsos/roco-go/internal/pet"
)

// 培育线的自动记录链路(见 breeding.go)。
//
// 这条链要在一个消息里跑通:「蛋 → 孵出的宠物」这唯一一对 gid 只出现在破壳那一刻
// (c2s 请求的 egg_gid + 回包的 hatched_pet_gid,实测见 docs/data.md 3.6),而双亲快照
// 只存在蛋那一行上 —— 记一代与删蛋的先后、以及子代晚一步入库(同一条消息先过蛋这一路、
// 再过宠物那一路)都是容易写错的时序,故这里按真实消息顺序喂两包,断言落在落库的培育线上。

// crackReqBody 构造 0x030b 破壳请求:field1=egg_gid(c2s 前面还有 6 字节子头)。
func crackReqBody(eggGid uint32) []byte {
	b := make([]byte, c2sSubHeaderLen)
	b = protowire.AppendTag(b, 1, protowire.VarintType)
	return protowire.AppendVarint(b, uint64(eggGid))
}

// c2sSubHeaderLen 与 pet 包内的 c2sSubHeader 同值(那边不导出),测试里照拼一份。
const c2sSubHeaderLen = 6

// crackRspBody 构造 0x030c 破壳回包:field2=孵出的宠物 gid,field1(ret_info)里嵌一条完整
// PetData。pd 为 nil 时只有 gid —— 模拟「只抓到回包、没抓到宠物」的那种漏包。
func crackRspBody(t *testing.T, childGid uint32, pd *pb.PetData) []byte {
	t.Helper()
	b := protowire.AppendTag(nil, 2, protowire.VarintType)
	b = protowire.AppendVarint(b, uint64(childGid))
	if pd == nil {
		return b
	}
	raw, err := proto.Marshal(pd)
	if err != nil {
		t.Fatalf("序列化 PetData: %v", err)
	}
	b = protowire.AppendTag(b, 1, protowire.BytesType)
	return protowire.AppendBytes(b, raw)
}

// pickTestPet 从名称库里挑一个可用形态。conf_id 要 >1000 且名字含中文 —— 这正是
// pet.FindNewPet 判定「这段字节是一条 PetData」的两条判据,取不到就喂不进去。
func pickTestPet(t *testing.T, p *Pipeline) (uint32, gamedata.PetBaseInfo) {
	t.Helper()
	for _, o := range p.db.PetForms() {
		if o.Base <= 1000 {
			continue
		}
		if info, ok := p.db.PetBase(o.Base); ok && info.Name != "" {
			return o.Base, info
		}
	}
	t.Fatal("名称库里挑不出可用形态")
	return 0, gamedata.PetBaseInfo{}
}

func pctPtr(v float64) *float64 { return &v }

// seedEgg 直接往库里放一颗带双亲快照的家园蛋(收蛋那一步的产物),返回双亲快照。
// 不模拟整条收蛋链路:这里要验的是破壳之后的事,蛋怎么来的由 eggs_hatch_test.go 覆盖。
func seedEgg(t *testing.T, p *Pipeline, gid uint32, species string, confID uint32) *pet.EggParents {
	t.Helper()
	sc := p.st.For(testAcc)
	if err := sc.UpsertEggs([]*pet.EggView{{
		Gid: gid, ItemID: 310001, ConfID: confID, Name: species + "的蛋", Species: species,
	}}, msg(gcp.S2C, pet.OpGoodsRewardNotify, nil).Time.Unix(), nil); err != nil {
		t.Fatalf("蛋入库: %v", err)
	}
	snap := &pet.EggParents{
		Mother: &pet.EggParent{Gid: 7001, Name: "母本", Species: species, ConfID: confID,
			Gender: "♀", Voice: 40, WeightPct: pctPtr(60)},
		Fathers: []pet.EggParent{{Gid: 7002, Name: "父本", Species: "别的品种", ConfID: confID,
			Gender: "♂", Voice: 100, WeightPct: pctPtr(90)}},
	}
	if err := sc.SetEggParents(gid, snap); err != nil {
		t.Fatalf("记双亲: %v", err)
	}
	return snap
}

// TestHatchRecordsAndClaimsChild 验证完整链路:破壳自动建线 + 记一代,子代入库后自动认领。
func TestHatchRecordsAndClaimsChild(t *testing.T) {
	p, _ := newTestPipeline(t)
	login(t, p, 1)
	sc := p.st.For(testAcc)

	base, info := pickTestPet(t, p)
	const eggGid, childGid = 4001, 9001
	seedEgg(t, p, eggGid, info.Name, base)

	// 破壳:先 c2s(记下 egg_gid),再回包(子代 PetData 一并下发)。
	p.handle(msg(gcp.C2S, pet.OpCrackEggReq, crackReqBody(eggGid)))
	p.handle(msg(gcp.S2C, pet.OpCrackEggRsp, crackRspBody(t, childGid, &pb.PetData{
		Gid:        proto.Uint32(childGid),
		ConfId:     proto.Uint32(base),
		BaseConfId: proto.Uint32(base),
		Name:       []byte(info.Name),
		Voice:      proto.Int32(96),
		Height:     proto.Uint32(info.HeightHigh),
		Weight:     proto.Uint32(info.WeightHigh),
	})))

	if egg, err := sc.GetEggBreedingInfo(eggGid); err != nil || egg != nil {
		t.Errorf("破壳后蛋行仍应删掉: egg=%+v err=%v", egg, err)
	}

	lines, err := sc.ListBreedingLines()
	if err != nil {
		t.Fatalf("读培育线: %v", err)
	}
	if len(lines) != 1 {
		t.Fatalf("培育线 %d 条, 期望 1 条(该品种还没有线,破壳自动建)", len(lines))
	}
	line := lines[0]
	if line.Species != info.Name {
		t.Errorf("线物种 = %q, 期望 %q(蛋的物种随母本)", line.Species, info.Name)
	}
	if len(line.Pending) != 0 {
		t.Errorf("子代已入库却仍有 %d 代待认领", len(line.Pending))
	}
	if len(line.Gens) != 1 {
		t.Fatalf("代数 = %d, 期望 1", len(line.Gens))
	}
	g := line.Gens[0]
	if g.Gen != 1 || g.Source != pet.GenSourceAuto {
		t.Errorf("gen=%d source=%q, 期望 1/auto", g.Gen, g.Source)
	}
	if g.Mother == nil || g.Mother.Gid != 7001 {
		t.Errorf("母本 = %+v, 期望 gid 7001", g.Mother)
	}
	if g.Father == nil || g.Father.Gid != 7002 {
		t.Errorf("父本 = %+v, 期望 gid 7002(单一候选当场定下)", g.Father)
	}
	if g.Child == nil {
		t.Fatal("子代未认领:破壳回包给出 gid,子代入库时应自动补上")
	}
	if g.Child.Gid != childGid {
		t.Errorf("子代 gid = %d, 期望 %d", g.Child.Gid, childGid)
	}
	if g.Child.Voice != 96 {
		t.Errorf("子代嗓音 = %d, 期望 96(取自子代自己的 PetData)", g.Child.Voice)
	}
	// 体重百分位要由 gamedata 的区间算出来(按最高值给的体重 → 100)
	if g.Child.WeightPct == nil || *g.Child.WeightPct != 100 {
		t.Errorf("子代体重百分位 = %v, 期望 100", g.Child.WeightPct)
	}
}

// TestHatchLeavesPendingWhenChildMissing 验证漏包时的兜底:只抓到回包的 gid、没抓到子代
// 宠物时,这一代留在「待认领」里等玩家在页面上指定,而不是被丢掉或猜一只替他认。
// 顺带钉住 NextGen 的口径:待认领的一代已占号,下一颗蛋要往后排,不能同号。
func TestHatchLeavesPendingWhenChildMissing(t *testing.T) {
	p, _ := newTestPipeline(t)
	login(t, p, 1)
	sc := p.st.For(testAcc)

	base, info := pickTestPet(t, p)
	seedEgg(t, p, 4101, info.Name, base)

	for _, egg := range []uint32{4101, 4102} {
		if egg == 4102 {
			seedEgg(t, p, egg, info.Name, base)
		}
		p.handle(msg(gcp.C2S, pet.OpCrackEggReq, crackReqBody(egg)))
		p.handle(msg(gcp.S2C, pet.OpCrackEggRsp, crackRspBody(t, egg+5000, nil)))
	}

	lines, err := sc.ListBreedingLines()
	if err != nil {
		t.Fatalf("读培育线: %v", err)
	}
	if len(lines) != 1 {
		t.Fatalf("培育线 %d 条, 期望 1 条(同品种的第二颗蛋应记到同一条线上)", len(lines))
	}
	line := lines[0]
	if len(line.Gens) != 0 {
		t.Errorf("代数 = %d, 期望 0(子代没抓到,不该进正式代数)", len(line.Gens))
	}
	if len(line.Pending) != 2 {
		t.Fatalf("待认领 = %d 代, 期望 2", len(line.Pending))
	}
	if line.Pending[0].Gen != 1 || line.Pending[1].Gen != 2 {
		t.Errorf("待认领代数 = %d,%d, 期望 1,2(待认领的一代也占号)",
			line.Pending[0].Gen, line.Pending[1].Gen)
	}
	for i, g := range line.Pending {
		if g.Child != nil {
			t.Errorf("第 %d 代不该有子代: %+v", i+1, g.Child)
		}
		if g.Source != pet.GenSourceAuto {
			t.Errorf("第 %d 代 source = %q, 期望 auto", i+1, g.Source)
		}
	}
}

// TestLayHatchClaimAcrossSessions 收蛋即记 → 破壳记 gid → 认领,且**连接已经不在**也不丢。
//
// 升级前破壳与认领之间靠 connState 里的内存配对 + 15 分钟保鲜期串起来:进程重启、离线回放、
// 玩家在别的设备上孵的蛋,都会让那一代永远停在待认领。现在三段之间的凭据都在库里
// (蛋上的 lineId/gen、代上的 childGid),故这里故意给认领传一个**空的连接状态**,
// 逼它走「按 gid 扫线」的慢路径。
//
// 顺带钉住收蛋那一侧的两条:蛋重放不重复建代、代上要记着是哪颗蛋。
func TestLayHatchClaimAcrossSessions(t *testing.T) {
	p, _ := newTestPipeline(t)
	login(t, p, 1)
	sc := p.st.For(testAcc)
	base, info := pickTestPet(t, p)
	const eggGid, childGid = 4301, 9301
	now := msg(gcp.S2C, pet.OpGoodsRewardNotify, nil).Time

	// 1) 收蛋:双亲到手即记一代,并回填 lineId/gen 供随后写进蛋行。
	ps := &pet.EggParents{
		Mother: &pet.EggParent{Gid: 7001, Name: "母本", Species: info.Name, ConfID: base,
			Gender: "♀", Voice: 40},
		Fathers: []pet.EggParent{{Gid: 7002, Name: "父本", Gender: "♂", Voice: 60}},
	}
	if err := sc.UpsertEggs([]*pet.EggView{{
		Gid: eggGid, ItemID: 310001, ConfID: base, Name: info.Name + "的蛋", Species: info.Name,
	}}, now.Unix(), nil); err != nil {
		t.Fatalf("蛋入库: %v", err)
	}
	p.recordLay(sc, testAcc, eggGid, ps, info.Name, now)
	if ps.LineID == "" || ps.Gen == 0 {
		t.Fatalf("收蛋没回填 lineId/gen: %+v", ps)
	}
	// 真实链路里 recordEggParents 紧接着把双亲(含 lineId/gen)写进蛋行
	if err := sc.SetEggParents(eggGid, ps); err != nil {
		t.Fatalf("记双亲: %v", err)
	}
	lines, err := sc.ListBreedingLines()
	if err != nil {
		t.Fatalf("读培育线: %v", err)
	}
	if len(lines) != 1 {
		t.Fatalf("培育线 %d 条, 期望 1(收蛋即建)", len(lines))
	}
	lineID := lines[0].ID
	if len(lines[0].Pending) != 1 {
		t.Fatalf("待处理 %d 代, 期望 1", len(lines[0].Pending))
	}
	if lines[0].Pending[0].EggGid != eggGid {
		t.Errorf("代上记的蛋 = %d, 期望 %d(溯源用)", lines[0].Pending[0].EggGid, eggGid)
	}

	// 2) 同一颗蛋重放(0x0243 重复下发):不该再建一代
	p.recordLay(sc, testAcc, eggGid, ps, info.Name, now)
	if lines, _ := sc.ListBreedingLines(); len(lines[0].Pending) != 1 {
		t.Errorf("重放的蛋又记了一代:待处理 %d 代", len(lines[0].Pending))
	}

	// 3) 破壳(换一个连接状态):按蛋行上的 lineId/gen 定位,把孵出的 gid 记上
	p.recordHatch(p.conn("other-session"), sc, testAcc, eggGid, childGid, now)
	line, err := sc.GetBreedingLine(lineID)
	if err != nil {
		t.Fatalf("读培育线: %v", err)
	}
	if len(line.Pending) != 1 || line.Pending[0].ChildGid != childGid {
		t.Fatalf("破壳后 = %+v, 期望那一代记着 childGid %d", line.Pending, childGid)
	}

	// 4) 认领:**空连接状态** —— 快路径用不上,只能靠按 gid 扫线的慢路径
	pp := &pet.Pet{Gid: childGid, ConfID: base, BaseConfID: base, Name: info.Name, Voice: 96,
		HeightM: float64(info.HeightHigh) / 100, WeightKg: float64(info.WeightHigh) / 1000}
	p.claimHatchedChild(&connState{}, sc, testAcc, pp, now)
	line, err = sc.GetBreedingLine(lineID)
	if err != nil {
		t.Fatalf("读培育线: %v", err)
	}
	if len(line.Gens) != 1 || line.Gens[0].Child == nil || line.Gens[0].Child.Gid != childGid {
		t.Fatalf("跨会话认领失败: gens=%+v pending=%+v", line.Gens, line.Pending)
	}
	if line.Gens[0].Child.Voice != 96 {
		t.Errorf("子代嗓音 = %d, 期望 96(取自子代自己的 PetData)", line.Gens[0].Child.Voice)
	}
}

// TestClaimPendingChildrenAfterFullSync 断网期间孵出的子代,靠全量对账后的补扫接上。
//
// 成因:认领挂在 applyNewPet(增量消息 + isNew)里,而这类子代是随**背包分页全量**
// (applyPetPage)进库的 —— 那条路不调认领,于是那一代永远停在「待认领」。
// 培育线是事件流产物,宠物与蛋有全量对账可以自愈、唯独它没有,故缺口只能在补扫里补。
func TestClaimPendingChildrenAfterFullSync(t *testing.T) {
	p, _ := newTestPipeline(t)
	login(t, p, 1)
	sc := p.st.For(testAcc)
	base, info := pickTestPet(t, p)
	const eggGid, childGid = 4401, 9401
	now := msg(gcp.S2C, pet.OpGoodsRewardNotify, nil).Time

	ps := &pet.EggParents{
		Mother: &pet.EggParent{Gid: 7001, Name: "母本", Species: info.Name, ConfID: base,
			Gender: "♀", Voice: 40},
		Fathers: []pet.EggParent{{Gid: 7002, Name: "父本", Gender: "♂", Voice: 60}},
	}
	if err := sc.UpsertEggs([]*pet.EggView{{
		Gid: eggGid, ItemID: 310001, ConfID: base, Name: info.Name + "的蛋", Species: info.Name,
	}}, now.Unix(), nil); err != nil {
		t.Fatalf("蛋入库: %v", err)
	}
	p.recordLay(sc, testAcc, eggGid, ps, info.Name, now)
	// 真实链路里 SetEggParents 紧跟 recordLay:破壳是靠蛋行上的 lineId/gen 认出那一代的
	if err := sc.SetEggParents(eggGid, ps); err != nil {
		t.Fatalf("记双亲: %v", err)
	}
	p.recordHatch(p.conn("other-session"), sc, testAcc, eggGid, childGid, now)

	// 子代此刻还没进库:补扫不该有动作(更不该把它当成「已不在库」)
	p.claimPendingChildren(sc, testAcc, now)
	if line, _ := sc.GetBreedingLine(ps.LineID); len(line.Gens) != 0 {
		t.Fatalf("子代还没进库就认领了: gens=%+v", line.Gens)
	}

	// 子代随**全量分页**进库(不经过 applyNewPet,故不会触发认领)
	pp := &pet.Pet{Gid: childGid, ConfID: base, BaseConfID: base, Name: info.Name, Voice: 96}
	if _, err := sc.UpsertPet(pp); err != nil {
		t.Fatalf("子代入库: %v", err)
	}
	p.claimPendingChildren(sc, testAcc, now)
	line, err := sc.GetBreedingLine(ps.LineID)
	if err != nil {
		t.Fatalf("读培育线: %v", err)
	}
	if len(line.Gens) != 1 || line.Gens[0].Child == nil || line.Gens[0].Child.Gid != childGid {
		t.Fatalf("补扫认领失败: gens=%+v pending=%+v", line.Gens, line.Pending)
	}
	if len(line.Pending) != 0 {
		t.Errorf("补扫后仍剩 %d 代待处理", len(line.Pending))
	}
}

// TestClaimPendingChildrenKeepsMissingChild 子代不在库(放生/送人)时:不认领、也不丢弃 ——
// 留着 childGid 让页面说清「孵出的 #gid 已不在库」,由玩家决定补录还是丢弃。
func TestClaimPendingChildrenKeepsMissingChild(t *testing.T) {
	p, _ := newTestPipeline(t)
	login(t, p, 1)
	sc := p.st.For(testAcc)
	base, info := pickTestPet(t, p)
	const eggGid, childGid = 4501, 9501
	now := msg(gcp.S2C, pet.OpGoodsRewardNotify, nil).Time

	ps := &pet.EggParents{
		Mother:  &pet.EggParent{Gid: 7001, Name: "母本", Species: info.Name, ConfID: base, Gender: "♀"},
		Fathers: []pet.EggParent{{Gid: 7002, Name: "父本", Gender: "♂"}},
	}
	sc.UpsertEggs([]*pet.EggView{{
		Gid: eggGid, ItemID: 310001, ConfID: base, Species: info.Name,
	}}, now.Unix(), nil)
	p.recordLay(sc, testAcc, eggGid, ps, info.Name, now)
	if err := sc.SetEggParents(eggGid, ps); err != nil {
		t.Fatalf("记双亲: %v", err)
	}
	p.recordHatch(p.conn("s"), sc, testAcc, eggGid, childGid, now)

	// 那只宠始终没进库(断网期间孵了又放生,工具从没见过它)
	p.claimPendingChildren(sc, testAcc, now)
	line, err := sc.GetBreedingLine(ps.LineID)
	if err != nil {
		t.Fatalf("读培育线: %v", err)
	}
	if len(line.Gens) != 0 {
		t.Errorf("不该认领: gens=%+v", line.Gens)
	}
	if len(line.Pending) != 1 || line.Pending[0].ChildGid != childGid {
		t.Errorf("该代应留着 childGid 供页面提示,实为 %+v", line.Pending)
	}
}
