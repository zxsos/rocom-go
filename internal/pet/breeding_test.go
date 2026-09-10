package pet

import (
	"math"
	"testing"

	"github.com/whoisnian/rocom-capture/internal/gamedata"
)

// brI32 / brF64 取地址的小工具:目标字段都是指针(nil = 玩家没填这一项)。
func brI32(v int32) *int32     { return &v }
func brF64(v float64) *float64 { return &v }

// brNear 比两个概率是否相等。它是几项浮点相加的结果,**相加顺序不同末位就会差一点点**
// (0.4/30 + 0.3 + 0.3 与 2×0.3 + 0.4/30 不等),故不能直接用 !=。
func brNear(a, b float64) bool { return math.Abs(a-b) < 1e-12 }

// brParent 造一个亲本快照:只填与培育有关的那几项。
func brParent(gid uint32, name string, voice int32, pct float64, nature string) EggParent {
	return EggParent{Gid: gid, Name: name, Voice: voice, WeightPct: brF64(pct), Nature: nature}
}

// brChild 同上,但返回指针(Generation.Child 是指针:待认领的一代子代为空)。
func brChild(gid uint32, name string, voice int32, pct float64, nature string) *EggParent {
	p := brParent(gid, name, voice, pct, nature)
	return &p
}

// TestPredictVoiceAndWeight 嗓音与体重的预测口径。
//
// 嗓音取双亲均值向下取整(已由 parentVoice 落地);体重取双亲百分位均值,并给一个乐观上界 ——
// 两条实测都比均值高(94.610 → 96.332 是 +1.72pp,99.754 → 100 是 +0.25pp),故上界取
// 均值 +2pp 且不超过 100。这里用实测样本当输入,免得口径漂了没人发现。
func TestPredictVoiceAndWeight(t *testing.T) {
	cases := []struct {
		name              string
		mv, fv            int32
		mw, fw            float64
		wantVoice         int32
		wantPct, wantHiPx float64
	}{
		{"实测样本:双亲同 94.610%", 96, 96, 94.610, 94.610, 96, 94.610, 96.610},
		{"实测样本:双亲同 99.754% 时上界被 100 截断", 100, 100, 99.754, 99.754, 100, 99.754, 100},
		{"嗓音向下取整:61 与 62 → 61", 61, 62, 50, 50, 61, 50, 52},
		{"负嗓音同样向下取整:-61 与 -62 → -62", -61, -62, 1, 1, -62, 1, 3},
		{"双亲百分位不同则取均值", 0, 0, 40, 80, 0, 60, 62},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := brParent(1, "母", c.mv, c.mw, "")
			f := brParent(2, "父", c.fv, c.fw, "")
			got := Predict(m, f, BreedingGoal{})
			if got.Voice != c.wantVoice {
				t.Errorf("嗓音 = %d, 期望 %d(双亲均值向下取整)", got.Voice, c.wantVoice)
			}
			if math.Abs(got.WeightPct-c.wantPct) > 1e-9 {
				t.Errorf("体重百分位 = %.3f, 期望 %.3f", got.WeightPct, c.wantPct)
			}
			if math.Abs(got.WeightHi-c.wantHiPx) > 1e-9 {
				t.Errorf("体重上界 = %.3f, 期望 %.3f", got.WeightHi, c.wantHiPx)
			}
		})
	}
}

// TestPredictWeightWithMissingPct 百分位缺失(该形态没有取值范围)时不能算出个 0 来糊弄:
// 只有一方有就按那一方算,两方都没有则 0(UI 上表现为「无从预测」)。
func TestPredictWeightWithMissingPct(t *testing.T) {
	m := EggParent{Gid: 1, Name: "母", Voice: 10, WeightPct: brF64(60)}
	f := EggParent{Gid: 2, Name: "父", Voice: 10}
	if got := Predict(m, f, BreedingGoal{}).WeightPct; math.Abs(got-60) > 1e-9 {
		t.Errorf("母方 60%% / 父方未知时 = %.1f, 期望 60(以已知的一方为准)", got)
	}
	f.WeightPct = brF64(80)
	m.WeightPct = nil
	if got := Predict(m, f, BreedingGoal{}).WeightPct; math.Abs(got-80) > 1e-9 {
		t.Errorf("母方未知 / 父方 80%% 时 = %.1f, 期望 80", got)
	}
	m.WeightPct, f.WeightPct = nil, nil
	if got := Predict(m, f, BreedingGoal{}).WeightPct; got != 0 {
		t.Errorf("双方都未知时 = %.1f, 期望 0", got)
	}
}

