package server

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/zxsos/rocom-go/internal/gamedata"
	"github.com/zxsos/rocom-go/internal/pet"
	"github.com/zxsos/rocom-go/internal/store"
)

// 建议最多返回多少条组合。5 条够玩家在小窝里挑 —— 再多只是把同一只母本换个父亲再列一遍,
// 而他要的是「用哪两只」。
const breedingSuggestN = 5

// handleBreeding 返回本账号全部培育线(GET /api/breeding),每条附带选种建议与回交建议。
//
// 建议与线一并下发、不另开 /suggest 接口的理由见 payload.go 的 BreedingLinePayload。
func (s *Server) handleBreeding(w http.ResponseWriter, r *http.Request) {
	sc := s.store.For(s.acct(r))
	lines, err := sc.ListBreedingLines()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// 候选池整个宠物库只取一次:所有线共用同一份快照。线通常个位数,而宠物库几百上千只 ——
	// 逐线重查等于把同一次 JSON 解析做 N 遍。
	pets, err := sc.ListBreedingPets()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// 按 gid 索引一次,供全部线做属性补全(见 fillSnapshots):逐线各建一遍是白花钱。
	byGid := make(map[uint32]*pet.Pet, len(pets))
	for _, p := range pets {
		byGid[p.Gid] = p
	}
	c := breedingCtx{
		db:    s.db,
		pets:  pets,
		byGid: byGid,
		// 蛋只在这一刻回查:有待孵代的线才需要,没有就完全不碰蛋表。
		eggs: s.eggSnapshots(sc, lines),
		// 雄性快照与蛋组掩码只按性别筛,每条线都一样(见 pet.PoolSource)。
		src: pet.NewPoolSource(pets),
		// 学院小窝(全库唯一一只):整份响应只读一次库,逐组合传同一个 gid
		// (见 pet.Suggest 的 nestGid)。
		nestGid: s.store.AcademyGid(),
		refs:    make(map[chainKey]pet.ChainRef, len(lines)),
	}
	out := make([]BreedingLinePayload, 0, len(lines))
	for _, l := range lines {
		out = append(out, s.breedingView(l, c))
	}
	// nest 与小窝接口(GET /api/nest)同一份视图:页面顶部的「小窝里是谁」与亲本卡上的勾选
	// 都靠它,顺手带在培育响应里就不必为它多拉一次(见 handleNest)。
	writeJSON(w, map[string]any{"lines": out, "nest": s.nestView(sc, c.nestGid)})
}

// breedingCtx 一次 GET /api/breeding 里**跨培育线共用**的素材。
//
// 线通常个位数、宠物库上千只,逐线各查一遍库 / 各建一次候选素材等于把同一件事做 N 遍。
// pets 与 byGid 早就这么做了,这里把候选素材与品种引用也一并收进来。
type breedingCtx struct {
	db    *gamedata.DB
	pets  []*pet.Pet
	byGid map[uint32]*pet.Pet
	eggs  map[uint32]*pet.EggSnapshot
	src   pet.PoolSource
	// nestGid 学院小窝里那只的 gid(0 = 空着):它在某组亲本里时,那组的性格按 100% 随它算
	// (见 pet.Suggest / pet.nestNature)。
	nestGid uint32
	refs    map[chainKey]pet.ChainRef
}

// chainKey 品种引用(见 pet.ChainRefOf)的缓存键:建一次要展开整条链的形态集合。
type chainKey struct {
	evo     uint32
	species string
}

// chainRefOf 取这条线认的品种引用,同品种的线只建一次。
func (c breedingCtx) chainRefOf(l *pet.BreedingLine) pet.ChainRef {
	k := chainKey{l.Evo, l.Species}
	if r, ok := c.refs[k]; ok {
		return r
	}
	r := pet.ChainRefOf(c.db, l.Evo, l.Species)
	c.refs[k] = r
	return r
}

