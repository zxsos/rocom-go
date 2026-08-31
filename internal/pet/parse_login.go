package pet

import (
	"bytes"

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

// ParseLoginCoins 从 ZoneLoginRsp(opcode 0x0102)取玩家洛克贝数量。
// 洛克贝位于 player_info.brief_info.vitem_info 的 field3(vitem_list,裸 varint 数组,
// 下标 1=洛克贝,下标 0 恒 0,实测 19517164 与游戏内一致)。vitem_info 的 field4 是
// liabilities_num 槽数组(槽数随版本变化,实测 2026-08 为 81 槽),故用
// 「field3 varint 60~120 个 + field4 varint ≥3 个」的特征在 body 中唯一定位。
// 注意:真实 wire 与 all.pb 描述符存在版本偏移(字段号 +2),pcapdump 按描述符
// 解码显示 vitem_list 在 field1/liabilities_num 在 field2 是假象,不能直接照搬。
func ParseLoginCoins(body []byte) (coins int64, ok bool) {
	wire.Walk(body, func(v []byte) bool {
		if ok {
			return false
		}
		list := wire.FieldVarints(v, 3)
		if len(list) < 60 || len(list) > 120 {
			return true // 不是 vitem_info,继续下钻
		}
		if n := len(wire.FieldVarints(v, 4)); n < 3 {
			return true
		}
		if len(list) > 1 && list[1] < 1<<50 {
			coins, ok = int64(list[1]), true
		}
		return false
	})
	return coins, ok
}

// avatarURLPrefix / avatarMinLen / avatarMaxLen 是识别头像 URL 的特征。
//
// 只认 https:// 开头:平台头像一律 https,顺带挡掉 javascript: 之类的伪协议
// (URL 会经 /api/accounts 下发到前端,前端可能直接塞进 <img src>)。
const (
	avatarURLPrefix = "https://"
	avatarMinLen    = 32
	avatarMaxLen    = 512
)

// ParseLoginAvatar 从 ZoneLoginRsp(0x0102)取玩家平台头像 URL。
//
// 目标字段是 player_info.brief_info.additional_data.plat_avatar_url(实测微信直链
// https://thirdwx.qlogo.cn/mmopen/.../132,末段换成 0/46/64/96/132 可取不同尺寸)。
// 与 ParseLoginCoins 同理:**真实 wire 与 all.pb 描述符存在版本偏移,字段号不可信**,
// 故不按字段号下钻,而在全包内按「以 https:// 开头、长度适中、纯可打印 ASCII 的短串」
// 唯一定位 —— 实测整条登录回包只有这一处 URL(pcapdump 转储核对,见 0x0102.md)。
// 将来若包里出现第二个 https URL(如公告 CDN),需在这里加域名白名单收紧。
//
// 取不到(游客号/未绑定平台/版本变更)返回 ok=false,调用方据此**保留旧头像**而非清空。
func ParseLoginAvatar(body []byte) (url string, ok bool) {
	wire.Walk(body, func(v []byte) bool {
		if ok || len(v) < avatarMinLen || len(v) > avatarMaxLen {
			return true
		}
		if !bytes.HasPrefix(v, []byte(avatarURLPrefix)) {
			return true
		}
		for _, c := range v {
			// 空白/控制字符/非 ASCII:URL 里不该出现(空格会编码成 %20)。出现即说明这是
			// 误命中的二进制 —— 拼进 <img src> 会破坏属性、写进日志会污染行结构。
			if c <= 0x20 || c >= 0x7f {
				return true
			}
		}
		url, ok = string(v), true
		return false // 已命中,不必再下钻该值内部
	})
	return url, ok
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
