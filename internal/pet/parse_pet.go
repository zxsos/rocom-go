package pet

import (
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"

	"github.com/whoisnian/rocom-capture/internal/pb"
	"github.com/whoisnian/rocom-capture/internal/wire"
)

// hasCJK 判断字节串是否含中日韩统一表意文字(宠物名为中文)。
func hasCJK(b []byte) bool {
	for _, r := range string(b) {
		if r >= 0x4E00 && r <= 0x9FFF {
			return true
		}
	}
	return false
}

// FindNewPet 在响应 body 中递归查找新宠物 PetData。
// 孵蛋/捕捉获得的宠物作为奖励嵌套在 ret_info.goods_reward.rewards[].pet 里,逐层路径随消息
// 而异,故递归尝试把每个 LEN 子字段反序列化为 PetData,以 gid/conf_id/name 均有效作为命中判据。
func FindNewPet(body []byte) *pb.PetData {
	var found *pb.PetData
	wire.Walk(body, func(v []byte) bool {
		if found != nil {
			return false
		}
		var pd pb.PetData
		if proto.Unmarshal(v, &pd) == nil &&
			pd.GetGid() > 0 && pd.GetConfId() > 1000 && hasCJK(pd.GetName()) {
			found = &pd
			return false
		}
		return true
	})
	return found
}

// ParseFreeRsp 解析 ZonePetFreeRsp(放生)的 body,返回被放生的 gid 列表。
// 消息结构: { RetInfo ret_info=1; repeated uint32 pet_gid=2; }(packed 与非 packed 都兼容)。
func ParseFreeRsp(body []byte) []uint32 {
	var gids []uint32
	wire.ScanFields(body, func(num protowire.Number, typ protowire.Type, val []byte, v uint64) {
		if num != 2 {
			return
		}
		switch typ {
		case protowire.VarintType:
			gids = append(gids, uint32(v))
		case protowire.BytesType:
			for _, x := range wire.PackedVarints(val) {
				gids = append(gids, uint32(x))
			}
		}
	})
	return gids
}

// ParseTogetherCatchGiftRsp 解析 ZoneTogetherCatchPetForGiftingRsp(赠送共同捕捉的宠物)的 body,
// 返回被赠送出的 pet_gid(0 表示非赠送执行回包)。捕捉与赠送是相互独立的事件:先前捕捉照常入库,
// 之后玩家开盒子手动选择赠送才走本回包,应据此从自己的库中移除。
// 该 opcode 有两种回包且都在顶层带 pet_gid(field3):一种是内嵌完整 PetData 的宠物详情(赠送前预览/
// 同步,不代表已送出),一种是紧凑的执行确认 ack({ RetInfo ret_info=1; uint32 _=2; uint32 pet_gid=3; })。
// 只认后者:内嵌 PetData 的直接返回 0,避免预览误记 + 两种回包重复记;ack 里 result==0 且 gid>0 才算成功。
func ParseTogetherCatchGiftRsp(body []byte) uint32 {
	if FindNewPet(body) != nil { // 宠物详情回包(预览/同步),非执行确认
		return 0
	}
	if retResult(body) != 0 {
		return 0
	}
	gid, _ := wire.Varint(body, 3)
	return uint32(gid)
}

// PageResult 是一页宠物列表的解析结果。
type PageResult struct {
	TotalPage uint32
	ReqPage   uint32
	PageNum   uint32 // 实为每页容量(实测恒 50),不是页序,不能用作累积接续判据
	Pets      []*pb.PetData
}

// ParsePetListRsp 解析 ZoneGetPetInfoByPageRsp(opcode 0x1346)的 protobuf body。
// 只取需要的字段:total_page=2, req_page=3, pet_info=4(PetDataInfoList), page_num=5。
func ParsePetListRsp(body []byte) *PageResult {
	res := &PageResult{}
	wire.ScanFields(body, func(num protowire.Number, typ protowire.Type, val []byte, v uint64) {
		switch {
		case num == 2 && typ == protowire.VarintType:
			res.TotalPage = uint32(v)
		case num == 3 && typ == protowire.VarintType:
			res.ReqPage = uint32(v)
		case num == 5 && typ == protowire.VarintType:
			res.PageNum = uint32(v)
		case num == 4 && typ == protowire.BytesType:
			var list pb.PetDataInfoList
			if proto.Unmarshal(val, &list) == nil {
				res.Pets = append(res.Pets, list.PetData...)
			}
		}
	})
	return res
}
