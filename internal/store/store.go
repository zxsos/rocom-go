// Package store 用 SQLite(纯 Go 驱动)持久化宠物当前状态与事件历史,并支持筛选查询。
// 文件按领域划分:session(连接会话缓存)/ account / pet / location(盒子·队伍·奖牌)/
// event / star(眠枭之星)/ query(筛选查询)/ flower(花种挑战)。
package store

import (
	"database/sql"
	"log"
	"net/url"
	"runtime"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/whoisnian/rocom-capture/internal/gamedata"
)

// Store 封装 SQLite 连接。跨账号操作(建表/accounts/sessions/star 表)挂在此。
// gd 用于在写入时把身高/体重换算成形态内百分位并落列,支撑跨种族的百分位排序。
//
// 读写分两个连接池(见 New):db 只写(单连接串行),rdb 只读(多连接并发)。
// 约定:Exec/Begin 走 db,Query/QueryRow 走 rdb。
type Store struct {
	db  *sql.DB
	rdb *sql.DB
	gd  *gamedata.DB

	rules *ruleCache // 账号黑白名单内存缓存(见 rule.go),读写并发安全
}

// Scoped 是绑定了某个 account 的 Store 视图:所有按账号隔离的读写都经它进行,
// account 由 For 注入,方法内部不再显式接收 account,避免漏传导致跨账号串数据。
type Scoped struct {
	db      *sql.DB
	rdb     *sql.DB
	gd      *gamedata.DB
	account string
}

// For 返回绑定指定 account 的视图。
func (s *Store) For(account string) *Scoped {
	return &Scoped{db: s.db, rdb: s.rdb, gd: s.gd, account: account}
}

// maxReadConns 是只读池的连接数上限:读多是 CPU 密集(扫行 + JSON 编解码),开到超过核数
// 只会徒增争用。网关一类弱机核少,按核数自适应即可。
func maxReadConns() int {
	return min(max(runtime.NumCPU(), 2), 8)
}

// dsn 把库路径拼成带 pragma 的 file: URI。pragma 必须走 DSN 而不能事后 db.Exec:
// database/sql 的 Exec 只落在池里某一条连接上,多连接的池里其余连接拿不到设置。
func dsn(path string, pragmas ...string) string {
	q := url.Values{}
	for _, p := range pragmas {
		q.Add("_pragma", p)
	}
	// "file:rocom.db" / "file:/var/lib/rocom.db",相对与绝对路径都成立。
	return "file:" + (&url.URL{Path: path}).EscapedPath() + "?" + q.Encode()
}

// New 打开(或创建)数据库并建表。gd 供写入时计算身高/体重百分位排序列。
func New(path string, gd *gamedata.DB) (*Store, error) {
	// 性能:默认 rollback 日志 + synchronous=FULL 会对每次自动提交 fsync,登录后全量宠物分页
	// 逐只 UpsertPet(数百次独立提交)时整轮拖到近 10s,处理速度赶不上抓包到达速度而积压。
	// 改 WAL + synchronous=NORMAL:提交不再逐次 fsync(仅 checkpoint 时落盘),被动抓包库
	// 即便宕机最多丢尾部若干条、可经下次登录快照重建,该取舍安全。busy_timeout 兜底。
	// wal_autocheckpoint 调大:默认 1000 页(约 4MB)时 SQLite 自动 checkpoint,在弱机/慢磁盘上
	// 一次性刷几 MB 是秒级停顿,且恰好落在高频抓包时段。调大后自动 checkpoint 基本不再触发,
	// 改由后台 goroutine(checkpointLoop)主动错峰执行 PASSIVE checkpoint,摊薄落盘停顿。
	pragmas := []string{"busy_timeout(5000)", "journal_mode(WAL)", "synchronous(NORMAL)", "wal_autocheckpoint(65536)"}
	db, err := sql.Open("sqlite", dsn(path, pragmas...))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // SQLite 写入串行化,避免 database is locked

	// 读另开一个池:此前读写共用那一条连接,Web 端同时发来的几个 API 只能排队,墙钟时间成了
	// 各自耗时之和(实测宠物列表页四个请求 13+50+62+230ms 串成 330ms)。WAL 下读者之间、
	// 读者与写者之间都不互斥,故读放开并发;写仍只有一条连接,原先的写串行保证不变。
	// 只读连接始终处于 autocommit,每条语句各取一次 WAL 快照,读得到写者已提交的最新数据。
	rdb, err := sql.Open("sqlite", dsn(path, pragmas...))
	if err != nil {
		db.Close()
		return nil, err
	}
	rdb.SetMaxOpenConns(maxReadConns())

	s := &Store{db: db, rdb: rdb, gd: gd, rules: newRuleCache()}
	if err := s.initSchema(); err != nil {
		db.Close()
		rdb.Close()
		return nil, err
	}
	s.loadRules()
	go s.checkpointLoop()
	return s, nil
}

