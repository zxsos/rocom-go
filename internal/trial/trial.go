// Package trial 解析「草系徽章试炼」(ZONE_GRASS_TRIAL_*)的消息,供草系试炼页实时同步
// 游戏内的一局进度。
//
// 这族消息**不在 internal/pb 的生成范围里**(gen_proto.py 只生成 com_pet / com_pet_team
// 两个根的闭包),故与 scene 包同路:按实测字段号在 wire 层取值,不依赖生成代码。
// 字段号取自游戏描述符 all.pb(可用 `go run ./cmd/fielddump GrassTrial` 复核)。
//
// 玩法与报文时序见 docs/pcap-20260831-grass-trial.md:一局 = 选一只自己的宠物 → 连打 3 章
// (每章 8 个节点)→ 每节点是事件/战斗/祝福 → 战利品当场装配 → 章末商店 → 章末 BOSS。
package trial

import (
	"bytes"

	"google.golang.org/protobuf/encoding/protowire"

	"github.com/whoisnian/rocom-capture/internal/wire"
)

// 草系试炼 opcode(来自 ZoneSvrCmd,见 names.json opcodes / pbdesc 的 opmsg.json)。
const (
	OpStartChallengeRsp = 0x1951 // s2c 开始挑战回包(challenge_data,内含完整 PetData)
	OpEnterSceneReq     = 0x1952 // c2s 进入试炼场景
	OpGetInfoRsp        = 0x1959 // s2c 全量:trial_data(进行中的一局 + 账号档案) + 禁用技能
	OpAbandonRsp        = 0x195b // s2c 放弃挑战回包
	OpChallengeDataSync = 0x195c // s2c 进行中一局的全量快照(每章开头各推一次)
	OpResumeRsp         = 0x1960 // s2c 恢复(断线重连)一局
	OpNextNodeReq       = 0x1961 // c2s 推进到下一节点(chapter_id/node_index)
	OpNextNodeRsp       = 0x1962 // s2c 节点内容:事件三选一 或 祝福三选一
	OpSelectEventRsp    = 0x1964 // s2c 空壳(战斗经 0x1316 下发;祝福与商店在同一包里)
	OpNodeRefreshRsp    = 0x1966 // s2c 重掷后的整节选项 + 累计花费 + 剩余金币
	OpRewardNotify      = 0x1967 // s2c 节点奖励到账(event_conf_id/reward_id/额外碎片)
	OpRewardReq         = 0x1968 // c2s 处理奖励(action)
	OpRewardRsp         = 0x1969 // s2c 处理后的宠物与剩余金币
	OpSettleNotify      = 0x196a // s2c 一局结算(review 战绩 + 总分)
	OpBossBattleEnter   = 0x196d // c2s 进入 BOSS 战
	OpBlessSelectReq    = 0x196f // c2s 选祝福选项
	OpBlessSelectRsp    = 0x1970 // s2c 候选技能/碎片 + 下一步
	OpBlessConfirmReq   = 0x1971 // c2s 确认祝福(选技能 / 合并两个技能槽)
	OpBlessConfirmRsp   = 0x1972 // s2c 处理后的宠物 + finished 标记
	OpProgressDataSync  = 0x1975 // s2c 账号级档案全量(55KB:战绩回顾/图鉴槽/见闻录)
	OpShopBuyRsp        = 0x1979 // s2c 购买结果(商品 + 宠物 + 剩余金币)
	OpChangeSkillRsp    = 0x197b // s2c 换出战技能后的宠物
	OpQueryChallengeRsp = 0x197d // s2c 开局前查询:可选章节 / 本周词条 / 初始金币
	OpTeleportFirstDun  = 0x1a42 // c2s 传送到试炼营地
)

// c2sSubHeaderLen 是 c2s AppBody 里 protobuf 之前的子头长度(实测恒为 6,同 scene 包)。
const c2sSubHeaderLen = 6

// tsf4gMark 是应用层 protobuf body 之后的尾标记,解码在其前停止。
var tsf4gMark = []byte("tsf4g")

// body 裁剪出 protobuf 部分:去 tsf4g 尾,c2s 再跳 6 字节子头。
func body(appBody []byte, c2s bool) []byte {
	b := appBody
	if i := bytes.Index(b, tsf4gMark); i >= 0 {
		b = b[:i]
	}
	if c2s {
		if len(b) <= c2sSubHeaderLen {
			return nil
		}
		b = b[c2sSubHeaderLen:]
	}
	return b
}

// appendU32 把一个 repeated uint32 字段值追加到 dst:兼容非 packed(独立 tag 的 varint)
// 与 packed(单个 bytes 内含一串)两种编码。
func appendU32(dst []uint32, val []byte, typ protowire.Type, v uint64) []uint32 {
	if typ == protowire.VarintType {
		return append(dst, uint32(v))
	}
	if typ == protowire.BytesType {
		for _, x := range wire.PackedVarints(val) {
			dst = append(dst, uint32(x))
		}
	}
	return dst
}

// ---- 数据结构 ----

