package store

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/whoisnian/rocom-capture/internal/pet"
)

// 精灵蛋的持久化。与宠物同样按 account 隔离,主键 (account, egg gid)。
//
// 两个设计要点(见 docs/data.md 3.6):
//   - **双亲快照单列存**:亲本可能被放生/赠送(pets 行随之删除),而蛋上记下的双亲要留存,
//     故 parents 存的是收蛋那一刻的 JSON 快照,不引用 pets 表。写蛋的常规 upsert 不碰它。
//   - **破壳后不删行**:置 hatched 并记下孵出的宠物 gid,页面可回看「这只是哪颗蛋孵的」。
//     背包对账(PruneMissingEggs)同样只标记不删,免得历史一走了之。

// EggState 是一颗蛋的当前去向。
const (
	EggInBag   = 0 // 还在背包里(含正在孵)
	EggHatched = 1 // 已破壳
	EggGone    = 2 // 已不在背包(赠送/别处消耗),且未见破壳
)

// UpsertEggs 批量写入/更新蛋(不动 parents 与 first_seen)。
func (sc *Scoped) UpsertEggs(eggs []*pet.EggView) error {
	if len(eggs) == 0 {
		return nil
	}
	now := time.Now().Unix()
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
  updated_at=excluded.updated_at, data=excluded.data,
  state=CASE WHEN eggs.state=? THEN eggs.state ELSE ? END`,
		appendAll(rows, EggHatched, EggInBag))
}

// appendAll 给每行追加相同的尾参数(上面的 CASE 用到两个常量)。
func appendAll(rows [][]any, tail ...any) [][]any {
	for i := range rows {
		rows[i] = append(rows[i], tail...)
	}
	return rows
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

// MarkEggHatched 记录破壳:置状态并关联孵出的宠物 gid(行保留作历史)。
func (sc *Scoped) MarkEggHatched(gid, petGid uint32, at int64) error {
	_, err := sc.db.Exec(
		`UPDATE eggs SET state=?, hatched_at=?, pet_gid=?, hatching=0, updated_at=? WHERE account=? AND gid=?`,
		EggHatched, at, petGid, time.Now().Unix(), sc.account, gid)
	return err
}

// PruneMissingEggs 据一轮完整的背包全量对账:仍在背包的置回 EggInBag,不在的标 EggGone
// (已破壳的不动)。before 之后才首次见到的行放过,避免与同一时刻的新蛋抢跑。
func (sc *Scoped) PruneMissingEggs(keep map[uint32]bool, before int64) error {
	rows, err := sc.db.Query(`SELECT gid, state FROM eggs WHERE account=? AND state!=?`, sc.account, EggHatched)
	if err != nil {
		return err
	}
	type row struct {
		gid   uint32
		state int
	}
	var all []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.gid, &r.state); err == nil {
			all = append(all, r)
		}
	}
	rows.Close()
	now := time.Now().Unix()
	for _, r := range all {
		want := EggGone
		if keep[r.gid] {
			want = EggInBag
		}
		if want == r.state {
			continue
		}
		if want == EggGone {
			var first int64
			if err := sc.db.QueryRow(`SELECT first_seen FROM eggs WHERE account=? AND gid=?`,
				sc.account, r.gid).Scan(&first); err == nil && first > before {
				continue // 本轮之后才入库的新蛋:不参与这一轮对账
			}
		}
		sc.db.Exec(`UPDATE eggs SET state=?, updated_at=? WHERE account=? AND gid=?`, want, now, sc.account, r.gid)
	}
	return nil
}

// EggFilter 是精灵蛋列表的筛选条件(空值即不限)。
type EggFilter struct {
	State    int    // -1=全部;默认只看在背包的
	Hatching bool   // 只看在孵蛋器里的
	Search   string // 按蛋名/物种名模糊
	Sort     string // obtained/weight/height/name;默认 obtained
	Order    string // asc/desc;默认 desc
}

// ListEggs 按筛选返回蛋列表(已合并 parents 快照)。
func (sc *Scoped) ListEggs(f EggFilter) ([]*pet.EggView, error) {
	where := `account=?`
	args := []any{sc.account}
	if f.State >= 0 {
		where += ` AND state=?`
		args = append(args, f.State)
	}
	if f.Hatching {
		where += ` AND hatching=1`
	}
	if f.Search != "" {
		where += ` AND (name LIKE ? OR species LIKE ?)`
		args = append(args, "%"+f.Search+"%", "%"+f.Search+"%")
	}
	order := map[string]string{
		"obtained": "obtained_at", "weight": "weight_pct", "height": "height_pct", "name": "name",
	}[f.Sort]
	if order == "" {
		order = "obtained_at"
	}
	dir := "DESC"
	if f.Order == "asc" {
		dir = "ASC"
	}
	rows, err := sc.db.Query(
		`SELECT data, parents, state, hatched_at, pet_gid FROM eggs WHERE `+where+
			` ORDER BY `+order+` `+dir+` NULLS LAST, gid DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*pet.EggView
	for rows.Next() {
		var data string
		var parents sql.NullString
		var state int
		var hatchedAt sql.NullInt64
		var petGid sql.NullInt64
		if err := rows.Scan(&data, &parents, &state, &hatchedAt, &petGid); err != nil {
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
		e.Hatched = state == EggHatched
		e.HatchedAt = hatchedAt.Int64
		e.PetGid = uint32(petGid.Int64)
		out = append(out, &e)
	}
	return out, rows.Err()
}

// CountEggs 返回本账号各状态的蛋数量(在背包 / 在孵 / 已破壳)。
func (sc *Scoped) CountEggs() (inBag, hatching, hatched int) {
	sc.db.QueryRow(`SELECT
  COUNT(*) FILTER (WHERE state=?),
  COUNT(*) FILTER (WHERE state=? AND hatching=1),
  COUNT(*) FILTER (WHERE state=?) FROM eggs WHERE account=?`,
		EggInBag, EggInBag, EggHatched, sc.account).Scan(&inBag, &hatching, &hatched)
	return
}
