package gamedata

import (
	mathrand "math/rand"
	"sort"
	"strconv"
	"strings"
)

// Medal 是奖牌的名称与描述。
type Medal struct {
	Name string `json:"name"`
	Desc string `json:"desc"`
}

// imageEntry 是 petbase 形态的图片文件名(头像为数字,全身图去掉 JL_ 前缀)。
type imageEntry struct {
	H   string `json:"h"`   // 小头像文件名
	B   string `json:"b"`   // 大头像文件名
	P   string `json:"p"`   // 全身图拼音键(实际文件名为 JL_<p>)
	PS  string `json:"ps"`  // 全身缩略拼音键
	SH  string `json:"sh"`  // 异色小头像(形如 3010_1;仅有专属异色图者)
	SB  string `json:"sb"`  // 异色大头像
	SPS string `json:"sps"` // 异色全身缩略拼音键(形如 emoding_yise)
}

// PetImage 是宠物各尺寸图片的相对路径(相对图片根,空串表示缺图)。
// 四个字段都是「确实 embed 了才给」,故非空即保证能取到(见 imageOf)。
type PetImage struct {
	Head          string `json:"head"`          // 小头像 HeadIcon/<n>.webp
	BigHead       string `json:"bigHead"`       // 大头像 BigHeadIcon256/<n>.webp
	Portrait      string `json:"portrait"`      // 全身图 Pet1024/JL_<x>.webp;该尺寸暂未 embed,恒为空串
	PortraitSmall string `json:"portraitSmall"` // 全身缩略 Pet256/JL_<x>.webp
}

// PetBaseInfo 是 petbase 形态的元数据(名称/图鉴号/形态名/进化阶段/进化链分组/身高体重范围)。
type PetBaseInfo struct {
	Name  string // 当前形态名(火神/音速犬/岚鸟…)
	Book  uint32 // 图鉴编号(pictorial_book_id)
	Form  string // 地区/季节形态名(春天的样子…),普通宠物为空
	Stage uint32 // 进化阶段(1 起)
	Evo   uint32 // 进化链分组 id(同链共享),用于重建进化链
	// 身高/体重取值范围(原始整数,与 PetData.height/weight 同单位:height÷100=米,weight÷1000=千克)。
	HeightLow  uint32
	HeightHigh uint32
	WeightLow  uint32
	WeightHigh uint32
	EggGroups  []uint32 // 蛋组(繁殖组)编号,1~2 个,对应 EggGroup.ID
}

// EggGroup 是蛋组(繁殖组)信息:社区流行名 + 官方描述(源自 PET_LIKE_ELEMENT_CONF)。
type EggGroup struct {
	ID   uint32 `json:"id"`
	Name string `json:"name"`
	Desc string `json:"desc"`
}

// NatureEffect 是性格对六维的增减维度(六维编号 1-6:1生命2物攻3魔攻4物防5魔防6速度)。
type NatureEffect struct {
	Pos int32 `json:"pos"` // +10% 维度
	Neg int32 `json:"neg"` // -10% 维度
}

// PetImage 返回宠物各尺寸图片的相对路径(经 base_id 归并到 petbase 形态;缺图为空串);
// shiny=true 时优先取异色图(无专属异色图或未 embed 时回退普通)。
func (db *DB) PetImage(confID uint32, shiny bool) PetImage {
	pid, ok := db.imageBase[key(confID)]
	if !ok {
		pid = key(confID) // base==自身,直接按 conf_id 查 petbase
	}
	return db.imageOf(pid, shiny)
}

// PetImageByBase 按 petbase_id 直接取图片(base_conf_id 本身即 petbase id,给出当前形态)。
func (db *DB) PetImageByBase(petbaseID uint32, shiny bool) PetImage {
	return db.imageOf(key(petbaseID), shiny)
}

func (db *DB) imageOf(petbaseID string, shiny bool) PetImage {
	e, ok := db.images[petbaseID]
	if !ok {
		return PetImage{}
	}
	head, big, ps := e.H, e.B, e.PS
	// 异色变体仅在「索引有该字段且对应 webp 确已 embed」时启用,否则回退普通图。
	if shiny {
		if e.SH != "" && db.imgFiles["HeadIcon/"+e.SH+".webp"] {
			head = e.SH
		}
		if e.SB != "" && db.imgFiles["BigHeadIcon256/"+e.SB+".webp"] {
			big = e.SB
		}
		if e.SPS != "" && db.imgFiles["Pet256/JL_"+e.SPS+".webp"] {
			ps = e.SPS
		}
	}
	// 普通图同样要过 imgFiles 校验(iconPath 内部做),不能照索引直接拼路径:
	// 索引是游戏侧的表、图片是解包导出的,两者会不一致 —— 未上线形态整体无美术,
	// 部分形态只缺一项(3219 只有 Pet1024 大图、3731 索引声明了异色头像但客户端没这张图)。
	// 漏校验就会下发 404 路径,而前端**不报错**:静默退占位图,只是白跑一次请求,极难察觉。
	var img PetImage
	img.Head = db.iconPath("HeadIcon", head)
	img.BigHead = db.iconPath("BigHeadIcon256", big)
	if e.P != "" {
		img.Portrait = db.iconPath("Pet1024", "JL_"+e.P)
	}
	if ps != "" {
		img.PortraitSmall = db.iconPath("Pet256", "JL_"+ps)
	}
	return img
}

