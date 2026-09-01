package gamedata

import "testing"

// 本文件验证「形态全名」这条口径 —— 它是 wiki 数据(join 的钥匙)与游戏数据
// (id/头像)之间唯一的桥。
//
// 为什么值得单独测:拼错一个字符(半角括号、下划线)**没有报错、只是静默查不到**。
// 特性桥接因此整体失效,而页面看起来只是「这些精灵没有特性名」,
// 谁也不会想到是括号写错了。这类错误只有断言能抓住。
//
// 覆盖:
//   - PetFullName 的拼接口径(全角括号);
//   - PetByName 反查(含同名形态取最小 id);
//   - PetForms 的过滤(有图鉴号的才算);
//   - FeatureNameOfBase 端到端:形态 id → 特性名。

func TestPetFullName(t *testing.T) {
	db, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// 找两个已知样本:一个带形态名、一个不带。不写死 id ——
	// 解包数据换版本后 id 会变,按形状找才稳。
	var withForm, withoutForm uint32
	for id, info := range db.petbase {
		if info.Book == 0 {
			continue
		}
		if info.Form != "" && withForm == 0 {
			withForm = id
		}
		if info.Form == "" && withoutForm == 0 {
			withoutForm = id
		}
	}
	if withForm == 0 || withoutForm == 0 {
		t.Fatalf("样本不足: 带形态名 %d, 不带 %d", withForm, withoutForm)
	}

	f := db.petbase[withForm]
	want := f.Name + "（" + f.Form + "）"
	if got := db.PetFullName(withForm); got != want {
		t.Errorf("带形态名: = %q, 期望 %q", got, want)
	}
	// 半角括号是**错的**:wiki 用全角。这条断言是防半角回归的关键。
	if wantHalf := f.Name + "(" + f.Form + ")"; db.PetFullName(withForm) == wantHalf {
		t.Errorf("形态名用了半角括号 —— wiki 是全角,反查会全部落空")
	}
	if got := db.PetFullName(withoutForm); got != db.petbase[withoutForm].Name {
		t.Errorf("无形态名: = %q, 期望 %q(不该带括号)", got, db.petbase[withoutForm].Name)
	}
	if got := db.PetFullName(99999999); got != "" {
		t.Errorf("未知 id: = %q, 期望空串", got)
	}
}

func TestPetByName(t *testing.T) {
	db, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// 反查回来必须能拿到原 id(或同名的更小 id)
	checked := 0
	for id, info := range db.petbase {
		if info.Book == 0 {
			continue
		}
		gotID, gotInfo, ok := db.PetByName(db.PetFullName(id))
		if !ok {
			t.Fatalf("%q 反查不到 —— petNames 表没建全", db.PetFullName(id))
		}
		if gotID > id {
			t.Errorf("%q 反查到 %d, 期望 <= %d(同名取最小 id)", db.PetFullName(id), gotID, id)
		}
		if gotInfo.Name != info.Name {
			t.Errorf("%q 反查到异名 %q", db.PetFullName(id), gotInfo.Name)
		}
		checked++
		if checked >= 200 { // 全量 1136 个,抽 200 个足够
			break
		}
	}
	if _, _, ok := db.PetByName("不存在的精灵"); ok {
		t.Error("查不到的名字应返回 false")
	}
}

