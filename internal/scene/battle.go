package scene

import (
	"google.golang.org/protobuf/encoding/protowire"
)

// 玩家「是否正在对战」的判定(供实时地图在玩家头顶挂一个图标,见 docs/data.md 3.7)。
//
// 只需一个布尔,故取最容易判的两条:
//
//	进入:0x1316 ZONE_BATTLE_ENTER_NOTIFY(s2c) —— 服务器广播进战,带 battle_id
//	结束:0x132c ZONE_BATTLE_FINISH_NOTIFY(s2c) —— 结算(见 catch.go,那里同一条
//	      消息用来判野怪是否被捉走/打死,两处用途不同但消息是同一条)
//
// 0x132a(ZONE_BATTLE_ROLE_LEAVE_NOTIFY)与 0x132d(ZONE_BATTLE_FORCE_FINISH_NOTIFY)
// 也能表示结束,但实测六份 pcap 里 0x132c 每次战斗都有、且必带 battle_id,用它足够;
// 兜底交给 pipeline 的超时(漏包时图标不会永久挂着)。
//
// ⚠️ 这里**不解析 battle_mode / 结果**:前端只要「在不在打」这个状态,多解析一个
// 字段就多一处要跟版本的地方(且 battle_mode 的枚举含义会随版本加值)。
//
// OpBattleFinishNotify(0x132c)定义在 catch.go —— 那条消息先被野怪捕捉用上,
// 这里只是复用它;常量不重复声明,以免两处数值走偏。
const OpBattleEnterNotify = 0x1316 // ZONE_BATTLE_ENTER_NOTIFY, s2c,进入战斗

// ParseBattleID 从 s2c 战斗通知(0x1316 进战 / 0x132c 结算)取 battle_id。
//
// 两条消息的 field 1 都是 battle_id,故共用一份解析。只需 id 用于进/出配对,
// 故不解析其余字段(参战角色、宠物、阵营等)。返回 0 表示没解出。
//
// ⚠️ battle_id 是 uint64,与 catch.go 里的 actor_id 不是一回事,别混用。
func ParseBattleID(body []byte) uint64 {
	for len(body) > 0 {
		num, typ, n := protowire.ConsumeTag(body)
		if n < 0 {
			return 0
		}
		body = body[n:]
		if num == 1 && typ == protowire.VarintType {
			v, n := protowire.ConsumeVarint(body)
			if n < 0 {
				return 0
			}
			return v
		}
		var l int
		switch typ {
		case protowire.VarintType:
			_, l = protowire.ConsumeVarint(body)
		case protowire.Fixed32Type:
			_, l = protowire.ConsumeFixed32(body)
		case protowire.Fixed64Type:
			_, l = protowire.ConsumeFixed64(body)
		case protowire.BytesType:
			_, l = protowire.ConsumeBytes(body)
		default:
			return 0
		}
		if l < 0 {
			return 0
		}
		body = body[l:]
	}
	return 0
}
