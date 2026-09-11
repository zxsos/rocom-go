package pet

import (
	"fmt"
	"math"
	"testing"

	"github.com/zxsos/roco-go/internal/gamedata"
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

// brGendered 在 brChild 的基础上补一个性别 —— 性别目标只跟它比(其它 helper 不带性别)。
func brGendered(gid uint32, name string, voice int32, pct float64, nature, gender string) *EggParent {
	p := brChild(gid, name, voice, pct, nature)
	p.Gender = gender
	return p
}

// TestPredictVoiceAndWeight 嗓音与体重的预测口径。
//
// 嗓音取双亲均值**向零取整**(正数向下、负数向上,见 breeding.go 的 voiceOf);体重取双亲百分位
// 均值,并给一个乐观上界 —— 两条实测都比均值高(94.610 → 96.332 是 +1.72pp,99.754 → 100 是
// +0.25pp),故上界取均值 +2pp 且不超过 100。这里用实测样本当输入,免得口径漂了没人发现。
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
		{"非负嗓音向下取整:61 与 62 → 61", 61, 62, 50, 50, 61, 50, 52},
		{"负嗓音向上取整(向零):-61 与 -62 → -61", -61, -62, 1, 1, -61, 1, 3},
		{"双亲百分位不同则取均值", 0, 0, 40, 80, 0, 60, 62},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := brParent(1, "母", c.mv, c.mw, "")
			f := brParent(2, "父", c.fv, c.fw, "")
			got := Predict(m, f, BreedingGoal{})
			if got.Voice != c.wantVoice {
				t.Errorf("嗓音 = %d, 期望 %d(双亲均值向零取整)", got.Voice, c.wantVoice)
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

// brPool 按「每位母本 × 它的父本列表」造候选池(测试用)。
//
// 真池子的父本是**共用**的(Candidate.FatherIdx 指向 Pool.Fathers),这里不去重 ——
// 按给的顺序追加进父本表、下标自然对上,组合的遍历次序与逐母本各存一份时完全一致。
func brPool(mothers []EggParent, fatherLists ...[]EggParent) Pool {
	p := Pool{}
	for i, m := range mothers {
		c := Candidate{Mother: m, Ambiguous: len(fatherLists[i]) > 1}
		for _, f := range fatherLists[i] {
			c.FatherIdx = append(c.FatherIdx, int32(len(p.Fathers)))
			p.Fathers = append(p.Fathers, f)
		}
		p.Cands = append(p.Cands, c)
	}
	return p
}

// refSuggest 是 Suggest 的**参照实现**:全量物化 + 稳定排序 + 截断(优化前的做法)。
//
// 留着它只为一个目的:证明改成「单趟维护 top-N」之后结果没变。算法一换,最怕的就是
// 输出悄悄漂移 —— 契约 golden 只有 3 组建议、覆盖不到等价元素的次序,而这条能。
func refSuggest(pool Pool, g BreedingGoal, n int, childGids map[uint32]bool) []Suggestion {
	var out []Suggestion
	for _, c := range pool.Cands {
		for _, idx := range c.FatherIdx {
			f := pool.Fathers[idx]
			e := Predict(c.Mother, f, g)
			out = append(out, Suggestion{
				Mother:    c.Mother,
				Father:    f,
				Exp:       e,
				Score:     Score(e, g),
				Ambiguous: c.Ambiguous,
				Backcross: childGids[f.Gid],
			})
		}
	}
	sortSuggestions(out)
	if n > 0 && len(out) > n {
		out = out[:n]
	}
	return out
}

// TestSuggestTopNMatchesFullSort 单趟 top-N 必须与「全量稳定排序后取前 n」**逐条一致**。
//
// 组合数远超 n,且刻意造出**完全等价**的组合(双亲属性一模一样 → Score/WeightHi/Voice
// 三键全等):这类元素的次序最容易在换算法时漂移,而页面虽看不出、契约 golden 也未必覆盖。
func TestSuggestTopNMatchesFullSort(t *testing.T) {
	// 每只属性只取少数几种取值 → 大量组合三键全等
	mk := func(gid uint32, gender string, voice int32, pct float64) *Pet {
		return &Pet{Gid: gid, Species: "火神", BaseConfID: 3006, Gender: gender,
			Voice: voice, WeightPct: &pct, EggGroups: []gamedata.EggGroup{{Name: "龙"}}}
	}
	pets := []*Pet{
		mk(1, "♀", 40, 60), mk(2, "♀", 40, 60), mk(3, "♀", -20, 60),
		mk(101, "♂", 88, 60), mk(102, "♂", 88, 60), mk(103, "♂", 0, 60), mk(104, "♂", 0, 60),
	}
	pool := BreedPool(ChainRef{Evo: 1, Bases: map[uint32]bool{3006: true}}, pets)
	if len(pool.Cands) == 0 {
		t.Fatal("候选池为空")
	}
	v, w := int32(96), 98.0
	goal := BreedingGoal{Voice: &v, WeightPct: &w, Nature: "固执"}

	for _, n := range []int{1, 3, 5, 100} {
		got := Suggest(pool, goal, n, nil)
		want := refSuggest(pool, goal, n, nil)
		if len(got) != len(want) {
			t.Fatalf("n=%d 条数 = %d, 期望 %d", n, len(got), len(want))
		}
		for i := range want {
			if got[i].Mother.Gid != want[i].Mother.Gid || got[i].Father.Gid != want[i].Father.Gid {
				t.Errorf("n=%d 第 %d 条 = 母%d×父%d, 期望 母%d×父%d(等价元素的次序漂移了)",
					n, i, got[i].Mother.Gid, got[i].Father.Gid, want[i].Mother.Gid, want[i].Father.Gid)
			}
			if !brNear(got[i].Score, want[i].Score) {
				t.Errorf("n=%d 第 %d 条分数 = %v, 期望 %v", n, i, got[i].Score, want[i].Score)
			}
			if got[i].Ambiguous != want[i].Ambiguous {
				t.Errorf("n=%d 第 %d 条 Ambiguous = %v, 期望 %v", n, i, got[i].Ambiguous, want[i].Ambiguous)
			}
		}
	}
}

// TestTopNInsertKeepsEarlierOnTie 池满时,与末位**等价**的新元素不能挤掉先来者。
// 这正是「稳定排序」的语义;若实现成抢占,契约 golden 与上面那条等价性测试都会红。
func TestTopNInsertKeepsEarlierOnTie(t *testing.T) {
	eq := func(gid uint32) Suggestion {
		return Suggestion{Mother: EggParent{Gid: gid}, Score: 0.5, Exp: Expectation{Voice: 10, WeightHi: 50}}
	}
	// 三个完全等价的元素依次入池(容量 2):留下来的必须是前两个
	top := topNInsert(topNInsert(topNInsert(nil, eq(1), 2), eq(2), 2), eq(3), 2)
	if len(top) != 2 || top[0].Mother.Gid != 1 || top[1].Mother.Gid != 2 {
		t.Fatalf("等价元素被后来的挤掉了: %d,%d", top[0].Mother.Gid, top[1].Mother.Gid)
	}
	// 更好的元素应当插到前面
	better := Suggestion{Mother: EggParent{Gid: 9}, Score: 0.1, Exp: Expectation{Voice: 10, WeightHi: 50}}
	top = topNInsert(top, better, 2)
	if top[0].Mother.Gid != 9 {
		t.Errorf("更好的元素应排在最前,实则 %d", top[0].Mother.Gid)
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
	cands := brPool([]EggParent{m}, []EggParent{sire, own})

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
	// 「达标即满分」:方向化后超过目标(高目标)或更轻(低目标)都算 0 分 ——
	// 不该再因为「离目标更远」被扣分,否则达标的组合会被排到未达标的后面。
	if got := Score(Expectation{Voice: 100}, BreedingGoal{Voice: brI32(90)}); got != 0 {
		t.Errorf("嗓音超过目标(100 ≥ 90) = %.4f, 期望 0", got)
	}
	if got := Score(Expectation{WeightPct: 20}, BreedingGoal{WeightPct: brF64(30)}); got != 0 {
		t.Errorf("低体重目标(20 ≤ 30) = %.4f, 期望 0", got)
	}
	// 未达标才扣分:目标 90 只到 80 → |80-90|/100 = 0.1
	if got := Score(Expectation{WeightPct: 80}, BreedingGoal{WeightPct: brF64(90)}); math.Abs(got-0.1) > 1e-9 {
		t.Errorf("体重未达标 = %.4f, 期望 0.1000", got)
	}
	// 一个都没填:所有组合等价,不能除零
	if got := Score(e, BreedingGoal{}); got != 0 {
		t.Errorf("没填目标 = %.4f, 期望 0", got)
	}
}

// TestScoreIgnoresGender 性别目标**不进 Score**:它在破壳前不可预测,双亲怎么配都一样,
// 计进去只会给每条建议加同一个常数、把「差多少」这个数搅浑。
func TestScoreIgnoresGender(t *testing.T) {
	e := Expectation{Voice: 96, WeightPct: 60, NatureP: 0.5}
	if withG, without := Score(e, BreedingGoal{Gender: "♀"}), Score(e, BreedingGoal{}); withG != without {
		t.Errorf("只填性别 = %.4f, 没填 = %.4f —— 性别不该影响打分", withG, without)
	}
	if withG, without := Score(e, BreedingGoal{Voice: brI32(96), Gender: "♀"}), Score(e, BreedingGoal{Voice: brI32(96)}); withG != without {
		t.Error("性别目标不该改变其它已填项的打分")
	}
}

// TestSuggestSortsByDistanceAndFlagsAmbiguous 建议按离目标的差距升序,串窝的母本为每个父本
// 候选各出一条并标 Ambiguous —— 不能替玩家猜实际是哪个父本(见 docs/data.md 3.6 的串窝)。
func TestSuggestSortsByDistanceAndFlagsAmbiguous(t *testing.T) {
	cands := brPool(
		[]EggParent{
			brParent(10, "狼灵甲", 90, 80, ""), // 单候选母本:嗓音 90 × 100 → 95,还差 5
			brParent(20, "狼灵丙", 80, 80, ""), // 串窝母本:两个父本候选,期望分别是 85 与 100
		},
		[]EggParent{brParent(11, "狼灵乙", 100, 80, "")},
		[]EggParent{brParent(21, "候选一", 90, 80, ""), brParent(22, "候选二", 120, 80, "")},
	)
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

// TestVoiceReachRoundTowardZero 可达性:子代嗓音 = 双亲均值**向零取整**(正数向下、负数向上),
// 故目标只有「双亲都到位」才够得着,差一点也不行 —— 且正负对称(+100 与 -100 是同一条规则)。
func TestVoiceReachRoundTowardZero(t *testing.T) {
	// 一位母本 × 若干父本候选,是最小的候选池形状。
	pool := func(mv int32, fvs ...int32) Pool {
		fathers := make([]EggParent, 0, len(fvs))
		for i, fv := range fvs {
			fathers = append(fathers, brParent(uint32(10+i), "父", fv, 80, ""))
		}
		return brPool([]EggParent{brParent(1, "母", mv, 80, "")}, fathers)
	}

	if got := VoiceReachOf(pool(100, 100), 100); !got.Hit || got.Best != 100 {
		t.Errorf("100 × 100 → %+v, 期望命中 100", got)
	}
	// 99 × 100 → 99:离目标只差 1,但迭代多少次都到不了(这是本次要提示给玩家的情形)。
	if got := VoiceReachOf(pool(100, 99), 100); got.Hit || got.Best != 99 {
		t.Errorf("99 × 100 → %+v, 期望不命中且 Best=99", got)
	}
	// 负向与正向对称:取整朝零,故 -100 × -99 → -99(不是 -100),够不到目标 -100;
	// 只有 -100 × -100 才够得着。
	if got := VoiceReachOf(pool(-100, -100), -100); !got.Hit || got.Best != -100 {
		t.Errorf("-100 × -100 → %+v, 期望命中 -100", got)
	}
	if got := VoiceReachOf(pool(-100, -99), -100); got.Hit || got.Best != -99 {
		t.Errorf("-100 × -99 → %+v, 期望不命中且 Best=-99(取整朝零)", got)
	}
	// 多候选:目标 100 在中心 0 之上(方向为高),Best 取**目标方向上的极值**(此处即最大)。
	multi := brPool(
		[]EggParent{brParent(1, "母", 40, 80, ""), brParent(2, "母二", 90, 80, "")},
		[]EggParent{brParent(10, "父", 88, 80, ""), brParent(11, "父", 60, 80, "")},
		[]EggParent{brParent(20, "父二", 100, 80, "")},
	)
	if got := VoiceReachOf(multi, 100); got.Hit || got.Best != 95 {
		t.Errorf("多候选池 → %+v, 期望 Best=95(90 与 100 的均值)且不命中", got)
	}
	// 方向化后 Best 取方向极值、不是「离目标最近」:可达集 {95,100} 对目标 96,「最近」会挑到
	// 反方向的 95(判够不着),而 100 其实已经达标 —— 照它推荐会让玩家白配好几代。
	reach := brPool(
		[]EggParent{brParent(1, "母", 90, 80, ""), brParent(2, "母二", 100, 80, "")},
		[]EggParent{brParent(10, "父", 100, 80, "")},
		[]EggParent{brParent(11, "父二", 100, 80, "")},
	)
	if got := VoiceReachOf(reach, 96); !got.Hit || got.Best != 100 {
		t.Errorf("可达集 {95,100} 对目标 96 → %+v, 期望命中且 Best=100", got)
	}
	// 空池:够不着就报够不着 —— 刚建的空线不该被显示成「目标已可达」。
	if got := VoiceReachOf(Pool{}, 100); got.Hit || got.Best != 100 {
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

// TestLineStats 汇总口径:填了目标取「**达标优先,其次离目标最近**」(玩家要的是达标);
// 没填目标时取最极端的(收集向玩法要的就是极端个体)。
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
		t.Errorf("目标嗓音 100 时最佳 = %v, 期望 96(都没达标,取离目标最近)", bv)
	}
	line.Goal = BreedingGoal{WeightPct: brF64(60)}
	_, _, bw = LineStats(line)
	if bw == nil || *bw != 60 {
		t.Errorf("目标体重 60%% 时最佳 = %v, 期望 60", bw)
	}
	// 方向化后「达标优先」:一个已达标的 100 比一个只差 1 却未达标的 95 更该当选 ——
	// 「只取离目标最近」会挑出 95,让卡片写着一个未达标的「最佳」。
	line.Goal = BreedingGoal{Voice: brI32(96)}
	line.Gens = []Generation{
		{Gen: 1, Child: brChild(1, "差一点", 95, 60, "")},
		{Gen: 2, Child: brChild(2, "已达标", 100, 60, "")},
	}
	if _, bv, _ := LineStats(line); bv == nil || *bv != 100 {
		t.Errorf("达标优先时最佳 = %v, 期望 100(已达标,优于未达标的 95)", bv)
	}
	if _, bv, bw := LineStats(nil); bv != nil || bw != nil {
		t.Error("nil 培育线不该panic,也不该给出最佳值")
	}
}

// TestReachGoal 达成判定:存在某一代的子代**同时**满足全部已填目标项。
//
// 口径是**方向化**的(见 reached):目标偏高(嗓音 > 0、体重 > 50)时「≥ 目标」算到、偏低时
// 「≤ 目标」算到、正好在中心时精确相等;体重**没有** ±2pp 容差。每一项都配了反例 ——
// 它们都容易被「顺手放宽」改坏,而放宽错方向恰恰会毁掉往低刷的目标(极限嗓音 -100 / 体重 0%)。
func TestReachGoal(t *testing.T) {
	cases := []struct {
		name  string
		goal  BreedingGoal
		child *EggParent
		want  bool
	}{
		{"高嗓音:正好命中", BreedingGoal{Voice: brI32(96)}, brChild(1, "一代", 96, 50, ""), true},
		{"高嗓音:超过也算命中", BreedingGoal{Voice: brI32(96)}, brChild(1, "一代", 97, 50, ""), true},
		{"高嗓音:低于目标不算", BreedingGoal{Voice: brI32(96)}, brChild(1, "一代", 95, 50, ""), false},
		{"低嗓音:更低也算命中", BreedingGoal{Voice: brI32(-96)}, brChild(1, "一代", -100, 50, ""), true},
		{"低嗓音:高于目标不算", BreedingGoal{Voice: brI32(-96)}, brChild(1, "一代", -95, 50, ""), false},
		{"中心嗓音:按精确相等", BreedingGoal{Voice: brI32(0)}, brChild(1, "一代", 1, 50, ""), false},
		{"体重正好命中", BreedingGoal{WeightPct: brF64(90)}, brChild(1, "一代", 0, 90, ""), true},
		{"体重超过也算命中", BreedingGoal{WeightPct: brF64(90)}, brChild(1, "一代", 0, 94, ""), true},
		{"体重低于目标 2pp 不再算命中", BreedingGoal{WeightPct: brF64(90)}, brChild(1, "一代", 0, 88, ""), false},
		{"低体重目标:更轻也算命中", BreedingGoal{WeightPct: brF64(10)}, brChild(1, "一代", 0, 5, ""), true},
		{"低体重目标:更重不算命中", BreedingGoal{WeightPct: brF64(10)}, brChild(1, "一代", 0, 12, ""), false},
		{"体重未知不算命中", BreedingGoal{WeightPct: brF64(90)}, &EggParent{Gid: 1, Name: "一代"}, false},
		{"性格全等", BreedingGoal{Nature: "胆小"}, brChild(1, "一代", 0, 50, "胆小"), true},
		{"性格不同不算命中", BreedingGoal{Nature: "胆小"}, brChild(1, "一代", 0, 50, "固执"), false},
		{
			"三项同时满足",
			BreedingGoal{Voice: brI32(96), WeightPct: brF64(90), Nature: "胆小"},
			brChild(1, "一代", 100, 91, "胆小"), true,
		},
		{
			"三项里缺一项",
			BreedingGoal{Voice: brI32(96), WeightPct: brF64(90), Nature: "胆小"},
			brChild(1, "一代", 100, 91, "固执"), false,
		},
		{"性别:命中", BreedingGoal{Gender: "♀"}, brGendered(1, "一代", 0, 50, "", "♀"), true},
		{"性别:不符不算命中", BreedingGoal{Gender: "♀"}, brGendered(1, "一代", 0, 50, "", "♂"), false},
		{"性别:快照缺性别不算命中", BreedingGoal{Gender: "♀"}, brGendered(1, "一代", 0, 50, "", ""), false},
		{
			"嗓音+体重+性格+性别同时满足",
			BreedingGoal{Voice: brI32(96), WeightPct: brF64(90), Nature: "胆小", Gender: "♀"},
			brGendered(1, "一代", 100, 91, "胆小", "♀"), true,
		},
		{
			"性别是唯一缺项",
			BreedingGoal{Voice: brI32(96), Gender: "♀"},
			brGendered(1, "一代", 100, 50, "", "♂"), false,
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

// TestReached 方向化达标判定的本体:高目标 ≥、低目标 ≤、中心精确。
//
// 直接测这个函数(而不是只经 ReachGoal)是因为**中心与两个方向**是三条独立分支,混进业务
// 用例里不容易看全;方向判反正是「页面显示达标、状态却不翻」这类静默错误的根。
func TestReached(t *testing.T) {
	cases := []struct {
		name              string
		cur, goal, lo, hi float64
		want              bool
	}{
		{"高目标:等于", 96, 96, -100, 100, true},
		{"高目标:超过", 97, 96, -100, 100, true},
		{"高目标:不足", 95, 96, -100, 100, false},
		{"低目标:等于", -96, -96, -100, 100, true},
		{"低目标:更低", -100, -96, -100, 100, true},
		{"低目标:不足", -95, -96, -100, 100, false},
		{"中心:精确相等", 0, 0, -100, 100, true},
		{"中心:偏离不算", 1, 0, -100, 100, false},
		{"体重高目标:等于", 90, 90, 0, 100, true},
		{"体重高目标:更重", 95, 90, 0, 100, true},
		{"体重低目标:更轻", 5, 10, 0, 100, true},
		{"体重低目标:更重不算", 12, 10, 0, 100, false},
		{"体重中心:精确", 50, 50, 0, 100, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := reached(c.cur, c.goal, c.lo, c.hi); got != c.want {
				t.Errorf("reached(%v, %v) = %v, 期望 %v", c.cur, c.goal, got, c.want)
			}
		})
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
		mk(2, "甲公", "火神", "♂", "龙"), // 与"龙"有交 → 进种公
		mk(3, "丙公", "火神", "♂", "虫"), // 蛋组不交 → 被粗筛掉
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
	if got := BreedPool(ref, pets); len(got.Cands) != 1 || got.Cands[0].Mother.Species != later.name {
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

// TestEggMatchSameAsGroupsMatch PoolSource.eggMatch(走蛋组掩码)必须与 EggGroupsMatch
// (逐个比组名)给出**同一个答案**。
//
// 为什么钉这条:掩码是内层「每只母本 × 全部雄性」这个平方级热点的捷径,而捷径与口径一旦
// 分叉,表现是「某些组合莫名配不上 / 莫名配上了」—— 不报错,页面上也只是一组建议消失了。
// 空组、空名、多组这几类边界正是最容易分叉的地方,故都列进用例。
func TestEggMatchSameAsGroupsMatch(t *testing.T) {
	groups := [][]gamedata.EggGroup{
		nil,
		{},
		{{Name: "龙"}},
		{{Name: "兽"}},
		{{Name: "龙"}, {Name: "兽"}},
		{{Name: "龙"}, {Name: "飞行"}},
		{{Name: ""}},              // 只有空名:与「没有蛋组」同义
		{{Name: ""}, {Name: "龙"}}, // 空名不该挡住另一个有效组
		{{ID: 1, Name: "龙", Desc: "描述不参与匹配"}}, // 只有 ID/Desc 不同的同一组
	}
	// 每种蛋组各造一只 ♀ 与一只 ♂:src.males[i] 于是正好对应 groups[i]
	pets := make([]*Pet, 0, len(groups)*2)
	for i, gs := range groups {
		pets = append(pets, &Pet{Gid: uint32(100 + i), Name: "母", Gender: "♀", EggGroups: gs})
		pets = append(pets, &Pet{Gid: uint32(200 + i), Name: "公", Gender: "♂", EggGroups: gs})
	}
	src := NewPoolSource(pets)
	if src.bitOf == nil {
		t.Fatal("蛋组种类远少于 64,掩码本该启用")
	}
	for _, m := range groups {
		mMask := src.maskOf(m)
		for j := range groups {
			want := EggGroupsMatch(m, groups[j])
			if got := src.eggMatch(m, mMask, j); got != want {
				t.Errorf("母 %+v × 公 %+v: 掩码 = %v, EggGroupsMatch = %v", m, groups[j], got, want)
			}
		}
	}
}

// TestEggMatchFallsBackWhenTooManyGroups 蛋组种类超过 64 时掩码装不下,必须**整体**退回
// 字符串比较 —— 而不是给一部分宠物用掩码、另一部分不用(那会让「能不能配」自相矛盾)。
func TestEggMatchFallsBackWhenTooManyGroups(t *testing.T) {
	const n = 70 // > 64
	groups := make([][]gamedata.EggGroup, 0, n)
	pets := make([]*Pet, 0, n*2)
	for i := 0; i < n; i++ {
		g := []gamedata.EggGroup{{Name: fmt.Sprintf("组%d", i)}}
		groups = append(groups, g)
		pets = append(pets, &Pet{Gid: uint32(1000 + i), Name: "母", Gender: "♀", EggGroups: g})
		pets = append(pets, &Pet{Gid: uint32(2000 + i), Name: "公", Gender: "♂", EggGroups: g})
	}
	src := NewPoolSource(pets)
	if src.bitOf != nil {
		t.Fatalf("%d 种蛋组仍启用了掩码:超过 64 就该整体退回字符串比较", n)
	}
	for _, i := range []int{0, 1, 33, 68, 69} {
		for _, j := range []int{0, 5, 64, 69} {
			want := EggGroupsMatch(groups[i], groups[j])
			if got := src.eggMatch(groups[i], 0, j); got != want {
				t.Errorf("退回路径 组%d × 组%d = %v, 期望 %v", i, j, got, want)
			}
		}
	}
	// 退回之后候选池仍要照常工作:每一位母本只配上同组那只公(还要排除自己,故为 0)。
	pool := src.BreedPool(ChainRef{Species: "母"})
	if len(pool.Cands) != 0 {
		t.Errorf("空品种引用 = %d 位候选, 期望 0", len(pool.Cands))
	}
}

// TestMotherGidOf 老线没有 MotherGid 字段,要靠末代派生 —— 这是「不做迁移就能认种母」的前提。
func TestMotherGidOf(t *testing.T) {
	mk := func(gid uint32) *EggParent { return &EggParent{Gid: gid} }
	// 新线:存了就直接用
	if got := MotherGidOf(&BreedingLine{MotherGid: 77}); got != 77 {
		t.Errorf("新线 = %d, 期望 77", got)
	}
	// 老线:取**末代**的母本,不是第一代
	old := &BreedingLine{Gens: []Generation{
		{Gen: 1, Mother: mk(11)},
		{Gen: 2, Mother: mk(22)},
		{Gen: 3, Mother: mk(33)},
	}}
	if got := MotherGidOf(old); got != 33 {
		t.Errorf("老线 = %d, 期望 33(末代的母本,不是第一代的 11)", got)
	}
	// 末代可能在 Pending 里(待认领的代同样带母本)
	if got := MotherGidOf(&BreedingLine{Gens: old.Gens, Pending: []Generation{{Gen: 4, Mother: mk(44)}}}); got != 44 {
		t.Errorf("末代在 Pending = %d, 期望 44", got)
	}
	// 某代没有母本(手工提交上来的代):跳过它往前找,而不是返回 0
	if got := MotherGidOf(&BreedingLine{Gens: []Generation{
		{Gen: 1, Mother: mk(11)}, {Gen: 2}, {Gen: 3},
	}}); got != 11 {
		t.Errorf("末两代缺母本 = %d, 期望 11(往前找到仅有的那只)", got)
	}
	// 一代都没有:手工建的空线,派生不出来 → 0(此时按品种兜底)
	if got := MotherGidOf(&BreedingLine{}); got != 0 {
		t.Errorf("空线 = %d, 期望 0", got)
	}
	if got := MotherGidOf(nil); got != 0 {
		t.Errorf("nil 线 = %d, 期望 0", got)
	}
}

// TestChildLineInherits 换上的新种母是本线子代时自动开子线:目标与代数都要接上,否则培育史断在这里。
func TestChildLineInherits(t *testing.T) {
	v, w := int32(96), 98.0
	parent := &BreedingLine{
		ID: "mom", MotherGid: 1,
		Goal: BreedingGoal{Voice: &v, WeightPct: &w, Nature: "固执"},
		Gens: []Generation{{Gen: 1}, {Gen: 2}, {Gen: 3}},
	}
	ps := &EggParents{Mother: &EggParent{Gid: 9, Name: "接班的子代", Species: "火神"}}
	child := NewChildLine("kid", parent, ps, 100)

	if child.ParentLineID != "mom" {
		t.Errorf("ParentLineID = %q, 期望 mom", child.ParentLineID)
	}
	if child.MotherGid != 9 {
		t.Errorf("MotherGid = %d, 期望 9(新种母)", child.MotherGid)
	}
	if child.Goal.Voice == nil || *child.Goal.Voice != 96 {
		t.Errorf("目标没继承 = %+v, 期望嗓音 96", child.Goal)
	}
	// 代数接在母线之后:子线第一代是 4,不是 1
	if got := NextGen(child); got != 4 {
		t.Errorf("子线第一代 = %d, 期望 4(母线已有 3 代)", got)
	}
	if got := AppendPending(child, ps, 0, 100); got != 4 {
		t.Errorf("AppendPending 返回 %d, 期望 4", got)
	}
	// 普通线(非子线)仍从 1 开始
	if got := NextGen(&BreedingLine{}); got != 1 {
		t.Errorf("普通线第一代 = %d, 期望 1", got)
	}
}

// TestDescendantGids 历代子代集合:回交标签与「换上的母本是不是本线孵出来的」都靠它。
func TestDescendantGids(t *testing.T) {
	l := &BreedingLine{Gens: []Generation{
		{Gen: 1, Child: &EggParent{Gid: 101}},
		{Gen: 2}, // 还没认领:不算
		{Gen: 3, Child: &EggParent{Gid: 103}},
	}}
	got := DescendantGids(l)
	if !got[101] || !got[103] {
		t.Errorf("集合 = %v, 期望含 101 与 103", got)
	}
	if len(got) != 2 {
		t.Errorf("集合大小 = %d, 期望 2(未认领的那代不算)", len(got))
	}
}

// TestMergeChildLine 子线并入母线:代数**原样搬**而不是重排(子线代数本就接着母线数),
// 目标不搬回去(母线那份才是权威的),且只认真正的父子关系。
func TestMergeChildLine(t *testing.T) {
	v := int32(96)
	parent := &BreedingLine{
		ID: "mom", MotherGid: 1, Goal: BreedingGoal{Voice: &v},
		Gens: []Generation{{Gen: 1}, {Gen: 2}},
	}
	child := &BreedingLine{
		ID: "kid", ParentLineID: "mom", MotherGid: 9, GenBase: 2,
		Goal: BreedingGoal{Voice: &v}, // 从母线继承来的
		Gens: []Generation{{Gen: 3}, {Gen: 4}},
		Pending: []Generation{
			{Gen: 5},
		},
	}
	if !MergeChildLine(parent, child) {
		t.Fatal("合并没成功")
	}
	if len(parent.Gens) != 4 {
		t.Fatalf("母线代数 = %d, 期望 4", len(parent.Gens))
	}
	// 代数必须连续且升序 —— 原样搬的前提是子线本就接着数(GenBase)
	for i, g := range parent.Gens {
		if g.Gen != i+1 {
			t.Errorf("第 %d 条的代数 = %d, 期望 %d(搬完之后要按代数排好)", i, g.Gen, i+1)
		}
	}
	if len(parent.Pending) != 1 || parent.Pending[0].Gen != 5 {
		t.Errorf("待认领 = %+v, 期望还剩第 5 代", parent.Pending)
	}
	if child.Gens != nil || child.Pending != nil {
		t.Error("子线的代数没清空:删掉它之后这些就没了,留着只是隐患")
	}
	// 目标仍是母线自己的(没被子线那份副本覆盖)
	if parent.Goal.Voice == nil || *parent.Goal.Voice != 96 {
		t.Errorf("母线目标 = %+v, 期望不变", parent.Goal)
	}

	// 不是这条线的孩子 → 不动手(否则会把别人的历史并进来)
	other := &BreedingLine{ID: "x", ParentLineID: "someone-else", Gens: []Generation{{Gen: 1}}}
	if MergeChildLine(parent, other) {
		t.Error("不是子线却合并成功了")
	}
	if len(parent.Gens) != 4 {
		t.Errorf("母线代数被改成了 %d, 期望保持 4", len(parent.Gens))
	}
	// 没有代可搬 → 不动手
	if MergeChildLine(parent, &BreedingLine{ID: "empty", ParentLineID: "mom"}) {
		t.Error("空子线也合并成功了")
	}
}
