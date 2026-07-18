package store

import (
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/whoisnian/rocom-capture/internal/pet"
)

// petUpsertSQL 是单只宠物的 upsert 语句(UpsertPet 与 UpsertPets 共用,占位符顺序见 petArgs)。
const petUpsertSQL = `
INSERT INTO pets(account,gid,conf_id,species,name,level,nature_id,nature,gender,types,
  height,weight,voice,talent_rank,medal,medal_id,partner_mark,speciality,speciality_id,
  catch_time,shiny,colorful,hp,attack,defense,sp_attack,sp_defense,speed,form,egg_groups,
  weight_pct,height_pct,data,updated_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(account,gid) DO UPDATE SET
  conf_id=excluded.conf_id,species=excluded.species,name=excluded.name,level=excluded.level,
  nature_id=excluded.nature_id,nature=excluded.nature,gender=excluded.gender,types=excluded.types,
  height=excluded.height,weight=excluded.weight,voice=excluded.voice,talent_rank=excluded.talent_rank,
  medal=excluded.medal,medal_id=excluded.medal_id,partner_mark=excluded.partner_mark,
  speciality=excluded.speciality,speciality_id=excluded.speciality_id,catch_time=excluded.catch_time,
  shiny=excluded.shiny,colorful=excluded.colorful,hp=excluded.hp,attack=excluded.attack,defense=excluded.defense,
  sp_attack=excluded.sp_attack,sp_defense=excluded.sp_defense,speed=excluded.speed,form=excluded.form,
  egg_groups=excluded.egg_groups,weight_pct=excluded.weight_pct,height_pct=excluded.height_pct,
  data=excluded.data,updated_at=excluded.updated_at`

// petArgs 组装 petUpsertSQL 的绑定参数。副作用:按 gamedata 计算身高/体重百分位并落到 p 上
// (排序键;同时写入 data JSON,读取时会再刷新,无害)。
func (sc *Scoped) petArgs(p *pet.Pet, now int64) []any {
	pet.FillSizePercentile(sc.gd, p)
	data, _ := json.Marshal(p)
	types, _ := json.Marshal(p.Types)
	// 蛋组存组名 JSON 数组(与 types 同法),供 egg_groups LIKE '%"名"%' 过滤。
	eggNames := make([]string, 0, len(p.EggGroups))
	for _, g := range p.EggGroups {
		eggNames = append(eggNames, g.Name)
	}
	eggGroups, _ := json.Marshal(eggNames)
	return []any{
		sc.account, p.Gid, p.ConfID, p.Species, p.Name, p.Level, p.NatureID, p.Nature, p.Gender, string(types),
		p.HeightM, p.WeightKg, p.Voice, p.TalentRank, p.Medal, p.WearMedalConfID, p.PartnerMark,
		p.Speciality, p.SpecialityID, p.CatchTime, b2i(p.Shiny), b2i(p.Colorful),
		p.HP.Value, p.Attack.Value, p.Defense.Value, p.SpAttack.Value, p.SpDefense.Value, p.Speed.Value,
		p.Form, string(eggGroups), nullPct(p.WeightPct), nullPct(p.HeightPct), string(data), now,
	}
}

// UpsertPet 插入或更新本账号一只宠物,返回是否为新增(该账号此前无该 gid)。
func (sc *Scoped) UpsertPet(p *pet.Pet) (isNew bool, err error) {
	var one int
	err = sc.db.QueryRow(`SELECT 1 FROM pets WHERE account=? AND gid=?`, sc.account, p.Gid).Scan(&one)
	isNew = err == sql.ErrNoRows
	if err != nil && err != sql.ErrNoRows {
		return false, err
	}
	_, err = sc.db.Exec(petUpsertSQL, sc.petArgs(p, time.Now().Unix())...)
	return isNew, err
}

