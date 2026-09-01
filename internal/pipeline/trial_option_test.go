package pipeline

import (
	"testing"

	"google.golang.org/protobuf/encoding/protowire"

	"github.com/whoisnian/rocom-capture/internal/gamedata"
	"github.com/whoisnian/rocom-capture/internal/store"
	"github.com/whoisnian/rocom-capture/internal/trial"
)

// 本文件锁住「节点事件卡片」的两块新增内容,它们都是**靠标注才有**的,
// 而 golden 契约测试锁不住:
//
//  1. 抽取池(pool/used)与 5 个 id 的名字 —— 换奖励就是在这 5 个里重抽,
//     只看当前那条 reward 无法预判重掷会出什么;
//  2. 事件对应哪只精灵(pet)—— 协议不下发,靠 kind=event 的标注补。
//
// 前者是透传(漏了只是少字段),后者是「查库 + 反查形态 + 取头像」三步,
// 任何一步错了都静默变回占位,故在此断言端到端结果。

// trialEventBody 拼 GrassTrialNodeSelection:field1=node_events[],
// 每个事件 field1=slot_index / field2=event_conf_id / field3=reward_id /
// field6=random_skills[] / field7=level / field9=extra_reward_ids[]。
// 字段号与 trial.parseNodeSelection 一致(见 internal/trial/trial.go)。
func trialEventBody(slot, event, reward, level uint32, pool []uint32, extra ...uint32) []byte {
	var e []byte
	e = protowire.AppendTag(e, 1, protowire.VarintType)
	e = protowire.AppendVarint(e, uint64(slot))
	e = protowire.AppendTag(e, 2, protowire.VarintType)
	e = protowire.AppendVarint(e, uint64(event))
	e = protowire.AppendTag(e, 3, protowire.VarintType)
	e = protowire.AppendVarint(e, uint64(reward))
	for _, id := range pool {
		e = protowire.AppendTag(e, 6, protowire.VarintType)
		e = protowire.AppendVarint(e, uint64(id))
	}
	e = protowire.AppendTag(e, 7, protowire.VarintType)
	e = protowire.AppendVarint(e, uint64(level))
	for _, id := range extra {
		e = protowire.AppendTag(e, 9, protowire.VarintType)
		e = protowire.AppendVarint(e, uint64(id))
	}
	b := protowire.AppendTag(nil, 1, protowire.BytesType)
	return protowire.AppendBytes(b, e)
}

// trialRunWithSelection 造一份「正在进行的一局 + 一个节点事件」的镜像。
func trialRunWithSelection(sel *trial.Selection) *trialRun {
	return &trialRun{
		trialConfID: 10002, slotID: 1000, chapterID: 3000, nodeIndex: 1,
		chapters: []uint32{3000, 3001, 3002}, active: true,
		selection: sel,
	}
}

// approveEventAnnotation 直接写库并审核通过一条 event 标注。
// 走 store 而非 HTTP:这里要的是「审核通过后管线能看到」这一条链路。
func approveEventAnnotation(t *testing.T, p *Pipeline, code int64, name string) {
	t.Helper()
	a, err := p.st.SubmitAnnotation(store.Annotation{Kind: "event", Code: code, Name: name, Submitter: testAcc})
	if err != nil {
		t.Fatalf("提交标注: %v", err)
	}
	if err := p.st.ReviewAnnotation(a.ID, true, "test"); err != nil {
		t.Fatalf("审核标注: %v", err)
	}
}

// findPetWithFeature 找一个「wiki 收录了特性」的精灵形态,返回形态 id 与特性名。
// 动态找而非写死 id:解包数据换版本后 id 会变,写死会让测试随机变红。
func findPetWithFeature(t *testing.T, db *gamedata.DB) (uint32, string) {
	t.Helper()
	for _, f := range db.PetForms() {
		if n := db.FeatureNameOfBase(f.Base); n != "" {
			return f.Base, n
		}
	}
	t.Skip("没有 wiki 收录特性的形态(features.json 未生成?)")
	return 0, ""
}

