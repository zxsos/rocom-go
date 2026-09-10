package store

import (
	"database/sql"
	"encoding/json"

	"github.com/whoisnian/rocom-capture/internal/pet"
)

// 培育线的持久化(培育页,见 internal/pet/breeding.go)。按 account 隔离,主键 (account, id)。
//
// 与 eggs 表同一套取舍:整条线(含所有代数)序列化进 data 列,投影列只用来做列表的
// 筛选与排序。一条线的代数上限天然很小(几十代),整条读写的代价远低于拆表的 JOIN。
//
// 双亲与子代都存 **EggParent 快照**,不引用 pets 表:亲本之后被放生/送人(pets 行删除)
// 不影响这条培育史 —— 培育看的正是「当初用过的那几只」,不是「现在还活着谁」。

// UpsertBreedingLine 写入/更新一条培育线。
//
// 投影列(代数、最佳嗓音、最佳体重)由 pet.LineStats 从代数里算出来写进去,列表页据此排序,
// 不必把每条线的 JSON 都解开;created_at 只在首次写入时给,更新不动它。
func (sc *Scoped) UpsertBreedingLine(l *pet.BreedingLine) error {
	if l == nil || l.ID == "" {
		return nil
	}
	data, err := json.Marshal(l)
	if err != nil {
		return err
	}
	gens, bestVoice, bestWeight := pet.LineStats(l)
	var tv, bv any
	if l.Goal.Voice != nil {
		tv = *l.Goal.Voice
	}
	if bestVoice != nil {
		bv = *bestVoice
	}
	var tw, bw any
	if l.Goal.WeightPct != nil {
		tw = *l.Goal.WeightPct
	}
	if bestWeight != nil {
		bw = *bestWeight
	}
	var nature any
	if l.Goal.Nature != "" {
		nature = l.Goal.Nature
	}
	if l.Status == "" {
		l.Status = pet.BreedingActive
	}
	_, err = sc.db.Exec(
		`INSERT INTO breeding_line
		   (account, id, species, evo, conf_id, target_voice, target_weight_pct, target_nature,
		    status, gen_count, best_voice, best_weight_pct, created_at, updated_at, data)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(account, id) DO UPDATE SET
		   species=excluded.species, evo=excluded.evo, conf_id=excluded.conf_id,
		   target_voice=excluded.target_voice, target_weight_pct=excluded.target_weight_pct,
		   target_nature=excluded.target_nature, status=excluded.status,
		   gen_count=excluded.gen_count, best_voice=excluded.best_voice,
		   best_weight_pct=excluded.best_weight_pct,
		   updated_at=excluded.updated_at, data=excluded.data`,
		sc.account, l.ID, l.Species, l.Evo, l.ConfID, tv, tw, nature,
		l.Status, gens, bv, bw, l.CreatedAt, l.UpdatedAt, string(data))
	return err
}

// fillLineChain 给从库里读出的线补上品种身份(进化链 id):老线只存了 species,读出来时按
// 名字推导一次(见 pet.DeriveChain)。放在这里是为了让三个读入口共用一处 —— 少补一处,
// 那一处看到的线就像「没有品种」的线,而它偏偏是跨版本升级时才出现的状态。
func (sc *Scoped) fillLineChain(l *pet.BreedingLine) { pet.DeriveChain(sc.gd, l) }

// ListBreedingLines 返回本账号全部培育线(更新时间新的在前)。
func (sc *Scoped) ListBreedingLines() ([]*pet.BreedingLine, error) {
	rows, err := sc.rdb.Query(
		`SELECT data FROM breeding_line WHERE account=? ORDER BY updated_at DESC, id`, sc.account)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*pet.BreedingLine
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			continue
		}
		var l pet.BreedingLine
		if json.Unmarshal([]byte(data), &l) != nil {
			continue
		}
		sc.fillLineChain(&l) // 老线按名字补出品种身份(见 fillLineChain)
		out = append(out, &l)
	}
	return out, rows.Err()
}

