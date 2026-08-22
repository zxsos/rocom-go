// Package store 的管理员认证持久化:admin 表存密码哈希(单行 id=1),首启未配置时前端引导设置。
package store

import "time"

// AdminConfigured 返回管理员密码是否已设置(首启未设置时,前端引导设置)。
func (s *Store) AdminConfigured() (bool, error) {
	var n int
	err := s.rdb.QueryRow(`SELECT COUNT(*) FROM admin`).Scan(&n)
	return n > 0, err
}

// SetAdminPassword 写入管理员密码哈希(首启设置;id=1 冲突时覆盖)。
func (s *Store) SetAdminPassword(hash string) error {
	_, err := s.db.Exec(`
INSERT INTO admin(id, pass_hash, created_at) VALUES(1, ?, ?)
ON CONFLICT(id) DO UPDATE SET pass_hash=excluded.pass_hash`,
		hash, time.Now().Unix())
	return err
}

// AdminPassHash 返回管理员密码哈希;未设置时返回空串。
func (s *Store) AdminPassHash() string {
	var h string
	_ = s.rdb.QueryRow(`SELECT pass_hash FROM admin WHERE id=1`).Scan(&h)
	return h
}