// TestPredictNatureChance 性格是**概率**而非定值,且按固定分档:母 30% / 父 30% / 其余 40% 重掷
// (见 docs/data.md 3.6)。故命中率只有三种取值:双亲都带 → 60%、只有一方带 → 30%、双亲都没有 → 0,
// 三者再各自加上「重掷槽本身也掷中」的那一份 0.4/30。
func TestPredictNatureChance(t *testing.T) {
	m := brParent(1, "母", 0, 50, "勇敢")
	f := brParent(2, "父", 0, 50, "胆小")
	// 期望值**写死规则给的数**,不拿包里的常量现算:用常量算的话,常量被改错时期望跟着一起改,
	// 测试永远是绿的(变异测试实测:把 natureInheritChance 改成 0.20,那种写法照样通过)。
	// 分母 30 = **去重后**的性格种类数(见 breeding.go 的 natureCount),不是 names.json 的行数 31
	// —— 那张表里 id 28 与 id 31 同为「平和」,实测 7545 只宠物的性格 id 里 31 从未出现。
	const rollPart = 0.4 / 30 // 重掷槽掷中的那一份 —— 三档都要加上它

	if got := Predict(m, f, BreedingGoal{Nature: "勇敢"}); got.NatureFrom != "parent" || !brNear(got.NatureP, 0.3+rollPart) {
		t.Errorf("目标性格只在母方身上时 = %s/%.4f, 期望 parent/%.4f", got.NatureFrom, got.NatureP, 0.3+rollPart)
	}
	if got := Predict(m, f, BreedingGoal{Nature: "胆小"}); got.NatureFrom != "parent" || !brNear(got.NatureP, 0.3+rollPart) {
		t.Errorf("目标性格只在父方身上时 = %s/%.4f, 期望 parent/%.4f", got.NatureFrom, got.NatureP, 0.3+rollPart)
	}
	// 双亲性格相同:两个槽位指向同一个性格,合计 60%。**不是两次独立判定** ——
	// 那样会算成 1 − 0.7 × 0.7 = 51%,与「两个都是则 60%」的规则不符。
	same := brParent(3, "父", 0, 50, "勇敢")
	if got := Predict(m, same, BreedingGoal{Nature: "勇敢"}); got.NatureFrom != "parent" || !brNear(got.NatureP, 0.6+rollPart) {
		t.Errorf("双亲都带目标性格时 = %s/%.4f, 期望 parent/%.4f", got.NatureFrom, got.NatureP, 0.6+rollPart)
	}
	if got := Predict(m, f, BreedingGoal{Nature: "固执"}); got.NatureFrom != "roll" || !brNear(got.NatureP, rollPart) {
		t.Errorf("双亲都没有该性格时 = %s/%.4f, 期望 roll/%.4f", got.NatureFrom, got.NatureP, rollPart)
	}
	if got := Predict(m, f, BreedingGoal{}); got.NatureFrom != "none" || got.NatureP != 0 {
		t.Errorf("没设性格目标时 = %s/%.2f, 期望 none/0", got.NatureFrom, got.NatureP)
	}
}

// TestPredictNatureSetGoal 目标性格可以是一**组**名字(「性格正面加某维」= 该维 +10% 的那 5 个,
// 前端按 6×6 方阵的整行展开)。命中率 = 重掷槽按集合大小分摊(0.4 × |集合| / 去重后的性格种类数 30)
// 加上双亲各自带集合内性格时的 30%。
//
// 期望值同样写死规则给的数(理由见上一条测试)。
func TestPredictNatureSetGoal(t *testing.T) {
	// 物攻↑ 的那一行(真实名字取自名字库,这里只关心它们在不在集合里)
	row := []string{"逞强", "固执", "大胆", "调皮", "勇敢"}
	m := brParent(1, "母", 0, 50, "固执") // 在集合里
	f := brParent(2, "父", 0, 50, "胆小") // 不在

	if got := Predict(m, f, BreedingGoal{NatureIn: row}); got.NatureFrom != "parent" || !brNear(got.NatureP, 0.3+0.4*5/30) {
		t.Errorf("只有母方在集合里时 = %s/%.4f, 期望 parent/%.4f", got.NatureFrom, got.NatureP, 0.3+0.4*5/30)
	}
	// 双亲都在集合里(两个槽位各自命中,仍合计 60%)
	f2 := brParent(3, "父", 0, 50, "大胆")
	if got := Predict(m, f2, BreedingGoal{NatureIn: row}); got.NatureFrom != "parent" || !brNear(got.NatureP, 0.6+0.4*5/30) {
		t.Errorf("双亲都在集合里时 = %s/%.4f, 期望 parent/%.4f", got.NatureFrom, got.NatureP, 0.6+0.4*5/30)
	}
	// 双亲都不在集合里 → 只剩重掷槽,且它按集合大小(2 个)分摊
	other := []string{"沉默", "平和"}
	if got := Predict(m, f, BreedingGoal{NatureIn: other}); got.NatureFrom != "roll" || !brNear(got.NatureP, 0.4*2/30) {
		t.Errorf("双亲都不在集合里时 = %s/%.4f, 期望 roll/%.4f", got.NatureFrom, got.NatureP, 0.4*2/30)
	}
	// Nature 与 NatureIn 同时填 → 并集:重复的名字只算一次,空名一律丢掉
	dup := BreedingGoal{Nature: "固执", NatureIn: []string{"固执", ""}}
	if got := Predict(m, f, dup); !brNear(got.NatureP, 0.3+0.4/30) {
		t.Errorf("Nature 与 NatureIn 重复时 = %.4f, 期望 %.4f(去重后的并集)", got.NatureP, 0.3+0.4/30)
	}
}

// TestAppendPendingDedupByEgg 同一颗蛋只记一代:0x0243 重放(或收蛋与破壳两条路都走到)
// 不该建出两代 —— 重放出来的那几代双亲一模一样、永远等不到子代,而玩家无从分辨哪条是真的。
func TestAppendPendingDedupByEgg(t *testing.T) {
	ps := &EggParents{Mother: &EggParent{Gid: 7001, Name: "母本", Species: "火神"}}
	l := &BreedingLine{ID: "l1", Species: "火神"}

	if gen := AppendPending(l, ps, 5001, 100); gen != 1 {
		t.Fatalf("第一颗蛋 = 第 %d 代, 期望 1", gen)
	}
	if gen := AppendPending(l, ps, 5001, 200); gen != 1 {
		t.Errorf("同一颗蛋重放又建了一代:第 %d 代", gen)
	}
	if len(l.Pending) != 1 {
		t.Fatalf("待处理 %d 代, 期望 1(重放的那次应被去重)", len(l.Pending))
	}
	// 另一颗蛋正常往后排
	if gen := AppendPending(l, ps, 5002, 300); gen != 2 {
		t.Errorf("第二颗蛋 = 第 %d 代, 期望 2", gen)
	}
	// eggGid 为 0(老路径 / 补录)不参与去重,否则连正常的第二颗蛋都建不出来
	if gen := AppendPending(l, ps, 0, 400); gen != 3 {
		t.Errorf("无蛋 gid = 第 %d 代, 期望 3(不去重)", gen)
	}
	// 已认领的代里若已有这颗蛋,也不该再建
	l2 := &BreedingLine{ID: "l2"}
	AppendPending(l2, ps, 6001, 100)
	claimAt(l2, 0, EggParent{Gid: 9001})
	if gen := AppendPending(l2, ps, 6001, 500); gen != 1 {
		t.Errorf("已孵过的蛋 = 第 %d 代, 期望仍是 1", gen)
	}
}

