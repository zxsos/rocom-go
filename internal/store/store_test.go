package store

import (
	"encoding/json"
	"path/filepath"
	"strconv"
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
