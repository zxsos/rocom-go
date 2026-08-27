// 远行商人订阅与已通知记录(表结构见 store.go 的 merchant_subs / merchant_notified)。
// 订阅是玩家主动登记:收件邮箱 + 逗号分隔的商品名关键词(空=全部);每槽每邮箱只发一次,
// 去重记录也随 merchant_slots 的 2 天清理节奏一并清掉。
package store

import "time"

// MerchantSub 一条订阅记录。
type MerchantSub struct {
	Email     string
	Keywords  string
	CreatedAt int64
}

// UpsertMerchantSub 创建或更新订阅(同一邮箱只保留一条,关键词覆盖)。
func (s *Store) UpsertMerchantSub(email, keywords string) error {
	_, err := s.db.Exec(`INSERT INTO merchant_subs(email, keywords, created_at) VALUES(?,?,?)
		ON CONFLICT(email) DO UPDATE SET keywords=excluded.keywords`,
		email, keywords, time.Now().Unix())
	return err
}

// GetMerchantSub 读取某邮箱的订阅关键词。无订阅时 ok=false。
func (s *Store) GetMerchantSub(email string) (keywords string, ok bool) {
	err := s.rdb.QueryRow(`SELECT keywords FROM merchant_subs WHERE email=?`, email).Scan(&keywords)
	if err != nil {
		return "", false
	}
	return keywords, true
}

// ListMerchantSubs 列出全部订阅(通知循环遍历用,量小)。
func (s *Store) ListMerchantSubs() ([]MerchantSub, error) {
	rows, err := s.rdb.Query(`SELECT email, keywords, created_at FROM merchant_subs`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MerchantSub
	for rows.Next() {
		var sub MerchantSub
		if err := rows.Scan(&sub.Email, &sub.Keywords, &sub.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, sub)
	}
	return out, rows.Err()
}

// DeleteMerchantSub 退订。
func (s *Store) DeleteMerchantSub(email string) error {
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
