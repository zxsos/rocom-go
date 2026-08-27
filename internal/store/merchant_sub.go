// 远行商人订阅与已通知记录(表结构见 store.go 的 merchant_subs / merchant_notified)。
// 订阅按登录账号绑定:一个账号只绑一个收件邮箱,换设备登录同一账号也能识别已订阅;
// 收件邮箱 + 逗号分隔的商品名关键词(空=全部);每槽每邮箱只发一次,
// 去重记录也随 merchant_slots 的 2 天清理节奏一并清掉。
package store

import "time"

// MerchantSub 一条订阅记录。
type MerchantSub struct {
	Account   string
	Email     string
	Keywords  string
	CreatedAt int64
}

// UpsertMerchantSub 创建或更新订阅(同一账号只保留一条,邮箱/关键词覆盖)。
func (s *Store) UpsertMerchantSub(account, email, keywords string) error {
	_, err := s.db.Exec(`INSERT INTO merchant_subs(account, email, keywords, created_at) VALUES(?,?,?,?)
		ON CONFLICT(account) DO UPDATE SET email=excluded.email, keywords=excluded.keywords`,
		account, email, keywords, time.Now().Unix())
	return err
}

// GetMerchantSub 读取某账号的订阅邮箱与关键词。未订阅时 ok=false。
func (s *Store) GetMerchantSub(account string) (email, keywords string, ok bool) {
	err := s.rdb.QueryRow(`SELECT email, keywords FROM merchant_subs WHERE account=?`, account).Scan(&email, &keywords)
	if err != nil {
		return "", "", false
	}
	return email, keywords, true
}

// ListMerchantSubs 列出全部订阅(通知循环遍历用,量小)。
func (s *Store) ListMerchantSubs() ([]MerchantSub, error) {
	rows, err := s.rdb.Query(`SELECT account, email, keywords, created_at FROM merchant_subs`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MerchantSub
	for rows.Next() {
		var sub MerchantSub
		if err := rows.Scan(&sub.Account, &sub.Email, &sub.Keywords, &sub.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, sub)
	}
	return out, rows.Err()
}

// DeleteMerchantSub 退订(按登录账号)。
func (s *Store) DeleteMerchantSub(account string) error {
	_, err := s.db.Exec(`DELETE FROM merchant_subs WHERE account=?`, account)
	return err
}

// DeleteMerchantSubByEmail 管理接口:按收件邮箱删除关联的全部订阅。
func (s *Store) DeleteMerchantSubByEmail(email string) error {
	_, err := s.db.Exec(`DELETE FROM merchant_subs WHERE email=?`, email)
	return err
}

// MerchantNotified 判断某槽是否已给该邮箱发过提醒。
func (s *Store) MerchantNotified(slot int64, email string) bool {
	var one int
	err := s.rdb.QueryRow(`SELECT 1 FROM merchant_notified WHERE slot=? AND email=?`, slot, email).Scan(&one)
	return err == nil
}

// MarkMerchantNotified 记录某槽已通知该邮箱,顺带清理 2 天前的去重记录(与槽缓存同节奏)。
func (s *Store) MarkMerchantNotified(slot int64, email string) error {
	if _, err := s.db.Exec(`INSERT OR IGNORE INTO merchant_notified(slot, email) VALUES(?,?)`, slot, email); err != nil {
		return err
	}
	_, err := s.db.Exec(`DELETE FROM merchant_notified WHERE slot < ?`, time.Now().Add(-merchantRetain).Unix())
	return err
}