// PetBaseOf 按宠物 conf_id 取所属 petbase 形态(与 PetImage 同一套 base 归并):
// 蛋只带 conf_id(如 3062001),而身高体重区间挂在 petbase(3062)上,故需这一跳。
func (db *DB) PetBaseOf(confID uint32) (uint32, PetBaseInfo, bool) {
	pid := key(confID)
	if b, ok := db.imageBase[pid]; ok {
		pid = b
	}
	id, err := strconv.ParseUint(pid, 10, 32)
	if err != nil {
		return 0, PetBaseInfo{}, false
	}
	info, ok := db.petbase[uint32(id)]
	return uint32(id), info, ok
}

// PetBase 返回 petbase 形态元数据(base_conf_id);ok=false 表示未知。
func (db *DB) PetBase(petbaseID uint32) (PetBaseInfo, bool) {
	v, ok := db.petbase[petbaseID]
	return v, ok
}

// NpcPetBase 返回野生宠物 NPC(NPC_CONF.id)对应的 petbase 形态 id;ok=false 表示该 NPC
// 不在可捕捉野生宠清单里(表只用于取名称/头像,判定实体是不是野生宠见 scene.NpcActor.IsWildPet)。
func (db *DB) NpcPetBase(npcCfgID uint32) (uint32, bool) {
	v, ok := db.npcPets[npcCfgID]
	return v, ok
}

// WildPetOption 是管理员面板「向指定成员投放稀有精灵」可选的野生宠物形态。
type WildPetOption struct {
	Base  uint32 `json:"base"`  // petbase 形态 id(投放时据此取名称/头像/身高体重区间)
	Name  string `json:"name"`  // 形态名(珀尔鼬…)
	Book  uint32 `json:"book"`  // 图鉴编号(排序用)
	Shiny bool   `json:"shiny"` // 是否有可用的异色小头像(投放异色时只列这些)
}

// WildPetOptions 返回全部可投放的野生宠物形态(去重后按图鉴号升序)。数据源是
// npc_pets(NPC_CONF→petbase),同一 petbase 可能有多个野生 NPC 映射,故按 petbase 去重。
// 过滤掉没有可用普通小头像的形态:投放(异色/炫彩)前端地图标记都要靠小头像显示,
// 列出无头像的精灵会让管理员注入后标记显示不出图,观感像「投放失败」。
func (db *DB) WildPetOptions() []WildPetOption {
	seen := map[uint32]bool{}
	var out []WildPetOption
	for _, base := range db.npcPets {
		if seen[base] {
			continue
		}
		seen[base] = true
		info, ok := db.petbase[base]
		if !ok {
			continue
		}
		if !db.HasHeadImage(base) {
			continue
		}
		out = append(out, WildPetOption{Base: base, Name: info.Name, Book: info.Book, Shiny: db.HasShinyImage(base)})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Book != out[j].Book {
			return out[i].Book < out[j].Book
		}
		return out[i].Base < out[j].Base
	})
	return out
}

// HasShinyImage 报告该 petbase 形态是否有可用的异色小头像(与 imageOf 的 shiny 分支同一套
// 判定:e.SH 非空且对应 HeadIcon webp 确已 embed)。投放异色只取小头像,故只看 e.SH。
func (db *DB) HasShinyImage(petbaseID uint32) bool {
	e, ok := db.images[key(petbaseID)]
	if !ok {
		return false
	}
	return e.SH != "" && db.imgFiles["HeadIcon/"+e.SH+".webp"]
}

// HasHeadImage 报告该 petbase 形态是否有可用的普通小头像(e.H 非空且对应 HeadIcon webp
// 确已 embed)。投放炫彩只取普通小头像,管理员面板下拉据此过滤掉无头像的精灵,避免注入后
// 前端地图标记因 img 为空而显示不出头像。
func (db *DB) HasHeadImage(petbaseID uint32) bool {
	e, ok := db.images[key(petbaseID)]
	if !ok {
		return false
	}
	return e.H != "" && db.imgFiles["HeadIcon/"+e.H+".webp"]
}

// IsNpcBoss 报告该 NPC(NPC_CONF.id)是不是野外首领(throwing_interact_type=4:祭礼巨像/
// 女王蜂/钻石蜗…)。它们的 AOI 下发距离远得多(实测 128-176m,普通野生宠 80m,见 docs/data.md 3.7),
// 故涂地不能拿它们当「这条线扫过了」的凭据(见 docs/data.md 3.8);地图标记不受影响。
func (db *DB) IsNpcBoss(npcCfgID uint32) bool { return db.npcBosses[npcCfgID] }