// handleBreedingPool 返回某品种的补录候选池(GET /api/breeding/pool?evo=&species=)。
//
// 参数是**品种标识**(见 pet.ChainRef):evo = 进化链 id(线认的品种),species = 形态名
// (只有无链形态用得上)。两者至少给一个。
//
// 与 handleBreeding 一样要读整个宠物库(候选池不能按页取,见 store.ListAllPets),但**不做缓存**:
// 宠物库随时在变(抓捕/放生/进化),缓存住会让玩家选到库里已经没有的个体,而补录提交的是快照,
// 错认一只就污染整条培育史。单机自用,一次全库解析几十毫秒、按需触发(打开补录面板时才拉)。
func (s *Server) handleBreedingPool(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	evo, _ := strconv.ParseUint(q.Get("evo"), 10, 32)
	// 品种可以**不给**:建线表单允许先挑种母/种公,再由种母把品种(蛋)带出来(见 web 的 Breeding),
	// 那一刻还没有品种可传。空引用走「全库雌雄」那一支(见 pet.BreedCandidates)。
	ref := pet.ChainRefOf(s.db, uint32(evo), q.Get("species"))
	pets, err := s.store.For(s.acct(r)).ListBreedingPets()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	mothers, fathers, kids := pet.BreedCandidates(s.db, ref, pets)
	// nil 切片会序列化成 null,前端得为每个队列写一次 `|| []`;这里统一给空数组 ——
	// 「这个品种一只候选都没有」是常态(新建的空品种),不该让前端到处防 null。
	if mothers == nil {
		mothers = []pet.PetCandidate{}
	}
	if fathers == nil {
		fathers = []pet.PetCandidate{}
	}
	if kids == nil {
		kids = []pet.PetCandidate{}
	}
	// 回显品种标识:Evo/Species 正是前端缓存这份候选池的键(切换品种时按它判要不要重拉),
	// 由服务端给回来,前端不必自己记「刚才请求的是什么」。
	writeJSON(w, BreedingPoolPayload{
		Evo: ref.Evo, Species: ref.Species, Mothers: mothers, Fathers: fathers, Kids: kids,
	})
}

