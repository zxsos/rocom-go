package pipeline

import (
	"sort"

	"github.com/whoisnian/rocom-capture/internal/capture"
	"github.com/whoisnian/rocom-capture/internal/scene"
)

// flowerItem 是一只花种(花灵)BOSS 的展示信息:花种页卡片按此渲染。
// 面板 0x0375 整组下发基础字段;玩家点击地图花种后的 0x0338 详情(等级/炫彩/绑定宠物/奖牌)合并进来。
type flowerItem struct {
	ID          uint32 `json:"id"`          // 花种 NPC 配置 id
	Name        string `json:"name"`        // 守护宠物名(petbase,未知时为空)
	Img         string `json:"img"`         // 守护宠物头像 /img/<此路径>(未知时为空,前端回退)
	Star        uint32 `json:"star"`        // 星级(普通花灵 5,特殊花灵 7)
	Blood       uint32 `json:"blood"`       // 血脉 id(PET_BLOOD_CONF.blood,1-24)
	BloodName   string `json:"bloodName"`   // 血脉中文短名(普通/草/火…;未知时为空)
	BloodIcon   string `json:"bloodIcon"`   // 血脉主图标 /img/<此路径>(未知时为空)
	NpcLogicID  uint64 `json:"npcLogicId"`  // NPC 逻辑 id(每只花种唯一;详情合并按此匹配)
	EndTs       uint64 `json:"endTs"`       // 活动结束 Unix 秒(0=未设置)
	SpecSeedID  uint32 `json:"specSeedId"`  // 特殊花种种子 id(0=普通花种)
	ActivityID  uint32 `json:"activityId"`  // 活动 id
	OwnerUserID uint64 `json:"ownerUserId"` // 已选花种的玩家 user_id(0=无人选择)
	Detail     bool   `json:"detail"`       // 是否已收到 0x0338 详情(玩家点过地图花种;未点过=false)
	// 以下为 0x0338 详情(点击地图花种后更新;0/空=尚未获取,普通花种绑定/奖牌恒为 0/空):
	Lv        uint32 `json:"lv"`        // 等级
	GlassType int32  `json:"glassType"` // 炫彩类型(0=无炫彩 / 1=普通 / 2=隐藏;仅在 detail=true 时有效)
	Glass     string `json:"glass"`     // 炫彩中文描述(GlassDesc;空=无炫彩或未获取)
	BindName  string `json:"bindName"`  // 绑定守护宠物名(空=无绑定)
	BindImg   string `json:"bindImg"`   // 绑定守护宠物头像 /img/<此路径>
	BindEvo   uint32 `json:"bindEvo"`   // 绑定宠物进化阶段 id(0=无)
	MedalName string `json:"medalName"` // 绑定宠物佩戴的奖牌名(空=无)
	MedalIcon string `json:"medalIcon"` // 奖牌小图 /img/<此路径>
}

// flowerKey 兜底定位一只花种:同种花种可同时存在多只,游戏内按血脉区分。
// 优先用 npc_logic_id(每只花种唯一)匹配,旧分组里没有时才退回此键。
type flowerKey struct {
	id    uint32
	blood uint32
}

// onBossNpcInfo 处理 s2c 0x0375 花种 BOSS 分组:只把 flower_npcs(花灵,普通+特殊)渲染到花种页。
// world_leader_npcs(世界 BOSS)与 legendary_npcs(传说 NPC)与花种无关,解析时不取。
// 游戏内每次打开面板都会整组重发:基础字段以新下发为准,但已点过的 0x0338 详情
// 按 npc_logic_id 从旧分组里保留,避免整组刷新把查看状态冲掉。
func (p *Pipeline) onBossNpcInfo(m capture.Message, acc string) {
	// 先读旧分组,收集已点过(有 0x0338 详情)的项:按 npc_logic_id 索引,兜底按 (id,blood)。
	prevLogic := make(map[uint64]flowerItem)
	prevKey := make(map[flowerKey]flowerItem)
	if raw := p.srv.GetLastFlowers(acc); raw != nil {
		if payload, ok := raw.(map[string]any); ok {
			if items, ok := payload["flowers"].([]flowerItem); ok {
				for _, it := range items {
					if !it.Detail {
						continue
					}
					if it.NpcLogicID != 0 {
						prevLogic[it.NpcLogicID] = it
					} else {
						prevKey[flowerKey{it.ID, it.Blood}] = it
					}
				}
			}
		}
	}
	items := make([]flowerItem, 0, 23)
	for _, b := range scene.ParseBossNpcInfoRsp(m.AppBody) {
		it := flowerItem{
			ID:          b.NpcCfgID,
			Star:        b.Star,
			Blood:       b.Blood,
			NpcLogicID:  b.NpcLogicID,
			EndTs:       b.EndTs,
			SpecSeedID:  b.SpecSeedID,
			ActivityID:  b.ActivityID,
			OwnerUserID: b.OwnerUserID,
		}
		if base, ok := p.db.PetBase(b.PetBaseID); ok {
			it.Name = base.Name
		}
		it.BloodName = p.db.BloodName(b.Blood)
		it.BloodIcon = p.db.BloodIcon(b.Blood)
		it.Img = p.db.PetImageByBase(b.PetBaseID, false).Head
		// 合并旧详情:面板整组重发不丢已点过的 0x0338 查看状态。
		var old flowerItem
		var ok bool
		if it.NpcLogicID != 0 {
			old, ok = prevLogic[it.NpcLogicID]
		}
		if !ok {
			old, ok = prevKey[flowerKey{it.ID, it.Blood}]
		}
		if ok {
			it.Detail = old.Detail
			it.Lv = old.Lv
			it.GlassType = old.GlassType
			it.Glass = old.Glass
			it.BindName = old.BindName
			it.BindImg = old.BindImg
			it.BindEvo = old.BindEvo
			it.MedalName = old.MedalName
			it.MedalIcon = old.MedalIcon
		}
		items = append(items, it)
	}
	// 顺序稳定:特殊花种(7 星)在前,普通花种按血脉 id 升序。
	sort.Slice(items, func(i, j int) bool {
		if items[i].Star != items[j].Star {
			return items[i].Star > items[j].Star
		}
		return items[i].Blood < items[j].Blood
	})
	payload := map[string]any{"account": acc, "flowers": items}
	p.srv.SetLastFlowers(acc, payload)
	p.srv.Hub().Broadcast("flowers", acc, payload)
}

