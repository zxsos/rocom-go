package gamedata

import "strconv"

// 场景与大地图(实时地图页)的查找表,见 docs/data.md 3.1、3.2。

// sceneRes 是一个场景资源(scene_res_cfg_id)的名称与所属场景(scene_cfg_id)。
type sceneRes struct {
	N string `json:"n"`
	S int32  `json:"s"`
}

// MapInfo 是一张大地图底图的投影参数(SCENE_RES 世界坐标 → 底图归一化坐标)。
// 底图 webp 路径为 bigmap/<scene_res_cfg_id>.webp(家园室内 30001 为 30001_<房屋等级>)。
type MapInfo struct {
	Name  string `json:"name"`  // 地图名(卡洛西亚大陆…)
	OX    int32  `json:"ox"`    // 底图左上角世界坐标 X(= 地块中心X - 边长/2)
	OY    int32  `json:"oy"`    // 底图左上角世界坐标 Y
	Side  int32  `json:"side"`  // 地块世界边长(厘米);u=(x-ox)/side, v=(y-oy)/side
	World bool   `json:"world"` // 大世界(底图 4096²)否则家园场景(2048²)
	Rooms int    `json:"rooms"` // >0 表示按房屋等级分层(家园室内 30001,底图 30001_<lv>)
}

// LayerInfo 是一个分层地图(洞穴/地下层/家园楼层)的切片图与投影(见 docs/data.md 3.2)。
// 与所属场景同坐标系,进入该层时把切片叠加到底图对应位置。切片 webp 路径为 bigmap/layer/<Img>.webp。
//
// 「当前在哪一层」由服务器的区域进/出事件决定(scene.ParseAreaActs):玩家所在区域的
// area_func_id 命中本层的 AreaFunc 即在本层。不能用位置点对区域多边形做 2D 判定——那样在洞穴
// 正上方的地表也会命中(多边形只有 x/y,分不清人在洞里还是在洞顶)。
type LayerInfo struct {
	ID       uint32 // 层 id(LAYERED_WORLD_MAP_CONF.id)
	Name     string // 层名(信仰者村落一层…)
	Group    int32  // 层组;同组共享地表底图,组内多个楼层
	Res      int32  // 所属 scene_res_cfg_id(家园层为 0)
	Img      string // 切片图文件名(bigmap/layer/<Img>.webp)
	OX       int32  // 层投影左上角世界坐标 X(= camera_center.x - Ortho_width/2)
	OY       int32  // 层投影左上角世界坐标 Y
	Side     int32  // 层投影世界边长(= Ortho_width);切片在底图上的矩形据此算
	AreaFunc uint32 // 本层对应的 area_func_id(LAYERED_WORLD_MAP_CONF.area_func_id)
}

// POIKind 是一类大地图 POI(实时地图页的一个可开关图层),取自 names.json 的 poi_kinds。
type POIKind struct {
	K    string `json:"k"`    // 图层键(alchemy/mana/…),与 POI.K 对应
	N    string `json:"n"`    // 中文名(炼金釜/魔力之源…)
	Icon string `json:"icon"` // 图标原始文件名;Go 侧拼 worldmap/<原名>.webp
	On   bool   `json:"on"`   // 默认开启(魔力之源、炼金釜)
}

// POI 是一个大地图标记点(世界坐标,厘米)。名称取自 WORLD_MAP_CONF.element_text_name
// (如「月牙湖岸的魔力之源」),无名时退到图层名。坐标来源与提取见 docs/data.md 3.3。
type POI struct {
	K string  `json:"k"` // 所属图层键
	R int32   `json:"r"` // 刷新点 id(NPC_REFRESH_CONTENT_CONF.id);服务器下发的 NPC 实体带同一个 id
	X int32   `json:"x"` // 世界坐标 X(厘米)
	Y int32   `json:"y"` // 世界坐标 Y
	N string  `json:"n"` // 名称(悬停显示)
	Z []int32 `json:"z"` // 候选区域营地 id 列表(仅眠枭之星;管辖区重叠带上的点会有多个,见 docs/data.md 3.4)
}

