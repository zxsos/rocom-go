package gamedata

import (
	mathrand "math/rand"
	"sort"
	"strconv"
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
type PetImage struct {
	Head          string `json:"head"`          // 小头像 HeadIcon/<n>.webp
	BigHead       string `json:"bigHead"`       // 大头像 BigHeadIcon256/<n>.webp
	Portrait      string `json:"portrait"`      // 全身图 Pet1024/JL_<x>.webp
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
	var img PetImage
	if head != "" {
		img.Head = "HeadIcon/" + head + ".webp"
	}
	if big != "" {
		img.BigHead = "BigHeadIcon256/" + big + ".webp"
	}
	if e.P != "" {
		img.Portrait = "Pet1024/JL_" + e.P + ".webp"
	}
	if ps != "" {
		img.PortraitSmall = "Pet256/JL_" + ps + ".webp"
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
	// 按阶段升序;同阶段(分支进化,如果冻→抹茶/椰浆/熔岩布丁)再按图鉴号,保证顺序稳定。
	sort.Slice(steps, func(i, j int) bool {
		if steps[i].Stage != steps[j].Stage {
			return steps[i].Stage < steps[j].Stage
		}
		return steps[i].Book < steps[j].Book
	})
	return steps
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