// TestMarkHatchedThenClaimChild 破壳记 gid、认领补快照 —— 这两步可以隔很久
// (离线、重启、换设备),认领靠 gid 而不是进程内存里的配对。
func TestMarkHatchedThenClaimChild(t *testing.T) {
	ps := &EggParents{Mother: &EggParent{Gid: 7001, Name: "母本", Species: "火神"}}
	l := &BreedingLine{ID: "l1"}
	gen := AppendPending(l, ps, 6001, 100)

	if !MarkHatched(l, gen, 9001) {
		t.Fatal("破壳没把子代 gid 记到那一代上")
	}
	if l.Pending[0].ChildGid != 9001 {
		t.Errorf("ChildGid = %d, 期望 9001", l.Pending[0].ChildGid)
	}
	// 同一颗蛋重复破壳不改(幂等)
	if MarkHatched(l, gen, 9001) {
		t.Error("重复破壳应无动作")
	}
	// 认领走「按 gid」那条路(连接早已不在的情形)
	child := EggParent{Gid: 9001, Name: "崽", Voice: 70}
	if !ClaimChild(l, 9001, child) {
		t.Fatal("按 gid 认领失败")
	}
	if len(l.Gens) != 1 || l.Gens[0].Child == nil || l.Gens[0].Child.Gid != 9001 {
		t.Fatalf("认领后代数 = %+v", l.Gens)
	}
	if len(l.Pending) != 0 {
		t.Errorf("认领后仍剩 %d 代待处理", len(l.Pending))
	}
	// 认过就别再认一次(同一只宠物可能经多个 opcode 重复下发)
	if ClaimChild(l, 9001, child) {
		t.Error("已认领的代不该再认一次")
	}
	// gid 对不上不该认领
	l2 := &BreedingLine{ID: "l2"}
	AppendPending(l2, ps, 6002, 100)
	MarkHatched(l2, 1, 9002)
	if ClaimChild(l2, 9999, child) {
		t.Error("gid 对不上却认领成功了")
	}
}

// TestFillParentSnapshotOnlyBlanks 补全只填**空缺**的固有属性,形态相关的一律不碰。
func TestFillParentSnapshotOnlyBlanks(t *testing.T) {
	// 收蛋那一刻母本还没进宠物库:快照只有 gid 与名字
	snap := EggParent{Gid: 1, Name: "母本"}
	src := &Pet{Gid: 1, Species: "罗隐", ConfID: 3069, Voice: 40, Nature: "固执",
		TalentRank: "A", HeightM: 1.5, WeightKg: 88}
	if !FillParentSnapshot(&snap, src) {
		t.Fatal("有空缺却报「没补过」")
	}
	if snap.Voice != 40 || snap.Nature != "固执" || snap.Talent != "A" {
		t.Errorf("固有属性没补齐: %+v", snap)
	}
	// ⚠️ 形态相关的不能补:收蛋那一刻她还是阿米亚特,如今已进化成罗隐 ——
	// 补上去就把史实改成现状,而 ChainRef 认品种正是靠这份「当时是谁」。
	if snap.Species != "" || snap.Evo != 0 || snap.ConfID != 0 || snap.Img != "" {
		t.Errorf("回填了形态相关字段: %+v(会把「当时是阿米亚特」改成「罗隐」)", snap)
	}
	// 已有值不被覆盖
	kept := EggParent{Gid: 2, Nature: "胆小", Voice: -30}
	FillParentSnapshot(&kept, &Pet{Gid: 2, Nature: "固执", Voice: 90})
	if kept.Nature != "胆小" || kept.Voice != -30 {
		t.Errorf("覆盖了已有值: %+v", kept)
	}
	// 库里没有那只宠时不动
	miss := EggParent{Gid: 3, Name: "查无此宠"}
	if FillParentSnapshot(&miss, nil) {
		t.Error("没有来源却报「补过了」")
	}
}

// TestBackcrossAdvicePicksOppositeSexParent 回交对象必须是**与子代性别相反**的亲本。
//
// 升级前按「离目标更近」挑,雄性子代会被配给父亲 —— ♂ × ♂ 根本配不出蛋,整条建议是反的。
func TestBackcrossAdvicePicksOppositeSexParent(t *testing.T) {
	m := EggParent{Gid: 1, Name: "母", Gender: "♀", Voice: 90} // 更接近目标 100
	f := EggParent{Gid: 2, Name: "父", Gender: "♂", Voice: 40}
	g := BreedingGoal{Voice: brI32(100)}

	// 子代 ♂ → 只能回配母本(♀)
	son := EggParent{Gid: 9, Name: "崽", Gender: "♂", Voice: 70}
	got := BackcrossAdvice(son, m, f, nil, g)
	if got.WithParent == nil || got.WithParent.Gid != 1 {
		t.Errorf("♂ 子代的回交对象 = %+v, 期望母本( gid 1 )—— 只能回配 ♀", got.WithParent)
	}
	// 子代 ♀ → 只能回配父本(♂)
	daughter := EggParent{Gid: 9, Name: "崽", Gender: "♀", Voice: 70}
	got2 := BackcrossAdvice(daughter, m, f, nil, g)
	if got2.WithParent == nil || got2.WithParent.Gid != 2 {
		t.Errorf("♀ 子代的回交对象 = %+v, 期望父本( gid 2 )—— 只能回配 ♂", got2.WithParent)
	}
	// 性别缺失时退回老口径(按离目标更近挑 → 母本 90 比父本 40 近)
	unknown := EggParent{Gid: 9, Name: "崽", Voice: 70}
	got3 := BackcrossAdvice(unknown, m, f, nil, g)
	if got3.WithParent == nil || got3.WithParent.Gid != 1 {
		t.Errorf("性别未知时 = %+v, 期望按老口径挑母本( gid 1 )", got3.WithParent)
	}
}

