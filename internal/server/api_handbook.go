package server

import (
	"net/http"
	"sort"

	"github.com/whoisnian/rocom-capture/internal/gamedata"
)

// glassBookItem 是炫彩图鉴页按品种聚合的一条记录:该品种抓到过的全部炫彩变体
// (普通/隐藏各一个列表,value 已去重并保序)。
type glassBookItem struct {
	Base   uint32  `json:"base"` // 品种 pet_base_id
	Name   string  `json:"name"` // 品种名(petbase,未知时为空)
	Book   uint32  `json:"book"` // 图鉴编号(排序用)
	Head   string  `json:"head"` // 小头像 /img/<此路径>(未知时为空)
	Common []int32 `json:"common"` // 普通炫彩 glass_value 列表(空=无)
	Hidden []int32 `json:"hidden"` // 隐藏炫彩 glass_value 列表(空=无)
}

// handleHandbookGlasses 返回本账号图鉴炫彩收集(按品种聚合,图鉴号升序)。
// 数据来自登录包 PlayerPetInfo.pet_handbook(见 pet.ParseHandbookGlasses)。
func (s *Server) handleHandbookGlasses(w http.ResponseWriter, r *http.Request) {
	records, err := s.store.For(s.acct(r)).ListHandbookGlasses()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	items := map[uint32]*glassBookItem{}
	for _, rec := range records {
		it := items[rec.PetBaseID]
		if it == nil {
			it = &glassBookItem{Base: rec.PetBaseID}
			if b, ok := s.db.PetBase(rec.PetBaseID); ok {
				it.Name = b.Name
				it.Book = b.Book
			}
			it.Head = s.db.PetImageByBase(rec.PetBaseID, false).Head
			items[rec.PetBaseID] = it
		}
		switch rec.GlassType {
		case gamedata.GlassCommon:
			it.Common = append(it.Common, rec.GlassValue)
		case gamedata.GlassHidden:
			it.Hidden = append(it.Hidden, rec.GlassValue)
		}
	}
	out := make([]*glassBookItem, 0, len(items))
	for _, it := range items {
		it.Common = uniqInt32(it.Common)
		it.Hidden = uniqInt32(it.Hidden)
		out = append(out, it)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Book < out[j].Book })
	writeJSON(w, map[string]any{"glasses": out})
}

// uniqInt32 去重保序(登录包同一品种的相同炫彩变体可能跨 record 重复出现)。
func uniqInt32(in []int32) []int32 {
	if len(in) < 2 {
		return in
	}
	seen := make(map[int32]bool, len(in))
	out := in[:0]
	for _, v := range in {
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}
