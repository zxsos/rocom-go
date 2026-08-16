package pet

// 精灵蛋(背包物品)的解析。蛋不是 PetData 而是 BagItem(type==8)上挂的 PetEggBrief,
// 字段语义与坑见 docs/data.md 3.6。
//
// 蛋会经多条消息露面,但载体只有两种:
//   - 背包全量分页 0x1344:bag_info(4).item_list(3).items(2)
//   - 任何带 ret_info 的回包/通知:ret_info(1).goods_change_info(4).changes(1).bag_item(4)
//     (收蛋 0x0243、商店购买 0x0262、放进孵蛋器 0x0164、孵化状态 0x0312、破壳 0x030c 都走这条)
// 两者的元素都是 BagItem,故只需一个 parseBagItem。

import (
	"math"

	"google.golang.org/protobuf/encoding/protowire"

	"github.com/whoisnian/rocom-capture/internal/gamedata"
	"github.com/whoisnian/rocom-capture/internal/wire"
)

// 精灵蛋相关 opcode(ZoneSvrCmd)。
const (
	OpGetBagItemInfoByPageRsp = 0x1344 // ZONE_GET_BAG_ITEM_INFO_BY_PAGE_RSP(4932), 背包分页全量
	OpUseBagItemRsp           = 0x0164 // ZONE_USE_BAG_ITEM_RSP(356), 用道具(把蛋放进孵蛋器即走这条)
	OpGetAllHatchStatusRsp    = 0x0312 // ZONE_GET_ALL_HATCH_STATUS_RSP(786), 孵蛋器里各蛋的进度
	OpCrackEggReq             = 0x030b // ZONE_CRACK_EGG_REQ(779), c2s 破壳(egg_gid + 选用的球)
	OpShopBuyItemRsp          = 0x0262 // ZONE_SHOP_BUY_ITEM_RSP(610), 商店购买(远行商人的神奇的蛋走这条,不发奖励通知)
)

// EggItemType 是精灵蛋在 BAG_ITEM_CONF/BagItem 里的 type 值。
const EggItemType = 8

// Egg 是一颗背包里的精灵蛋(BagItem + egg_data)。
type Egg struct {
	Gid        uint32 // 背包物品 gid:蛋的唯一 id(孵化状态里的 egg_gid 即此)
	ItemID     uint32 // 蛋物品 id(107028…),查 gamedata.EggItemInfo 得显示名/图标/物种
	UpdateTime int32  // bag_item.update_time:进包/最后变更时刻,即「获得时间」
	// 以下来自 egg_data(PetEggBrief)
	ConfID      uint32 // 物种 conf_id;随机蛋(神奇的蛋)为 0
	Height      int32  // ÷100 米(下蛋时定死)
	Weight      int32  // ÷1000 千克
	HatchedSec  int32  // 已孵秒数(服务器在 HatchUpdate 时刻算出)
	MaxSec      int32  // 孵满所需秒数(随机蛋只能靠它,见 3.6)
	HatchUpdate int32  // last_hatch_update_sec:HatchedSec 的计算时刻
	StartHatch  int32  // start_hatch_time:放进孵蛋器的时刻;0=不在孵蛋器里
	Src         int32  // EggAcquireWayType:6=牧场(家园小窝),5=好友赐福,0=其他
	RandomConf  uint32 // random_egg_conf:随机蛋的外观配置(非 0 即随机蛋)
	Precious    int32  // precious_egg_type:蛋品类(异色/炫彩…)。实测服务器不下发,
	// 客户端自己查 PET_EGG_CONF[conf_id].precious_egg_type,这里同样以配置为准(见 ToEggView)
}

// Hatching 报告这颗蛋是否正在孵蛋器里。
func (e Egg) Hatching() bool { return e.StartHatch > 0 }