// onSelectFlowerSeedBoss 记录 c2s 0x0846 选中的花种 npc_logic_id:
// 玩家点某朵花进战斗时发出,作为捕捉成功(0x0160 catch_way=4)后清理详情的定位锚点。
func (p *Pipeline) onSelectFlowerSeedBoss(m capture.Message, acc string) {
	if logicID := scene.ParseSelectFlowerSeedBossReq(m.AppBody); logicID != 0 {
		p.acct(acc).lastFlowerLogicID = logicID
	}
}

// clearFlowerDetail 花种精灵捕捉成功(catch_way=4)后清理对应花种的 0x0338 详情:
// 捕捉后该花种重生为新的个体,旧详情(等级/炫彩/绑定/奖牌)不再有效,需玩家重新点击查看。
// 定位用最近一次 c2s 0x0846 选中的 npc_logic_id;清空详情字段后广播,前端恢复「未查看」。
func (p *Pipeline) clearFlowerDetail(acc string) {
	logicID := p.acct(acc).lastFlowerLogicID
	if logicID == 0 {
		return
	}
	raw := p.srv.GetLastFlowers(acc)
	if raw == nil {
		return
	}
	payload, ok := raw.(map[string]any)
	if !ok {
		return
	}
	items, ok := payload["flowers"].([]flowerItem)
	if !ok {
		return
	}
	// 复制一份再改,避免直接动 server 缓存里的共享切片。
	items = append([]flowerItem(nil), items...)
	updated := false
	for i := range items {
		f := &items[i]
		if f.NpcLogicID != logicID {
			continue
		}
		f.Detail = false
		f.Lv = 0
		f.GlassType = 0
		f.Glass = ""
		f.BindName = ""
		f.BindImg = ""
		f.BindEvo = 0
		f.MedalName = ""
		f.MedalIcon = ""
		updated = true
		break
	}
	if !updated {
		return
	}
	payload = map[string]any{"account": acc, "flowers": items}
	p.srv.SetLastFlowers(acc, payload)
	p.srv.Hub().Broadcast("flowers", acc, payload)
}

// onTeamBattleInfo 处理 s2c 0x0338(点击地图花种的详情回包):
// 优先按 npc_logic_id(每只花种唯一)匹配到已有卡片,兜底按 npc_cfg_id+blood,
// 合并等级/炫彩/绑定宠物/奖牌等详情后重新广播。
// 面板 0x0375 整组重发时 onBossNpcInfo 会从旧分组保留详情,玩家无需再次点击。
func (p *Pipeline) onTeamBattleInfo(m capture.Message, acc string) {
	d, ok := scene.ParseTeamBattleInfoQueryRsp(m.AppBody)
	if !ok {
		return
	}
	raw := p.srv.GetLastFlowers(acc)
	if raw == nil {
		return // 还没收到过面板分组,无从合并
	}
	payload, ok := raw.(map[string]any)
	if !ok {
		return
	}
	items, ok := payload["flowers"].([]flowerItem)
	if !ok {
		return
	}
	// 复制一份再改,避免直接动 server 缓存里的共享切片。
	items = append([]flowerItem(nil), items...)
	updated := false
	for i := range items {
		f := &items[i]
		if d.NpcLogicID != 0 {
			if f.NpcLogicID == 0 || f.NpcLogicID != d.NpcLogicID {
				continue
			}
		} else if f.ID != d.NpcCfgID || f.Blood != d.Blood {
			continue
		}
		updated = true
		f.Detail = true
		if d.NpcLogicID != 0 {
			f.NpcLogicID = d.NpcLogicID
		}
		f.Lv = d.Lv
		f.GlassType = d.GlassType
		if d.EndTs != 0 {
			f.EndTs = d.EndTs
		}
		if d.SpecSeedID != 0 {
			f.SpecSeedID = d.SpecSeedID
		}
		if d.ActivityID != 0 {
			f.ActivityID = d.ActivityID
		}
		if d.OwnerUserID != 0 {
			f.OwnerUserID = d.OwnerUserID
		}
		f.Glass = p.db.GlassDesc(d.GlassType, d.GlassValue)
		if d.BindBaseID != 0 {
			if base, ok := p.db.PetBase(d.BindBaseID); ok {
				f.BindName = base.Name
			}
			f.BindImg = p.db.PetImageByBase(d.BindBaseID, false).Head
			f.BindEvo = d.BindEvoID
		}
		if d.MedalID != 0 {
			if md, ok := p.db.Medal(d.MedalID); ok {
				f.MedalName = md.Name
			}
			f.MedalIcon = p.db.MedalIcon(d.MedalID)
		}
		break
	}
	if !updated {
		return
	}
	payload = map[string]any{"account": acc, "flowers": items}
	p.srv.SetLastFlowers(acc, payload)
	p.srv.Hub().Broadcast("flowers", acc, payload)
}
