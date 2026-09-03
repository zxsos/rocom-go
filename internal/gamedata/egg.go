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
	Precious   int32  `json:"p"` // 蛋品类 precious_egg_type(0=普通,2=异色…见 EggType)
	// 外形 model_id:**蛋长什么样只由它决定**,与 conf_id 是多对一(917 条蛋只有 295 个 model)。
	// 血脉变体(果冻 9 个形态)、自选炫彩蛋(8 位 3xxx0001)、活动纪念蛋(9900xxxx)都指向别的
	// 条目 —— 后者指向的是它借用的那个物种,不是自己。等于 conf_id 的基础形态不落盘(占多数)。
	//
	// 注意形态组:conf_id 前 4 位是**形态组 id**,同一图鉴号的地区/季节形态各占一个组
	// (地鼠 3020 枯水期 / 3454 储水时、波波螺 3508 本来 / 3511 被污染…共 15 个物种多形态)。
	// model 一般落在自己组内,但组号不同的条目之间**没有任何推断关系** —— 别拿一个组的
	// model 去补另一个组的缺。
	ModelID uint32 `json:"m"`
}

// EggItem 是一件背包里的蛋物品(BAG_ITEM_CONF 里 type==8 的行)。
type EggItem struct {
	ID      uint32 // 物品 id
	Name    string // 显示名(「友爱天天的蛋」/「神奇的蛋」;known_name 模板已填好)
	Conf    uint32 // 物种 conf_id(对应 EggConf);随机蛋(神奇的蛋)为 0
	Icon    string // 图标原始文件名,前端拼 egg/<Icon>.webp
	Quality int32  // 物品品质(item_quality,4/5)——游戏内「品质排序」的键之一
	SortID  int32  // 物品排序号(sort_id)——同品质时的次级键
}

// EggType 是蛋的品类(EGG_TYPE_CONF 的 precious_egg_type:异色/炫彩/珍贵/唯一…)。
// Order 即游戏内「品质排序」的首要键(display_order,越小越靠前;普通蛋 100000 垫底)。
type EggType struct {
	ID    int32  `json:"-"`
	Name  string `json:"n,omitempty"`
	Order int32  `json:"o"`
	Icon  string `json:"img,omitempty"` // 角标原名,前端拼 egg/<Icon>.webp
}

// SizeMedal 是按百分位自动授予的奖牌(MEDAL_TASK_CONF 里 get_condition==3 的四枚:
// 大块头/小不点看体重、婉转声/粗嗓门看嗓音)。蛋的百分位孵化后原样保留,故体重那两枚
// 破壳前就能算出来;嗓音那两枚要等破壳(见 docs/data.md 3.6)。
type SizeMedal struct {
	ID   uint32 `json:"id"`
	Name string `json:"n"`
	Dim  int32  `json:"d"`  // 判定维度:2=体重百分位 3=嗓音百分位
	Low  int32  `json:"lo"` // 百分位窗口(含)
	High int32  `json:"hi"`
}

// 自动奖牌的判定维度(MEDAL_TASK_CONF.condition_data1)。
const (
	MedalDimWeight = 2
	MedalDimVoice  = 3
)

// EggTypeInfo 返回蛋品类信息(precious_egg_type);普通蛋(0)或未知返回 ok=false。
func (db *DB) EggTypeInfo(t int32) (EggType, bool) {
	v, ok := db.eggTypes[t]
	if !ok || t == 0 {
		return EggType{}, false
	}
	return v, true
}

// EggTypeOrder 返回蛋品类的排序号(display_order);未知品类排在最后。
func (db *DB) EggTypeOrder(t int32) int32 {
	if v, ok := db.eggTypes[t]; ok {
		return v.Order
	}
	return 1 << 30
}

// SizeMedals 返回按百分位自动授予的奖牌清单(4 枚,按 id 升序)。
func (db *DB) SizeMedals() []SizeMedal { return db.sizeMedals }

// EggItemInfo 返回蛋物品信息。
func (db *DB) EggItemInfo(id uint32) (EggItem, bool) { e, ok := db.eggItems[id]; return e, ok }

// EggConfInfo 返回某物种的蛋配置(区间/孵化时长)。
func (db *DB) EggConfInfo(conf uint32) (EggConf, bool) { c, ok := db.eggConf[conf]; return c, ok }

// EggModelConf 返回这颗蛋的**外形**配置 id(PET_EGG_CONF.model_id):蛋长什么样只由它决定,
// 多个 conf_id 可指向同一个 model。查不到该物种时原样返回入参。
//
// 三类条目会指向别人(见 EggConf.ModelID):血脉变体指向基础形态、自选炫彩蛋(8 位 3xxx0001)
// 指向同物种的基础条目、活动纪念蛋(9900xxxx)指向它借用的那个物种 —— 尤其注意最后一种,
// 99000055 的外形是 3457001(月牙雪熊),与它自己的 id 毫无关系。
//
// model 落在**宠物形态 id 空间**,不保证有对应的蛋配置行 —— 全表 6 条指向了只有物种名、
// 没有 PET_EGG_CONF 行的形态。成因是**异色蛋的外形固定取同组 2 号变体**:109/110 条
// 异色蛋(precious=2)的 model 都是 组号+"002",而地鼠的两个形态组(3020 枯水期、
// 3454 储水时)恰好都没配 2 号变体,4 条异色蛋的外形因此落空;另 2 条是钨丝贝贝 3733 组
// 缺 1 号、只有 2 号,而它的 2 号与自选炫彩蛋都指回 1 号。这是**配置缺失**不是解析错误,
// 游戏内这些蛋大概同样取不到专属外形。
//
// 结论:EggConfInfo(model) 可能查不到,取区间/时长永远要用**自己的 conf_id**,
// 别拿 model 去查蛋配置。
func (db *DB) EggModelConf(conf uint32) uint32 {
	if c, ok := db.eggConf[conf]; ok && c.ModelID != 0 {
		return c.ModelID
	}
	return conf
}

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
