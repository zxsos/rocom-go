package store

import (
	"database/sql"
	"encoding/json"

	"github.com/whoisnian/rocom-capture/internal/pet"
)

// 精灵蛋的持久化。与宠物同样按 account 隔离,主键 (account, egg gid)。
//
// 这张表就是**当前背包里的蛋**:破壳/送人/用掉的直接删行(页面只看背包,留着也没人看)。
// 唯一要当心的是 **双亲快照单列存**:亲本可能被放生/赠送(pets 行随之删除),而蛋上记下的
// 双亲要留存,故 parents 存的是收蛋那一刻的 JSON 快照,不引用 pets 表;常规 upsert 不碰它。

// UpsertEggs 批量写入/更新蛋(不动 parents 与 first_seen)。
// now 取**消息时刻**而非 time.Now():离线回放的包时间是几小时前的,与挂钟混用会让
// PruneMissingEggs 的 first_seen<=before 永远不成立,过期的蛋就永远删不掉。
func (sc *Scoped) UpsertEggs(eggs []*pet.EggView, now int64) error {
	if len(eggs) == 0 {
		return nil
	}
	rows := make([][]any, 0, len(eggs))
	for _, e := range eggs {
		data, err := json.Marshal(e)
		if err != nil {
			continue
		}
		var hpct, wpct any
		if e.HeightPct != nil {
			hpct = *e.HeightPct
		}
		if e.WeightPct != nil {
			wpct = *e.WeightPct
		}
		rows = append(rows, []any{
			sc.account, e.Gid, e.ItemID, e.ConfID, e.Name, e.Species,
			e.HeightM, e.WeightKg, hpct, wpct, e.Src, e.Hatching, e.ObtainedAt,
			now, now, string(data),
		})
	}
	return execBatch(sc.db, `
INSERT INTO eggs(account, gid, item_id, conf_id, name, species,
                 height, weight, height_pct, weight_pct, src, hatching, obtained_at,
                 first_seen, updated_at, data)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(account, gid) DO UPDATE SET
  item_id=excluded.item_id, conf_id=excluded.conf_id, name=excluded.name, species=excluded.species,
  height=excluded.height, weight=excluded.weight,
  height_pct=excluded.height_pct, weight_pct=excluded.weight_pct,
  src=excluded.src, hatching=excluded.hatching, obtained_at=excluded.obtained_at,
  updated_at=excluded.updated_at, data=excluded.data`, rows)
}

// SetEggParents 记下某颗蛋的双亲快照(收蛋那一刻推断出来的);已有记录不覆盖,
// 免得后来的背包全量或再次进家园把当时的快照冲掉。
func (sc *Scoped) SetEggParents(gid uint32, p *pet.EggParents) error {
	blob, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = sc.db.Exec(
		`UPDATE eggs SET parents=? WHERE account=? AND gid=? AND (parents IS NULL OR parents='')`,
		string(blob), sc.account, gid)
	return err
}

// SetEggOrder 记下这些蛋在背包里的次序(下标即服务器下发顺序)。
// 页面的两种排序都可能出现「所有键都相等」的两颗蛋(同一时刻入包的同种蛋),游戏内此时保持
// 背包原始次序(客户端 table.sort 的输入就是这个顺序),故这里把它存下来当基准。
func (sc *Scoped) SetEggOrder(order []uint32) error {
	rows := make([][]any, 0, len(order))
	for i, gid := range order {
		rows = append(rows, []any{i, sc.account, gid})
	}
	return execBatch(sc.db, `UPDATE eggs SET seq=? WHERE account=? AND gid=?`, rows)
}

// DeleteEgg 删掉一颗蛋(破壳即用完了,背包里没有这一件了)。
func (sc *Scoped) DeleteEgg(gid uint32) error {
	_, err := sc.db.Exec(`DELETE FROM eggs WHERE account=? AND gid=?`, sc.account, gid)
	return err
}

// PruneMissingEggs 据一轮完整的背包全量对账:不在背包里的直接删掉。
// before 之后才首次见到的行放过,避免与同一时刻的新蛋抢跑。
func (sc *Scoped) PruneMissingEggs(keep map[uint32]bool, before int64) error {
	rows, err := sc.db.Query(`SELECT gid FROM eggs WHERE account=? AND first_seen<=?`, sc.account, before)
	if err != nil {
		return err
	}
	var gone []uint32
	for rows.Next() {
		var gid uint32
		if err := rows.Scan(&gid); err == nil && !keep[gid] {
			gone = append(gone, gid)
		}
	}
	rows.Close()
	for _, gid := range gone {
		sc.db.Exec(`DELETE FROM eggs WHERE account=? AND gid=?`, sc.account, gid)
	}
	return nil
}

// EggFilter 是精灵蛋列表的筛选条件(空值即不限)。
// 排序不在这里:游戏内的「品质排序」是品类/品质/物品排序号的复合键,这些键取自名称库、
// 读取时才重算(见 pet.RefreshEggView),故排序由调用方在重算之后用 pet.SortEggs 做。
type EggFilter struct {
	Search string // 按蛋名/物种名模糊
}

// ListEggs 按筛选返回蛋列表(已合并 parents 快照)。
func (sc *Scoped) ListEggs(f EggFilter) ([]*pet.EggView, error) {
	where := `account=?`
	args := []any{sc.account}
	if f.Search != "" {
		where += ` AND (name LIKE ? OR species LIKE ?)`
		args = append(args, "%"+f.Search+"%", "%"+f.Search+"%")
	}
	// 基准顺序 = 背包里的原始次序(见 SetEggOrder);还没对过账的新蛋没有 seq,排在最后。
	// pet.SortEggs 用的是稳定排序,故所有键都相等的蛋会保持这个次序,与游戏内一致。
	rows, err := sc.db.Query(
		`SELECT data, parents FROM eggs WHERE `+where+
			` ORDER BY seq IS NULL, seq, gid`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*pet.EggView
	for rows.Next() {
		var data string
		var parents sql.NullString
		if err := rows.Scan(&data, &parents); err != nil {
			continue
		}
		var e pet.EggView
		if json.Unmarshal([]byte(data), &e) != nil {
			continue
		}
		if parents.Valid && parents.String != "" {
			var p pet.EggParents
			if json.Unmarshal([]byte(parents.String), &p) == nil {
				e.Parents = &p
			}
		}
		out = append(out, &e)
	}
	return out, rows.Err()
}
