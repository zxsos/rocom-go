package server

import "time"

// FlowerItem 是一只花种(花灵)BOSS 的展示信息:花种页卡片按此渲染。
// 面板 0x0375 整组下发基础字段;玩家点击地图花种后的 0x0338 详情(等级/炫彩/绑定宠物/奖牌)合并进来。
// 定义在 server 包(而非 pipeline):pipeline 通过类型别名引用(见 pipeline/boss.go),
// 使管理员面板(server 包)能在不引入 pipeline 的前提下构造/读取花种数据。
type FlowerItem struct {
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
	Lv         uint32 `json:"lv"`         // 等级
	GlassType  int32  `json:"glassType"`  // 炫彩类型(0=无炫彩 / 1=普通 / 2=隐藏;仅在 detail=true 时有效)
	Glass      string `json:"glass"`      // 炫彩中文描述(GlassDesc;空=无炫彩或未获取)
	GlassValue int32  `json:"glassValue"` // 炫彩数值(普通=(粒子id<<20)|配色id;隐藏=1/2/3 赛季、1000;前端据此渲染色卡)
	BindName   string `json:"bindName"`   // 绑定守护宠物名(空=无绑定)
	BindImg    string `json:"bindImg"`    // 绑定守护宠物头像 /img/<此路径>
	BindEvo    uint32 `json:"bindEvo"`    // 绑定宠物进化阶段 id(0=无)
	MedalName  string `json:"medalName"`  // 绑定宠物佩戴的奖牌名(空=无)
	MedalIcon  string `json:"medalIcon"`  // 奖牌小图 /img/<此路径>
}

// InjectFlowerItem 向某账号「当前花种分组」头部注入一只花种(管理员投放的假炫彩花种),
// 同步写入自己世界存档槽,并广播 flowers 让花种页立即显示。不改游戏真实流量,
// 仅操作 server 缓存的最近分组(与 pipeline 共用同一 payload 缓存,字段类型一致)。
func (s *Server) InjectFlowerItem(account string, f FlowerItem) {
	payload, _ := s.GetLastFlowers(account).(map[string]any)
	if payload == nil {
		payload = map[string]any{"account": account, "flowers": []FlowerItem{}, "cur": "self", "worlds": map[string]any{}}
	}
	flowers, _ := payload["flowers"].([]FlowerItem)
	// 复制一份再改,避免改动缓存里 pipeline 持有的共享切片。
	flowers = append([]FlowerItem{f}, flowers...)
	payload["flowers"] = flowers

	// 同步自己世界槽:管理页切到「自己世界」槽位时数据一致;槽不存在则创建。
	worlds, _ := payload["worlds"].(map[string]any)
	if worlds == nil {
		worlds = map[string]any{}
		payload["worlds"] = worlds
	}
	if self, ok := worlds["self"].(map[string]any); ok {
		slotFlowers, _ := self["flowers"].([]FlowerItem)
		self["flowers"] = append([]FlowerItem{f}, slotFlowers...)
		self["ts"] = time.Now().Unix()
	} else {
		worlds["self"] = map[string]any{"flowers": []FlowerItem{f}, "ts": time.Now().Unix()}
	}
	if cur, _ := payload["cur"].(string); cur == "" {
		payload["cur"] = "self"
	}
	s.SetLastFlowers(account, payload)
	s.hub.Broadcast("flowers", account, payload)
}

// RemoveFlowerItem 从某账号「当前花种分组」删除指定 npc_logic_id 的花种(撤销投放),
// 同步从自己世界槽中删除,并广播 flowers。返回 false 表示该花种已不在分组里。
func (s *Server) RemoveFlowerItem(account string, logicID uint64) bool {
	payload, _ := s.GetLastFlowers(account).(map[string]any)
	if payload == nil {
		return false
	}
	flowers, ok := payload["flowers"].([]FlowerItem)
	if !ok {
		return false
	}
	out := make([]FlowerItem, 0, len(flowers))
	for _, f := range flowers {
		if f.NpcLogicID == logicID {
			continue
		}
		out = append(out, f)
	}
	if len(out) == len(flowers) {
		return false
	}
	payload["flowers"] = out
	if worlds, ok := payload["worlds"].(map[string]any); ok {
		if self, ok := worlds["self"].(map[string]any); ok {
			slotFlowers, _ := self["flowers"].([]FlowerItem)
			so := make([]FlowerItem, 0, len(slotFlowers))
			for _, f := range slotFlowers {
				if f.NpcLogicID != logicID {
					so = append(so, f)
				}
			}
			self["flowers"] = so
			self["ts"] = time.Now().Unix()
		}
	}
	s.SetLastFlowers(account, payload)
	s.hub.Broadcast("flowers", account, payload)
	return true
}
