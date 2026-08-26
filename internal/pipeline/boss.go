package pipeline

import (
	"reflect"
	"sort"
	"strconv"

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
	OwnerUserID uint64 `json:"ownerUserId"` // 世界归属判据:0=自己世界;非 0=好友世界,即世界归属者 user_id
	Detail      bool   `json:"detail"`      // 是否已收到 0x0338 详情(玩家点过地图花种;未点过=false)
	// 以下为 0x0338 详情(点击地图花种后更新;0/空=尚未获取,普通花种绑定/奖牌恒为 0/空):
	Lv        uint32 `json:"lv"`        // 等级
	GlassType int32  `json:"glassType"` // 炫彩类型(0=无炫彩 / 1=普通 / 2=隐藏;仅在 detail=true 时有效)
	Glass     string `json:"glass"`     // 炫彩中文描述(GlassDesc;空=无炫彩或未获取)
	GlassValue int32 `json:"glassValue"` // 炫彩数值(普通=(粒子id<<20)|配色id;隐藏=1/2/3 赛季、1000;前端据此渲染色卡)
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

// maxFriendWorlds 是好友世界槽上限:超过后按最近访问时间(ts)淘汰最早的;
// 自己世界槽("self")不占名额、永不淘汰,可自由访问任意多好友世界。
const maxFriendWorlds = 5

// flowerWorldOwner 取花种列表的世界归属判据(select_flower_owner_id):
// 全为 0 = 自己世界;有非 0 值 = 好友世界,该值即世界归属者 user_id(游戏账号唯一标识,
// 每账号不同)。同世界内各花种取值一致(0 或同一归属者),取第一个非 0 即可。
func flowerWorldOwner(items []flowerItem) uint64 {
	for _, it := range items {
		if it.OwnerUserID != 0 {
			return it.OwnerUserID
		}
	}
	return 0
}

// flowerOwnerKey 生成好友世界槽的 key:归属者 user_id 前加 "owner:" 前缀,与 "self" 区分。
func flowerOwnerKey(owner uint64) string {
	return "owner:" + strconv.FormatUint(owner, 10)
}

// mergeFlowerDetail 把旧槽列表里已点过的 0x0338 详情按 npc_logic_id(兜底 id+blood)合并进新列表。
// 仅在同世界内调用:(id,blood) 兜底依赖同世界内品种唯一,跨世界可能误继承,故不得跨世界使用。
func mergeFlowerDetail(items, prev []flowerItem) []flowerItem {
	prevLogic := make(map[uint64]flowerItem)
	prevKey := make(map[flowerKey]flowerItem)
	for _, it := range prev {
		if !it.Detail {
			continue
		}
		if it.NpcLogicID != 0 {
			prevLogic[it.NpcLogicID] = it
		} else {
			prevKey[flowerKey{it.ID, it.Blood}] = it
		}
	}
	for i := range items {
		it := &items[i]
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
			it.GlassValue = old.GlassValue
			it.BindName = old.BindName
			it.BindImg = old.BindImg
			it.BindEvo = old.BindEvo
			it.MedalName = old.MedalName
			it.MedalIcon = old.MedalIcon
		}
	}
	return items
}

// cloneWorlds 浅复制存档表,后续增删槽不污染 server 缓存里的共享 map。
func cloneWorlds(worlds map[string]any) map[string]any {
	nw := make(map[string]any, len(worlds))
	for k, v := range worlds {
		nw[k] = v
	}
	return nw
}

// slotFlowers 读存档表某槽的列表与最近访问时间。
func slotFlowers(worlds map[string]any, key string) ([]flowerItem, int64, bool) {
	v, ok := worlds[key]
	if !ok {
		return nil, 0, false
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, 0, false
	}
	items, _ := m["flowers"].([]flowerItem)
	ts, _ := m["ts"].(int64)
	return items, ts, true
}

// trimFriendWorlds 好友槽超上限时按最近访问时间(ts 升序)淘汰最老的,self 槽不受影响。
func trimFriendWorlds(worlds map[string]any) {
	type entry struct {
		key string
		ts  int64
	}
	var list []entry
	for k, v := range worlds {
		if k == "self" {
			continue
		}
		if m, ok := v.(map[string]any); ok {
			if ts, ok := m["ts"].(int64); ok {
				list = append(list, entry{k, ts})
			}
		}
	}
	if len(list) <= maxFriendWorlds {
		return
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ts < list[j].ts })
	for _, e := range list[:len(list)-maxFriendWorlds] {
		delete(worlds, e.key)
	}
}

// setFlowers 更新当前世界列表并写回对应槽后广播(0x0338 详情合并/捕捉清理共用)。
// 槽同步写回,保证回访该世界时恢复的是最新 detail。
func (p *Pipeline) setFlowers(acc string, items []flowerItem) {
	payload, _ := p.srv.GetLastFlowers(acc).(map[string]any)
	var worlds map[string]any
	var cur string
	if payload != nil {
		worlds, _ = payload["worlds"].(map[string]any)
		cur, _ = payload["cur"].(string)
	}
	if worlds == nil {
		worlds = map[string]any{}
	}
	worlds = cloneWorlds(worlds)
	if cur != "" {
		if m, ok := worlds[cur].(map[string]any); ok {
			ns := make(map[string]any, len(m))
			for k, v := range m {
				if k == "avatar" {
					continue // 0x01df 指纹键已退役,顺带清理旧缓存残留
				}
				ns[k] = v
			}
			ns["flowers"] = items
			worlds[cur] = ns
		}
	}
	payload = map[string]any{"account": acc, "flowers": items, "cur": cur, "worlds": worlds}
	p.srv.SetLastFlowers(acc, payload)
	p.srv.Hub().Broadcast("flowers", acc, payload)
}