// ParseBagEggs 从背包分页回包(0x1344)取本页的全部精灵蛋,并返回本页页号与总页数
// (供调用方判断一轮全量是否已收齐,与宠物列表的分页对账同一套路)。
func ParseBagEggs(body []byte) (eggs []Egg, page, total uint32) {
	if v, ok := wire.Varint(body, 3); ok { // req_page
		page = uint32(v)
	}
	if v, ok := wire.Varint(body, 2); ok { // total_page
		total = uint32(v)
	}
	bag := wire.SubMsg(body, 4) // bag_info(PlayerBagInfo)
	if bag == nil {
		return nil, page, total
	}
	for _, lst := range wire.Subs(bag, 3) { // item_list(BagItemTypeList)
		if t, ok := wire.Varint(lst, 1); ok && t != EggItemType {
			continue // 按类型分组下发,非蛋组整组跳过
		}
		for _, it := range wire.Subs(lst, 2) { // items(BagItem)
			if e, ok := parseBagItemEgg(it); ok {
				eggs = append(eggs, e)
			}
		}
	}
	return eggs, page, total
}

// ParseChangedEggs 从任意带 ret_info 的消息取 goods_change_info 里变更的精灵蛋。
// 收蛋/入孵/孵化进度/破壳都经此路径下发同一份 BagItem。
func ParseChangedEggs(body []byte) []Egg {
	var out []Egg
	ret := wire.SubMsg(body, 1) // ret_info
	if ret == nil {
		return nil
	}
	chg := wire.SubMsg(ret, 4) // goods_change_info(GoodsChange)
	if chg == nil {
		return nil
	}
	for _, c := range wire.Subs(chg, 1) { // changes(GoodsChangeItem)
		bi := wire.SubMsg(c, 4) // bag_item
		if bi == nil {
			continue
		}
		if e, ok := parseBagItemEgg(bi); ok {
			out = append(out, e)
		}
	}
	return out
}

// ParseFlowReason 取奖励通知(0x0243)的 flow_reason(3)。223 = FLOW_REASON_PET_HOME_LAY,
// 即「家园宠物下蛋」——从小窝上收下来的蛋走的就是这个理由(见 docs/data.md 3.6)。
func ParseFlowReason(body []byte) int32 {
	if v, ok := wire.Varint(body, 3); ok {
		return int32(v)
	}
	return 0
}

// FlowReasonHomeLay 是家园小窝下蛋的 flow_reason(ProtoEnum.FlowReason)。
const FlowReasonHomeLay = 223

// ParseCrackEggReq 取 c2s 破壳请求(0x030b)里的 egg_gid;c2s 有 6 字节子头,故先定位。
func ParseCrackEggReq(appBody []byte) uint32 {
	body := appBody
	if len(body) > c2sSubHeader {
		body = body[c2sSubHeader:]
	}
	if v, ok := wire.Varint(body, 1); ok {
		return uint32(v)
	}
	return 0
}

// c2sSubHeader 是 c2s AppBody 里 protobuf 之前的子头长度(实测恒为 6,见 docs/protocol.md)。
const c2sSubHeader = 6

// ParseCrackEggRsp 取破壳回包(0x030c)里孵出的宠物 gid。
func ParseCrackEggRsp(body []byte) uint32 {
	if v, ok := wire.Varint(body, 2); ok {
		return uint32(v)
	}
	return 0
}

// parseBagItemEgg 解一件 BagItem:gid(1)/id(2)/update_time(4)/type(14)/egg_data(15)。
// 非精灵蛋(无 egg_data)返回 ok=false。
func parseBagItemEgg(b []byte) (Egg, bool) {
	var e Egg
	var got bool
	wire.ScanFields(b, func(num protowire.Number, typ protowire.Type, val []byte, v uint64) {
		switch {
		case num == 1 && typ == protowire.VarintType:
			e.Gid = uint32(v)
		case num == 2 && typ == protowire.VarintType:
			e.ItemID = uint32(v)
		case num == 4 && typ == protowire.VarintType:
			e.UpdateTime = int32(v)
		case num == 15 && typ == protowire.BytesType: // egg_data(PetEggBrief)
			got = true
			parseEggBrief(val, &e)
		}
	})
	if !got || e.Gid == 0 {
		return e, false
	}
	return e, true
}

