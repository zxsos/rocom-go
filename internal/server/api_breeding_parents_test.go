package server

import (
	"encoding/json"
	"testing"

	"github.com/zxsos/rocom-go/internal/pet"
)

// 本文件钉住「线上指定种母 / 种公」在**接口层**的读写:建线时带上 gid,读取时要拿到对应的
// 亲本快照;指定的那只已经不在库(放生 / 送人)时给 released 标记。
//
// 为什么值得单独测:这几个字段全是 omitempty,漏接的表现是「页面亲本那一栏永远是空的」——
// Go 编译、落库、别的接口测试全都照样绿,只有页面空着。更要紧的是「查不到」与「没指定」
// 在前端是两种完全不同的显示(已不在库 / 未指定):混了就会让玩家去等一只永远不会回来的宠物。
//
// ⚠️ 已做变异测试(每条断言都验证过会红):
//   - 去掉 breedingView 里的 father 注入        → 快照断言红
//   - Father == nil 就一律置 FatherReleased     → 「没指定≠已不在库」断言红
//   - 保存时拿 FatherGid 去填各代的父本          → 「计划值不写进事实」断言红

// seedParent 入库一只可作亲本的宠物(gid 与性别由调用方给)。
func seedParent(t *testing.T, s *Server, gid uint32, name, gender string) {
	t.Helper()
	p := &pet.Pet{
		Gid: gid, ConfID: 2000672, BaseConfID: 3006,
		Species: "火神", Name: name, Level: 60, Gender: gender,
		Nature: "固执", Voice: 10, HeightM: 1.6, WeightKg: 80,
	}
	if _, err := s.store.For(contractAcc).UpsertPet(p); err != nil {
		t.Fatalf("入库 %s(gid=%d): %v", name, gid, err)
	}
}

// findLine 取 GET /api/breeding 下发的那条线。
func findLine(t *testing.T, s *Server, id string) BreedingLinePayload {
	t.Helper()
	body := get(t, s, "/api/breeding?account="+contractAcc)
	var resp struct {
		Lines []BreedingLinePayload `json:"lines"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("解 /api/breeding 响应: %v\n%s", err, body)
	}
	for _, l := range resp.Lines {
		if l.ID == id {
			return l
		}
	}
	t.Fatalf("响应里没有线 %s(共 %d 条)", id, len(resp.Lines))
	return BreedingLinePayload{}
}

// TestBreedingPoolWithoutRef 不带品种调候选池 = 「还没定品种」:要给**全库**的雌雄。
//
// 为什么需要它:建线表单允许先挑种母(再由她把品种带出来,蛋随母本),那一刻前端还没有品种可传。
// 这里若报 400 或返回空列表,「先选种母」这一步就根本没法开始 —— 而它在界面上只表现为
// 「下拉里什么都没有」,完全不报错。
//
// 样本特意用**两个不同品种**:只按同品种筛的实现也能让每类凑出一只,分不出来。
func TestBreedingPoolWithoutRef(t *testing.T) {
	s := newTestServer(t)
	sc := s.store.For(contractAcc)
	mk := func(gid uint32, name, gender string, confID, baseID uint32) {
		p := &pet.Pet{
			Gid: gid, ConfID: confID, BaseConfID: baseID,
			Species: name, Name: name, Level: 30, Gender: gender, Nature: "固执",
		}
		if _, err := sc.UpsertPet(p); err != nil {
			t.Fatalf("写入 %s(%s): %v", name, gender, err)
		}
	}
	mk(4001, "火的母", "♀", 2000672, 3006) // 火神那条链
	mk(4002, "水的母", "♀", 3001, 3001)    // 另一个品种
	mk(4003, "火的公", "♂", 2000672, 3006)
	mk(4004, "水的公", "♂", 3001, 3001)

	// 不给 evo / species:应当 200 而不是 400(见 get 的 Fatal),且给的是全库雌雄。
	body := get(t, s, "/api/breeding/pool?account="+contractAcc)
	var resp struct {
		Mothers []pet.PetCandidate `json:"mothers"`
		Fathers []pet.PetCandidate `json:"fathers"`
		Kids    []pet.PetCandidate `json:"kids"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("解响应: %v\n%s", err, body)
	}
	if len(resp.Mothers) != 2 {
		t.Errorf("种母候选 = %d 只, 期望 2(全库的 ♀,不按品种筛)", len(resp.Mothers))
	}
	if len(resp.Fathers) != 2 {
		t.Errorf("种公候选 = %d 只, 期望 2(全库的 ♂)", len(resp.Fathers))
	}
	if len(resp.Kids) != 0 {
		t.Errorf("子代候选 = %d 只, 期望 0(没定品种时给不出有意义的子代范围)", len(resp.Kids))
	}
	for _, m := range resp.Mothers {
		if m.Gender != "♀" {
			t.Errorf("种母队列里混进了 %s(%s)", m.Gender, m.Name)
		}
	}
	// 候选必须带**所属进化链**:建线表单选完种母就靠它把「品种(蛋)」自动填好(蛋随母本)。
	// 这条字段一直都在 schema 里、只是恒为 0 —— 线上一度因此「选完种母品种是空的」,
	// 而接口层的其它断言全都照样绿。
	wantEvo := s.db.ChainOf(3006)
	got := 0
	for _, m := range resp.Mothers {
		if m.Name == "火的母" {
			got = int(m.Evo)
		}
	}
	if got != int(wantEvo) || got == 0 {
		t.Errorf("候选的 Evo = %d, 期望 %d(火神那条链)—— 前端据此填品种", got, wantEvo)
	}
}

