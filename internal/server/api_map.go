package server

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// posFresh 是缓存位置仍可用于外推的时限(客户端移动中最长 3s 一个心跳包,留些余量)。
const posFresh = 4 * time.Second

// SetLastPosition 缓存某账号最近一次实时位置(由抓包消费循环在广播 position 时调用),
// 供实时地图页加载时经 GET /api/position 即时回显,不必等玩家下一次移动。
func (s *Server) SetLastPosition(account string, pos *PositionPayload) {
	if account == "" {
		return
	}
	s.snap.setPos(account, pos)
}

// GetLastPosition 返回某账号缓存的最近一次位置;无记录返回 nil。
//
// 与 GetLastFlowers 对称:管线读回缓存后在其上做增量修改(如分层更新合并回位置载荷),
// 另供测试直接断言推送出的载荷。调用方**不得**原地修改返回值 —— 它是共享快照。
func (s *Server) GetLastPosition(account string) *PositionPayload {
	return s.snap.getPos(account)
}

// handlePosition 返回当前账号最近一次位置(实时地图页初始定位);无记录返回 null。
// 缓存位置可能已是很久以前的(玩家早已停下/离线),前端据 vu/vv 外推会一路飘走,
// 故过期(超过 posFresh)就抹掉速度:页面加载先静态回显,下一个移动包到达后自然接管。
func (s *Server) handlePosition(w http.ResponseWriter, r *http.Request) {
	acc := s.acct(r)
	pos := s.snap.getPos(acc)
	if pos != nil && time.Since(time.UnixMilli(pos.TsMs)) > posFresh {
		stale := *pos // 浅拷贝:下面几个字段置 nil 不会影响原快照
		stale.VU = nil
		stale.VV = nil
		stale.Path = nil // 陈旧轨迹不该再回放一遍
		pos = &stale
	}
	writeJSON(w, pos) // pos 为 nil 时输出 null
}

// SetLastWildPets 缓存某账号最近一次野生宠物标记(由消费管线在广播 wildpets 时调用),
// 供实时地图页加载时经 GET /api/wildpets 即时回显。
func (s *Server) SetLastWildPets(account string, payload *WildPayload) {
	if account == "" {
		return
	}
	s.snap.setWild(account, payload)
}

// GetLastWildPets 返回某账号缓存的最近一次野生宠标记;无记录返回 nil。
//
// 与 GetLastPosition 对称:wildpets 是**广播**而非请求-响应,测试够不着它;而广播的
// 内容正是这份缓存(SetLastWildPets 与 Broadcast 在同一处调用),故读它就等于断言
// 推出去的载荷。调用方**不得**原地修改返回值 —— 它是共享快照。
func (s *Server) GetLastWildPets(account string) *WildPayload {
	return s.snap.getWild(account)
}

// handleWildPets 返回当前账号最近一次野生宠物标记(异色/炫彩、污染、满声音);无记录返回 null。
// 与位置不同,这里不做过期抹除:标记本身已带 stale 标志(实体离开 AOI 后由管线置位并限时保留)。
func (s *Server) handleWildPets(w http.ResponseWriter, r *http.Request) {
	v := s.snap.getWild(s.acct(r))
	writeJSON(w, v)
}

// SetWildsClearer 由消费管线注册「清空野生宠标记」的回调。
//
// 为什么要绕这一层:野生宠的观测态(connState.wilds)归 **pipeline** 持有,server 够不着;
// 而 pipeline 已经依赖 server(pipeline.New 收 *Server),若 server 再 import pipeline 就是
// 循环引用。故用函数回调把反向调用注入进来 —— 与涂地不同,涂地状态在 server 里
// (s.paint),可以直接清(见 handlePaintReset)。
func (s *Server) SetWildsClearer(fn func(account string)) {
	s.wildsMu.Lock()
	s.wildsClearer = fn
	s.wildsMu.Unlock()
}

// handleWildPetsClear 主动清空当前账号的野生宠标记(用户点「清空」时用)。
//
// 与「换场景/传送」是两回事:那是管线自动做的、且**只置灰不删**(见 pipeline.resetWilds),
// 这是用户**明确要求**抹掉 —— 灰点也一并清掉。这是唯一会真正删除标记的入口。
func (s *Server) handleWildPetsClear(w http.ResponseWriter, r *http.Request) {
	acc := s.acct(r)
	s.wildsMu.Lock()
	fn := s.wildsClearer
	s.wildsMu.Unlock()
	if fn != nil {
		fn(acc)
	}
	writeJSON(w, map[string]any{"ok": true})
}

