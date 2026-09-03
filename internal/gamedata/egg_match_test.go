package gamedata

import (
	"math"
	"strings"
	"testing"
)

// 本文件验证随机蛋候选反推(MatchRandomEgg,见 egg_match.go 与 docs/data.md)。
//
// 为什么值得测:这条链路的**唯一实测样本只有一例**(2026-08-15 那份 pcap 里随机蛋
// gid 2985 破壳),而筛选靠的是三条件联立(孵化时长 + 身高区间 + 体重区间)——
// 少一个条件、或单位换算差一位(米/厘米、千克/克),候选集就整个变了。
// 这种错误**没有任何报错**:页面照样列出一堆候选,玩家看不出对错。

// TestMatchRandomEggKnownHatch 锁定那唯一一例破壳实测。
//
// 样本:随机蛋 gid 2985,height=20(÷100 米)、weight=11443(÷1000 千克)、
// max_hatched_secs=57600,孵出**权杖-Ⅱ**(conf_id 3410001,hatch_data 57600)。
// 期望:候选里真值在、且不是靠"放过所有行"混进来的(候选数要少而准)。
func TestMatchRandomEggKnownHatch(t *testing.T) {
	db, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// 入参用展示单位,与前端 EggView 的 heightM/weightKg/maxSecs 完全一致
	got := db.MatchRandomEgg(0.20, 11.443, 57600)
	if len(got) == 0 {
		t.Fatal("一个候选都没给:筛选条件多半写错了")
	}
	const wantConf, wantName = uint32(3410001), "权杖-Ⅱ"
	var rank int
	for i, c := range got {
		if c.ConfID == wantConf {
			rank = i + 1
			break
		}
	}
	if rank == 0 {
		names := make([]string, 0, len(got))
		for _, c := range got {
			names = append(names, c.Name)
		}
		t.Fatalf("真值 %s(%d) 不在候选里: %v", wantName, wantConf, names)
	}
	// 候选集要收得住:若条件放行太多,猜就没有意义了。
	if len(got) > 12 {
		t.Errorf("候选 %d 条,太多(该样本实测 3 条),筛选条件可能漏了一维", len(got))
	}
	// 时长维是唯一有实测支撑的:候选必须清一色等于入参时长。
	for _, c := range got {
		if c.HatchSecs != 57600 {
			t.Errorf("候选 %s(%d) 的 hatchSecs=%d, 应恒等于入参 57600", c.Name, c.ConfID, c.HatchSecs)
		}
	}
	// 分数只用于排序,必须是降序且落在 0-100。
	for i := 1; i < len(got); i++ {
		if got[i-1].Score < got[i].Score {
			t.Errorf("候选未按 score 降序: %v", got)
			break
		}
	}
}

// TestMatchRandomEggDedup 验证候选按「孵出来是谁」收敛过,不再一条配置行一个条目。
//
// 为什么值得测:原始筛选给出的是**蛋配置行**,而同一物种在表里占很多行 ——
// 血脉变体、异色形态、首领版本、活动纪念蛋。它们区间与时长完全一致,会整组一起落进
// 候选。去重坏掉时**没有任何报错**,只是列表变长、玩家看到一堆名字重复的行,
// 很容易以为是匹配坏了。
//
// 用实测样本:2026-09-03 一颗 0.26m / 2.057kg / 28800s 的随机蛋,去重前是 8 条
// (草头鸭 ×2、蹦蹦种子 ×2、蝴蝶陶陶 ×3),去重后应为 4 条。
func TestMatchRandomEggDedup(t *testing.T) {
	db, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// 场景一:变体重复(血脉/首领/异色各占一行,与基础形态同区间同时长)
	assertDedup(t, db, 0.26, 2.057, 28800, []string{"草头鸭", "绿耳松鼠", "蝴蝶陶陶", "蹦蹦种子"})

	// 场景二:**同名但不同外形**。蹦蹦种子有 4 个地区形态(草地/火山/沙地/雪山),
	// 各自一个 model_id、名字完全一样,去重前会占 3 行。
	// 这一条单独有意义:按 model 归并合不掉它们(model 本就不同),
	// 必须靠最后那次「按物种名」收敛 —— 少了那一步,这里就会冒出 3 个蹦蹦种子。
	assertDedup(t, db, 0.26, 2.057, 43200,
		[]string{"小丑豆豆的蛋", "蹦蹦种子", "乖乖鹄", "学院呱呱", "矿晶虫"})
}