// TestTrialOptionNamesFromAnnotation 锁住本功能的收益:**标出精灵,特性名自动带上**。
//
// 一条 event 标注同时省掉两步 ——
//   - 事件 → 精灵:卡片显示头像与名字;
//   - 精灵 → 特性:池里那条 288xxx 走「精灵 → 特性」表带上名字,玩家不必再标一次。
//
// 这是「开局就能自动绑定一个特性」的落点,也是整个改动唯一真正的省事之处。
func TestTrialOptionNamesFromAnnotation(t *testing.T) {
	p, _ := newTestPipeline(t)
	base, featName := findPetWithFeature(t, p.db)
	petName := p.db.PetFullName(base)

	// 池:1 个特性 + 4 个技能(换奖励就在这 5 个里重抽)
	catalog := p.db.SkillCatalog()
	if len(catalog) < 4 {
		t.Fatalf("技能目录只有 %d 条,样本不足", len(catalog))
	}
	skills := []uint32{catalog[0].ID, catalog[1].ID, catalog[2].ID, catalog[3].ID}
	feat := uint32(288001)
	pool := append([]uint32{feat}, skills...)
	event := uint32(130056)

	approveEventAnnotation(t, p, int64(event), petName)
	body := trialEventBody(1, event, skills[0], 40, pool, 2016)
	sel := trial.ParseSelection(body)
	if sel == nil || len(sel.Events) != 1 {
		t.Fatalf("解析节点选项失败: %+v", sel)
	}
	out := p.trialRunPayload(trialRunWithSelection(sel), nil)

	if len(out.Options) != 1 {
		t.Fatalf("应有 1 个事件,实际 %d", len(out.Options))
	}
	o := out.Options[0]
	if o.Pet == nil {
		t.Fatal("已标注事件却没有 pet —— 反查形态或取头像失败")
	}
	if o.Pet.Base != base {
		t.Errorf("pet.base = %d, 期望 %d(%s)", o.Pet.Base, base, petName)
	}
	// 池完整下发:换奖励的候选要看得见
	if len(o.Pool) != 5 {
		t.Errorf("pool 长度 = %d, 期望 5", len(o.Pool))
	}
	// 技能名来自 skills.json,4 个都该有
	for i, id := range skills {
		if got := o.Names[id]; got != catalog[i].Name {
			t.Errorf("技能 %d 名 = %q, 期望 %q", id, got, catalog[i].Name)
		}
	}
	// 特性名来自「精灵 → 特性」表桥接:标了精灵才有
	if got := o.Names[feat]; got != featName {
		t.Errorf("特性 %d 名 = %q, 期望 %q(标出精灵后应自动带上)", feat, got, featName)
	}
}

// TestTrialOptionNeedsAnnotation 锁住**没标注时不猜**:
// pet 缺失、特性无名(技能名仍然是有的 —— 那是查表查的,与标注无关)。
// 猜一个精灵名字比留空更糟:玩家会以为那是服务器说的。
func TestTrialOptionNeedsAnnotation(t *testing.T) {
	p, _ := newTestPipeline(t)
	catalog := p.db.SkillCatalog()
	if len(catalog) < 2 {
		t.Fatalf("技能目录只有 %d 条,样本不足", len(catalog))
	}
	pool := []uint32{288001, catalog[0].ID, catalog[1].ID}
	body := trialEventBody(2, 110041, catalog[0].ID, 40, pool)
	sel := trial.ParseSelection(body)
	if sel == nil {
		t.Fatal("解析节点选项失败")
	}
	out := p.trialRunPayload(trialRunWithSelection(sel), nil)

	o := out.Options[0]
	if o.Pet != nil {
		t.Errorf("未标注时 pet 应为 nil,实际 %+v", o.Pet)
	}
	if _, ok := o.Names[288001]; ok {
		t.Error("未标注时特性不该有名字(精灵未知,无从桥接)")
	}
	if got := o.Names[catalog[0].ID]; got != catalog[0].Name {
		t.Errorf("技能 %d 名 = %q, 期望 %q(技能名与标注无关,应照常给)", catalog[0].ID, got, catalog[0].Name)
	}
}

// TestTrialOptionAmbiguousFeatureNotNamed 锁住「池里出现多条特性时都不给名」。
//
// 一只精灵只有一个自身特性 —— 池里出现两条 288xxx,说明我们对 pool 的理解是错的,
// 这时拿精灵的特性名去绑必然绑错一条。标错比不标更糟,故整条都不给。
func TestTrialOptionAmbiguousFeatureNotNamed(t *testing.T) {
	p, _ := newTestPipeline(t)
	base, _ := findPetWithFeature(t, p.db)
	approveEventAnnotation(t, p, 110116, p.db.PetFullName(base))

	body := trialEventBody(3, 110116, 288001, 40, []uint32{288001, 288002})
	sel := trial.ParseSelection(body)
	if sel == nil {
		t.Fatal("解析节点选项失败")
	}
	out := p.trialRunPayload(trialRunWithSelection(sel), nil)
	o := out.Options[0]
	if o.Pet == nil {
		t.Fatal("已标注事件却没有 pet")
	}
	for _, id := range []uint32{288001, 288002} {
		if n, ok := o.Names[id]; ok {
			t.Errorf("池里有多条特性时 %d 不该有名字,实际给了 %q", id, n)
		}
	}
}

