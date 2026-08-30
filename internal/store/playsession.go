package store

import (
	"strings"
	"time"
)

// PlaySession 是一次游玩会话(玩家上线 → 下线的完整时间段),管理后台「游玩记录」展示用。
// LogoutTime 为 nil 表示会话进行中(玩家在线);Duration 是下线时写入的游玩时长(秒),
// 进行中的会话按「现在-登录时刻」由汇总侧折算,明细里以 Online 区分。
//
// ID 是 play_sessions 的自增主键,前端列表的 React key **必须用它**:
// 早期用 account+loginTime 拼 key,而同一账号同一秒内可能有多条会话(实测存在),
// key 撞车会让翻页时旧行不被回收 —— 表现为第 2 页混进第 1 页的残留行。
type PlaySession struct {
	ID         int64  `json:"id"`
	Account    string `json:"account"`
	Name       string `json:"name"`
	LoginTime  int64  `json:"loginTime"`
	LogoutTime *int64 `json:"logoutTime"`
	Duration   int64  `json:"duration"`
	Online     bool   `json:"online"`
}

// PlayDaily 是某天的游玩聚合(管理后台「每日游玩」图),按登录日归属。
type PlayDaily struct {
	Day      string `json:"day"`
	Sessions int    `json:"sessions"`
	Duration int64  `json:"duration"`
}

// PlaySummary 是游玩记录汇总(管理后台卡片):当前在线数、今日会话数/时长、近 14 天每日聚合。
type PlaySummary struct {
	Online        int         `json:"online"`
	TodaySessions int         `json:"todaySessions"`
	TodayDuration int64       `json:"todayDuration"`
	Daily         []PlayDaily `json:"daily"`
}

// StartPlaySession 开启一次游玩会话(幂等,**按账号**):该账号已有进行中会话时不重复开。
//
// 为什么按账号而不是按连接:一个玩家常同时保持**多条 TCP 连接**(实测同一账号有
// 2~3 条),且重连会换新端口(= 新 conn_id)。按连接记会把一次在线拆成好几条
// 时间重叠的会话 —— 管理后台里就是「好几条并行的在线中」+ 一堆几秒的碎片,
// 而「今日游玩时长」把并行的几条都累加进去,直接翻倍。
// conn_id 仍落库,记的是**开这条会话的那条连接**,仅用于溯源;会话存续期间该连接
// 断开不代表下线(见 pipeline 的 settleSessions)。
func (s *Store) StartPlaySession(connID, account string, ts int64) error {
	_, err := s.db.Exec(`
INSERT INTO play_sessions(conn_id, account, login_time)
SELECT ?, ?, ?
WHERE NOT EXISTS (SELECT 1 FROM play_sessions WHERE account = ? AND logout_time IS NULL)`,
		connID, account, ts, account)
	return err
}

// EndAccountSessions 结束某账号的全部进行中会话,写入下线时间与时长(时长=下线-登录,秒)。
// 没有进行中会话时无效果。用于玩家换号(旧账号立即下线)。
func (s *Store) EndAccountSessions(account string, ts int64) error {
	if account == "" {
		return nil
	}
	_, err := s.db.Exec(`
UPDATE play_sessions
SET logout_time = ?, duration = MAX(0, ? - login_time)
WHERE account = ? AND logout_time IS NULL`,
		ts, ts, account)
	return err
}

// EndStalePlaySessions 结束「账号已不再活跃」的进行中会话:active 之外的账号一律记下线。
//
// active 由调用方(pipeline)按内存里的连接状态给出:只要该账号还有**任意一条**连接
// 在近期有消息,就算活跃 —— 玩家同时开多条连接,关掉其中一条不等于下线,这正是
// 逐连接判定时产生碎片会话的原因。
//
// 它同时覆盖了原先 ForceEndStaleSessions 想兜住的场景(服务重启后内存连接表清空,
// 库里的会话无人认领),而且**及时**:下一轮 sweep 就清掉,不必像按 login_time 判
// 定时那样挂 24 小时才被回收、还被记成 24 小时时长。
// active 为空表示当前无人在线,此时结束全部进行中会话。
func (s *Store) EndStalePlaySessions(active []string, ts int64) error {
	q := `UPDATE play_sessions
SET logout_time = ?, duration = MAX(0, ? - login_time)
WHERE logout_time IS NULL`
	args := []any{ts, ts}
	if len(active) > 0 {
		q += ` AND account NOT IN (?` + strings.Repeat(",?", len(active)-1) + `)`
		for _, a := range active {
			args = append(args, a)
		}
	}
	_, err := s.db.Exec(q, args...)
	return err
}

