package scene

import (
	"google.golang.org/protobuf/encoding/protowire"

	"github.com/whoisnian/rocom-capture/internal/wire"
)

// 花种(花灵)活动 BOSS 信息(见 docs/data.md 花种页)。
//
// 花种界面依赖 s2c 0x0375(883) ZONE_SCENE_QUERY_BOSS_NPC_INFO_RSP:客户端打开花种面板时
// 服务器一次性下发全部 BOSS 分组,其中 flower_npcs 是花种(花灵)活动 BOSS——每颗花种对应一只
// 守护宠物(battle_petbase_id),带星级/血脉/活动结束时间;另有 spec_flower_seed_id 的非零的
// 3 只 7 星特殊花种。world_leader_npcs(世界 BOSS)与 legendary_npcs(传说 NPC)与花种无关,不解析。
const (
	OpQueryBossNpcInfoRsp           = 0x0375 // ZONE_SCENE_QUERY_BOSS_NPC_INFO_RSP(883), s2c,花种等 BOSS 分组信息
	OpTeamBattleInfoQueryRsp        = 0x0338 // ZONE_SCENE_TEAM_BATTLE_INFO_QUERY_RSP(824), s2c,单只花种详情(点击地图花种触发)
	OpSelectTeamBattleFlowerSeedReq = 0x0846 // ZONE_SELECT_TEAM_BATTLE_FLOWER_SEED_BOSS_REQ(2118), c2s,选中某朵花种进入战斗
)

// ParseSelectFlowerSeedBossReq 解析 c2s ZoneSelectTeamBattleFlowerSeedBossReq(0x0846):
// 取 npc_logic_id(field 2,uint64)。玩家在花种页点某朵花进战斗时发出,
// 用于把「最近挑战的花种」关联到捕捉成功(0x0160 catch_way=4)后的清理。
func ParseSelectFlowerSeedBossReq(body []byte) uint64 {
	var logicID uint64
	wire.ScanFields(body, func(num protowire.Number, typ protowire.Type, _ []byte, v uint64) {
		if num == 2 && typ == protowire.VarintType {
			logicID = v
		}
	})
	return logicID
}

// BossNpcInfo 是一只花种(BOSS)的展示字段。字段号据 all.pb 描述符(见 scripts/pbdesc.py):
// BossNpcInfos.boss_npcs(1,repeated BossNpcInfo) 内:
//
//	npc_cfg_id(1) / star(2) / blood(3) / battle_petbase_id(5) / npc_logic_id(6,每只花种唯一) /
//	npc_obj_id(7) / end_timestamp(10) / spec_flower_seed_id(11,非零=特殊花种) /
//	activity_id(12) / select_flower_owner_id(24)
type BossNpcInfo struct {
	NpcCfgID    uint32 // 花种 NPC 配置 id(20129-20144 普通 / 700002-700004 特殊)
	Star        uint32 // 星级(普通 5 / 特殊 7)
	Blood       uint32 // 血脉 id(PET_BLOOD_CONF.blood,1-24;同种花种可刷不同血脉)
	NpcLogicID  uint64 // NPC 逻辑 id(每只花种唯一;0x0338 详情与客户端请求都用它定位)
	PetBaseID   uint32 // 守护宠物 petbase id(取名称/头像)
	EndTs       uint64 // 活动结束时间戳(Unix 秒)
	SpecSeedID  uint32 // 特殊花种种子 id(0=普通花种)
	ActivityID  uint32 // 活动 id(仅特殊花种带)
	OwnerUserID uint64 // 已选花种的玩家 user_id(0=未被选)
}

// ParseBossNpcInfoRsp 从 s2c ZoneSceneQueryBossNpcInfoRsp(0x0375)取 flower_npcs(2) 分组:
// flower_npcs(2,BossNpcInfos) → boss_npcs(1,repeated BossNpcInfo)。
// 只取花种所需的字段;world_leader_npcs/legendary_npcs 与花种无关,跳过。
func ParseBossNpcInfoRsp(body []byte) []BossNpcInfo {
	flowers := wire.SubMsg(body, 2) // flower_npcs
	if flowers == nil {
		return nil
	}
	var out []BossNpcInfo
	for _, b := range wire.Subs(flowers, 1) { // boss_npcs
		var n BossNpcInfo
		wire.ScanFields(b, func(num protowire.Number, typ protowire.Type, _ []byte, v uint64) {
			if typ != protowire.VarintType {
				return
			}
			switch num {
			case 1:
				n.NpcCfgID = uint32(v)
			case 2:
				n.Star = uint32(v)
			case 3:
				n.Blood = uint32(v)
			case 5:
				n.PetBaseID = uint32(v)
			case 6:
				n.NpcLogicID = v
			case 10:
				n.EndTs = v
			case 11:
				n.SpecSeedID = uint32(v)
			case 12:
				n.ActivityID = uint32(v)
			case 24:
				n.OwnerUserID = v
			}
			})
			if n.NpcCfgID != 0 {
			out = append(out, n)
			}
	}
	return out
}

