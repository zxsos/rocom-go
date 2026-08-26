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
//
// **hatching 列是「在孵蛋器里」的权威状态,只由 0x0312 对账(ReconcileHatching)维护**:
// 服务器取出蛋时不清 start_hatch_time,放入/取出/破壳动作的回包与背包快照(0x1344/登录)
// 的入孵时刻都不可信,故 UpsertEggs 只把新行初始为 0、更新时不碰该列;放入(0x0164)、
// 取出(0x0300)回包一律只当普通变化入库,标记在玩家下次打开孵蛋器(0x0312)时全量收敛。
// 读取时以列为准覆盖 data 里的推断值(见 ListEggs)。

// UpsertEggs 批量写入/更新蛋(不动 parents、first_seen 与 hatching 列)。
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
			e.HeightM, e.WeightKg, hpct, wpct, e.Src, 0, e.ObtainedAt, // hatching 恒写 0:权威状态只由 0x0312 维护
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
  src=excluded.src, obtained_at=excluded.obtained_at,
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
	rows, err := sc.rdb.Query(`SELECT gid FROM eggs WHERE account=? AND first_seen<=?`, sc.account, before)
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

// ReconcileHatching 用 0x0312 的权威列表对账「在孵蛋器」标记(见 docs/data.md 3.6)。
// hatching 列**只由这里维护**:放入/取出/破壳都不再写列,全靠它全量收敛——
//   - gids 为空(孵蛋器空) → 本账号所有标着在孵的蛋全部清列与进度
//   - gids 里没有、但列标着在孵的蛋 → 清列与进度(取出/破壳残留的收敛点)
//   - gids 里的蛋 → 确保列为在孵;skip 里的蛋刚由 ret_info.changes 精确刷新过
//     (含 last_hatch_update_sec),不再动它们的进度
//   - secs 与 gids 长度一致时才顺带刷新进度(HatchUpdate 取消息时刻近似;proto3 会省掉
//     hatched_secs=0 的项,数组对不上时只对账标记)
//
// 返回是否有行被改动。
func (sc *Scoped) ReconcileHatching(gids []uint32, secs []int32, skip map[uint32]bool, now int64) (bool, error) {
	in := make(map[uint32]bool, len(gids))
	for _, g := range gids {
		in[g] = true
	}
	secBy := map[uint32]int32{}
	if paired := len(secs) == len(gids); paired {
		for i, g := range gids {
			secBy[g] = secs[i]
		}
	}
	rows, err := sc.rdb.Query(`SELECT gid, hatching, data FROM eggs WHERE account=?`, sc.account)
	if err != nil {
		return false, err
	}
	var updates [][]any
	for rows.Next() {
		var gid uint32
		var hatching int
		var data string
		if err := rows.Scan(&gid, &hatching, &data); err != nil {
			continue
		}
		var v pet.EggView
		if json.Unmarshal([]byte(data), &v) != nil {
			continue
		}
		if in[gid] {
			if hatching == 1 {
				if skip[gid] {
					continue // changes 已精确刷新过,不动
				}
				if sec, ok := secBy[gid]; !ok || (v.HatchedSecs == sec && v.HatchUpdate == now) {
					continue // 无可刷新进度
				}
			} else {
				hatching = 1
				v.Hatching = true
			}
			if sec, ok := secBy[gid]; ok {
				v.HatchedSecs, v.HatchUpdate = sec, now
			}
		} else if hatching == 1 {
			hatching = 0
			v.Hatching = false
			v.HatchedSecs, v.HatchUpdate, v.StartHatch = 0, 0, 0
		} else {
			continue
		}
		blob, err := json.Marshal(&v)
		if err != nil {
			continue
		}
		updates = append(updates, []any{hatching, string(blob), now, sc.account, gid})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return false, err
	}
	if len(updates) == 0 {
		return false, nil
	}
	return true, execBatch(sc.db,
		`UPDATE eggs SET hatching=?, data=?, updated_at=? WHERE account=? AND gid=?`, updates)
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
	rows, err := sc.rdb.Query(
		`SELECT data, parents, hatching FROM eggs WHERE `+where+
			` ORDER BY seq IS NULL, seq, gid`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*pet.EggView
	for rows.Next() {
		var data string
		var parents sql.NullString
		var hatching int
		if err := rows.Scan(&data, &parents, &hatching); err != nil {
			continue
		}
		var e pet.EggView
		if json.Unmarshal([]byte(data), &e) != nil {
			continue
		}
		e.Hatching = hatching == 1 // 以权威列覆盖 data 里的推断值
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
