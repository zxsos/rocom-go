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
