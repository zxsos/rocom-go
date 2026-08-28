package store

import (
	"sort"
	"time"
)

// AccountInfo 是一个账号的概要(供前端账号下拉)。
// Online 由 server 层判定后合并(最近 onlineWindow 秒内有流量),store 不持久化。
// HasPin 由 store 层填(pin_hash 非空即 true),前端据此显示锁标。
// Coins 是最近一次登录回包解析到的金币数;HasCoins=true 表示已解析过
// (coins 为 0 也是真实值),false 表示从未解析(未知,待玩家重新登录游戏同步)。
type AccountInfo struct {
	Account  string `json:"account"`
	Name     string `json:"name"`
	PetCount int    `json:"petCount"`
	Online   bool   `json:"online"`
	HasPin   bool   `json:"hasPin"`
	Coins    int64  `json:"coins"`
	HasCoins bool   `json:"hasCoins"`
	// Join 表示该账号是否参加排行榜(福布斯/盈亏),默认参加,可在金币旁一键退出。
	Join bool `json:"join"`
	// Title 是该账号今天佩戴的排行榜称号(大富翁/赚钱王/败家子),无则空串。
	// 由 server 层在 ListAccounts 后按当日称号合并填充。
	Title string `json:"title"`
}

// UpsertAccount 登记/更新一个账号的展示名与活跃时间。
func (s *Store) UpsertAccount(account, name string) error {
	_, err := s.db.Exec(`
INSERT INTO accounts(account,name,updated_at) VALUES(?,?,?)
ON CONFLICT(account) DO UPDATE SET name=excluded.name, updated_at=excluded.updated_at`,
		account, name, time.Now().Unix())
	return err
}

// ListAccounts 返回已知账号(按最近活跃倒序),petCount 含零宠物账号(LEFT JOIN)。
func (s *Store) ListAccounts() ([]AccountInfo, error) {
	rows, err := s.rdb.Query(`
SELECT a.account, a.name, COUNT(p.gid), a.pin_hash IS NOT NULL AND a.pin_hash != '', a.coins, a.has_coins, a.rank_join
FROM accounts a LEFT JOIN pets p ON p.account = a.account
GROUP BY a.account, a.name, a.pin_hash, a.coins, a.has_coins, a.rank_join
ORDER BY a.updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AccountInfo
	for rows.Next() {
		var a AccountInfo
		if err := rows.Scan(&a.Account, &a.Name, &a.PetCount, &a.HasPin, &a.Coins, &a.HasCoins, &a.Join); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// SetAccountCoins 写入账号的金币数(登录回包解析成功后调用;coins 为 0 也是真实值,
// 一并置 has_coins=1 与「从未解析」区分,前端可显示「待同步」)。
// 同时记一条金币快照(coin_snapshots),排行榜盈亏以首条快照为基线,单事务保证一致。
func (s *Store) SetAccountCoins(account string, coins int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE accounts SET coins=?, has_coins=1 WHERE account=?`, coins, account); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO coin_snapshots(account, coins, ts) VALUES(?,?,?)`,
		account, coins, time.Now().Unix()); err != nil {
		return err
	}
	return tx.Commit()
}

// SetAccountRankJoin 设置账号是否参加排行榜(join=true 参加,false 退出)。
func (s *Store) SetAccountRankJoin(account string, join bool) error {
	_, err := s.db.Exec(`UPDATE accounts SET rank_join=? WHERE account=?`, b2i(join), account)
	return err
}

// RankEntry 是排行榜上单个账号的数据。
// Baseline 为首次金币快照(起始资金);无快照时退回当前 coins,盈亏为 0。
// Title 由 server 层按当日称号合并填充。
type RankEntry struct {
	Account  string `json:"account"`
	Name     string `json:"name"`
	Coins    int64  `json:"coins"`
	HasCoins bool   `json:"hasCoins"`
	Join     bool   `json:"join"`
	Baseline int64  `json:"baseline"`
	Profit   int64  `json:"profit"`
	Title    string `json:"title"`
}

// Leaderboard 返回全部参加排行(rank_join=1)的账号,含当前金币、首次快照基线
// 与盈亏(当前-基线)。调用方自行按福布斯/盈亏排序。
func (s *Store) Leaderboard() ([]RankEntry, error) {
	rows, err := s.rdb.Query(`
SELECT a.account, a.name, a.coins, a.has_coins, 1,
  COALESCE((SELECT cs.coins FROM coin_snapshots cs
            WHERE cs.account = a.account ORDER BY cs.ts ASC, cs.id ASC LIMIT 1), a.coins)
FROM accounts a
WHERE a.rank_join = 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RankEntry
	for rows.Next() {
		var e RankEntry
		if err := rows.Scan(&e.Account, &e.Name, &e.Coins, &e.HasCoins, &e.Join, &e.Baseline); err != nil {
			return nil, err
		}
		e.Profit = e.Coins - e.Baseline
		out = append(out, e)
	}
	return out, rows.Err()
}