// breedingView 组一条线的响应:线本体 + 按它自己的目标算出的建议。
//
// byGid 是全库宠物按 gid 索引的快照(已算过百分位):缺失属性的亲本/子代按它**投影补全**
// (见 fillSnapshots)—— 补出来的值只进这一次响应,不落库。
//
// eggs 是本账号全部蛋按 gid 索引的投影(见 eggSnapshots);本线只挑用得上的那几颗。
func (s *Server) breedingView(l *pet.BreedingLine, c breedingCtx) BreedingLinePayload {
	fillSnapshots(l, c.byGid)
	v := BreedingLinePayload{BreedingLine: l}
	// 只带本线待孵代要用的蛋:别把全账号的蛋都下发下去。
	v.Eggs = eggsForLine(l, c.eggs)
	// 「孵出的那只已不在库」的代:依据本次取的全库宠物 —— 在库的话早被认领了,
	// 故这里只会列出真正等不到的那些(见 LostChildGens 的注释)。
	for _, g := range l.Pending {
		if g.ChildGid != 0 && g.Child == nil && c.byGid[g.ChildGid] == nil {
			v.LostChildGens = append(v.LostChildGens, g.Gen)
		}
	}
	// 品种的展示名(链上各阶段都列出来,见 gamedata.ChainLabelOf):只显示 Species 会让人以为
	// 这条线只认那一个形态,而链上其它阶段的 ♀ 同样能用 —— 那正是这个页面最容易踩的坑。
	v.ChainName = c.db.ChainLabelOf(l.Evo, l.Species)
	// 种母快照:同品种几条线靠它区分(见 payload 的 Mother)。库里已没有她(放生/送人)时留空,
	// 但要**标记**出来 —— 「固定过、被放生」与「从没固定过」是两回事,前端要分开显示(见
	// payload 的 MotherReleased)。
	if mg := pet.MotherGidOf(l); mg != 0 {
		if p, ok := c.byGid[mg]; ok {
			snap := pet.ParentSnapshot(p)
			v.Mother = &snap
		} else {
			v.MotherReleased = true
		}
	}
	// 指定种公快照:与种母成对展示(见 payload 的 Father)。**不派生、不参与任何计算** ——
	// 只认线上显式指定的那个 gid,查不到就是他已不在库,照种母的样子标记出来(见 FatherReleased)。
	if fg := l.FatherGid; fg != 0 {
		if p, ok := c.byGid[fg]; ok {
			snap := pet.ParentSnapshot(p)
			v.Father = &snap
		} else {
			v.FatherReleased = true
		}
	}
	// 学院小窝的**生效值**:这条线设了计划就按计划算(那是玩家在「打算怎么放」时主动要的
	// 试算),否则按游戏真值算(由家园管线自动维护,见 pipeline.syncAcademyNest)。
	// 两个来源都在响应里下发(顶层 nest + 本线的 nestPlan),页面同时显示、不一致时说清楚。
	nestGid := c.nestGid
	if l.NestPlanGid != 0 {
		nestGid = l.NestPlanGid
		if p, ok := c.byGid[l.NestPlanGid]; ok {
			snap := pet.ParentSnapshot(p)
			v.NestPlan = &snap
		} else {
			v.NestPlanReleased = true
		}
	}
	pool := c.src.BreedPool(c.chainRefOf(l))
	v.Suggest = pet.Suggest(pool, l.Goal, breedingSuggestN, pet.DescendantGids(l), nestGid)
	// 嗓音目标是**向下取整的均值**,任一方不到目标值就永远到不了(见 pet.VoiceReach)。
	// 只在填了嗓音目标时算:体重的预期是浮点均值、且实测可高于双亲均值,没有这种
	// 「必须双亲都到位」的性质,不该跟着给结论;性格本就是概率。
	if l.Goal.Voice != nil {
		r := pet.VoiceReachOf(pool, *l.Goal.Voice)
		v.Reach = &r
	}
	// 回交只在已经有子代之后才谈得上(没有可回交的对象)。没子代时留 nil,
	// 前端据此整块不显示,而不是显示一个空壳。
	if child, m, f, ok := pet.LatestChild(l); ok {
		bc := pet.BackcrossAdvice(child, m, f, breedMates(pool, child), l.Goal, nestGid)
		v.Backcross = &bc
	}
	return v
}

// breedMates 按**子代性别**取「另一半」的候选池。
//
// 蛋的物种必定随母本,故这两档不是 Preference 而是规则:
//   - 子代 ♀ → 与母本同蛋组的 ♂(父亲是谁不影响物种,能配上就有意义);
//   - 子代 ♂ → 本线品种的 ♀(换谁都得是同一品种 —— 配一只别品种的 ♀,孵出来就是那只 ♀
//     的品种,这条线也就断了)。
//
// 升级前一律给 ♂ 池,雄性子代的回交/换种建议因此整个是反的。
func breedMates(pool pet.Pool, child pet.EggParent) []pet.EggParent {
	if child.Gender == "♂" {
		return breedMothers(pool)
	}
	return breedFathers(pool)
}

// breedFathers 摊平候选池里的父本(按 gid 去重),当回交建议的「换种」备选。
//
// 去重是必要的:同一位种公能给多位母本配对,逐母本摊平会让它重复出现,而 BackcrossAdvice
// 是逐个比出最接近目标的那只 —— 重复项不会改变结论,却让这次比较白做几遍。
func breedFathers(pool pet.Pool) []pet.EggParent {
	seen := make(map[uint32]bool)
	var out []pet.EggParent
	for _, c := range pool.Cands {
		for _, idx := range c.FatherIdx {
			f := pool.Fathers[idx]
			if seen[f.Gid] {
				continue
			}
			seen[f.Gid] = true
			out = append(out, f)
		}
	}
	return out
}