// assertDedup 断言候选去重后恰好是 want 这些物种(顺序由匹配度决定,故只比集合)。
func assertDedup(t *testing.T, db *DB, h, w float64, secs int32, want []string) {
	t.Helper()
	got := db.MatchRandomEgg(h, w, secs)
	gotNames := make([]string, 0, len(got))
	for _, c := range got {
		gotNames = append(gotNames, c.Name)
	}
	if len(got) != len(want) {
		t.Fatalf("h=%v w=%v t=%d: 候选应 %d 条,实际 %d 条: %v", h, w, secs, len(want), len(got), gotNames)
	}
	seen := map[string]int{}
	for _, n := range gotNames {
		seen[n]++
	}
	for _, n := range want {
		if seen[n] == 0 {
			t.Errorf("h=%v w=%v t=%d: 候选缺 %s: %v", h, w, secs, n, gotNames)
		}
		// 名字不许重复:这条才是"去重"本身 —— 只查"该有的都在"的话,
		// 把重复行全留下也能通过。
		if seen[n] > 1 {
			t.Errorf("h=%v w=%v t=%d: 候选里 %s 出现了 %d 次: %v", h, w, secs, n, seen[n], gotNames)
		}
	}
	// 变体不许出现:异色/血脉/首领。
	for _, c := range got {
		if c.Precious == 2 || strings.Contains(c.Name, "血脉") || strings.Contains(c.Name, "首领") {
			t.Errorf("h=%v w=%v t=%d: 候选里出现了变体: %s (p=%d)", h, w, secs, c.Name, c.Precious)
		}
	}
}

// TestIsVariantEgg 单测变体判定。
//
// 为什么单独测:这条判定在**整链路测试里是冗余的** —— 异色行就算没被剔掉,也会被
// 「按物种名去重」顺手并掉(同名基础形态的 conf_id 更小,留下来的是它)。于是把
// 这里的判据改坏,上层测试照样全绿,而判定本身已经不对了。两道防线都在,但每一道
// 都得自己站得住 —— 故单独钉住。
//
// 关键用例:异色**必须看品类而不是名字** —— 110 条异色蛋里 16 条名字不带「异色」
// (如 3360002 蝴蝶陶陶),只匹配名字会漏掉它们。
func TestIsVariantEgg(t *testing.T) {
	for _, c := range []struct {
		name string
		in   EggCandidate
		want bool
	}{
		{"普通蛋", EggCandidate{Name: "草头鸭", Precious: 0}, false},
		{"异色且名字带异色", EggCandidate{Name: "异色蝴蝶陶陶", Precious: 2}, true},
		{"异色但名字不带异色(关键)", EggCandidate{Name: "蝴蝶陶陶", Precious: 2}, true},
		{"水系血脉", EggCandidate{Name: "草头鸭（水系血脉）", Precious: 1}, true},
		{"首领", EggCandidate{Name: "首领蹦蹦种子", Precious: 1}, true},
		{"首领血脉", EggCandidate{Name: "小独角兽（首领血脉）", Precious: 5}, true},
		{"珍贵蛋不是变体", EggCandidate{Name: "迪莫", Precious: 1}, false},
		{"活动纪念蛋不是变体", EggCandidate{Name: "乖乖鹄", Precious: 4}, false},
	} {
		if got := isVariantEgg(c.in); got != c.want {
			t.Errorf("%s: isVariantEgg = %v, 期望 %v", c.name, got, c.want)
		}
	}
}

