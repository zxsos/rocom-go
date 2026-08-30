package store

import (
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

// StartPlaySession 开启一次游玩会话(幂等):同一连接已有进行中会话时不重复开。
// 登录后首条可归属消息、挂后台回前台、断线重连等场景都会走到这里;库里已有进行中
// 会话(如服务重启时玩家一直在线)时 NOT EXISTS 挡住,不会重复记。
func (s *Store) StartPlaySession(connID, account string, ts int64) error {
	_, err := s.db.Exec(`
INSERT INTO play_sessions(conn_id, account, login_time)
SELECT ?, ?, ?
WHERE NOT EXISTS (SELECT 1 FROM play_sessions WHERE conn_id = ? AND logout_time IS NULL)`,
		connID, account, ts, connID)
	return err
}

// EndPlaySession 关闭某连接的全部进行中会话,写入下线时间与时长(时长=下线-登录,秒)。
// 重复调用安全(没有进行中会话时无效果)。
func (s *Store) EndPlaySession(connID string, ts int64) error {
	_, err := s.db.Exec(`
UPDATE play_sessions
SET logout_time = ?, duration = MAX(0, ? - login_time)
WHERE conn_id = ? AND logout_time IS NULL`,
		ts, ts, connID)
	return err
}

// ForceEndStaleSessions 强制结束所有 login_time 早于 cutoff 的进行中会话(悬挂清理)。
// 覆盖极端场景:如服务重启时连接已断开且未留场景现场、关闭通知缓冲丢失等,避免管理后台
// 永远显示某玩家「在线中」。
func (s *Store) ForceEndStaleSessions(cutoff, now int64) error {
	_, err := s.db.Exec(`
UPDATE play_sessions
SET logout_time = ?, duration = MAX(0, ? - login_time)
WHERE logout_time IS NULL AND login_time < ?`,
		now, now, cutoff)
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
