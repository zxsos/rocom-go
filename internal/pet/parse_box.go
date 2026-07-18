package pet

import (
	"google.golang.org/protobuf/proto"

	"github.com/whoisnian/rocom-capture/internal/pb"
	"github.com/whoisnian/rocom-capture/internal/wire"
)

// warehouseMark 是 WarehouseMarkType(盒子分类标记)枚举值 -> 中文。
var warehouseMark = map[int32]string{1: "首领", 2: "污染", 4: "奇异", 8: "炫彩", 16: "闪光"}

// MarkName 返回盒子分类标记中文(0/未知返回空)。
func MarkName(v int32) string { return warehouseMark[v] }

// BoxEntry 是一只宠物的盒子位置(供 store 落库)。
type BoxEntry struct {
	Gid     uint32
	BoxID   int32
	Slot    int32
	BoxName string
	Mark    int32
}

// BoxMeta 是一个盒子的元数据(名称/标记/是否锁定),与是否有宠物无关——空盒子也在内。
// 盒号 box_id 即展示位置(1 起),改名/换位/解锁都会更新这份全量元数据。
type BoxMeta struct {
	BoxID int32
	Name  string
	Mark  int32
	Lock  bool
}

// boxValid 判断一个 PetBox 的数值是否在合理范围(越界即为递归反序列化的误命中)。
func boxValid(bx *pb.PetBox) bool {
	return bx.GetVacancyNum() >= 0 && bx.GetVacancyNum() <= 200 && bx.GetBoxId() >= 0 && bx.GetBoxId() <= 1000
}

// boxesToLayout 把一组 PetBox 展开为「占用项(有宠物的格)」+「全量盒子元数据(含空盒)」。
// 任一盒子数值越界即判为误解析,返回 (nil,nil,false)。
func boxesToLayout(boxes []*pb.PetBox) (entries []BoxEntry, metas []BoxMeta, ok bool) {
	for _, bx := range boxes {
		if !boxValid(bx) {
			return nil, nil, false
		}
		name := string(bx.GetBoxName())
		mark := int32(bx.GetMarkType())
		metas = append(metas, BoxMeta{BoxID: bx.GetBoxId(), Name: name, Mark: mark, Lock: bx.GetLock()})
		for slot, g := range bx.GetPetGid() {
			if g != 0 {
				entries = append(entries, BoxEntry{Gid: g, BoxID: bx.GetBoxId(), Slot: int32(slot), BoxName: name, Mark: mark})
			}
		}
	}
	return entries, metas, true
}

// ParseBackpack 在 body 里找最完整的 PetBackpackInfo(登录/整理回包),展开为盒子占用项 +
// 全量盒子元数据。位置 = 宠物 gid 在 PetBox.pet_gid[] 中的下标(空格为 0,跳过)。取非零 gid 数
// 最多的候选以排除误解析;少于 5 只视为非真实背包,返回 (nil,nil)。
func ParseBackpack(body []byte) ([]BoxEntry, []BoxMeta) {
	var best *pb.PetBackpackInfo
	bestN := 0
	wire.Walk(body, func(v []byte) bool {
		var bp pb.PetBackpackInfo
		if proto.Unmarshal(v, &bp) != nil || len(bp.GetBoxes()) == 0 {
			return true
		}
		n := 0
		for _, bx := range bp.GetBoxes() {
			if !boxValid(bx) {
				n = -1 // 数值不合理,整体判为误解析
				break
			}
			for _, g := range bx.GetPetGid() {
				if g != 0 {
					n++
				}
			}
		}
		if n > bestN {
			bestN, best = n, &bp
		}
		return true
	})
	if best == nil || bestN < 5 {
		return nil, nil
	}
	entries, metas, _ := boxesToLayout(best.GetBoxes())
	return entries, metas
}

