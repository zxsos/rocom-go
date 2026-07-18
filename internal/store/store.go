// Package store 用 SQLite(纯 Go 驱动)持久化宠物当前状态与事件历史,并支持筛选查询。
// 文件按领域划分:session(连接会话缓存)/ account / pet / location(盒子·队伍·奖牌)/
// event / star(眠枭之星)/ query(筛选查询)。
package store

import (
	"database/sql"

	_ "modernc.org/sqlite"

	"github.com/whoisnian/rocom-capture/internal/gamedata"
)

// Store 封装 SQLite 连接。跨账号操作(migrate/accounts/sessions/star 表)挂在此。
// gd 用于在写入时把身高/体重换算成形态内百分位并落列,支撑跨种族的百分位排序。
type Store struct {
	db *sql.DB
	gd *gamedata.DB
}

// Scoped 是绑定了某个 account 的 Store 视图:所有按账号隔离的读写都经它进行,
// account 由 For 注入,方法内部不再显式接收 account,避免漏传导致跨账号串数据。
type Scoped struct {
	db      *sql.DB
	gd      *gamedata.DB
	account string
}

// For 返回绑定指定 account 的视图。
func (s *Store) For(account string) *Scoped { return &Scoped{db: s.db, gd: s.gd, account: account} }

// New 打开(或创建)数据库并建表。gd 供写入时计算身高/体重百分位排序列。
func New(path string, gd *gamedata.DB) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // SQLite 写入串行化,避免 database is locked
	// 性能:默认 rollback 日志 + synchronous=FULL 会对每次自动提交 fsync,登录后全量宠物分页
	// 逐只 UpsertPet(数百次独立提交)时整轮拖到近 10s,处理速度赶不上抓包到达速度而积压。
	// 改 WAL + synchronous=NORMAL:提交不再逐次 fsync(仅 checkpoint 时落盘),被动抓包库
	// 即便宕机最多丢尾部若干条、可经下次登录快照重建,该取舍安全。busy_timeout 兜底。
	for _, pragma := range []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA synchronous=NORMAL`,
		`PRAGMA busy_timeout=5000`,
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, err
		}
	}
	s := &Store{db: db, gd: gd}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS pets (
  account TEXT NOT NULL,
  gid INTEGER,
  conf_id INTEGER, species TEXT, name TEXT, level INTEGER,
  nature_id INTEGER, nature TEXT, gender TEXT, types TEXT,
  height REAL, weight REAL, voice INTEGER,
  talent_rank TEXT, medal TEXT, medal_id INTEGER, partner_mark TEXT,
  speciality TEXT, speciality_id INTEGER,
  catch_time INTEGER, shiny INTEGER, colorful INTEGER,
  hp INTEGER, attack INTEGER, defense INTEGER,
  sp_attack INTEGER, sp_defense INTEGER, speed INTEGER,
  form TEXT, egg_groups TEXT,
  data TEXT, updated_at INTEGER,
  PRIMARY KEY(account, gid)
);
CREATE INDEX IF NOT EXISTS idx_pets_species ON pets(species);
CREATE INDEX IF NOT EXISTS idx_pets_level ON pets(level);
CREATE INDEX IF NOT EXISTS idx_pets_form ON pets(form);
CREATE TABLE IF NOT EXISTS events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  account TEXT NOT NULL,
  time INTEGER, sub_kind TEXT, gid INTEGER,
  species TEXT, nature TEXT, medal TEXT, shiny INTEGER,
  data TEXT
);
CREATE INDEX IF NOT EXISTS idx_events_account_time ON events(account, time);
CREATE TABLE IF NOT EXISTS pet_box (
  account TEXT NOT NULL, gid INTEGER,
  box_id INTEGER, slot INTEGER, box_name TEXT, mark INTEGER,
  PRIMARY KEY(account, gid)
);
CREATE TABLE IF NOT EXISTS pet_boxes (
  account TEXT NOT NULL, box_id INTEGER,
  name TEXT, mark INTEGER, lock INTEGER,
  PRIMARY KEY(account, box_id)
);
CREATE TABLE IF NOT EXISTS pet_team (
  account TEXT NOT NULL, gid INTEGER,
  team_idx INTEGER, pos INTEGER,
  PRIMARY KEY(account, gid)
);
CREATE TABLE IF NOT EXISTS pet_medal (
  account TEXT NOT NULL, gid INTEGER, medal_id INTEGER,
  PRIMARY KEY(account, gid, medal_id)
);
CREATE TABLE IF NOT EXISTS accounts (
  account TEXT PRIMARY KEY, name TEXT, updated_at INTEGER
);
CREATE TABLE IF NOT EXISTS sessions (
  conn_id TEXT PRIMARY KEY, key BLOB, account TEXT, updated_at INTEGER
);
-- 眠枭之星的收集状态(按账号、按刷新点)。1=未收集(收到过该点的 NPC 实体),2=已收集(走近了却
-- 没有实体——已收集的星星服务器不刷,见 docs/data.md 3.4)。没有行 = 尚未确认(前端照常显示)。
CREATE TABLE IF NOT EXISTS star_state (
  account TEXT NOT NULL, refresh_id INTEGER,
  state INTEGER, updated_at INTEGER,
  PRIMARY KEY(account, refresh_id)
);
-- 服务器口径的按区域收集进度(进场景包下发);区域收满即可整片隐藏,无需逐点走到。
CREATE TABLE IF NOT EXISTS star_zone (
  account TEXT NOT NULL, camp INTEGER, npc_id INTEGER,
  got INTEGER, total INTEGER, updated_at INTEGER,
  PRIMARY KEY(account, camp, npc_id)
);
`)
	if err != nil {
		return err
	}
	// 为早于该列的旧库补列(CREATE TABLE IF NOT EXISTS 不会新增列);已存在则忽略错误。
	s.db.Exec(`ALTER TABLE sessions ADD COLUMN scene_res INTEGER`) // 实时地图:当前场景 res,供重启恢复
	s.db.Exec(`ALTER TABLE sessions ADD COLUMN home_room INTEGER`) // 家园室内房屋等级(选分层底图)
	s.db.Exec(`ALTER TABLE sessions ADD COLUMN areas TEXT`)        // 当前所在区域(area_func→area_id,选洞穴/楼层)
	s.db.Exec(`ALTER TABLE pets ADD COLUMN egg_groups TEXT`)
	// 身高/体重在当前形态取值范围内的百分位(0-100),写入时按 gamedata 计算并落列,
	// 供跨种族按「相对自身范围偏大/偏小」排序(见 buildOrder);范围缺失或旧库未回填时为
	// NULL(排序排末尾),清库重登后即补齐。
	s.db.Exec(`ALTER TABLE pets ADD COLUMN weight_pct REAL`)
	s.db.Exec(`ALTER TABLE pets ADD COLUMN height_pct REAL`)
	return nil
}

// execBatch 在一个事务里对每组参数执行同一条语句(upsert 批量写入用)。
func execBatch(db *sql.DB, query string, rows [][]any) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(query)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, r := range rows {
		if _, err := stmt.Exec(r...); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// replaceAll 在一个事务里清空本账号在 table 的所有行,再按 rows 批量插入(全量快照替换用)。
func (sc *Scoped) replaceAll(table, insertSQL string, rows [][]any) error {
	tx, err := sc.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`DELETE FROM `+table+` WHERE account=?`, sc.account); err != nil {
		return err
	}
	stmt, err := tx.Prepare(insertSQL)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, r := range rows {
		if _, err = stmt.Exec(r...); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}