// TestSuggestMarksBackcross 「回交」标签要真的亮起来:父本是本线自己的子代时置位。
// 此前 Suggest 从不给它赋值,前端那个标签一直是死的。
func TestSuggestMarksBackcross(t *testing.T) {
	m := brParent(1, "母", 0, 50, "固执")
	sire := brParent(2, "种公", 0, 50, "固执")
	own := brParent(9, "自己的崽", 0, 50, "固执")
	cands := []Candidate{{Mother: m, Fathers: []EggParent{sire, own}}}

	got := Suggest(cands, BreedingGoal{Voice: brI32(100)}, 0, map[uint32]bool{9: true})
	if len(got) != 2 {
		t.Fatalf("组合数 = %d, 期望 2", len(got))
	}
	byFather := map[uint32]Suggestion{}
	for _, s := range got {
		byFather[s.Father.Gid] = s
	}
	if byFather[2].Backcross {
		t.Error("普通种公被标成了回交")
	}
	if !byFather[9].Backcross {
		t.Error("父本是本线子代却没标回交")
	}
	// 不传子代集合时一律不标(老调用方行为不变)
	for _, s := range Suggest(cands, BreedingGoal{Voice: brI32(100)}, 0, nil) {
		if s.Backcross {
			t.Error("没有子代集合却标了回交")
		}
	}
}

// TestReachGoalNatureSet 集合目标下「达成」的判定:子代性格落在集合里就算,不在就不算。
//
// 为什么值得单测:集合是能被手改的(接口不校验名字是否真实存在),而判定写错的方向是
// **假达成** —— 线上被打上「已达成」比漏判更糟,玩家会照着它停手。
func TestReachGoalNatureSet(t *testing.T) {
	row := []string{"逞强", "固执", "大胆", "调皮", "勇敢"}
	line := &BreedingLine{
		Goal: BreedingGoal{NatureIn: row},
		Gens: []Generation{{Gen: 1, Child: brChild(1, "一代", 0, 50, "大胆")}},
	}
	if !ReachGoal(line) {
		t.Error("子代性格在目标集合里,应算达成")
	}
	line.Gens[0].Child = brChild(2, "二代", 0, 50, "胆小")
	if ReachGoal(line) {
		t.Error("子代性格不在目标集合里,不该算达成")
	}
	// 快照缺性格(空名)不算命中:拿空串去比会把「测不出」当成「正好是要的那个」
	line.Gens[0].Child = brChild(3, "三代", 0, 50, "")
	if ReachGoal(line) {
		t.Error("子代性格为空时不该算达成")
	}
	// 集合为空 = 没填性格目标:即使子代性格空着也不能算「性格达标」而过关
	if ReachGoal(&BreedingLine{Goal: BreedingGoal{NatureIn: []string{""}}, Gens: line.Gens}) {
		t.Error("集合里只有空名时等于没填性格目标,不该算达成")
	}
}

// TestScoreOnlyFilledGoals 打分**只对填了的项**计分 —— 否则「只想刷嗓音」的线会被没填的
// 体重项拖着走:一个五音完美的组合会因为体重不是 50% 而被判成差。
func TestScoreOnlyFilledGoals(t *testing.T) {
	// 性格概率取「双亲都没有」那一档即可 —— 这里考的是 Score 的加权口径,不是性格模型本身。
	roll := natureRollChance / natureCount
	e := Expectation{Voice: 100, WeightPct: 40, NatureP: roll}

	if got := Score(e, BreedingGoal{Voice: brI32(100)}); got != 0 {
		t.Errorf("只填嗓音且完全命中 = %.4f, 期望 0", got)
	}
	// 补上体重目标 60%:差距 |40-60|/100 = 0.2,与嗓音的 0 平均 → 0.1
	if got := Score(e, BreedingGoal{Voice: brI32(100), WeightPct: brF64(60)}); math.Abs(got-0.1) > 1e-9 {
		t.Errorf("嗓音+体重 = %.4f, 期望 0.1000", got)
	}
	// 三项全填:嗓音 0、体重 0.2、性格 (1-roll),三者取平均
	want := (0 + 0.2 + (1 - roll)) / 3
	if got := Score(e, BreedingGoal{Voice: brI32(100), WeightPct: brF64(60), Nature: "勇敢"}); math.Abs(got-want) > 1e-9 {
		t.Errorf("三项全填 = %.4f, 期望 %.4f", got, want)
	}
	// 一个都没填:所有组合等价,不能除零
	if got := Score(e, BreedingGoal{}); got != 0 {
		t.Errorf("没填目标 = %.4f, 期望 0", got)
	}
}

// TestSuggestSortsByDistanceAndFlagsAmbiguous 建议按离目标的差距升序,串窝的母本为每个父本
// 候选各出一条并标 Ambiguous —— 不能替玩家猜实际是哪个父本(见 docs/data.md 3.6 的串窝)。
func TestSuggestSortsByDistanceAndFlagsAmbiguous(t *testing.T) {
	cands := []Candidate{
		{ // 单候选母本:嗓音 90 × 100 → 95,还差 5
			Mother:  brParent(10, "狼灵甲", 90, 80, ""),
			Fathers: []EggParent{brParent(11, "狼灵乙", 100, 80, "")},
		},
		{ // 串窝母本:两个父本候选,期望分别是 85 与 100
			Mother:    brParent(20, "狼灵丙", 80, 80, ""),
			Fathers:   []EggParent{brParent(21, "候选一", 90, 80, ""), brParent(22, "候选二", 120, 80, "")},
			Ambiguous: true,
		},
	}
	got := Suggest(cands, BreedingGoal{Voice: brI32(100)}, 0, nil)
	if len(got) != 3 {
		t.Fatalf("组合数 = %d, 期望 3(串窝母本出两条)", len(got))
	}
	if got[0].Father.Gid != 22 {
		t.Errorf("首条 = 父 %d, 期望 22(80 与 120 的均值正好 100,与目标零差距)", got[0].Father.Gid)
	}
	for i := 1; i < len(got); i++ {
		if got[i-1].Score > got[i].Score {
			t.Errorf("第 %d 条(%f)比第 %d 条(%f)更差,排序不是升序", i-1, got[i-1].Score, i, got[i].Score)
		}
	}
	n := 0
	for _, s := range got {
		if s.Ambiguous {
			n++
		}
	}
	if n != 2 {
		t.Errorf("标了串窝的组合 = %d, 期望 2(串窝母本的两个候选都要标)", n)
	}
	// top N 截断
	if top := Suggest(cands, BreedingGoal{Voice: brI32(100)}, 2, nil); len(top) != 2 {
		t.Errorf("top2 返回 %d 条", len(top))
	}
}

