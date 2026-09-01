package server

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/whoisnian/rocom-capture/internal/gamedata"
	"github.com/whoisnian/rocom-capture/internal/store"
)

// 草系徽章试炼接口(见 internal/trial 与 docs/pcap-20260831-grass-trial.md)。
//
// 与 wildpets / home / flowers 同一路数:管线把试炼状态推给 server 缓存,页面加载时
// 经本接口即时回显,之后由 SSE 的 trial 消息实时覆盖。

// SetLastTrial 缓存某账号最近一次试炼状态(由消费管线在广播 trial 时调用)。
func (s *Server) SetLastTrial(account string, payload *TrialPayload) {
	if account == "" {
		return
	}
	s.snap.setTrial(account, payload)
}

// handleTrial 返回当前账号最近一次草系试炼状态;无记录返回 null。
func (s *Server) handleTrial(w http.ResponseWriter, r *http.Request) {
	v := s.snap.getTrial(s.acct(r))
	writeJSON(w, v)
}

// handleTrialEncounters 返回试炼的「遇见记录」:三章各一张精灵图,遇到过的置灰。
//
// 与 handleTrial(实时状态)不同,这份是**累积的历史**,直接读库而非内存快照。
// 精灵池来自静态配置(gamedata/trial.go,数据源是 wiki),遇见情况来自
// trial_encounter 表(管线解析 0x1316 时写入)。
func (s *Server) handleTrialEncounters(w http.ResponseWriter, r *http.Request) {
	acc := s.acct(r)
	out := &TrialEncountersPayload{Account: acc, Ts: time.Now().Unix()}

	// 三章;静态配置缺某章时那张图的池为空,但结构仍返回(前端好统一渲染)
	for ch := uint32(1); ch <= 3; ch++ {
		book := TrialEncounterBook{
			Chapter: ch,
			Name:    s.db.TrialChapterName(ch),
			// 保底给空切片而非 nil:JSON 里是 [] 不是 null,前端可以直接 .length。
			// 否则某章 pools 缺失、而 bosses 仍在时 Total>0 会进列表,
			// 前端拿到的 null 会让整页报错白屏。
			Normal: []TrialEncounterPet{},
		}
		// 本章的遇见记录(每章独立,见 TrialEncountersPayload 的说明)
		seen := s.store.TrialEncounters(acc, ch)
		// 普通池:第 1/2/3/5 层可能遇到的
		for _, base := range s.db.TrialPool(ch) {
			book.Normal = append(book.Normal, s.encounterPet(base, seen))
		}
		// 首领:第 4 层的 22 人名单,三章共用
		for _, base := range s.db.TrialBosses() {
			book.Boss = append(book.Boss, s.encounterPet(base, seen))
		}
		n := 0
		for _, p := range book.Normal {
			if p.Seen {
				n++
			}
		}
		for _, p := range book.Boss {
			if p.Seen {
				n++
			}
		}
		// 见过但**不在本章池里**的:主要是 NPC 战(kind=2)与最终 BOSS(kind=3)。
		//
		// 静态配置只有「普通池」(第 1/2/3/5 层)与「22 名首领」(第 4 层),**没有第 7 层的
		// NPC 阵容和最终 BOSS 的精灵池** —— 实测回放就撞上了:3027(NPC 战)与 5061
		// (最终 BOSS)都真实打过照面,却不在 pools/bosses 里,按上面的逻辑会无声丢失。
		// 静默丢弃比不记录更糟:用户明明遇到过,图上却永远显示未遇见。
		// 故单列一组,不混进上面的图 —— 那两组的进度口径是「池子里还剩多少」,
		// 塞进来源不明的条目会让分母失去意义。
		for base := range seen {
			if inBook(base, book.Normal) || inBook(base, book.Boss) {
				continue
			}
			book.Extra = append(book.Extra, s.encounterPet(base, seen))
		}
		sort.Slice(book.Extra, func(i, j int) bool {
			if book.Extra[i].Time != nil && book.Extra[j].Time != nil {
				return *book.Extra[i].Time < *book.Extra[j].Time
			}
			return book.Extra[i].Base < book.Extra[j].Base
		})
		book.Total = uint32(len(book.Normal) + len(book.Boss))
		book.Seen = uint32(n)
		if book.Total > 0 {
			out.Chapters = append(out.Chapters, book)
		}
	}
	out.Updated = trialDataUpdated()
	writeJSON(w, out)
}

// inBook 判断某 petbase 是否已在某组里(O(n²) 但 n 最大 337,且只在组接口时跑一次)。
// 用 map 更快,但避免为此多维护一份临时结构 —— 这个接口的量级完全撑得住。
func inBook(base uint32, list []TrialEncounterPet) bool {
	for _, p := range list {
		if p.Base == base {
			return true
		}
	}
	return false
}

// encounterPet 组装图里的一只精灵:名字、头像、本章是否遇到过。
func (s *Server) encounterPet(base uint32, seen map[uint32]store.TrialEncounter) TrialEncounterPet {
	p := TrialEncounterPet{Base: base}
	if info, ok := s.db.PetBase(base); ok {
		p.Name = info.Name
		if info.Form != "" {
			p.Name += "_" + info.Form
		}
		if im := s.db.PetImageByBase(base, false); im.Head != "" {
			p.Img = im.Head
		}
	}
	if e, ok := seen[base]; ok {
		p.Seen = true
		kind, first := e.Kind, e.FirstSeen
		p.Kind, p.Time = &kind, &first
	}
	return p
}

// trialDataUpdated 返回静态配置的更新时间,让前端能提示「数据可能已过期」。
// 取不到就不填 —— 这是个锦上添花的提示,不该因为它让接口失败。
func trialDataUpdated() string {
	var raw struct {
		Updated string `json:"_updated"`
	}
	if err := json.Unmarshal(gamedata.TrialJSON(), &raw); err != nil {
		return ""
	}
	return raw.Updated
}

// handleDeleteTrialEncounters 清空遇见记录。?chapter=1|2|3 只清某一章,不给则全清。
//
// 存在的理由:精灵池是**第三方 wiki 的静态配置**,游戏换版本换池子后旧记录就对不上了
// —— 里头会有当前版本根本遇不到的 petbase,进度永远停在某个不满的数。此时需要手动清。
// 见 store.ClearTrialEncounters 的注释。
//
// 注意这不是「重置本局」:本局状态走 SSE,与此无关。清的是**累积的历史**,不可恢复。
func (s *Server) handleDeleteTrialEncounters(w http.ResponseWriter, r *http.Request) {
	acc := s.acct(r)
	var ch uint32
	if v := r.URL.Query().Get("chapter"); v != "" {
		n, err := strconv.ParseUint(v, 10, 32)
		if err != nil || n < 1 || n > 3 {
			http.Error(w, "chapter 须为 1/2/3", http.StatusBadRequest)
			return
		}
		ch = uint32(n)
	}
	if err := s.store.ClearTrialEncounters(acc, ch); err != nil {
		http.Error(w, "清空失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "chapter": ch})
}
