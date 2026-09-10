package gamedata

import (
	"fmt"
	"strings"
	"testing"
)

// 本文件钉住「品种展示名与蛋图里的形态口径」(见 pet.go 的 ChainLabel / ChainOption.Egg)。
//
// 为什么值得测:同一只精灵的地区/季节形态各占一条链,而**阶段名完全一样** —— 地鼠的两条链
// 都是「地鼠/遁鼠/遁地鼠」、波波螺都是「波波螺/消波螺/嗜波螺」、小狮鹫都是「小狮鹫/神圣狮鹫/
// 皇家狮鹫」。展示名里少写形态,品种下拉里就会并排出现两条**一模一样**的选项,玩家分不清选的是
// 哪条 —— 而品种直接决定这条线能配哪些母本(见 pet.ChainRef)。蛋图同理:不按形态查就会让两个
// 品种指着同一张图,「这条线要孵哪种蛋」当场答错。
//
// 这两样都是「看着正常、只是分不清」的错,不报错也不写日志,只有断言抓得住。

// chainHeads 返回每条链的**链头**(阶段最小的那个形态)的 petbase id,按 evo 升序。
func chainHeads(db *DB) []uint32 {
	out := make([]uint32, 0, len(db.evoIndex))
	for _, ids := range db.evoIndex {
		if len(ids) == 0 {
			continue
		}
		out = append(out, db.EvolutionChain(ids[0])[0].Petbase)
	}
	return out
}

// chainGroupsByName 把各条链按「阶段名序列」分组,返回组 → 链头们。
//
// 同名多形态的链**阶段名完全相同**、只有形态不同,故同一组就是「在界面上长得一样的那几条」;
// 组内多于一条时,展示名与蛋图必须能把它们分开。不写死 id:解包数据换版本后这个形状仍在。
func chainGroupsByName(db *DB) map[string][]uint32 {
	out := map[string][]uint32{}
	for _, head := range chainHeads(db) {
		steps := db.EvolutionChain(head)
		names := make([]string, 0, len(steps))
		for _, s := range steps {
			names = append(names, s.Name)
		}
		k := strings.Join(names, "/")
		out[k] = append(out[k], head)
	}
	return out
}

// TestEvolutionChainOrderIsTotal:链内顺序必须**完全**由数据决定,不能有并列项落在 map 迭代次序上。
//
// 阶段与图鉴号都会撞(喵喵的两支终极形态 武斗酷猫 5061 / 叶冕魔力猫 5003 同为 stage 4 / book 4),
// 只比到图鉴号时剩下那一段就是 evoIndex 的**装载顺序**,而它来自 map 遍历 —— 换个进程就可能反过来。
// 顺序是对外契约:/api/evolution 按它下发,ChainLabel 也按它拼展示名,于是契约 golden 会间歇性报
// 不一致、页面上同一个品种括号里的名字也会随重启跳。故这里把「排序键是完整的」钉死。
func TestEvolutionChainOrderIsTotal(t *testing.T) {
	db, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(db.evoIndex) == 0 {
		t.Fatal("evoIndex 为空")
	}
	ties, chains := 0, 0
	for _, ids := range db.evoIndex {
		if len(ids) == 0 {
			continue
		}
		steps := db.EvolutionChain(ids[0])
		chains++
		for i := 1; i < len(steps); i++ {
			a, b := steps[i-1], steps[i]
			if a.Stage == b.Stage && a.Book == b.Book {
				ties++
			}
			if a.Stage > b.Stage || (a.Stage == b.Stage && a.Book > b.Book) ||
				(a.Stage == b.Stage && a.Book == b.Book && a.Petbase > b.Petbase) {
				t.Errorf("链 %d 的顺序不是 (阶段,图鉴号,petbase) 升序:%d/%s 排在了 %d/%s 前面",
					db.petbase[ids[0]].Evo, a.Petbase, a.Name, b.Petbase, b.Name)
				break
			}
		}
	}
	if chains == 0 {
		t.Fatal("一条链都没有 —— 样本不足")
	}
	if ties == 0 {
		t.Fatal("没有「阶段与图鉴号都相同」的并列项 —— 这类样本正是这条测试要防的,样本不足")
	}
}