// SetLastHome 缓存某账号最近一次家园小窝图层(由消费管线在广播 home 时调用),
// 供实时地图页加载时经 GET /api/home 即时回显。玩家不在家园时缓存的是空列表。
func (s *Server) SetLastHome(account string, payload *HomePayload) {
	if account == "" {
		return
	}
	s.snap.setHome(account, payload)
}

// handleHome 返回当前账号最近一次家园小窝图层(空窝也在内);无记录返回 null。
func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	v := s.snap.getHome(s.acct(r))
	writeJSON(w, v)
}

// SetLastFlowers 缓存某账号最近一次花种(花灵)BOSS 分组(由消费管线在广播 flowers 时调用),
// 供花种页加载时经 GET /api/flowers 即时回显。字段定义见 pipeline/boss.go 的 flowerItem。
func (s *Server) SetLastFlowers(account string, payload *FlowerPayload) {
	if account == "" {
		return
	}
	s.snap.setFlower(account, payload)
}

// GetLastFlowers 返回某账号最近一次花种分组(与 SetLastFlowers 对应);无记录返回 nil。
// 供管线在收到 0x0338 详情时读取当前分组以合并字段。
func (s *Server) GetLastFlowers(account string) *FlowerPayload {
	if account == "" {
		return nil
	}
	return s.snap.getFlower(account)
}

// handleFlowers 返回当前账号最近一次花种(花灵)BOSS 分组;无记录(尚未收到 0x0375)返回 null。
// 内部字段(cur/worlds:世界存档表,见 pipeline/boss.go)对前端隐藏,只透传 account/flowers。
func (s *Server) handleFlowers(w http.ResponseWriter, r *http.Request) {
	// 转出专用类型,由结构保证 cur/worlds 不外泄(原先靠运行时过滤键,漏一个就泄了)
	v := s.snap.getFlower(s.acct(r))
	if v == nil {
		writeJSON(w, nil)
		return
	}
	writeJSON(w, flowerView{Account: v.Account, Flowers: v.Flowers})
}

// flowerSlotOut 是一个花种世界存档槽(槽位管理用,见 pipeline/boss.go 的 worlds 表):
// key 为 "self"(自己世界)或 "owner:<uid>"(好友世界,归属者 user_id),值 {"flowers","ts"}。
type flowerSlotOut struct {
	Key     string `json:"key"`     // 槽键:"self" 或 "owner:<uid>"
	Name    string `json:"name"`    // 展示名:"自己世界" 或 "好友 UID:<uid>"
	Ts      int64  `json:"ts"`      // 最近访问 Unix 秒
	Flowers any    `json:"flowers"` // 存储的花种列表(flowerItem,含 0x0338 详情)
}

// flowerSlotName 生成槽的展示名:self=自己世界,owner:<uid>=好友 UID:<uid>。
func flowerSlotName(key string) string {
	if key == "self" {
		return "自己世界"
	}
	if uid, ok := strings.CutPrefix(key, "owner:"); ok {
		return "好友 UID:" + uid
	}
	return key
}

// handleFlowerSlots 返回当前账号的花种世界存档槽位列表(槽位管理下拉用):
// self 槽在前,好友槽按归属者 uid 升序。仅透传存档数据,不修改。
func (s *Server) handleFlowerSlots(w http.ResponseWriter, r *http.Request) {
	v := s.snap.getFlower(s.acct(r))
	out := []flowerSlotOut{}
	if v != nil {
		for k, w := range v.Worlds {
			slot := flowerSlotOut{Key: k, Name: flowerSlotName(k)}
			if w != nil {
				slot.Ts, slot.Flowers = w.TS, w.Flowers
			}
			out = append(out, slot)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if (out[i].Key == "self") != (out[j].Key == "self") {
			return out[i].Key == "self"
		}
		return out[i].Key < out[j].Key
	})
	writeJSON(w, map[string]any{"slots": out})
}