// parseEggBrief 解 PetEggBrief(字段表见 docs/data.md 3.6)。
func parseEggBrief(b []byte, e *Egg) {
	wire.ScanFields(b, func(num protowire.Number, typ protowire.Type, _ []byte, v uint64) {
		if typ != protowire.VarintType {
			return
		}
		switch num {
		case 1:
			e.ConfID = uint32(v)
		case 2:
			e.Height = int32(v)
		case 3:
			e.Weight = int32(v)
		case 4:
			e.HatchedSec = int32(v)
		case 5:
			e.HatchUpdate = int32(v)
		case 6:
			e.MaxSec = int32(v)
		case 9:
			e.StartHatch = int32(v)
		case 10:
			e.Src = int32(v)
		case 17:
			e.RandomConf = uint32(v)
		case 21:
			e.Precious = int32(v)
		}
	})
}

// ---- 展示模型(精灵蛋页面)----

// EggParent 是一只推测的亲本在**收蛋那一刻**的快照。存快照而非引用 gid,是因为宠物可能
// 被放生/赠送(pets 行随之删除),而蛋上的双亲信息应当留存(见 docs/data.md 3.6)。
type EggParent struct {
	Gid       uint32   `json:"gid"`
	Name      string   `json:"name"`
	Species   string   `json:"species"`
	ConfID    uint32   `json:"confId,omitempty"`
	Img       string   `json:"img,omitempty"`
	Gender    string   `json:"gender,omitempty"`
	HeightM   float64  `json:"heightM,omitempty"`
	WeightKg  float64  `json:"weightKg,omitempty"`
	HeightPct *float64 `json:"heightPct,omitempty"`
	WeightPct *float64 `json:"weightPct,omitempty"`
	Voice     int32    `json:"voice"`
	Nature    string   `json:"nature,omitempty"`
	Talent    string   `json:"talentRank,omitempty"`
}

// EggParents 是一颗家园蛋的推测双亲。母本确定(蛋就趴在她的窝上);父本取服务器下发的
// 配对候选(lay_egg_couple),几个窝挨太近「串窝」时会有多个候选,此时无法确定实际父本。
type EggParents struct {
	Mother     *EggParent  `json:"mother,omitempty"`
	Fathers    []EggParent `json:"fathers,omitempty"`
	Ambiguous  bool        `json:"ambiguous,omitempty"` // 父本候选多于一个(串窝)
	RecordedAt int64       `json:"recordedAt,omitempty"`
}

// EggMedal 是这颗蛋**确定**能拿到的一枚百分位奖牌(拿不到、或还判不了的都不进这个列表)。
// 体重那两枚(大块头/小不点)靠蛋自己的百分位;嗓音那两枚(婉转声/粗嗓门)靠双亲推出的嗓音。
type EggMedal struct {
	Dim  int32  `json:"dim"`  // 判定维度:2=体重 3=嗓音
	Name string `json:"name"` // 奖牌名
}