// TestChainLabelCarriesForm:链的展示名必须带得出形态,且两条链不会撞名 ——
// 撞名就意味着下拉里有一条**选不对**的选项(玩家只会以为页面坏了)。
func TestChainLabelCarriesForm(t *testing.T) {
	db, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(db.evoIndex) == 0 {
		t.Fatal("evoIndex 为空")
	}

	byLabel := make(map[string]uint32, len(db.evoIndex))
	withForm := 0
	for _, head := range chainHeads(db) {
		evo := db.petbase[head].Evo
		label := db.ChainLabel(head)
		if label == "" {
			t.Errorf("链 %d 的展示名为空", evo)
			continue
		}
		if form := db.petbase[head].Form; form != "" {
			withForm++
			if !strings.Contains(label, form) {
				t.Errorf("链 %d(%s)的展示名 %q 没带形态 %q", evo, db.petbase[head].Name, label, form)
			}
		}
		if other, dup := byLabel[label]; dup {
			t.Errorf("链 %d 与链 %d 的展示名撞了(%q)—— 下拉里两条一样的选项,玩家分不清选的是哪条", evo, other, label)
			continue
		}
		byLabel[label] = evo
	}
	if withForm == 0 {
		t.Error("一条带形态的链都没有 —— 形态(f)没进来,上面那段断言等于没测")
	}

	// 只比「同名多形态」的那几组:它们的阶段名一模一样,是不是分得开全看形态有没有写进去。
	multiFormGroups := 0
	for names, heads := range chainGroupsByName(db) {
		if len(heads) < 2 {
			continue
		}
		multiFormGroups++
		seen := make(map[string]bool, len(heads))
		for _, h := range heads {
			l := db.ChainLabel(h)
			if seen[l] {
				t.Errorf("阶段名相同的两条链(「%s」)展示名也相同(%q)—— 形态没进展示名", names, l)
			}
			seen[l] = true
		}
	}
	if multiFormGroups == 0 {
		t.Error("没有「阶段名完全相同、只有形态不同」的链组 —— 样本不足,这条测试等于没测")
	}
}

// TestChainLabelNoFormWithoutChain:没有进化链的品种只写**名字**、不写形态,而且与链的展示名不撞。
//
// 无链品种是按**名字**认的(见 pet.ChainRef 的 evo==0 分支):海枝枝的 4 个样子(碧蓝珊瑚/杏黄百合/
// 洋红沙丁/翠绿纶布)、首领变体与草系徽章变体在库里都是同一个品种 —— 一个选项/一条线跨的就是这一整组
// 形态,写上其中一个样子等于把「这条线只认这一个形状」说成事实。反过来也不能与链的展示名撞:那会让
// 下拉里并排出现两条看不出差别的选项,而一条是无链形态、另一条是整条链,选错就配错了。
func TestChainLabelNoFormWithoutChain(t *testing.T) {
	db, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	byLabel := map[string]string{}
	for _, head := range chainHeads(db) {
		byLabel[db.ChainLabel(head)] = fmt.Sprintf("链 %d", db.petbase[head].Evo)
	}
	if len(byLabel) == 0 {
		t.Fatal("一条链都没有 —— 样本不足")
	}

	seen := map[string]bool{}
	noChain, multiForm := 0, 0
	for id, info := range db.petbase {
		if info.Evo != 0 || info.Book == 0 {
			continue // Evo==0 才是无链;Book==0 是内部占位形态,与 petbase 的收录口径一致
		}
		if seen[info.Name] {
			// 同名多形态会并成同一个选项,故按名字去重后才断言(它们本就该给出同一个展示名)。
			multiForm++
			continue
		}
		seen[info.Name] = true
		noChain++

		label := db.ChainLabel(id)
		if label != info.Name {
			t.Errorf("无链形态 %d(%s,形态 %q)的展示名是 %q —— 无链品种按名字认,不该带形态",
				id, info.Name, info.Form, label)
		}
		if other, dup := byLabel[label]; dup {
			t.Errorf("无链品种 %s 与%s的展示名撞了(%q)—— 下拉里两条看不出差别的选项", info.Name, other, label)
		}
		byLabel[label] = "无链品种 " + info.Name
	}
	if noChain == 0 {
		t.Fatal("没有无链品种 —— 样本不足")
	}
	if multiForm == 0 {
		t.Error("没有一个「无链且同名多形态」的品种(样本不足)—— 上面按名字去重那段等于没测")
	}
}

