package store

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/whoisnian/rocom-capture/internal/gamedata"
	"github.com/whoisnian/rocom-capture/internal/pet"
)

const testAcc = "UID:1"

func newTestStore(t *testing.T) *Store {
	t.Helper()
	gd, err := gamedata.Load()
	if err != nil {
		t.Fatalf("加载名称库: %v", err)
	}
	st, err := New(filepath.Join(t.TempDir(), "t.db"), gd)
	if err != nil {
		t.Fatalf("打开数据库: %v", err)
	}
	// 不 Close 的话 SQLite 句柄一直占着文件,Windows 上 TempDir 清理必然失败
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// mkPet 造一只最小可用的宠物,image 按 pet.ToPet 的算式填好(优先当前形态 base_conf_id,
// 回退 conf_id)。conf_id/base_conf_id 取真实存在的一对:火神 conf 2000672 属于 petbase 3006,
// 二者头像不同,足以暴露「只按 conf_id 取图会拿到进化线一阶」的错。
func mkPet(gd *gamedata.DB, gid uint32, confID, baseConfID uint32) *pet.Pet {
	p := &pet.Pet{
		Gid: gid, ConfID: confID, BaseConfID: baseConfID,
		Species: "火神", Name: "火神", Level: 60, Nature: "固执",
	}
	p.Image = gd.PetImage(confID, p.Shiny)
	if baseConfID != 0 {
		if img := gd.PetImageByBase(baseConfID, p.Shiny); img != (gamedata.PetImage{}) {
			p.Image = img
		}
	}
	return p
}

// TestPetHeadsMatchesBlob 校验 petHeads 只查 conf_id/base_conf_id/shiny 三列算出的头像,与
// 同一行 data blob 里存着的 image.head 一致——这正是不再逐条解 blob 的前提(见 petHeads)。
func TestPetHeadsMatchesBlob(t *testing.T) {
	st := newTestStore(t)
	sc := st.For(testAcc)

	// 覆盖两类:已进化(base 与 conf 指向不同 petbase)、未进化(指向同一个)。
	pets := []*pet.Pet{
		mkPet(st.gd, 1, 2000672, 3006),
		mkPet(st.gd, 2, 3001, 3001),
	}
	gids := make([]uint32, len(pets))
	for i, p := range pets {
		gids[i] = p.Gid
		if _, err := sc.UpsertPet(p); err != nil {
			t.Fatalf("写入 gid=%d: %v", p.Gid, err)
		}
	}
	if pets[0].Image.Head == pets[1].Image.Head {
		t.Fatalf("两只用例宠物头像相同(%q),分不出按 base 还是按 conf 取图", pets[0].Image.Head)
	}

	heads := sc.petHeads(gids)
	for _, p := range pets {
		var blob string
		if err := sc.rdb.QueryRow(`SELECT data FROM pets WHERE account=? AND gid=?`,
			testAcc, p.Gid).Scan(&blob); err != nil {
			t.Fatalf("读 gid=%d 的 data: %v", p.Gid, err)
		}
		var stored pet.Pet
		if err := json.Unmarshal([]byte(blob), &stored); err != nil {
			t.Fatalf("解 gid=%d 的 data: %v", p.Gid, err)
		}
		if want := stored.Image.Head; heads[strconv.FormatUint(uint64(p.Gid), 10)] != want {
			t.Errorf("gid=%d 头像 = %q, blob 里是 %q",
				p.Gid, heads[strconv.FormatUint(uint64(p.Gid), 10)], want)
		} else if want == "" {
			t.Errorf("gid=%d 头像为空,用例没起到校验作用", p.Gid)
		}
	}
}

// TestListBreedingPetsParity 窄投影(ListBreedingPets)与整条 data(ListAllPets)必须
// 给出同一份培育用字段。
//
// 为什么需要这条:培育页改用窄投影后,两条路读的是**不同的列**,而分叉的表现是
// 「建议里平白少一只母本」「头像变成进化前的样子」—— 不报错、也看不出来,只能钉在这。
//
// 只比培育页真正会用的那几项(见 ListBreedingPets 的注释):六维、奖牌之类本就不填,不比。
// 百分位两边都由 FillSizePercentile 现算,故先对齐再比,拿到手的就是可比的。
func TestListBreedingPetsParity(t *testing.T) {
	st := newTestStore(t)
	sc := st.For(testAcc)

	// 三种最容易分叉的情形:
	//   ① 已进化(base ≠ conf)—— 头像要按 base 取,按 conf 取会拿到进化线一阶的样子;
	//   ② 未进化(base == conf);
	//   ③ 异色 —— 有专属头像,取图时漏传 shiny 会退回普通图。
	sid := shinyBase(t, st.gd)
	pets := []*pet.Pet{
		mkPet(st.gd, 1, 2000672, 3006),
		mkPet(st.gd, 2, 3001, 3001),
		mkPet(st.gd, 3, sid, sid), // 异色 + 形态 id 可用:走「按 base 取」那一路
		mkPet(st.gd, 4, sid, 0),   // 异色 + 形态 id 缺失:走「退回 conf」那一路
	}
	pets[0].Gender, pets[0].Voice, pets[0].TalentRank = "♀", -73, "优秀"
	pets[1].Gender, pets[1].Voice, pets[1].Nature = "♂", 100, "坦率"
	pets[2].Gender, pets[2].Voice, pets[2].Shiny = "♀", 0, true
	pets[3].Gender, pets[3].Voice, pets[3].Shiny = "♂", 42, true
	// 异色头像要在 Shiny 置位之后重算。这里**不用** pet.FillPetImage 算 —— 那正是被测的
	// 那个函数,拿它造期望值等于自己跟自己比(漏传 shiny 这种错它两边一起错,测不出来)。
	pets[2].Image = st.gd.PetImage(pets[2].ConfID, true)
	if img := st.gd.PetImageByBase(pets[2].BaseConfID, true); img != (gamedata.PetImage{}) {
		pets[2].Image = img
	}
	pets[3].Image = st.gd.PetImage(pets[3].ConfID, true)

	for i, p := range pets {
		p.Name = fmt.Sprintf("宠物%d", i+1)
		p.HeightM, p.WeightKg = 1.2+float64(i)*0.1, 30+float64(i)*5
		// 蛋组与写库时同源:窄投影读 egg_groups 列、全量读 data,两边都得是同一份
		p.EggGroups = st.gd.PetEggGroups(p.BaseConfID)
		if _, err := sc.UpsertPet(p); err != nil {
			t.Fatalf("写入 gid=%d: %v", p.Gid, err)
		}
	}
	if pets[0].Image.Head == pets[1].Image.Head {
		t.Fatalf("已进化与未进化的头像相同(%q),分不出按 base 还是按 conf 取图", pets[0].Image.Head)
	}
	for _, p := range pets[2:] {
		if p.Image.Head == "" {
			t.Fatalf("gid=%d 头像为空,异色那条用例没起到校验作用", p.Gid)
		}
	}

	full, err := sc.ListAllPets()
	if err != nil {
		t.Fatalf("ListAllPets: %v", err)
	}
	narrow, err := sc.ListBreedingPets()
	if err != nil {
		t.Fatalf("ListBreedingPets: %v", err)
	}
	pet.FillSizePercentile(st.gd, full...) // 培育页拿到全量结果也会先过这一遍
	if len(full) != len(narrow) {
		t.Fatalf("条数 %d vs %d:窄投影丢了行(见 breedingPetSQL 的 COALESCE)", len(narrow), len(full))
	}
	byGid := make(map[uint32]*pet.Pet, len(full))
	for _, p := range full {
		byGid[p.Gid] = p
	}
	for _, got := range narrow {
		want := byGid[got.Gid]
		if want == nil {
			t.Errorf("gid=%d 只出现在窄投影里", got.Gid)
			continue
		}
		if got.ConfID != want.ConfID || got.BaseConfID != want.BaseConfID ||
			got.Species != want.Species || got.Name != want.Name || got.Gender != want.Gender ||
			got.HeightM != want.HeightM || got.WeightKg != want.WeightKg ||
			got.Voice != want.Voice || got.Nature != want.Nature ||
			got.TalentRank != want.TalentRank || got.Shiny != want.Shiny {
			t.Errorf("gid=%d 窄 = %+v, 全 = %+v", got.Gid, got, want)
		}
		if got.Image != want.Image {
			t.Errorf("gid=%d image = %+v, 期望 %+v", got.Gid, got.Image, want.Image)
		}
		if !samePct(got.HeightPct, want.HeightPct) {
			t.Errorf("gid=%d heightPct = %v, 期望 %v", got.Gid, got.HeightPct, want.HeightPct)
		}
		if !samePct(got.WeightPct, want.WeightPct) {
			t.Errorf("gid=%d weightPct = %v, 期望 %v", got.Gid, got.WeightPct, want.WeightPct)
		}
		// 蛋组只比**组名**:egg_groups 列存的就只有名字(见 eggGroupsFromNames),
		// ID 与官方描述培育页用不上,故不参与比对。
		if eggNamesOf(got.EggGroups) != eggNamesOf(want.EggGroups) {
			t.Errorf("gid=%d 蛋组 = %q, 期望 %q",
				got.Gid, eggNamesOf(got.EggGroups), eggNamesOf(want.EggGroups))
		}
	}
}

// shinyBase 找一个**确有异色专属头像**的形态。
//
// 不是每个形态都有:见 gamedata.imageOf —— 索引里没有 SH、或对应 webp 没 embed 时一律
// 回退普通图。随便挑一只(比如固定用 3006)会让「shiny 有没有传下去」这条断言落空。
func shinyBase(t *testing.T, gd *gamedata.DB) uint32 {
	t.Helper()
	for id := uint32(1); id <= 20000; id++ {
		if info, ok := gd.PetBase(id); !ok || info.Name == "" {
			continue
		}
		if gd.PetImageByBase(id, true) != gd.PetImageByBase(id, false) {
			return id
		}
	}
	t.Fatal("gamedata 里没有任何形态带异色专属头像,shiny 分支无从覆盖")
	return 0
}

func samePct(a, b *float64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func eggNamesOf(gs []gamedata.EggGroup) string {
	names := make([]string, 0, len(gs))
	for _, g := range gs {
		names = append(names, g.Name)
	}
	return strings.Join(names, ",")
}

// TestConcurrentReadWhileWrite 压一遍读写分池:多个读者与写者同时干活,不该出现
// "database is locked"。此前读写共用单连接时不可能撞上,分池后才需要 WAL 兜住。
func TestConcurrentReadWhileWrite(t *testing.T) {
	st := newTestStore(t)
	sc := st.For(testAcc)
	for gid := uint32(1); gid <= 50; gid++ {
		if _, err := sc.UpsertPet(mkPet(st.gd, gid, 2000672, 3006)); err != nil {
			t.Fatalf("预置 gid=%d: %v", gid, err)
		}
	}

	var wg sync.WaitGroup
	errs := make(chan error, 64)
	stop := make(chan struct{})

	wg.Add(1)
	go func() { // 写者:模拟抓包侧持续 upsert
		defer wg.Done()
		for i := uint32(0); i < 300; i++ {
			select {
			case <-stop:
				return
			default:
			}
			if _, err := sc.UpsertPet(mkPet(st.gd, i%50+1, 2000672, 3006)); err != nil {
				errs <- err
				return
			}
		}
	}()

	for r := 0; r < 4; r++ { // 读者:模拟同时打开页面的几个 API
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				if _, _, err := sc.ListPets(Filter{PageSize: 20}); err != nil {
					errs <- err
					return
				}
				sc.FilterOptions()
				sc.BoxLayouts()
			}
		}()
	}

	wg.Wait()
	close(stop)
	close(errs)
	for err := range errs {
		t.Fatalf("并发读写出错: %v", err)
	}
}