// SceneName 返回场景名(scene_cfg_id → SCENE_CONF.scene_name)。
func (db *DB) SceneName(cfgID int32) string {
	return db.scenes[strconv.FormatInt(int64(cfgID), 10)]
}

// SceneResName 返回场景资源名(scene_res_cfg_id → SCENE_RES_CONF)。
func (db *DB) SceneResName(resID int32) string {
	return db.sceneRes[strconv.FormatInt(int64(resID), 10)].N
}

// DefaultSceneRes 返回某 scene_cfg_id 的默认 scene_res_id(SCENE_CONF 主行);无则 0。
// 当无法从进入/传送通知得知精确 res 时(中途开抓/无缓存),据此兜底定位底图。
func (db *DB) DefaultSceneRes(cfgID int32) int32 {
	return db.sceneDefRes[strconv.FormatInt(int64(cfgID), 10)]
}

// MapInfo 返回某 scene_res_cfg_id 的大地图投影参数;第二返回值表示该场景是否有底图。
func (db *DB) MapInfo(resID uint32) (MapInfo, bool) { m, ok := db.maps[resID]; return m, ok }

// MapImage 返回某场景底图的 webp 文件名(不含扩展名),前端拼 /img/bigmap/<名>.webp;无底图返回 ""。
// 家园室内(Rooms>0)按房屋等级分图 <res>_<level>(level 越界则夹取,未知按 1);其余场景为 <res>。
func (db *DB) MapImage(resID uint32, room int32) string {
	m, ok := db.maps[resID]
	if !ok {
		return ""
	}
	if m.Rooms > 0 {
		if room < 1 {
			room = 1
		}
		if int(room) > m.Rooms {
			room = int32(m.Rooms)
		}
		return strconv.FormatInt(int64(resID), 10) + "_" + strconv.FormatInt(int64(room), 10)
	}
	return strconv.FormatInt(int64(resID), 10)
}

// Project 把场景世界坐标(厘米)投影为底图归一化坐标 u,v∈[0,1](复刻客户端
// BigMapUtils.ScenePosToImagePosF)。该 scene_res 无底图时 ok=false。u,v 可能越界
// [0,1](角色在底图覆盖范围外,如迷雾区),调用方自行决定是否裁剪。
func (db *DB) Project(resID uint32, x, y int32) (u, v float64, ok bool) {
	m, ok := db.maps[resID]
	if !ok || m.Side == 0 {
		return 0, 0, false
	}
	return float64(x-m.OX) / float64(m.Side), float64(y-m.OY) / float64(m.Side), true
}

// LayerIn 返回玩家当前所在的分层地图:activeFuncs 是服务器区域进/出事件维护的「玩家当前所在
// 区域的 area_func_id 集合」(见 scene.ParseAreaActs),命中本场景(res)某层的 AreaFunc 即在该层。
// 不在任何层返回 ok=false(显示地表底图)。
//
// 复刻客户端 BigMapModuleData:GetCurMapLayerId(它同样是拿玩家所在 zone 的 area_func_id 查分层表)。
// 早前用「位置点在区域多边形内」近似,会在洞穴正上方的地表误叠洞穴图——多边形只有 x/y,
// 而区域触发体是 3D 的,分不清人在洞里还是在洞顶。见 docs/data.md 3.2。
func (db *DB) LayerIn(res int32, activeFuncs map[uint32]bool) (LayerInfo, bool) {
	if len(activeFuncs) == 0 {
		return LayerInfo{}, false
	}
	for _, l := range db.layers {
		if l.Res == res && l.AreaFunc != 0 && activeFuncs[l.AreaFunc] {
			return l, true
		}
	}
	return LayerInfo{}, false
}

// POIKinds 返回大地图 POI 图层清单(有序:魔力之源、炼金釜、守护地、庇护所、眠枭之星)。
func (db *DB) POIKinds() []POIKind { return db.poiKinds }

// POIs 返回某场景的全部 POI(世界坐标);无底图的场景不收录,返回 nil。
func (db *DB) POIs(resID uint32) []POI { return db.pois[resID] }

// ZoneName 返回区域名(键为该区域营地(魔力之源)的刷新点 id,即服务器收集进度里的区域键)。
func (db *DB) ZoneName(camp int32) string { return db.zones[key(uint32(camp))] }
