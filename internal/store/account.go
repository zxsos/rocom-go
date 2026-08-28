package store

import (
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
SELECT a.account, a.name, COUNT(p.gid), a.pin_hash IS NOT NULL AND a.pin_hash != '', a.coins, a.has_coins
FROM accounts a LEFT JOIN pets p ON p.account = a.account
GROUP BY a.account, a.name, a.pin_hash, a.coins, a.has_coins
ORDER BY a.updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AccountInfo
	for rows.Next() {
		var a AccountInfo
		if err := rows.Scan(&a.Account, &a.Name, &a.PetCount, &a.HasPin, &a.Coins, &a.HasCoins); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// SetAccountCoins 写入账号的金币数(登录回包解析成功后调用;coins 为 0 也是真实值,
// 一并置 has_coins=1 与「从未解析」区分,前端可显示「待同步」)。
func (s *Store) SetAccountCoins(account string, coins int64) error {
	_, err := s.db.Exec(`UPDATE accounts SET coins=?, has_coins=1 WHERE account=?`, coins, account)
	return err
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

// DeleteAccount 删除一个账号的全部数据(12 张含 account 列的表),单事务。
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
		"account_rule", "accounts",
	}
	for _, t := range tables {
		if _, err := tx.Exec("DELETE FROM "+t+" WHERE account=?", account); err != nil {
			return err
		}
	}
	return tx.Commit()
}