// TestFilterOptionsDistinctAndSorted 校验合并成一次扫描后,各维度仍是去重且升序。
func TestFilterOptionsDistinctAndSorted(t *testing.T) {
	st := newTestStore(t)
	sc := st.For(testAcc)
	natures := []string{"固执", "胆小", "固执", "开朗", ""}
	for i, n := range natures {
		p := mkPet(st.gd, uint32(i+1), 2000672, 3006)
		p.Nature = n
		if _, err := sc.UpsertPet(p); err != nil {
			t.Fatalf("写入: %v", err)
		}
	}
	got := sc.FilterOptions()["nature"]
	want := []string{"固执", "开朗", "胆小"} // UTF-8 字节序
	if len(got) != len(want) {
		t.Fatalf("性格可选值 = %v, 期望 %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("性格可选值 = %v, 期望 %v", got, want)
		}
	}
}

// TestPetHeadsBothBranches 校验 petHeads 走 IN 与走整表扫两条分支结果一致(见 petHeadsInMax):
// 队伍布局只要十几只走 IN,盒子示意图上百只走扫表,两条路必须给出同一份头像。
func TestPetHeadsBothBranches(t *testing.T) {
	st := newTestStore(t)
	sc := st.For(testAcc)
	const n = petHeadsInMax * 2 // 足以越过阈值
	all := make([]uint32, 0, n)
	for gid := uint32(1); gid <= n; gid++ {
		p := mkPet(st.gd, gid, 2000672, 3006)
		if gid%2 == 0 { // 掺一半未进化的,两类头像不同
			p = mkPet(st.gd, gid, 3001, 3001)
		}
		if _, err := sc.UpsertPet(p); err != nil {
			t.Fatalf("写入 gid=%d: %v", gid, err)
		}
		all = append(all, gid)
	}

	// 走扫表分支(一次要全部 n 只)
	scan := sc.petHeads(all)
	if len(scan) != n {
		t.Fatalf("扫表分支返回 %d 条头像, 期望 %d", len(scan), n)
	}
	// 走 IN 分支:每次只问一小撮,凑齐后与扫表结果比对
	in := map[string]string{}
	for i := 0; i < len(all); i += petHeadsInMax {
		chunk := all[i:min(i+petHeadsInMax, len(all))]
		for k, v := range sc.petHeads(chunk) {
			in[k] = v
		}
	}
	if len(in) != len(scan) {
		t.Fatalf("IN 分支 %d 条, 扫表分支 %d 条", len(in), len(scan))
	}
	for k, v := range scan {
		if in[k] != v {
			t.Errorf("gid=%s: 扫表得 %q, IN 得 %q", k, v, in[k])
		}
	}
	// 别让两类宠物的头像恰好相同,否则用例形同虚设
	if scan["1"] == scan["2"] {
		t.Fatalf("两类用例宠物头像相同(%q),分不出取图是否正确", scan["1"])
	}
}