// 炫彩类型(GlassInfo.glass_type,dataconfig.GlassType)。
const (
	GlassNull   = 0 // GT_NULL,非炫彩
	GlassCommon = 1 // GT_COMMON,普通炫彩(glass_value 是打包色号)
	GlassHidden = 2 // GT_HIDDEN,隐藏炫彩(glass_value 是 HIDDEN_GLASS_CONF.id)
)

// glassParticleShift 是普通炫彩色号的打包位宽:glass_value = (粒子id << 20) | 配色id
// (客户端 PetUtils.GetShineDataValue 即按 20 位拆)。
const glassParticleShift = 20

// GlassDesc 返回炫彩外观的中文描述(见 docs/data.md 3.5):
// 隐藏炫彩给外观名(暗夜拾光…),普通炫彩给「粒子·配色」(四角星·亮X暗 - 浅紫橙)。
// 非炫彩或查不到时返回空串(调用方自行兜底)。
func (db *DB) GlassDesc(glassType, glassValue int32) string {
	switch glassType {
	case GlassHidden:
		return db.glassNames[key(uint32(glassValue))]
	case GlassCommon:
		if glassValue <= 0 {
			return ""
		}
		particle := db.glassParticles[key(uint32(glassValue)>>glassParticleShift)]
		color := db.glassColors[key(uint32(glassValue)&(1<<glassParticleShift-1))]
		switch {
		case particle != "" && color != "":
			return particle + "·" + color
		case color != "":
			return color
		default:
			return particle
		}
	}
	return ""
}

// GlassValid 报告炫彩色卡组合是否合法:普通炫彩要求粒子/配色都能在配置里查到,
// 隐藏炫彩要求有外观名(暗夜拾光等)。用于管理员投放假炫彩时校验手填的色号。
func (db *DB) GlassValid(glassType, glassValue int32) bool {
	return db.GlassDesc(glassType, glassValue) != ""
}

// RandGlass 返回一个随机的合法炫彩色卡组合:隐藏炫彩(赛季 1/2/3、黑白)与普通炫彩
// (随机粒子 × 随机配色打包)各半,模拟真实投放的多样性。配置缺失时兜底 1 号普通色卡。
func (db *DB) RandGlass() (glassType, glassValue int32) {
	randKey := func(m map[string]string) string {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		return keys[mathrand.Intn(len(keys))]
	}
	if len(db.glassNames) > 0 && mathrand.Intn(2) == 0 {
		v, err := strconv.ParseInt(randKey(db.glassNames), 10, 32)
		if err == nil {
			return GlassHidden, int32(v)
		}
	}
	if len(db.glassParticles) > 0 && len(db.glassColors) > 0 {
		p, err1 := strconv.ParseInt(randKey(db.glassParticles), 10, 32)
		c, err2 := strconv.ParseInt(randKey(db.glassColors), 10, 32)
		if err1 == nil && err2 == nil {
			return GlassCommon, int32(p)<<glassParticleShift | int32(c)
		}
	}
	return GlassCommon, 1
}

// PetFullName 返回 petbase 形态的**全名**:「名」或「名（形态）」。
//
// 括号是**全角**,与 wiki 精灵图鉴页的命名一致(实测 3012 → 「鸭吉吉（蓬松的样子）」)。
// 这不是排版偏好:wiki 的「精灵 → 特性」表(features.json 的 pet_feature)就是按这个
// 口径建的键,故凡是要拿形态名去反查 wiki 的地方都必须走这里拼,别自造格式 ——
// 用半角括号或下划线都会**静默查不到**(实测 494 个 wiki 键里只能对上 351 个)。
func (db *DB) PetFullName(petbaseID uint32) string {
	info, ok := db.petbase[petbaseID]
	if !ok {
		return ""
	}
	return petFullName(info.Name, info.Form)
}

// petFullName 是形态全名的**唯一拼装点**:名 + 全角括号的形态后缀。
//
// 建反查表(gamedata.go)与对外查询(PetFullName)都必须走这里 —— 两处各拼一份,
// 迟早会有一处改了另一处没改,而这种不一致的后果是**静默查不到**而非报错。
func petFullName(name, form string) string {
	if form != "" {
		return name + "（" + form + "）"
	}
	return name
}

// PetByName 按形态全名(PetFullName 的口径,含全角括号)反查 petbase id;查不到返回 false。
//
// 同名形态取**最小 id**:同一只精灵的若干变体在配置里是多条记录(「迪莫」有
// 3004/8007/103004/16000007),取最小的是基础形态,头像与名字都最"正"。
//
// ⚠️ 只用于「名字 → 形态」这类展示场景:同名形态本就是同一只精灵,
// 选哪个都不算错。别拿它做身份判定(比如判定"遇到的到底是哪一只")——
// 那条信息协议里没有,反查推不出来。
func (db *DB) PetByName(fullName string) (uint32, PetBaseInfo, bool) {
	id, ok := db.petNames[fullName]
	if !ok {
		return 0, PetBaseInfo{}, false
	}
	info, ok := db.petbase[id]
	return id, info, ok
}

