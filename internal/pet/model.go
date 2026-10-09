package pet

import (
	"math"

	"github.com/zxsos/rocom-go/internal/gamedata"
	"github.com/zxsos/rocom-go-parse/pb"
)

// PetBoxLoc 是宠物在仓库盒子里的位置(box_id 从 1 起,slot 盒内格位从 0 起)。
type PetBoxLoc struct {
	BoxID   int32  `json:"boxId"`             // 盒子编号
	Slot    int32  `json:"slot"`              // 盒内格位(0 起)
	BoxName string `json:"boxName,omitempty"` // 盒子名(玩家命名,可空)
	Mark    string `json:"mark,omitempty"`    // 分类标记中文(首领/污染/奇异/炫彩/闪光)
}

// PetTeamLoc 是宠物在大世界队伍中的位置(teamIdx/pos 均从 0 起,最多 3 队、每队 6 位)。
type PetTeamLoc struct {
	TeamIdx int32 `json:"teamIdx"` // 第几队(0 起)
	Pos     int32 `json:"pos"`     // 队内位置(0 起)
}

// Stat 是一项六维属性。
type Stat struct {
	Value    int32 `json:"value"`    // 最终面板值
	TalentLv int32 `json:"talentLv"` // 天分等级(1-10，0 表示该维度无天分)
	Nature   int8  `json:"nature"`   // 性格影响：1=增益(+10%) -1=减益(-10%) 0=无
}

// Pet 是用于前端展示/存储的业务模型(已中文化)。
type Pet struct {
	Gid        uint32 `json:"gid"`             // 唯一实例 id
	ConfID     uint32 `json:"confId"`          // 种类配置 id(指向进化线一阶 base)
	BaseConfID uint32 `json:"baseConfId"`      // 当前形态 petbase id(进化后随之变化)
	Species    string `json:"species"`         // 种类名(当前形态)
	Book       uint32 `json:"book,omitempty"`  // 图鉴编号
	Form       string `json:"form,omitempty"`  // 地区/季节形态名(普通宠物为空)
	Stage      uint32 `json:"stage,omitempty"` // 进化阶段
	Name       string `json:"name"`            // 昵称
	Level      uint32 `json:"level"`

	NatureID uint32   `json:"natureId"`
	Nature   string   `json:"nature"` // 性格名
	Gender   string   `json:"gender"` // ♂ / ♀
	Types    []string `json:"types"`  // 系别中文(可多系)
	// 系别图标相对路径(与 Types 一一对应;无图为空串,由前端拼到 /img/ 下)。
	TypeIcons []string `json:"typeIcons"`

	BloodID   uint32 `json:"bloodId,omitempty"`   // 血脉编号(1-24,PetData.blood_id)
	Blood     string `json:"blood,omitempty"`     // 血脉中文短名(普通/草/火…)
	BloodIcon string `json:"bloodIcon,omitempty"` // 血脉主图标相对路径

	EggGroups []gamedata.EggGroup `json:"eggGroups,omitempty"` // 蛋组(繁殖组),1~2 个,name 社区名 + desc 官方描述

	HeightM  float64 `json:"heightM"`  // 身高(米)
	WeightKg float64 `json:"weightKg"` // 体重(千克)
	// 当前形态的身高/体重取值范围(米/千克)与当前值在范围内的百分位(0-100);
	// 由 FillSizePercentile 按 base_conf_id 在读取时注入(缺该形态数据则为 nil/0,前端据此不显示区间)。
	HeightMin float64  `json:"heightMin,omitempty"`
	HeightMax float64  `json:"heightMax,omitempty"`
	HeightPct *float64 `json:"heightPct,omitempty"`
	WeightMin float64  `json:"weightMin,omitempty"`
	WeightMax float64  `json:"weightMax,omitempty"`
	WeightPct *float64 `json:"weightPct,omitempty"`
	Voice     int32    `json:"voice"` // 声音值

	TalentRank      string   `json:"talentRank"` // 天分评价
	Medal           string   `json:"medal"`      // 佩戴奖牌名
	MedalDesc       string   `json:"medalDesc"`
	MedalIcon       string   `json:"medalIcon,omitempty"` // 佩戴奖牌小图相对路径
	WearMedalConfID uint32   `json:"wearMedalConfId"`
	MedalIDs        []uint32 `json:"medalIds"`                  // 该宠物已拥有的奖牌 id(佩戴+custom+free,去重)
	PartnerMark     string   `json:"partnerMark"`               // 标记
	PartnerMarkIcon string   `json:"partnerMarkIcon,omitempty"` // 搭档标记图标相对路径
	Speciality      string   `json:"speciality"`                // 特长
	SpecialityID    uint32   `json:"specialityId"`

	CatchTime int64 `json:"catchTime"` // 捕捉时间(unix 秒)
	Shiny     bool  `json:"shiny"`     // 异色(mutation_type bit0)
	Colorful  bool  `json:"colorful"`  // 炫彩(mutation_type bit3)

	// 炫彩类型与数值(见 gamedata.GlassDesc):普通炫彩 glassValue 是打包色号
	// ((粒子id<<20)|配色id),隐藏炫彩是 HIDDEN_GLASS_CONF.id(1/2/3 赛季、1000 黑白)。
	// 前端据此用 glassConf.js 的素材 CSS mask 渲染色卡,不预生成图片。
	GlassType  int32 `json:"glassType,omitempty"`
	GlassValue int32 `json:"glassValue,omitempty"`

	Image gamedata.PetImage `json:"image"` // 各尺寸图片相对路径(由前端拼到 /img/ 下)

	Box  *PetBoxLoc  `json:"box,omitempty"`  // 仓库盒子位置(来自 PetBackpackInfo,读取时 JOIN 注入)
	Team *PetTeamLoc `json:"team,omitempty"` // 大世界队伍位置(在队宠物不在盒子里,二者互斥)

	HP        Stat `json:"hp"`
	Attack    Stat `json:"attack"`    // 物攻
	Defense   Stat `json:"defense"`   // 物防
	SpAttack  Stat `json:"spAttack"`  // 魔攻
	SpDefense Stat `json:"spDefense"` // 魔防
	Speed     Stat `json:"speed"`

	// 技能列表(SkillIDs)已整条移除 —— 见 git 提交「移除技能列表」的原因:
	// 它是**可换的配置**而非个体属性,本地化没梳理只能显示裸编号,任何筛选/排序都用不上,
	// 却要往每只宠物的 data JSON 里塞十来个编号进库(上游实测 983 只:1357 → 1220 KiB)。
}