// Skill 是试炼宠物身上的一个技能槽(融合过的技能)。
// 与普通 PetData 的技能不同:这里的技能是**试炼专属**的融合体,
// 由 base_skill_id 与若干 merged_skill_ids 合成,威力/能耗随融合次数变。
type Skill struct {
	BaseID      uint32
	Power       uint32 // 融合后威力
	EnergyCost  uint32 // 融合后能耗
	FusionCount uint32 // 融合过几次(0=未融合)
	SkillType   uint32
	SlotPos     uint32 // 技能槽位(1 起)
	Merged      []uint32
}

// Pet 是试炼里的宠物副本。它是**独立于真实仓库**的一份状态:血量/技能/特性/碎片
// 只在试炼内有效,一局结束即丢弃(实测见 docs/pcap-20260831-grass-trial.md 第 5 节)。
type Pet struct {
	Gid        uint32
	BaseConfID uint32
	HP         uint32
	MaxHP      uint32
	Level      uint32
	EnergyCeil uint32
	Growth     uint32
	Skills     []Skill
	Features   []uint32 // 已获得的特性 id(288xxx):天生 + 试炼中获得,见 InitialFeatures
	Shards     []uint32 // 已获得的碎片 id(20xx/30xx)
	Equipped   []uint32 // 出战技能槽位(equipped_skill_slots)
	Name       string   // 昵称,取自内嵌 PetData 的 name(玩家可能改过名)
	ConfID     uint32   // 宠物 conf_id(取自内嵌 PetData,供查种类名)
}

// Has 判断某特性是否属于天生。
func (f InitialFeatures) Has(id uint32) bool {
	for _, x := range f {
		if x == id {
			return true
		}
	}
	return false
}

// IsFeatureID 报告一个试炼 id 是不是特性(288xxx)。
//
// 判据是**数值区间**:协议不下发类型字段,只能按区间分 ——
// 实测(逐条对照 0x1968 的 updated_pet 落点,见 docs/pcap-20260831-grass-trial.md 2.1):
//
//	7xxxxxx    技能(进 skills[])
//	288xxx     特性(进 acquired_feature_ids[])
//	20xx/30xx  碎片(进 acquired_shard_effect_ids[])
//
// 区间外一律答 false:认错会把名字绑到错误的 id 上,不认只是少一个名字。
func IsFeatureID(id uint32) bool { return id >= 288000 && id < 289000 }

// InitialFeatures 是宠物**天生的**特性(局级 initial_feature_ids,#33)。
//
// 与 Pet.Features(宠物级 acquired_feature_ids,#11)的关系:
//
//	Features        = 天生 + 试炼中获得的(会随推进增长)
//	InitialFeatures = 只有天生的(整局不变)
//	差集            = 试炼中获得的
//
// 为什么值得区分 —— 实测一份完整试炼(17 场战斗)的特性序列:
//
//	第 1场 [288135]
//	第 2场 [288135, 288001]
//	第 6场 [288135, 288001, 288022]
//	第17场 [288135, 288001, 288022, 288025, 288154, 288043]
//
// 288135 从头到尾都在、且始终是第一个,它就是宠物自带的特性;后面逐个累积的
// 是打节点拿到的。玩家的这条经验(「第一个一定是自己本身的特性」)与数据完全吻合
// —— 因为 initial_feature_ids 恒定,而 acquired 逐个追加。
//
// ⚠️ 别假设 InitialFeatures 是 Features 的前缀子集: acquired 是**累积追加**的,
// 实测看确实是前缀,但那是观察到的行为而非协议保证。取差集时两边都查,
// 不要只按下标切片。
type InitialFeatures []uint32

// NodeEvent 是当前节点里的一个候选事件(通常三个槽位,各带一个奖励)。
type NodeEvent struct {
	SlotIndex    uint32
	EventConfID  uint32
	RewardID     uint32
	EventCost    uint32 // 重掷该事件的报价
	RewardCost   uint32 // 重掷奖励的报价
	Level        uint32
	RandomSkills []uint32
	UsedRewards  []uint32 // 已经出过的奖励,重掷时排除
	ExtraRewards []uint32
}

// Selection 是当前节点的整节选项(重掷时服务器回全量)。
type Selection struct {
	Events           []NodeEvent
	EventRefreshes   uint32
	RewardRefreshes  uint32
	NpcObjID         uint64
	TotalRefreshCost uint32 // 本节点累计已花的刷新费
}

// BlessOption 是祝福的一个选项。
type BlessOption struct {
	OptionConfID uint32
	IsInfeasible bool
}

// BlessSelection 是祝福节点:先选一个选项,再在候选技能里挑一个。
type BlessSelection struct {
	EventConfID uint32
	Options     []BlessOption
}

// PendingStep 是祝福的「下一步」:effect 决定这一步要做什么(0=从候选技能里选一个学会、
// 9=合并两个技能槽)。
type PendingStep struct {
	Effect         uint32
	CandidateSkill []uint32
	CandidateShard []uint32
	CandidatePos   []uint32
}

