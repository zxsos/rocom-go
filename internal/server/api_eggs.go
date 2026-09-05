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
	// hatchRate 已含「此刻是否在移动」(见 pet.HatchRate),前端拿它直接外推即可。
	// 之所以能实时,是因为它由两部分拼出,各自都拿得到:
	//   - 活动倍率:加速日时间表,按时刻算(离线也准,不存在冷启动);
	//   - 移动增益:玩家此刻在不在动,由移动包 0x0133 观测(翻转会经 SSE 推送)。
	//
	// 语义要说清:这个倍率对应「**若保持当前状态**」。玩家跑动时 ETA 短、停下就变长,
	// 故前端必须把状态标出来(「移动中」/「静止」),否则数字来回跳会让人困惑。
	acc := s.acct(r)
	moving := s.isHatchMoving(acc)
	writeJSON(w, map[string]any{
		"eggs":          eggs,
		"hatchRate":     pet.HatchRate(time.Now().Unix(), moving),
		"hatchMoving":   moving,
		"hatchMovingAt": s.hatchMovingAt(acc).Unix(),
	})
}

// hatchMoveTTL 是「多久没收到移动包就算停下了」(与 pipeline 的同名常量一致,见
// position.go 的 observeHatchMove)。移动包最疏是 2.5-3s 一次心跳,故要明显大于 3s。
const hatchMoveTTL = 10 * time.Second

// SetHatchMoving 记录某账号最近一次观测到「玩家在移动」的时刻;at 为零值表示明确停止。
// 由抓包管线在每次移动包(0x0133)时调用,HTTP 读取方据此判定时效。
//
// **状态翻转时广播 SSE**(事件名 hatch):不推的话,孵蛋页只在自己重拉时才更新
// 移动状态 —— 玩家跑起来了页面却还按静止算,「实时」就无从谈起。
// 只在翻转时推:移动包峰值约 8 条/秒,逐包广播既无意义也浪费。
func (s *Server) SetHatchMoving(account string, at time.Time) {
	if account == "" {
		return
	}
	s.hatchMovingMu.Lock()
	if s.hatchMoving == nil {
		s.hatchMoving = map[string]time.Time{}
	}
	prev := !s.hatchMoving[account].IsZero() &&
		time.Since(s.hatchMoving[account]) <= hatchMoveTTL
	s.hatchMoving[account] = at
	now := !at.IsZero() && time.Since(at) <= hatchMoveTTL
	changed := prev != now
	s.hatchMovingMu.Unlock()

	if changed {
		// 只带状态与时刻,倍率由前端按当前的「加速日时间表 + 是否在动」自己算:
		// 倍率会随加速日窗口起止而变,推一个算好的值过去,翻过 04:00 那一刻就成了
		// 过期数字。前端复算的口径见 web/src/pages/eggs/hatch.js 的 hatchRateNow。
		s.hub.Broadcast("hatch", account, map[string]any{
			"moving": now,
			"at":     at.Unix(), // 零值(明确停止)时为 0
		})
	}
}

// hatchMovingAt 返回最近一次观测到「在移动」的时刻;从未移动过则返回零值。
//
// 单独暴露给前端是因为移动状态**有时效**:玩家站住后,客户端未必补发 stop_move 包,
// 后端只能靠 TTL 自然过期 —— 而过期那一刻没有事件可推(没人发包)。前端据此每秒
// 自己判断,才能及时翻回「静止」,不必等下一次请求。
func (s *Server) hatchMovingAt(account string) time.Time {
	s.hatchMovingMu.Lock()
	defer s.hatchMovingMu.Unlock()
	return s.hatchMoving[account]
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