// UpsertPets 在单个事务里批量 upsert 一页宠物,返回其中此前不存在的 gid 集合(供调用方产生获得
// 事件)。相比逐只 UpsertPet 省去每只一次自动提交 fsync 与一次存在性 SELECT:登录全量同步
// 数百只时把提交数从每只一次压到每页一次(见 store.go WAL 说明)。空切片直接返回。
func (sc *Scoped) UpsertPets(pets []*pet.Pet) (newGids map[uint32]bool, err error) {
	newGids = map[uint32]bool{}
	if len(pets) == 0 {
		return newGids, nil
	}
	gids := make([]uint32, len(pets))
	for i, p := range pets {
		gids[i] = p.Gid
	}
	existing := sc.existingGids(gids)
	for _, g := range gids {
		if !existing[g] {
			newGids[g] = true
		}
	}

	now := time.Now().Unix()
	tx, err := sc.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(petUpsertSQL)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()
	for _, p := range pets {
		if _, err = stmt.Exec(sc.petArgs(p, now)...); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return newGids, nil
}

// existingGids 一次查询判定本账号哪些 gid 已存在(批量存在性判断,替代逐只 SELECT 1)。
func (sc *Scoped) existingGids(gids []uint32) map[uint32]bool {
	ph := make([]string, len(gids))
	args := make([]any, 0, len(gids)+1)
	args = append(args, sc.account)
	for i, g := range gids {
		ph[i] = "?"
		args = append(args, g)
	}
	out := map[uint32]bool{}
	rows, err := sc.db.Query(`SELECT gid FROM pets WHERE account=? AND gid IN (`+strings.Join(ph, ",")+`)`, args...)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var g uint32
		if rows.Scan(&g) == nil {
			out[g] = true
		}
	}
	return out
}

// nullPct 把可空百分位指针转为可绑定的 sql.NullFloat64(nil→NULL)。
func nullPct(v *float64) sql.NullFloat64 {
	if v == nil {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: *v, Valid: true}
}

// petRowTables 是一只宠物涉及的全部按 gid 分区的表(删除某宠物时须一并清理,
// 否则示意图仍把该格当作占用:灰底可点、却无头像)。
var petRowTables = []string{"pets", "pet_box", "pet_team", "pet_medal"}

// deletePetRows 删除本账号一只宠物及其盒位/队位/奖牌关联。
func (sc *Scoped) deletePetRows(gid uint32) {
	for _, table := range petRowTables {
		sc.db.Exec(`DELETE FROM `+table+` WHERE account=? AND gid=?`, sc.account, gid)
	}
}

// RemovePet 删除本账号宠物(含关联行),返回被删除的快照(若存在)。
func (sc *Scoped) RemovePet(gid uint32) (*pet.Pet, error) {
	p, err := sc.GetPet(gid)
	if err != nil || p == nil {
		return nil, err
	}
	sc.deletePetRows(gid)
	return p, nil
}

// PruneMissingPets 删除本账号中 gid 不在 keep 集合内的宠物(及其关联行),返回被删除的 gid。
// 用于分页宠物列表全量下发后对账:玩家在别处放生/赠送的宠物不会出现在快照里,若不清除则会
// 以"位置待同步"残留在列表(登录快照只做增改、从不删)。
// before 为本轮对账开始时刻:仅清除此前就已存在(updated_at < before)的宠物,从而放过对账
// 期间刚捕获/更新(updated_at≥before)却未落入快照的宠物,避免误删刚入库的新宠。
func (sc *Scoped) PruneMissingPets(keep map[uint32]bool, before int64) ([]uint32, error) {
	// 先收齐待删 gid 再执行删除:SetMaxOpenConns(1) 下遍历结果集时嵌套写会死锁。
	rows, err := sc.db.Query(`SELECT gid FROM pets WHERE account=? AND (updated_at IS NULL OR updated_at < ?)`, sc.account, before)
	if err != nil {
		return nil, err
	}
	var stale []uint32
	for rows.Next() {
		var g uint32
		if rows.Scan(&g) == nil && !keep[g] {
			stale = append(stale, g)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(stale) == 0 {
		return stale, nil
	}
	// 单事务内清理待删宠物的全部关联行(逐只 4 表 × 独立提交 → 一次提交)。
	tx, err := sc.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	for _, table := range petRowTables {
		stmt, err := tx.Prepare(`DELETE FROM ` + table + ` WHERE account=? AND gid=?`)
		if err != nil {
			return nil, err
		}
		for _, g := range stale {
			if _, err := stmt.Exec(sc.account, g); err != nil {
				stmt.Close()
				return nil, err
			}
		}
		stmt.Close()
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return stale, nil
}

// GetPet 按 gid 返回本账号宠物(含盒位/队位/拥有奖牌)。
func (sc *Scoped) GetPet(gid uint32) (*pet.Pet, error) {
	var data string
	err := sc.db.QueryRow(`SELECT data FROM pets WHERE account=? AND gid=?`, sc.account, gid).Scan(&data)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var p pet.Pet
	if err := json.Unmarshal([]byte(data), &p); err != nil {
		return nil, err
	}
	p.Box = sc.boxLocFor(gid)
	p.Team = sc.teamLocFor(gid)
	if ms := sc.medalsFor(gid); ms != nil {
		p.MedalIDs = ms
	}
	return &p, nil
}