// TestBetterEggCandidate 单测「同一组留哪条」。
//
// 同上:在整链路测试里它大半被后续步骤掩盖(同组各成员区间一致 → 分数一致 →
// 最后由「按名去重」的那次比较定胜负),故单独钉住。
func TestBetterEggCandidate(t *testing.T) {
	base := EggCandidate{ConfID: 3008001, ModelID: 3008001, Name: "草头鸭", Score: 50}
	// 基础形态优先:哪怕分数略低也要留它 —— 变体的 model 都指向基础形态,
	// 它才是「这个外形真正代表的那个条目」。
	variant := EggCandidate{ConfID: 3008002, ModelID: 3008001, Name: "草头鸭（水系血脉）", Score: 80}
	if !betterEggCandidate(base, variant) {
		t.Error("同组内应优先留基础形态,即使它的匹配度更低")
	}
	if betterEggCandidate(variant, base) {
		t.Error("betterEggCandidate 应自反一致")
	}
	// 都不是基础形态时比分数
	a := EggCandidate{ConfID: 2, ModelID: 9, Score: 70}
	b := EggCandidate{ConfID: 3, ModelID: 9, Score: 60}
	if !betterEggCandidate(a, b) {
		t.Error("分数高的应留下")
	}
	// 分数也相同时比 conf_id:纯粹为了结果稳定,否则留谁取决于 map 遍历次序,
	// 每次刷新候选都在跳。
	c1 := EggCandidate{ConfID: 5, ModelID: 9, Score: 70}
	c2 := EggCandidate{ConfID: 7, ModelID: 9, Score: 70}
	if !betterEggCandidate(c1, c2) || betterEggCandidate(c2, c1) {
		t.Error("同分时应取 conf_id 小的(保证结果稳定)")
	}
}

// TestDedupeEggCandidates 用**构造输入**分别钉住三步里的每一步。
//
// 必须构造:整链路测试用的是真实数据,而真实数据里这三步互相掩盖 ——
// 同 model 的条目区间一致 → 分数一致 → 就算「按 model 归并」整步删掉,
// 后面「按名去重」也会顺手并掉,上层测试照样全绿。于是每一步都得单独证明自己
// 站得住,否则将来有人删掉其中一步,没有任何东西会拦他。
func TestDedupeEggCandidates(t *testing.T) {
	// 1) 按外形归并 + 基础形态优先:同一个 model 下,基础形态哪怕分低也要留。
	//    真实数据里同组各成员分数相同(区间一致),比不出这一条,故必须构造。
	got := dedupeEggCandidates([]EggCandidate{
		{ConfID: 100, ModelID: 100, Name: "甲", Score: 50}, // 基础形态
		{ConfID: 101, ModelID: 100, Name: "乙", Score: 90}, // 变体,分更高
	})
	if len(got) != 1 {
		t.Fatalf("同一 model 应并成 1 条,实际 %d 条: %+v", len(got), got)
	}
	if got[0].ConfID != 100 {
		t.Errorf("同一 model 内应留基础形态(100),实际留了 %d", got[0].ConfID)
	}

	// 2) 按物种名收敛:不同 model、同名(如蹦蹦种子的 4 个地区形态)。
	got = dedupeEggCandidates([]EggCandidate{
		{ConfID: 200, ModelID: 200, Name: "丙", Score: 10},
		{ConfID: 300, ModelID: 300, Name: "丙", Score: 10},
	})
	if len(got) != 1 {
		t.Fatalf("同名应并成 1 条,实际 %d 条: %+v", len(got), got)
	}
	if got[0].ConfID != 200 {
		t.Errorf("同分时应留 conf_id 小的(结果稳定),实际留了 %d", got[0].ConfID)
	}

	// 3) 剔变体:整组只剩变体时,该组整体不出现。
	got = dedupeEggCandidates([]EggCandidate{
		{ConfID: 400, ModelID: 400, Name: "异色丁", Precious: 2, Score: 99},
	})
	if len(got) != 0 {
		t.Errorf("只剩异色行时应整组剔除,实际留下 %+v", got)
	}

	// 4) 互不干扰:两个不同物种不该被并掉。
	got = dedupeEggCandidates([]EggCandidate{
		{ConfID: 500, ModelID: 500, Name: "戊", Score: 70},
		{ConfID: 600, ModelID: 600, Name: "己", Score: 60},
	})
	if len(got) != 2 {
		t.Errorf("不同物种应各留一条,实际 %d 条: %+v", len(got), got)
	}
}