// TestBreedingLineParentsDispatch 建线时指定的种母/种公,读取时要变成对应快照下发。
func TestBreedingLineParentsDispatch(t *testing.T) {
	s := newTestServer(t)
	seedParent(t, s, 4001, "种母", "♀")
	seedParent(t, s, 4002, "种公", "♂")

	saveLine(t, s, pet.BreedingLine{
		ID: "parents-dispatch", Species: "火神", Status: pet.BreedingActive,
		MotherGid: 4001, FatherGid: 4002,
	})

	got := findLine(t, s, "parents-dispatch")
	if got.Mother == nil || got.Mother.Gid != 4001 {
		t.Errorf("种母快照 = %+v, 期望 gid=4001", got.Mother)
	}
	if got.Father == nil || got.Father.Gid != 4002 {
		t.Errorf("种公快照 = %+v, 期望 gid=4002 —— 页面亲本那一栏会空着", got.Father)
	}
	if got.FatherReleased || got.MotherReleased {
		t.Errorf("两只都在库里,不该置已放生标记(种母 %v / 种公 %v)", got.MotherReleased, got.FatherReleased)
	}
	// 快照是**计划值**,必须能一眼看出是它本人:名字来自库里那只,而不是 gid 的回显。
	if got.Father != nil && got.Father.Species != "火神" {
		t.Errorf("种公快照的物种 = %q, 期望取自库里的火神", got.Father.Species)
	}
}

// TestBreedingParentNotInLibrary 指定的那只已不在库时给 released 标记,
// 且「没指定」与「指定的那只不在了」必须分得开 —— 前端一处写「已不在库」、另一处写「未指定」。
func TestBreedingParentNotInLibrary(t *testing.T) {
	s := newTestServer(t)
	saveLine(t, s, pet.BreedingLine{
		ID: "parents-released", Species: "火神", Status: pet.BreedingActive,
		// 只有种公,且他不在库里;种母压根没指定。
		FatherGid: 9999,
	})

	got := findLine(t, s, "parents-released")
	if got.Father != nil {
		t.Errorf("种公不在库里,快照该为空,却拿到 %+v", got.Father)
	}
	if !got.FatherReleased {
		t.Error("指定过种公但他不在库,该置 fatherReleased=true")
	}
	// 关键的反向断言:种母**没指定过**,不能跟着被标成"已放生"。
	// 少了它,「快照为空就置 released」这种写法会一路绿 —— 而玩家会看到「种母已放生」,
	// 明明他建线时就没选种母。
	if got.MotherReleased {
		t.Error("没指定种母 ≠ 种母已放生,不该置 motherReleased")
	}

	// 更直接的一条:两位都没指定的线,两个 released 都必须是 false。
	// 这条与上面那条一起,把「查不到」和「没指定」彻底分开(前端靠它决定写「已不在库」还是「未指定」)。
	saveLine(t, s, pet.BreedingLine{ID: "parents-none", Species: "火神", Status: pet.BreedingActive})
	none := findLine(t, s, "parents-none")
	if none.Mother != nil || none.Father != nil || none.MotherReleased || none.FatherReleased {
		t.Errorf("两位都没指定:母 %+v/公 %+v,已放生标记 母 %v/公 %v —— 期望全空且不置位",
			none.Mother, none.Father, none.MotherReleased, none.FatherReleased)
	}
}

// TestBreedingFatherGidNeverFillsFacts 指定种公**只是计划值**:它不许被用来给任何一代补父本。
//
// 这条防的是「顺手把计划填进历史」:真做过这个"优化"的话,抓包抓到的真实父本会被计划值顶掉,
// 或者给"还没配过"的那一代凭空写上一个父亲 —— 培育史从此不再可信,而界面上看不出来。
func TestBreedingFatherGidNeverFillsFacts(t *testing.T) {
	s := newTestServer(t)
	seedParent(t, s, 4002, "种公", "♂")
	saveLine(t, s, pet.BreedingLine{
		ID: "parents-no-fill", Species: "火神", Status: pet.BreedingActive,
		FatherGid: 4002,
		// 这一代爸爸是谁还没确认(串窝),故不给 Father。
		Gens: []pet.Generation{{
			Gen: 1, Source: pet.GenSourceAuto,
			Mother: &pet.EggParent{Gid: 4001, Name: "种母", Species: "火神"},
		}},
	})

	got := findLine(t, s, "parents-no-fill")
	if got.Father == nil || got.Father.Gid != 4002 {
		t.Fatalf("线上指定的种公没有下发: %+v", got.Father)
	}
	if len(got.Gens) != 1 {
		t.Fatalf("代数 = %d, 期望 1", len(got.Gens))
	}
	if got.Gens[0].Father != nil {
		t.Errorf("这一代被凭空填上了父本 %+v —— 计划值不许写进历史事实", got.Gens[0].Father)
	}
}
