package server

import (
	"encoding/json"
	"net/url"
	"slices"
	"strconv"
	"testing"

	"github.com/zxsos/rocom-go/internal/gamedata"
	"github.com/zxsos/rocom-go/internal/pet"
)

// 本文件钉住宠物列表**查询参数名**这条接线。
//
// 为什么单拎出来测:参数名是前后端之间唯一的接点,而它写错时**没有任何报错** ——
// 后端 `q.Get` 拿不到值就只当这个条件没传,/api/pets 照常返回(只是没筛),前端也照常
// 渲染。表现就是列表长按「筛选相同蛋组」后什么都没发生。语义本身由 store 的单测覆盖,
// 这里只回答一件事:参数到底有没有接上。

// seedEggPets 写四只蛋组不同的宠物:目标那一套、同两组但写入顺序相反、在目标基础上多带一组、
// 以及只有一个组的。四种足以区分「精确」「任一命中」,也能验出「只比个数不比内容」的实现。
func seedEggPets(t *testing.T, s *Server) {
	t.Helper()
	sc := s.store.For(contractAcc)
	for i, gs := range [][]string{{"天空", "龙"}, {"龙", "天空"}, {"天空", "草"}, {"天空"}} {
		p := &pet.Pet{
			Gid: uint32(2001 + i), ConfID: 2000672, BaseConfID: 3006,
			Species: "火神", Name: "蛋组样本", Level: 60, Gender: "♀",
		}
		p.Image = s.db.PetImageByBase(p.BaseConfID, false)
		for _, n := range gs {
			p.EggGroups = append(p.EggGroups, gamedata.EggGroup{Name: n})
		}
		if _, err := sc.UpsertPet(p); err != nil {
			t.Fatalf("写入蛋组样本 gid=%d: %v", p.Gid, err)
		}
	}
}

// getPetsRaw 取 /api/pets 响应里的 total 与 gid 列表(gid 升序,便于与期望直接比)。
func getPetsRaw(t *testing.T, s *Server, query string) (int, []uint32) {
	t.Helper()
	body := get(t, s, "/api/pets?"+query+"&account="+contractAcc)
	var resp struct {
		Total int `json:"total"`
		Pets  []struct {
			Gid uint32 `json:"gid"`
		} `json:"pets"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("解 /api/pets 响应: %v\n%s", err, body)
	}
	gids := make([]uint32, 0, len(resp.Pets))
	for _, p := range resp.Pets {
		gids = append(gids, p.Gid)
	}
	slices.Sort(gids)
	return resp.Total, gids
}

// TestPetsEggGroupsExactParam eggGroupsExact 要真的接上,且与 eggGroups(OR)口径不同。
func TestPetsEggGroupsExactParam(t *testing.T) {
	s := newTestServer(t)
	seedEggPets(t, s)

	// 精确:要求蛋组完全一致,故「多带一组」的 2003 不能进来,顺序不同的 2002 要在。
	total, gids := getPetsRaw(t, s, "eggGroupsExact="+url.QueryEscape("天空,龙"))
	if want := []uint32{2001, 2002}; !slices.Equal(gids, want) {
		t.Errorf("eggGroupsExact=天空,龙 命中 %v, 期望 %v", gids, want)
	}
	if total != 2 {
		t.Errorf("eggGroupsExact=天空,龙 总数 = %d, 期望 2", total)
	}

	// 单组:2001/2002/2003 都是「含天空但还带别的组」的,不该被「精确 天空」带出来
	// (这正是与 OR 的差别),只留蛋组恰好是天空的 2004。
	total, gids = getPetsRaw(t, s, "eggGroupsExact="+url.QueryEscape("天空"))
	if want := []uint32{2004}; !slices.Equal(gids, want) {
		t.Errorf("eggGroupsExact=天空 命中 %v, 期望 %v(只留蛋组恰好是天空的)", gids, want)
	}
	if total != 1 {
		t.Errorf("eggGroupsExact=天空 总数 = %d, 期望 1", total)
	}

	// 老口径不能受影响:OR 下「天空」应含全部四只。
	total, gids = getPetsRaw(t, s, "eggGroups="+url.QueryEscape("天空"))
	if want := []uint32{2001, 2002, 2003, 2004}; !slices.Equal(gids, want) {
		t.Errorf("eggGroups=天空 命中 %v, 期望 %v(任一命中)", gids, want)
	}
	if total != 4 {
		t.Errorf("eggGroups=天空 总数 = %d, 期望 4", total)
	}
}

// TestPetPageEggGroupsExactParam pet-page 与 pets 共用 parseFilter,但它是**独立的一条
// 路由**:共用了没接上照样是静默不筛 —— 而它的用途正是「长按筛完跳到某只宠所在页」,
// 不筛会让页码指向错误的位置(跳过去是另一批宠物)。
func TestPetPageEggGroupsExactParam(t *testing.T) {
	s := newTestServer(t)
	seedEggPets(t, s)

	for _, tc := range []struct {
		name  string
		exact string
		gid   uint32
		want  bool
	}{
		{"完全一致:命中", "天空,龙", 2002, true},
		{"完全一致:组数不同不命中", "天空", 2001, false},
		{"完全一致:多带一组不命中", "天空,龙", 2003, false},
	} {
		body := get(t, s, "/api/pet-page?gid="+strconv.FormatUint(uint64(tc.gid), 10)+
			"&eggGroupsExact="+url.QueryEscape(tc.exact)+"&account="+contractAcc)
		var resp struct {
			Found bool `json:"found"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			t.Fatalf("%s: 解响应: %v\n%s", tc.name, err, body)
		}
		if resp.Found != tc.want {
			t.Errorf("%s: gid=%d eggGroupsExact=%s found=%v, 期望 %v", tc.name, tc.gid, tc.exact, resp.Found, tc.want)
		}
	}
}