// TestVoiceReachFloorMean 可达性:子代嗓音 = floor((母+父)/2),故目标只有「双亲都到位」
// 才够得着,差一点也不行 —— 且正负对称(+100 与 -100 是同一条规则)。
func TestVoiceReachFloorMean(t *testing.T) {
	// 一位母本 × 若干父本候选,是最小的候选池形状。
	pool := func(mv int32, fvs ...int32) []Candidate {
		c := Candidate{Mother: brParent(1, "母", mv, 80, "")}
		for i, fv := range fvs {
			c.Fathers = append(c.Fathers, brParent(uint32(10+i), "父", fv, 80, ""))
		}
		return []Candidate{c}
	}

	if got := VoiceReachOf(pool(100, 100), 100); !got.Hit || got.Best != 100 {
		t.Errorf("100 × 100 → %+v, 期望命中 100", got)
	}
	// 99 × 100 → 99:离目标只差 1,但迭代多少次都到不了(这是本次要提示给玩家的情形)。
	if got := VoiceReachOf(pool(100, 99), 100); got.Hit || got.Best != 99 {
		t.Errorf("99 × 100 → %+v, 期望不命中且 Best=99", got)
	}
	// 负向**不是**简单镜像:floor 朝负无穷取整,故 -100 × -99 → floor(-99.5) = -100 够得着,
	// 而 -99 × -99 → -99 够不着。可达性只能按公式逐组枚举,不能拿「±对称」去猜。
	if got := VoiceReachOf(pool(-100, -99), -100); !got.Hit || got.Best != -100 {
		t.Errorf("-100 × -99 → %+v, 期望命中 -100(floor 朝负无穷)", got)
	}
	if got := VoiceReachOf(pool(-99, -99), -100); got.Hit || got.Best != -99 {
		t.Errorf("-99 × -99 → %+v, 期望不命中且 Best=-99", got)
	}
	// 多候选:取**离目标最近**的那一组,不是嗓音最大的那一组(目标 100 时二者恰好一致,
	// 故这里用两位母本把两种口径区分开)。
	cands := append(pool(40, 88, 60), Candidate{
		Mother:  brParent(2, "母二", 90, 80, ""),
		Fathers: []EggParent{brParent(20, "父二", 100, 80, "")},
	})
	if got := VoiceReachOf(cands, 100); got.Hit || got.Best != 95 {
		t.Errorf("多候选池 → %+v, 期望 Best=95(90 与 100 的均值)且不命中", got)
	}
	// 空池:够不着就报够不着 —— 刚建的空线不该被显示成「目标已可达」。
	if got := VoiceReachOf(nil, 100); got.Hit || got.Best != 100 {
		t.Errorf("空池 → %+v, 期望不命中", got)
	}
}

// TestBackcrossPrefersChildOverPoorParent 子代已优于双亲时,回交是首选(把好基因固定下来)。
func TestBackcrossPrefersChildOverPoorParent(t *testing.T) {
	m := brParent(1, "母", 80, 80, "")
	f := brParent(2, "父", 80, 80, "")
	child := brParent(3, "子代", 96, 90, "")
	got := BackcrossAdvice(child, m, f, nil, BreedingGoal{Voice: brI32(100)})
	if !got.Advised {
		t.Errorf("没有别的候选时 = 不建议回交(%s), 期望建议", got.Reason)
	}
	if got.WithParent == nil || got.WithParent.Gid != 1 {
		t.Errorf("回交对象 = %v, 期望母本(双亲同分时取其一)", got.WithParent)
	}
	if want := int32(88); got.Exp.Voice != want { // floor((96+80)/2)
		t.Errorf("回交预期嗓音 = %d, 期望 %d", got.Exp.Voice, want)
	}
}

// TestBackcrossRejectsWhenPoolIsBetter 池子里有更好的种公时不该硬劝回交 —— 回交会放弃
// 一次性拉近目标的机会,两种取舍都常见,故要把两边数字都摆出来(Advised 只表示谁更优)。
func TestBackcrossRejectsWhenPoolIsBetter(t *testing.T) {
	m := brParent(1, "母", 80, 80, "")
	f := brParent(2, "父", 80, 80, "")
	child := brParent(3, "子代", 96, 90, "")
	// 别的候选更接近目标:96 × 98 → 97,比拼回交的 88 近得多
	pool := []EggParent{
		brParent(1, "母", 80, 80, ""),  // 池子里含亲本,应被排除
		brParent(3, "子代", 96, 90, ""), // 也不该拿自己配自己
		brParent(4, "狼王", 98, 90, ""),
	}
	got := BackcrossAdvice(child, m, f, pool, BreedingGoal{Voice: brI32(100)})
	if got.Advised {
		t.Errorf("池子里有 98 的候选时仍建议回交(%s)", got.Reason)
	}
	if got.Alt == nil || got.Alt.Gid != 4 {
		t.Fatalf("替代候选 = %v, 期望 4(98 那只)", got.Alt)
	}
	if got.AltExp.Voice != 97 { // floor((96+98)/2)
		t.Errorf("换种预期嗓音 = %d, 期望 97", got.AltExp.Voice)
	}
	if got.Exp.Voice != 88 {
		t.Errorf("回交预期嗓音 = %d, 期望 88 —— 两边数字都要给出才能让玩家自己权衡", got.Exp.Voice)
	}
}

