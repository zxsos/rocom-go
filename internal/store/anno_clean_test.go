package store

import "testing"

// TestAnnotationNameSpacesCleaned 启动时清洗历史标注名字里的空白。
//
// 这是修「用户库里已有脏数据」的那一步 —— 提交侧的清洗(见 server 的
// cleanAnnotationName)救不了已经入库的记录,必须在打开库时补一遍。
func TestAnnotationNameSpacesCleaned(t *testing.T) {
	st := newTestStore(t)
	// 直接插带空格的名字,模拟清洗前提交的记录(覆盖:字间空格、末尾空格、
	// 全角空格 U+3000、以及本来干净的)
	dirty := []struct{ name, want string }{
		{"魔 法 增 效 ", "魔法增效"},
		{"挺 起 胸 脯", "挺起胸脯"},
		{"烈　焰风暴", "烈焰风暴"},
		{"已经干净", "已经干净"},
	}
	for i, d := range dirty {
		if _, err := st.db.Exec(`INSERT INTO annotations(kind, code, name, desc, submitter, status, created_at)
			VALUES('skill', ?, ?, '', 'UID:1', 'approved', 0)`, 7880001+i, d.name); err != nil {
			t.Fatalf("插脏数据 %q: %v", d.name, err)
		}
	}

	// 清洗挂在 initSchema 里,它就是启动时跑的那一段;幂等,这里再跑一次即可验证。
	if err := st.initSchema(); err != nil {
		t.Fatalf("initSchema: %v", err)
	}

	for i, d := range dirty {
		var got string
		if err := st.rdb.QueryRow(`SELECT name FROM annotations WHERE code = ?`, 7880001+i).Scan(&got); err != nil {
			t.Errorf("读回 %q: %v", d.name, err)
			continue
		}
		if got != d.want {
			t.Errorf("清洗后 = %q, 期望 %q(原值 %q)", got, d.want, d.name)
		}
	}
}

// TestAnnotationNameCleanIsIdempotent 清洗幂等:再跑一次不该把名字改坏。
// 守护「清洗 SQL 写错(如把 name 清成空串、或去掉 WHERE 变成全表操作)」这类回归
// —— 那会让全服标注的名字集体消失,比带空格严重得多。
func TestAnnotationNameCleanIsIdempotent(t *testing.T) {
	st := newTestStore(t)
	// 两条:**一条带空格**(应被清洗)、**一条本来干净**(应原样不动)。
	// 只测前者的话,「SET name='' 清空全表」这种写法也只会在前者上报错,但如果
	// 断言写得松(比如只查「有没有叫魔法增效的」)就可能漏过去;补上后者,任何
	// 波及到干净行的破坏都跑不掉。
	for _, c := range []struct {
		code int64
		name string
		want string
	}{
		{7880099, "魔法增效", "魔法增效"}, // 干净:必须原样保留
		{7880098, "魔 法 增 效", "魔法增效"}, // 脏:清洗
	} {
		if _, err := st.db.Exec(`INSERT INTO annotations(kind, code, name, desc, submitter, status, created_at)
			VALUES('skill', ?, ?, '', 'UID:1', 'approved', 0)`, c.code, c.name); err != nil {
			t.Fatalf("插数据 %q: %v", c.name, err)
		}
	}
	if err := st.initSchema(); err != nil {
		t.Fatalf("第一次: %v", err)
	}
	if err := st.initSchema(); err != nil {
		t.Fatalf("第二次: %v", err)
	}
	for _, c := range []struct {
		code int64
		want string
	}{
		{7880099, "魔法增效"}, // 干净行:重复清洗后仍原样
		{7880098, "魔法增效"}, // 脏行:已清洗
	} {
		var got string
		if err := st.rdb.QueryRow(`SELECT name FROM annotations WHERE code = ?`, c.code).Scan(&got); err != nil {
			t.Fatalf("读回 code=%d: %v", c.code, err)
		}
		if got != c.want {
			t.Errorf("重复清洗后 code=%d 的名字 = %q, 期望 %q", c.code, got, c.want)
		}
	}
}
