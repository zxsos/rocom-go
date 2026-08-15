package pet

// 精灵蛋(背包物品)的解析。蛋不是 PetData 而是 BagItem(type==8)上挂的 PetEggBrief,
// 字段语义与坑见 docs/data.md 3.6。
//
// 蛋会经多条消息露面,但载体只有两种:
//   - 背包全量分页 0x1344:bag_info(4).item_list(3).items(2)
//   - 任何带 ret_info 的回包/通知:ret_info(1).goods_change_info(4).changes(1).bag_item(4)
//     (收蛋 0x0243、放进孵蛋器 0x0164、孵化状态 0x0312、破壳 0x030c 都走这条)
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

	Src        int32  `json:"src"`               // EggAcquireWayType
	SrcName    string `json:"srcName,omitempty"` // 牧场/好友赐福/其他
	Random     bool   `json:"random,omitempty"`  // 神奇的蛋(物种未知)
	ObtainedAt int64  `json:"obtainedAt"`        // 获得时间(unix 秒)

	Hatching    bool  `json:"hatching"`              // 在孵蛋器里
	HatchedSecs int32 `json:"hatchedSecs,omitempty"` // 已孵秒数(HatchUpdate 时刻的快照)
	MaxSecs     int32 `json:"maxSecs,omitempty"`     // 孵满所需秒数
	HatchUpdate int64 `json:"hatchUpdate,omitempty"` // 上面那个数的计算时刻(前端据此外推)
	StartHatch  int64 `json:"startHatch,omitempty"`  // 放进孵蛋器的时刻

	Hatched   bool   `json:"hatched,omitempty"`   // 已破壳(记录保留)
	HatchedAt int64  `json:"hatchedAt,omitempty"` // 破壳时刻
	PetGid    uint32 `json:"petGid,omitempty"`    // 孵出的宠物 gid

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
		v.Name = it.Name
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
	return v
}

func round3(v float64) float64 { return math.Round(v*1000) / 1000 }
