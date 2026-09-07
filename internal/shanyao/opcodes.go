// Package shanyao 识别「当前这一局 PVP」(隐藏模块 闪耀大赛):解析战斗相关的服务端广播,
// 维护「双方阵容 + 技能槽 + 实时血量」的状态机,供隐藏页 #/shanyao 展示。
//
// 数据来源(字段号均为实测,核对方式见 parse.go 的文件头注释):
//
//	0x1316 ZONE_BATTLE_ENTER_NOTIFY     进战:双方队伍(我方全量,对手只给首发一只)
//	0x131a ZONE_BATTLE_ROUND_START      回合开始:双方**在场**宠物的完整信息 + 回合号
//	0x1324 ZONE_BATTLE_PERFORM_START    演出:宠物 HP 变化(sync_data.pet_sync_info.hp_result)
//	0x132c ZONE_BATTLE_FINISH_NOTIFY    结算:最终 HP、状态与胜负
//
// 血量来源实测结论(2026-09-07 的 PVP 资格赛 pcap,单局 12 分钟):
//   - **当前血量只在 0x1324 里**,形如 perform_cmd(1).perform_info(2).sync_data(12)
//     .pet_sync_info(2){pet_id(1), hp_result(3)};给的是**结果值**不是增量,
//     hp_result=0 表示倒下(不能当「没给」丢掉,故 HPUpdate 带 HasHP)。
//   - 0x131a 的 battle_attr[1] 是**最大** HP(进战与回合包里恒为满值,不随伤害变化),
//     拿它当当前血量会得到一条永远满格的血条。
//   - 0x1322 CMD_SYNC_NOTIFY 只是「谁放了什么技能」的回显(player_uin +
//     req.cast_skill{skill_id, caster_pet_id}),**不带血量**;97 条里一条都没有。
//   - 0x130c CMD_PUSHBACK_RSP 也带 sync_data.pet_sync_info{pet_id, hp_result},
//     但它是**自己那条指令**的回包(只有施法宠),覆盖不全;以 0x1324 为准。
//
// 为什么不使用生成代码:internal/pb 只收录了 com_* 系列(背包/队伍/技能),不含
// ZoneBattle* 消息;补生成要跑 scripts/gen_proto.py(依赖解包出的 all.pb)。
// 本项目其它战斗解析(internal/scene/battle.go、scene/catch.go、internal/trial)
// 同样是在 wire 层按实测字段号取值,此处保持一致。
//
// 本包不依赖 gamedata:只产出 id 与原始数值,名称/图片由调用方(管线)查库补全,
// 这样解析层可以脱离 embed 数据单测。
package shanyao

import "github.com/whoisnian/rocom-capture/internal/scene"

// opcode 常量。
//
// 0x1316 / 0x132c 复用 internal/scene 的声明:同一批消息已在那里被「是否在对战中」
// 与「野怪是否被捉走」两处使用,数值在两处各写一份迟早走偏(见 scene/battle.go
// 与 scene/catch.go 的说明)。
const (
	OpBattleEnterNotify        = scene.OpBattleEnterNotify  // 0x1316 ZONE_BATTLE_ENTER_NOTIFY
	OpBattleRoundStartNotify   = 0x131a                     // ZONE_BATTLE_ROUND_START_NOTIFY
	OpBattlePerformStartNotify = 0x1324                     // ZONE_BATTLE_PERFORM_START_NOTIFY
	OpBattleFinishNotify       = scene.OpBattleFinishNotify // 0x132c ZONE_BATTLE_FINISH_NOTIFY
)

// 怪物结算状态(BATTLE_MONSTER_RESULT_TYPE)里表示「已倒下」的两个值。
// 其余(ALIVE=3 存活、RUNAWAY=2 逃跑)不算倒下 —— 逃跑的宠在世界上还在。
const (
	MonsterDefeated = 0 // BATTLE_MONSTER_DEFEATED
	MonsterCatched  = 1 // BATTLE_MONSTER_CATCHED
)

// 炫彩判据:mutation_type 的 bit 3。
//
// ⚠️ 必须写成 `mutation_type & 8` —— 早期版本曾用过「glass_type != 0」判断,
// 普通炫彩中有一部分是 GT_NULL 但有 mutation 位,那会漏判;反之隐藏炫彩也可能
// 不带 mutation 位。两处的权威口径见 docs/data.md 与 internal/pet/model.go 的 Colorful。
const MutationShinyBit = 8