// ChainByName 按**裸形态名**(不带 PetFullName 的「（形态）」后缀)反查品种(进化链 id);
// 查不到这个形态名、或同名的几个形态**并不并属一条链**时返回 0。
//
// 给「手里只有一个名字」的补全用:培育线的老记录(链口径之前建的)只存了 EggParent.Species
// 这个裸名,读取时由它补出品种身份(见 pet.DeriveChain 与 pet.ChainRefOf)。
//
// 为什么有歧义就返回 0,而不是取最小 id 猜一个:这条路径补错的代价是不对称的 —— 一旦补成链,
// 调用方那条「按名字匹配」的退路就没了,同名但不在链上的那只从此一只都配不出来;而返回 0
// (继续按名字匹配)至少与升级前一样能配。实测 676 个有图鉴号的形态里,52 组名字同时有链形态
// 与无链变体记录,其中 25 组的最小 id 落在链上、另一半落在无链的那条上,拿最小 id 去猜
// 两边都会踩雷。
//
// 与 PetByName 的分工:那个按**全名**反查(展示场景),这个按裸名给**品种**。
func (db *DB) ChainByName(name string) uint32 {
	if name == "" {
		return 0
	}
	evo, first := uint32(0), true
	for _, info := range db.petbase {
		if info.Name != name || info.Book == 0 {
			continue
		}
		if first {
			evo, first = info.Evo, false
			continue
		}
		if evo != info.Evo {
			return 0 // 同名形态不并属一条链:不猜
		}
	}
	if first {
		return 0 // 没有这个形态名(或都没有图鉴号)
	}
	return evo
}

// PetFormOption 是形态枚举里的一条(base/名字/图鉴号/头像)。
type PetFormOption struct {
	Base uint32 `json:"base"` // petbase 形态 id
	Name string `json:"name"` // 形态全名(PetFullName 口径)
	Book uint32 `json:"book"` // 图鉴编号(排序用)
	// Img 是头像路径(web 相对路径,前端拼 webAssetsBase);没有素材时为空串。
	//
	// 同名形态极多 —— 676 个有图鉴号的形态里 100 个基础名是重名的
	// (「棋契陛下」有 10 个形态、「圣代甜甜」9 个),文字一样时只能靠图区分。
	Img string `json:"img,omitempty"`
}

// PetForms 返回全部精灵形态,同名去重后按图鉴号、id 升序。
//
// 只收**有图鉴号**的形态(b≠0):没有图鉴号的是内部占位形态(实测 9801「鸭吉吉_普通」
// 这类属性变换用的记录),玩家在游戏里见不到。
//
// 形态枚举是「按形态遍历取头像 / 查特性」类逻辑的地基(试炼的异色头像与特性桥接
// 测试都要先枚举一遍形态),也是 PetByName 反查口的互补 —— 两条路必须同口径,
// 同名同图的多条形态记录才不会在两条路里落到不同的那条上。
func (db *DB) PetForms() []PetFormOption {
	out := make([]PetFormOption, 0, len(db.petbase))
	// 按**形态全名去重**:同一只精灵在配置里可能有若干条形态记录 ——
	// 「棋契陛下」有 8 条(四条进化来源各一条)、「鸭吉吉国王」6 条、「钻石蜗」6 条,
	// 共 16 组、52 个形态。它们的名字与头像**完全一样**,枚举出多条没有意义。
	//
	// 保留最小 id,与 PetByName(同名取最小)保持同一口径。
	//
	// 去重会丢掉一部分 petbase id —— 可以接受:同名同图的两条记录无从区分,
	// 只保留其中之一,任何按名反查都与枚举落在同一个上。
	//
	// 去重前**必须先排序**:map 的迭代顺序是随机的,若边遍历边去重,
	// 保留下来的是「碰巧先被遍历到的那个」而非最小的 —— 于是每次启动
	// 枚举挂的 petbase id 都可能不同,而 PetByName 总是取最小的,
	// 结果就是两路不一致,且这种不一致挑不出规律。
	//
	// 是的,排序后先遇到的一定是最小 id(排序键第二位是 Base)。
	for id, info := range db.petbase {
		if info.Book != 0 {
			out = append(out, PetFormOption{Base: id, Name: db.PetFullName(id), Book: info.Book})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Book != out[j].Book {
			return out[i].Book < out[j].Book
		}
		return out[i].Base < out[j].Base
	})

	// 就地去重(去掉重复名字时保留排在最前 = id 最小的那条)
	uniq := out[:0]
	seen := map[string]bool{}
	for _, o := range out {
		if seen[o.Name] {
			continue
		}
		seen[o.Name] = true
		// 只取头像(不取全身图):候选列表里要的是「一眼认出是哪只」,
		// 全身图尺寸大、占比高,会挤掉本就紧张的列表空间。
		if im := db.PetImageByBase(o.Base, false); im.Head != "" {
			o.Img = im.Head
		}
		uniq = append(uniq, o)
	}
	return uniq
}

