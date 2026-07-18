package store

import (
	"encoding/json"
	"time"
)

// SessionTTL 是持久化会话(密钥/账号归属)的有效期:超过此时长的连接不再复用,
// 兜底防止四元组被新连接复用时套用陈旧密钥(主校验仍是 gcp.ValidPlain 的明文自检)。
const SessionTTL = 24 * time.Hour

// SaveSessionKey 持久化某 GCP 连接(conn_id)的会话 AES 密钥,
// 供抓包服务异常重启后继续解密同一条仍存活的连接。account 列保持不变。
func (s *Store) SaveSessionKey(connID string, key []byte) error {
	_, err := s.db.Exec(`
INSERT INTO sessions(conn_id,key,updated_at) VALUES(?,?,?)
ON CONFLICT(conn_id) DO UPDATE SET key=excluded.key, updated_at=excluded.updated_at`,
		connID, key, time.Now().Unix())
	return err
}

// LoadKey 读取某连接近 SessionTTL 内更新过的会话密钥;无/过期/为空均返回 false。
// 实现 capture.KeyStore,供 Engine 在连接首次出现时预热密钥。
func (s *Store) LoadKey(connID string) ([]byte, bool) {
	var key []byte
	err := s.db.QueryRow(`SELECT key FROM sessions WHERE conn_id=? AND updated_at>=?`,
		connID, time.Now().Add(-SessionTTL).Unix()).Scan(&key)
	if err != nil || len(key) == 0 {
		return nil, false
	}
	return key, true
}

// SaveKey 实现 capture.KeyStore(SaveSessionKey 的忽略错误封装)。
func (s *Store) SaveKey(connID string, key []byte) { s.SaveSessionKey(connID, key) }

// SaveSessionAccount 持久化某连接的账号归属("UID:<user_id>"),
// 使重启后无需再次等到登录回包即可归属该连接解密出的消息。key 列保持不变。
func (s *Store) SaveSessionAccount(connID, account string) error {
	_, err := s.db.Exec(`
INSERT INTO sessions(conn_id,account,updated_at) VALUES(?,?,?)
ON CONFLICT(conn_id) DO UPDATE SET account=excluded.account, updated_at=excluded.updated_at`,
		connID, account, time.Now().Unix())
	return err
}

// SessionScene 是一个连接缓存的场景态:当前 scene_res、家园房屋等级(非家园为 0)与所在区域。
type SessionScene struct {
	Res   int32
	Room  int32
	Areas map[uint32][]uint32 // area_func_id → 该 func 下已进入的 area_id(选分层地图)
}

// SaveSessionScene 持久化某连接当前所在的 scene_res_cfg_id 与家园房屋等级(实时地图页用)。
// 场景 res 只在进入/传送时下发,游戏中途不再重发,故须落盘以便抓包服务重启后恢复地图定位
// (移动包只带 scene_cfg_id,单靠它无法区分同 cfg 下的多个 res)。key/account 列保持不变。
func (s *Store) SaveSessionScene(connID string, res, room int32) error {
	_, err := s.db.Exec(`
INSERT INTO sessions(conn_id,scene_res,home_room,updated_at) VALUES(?,?,?,?)
ON CONFLICT(conn_id) DO UPDATE SET scene_res=excluded.scene_res, home_room=excluded.home_room, updated_at=excluded.updated_at`,
		connID, res, room, time.Now().Unix())
	return err
}

// SaveSessionAreas 持久化某连接当前所在的区域集合(area_func_id → 已进入的 area_id)。
// 区域进/出只在跨越触发体时下发(ZONE_SCENE_PLAY_ACTS_NOTIFY),游戏中途不重发,故与场景 res
// 一样须落盘:否则抓包服务重启后不知玩家还在洞里,地图会退回地表底图。
func (s *Store) SaveSessionAreas(connID string, areas map[uint32][]uint32) error {
	blob, err := json.Marshal(areas)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`
INSERT INTO sessions(conn_id,areas,updated_at) VALUES(?,?,?)
ON CONFLICT(conn_id) DO UPDATE SET areas=excluded.areas, updated_at=excluded.updated_at`,
		connID, string(blob), time.Now().Unix())
	return err
}

// LoadSessionScenes 读取近 SessionTTL 内的 conn_id→场景态映射(重启预热用)。
func (s *Store) LoadSessionScenes() (map[string]SessionScene, error) {
	rows, err := s.db.Query(`SELECT conn_id,scene_res,COALESCE(home_room,0),COALESCE(areas,'') FROM sessions WHERE scene_res IS NOT NULL AND scene_res<>0 AND updated_at>=?`,
		time.Now().Add(-SessionTTL).Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]SessionScene{}
	for rows.Next() {
		var connID, areas string
		var sc SessionScene
		if rows.Scan(&connID, &sc.Res, &sc.Room, &areas) != nil {
			continue
		}
		if areas != "" {
			json.Unmarshal([]byte(areas), &sc.Areas) // 解不动就当没有(退回地表底图)
		}
		out[connID] = sc
	}
	return out, rows.Err()
}

// LoadSessionAccounts 读取近 SessionTTL 内的 conn_id→account 映射(重启预热用),
// 并顺带清理过期会话行,避免长期累积。仅返回 account 非空的连接。
func (s *Store) LoadSessionAccounts() (map[string]string, error) {
	cutoff := time.Now().Add(-SessionTTL).Unix()
	s.db.Exec(`DELETE FROM sessions WHERE updated_at<?`, cutoff)
	rows, err := s.db.Query(`SELECT conn_id,account FROM sessions WHERE account IS NOT NULL AND account<>''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var connID, account string
		if rows.Scan(&connID, &account) == nil {
			out[connID] = account
		}
	}
	return out, rows.Err()
}