// AccountRank 返回单个账号的排行参与信息(含 join 开关)。账号不存在时返回零值。
func (s *Store) AccountRank(account string) RankEntry {
	var e RankEntry
	_ = s.rdb.QueryRow(`
SELECT a.account, a.name, a.coins, a.has_coins, a.rank_join,
  COALESCE((SELECT cs.coins FROM coin_snapshots cs
            WHERE cs.account = a.account ORDER BY cs.ts ASC, cs.id ASC LIMIT 1), a.coins)
FROM accounts a WHERE a.account = ?`, account).
		Scan(&e.Account, &e.Name, &e.Coins, &e.HasCoins, &e.Join, &e.Baseline)
	e.Profit = e.Coins - e.Baseline
	return e
}

// RankTitleRow 是某天一条称号记录(大富翁/赚钱王/败家子)。
type RankTitleRow struct {
	Date    string `json:"date"`
	Account string `json:"account"`
	Name    string `json:"name"`
	Title   string `json:"title"`
}

// accStat 是结算时单个账号的前一日金币状态(见 SettleRankTitles)。
type accStat struct {
	hasCoins   bool
	endCoins   int64 // 前一日结束(最后一条 ts<d0 快照)时的金币
	hasData    bool  // 前一日 [p0,d0) 内有快照
	preDaySet  bool  // 存在 p0 前的快照(带入前一日)
	preDay     int64 // 最后一条 p0 前快照的金币
	dayStart   int64 // 前一日起点金币
	dayStartOK bool
	dayChange  int64 // 前一日净变化
}