// TestBackcrossWithoutChild 还没孵出子代时不该硬给回交建议(没有可回交的对象)。
func TestBackcrossWithoutChild(t *testing.T) {
	m := brParent(1, "母", 80, 80, "")
	f := brParent(2, "父", 80, 80, "")
	got := BackcrossAdvice(EggParent{}, m, f, nil, BreedingGoal{Voice: brI32(100)})
	if got.Advised {
		t.Error("没有子代却建议了回交")
	}
	if got.Reason == "" {
		t.Error("没有子代时理由为空 —— 面板上会显示成一块无字的建议")
	}
}

// TestLineStats 汇总口径:填了目标取**离目标最近**的(玩家要的是达标);没填目标时取
// 最极端的(收集向玩法要的就是极端个体)。
func TestLineStats(t *testing.T) {
	line := &BreedingLine{
		Gens: []Generation{
			{Gen: 1, Child: brChild(1, "一代", -50, 60, "")},
			{Gen: 2, Child: brChild(2, "二代", 70, 70, "")},
			{Gen: 3, Child: nil}, // 待认领的一代不参与统计
			{Gen: 4, Child: brChild(4, "四代", 96, 80, "")},
		},
	}
	gens, bv, bw := LineStats(line)
	if gens != 4 {
		t.Errorf("代数 = %d, 期望 4(含待认领的一代)", gens)
	}
	if bv == nil || *bv != 96 { // 无目标:|96| 比 |-50| 更极端
		t.Errorf("无目标时最佳嗓音 = %v, 期望 96(绝对值最大)", bv)
	}
	if bw == nil || *bw != 80 {
		t.Errorf("无目标时最佳体重 = %v, 期望 80(最重)", bw)
	}

	line.Goal = BreedingGoal{Voice: brI32(100)}
	_, bv, _ = LineStats(line)
	if bv == nil || *bv != 96 {
		t.Errorf("目标嗓音 100 时最佳 = %v, 期望 96(离目标最近)", bv)
	}
	line.Goal = BreedingGoal{WeightPct: brF64(60)}
	_, _, bw = LineStats(line)
	if bw == nil || *bw != 60 {
		t.Errorf("目标体重 60%% 时最佳 = %v, 期望 60", bw)
	}
	if _, bv, bw := LineStats(nil); bv != nil || bw != nil {
		t.Error("nil 培育线不该panic,也不该给出最佳值")
	}
}

// TestReachGoal 达成判定:存在某一代的子代**同时**满足全部已填目标项。
//
// 三项口径各配一个反例,因为它们都容易被「顺手放宽」改坏:嗓音是确定值(差 1 就是没到)、
// 体重允许 2pp(实测波动,精确相等等于永不达标)、性格全等。
func TestReachGoal(t *testing.T) {
	cases := []struct {
		name  string
		goal  BreedingGoal
		child *EggParent
		want  bool
	}{
		{"嗓音命中", BreedingGoal{Voice: brI32(100)}, brChild(1, "一代", 100, 50, ""), true},
		{"嗓音差 1 不算命中", BreedingGoal{Voice: brI32(100)}, brChild(1, "一代", 99, 50, ""), false},
		{"体重在 2pp 容差内", BreedingGoal{WeightPct: brF64(90)}, brChild(1, "一代", 0, 88, ""), true},
		{"体重超出 2pp", BreedingGoal{WeightPct: brF64(90)}, brChild(1, "一代", 0, 87.9, ""), false},
		{"体重未知不算命中", BreedingGoal{WeightPct: brF64(90)}, &EggParent{Gid: 1, Name: "一代"}, false},
		{"性格全等", BreedingGoal{Nature: "胆小"}, brChild(1, "一代", 0, 50, "胆小"), true},
		{"性格不同不算命中", BreedingGoal{Nature: "胆小"}, brChild(1, "一代", 0, 50, "固执"), false},
		{
			"三项同时满足",
			BreedingGoal{Voice: brI32(100), WeightPct: brF64(90), Nature: "胆小"},
			brChild(1, "一代", 100, 91, "胆小"), true,
		},
		{
			"三项里缺一项",
			BreedingGoal{Voice: brI32(100), WeightPct: brF64(90), Nature: "胆小"},
			brChild(1, "一代", 100, 91, "固执"), false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			line := &BreedingLine{Goal: c.goal, Gens: []Generation{{Gen: 1, Child: c.child}}}
			if got := ReachGoal(line); got != c.want {
				t.Errorf("ReachGoal = %v, 期望 %v", got, c.want)
			}
		})
	}

	// 两项被**不同**子代分别满足时不算达成 —— 玩家要的是那一只「大块头婉转声」。
	split := &BreedingLine{
		Goal: BreedingGoal{Voice: brI32(100), WeightPct: brF64(90)},
		Gens: []Generation{
			{Gen: 1, Child: brChild(1, "嗓音够", 100, 40, "")},
			{Gen: 2, Child: brChild(2, "体重够", 80, 90, "")},
		},
	}
	if ReachGoal(split) {
		t.Error("两项分别被不同子代满足时判成了达成 —— 玩家要的是一只同时满足的个体")
	}

	// 没填目标恒为 false:否则新建的空线一进来就是「已达成」。
	if ReachGoal(&BreedingLine{Gens: []Generation{{Gen: 1, Child: brChild(1, "一代", 100, 100, "")}}}) {
		t.Error("没填任何目标时判成了达成")
	}
	// 待认领的一代(Child 为空)不参与。
	if ReachGoal(&BreedingLine{Goal: BreedingGoal{Voice: brI32(100)}, Gens: []Generation{{Gen: 1}}}) {
		t.Error("只有待认领的一代时判成了达成")
	}
	if ReachGoal(nil) {
		t.Error("nil 培育线判成了达成")
	}
}

