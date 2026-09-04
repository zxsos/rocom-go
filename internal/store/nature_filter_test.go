package store

import "testing"

// TestFilterNatureIn 性格多选:命中其中任一即算命中。
//
// 存在理由:宠物列表的性格筛选改成了 6×6 方阵(见 gamedata.NatureMatrix),
// 点行头/列头会一次选中 5 个性格,走的是 nature IN (...) 而不是单个等值。
// 这条 SQL 若退化成「只取第一个」或写成 AND,表现是**筛选结果变少但界面完全正常**
// —— 没有任何报错,只有玩家觉得「怎么少筛出来几只」,极难定位。
func TestFilterNatureIn(t *testing.T) {
	st := newTestStore(t)
	sc := st.For(testAcc)

	// 四只宠物,性格各不相同;前三只落在所选的三个里,最后一只不在
	want := []string{"固执", "开朗", "大胆"}
	other := "冷静"
	var gids []uint32
	gid := uint32(1)
	for _, n := range append(append([]string{}, want...), other) {
		p := mkPet(st.gd, gid, 2000672, 3006)
		p.Nature = n
		if _, err := sc.UpsertPet(p); err != nil {
			t.Fatalf("写入 gid=%d 性格=%s: %v", p.Gid, n, err)
		}
		gids = append(gids, gid)
		gid++
	}

	all, total, err := sc.ListPets(Filter{Page: 1, PageSize: 50})
	if err != nil {
		t.Fatalf("列出全部: %v", err)
	}
	if total != len(gids) {
		t.Fatalf("准备失败: 总数 = %d, 期望 %d", total, len(gids))
	}
	if len(all) != len(gids) {
		t.Fatalf("准备失败: 返回 %d 只, 期望 %d", len(all), len(gids))
	}

	// 只选「固执」(NatureIn 的单项形式):应恰好 1 只
	pets, total, err := sc.ListPets(Filter{NatureIn: []string{"固执"}, Page: 1, PageSize: 50})
	if err != nil {
		t.Fatalf("按 NatureIn 单项查询: %v", err)
	}
	if total != 1 {
		t.Errorf("NatureIn=[固执] 总数 = %d, 期望 1", total)
	} else if len(pets) != 1 || pets[0].Nature != "固执" {
		t.Errorf("NatureIn=[固执] 返回 %+v, 期望恰一只固执", pets)
	}

	// 选三个:应恰好 3 只。得 1 只说明退化成了 AND 或只取了第一个 ——
	// 这是本测试最要紧的断言(单项形式测不出来)。
	pets, total, err = sc.ListPets(Filter{NatureIn: want, Page: 1, PageSize: 50})
	if err != nil {
		t.Fatalf("按 NatureIn 多项查询: %v", err)
	}
	if total != len(want) {
		t.Errorf("NatureIn 多项 总数 = %d, 期望 %d —— 若得 1 说明退化成了 AND", total, len(want))
	}
	seen := map[string]bool{}
	for _, p := range pets {
		seen[p.Nature] = true
	}
	for _, n := range want {
		if !seen[n] {
			t.Errorf("结果里缺性格 %q", n)
		}
	}
	if seen[other] {
		t.Errorf("结果里混入了未选的性格 %q", other)
	}
}

// TestFilterNatureInAndExclude 多选与排除同时给时,两者都要生效(互不吞掉)。
func TestFilterNatureInAndExclude(t *testing.T) {
	st := newTestStore(t)
	sc := st.For(testAcc)

	for i, n := range []string{"固执", "开朗", "大胆"} {
		p := mkPet(st.gd, uint32(i+1), 2000672, 3006)
		p.Nature = n
		if _, err := sc.UpsertPet(p); err != nil {
			t.Fatalf("写入: %v", err)
		}
	}
	// 三个全选,再排除「大胆」→ 应剩 2 只
	pets, total, err := sc.ListPets(Filter{
		NatureIn:      []string{"固执", "开朗", "大胆"},
		NatureExclude: []string{"大胆"},
		Page:          1, PageSize: 50,
	})
	if err != nil {
		t.Fatalf("查询: %v", err)
	}
	if total != 2 {
		t.Errorf("总数 = %d, 期望 2(IN 命中 3 个,排除 1 个)", total)
	}
	for _, p := range pets {
		if p.Nature == "大胆" {
			t.Errorf("排除条件未生效,结果里仍有大胆")
		}
	}
}

// TestNatureColumnRoundTrip 性格确实落到**列**上(ListPets 读列而非 blob 里的副本);
// 若哪天改成只写 blob,上面两条的筛选会静默失效。
func TestNatureColumnRoundTrip(t *testing.T) {
	st := newTestStore(t)
	sc := st.For(testAcc)
	p := mkPet(st.gd, 1, 2000672, 3006)
	p.Nature = "急躁"
	if _, err := sc.UpsertPet(p); err != nil {
		t.Fatal(err)
	}
	pets, _, err := sc.ListPets(Filter{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(pets) != 1 {
		t.Fatalf("返回 %d 只, 期望 1", len(pets))
	}
	if pets[0].Nature != "急躁" {
		t.Errorf("读回的性格 = %q, 期望 急躁", pets[0].Nature)
	}
}
