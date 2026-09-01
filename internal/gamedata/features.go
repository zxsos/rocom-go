package gamedata

import (
	_ "embed" // go:embed 需要
	"encoding/json"
	"strings"
)

// 特性词典(由 scripts/fetch_features.py + scripts/gen_features.py 生成)。
//
// 特性是协议里的**全局编号**(草系试炼等只下发 288xxx 的 id),技能有
// skills.json 兜底,特性此前没有任何名表,试炼页只能按 id 展示。这份数据来自
// wiki 精灵图鉴页逐只精灵的 `|特性=`/`|特性描述=` 字段 —— **只有名字,没有
// 288xxx id**(详见 docs/data.md「特性名」)。
//
// 用途:
//  1. 标注模式的**候选词典**:玩家对未知特性 id 搜索选取名字并提交,管理员审核后
//     全服共享(见 internal/store 的 annotation 表与 server 的标注 API);
//  2. 试炼抓包桥接:pet_feature 表(精灵页名 -> 特性名)配合「宠物与其天生特性
//     id 同时出现」的抓包数据,可反推 id -> 名字。
//
//go:embed data/features.json
var featuresJSON []byte

// Feature 是一条特性词典条目:名字 + 描述 + 拥有该特性的精灵(按 wiki 收录)。
// 注意没有协议 id —— 名字到 288xxx id 的映射靠标注/抓包补,不在本文件。
type Feature struct {
	Name string   `json:"name"`
	Desc string   `json:"desc"`
	Pets []string `json:"pets"`
}

// featuresDB 是解析后的特性词典。
type featuresDB struct {
	features  []Feature         // 词典(按名排序,来自生成脚本)
	byName    map[string]int    // 特性名 -> features 下标
	petToName map[string]string // 精灵页名 -> 特性名(试炼 id 桥接用)
}

// loadFeatures 解析内嵌的特性词典;文件缺失或格式不符时返回空表(不影响其余功能)。
func loadFeatures() *featuresDB {
	db := &featuresDB{
		byName:    map[string]int{},
		petToName: map[string]string{},
	}
	var raw struct {
		Features   []Feature         `json:"features"`
		PetFeature map[string]string `json:"pet_feature"`
	}
	if err := json.Unmarshal(featuresJSON, &raw); err != nil {
		return db
	}
	db.features = raw.Features
	for i, f := range raw.Features {
		db.byName[f.Name] = i
	}
	db.petToName = raw.PetFeature
	return db
}

// Features 返回全部特性词典条目(标注候选库的搜索源)。
// 结果按名排序,直接用于 web 端「搜索选取」交互。
func (db *DB) Features() []Feature {
	fdb := db.features
	if fdb == nil {
		return nil
	}
	return fdb.features
}

// FeatureByName 按特性名查描述与拥有精灵;查不到返回空条目。
// 用于标注提交前校验候选是否存在(wiki 词典里没有的名字不应凭空出现)。
func (db *DB) FeatureByName(name string) (Feature, bool) {
	fdb := db.features
	if fdb == nil {
		return Feature{}, false
	}
	i, ok := fdb.byName[name]
	if !ok {
		return Feature{}, false
	}
	return fdb.features[i], true
}

// PetFeatureName 返回某精灵页名的特性名(用于试炼「宠物与其特性同时出现」时
// 把协议 id 桥接成名字);查不到返回空串 —— wiki 只收录了 494 只精灵的特性,
// 未收录的宠物查不到不是错误。
func (db *DB) PetFeatureName(pet string) (string, bool) {
	fdb := db.features
	if fdb == nil {
		return "", false
	}
	n, ok := fdb.petToName[strings.TrimSpace(pet)]
	return n, ok
}

// FeatureNameOfBase 按 petbase 形态查该精灵的特性名(wiki 精灵图鉴页口径)。
//
// 这是特性 id 桥接的落点。两头都不完整,拼起来才有用:
//
//	协议侧:只给特性 id(288xxx),名字一个都没有;
//	wiki 侧:只有「精灵 → 特性名」,一个 id 都没有。
//
// 桥是「宠物与它的天生特性 id **总是同时出现**」(试炼的 initial_feature_ids 与
// 宠物的 base_conf_id 同包下发):拿形态全名去 wiki 查到特性名,再把这个名字
// 绑到那条 id 上,于是 288135 就有了名字。
//
// 覆盖率:wiki 收录 494 只精灵,其中 481 只能与形态全名对上(97%)。查不到返回
// 空串 —— 形态名口径不一致就查不到,宁可不显示也不猜(标错比不标更糟)。
//
// ⚠️ 只对**天生**特性成立:试炼中获得的特性是节点随机给的,与精灵本身无关,
// 拿精灵的特性名去绑它们必然是错的。
func (db *DB) FeatureNameOfBase(petbaseID uint32) string {
	name := db.PetFullName(petbaseID)
	if name == "" {
		return ""
	}
	n, _ := db.PetFeatureName(name)
	return n
}