// TestAutoDoneOnReach 自动置 done 只在「由未达成变达成」时动手。
//
// 这条最要紧的是第二种情形:玩家可以手动把线改回「进行中」接着刷。若只看当前结果,下一次
// 写入(又一次破壳、又一次改目标)就把他掰回去了,手动选择等于无效。
func TestAutoDoneOnReach(t *testing.T) {
	reached := func(status string) *BreedingLine {
		return &BreedingLine{
			Status: status,
			Goal:   BreedingGoal{Voice: brI32(100)},
			Gens:   []Generation{{Gen: 1, Child: brChild(1, "一代", 100, 50, "")}},
		}
	}

	l := reached(BreedingActive)
	if !AutoDoneOnReach(l, false) || l.Status != BreedingDone {
		t.Errorf("由未达成变达成时 status = %q, 期望自动置 %q", l.Status, BreedingDone)
	}
	// 早已达成(before=true):不动手 —— 这就是「手动改回进行中」的保护。
	back := reached(BreedingActive)
	if AutoDoneOnReach(back, true) || back.Status != BreedingActive {
		t.Errorf("已达成过的线又被自动置 done(status = %q)", back.Status)
	}
	// 玩家手动设成的其它状态不被覆盖。
	arch := reached(BreedingArchived)
	if AutoDoneOnReach(arch, false) || arch.Status != BreedingArchived {
		t.Errorf("归档的线被自动置 done(status = %q)", arch.Status)
	}
	// 还没达成:不动。
	todo := &BreedingLine{
		Status: BreedingActive,
		Goal:   BreedingGoal{Voice: brI32(100)},
		Gens:   []Generation{{Gen: 1, Child: brChild(1, "一代", 99, 50, "")}},
	}
	if AutoDoneOnReach(todo, false) || todo.Status != BreedingActive {
		t.Errorf("未达成的线被置成了 %q", todo.Status)
	}
	if AutoDoneOnReach(nil, false) {
		t.Error("nil 培育线被判成需要改状态")
	}
}

// TestBreedCandidates 补录候选池的三类范围与顺序。
//
// 范围与前端 pets.js 的三个纯函数一一对应(种母 = 同品种 ♀、子代 = 同品种、种公 = ♂ 且
// 按该品种雌性的蛋组粗筛)。最要紧的是**粗筛只许漏不许挡**:蛋组未知的雌性在前端口径里
// 是「不限蛋组」,这里若跟着筛,她的种公就被清空了 —— 那一刻玩家在补录面板里选不出父亲。
//
// 这里用**无链**的品种(ChainRef.Species)走名字匹配那条路;链口径见 TestChainRefMatch。
func TestBreedCandidates(t *testing.T) {
	eggs := func(names ...string) []gamedata.EggGroup {
		out := make([]gamedata.EggGroup, 0, len(names))
		for _, n := range names {
			out = append(out, gamedata.EggGroup{Name: n})
		}
		return out
	}
	mk := func(gid uint32, name, species, gender string, groups ...string) *Pet {
		return &Pet{Gid: gid, Name: name, Species: species, Gender: gender, EggGroups: eggs(groups...)}
	}
	pets := []*Pet{
		mk(1, "乙母", "火神", "♀", "龙"),
		mk(2, "甲公", "火神", "♂", "龙"),   // 与"龙"有交 → 进种公
		mk(3, "丙公", "火神", "♂", "虫"),   // 蛋组不交 → 被粗筛掉
		mk(4, "甲母", "火神", "♀", "龙"),
		mk(5, "小子", "火神", "♂", "龙"),
		mk(6, "外人", "水灵", "♀", "龙"), // 别的品种 → 三类都不进
	}

	// 顺序只校验性质(名字升序)与集合相等,不硬编码中文的码点顺序 —— 那既容易写错,
	// 也会在换排序键时无意义地失败。
	assertCands := func(label string, cs []PetCandidate, want ...string) {
		t.Helper()
		got := candNames(cs)
		if len(got) != len(want) {
			t.Fatalf("%s = %v, 期望 %v", label, got, want)
		}
		seen := map[string]int{}
		for _, n := range got {
			seen[n]++
		}
		for _, n := range want {
			if seen[n] == 0 {
				t.Fatalf("%s = %v, 期望含 %q", label, got, n)
			}
			seen[n]--
		}
		for i := 1; i < len(cs); i++ {
			if cs[i-1].Name > cs[i].Name {
				t.Errorf("%s 未按名字升序:%q 排在了 %q 之前", label, cs[i-1].Name, cs[i].Name)
			}
		}
	}

	ref := ChainRef{Species: "火神"} // 无链形态:按名字认品种(见 ChainRef.Match)
	mothers, fathers, kids := BreedCandidates(ref, pets)
	assertCands("种母", mothers, "乙母", "甲母")
	assertCands("种公", fathers, "甲公", "小子")
	assertCands("子代", kids, "乙母", "甲公", "丙公", "甲母", "小子")
	for _, cs := range [][]PetCandidate{mothers, fathers, kids} {
		for _, c := range cs {
			if c.Species != "火神" {
				t.Errorf("候选里混进了别的品种:%+v", c)
			}
		}
	}

	// 同名时按 gid 降序:刷了一窝同名个体时,最新获得的那只排在前面。
	dupes := []*Pet{mk(10, "同名", "火神", "♂", "龙"), mk(11, "同名", "火神", "♂", "龙")}
	if _, f, _ := BreedCandidates(ref, dupes); f[0].Gid != 11 {
		t.Errorf("同名候选首个 gid = %d, 期望 11(同名按 gid 降序)", f[0].Gid)
	}

	// 关键:任意一只雌性的蛋组未知 → 整个不做粗筛。否则「丙公」(虫组)会被挡掉,
	// 而前端对那只母本的口径是不限蛋组,玩家就永远选不到它。
	mixed := append(append([]*Pet{}, pets...), mk(7, "无组母", "火神", "♀"))
	if _, f, _ := BreedCandidates(ref, mixed); len(f) != 3 {
		t.Errorf("有雌性蛋组未知时种公 = %d 只, 期望 3(粗筛整体停用,宁多勿漏)", len(f))
	}

	// 空品种与库里没有该品种:三类都空,且不 panic。
	if m, f, k := BreedCandidates(ChainRef{}, pets); len(m)+len(f)+len(k) != 0 {
		t.Error("品种为空时不该给出候选")
	}
	if m, f, k := BreedCandidates(ChainRef{Species: "不存在的品种"}, pets); len(m)+len(f)+len(k) != 0 {
		t.Error("库里没有该品种时不该给出候选")
	}
}