// ToPet 把解码后的 PetData 结合名称库转成业务模型。
func ToPet(p *pb.PetData, db *gamedata.DB) *Pet {
	types := make([]string, 0, len(p.GetSkillDamType()))
	typeIcons := make([]string, 0, len(p.GetSkillDamType()))
	for _, t := range p.GetSkillDamType() {
		if name := db.SkillDamType(int32(t)); name != "" {
			types = append(types, name)
			typeIcons = append(typeIcons, db.SkillDamTypeIcon(int32(t)))
		}
	}

	// 当前形态:base_conf_id 直接指向当前 petbase(进化后随之变化),据此取名称/头像/图鉴/形态;
	// 旧逻辑用 conf_id 只会得到进化线一阶 base(火神显示成火花),故优先用 base_conf_id,缺失再回退。
	// mutation_type bit0=异色,异色宠物部分有专属头像/全身图(无则回退普通)。
	shiny := p.GetMutationType()&1 != 0
	confID, base := p.GetConfId(), p.GetBaseConfId()
	species := db.Species(confID)
	var book, stage uint32
	var form string
	if base != 0 {
		if info, ok := db.PetBase(base); ok {
			if info.Name != "" {
				species = info.Name
			}
			book, form, stage = info.Book, info.Form, info.Stage
		}
	}

	out := &Pet{
		Gid:        p.GetGid(),
		ConfID:     confID,
		BaseConfID: base,
		Species:    species,
		Book:       book,
		Form:       form,
		Stage:      stage,
		Name:       string(p.GetName()),
		Level:      p.GetLevel(),
		NatureID:   p.GetNature(),
		Nature:     db.Nature(p.GetNature()),
		Gender:     gamedata.GenderName(p.GetGender()),
		Types:      types,
		TypeIcons:  typeIcons,
		BloodID:    p.GetBloodId(),
		Blood:      db.BloodName(p.GetBloodId()),
		BloodIcon:  db.BloodIcon(p.GetBloodId()),
		EggGroups:  db.PetEggGroups(base),
		HeightM:    float64(p.GetHeight()) / 100,
		WeightKg:   float64(p.GetWeight()) / 1000,
		Voice:      p.GetVoice(),

		TalentRank:      db.TalentRate(p.GetTalentRank()),
		WearMedalConfID: p.GetWearMedalConfId(),
		MedalIcon:       db.MedalIcon(p.GetWearMedalConfId()),
		PartnerMark:     db.PartnerMark(int32(p.GetPartnerMark())),
		PartnerMarkIcon: db.PartnerMarkIcon(int32(p.GetPartnerMark())),
		SpecialityID:    p.GetSpecialityId(),
		Speciality:      db.Speciality(p.GetSpecialityId()),

		CatchTime: int64(p.GetAddTime()),
		// mutation_type 为位标志: bit0=异色, bit3=炫彩(实测样本验证)。
		Shiny:    shiny,
		Colorful: p.GetMutationType()&8 != 0,
	}
	// 图片按「当前形态 → 进化线一阶」的次序取,单独走 FillPetImage —— store 的窄投影
	// 读取要用同一套,两处各写一套迟早分叉(见该函数的注释)。
	FillPetImage(db, out)

	// 炫彩外观类型/数值直接取自 GlassInfo(与 mutation_type bit3 一致),
	// 前端据此用玻璃色卡素材 CSS mask 渲染色卡(见 web/src/components/badges.jsx)。
	if gi := p.GetGlassInfo(); gi != nil {
		out.GlassType = int32(gi.GetGlassType())
		out.GlassValue = gi.GetGlassValue()
	}

	if m, ok := db.Medal(p.GetWearMedalConfId()); ok {
		out.Medal = m.Name
		out.MedalDesc = m.Desc
	}
	// 该宠物已拥有的奖牌(佩戴 + custom + free,去重),供奖牌墙高亮。
	seen := map[uint32]bool{}
	for _, id := range append([]uint32{p.GetWearMedalConfId()}, append(p.GetCustomMedalConfId(), p.GetFreeMedalConfIds()...)...) {
		if id != 0 && !seen[id] {
			seen[id] = true
			out.MedalIDs = append(out.MedalIDs, id)
		}
	}

	// 六维按编号 1-6 顺序: 1生命 2物攻 3魔攻 4物防 5魔防 6速度。
	stats := []*Stat{&out.HP, &out.Attack, &out.SpAttack, &out.Defense, &out.SpDefense, &out.Speed}

	// 性格增减维度(道具修改过则以 changed_nature_* 为准，否则取性格默认)。
	ne := db.NatureEffect(p.GetNature())
	posAttr, negAttr := ne.Pos, ne.Neg
	if t := int32(p.GetChangedNaturePosAttrType()); t != 0 {
		posAttr = t
	}
	if t := int32(p.GetChangedNatureNegAttrType()); t != 0 {
		negAttr = t
	}
	for i, s := range stats {
		idx := int32(i + 1)
		if idx == posAttr {
			s.Nature = 1
		} else if idx == negAttr {
			s.Nature = -1
		}
	}

	if attr := p.GetAttributeInfo(); attr != nil {
		src := []*pb.PetAttributeData{
			attr.GetHp(), attr.GetAttack(), attr.GetSpecialAttack(),
			attr.GetDefense(), attr.GetSpecialDefense(), attr.GetSpeed(),
		}
		for i, a := range src {
			if a != nil {
				stats[i].Value = int32(a.GetBaseValue())
				stats[i].TalentLv = a.GetTalentAddValue() // 天分(1-10)
			}
		}
	}

	// attribute_new_info 直接给出最终面板值(已含等级/努力/奖牌加成)，覆盖 base_value。
	if newAttr := p.GetAttributeNewInfo(); newAttr != nil {
		finals := make(map[int32]int32)
		for _, a := range newAttr.GetAddiAttrData() {
			finals[a.GetType()] += a.GetAddiAttr()
		}
		for i, s := range stats {
			if v, ok := finals[int32(i+1)]; ok {
				s.Value = v
			}
		}
	}

	return out
}

