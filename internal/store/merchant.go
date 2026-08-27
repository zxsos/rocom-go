package store

import "time"

// 远行商人第三方数据缓存(表结构见 store.go 的 merchant_slots)。
// 每个 4h 槽一条记录,防止玩家反复打开页面时重复烧第三方 token;
// 记录只保留最近 2 天,写入时顺手清理更早的(业务模型见 server/api_merchant.go)。
const merchantRetain = 2 * 24 * time.Hour

// GetMerchantSlot 读取某 4h 槽的缓存记录。无记录时 ok=false(尚未查过,可回源)。
func (s *Store) GetMerchantSlot(slot int64) (empty bool, data string, ok bool) {
	err := s.rdb.QueryRow(`SELECT empty, data FROM merchant_slots WHERE slot=?`, slot).Scan(&empty, &data)
	if err != nil {
		return false, "", false
	}
	return empty, data, true
}

// PutMerchantSlot 写入/更新某槽缓存(empty=查过但无货,data 为空;有货存第三方原始 JSON),
// 并顺手删除 2 天前的过期记录。
func (s *Store) PutMerchantSlot(slot int64, empty bool, data string) error {
	if _, err := s.db.Exec(`INSERT INTO merchant_slots(slot, empty, data, fetched_at) VALUES(?,?,?,?)
		ON CONFLICT(slot) DO UPDATE SET empty=excluded.empty, data=excluded.data, fetched_at=excluded.fetched_at`,
		slot, b2i(empty), data, time.Now().Unix()); err != nil {
		return err
	}
	// 写路径低频(一天至多几次),每次写都带一次删除无压力。
	_, err := s.db.Exec(`DELETE FROM merchant_slots WHERE slot < ?`, time.Now().Add(-merchantRetain).Unix())
	return err
}