// TestEggIconFallsBackToBaseForm:自己没有蛋 id 的形态,蛋图退回**本来样子**那颗。
//
// 依据就是**有没有蛋 id** —— 玩家实测的多形态物种里,绝大多数每个形态各有各的蛋(鸭吉吉 6 种
// 形态 6 颗蛋、地鼠两支是 egg_dishu / egg_dishu_2、波波螺是 egg_boboluo / egg_bobolouar…),
// 只有板板壳、石肤蜥、海盔虫这三支的第二形态没有自己的蛋 id(官方设定是**后天**变成的样子,孵
// 出来还是本来样子那颗蛋)。故:自己有蛋 id 的一律用自己的,绝不互相借 —— 借错了就是把两个品种
// 的蛋混成一颗,而这两颗蛋本来就长得不一样(下拉里正是靠它分人的)。
func TestEggIconFallsBackToBaseForm(t *testing.T) {
	db, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// 与 EggIconOfBase 内部同一条拼路径逻辑,供对照(「该给哪张」vs「实给哪张」)。
	iconOf := func(item uint32) string {
		icon := db.eggItems[item].Icon
		if icon == "" {
			return ""
		}
		if p := "egg/" + icon + ".webp"; db.imgFiles[p] {
			return p
		}
		return ""
	}

	own, fellBack, unknown := 0, 0, 0
	for id, info := range db.petbase {
		got := db.EggIconOfBase(id)
		if item, ok := db.eggByGroup[id]; ok {
			own++
			if want := iconOf(item); got != want {
				t.Errorf("形态 %d(%s)有自己的蛋 id,蛋图却是 %q —— 该是自己那颗 %q", id, info.Name, got, want)
			}
			continue
		}
		other, ok := db.eggByName[info.Name]
		if info.Name == "" || !ok {
			unknown++
			continue // 同名也没有蛋(超进化/分支形态)—— 认不出来,由调用方退回头像
		}
		fellBack++
		if want := iconOf(db.eggByGroup[other]); got != want {
			t.Errorf("形态 %d(%s)没有蛋 id,该退回本来样子 %d 那颗 %q,实得 %q",
				id, info.Name, other, want, got)
		}
	}
	if own == 0 || fellBack == 0 || unknown == 0 {
		t.Fatalf("样本不足:自己有蛋 %d 个、走兜底 %d 个、认不出 %d 个", own, fellBack, unknown)
	}

	// 清单:玩家实测的 17 个多形态物种 —— 14 个「各形态各有各的蛋」、3 个「只能生本来样子」。
	// 钉住这条界线,是因为它决定兜底该不该发生:多借一个,两个品种的蛋在下拉里就混了。
	cases := []struct {
		name  string
		forms int
		own   bool // true = 每个形态都有自己的蛋 id
	}{
		{"鸭吉吉", 6, true}, {"雪绒鸟", 4, true}, {"丢丢", 4, true}, {"蹦蹦种子", 4, true},
		{"海枝枝", 4, true}, {"小星光", 2, true}, {"旋叶虫", 2, true}, {"小狮鹫", 2, true},
		{"波波螺", 2, true}, {"梦游", 2, true}, {"棋棋", 2, true}, {"刺轮砣", 2, true},
		{"乌达", 2, true}, {"地鼠", 2, true},
		{"板板壳", 2, false}, {"石肤蜥", 2, false}, {"海盔虫", 2, false},
	}
	for _, c := range cases {
		ids := make([]uint32, 0, c.forms)
		for id, info := range db.petbase {
			if info.Name == c.name && info.Book != 0 { // 与 petbase 的收录口径一致
				ids = append(ids, id)
			}
		}
		if len(ids) != c.forms {
			t.Errorf("%s 有 %d 个形态,清单写的是 %d 个 —— 清单要与游戏数据一致", c.name, len(ids), c.forms)
			continue
		}
		withEgg := 0
		for _, id := range ids {
			if _, ok := db.eggByGroup[id]; ok {
				withEgg++
			}
		}
		if c.own && withEgg != c.forms {
			t.Errorf("%s 的 %d 个形态里只有 %d 个有自己的蛋 id —— 它该是「各形态各有各的蛋」",
				c.name, c.forms, withEgg)
		}
		if !c.own && withEgg != 1 {
			t.Errorf("%s 的 %d 个形态里有 %d 个有自己的蛋 id —— 它该是「只能生本来样子那颗」",
				c.name, c.forms, withEgg)
		}
	}
}