// ShopItem 是章末商店的一件商品。
type ShopItem struct {
	ItemType    uint32 // 2=特性(288xxx) 3=碎片(20xx/30xx)
	ItemID      uint32
	Price       uint32
	IsPurchased bool
	Index       uint32
}

// Challenge 是一局进行中的状态(GrassTrialChallengeData 的子集)。
type Challenge struct {
	State          uint32
	TrialConfID    uint32
	ChapterID      uint32
	NodeIndex      uint32
	Coin           uint32
	SlotID         uint32 // 属性系图鉴槽位:1000 + (dam_type - 2)
	ChallengeID    uint64
	StartTime      uint64
	FusionType     uint32
	FuseMaxTime    uint32
	FirstDungeonID uint32
	Pet            *Pet
	Selection      *Selection
	Effects        []uint32 // 本局生效的试炼词条(trial_effect_ids)
	Chapters       []uint32 // 可选章节(3000/3001/3002)
	// InitialFeatures 是宠物**天生的**特性(局级 #33,整局不变)。
	// 与 Pet.Features 之差即「试炼中获得的」,详见 InitialFeatures 的说明。
	InitialFeatures InitialFeatures
}

// Reward 是刚到账、等待玩家处理的节点奖励。
type Reward struct {
	EventConfID uint32
	RewardID    uint32
	Coin        uint32
	ExtraIDs    []uint32
}

// ReviewRecord 是一条历史战绩(服务器滚动保留 250 条)。
type ReviewRecord struct {
	SettleAt  uint64
	PetBaseID uint32
	PetLevel  uint32
	TrialID   uint32
	Victory   bool
	Duration  uint32
	SlotID    uint32
	Mutation  uint32
	FirstWin  bool
	// Skills 是结算快照里的技能(字段 9,repeated GrassTrialFusedSkillData)。
	// 与试炼槽里的技能同结构,含融合后的威力 —— 历史战绩只能看到融合态。
	//
	// 它是「试炼专属 id」(788 段)的唯一已知来源:当前局只见过 7880058,
	// 而 7880000~7880071 共 27 个只在历史战绩里出现。要还原它们的真身,
	// 只能靠这里的 fused_power/fused_energy_cost/skill_type 反查。
	Skills []Skill
}

// SlotProgress 是图鉴里某属性系槽位的通关情况(18 个系 × 3 个难度)。
type SlotProgress struct {
	SlotID     uint32
	SlotType   uint32
	DamType    uint32
	ClearedIDs []uint32 // 已通关的 trial_conf_id
}

// LogRecord 是见闻录的一册(账号档案 0x1975 里下发,**服务器保存的完整历史**)。
//
// 这是「已经遇见过哪些精灵」的**权威来源**,比抓 0x1316 战斗通知全得多:
// 战斗通知只能记到抓包期间发生的那几场,而见闻录是账号自始至终的累积
// (实测同一账号:抓包 17 场 → 见闻录 292 只)。
//
// ⚠️ DiscoveredIDs 曾一度只被数成 Discovered 数量、把 id 全丢了 —— 那样
// 登录后无法补录历史,已遇见的精灵得重新打一遍才会显示。别改回只计数。
type LogRecord struct {
	LogConfID     uint32
	Chapters      []uint32
	DiscoveredIDs []uint32 // discovered_petbase_ids:已发现的 petbase id
	Discovered    uint32   // 已发现的形态数(= len(DiscoveredIDs))
	Total         uint32   // 该册总数
	Unlocked      bool
}

// ChapterOf 返回本册对应第几章(1 起),认不出来返回 0。
//
// 映射 log_conf_id 100/101/102 → 第 1/2/3 章是**用实测数据验证过的**,
// 不是照名字猜:三册各自减去「对应章的池 ∪ 22 名首领」后差集全为 0
// (100: 148+19=167、101: 111+17=128、102: 87+14=101),吻合到个位。
// 协议里的 chapters 字段实测为空,推不出章节,只能靠这个映射。
//
// 记错章的后果比不记更糟:会把一章的进度污染到另一章上,而用户很难察觉。
// 故认不出来时返回 0,让调用方丢弃,而不是硬套一个默认值。
func (l *LogRecord) ChapterOf() uint32 {
	switch l.LogConfID {
	case 100:
		return 1
	case 101:
		return 2
	case 102:
		return 3
	}
	return 0
}

// Progress 是账号级的试炼档案(0x1975,约 55KB)。
type Progress struct {
	ClearedTrials []uint32
	Slots         []SlotProgress
	Reviews       []ReviewRecord
	Logs          []LogRecord
	ChallengeInc  uint32 // 累计挑战次数自增 id
}

// Settle 是一局结算(0x196a)。
type Settle struct {
	Review     ReviewRecord
	TotalScore uint32
	Weekly     uint32
}

// ---- 子结构 ----