// TestPetFormsMatchPetByName 锁住「候选名单」与「标注回填」必须同口径。
//
// 玩家在候选里选「圣光迪莫」提交,后端拿这个名字经 PetByName 反查成形态 id
// 再取头像。两条路若取到不同的形态,结果就是**玩家标了 A、页面显示 B** ——
// 不报错、两者各自都"对",只有把两边放在一起比才看得出来。
//
// 这条不一致真实发生过两次,两次都是口径差一点点:
//
//  1. PetForms 只收有图鉴号的形态,而 petNames 收全部 ——
//     于是「圣光迪莫」候选挂 5025(有图鉴号),回填却取到 3048(无图鉴号的
//     占位形态,压根不在候选里)。头像对得上才怪。
//  2. PetForms 按 (Book, Base) 排序后去重、PetByName 按全局最小 id 取 ——
//     同名但分属不同图鉴号时,两者会挑中不同的记录。
//
// 故这里**逐个候选**比对,而不是抽查:不一致只在个别名字上出现,抽样会漏。
func TestPetFormsMatchPetByName(t *testing.T) {
	db, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	forms := db.PetForms()
	if len(forms) == 0 {
		t.Fatal("PetForms 为空")
	}
	// 1. 候选按名字唯一:同名同图的多条记录(棋契陛下 8 条、鸭吉吉国王 6 条)
	//    在玩家眼里完全一样,列 8 遍只会让人瞎选。
	seen := map[string]bool{}
	for _, f := range forms {
		if seen[f.Name] {
			t.Errorf("候选里 %q 重复出现 —— 玩家无从分辨,去重失效了", f.Name)
		}
		seen[f.Name] = true
	}
	// 2. 每个候选的 Base 必须就是 PetByName 反查的结果
	for _, f := range forms {
		id, _, ok := db.PetByName(f.Name)
		if !ok {
			t.Fatalf("%q 在候选里但反查不到 —— 两表口径不一致", f.Name)
		}
		if id != f.Base {
			t.Errorf("%q: 候选 base=%d 但 PetByName=%d —— 玩家标了 A 会显示 B",
				f.Name, f.Base, id)
		}
	}
	// 3. 稳定性:map 迭代顺序随机,若去重前没排序,每次启动挂的 id 都可能不同。
	//    跑多次比对,单次运行过不代表部署后还过。
	want := forms
	for i := 0; i < 3; i++ {
		got := db.PetForms()
		if len(got) != len(want) {
			t.Fatalf("第 %d 次调用长度不同: %d vs %d —— 去重前没排序", i, len(got), len(want))
		}
		for j := range got {
			if got[j] != want[j] {
				t.Fatalf("第 %d 次第 %d 项不同: %+v vs %+v", i, j, got[j], want[j])
			}
		}
	}
	t.Logf("候选 %d 个,均与 PetByName 一致", len(forms))
}

func TestPetForms(t *testing.T) {
	db, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	forms := db.PetForms()
	if len(forms) == 0 {
		t.Fatal("PetForms 为空")
	}
	// 无图鉴号的占位形态(「鸭吉吉_普通」这类属性变换记录)不该进候选:
	// 玩家在游戏里见不到它们,混进标注候选只会干扰搜索。
	for _, f := range forms {
		if f.Book == 0 {
			t.Errorf("base=%d (name=%q) 无图鉴号却进了候选", f.Base, f.Name)
		}
		if f.Name == "" {
			t.Errorf("base=%d 名字为空", f.Base)
		}
	}
	// 按图鉴号、id 升序:候选列表要给玩家搜,乱序没法用
	for i := 1; i < len(forms); i++ {
		a, b := forms[i-1], forms[i]
		if a.Book > b.Book || (a.Book == b.Book && a.Base > b.Base) {
			t.Fatalf("未按序: %d/%d 在 %d/%d 之后", a.Book, a.Base, b.Book, b.Base)
		}
	}
	// 头像:同名形态极多(「棋契陛下」有 10 个形态),搜出来 10 条一模一样的
	// 文字,玩家无从分辨该选哪只,只能瞎选 —— 标注数据因此不可信。
	// 故候选**必须**带图。这里不断言「每只都要有图」(素材不全,强求会变红),
	// 断言的是「有图的那部分占绝大多数」:整体掉到 0 说明图索引坏了。
	withImg := 0
	for _, f := range forms {
		if f.Img != "" {
			withImg++
		}
	}
	if withImg == 0 {
		t.Fatal("候选一个头像都没有 —— 标注时无从分辨同名形态")
	}
	if want := len(forms) * 9 / 10; withImg < want {
		t.Errorf("只有 %d/%d 个候选带头像, 期望至少 %d —— 图索引出问题了",
			withImg, len(forms), want)
	}
	// 抽验头像路径能对应到该形态(取图路径写错同样会让「有图」变成假的)
	for _, f := range forms {
		if f.Img == "" {
			continue
		}
		if got := db.PetImageByBase(f.Base, false).Head; got != f.Img {
			t.Fatalf("base=%d 的候选 img=%q 与实际取图 %q 不一致", f.Base, f.Img, got)
		}
	}
	t.Logf("候选形态 %d 个, 其中 %d 个带头像", len(forms), withImg)
}

// TestFeatureNameOfBase 端到端验证特性桥接:形态 id → 特性名。
//
// 这是「开局自动绑定特性」的落点。两头的数据都可能缺(wiki 没收录、形态名对不上),
// 故**不断言某个具体 id 必须有名字** —— 那只会在 wiki 更新后随机变红。
// 断言的是「凡是对得上的形态,查出来的名字必须是 wiki 词典里存在的一个名字」:
// 这能抓住拼错括号(整体查不到)、查错表(返回了别的东西)这类真回归。
func TestFeatureNameOfBase(t *testing.T) {
	db, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if db.features == nil || len(db.features.petToName) == 0 {
		t.Skip("features.json 未生成,跳过")
	}
	known := map[string]bool{}
	for _, f := range db.features.features {
		known[f.Name] = true
	}
	var hits int
	for id := range db.petbase {
		got := db.FeatureNameOfBase(id)
		if got == "" {
			continue
		}
		hits++
		if !known[got] {
			t.Fatalf("base=%d 查到 %q, 但不在 features 词典里(桥接查错了表)", id, got)
		}
	}
	// 覆盖率下限:全表对不上说明括号口径坏了(实测 500+ 个形态查得到)
	if hits < 100 {
		t.Errorf("只桥接出 %d 个特性名, 远低于预期 —— 形态全名的口径多半坏了", hits)
	}
	t.Logf("桥接出特性名的形态 %d 个", hits)
}

