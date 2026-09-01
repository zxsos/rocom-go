package trial

import (
	"github.com/whoisnian/rocom-capture/internal/wire"
	"google.golang.org/protobuf/encoding/protowire"
)

// 本文件解析战斗进入通知(0x1316),用于记录「试炼里遇到过哪些精灵」。
//
// 为什么在这里手写 wire 解析而不复用 internal/pb:
//   - internal/pb 只生成了 com_* 那几个公共类型,没有 ZoneBattleEnterNotify;
//   - 用 pbdesc + dynamicpb 动态解虽可行,但要加载 3 MB 描述符、且得自己试消息边界,
//     放进常驻管线(每场战斗都要跑一次)不划算;
//   - 我们只需要两个字段,路径已用 pbdesc 描述符确认过,手写 wire 解析足够且快。
//
// 字段路径(每个编号都从 pbdesc 的 ZoneBattleEnterNotify 描述符逐级核对过):
//
//	#6  init_info                                -> BattleInitInfo
//	      #6  enemy_team (repeated)              -> BattleRoleInfo
//	            #1  base                         -> BattleRoleBaseInfo
//	                  #25 npc_id
//	            #2  pets (repeated)              -> BattlePetInfo
//	                  #2  battle_common_pet_info -> PetData
//	                        #15 base_conf_id      ← 易错点
//	      #25 grass_trial_battle_info             -> GrassTrialBattleInfo
//	            #3  event_type                    -> 战斗类型,见 BattleType
//
// 别照字段名猜编号 —— base_conf_id 是 **15 不是 25**。PetData 的 #25 是
// success_catch_cnt(成功捕获次数):取错不会报错、也不会拿到空值,而是把
// 「捕获次数」当成 petbase id 存进遇见记录。每场战斗都会记下一个看着挺合理
// 的编号,图鉴里出现一堆不存在的精灵,排查时极难想到是这里。
// 核对编号请查 pbdesc 的描述符,别信字段名。
//
// 关于消息边界:AppBody 末尾带 tsf4g 尾标记。ScanFields 遇到无法解析的字节会停止,
// 故尾部残留不影响解析 —— 不需要像 pcapdump 那样先试探边界。
const (
	OpBattleEnterNotify = 0x1316 // ZONE_BATTLE_ENTER_NOTIFY(4886),战斗进入通知
)

// BattleType 是试炼里一场战斗的类型,取自 GrassTrialBattleInfo.event_type。
//
// 这是**协议自带的**分类,比查 wiki 或按 battle_conf_id 猜都可靠 ——
// 实测 17 场战斗:0 出现 11 次(普通)、1 三次(首领)、2 两次(NPC)、3 一次(最终 BOSS),
// 与节点推进的层结构完全对应。
type BattleType uint32

const (
	BattleNormal  BattleType = 0  // 普通池(第 1/2/3/5 层)
	BattleBoss    BattleType = 1  // 首领池(第 4 层)
	BattleNPC     BattleType = 2  // NPC 阵容(第 7 层,第一/二章)
	BattleFinal   BattleType = 3  // 最终 BOSS(第 7 层,第三章 —— 敌方式斗酷猫这类,非普通 NPC)
	BattleUnknown BattleType = 99 // 没有 grass_trial_battle_info(可能不是试炼战斗)
)

// IsTrial 判断这场战斗是否属于草系试炼 —— 以「带 grass_trial_battle_info」为准。
//
// 这是判定归属的**唯一可靠依据**:试炼外也有战斗(野外/PVP),而 battle_conf_id
// 无法区分(普通池战斗的 id 段与野外战斗重叠)。早先打算用「有活跃试炼」这个
// 时间窗来判定,但那会在战斗拖过退出试炼时误记 —— 用这个字段则不会。
func (t BattleType) IsTrial() bool { return t != BattleUnknown }

// Label 返回中文名,供前端直接展示。
func (t BattleType) Label() string {
	switch t {
	case BattleNormal:
		return "普通"
	case BattleBoss:
		return "首领"
	case BattleNPC:
		return "NPC"
	case BattleFinal:
		return "最终BOSS"
	}
	return ""
}

// BattleEncounter 是一场战斗里「遇到了谁」。
type BattleEncounter struct {
	Type      BattleType // 战斗类型(试炼专属字段)
	NPCObject uint32     // 对手的 npc_id;普通/首领战为 0
	PetBases  []uint32   // 敌方精灵的 petbase id(实测每场 1 只)
}

// ParseBattleEnter 解析 0x1316。返回 nil 表示解不出来。
//
// 只有带 grass_trial_battle_info 的才算试炼战斗,此时 Type.IsTrial() 为真;
// 其余战斗(野外、PVP 等)也会返回非 nil 但 Type 为 BattleUnknown,
// 调用方据此丢弃 —— 这样「试炼外战斗不会污染遇见记录」。
func ParseBattleEnter(b []byte) *BattleEncounter {
	if len(b) == 0 {
		return nil
	}
	out := &BattleEncounter{Type: BattleUnknown}
	wire.ScanFields(b, func(num protowire.Number, typ protowire.Type, val []byte, _ uint64) {
		if num == 6 && typ == protowire.BytesType { // init_info
			parseInitInfo(val, out)
		}
	})
	return out
}

func parseInitInfo(b []byte, out *BattleEncounter) {
	wire.ScanFields(b, func(num protowire.Number, typ protowire.Type, val []byte, _ uint64) {
		switch {
		case num == 25 && typ == protowire.BytesType: // grass_trial_battle_info
			wire.ScanFields(val, func(n2 protowire.Number, t2 protowire.Type, _ []byte, v2 uint64) {
				if n2 == 3 && t2 == protowire.VarintType { // event_type
					out.Type = BattleType(v2)
				}
			})
		case num == 6 && typ == protowire.BytesType: // enemy_team (repeated)
			// 直接解当前这个元素;不要在这里再调 Subs 扫全量,那会对同一个
			// 元素重复解析(repeated 字段的每个元素都会触发一次回调)。
			parseEnemyTeam(val, out)
		}
	})
}

func parseEnemyTeam(b []byte, out *BattleEncounter) {
	wire.ScanFields(b, func(num protowire.Number, typ protowire.Type, val []byte, _ uint64) {
		if num == 1 && typ == protowire.BytesType { // base
			if v, ok := wire.Varint(val, 25); ok { // npc_id
				out.NPCObject = uint32(v)
			}
		}
	})
	// pets 是 repeated,每个元素里的 battle_common_pet_info 才是宠物本体
	for _, pet := range wire.Subs(b, 2) {
		info := wire.SubMsg(pet, 2) // battle_common_pet_info
		if info == nil {
			continue
		}
		if v, ok := wire.Varint(info, 15); ok { // base_conf_id(15! 见文件头注释)
			out.PetBases = append(out.PetBases, uint32(v))
		}
	}
	dedup(&out.PetBases)
}

// dedup 去重保序 —— 同一只宠物可能在 pets 里以多个字段块出现。
func dedup(ids *[]uint32) {
	seen := map[uint32]bool{}
	out := (*ids)[:0]
	for _, id := range *ids {
		if id == 0 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	*ids = out
}