// ParseChallengeData 解析 GrassTrialChallengeData。
func ParseChallengeData(b []byte) *Challenge {
	if len(b) == 0 {
		return nil
	}
	c := &Challenge{}
	wire.ScanFields(b, func(num protowire.Number, typ protowire.Type, val []byte, v uint64) {
		switch {
		case num == 1 && typ == protowire.VarintType:
			c.State = uint32(v)
		case num == 2 && typ == protowire.VarintType:
			c.TrialConfID = uint32(v)
		case num == 3 && typ == protowire.VarintType:
			c.ChapterID = uint32(v)
		case num == 4 && typ == protowire.VarintType:
			c.NodeIndex = uint32(v)
		case num == 5 && typ == protowire.BytesType:
			c.Pet = ParsePet(val)
		case num == 7 && typ == protowire.VarintType:
			c.Coin = uint32(v)
		case num == 8:
			c.Effects = appendU32(c.Effects, val, typ, v)
		case num == 11 && typ == protowire.BytesType:
			c.Selection = ParseSelection(val)
		case num == 12 && typ == protowire.VarintType:
			c.StartTime = v
		case num == 15 && typ == protowire.VarintType:
			c.FusionType = uint32(v)
		case num == 17 && typ == protowire.VarintType:
			c.FirstDungeonID = uint32(v)
		case num == 19 && typ == protowire.VarintType:
			c.FuseMaxTime = uint32(v)
		case num == 26 && typ == protowire.VarintType:
			c.SlotID = uint32(v)
		case num == 27 && typ == protowire.VarintType:
			c.ChallengeID = v
		case num == 31:
			c.Chapters = appendU32(c.Chapters, val, typ, v)
		case num == 33:
			// initial_feature_ids(#33,局级):宠物天生的特性。
			// 与宠物级 #11(acquired)之差即试炼中获得的 —— 见 InitialFeatures。
			c.InitialFeatures = appendU32(c.InitialFeatures, val, typ, v)
		}
	})
	return c
}

// ParsePet 解析 GrassTrialPet(试炼宠物副本)。
func ParsePet(b []byte) *Pet {
	if len(b) == 0 {
		return nil
	}
	p := &Pet{}
	wire.ScanFields(b, func(num protowire.Number, typ protowire.Type, val []byte, v uint64) {
		switch {
		case num == 1 && typ == protowire.VarintType:
			p.Gid = uint32(v)
		case num == 2 && typ == protowire.VarintType:
			p.BaseConfID = uint32(v)
		case num == 3 && typ == protowire.VarintType:
			p.HP = uint32(v)
		case num == 4 && typ == protowire.VarintType:
			p.MaxHP = uint32(v)
		case num == 5 && typ == protowire.VarintType:
			p.Level = uint32(v)
		case num == 6 && typ == protowire.VarintType:
			p.EnergyCeil = uint32(v)
		case num == 7 && typ == protowire.VarintType:
			p.Growth = uint32(v)
		case num == 10 && typ == protowire.BytesType:
			p.Skills = append(p.Skills, ParseSkill(val))
		case num == 11:
			p.Features = appendU32(p.Features, val, typ, v)
		case num == 12:
			p.Shards = appendU32(p.Shards, val, typ, v)
		case num == 13 && typ == protowire.BytesType:
			// 内嵌的真实 PetData:只要昵称与 conf_id(种类名/头像走 gamedata 按 base 查)。
			if cid, ok := wire.Varint(val, 2); ok {
				p.ConfID = uint32(cid)
			}
			if nb, ok := wire.Bytes(val, 3); ok {
				p.Name = string(nb)
			}
		case num == 14:
			p.Equipped = appendU32(p.Equipped, val, typ, v)
		}
	})
	return p
}

// ParseSkill 解析 GrassTrialFusedSkillData。
func ParseSkill(b []byte) Skill {
	var s Skill
	wire.ScanFields(b, func(num protowire.Number, typ protowire.Type, val []byte, v uint64) {
		switch {
		case num == 1 && typ == protowire.VarintType:
			s.BaseID = uint32(v)
		case num == 2 && typ == protowire.VarintType:
			s.Power = uint32(v)
		case num == 3 && typ == protowire.VarintType:
			s.EnergyCost = uint32(v)
		case num == 4 && typ == protowire.VarintType:
			s.FusionCount = uint32(v)
		case num == 5 && typ == protowire.VarintType:
			s.SkillType = uint32(v)
		case num == 6:
			s.Merged = appendU32(s.Merged, val, typ, v)
		case num == 7 && typ == protowire.VarintType:
			s.SlotPos = uint32(v)
		}
	})
	return s
}

// ParseSelection 解析 GrassTrialNodeSelection(整节选项)。
func ParseSelection(b []byte) *Selection {
	if len(b) == 0 {
		return nil
	}
	s := &Selection{}
	wire.ScanFields(b, func(num protowire.Number, typ protowire.Type, val []byte, v uint64) {
		switch {
		case num == 1 && typ == protowire.BytesType:
			s.Events = append(s.Events, ParseNodeEvent(val))
		case num == 2 && typ == protowire.VarintType:
			s.EventRefreshes = uint32(v)
		case num == 3 && typ == protowire.VarintType:
			s.RewardRefreshes = uint32(v)
		case num == 4 && typ == protowire.VarintType:
			s.NpcObjID = v
		case num == 5 && typ == protowire.VarintType:
			s.TotalRefreshCost = uint32(v)
		}
	})
	return s
}

