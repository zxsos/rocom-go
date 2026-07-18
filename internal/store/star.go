package store

import "time"

// 眠枭之星收集状态(见 docs/data.md 3.4):未确认(无行)/未收集/已收集。
const (
	StarUncollected = 1 // 收到过该刷新点的 NPC 实体 ⇒ 还在原地,未收集
	StarCollected   = 2 // 玩家走近了却没有该实体 ⇒ 已收集(已收集的星星服务器不刷)
)

// SetStarStates 批量记录星星刷新点的收集状态(按账号 upsert)。
func (s *Store) SetStarStates(account string, states map[int32]int) error {
	if len(states) == 0 {
		return nil
	}
	now := time.Now().Unix()
	rows := make([][]any, 0, len(states))
	for rid, st := range states {
		rows = append(rows, []any{account, rid, st, now})
	}
	return execBatch(s.db, `INSERT INTO star_state(account, refresh_id, state, updated_at) VALUES(?,?,?,?)
		ON CONFLICT(account, refresh_id) DO UPDATE SET state=excluded.state, updated_at=excluded.updated_at`, rows)
}

// StarStates 返回某账号已确认的星星状态(刷新点 id -> 状态)。
func (s *Store) StarStates(account string) map[int32]int {
	out := map[int32]int{}
	rows, err := s.db.Query(`SELECT refresh_id, state FROM star_state WHERE account=?`, account)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var rid int32
		var st int
		if rows.Scan(&rid, &st) == nil {
			out[rid] = st
		}
	}
	return out
}

// ZoneProgressRow 是某账号某区域某类星星的收集进度(服务器口径)。
type ZoneProgressRow struct {
	Camp  int32 `json:"camp"`
	NpcID int32 `json:"npc"`
	Got   int32 `json:"got"`
	Total int32 `json:"tot"`
}

// SetStarZones 覆盖写入某账号的按区域收集进度(进场景包每次都给全量)。
func (s *Store) SetStarZones(account string, rows []ZoneProgressRow) error {
	if len(rows) == 0 {
		return nil
	}
	now := time.Now().Unix()
	batch := make([][]any, 0, len(rows))
	for _, r := range rows {
		batch = append(batch, []any{account, r.Camp, r.NpcID, r.Got, r.Total, now})
	}
	return execBatch(s.db, `INSERT INTO star_zone(account, camp, npc_id, got, total, updated_at) VALUES(?,?,?,?,?,?)
		ON CONFLICT(account, camp, npc_id) DO UPDATE SET got=excluded.got, total=excluded.total, updated_at=excluded.updated_at`, batch)
}

// StarZones 返回某账号的按区域收集进度。
func (s *Store) StarZones(account string) []ZoneProgressRow {
	var out []ZoneProgressRow
	rows, err := s.db.Query(`SELECT camp, npc_id, got, total FROM star_zone WHERE account=?`, account)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var r ZoneProgressRow
		if rows.Scan(&r.Camp, &r.NpcID, &r.Got, &r.Total) == nil {
			out = append(out, r)
		}
	}
	return out
}