// TeamBattleInfo 是一只花种(BOSS)的详情字段,来自 0x0338 team_battle_info(2)。
// 与 0x0375 面板整组下发互补:多出等级/炫彩/绑定宠物/奖牌等详情,且字段号与 BossNpcInfo 不同
// (end_timestamp 27、spec_flower_seed_id 25、activity_id 26、select_flower_owner_id 36),不能复用。
// 字段号据 all.pb 描述符(见 scripts/pbdesc.py):TeamBattleInfo 内:
//
//	npc_cfg_id(1) / star(2) / blood(3) / randed_battle_npc_glass(4,bool) / battle_petbase_id(5) /
//	npc_logic_id(6) / npc_obj_id(7) / battle_npc_lv(10) / catch_vitem_quantity(24) /
//	spec_flower_seed_id(25) / activity_id(26) / end_timestamp(27) / bind_pet_gid(30) /
//	battle_npc_glass_info(32,GlassInfo) / bind_petbase_id(33) / bind_evolution_id(34) /
//	select_flower_owner_id(36) / medal_id(37)
type TeamBattleInfo struct {
	NpcCfgID    uint32 // 花种 NPC 配置 id(20129-20144 普通 / 700002-700004 特殊)
	Star        uint32 // 星级(普通 5 / 特殊 7)
	Blood       uint32 // 血脉 id(PET_BLOOD_CONF.blood,1-24;同种花种可刷不同血脉)
	PetBaseID   uint32 // 守护宠物 petbase id(取名称/头像)
	NpcLogicID  uint64 // NPC 逻辑 id(客户端请求定位用,每只花种唯一)
	NpcObjID    uint64 // 场景对象 id
	Lv          uint32 // 等级
	GlassType   int32  // 炫彩类型(0=GT_NULL 无 / 1=普通 / 2=隐藏;判据 !=0,见 gamedata.DB.GlassDesc)
	GlassValue  int32  // 炫彩值(普通=打包色号 (粒子id<<20)|配色id;隐藏=HIDDEN_GLASS_CONF.id)
	SpecSeedID  uint32 // 特殊花种种子 id(0=普通花种)
	ActivityID  uint32 // 活动 id
	EndTs       uint64 // 活动结束时间戳(Unix 秒)
	OwnerUserID uint64 // 已选花种的玩家 user_id(0=未被选)
	BindPetGID  uint64 // 绑定宠物 gid(背包实体;0=普通花种无绑定)
	BindBaseID  uint32 // 绑定宠物 petbase id(0=无)
	BindEvoID   uint32 // 绑定宠物进化阶段 id
	MedalID     uint32 // 绑定宠物佩戴的奖牌 id(0=无)
}

// ParseTeamBattleInfoQueryRsp 解析 s2c ZoneSceneTeamBattleInfoQueryRsp(0x0338):
// 外层取 team_battle_info(2);glass_info 是子消息,单独解出炫彩类型/值。
func ParseTeamBattleInfoQueryRsp(body []byte) (TeamBattleInfo, bool) {
	var n TeamBattleInfo
	info := wire.SubMsg(body, 2) // team_battle_info
	if info == nil {
		return n, false
	}
	wire.ScanFields(info, func(num protowire.Number, typ protowire.Type, val []byte, v uint64) {
		if num == 32 && typ == protowire.BytesType { // battle_npc_glass_info(GlassInfo)
			wire.ScanFields(val, func(fn protowire.Number, t protowire.Type, _ []byte, gv uint64) {
				if t != protowire.VarintType {
					return
				}
				switch fn {
				case 1:
					n.GlassType = int32(gv)
				case 2:
					n.GlassValue = int32(gv)
				}
			})
			return
		}
		if typ != protowire.VarintType {
			return
		}
		switch num {
		case 1:
			n.NpcCfgID = uint32(v)
		case 2:
			n.Star = uint32(v)
		case 3:
			n.Blood = uint32(v)
		case 5:
			n.PetBaseID = uint32(v)
		case 6:
			n.NpcLogicID = v
		case 7:
			n.NpcObjID = v
		case 10:
			n.Lv = uint32(v)
		case 25:
			n.SpecSeedID = uint32(v)
		case 26:
			n.ActivityID = uint32(v)
		case 27:
			n.EndTs = v
		case 30:
			n.BindPetGID = v
		case 33:
			n.BindBaseID = uint32(v)
		case 34:
			n.BindEvoID = uint32(v)
		case 36:
			n.OwnerUserID = v
		case 37:
			n.MedalID = uint32(v)
		}
	})
	return n, n.NpcCfgID != 0
}