// ParseNodeEvent 解析 GrassTrialNodeEvent。
func ParseNodeEvent(b []byte) NodeEvent {
	var e NodeEvent
	wire.ScanFields(b, func(num protowire.Number, typ protowire.Type, val []byte, v uint64) {
		switch {
		case num == 1 && typ == protowire.VarintType:
			e.SlotIndex = uint32(v)
		case num == 2 && typ == protowire.VarintType:
			e.EventConfID = uint32(v)
		case num == 3 && typ == protowire.VarintType:
			e.RewardID = uint32(v)
		case num == 4 && typ == protowire.VarintType:
			e.EventCost = uint32(v)
		case num == 5 && typ == protowire.VarintType:
			e.RewardCost = uint32(v)
		case num == 6:
			e.RandomSkills = appendU32(e.RandomSkills, val, typ, v)
		case num == 7 && typ == protowire.VarintType:
			e.Level = uint32(v)
		case num == 8:
			e.UsedRewards = appendU32(e.UsedRewards, val, typ, v)
		case num == 9:
			e.ExtraRewards = appendU32(e.ExtraRewards, val, typ, v)
		}
	})
	return e
}

// ParseBlessSelection 解析 GrassTrialBlessSelection。
func ParseBlessSelection(b []byte) *BlessSelection {
	if len(b) == 0 {
		return nil
	}
	bs := &BlessSelection{}
	wire.ScanFields(b, func(num protowire.Number, typ protowire.Type, val []byte, v uint64) {
		switch {
		case num == 1 && typ == protowire.VarintType:
			bs.EventConfID = uint32(v)
		case num == 2 && typ == protowire.BytesType:
			var o BlessOption
			wire.ScanFields(val, func(n2 protowire.Number, t2 protowire.Type, _ []byte, v2 uint64) {
				switch {
				case n2 == 1 && t2 == protowire.VarintType:
					o.OptionConfID = uint32(v2)
				case n2 == 4 && t2 == protowire.VarintType:
					o.IsInfeasible = v2 != 0
				}
			})
			bs.Options = append(bs.Options, o)
		}
	})
	return bs
}

// ParsePendingStep 解析 GrassTrialBlessPendingStep。
func ParsePendingStep(b []byte) *PendingStep {
	if len(b) == 0 {
		return nil
	}
	p := &PendingStep{}
	wire.ScanFields(b, func(num protowire.Number, typ protowire.Type, val []byte, v uint64) {
		switch {
		case num == 1 && typ == protowire.VarintType:
			p.Effect = uint32(v)
		case num == 2:
			p.CandidateSkill = appendU32(p.CandidateSkill, val, typ, v)
		case num == 3:
			p.CandidateShard = appendU32(p.CandidateShard, val, typ, v)
		case num == 4:
			p.CandidatePos = appendU32(p.CandidatePos, val, typ, v)
		}
	})
	return p
}

// ParseShopItem 解析 GrassTrialShopItem。
func ParseShopItem(b []byte) ShopItem {
	var it ShopItem
	wire.ScanFields(b, func(num protowire.Number, typ protowire.Type, _ []byte, v uint64) {
		switch {
		case num == 1 && typ == protowire.VarintType:
			it.ItemType = uint32(v)
		case num == 2 && typ == protowire.VarintType:
			it.ItemID = uint32(v)
		case num == 3 && typ == protowire.VarintType:
			it.Price = uint32(v)
		case num == 4 && typ == protowire.VarintType:
			it.IsPurchased = v != 0
		case num == 5 && typ == protowire.VarintType:
			it.Index = uint32(v)
		}
	})
	return it
}

// ParseProgress 解析 GrassTrialProgressData(账号级档案)。
func ParseProgress(b []byte) *Progress {
	if len(b) == 0 {
		return nil
	}
	p := &Progress{}
	wire.ScanFields(b, func(num protowire.Number, typ protowire.Type, val []byte, v uint64) {
		switch {
		case num == 1:
			p.ClearedTrials = appendU32(p.ClearedTrials, val, typ, v)
		case num == 2 && typ == protowire.BytesType:
			p.Slots = append(p.Slots, ParseSlotProgress(val))
		case num == 3 && typ == protowire.BytesType:
			p.Reviews = append(p.Reviews, ParseReview(val))
		case num == 4 && typ == protowire.BytesType:
			p.Logs = append(p.Logs, ParseLogRecord(val))
		case num == 6 && typ == protowire.VarintType:
			p.ChallengeInc = uint32(v)
		}
	})
	return p
}

