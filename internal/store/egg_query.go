package store

import (
	"log"
	"time"
)

// 查蛋 API(第三方图鉴)使用统计。查蛋一次烧一次第三方 token,管理面板据此
// 看「今天消耗多少额度 / 成功率 / 谁在查」。表结构见 store.go 的 egg_queries。

// EggQueryStats 是管理面板「查蛋 API 统计」的聚合结果。
type EggQueryStats struct {
	KeySet      bool         `json:"keySet"`      // 服务端是否配置 -egg-api-key(未配置则统计恒为 0)
	Total       int          `json:"total"`       // 累计查询次数(发起过第三方请求的)
	TodayTotal  int          `json:"todayTotal"`  // 今日查询次数
	TodayOK     int          `json:"todayOK"`     // 今日成功(拿到第三方正常响应)
	TodayFail   int          `json:"todayFail"`   // 今日失败(网络/HTTP/解析失败)
	SuccessRate float64      `json:"successRate"` // 累计成功率(0-100,保留 1 位小数)
	Daily       []EggDaily   `json:"daily"`       // 近 14 天每日次数
	ByAccount   []EggByAcct  `json:"byAccount"`   // 按账号排行(次数倒序)
	Recent      []EggRecent  `json:"recent"`      // 最近 10 条明细
}

type EggDaily struct {
	Day   string `json:"day"`   // 格式 01-02
	Total int    `json:"total"` // 当日查询次数
	OK    int    `json:"ok"`    // 当日成功次数
}

type EggByAcct struct {
	Account string `json:"account"`
	Name    string `json:"name"`
	Total   int    `json:"total"`
	Today   int    `json:"today"`
}

type EggRecent struct {
	Account string `json:"account"`
	Name    string `json:"name"`
	Time    int64  `json:"time"`
	OK      bool   `json:"ok"`
	CostMS  int    `json:"costMs"`
	Matches int    `json:"matches"`
	Height  string `json:"height"`
	Weight  string `json:"weight"`
}

// LogEggQuery 记录一次查蛋请求。account 为空时记空串(未登录也记,方便看总量)。
// 只在「已发起第三方请求」时调用:未配置 key 或请求还没发出去(构造失败)不记。
func (s *Store) LogEggQuery(account string, ok bool, costMS, matches int, height, weight string) {
	_, err := s.db.Exec(`INSERT INTO egg_queries(account, ts, ok, cost_ms, matches, height, weight) VALUES(?,?,?,?,?,?,?)`,
		account, time.Now().Unix(), b2i(ok), costMS, matches, height, weight)
	if err != nil {
		log.Printf("记录查蛋统计失败: %v", err)
	}
}

// EggQueryStats 聚合查蛋统计。查蛋量小(一天至多几十次),直接全量读近 14 天在 Go 里分组,
// 省去 SQLite 日期/时区函数的跨平台坑(项目里其它按天聚合同此模式,见 event.go)。
func (s *Store) EggQueryStats() (*EggQueryStats, error) {
	st := &EggQueryStats{Daily: make([]EggDaily, 0, 14)}

	// 累计与今日
	dayStart := dayStartUnix(time.Now())
	if err := s.rdb.QueryRow(`SELECT COUNT(*) FROM egg_queries`).Scan(&st.Total); err != nil {
		return nil, err
	}
	if err := s.rdb.QueryRow(`SELECT COUNT(*), COALESCE(SUM(ok),0) FROM egg_queries WHERE ts>=?`, dayStart).Scan(&st.TodayTotal, &st.TodayOK); err != nil {
		return nil, err
	}
	st.TodayFail = st.TodayTotal - st.TodayOK
	if st.Total > 0 {
		var okAll int
		if err := s.rdb.QueryRow(`SELECT COUNT(*) FROM egg_queries WHERE ok=1`).Scan(&okAll); err != nil {
			return nil, err
		}
		st.SuccessRate = float64(int(float64(okAll)/float64(st.Total)*1000+0.5)) / 10
	}

	// 近 14 天每日:构造 0-13 的日期 key,一次扫描归组。
	from := dayStart - 14*24*3600
	rows, err := s.rdb.Query(`SELECT ts, ok FROM egg_queries WHERE ts>=?`, from)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	dayIdx := make(map[string]int, 14)
	now := time.Now()
	for i := 13; i >= 0; i-- {
		day := now.AddDate(0, 0, -i).Format("01-02")
		dayIdx[day] = len(st.Daily)
		st.Daily = append(st.Daily, EggDaily{Day: day})
	}
	for rows.Next() {
		var ts int64
		var ok int
		if err := rows.Scan(&ts, &ok); err != nil {
			return nil, err
		}
		d := time.Unix(ts, 0).Format("01-02")
		if i, hit := dayIdx[d]; hit {
			st.Daily[i].Total++
			st.Daily[i].OK += ok
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 按账号排行(联 accounts 取名字)。
	if st.ByAccount, err = s.eggByAccount(dayStart); err != nil {
		return nil, err
	}
	if st.Recent, err = s.eggRecent(); err != nil {
		return nil, err
	}
	return st, nil
}

func (s *Store) eggByAccount(dayStart int64) ([]EggByAcct, error) {
	rows, err := s.rdb.Query(`
SELECT q.account, COALESCE(a.name,'') AS name, COUNT(*) AS total,
       COALESCE(SUM(CASE WHEN q.ts>=? THEN 1 ELSE 0 END),0) AS today
FROM egg_queries q LEFT JOIN accounts a ON a.account=q.account
GROUP BY q.account ORDER BY total DESC`, dayStart)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []EggByAcct{}
	for rows.Next() {
		var e EggByAcct
		if err := rows.Scan(&e.Account, &e.Name, &e.Total, &e.Today); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) eggRecent() ([]EggRecent, error) {
	rows, err := s.rdb.Query(`
SELECT q.account, COALESCE(a.name,'') AS name, q.ts AS time, q.ok AS ok,
       q.cost_ms AS cost_ms, q.matches AS matches, q.height AS height, q.weight AS weight
FROM egg_queries q LEFT JOIN accounts a ON a.account=q.account
ORDER BY q.id DESC LIMIT 10`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []EggRecent{}
	for rows.Next() {
		var e EggRecent
		var ok int
		if err := rows.Scan(&e.Account, &e.Name, &e.Time, &ok, &e.CostMS, &e.Matches, &e.Height, &e.Weight); err != nil {
			return nil, err
		}
		e.OK = ok == 1
		out = append(out, e)
	}
	return out, rows.Err()
}

// dayStartUnix 返回当天 0 点(本地时区)的 Unix 秒。
func dayStartUnix(t time.Time) int64 {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location()).Unix()
}
