package store

import (
	"sync"
	"time"
)

// Rule 是账号级黑白名单规则(管理面板配置)。
// Mode 取值:black=黑名单(丢弃该账号全部流量),white=白名单(仅白名单内账号被处理)。
type Rule struct {
	Account string `json:"account"`
	Mode    string `json:"mode"`
	Note    string `json:"note,omitempty"`
}

// ruleCache 是 account_rule 表的内存镜像:规则极少(管理员手配),全量载入读零开销,
// Set/Delete 落库后同步更新,保证 pipeline 实时生效。
type ruleCache struct {
	mu    sync.RWMutex
	rules map[string]Rule
}

func newRuleCache() *ruleCache {
	return &ruleCache{rules: map[string]Rule{}}
}

// loadRules 从库加载全部规则到内存(启动时调用)。
func (s *Store) loadRules() {
	rows, err := s.rdb.Query(`SELECT account, mode, note FROM account_rule`)
	if err != nil {
		return
	}
	defer rows.Close()
	cache := map[string]Rule{}
	for rows.Next() {
		var r Rule
		if rows.Scan(&r.Account, &r.Mode, &r.Note) == nil {
			cache[r.Account] = r
		}
	}
	s.rules.mu.Lock()
	s.rules.rules = cache
	s.rules.mu.Unlock()
}

// RuleMode 返回账号的规则模式(black/white/空串=无规则)。
func (s *Store) RuleMode(acc string) string {
	s.rules.mu.RLock()
	defer s.rules.mu.RUnlock()
	return s.rules.rules[acc].Mode
}

// WhiteListNonEmpty 返回白名单是否非空(pipeline 据此决定是否只处理白名单内账号)。
func (s *Store) WhiteListNonEmpty() bool {
	s.rules.mu.RLock()
	defer s.rules.mu.RUnlock()
	for _, r := range s.rules.rules {
		if r.Mode == "white" {
			return true
		}
	}
	return false
}

// ListRules 列出全部规则(管理面板展示)。
func (s *Store) ListRules() ([]Rule, error) {
	s.rules.mu.RLock()
	defer s.rules.mu.RUnlock()
	out := make([]Rule, 0, len(s.rules.rules))
	for _, r := range s.rules.rules {
		out = append(out, r)
	}
	return out, nil
}

// SetRule 新增或更新一条规则(mode 取值 black/white)。
func (s *Store) SetRule(account, mode, note string) error {
	if _, err := s.db.Exec(`
INSERT INTO account_rule(account, mode, note, updated_at) VALUES(?,?,?,?)
ON CONFLICT(account) DO UPDATE SET mode=excluded.mode, note=excluded.note, updated_at=excluded.updated_at`,
		account, mode, note, time.Now().Unix()); err != nil {
		return err
	}
	s.rules.mu.Lock()
	s.rules.rules[account] = Rule{Account: account, Mode: mode, Note: note}
	s.rules.mu.Unlock()
	return nil
}

// DeleteRule 删除一条规则。
func (s *Store) DeleteRule(account string) error {
	if _, err := s.db.Exec(`DELETE FROM account_rule WHERE account=?`, account); err != nil {
		return err
	}
	s.rules.mu.Lock()
	delete(s.rules.rules, account)
	s.rules.mu.Unlock()
	return nil
}