// TestTrialPetFeatureNameBridged 锁住试炼**宠物**那侧的同款桥接:
// 天生特性带上名字,试炼中获得的**不带**(那些是节点随机给的,与精灵无关)。
func TestTrialPetFeatureNameBridged(t *testing.T) {
	p, _ := newTestPipeline(t)
	base, featName := findPetWithFeature(t, p.db)
	innate := uint32(288135)
	gained := uint32(288999)

	r := trialRunWithSelection(nil)
	r.pet = &trial.Pet{
		Gid: 133, BaseConfID: base, Name: p.db.PetFullName(base),
		Features: []uint32{innate, gained},
	}
	r.initialFeatures = trial.InitialFeatures{innate}

	out := p.trialRunPayload(r, nil)
	if len(out.Pet.InnateFeatures) != 1 || out.Pet.InnateFeatures[0] != innate {
		t.Fatalf("天生特性 = %v, 期望 [%d]", out.Pet.InnateFeatures, innate)
	}
	if got := out.Pet.FeatureNames[innate]; got != featName {
		t.Errorf("天生特性 %d 名 = %q, 期望 %q", innate, got, featName)
	}
	if _, ok := out.Pet.FeatureNames[gained]; ok {
		t.Error("试炼中获得的特性不该有名字 —— 它是节点随机给的,与精灵本身无关")
	}
}

// petWithMutationBody 拼 GrassTrialPetData:field1=gid / field2=base_conf_id /
// field13=内嵌 PetData。内嵌层里只放解析真正要用的三个字段:
// field2=conf_id、field3=name(nickname)、field45=mutation_type。
//
// mutation_type 是**位标志**:bit0(1)=异色、bit3(8)=炫彩,与宠物主流程同判据
// (internal/pet/model.go 有实测样本的验证记录)。
func petWithMutationBody(base, mutation uint32) []byte {
	var inner []byte
	inner = protowire.AppendTag(inner, 2, protowire.VarintType)
	inner = protowire.AppendVarint(inner, uint64(base))
	inner = protowire.AppendTag(inner, 3, protowire.BytesType)
	inner = protowire.AppendBytes(inner, []byte("异色的它"))
	inner = protowire.AppendTag(inner, 45, protowire.VarintType)
	inner = protowire.AppendVarint(inner, uint64(mutation))

	var b []byte
	b = protowire.AppendTag(b, 1, protowire.VarintType)
	b = protowire.AppendVarint(b, 133) // gid
	b = protowire.AppendTag(b, 2, protowire.VarintType)
	b = protowire.AppendVarint(b, uint64(base))
	b = protowire.AppendTag(b, 13, protowire.BytesType)
	return protowire.AppendBytes(b, inner)
}

// findPetWithShinyImage 找一个**确有异色头像素材**的形态。
//
// 必须动态找:异色图不是每只精灵都有(没有时 gamedata 静默回退普通图),
// 写死某个 id 会在数据更新后变成假阳性 —— 测试通过但什么也没守住。
func findPetWithShinyImage(t *testing.T, db *gamedata.DB) uint32 {
	t.Helper()
	for _, f := range db.PetForms() {
		if n, s := db.PetImageByBase(f.Base, false), db.PetImageByBase(f.Base, true); n.Head != "" && s.Head != "" && n.Head != s.Head {
			return f.Base
		}
	}
	t.Skip("没有带异色头像素材的形态")
	return 0
}

// TestTrialPetMutationImage 端到端锁住「异色精灵进试炼,头像是异色的」。
//
// 这是真实踩过的 bug:试炼带的是玩家**自己的**精灵,异色原样带进去,
// 而外观标志(mutation_type)只在内嵌 PetData 里。整条链路上任何一环断了 ——
// 解析漏字段、取图传了 false —— 结果都是**异色精灵显示普通头像**,
// 不报错、不缺字段,只是图片看着不对,谁也发现不了。
//
// 故这里从**原始字节**走到对外载荷,把整条链路串起来测:
//
//	bytes → ParsePet(mutation_type) → trialPetPayload → img / shiny / colorful
//
// ⚠️ 契约 golden(contract_test.go 的 trial-shiny)守不住这条链:
// 它是手工构造的 TrialPayload,根本不经过 pipeline 的解析与取图。
// 那份 golden 只保证「字段在不在」,这里保证「取值对不对」。
func TestTrialPetMutationImage(t *testing.T) {
	p, _ := newTestPipeline(t)
	base := findPetWithShinyImage(t, p.db)
	normal, shinyImg := p.db.PetImageByBase(base, false), p.db.PetImageByBase(base, true)

	// mutation=0:普通,取普通图
	plain := p.trialPetPayload(trial.ParsePet(petWithMutationBody(base, 0)), nil)
	if plain.Shiny {
		t.Error("mutation=0 不该判为异色")
	}
	if plain.Img != normal.Head {
		t.Errorf("普通宠 img = %q, 期望 %q", plain.Img, normal.Head)
	}

	// mutation=1(bit0):异色,取异色图
	sh := p.trialPetPayload(trial.ParsePet(petWithMutationBody(base, 1)), nil)
	if !sh.Shiny {
		t.Error("mutation bit0 置位应判为异色 —— 解析漏了字段 45?")
	}
	if sh.Img != shinyImg.Head {
		t.Errorf("异色宠 img = %q, 期望异色图 %q(取到了普通图)", sh.Img, shinyImg.Head)
	}
	if sh.Img == normal.Head {
		t.Error("异色宠取到了普通图 —— 取图时传了 false?")
	}
	if sh.Colorful {
		t.Error("mutation=1 不该判为炫彩(bit3 未置位)")
	}

	// mutation=9(bit0 + bit3):异色炫彩,两者都成立 —— 位标志互不排斥
	both := p.trialPetPayload(trial.ParsePet(petWithMutationBody(base, 9)), nil)
	if !both.Shiny || !both.Colorful {
		t.Errorf("mutation=9 应为异色+炫彩,实际 shiny=%v colorful=%v —— 别写成互斥的三元",
			both.Shiny, both.Colorful)
	}
	if both.Img != shinyImg.Head {
		t.Errorf("异色炫彩 img = %q, 期望异色图 %q", both.Img, shinyImg.Head)
	}
}