// TestChainRefMatch 品种的口径:**一条进化链上的各阶段是同一个品种**,而不同形态各占一条链。
//
// 这两条正是这个页面此前配不出候选的原因:
//   - 按名字认品种时,「线记的是罗隐、窝里那只却是阿米亚特♀」被判成别的品种,候选池直接空掉 ——
//     而蛋的物种随母本,链上任一阶段的 ♀ 都能配出这个品种;
//   - 反过来,同名的两个形态(嗜波螺「本来的样子」/「被污染的样子」)各是一条链,按名字认
//     会让它们互相串味。
//
// 样本按形状找、不写死 id(解包数据换版本后 id 会变,见 pet_image_test.go 的同款做法)。
func TestChainRefMatch(t *testing.T) {
	db, err := gamedata.Load()
	if err != nil {
		t.Fatalf("载入 gamedata 失败: %v", err)
	}
	// 链条目里各形态名(同名只留一个),用来挑两个名字不同的阶段。
	membersOf := func(evo uint32) []struct {
		base uint32
		name string
	} {
		seen := map[string]bool{}
		var out []struct {
			base uint32
			name string
		}
		for _, b := range db.ChainMembers(evo) {
			info, ok := db.PetBase(b)
			if !ok || info.Name == "" || seen[info.Name] {
				continue
			}
			seen[info.Name] = true
			out = append(out, struct {
				base uint32
				name string
			}{b, info.Name})
		}
		return out
	}

	var evo uint32
	var ms []struct {
		base uint32
		name string
	}
	for _, f := range db.PetForms() {
		if e := db.ChainOf(f.Base); e != 0 {
			if m := membersOf(e); len(m) >= 2 {
				evo, ms = e, m
				break
			}
		}
	}
	if evo == 0 {
		t.Skip("本版本没有多阶段的进化链")
	}
	// 再找一个**别的**链,用来验证「链不同即品种不同」。
	var otherEvo uint32
	for _, f := range db.PetForms() {
		if e := db.ChainOf(f.Base); e != 0 && e != evo {
			otherEvo = e
			break
		}
	}
	later := ms[1] // 链上另一个阶段(名字与初始形态不同)
	ref := ChainRefOf(db, evo, ms[0].name)
	egg := []gamedata.EggGroup{{Name: "龙"}}
	mother := &Pet{Gid: 1, Name: "窝里这只", Species: later.name, Gender: "♀", BaseConfID: later.base, EggGroups: egg}
	father := &Pet{Gid: 2, Name: "种公", Species: ms[0].name, Gender: "♂", BaseConfID: ms[0].base, EggGroups: egg}
	pets := []*Pet{mother, father}
	if !ref.Match(mother) || !ref.Match(father) {
		t.Fatalf("链 %d 上的 %s/%s 没被认成「%s」这个品种的一员", evo, later.name, ms[0].name, ms[0].name)
	}
	if got := BreedPool(ref, pets); len(got) != 1 || got[0].Mother.Species != later.name {
		t.Fatalf("候选池 = %+v, 期望把链上另一阶段的 %s 当种母", got, later.name)
	}
	// 反过来:同一只个体在「只认名字」的引用下不匹配(这正是升级前的行为)。
	if (ChainRef{Species: ms[0].name}).Match(mother) {
		t.Errorf("按名字认品种时 %s 竟匹配上了 %s", ms[0].name, later.name)
	}
	// 另一条链上的个体一律不进来:链不同即品种不同。
	if bs := db.ChainMembers(otherEvo); len(bs) > 0 && ref.Match(&Pet{Species: later.name, BaseConfID: bs[0]}) {
		t.Errorf("链 %d 的个体被认成了链 %d 的品种", otherEvo, evo)
	}
	if ref.Match(nil) {
		t.Error("nil 个体不该匹配任何品种")
	}

	// 无链形态(Evo==0)按名字认:与升级前一致,否则那些首领/活动形态连一只候选都配不出来。
	for _, f := range db.PetForms() {
		info, ok := db.PetBase(f.Base)
		if !ok || db.ChainOf(f.Base) != 0 {
			continue
		}
		nref := ChainRefOf(db, 0, info.Name)
		if !nref.Match(&Pet{Species: info.Name, BaseConfID: f.Base}) {
			t.Errorf("无链形态 %s 按名字没匹配上", info.Name)
		}
		if nref.Match(&Pet{Species: info.Name + "·别的"}) {
			t.Errorf("无链引用按名字匹配到了别的品种(%s)", info.Name)
		}
		// 老线(只存了名字)补身份:名字唯一对应一条链时补上(与 ChainRefOf 同口径),
		// 否则保持 0(继续按名字认,见 DeriveChain)。
		l := &BreedingLine{Species: info.Name}
		DeriveChain(db, l)
		if want := db.ChainByName(info.Name); l.Evo != want {
			t.Errorf("老线 %s 补出的 Evo = %d, 期望 %d", info.Name, l.Evo, want)
		}
		break
	}

	// 名字唯一对应一条链的形态:补出来的必须是链,且按它建的引用认链上的全部阶段。
	for _, f := range db.PetForms() {
		info, ok := db.PetBase(f.Base)
		if !ok || info.Form != "" || db.ChainOf(f.Base) == 0 {
			continue
		}
		if evo := db.ChainByName(info.Name); evo == db.ChainOf(f.Base) {
			l := &BreedingLine{Species: info.Name}
			DeriveChain(db, l)
			if l.Evo == 0 {
				t.Errorf("%s 老线没补出品种(名字唯一对应链 %d)", info.Name, evo)
			}
			if !ChainRefOf(db, l.Evo, l.Species).Match(&Pet{Species: info.Name, BaseConfID: f.Base}) {
				t.Errorf("补出品种后 %s 的引用反而不认它自己了", info.Name)
			}
			break
		}
	}
}

// candNames 取候选名字序列,供顺序断言。
func candNames(cs []PetCandidate) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.Name)
	}
	return out
}
