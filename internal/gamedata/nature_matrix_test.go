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

// TestNatureUniqueNames 锁住**去重后**的性格种类数 == 30,且它必须等于方阵填的格数。
//
// 为什么值得单测:`internal/pet` 的 natureCount(性格重掷槽的分母)按这个数硬编码,而
// names.json 的 nature 表是**行数** 31 —— 里面 id 28 与 id 31 同为「平和」(+生命 −魔攻),
// 且实测 7545 只宠物的性格 id 分布里 **31 一次都没出现过**(它是配置残留,永远不会落到
// 宠物身上)。拿行数当种类数会把重掷那一份算小(1/31 而非 1/30),而这种偏差**不报错**:
// 页面只是显示 61.29% 而不是 61.33%,谁也看不出来 —— 与上面那两条一样,属于「结构错了
// 而界面完全正常」的那类失败。
//
// 第二条断言(种类数 == 方阵格数)防的是另一种漂移:多出一个在方阵里没有位置的性格
// (如无增减的中性性格),它进不了前端的目标性格下拉,却被算进重掷分母 —— 两边同口径才对。
//
// 游戏改了性格表时会红,届时要连带改 internal/pet/breeding.go 的 natureCount、
// docs/data.md 里那三档概率,以及前端 SuggestPanel 的性格条数文案。
func TestNatureUniqueNames(t *testing.T) {
	db, err := Load()
	if err != nil {
		t.Fatalf("加载名称库: %v", err)
	}
	seen := map[string]bool{}
	for _, name := range db.nature {
		if name != "" {
			seen[name] = true
		}
	}
	if len(seen) != 30 {
		t.Errorf("去重后的性格种类数 = %d, 期望 30(nature 表有 %d 行,其中包含重复项)",
			len(seen), len(db.nature))
	}
	m := db.NatureMatrix()
	filled := 0
	for i := 0; i < 6; i++ {
		for j := 0; j < 6; j++ {
			if m[i][j] != "" {
				filled++
			}
		}
	}
	if filled != len(seen) {
		t.Errorf("方阵填充 %d 格 != 去重性格数 %d:有性格在方阵里没有位置(前端的目标性格下拉选不到它)",
			filled, len(seen))
	}
}
