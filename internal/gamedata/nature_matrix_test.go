package gamedata

import "testing"

// TestNatureMatrixShape 锁住性格方阵的**结构**:6×6、对角线空、非对角线填满 30 格。
//
// 为什么值得单测:前端照这张表铺格子,若维度顺序(pos/neg ↔ 展示顺序)漂移,
// 表现是**每个格子里装着一个别的性格** —— 界面完全正常、不报任何错,
// 只有玩家筛出来的宠物对不上才发现。结构对了,错位这一大类问题就堵死了。
//
// 不逐个断言 30 个性格名:名字随游戏版本变(见 names.json 由生成脚本产出),
// 钉死内容会让每次版本更新都红,而那不是 bug。形状才是契约。
func TestNatureMatrixShape(t *testing.T) {
	db, err := Load()
	if err != nil {
		t.Fatalf("加载名称库: %v", err)
	}
	m := db.NatureMatrix()
	filled := 0
	for i := 0; i < 6; i++ {
		// 对角线(增減同一维)游戏内不存在,必须为空
		if m[i][i] != "" {
			t.Errorf("对角线 [%d][%d] 应为空, 实为 %q", i, i, m[i][i])
		}
		for j := 0; j < 6; j++ {
			if m[i][j] != "" {
				filled++
			}
		}
	}
	if filled != 30 {
		t.Errorf("方阵填充格数 = %d, 期望 30(6×6 去掉对角线)", filled)
	}
}

// TestNatureMatrixNoDuplicate 同一个性格名不能占两格 —— 否则前端按名字回查选中态时会命中两个格子。
func TestNatureMatrixNoDuplicate(t *testing.T) {
	db, _ := Load()
	m := db.NatureMatrix()
	seen := map[string]bool{}
	for i := 0; i < 6; i++ {
		for j := 0; j < 6; j++ {
			n := m[i][j]
			if n == "" {
				continue
			}
			if seen[n] {
				t.Errorf("性格 %q 重复出现在 [%d][%d]", n, i, j)
			}
			seen[n] = true
		}
	}
}
