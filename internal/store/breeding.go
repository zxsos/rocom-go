package store

import (
	"database/sql"
	"encoding/json"

	"github.com/zxsos/roco-go/internal/pet"
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
//
// ⚠️ 「最佳」的口径随 pet.LineStats 走(达标优先,其次离目标最近,见其注释)。它是**写入时**
// 算下的快照,语义变了老行不会自动重算 —— 与 DeriveChain / MotherGidOf 的「读取时补、不迁移」
// 一致,下次保存这条线时自然收敛(它只影响列表排序,短暂不一致无碍)。
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

// ReparentLines 把以 oldParent 为母线的线改挂到 newParent 上(合并子线时用,见
// pet.MergeChildLine)。
//
// 为什么必须有:并入母线之后子线就被删掉了,孙辈若还指着它,谱系视图就从那一代**断掉** ——
// 而它们本该接着母线的历史往下走。
//
// 只能读出整条再写回:parentLineId 在 data 这块 JSON 里,没有单独的列可 UPDATE(按约定
// 表结构不动)。合并不常发生,且要改的通常只有一两条,这点开销无所谓。
func (sc *Scoped) ReparentLines(oldParent, newParent string) error {
	if oldParent == "" || newParent == "" || oldParent == newParent {
		return nil
	}
	lines, err := sc.ListBreedingLines()
	if err != nil {
		return err
	}
	for _, l := range lines {
		if l == nil || l.ParentLineID != oldParent {
			continue
		}
		l.ParentLineID = newParent
		if err := sc.UpsertBreedingLine(l); err != nil {
			return err
		}
	}
	return nil
}

// FindLineForMother 收蛋时定位这颗蛋该记到哪条线,**一次扫描同时回答两件事**:
//
//   - hit:当前固定在这只种母身上的进行中的线;nil = 没有,调用方据此开新线。
//   - parent:这只种母是**谁孵出来的**(某条线的历代子代里有它);nil = 不是任何线孵的。
//
// 为什么按种母而不是按品种:蛋趴在谁的窝上是**确定的事实**,而「同品种最近更新的那条线」
// 是猜的 —— 同一个品种同时开几个窝、几只母本各孵各的时,那几颗蛋会被全记进一条线,而且
// 记到哪条还会随 updated_at 漂移(玩家在另一条上改一下目标,归宿就变了)。
//
// parent 决定新线是「子线」还是「独立线」:
//   - parent != nil → 本线子代接班当种母(选育的典型操作),开**子线**接在 parent 之后;
//   - parent == nil → 野外抓来的、与任何线都没有血缘,开**独立新线**。
//     这种不能去猜挂谁 —— 5 条同品种的线里挑一条就是猜,猜错是把一颗蛋记进别人的培育史。
//
// 老线没有 MotherGid 字段,由 pet.MotherGidOf 从末代派生(见它),故老数据无需迁移。
// 连派生都派生不出的(手工建的、一代都没有的线)才按品种兜底 —— 那种线还没接过蛋,
// 归谁都谈不上历史,取最近更新过的一条即可。
//
// 为什么不在 SQL 里筛:种母的口径含「老线按末代派生」这段推导,必须与别处共用同一份实现
// (pet.MotherGidOf),SQL 里再写一套 WHERE 早晚分叉。一个账号的线通常个位数,整份读进来
// 逐条比的开销可忽略(每颗蛋收进窝时才走一次)。
func (sc *Scoped) FindLineForMother(motherGid uint32, ref pet.ChainRef) (hit, parent *pet.BreedingLine, err error) {
	if ref.Empty() || motherGid == 0 {
		return nil, nil, nil
	}
	rows, err := sc.rdb.Query(
		`SELECT data FROM breeding_line WHERE account=? AND status=? ORDER BY updated_at DESC, id`,
		sc.account, pet.BreedingActive)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var fallback *pet.BreedingLine // 派生不出种母的空线,按品种兜底
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
		if mg := pet.MotherGidOf(&l); mg != 0 {
			if mg == motherGid {
				// ① 精确命中:这条线就固定在她身上,parent 不必再找
				return &l, nil, nil
			}
		} else if fallback == nil && pet.ChainRefOf(sc.gd, l.Evo, l.Species).Same(ref) {
			fallback = &l
		}
		if parent == nil && pet.DescendantGids(&l)[motherGid] {
			parent = &l
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	return fallback, parent, nil
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
