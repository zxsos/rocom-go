package server

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/whoisnian/rocom-capture/internal/gamedata"
	"github.com/whoisnian/rocom-capture/internal/pet"
	"github.com/whoisnian/rocom-capture/internal/store"
)

// parseFilter 从查询参数构造 store.Filter(handlePets/handlePetPage 共用)。
// 奖牌按名筛选,这里将奖牌名解析为 id 列表(pet_medal 存 id),同名多枚时全含。
func (s *Server) parseFilter(q url.Values) store.Filter {
	atoi := func(k string) int { n, _ := strconv.Atoi(q.Get(k)); return n }
	atoi64 := func(k string) int64 { n, _ := strconv.ParseInt(q.Get(k), 10, 64); return n }
	f := store.Filter{
		Search:      q.Get("search"),
		Nature:      q.Get("nature"),
		Gender:      q.Get("gender"),
		TalentRank:  q.Get("talentRank"),
		MedalIDs:    s.medalIDs[q.Get("medal")],
		Speciality:  q.Get("speciality"),
		EggGroup:    q.Get("eggGroup"),
		PartnerMark: q.Get("partnerMark"),
		Shiny:       q.Get("shiny"),
		Colorful:    q.Get("colorful"),
		Form:        q.Get("form"),
		Box:         q.Get("box"),
		CatchAfter:  atoi64("catchAfter"),
		LevelMin:    atoi("levelMin"),
		LevelMax:    atoi("levelMax"),
		Sort:        q.Get("sort"),
		Order:       q.Get("order"),
		Page:        atoi("page"),
		PageSize:    atoi("pageSize"),
	}
	if t := q.Get("types"); t != "" {
		f.Types = strings.Split(t, ",")
	}
	if ne := q.Get("natureExclude"); ne != "" {
		f.NatureExclude = strings.Split(ne, ",")
	}
	// 性格多选:命中其中任一即算。与 Nature 互斥(前端矩阵只在点单格时用 Nature)。
	if ni := q.Get("natureIn"); ni != "" {
		f.NatureIn = strings.Split(ni, ",")
	}
	// 奖牌特征(数值判定,与地图奖牌筛选同口径):各参数为 "1" 时启用对应条件,多选=同时满足。
	// 阈值固定为奖牌边界(大块头体重百分位≥98 / 小不点≤2 / 婉转声嗓音≥96 / 粗嗓门≤-96)。
	if q.Get("medalBig") == "1" {
		f.WeightPctMin = 98
	}
	if q.Get("medalSmall") == "1" {
		f.WeightPctMax = 2
	}
	if q.Get("medalHigh") == "1" {
		f.VoiceMin = 96
	}
	if q.Get("medalLow") == "1" {
		f.VoiceMax = -96
	}
	return f
}

func (s *Server) handlePets(w http.ResponseWriter, r *http.Request) {
	f := s.parseFilter(r.URL.Query())
	pets, total, err := s.store.For(s.acct(r)).ListPets(f)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if pets == nil {
		pets = []*pet.Pet{}
	}
	pet.FillSizePercentile(s.db, pets...) // 读取时注入身高/体重范围与百分位(静态参考,不入库)
	writeJSON(w, map[string]any{"total": total, "pets": pets})
}

// petDetailView 是单只宠物详情的响应形状:宠物本体字段平铺 + 该形态的三类技能。
//
// 为什么用包装而非给 pet.Pet 加字段:Pet 会被整个序列化进 pets.data 列,
// 加字段等于给每只宠物都存一份技能(体积 × 宠物数)—— 正是 git 0762eb6 移除
// Pet.SkillIDs 的理由之一。故技能只在**详情接口读取时**按形态注入,不进库。
// 内嵌 *pet.Pet 让 JSON 字段平铺,前端拿到的结构与改造前一致,只多出技能字段。
type petDetailView struct {
	*pet.Pet
	// 以下三项都是**该形态可获得的技能**(可换配置),不是这只宠物当前携带的。
	// 三者按获取途径划分,几乎互斥(天生∩技能石 3 条、与血脉 0 条),故并列不去重:
	//   Skills     天生:升级自然学会,带 level
	//   Learnable  技能石:需消耗技能石才能学
	//   Bloodline  血脉:通过血脉获得(资料站无等级/条件,故只有技能本身)
	// 第三方资料没覆盖该形态时为 nil,键不出现 —— 见 gamedata.InnateSkills 等。
	Skills    []gamedata.InnateSkill `json:"skills,omitempty"`
	Learnable []gamedata.Skill       `json:"learnable,omitempty"`
	Bloodline []gamedata.Skill       `json:"bloodline,omitempty"`
}

func (s *Server) handlePet(w http.ResponseWriter, r *http.Request) {
	gid, _ := strconv.ParseUint(r.PathValue("gid"), 10, 32)
	p, err := s.store.For(s.acct(r)).GetPet(uint32(gid))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if p == nil {
		http.Error(w, "not found", 404)
		return
	}
	pet.FillSizePercentile(s.db, p)
	// 技能按**当前形态**取(base_conf_id);它比 conf_id(进化线一阶)更准,
	// 已进化的宠物才能看到本形态的技能。
	v := &petDetailView{Pet: p}
	if sk := s.db.InnateSkills(p.BaseConfID); len(sk) > 0 {
		v.Skills = sk
	}
	if sk := s.db.LearnableSkills(p.BaseConfID); len(sk) > 0 {
		v.Learnable = sk
	}
	if sk := s.db.BloodlineSkills(p.BaseConfID); len(sk) > 0 {
		v.Bloodline = sk
	}
	writeJSON(w, v)
}

