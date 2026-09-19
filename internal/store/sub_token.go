// 订阅设备令牌与短链(表结构见 store.go 的 sub_tokens / sub_links)。
//
// 为什么不再用「密码派生的单一令牌」(见 hy2.SubToken):派生令牌把「吊销一台设备」与
// 「换密码」焊死了 —— 想踢掉某个人就得换密码,而换密码又会让所有人的订阅一起失效。
// 朋友各自拿一个令牌之后,吊销就只是这一行的事(换密码仍是想要的兜底手段,但不必
// 因为它而牵连所有人,详见 docs/deploy.md 的三层吊销)。
//
// 令牌与短链是**两件东西**:令牌是给客户端吃的秘密,短链是给人传的入口。分开之后
// 短码可以一直不变,后台换令牌也不影响已经发出去的链接与二维码。
package store

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// subCodeLen 短码长度。8 位从 32 个字符里取 ≈ 40 bit,扫码/手输都够短,
// 而对「猜短码」这类扫描来说空间依然足够(且猜中只等于拿到订阅,不等于拿到密码)。
const subCodeLen = 8

// subCodeAlphabet 去掉了 0/O 与 1/I/L:手抄短码时它们最容易看错,而看错的代价是
// 朋友以为链接坏了、直接放弃。
const subCodeAlphabet = "23456789ABCDEFGHJKMNPQRSTUVWXYZ"

// SubDevice 一台设备(或一个朋友)的订阅令牌及其近况。
// 近况几列是排障用的:面板上要能一眼分开「根本没来拉」与「拉到了但规则没生效」,
// 而这两种故障在下游的表现一模一样(就是没有数据)。
type SubDevice struct {
	Token      string
	Label      string
	Code       string // 短码;为空表示还没生成过短链
	CreatedAt  int64
	ExpiresAt  int64 // Unix 秒,0 = 长期有效
	RevokedAt  int64 // Unix 秒,0 = 未吊销
	LastSeenAt int64
	LastUA     string
	LastFormat string
	Hits       int64
}

// Revoked 是否已被吊销。
func (d SubDevice) Revoked() bool { return d.RevokedAt > 0 }

// Expired 是否已过期(0 = 长期)。
func (d SubDevice) Expired() bool { return d.ExpiresAt > 0 && time.Now().Unix() > d.ExpiresAt }

// Usable 现在还能不能拉到配置。
func (d SubDevice) Usable() bool { return !d.Revoked() && !d.Expired() }

// CreateSubDevice 新建一个设备令牌并顺带生成它的短链。
// ttlSeconds <= 0 表示长期有效(默认用法:靠手动吊销,不靠过期)。
func (s *Store) CreateSubDevice(label string, ttlSeconds int64) (*SubDevice, error) {
	tok, err := newSubToken()
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	var exp int64
	if ttlSeconds > 0 {
		exp = now + ttlSeconds
	}
	if _, err := s.db.Exec(`INSERT INTO sub_tokens(token, label, created_at, expires_at, revoked_at,
		last_seen_at, last_ua, last_format, hits) VALUES(?,?,?,?,0,0,'','',0)`,
		tok, label, now, exp); err != nil {
		return nil, err
	}
	code, err := s.EnsureSubLink(tok)
	if err != nil {
		return nil, err
	}
	return &SubDevice{Token: tok, Label: label, Code: code, CreatedAt: now, ExpiresAt: exp}, nil
}

// GetSubDevice 按令牌取一台设备。不存在时 ok=false(调用方一律 404,不区分原因)。
func (s *Store) GetSubDevice(token string) (SubDevice, bool) {
	var d SubDevice
	err := s.rdb.QueryRow(`SELECT token, label, created_at, expires_at, revoked_at,
		last_seen_at, last_ua, last_format, hits FROM sub_tokens WHERE token=?`, token).
		Scan(&d.Token, &d.Label, &d.CreatedAt, &d.ExpiresAt, &d.RevokedAt,
			&d.LastSeenAt, &d.LastUA, &d.LastFormat, &d.Hits)
	if err != nil {
		return SubDevice{}, false
	}
	d.Code, _ = s.codeOfToken(token)
	return d, true
}