// PetEggGroups 返回某 petbase 形态的蛋组列表(社区名+描述,按配置顺序);无则返回 nil。
func (db *DB) PetEggGroups(petbaseID uint32) []EggGroup {
	info, ok := db.petbase[petbaseID]
	if !ok || len(info.EggGroups) == 0 {
		return nil
	}
	out := make([]EggGroup, 0, len(info.EggGroups))
	for _, id := range info.EggGroups {
		if g, ok := db.eggGroup[id]; ok {
			out = append(out, g)
		}
	}
	return out
}

// eggGroupNames 取蛋组的名字。只下发名字:收窄候选与筛选都是按名字比(见 store.Filter 的蛋组
// 谓词),描述是 hover 用的展示信息,只有宠物详情那种要解释「这组是什么」的地方才需要。
func eggGroupNames(gs []EggGroup) []string {
	if len(gs) == 0 {
		return nil
	}
	out := make([]string, 0, len(gs))
	for _, g := range gs {
		out = append(out, g.Name)
	}
	return out
}

// ChainStep 是进化链上的一个形态(按阶段升序)。
type ChainStep struct {
	Petbase uint32   `json:"petbase"`
	Name    string   `json:"name"`
	Book    uint32   `json:"book"`
	Stage   uint32   `json:"stage"`
	Image   PetImage `json:"image"`
}

// EvolutionChain 返回 petbase 所属进化链(同一形态线,按阶段升序);未知或单形态返回自身一项。
func (db *DB) EvolutionChain(petbaseID uint32) []ChainStep {
	info, ok := db.petbase[petbaseID]
	if !ok {
		return nil
	}
	ids := db.evoIndex[info.Evo]
	if info.Evo == 0 || len(ids) == 0 {
		ids = []uint32{petbaseID} // 无进化链分组:仅自身
	}
	steps := make([]ChainStep, 0, len(ids))
	for _, id := range ids {
		pi := db.petbase[id]
		steps = append(steps, ChainStep{Petbase: id, Name: pi.Name, Book: pi.Book, Stage: pi.Stage, Image: db.PetImageByBase(id, false)})
	}
	// 按阶段升序;同阶段(分支进化,如果冻→抹茶/椰浆/熔岩布丁)再按图鉴号,最后按 petbase id。
	//
	// 最后一档不能省:阶段与图鉴号**都会撞**(喵喵的两支终极形态 武斗酷猫 5061 / 叶冕魔力猫 5003
	// 同为 stage 4、book 4),只比到图鉴号时剩下的一段顺序直接来自 evoIndex 的**map 迭代次序** ——
	// 每次启动都可能不同。而这个顺序是**对外契约的一部分**:/api/evolution 按它下发,ChainLabel
	// 也按它拼展示名(「喵喵（喵呜/魔力猫/武斗酷猫/叶冕魔力猫）」),不定序会让契约 golden 间歇性
	// 报不一致,且页面上同一品种的括号里两个名字顺序会随重启跳。
	sort.Slice(steps, func(i, j int) bool {
		if steps[i].Stage != steps[j].Stage {
			return steps[i].Stage < steps[j].Stage
		}
		if steps[i].Book != steps[j].Book {
			return steps[i].Book < steps[j].Book
		}
		return steps[i].Petbase < steps[j].Petbase
	})
	return steps
}

// ChainOf 返回某 petbase 形态所属的**培育品种**:进化链分组 id(同链共享);未知或无链返回 0。
//
// 培育页此前按「当前形态名」认品种,那是不成立的:
//   - 一条链上的各阶段是**同一个**品种 —— 蛋的物种随母本(docs/data.md 3.6),链上任一阶段的 ♀
//     都能当种母。按名字比会把链上的其它阶段全漏掉:记的是罗隐,而窝里是只阿米亚特就配不出来了。
//   - 反过来,同一只精灵的**不同形态**各占一条链(嗜波螺「本来的样子」3508 / 「被污染的样子」
//     3511),链 id 天然把它们分开,不必再给身份加「形态」这一维。
//
// 0 表示该形态**没有链**(实测 1147 条 petbase 里 526 条:首领/活动/内部占位形态),只能单条成链
// —— 此时按**名字**认品种(见 pet.ChainRef),与升级前的行为一致。
func (db *DB) ChainOf(petbaseID uint32) uint32 {
	info, ok := db.petbase[petbaseID]
	if !ok {
		return 0
	}
	return info.Evo
}

// ChainMembers 返回某条进化链的全部 petbase 形态 id;evo==0 或未知返回 nil。
//
// 培育页拿它把「这条线认的品种」展开成可逐个比对的形态集合(见 pet.ChainRefOf):个体身上只有
// base_conf_id(当前形态),要认它属不属于这条链,只能拿这个集合去撞。
// 返回的是内部切片,**调用方不得修改**。
func (db *DB) ChainMembers(evo uint32) []uint32 {
	if evo == 0 {
		return nil
	}
	return db.evoIndex[evo]
}