// ParseSlotProgress 解析 GrassTrialSlotProgress。只收已通关的 trial_conf_id。
func ParseSlotProgress(b []byte) SlotProgress {
	var s SlotProgress
	wire.ScanFields(b, func(num protowire.Number, typ protowire.Type, val []byte, v uint64) {
		switch {
		case num == 1 && typ == protowire.VarintType:
			s.SlotID = uint32(v)
		case num == 2 && typ == protowire.VarintType:
			s.SlotType = uint32(v)
		case num == 4 && typ == protowire.BytesType:
			// rewards[]{trial_conf_id(1), reward_state(2), is_cleared(3)}
			if cl, ok := wire.Varint(val, 3); ok && cl != 0 {
				if id, ok := wire.Varint(val, 1); ok {
					s.ClearedIDs = append(s.ClearedIDs, uint32(id))
				}
			}
		case num == 5 && typ == protowire.VarintType:
			s.DamType = uint32(v)
		}
	})
	return s
}

// ParseReview 解析 GrassTrialReviewRecord。
func ParseReview(b []byte) ReviewRecord {
	var r ReviewRecord
	wire.ScanFields(b, func(num protowire.Number, typ protowire.Type, val []byte, v uint64) {
		switch {
		case num == 9 && typ == protowire.BytesType:
			r.Skills = append(r.Skills, ParseSkill(val))
		case num == 1 && typ == protowire.VarintType:
			r.SettleAt = v
		case num == 2 && typ == protowire.VarintType:
			r.PetBaseID = uint32(v)
		case num == 3 && typ == protowire.VarintType:
			r.PetLevel = uint32(v)
		case num == 5 && typ == protowire.VarintType:
			r.TrialID = uint32(v)
		case num == 6 && typ == protowire.VarintType:
			r.Victory = v != 0
		case num == 7 && typ == protowire.VarintType:
			r.Duration = uint32(v)
		case num == 12 && typ == protowire.VarintType:
			r.FirstWin = v != 0
		case num == 13 && typ == protowire.VarintType:
			r.SlotID = uint32(v)
		case num == 14 && typ == protowire.VarintType:
			r.Mutation = uint32(v)
		}
	})
	return r
}

// ParseLogRecord 解析 GrassTrialLogSceneRecord。
func ParseLogRecord(b []byte) LogRecord {
	var l LogRecord
	wire.ScanFields(b, func(num protowire.Number, typ protowire.Type, val []byte, v uint64) {
		switch {
		case num == 1 && typ == protowire.VarintType:
			l.LogConfID = uint32(v)
		case num == 2:
			l.Chapters = appendU32(l.Chapters, val, typ, v)
		case num == 3:
			// discovered_petbase_ids 实测是**非 packed**(每个 id 一个独立 tag,392 个),
			// 但协议允许 packed,故两种都认:packed 时一次到位,非 packed 时逐个累加。
			// 这里要的是**具体 id** 而非数量(见 LogRecord 的说明)——
			// 它们是补录「已经遇见过哪些精灵」的唯一来源。
			if typ == protowire.BytesType {
				for _, id := range wire.PackedVarints(val) {
					if id != 0 {
						l.DiscoveredIDs = append(l.DiscoveredIDs, uint32(id))
					}
				}
			} else if typ == protowire.VarintType && v != 0 {
				l.DiscoveredIDs = append(l.DiscoveredIDs, uint32(v))
			}
			l.Discovered = uint32(len(l.DiscoveredIDs))
		case num == 5 && typ == protowire.VarintType:
			l.Unlocked = v != 0
		case num == 6 && typ == protowire.VarintType:
			l.Total = uint32(v)
		}
	})
	return l
}

// ---- 各消息的解析 ----

// ParseGetInfoRsp 解析 0x1959:返回进行中的一局(可能为 nil)与账号档案。
func ParseGetInfoRsp(appBody []byte) (*Challenge, *Progress) {
	td := wire.SubMsg(body(appBody, false), 2)
	if td == nil {
		return nil, nil
	}
	var c *Challenge
	var p *Progress
	wire.ScanFields(td, func(num protowire.Number, typ protowire.Type, val []byte, _ uint64) {
		if typ != protowire.BytesType {
			return
		}
		switch num {
		case 1:
			c = ParseChallengeData(val)
		case 2:
			p = ParseProgress(val)
		}
	})
	return c, p
}

// ParseChallengeRsp 解析 s2c 的 0x1951 / 0x1960(挑战数据挂在字段 2)。
func ParseChallengeRsp(appBody []byte) *Challenge {
	return ParseChallengeData(wire.SubMsg(body(appBody, false), 2))
}

// ParseChallengeSync 解析 s2c 的 0x195c(挑战数据挂在字段 1)。
func ParseChallengeSync(appBody []byte) *Challenge {
	return ParseChallengeData(wire.SubMsg(body(appBody, false), 1))
}

// ParseProgressSync 解析 s2c 的 0x1975(档案挂在字段 1)。
func ParseProgressSync(appBody []byte) *Progress {
	return ParseProgress(wire.SubMsg(body(appBody, false), 1))
}

// NextNode 是 c2s 0x1961 的内容:玩家推进到某章某节点。
type NextNode struct {
	ChapterID uint32
	NodeIndex uint32
	NpcObjID  uint64
}