// TestTrialPetGlassInfo 锁住炫彩的 glass_type / glass_value 透传。
//
// 炫彩外观只靠这两个值还原(前端按色卡素材 CSS mask 渲染,见 badges.jsx),
// 而它们藏在内嵌 PetData 的 glass_info(字段 86)里 —— 解析漏了,炫彩精灵
// 在试炼页就只是个普通精灵,连色卡都出不来。
func TestTrialPetGlassInfo(t *testing.T) {
	p, _ := newTestPipeline(t)
	base := findPetWithShinyImage(t, p.db)

	var gi []byte
	gi = protowire.AppendTag(gi, 17, protowire.VarintType)
	gi = protowire.AppendVarint(gi, 2) // glass_type
	gi = protowire.AppendTag(gi, 18, protowire.VarintType)
	gi = protowire.AppendVarint(gi, 0x00030004) // glass_value

	var inner []byte
	inner = protowire.AppendTag(inner, 2, protowire.VarintType)
	inner = protowire.AppendVarint(inner, uint64(base))
	inner = protowire.AppendTag(inner, 45, protowire.VarintType)
	inner = protowire.AppendVarint(inner, 8) // bit3 = 炫彩(非异色)
	inner = protowire.AppendTag(inner, 86, protowire.BytesType)
	inner = protowire.AppendBytes(inner, gi)

	var b []byte
	b = protowire.AppendTag(b, 2, protowire.VarintType)
	b = protowire.AppendVarint(b, uint64(base))
	b = protowire.AppendTag(b, 13, protowire.BytesType)
	b = protowire.AppendBytes(b, inner)

	out := p.trialPetPayload(trial.ParsePet(b), nil)
	if !out.Colorful {
		t.Fatal("mutation bit3 置位应判为炫彩")
	}
	if out.Shiny {
		t.Error("mutation=8 不含 bit0,不该判为异色")
	}
	if out.GlassType != 2 {
		t.Errorf("glassType = %d, 期望 2 —— 解析漏了 glass_info(字段 86)?", out.GlassType)
	}
	if out.GlassValue != 0x00030004 {
		t.Errorf("glassValue = %#x, 期望 %#x", out.GlassValue, 0x00030004)
	}
}

// TestTrialPetManyInnateNotNamed 锁住「天生特性有多条时都不给名」。
//
// 桥接表的口径是「一只精灵 → 一个特性名」,多条天生时无从判断该把名字绑给谁,
// 绑错一条比全不绑更糟(玩家会把错的名字当事实)。这条同时是
// `len(InnateFeatures) == 1` 那个判定的变异守卫 —— 放宽成 >=1 会让它变红。
func TestTrialPetManyInnateNotNamed(t *testing.T) {
	p, _ := newTestPipeline(t)
	base, _ := findPetWithFeature(t, p.db)
	r := trialRunWithSelection(nil)
	r.pet = &trial.Pet{Gid: 133, BaseConfID: base, Name: p.db.PetFullName(base),
		Features: []uint32{288135, 288136}}
	r.initialFeatures = trial.InitialFeatures{288135, 288136}

	out := p.trialRunPayload(r, nil)
	if len(out.Pet.InnateFeatures) != 2 {
		t.Fatalf("天生特性 = %v, 期望 2 条", out.Pet.InnateFeatures)
	}
	for _, id := range out.Pet.InnateFeatures {
		if n, ok := out.Pet.FeatureNames[id]; ok {
			t.Errorf("有多条天生特性时 %d 不该有名字,实际给了 %q", id, n)
		}
	}
}