// EggView 是精灵蛋页面展示用的业务模型(已中文化、含百分位与孵化进度)。
type EggView struct {
	Gid     uint32 `json:"gid"`               // 背包物品 gid(唯一 id)
	ItemID  uint32 `json:"itemId"`            // 蛋物品 id
	Name    string `json:"name"`              // 「友爱天天的蛋」/「神奇的蛋」
	Species string `json:"species,omitempty"` // 孵出物种名(随机蛋未知,为空)
	ConfID  uint32 `json:"confId,omitempty"`
	Icon    string `json:"icon,omitempty"`   // 蛋图标相对路径(egg/<原名>.webp)
	PetImg  string `json:"petImg,omitempty"` // 孵出物种的头像(随机蛋为空)

	HeightM   float64  `json:"heightM"`
	WeightKg  float64  `json:"weightKg"`
	HeightMin float64  `json:"heightMin,omitempty"` // 该物种蛋自身的取值区间(非成体区间)
	HeightMax float64  `json:"heightMax,omitempty"`
	WeightMin float64  `json:"weightMin,omitempty"`
	WeightMax float64  `json:"weightMax,omitempty"`
	HeightPct *float64 `json:"heightPct,omitempty"`
	WeightPct *float64 `json:"weightPct,omitempty"`
	// 按同一百分位换算的成体尺寸(普通蛋实测原样保留,随机蛋不适用故为 0,见 docs/data.md 3.6)。
	AdultHeightM  float64 `json:"adultHeightM,omitempty"`
	AdultWeightKg float64 `json:"adultWeightKg,omitempty"`

	// 品类与排序键(游戏内背包的「品质排序」用,见 SortEggs)。
	TypeID    int32  `json:"typeId,omitempty"`    // precious_egg_type:0=普通,2=异色,6=炫彩…
	TypeName  string `json:"typeName,omitempty"`  // 异色精灵蛋 / 炫彩精灵蛋 …
	TypeIcon  string `json:"typeIcon,omitempty"`  // 品类角标 egg/<原名>.webp
	TypeOrder int32  `json:"typeOrder,omitempty"` // 品类排序号(display_order,越小越靠前)
	Quality   int32  `json:"quality,omitempty"`   // 物品品质(4/5;珍贵蛋按 5 算,同客户端)
	SortID    int32  `json:"sortId,omitempty"`    // 物品排序号(同品质时的次级键)

	Shiny    bool `json:"shiny,omitempty"`    // 异色蛋(品类 2/3):用全站统一的异色标记显示
	Colorful bool `json:"colorful,omitempty"` // 炫彩蛋(品类 3/6/7)

	// Voice 是从双亲推出的嗓音(双亲均值向下取整,见 docs/data.md 3.6);蛋上没有嗓音字段,
	// 非家园蛋(没有双亲快照)推不出来,为 nil。串窝时父本不唯一,VoiceMax 给出区间上界。
	Voice    *int32 `json:"voice,omitempty"`
	VoiceMax *int32 `json:"voiceMax,omitempty"`

	// Medals 是这颗蛋确定能拿到的百分位奖牌(判不了的不进来)。读取时算(要等双亲快照挂上
	// 才知道嗓音),见 FillEggDerived。
	Medals []EggMedal `json:"medals,omitempty"`

	Src        int32  `json:"src"`               // EggAcquireWayType
	SrcName    string `json:"srcName,omitempty"` // 牧场/好友赐福/其他
	Random     bool   `json:"random,omitempty"`  // 神奇的蛋(物种未知)
	ObtainedAt int64  `json:"obtainedAt"`        // 获得时间(unix 秒)

	Hatching    bool  `json:"hatching"`              // 在孵蛋器里
	HatchedSecs int32 `json:"hatchedSecs,omitempty"` // 已孵秒数(HatchUpdate 时刻的快照)
	MaxSecs     int32 `json:"maxSecs,omitempty"`     // 孵满所需秒数
	HatchUpdate int64 `json:"hatchUpdate,omitempty"` // 上面那个数的计算时刻(前端据此外推)
	StartHatch  int64 `json:"startHatch,omitempty"`  // 放进孵蛋器的时刻

	Parents *EggParents `json:"parents,omitempty"`
}

// eggSrcNames 是 EggAcquireWayType 的中文说明(ProtoEnum.EggAcquireWayType)。
var eggSrcNames = map[int32]string{
	1: "远行", 2: "首领", 3: "好友交换", 4: "奇迹交换", 5: "好友赐福", 6: "家园牧场",
}

