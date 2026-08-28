package pet

import (
	"google.golang.org/protobuf/encoding/protowire"

	"github.com/whoisnian/rocom-capture/internal/wire"
)

// GlassCollect 是图鉴记录里收集到的一种炫彩变体(普通/隐藏)。
type GlassCollect struct {
	PetBaseID  uint32 // 品种(pet_base_id)
	GlassType  int32  // 1=普通炫彩 GT_COMMON, 2=隐藏炫彩 GT_HIDDEN
	GlassValue int32  // 普通: (粒子id<<20)|配色id; 隐藏: 1/2/3 赛季、1000 黑白
}

// ParseHandbookGlasses 从登录数据(ZoneLoginRsp 的 PlayerPetInfo.pet_handbook)递归解析全部
// 图鉴炫彩收集。handbook 结构(实测,与 all.pb 一致):PetHandbook.#2 record_collection 组 →
// #1 handbook_id / #2 record 组 → HandbookRecord.#1 pet_base_id / #20 glass_infos /
// #21 shine_glass_infos;GlassInfo 为 #1 glass_type / #2 glass_value。
// 识别特征:有 pet_base_id(1000 起)且含 #20/#21 子消息组(GlassInfo),其余消息误判概率极低。
func ParseHandbookGlasses(body []byte) []GlassCollect {
	var out []GlassCollect
	wire.Walk(body, func(v []byte) bool { return !tryHandbookRecord(v, &out) })
	return out
}

// tryHandbookRecord 尝试把 b 识别为图鉴记录(pet_base_id 合理区间 + 有玻璃列表组);
// 命中则收集其全部炫彩变体并返回 true(调用方据此不再深入)。
func tryHandbookRecord(b []byte, out *[]GlassCollect) bool {
	base, ok := wire.Varint(b, 1)
	if !ok || base < 1000 || base >= 100000 {
		return false
	}
	found := false
	for _, f := range []protowire.Number{20, 21} { // glass_infos / shine_glass_infos
		for _, g := range wire.Subs(b, f) {
			gt, ok1 := wire.Varint(g, 1)
			gv, ok2 := wire.Varint(g, 2)
			if !ok1 || !ok2 {
				continue
			}
			if gt == 0 { // GT_NULL
				continue
			}
			*out = append(*out, GlassCollect{PetBaseID: uint32(base), GlassType: int32(gt), GlassValue: int32(gv)})
			found = true
		}
	}
	return found
}
