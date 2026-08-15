package server

import (
	"net/http"
	"strconv"

	"github.com/whoisnian/rocom-capture/internal/pet"
	"github.com/whoisnian/rocom-capture/internal/store"
)

// handleEggs 返回当前账号的精灵蛋列表(默认只看还在背包里的)。
//
// 参数:
//
//	state=0|1|2|all  0=在背包(默认) 1=已破壳(历史) 2=已不在背包
//	hatching=1       只看正在孵蛋器里的
//	search=          按蛋名/物种名模糊
//	sort=obtained|weight|height|name  order=asc|desc
//
// 返回 {eggs:[…], counts:{inBag, hatching, hatched}}——计数供页面上的标签页显示。
func (s *Server) handleEggs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := store.EggFilter{
		State:    store.EggInBag,
		Hatching: q.Get("hatching") == "1",
		Search:   q.Get("search"),
		Sort:     q.Get("sort"),
		Order:    q.Get("order"),
	}
	switch st := q.Get("state"); st {
	case "":
	case "all":
		f.State = -1
	default:
		if n, err := strconv.Atoi(st); err == nil {
			f.State = n
		}
	}
	sc := s.store.For(s.acct(r))
	eggs, err := sc.ListEggs(f)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	inBag, hatching, hatched := sc.CountEggs()
	if eggs == nil {
		eggs = []*pet.EggView{} // 空列表输出 [],前端不必再判 null
	}
	writeJSON(w, map[string]any{
		"eggs":   eggs,
		"counts": map[string]int{"inBag": inBag, "hatching": hatching, "hatched": hatched},
	})
}