// TestBoxTeamSwapClearsStaleSide 复现「宠物盒 ↔ 大世界队伍拖动交换后位置不同步」。
//
// 互换回包(0x1888)里:
//   - 「挤进盒子」那只走 box_pet_change 增量落库,ApplyBoxMoves 顺带清它残留的 pet_team 行;
//   - 「挤进队伍」那只**只**出现在同包的完整队伍快照里,走 ReplacePetTeams 全量替换 pet_team。
//
// 镜像关系:ApplyBoxMoves 清的是 pet_team,那 ReplacePetTeams 就该清 pet_box —— 否则
// 从盒子拖进队伍的那只会同时挂在两张表下,列表页仍显示它占着原盒位,看起来像
// 「盒子 → 队伍」这个方向没同步。(在队宠物不可能同时在盒子里,见 pet.Pet 的 Box/Team。)
//
// 反方向**没做**:ReplacePetBoxes 不清 pet_team。实测登录包 0x0102 的盒位(857 个 gid)与
// 队位(18 个 gid)**交集为 0** —— 游戏把在队宠物排除在盒快照外,故那一步永远删不到行,
// 是死代码。理由与复审提示见 docs/data.md 同名小节。
func TestBoxTeamSwapClearsStaleSide(t *testing.T) {
	st := newTestStore(t)
	sc := st.For(testAcc)

	team := mkPet(st.gd, 12, 2000672, 3006)    // 初始在大世界队伍
	boxed := mkPet(st.gd, 6476, 2000672, 3006) // 初始在宠物盒
	for _, p := range []*pet.Pet{team, boxed} {
		if _, err := sc.UpsertPet(p); err != nil {
			t.Fatalf("写入 gid=%d: %v", p.Gid, err)
		}
	}
	if err := sc.ReplacePetTeams([]pet.TeamEntry{{Gid: 12, TeamIdx: 0, Pos: 0}}); err != nil {
		t.Fatalf("初始化队伍: %v", err)
	}
	if err := sc.ReplacePetBoxes([]pet.BoxEntry{{Gid: 6476, BoxID: 1, Slot: 0}}); err != nil {
		t.Fatalf("初始化盒子: %v", err)
	}

	// 拖动交换:12 队伍→盒子、6476 盒子→队伍。顺序与 pipeline 处理回包一致:
	// 先按完整队伍快照替换 pet_team,再按 box_pet_change 增量落 12 的新盒位。
	if err := sc.ReplacePetTeams([]pet.TeamEntry{{Gid: 6476, TeamIdx: 0, Pos: 0}}); err != nil {
		t.Fatalf("替换队伍快照: %v", err)
	}
	if err := sc.ApplyBoxMoves([]pet.BoxEntry{{Gid: 12, BoxID: 1, Slot: 0}}); err != nil {
		t.Fatalf("应用盒位移动: %v", err)
	}

	pets, _, err := sc.ListPets(Filter{})
	if err != nil {
		t.Fatalf("查询宠物列表: %v", err)
	}
	byGid := map[uint32]*pet.Pet{}
	for _, p := range pets {
		byGid[p.Gid] = p
	}

	if p := byGid[6476]; p.Box != nil {
		t.Errorf("gid=6476 已移入队伍,盒子位置应清空,实得 %+v", p.Box)
	} else if p.Team == nil || p.Team.TeamIdx != 0 || p.Team.Pos != 0 {
		t.Errorf("gid=6476 队伍位置不对: %+v", p.Team)
	}
	if p := byGid[12]; p.Team != nil {
		t.Errorf("gid=12 已移入盒子,队伍位置应清空,实得 %+v", p.Team)
	} else if p.Box == nil || p.Box.BoxID != 1 || p.Box.Slot != 0 {
		t.Errorf("gid=12 盒子位置不对: %+v", p.Box)
	}
}