// ChainLabel 返回培育品种的展示名:初始形态在前,其余阶段列在全角括号里 ——
// 「阿米亚特（阿米樱/罗隐/深渊罗隐）」「地鼠（枯水期的样子）（遁鼠/遁地鼠）」。
//
// 为什么要把链上**所有阶段**都写出来:这条线的种母可以是链上任一阶段的 ♀,只写「罗隐」会让
// 「阿米亚特♀ 也能用」在界面上看不出来 —— 而那正是这个页面最容易踩的坑。
//
// 为什么**形态名**也必须写出来:同一只精灵的地区/季节形态各占一条链(地鼠 3020 枯水期 /
// 3454 储水时、波波螺 3508 本来 / 3511 被污染),而它们的阶段名一模一样 —— 只写形态名之外的
// 部分,下拉里就会并排出现两条**完全一样**的「地鼠（遁鼠/遁地鼠）」,玩家无从分辨,而选错品种
// 直接决定了这条线能配哪些母本。形态名走 petFullName(与 wiki 键同口径的全角括号)。
//
// 形态在链内通常一致(实测地鼠/遁地鼠/波波螺/海盔虫等多形态物种的整条链都是同一个形态),
// 故只在**与链首不同**的阶段重复标注 —— 否则「地鼠（枯水期的样子）（遁鼠（枯水期的样子）/
// 遁地鼠（枯水期的样子））」会平白啰嗦一倍。链内形态确实不同的(化蝶 3136:平常/幽冥眼/
// 喵喵/奇丽花四个样子同链)才逐个带出来。
//
// 例外:**无链**的形态不带形态名(只写名字)—— 那种品种是按名字认的,选项跨的就是同名的一整组
// 形态,写上其中一个样子即错,理由见函数内。
//
// ⚠️ 这是**纯展示**字符串,不参与任何反查:身份与匹配一律走 ChainOf(链 id,见 pet.ChainRef)。
// 故分隔符可以自由挑,与 PetFullName 那种「一个字符不对就静默查不到」的 wiki 键完全不同 ——
// 但两条路都各自只有一个拼装点,别在别处再拼一份。
func (db *DB) ChainLabel(petbaseID uint32) string {
	steps := db.EvolutionChain(petbaseID)
	if len(steps) == 0 {
		return ""
	}
	// 无进化链的形态**只写名字**、不带形态:这种品种是按**名字**认的(见 ChainRef 的 evo==0 分支),
	// 一个选项/一条线跨的是「同名的那一整组形态」—— 海枝枝的 4 个样子(碧蓝珊瑚/杏黄百合/洋红沙丁/
	// 翠绿纶布)、首领变体与草系徽章变体,后端一律当同一只配。带上其中一个样子,等于把「这条线只认
	// 这一个形状」说成事实,而那正是玩家选错品种的由来。与 ChainLabelOf 的 evo==0 分支同口径。
	if db.petbase[petbaseID].Evo == 0 {
		return steps[0].Name
	}
	formOf := func(id uint32) string { return db.petbase[id].Form }
	head, headForm := steps[0], formOf(steps[0].Petbase)
	var sb strings.Builder
	sb.WriteString(petFullName(head.Name, headForm))
	if len(steps) > 1 {
		sb.WriteString("（")
		for i, s := range steps[1:] {
			if i > 0 {
				sb.WriteString("/")
			}
			if f := formOf(s.Petbase); f == headForm {
				sb.WriteString(s.Name)
			} else {
				sb.WriteString(petFullName(s.Name, f))
			}
		}
		sb.WriteString("）")
	}
	return sb.String()
}

// ChainLabelOf 按**品种标识**(链 id + 名字,见 pet.BreedingLine)取展示名。
//
// 培育线存的不是 petbase id(那会随游戏版本改数据而失效),故不能直接调 ChainLabel:
// 有链时从链上任一成员出发都能得到同一条链(EvolutionChain 按阶段排序,结果与起点无关),
// 无链时就是这个形态自己 —— 名字,与 ChainLabel 的无链分支同口径。
func (db *DB) ChainLabelOf(evo uint32, species string) string {
	if evo != 0 {
		if ids := db.evoIndex[evo]; len(ids) > 0 {
			return db.ChainLabel(ids[0])
		}
	}
	return species
}

