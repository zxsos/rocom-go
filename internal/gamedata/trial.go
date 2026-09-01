package gamedata

import (
	_ "embed" // go:embed 需要(即使本文件不直接用 embed.FS)
	"encoding/json"
	"strconv"
)

// 草系徽章试炼的静态配置(由 scripts/gen_trial.py 生成)。
//
// 与 skills.json 一样,这份数据的来源**不是游戏解包**,而是玩家维护的 wiki
// (biligame.com,CC BY-NC-SA 4.0)。协议里不下发这些内容 —— 服务器只给
// chapter_id / node_index 这类编号,「这一层是首领还是商人」「对面可能是谁」
// 得靠这份静态配置才知道。
//
// 三样东西:
//  1. floors:按 node_index 索引的层类型(普通/首领/商人/NPC);
//  2. pools:各章普通池与 22 名首领(能显示头像);
//  3. npc:第 7 层 NPC 的候选阵容(按难度 × 章)。
//
// 层类型的对应关系是**实测得出的**,不是照抄 wiki —— 见 gen_trial.py 文件头
// 的两条证据(node_index 6 无战斗、node_index 7 对手是 NPC)。wiki 说每章 7 层、
// 协议是 8 个节点,直接套用会错位一层。
//
//go:embed data/trial.json
var trialJSON []byte

// TrialJSON 暴露内嵌的试炼配置原文,供 server 侧读取 _updated 等元信息。
// 只读,调用方不要修改返回的切片。
func TrialJSON() []byte { return trialJSON }

// FloorType 是试炼里一个节点的类型。
type FloorType string

const (
	FloorStart    FloorType = "start"    // 章节起点,无战斗
	FloorNormal   FloorType = "normal"   // 普通池,随机 3 个对手选项
	FloorBoss     FloorType = "boss"     // 首领池,22 名首领里随机 3 个
	FloorMerchant FloorType = "merchant" // 无精灵:远行商人或魔力之源
	FloorNPC      FloorType = "npc"      // NPC 阵容
	FloorUnknown  FloorType = ""         // node_index 越界或数据缺失
)

// Label 返回层类型的中文名,供前端直接展示。
func (t FloorType) Label() string {
	switch t {
	case FloorStart:
		return "起点"
	case FloorNormal:
		return "普通"
	case FloorBoss:
		return "首领"
	case FloorMerchant:
		return "商人"
	case FloorNPC:
		return "NPC"
	}
	return ""
}

// TrialOpponent 是第 7 层的一个候选 NPC 阵容。
//
// ID 是 wiki 那边的 opponent 编号(300xxx/310xxx/400xxx),**与协议里的 npc_id
// 不是同一套**(实测协议 npc_id=86023),无从绑定。故只能给出候选池,
// 不能断言当前遭遇的是哪一个 —— 前端应照此表述,别写成「对面就是这只」。
type TrialOpponent struct {
	ID   uint32   `json:"id"`
	Name string   `json:"name"` // NPC 名(研究员/易西/罗兰)
	Pets []uint32 `json:"pets"` // 阵容:petbase id 列表
}

// trialDB 是解析后的试炼静态配置。
type trialDB struct {
	floors   []FloorType                           // 按 node_index 索引
	chapters map[uint32]string                     // 章序号 -> 章节名(如「记忆中的索米亚草原」)
	pools    map[uint32][]uint32                   // 章序号 -> 普通池 petbase
	bosses   []uint32                              // 22 名首领 petbase
	npc      map[uint32]map[uint32][]TrialOpponent // 难度 -> 章序号 -> 候选阵容
}