// TestAccountAvatar 锁定头像的存取语义:登录解析到就写入,解析不到(空串)保留旧值。
// 后者是真实场景 —— 快速登录回包不带头像,若空串覆盖,玩家头像会莫明消失。
func TestAccountAvatar(t *testing.T) {
	st := newTestStore(t)
	if err := st.UpsertAccount(testAcc, "测试账号"); err != nil {
		t.Fatalf("建账号: %v", err)
	}
	avatarOf := func() string {
		accs, err := st.ListAccounts()
		if err != nil {
			t.Fatalf("ListAccounts: %v", err)
		}
		for _, a := range accs {
			if a.Account == testAcc {
				return a.Avatar
			}
		}
		t.Fatalf("账号 %s 未出现在列表里", testAcc)
		return ""
	}
	const url = "https://thirdwx.qlogo.cn/mmopen/vi_32/abc/132"
	if got := avatarOf(); got != "" {
		t.Fatalf("新账号头像应为空,实得 %q", got)
	}
	if err := st.SetAccountAvatar(testAcc, url); err != nil {
		t.Fatalf("SetAccountAvatar: %v", err)
	}
	if got := avatarOf(); got != url {
		t.Fatalf("期望 %q,实得 %q", url, got)
	}
	if err := st.SetAccountAvatar(testAcc, ""); err != nil {
		t.Fatalf("SetAccountAvatar(空): %v", err)
	}
	if got := avatarOf(); got != url {
		t.Fatalf("空 URL 不应覆盖已有头像,实得 %q", got)
	}
}