// ToEggView 把一颗解析出的蛋结合名称库转成展示模型(不含双亲,双亲由 pipeline 另行推断)。
func ToEggView(e Egg, db *gamedata.DB) *EggView {
	v := &EggView{
		Gid: e.Gid, ItemID: e.ItemID, ConfID: e.ConfID,
		HeightM: float64(e.Height) / 100, WeightKg: float64(e.Weight) / 1000,
		Src: e.Src, SrcName: eggSrcNames[e.Src],
		Random:     e.ConfID == 0 || e.RandomConf != 0,
		ObtainedAt: int64(e.UpdateTime),
		Hatching:   e.Hatching(), HatchedSecs: e.HatchedSec, MaxSecs: e.MaxSec,
		HatchUpdate: int64(e.HatchUpdate), StartHatch: int64(e.StartHatch),
		Icon: db.EggIcon(e.ItemID),
	}
	if it, ok := db.EggItemInfo(e.ItemID); ok {
		v.Name, v.Quality, v.SortID = it.Name, it.Quality, it.SortID
	}
	if c, ok := db.EggConfInfo(e.ConfID); ok {
		v.Species = c.Name
		v.HeightMin, v.HeightMax = float64(c.HeightLow)/100, float64(c.HeightHigh)/100
		v.WeightMin, v.WeightMax = float64(c.WeightLow)/1000, float64(c.WeightHigh)/1000
		v.HeightPct = SizePercentile(v.HeightM, v.HeightMin, v.HeightMax)
		v.WeightPct = SizePercentile(v.WeightKg, v.WeightMin, v.WeightMax)
		if v.MaxSecs == 0 {
			v.MaxSecs = c.HatchSecs
		}
		// 成体尺寸:普通蛋的百分位在破壳时原样保留(实测三例),故可直接换算;
		// 随机蛋(conf_id=0)查不到物种,这里自然也算不出。
		if base, info, ok := db.PetBaseOf(e.ConfID); ok && base != 0 {
			if v.HeightPct != nil {
				v.AdultHeightM = round3(float64(info.HeightLow)/100 + *v.HeightPct/100*float64(info.HeightHigh-info.HeightLow)/100)
			}
			if v.WeightPct != nil {
				v.AdultWeightKg = round3(float64(info.WeightLow)/1000 + *v.WeightPct/100*float64(info.WeightHigh-info.WeightLow)/1000)
			}
			v.PetImg = db.PetImageByBase(base, false).Head
		}
	}
	if v.Name == "" {
		v.Name = "精灵蛋"
	}
	v.fillType(e, db)
	return v
}

// fillType 定下蛋的品类(异色/炫彩/珍贵…)与排序键。
// 品类**以配置为准**:PetEggBrief 有 precious_egg_type 字段,但实测服务器不填(97 颗蛋全空),
// 客户端自己查 PET_EGG_CONF[conf_id](见 PetUtils.GetPetEggConfigTypeByGID),这里照做;
// 服务器哪天真填了就优先用它。珍贵蛋按品质 5 算,与客户端 SortEggQualityDown 一致。
func (v *EggView) fillType(e Egg, db *gamedata.DB) {
	v.TypeID = e.Precious
	if v.TypeID == 0 {
		if c, ok := db.EggConfInfo(e.ConfID); ok {
			v.TypeID = c.Precious
		}
	}
	v.TypeOrder = db.EggTypeOrder(v.TypeID)
	if t, ok := db.EggTypeInfo(v.TypeID); ok {
		v.TypeName = t.Name
		if t.Icon != "" {
			v.TypeIcon = "egg/" + t.Icon + ".webp"
		}
	}
	v.Shiny = v.TypeID == PreciousEggShiny || v.TypeID == PreciousEggShinyGlass
	v.Colorful = v.TypeID == PreciousEggShinyGlass || v.TypeID == PreciousEggGlass ||
		v.TypeID == PreciousEggPickGlass
	if v.TypeID == PreciousEggPrecious && v.Quality < 5 {
		v.Quality = 5
	}
	// 异色蛋孵出的是异色个体,头像也该用异色版(没有专属异色图时自动回退普通)。
	if v.TypeID == PreciousEggShiny || v.TypeID == PreciousEggShinyGlass {
		if base, _, ok := db.PetBaseOf(e.ConfID); ok && base != 0 {
			if img := db.PetImageByBase(base, true).Head; img != "" {
				v.PetImg = img
			}
		}
	}
}

// dataconfig.PreciousEggType 里用得上的几个(其余品类只在排序/角标里按 id 走)。
const (
	PreciousEggPrecious   = 1 // 珍贵精灵蛋(客户端按品质 5 排)
	PreciousEggShiny      = 2 // 异色精灵蛋
	PreciousEggShinyGlass = 3 // 异色炫彩精灵蛋
	PreciousEggGlass      = 6 // 炫彩精灵蛋
	PreciousEggPickGlass  = 7 // 自选炫彩精灵蛋
)