// ParseBoxSettingUp 解析 ZonePetBoxSettingUpRsp(整理/编辑排列,含改名/换位)的 body。
// 该回包不是 PetBackpackInfo,而是 { RetInfo ret_info=1; repeated PetBox boxes=2; }——盒子直接
// 挂在顶层 field2。盒子数 <5 或数值越界视为误解析返回 (nil,nil)。
// box_id 即新的展示位置,故整体替换即让盒内宠物随盒换位。
func ParseBoxSettingUp(body []byte) ([]BoxEntry, []BoxMeta) {
	var boxes []*pb.PetBox
	for _, v := range wire.Subs(body, 2) {
		var bx pb.PetBox
		if proto.Unmarshal(v, &bx) == nil && bx.BoxId != nil {
			boxes = append(boxes, &bx)
		}
	}
	if len(boxes) < 5 {
		return nil, nil
	}
	entries, metas, ok := boxesToLayout(boxes)
	if !ok {
		return nil, nil
	}
	return entries, metas
}

// ParseBoxUnlock 解析 ZonePetBoxUnlockRsp(解锁新盒)的 body,返回新盒的元数据(增量,单盒)。
// 结构: { RetInfo ret_info=1(含玩家数据); PetBox new_box=2 }。box_id 越界或未找到返回 nil。
func ParseBoxUnlock(body []byte) *BoxMeta {
	for _, v := range wire.Subs(body, 2) {
		var bx pb.PetBox
		if proto.Unmarshal(v, &bx) == nil && bx.BoxId != nil &&
			bx.GetBoxId() > 0 && bx.GetBoxId() <= 1000 {
			return &BoxMeta{BoxID: bx.GetBoxId(), Name: string(bx.GetBoxName()), Mark: int32(bx.GetMarkType()), Lock: bx.GetLock()}
		}
	}
	return nil
}

// ParseBoxSetMark 解析 ZonePetBoxSetMarkTypeRsp(设标记/改名)的 body,返回该盒更新后的元数据。
// 结构(非 PetBox): { RetInfo ret_info=1; uint32 box_id=2; WarehouseMarkType mark=3; bytes name=4;
// bool lock=5 }。仅 ret_info.result==0 且 box_id 合法才返回,否则 nil。
func ParseBoxSetMark(body []byte) *BoxMeta {
	if retResult(body) != 0 {
		return nil
	}
	boxID, _ := wire.Varint(body, 2)
	if boxID == 0 || boxID > 1000 {
		return nil
	}
	mark, _ := wire.Varint(body, 3)
	name, _ := wire.Bytes(body, 4)
	lock, _ := wire.Varint(body, 5)
	return &BoxMeta{BoxID: int32(boxID), Name: string(name), Mark: int32(mark), Lock: lock != 0}
}

// retResult 取顶层 ret_info(field1)的 result(其 field1);缺失返回 -1(不可与成功 0 混同)。
func retResult(body []byte) int64 {
	ri, ok := wire.Bytes(body, 1)
	if !ok {
		return -1
	}
	v, ok := wire.Varint(ri, 1)
	if !ok {
		return -1
	}
	return int64(v)
}

// ParseBoxMoves 从盒子操作回包(GoodsChangeItem.box_pet_change)抽取盒位增量变更。
// 每个 PetBoxPetChange = {pet_gid, is_in_team, id=box_id, pos(1 起)};只取非在队、gid 非 0、
// box/pos 在合理范围的盒位放置,转为 BoxEntry(Slot=pos-1)。空位变更(gid=0)被移走的宠物
// 必有对应的非 0 落位项,故跳过。
func ParseBoxMoves(body []byte) []BoxEntry {
	var out []BoxEntry
	wire.Walk(body, func(v []byte) bool {
		var c pb.PetBoxPetChange
		if proto.Unmarshal(v, &c) != nil || c.Id == nil || c.Pos == nil {
			return true
		}
		box, pos := c.GetId(), c.GetPos()
		if !c.GetIsInTeam() && c.GetPetGid() != 0 && box >= 1 && box <= 50 && pos >= 1 && pos <= 30 {
			out = append(out, BoxEntry{Gid: c.GetPetGid(), BoxID: box, Slot: pos - 1})
		}
		return true
	})
	return out
}