// fullPet 造一只**字段填满**的宠物,量级对齐真实数据(两个系别、两个蛋组、五个勋章 id、
// 六个六维、盒位、图片)。空壳 Pet 的 data JSON 只有真实数据的几分之一,拿它测反序列化
// 会低估好几倍 —— 而这正是本次要测的东西。
func fullPet(gd *gamedata.DB, gid uint32) *pet.Pet {
	return &pet.Pet{
		Gid: gid, ConfID: 2000672, BaseConfID: 3006,
		Species: "罗隐", Book: 128, Stage: 3, Name: "小火猴", Level: 60,
		NatureID: 2, Nature: "固执", Gender: "♂",
		Types: []string{"火", "格斗"}, TypeIcons: []string{"type/1.png", "type/2.png"},
		BloodID: 3, Blood: "火", BloodIcon: "blood/3.png",
		EggGroups: gd.PetEggGroups(3006),
		HeightM:   1.23, WeightKg: 45.6, Voice: int32(gid%200 - 100),
		TalentRank: "优秀",
		Medal:      "勇气勋章", MedalDesc: "佩戴后物攻提升若干", MedalIcon: "medal/1.png",
		WearMedalConfID: 12, MedalIDs: []uint32{1, 2, 3, 4, 5},
		PartnerMark: "首领", PartnerMarkIcon: "mark/1.png",
		Speciality: "物攻", SpecialityID: 3,
		CatchTime: 1700000000, Shiny: gid%7 == 0,
		Box:       &pet.PetBoxLoc{BoxID: 3, Slot: 12, BoxName: "常用", Mark: "首领"},
		HP:        pet.Stat{Value: 320, TalentLv: 8, Nature: 1},
		Attack:    pet.Stat{Value: 280, TalentLv: 9, Nature: 1},
		Defense:   pet.Stat{Value: 240, TalentLv: 7},
		SpAttack:  pet.Stat{Value: 150, TalentLv: 5},
		SpDefense: pet.Stat{Value: 210, TalentLv: 6, Nature: -1},
		Speed:     pet.Stat{Value: 260, TalentLv: 8},
	}
}

