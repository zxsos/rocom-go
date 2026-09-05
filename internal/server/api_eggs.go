package server

import (
	"net/http"
	"time"

	"github.com/whoisnian/rocom-capture/internal/pet"
	"github.com/whoisnian/rocom-capture/internal/store"
)

// handleEggs 返回当前账号背包里的精灵蛋(库里存的就是背包现状,破壳/送人的行已删)。
//
// 参数:
//
//	search=          按蛋名/物种名模糊
//	sort=quality|obtained  order=asc|desc(复刻游戏内背包的两种排序,见 docs/data.md 3.6)
//
// 页面不分标签页:一次取回全部。在孵的蛋留在最前且不参与背包排序(它们属于孵蛋器),
// 但自己按槽位序排一遍(入孵时刻升序,与背包次序无关,见 pet.SortHatchingEggs)。
// **排序只作用于仓库那部分**:客户端也是先把在孵的蛋摘掉(IsRemoveEggItem)再 table.sort,
// 喂给排序的列表不同,同键蛋的落位就不同。
// 前端还会按孵化进度把标记蛋再分成「在孵(未满)/ 已孵化(满)」两段(残留标记蛋进度恒满,
// 见 web/src/pages/eggs/EggList.jsx)——进度是前端外推的,后端无法判断,故这里只给 hatching 标志。
func (s *Server) handleEggs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	sc := s.store.For(s.acct(r))
	eggs, err := sc.ListEggs(store.EggFilter{Search: q.Get("search")})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for i, e := range eggs {
		// 按当前名称库重算(旧行可能是本工具还没有异色标记/排序键时写的),
		// 顺带补上要等双亲快照才算得出的推测嗓音与奖牌。
		eggs[i] = pet.RefreshEggView(e, s.db)
	}
	// 排序键来自上面的重算,故排在其后。在孵的蛋留在最前且不参与背包排序(它们属于孵蛋器),
	// 但自己按槽位顺序排一遍——槽位是入孵时刻升序,与背包次序无关(见 pet.SortHatchingEggs)。
	hatching, bag := []*pet.EggView{}, []*pet.EggView{}
	for _, e := range eggs {
		if e.Hatching {
			hatching = append(hatching, e)
		} else {
			bag = append(bag, e)
		}
	}
	pet.SortHatchingEggs(hatching)
	pet.SortEggs(bag, q.Get("sort"), q.Get("order") == "asc")
	eggs = append(hatching, bag...)
	// 孵化倍率**整个响应一份**而非逐蛋:倍率是全局的,不逐蛋(实测三颗不同 maxSecs
	// 的蛋同秒各 +10s,统一 5.00)。
	//
	// hatchRate 是**外推用的**倍率 = 活动倍率(固定时间表,见 pet.HatchActivityRate)。
	// 它不靠测量:加速日是每周五 04:00~周一 04:00 的固定活动,服务器在窗口内照此推进,
	// **玩家离线时也一样** —— 故按时间表算,离线外推天然准确,不必攒样本、不存在冷启动。
	//
	// hatchMoving 单列:玩家移动/挂风场会在活动倍率之上再加,但那是**在线行为加成**,
	// 随速度与移动方式而变(实测静止 5.00、移动 16.9~25.8),样本不足以定出一个可信的
	// 数。故它**不进** hatchRate(拿它外推会虚报「可破壳」),只作定性提示:此刻确实
	// 在动,进度会比标注的更快。
	writeJSON(w, map[string]any{
		"eggs":        eggs,
		"hatchRate":   pet.HatchActivityRate(time.Now().Unix()),
		"hatchMoving": s.isHatchMoving(s.acct(r)),
	})
}

// hatchMoveTTL 是「多久没收到移动包就算停下了」(与 pipeline 的同名常量一致,见
// position.go 的 observeHatchMove)。移动包最疏是 2.5-3s 一次心跳,故要明显大于 3s。
const hatchMoveTTL = 10 * time.Second

// SetHatchMoving 记录某账号最近一次观测到「玩家在移动」的时刻;at 为零值表示明确停止。
// 由抓包管线在每次移动包(0x0133)时调用,HTTP 读取方据此判定时效。
func (s *Server) SetHatchMoving(account string, at time.Time) {
	if account == "" {
		return
	}
	s.hatchMovingMu.Lock()
	if s.hatchMoving == nil {
		s.hatchMoving = map[string]time.Time{}
	}
	s.hatchMoving[account] = at
	s.hatchMovingMu.Unlock()
}

// isHatchMoving 报告某账号此刻是否算「在移动」:最近一次移动观测还在 TTL 内。
//
// 超时即否,覆盖两种不再推送的情况:客户端上报了 stop_move(会记零值,当即翻回)、
// 以及切后台/断线(移动包直接停发,只剩 TTL 兜底)。
//
// ⚠️ **离线回放时恒为 false,这是预期而非缺陷**:回放的是历史录像,包内时刻与
// time.Now() 差好几个小时(实测 5373 秒),必然超时。而语义上也没错 —— 后端此刻
// 并没有实时流,无从得知玩家现在在不在动,「不在移动」是保守的默认。实时抓包时
// 包内时刻 ≈ 当前时刻,判定正常。翻转逻辑本身由单元测试覆盖(见 api_eggs_test.go),
// 回放覆盖不到它。
// IsHatchMoving 是 isHatchMoving 的对外版本,供管线测试断言(跨包)。
// 生产路径用小写那个。
func (s *Server) IsHatchMoving(account string) bool {
	return s.isHatchMoving(account)
}

func (s *Server) isHatchMoving(account string) bool {
	s.hatchMovingMu.Lock()
	at := s.hatchMoving[account]
	s.hatchMovingMu.Unlock()
	if at.IsZero() {
		return false
	}
	return time.Since(at) <= hatchMoveTTL
}