// FillPetImage 按「当前形态 → 进化线一阶」的次序补出各尺寸图片路径。
//
// 为什么这个次序:base_conf_id 指向**当前形态**,conf_id 只到进化线一阶 —— 只按 conf_id
// 取会把进化过的宠物显示成一阶的样子(火神显示成火花),故先给一阶兜底、再由形态覆盖。
//
// 抽成函数是因为 store 的**窄投影**读取也要补这一项:那条路不反序列化 data JSON,图片
// 只能按同样的规则从 gamedata 重算。两处各写一套迟早分叉(与 FillSizePercentile 同理)。
//
// shiny 参与取图:异色变体有专属头像,但仅在「索引里有该字段且 webp 确已 embed」时才用,
// 否则回退普通图(见 gamedata.imageOf)—— 故这里必须把 Shiny 传下去。
func FillPetImage(db *gamedata.DB, pets ...*Pet) {
	for _, p := range pets {
		img := db.PetImage(p.ConfID, p.Shiny)
		if p.BaseConfID != 0 {
			if byBase := db.PetImageByBase(p.BaseConfID, p.Shiny); byBase != (gamedata.PetImage{}) {
				img = byBase
			}
		}
		p.Image = img
	}
}

// FillSizePercentile 按当前形态(base_conf_id)为宠物注入身高/体重取值范围及当前值百分位。
//
// 范围属静态参考数据,不随宠物存库,故在读取时注入(与奖牌墙同):这样历史入库的宠物无需
// 重新抓包也能显示,且游戏版本更新后范围随 gamedata 同步刷新。范围原始整数与 PetData.height/
// weight 同单位(÷100 米、÷1000 千克),百分位 = (当前值-下限)/(上限-下限),裁剪到 0-100。
func FillSizePercentile(db *gamedata.DB, pets ...*Pet) {
	for _, p := range pets {
		info, ok := db.PetBase(p.BaseConfID)
		if !ok {
			continue
		}
		p.HeightMin, p.HeightMax = float64(info.HeightLow)/100, float64(info.HeightHigh)/100
		p.WeightMin, p.WeightMax = float64(info.WeightLow)/1000, float64(info.WeightHigh)/1000
		p.HeightPct = SizePercentile(p.HeightM, p.HeightMin, p.HeightMax)
		p.WeightPct = SizePercentile(p.WeightKg, p.WeightMin, p.WeightMax)
	}
}