// BenchmarkBreedingPetsNarrow 窄投影(ListBreedingPets)对整只反序列化(ListAllPets)。
//
// 这是培育页换读法的**全部理由**,故把两条路放在同一个基准里直接比:同一批数据、同一个
// 库、同样的 7545 只(本账号实测的宠物总数)。只差在读法上。
func BenchmarkBreedingPetsNarrow(b *testing.B) {
	gd, err := gamedata.Load()
	if err != nil {
		b.Fatalf("加载名称库: %v", err)
	}
	st, err := New(filepath.Join(b.TempDir(), "bench.db"), gd)
	if err != nil {
		b.Fatalf("打开数据库: %v", err)
	}
	defer st.Close()
	sc := st.For(testAcc)

	const n = 7545
	all := make([]*pet.Pet, 0, n)
	for gid := uint32(1); gid <= n; gid++ {
		all = append(all, fullPet(gd, gid))
	}
	for i := 0; i < len(all); i += 500 {
		end := min(i+500, len(all))
		if _, err := sc.UpsertPets(all[i:end]); err != nil {
			b.Fatalf("预置 %d 只: %v", n, err)
		}
	}

	b.Run("ListAllPets", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := sc.ListAllPets(); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("ListBreedingPets", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := sc.ListBreedingPets(); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// mkLine 造一条培育线:moms 是历代母本 gid、kids 是历代子代 gid(可不等长)。
// MotherGid 为 0 且给了 moms = 老线(靠末代派生种母);MotherGid 非 0 = 新线(直接固定)。
func mkLine(id string, motherGid uint32, moms, kids []uint32) *pet.BreedingLine {
	l := &pet.BreedingLine{ID: id, Species: "火神", MotherGid: motherGid,
		Status: pet.BreedingActive, UpdatedAt: 1}
	n := len(moms)
	if len(kids) > n {
		n = len(kids)
	}
	for i := 0; i < n; i++ {
		g := pet.Generation{Gen: i + 1}
		if i < len(moms) {
			g.Mother = &pet.EggParent{Gid: moms[i]}
		}
		if i < len(kids) {
			g.Child = &pet.EggParent{Gid: kids[i]}
		}
		l.Gens = append(l.Gens, g)
	}
	return l
}

func lineID(l *pet.BreedingLine) string {
	if l == nil {
		return "<nil>"
	}
	return l.ID
}

// TestFindLineForMother 收蛋的三級归属。**三级次序不能换**,每条各一个子测试:
//
//	① 种母身上已挂线 → 沿用(不换种母就不换线)
//	② 母本是某条线孵出来的 → hit=nil / parent=那条线(开子线)
//	③ 都不是 → hit=nil / parent=nil(开独立新线)
//
// ③ 是本次要修掉的那个坑:改之前它走「同品种最近更新的那条」,5 条同品种的线里挑一条就是猜。
func TestFindLineForMother(t *testing.T) {
	st := newTestStore(t)
	sc := st.For(testAcc)
	// 与 pipeline.recordLay 同一套构造方式:一律走 ChainRefOf,别手搓 ChainRef ——
	// 空线会被 fillLineChain 派生出 Evo,手搓一个 Evo=0 的 ref 与它比不上。
	ref := pet.ChainRefOf(st.gd, 0, "火神")

	// pinned = 新线(字段固定种母 101);old = 老线(无字段,末代母本 202、子代 301/302)
	for _, l := range []*pet.BreedingLine{
		mkLine("pinned", 101, nil, nil),
		mkLine("old", 0, []uint32{201, 202}, []uint32{301, 302}),
	} {
		if err := sc.UpsertBreedingLine(l); err != nil {
			t.Fatalf("写线 %s: %v", l.ID, err)
		}
	}

	t.Run("种母已挂线就沿用", func(t *testing.T) {
		hit, parent, err := sc.FindLineForMother(101, ref)
		if err != nil {
			t.Fatal(err)
		}
		if lineID(hit) != "pinned" {
			t.Errorf("hit = %s, 期望 pinned", lineID(hit))
		}
		if parent != nil {
			t.Errorf("parent = %s, 期望 nil(已精确命中,不关子线的事)", lineID(parent))
		}
	})

	t.Run("老线按末代派生种母", func(t *testing.T) {
		hit, _, err := sc.FindLineForMother(202, ref) // 末代的母本,不是第一代的 201
		if err != nil {
			t.Fatal(err)
		}
		if lineID(hit) != "old" {
			t.Errorf("hit = %s, 期望 old", lineID(hit))
		}
	})

	t.Run("本线子代接班当种母", func(t *testing.T) {
		hit, parent, err := sc.FindLineForMother(302, ref)
		if err != nil {
			t.Fatal(err)
		}
		if hit != nil {
			t.Errorf("hit = %s, 期望 nil(302 还没挂在任何线上)", lineID(hit))
		}
		if lineID(parent) != "old" {
			t.Errorf("parent = %s, 期望 old(302 是它孵出来的)", lineID(parent))
		}
	})

	t.Run("外来血脉不猜挂谁", func(t *testing.T) {
		// 野外抓的 999:不是任何线的种母,也不是任何线孵出来的
		hit, parent, err := sc.FindLineForMother(999, ref)
		if err != nil {
			t.Fatal(err)
		}
		if hit != nil || parent != nil {
			t.Errorf("外来种母 hit=%s parent=%s, 期望都是 nil(该开独立新线)",
				lineID(hit), lineID(parent))
		}
	})

	t.Run("同品种多条线互不干扰", func(t *testing.T) {
		// 再加一条同品种、挂在另一只种母身上的线:两颗蛋必须各进各的
		if err := sc.UpsertBreedingLine(mkLine("pinned2", 102, nil, nil)); err != nil {
			t.Fatal(err)
		}
		for gid, want := range map[uint32]string{101: "pinned", 102: "pinned2"} {
			hit, _, err := sc.FindLineForMother(gid, ref)
			if err != nil {
				t.Fatal(err)
			}
			if lineID(hit) != want {
				t.Errorf("种母 %d → %s, 期望 %s", gid, lineID(hit), want)
			}
		}
	})
}

// TestFindLineForMotherEmptyFallback 连种母都派生不出的线(手工建的、一代都没有)才按品种兜底。
// 这是唯一还保留「按品种找」的地方:那种线还没接过蛋,归谁都谈不上历史。
func TestFindLineForMotherEmptyFallback(t *testing.T) {
	st := newTestStore(t)
	sc := st.For(testAcc)
	ref := pet.ChainRefOf(st.gd, 0, "火神")
	if err := sc.UpsertBreedingLine(mkLine("empty", 0, nil, nil)); err != nil {
		t.Fatal(err)
	}
	hit, parent, err := sc.FindLineForMother(999, ref)
	if err != nil {
		t.Fatal(err)
	}
	if lineID(hit) != "empty" {
		t.Errorf("hit = %s, 期望 empty(空线按品种兜底)", lineID(hit))
	}
	if parent != nil {
		t.Errorf("parent = %s, 期望 nil", lineID(parent))
	}
	// 别的品种不该被兜底命中
	hit2, _, err := sc.FindLineForMother(999, pet.ChainRefOf(st.gd, 0, "水灵"))
	if err != nil {
		t.Fatal(err)
	}
	if hit2 != nil {
		t.Errorf("别的品种 hit = %s, 期望 nil", lineID(hit2))
	}
}

// TestReparentLines 合并子线时孙辈要改挂:并入母线后子线就被删了,孙辈若还指着它,
// 谱系视图就从那一代断掉。
func TestReparentLines(t *testing.T) {
	st := newTestStore(t)
	sc := st.For(testAcc)
	for _, l := range []*pet.BreedingLine{
		{ID: "mom", Species: "火神", Status: pet.BreedingActive},
		{ID: "kid", Species: "火神", Status: pet.BreedingActive, ParentLineID: "mom"},
		{ID: "grandkid", Species: "火神", Status: pet.BreedingActive, ParentLineID: "kid"},
		{ID: "unrelated", Species: "火神", Status: pet.BreedingActive, ParentLineID: "other"},
	} {
		if err := sc.UpsertBreedingLine(l); err != nil {
			t.Fatalf("写线 %s: %v", l.ID, err)
		}
	}
	if err := sc.ReparentLines("kid", "mom"); err != nil {
		t.Fatal(err)
	}
	got, err := sc.GetBreedingLine("grandkid")
	if err != nil || got == nil {
		t.Fatalf("取孙辈: %v", err)
	}
	if got.ParentLineID != "mom" {
		t.Errorf("孙辈 parentLineId = %q, 期望 mom(改挂到母线)", got.ParentLineID)
	}
	// 无关的线不该被碰
	keep, err := sc.GetBreedingLine("unrelated")
	if err != nil || keep == nil {
		t.Fatalf("取无关线: %v", err)
	}
	if keep.ParentLineID != "other" {
		t.Errorf("无关线的 parentLineId = %q, 期望 other(不该被改)", keep.ParentLineID)
	}
	// 空 / 自指都是空操作,不该报错也不该改数据
	if err := sc.ReparentLines("", "mom"); err != nil {
		t.Errorf("空 oldParent = %v, 期望 nil", err)
	}
	if err := sc.ReparentLines("mom", "mom"); err != nil {
		t.Errorf("自指 = %v, 期望 nil", err)
	}
}