// SettleRankTitles 结算某天(归属日 date,yyyy-mm-dd)的排行榜称号并写入 rank_titles,
// 依据 date 前一日 00:00~date 00:00(北京时间,UTC+8)的金币快照:
//
//	大富翁 = 前一日结束时金币最多(须 >0)
//	赚钱王 = 前一日净赚最多(须 >0)
//	败家子 = 前一日净亏最多(须 <0)
//
// 无人符合(如全员当日未登录)则该称号当天不发放;同日期旧结果整日覆盖(幂等)。
func (s *Store) SettleRankTitles(date string) error {
	loc := time.FixedZone("CST", 8*3600) // 北京时间,无夏令时
	d, err := time.ParseInLocation("2006-01-02", date, loc)
	if err != nil {
		return err
	}
	d0 := d.Unix() // date 当天 00:00
	p0 := d0 - 24*3600

	// 参加排行榜的账号
	accRows, err := s.rdb.Query(`SELECT account, coins, has_coins FROM accounts WHERE rank_join = 1`)
	if err != nil {
		return err
	}
	defer accRows.Close()
	stats := map[string]*accStat{}
	for accRows.Next() {
		var account string
		var coins, hasCoins int64
		if err := accRows.Scan(&account, &coins, &hasCoins); err != nil {
			return err
		}
		stats[account] = &accStat{hasCoins: hasCoins != 0, endCoins: coins}
	}
	if err := accRows.Err(); err != nil {
		return err
	}

	// 逐条回放 date 00:00 前的快照(升序),归集每个账号的前一日状态
	snapRows, err := s.rdb.Query(`SELECT account, coins, ts FROM coin_snapshots WHERE ts < ? ORDER BY ts ASC, id ASC`, d0)
	if err != nil {
		return err
	}
	defer snapRows.Close()
	for snapRows.Next() {
		var account string
		var coins, ts int64
		if err := snapRows.Scan(&account, &coins, &ts); err != nil {
			return err
		}
		st := stats[account]
		if st == nil {
			continue // 已退出排行榜的账号不参与评选
		}
		st.hasCoins = true
		st.endCoins = coins
		if ts < p0 {
			st.preDaySet = true
			st.preDay = coins
		} else { // ts in [p0, d0):前一日内的快照
			st.hasData = true
			if !st.dayStartOK {
				if st.preDaySet {
					st.dayStart = st.preDay
				} else {
					st.dayStart = coins
				}
				st.dayStartOK = true
			}
		}
	}
	if err := snapRows.Err(); err != nil {
		return err
	}
	for _, st := range stats {
		if st.dayStartOK {
			st.dayChange = st.endCoins - st.dayStart
		}
	}

	// 评选(严格比较,先到先得,账号排序保证确定性)
	var rich, earner, spender string
	var richCoins, earnerGain, spenderLoss int64
	richCoins, earnerGain, spenderLoss = -1, -1, 1
	for _, account := range sortedKeys(stats) {
		st := stats[account]
		if st.hasCoins && st.endCoins > 0 && st.endCoins > richCoins {
			rich, richCoins = account, st.endCoins
		}
		if st.hasData && st.dayChange > 0 && st.dayChange > earnerGain {
			earner, earnerGain = account, st.dayChange
		}
		if st.hasData && st.dayChange < 0 && st.dayChange < spenderLoss {
			spender, spenderLoss = account, st.dayChange
		}
	}

	// 整日覆盖写入
	winners := map[string]string{}
	if rich != "" {
		winners["大富翁"] = rich
	}
	if earner != "" {
		winners["赚钱王"] = earner
	}
	if spender != "" {
		winners["败家子"] = spender
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM rank_titles WHERE date = ?`, date); err != nil {
		return err
	}
	for title, account := range winners {
		if _, err := tx.Exec(`INSERT INTO rank_titles(date, account, title) VALUES(?,?,?)`,
			date, account, title); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// RankTitles 返回某天(归属日)的称号获奖名单(按 大富翁→赚钱王→败家子 排序)。
func (s *Store) RankTitles(date string) ([]RankTitleRow, error) {
	rows, err := s.rdb.Query(`
SELECT t.date, t.account, a.name, t.title
FROM rank_titles t LEFT JOIN accounts a ON a.account = t.account
WHERE t.date = ?
ORDER BY CASE t.title WHEN '大富翁' THEN 1 WHEN '赚钱王' THEN 2 ELSE 3 END`, date)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RankTitleRow
	for rows.Next() {
		var r RankTitleRow
		if err := rows.Scan(&r.Date, &r.Account, &r.Name, &r.Title); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// sortedKeys 返回 map 键的字典序列表(评选时保证结果确定性)。
func sortedKeys(m map[string]*accStat) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// AccountPinHash 返回账号的 PIN 哈希;未设置时返回空串。
func (s *Store) AccountPinHash(account string) string {
	var h string
	_ = s.rdb.QueryRow(`SELECT pin_hash FROM accounts WHERE account=?`, account).Scan(&h)
	return h
}

// SetAccountPin 写入账号的 PIN 哈希(管理员设置或用户自改)。
func (s *Store) SetAccountPin(account, hash string) error {
	_, err := s.db.Exec(`UPDATE accounts SET pin_hash=? WHERE account=?`, hash, account)
	return err
}

// DeleteAccount 删除一个账号的全部数据(13 张含 account 列的表),单事务。
// 返回删除是否成功。account_rule 也一并清理(黑白名单规则)。
func (s *Store) DeleteAccount(account string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	tables := []string{
		"pets", "events", "pet_box", "pet_boxes", "pet_team", "pet_medal",
		"eggs", "star_state", "star_zone", "paint", "sessions",
		"account_rule", "coin_snapshots", "rank_titles", "accounts",
	}
	for _, t := range tables {
		if _, err := tx.Exec("DELETE FROM "+t+" WHERE account=?", account); err != nil {
			return err
		}
	}
	return tx.Commit()
}