// checkpointInterval 是主动 checkpoint 的间隔。
//
// 早期实现每 30s 跑一次 PRAGMA wal_checkpoint(PASSIVE):每次都含一次 fsync,在慢磁盘
// (SD卡/eMMC 网关)上可达几十~上百 ms,期间整个进程的 I/O 调度受影响、Web 请求延迟飙升,
// 表现为「隔 30s 延迟上涨几百 ms、持续几秒」的周期性抖动。
//
// 现改为:间隔放宽到 5 分钟,减少 fsync 频次;wal_autocheckpoint 已设为 65536(约 256MB),
// 常态下被动抓包的写入量远达不到该阈值,故自动 checkpoint 基本不会触发;
// 主动 checkpoint 用 RESTART 模式:无活跃事务时把 WAL 文件重置为初始大小(避免 WAL 越长
// 单次 fsync 越久),有活跃事务时退化为 PASSIVE 行为(不阻塞)。
const checkpointInterval = 5 * time.Minute

// checkpointLoop 定期把 WAL 刷回主库。用 rdb(只读池)执行:checkpoint 操作共享 WAL 文件,
// 任意连接都能触发;不占用唯一的写连接 db,也就不会和正常的宠物入库/快照替换抢写锁。
// 进程常驻运行,随进程退出自然终止。
func (s *Store) checkpointLoop() {
	t := time.NewTicker(checkpointInterval)
	defer t.Stop()
	for range t.C {
		// RESTART:尽可能把 WAL 全部刷回主库并重置 WAL 文件大小;有活跃读者/写者时
		// 退化为 PASSIVE(只刷可刷部分,不阻塞)。比固定 PASSIVE 更能抑制 WAL 增长。
		if _, err := s.rdb.Exec(`PRAGMA wal_checkpoint(RESTART)`); err != nil {
			// checkpoint 失败无害(WAL 仍在,下轮重试),不打扰日志刷屏;仅在连接级错误时提示。
			log.Printf("wal_checkpoint(RESTART) 失败: %v", err)
		}
	}
}