// ParseNextNodeReq 解析 c2s 0x1961。
func ParseNextNodeReq(appBody []byte) (NextNode, bool) {
	var n NextNode
	wire.ScanFields(body(appBody, true), func(num protowire.Number, typ protowire.Type, _ []byte, v uint64) {
		if typ != protowire.VarintType {
			return
		}
		switch num {
		case 1:
			n.ChapterID = uint32(v)
		case 2:
			n.NodeIndex = uint32(v)
		case 3:
			n.NpcObjID = v
		}
	})
	return n, n.ChapterID != 0
}

// NodeContent 是节点内容:要么是事件三选一,要么是祝福三选一,章末还带商店。
type NodeContent struct {
	Selection *Selection
	Bless     *BlessSelection
	Shop      []ShopItem
}

// ParseNextNodeRsp 解析 s2c 0x1962(node_selection 2 / bless_selection 3)。
func ParseNextNodeRsp(appBody []byte) NodeContent {
	var c NodeContent
	wire.ScanFields(body(appBody, false), func(num protowire.Number, typ protowire.Type, val []byte, _ uint64) {
		if typ != protowire.BytesType {
			return
		}
		switch num {
		case 2:
			c.Selection = ParseSelection(val)
		case 3:
			c.Bless = ParseBlessSelection(val)
		}
	})
	return c
}

// ParseSelectEventRsp 解析 s2c 0x1964:祝福与商店在同一包里(战斗经 0x1316 下发)。
func ParseSelectEventRsp(appBody []byte) NodeContent {
	var c NodeContent
	wire.ScanFields(body(appBody, false), func(num protowire.Number, typ protowire.Type, val []byte, _ uint64) {
		if typ != protowire.BytesType {
			return
		}
		switch num {
		case 3:
			c.Bless = ParseBlessSelection(val)
		case 4:
			c.Shop = append(c.Shop, ParseShopItem(val))
		}
	})
	return c
}

// NodeRefresh 是 s2c 0x1966 的内容。
type NodeRefresh struct {
	RefreshType uint32
	Selection   *Selection
	Coin        uint32
}

// ParseNodeRefreshRsp 解析 s2c 0x1966。
func ParseNodeRefreshRsp(appBody []byte) NodeRefresh {
	var r NodeRefresh
	wire.ScanFields(body(appBody, false), func(num protowire.Number, typ protowire.Type, val []byte, v uint64) {
		switch {
		case num == 2 && typ == protowire.VarintType:
			r.RefreshType = uint32(v)
		case num == 3 && typ == protowire.BytesType:
			r.Selection = ParseSelection(val)
		case num == 4 && typ == protowire.VarintType:
			r.Coin = uint32(v)
		}
	})
	return r
}

// ParseRewardNotify 解析 s2c 0x1967:节点奖励到账,等玩家处理。
func ParseRewardNotify(appBody []byte) *Reward {
	r := &Reward{}
	wire.ScanFields(body(appBody, false), func(num protowire.Number, typ protowire.Type, val []byte, v uint64) {
		switch {
		case num == 1 && typ == protowire.VarintType:
			r.EventConfID = uint32(v)
		case num == 2 && typ == protowire.VarintType:
			r.RewardID = uint32(v)
		case num == 3 && typ == protowire.VarintType:
			r.Coin = uint32(v)
		case num == 4:
			r.ExtraIDs = appendU32(r.ExtraIDs, val, typ, v)
		}
	})
	if r.RewardID == 0 && r.EventConfID == 0 {
		return nil
	}
	return r
}

// RewardAction 是 c2s 0x1968 里玩家对奖励的处理方式。
// 取值经 26 条请求逐条对照 updated_pet 推出(见 docs/pcap-20260831-grass-trial.md 2.1):
// 0=融合到指定槽 1=作为新技能 2=直接收下 3=换金币 4=仅刷新(不带 reward_id)。
type RewardAction struct {
	Action        uint32
	RewardID      uint32
	TargetSlotPos uint32
}

// ParseRewardReq 解析 c2s 0x1968。
func ParseRewardReq(appBody []byte) RewardAction {
	var a RewardAction
	wire.ScanFields(body(appBody, true), func(num protowire.Number, typ protowire.Type, _ []byte, v uint64) {
		if typ != protowire.VarintType {
			return
		}
		switch num {
		case 1:
			a.Action = uint32(v)
		case 2:
			a.RewardID = uint32(v)
		case 3:
			a.TargetSlotPos = uint32(v)
		}
	})
	return a
}

// Update 是「宠物被更新」类回包的共同输出。
//
// 这类回包的字段号**并不统一**(0x1969 的宠物在 2、金币在 3;0x1970/0x1972 的宠物在 4、
// 金币在 5;0x1979 的金币在 2、宠物在 4),故每种消息各自解析,只把结果收进这一个结构,
// 免得调用方按 opcode 分支。
type Update struct {
	Pet       *Pet
	Coin      uint32
	Pending   *PendingStep
	Finished  bool
	ShopItem  *ShopItem
	ItemIndex uint32
}

