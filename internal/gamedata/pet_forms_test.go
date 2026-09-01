package gamedata

import "testing"

// 本文件验证「形态全名」这条口径 —— 它是 wiki 数据(join 的钥匙)与游戏数据
// (id/头像)之间唯一的桥。
//
// 为什么值得单独测:拼错一个字符(半角括号、下划线)**没有报错、只是静默查不到**。
// 特性桥接因此整体失效,而页面看起来只是「这些精灵没有特性名」,
// 谁也不会想到是括号写错了。这类错误只有断言能抓住。
//
// 覆盖:
//   - PetFullName 的拼接口径(全角括号);
//   - PetByName 反查(含同名形态取最小 id);
//   - PetForms 的过滤(有图鉴号的才算);
//   - FeatureNameOfBase 端到端:形态 id → 特性名。

func TestPetFullName(t *testing.T) {
	db, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// 找两个已知样本:一个带形态名、一个不带。不写死 id ——
	// 解包数据换版本后 id 会变,按形状找才稳。
	var withForm, withoutForm uint32
	for id, info := range db.petbase {
		if info.Book == 0 {
			continue
		}
		if info.Form != "" && withForm == 0 {
			withForm = id
		}
		if info.Form == "" && withoutForm == 0 {
			withoutForm = id
		}
	}
	if withForm == 0 || withoutForm == 0 {
		t.Fatalf("样本不足: 带形态名 %d, 不带 %d", withForm, withoutForm)
	}

	f := db.petbase[withForm]
	want := f.Name + "（" + f.Form + "）"
	if got := db.PetFullName(withForm); got != want {
		t.Errorf("带形态名: = %q, 期望 %q", got, want)
	}
	// 半角括号是**错的**:wiki 用全角。这条断言是防半角回归的关键。
	if wantHalf := f.Name + "(" + f.Form + ")"; db.PetFullName(withForm) == wantHalf {
		t.Errorf("形态名用了半角括号 —— wiki 是全角,反查会全部落空")
	}
	if got := db.PetFullName(withoutForm); got != db.petbase[withoutForm].Name {
		t.Errorf("无形态名: = %q, 期望 %q(不该带括号)", got, db.petbase[withoutForm].Name)
	}
	if got := db.PetFullName(99999999); got != "" {
		t.Errorf("未知 id: = %q, 期望空串", got)
	}
}

func TestPetByName(t *testing.T) {
	db, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// 反查回来必须能拿到原 id(或同名的更小 id)
	checked := 0
	for id, info := range db.petbase {
		if info.Book == 0 {
			continue
		}
		gotID, gotInfo, ok := db.PetByName(db.PetFullName(id))
		if !ok {
			t.Fatalf("%q 反查不到 —— petNames 表没建全", db.PetFullName(id))
		}
		if gotID > id {
			t.Errorf("%q 反查到 %d, 期望 <= %d(同名取最小 id)", db.PetFullName(id), gotID, id)
		}
		if gotInfo.Name != info.Name {
			t.Errorf("%q 反查到异名 %q", db.PetFullName(id), gotInfo.Name)
		}
		checked++
		if checked >= 200 { // 全量 1136 个,抽 200 个足够
			break
		}
	}
	if _, _, ok := db.PetByName("不存在的精灵"); ok {
		t.Error("查不到的名字应返回 false")
	}
}

func TestPetForms(t *testing.T) {
	db, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	forms := db.PetForms()
	if len(forms) == 0 {
		t.Fatal("PetForms 为空")
	}
	// 无图鉴号的占位形态(「鸭吉吉_普通」这类属性变换记录)不该进候选:
	// 玩家在游戏里见不到它们,混进标注候选只会干扰搜索。
	for _, f := range forms {
		if f.Book == 0 {
			t.Errorf("base=%d (name=%q) 无图鉴号却进了候选", f.Base, f.Name)
		}
		if f.Name == "" {
			t.Errorf("base=%d 名字为空", f.Base)
		}
	}
	// 按图鉴号、id 升序:候选列表要给玩家搜,乱序没法用
	for i := 1; i < len(forms); i++ {
		a, b := forms[i-1], forms[i]
		if a.Book > b.Book || (a.Book == b.Book && a.Base > b.Base) {
			t.Fatalf("未按序: %d/%d 在 %d/%d 之后", a.Book, a.Base, b.Book, b.Base)
		}
	}
	t.Logf("候选形态 %d 个", len(forms))
}

// TestFeatureNameOfBase 端到端验证特性桥接:形态 id → 特性名。
//
// 这是「开局自动绑定特性」的落点。两头的数据都可能缺(wiki 没收录、形态名对不上),
// 故**不断言某个具体 id 必须有名字** —— 那只会在 wiki 更新后随机变红。
// 断言的是「凡是对得上的形态,查出来的名字必须是 wiki 词典里存在的一个名字」:
// 这能抓住拼错括号(整体查不到)、查错表(返回了别的东西)这类真回归。
func TestFeatureNameOfBase(t *testing.T) {
	db, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if db.features == nil || len(db.features.petToName) == 0 {
		t.Skip("features.json 未生成,跳过")
	}
	known := map[string]bool{}
	for _, f := range db.features.features {
		known[f.Name] = true
	}
	var hits int
	for id := range db.petbase {
		got := db.FeatureNameOfBase(id)
		if got == "" {
			continue
		}
		hits++
		if !known[got] {
			t.Fatalf("base=%d 查到 %q, 但不在 features 词典里(桥接查错了表)", id, got)
		}
	}
	// 覆盖率下限:全表对不上说明括号口径坏了(实测 500+ 个形态查得到)
	if hits < 100 {
		t.Errorf("只桥接出 %d 个特性名, 远低于预期 —— 形态全名的口径多半坏了", hits)
	}
	t.Logf("桥接出特性名的形态 %d 个", hits)
}