// initSchema 建表建索引(幂等,每次启动都跑一遍)。
// 库结构只有这一处定义:早先散在下面的一串 ALTER 已并回各自的 CREATE TABLE,不再做旧库升级
// ——版本对不上就删掉 rocom.db 重启,数据在下次登录的全量快照里重建。
func (s *Store) initSchema() error {
	_, err := s.db.Exec(`
-- 宠物当前状态。列大多是 data(整只宠物的 JSON)里挑出来供筛选/排序用的冗余投影。
-- base_conf_id 是当前形态的 petbase(进化后随之变化):与 conf_id、shiny 一起够经 gamedata
-- 内存查表得出头像,盒子示意图取几百只头像时便无需逐条解 data(见 petHeads)。
-- *_pct 是身高/体重在当前形态取值范围内的百分位(0-100),写入时按 gamedata 算,供跨种族按
-- 「相对自身范围偏大/偏小」排序(见 buildOrder);gamedata 缺该形态范围时为 NULL,排序排末尾。
CREATE TABLE IF NOT EXISTS pets (
  account TEXT NOT NULL,
  gid INTEGER,
  conf_id INTEGER, base_conf_id INTEGER, species TEXT, name TEXT, level INTEGER,
  nature_id INTEGER, nature TEXT, gender TEXT, types TEXT,
  height REAL, weight REAL, height_pct REAL, weight_pct REAL, voice INTEGER,
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

-- 获得宠物的事件历史(失去不入库)。
CREATE TABLE IF NOT EXISTS events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  account TEXT NOT NULL,
  time INTEGER, sub_kind TEXT, gid INTEGER,
  species TEXT, nature TEXT, medal TEXT, shiny INTEGER,
  data TEXT
);
CREATE INDEX IF NOT EXISTS idx_events_account_time ON events(account, time);

-- 宠物所在位置:pet_box 是「哪只宠物在哪个盒的哪一格」,pet_boxes 是盒子本身的元数据
-- (含空盒,是盒名/数量的权威来源);在队伍里的宠物不在盒子里,二者互斥。
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
-- 宠物拥有过的奖牌(多对多;佩戴中的那枚另存在 pets.medal_id)。
CREATE TABLE IF NOT EXISTS pet_medal (
  account TEXT NOT NULL, gid INTEGER, medal_id INTEGER,
  PRIMARY KEY(account, gid, medal_id)
);

CREATE TABLE IF NOT EXISTS accounts (
  account TEXT PRIMARY KEY, name TEXT, updated_at INTEGER, pin_hash TEXT,
  coins INTEGER NOT NULL DEFAULT 0, has_coins INTEGER NOT NULL DEFAULT 0,
  rank_join INTEGER NOT NULL DEFAULT 1, avatar TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_accounts_updated_at ON accounts(updated_at DESC);

-- 洛克贝快照(排行榜盈亏统计用):每次登录回包解析到洛克贝记一行,
-- 首行为「起始资金」基线,最新行即当前 coins;盈亏 = 当前 - 基线。
CREATE TABLE IF NOT EXISTS coin_snapshots (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  account TEXT NOT NULL,
  coins INTEGER NOT NULL,
  ts INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_coin_snapshots_account ON coin_snapshots(account, ts);

-- 排行榜每日称号:每晚 00:05(北京时间)结算前一日数据,评出大富翁/赚钱王/败家子,
-- 写入 date(归属日,当天佩戴一天)。每天最多 3 行,结算时整日覆盖。
CREATE TABLE IF NOT EXISTS rank_titles (
  date TEXT NOT NULL,
  account TEXT NOT NULL,
  title TEXT NOT NULL,
  PRIMARY KEY (date, title)
);
-- 连接会话:key 是会话密钥(供重启后对存活连接续解,见 docs/architecture.md 3),
-- 其余几列是实时地图重启回显所需的现场(当前场景 / 家园房屋等级 / 所在区域)。
CREATE TABLE IF NOT EXISTS sessions (
  conn_id TEXT PRIMARY KEY, key BLOB, account TEXT, updated_at INTEGER,
  scene_res INTEGER, home_room INTEGER, areas TEXT
);

-- 精灵蛋(背包物品 gid 为键)。表里只有背包现状,破壳/送人的行直接删。
-- parents 是收蛋那一刻的双亲快照 JSON:亲本可能被放生/赠送,蛋上的双亲信息不应随之消失,
-- 故存快照而非引用 pets(见 docs/data.md 3.6)。seq 是背包里的原始次序(服务器下发顺序)。
CREATE TABLE IF NOT EXISTS eggs (
  account TEXT NOT NULL, gid INTEGER,
  item_id INTEGER, conf_id INTEGER, name TEXT, species TEXT,
  height REAL, weight REAL, height_pct REAL, weight_pct REAL,
  src INTEGER, hatching INTEGER, obtained_at INTEGER,
  seq INTEGER, parents TEXT, first_seen INTEGER, updated_at INTEGER, data TEXT,
  PRIMARY KEY(account, gid)
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

-- 草系徽章试炼的遇见记录(试炼页「遇见记录」用,见 docs/data.md 与 trial/battle.go)。
-- chapter 是第几章(1/2/3),**每章独立计算** —— 与 wiki 的口径一致:页面注明
-- 「3 章首领按章节独立计算」,即同一只精灵在第 1 章遇到过、第 2 章的图里仍算未遇见。
-- kind 是战斗类型(0 普通 / 1 首领 / 2 NPC / 3 最终 BOSS,见 trial.BattleType)。
-- 只记试炼战斗:解析时以「带 grass_trial_battle_info」为准,试炼外的战斗不会进来。
CREATE TABLE IF NOT EXISTS trial_encounter (
  account TEXT NOT NULL,
  petbase INTEGER NOT NULL,
  chapter INTEGER NOT NULL,
  kind INTEGER NOT NULL,
  first_seen INTEGER NOT NULL,
  last_seen INTEGER NOT NULL,
  times INTEGER NOT NULL DEFAULT 1,
  PRIMARY KEY(account, petbase, chapter)
);
CREATE INDEX IF NOT EXISTS idx_trial_encounter_account ON trial_encounter(account, chapter);

-- 涂地(实时地图页的覆盖图层,见 docs/data.md 3.8):「玩家 ↔ 已下发的野生宠」之间那条走廊
-- 扫过的格子各记一位,cells 是 w*h 的位图(每字节 8 格,低位在前),按账号 + 场景 + 分层各存一张。
CREATE TABLE IF NOT EXISTS paint (
  account TEXT NOT NULL, res INTEGER, layer INTEGER,
  w INTEGER, h INTEGER, cells BLOB, updated_at INTEGER,
  PRIMARY KEY(account, res, layer)
);

-- 管理员(隐式管理面板,手动输入 #/admin 进入):pass_hash 存 PBKDF2-SHA256 哈希,首启设置后凭密码登录。
CREATE TABLE IF NOT EXISTS admin (
  id INTEGER PRIMARY KEY,
  pass_hash TEXT NOT NULL,
  created_at INTEGER
);

-- 账号级黑白名单(管理面板配置,见 rule.go):mode=black 丢弃该账号全部流量,mode=white 属白名单。
-- 白名单非空时只处理白名单内账号(黑名单优先),白名单为空时仅黑名单生效。规则少,全量载入内存。
CREATE TABLE IF NOT EXISTS account_rule (
  account TEXT PRIMARY KEY, mode TEXT, note TEXT, updated_at INTEGER
);

-- 游玩会话(管理后台「游玩记录」,见 playsession.go):conn_id 对应 sessions 的连接标识,
-- 记录玩家每次上线的起止时间与游玩时长。logout_time 为 NULL 表示会话进行中(玩家在线),
-- duration 在下线时写入;进行中的时长按「现在-登录时刻」折算,不入库。
CREATE TABLE IF NOT EXISTS play_sessions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  conn_id TEXT NOT NULL,
  account TEXT NOT NULL,
  login_time INTEGER NOT NULL,
  logout_time INTEGER,
  duration INTEGER
);
CREATE INDEX IF NOT EXISTS idx_play_sessions_account ON play_sessions(account, login_time);
CREATE INDEX IF NOT EXISTS idx_play_sessions_login ON play_sessions(login_time);

-- 远行商人第三方数据缓存(见 server/api_merchant.go 的业务模型):slot 是 4h 槽的开始时间戳
-- (Unix 秒,对齐 8/12/16/20/0/4 点,跨天唯一),empty=1 表示「该槽查过但无货/已收摊」,
-- data 存第三方原始 JSON;记录只保留 2 天,写入时顺手清理更早的。
CREATE TABLE IF NOT EXISTS merchant_slots (
  slot INTEGER PRIMARY KEY,
  empty INTEGER NOT NULL DEFAULT 0,
  data TEXT NOT NULL DEFAULT '',
  fetched_at INTEGER NOT NULL
);

-- 远行商人订阅(邮件提醒,见 server/api_merchant.go):email 是玩家填的收件 QQ 邮箱,
-- keywords 是逗号分隔的商品名关键词(空=该邮箱订阅全部新上架商品);订阅长期有效不退。
-- 远行商人邮件订阅:按登录账号绑定(一账号一邮箱,换设备/切账号都能识别到同一订阅)。
-- email 是收件邮箱,account 是登录账号键(如 UID:xxx)。
CREATE TABLE IF NOT EXISTS merchant_subs (
  account TEXT PRIMARY KEY,
  email TEXT NOT NULL,
  keywords TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL
);
-- 已通知记录:同一槽(4h)对同一邮箱,**每个商品只提醒一次**(见 server/merchant_notify.go)。
-- items 是已通知过的商品名清单(逗号分隔),空串 = 该槽对他还一封都没发过。
-- 去重粒度细化到商品(而非整槽)是因为第三方会滞后补货:同一轮要回源多次,
-- 每次都可能带来新商品,只按槽去重会把后到的商品永久挡住(订阅者收不到提醒)。
-- 记录随槽缓存一起在写入时清理 2 天前的。
CREATE TABLE IF NOT EXISTS merchant_notified (
  slot INTEGER NOT NULL,
  email TEXT NOT NULL,
  items TEXT NOT NULL DEFAULT '',
  PRIMARY KEY(slot, email)
);

-- 查蛋 API(第三方图鉴,见 api_egg_query.go)使用统计:每次发起第三方请求记一行,
-- 管理面板据此看今日消耗/成功率/谁在查。量小(一天几十次),不做清理。
CREATE TABLE IF NOT EXISTS egg_queries (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  account TEXT NOT NULL DEFAULT '',
  ts INTEGER NOT NULL,
  ok INTEGER NOT NULL DEFAULT 0,
  cost_ms INTEGER NOT NULL DEFAULT 0,
  matches INTEGER NOT NULL DEFAULT 0,
  height TEXT NOT NULL DEFAULT '',
  weight TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_egg_queries_ts ON egg_queries(ts);

-- 花种挑战次数(见 flower.go):按账号 + 花种品种(npc_cfg_id + blood)累计,每次
-- c2s 0x034E 选中花种进入战斗记一次。计数挂在品种上而非个体 npc_logic_id——个体被采/
-- 刷新消失后计数保留(活动进行中会重生),下次同品种花种出现时卡片继续累计显示;
-- 活动结束(end_ts 过期)后删除整行,计数随之清零。量小,直接 UPSERT。
CREATE TABLE IF NOT EXISTS flower_challenges (
  account TEXT NOT NULL,
  npc_cfg_id INTEGER NOT NULL,
  blood INTEGER NOT NULL,
  count INTEGER NOT NULL DEFAULT 0,
  end_ts INTEGER NOT NULL DEFAULT 0, -- 活动结束时间(Unix 秒,0=未设置);过期即清理计数
  updated_at INTEGER,
  PRIMARY KEY(account, npc_cfg_id, blood)
);

-- 图鉴炫彩收集(见 pet/handbook.go):解析登录包 PlayerPetInfo.pet_handbook,按账号记录
-- 每个品种(pet_base_id)抓到过哪些炫彩变体(glass_type 普通/隐藏 + glass_value),
-- 登录时整体替换(数据仅登录包携带)。
CREATE TABLE IF NOT EXISTS handbook_glass (
  account TEXT NOT NULL,
  pet_base_id INTEGER NOT NULL,
  glass_type INTEGER NOT NULL,
  glass_value INTEGER NOT NULL,
  PRIMARY KEY(account, pet_base_id, glass_type, glass_value)
);

-- 全服共享标注(众包 + 管理员审核,见 annotations.go):玩家对协议里查不到名字的
-- 技能 id / 特性 id(288xxx)提交名字与描述,管理员审核通过后所有人可见。
-- 同一 (kind, code) 允许多条待审;approve 时同 code 其余 pending 自动转 rejected
-- (答案唯一)。status: pending / approved / rejected。
CREATE TABLE IF NOT EXISTS annotations (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  kind TEXT NOT NULL,          -- skill / feature
  code INTEGER NOT NULL,       -- 协议 id:技能 base_skill_id / 特性 288xxx
  name TEXT NOT NULL,
  desc TEXT NOT NULL DEFAULT '',
  submitter TEXT NOT NULL,     -- 提交的登录账号
  status TEXT NOT NULL DEFAULT 'pending',
  created_at INTEGER NOT NULL,
  reviewed_by TEXT NOT NULL DEFAULT '',
  reviewed_at INTEGER NOT NULL DEFAULT 0,
  UNIQUE(kind, code, name, submitter)
);
CREATE INDEX IF NOT EXISTS idx_annotations_lookup ON annotations(kind, status);
CREATE INDEX IF NOT EXISTS idx_annotations_code ON annotations(kind, code);
`)
	if err != nil {
		return err
	}
	// 老库平滑升级:accounts 补 coins 列(洛克贝)。历史库无此列,直接 ALTER 加列;
	// 新库建表已含该列,SQLite 报 duplicate column 可忽略。避免强迫删库(删库会丢 PIN)。
	if _, err := s.db.Exec(`ALTER TABLE accounts ADD COLUMN coins INTEGER NOT NULL DEFAULT 0`); err != nil &&
		!strings.Contains(err.Error(), "duplicate column") {
		return err
	}
	// 老库补 has_coins 列:区分「从未解析到洛克贝(未知)」与「解析到 0(真没钱)」,
	// 前端据此显示「待同步」而非把徽标直接隐藏。
	if _, err := s.db.Exec(`ALTER TABLE accounts ADD COLUMN has_coins INTEGER NOT NULL DEFAULT 0`); err != nil &&
		!strings.Contains(err.Error(), "duplicate column") {
		return err
	}
	// 老库补 rank_join 列(排行榜参与开关):DEFAULT 1 = 默认参加。
	if _, err := s.db.Exec(`ALTER TABLE accounts ADD COLUMN rank_join INTEGER NOT NULL DEFAULT 1`); err != nil &&
		!strings.Contains(err.Error(), "duplicate column") {
		return err
	}
	// 老库补 avatar 列(玩家平台头像 URL):空串 = 未取到(游客号/未绑平台),
	// 前端据此回退到昵称首字占位,不要显示破图。
	if _, err := s.db.Exec(`ALTER TABLE accounts ADD COLUMN avatar TEXT NOT NULL DEFAULT ''`); err != nil &&
		!strings.Contains(err.Error(), "duplicate column") {
		return err
	}
	// 老库补 merchant_notified.items 列(已通知商品清单):订阅去重从「每槽一次」细化到
	// 「每槽每商品一次」时老表无此列,直接 ALTER 加列;新库建表已含该列,报 duplicate
	// column 可忽略。已有行的 items 为空串 = 该槽尚未通知过任何商品,与旧语义一致
	// (旧表里「有行」只说明发过信,商品清单无从还原,最坏情况是补发一次)。
	if _, err := s.db.Exec(`ALTER TABLE merchant_notified ADD COLUMN items TEXT NOT NULL DEFAULT ''`); err != nil &&
		!strings.Contains(err.Error(), "duplicate column") {
		return err
	}
	// 老库补 flower_challenges.end_ts 列(花种活动结束时间):本轮实现花种挑战计数时
	// 老表无此列,直接 ALTER 加列;新库建表已含该列,报 duplicate column 可忽略。
	if _, err := s.db.Exec(`ALTER TABLE flower_challenges ADD COLUMN end_ts INTEGER NOT NULL DEFAULT 0`); err != nil &&
		!strings.Contains(err.Error(), "duplicate column") {
		return err
	}
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