// GetBreedingLine 按 id 取一条线;没找到返回 nil(不是错误 —— 建线前先查一次是常态)。
func (sc *Scoped) GetBreedingLine(id string) (*pet.BreedingLine, error) {
	var data string
	err := sc.rdb.QueryRow(
		`SELECT data FROM breeding_line WHERE account=? AND id=?`, sc.account, id).Scan(&data)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var l pet.BreedingLine
	if err := json.Unmarshal([]byte(data), &l); err != nil {
		return nil, err
	}
	sc.fillLineChain(&l) // 老线按名字补出品种身份(见 fillLineChain)
	return &l, nil
}

// FindActiveBreedingLine 按**品种**取一条进行中的培育线(最近更新的那条);没有返回 nil。
//
// 破壳自动记一代时用它定位该记到哪条线上(见 pipeline/breeding.go)。同一品种可能有多条线
// (目标不同,如一只刷嗓音、一只刷体重),取最近更新的那条 —— 玩家刚动过的那条就是他此刻在推的。
//
// 为什么不在 SQL 里按 species/evo 筛:品种的口径是「进化链」(见 pet.ChainRef),一条链有
// 好几个形态名,老线还只存着名字 —— 这段推导必须与匹配共用同一份实现(ChainRef.Match),
// SQL 里再写一套 WHERE 早晚分叉,而分叉的表现是「破壳后又另开一条新线」,玩家得自己发现。
// 一个账号的线通常个位数,整份读进来逐条比的开销可忽略(每条新蛋孵出时才走一次)。
func (sc *Scoped) FindActiveBreedingLine(ref pet.ChainRef) (*pet.BreedingLine, error) {
	if ref.Empty() {
		return nil, nil
	}
	rows, err := sc.rdb.Query(
		`SELECT data FROM breeding_line WHERE account=? AND status=? ORDER BY updated_at DESC, id`,
		sc.account, pet.BreedingActive)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var data string
		if rows.Scan(&data) != nil {
			continue
		}
		var l pet.BreedingLine
		if json.Unmarshal([]byte(data), &l) != nil {
			continue
		}
		sc.fillLineChain(&l) // 老线按名字补出品种身份(见 fillLineChain)
		if pet.ChainRefOf(sc.gd, l.Evo, l.Species).Same(ref) {
			return &l, nil
		}
	}
	return nil, rows.Err()
}

// FindLineClaimingChild 找**正在等这只宠**的培育线:它的待认领里有一代记着这个子代 gid。
//
// 给认领的**慢路径**用(见 pipeline.claimHatchedChild):连接不在时(离线回放、进程重启、
// 玩家在别的设备上孵的蛋)手头只剩「这只宠物是谁」,而 gid 早在破壳那一刻就随那一代落了库,
// 按它反查即可 —— 这正是「关联要落库而不是留在内存」换来的好处。
//
// 用 SQL 的 json_each 直接钻进 data 里筛,而不是把线全读出来在 Go 侧找:宠物入库是高频
// 消息(登录一次几百只),每条都把该账号的线全解一遍是白花钱;而这条查询一次就命中
// (一个账号的线通常个位数,且绝大多数没有待认领的代)。
//
// 没找到返回 nil(不是错误 —— 绝大多数新宠物都不是刚孵出来的)。
func (sc *Scoped) FindLineClaimingChild(childGid uint32) (*pet.BreedingLine, error) {
	if childGid == 0 {
		return nil, nil
	}
	var data string
	err := sc.rdb.QueryRow(
		`SELECT data FROM breeding_line
		  WHERE account=? AND EXISTS (
		        SELECT 1 FROM json_each(data, '$.pending')
		         WHERE json_extract(value, '$.childGid') = ?)
		  ORDER BY updated_at DESC, id LIMIT 1`,
		sc.account, childGid).Scan(&data)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var l pet.BreedingLine
	if err := json.Unmarshal([]byte(data), &l); err != nil {
		return nil, err
	}
	sc.fillLineChain(&l)
	return &l, nil
}

// DeleteBreedingLine 删一条线(连同它的全部代数 —— 代数就在同一行里,没有级联)。
func (sc *Scoped) DeleteBreedingLine(id string) error {
	_, err := sc.db.Exec(`DELETE FROM breeding_line WHERE account=? AND id=?`, sc.account, id)
	return err
}