// handlePetPage 返回某宠物在当前筛选+排序下所处的页码,供盒子示意图点击跳页。
func (s *Server) handlePetPage(w http.ResponseWriter, r *http.Request) {
	gid, _ := strconv.ParseUint(r.URL.Query().Get("gid"), 10, 32)
	page, found := s.store.For(s.acct(r)).PetPage(uint32(gid), s.parseFilter(r.URL.Query()))
	writeJSON(w, map[string]any{"page": page, "found": found})
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	beforeID, _ := strconv.Atoi(q.Get("beforeId"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	events, err := s.store.For(s.acct(r)).ListEvents(limit, beforeID, offset)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if events == nil {
		events = []*store.Event{}
	}
	// 补注体重/身高百分位(供事件页体重/声音高亮规则按百分位判定;历史事件也据当前 gamedata 刷新)。
	for _, ev := range events {
		if ev.Pet != nil {
			pet.FillSizePercentile(s.db, ev.Pet)
		}
	}
	writeJSON(w, events)
}

// handleEventCount 返回事件总数,供前端展示「累计获得宠物数」(失去事件不入库)。
func (s *Server) handleEventCount(w http.ResponseWriter, r *http.Request) {
	n, err := s.store.For(s.acct(r)).CountEvents()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, map[string]any{"count": n})
}

// handleEventStats 返回事件统计(总览/稀有/近30天分布/热门形态)。
func (s *Server) handleEventStats(w http.ResponseWriter, r *http.Request) {
	st, err := s.store.For(s.acct(r)).StatsEvents()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, st)
}

// handleClearEvents 清空事件历史。
func (s *Server) handleClearEvents(w http.ResponseWriter, r *http.Request) {
	if err := s.store.For(s.acct(r)).ClearEvents(); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleFilterOptions(w http.ResponseWriter, r *http.Request) {
	sc := s.store.For(s.acct(r))
	opts := sc.FilterOptions()
	// 奖牌下拉:按「拥有」筛选,列出本账号宠物拥有过的奖牌名(id→名,去重,保持 id 升序)。
	var names []string
	seen := map[string]bool{}
	for _, id := range sc.OwnedMedalIDs() {
		if m, ok := s.db.Medal(id); ok && m.Name != "" && !seen[m.Name] {
			seen[m.Name] = true
			names = append(names, m.Name)
		}
	}
	opts["medal"] = names
	writeJSON(w, opts)
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	count, _ := s.store.For(s.acct(r)).CountPets()
	writeJSON(w, map[string]any{"petCount": count})
}

// handleAccounts 返回已知账号列表(account/name/petCount/online),供前端账号切换下拉;
// online 由 server 内存表实时判定(最近 30s 内有流量,见 AccountOnline),不落库。
func (s *Server) handleAccounts(w http.ResponseWriter, r *http.Request) {
	accs, err := s.store.ListAccounts()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if accs == nil {
		accs = []store.AccountInfo{}
	}
	// 合并当日排行榜称号(大富翁/赚钱王/败家子,佩戴一天),供账号下拉等处展示
	titleOf := map[string]string{}
	if titles, err := s.store.RankTitles(todayCST()); err == nil {
		for _, t := range titles {
			titleOf[t.Account] = t.Title
		}
	}
	for i := range accs {
		accs[i].Online = s.AccountOnline(accs[i].Account)
		accs[i].Title = titleOf[accs[i].Account]
	}
	writeJSON(w, accs)
}

// handleMedals 返回全部奖牌(id/name/desc/icon),供宠物详情奖牌墙展示。
func (s *Server) handleMedals(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.medals)
}

// handleNameOptions 返回全量特长名(gamedata 全表,非按账号),供事件页高亮规则点选;
// 另附 6×6 性格方阵,供宠物列表的性格矩阵筛选直接铺格子(见 gamedata.NatureMatrix)。
func (s *Server) handleNameOptions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"speciality": s.db.AllSpecialities(),
		"nature":     s.db.NatureMatrix(),
	})
}

// handleIcons 返回全局固定图标(六维属性小图 + 异色/炫彩/污染标记图),供前端一次性缓存。
func (s *Server) handleIcons(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.icons)
}

// handleBoxes 返回各盒子的槽位布局,供宠物列表左侧盒子示意图。
func (s *Server) handleBoxes(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.store.For(s.acct(r)).BoxLayouts())
}

// handleTeams 返回大世界三队的 18 格布局,供盒子示意图。
func (s *Server) handleTeams(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.store.For(s.acct(r)).TeamLayouts())
}

// handleEvolution 返回某 petbase(base_conf_id)所属进化链(按阶段升序),供详情页展示。
func (s *Server) handleEvolution(w http.ResponseWriter, r *http.Request) {
	base, _ := strconv.ParseUint(r.URL.Query().Get("base"), 10, 32)
	chain := s.db.EvolutionChain(uint32(base))
	if chain == nil {
		chain = []gamedata.ChainStep{}
	}
	writeJSON(w, chain)
}
