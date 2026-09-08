package pipeline

import (
	"log"
	"reflect"
	"sort"
	"strconv"

	"github.com/whoisnian/rocom-capture/internal/capture"
	"github.com/whoisnian/rocom-capture/internal/scene"
	"github.com/whoisnian/rocom-capture/internal/server"
	"github.com/whoisnian/rocom-capture/internal/store"
)

// flowerItem 是一只花种(花灵)BOSS 的展示信息:花种页卡片按此渲染。
// 面板 0x0375 整组下发基础字段;玩家点击地图花种后的 0x0338 详情(等级/炫彩/绑定宠物/奖牌)合并进来。
// 类型本体定义在 server 包(管理员面板要在不引入 pipeline 的前提下构造/读取花种数据),
// 这里用类型别名保持本包内既有引用零改动。
type flowerItem = server.FlowerItem

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
func cloneWorlds(worlds server.FlowerWorlds) server.FlowerWorlds {
	nw := make(server.FlowerWorlds, len(worlds))
	for k, v := range worlds {
		nw[k] = v
	}
	return nw
}

// slotFlowers 读存档表某槽的列表与最近访问时间。
func slotFlowers(worlds server.FlowerWorlds, key string) ([]flowerItem, int64, bool) {
	m, ok := worlds[key]
	if !ok || m == nil {
		return nil, 0, false
	}
	return m.Flowers, m.TS, true
}

