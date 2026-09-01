package store

import (
	"path/filepath"
	"testing"

	"github.com/whoisnian/rocom-capture/internal/gamedata"
)

// TestMigrateCleansExistingSpacesOnDeploy 端到端验证「部署新版 + 重启服务」能自动
// 清洗老库里已存在的脏名字 —— 用的是**真实建库路径**(New + 重新打开),而不是只调
// initSchema 那段 SQL,因为用户问的正是「已经部署了会自动改回来吗」。
func TestMigrateCleansExistingSpacesOnDeploy(t *testing.T) {
	dir := t.TempDir()
	gd, err := gamedata.Load()
	if err != nil {
		t.Fatalf("加载名称库: %v", err)
	}

	// ① 建库并插入「清洗前」提交的脏名字(模拟用户网关上的现状)
	dirty := []struct{ name, want string }{
		{"魔 法 增 效 ", "魔法增效"},
		{"挺 起 胸 脯", "挺起胸脯"},
		{"烈 焰 风 暴 ", "烈焰风暴"},
	}
	func() {
		st, err := New(filepath.Join(dir, "rocom.db"), gd)
		if err != nil {
			t.Fatalf("建库: %v", err)
		}
		for i, d := range dirty {
			if _, err := st.db.Exec(`INSERT INTO annotations(kind, code, name, desc, submitter, status, created_at)
				VALUES('skill', ?, ?, '', 'UID:100000002', 'approved', 0)`, 7880001+i, d.name); err != nil {
				t.Fatalf("插脏数据: %v", err)
			}
		}
	}()

	// ② 重新打开 —— 等价于「部署新版本后启动服务」
	st2, err := New(filepath.Join(dir, "rocom.db"), gd)
	if err != nil {
		t.Fatalf("重开库: %v", err)
	}

	for i, d := range dirty {
		var got string
		if err := st2.rdb.QueryRow(`SELECT name FROM annotations WHERE code=?`, 7880001+i).Scan(&got); err != nil {
			t.Errorf("读回 %d: %v", i, err)
			continue
		}
		if got != d.want {
			t.Errorf("重启后名字 = %q, 期望 %q(原值 %q) —— 老库的脏数据不会被清洗",
				got, d.want, d.name)
		}
	}
}
