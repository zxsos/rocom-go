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
	writeJSON(w, p)
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
	events, err := s.store.For(s.acct(r)).ListEvents(limit, beforeID)
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

// handleAccounts 返回已知账号列表(account/name/petCount),供前端账号切换下拉。
func (s *Server) handleAccounts(w http.ResponseWriter, r *http.Request) {
	accs, err := s.store.ListAccounts()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if accs == nil {
		accs = []store.AccountInfo{}
	}
	writeJSON(w, accs)
}

// handleMedals 返回全部奖牌(id/name/desc/icon),供宠物详情奖牌墙展示。
func (s *Server) handleMedals(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.medals)
}

// handleNameOptions 返回全量特长名(gamedata 全表,非按账号),供事件页高亮规则点选。
func (s *Server) handleNameOptions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string][]string{"speciality": s.db.AllSpecialities()})
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
