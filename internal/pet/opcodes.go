// Package pet 负责把宠物相关的应用层消息解析为业务模型，并检测宠物增减事件。
package pet

// 宠物相关 opcode(来自 ZoneSvrCmd enum,见 names.json opcodes,源自游戏描述符 all.pb)。
const (
	OpGetPetInfoByPageRsp    = 0x1346 // ZONE_GET_PET_INFO_BY_PAGE_RSP(4934), 分页宠物列表
	OpPetFreeRsp             = 0x01c5 // ZONE_PET_FREE_RSP(453), 放生(下行含 pet_gid 列表)
	OpTogetherCatchGiftRsp   = 0x1808 // ZONE_TOGETHER_CATCH_PET_FOR_GIFTING_RSP(6152), 赠送共同捕捉的宠物给好友(执行回包含 gid)
	OpCrackEggRsp            = 0x030c // ZONE_CRACK_EGG_RSP(780), 孵蛋(新宠物嵌在 goods_reward)
	OpPetCatchRsp            = 0x1983 // ZONE_SCENE_THROW_CATCH_FINISH_RSP(6531), 战斗外捕捉(赛季球/高级球)
	OpGoodsRewardNotify      = 0x0243 // ZONE_GOODS_REWARD_NOTIFY, 奖励通知(普通战斗内捕捉等新宠物)
	OpPlayerSyncNotify       = 0x0160 // ZONE_PLAYER_SYNC_NOTIFY, 玩家数据同步(花种战斗内捕捉走此通道)
	OpBattleFinishNotify     = 0x132c // ZONE_BATTLE_FINISH_NOTIFY(4908), 战斗结束通知(传说精灵战后捕捉,catch_way=5,唯一下发通道)
	OpLoginRsp               = 0x0102 // ZONE_LOGIN_RSP(258), 登录数据(含完整背包 PetBackpackInfo)
	OpPetBoxChangePetRsp     = 0x1888 // ZONE_PET_BOX_CHANGE_PET_RSP(6280), 盒位移动回包(box_pet_change 增量)
	OpPetBoxSettingUpRsp     = 0x1891 // ZONE_PET_BOX_SETTING_UP_RSP(6289), 整理/编辑排列回包(改名/换位,全量 repeated PetBox)
	OpPetBoxUnlockRsp        = 0x1883 // ZONE_PET_BOX_UNLOCK_RSP(6275), 解锁新盒回包(field2=新 PetBox 增量)
	OpPetBoxSetMarkTypeRsp   = 0x1893 // ZONE_PET_BOX_SET_MARK_TYPE_RSP(6291), 设标记/改名回包(单盒元数据增量)
	OpPetMedalCommonRsp      = 0x141e // ZONE_PET_MEDAL_COMMON_RSP(5150), 换牌等回包(含更新后 PetData)
	OpPetEvoluteRsp          = 0x01ae // ZONE_PET_EVOLUTE_RSP(430), 进化回包(含进化后完整 PetData,base_conf_id 已换形态)
	OpUpdatePetCollectTagRsp = 0x0403 // ZONE_UPDATE_PET_COLLECT_TAG_RSP(1027), 伙伴标记增删改回包(含更新后完整 PetData)
)

// 盒子操作 opcode 区间(ZoneSvrCmd 十进制 6272-6292,如 TIDY_RSP/SETTING_UP_RSP 携带全量盒子)。
const boxOpcodeLo, boxOpcodeHi = 6272, 6292

// 队伍变更 opcode 区间(524 TEAM_CHANGE / 526 CHANGE_MAIN_TEAM 的 REQ/RSP,回包带刷新后队伍快照)。
const teamOpcodeLo, teamOpcodeHi = 524, 527

// CarriesBackpack 判断该 opcode 是否可能携带盒子布局(登录数据或盒子操作回包)。
func CarriesBackpack(opcode uint16) bool {
	return opcode == OpLoginRsp || (opcode >= boxOpcodeLo && opcode <= boxOpcodeHi)
}

// CarriesTeam 判断该 opcode 是否可能携带大世界队伍快照(登录、盒子操作或队伍变更回包)。
func CarriesTeam(opcode uint16) bool {
	return CarriesBackpack(opcode) || (opcode >= teamOpcodeLo && opcode <= teamOpcodeHi)
}
