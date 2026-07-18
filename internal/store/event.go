package store

import (
	"encoding/json"

	"github.com/whoisnian/rocom-capture/internal/pet"
)

// Event 是一条获得宠物事件(放生/赠送出等减少事件不入库)。
type Event struct {
	ID      int64    `json:"id"`
	Time    int64    `json:"time"`
	SubKind string   `json:"subKind"` // 捕捉/孵蛋/赠送 等(由 catch_way 推断)
	Gid     uint32   `json:"gid"`
	Pet     *pet.Pet `json:"pet"`
}

// AddEvent 写入本账号一条事件。
func (sc *Scoped) AddEvent(e *Event) error {
	// 注入盒位/队位(捕捉回包常同时携带落位,此时已 ApplyBoxMoves),使实时广播的事件即带位置。
	var species, nature, medal any = "", "", ""
	shiny := 0
	if e.Pet != nil {
		e.Pet.Box = sc.boxLocFor(e.Pet.Gid)
		e.Pet.Team = sc.teamLocFor(e.Pet.Gid)
		species, nature, medal, shiny = e.Pet.Species, e.Pet.Nature, e.Pet.Medal, b2i(e.Pet.Shiny)
	}
	data, _ := json.Marshal(e.Pet)
	res, err := sc.db.Exec(`INSERT INTO events(account,time,sub_kind,gid,species,nature,medal,shiny,data)
VALUES(?,?,?,?,?,?,?,?,?)`,
		sc.account, e.Time, e.SubKind, e.Gid, species, nature, medal, shiny, string(data))
	if err != nil {
		return err
	}
	e.ID, _ = res.LastInsertId()
	return nil
}

// ClearEvents 清空本账号事件历史。
func (sc *Scoped) ClearEvents() error {
	_, err := sc.db.Exec(`DELETE FROM events WHERE account=?`, sc.account)
	return err
}

// CountEvents 返回本账号事件总数(即自上次清空以来获得的宠物数,失去事件不入库)。
func (sc *Scoped) CountEvents() (int, error) {
	var n int
	err := sc.db.QueryRow(`SELECT COUNT(*) FROM events WHERE account=?`, sc.account).Scan(&n)
	return n, err
}

// ListEvents 返回本账号最近事件(按时间倒序)。
func (sc *Scoped) ListEvents(limit, beforeID int) ([]*Event, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := `SELECT id,time,sub_kind,gid,data FROM events WHERE account=?`
	args := []any{sc.account}
	if beforeID > 0 {
		q += ` AND id < ?`
		args = append(args, beforeID)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := sc.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	var out []*Event
	for rows.Next() {
		var e Event
		var data string
		if err := rows.Scan(&e.ID, &e.Time, &e.SubKind, &e.Gid, &data); err != nil {
			rows.Close()
			return nil, err
		}
		var p pet.Pet
		if json.Unmarshal([]byte(data), &p) == nil {
			e.Pet = &p
		}
		out = append(out, &e)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close() // 先关闭结果集再发后续查询:SetMaxOpenConns(1) 下迭代中嵌套查询会死锁
	// 注入当前盒位/队位(与宠物列表一致,反映该宠物现在所处位置;已放生则为空)。
	// 各一次批量查询,替代逐事件两次单行查询。
	gids := make([]uint32, 0, len(out))
	for _, e := range out {
		if e.Pet != nil {
			gids = append(gids, e.Gid)
		}
	}
	boxes := sc.batchBoxLocs(gids)
	teams := sc.batchTeamLocs(gids)
	for _, e := range out {
		if e.Pet != nil {
			e.Pet.Box = boxes[e.Gid]
			e.Pet.Team = teams[e.Gid]
		}
	}
	return out, nil
}
