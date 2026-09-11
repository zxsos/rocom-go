package store

import "time"

// 学院小窝:全库**唯一**一只宠物(单行表 id=1,范式同 merchant_source / egg_source)。
//
// 为什么是全局一行而不是按账号一份:小窝在游戏里就是**唯一的一个**,玩家把哪只放进去就是
// 哪只 —— 它不是某个账号的属性。切账号后这个 gid 落在别的账号的库里,谁都对不上,等于空着;
// 那也正是真相(那只不归这个账号,配不出来),故不做隔离、也不清空。
//
// 玩法(实现在 internal/pet/breeding.go 的 nestNature):小窝里那只参与孵蛋时,子代性格
// **100% 随它** —— 无论它是种母还是种公,也无论目标填的是单个性格还是「正面加某维」那组。

// AcademyGid 返回小窝里那只的 gid;小窝空着(或从没设过)时返回 0。
func (s *Store) AcademyGid() uint32 {
	var gid uint32
	// 读取失败(表不存在 / 无该行)按「小窝空着」处理:这张表是后加的,老库在下次写入前
	// 没有这一行属正常,不该让整个培育页读不出来。
	_ = s.rdb.QueryRow(`SELECT gid FROM academy_nest WHERE id=1`).Scan(&gid)
	return gid
}

// SetAcademyGid 把 gid 那只放进小窝(0 = 把小窝空出来)。同一时刻只可能有一只:
// 全表就一行,换一只即覆盖 —— 「小窝只有一个」这件事由结构本身保证,不靠调用方自觉。
func (s *Store) SetAcademyGid(gid uint32) error {
	_, err := s.db.Exec(`INSERT INTO academy_nest(id, gid, updated_at) VALUES(1,?,?)
		ON CONFLICT(id) DO UPDATE SET gid=excluded.gid, updated_at=excluded.updated_at`,
		gid, time.Now().Unix())
	return err
}
