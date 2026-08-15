package gamedata

// 精灵蛋与家园小窝的查找表(见 docs/data.md 3.6)。
//
// 三张表各司其职:
//   - EggConf(PET_EGG_CONF):按**物种 conf_id** 给出蛋自身的身高体重区间与孵化时长。
//     注意这套区间与成体的 PETBASE_CONF 区间不是一套数,百分位才是两者的公共语言。
//   - EggItem(BAG_ITEM_CONF 里 type==8):背包里那件蛋物品,给出显示名与图标。
//     同一物种可能有多件蛋物品(普通/活动/珍稀),显示名已在生成期按 known_name 模板填好。
//   - EggNPCItem:家园小窝上趴着的蛋是个 NPC(NPC_CONF id 形如 930xxx),据此反查是哪件蛋物品,
//     所以**收进背包之前就能知道窝里是什么蛋**(但尺寸要收下来才有)。

// EggConf 是一个物种的蛋配置(PET_EGG_CONF 行)。
type EggConf struct {
	Name       string `json:"n"`  // 物种名(孵出来是谁)
	HeightLow  int32  `json:"hl"` // 蛋身高区间(÷100 米)
	HeightHigh int32  `json:"hh"`
	WeightLow  int32  `json:"wl"` // 蛋体重区间(÷1000 千克)
	WeightHigh int32  `json:"wh"`
	HatchSecs  int32  `json:"t"` // 孵化所需秒数(hatch_data;无加速活动时即真实秒数)
}

// EggItem 是一件背包里的蛋物品(BAG_ITEM_CONF 里 type==8 的行)。
type EggItem struct {
	ID   uint32 // 物品 id
	Name string // 显示名(「友爱天天的蛋」/「神奇的蛋」;known_name 模板已填好)
	Conf uint32 // 物种 conf_id(对应 EggConf);随机蛋(神奇的蛋)为 0
	Icon string // 图标原始文件名,前端拼 egg/<Icon>.webp
}

// EggItemInfo 返回蛋物品信息。
func (db *DB) EggItemInfo(id uint32) (EggItem, bool) { e, ok := db.eggItems[id]; return e, ok }

// EggConfInfo 返回某物种的蛋配置(区间/孵化时长)。
func (db *DB) EggConfInfo(conf uint32) (EggConf, bool) { c, ok := db.eggConf[conf]; return c, ok }

// EggNPCItem 返回家园小窝上蛋 NPC(NPC_CONF id)对应的蛋物品 id;非蛋 NPC 返回 0。
func (db *DB) EggNPCItem(npcCfgID uint32) uint32 { return db.eggNPCs[npcCfgID] }

// NestFurniture 返回该家具 config_id 是否为可入住宠物的小窝,以及家具名。
func (db *DB) NestFurniture(cfgID uint32) (string, bool) { n, ok := db.nestFurn[cfgID]; return n, ok }

// EggIcon 返回蛋图标的相对路径(egg/<原名>.webp);图标缺失时回退通用蛋图。
// 少数未上线物种的蛋图没随包解出(gen_icons 会报「缺 PNG」),回退保证前端不出空图。
func (db *DB) EggIcon(itemID uint32) string {
	name := ""
	if e, ok := db.eggItems[itemID]; ok {
		name = e.Icon
	}
	if name != "" {
		if p := "egg/" + name + ".webp"; db.imgFiles[p] {
			return p
		}
	}
	if p := "egg/egg_tongyong.webp"; db.imgFiles[p] {
		return p
	}
	return ""
}