// loadTrial 解析内嵌的试炼配置;缺失或格式不符时返回空表(不影响其余功能)。
func loadTrial() *trialDB {
	db := &trialDB{}
	// 注意 npc 比别的字段多一层:难度 -> {name, chapters} -> 章 -> 阵容。
	// 少写这层 chapters 会让 Unmarshal **整体**失败 —— 而失败时这里静默返回空表,
	// 表现是「所有试炼配置都不见了」却没有任何报错(踩过一次,靠单测才发现)。
	var raw struct {
		Floors   []string `json:"floors"`
		Chapters map[string]struct {
			Name string `json:"name"`
		} `json:"chapters"`
		Pools  map[string][]uint32 `json:"pools"`
		Bosses []uint32            `json:"bosses"`
		NPC    map[string]struct {
			Name     string                     `json:"name"`
			Chapters map[string][]TrialOpponent `json:"chapters"`
		} `json:"npc"`
	}
	if err := json.Unmarshal(trialJSON, &raw); err != nil {
		return db
	}
	for _, s := range raw.Floors {
		db.floors = append(db.floors, FloorType(s))
	}
	if len(raw.Chapters) > 0 {
		db.chapters = make(map[uint32]string, len(raw.Chapters))
	}
	for k, v := range raw.Chapters {
		if n, err := strconv.ParseUint(k, 10, 32); err == nil {
			db.chapters[uint32(n)] = v.Name
		}
	}
	if len(raw.Pools) > 0 {
		db.pools = make(map[uint32][]uint32, len(raw.Pools))
	}
	for k, v := range raw.Pools {
		if n, err := strconv.ParseUint(k, 10, 32); err == nil {
			db.pools[uint32(n)] = v
		}
	}
	db.bosses = raw.Bosses
	if len(raw.NPC) > 0 {
		db.npc = make(map[uint32]map[uint32][]TrialOpponent, len(raw.NPC))
	}
	for mk, mode := range raw.NPC {
		id, err := strconv.ParseUint(mk, 10, 32)
		if err != nil {
			continue
		}
		byCh := map[uint32][]TrialOpponent{}
		for ck, ops := range mode.Chapters {
			if n, err := strconv.ParseUint(ck, 10, 32); err == nil {
				byCh[uint32(n)] = ops
			}
		}
		db.npc[uint32(id)] = byCh
	}
	return db
}

// TrialFloor 返回某节点(node_index)的层类型;越界返回 FloorUnknown。
//
// 调用方拿到的 node_index 来自协议的 0x1961(推进节点请求),取值 0~7。
// 这里不做取模:越界说明协议变了或解析有误,返回未知比猜一个类型安全。
func (db *DB) TrialFloor(nodeIndex uint32) FloorType {
	if db.trial == nil || int(nodeIndex) >= len(db.trial.floors) {
		return FloorUnknown
	}
	return db.trial.floors[nodeIndex]
}

// TrialChapterName 返回第 n 章(1 起)的章节名,如「记忆中的索米亚草原」;
// 查不到返回空串 —— 与协议给的 chapter_id 无关,这里按**章节序号**查
// (三套编号互不通用,见 gen_trial.py 的说明)。
func (db *DB) TrialChapterName(chapterIdx uint32) string {
	if db.trial == nil {
		return ""
	}
	return db.trial.chapters[chapterIdx]
}

// TrialBosses 返回 22 名首领的 petbase id(第 4 层的候选池)。
func (db *DB) TrialBosses() []uint32 {
	if db.trial == nil {
		return nil
	}
	return db.trial.bosses
}

// TrialPool 返回第 n 章(1 起)普通池的 petbase id 列表(208/315/177 只)。
//
// 这是「遇见记录」三张图的数据来源:每章一张图,图里列出本章可能遇到的精灵,
// 遇到过的置灰。查不到返回 nil(第 4 章等越界,或静态配置缺失)。
//
// 与 TrialBosses 是**两个独立来源**:首领来自第 4 层的 22 人名单(三章共用),
// 普通池来自第 1/2/3/5 层。前端一张图要同时展示两者,别混为一谈。
func (db *DB) TrialPool(chapterIdx uint32) []uint32 {
	if db.trial == nil {
		return nil
	}
	return db.trial.pools[chapterIdx]
}

// TrialNPCOpponents 返回第 7 层在某难度(10000/10001/10002)某章的候选阵容。
//
// 返回的是**候选池**:wiki 的 opponent id 与协议 npc_id 不是同一套编号,
// 无法锁定当前遭遇的是哪一个。查不到返回 nil(数据缺失不是错误)。
func (db *DB) TrialNPCOpponents(modeID, chapterIdx uint32) []TrialOpponent {
	if db.trial == nil {
		return nil
	}
	return db.trial.npc[modeID][chapterIdx]
}
