package store

import "time"

// FlowerChallengeKey 标识一个花种品种:同种花种可同时存在多只,按 npc_cfg_id + blood(血脉)区分。
// 挑战计数挂在品种上而非个体 npc_logic_id 上——个体随挑战/刷新消失,计数不随之丢失,
// 下次同品种花种出现时卡片继续累计显示。
type FlowerChallengeKey struct {
	NpcCfgID uint32
	Blood    uint32
}

// AddFlowerChallenge 记录一次花种挑战(按账号 + 花种品种累计,重启不丢)。
// 计数点:c2s 0x034E 选中花种进入战斗,一次挑战一条,量小(手动操作驱动),直接 UPSERT。
func (sc *Scoped) AddFlowerChallenge(npcCfgID, blood uint32) error {
	_, err := sc.db.Exec(`INSERT INTO flower_challenges(account,npc_cfg_id,blood,count,updated_at)
VALUES(?,?,?,1,?)
ON CONFLICT(account,npc_cfg_id,blood) DO UPDATE SET count=count+1, updated_at=excluded.updated_at`,
		sc.account, npcCfgID, blood, time.Now().Unix())
	return err
}

// FlowerChallengeCounts 返回本账号全部花种品种的累计挑战次数。
// 行数少(品种 = 配置 id × 血脉组合,几十行),每次 0x0375 整组下发全量查一次,内存聚合。
func (sc *Scoped) FlowerChallengeCounts() (map[FlowerChallengeKey]int, error) {
	rows, err := sc.rdb.Query(`SELECT npc_cfg_id, blood, count FROM flower_challenges WHERE account=?`, sc.account)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[FlowerChallengeKey]int{}
	for rows.Next() {
		var k FlowerChallengeKey
		var n int
		if err := rows.Scan(&k.NpcCfgID, &k.Blood, &n); err != nil {
			return nil, err
		}
		out[k] = n
	}
	return out, rows.Err()
}

// UpsertFlowerEndTs 记录某花种品种的活动结束时间(0x0375 每次整组下发都刷新最新值)。
// 只更新 end_ts 与 updated_at,count 保留——活动进行中的品种计数持续累计,
// 到期后由 DeleteFlowerChallenge / DeleteExpiredFlowerChallenges 清理。
func (sc *Scoped) UpsertFlowerEndTs(npcCfgID, blood uint32, endTs int64) error {
	_, err := sc.db.Exec(`INSERT INTO flower_challenges(account,npc_cfg_id,blood,count,end_ts,updated_at)
VALUES(?,?,?,0,?,?)
ON CONFLICT(account,npc_cfg_id,blood) DO UPDATE SET end_ts=excluded.end_ts, updated_at=excluded.updated_at`,
		sc.account, npcCfgID, blood, endTs, time.Now().Unix())
	return err
}

// DeleteFlowerChallenge 删除某花种品种的挑战计数:活动结束(end_ts 已过)后调用,
// 计数清零,下次新活动同品种花种出现时从 0 重新累计。
func (sc *Scoped) DeleteFlowerChallenge(npcCfgID, blood uint32) error {
	_, err := sc.db.Exec(`DELETE FROM flower_challenges WHERE account=? AND npc_cfg_id=? AND blood=?`,
		sc.account, npcCfgID, blood)
	return err
}

// DeleteExpiredFlowerChallenges 删除全部账号中活动已结束(end_ts 非 0 且早于 now)的花种计数。
// 由 pipeline 兜底扫描调用:活动结束后花种从分组消失,0x0375 不再触发实时删除,靠这里兜底。
func (s *Store) DeleteExpiredFlowerChallenges(now int64) error {
	_, err := s.db.Exec(`DELETE FROM flower_challenges WHERE end_ts <> 0 AND end_ts < ?`, now)
	return err
}
