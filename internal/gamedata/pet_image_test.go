package gamedata

import "testing"

// 本文件锁定「下发的图片路径一定真的 embed 了」这条不变量。
//
// 为什么值得单独测:`images` 索引来自游戏侧的表,图片本体来自解包导出,两者会不一致 ——
// 本版本 1112 个形态里有 55 个「索引有值但 webp 缺失」(未上线整体无美术;或只缺一项,
// 如 3219 只有 Pet1024 大图、3731 索引声明了异色头像但客户端没这张图)。漏做 imgFiles
// 校验时**不报错**:前端拿到 404 路径后静默退占位图,只是白跑一次请求,谁也不会想到
// 是拼路径时漏了校验。这类错误只有断言能抓住。

func TestPetImageOnlyEmitsEmbeddedFiles(t *testing.T) {
	db, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(db.images) == 0 {
		t.Fatal("images 索引为空,样本不足")
	}
	for id := range db.images {
		for _, shiny := range []bool{false, true} {
			img := db.imageOf(id, shiny)
			for _, p := range []string{img.Head, img.BigHead, img.Portrait, img.PortraitSmall} {
				if p != "" && !db.imgFiles[p] {
					t.Errorf("形态 %s(shiny=%v)下发了未 embed 的路径 %q", id, shiny, p)
				}
			}
		}
	}
}

// TestPetImageShinyFallsBackWhenArtMissing 钉住异色缺图时的回退:索引声明了异色头像、
// 但客户端没导出那张图时,不能照索引拼出 404 路径 —— 有普通头像就给普通头像,没有就给空串
// (3785 这类连普通头像都没导出)。本版本有 4 个这样的形态(3731/3732/3784/3785),故不会 Skip。
func TestPetImageShinyFallsBackWhenArtMissing(t *testing.T) {
	db, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	n := 0
	// 按形状找样本,不写死 id —— 解包数据换版本后 id 会变。
	for id, e := range db.images {
		if e.SH == "" || db.imgFiles["HeadIcon/"+e.SH+".webp"] {
			continue
		}
		n++
		got := db.imageOf(id, true).Head
		if got == "HeadIcon/"+e.SH+".webp" {
			t.Errorf("形态 %s 的异色头像 %s 未 embed,不该下发它", id, e.SH)
		}
		if want := "HeadIcon/" + e.H + ".webp"; db.imgFiles[want] && got != want {
			t.Errorf("形态 %s 异色头像缺图时应回退普通头像 %q,实得 %q", id, want, got)
		}
	}
	if n == 0 {
		t.Skip("本版本没有「声明了异色头像但缺图」的形态")
	}
}

// TestPetImagePortraitEmptyUntilPet1024Embedded 记录 Pet1024 未 embed 的现状:portrait
// 一律为空串(见 docs/data.md)。哪天把 Pet1024 加进 gen_images.py 的 DIRS,这条会红 ——
// 那正是该回来更新文档、并考虑前端取图的时机。
func TestPetImagePortraitEmptyUntilPet1024Embedded(t *testing.T) {
	db, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for id := range db.images {
		if got := db.imageOf(id, false).Portrait; got != "" {
			t.Fatalf("形态 %s 的 portrait = %q;Pet1024 未 embed 时不该有值", id, got)
		}
	}
}