// TestFeatureNameOfBasePrefersIdIndex 锁住「先按 petbase_id 查,查不到才回退名字」。
//
// 这个顺序**不能反**。两份资料的可靠性差一个量级:
//
//	roco.world 按 petbase_id 索引 —— 它的页面数据里有 petbase_id,
//	与我方解包出的 id 完全一致(实测 594/594),覆盖率 89%,不会抄串;
//	wiki 只能按精灵页名反查 —— 要靠「名 + 全角括号的形态后缀」拼对才查得到,
//	覆盖率 74%,且有 8 处**抄串**。
//
// 抄串最典型的是女王蜂(5015)与花魁蜂后(3157):wiki 把两只的特性对调了。
// 若优先用名字匹配,这两只会一直显示对方的特性名 —— 名字匹配"成功"了,
// 只是配错了,没有任何报错。
//
// 故这里拿这两只做**反向验证**:id 索引给的答案必须与 wiki 那份不同
// (说明没走回退),且各自正确。样本写死 id 是安全的 —— 形态 id 是游戏配置
// 的主键,跨版本稳定(不像图鉴号那种业务编号)。
func TestFeatureNameOfBasePrefersIdIndex(t *testing.T) {
	db, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// 硬失败而非 Skip:索引缺失意味着 generate 脚本没跑或数据丢了,
	// 此时 FeatureNameOfBase 会静默退化到名字匹配(覆盖率 89%→74%,且抄串),
	// 而**没有任何报错**。跳过的话 CI 一片绿,线上却一直在用较差的那条路。
	const missingMsg = "petbase_feature 未加载 —— 先跑 scripts/fetch_rocoworld.py 再 " +
		"scripts/gen_features.py;缺了它特性桥接会静默退化到名字匹配(覆盖率降且会抄串)"
	if db.features == nil || len(db.features.baseToName) == 0 {
		t.Fatal(missingMsg)
	}
	// 女王蜂(图鉴84 阶段4)与花魁蜂后(阶段3):wiki 把两者特性对调了
	queen, queenWiki := uint32(5015), "虫群鼓舞"
	bee, beeWiki := uint32(3157), "虫群突袭"
	if got := db.FeatureNameOfBase(queen); got != "虫群突袭" {
		t.Errorf("女王蜂(5015) = %q, 期望 %q —— 走了名字回退就会拿到 %q(wiki 抄串的那个)",
			got, "虫群突袭", queenWiki)
	}
	if got := db.FeatureNameOfBase(bee); got != "虫群鼓舞" {
		t.Errorf("花魁蜂后(3157) = %q, 期望 %q —— 走了名字回退就会拿到 %q",
			got, "虫群鼓舞", beeWiki)
	}
	// 确认这份数据里 wiki 那份确实是反的 —— 否则上面的断言就失去意义了
	if w, _ := db.PetFeatureName(db.PetFullName(queen)); w != queenWiki {
		t.Logf("注意:wiki 那份已修正为 %q,本用例的「反向验证」前提不再成立", w)
	}
	// 学院呱呱:wiki 压根没收录,只能靠 id 索引查到
	if got := db.FeatureNameOfBase(3620); got != "留学生" {
		t.Errorf("学院呱呱(3620) = %q, 期望 %q —— wiki 未收录它,查不到说明 id 索引没生效",
			got, "留学生")
	}
	// 覆盖率:id 索引应显著优于纯名字匹配(实测 89% vs 74%)
	var byID, byName int
	for _, f := range db.PetForms() {
		if _, ok := db.features.baseToName[f.Base]; ok {
			byID++
		}
		if _, ok := db.PetFeatureName(f.Name); ok {
			byName++
		}
	}
	if byID <= byName {
		t.Errorf("id 索引覆盖 %d 个形态, 未超过名字匹配的 %d 个 —— 数据源没生效或退化了",
			byID, byName)
	}
	t.Logf("id 索引覆盖 %d 个形态, 名字匹配 %d 个", byID, byName)
}
