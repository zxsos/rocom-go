package gamedata

import (
	_ "embed" // go:embed 需要
	"encoding/json"
	"strings"
)

// 特性词典(由 scripts/fetch_features.py + scripts/fetch_rocoworld.py
// 抓取、scripts/gen_features.py 生成)。
//
// 特性是协议里的**全局编号**(草系试炼等只下发 288xxx 的 id),技能有
// skills.json 兜底,特性此前没有任何名表,试炼页只能按 id 展示。
// 资料站给的是特性名与描述,**没有 288xxx id**(详见 docs/data.md「特性名」)。
//
// 用途:试炼抓包桥接 —— 「精灵 → 特性名」索引配合「宠物与其天生特性 id 同时出现」
// 的抓包数据,可反推 id -> 名字。
//
//go:embed data/features.json
var featuresJSON []byte

// Feature 是一条特性词典条目:名字 + 描述 + 拥有该特性的精灵(按 wiki 收录)。
// 注意没有协议 id —— 名字到 288xxx id 的映射靠抓包反推补,不在本文件。
type Feature struct {
	Name string   `json:"name"`
	Desc string   `json:"desc"`
	Pets []string `json:"pets"`
}

// featuresDB 是解析后的特性词典。
type featuresDB struct {
	features   []Feature         // 词典(按名排序,来自生成脚本)
	byName     map[string]int    // 特性名 -> features 下标
	petToName  map[string]string // 精灵页名 -> 特性名(wiki,按名字建键)
	baseToName map[uint32]string // petbase_id -> 特性名(roco.world,按 id 建键)
}

// loadFeatures 解析内嵌的特性词典;文件缺失或格式不符时返回空表(不影响其余功能)。
func loadFeatures() *featuresDB {
	db := &featuresDB{
		byName:     map[string]int{},
		petToName:  map[string]string{},
		baseToName: map[uint32]string{},
	}
	var raw struct {
		Features       []Feature         `json:"features"`
		PetFeature     map[string]string `json:"pet_feature"`
		PetbaseFeature map[string]string `json:"petbase_feature"`
	}
	if err := json.Unmarshal(featuresJSON, &raw); err != nil {
		return db
	}
	db.features = raw.Features
	for i, f := range raw.Features {
		db.byName[f.Name] = i
	}
	db.petToName = raw.PetFeature
	for k, v := range raw.PetbaseFeature {
		var id uint64
		for _, c := range k {
			if c < '0' || c > '9' {
				id = 0
				break
			}
			id = id*10 + uint64(c-'0')
		}
		if id != 0 {
			db.baseToName[uint32(id)] = v
		}
	}
	return db
}

// PetFeatureName 返回某精灵页名的特性名。
//
// ⚠️ 这是 **wiki 那份按名字建键**的索引,新代码请改用 FeatureNameOfBase ——
// 它先按 petbase_id 查(准)、查不到才回退到这里(靠名字猜)。
// 保留本函数是因为 pet_feature 仍是个有效的数据源,全删了会丢掉那部分覆盖。
func (db *DB) PetFeatureName(pet string) (string, bool) {
	fdb := db.features
	if fdb == nil {
		return "", false
	}
	n, ok := fdb.petToName[strings.TrimSpace(pet)]
	return n, ok
}

// FeatureNameOfBase 按 petbase 形态查该精灵的特性名。
//
// 这是特性 id 桥接的落点。两头都不完整,拼起来才有用:
//
//	协议侧:只给特性 id(288xxx),名字一个都没有;
//	资料站:只有「精灵 → 特性名」,一个 id 都没有。
//
// 桥是「宠物与它的天生特性 id **总是同时出现**」(试炼的 initial_feature_ids 与
// 宠物的 base_conf_id 同包下发):查到该形态的特性名,再把这个名字绑到那条 id 上。
//
// ## 先查 id,再查名字 —— 这个顺序不能反
//
// roco.world 的图鉴页数据里带 petbase_id,与我方解包出的 id **完全一致**
// (实测 594/594 对得上),故可直接按 id 索引,不依赖任何名字匹配:
//
//	覆盖率 89%(640 个候选形态里 569 个),且**不会抄串**。
//
// wiki 那一份只能按精灵页名反查(键是「鸭吉吉（蓬松的样子）」这种形态全名),
// 覆盖率 74%,而且**有 8 处抄串** —— 最典型的是女王蜂与花魁蜂后:
// wiki 把两只的特性对调了,而 roco.world 按形态给出的是对的
// (花魁蜂后=虫群鼓舞、女王蜂=虫群突袭,两者是同一图鉴 84 的阶段 3 与 4)。
//
// 故:id 查不到时才回退名字。反过来的话,那 8 只精灵会一直显示错误的特性名,
// 且没人会发现 —— 名字匹配"成功"了,只是配错了。
//
// ⚠️ 只对**天生**特性成立:试炼中获得的特性是节点随机给的,与精灵本身无关,
// 拿精灵的特性名去绑它们必然是错的。
func (db *DB) FeatureNameOfBase(petbaseID uint32) string {
	fdb := db.features
	if fdb == nil {
		return ""
	}
	// 1. 按 petbase_id 直接查(roco.world,准)
	if n, ok := fdb.baseToName[petbaseID]; ok && n != "" {
		return n
	}
	// 2. 回退:拼形态全名去查 wiki 那一份(靠名字匹配,可能对不上)
	name := db.PetFullName(petbaseID)
	if name == "" {
		return ""
	}
	n, _ := db.PetFeatureName(name)
	return n
}