// handleDeleteFlowerSlot 删除一个好友世界存档槽(?key=),self 槽不可删(自己世界,删了下次推送即重建)。
// 删除后更新缓存;若删的是当前槽(cur),cur 置空。不广播——槽位只影响管理页,当前花种列表不变。
func (s *Server) handleDeleteFlowerSlot(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	if key == "" {
		http.Error(w, "bad key", http.StatusBadRequest)
		return
	}
	if key == "self" {
		http.Error(w, "self 槽不可删除", http.StatusBadRequest)
		return
	}
	acc := s.acct(r)
	// 读-改-写由 snapshotStore 在锁内完成(见 dropFlowerWorld),外部拿不到 flowerMu
	if !s.snap.dropFlowerWorld(acc, key) {
		http.Error(w, "槽不存在", http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// poiKind 是一个 POI 图层(前端的一个开关):图层键、中文名、图标路径、是否默认开启。
type poiKind struct {
	K       string `json:"k"`
	N       string `json:"n"`
	Icon    string `json:"icon"`    // /img/<此路径>
	On      bool   `json:"on"`      // 默认开启(魔力之源、炼金釜)
	Num     int    `json:"num"`     // 本场景该图层的点数(前端显示,0 则该层置灰)
	Collect bool   `json:"collect"` // 可收集图层(眠枭之星/不咕钟零件):前端「收集模式」覆盖这些层
}

// poiPoint 是一个 POI 标记:底图归一化坐标(与玩家位置同一投影)+ 名称。
// 眠枭之星另带刷新点 id、候选区域与收集状态(见 docs/data.md 3.4)。
type poiPoint struct {
	K    string  `json:"k"`
	U    float64 `json:"u"`
	V    float64 `json:"v"`
	N    string  `json:"n"`
	I    string  `json:"i,omitempty"`    // 图标路径(仅采集物:每点自带品种图标,其余图层共用图层图标)
	R    int32   `json:"r,omitempty"`    // 刷新点 id(星星:前端据此接收状态增量)
	Zone []int32 `json:"zone,omitempty"` // 候选区域营地 id 列表;全部收满才可隐藏(重叠带语义见 docs/data.md 3.4)
	St   int     `json:"st,omitempty"`   // 收集状态:0 未确认 / 1 未收集 / 2 已收集
}

// zoneProgress 是某区域的眠枭之星收集进度(服务器口径,合并同区域的独立星/光点/石像)。
type zoneProgress struct {
	Camp int32  `json:"camp"` // 区域键(营地 id),与 poiPoint.Zone 的元素对应
	Name string `json:"name"` // 区域名(商店街周边…)
	Got  int32  `json:"got"`
	Tot  int32  `json:"tot"`
}

// handlePois 返回某场景(?res=scene_res_cfg_id)的大地图 POI:图层清单 + 已投影到底图归一化坐标
// 的标记点。投影复用 db.Project(与玩家位置同一套),故前端不需要知道 ox/oy/side。
// 无底图的场景没有 POI(投影无从谈起),返回空列表。数据来源见 docs/data.md 3.3。
func (s *Server) handlePois(w http.ResponseWriter, r *http.Request) {
	res64, err := strconv.ParseUint(r.URL.Query().Get("res"), 10, 32)
	if err != nil {
		http.Error(w, "bad res", http.StatusBadRequest)
		return
	}
	res := uint32(res64)
	acc := s.acct(r)
	states := s.store.StarStates(acc) // 刷新点 -> 收集状态(逐点确认的结果)
	pts := []poiPoint{}
	num := map[string]int{}
	for _, p := range s.db.POIs(res) {
		u, v, ok := s.db.Project(res, p.X, p.Y)
		if !ok {
			continue
		}
		pt := poiPoint{K: p.K, U: u, V: v, N: p.N}
		if p.I != "" { // 采集物:点位自带品种图标,未 embed 时空串(前端回退到图层图标)
			pt.I = s.db.POIIconOf(p.I)
		}
		if s.db.CollectibleKind(p.K) {
			pt.R, pt.Zone, pt.St = p.R, p.Zone, states[p.R]
		}
		pts = append(pts, pt)
		num[p.K]++
	}
	kinds := []poiKind{}
	for _, k := range s.db.POIKinds() {
		kinds = append(kinds, poiKind{K: k.K, N: k.N, Icon: s.db.POIIcon(k), On: k.On, Num: num[k.K], Collect: k.Collect})
	}
	// 按区域的收集进度(服务器口径):点的候选区域全部收满 ⇒ 可隐藏(见 poiPoint.Z)。
	agg := map[int32]*zoneProgress{}
	for _, z := range s.store.StarZones(acc) {
		e := agg[z.Camp]
		if e == nil {
			e = &zoneProgress{Camp: z.Camp, Name: s.db.ZoneName(z.Camp)}
			agg[z.Camp] = e
		}
		e.Got += z.Got
		e.Tot += z.Total
	}
	zones := []zoneProgress{}
	for _, z := range agg {
		zones = append(zones, *z)
	}
	sort.Slice(zones, func(i, j int) bool { return zones[i].Camp < zones[j].Camp })
	writeJSON(w, map[string]any{"kinds": kinds, "pois": pts, "zones": zones})
}
