package store

import (
	"strings"
	"time"
)

// 远行商人第三方数据缓存(表结构见 store.go 的 merchant_slots)。
// 每个 4h 槽一条记录,防止玩家反复打开页面时重复烧第三方 token;
// 记录只保留最近 2 天,写入时顺手清理更早的(业务模型见 server/merchant.go)。
//
// 与最初的「每槽只查一次」不同,现在**进行中的槽允许按冷却重查** —— 第三方自己有缓存,
// 轮次开始后新上架的商品要滞后才出现在它的响应里,只查一次会永久错过(见 server/merchant.go
// 的 merchantShouldFetch)。故此处的 fetched_at 不只是审计字段,它参与重查冷却判定。
const merchantRetain = 2 * 24 * time.Hour

// GetMerchantSlot 读取某 4h 槽的缓存记录。无记录时 ok=false(尚未查过,可回源)。
// fetchedAt 是该记录的最后回源时刻(Unix 秒),供「进行中的槽按冷却重查」判定用。
func (s *Store) GetMerchantSlot(slot int64) (empty bool, data string, fetchedAt int64, ok bool) {
	err := s.rdb.QueryRow(`SELECT empty, data, fetched_at FROM merchant_slots WHERE slot=?`, slot).
		Scan(&empty, &data, &fetchedAt)
	if err != nil {
		return false, "", 0, false
	}
	return empty, data, fetchedAt, true
}

// PutMerchantSlotAt 同 PutMerchantSlot,但允许显式指定回源时刻。
//
// 只有**测试**会传非当前时间:重查冷却按「上次回源时刻」判定(见 server/merchant.go 的
// merchantShouldFetch),要构造「10 分钟前刚查过」这类用例必须能往回写时间戳 ——
// 否则只能真的睡 10 分钟。生产调用方一律用 PutMerchantSlot。
func (s *Store) PutMerchantSlotAt(slot int64, empty bool, data string, fetchedAt int64) error {
	if _, err := s.db.Exec(`INSERT INTO merchant_slots(slot, empty, data, fetched_at) VALUES(?,?,?,?)
		ON CONFLICT(slot) DO UPDATE SET empty=excluded.empty, data=excluded.data, fetched_at=excluded.fetched_at`,
		slot, b2i(empty), data, fetchedAt); err != nil {
		return err
	}
	// 写路径低频(一天至多几十次),每次写都带一次删除无压力。
	_, err := s.db.Exec(`DELETE FROM merchant_slots WHERE slot < ?`, time.Now().Add(-merchantRetain).Unix())
	return err
}

// PutMerchantSlot 写入/更新某槽缓存(empty=查过但无货,data 为空;有货存第三方原始 JSON),
// 回源时刻取当前时间,并顺手删除 2 天前的过期记录。
//
// 注意这是**全量覆盖**:重查拿到的数据会盖掉旧的。调用方需自行判断「这次结果更差时该不该写」
// (见 server/merchant.go merchantFetch 的空响应保护)。
func (s *Store) PutMerchantSlot(slot int64, empty bool, data string) error {
	return s.PutMerchantSlotAt(slot, empty, data, time.Now().Unix())
}

// TouchMerchantSlot 只把某槽的 fetched_at 推到当前时刻,不动 empty/data。
//
// 存在理由:重查撞上第三方瞬时抽风(限流/业务错误)时,我们会保留库里已有的货单。
// 但若不更新 fetched_at,重查冷却会立刻失效 —— 下一个 tick 又判定「早就该重查了」,
// 于是一路回源到窗口结束,白烧 token。推时间戳即「这次查过了,按冷却下次再来」。
func (s *Store) TouchMerchantSlot(slot int64) error {
	_, err := s.db.Exec(`UPDATE merchant_slots SET fetched_at=? WHERE slot=?`, time.Now().Unix(), slot)
	return err
}

// MerchantNotifiedItems 返回某槽已通知过该邮箱的商品名集合。未发过信时返回空集合(非 nil 也可判空)。
//
// 去重粒度是**商品**而非整槽:一轮内可能回源多次(第三方滞后补货),每次都可能带来新商品,
// 只按槽去重会让后到的商品被永久挡住 —— 正是这个集合让「第二次通知只发新增的那几件」成为可能。
func (s *Store) MerchantNotifiedItems(slot int64, email string) map[string]bool {
	var items string
	err := s.rdb.QueryRow(`SELECT items FROM merchant_notified WHERE slot=? AND email=?`, slot, email).Scan(&items)
	if err != nil {
		return map[string]bool{}
	}
	out := map[string]bool{}
	for _, n := range strings.Split(items, ",") {
		if n = strings.TrimSpace(n); n != "" {
			out[n] = true
		}
	}
	return out
}

// MarkMerchantNotified 记录某槽已通知该邮箱哪些商品(在既有清单上追加,不覆盖),
// 顺带清理 2 天前的去重记录(与槽缓存同节奏)。
//
// names 是本次成功投递的商品名;调用方保证它与库里已有的清单不相交(待发清单就是
// 「本槽全量 − 本槽已通知」算出来的),故追加不会产生重复。
func (s *Store) MarkMerchantNotified(slot int64, email string, names []string) error {
	if _, err := s.db.Exec(`INSERT INTO merchant_notified(slot, email, items) VALUES(?,?,?)
		ON CONFLICT(slot, email) DO UPDATE SET items=excluded.items || ',' || merchant_notified.items`,
		slot, email, strings.Join(names, ",")); err != nil {
		return err
	}
	_, err := s.db.Exec(`DELETE FROM merchant_notified WHERE slot < ?`, time.Now().Add(-merchantRetain).Unix())
	return err
}