// SizePercentile 返回 cur 落在 [min,max] 内的百分位(0-100,**保留四位小数**);范围无效(max<=min)
// 返回 nil。宠物列表/事件页的「W xx%」与实时地图野生宠物标记共用此口径,勿各算各的。
//
// ⚠️ **精度不许降到 3 位及以下(2026-09-13 连踩两次)。** 奖牌四件套是按百分位**阈值**判的
// (大块头 98 / 小不点 2 / 婉转声 96 / 粗嗓门 -96,见 pipeline/wildpets.go、store/query.go),
// 而 cur 是整数体重(weight/1000)反算出来的,百分位只落在**间距 100/(high-low)** 的格点上;
// 舍入窗口一旦比「格点到边界的最近距离」还宽,就会把格点挪过阈值、误判成拿得到那枚奖牌:
//
//	噼啪鸟(区间 89.5~127.5 kg)  126.739 kg → 97.997368%
//	  2 位 → 98.00  ✗ 差真边界(126.740 kg)还 1 克,却算大块头
//	  3 位 → 97.997 ✓(1 克 = 0.0026pp,窗口 0.0005pp 细于格点)
//	迷嶂布莱克(区间 668.5~870.4 kg,格点间距仅 0.000495pp)
//	  866.361 kg → 97.999505%
//	  3 位 → 98.000  ✗ 仍是差 1 克就被算成大块头(窗口 0.0005pp 比格点还粗)
//	  4 位 → 97.9995 ✓
//
// 所以位数不能拍脑袋定,得由数据定:遍历全部形态后,「可达格点到奖牌边界的最小距离」
// 是 0.000345pp(深渊罗隐 320~610 kg),4 位的窗口 ±0.00005pp 才够;3 位会让 12 条形态
// 记录(迷嶂布莱克/祭礼巨像/圣剑骑士/深渊罗隐及其变体)跨界误判,2 位则多达 257 条。
// 这条不变量由 TestSizePercentileRoundKeepsMedalVerdict 遍历全量形态守着 ——
// 将来新赛季加入区间更窄的形态时会直接报红,提示提高位数,而不是悄悄误判。
//
// 改这里必须同步改前端(web/src/utils/rules.js 的 round4 与各处 toFixed(4)):三处不同精度
// 就会重新出现「图上描了圈、列表里筛不到」。
func SizePercentile(cur, min, max float64) *float64 {
	if max <= min {
		return nil
	}
	v := (cur - min) / (max - min) * 100
	if v < 0 {
		v = 0
	} else if v > 100 {
		v = 100
	}
	v = math.Round(v*10000) / 10000
	return &v
}