// onBossNpcInfo 处理 s2c 0x0375 花种 BOSS 分组:只把 flower_npcs(花灵,普通+特殊)渲染到花种页。
// world_leader_npcs(世界 BOSS)与 legendary_npcs(传说 NPC)与花种无关,解析时不取。
//
// 世界归属由普通花种的 select_flower_owner_id 直接确定(用户确认的判据):
// 全为 0 = 自己世界("self",永不淘汰);有非 0 值 = 好友世界,该值即世界归属者
// user_id(游戏账号唯一标识),槽键用归属者 user_id("owner:<uid>",稳定唯一,回访直接命中)。
// 命中同世界槽即从该槽恢复已点过的 0x0338 详情,避免整组重发把查看状态冲掉
// (退回拜访过的世界时同样命中,标记不丢)。
//   - 结果与当前列表完全一致时不广播(退回自己世界且花未变时前端零感知)。
func (p *Pipeline) onBossNpcInfo(m capture.Message, acc string) {
	// 解析新列表(不含 detail)。
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
		items = append(items, it)
	}
	// 顺序稳定:特殊花种(7 星)在前,普通花种按血脉 id 升序。
	sort.Slice(items, func(i, j int) bool {
		if items[i].Star != items[j].Star {
			return items[i].Star > items[j].Star
		}
		return items[i].Blood < items[j].Blood
	})

	// 读缓存。
	var payload map[string]any
	if raw, _ := p.srv.GetLastFlowers(acc).(map[string]any); raw != nil {
		payload = raw
	}
	var oldItems []flowerItem
	var oldCur string
	var worlds map[string]any
	if payload != nil {
		oldItems, _ = payload["flowers"].([]flowerItem)
		oldCur, _ = payload["cur"].(string)
		worlds, _ = payload["worlds"].(map[string]any)
	}
	if worlds == nil {
		worlds = map[string]any{}
	}

	// 空分组(花种全被采完):当前世界清空显示,但保留槽内 detail 不覆盖,等下次推送恢复。
	if len(items) == 0 {
		if len(oldItems) != 0 {
			payload = map[string]any{"account": acc, "flowers": []flowerItem{}, "cur": oldCur, "worlds": worlds}
			p.srv.SetLastFlowers(acc, payload)
			p.srv.Hub().Broadcast("flowers", acc, payload)
		}
		return
	}

	// 世界归属(唯一判据):select_flower_owner_id 全 0 = 自己世界;有非 0 = 好友世界(归属者 user_id)。
	ownerID := flowerWorldOwner(items)
	targetKey := "self"
	if ownerID != 0 {
		targetKey = flowerOwnerKey(ownerID)
	}
	targetItems, _, _ := slotFlowers(worlds, targetKey)

	// 恢复同世界 detail 并更新槽(复制存档表,不动 server 缓存里的共享 map)。
	items = mergeFlowerDetail(items, targetItems)
	worlds = cloneWorlds(worlds)
	worlds[targetKey] = map[string]any{"flowers": items, "ts": m.Time.Unix()}
	trimFriendWorlds(worlds)

	// 结果与当前完全一致则不刷新(退回自己世界且花未变时前端零感知)。
	if oldCur == targetKey && reflect.DeepEqual(items, oldItems) {
		return
	}
	payload = map[string]any{"account": acc, "flowers": items, "cur": targetKey, "worlds": worlds}
	p.srv.SetLastFlowers(acc, payload)
	p.srv.Hub().Broadcast("flowers", acc, payload)
}

// onSelectFlowerSeedBoss 记录 c2s 0x034E 选中的花种 npc_logic_id:
// 玩家点某朵花进战斗时发出,作为捕捉成功(0x132c catch_way=4)后清理详情的定位锚点。
func (p *Pipeline) onSelectFlowerSeedBoss(m capture.Message, acc string) {
	if logicID := scene.ParseSelectFlowerSeedBossReq(m.AppBody); logicID != 0 {
		p.acct(acc).lastFlowerLogicID = logicID
	}
}

// clearFlowerDetail 花种精灵捕捉成功(catch_way=4)后清理对应花种的 0x0338 详情:
// 捕捉后该花种重生为新的个体,旧详情(等级/炫彩/绑定/奖牌)不再有效,需玩家重新点击查看。
// 定位用最近一次 c2s 0x034E 选中的 npc_logic_id;清空详情字段后广播,前端恢复「未查看」。
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
		f.GlassValue = 0
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
	p.setFlowers(acc, items)
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
	// 兜底锚点:玩家点花种查看详情必发 0x0338(c2s 0x034E 不一定触发),
	// 记录最近查看详情的 npc_logic_id,供捕捉成功后 clearFlowerDetail 清理。
	if d.NpcLogicID != 0 {
		p.acct(acc).lastFlowerLogicID = d.NpcLogicID
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
		f.GlassValue = d.GlassValue
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
	p.setFlowers(acc, items)
}
