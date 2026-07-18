package store

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/whoisnian/rocom-capture/internal/pet"
)

// ReplacePetBoxes 用一份完整背包快照替换本账号所有宠物盒子位置。
func (sc *Scoped) ReplacePetBoxes(entries []pet.BoxEntry) error {
	rows := make([][]any, 0, len(entries))
	for _, e := range entries {
		rows = append(rows, []any{sc.account, e.Gid, e.BoxID, e.Slot, e.BoxName, e.Mark})
	}
	return sc.replaceAll("pet_box",
		`INSERT OR REPLACE INTO pet_box(account,gid,box_id,slot,box_name,mark) VALUES(?,?,?,?,?,?)`, rows)
}

// ReplacePetBoxMetas 用一份完整盒子元数据快照替换本账号所有盒子。
// 元数据含空盒,是盒名/数量/位置(box_id)的权威来源;登录/整理回包携带全量盒列表时刷新。
func (sc *Scoped) ReplacePetBoxMetas(metas []pet.BoxMeta) error {
	rows := make([][]any, 0, len(metas))
	for _, mt := range metas {
		rows = append(rows, []any{sc.account, mt.BoxID, mt.Name, mt.Mark, mt.Lock})
	}
	return sc.replaceAll("pet_boxes",
		`INSERT OR REPLACE INTO pet_boxes(account,box_id,name,mark,lock) VALUES(?,?,?,?,?)`, rows)
}

// ReplacePetTeams 用一份大世界队伍快照替换本账号所有宠物队伍位置。
func (sc *Scoped) ReplacePetTeams(entries []pet.TeamEntry) error {
	rows := make([][]any, 0, len(entries))
	for _, e := range entries {
		rows = append(rows, []any{sc.account, e.Gid, e.TeamIdx, e.Pos})
	}
	return sc.replaceAll("pet_team",
		`INSERT OR REPLACE INTO pet_team(account,gid,team_idx,pos) VALUES(?,?,?,?)`, rows)
}

// ReplacePetMedals 用一份登录快照替换本账号所有宠物拥有的奖牌(gid↔medal 多对多)。
func (sc *Scoped) ReplacePetMedals(owns []pet.MedalOwn) error {
	rows := make([][]any, 0, len(owns))
	for _, o := range owns {
		rows = append(rows, []any{sc.account, o.Gid, o.MedalID})
	}
	return sc.replaceAll("pet_medal",
		`INSERT OR IGNORE INTO pet_medal(account,gid,medal_id) VALUES(?,?,?)`, rows)
}

// UpsertPetBoxMeta 增量更新单个盒子的元数据(解锁新盒 / 设标记 / 改名),不动其他盒子。
func (sc *Scoped) UpsertPetBoxMeta(meta pet.BoxMeta) error {
	_, err := sc.db.Exec(`INSERT OR REPLACE INTO pet_boxes(account,box_id,name,mark,lock) VALUES(?,?,?,?,?)`,
		sc.account, meta.BoxID, meta.Name, meta.Mark, meta.Lock)
	return err
}

