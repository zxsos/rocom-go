package server

import (
	"encoding/json"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/whoisnian/rocom-capture/internal/store"
)

// todayCST 返回今天(北京时间,UTC+8)的 yyyy-mm-dd。
// 排行榜结算与称号归属以此为日界(无夏令时,固定 +8 最稳妥)。
func todayCST() string {
	return time.Now().In(time.FixedZone("CST", 8*3600)).Format("2006-01-02")
}

// startRankSettlement 每日 00:05(北京时间)结算排行榜称号(评选前一日的大富翁/
// 赚钱王/败家子,当天佩戴一天)。启动时先补结算一次——若服务在结算点后启动,
// 当天的称号尚未评选,补算出来(幂等,重复结算结果一致)。
func (s *Server) startRankSettlement() {
	settle := func(now time.Time) {
		today := now.In(time.FixedZone("CST", 8*3600)).Format("2006-01-02")
		if err := s.store.SettleRankTitles(today); err != nil {
			log.Printf("rank settle %s: %v", today, err)
		} else {
			log.Printf("rank settle %s: done", today)
		}
	}
	settle(time.Now())
	loc := time.FixedZone("CST", 8*3600)
	for {
		now := time.Now().In(loc)
		next := time.Date(now.Year(), now.Month(), now.Day(), 0, 5, 0, 0, loc)
		if !now.Before(next) {
			next = next.AddDate(0, 0, 1) // 已过 00:05,等明天
		}
		time.Sleep(time.Until(next))
		settle(time.Now())
	}
}

// handleLeaderboard 返回排行榜数据:
//   - forbes:按洛克贝降序(未同步过洛克贝的参加者沉底,前端显示「待同步」)
//   - profit:按盈亏降序(当日盈亏 = 当前洛克贝 - 今日起点,见 dayStartBaselineSQL)
//   - titles:今天(佩戴日)已评出的称号获奖名单(每晚 00:05 结算)
//   - me:当前账号的参与状态(含 join 开关与今日称号),方便前端高亮/提示参加
func (s *Server) handleLeaderboard(w http.ResponseWriter, r *http.Request) {
	entries, err := s.store.Leaderboard()
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	forbes := append([]store.RankEntry(nil), entries...)
	sort.SliceStable(forbes, func(i, j int) bool {
		if forbes[i].HasCoins != forbes[j].HasCoins {
			return forbes[i].HasCoins // 有洛克贝的排前
		}
		return forbes[i].Coins > forbes[j].Coins
	})
	profit := append([]store.RankEntry(nil), entries...)
	sort.SliceStable(profit, func(i, j int) bool {
		if profit[i].HasCoins != profit[j].HasCoins {
			return profit[i].HasCoins
		}
		return profit[i].Profit > profit[j].Profit
	})
	// 合并当日称号
	titleOf := map[string]string{}
	titles, err := s.store.RankTitles(todayCST())
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	for _, t := range titles {
		titleOf[t.Account] = t.Title
	}
	for i := range forbes {
		forbes[i].Title = titleOf[forbes[i].Account]
	}
	for i := range profit {
		profit[i].Title = titleOf[profit[i].Account]
	}
	me := s.store.AccountRank(s.acct(r))
	me.Title = titleOf[me.Account]
	writeJSON(w, map[string]any{
		"forbes": forbes,
		"profit": profit,
		"titles": titles,
		"me":     me,
	})
}

// handleAccountRank 设置当前账号是否参加排行榜(join=true 参加,false 退出)。
func (s *Server) handleAccountRank(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Account string `json:"account"`
		Join    bool   `json:"join"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	req.Account = strings.TrimSpace(req.Account)
	if req.Account == "" {
		http.Error(w, "account required", http.StatusBadRequest)
		return
	}
	if err := s.store.SetAccountRankJoin(req.Account, req.Join); err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "join": req.Join})
}