// ParseRewardRsp 解析 s2c 0x1969:{updated_pet(2), remaining_coin(3)}。
func ParseRewardRsp(appBody []byte) Update {
	var u Update
	wire.ScanFields(body(appBody, false), func(num protowire.Number, typ protowire.Type, val []byte, v uint64) {
		switch {
		case num == 2 && typ == protowire.BytesType:
			u.Pet = ParsePet(val)
		case num == 3 && typ == protowire.VarintType:
			u.Coin = uint32(v)
		}
	})
	return u
}

// parseBlessRsp 解析 s2c 0x1970 / 0x1972:{pending_step(3), updated_pet(4),
// remaining_coin(5), finished(6)}。
func parseBlessRsp(appBody []byte) Update {
	var u Update
	wire.ScanFields(body(appBody, false), func(num protowire.Number, typ protowire.Type, val []byte, v uint64) {
		switch {
		case num == 3 && typ == protowire.BytesType:
			u.Pending = ParsePendingStep(val)
		case num == 4 && typ == protowire.BytesType:
			u.Pet = ParsePet(val)
		case num == 5 && typ == protowire.VarintType:
			u.Coin = uint32(v)
		case num == 6 && typ == protowire.VarintType:
			u.Finished = v != 0
		}
	})
	return u
}

// ParseBlessSelectRsp 解析 s2c 0x1970(选完祝福选项后的候选)。
func ParseBlessSelectRsp(appBody []byte) Update { return parseBlessRsp(appBody) }

// ParseBlessConfirmRsp 解析 s2c 0x1972(确认祝福后的宠物)。
func ParseBlessConfirmRsp(appBody []byte) Update { return parseBlessRsp(appBody) }

// ParseShopBuyRsp 解析 s2c 0x1979:{remaining_coin(2), updated_item(3),
// updated_pet(4), item_index(5)}。
func ParseShopBuyRsp(appBody []byte) Update {
	var u Update
	wire.ScanFields(body(appBody, false), func(num protowire.Number, typ protowire.Type, val []byte, v uint64) {
		switch {
		case num == 2 && typ == protowire.VarintType:
			u.Coin = uint32(v)
		case num == 3 && typ == protowire.BytesType:
			it := ParseShopItem(val)
			u.ShopItem = &it
		case num == 4 && typ == protowire.BytesType:
			u.Pet = ParsePet(val)
		case num == 5 && typ == protowire.VarintType:
			u.ItemIndex = uint32(v)
		}
	})
	return u
}

// ParseChangeSkillRsp 解析 s2c 0x197b:{updated_pet(2)}。
func ParseChangeSkillRsp(appBody []byte) Update {
	var u Update
	wire.ScanFields(body(appBody, false), func(num protowire.Number, typ protowire.Type, val []byte, _ uint64) {
		if num == 2 && typ == protowire.BytesType {
			u.Pet = ParsePet(val)
		}
	})
	return u
}

// BlessConfirm 是 c2s 0x1971 的内容。
type BlessConfirm struct {
	Effect        uint32
	ChosenSkillID uint32
	TargetSlot    uint32
	Action        uint32
	SecondSlot    uint32
}

// ParseBlessConfirmReq 解析 c2s 0x1971。
func ParseBlessConfirmReq(appBody []byte) BlessConfirm {
	var c BlessConfirm
	wire.ScanFields(body(appBody, true), func(num protowire.Number, typ protowire.Type, _ []byte, v uint64) {
		if typ != protowire.VarintType {
			return
		}
		switch num {
		case 1:
			c.Effect = uint32(v)
		case 2:
			c.ChosenSkillID = uint32(v)
		case 4:
			c.TargetSlot = uint32(v)
		case 5:
			c.Action = uint32(v)
		case 6:
			c.SecondSlot = uint32(v)
		}
	})
	return c
}

// ParseSettleNotify 解析 s2c 0x196a(一局结束):{review(1), total_score(2), weekly_score(3)}。
func ParseSettleNotify(appBody []byte) *Settle {
	b := body(appBody, false)
	rev := wire.SubMsg(b, 1)
	if rev == nil {
		return nil
	}
	s := &Settle{Review: ParseReview(rev)}
	wire.ScanFields(b, func(num protowire.Number, typ protowire.Type, _ []byte, v uint64) {
		if typ != protowire.VarintType {
			return
		}
		switch num {
		case 2:
			s.TotalScore = uint32(v)
		case 3:
			s.Weekly = uint32(v)
		}
	})
	return s
}

// QueryChallenge 是 s2c 0x197d 的内容(开局前的可选章节与本周词条)。
type QueryChallenge struct {
	Chapters []uint32
	Effects  []uint32
	InitCoin uint32
}

// ParseQueryChallengeRsp 解析 s2c 0x197d。
func ParseQueryChallengeRsp(appBody []byte) QueryChallenge {
	var q QueryChallenge
	wire.ScanFields(body(appBody, false), func(num protowire.Number, typ protowire.Type, val []byte, v uint64) {
		switch {
		case num == 2:
			q.Chapters = appendU32(q.Chapters, val, typ, v)
		case num == 3:
			q.Effects = appendU32(q.Effects, val, typ, v)
		case num == 4 && typ == protowire.VarintType:
			q.InitCoin = uint32(v)
		}
	})
	return q
}
