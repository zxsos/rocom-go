package pet

import (
	"google.golang.org/protobuf/encoding/protowire"

	"github.com/whoisnian/rocom-capture/internal/wire"
)

// ParseLoginAccount 从 ZoneLoginRsp(opcode 0x0102)取玩家 user_id 与昵称。
// body 结构(实测,见 docs/architecture.md「多账号隔离」):{ #1: RetInfo, #2: LoginData{ #1: base{...} } },
// base 内 #1=user_id(varint)、#2=openid(str)、#3=nickname(bytes)。user_id 全局唯一、
// 跨设备/跨服稳定,作账号身份键;昵称仅供展示(可能为占位名如「你的名字」)。
func ParseLoginAccount(body []byte) (userID uint64, name string, ok bool) {
	data, ok2 := wire.Bytes(body, 2) // LoginData
	if !ok2 {
		return 0, "", false
	}
	base, ok1 := wire.Bytes(data, 1) // LoginData.#1(玩家基础信息)
	if !ok1 {
		return 0, "", false
	}
	id, okID := wire.Varint(base, 1)
	if !okID || id == 0 {
		return 0, "", false
	}
	if nb, ok3 := wire.Bytes(base, 3); ok3 {
		name = string(nb)
	}
	return id, name, true
}

// MedalOwn 是一只宠物拥有的一枚奖牌(来自登录数据的 PetMedalInfo)。
type MedalOwn struct {
	Gid     uint32
	MedalID uint32
}

// ParsePetMedals 从登录数据(PlayerSvrDataInfo.pet_medal_info)递归解析每只宠物拥有的奖牌。
// PetMedalInfo:#1 medal_conf_id / #2 medal_type / #3 owner 组[],组内 #2 记录里宠物 gid = #8??#6??#2。
// 注:线上 wire 格式与 all.pb 的 PetMedalOwnerInfo 定义不一致(版本偏移),故纯按 wire 经验解码。
func ParsePetMedals(body []byte) []MedalOwn {
	var out []MedalOwn
	if tryMedalInfo(body, &out) {
		return out
	}
	wire.Walk(body, func(v []byte) bool { return !tryMedalInfo(v, &out) })
	return out
}

// tryMedalInfo 尝试把 b 识别为 PetMedalInfo(#1 在奖牌区间 + 有 medal_type + 有 owner 组);
// 命中则提取各 owner 的宠物 gid 并返回 true(调用方据此不再深入)。
func tryMedalInfo(b []byte, out *[]MedalOwn) bool {
	mc, ok := wire.Varint(b, 1)
	if !ok || mc < 1000 || mc >= 2000 {
		return false
	}
	if _, hasType := wire.Varint(b, 2); !hasType {
		return false
	}
	groups := wire.Subs(b, 3)
	if len(groups) == 0 {
		return false
	}
	for _, g := range groups {
		for _, rec := range wire.Subs(g, 2) {
			if gid := recPetGid(rec); gid != 0 {
				*out = append(*out, MedalOwn{Gid: gid, MedalID: uint32(mc)})
			}
		}
	}
	return true
}

// recPetGid 从奖牌记录里取宠物 gid(优先 obtain_pet_gid #8,退 #6,再退 #2)。
func recPetGid(rec []byte) uint32 {
	for _, f := range []protowire.Number{8, 6, 2} {
		if v, ok := wire.Varint(rec, f); ok {
			return uint32(v)
		}
	}
	return 0
}