// ChainOption 是培育页「品种」下拉的一项。
//
// 为什么给的是链而不是形态名:玩家要的是「培育阿米亚特这条线」,而库里那只可能早就进化成
// 罗隐了 —— 选项必须按品种(链)列,选一次就覆盖链上全部阶段。链首(初始形态)放在 Base 上,
// 下拉里拿它的图当图标:长名字(「阿米亚特（阿米樱/罗隐/深渊罗隐）」)靠文字认不出来,
// 而同名多形态的两条链连文字都一样(「地鼠（枯水期的样子）（遁鼠/遁地鼠）」)。
type ChainOption struct {
	Evo     uint32 `json:"evo"`     // 进化链 id;0 = 无链形态(按名字认)
	Species string `json:"species"` // 链首形态名;无链时即该形态名(按它匹配)
	Base    uint32 `json:"base"`    // 链首(初始形态)的 petbase id
	Label   string `json:"label"`
	Img     string `json:"img,omitempty"` // 链首头像(品种的「证件照」,形态之间的差别在头上最明显)
	// Egg 是这个品种的**蛋图**(见 EggIconOfBase):培育页要孵的就是它,「这条线对应哪种蛋」
	// 在选品种这一刻就该看得见。
	//
	// 认不出时为空(同一物种在 petbase 里可能有多个条目、蛋只挂在其中一个上,如板板壳 3055 有蛋
	// 而 3516 没有),此时由前端退回 Img。**它不是「能否生育」的判据** —— 那是 EggGroups 的事
	// (见 ChainOptions 里的过滤:蛋组「未发现」的品种才不进候选)。
	Egg string `json:"egg,omitempty"`
	// EggGroups 是**链首形态**的蛋组名(取自 PetEggGroups,与 IsInfertile 同一份数据)。
	//
	// 培育页用它把品种候选收窄到「与某只宠物至少共一个蛋组」的那批:配种要求母本与种公同蛋组,
	// 列一个配不上的品种等于让玩家白建一条线。繁殖组为「未发现」的品种本来就不在候选里
	// (见 ChainOptions 的过滤),故这里不会下发「未发现」。
	//
	// 取不到蛋组的形态留空(超进化/分支形态,实测 64 个)。前端遇到空值时**不能**据此滤掉这个
	// 品种 —— 「没配蛋组」不等于「配不上」(见 IsInfertile 的注释),宁多勿漏。
	EggGroups []string `json:"eggGroups,omitempty"`
	// Count 是库里这个品种有几只(入参逐只给,同形态出现几次即几只)。
	Count int `json:"count"`
}

// ChainOptions 把一串 petbase 形态收敛成「品种」选项(去重,按图鉴号 → 链首 id 升序)。
//
// 入参是本账号宠物**当前形态**的 base_conf_id,逐只给(见 store.Scoped.PetBaseIDs)。
// 只列库里确实有的品种:蛋的物种随母本,选了库里没有的品种就永远配不出任何组合。
//
// 去重键就是**匹配口径**(见 pet.ChainRef.Match):有链按链 id、无链按名字。按名字去重会把
// 「阿米亚特」与「罗隐」拆成两个品种(它们是一条链)、把两种嗜波螺并成一个(它们是两条链),
// 两边都错 —— 选项与匹配必须同一套口径,否则玩家选了也配不出来。
//
// 只列**生得出蛋**的品种(见下面对「未发现」蛋组的过滤):列一个生不出来的,玩家建完线才发现
// 这条线永远不会有子代,而那时候他已经在填目标、在挑种母了。
func (db *DB) ChainOptions(bases []uint32) []ChainOption {
	type entry struct {
		opt  ChainOption
		head uint32
		book uint32
	}
	idx := make(map[string]int, len(bases))
	out := make([]entry, 0, len(bases))
	for _, base := range bases {
		id, ok := db.petbase[base]
		if !ok {
			continue
		}
		// 去重键 = 匹配口径(见上):有链按链 id,无链按名字。
		key := "n:" + id.Name
		if id.Evo != 0 {
			key = "e:" + strconv.FormatUint(uint64(id.Evo), 10)
		}
		if i, ok := idx[key]; ok {
			out[i].opt.Count++
			continue
		}
		// 链首 = 阶段最小的那个形态(EvolutionChain 已按阶段排好;无链时就是自身)。
		head, species := base, id.Name
		if steps := db.EvolutionChain(base); len(steps) > 0 {
			head, species = steps[0].Petbase, steps[0].Name
		}
		hi, ok := db.petbase[head]
		if !ok {
			continue
		}
		idx[key] = len(out)
		out = append(out, entry{
			opt: ChainOption{
				Evo: id.Evo, Species: species, Base: head,
				Label: db.ChainLabel(base), Img: db.PetImageByBase(head, false).Head,
				Egg: db.EggIconOfBase(head), EggGroups: eggGroupNames(db.PetEggGroups(head)),
				Count: 1,
			},
			head: head, book: hi.Book,
		})
	}
	// 生不出蛋的品种不进候选:判据是**繁殖组(蛋组)**为「未发现」(见 IsInfertile),不是
	// 「查不查得到蛋图」—— 后者会误伤几十个正常品种(理由见 IsInfertile 的注释)。
	// 按**链首**判:整条链共享一个品种,链上任一阶段的 ♀ 都能当种母,而蛋组是按形态配的。
	kept := out[:0]
	for _, e := range out {
		if db.IsInfertile(e.head) {
			continue
		}
		kept = append(kept, e)
	}
	out = kept

	sort.Slice(out, func(i, j int) bool {
		if out[i].book != out[j].book {
			return out[i].book < out[j].book
		}
		return out[i].head < out[j].head
	})
	res := make([]ChainOption, 0, len(out))
	for _, e := range out {
		res = append(res, e.opt)
	}
	return res
}

