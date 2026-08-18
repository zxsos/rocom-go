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