// breedMothers 摊平候选池里的母本(按 gid 去重),给**雄性**子代当回交/换种的对象
// (见 breedMates)。候选池的母本本就是这条线品种的 ♀,正是唯一合法的那批。
func breedMothers(pool pet.Pool) []pet.EggParent {
	seen := make(map[uint32]bool)
	var out []pet.EggParent
	for _, c := range pool.Cands {
		if seen[c.Mother.Gid] {
			continue
		}
		seen[c.Mother.Gid] = true
		out = append(out, c.Mother)
	}
	return out
}

// eggSnapshots 回查「待孵的那几代」各自对应的蛋,按蛋 gid 索引。
//
// 只在**真的有待孵代**时才读蛋表:没有待孵代是常态(绝大多数请求),不该为它付一次全表读。
// 有则一次 ListEggs 取全量再按 gid 筛 —— 逐代各查一次是白花钱。
//
// 读不到某颗蛋时不报错、只是不给:那意味着蛋已经不在背包(孵掉 / 送人 / 对账清理),
// 前端比对着「代上有 eggGid、响应里没有这颗蛋」就能说出这句,而不是让用户干等。
func (s *Server) eggSnapshots(sc *store.Scoped, lines []*pet.BreedingLine) map[uint32]*pet.EggSnapshot {
	want := map[uint32]bool{}
	for _, l := range lines {
		for _, g := range l.Pending {
			if g.EggGid != 0 && g.ChildGid == 0 {
				want[g.EggGid] = true
			}
		}
	}
	if len(want) == 0 {
		return nil
	}
	eggs, err := sc.ListEggs(store.EggFilter{})
	if err != nil {
		log.Printf("读蛋表失败(待孵代暂不显示蛋属性): %v", err)
		return nil
	}
	out := make(map[uint32]*pet.EggSnapshot, len(want))
	for _, e := range eggs {
		if !want[e.Gid] {
			continue
		}
		// 补算「要双亲快照才推得出」的嗓音与奖牌 —— ListEggs 不负责这一步(见其注释)。
		out[e.Gid] = pet.EggSnapshotOf(pet.RefreshEggView(e, s.db))
	}
	return out
}

// eggsForLine 从全账号的蛋里挑出这条线用得上的那几颗(只有待孵代需要)。
func eggsForLine(l *pet.BreedingLine, all map[uint32]*pet.EggSnapshot) map[uint32]*pet.EggSnapshot {
	var out map[uint32]*pet.EggSnapshot
	for _, g := range l.Pending {
		if g.EggGid == 0 || g.ChildGid != 0 {
			continue
		}
		if snap, ok := all[g.EggGid]; ok {
			if out == nil {
				out = make(map[uint32]*pet.EggSnapshot, 1)
			}
			out[g.EggGid] = snap
		}
	}
	return out
}

// fillSnapshots 把这条线里**属性缺失**的亲本/子代快照按宠物库补上(只填空缺,见
// pet.FillParentSnapshot)。
//
// 只做读取时投影,**不落库**:库里存的仍是收蛋那一刻记下的原样。自动改写历史比缺一个
// 性格更糟 —— 玩家要的是「当时记的是什么」,而补出来的值来自今天的库。真要写进去由玩家
// 在页面上点「补全这一代」,走现有的整条覆盖写。
//
// byGid 由 handleBreeding 一次建好供全部线共用:宠物库几百上千只,逐线各建一次太浪费。
func fillSnapshots(l *pet.BreedingLine, byGid map[uint32]*pet.Pet) {
	for i := range l.Gens {
		fillGeneration(&l.Gens[i], byGid)
	}
	for i := range l.Pending {
		fillGeneration(&l.Pending[i], byGid)
	}
}

