package store

import (
	"encoding/json"
	"time"
)

// AdminMemberStats 是单个成员(账号)的抓捕统计(管理面板图表)。
// Daily 与 AdminStats.Days 时间轴对齐,升序,含 0。
type AdminMemberStats struct {
	Account  string `json:"account"`
	Name     string `json:"name"`
	Total    int    `json:"total"`
	Shiny    int    `json:"shiny"`
	Colorful int    `json:"colorful"`
	Daily    []int  `json:"daily"`
}

// AdminStats 是所有成员抓捕情况的跨账号聚合(管理面板图表)。
type AdminStats struct {
	Members []AdminMemberStats `json:"members"`
	Days    []string           `json:"days"`  // 近30天时间轴,升序 "MM-DD"
	Daily   []int              `json:"daily"` // 每天全成员合计,与 Days 对齐
}

// AdminStats 统计全部账号的抓捕情况。events 数据量小,一次扫表在内存聚合,避免复杂 SQL。
func (s *Store) AdminStats() (*AdminStats, error) {
	rows, err := s.rdb.Query(`
SELECT e.account, COALESCE(a.name,''), e.time, e.shiny, e.data
FROM events e LEFT JOIN accounts a ON a.account = e.account`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	const days = 30

	order := []string{}
	names := map[string]string{}
	dailyAll := make([]int, days)            // 全成员每日合计
	dailyByAcc := map[string][]int{}         // 各账号每日
	shiny := map[string]int{}
	colorful := map[string]int{}
	total := map[string]int{}

	for rows.Next() {
		var acct, name string
		var t int64
		var sh int
		var data string
		if err := rows.Scan(&acct, &name, &t, &sh, &data); err != nil {
			return nil, err
		}
		if _, ok := dailyByAcc[acct]; !ok {
			order = append(order, acct)
			dailyByAcc[acct] = make([]int, days)
		}
		if name != "" {
			names[acct] = name
		}
		total[acct]++
		if sh > 0 {
			shiny[acct]++
		}
		var m struct {
			Colorful bool `json:"colorful"`
		}
		if json.Unmarshal([]byte(data), &m) == nil && m.Colorful {
			colorful[acct]++
		}
		d := time.Unix(t, 0)
		day := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, d.Location())
		if idx := int(today.Sub(day).Hours() / 24); idx >= 0 && idx < days {
			dailyAll[idx]++
			dailyByAcc[acct][idx]++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	st := &AdminStats{Days: make([]string, days), Daily: dailyAll}
	for i := 0; i < days; i++ {
		st.Days[i] = today.AddDate(0, 0, i-days+1).Format("01-02")
	}
	st.Members = make([]AdminMemberStats, 0, len(order))
	for _, acct := range order {
		st.Members = append(st.Members, AdminMemberStats{
			Account:  acct,
			Name:     names[acct],
			Total:    total[acct],
			Shiny:    shiny[acct],
			Colorful: colorful[acct],
			Daily:    dailyByAcc[acct],
		})
	}
	return st, nil
}