// 嗓音取值范围(PET_GLOBAL_CONFIG.pet_voice_low/high),用来把嗓音换算成百分位:
// 婉转声要求百分位 ≥98 即嗓音 ≥96,与本机 812 只宠物的实测边界一致。
const (
	VoiceLow  = -100
	VoiceHigh = 100
)

// voicePct 把嗓音原值换算成百分位(0-100)。
func voicePct(v int32) float64 {
	return float64(v-VoiceLow) / float64(VoiceHigh-VoiceLow) * 100
}

// FillEggDerived 补算「要等双亲快照挂上才算得出」的部分:推测嗓音与百分位奖牌。
// 放在读取时做——双亲存在另一列,写蛋那一刻还不知道(见 store.ListEggs)。
func FillEggDerived(v *EggView, db *gamedata.DB) {
	v.Voice, v.VoiceMax = nil, nil
	if lo, hi, ok := parentVoice(v.Parents); ok {
		v.Voice = &lo
		if hi != lo {
			v.VoiceMax = &hi
		}
	}
	weight := pctRange{}
	if v.WeightPct != nil {
		weight = pctRange{lo: *v.WeightPct, hi: *v.WeightPct, known: true}
	}
	voice := pctRange{}
	if v.Voice != nil {
		hi := *v.Voice
		if v.VoiceMax != nil {
			hi = *v.VoiceMax
		}
		voice = pctRange{lo: voicePct(*v.Voice), hi: voicePct(hi), known: true}
	}
	v.Medals = eggMedals(db, weight, voice)
}

// parentVoice 按「双亲嗓音均值向下取整」推这颗蛋的嗓音(实测规律,见 docs/data.md 3.6)。
// 串窝时父本不唯一,逐个候选算一遍取上下界;没有双亲快照则 ok=false。
func parentVoice(p *EggParents) (lo, hi int32, ok bool) {
	if p == nil || p.Mother == nil || len(p.Fathers) == 0 {
		return 0, 0, false
	}
	for i, f := range p.Fathers {
		v := int32(math.Floor(float64(p.Mother.Voice+f.Voice) / 2))
		if i == 0 || v < lo {
			lo = v
		}
		if i == 0 || v > hi {
			hi = v
		}
	}
	return lo, hi, true
}

// pctRange 是某一维百分位的取值区间(串窝时嗓音只能给出区间);known=false 即这一维还不知道。
type pctRange struct {
	lo, hi float64
	known  bool
}

// eggMedals 判这颗蛋**确定**能拿到哪几枚百分位奖牌(体重最多一枚、嗓音最多一枚)。
// 体重百分位孵化后原样保留、嗓音可由双亲推出(见 docs/data.md 3.6),两者都能提前判;
// 值还不知道(随机蛋没区间 / 没有双亲)、或区间跨在窗口边上(串窝)时说不准,就不给。
func eggMedals(db *gamedata.DB, weight, voice pctRange) []EggMedal {
	var out []EggMedal
	for _, r := range []struct {
		dim int32
		pct pctRange
	}{{gamedata.MedalDimWeight, weight}, {gamedata.MedalDimVoice, voice}} {
		if !r.pct.known {
			continue
		}
		for _, sm := range db.SizeMedals() {
			if sm.Dim != r.dim {
				continue
			}
			loIn := r.pct.lo >= float64(sm.Low) && r.pct.lo <= float64(sm.High)
			hiIn := r.pct.hi >= float64(sm.Low) && r.pct.hi <= float64(sm.High)
			if loIn && hiIn {
				out = append(out, EggMedal{Dim: r.dim, Name: sm.Name})
				break
			}
			if loIn != hiIn {
				break // 区间跨在窗口边上:拿不拿得到说不准,不给
			}
		}
	}
	return out
}

// eggFromView 把落库的展示模型还原成原始蛋。库里 data 列存的是展示模型,但原始事实
// (物品/物种/尺寸/来源/孵化时刻)一个不少,还原后即可按当前名称库重算。
func eggFromView(v *EggView) Egg {
	return Egg{
		Gid: v.Gid, ItemID: v.ItemID, ConfID: v.ConfID,
		UpdateTime: int32(v.ObtainedAt),
		Height:     int32(math.Round(v.HeightM * 100)),
		Weight:     int32(math.Round(v.WeightKg * 1000)),
		HatchedSec: v.HatchedSecs, MaxSec: v.MaxSecs,
		HatchUpdate: int32(v.HatchUpdate), StartHatch: int32(v.StartHatch),
		Src: v.Src, Precious: v.TypeID,
	}
}

