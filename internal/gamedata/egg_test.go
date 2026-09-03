package gamedata

import (
	"strconv"
	"testing"
)

// 本文件验证从 PET_EGG_CONF 新吸收的两样东西(见 docs/data.md 3.6):
//   - 蛋外形 model_id:多个 conf_id 指向同一个 model,且**必须指向表内真实存在的条目**;
//   - 蛋品类的中文名:品类 4 在 EGG_TYPE_CONF 里没有名字,由背包物品的显示名补上。
//
// 为什么值得测:两者都是「查不到就静默降级」的口径 —— model 指到表外只是取不到图,
// 品类名缺失只是 tooltip 空白,都不报错、也不写日志。只有断言能抓住。

// eggTypesWithoutName 是**已知**没有中文名的品类,出现在这之外的无名品类即为回归。
//
//	0: 普通蛋 —— 游戏里本来就没名,只提供排序号 100000 垫在所有特殊蛋之后。
//	8: 噩梦 —— EGG_TYPE_CONF 有图标无名字,且它的蛋全是随机蛋(conf=0),
//	   没有按物种反查显示名的入口,补不了(新出现的同类品类也要照此核对,别直接加进来)。
var eggTypesWithoutName = map[int32]bool{0: true, 8: true}

func TestEggModelConf(t *testing.T) {
	db, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(db.eggConf) == 0 {
		t.Fatal("egg_conf 为空")
	}
	// 不写死 id:解包数据换版本后 id 会变,按「自指 / 指向别人」的形状找样本才稳。
	//
	// model 只保证落在**宠物形态 id 空间**(能查到物种名),不保证有对应的蛋配置行 ——
	// 实测 6 条指向了只有物种名、没有 PET_EGG_CONF 行的形态(地鼠 3020002、钨丝贝贝 3733001)。
	var self, aliased uint32
	for conf := range db.eggConf {
		model := db.EggModelConf(conf)
		if db.species[strconv.FormatUint(uint64(model), 10)] == "" {
			t.Errorf("conf %d 的外形 model %d 在物种名表里查不到", conf, model)
			continue
		}
		switch {
		case model == conf && self == 0:
			self = conf
		case model != conf && aliased == 0:
			aliased = conf
		}
	}
	if self == 0 {
		t.Error("没有任何外形自指的条目:基础形态的 model_id 应等于自身 id")
	}
	if aliased == 0 {
		t.Error("没有任何外形指向别人的条目:names.json 的 m 字段多半没落盘")
	}
	// 归一化的另一半:没落 m 的条目读出来也必须是自指,而不是 0。
	if got, ok := db.EggConfInfo(self); !ok || got.ModelID != self {
		t.Errorf("基础形态 %d 的 ModelID = %d (ok=%v), 期望 %d", self, got.ModelID, ok, self)
	}
}

// TestEggNamesPresent 断言蛋表的名字**没有成片丢失**。
//
// 为什么值得测:这些名字全部来自 scripts/gen_gamedata.py 读解包配置,而生成脚本
// 重跑时若上游表的 name 字段取不到值,`v.get("name", "")` 会**静默落空串** ——
// 没有报错、不写日志,生成出来的是个看起来结构完好的 names.json。
//
// 这个事故真的发生过一次(2026-09-03 拉取的那批提交回归):
//   - egg_conf  917 条物种名**全部**变成空串;
//   - egg_items 628 条「喵喵的蛋」变成「的蛋」—— 模板 "{0}的蛋" 里的 {0} 落到空串,
//     于是只剩后缀。
//
// 而后端测试当时只有两条挂了(查蛋的),因为只有它们按名字断言。页面上的表现是
// 蛋列表一片空白、候选列表空 —— 不报错,只是"没数据"。
//
// 阈值取 1% 而非「一条都不能空」:解包数据本身可能有少量条目真的没名字,
// 断言过严会让它成为常态噪音。要防的是**成片丢失**,不是零星几条。
func TestEggNamesPresent(t *testing.T) {
	db, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	const maxEmptyRatio = 0.01

	// 蛋配置:物种名
	if n := len(db.eggConf); n > 0 {
		var empty int
		for _, c := range db.eggConf {
			if c.Name == "" {
				empty++
			}
		}
		if float64(empty)/float64(n) > maxEmptyRatio {
			t.Errorf("egg_conf 有 %d/%d 条没有物种名 —— 多半是生成脚本重跑时上游字段取不到值", empty, n)
		}
	}
	// 背包蛋物品:显示名。除"没名字"外还要挡"丢了前缀" —— 后者是模板 {0} 落空
	// 的特征,字形上是「的蛋」这种没有物种名的残缺名。
	if n := len(db.eggItems); n > 0 {
		var empty, broken int
		for _, e := range db.eggItems {
			switch {
			case e.Name == "":
				empty++
			case e.Name == "的蛋":
				broken++
			}
		}
		if float64(empty)/float64(n) > maxEmptyRatio {
			t.Errorf("egg_items 有 %d/%d 条没有显示名", empty, n)
		}
		if broken > 0 {
			t.Errorf("egg_items 有 %d 条显示名只剩「的蛋」—— 模板 {0} 的物种名是空的", broken)
		}
	}
}

// TestEggTypeNames 断言每个品类都有名字(已知的普通蛋/噩梦除外)。
// 品类 4(活动纪念)是这次补的:它的名字不在 EGG_TYPE_CONF 里,而是从 122 件
// 「活动纪念精灵蛋」的背包显示名反推出来的 —— 少了这一步,前端角标的 tooltip 就是空的。
func TestEggTypeNames(t *testing.T) {
	db, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for id, v := range db.eggTypes {
		if v.Name == "" && !eggTypesWithoutName[id] {
			t.Errorf("蛋品类 %d 没有中文名(排序号 %d)", id, v.Order)
		}
	}
	for id := range eggTypesWithoutName {
		if _, ok := db.eggTypes[id]; !ok {
			t.Errorf("蛋品类 %d 在白名单里却已不存在,请更新白名单", id)
		}
	}
}