// ListSubDevices 列出全部设备(量小,按创建时间倒序)。
func (s *Store) ListSubDevices() ([]SubDevice, error) {
	// 短码用标量子查询取最新那条,而不是 JOIN:吊销后仍要显示它对应过哪个短码 ——
	// 管理员看列表时想确认的正是「我刚吊销的是不是这一条」。
	rows, err := s.rdb.Query(`SELECT t.token, t.label, t.created_at, t.expires_at, t.revoked_at,
		t.last_seen_at, t.last_ua, t.last_format, t.hits,
		COALESCE((SELECT l.code FROM sub_links l WHERE l.token = t.token
			ORDER BY l.created_at DESC LIMIT 1), '')
		FROM sub_tokens t ORDER BY t.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SubDevice
	for rows.Next() {
		var d SubDevice
		if err := rows.Scan(&d.Token, &d.Label, &d.CreatedAt, &d.ExpiresAt, &d.RevokedAt,
			&d.LastSeenAt, &d.LastUA, &d.LastFormat, &d.Hits, &d.Code); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// RevokeSubDevice 吊销一台设备(软删:留着行,面板上仍能看到它最后一次来拉是什么时候)。
// 语义只到「下次拉订阅被拒」为止;要让它再也连不上,得换 hy2 密码(见 docs/deploy.md)。
func (s *Store) RevokeSubDevice(token string) error {
	res, err := s.db.Exec(`UPDATE sub_tokens SET revoked_at=? WHERE token=? AND revoked_at=0`,
		time.Now().Unix(), token)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err == nil && n == 0 {
		return fmt.Errorf("store: 令牌不存在或已吊销")
	}
	// 短链一并作废:它是这枚令牌的入口,留着只会让人点进去看到 403。
	_, _ = s.db.Exec(`UPDATE sub_links SET revoked_at=? WHERE token=? AND revoked_at=0`,
		time.Now().Unix(), token)
	return nil
}

// RenameSubDevice 改设备备注名。
func (s *Store) RenameSubDevice(token, label string) error {
	_, err := s.db.Exec(`UPDATE sub_tokens SET label=? WHERE token=?`, label, token)
	return err
}

// NoteSubFetch 记一次成功下发:命中数、最后来访时间、UA 与实发格式。
// 一条 UPDATE 顺带把短链的命中也加上 —— 订阅是低频请求(客户端几小时一次),
// 不值得为它开事务。
func (s *Store) NoteSubFetch(token, ua, format string) error {
	now := time.Now().Unix()
	if _, err := s.db.Exec(`UPDATE sub_tokens SET hits=hits+1, last_seen_at=?, last_ua=?, last_format=?
		WHERE token=?`, now, ua, format, token); err != nil {
		return err
	}
	_, _ = s.db.Exec(`UPDATE sub_links SET hits=hits+1 WHERE token=? AND revoked_at=0`, token)
	return nil
}

// EnsureSubLink 保证该令牌有一条可用短链;已有则直接返回旧码。
// 幂等是刻意的:二维码一旦发出去就无法收回,重新生成等于把已经扫过的人踢掉。
func (s *Store) EnsureSubLink(token string) (string, error) {
	if code, ok := s.codeOfToken(token); ok {
		return code, nil
	}
	code, err := s.newSubCode()
	if err != nil {
		return "", err
	}
	if _, err := s.db.Exec(`INSERT INTO sub_links(code, token, created_at, hits, revoked_at)
		VALUES(?,?,?,0,0)`, code, token, time.Now().Unix()); err != nil {
		return "", err
	}
	return code, nil
}

// ResolveSubCode 由短码找令牌。
//
// 刻意**不**在这里过滤 revoked_at:短码要能在吊销之后仍然解析出令牌,调用方才能分清
// 「这条被吊销了」(403 + 一句人话)与「短码不存在」(404)。两者对人的意义完全不同 ——
// 前者他会去找发链接的人要新的,后者他会以为自己把链接抄错了。
func (s *Store) ResolveSubCode(code string) (token string, ok bool) {
	err := s.rdb.QueryRow(`SELECT token FROM sub_links WHERE code=? ORDER BY created_at DESC LIMIT 1`, code).
		Scan(&token)
	return token, err == nil && token != ""
}

func (s *Store) codeOfToken(token string) (string, bool) {
	var code string
	err := s.rdb.QueryRow(`SELECT code FROM sub_links WHERE token=? AND revoked_at=0
		ORDER BY created_at DESC LIMIT 1`, token).Scan(&code)
	return code, err == nil && code != ""
}

func newSubToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("store: 生成订阅令牌失败: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// newSubCode 取一个未用过的短码。撞库概率极低,撞了就换一个重取,不与人较劲。
func (s *Store) newSubCode() (string, error) {
	var last error
	for i := 0; i < 10; i++ {
		code, err := randomSubCode()
		if err != nil {
			return "", err
		}
		var n int
		if err := s.rdb.QueryRow(`SELECT COUNT(1) FROM sub_links WHERE code=?`, code).Scan(&n); err != nil {
			last = err
			continue
		}
		if n == 0 {
			return code, nil
		}
		last = fmt.Errorf("store: 短码冲突")
	}
	return "", fmt.Errorf("store: 生成短码失败: %w", last)
}

// randomSubCode 从字母表里逐字符取,取模会有轻微偏斜,对 8 位短码无所谓。
func randomSubCode() (string, error) {
	b := make([]byte, subCodeLen)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("store: 生成短码失败: %w", err)
	}
	out := make([]byte, subCodeLen)
	for i, v := range b {
		out[i] = subCodeAlphabet[int(v)%len(subCodeAlphabet)]
	}
	return string(out), nil
}