// RefreshEggView 按**当前**名称库重算一颗落库的蛋。
// 库里那份是写入当时的样子:本工具后加的字段(异色标记、品类排序键…)在旧行里根本没有,
// 游戏版本更新后名称/区间也可能变——不重算的话,得等玩家再开一次背包才对得上。
// 双亲快照存在另一列,原样带过来,再据此补推测嗓音与奖牌。
func RefreshEggView(v *EggView, db *gamedata.DB) *EggView {
	out := ToEggView(eggFromView(v), db)
	out.Parents = v.Parents
	FillEggDerived(out, db)
	return out
}

// SortEggs 按游戏内背包的排序方式重排(见 docs/data.md 3.6,复刻客户端 BagModuleData):
//
//	quality(品质排序 SortEggQualityDown): 品类排序号升 → 品质降 → 物品排序号升 → 获得时间降
//	obtained(获取时间 SortTimeDown):     获得时间降
//
// asc=true 即游戏里那个反向开关(IsReversalSort),整条比较取反。
//
// **连排序算法一起复刻**:上面这些键分不出高低的蛋(同一时刻入包的两颗同种蛋)最终谁在前,
// 取决于算法本身——客户端用的是 Lua 的 table.sort(快排,不稳定),换个方向排,相等的两个
// 就可能换位置。故这里不用 Go 的稳定排序,而是走 luaSort(逐位复刻 Lua 5.4,见 luasort.go),
// 并要求调用方按**背包原始次序**把蛋传进来(store.ListEggs 已按 seq 排,见 SetEggOrder)——
// 那正是客户端喂给 table.sort 的顺序。
func SortEggs(eggs []*EggView, by string, asc bool) {
	less := func(a, b *EggView) bool { return a.ObtainedAt > b.ObtainedAt }
	if by == "quality" {
		less = func(a, b *EggView) bool {
			switch {
			case a.TypeOrder != b.TypeOrder:
				return a.TypeOrder < b.TypeOrder
			case a.Quality != b.Quality:
				return a.Quality > b.Quality
			case a.SortID != b.SortID:
				return a.SortID < b.SortID
			}
			return a.ObtainedAt > b.ObtainedAt
		}
	}
	luaSort(len(eggs), func(i, j int) bool { // 下标 1 起
		if asc {
			return less(eggs[j-1], eggs[i-1])
		}
		return less(eggs[i-1], eggs[j-1])
	}, func(i, j int) {
		eggs[i-1], eggs[j-1] = eggs[j-1], eggs[i-1]
	})
}

// SortHatchingEggs 把孵蛋器里的蛋排成游戏内的槽位顺序:**按入孵时刻升序**
// (先放进去的占 1 号槽)。复刻客户端 UMG_PetHatching_C:UpdatePanel:
//
//	table.sort(backpackEggList, function(a, b)
//	  return a.eggData.start_hatch_time < b.eggData.start_hatch_time end)
//	for i = 1, 3 do ... positionIndex = i ... end
//
// 槽位顺序与背包次序无关(实测三颗在孵的蛋,背包次序恰好是入孵次序的倒序,页面就整个反了)。
// 客户端喂给 table.sort 的是 PetBackpackInfo.egg_gid 的顺序,本工具手头只有背包次序,
// 两颗蛋同秒入孵时落位可能与游戏不同(实测未见);排序仍走 luaSort,见 SortEggs 的说明。
func SortHatchingEggs(eggs []*EggView) {
	luaSort(len(eggs), func(i, j int) bool {
		return eggs[i-1].StartHatch < eggs[j-1].StartHatch
	}, func(i, j int) {
		eggs[i-1], eggs[j-1] = eggs[j-1], eggs[i-1]
	})
}

func round3(v float64) float64 { return math.Round(v*1000) / 1000 }