// NatureEffect 返回性格的 +10%/-10% 维度(六维编号 1-6;0 表示无)。
func (db *DB) NatureEffect(natureID uint32) NatureEffect { return db.natureEffect[key(natureID)] }

// NatureMatrix 返回 6×6 性格方阵:第一维是**增益**维度编号-1(0-5),第二维是
// **减益**维度编号-1(0-5),值即该格性格名;对角线(增減同一维,游戏内不存在)
// 与数据缺失的格子为空串。
//
// 为什么按方阵给而不是给一份「性格 → {pos,neg}」清单:性格数据本身就是这张表
// (31 个性格里 30 个非中性,恰好铺满 6×6 去掉对角线),前端要画的就是这个形状。
// 让前端自己按 pos/neg 拼表会把「维度编号 ↔ 展示顺序」的对应关系复制一份到前端,
// 两边一旦漂移,格子里的名字就全错位 —— 而错位后**看起来完全正常**(只是每格
// 装着一个别的性格),无从发现。
//
// 维度编号(1-6)与六维的对应见 internal/server 的 iconMeta.Stat:1生命 2物攻 3魔攻
// 4物防 5魔防 6速度。
func (db *DB) NatureMatrix() [6][6]string {
	var out [6][6]string
	for k, name := range db.nature {
		if name == "" {
			continue
		}
		id, err := strconv.ParseUint(k, 10, 64)
		if err != nil {
			continue
		}
		eff := db.natureEffect[key(uint32(id))]
		pos, neg := eff.Pos, eff.Neg
		if pos < 1 || pos > 6 || neg < 1 || neg > 6 || pos == neg {
			continue // 中性或越界:不进方阵
		}
		out[pos-1][neg-1] = name
	}
	return out
}

// Species 返回种类名(conf_id)。
func (db *DB) Species(confID uint32) string { return db.species[key(confID)] }

// Nature 返回性格名(nature id)。
func (db *DB) Nature(id uint32) string { return db.nature[key(id)] }

// SkillDamType 返回系别名(SkillDamType enum 整数值)。
func (db *DB) SkillDamType(v int32) string { return db.skillDamType[strconv.FormatInt(int64(v), 10)] }

// TalentRate 返回天分评价名(talent_rank)。
func (db *DB) TalentRate(rank uint32) string { return db.talentRate[key(rank)] }

// PartnerMark 返回标记名(PetPartnerMarkType enum 整数值)。
func (db *DB) PartnerMark(v int32) string { return db.partnerMark[strconv.FormatInt(int64(v), 10)] }

// Speciality 返回特长名(speciality_id)。
func (db *DB) Speciality(id uint32) string { return db.speciality[key(id)] }

// Medal 返回奖牌名称与描述(wear_medal_conf_id)。
func (db *DB) Medal(id uint32) (Medal, bool) { m, ok := db.medal[key(id)]; return m, ok }

// MedalEntry 是带 id 的奖牌(用于全量奖牌墙)。
type MedalEntry struct {
	ID   uint32 `json:"id"`
	Name string `json:"name"`
	Desc string `json:"desc"`
	Icon string `json:"icon,omitempty"` // medal/<原名>.webp(无图或未 embed 时空)
}

// AllMedals 返回全部奖牌,按 id 升序(供前端奖牌墙展示全部奖牌)。
func (db *DB) AllMedals() []MedalEntry {
	out := make([]MedalEntry, 0, len(db.medal))
	for k, v := range db.medal {
		id, _ := strconv.ParseUint(k, 10, 32)
		out = append(out, MedalEntry{ID: uint32(id), Name: v.Name, Desc: v.Desc, Icon: db.MedalIcon(uint32(id))})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// AllSpecialities 返回全部特长名(按 id 升序去重),供前端高亮规则点选。
func (db *DB) AllSpecialities() []string { return sortedNames(db.speciality) }

// sortedNames 把 id(字符串)→名 的映射按数值 id 升序取名、去空去重。
func sortedNames(m map[string]string) []string {
	type kv struct {
		id   uint64
		name string
	}
	arr := make([]kv, 0, len(m))
	for k, v := range m {
		if v == "" {
			continue
		}
		id, _ := strconv.ParseUint(k, 10, 64)
		arr = append(arr, kv{id, v})
	}
	sort.Slice(arr, func(i, j int) bool { return arr[i].id < arr[j].id })
	out := make([]string, 0, len(arr))
	seen := map[string]bool{}
	for _, e := range arr {
		if seen[e.name] {
			continue
		}
		seen[e.name] = true
		out = append(out, e.name)
	}
	return out
}

// GenderName 返回性别符号。
func GenderName(g uint32) string {
	switch g {
	case 1:
		return "♂"
	case 2:
		return "♀"
	default:
		return ""
	}
}
