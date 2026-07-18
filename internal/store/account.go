package store

import "time"

// AccountInfo 是一个账号的概要(供前端账号下拉)。
type AccountInfo struct {
	Account  string `json:"account"`
	Name     string `json:"name"`
	PetCount int    `json:"petCount"`
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
	rows, err := s.db.Query(`
SELECT a.account, a.name, COUNT(p.gid)
FROM accounts a LEFT JOIN pets p ON p.account = a.account
GROUP BY a.account, a.name
ORDER BY a.updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AccountInfo
	for rows.Next() {
		var a AccountInfo
		if err := rows.Scan(&a.Account, &a.Name, &a.PetCount); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