// trimFriendWorlds 好友槽超上限时按最近访问时间(ts 升序)淘汰最老的,self 槽不受影响。
func trimFriendWorlds(worlds server.FlowerWorlds) {
	type entry struct {
		key string
		ts  int64
	}
	var list []entry
	for k, v := range worlds {
		if k == "self" || v == nil {
			continue
		}
		list = append(list, entry{k, v.TS})
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
	payload := p.srv.GetLastFlowers(acc)
	var worlds server.FlowerWorlds
	var cur string
	if payload != nil {
		worlds, cur = payload.Worlds, payload.Cur
	}
	if worlds == nil {
		worlds = server.FlowerWorlds{}
	}
	worlds = cloneWorlds(worlds)
	if cur != "" {
		if m, ok := worlds[cur]; ok && m != nil {
			// 复制槽再改:worlds 是共享表,槽对象也可能被 HTTP 读取方持有
			ns := *m
			ns.Flowers = items
			worlds[cur] = &ns
		}
	}
	payload = &server.FlowerPayload{Account: acc, Flowers: items, Cur: cur, Worlds: worlds}
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

	// 花种活动结束处理:end_ts 已过的品种(活动结束)删除挑战计数,卡片清零、库中清理,
	// 下次新活动同品种出现时从 0 重新累计;未结束的品种刷新记录的活动结束时间
	// (供 sweepOnce 兜底清理——活动结束后花种从分组消失,0x0375 不再触发实时删除)。
	now := m.Time.Unix()
	for i := range items {
		it := &items[i]
		if it.EndTs == 0 {
			continue // 未设置结束时间,不参与活动判定
		}
		if int64(it.EndTs) < now {
			if err := p.st.For(acc).DeleteFlowerChallenge(it.ID, it.Blood); err != nil {
				log.Printf("DeleteFlowerChallenge 失败: %v", err)
			}
		} else {
			if err := p.st.For(acc).UpsertFlowerEndTs(it.ID, it.Blood, int64(it.EndTs)); err != nil {
				log.Printf("UpsertFlowerEndTs 失败: %v", err)
			}
		}
	}

	// 填充本账号累计挑战次数(按品种 npc_cfg_id+blood 持久化,花种消失仍保留):
	// 每次整组下发都刷新卡片上的真实累计值。
	if counts, err := p.st.For(acc).FlowerChallengeCounts(); err == nil {
		for i := range items {
			items[i].ChallengeCount = uint32(counts[store.FlowerChallengeKey{NpcCfgID: items[i].ID, Blood: items[i].Blood}])
		}
	}

	// 读缓存。
	payload := p.srv.GetLastFlowers(acc)
	var oldItems []flowerItem
	var oldCur string
	var worlds server.FlowerWorlds
	if payload != nil {
		oldItems, oldCur, worlds = payload.Flowers, payload.Cur, payload.Worlds
	}
	if worlds == nil {
		worlds = server.FlowerWorlds{}
	}

	// 空分组(花种全被采完):当前世界清空显示,但保留槽内 detail 不覆盖,等下次推送恢复。
	if len(items) == 0 {
		if len(oldItems) != 0 {
			np := &server.FlowerPayload{Account: acc, Flowers: []flowerItem{}, Cur: oldCur, Worlds: worlds}
			p.srv.SetLastFlowers(acc, np)
			p.srv.Hub().Broadcast("flowers", acc, np)
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
	worlds[targetKey] = &server.FlowerWorld{Flowers: items, TS: m.Time.Unix()}
	trimFriendWorlds(worlds)

	// 结果与当前完全一致则不刷新(退回自己世界且花未变时前端零感知)。
	if oldCur == targetKey && reflect.DeepEqual(items, oldItems) {
		return
	}
	np := &server.FlowerPayload{Account: acc, Flowers: items, Cur: targetKey, Worlds: worlds}
	p.srv.SetLastFlowers(acc, np)
	p.srv.Hub().Broadcast("flowers", acc, np)
}

// onSelectFlowerSeedBoss 记录 c2s 0x034E 选中的花种 npc_logic_id:
// 玩家点某朵花进战斗时发出,作为捕捉成功(0x132c catch_way=4)后清理详情的定位锚点,
// 同时给该花种品种累计一次挑战次数(见 countFlowerChallenge)。
func (p *Pipeline) onSelectFlowerSeedBoss(m capture.Message, acc string) {
	logicID := scene.ParseSelectFlowerSeedBossReq(m.AppBody)
	if logicID == 0 {
		return
	}
	p.acct(acc).lastFlowerLogicID = logicID
	p.countFlowerChallenge(acc, logicID)
}

// countFlowerChallenge 花种挑战计数:0x034E 只带 npc_logic_id,从当前分组反查品种
// (npc_cfg_id + blood),按品种累计落库(花种消失/刷新后计数保留,下次同品种花种出现
// 卡片继续显示)并广播更新卡片。分组里找不到该 logic_id(极罕见)则跳过,不影响链路。
func (p *Pipeline) countFlowerChallenge(acc string, logicID uint64) {
	// GetLastFlowers 已返回强类型,无需再做类型断言
	payload := p.srv.GetLastFlowers(acc)
	if payload == nil {
		return
	}
	items := payload.Flowers
	var cfgID, blood uint32
	idx := -1
	for i := range items {
		if items[i].NpcLogicID == logicID {
			cfgID, blood = items[i].ID, items[i].Blood
			idx = i
			break
		}
	}
	if idx < 0 {
		return
	}
	if err := p.st.For(acc).AddFlowerChallenge(cfgID, blood); err != nil {
		log.Printf("AddFlowerChallenge 失败: %v", err)
		return // 落库失败不更新内存,避免与库不一致
	}
	// 复制一份再改,避免直接动 server 缓存里的共享切片。
	items = append([]flowerItem(nil), items...)
	items[idx].ChallengeCount++
	p.setFlowers(acc, items)
}

// clearFlowerDetail 花种精灵捕捉成功(catch_way=4)后清理对应花种的 0x0338 详情:
// 捕捉后该花种重生为新的个体,旧详情(等级/炫彩/绑定/奖牌)不再有效,需玩家重新点击查看。
// 定位用最近一次 c2s 0x034E 选中的 npc_logic_id;清空详情字段后广播,前端恢复「未查看」。
func (p *Pipeline) clearFlowerDetail(acc string) {
	logicID := p.acct(acc).lastFlowerLogicID
	if logicID == 0 {
		return
	}
	// GetLastFlowers 已返回强类型,无需再做类型断言
	payload := p.srv.GetLastFlowers(acc)
	if payload == nil {
		return
	}
	items := payload.Flowers
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
	// GetLastFlowers 已返回强类型,无需再做类型断言
	payload := p.srv.GetLastFlowers(acc)
	if payload == nil {
		return // 还没收到过面板分组,无从合并
	}
	items := payload.Flowers
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
