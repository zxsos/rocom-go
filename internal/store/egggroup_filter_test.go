package store

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"

	"github.com/zxsos/rocom-go/internal/gamedata"
	"github.com/zxsos/rocom-go/internal/pet"
)

// eggGroupsOf 造一组蛋组只填名字:egg_groups 列与筛选谓词都只认名字
// (见 pet.eggGroupsFromNames —— 列里存的是组名 JSON 数组,不是 id)。
func eggGroupsOf(names ...string) []gamedata.EggGroup {
	out := make([]gamedata.EggGroup, 0, len(names))
	for _, n := range names {
		out = append(out, gamedata.EggGroup{Name: n})
	}
	return out
}

// gidsOf 取出返回宠物的 gid 集合并升序排好,便于与期望的「命中哪几只」直接比。
// 空结果返回 nil —— 与期望值里的 nil 同形,免得每次都要写一个空切片。
func gidsOf(pets []*pet.Pet) []uint32 {
	if len(pets) == 0 {
		return nil
	}
	out := make([]uint32, 0, len(pets))
	for _, p := range pets {
		out = append(out, p.Gid)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// TestFilterEggGroupsExact 列表长按的「筛选相同蛋组」:候选的蛋组必须与目标**完全一致**。
//
// 为什么值得单独钉住:这条谓词写错时表现都是**安静**的 —— 退化成 OR 会混进一堆只共一个
// 蛋组的宠物(看起来像「筛选没生效」),漏掉个数比较会放进「多带一组」的宠物(配种时才
// 发现配错了)。两种都不报错,故逐种情形比「命中哪几只」而不只是比条数。
func TestFilterEggGroupsExact(t *testing.T) {
	st := newTestStore(t)
	sc := st.For(testAcc)

	// 五种情形:两组、同两组但顺序相反、单组、多带一组、空蛋组(超进化/分支形态那种)。
	cases := []struct {
		gid    uint32
		groups []string
	}{
		{1, []string{"天空", "龙"}},
		{2, []string{"龙", "天空"}}, // 与 gid=1 是同一套,只是写入顺序不同
		{3, []string{"天空"}},
		{4, []string{"天空", "草"}},
		{5, nil}, // 空蛋组
	}
	for _, c := range cases {
		p := mkPet(st.gd, c.gid, 2000672, 3006)
		p.EggGroups = eggGroupsOf(c.groups...)
		if _, err := sc.UpsertPet(p); err != nil {
			t.Fatalf("写入 gid=%d: %v", c.gid, err)
		}
	}

	for _, tc := range []struct {
		name  string
		exact []string
		want  []uint32
	}{
		{"两组", []string{"天空", "龙"}, []uint32{1, 2}},
		{"顺序无关", []string{"龙", "天空"}, []uint32{1, 2}},
		{"单组:不能带上多一组的", []string{"天空"}, []uint32{3}},
		{"只有一组但库里没有", []string{"龙"}, nil},
		{"另一套两组", []string{"天空", "草"}, []uint32{4}},
		{"库里没有的蛋组", []string{"幽灵"}, nil},
	} {
		pets, total, err := sc.ListPets(Filter{EggGroupsExact: tc.exact, Page: 1, PageSize: 50})
		if err != nil {
			t.Fatalf("%s: 查询: %v", tc.name, err)
		}
		if got := gidsOf(pets); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: eggGroupsExact=%v 命中 %v, 期望 %v", tc.name, tc.exact, got, tc.want)
		}
		if total != len(tc.want) {
			t.Errorf("%s: 总数 = %d, 期望 %d", tc.name, total, len(tc.want))
		}
	}
}

// TestEggGroupsExactNeverMatchesEmpty 空蛋组的宠物在精确口径下**永不命中**:
// 「没有蛋组」不是「一套叫空的蛋组」(见 gamedata.IsInfertile 的注释:超进化/分支形态只
// 是没配蛋组,实测 64 个形态)。写进 json_array_length 时它返回 0、写 LIKE 时它不成立。
func TestEggGroupsExactNeverMatchesEmpty(t *testing.T) {
	st := newTestStore(t)
	sc := st.For(testAcc)
	p := mkPet(st.gd, 1, 2000672, 3006)
	p.EggGroups = nil
	if _, err := sc.UpsertPet(p); err != nil {
		t.Fatal(err)
	}
	// 任意一组都不该命中它;空切片是「未启用筛选」而不是「筛空组」,故不在此列。
	for _, g := range []string{"天空", "龙", "巨灵", "未发现"} {
		if _, total, err := sc.ListPets(Filter{EggGroupsExact: []string{g}, Page: 1, PageSize: 50}); err != nil {
			t.Fatalf("查询 %s: %v", g, err)
		} else if total != 0 {
			t.Errorf("eggGroupsExact=[%s] 命中了 %d 只空蛋组的宠物, 期望 0", g, total)
		}
	}
}

// TestFilterEggGroupsOrUnchanged 精确口径落地后,筛选面板那套 OR 口径必须原样保留 ——
// 「这几组里有哪些」本来就该把「多带一组」的也算上。改精确谓词时最容易顺手把 OR 也
// 加成 AND,那时面板上选的组会静默少出一批宠物。
func TestFilterEggGroupsOrUnchanged(t *testing.T) {
	st := newTestStore(t)
	sc := st.For(testAcc)
	for i, gs := range [][]string{{"天空", "龙"}, {"天空"}, {"天空", "草"}, {"巨灵"}} {
		p := mkPet(st.gd, uint32(i+1), 2000672, 3006)
		p.EggGroups = eggGroupsOf(gs...)
		if _, err := sc.UpsertPet(p); err != nil {
			t.Fatalf("写入 gid=%d: %v", i+1, err)
		}
	}
	pets, total, err := sc.ListPets(Filter{EggGroups: []string{"天空"}, Page: 1, PageSize: 50})
	if err != nil {
		t.Fatalf("查询: %v", err)
	}
	if want := []uint32{1, 2, 3}; !reflect.DeepEqual(gidsOf(pets), want) {
		t.Errorf("eggGroups=[天空] 命中 %v, 期望 %v(任一命中)", gidsOf(pets), want)
	}
	if total != 3 {
		t.Errorf("eggGroups=[天空] 总数 = %d, 期望 3", total)
	}
}

// TestFilterEggGroupsExactWinsOverOr 两种口径同时给出时以精确为准(与 Nature/NatureIn 同法)。
// 前端只会给一个,这条钉的是「叠加生效」——那种情况下面板上选中的组没生效、列表却按另一种
// 口径收窄了,用户完全看不出发生了什么。
//
// OR 那份特意选**与精确集不相交**的「巨灵」:若拿 [天空,龙] 当 OR,它恰好把精确集包在里面
// (精确命中的一定也命中 OR),叠加生效与「精确优先」会给出同一个结果 —— 这条断言就废了。
func TestFilterEggGroupsExactWinsOverOr(t *testing.T) {
	st := newTestStore(t)
	sc := st.For(testAcc)
	for i, gs := range [][]string{{"天空", "龙"}, {"天空"}, {"天空", "草"}} {
		p := mkPet(st.gd, uint32(i+1), 2000672, 3006)
		p.EggGroups = eggGroupsOf(gs...)
		if _, err := sc.UpsertPet(p); err != nil {
			t.Fatalf("写入 gid=%d: %v", i+1, err)
		}
	}
	pets, _, err := sc.ListPets(Filter{
		EggGroups:      []string{"巨灵"},
		EggGroupsExact: []string{"天空"},
		Page:           1, PageSize: 50,
	})
	if err != nil {
		t.Fatalf("查询: %v", err)
	}
	if want := []uint32{2}; !reflect.DeepEqual(gidsOf(pets), want) {
		t.Errorf("同时给出两种口径时命中 %v, 期望 %v(以 eggGroupsExact=[天空] 为准)", gidsOf(pets), want)
	}
}

// TestEggGroupsColumnIsNameArray 精确谓词直接吃 egg_groups 列(JSON1 的 json_array_length
// 与 LIKE),故这列必须是**组名 JSON 数组**:哪天改成存 id、或改存别处的副本,精确筛选与
// 面板的 OR 筛选都会静默变成空结果(不报错)。
func TestEggGroupsColumnIsNameArray(t *testing.T) {
	st := newTestStore(t)
	sc := st.For(testAcc)
	p := mkPet(st.gd, 1, 2000672, 3006)
	p.EggGroups = eggGroupsOf("天空", "龙")
	if _, err := sc.UpsertPet(p); err != nil {
		t.Fatal(err)
	}
	var col string
	if err := sc.rdb.QueryRow(`SELECT egg_groups FROM pets WHERE account=? AND gid=?`,
		testAcc, p.Gid).Scan(&col); err != nil {
		t.Fatalf("读 egg_groups 列: %v", err)
	}
	var names []string
	if err := json.Unmarshal([]byte(col), &names); err != nil {
		t.Fatalf("egg_groups 列不是 JSON 数组(%q): %v", col, err)
	}
	if !reflect.DeepEqual(names, []string{"天空", "龙"}) {
		t.Errorf("egg_groups 列 = %q, 期望 [\"天空\",\"龙\"]", col)
	}
}
