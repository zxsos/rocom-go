package store

import (
	"github.com/whoisnian/rocom-capture/internal/pet"
)

// ReplaceHandbookGlasses 用登录包解析出的图鉴炫彩收集整体替换本账号全部记录
// (数据仅登录包携带,登录时快照替换,与 pet_medal 同理)。
func (sc *Scoped) ReplaceHandbookGlasses(records []pet.GlassCollect) error {
	rows := make([][]any, 0, len(records))
	for _, r := range records {
		rows = append(rows, []any{sc.account, r.PetBaseID, r.GlassType, r.GlassValue})
	}
	return sc.replaceAll("handbook_glass",
		`INSERT OR IGNORE INTO handbook_glass(account,pet_base_id,glass_type,glass_value) VALUES(?,?,?,?)`, rows)
}

// ListHandbookGlasses 返回本账号全部图鉴炫彩收集(按品种、类型、值排序)。
func (sc *Scoped) ListHandbookGlasses() ([]pet.GlassCollect, error) {
	rows, err := sc.rdb.Query(
		`SELECT pet_base_id,glass_type,glass_value FROM handbook_glass WHERE account=? ORDER BY pet_base_id,glass_type,glass_value`,
		sc.account)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []pet.GlassCollect
	for rows.Next() {
		var r pet.GlassCollect
		if err := rows.Scan(&r.PetBaseID, &r.GlassType, &r.GlassValue); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