// ApplyBoxMoves 增量应用盒位变更(box_pet_change):把每只宠物移到新(盒,格);
// 盒名/标记随盒不随宠,取自 pet_boxes 元数据(增量包不携带,移入空盒也能拿到盒名),
// 元数据缺失(旧库)才回退取该盒任一既有宠物行;宠物入盒即不在队伍,清除其队伍位置。
func (sc *Scoped) ApplyBoxMoves(entries []pet.BoxEntry) error {
	tx, err := sc.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	up, err := tx.Prepare(`INSERT OR REPLACE INTO pet_box(account,gid,box_id,slot,box_name,mark) VALUES(?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer up.Close()
	for _, e := range entries {
		var name string
		var mark int32
		if tx.QueryRow(`SELECT name,mark FROM pet_boxes WHERE account=? AND box_id=?`, sc.account, e.BoxID).Scan(&name, &mark) != nil {
			tx.QueryRow(`SELECT box_name,mark FROM pet_box WHERE account=? AND box_id=? AND gid<>? LIMIT 1`, sc.account, e.BoxID, e.Gid).Scan(&name, &mark)
		}
		if _, err = up.Exec(sc.account, e.Gid, e.BoxID, e.Slot, name, mark); err != nil {
			return err
		}
		if _, err = tx.Exec(`DELETE FROM pet_team WHERE account=? AND gid=?`, sc.account, e.Gid); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// BoxLayout 是一个盒子的槽位布局(30 格,gid=0 表示空)。
type BoxLayout struct {
	ID    int32             `json:"id"`
	Name  string            `json:"name"`
	Slots []uint32          `json:"slots"`           // 长 30,下标=格位(0 起),值=宠物 gid(0 空)
	Heads map[string]string `json:"heads,omitempty"` // gid(字符串)→小头像路径,供示意图渲染头像
}

// BoxLayouts 返回本账号全部盒子的槽位布局(按 box_id/展示位置升序),供前端盒子示意图。
// 盒子全集与盒名取自 pet_boxes 元数据(含空盒),占用格从 pet_box 填入;两表都缺时返回空。
// 无 pet_boxes 元数据(旧库尚未刷新)时回退按 pet_box 里出现过的盒号构造(仅有宠物的盒)。
func (sc *Scoped) BoxLayouts() []BoxLayout {
	m := map[int32]*BoxLayout{}
	ensure := func(id int32) *BoxLayout {
		bl := m[id]
		if bl == nil {
			bl = &BoxLayout{ID: id, Slots: make([]uint32, 30)}
			m[id] = bl
		}
		return bl
	}
	// 盒子全集 + 盒名(含空盒)
	if rows, err := sc.db.Query(`SELECT box_id, name FROM pet_boxes WHERE account=?`, sc.account); err == nil {
		for rows.Next() {
			var id int32
			var name string
			if rows.Scan(&id, &name) == nil {
				ensure(id).Name = name
			}
		}
		rows.Close()
	}
	// 占用格(旧库无元数据时,盒名回退用 pet_box.box_name)
	if rows, err := sc.db.Query(`SELECT box_id, slot, gid, box_name FROM pet_box WHERE account=?`, sc.account); err == nil {
		for rows.Next() {
			var boxID, slot int32
			var gid uint32
			var name string
			if rows.Scan(&boxID, &slot, &gid, &name) != nil {
				continue
			}
			bl := ensure(boxID)
			if bl.Name == "" && name != "" {
				bl.Name = name
			}
			if slot >= 0 && slot < 30 {
				bl.Slots[slot] = gid
			}
		}
		rows.Close()
	}
	order := make([]int32, 0, len(m))
	for id := range m {
		order = append(order, id)
	}
	sort.Slice(order, func(i, j int) bool { return order[i] < order[j] })
	// 全盒占用格的头像一次查出(替代每盒一次 petHeads),再按盒分发各自 gid 的头像。
	var allGids []uint32
	for _, bl := range m {
		allGids = append(allGids, occupiedGids(bl.Slots)...)
	}
	heads := sc.petHeads(allGids)
	out := make([]BoxLayout, 0, len(order))
	for _, id := range order {
		bl := m[id]
		for _, g := range occupiedGids(bl.Slots) {
			if k := strconv.FormatUint(uint64(g), 10); heads[k] != "" {
				if bl.Heads == nil {
					bl.Heads = map[string]string{}
				}
				bl.Heads[k] = heads[k]
			}
		}
		out = append(out, *bl)
	}
	return out
}

// TeamLayout 是大世界三支队伍的位置布局(18 格 = 3 队 × 6 位,下标=team_idx*6+pos)。
type TeamLayout struct {
	Slots []uint32          `json:"slots"`
	Heads map[string]string `json:"heads,omitempty"` // gid(字符串)→小头像路径
}

// TeamLayouts 返回本账号大世界队伍的 18 格布局(gid=0 表示空位)。
func (sc *Scoped) TeamLayouts() TeamLayout {
	tl := TeamLayout{Slots: make([]uint32, 18)}
	rows, err := sc.db.Query(`SELECT team_idx, pos, gid FROM pet_team WHERE account=?`, sc.account)
	if err != nil {
		return tl
	}
	defer rows.Close()
	for rows.Next() {
		var ti, pos int32
		var gid uint32
		if rows.Scan(&ti, &pos, &gid) == nil {
			if idx := ti*6 + pos; idx >= 0 && idx < 18 {
				tl.Slots[idx] = gid
			}
		}
	}
	tl.Heads = sc.petHeads(occupiedGids(tl.Slots))
	return tl
}

// occupiedGids 取槽位数组里的非零 gid。
func occupiedGids(slots []uint32) []uint32 {
	var out []uint32
	for _, g := range slots {
		if g != 0 {
			out = append(out, g)
		}
	}
	return out
}

// petHeads 批量读取本账号一组 gid 的小头像路径(image.head);空集或无图忽略。
func (sc *Scoped) petHeads(gids []uint32) map[string]string {
	if len(gids) == 0 {
		return nil
	}
	ph := make([]string, len(gids))
	args := make([]any, 0, len(gids)+1)
	args = append(args, sc.account)
	for i, g := range gids {
		ph[i] = "?"
		args = append(args, g)
	}
	rows, err := sc.db.Query(`SELECT gid,data FROM pets WHERE account=? AND gid IN (`+strings.Join(ph, ",")+`)`, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var gid uint32
		var data string
		if rows.Scan(&gid, &data) != nil {
			continue
		}
		var p pet.Pet
		if json.Unmarshal([]byte(data), &p) == nil && p.Image.Head != "" {
			out[strconv.FormatUint(uint64(gid), 10)] = p.Image.Head
		}
	}
	return out
}

// boxLocFor 读取本账号单只宠物的盒子位置(无则 nil),供 GetPet 注入。
// 盒号/格位取自 pet_box;盒名/标记优先取 pet_boxes 权威元数据(移入空盒也准),缺失才回退 pet_box。
func (sc *Scoped) boxLocFor(gid uint32) *pet.PetBoxLoc {
	var boxID, slot, mark int32
	var name string
	err := sc.db.QueryRow(`SELECT box_id,slot,box_name,mark FROM pet_box WHERE account=? AND gid=?`, sc.account, gid).Scan(&boxID, &slot, &name, &mark)
	if err != nil {
		return nil
	}
	var mName string
	var mMark int32
	if sc.db.QueryRow(`SELECT name,mark FROM pet_boxes WHERE account=? AND box_id=?`, sc.account, boxID).Scan(&mName, &mMark) == nil {
		name, mark = mName, mMark
	}
	return &pet.PetBoxLoc{BoxID: boxID, Slot: slot, BoxName: name, Mark: pet.MarkName(mark)}
}

// teamLocFor 读取本账号单只宠物的队伍位置(无则 nil),供 GetPet 注入。
func (sc *Scoped) teamLocFor(gid uint32) *pet.PetTeamLoc {
	var teamIdx, pos int32
	if sc.db.QueryRow(`SELECT team_idx,pos FROM pet_team WHERE account=? AND gid=?`, sc.account, gid).Scan(&teamIdx, &pos) != nil {
		return nil
	}
	return &pet.PetTeamLoc{TeamIdx: teamIdx, Pos: pos}
}

// gidIn 为一组 gid 拼出 `gid IN (?,?,…)` 片段及绑定参数(account 置首)。
func (sc *Scoped) gidIn(gids []uint32) (string, []any) {
	ph := make([]string, len(gids))
	args := make([]any, 0, len(gids)+1)
	args = append(args, sc.account)
	for i, g := range gids {
		ph[i] = "?"
		args = append(args, g)
	}
	return "gid IN (" + strings.Join(ph, ",") + ")", args
}

// batchBoxLocs 批量读取一组 gid 的盒位(与 boxLocFor 同语义:盒名/标记优先取 pet_boxes 权威
// 元数据,缺失才回退 pet_box);无盒位的 gid 不入结果。
func (sc *Scoped) batchBoxLocs(gids []uint32) map[uint32]*pet.PetBoxLoc {
	out := map[uint32]*pet.PetBoxLoc{}
	if len(gids) == 0 {
		return out
	}
	// 先取全量盒子元数据(小,含空盒),供逐行覆盖盒名/标记。
	meta := map[int32][2]int32{} // 仅记 mark;name 另存
	metaName := map[int32]string{}
	if rows, err := sc.db.Query(`SELECT box_id,name,mark FROM pet_boxes WHERE account=?`, sc.account); err == nil {
		for rows.Next() {
			var id, mark int32
			var name string
			if rows.Scan(&id, &name, &mark) == nil {
				meta[id] = [2]int32{mark}
				metaName[id] = name
			}
		}
		rows.Close()
	}
	in, args := sc.gidIn(gids)
	rows, err := sc.db.Query(`SELECT gid,box_id,slot,box_name,mark FROM pet_box WHERE account=? AND `+in, args...)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var gid uint32
		var boxID, slot, mark int32
		var name string
		if rows.Scan(&gid, &boxID, &slot, &name, &mark) != nil {
			continue
		}
		if _, ok := meta[boxID]; ok { // pet_boxes 元数据存在则覆盖(移入空盒也准)
			name, mark = metaName[boxID], meta[boxID][0]
		}
		out[gid] = &pet.PetBoxLoc{BoxID: boxID, Slot: slot, BoxName: name, Mark: pet.MarkName(mark)}
	}
	return out
}

// batchTeamLocs 批量读取一组 gid 的大世界队伍位置;无队位的 gid 不入结果。
func (sc *Scoped) batchTeamLocs(gids []uint32) map[uint32]*pet.PetTeamLoc {
	out := map[uint32]*pet.PetTeamLoc{}
	if len(gids) == 0 {
		return out
	}
	in, args := sc.gidIn(gids)
	rows, err := sc.db.Query(`SELECT gid,team_idx,pos FROM pet_team WHERE account=? AND `+in, args...)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var gid uint32
		var teamIdx, pos int32
		if rows.Scan(&gid, &teamIdx, &pos) == nil {
			out[gid] = &pet.PetTeamLoc{TeamIdx: teamIdx, Pos: pos}
		}
	}
	return out
}

// medalsFor 读取本账号单只宠物拥有的奖牌 id 列表(升序),供 GetPet 注入。
func (sc *Scoped) medalsFor(gid uint32) []uint32 {
	rows, err := sc.db.Query(`SELECT medal_id FROM pet_medal WHERE account=? AND gid=? ORDER BY medal_id`, sc.account, gid)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []uint32
	for rows.Next() {
		var id uint32
		if rows.Scan(&id) == nil {
			out = append(out, id)
		}
	}
	return out
}
