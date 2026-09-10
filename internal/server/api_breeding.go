package server

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/whoisnian/rocom-capture/internal/pet"
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
	pets, err := sc.ListAllPets()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	pet.FillSizePercentile(s.db, pets...) // 百分位按当前 gamedata 注入,不落库
	// 按 gid 索引一次,供全部线做属性补全(见 fillSnapshots):逐线各建一遍是白花钱。
	byGid := make(map[uint32]*pet.Pet, len(pets))
	for _, p := range pets {
		byGid[p.Gid] = p
	}
	out := make([]BreedingLinePayload, 0, len(lines))
	for _, l := range lines {
		out = append(out, s.breedingView(l, pets, byGid))
	}
	writeJSON(w, map[string]any{"lines": out})
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
	ref := pet.ChainRefOf(s.db, uint32(evo), q.Get("species"))
	if ref.Empty() {
		http.Error(w, "缺少 evo 或 species", http.StatusBadRequest)
		return
	}
	pets, err := s.store.For(s.acct(r)).ListAllPets()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	pet.FillSizePercentile(s.db, pets...) // 百分位按当前 gamedata 注入,不落库
	mothers, fathers, kids := pet.BreedCandidates(ref, pets)
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
func (s *Server) breedingView(l *pet.BreedingLine, pets []*pet.Pet, byGid map[uint32]*pet.Pet) BreedingLinePayload {
	fillSnapshots(l, byGid)
	v := BreedingLinePayload{BreedingLine: l}
	// 品种的展示名(链上各阶段都列出来,见 gamedata.ChainLabelOf):只显示 Species 会让人以为
	// 这条线只认那一个形态,而链上其它阶段的 ♀ 同样能用 —— 那正是这个页面最容易踩的坑。
	v.ChainName = s.db.ChainLabelOf(l.Evo, l.Species)
	pool := pet.BreedPool(pet.ChainRefOf(s.db, l.Evo, l.Species), pets)
	v.Suggest = pet.Suggest(pool, l.Goal, breedingSuggestN, descendantGids(l))
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
		bc := pet.BackcrossAdvice(child, m, f, breedMates(pool, child), l.Goal)
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
func breedMates(pool []pet.Candidate, child pet.EggParent) []pet.EggParent {
	if child.Gender == "♂" {
		return breedMothers(pool)
	}
	return breedFathers(pool)
}

// breedFathers 摊平候选池里的父本(按 gid 去重),当回交建议的「换种」备选。
//
// 去重是必要的:同一位种公能给多位母本配对,逐母本摊平会让它重复出现,而 BackcrossAdvice
// 是逐个比出最接近目标的那只 —— 重复项不会改变结论,却让这次比较白做几遍。
func breedFathers(pool []pet.Candidate) []pet.EggParent {
	seen := make(map[uint32]bool)
	var out []pet.EggParent
	for _, c := range pool {
		for _, f := range c.Fathers {
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
func breedMothers(pool []pet.Candidate) []pet.EggParent {
	seen := make(map[uint32]bool)
	var out []pet.EggParent
	for _, c := range pool {
		if seen[c.Mother.Gid] {
			continue
		}
		seen[c.Mother.Gid] = true
		out = append(out, c.Mother)
	}
	return out
}

// descendantGids 这条线历代子代的 gid 集合:建议里若父本就在其中,那便是回交
// (拿自己的子代往上倒着配),见 pet.Suggest 的 Backcross。
func descendantGids(l *pet.BreedingLine) map[uint32]bool {
	out := make(map[uint32]bool)
	for _, g := range l.Gens {
		if g.Child != nil && g.Child.Gid != 0 {
			out[g.Child.Gid] = true
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