// TestMatchRandomEggDedupKeepsSpecies 剔掉变体不能把物种整个剔没。
//
// 背景:异色蛋的 model 常指向基础形态之外的 id,剔变体时该组会整组消失。已核实这些
// 物种都有基础形态条目在同一时长下兜住(见 egg_match.go 的 dedupeEggCandidates),
// 故这里要求「变体行没了,但物种还在」—— 两边一起消失才是真丢了东西。
func TestMatchRandomEggDedupKeepsSpecies(t *testing.T) {
	db, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// 全表自查:每个「有异色行」的物种,在相同时长下必须还有一条非变体行。
	byTime := map[int32]map[string][]EggConf{}
	for _, c := range db.eggConf {
		byTime[c.HatchSecs] = byTime[c.HatchSecs]
	}
	for secs := range byTime {
		var variants, bases []string
		for _, c := range db.eggConf {
			if c.HatchSecs != secs {
				continue
			}
			base := strings.TrimSuffix(strings.TrimPrefix(c.Name, "异色"), "的蛋")
			if c.Precious == 2 {
				variants = append(variants, base)
			} else {
				bases = append(bases, base)
			}
		}
		baseSet := map[string]bool{}
		for _, b := range bases {
			baseSet[b] = true
		}
		for _, v := range variants {
			if !baseSet[v] {
				t.Errorf("时长 %d 下物种「%s」只有异色行,剔变体后就没了", secs, v)
			}
		}
	}
	// 哨兵:上面若因数据为空而空转,这条会挡住
	if len(byTime) == 0 {
		t.Fatal("没读到任何蛋配置")
	}
}

// TestMatchRandomEggFilters 逐个验证三个筛选维度,以及退化情形。
func TestMatchRandomEggFilters(t *testing.T) {
	db, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	base := db.MatchRandomEgg(0.20, 11.443, 57600)
	if len(base) == 0 {
		t.Fatal("基准样本无候选,后续断言无从谈起")
	}

	// 时长改一档(16h):候选集必须整体换掉 —— 说明时长维确实在筛。
	other := db.MatchRandomEgg(0.20, 11.443, 57600+14400)
	for _, c := range other {
		if c.HatchSecs == 57600 {
			t.Errorf("入参 72000s 时仍给出 57600s 的候选 %s:时长维没生效", c.Name)
		}
	}

	// 尺寸甩到人体不可能的范围:应当一个都不剩。
	if got := db.MatchRandomEgg(999, 999999, 57600); len(got) != 0 {
		t.Errorf("离谱尺寸应无候选,实际 %d 条", len(got))
	}

	// maxSecs=0(从未入孵的随机蛋):不做时长筛选,退化成纯尺寸匹配,
	// 结果必须是「带时长筛选」的超集 —— 少一维只能更宽,不能更窄。
	wide := db.MatchRandomEgg(0.20, 11.443, 0)
	if len(wide) < len(base) {
		t.Errorf("maxSecs=0 的候选(%d) 少于带时长的(%d):退化方向反了", len(wide), len(base))
	}
}

// TestToEggScaleRoundTrip 验证「整数 → 展示单位 → 整数」这条往返不丢值。
//
// 为什么单独测:前端传来的 heightM/weightKg 是 float64(int/100) 与 float64(int/1000),
// 后端要还原成整数才能跟配置区间比。若还原时截断而非四舍五入,0.29*100=28.999…
// 会变成 28,边界上的蛋就被筛出自己的区间 —— **候选列表照样列得出来,只是真值不见了**。
// 这类错误只在特定数值上出现,人工点几下页面根本碰不到。
func TestToEggScaleRoundTrip(t *testing.T) {
	bad := 0
	for h := int32(0); h <= 300; h++ { // 蛋身高实测全在 1~100 厘米量级,放宽到 300
		if got := toEggScale(float64(h)/100, 100); got != h && bad < 5 {
			t.Errorf("身高 %d -> %v -> %d, 往返丢值", h, float64(h)/100, got)
			bad++
		}
	}
	for w := int32(0); w <= 200000; w += 7 { // 步长取样,覆盖到 200 千克
		if got := toEggScale(float64(w)/1000, 1000); got != w && bad < 10 {
			t.Errorf("体重 %d -> %v -> %d, 往返丢值", w, float64(w)/1000, got)
			bad++
		}
	}
	// 点名几个实测会踩坑的值(截断即错),免得将来有人"顺手优化"掉 Round。
	for _, c := range []struct {
		v     float64
		scale int32
		want  int32
	}{{0.29, 100, 29}, {11.443, 1000, 11443}, {0.07, 100, 7}, {3.645, 1000, 3645}} {
		if got := toEggScale(c.v, c.scale); got != c.want {
			t.Errorf("toEggScale(%v, %d) = %d, 期望 %d", c.v, c.scale, got, c.want)
		}
	}
}