// fillGeneration 补一代里的双亲与子代快照(串窝的多个父本候选同样要补 —— 玩家靠它们
// 判断「当时到底可能用了谁」)。
func fillGeneration(g *pet.Generation, byGid map[uint32]*pet.Pet) {
	for _, p := range []*pet.EggParent{g.Mother, g.Father, g.Child} {
		if p != nil {
			pet.FillParentSnapshot(p, byGid[p.Gid])
		}
	}
	for i := range g.Fathers {
		pet.FillParentSnapshot(&g.Fathers[i], byGid[g.Fathers[i].Gid])
	}
}

// —— 学院小窝(全库唯一一只)——

// handleNest 读 / 写学院小窝「真值」(GET/POST /api/nest)。
//
// 玩法:小窝里那只参与孵蛋时,子代性格 **100% 随它**(见 pet.nestNature)。它在游戏里只有
// 一个,故这里存的是**全库唯一**的一个 gid(单行表,见 store.AcademyGid)—— 换一只即覆盖,
// 不是「每个账号各有一只」。
//
// ⚠️ **正常不需要用它**:真值由家园管线自动维护(自己家园里学院小窝的住户,见
// pipeline.syncAcademyNest),玩家在游戏里换一只,这边就跟着变。它保留下来是给**抓不到**的
// 场合兜底(没进过家园、包里没抓到、或想手工纠正),以及供页面上的「把计划记为真值」一键调用。
//
// 玩家自己「打算怎么放」是另一件事,存在**培育线**里(pet.BreedingLine.NestPlanGid,走
// POST /api/breeding 整条覆盖写),预测口径是「计划 ?? 真值」—— 详见 api_breeding.go 的 breedingView。
//
// 请求体 {gid}:0 = 把小窝空出来;其余必须是**本账号库里确实有**的宠物。为什么要校验:小窝里
// 那只参与孵蛋才有加成,库里没有的 gid 只会静默失效 —— 页面上勾了、百分比却不变,比报错难查
// 得多。切账号后旧 gid 不在这个账号的库里,也走这条(不给勾)。
func (s *Server) handleNest(w http.ResponseWriter, r *http.Request) {
	acc := s.acct(r)
	sc := s.store.For(acc)
	if r.Method == http.MethodGet {
		writeJSON(w, s.nestView(sc, s.store.AcademyGid()))
		return
	}
	var body struct {
		Gid uint32 `json:"gid"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "请求体不是合法 JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if body.Gid != 0 {
		p, err := sc.GetPet(body.Gid)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if p == nil {
			http.Error(w, "宠物库里没有这只(可能已放生 / 送人),不能放进学院小窝", http.StatusBadRequest)
			return
		}
	}
	if err := s.store.SetAcademyGid(body.Gid); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.hub.Broadcast("breeding", acc, map[string]any{"account": acc})
	writeJSON(w, s.nestView(sc, body.Gid))
}

// nestView 学院小窝的当前状态:{gid, name, nature}。
//
// name/nature 按 gid **现查**宠物库补上,不落库:页面据此写出「小窝里是小火(固执)· 子代性格
// 100% 随它」,而宠物被放生后这一行查不到、只给空 —— 由页面写「已不在库」,与亲本卡对 released
// 的处理同一口径(见 web 的 ParentSlot)。gid=0(小窝空着)时只有 gid 一个键。
func (s *Server) nestView(sc *store.Scoped, gid uint32) map[string]any {
	out := map[string]any{"gid": gid}
	if gid == 0 {
		return out
	}
	if p, err := sc.GetPet(gid); err == nil && p != nil {
		out["name"] = p.Name
		out["nature"] = p.Nature
	}
	return out
}

// handleBreedingSave 建线或整条更新(POST /api/breeding)。
//
// body 就是整条 BreedingLine:目标、状态、全部代数都在里面。一条线序列化后不过几 KB,
// 整条提交最省事,也让「改目标」「手动补录一代」「删掉某一代」共用一个入口 —— 不必为每种
// 编辑各开一个接口。代价是两个页面同时改会互相覆盖;这是单机自用工具,不值得为它做字段级
// 合并(真要并发,后写的那份也是用户自己刚点的)。
func (s *Server) handleBreedingSave(w http.ResponseWriter, r *http.Request) {
	var body pet.BreedingLine
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "请求体不是一条合法的培育线: "+err.Error(), http.StatusBadRequest)
		return
	}
	if body.ID == "" {
		http.Error(w, "id 不能为空", http.StatusBadRequest)
		return
	}
	now := time.Now().Unix()
	if body.CreatedAt == 0 {
		body.CreatedAt = now // 首次写入才定创建时间,之后改目标不该动它
	}
	body.UpdatedAt = now
	if body.Status == "" {
		body.Status = pet.BreedingActive
	}
	// 品种身份补一次(见 pet.DeriveChain):请求漏带 evo(老版本的页面、手工构造的请求)时按
	// species 反查补上,否则这条线会退化成按名字认品种 —— 能跑,但正是这个改动要修掉的那种
	// 「库里明明有候选却一只都配不出来」。
	pet.DeriveChain(s.db, &body)
	// 种母身份也补一次并**落库**(与 DeriveChain 同一套路子):老线没有 MotherGid 字段,
	// 读取时是靠末代派生的(见 pet.MotherGidOf);这里补上之后这条线就固定在这只种母身上,
	// 不必每次读都派生一遍。补录/改目标都会经这里,故老线在下一次保存时自然收敛。
	if body.MotherGid == 0 {
		body.MotherGid = pet.MotherGidOf(&body)
	}
	// 指定种公(FatherGid)**故意不做任何兜底**:它是个计划值,没有可派生的来源(见
	// pet.BreedingLine.FatherGid 的三点差别)。客户端给什么就是什么 —— 给一只已经放生的宠
	// 也照样落库,读取时按「已不在库」标记(见 payload.FatherReleased),这与 Gens 里允许
	// 存已放生亲本的快照是同一口径:线上存的是玩家当时的意图,不为它做校验或清洗。
	acc := s.acct(r)
	sc := s.store.For(acc)
	// 手动补录也走这条整条覆盖写,故达成判定同样挂在这里。读旧线**只为**取改动前的达成结果:
	// 没有这一读,玩家每次补录/改目标都会被自动置 done(哪怕早就达成过),手动改回进行中也会失效。
	old, err := sc.GetBreedingLine(body.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if pet.AutoDoneOnReach(&body, pet.ReachGoal(old)) {
		log.Printf("账号 %s 的培育线 %s 已刷到目标,自动标记为已达成", acc, body.ID)
	}
	if err := sc.UpsertBreedingLine(&body); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.hub.Broadcast("breeding", acc, map[string]any{"account": acc})
	writeJSON(w, body)
}

// handleBreedingDelete 删掉一条培育线(DELETE /api/breeding?id=)。
//
// 只删培育线本身,不碰任何宠物数据 —— 线上记的双亲/子代都只是快照,删线不该带走宠物。
func (s *Server) handleBreedingDelete(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "缺少 id", http.StatusBadRequest)
		return
	}
	acc := s.acct(r)
	if err := s.store.For(acc).DeleteBreedingLine(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.hub.Broadcast("breeding", acc, map[string]any{"account": acc})
	w.WriteHeader(http.StatusNoContent)
}

// handleBreedingMerge 把一条子线**并入**它的母线(POST /api/breeding/merge)。
//
// body: {"child":"<子线 id>"} —— 母线由子线的 ParentLineID 决定,不另传:传两个 id 就可能
// 指定一对并不存在父子关系的线,而「谁是谁的子线」是数据里已经定了的事。
//
// 为什么放后端、而不是让前端「改母线 + 删子线」两次调用:那是两次写,中间失败就留下半截
// (母线多了几代、子线还在 / 母线没改成、子线已删);合并这种**不可逆**的操作更不该有中间态。
// 与认领同理(见 handleBreedingClaim):搬动逻辑只该有一处。
//
// 二次确认由前端做(confirmDialog)—— 合并会删掉子线,而培育史删了就回不来。
func (s *Server) handleBreedingMerge(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Child string `json:"child"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "请求体无法解析: "+err.Error(), http.StatusBadRequest)
		return
	}
	if body.Child == "" {
		http.Error(w, "缺少 child(要并入母线的那条子线 id)", http.StatusBadRequest)
		return
	}
	acc := s.acct(r)
	sc := s.store.For(acc)
	child, err := sc.GetBreedingLine(body.Child)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if child == nil {
		http.Error(w, "这条培育线不存在", http.StatusNotFound)
		return
	}
	if child.ParentLineID == "" {
		http.Error(w, "这条线不是任何线的子线(没换过种母),无从并入", http.StatusBadRequest)
		return
	}
	parent, err := sc.GetBreedingLine(child.ParentLineID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if parent == nil {
		http.Error(w, "母线已被删除:先把子线的 parentLineId 清掉再试", http.StatusBadRequest)
		return
	}
	if !pet.MergeChildLine(parent, child) {
		http.Error(w, "这条子线没有可并入的代数", http.StatusBadRequest)
		return
	}
	parent.UpdatedAt = time.Now().Unix()
	if err := sc.UpsertBreedingLine(parent); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// 孙辈改挂到母线上:否则它们会指向一条马上要删掉的线,谱系视图上从这一代断掉。
	if err := sc.ReparentLines(child.ID, parent.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := sc.DeleteBreedingLine(child.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.hub.Broadcast("breeding", acc, map[string]any{"account": acc})
	writeJSON(w, parent)
}

// handleBreedingClaim 把库里的某只宠认领为某一代的子代(POST /api/breeding/claim)。
//
// body: {"id":"...","gen":2,"childGid":12345}
//
// 为什么认领要放后端、而不是让前端改完整条线再提交:子代快照(名字/嗓音/体重百分位/性格)
// 要按当前 gamedata 从 pets 里取,前端拿不到权威的那份;且「从待认领挪进正式代数」这个搬动
// 只该有一处实现 —— 否则管线里的自动认领与这里的手动认领早晚各写一套、行为不一致。
func (s *Server) handleBreedingClaim(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID       string `json:"id"`
		Gen      int    `json:"gen"`
		ChildGid uint32 `json:"childGid"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "请求体无法解析: "+err.Error(), http.StatusBadRequest)
		return
	}
	if body.ID == "" || body.Gen <= 0 || body.ChildGid == 0 {
		http.Error(w, "id / gen / childGid 都不能为空", http.StatusBadRequest)
		return
	}
	acc := s.acct(r)
	sc := s.store.For(acc)
	line, err := sc.GetBreedingLine(body.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if line == nil {
		http.Error(w, "没有这条培育线", http.StatusNotFound)
		return
	}
	child, err := sc.GetPet(body.ChildGid)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if child == nil {
		http.Error(w, "库里没有这只宠物", http.StatusNotFound)
		return
	}
	pet.FillSizePercentile(s.db, child)
	snap := pet.ParentSnapshot(child)
	// 认领前先算:自动置 done 只在「由未达成变达成」时动手(见 pet.AutoDoneOnReach)。
	reachedBefore := pet.ReachGoal(line)
	if !pet.ClaimGeneration(line, body.Gen, snap) {
		http.Error(w, "这一代不在待认领列表里", http.StatusBadRequest)
		return
	}
	line.UpdatedAt = time.Now().Unix()
	if pet.AutoDoneOnReach(line, reachedBefore) {
		log.Printf("账号 %s 认领培育线 %s 第 %d 代后刷到目标,自动标记为已达成", acc, line.ID, body.Gen)
	}
	if err := sc.UpsertBreedingLine(line); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.hub.Broadcast("breeding", acc, map[string]any{"account": acc})
	writeJSON(w, line)
}

// 认领本身的搬动逻辑在 pet.ClaimGeneration(管线里自动认领走的是同一份,见 pipeline/breeding.go)
// —— 两处各写一套的话,手动认领与自动认领的行为早晚会分叉。