// TestChainOptionsSkipsInfertile:培育页的品种候选里**不能出现生不出蛋的品种**。
//
// 判据是**繁殖组(蛋组)**为「未发现」(见 IsInfertile):迪莫、帕尔萨斯/圣羽翼王那一系、绒绒、
// 犀角鸟、热团团等 53 个形态都落在这一组,正是游戏里进不了小窝配种的那批特殊精灵。玩家照着它们
// 建出一条线,只会一直等一个永远不会来的子代;而这在下拉里**看不出来** —— 名字、头像甚至蛋图
// 都可能齐全,只是配不出。
//
// 反方向同样要钉住:**不能**因为查不到蛋图就把品种滤掉。蛋只挂在初始形态上,而同一物种在
// petbase 里可能有多个条目、蛋只挂在其中一个(板板壳 3055 有、3516 没有)—— 按「有没有蛋图」
// 过滤会误伤几十个正常品种(实测误滤 98 个品种),玩家只会以为这个页面坏了。
func TestChainOptionsSkipsInfertile(t *testing.T) {
	db, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	bases := make([]uint32, 0, len(db.petbase))
	for id, info := range db.petbase {
		if info.Book != 0 { // 与 ChainOptions 同口径:Book==0 是内部占位形态
			bases = append(bases, id)
		}
	}
	if len(bases) == 0 {
		t.Fatal("petbase 为空 —— 样本不足")
	}

	opts := db.ChainOptions(bases)
	if len(opts) == 0 {
		t.Fatal("一个品种都没有 —— 过滤过头,或样本不足")
	}
	for _, o := range opts {
		if db.IsInfertile(o.Base) {
			t.Errorf("品种 %q 的繁殖组是「未发现」,不该出现在候选里 —— 照它建的线永远等不到子代", o.Label)
		}
	}

	byKey := map[string]bool{}
	for _, o := range opts {
		byKey[chainOptKey(o.Evo, o.Species)] = true
	}
	headOf := func(id uint32) uint32 {
		if steps := db.EvolutionChain(id); len(steps) > 0 {
			return steps[0].Petbase
		}
		return id
	}

	// 1. 不能生育的:整条链的繁殖组都是「未发现」,其品种必须不在候选里。
	dropped := 0
	for _, id := range bases {
		if !db.IsInfertile(headOf(id)) {
			continue
		}
		dropped++
		info := db.petbase[id]
		if byKey[chainOptKey(info.Evo, info.Name)] {
			t.Errorf("形态 %d(%s)整条链的繁殖组都是「未发现」,它的品种却仍在候选里", id, info.Name)
		}
	}
	if dropped == 0 {
		t.Fatal("全库没有「不能生育」的形态 —— 样本不足,上面这段断言等于没测")
	}

	// 2. 能生育、只是查不到蛋图的:品种必须**还在**候选里(判据不能退回「有没有蛋图」)。
	eggless := 0
	for _, id := range bases {
		if db.IsInfertile(id) || db.EggIconOfBase(id) != "" {
			continue
		}
		if db.IsInfertile(headOf(id)) || db.EggIconOfBase(headOf(id)) != "" {
			continue
		}
		eggless++
		info := db.petbase[id]
		if !byKey[chainOptKey(info.Evo, info.Name)] {
			t.Errorf("形态 %d(%s)只是查不到蛋图,品种却被滤掉了 —— 判据不该用「有没有蛋图」", id, info.Name)
		}
	}
	if eggless == 0 {
		t.Fatal("没有「能生育但查不到蛋图」的形态 —— 样本不足,上面这段断言等于没测")
	}
	t.Logf("全库 %d 个形态 → 候选 %d 个品种;滤掉 %d 个形态所属的不育品种,保住 %d 个只是查不到蛋图的形态",
		len(bases), len(opts), dropped, eggless)
}

// chainOptKey 复刻 ChainOptions 的去重键(有链按链 id、无链按名字),供上面那条测试反查。
func chainOptKey(evo uint32, species string) string {
	if evo != 0 {
		return fmt.Sprintf("e:%d", evo)
	}
	return "n:" + species
}

// TestEggIconPerForm:同名多形态的两条链,蛋图各是各的 —— 按物种名查会让它们指着同一张图,而
// 「这条线要孵哪种蛋」正是培育页要回答的问题。
//
// 唯一的例外是**没有自己蛋 id** 的那几支:官方设定它们是后天变成的样子,孵出来还是本来样子那颗
// 蛋(板板壳 3516 与 3055、石肤蜥 3500 与 3106、海盔虫 3475 与 3330 实测如此,见
// TestEggIconFallsBackToBaseForm)。故:两边**都有**自己的蛋 id 时图必须不同;有一边没有(它借的
// 就是另一边那颗),相同才对。
func TestEggIconPerForm(t *testing.T) {
	db, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	pairs, both, inherited := 0, 0, 0
	for _, heads := range chainGroupsByName(db) {
		if len(heads) < 2 {
			continue
		}
		for i := 0; i < len(heads); i++ {
			for j := i + 1; j < len(heads); j++ {
				pairs++
				a, b := db.EggIconOfBase(heads[i]), db.EggIconOfBase(heads[j])
				if a == "" || b == "" {
					continue
				}
				both++
				if a != b {
					continue
				}
				_, ownI := db.eggByGroup[heads[i]]
				_, ownJ := db.eggByGroup[heads[j]]
				if ownI && ownJ {
					t.Errorf("链 %d 与链 %d(阶段名相同、只有形态不同)都有自己的蛋 id,蛋图却都是 %q —— 蛋图没按形态查",
						db.petbase[heads[i]].Evo, db.petbase[heads[j]].Evo, a)
					continue
				}
				inherited++
			}
		}
	}
	if pairs == 0 {
		t.Fatal("没有可比的形态对 —— 样本不足")
	}
	if both == 0 {
		t.Fatal("没有任何一对两边都拿得到蛋图 —— 反查整体失效(改目录名/改物 id 都会这样静默失效)")
	}
	if inherited == 0 {
		t.Error("没有一对「借本来样子的蛋」的同名多形态(样本不足)—— 上面放行那一段等于没测")
	}
}