// TestMatchScoreAndSpan 单测打分与偏离这两个**纯函数**。
// 不从 db.eggConf 里挑样本:map 遍历次序随机,挑到哪个不确定,断言就成了碰运气。
func TestMatchScoreAndSpan(t *testing.T) {
	// 区间 [10,20] 上:正中 15 偏离 0,两端 10/20 偏离 0.5。
	if got := spanDev(10, 20, 15); got != 0 {
		t.Errorf("正中 spanDev = %v, 期望 0", got)
	}
	if got := spanDev(10, 20, 20); math.Abs(got-0.5) > 1e-9 {
		t.Errorf("上沿 spanDev = %v, 期望 0.5", got)
	}
	if got := spanDev(10, 20, 10); math.Abs(got-0.5) > 1e-9 {
		t.Errorf("下沿 spanDev = %v, 期望 0.5", got)
	}
	// 退化区间(lo==hi):只有唯一取值,落进来就是它 —— 偏离 0、百分位 100,不许除零。
	if got := spanDev(5, 5, 5); got != 0 {
		t.Errorf("退化区间 spanDev = %v, 期望 0(不许除零)", got)
	}
	if got := spanPct(5, 5, 5); got != 100 {
		t.Errorf("退化区间 spanPct = %v, 期望 100", got)
	}

	mid := EggConf{HeightLow: 10, HeightHigh: 20, WeightLow: 1000, WeightHigh: 2000}
	if got := matchScore(mid, 15, 1500); math.Abs(got-100) > 1e-9 {
		t.Errorf("两维都在正中应得 100,实际 %v", got)
	}
	if got := matchScore(mid, 20, 2000); math.Abs(got) > 1e-9 {
		t.Errorf("两维都贴边应得 0,实际 %v", got)
	}
	// 一维正中一维贴边 → 50:两维等权,别悄悄加权。
	if got := matchScore(mid, 15, 2000); math.Abs(got-50) > 1e-9 {
		t.Errorf("一维正中一维贴边应得 50,实际 %v", got)
	}
}

// TestMatchRandomEggScoreRange 扫全表验证输出的数值健全性:分数落在 0-100、
// 百分位落在 0-100、无 NaN。
//
// NaN 尤其要挡:sort.Slice 的比较里出现 NaN 会得到"随机但稳定"的顺序,
// 页面每次刷新候选顺序都在跳,而没有任何报错。
func TestMatchRandomEggScoreRange(t *testing.T) {
	db, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	n := 0
	for conf, c := range db.eggConf {
		// 每个配置行都拿它自己的区间中点去查,保证一定有候选返回。
		got := db.MatchRandomEgg(
			float64(c.HeightLow+c.HeightHigh)/2/100,
			float64(c.WeightLow+c.WeightHigh)/2/1000,
			c.HatchSecs)
		for _, cand := range got {
			n++
			if math.IsNaN(cand.Score) || cand.Score < 0 || cand.Score > 100 {
				t.Errorf("conf %d: 分数越界 %v", conf, cand.Score)
			}
			if math.IsNaN(cand.HeightPct) || cand.HeightPct < 0 || cand.HeightPct > 100 {
				t.Errorf("conf %d: 身高百分位越界 %v", conf, cand.HeightPct)
			}
			if math.IsNaN(cand.WeightPct) || cand.WeightPct < 0 || cand.WeightPct > 100 {
				t.Errorf("conf %d: 体重百分位越界 %v", conf, cand.WeightPct)
			}
			if cand.HatchSecs != c.HatchSecs {
				t.Errorf("conf %d: 候选时长 %d != 入参 %d", conf, cand.HatchSecs, c.HatchSecs)
			}
		}
	}
	if n == 0 {
		t.Fatal("全表扫下来一个候选都没有:筛选条件多半写错了")
	}
}