// ListPlaySessions 列出一页游玩会话记录(管理后台),按上线时间倒序,可选账号过滤。
// limit/offset 供管理后台分页:offset 是跳过的条数,limit 是本页取几条(由 handler 校验范围)。
func (s *Store) ListPlaySessions(account string, limit, offset int) ([]PlaySession, error) {
	q := `SELECT ps.id, ps.account, COALESCE(a.name, ''), ps.login_time, ps.logout_time, COALESCE(ps.duration, 0)
FROM play_sessions ps LEFT JOIN accounts a ON a.account = ps.account`
	var args []any
	if account != "" {
		q += ` WHERE ps.account = ?`
		args = append(args, account)
	}
	q += ` ORDER BY ps.login_time DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)
	rows, err := s.rdb.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PlaySession{}
	for rows.Next() {
		var ps PlaySession
		if err := rows.Scan(&ps.ID, &ps.Account, &ps.Name, &ps.LoginTime, &ps.LogoutTime, &ps.Duration); err != nil {
			return nil, err
		}
		ps.Online = ps.LogoutTime == nil
		out = append(out, ps)
	}
	return out, rows.Err()
}

// CountPlaySessions 返回满足筛选的会话总条数(管理后台分页算总页数用)。
// 与 ListPlaySessions 共用同一套筛选口径,否则会出现「最后一页翻到空」这类错位。
func (s *Store) CountPlaySessions(account string) (int, error) {
	q := `SELECT COUNT(*) FROM play_sessions`
	var args []any
	if account != "" {
		q += ` WHERE account = ?`
		args = append(args, account)
	}
	var n int
	if err := s.rdb.QueryRow(q, args...).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// PlaySessionSummary 返回游玩记录汇总:当前在线数、今日会话数/时长、近 14 天每日聚合。
// 时长口径:进行中的会话按「查询时刻-登录时刻」折算,已结束的用写入的 duration;
// 按登录日归属(跨天会话算在登录那天)。
func (s *Store) PlaySessionSummary() (PlaySummary, error) {
	var sum PlaySummary
	now := time.Now()
	nowTS := now.Unix()
	// 当前在线:进行中的会话数。
	if err := s.rdb.QueryRow(`SELECT COUNT(*) FROM play_sessions WHERE logout_time IS NULL`).
		Scan(&sum.Online); err != nil {
		return sum, err
	}
	// 今日(本地时区 0 点起)登录的会话:进行中的按现在-登录折算。
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Unix()
	if err := s.rdb.QueryRow(`
SELECT COUNT(*), COALESCE(SUM(CASE WHEN logout_time IS NULL THEN ? - login_time ELSE duration END), 0)
FROM play_sessions WHERE login_time >= ?`, nowTS, dayStart).
		Scan(&sum.TodaySessions, &sum.TodayDuration); err != nil {
		return sum, err
	}
	// 近 14 天每日聚合(按登录日归属),SQLite 侧按日期分组,Go 侧补零。
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -13).Unix()
	rows, err := s.rdb.Query(`
SELECT date(login_time, 'unixepoch', 'localtime') AS d,
       COUNT(*), COALESCE(SUM(CASE WHEN logout_time IS NULL THEN ? - login_time ELSE duration END), 0)
FROM play_sessions WHERE login_time >= ?
GROUP BY d`, nowTS, start)
	if err != nil {
		return sum, err
	}
	byDay := map[string]PlayDaily{}
	for rows.Next() {
		var d string
		var pd PlayDaily
		if err := rows.Scan(&d, &pd.Sessions, &pd.Duration); err != nil {
			rows.Close()
			return sum, err
		}
		pd.Day = d
		byDay[d] = pd
	}
	rows.Close()
	for i := 13; i >= 0; i-- {
		d := now.AddDate(0, 0, -i).Format("2006-01-02")
		if pd, ok := byDay[d]; ok {
			sum.Daily = append(sum.Daily, pd)
		} else {
			sum.Daily = append(sum.Daily, PlayDaily{Day: d})
		}
	}
	return sum, rows.Err()
}
