package store

import (
	"encoding/json"
	"sort"
	"time"

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

// EventStats 事件统计:来源/稀有汇总 + 近30天按天分布 + 热门形态。
type EventStats struct {
	Total      int            `json:"total"`
	BySubKind  map[string]int `json:"bySubKind"` // 获得方式(捕捉/孵蛋/赠送获得/获得)
	Shiny      int            `json:"shiny"`     // 异色
	Colorful   int            `json:"colorful"`  // 炫彩
	Daily      []DailyCount   `json:"daily"`     // 近30天,时间升序(含0)
	TopSpecies []SpeciesCount `json:"topSpecies"` // 获得最多的形态,至多10
}

// DailyCount 某天的获得数。
type DailyCount struct {
	Day string `json:"day"` // "MM-DD"
	N   int    `json:"n"`
}

// SpeciesCount 某形态的获得数。
type SpeciesCount struct {
	S string `json:"s"`
	N int    `json:"n"`
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
	err := sc.rdb.QueryRow(`SELECT COUNT(*) FROM events WHERE account=?`, sc.account).Scan(&n)
	return n, err
}

// StatsEvents 统计本账号全部事件。events 数据量小,一次扫表在内存聚合,避免复杂 SQL。
func (sc *Scoped) StatsEvents() (*EventStats, error) {
	rows, err := sc.rdb.Query(`SELECT time, sub_kind, species, shiny, data FROM events WHERE account=?`, sc.account)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	st := &EventStats{BySubKind: map[string]int{}, TopSpecies: []SpeciesCount{}}
	species := map[string]int{}
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	daily := make([]int, 30) // daily[i] = i 天前(0=今天)
	for rows.Next() {
		var t int64
		var sub, sp string
		var shiny int
		var data string
		if err := rows.Scan(&t, &sub, &sp, &shiny, &data); err != nil {
			return nil, err
		}
		st.Total++
		st.BySubKind[sub]++
		if shiny > 0 {
			st.Shiny++
		}
		var m struct {
			Colorful bool `json:"colorful"`
		}
		if json.Unmarshal([]byte(data), &m) == nil && m.Colorful {
			st.Colorful++
		}
		if sp != "" {
			species[sp]++
		}
		d := time.Unix(t, 0)
		day := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, d.Location())
		if idx := int(today.Sub(day).Hours() / 24); idx >= 0 && idx < 30 {
			daily[idx]++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	st.Daily = make([]DailyCount, 30)
	for i := 0; i < 30; i++ {
		st.Daily[29-i] = DailyCount{Day: today.AddDate(0, 0, -i).Format("01-02"), N: daily[i]}
	}

	names := make([]string, 0, len(species))
	for s := range species {
		names = append(names, s)
	}
	sort.Slice(names, func(i, j int) bool { return species[names[i]] > species[names[j]] })
	if len(names) > 10 {
		names = names[:10]
	}
	for _, s := range names {
		st.TopSpecies = append(st.TopSpecies, SpeciesCount{S: s, N: species[s]})
	}
	return st, nil
}

// ListEvents 返回本账号最近事件(按时间倒序)。offset > 0 时做页码分页(跳过前 offset 条),
// 与 beforeID 游标二选一:页码分页供前端翻页,游标供「加载更早」流式追加。
func (sc *Scoped) ListEvents(limit, beforeID, offset int) ([]*Event, error) {
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
	if offset > 0 {
		q += ` OFFSET ?`
		args = append(args, offset)
	}
	rows, err := sc.rdb.Query(q, args...)
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
