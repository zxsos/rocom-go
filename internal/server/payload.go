package server

// 本文件定义实时推送(SSE)与快照接口的载荷类型。
//
// 背景:这些载荷原先一律是 map[string]any —— 键是字符串字面量,拼错一个
// (如 allPets 写成 allpets)前端就读到 undefined,而 Go 编译照样通过,测试也发现不了。
// 这是全仓最脆的一处:四个高频实时接口(position / wildpets / home / flowers)
// 的字段全靠字面量约定。
//
// 收成 struct 后字段名受编译期保护,且 pipeline 与 server 共用同一份定义,
// 不再各写一遍(此前 contract_test.go 还得照 pipeline 的未导出结构再抄一份)。
//
// 放在 server 包而非新建包,是因为 pipeline 已经依赖 server(p.srv.SetLast*),
// 放这儿不引入新包也不产生循环。

// FlowerPayload 是花种(花灵 BOSS)分组。
//
// Cur / Worlds 是**后端内部**状态,不应对外下发:handleFlowers 必须把它们剥掉
// (见 api_map.go 的 handleFlowers),只有 /api/flowers/slots 才透传 Worlds。
// 收成 struct 后这个约定由字段名表达,改 handler 时不会漏。
type FlowerPayload struct {
	Account string       `json:"account"`
	Flowers []FlowerItem `json:"flowers"`
	Cur     string       `json:"cur"`    // 当前正在看的世界槽 key("self" / "owner:<uid>")
	Worlds  FlowerWorlds `json:"worlds"` // 各世界槽的存档
}

// flowerView 是 /api/flowers 的输出形状:只有 account/flowers。
//
// 刻意与 FlowerPayload 分开成两个类型,而不是在 handler 里过滤键 ——
// 「内部字段不外泄」由结构保证,漏字段在编译期就会暴露,而不是等线上发现。
type flowerView struct {
	Account string       `json:"account"`
	Flowers []FlowerItem `json:"flowers"`
}

// FlowerWorlds 是所有世界槽,key 为 "self" 或 "owner:<uid>"。
type FlowerWorlds map[string]*FlowerWorld

// FlowerWorld 是一个世界槽的花种存档。
type FlowerWorld struct {
	TS      int64        `json:"ts"`      // 该槽最近一次更新的 Unix 秒
	Flowers []FlowerItem `json:"flowers"` // 该世界的花种列表
}

// PositionPayload 是位置推送。
//
// 可选字段一律用**指针**而非 omitempty 值类型:u/v 是底图归一化坐标(0-1),
// 停在左上角就是合法的 0;若用 float64 + omitempty,0 会被误删,前端读不到该键。
// 这是此处最易踩的坑 —— 之前用 map 时键是「有就写、没有就不写」,语义正是「可空但有值」。
type PositionPayload struct {
	Account    string  `json:"account"`
	SceneResID int32   `json:"sceneResId"`
	SceneCfgID int32   `json:"sceneCfgId"`
	SceneName  string  `json:"sceneName"`
	Img        string  `json:"img"` // 底图文件名(家园按等级 <res>_<lv>);无底图为空串
	X          int32   `json:"x"`
	Y          int32   `json:"y"`
	Z          int32   `json:"z"`
	Heading    float64 `json:"heading"`   // 朝向角(度),0=世界+X(地图东/右),顺时针增
	Stop       bool    `json:"stop"`      // 是否静止(静止时无速度向量)
	Paintable  bool    `json:"paintable"` // 该场景能否涂地,前端据此显示图层开关
	Ts         int64   `json:"ts"`        // Unix 秒
	TsMs       int64   `json:"tsMs"`      // Unix 毫秒;前端判断缓存位置是否过期(过期则不外推)

	// 底图投影坐标:该场景无底图时为 nil(整组不发)。
	U *float64 `json:"u,omitempty"`
	V *float64 `json:"v,omitempty"`
	// 速度向量(归一化底图坐标/秒),供前端在两包之间逐帧外推;静止时不下发。
	VU *float64 `json:"vu,omitempty"`
	VV *float64 `json:"vv,omitempty"`
	// Path 是客户端沉默一段后补报的真实轨迹(前端沿它把箭头滑回真实路线)。
	// 持续操作时上报本就 0.1s 一包,轨迹为空或极短,不下发。
	Path []PositionPoint `json:"path,omitempty"`

	// LayerOnly 标记这是**分层更新**包而非位置包:前端只据此叠加/撤下切片图,
	// 不动位置锚点(其余坐标字段此时无意义)。
	LayerOnly bool `json:"layerOnly,omitempty"`

	// Layer 是分层地图的切片图载荷(玩家进入某层时下发),取值见 layerPayload。
	//
	// omitempty 是安全的:原实现里 buildPos 不带该键、分层更新则在离开时显式发 null,
	// 两种形态并存。而前端一律用 falsy 判断(p.layer ? … : ''、p.layer || null),
	// null 与缺席等价,故统一按「无则不下发」处理。
	Layer any `json:"layer,omitempty"`
}

// PositionPoint 是轨迹上的一个点(底图归一化坐标)。
type PositionPoint struct {
	U float64 `json:"u"`
	V float64 `json:"v"`
}

// WildPayload 是野生宠物标记推送(稀有通道 + 普通通道)。
type WildPayload struct {
	Account    string        `json:"account"`
	SceneResID int32         `json:"sceneResId"`
	Pets       []WildMark    `json:"pets"`    // 稀有标记:异色/炫彩/污染/奖牌四件套,带全量字段
	AllPets    []WildAllMark `json:"allPets"` // 普通野生宠(「全部野生」图层),只有名/头像/坐标

	// 以下两个是**管理员注入**专用的广播字段,仅出现在 SSE 推送里:
	// 快照接口(GET /api/wildpets)永远不带,因为注入标记合进快照时不设这两个值。
	Inject bool `json:"inject,omitempty"` // 本次推送含新注入的精灵
	// InjectRevoke 是被撤销的注入精灵 id,前端据此立即撤掉该标记(不必等下一次全量推送)。
	InjectRevoke string `json:"injectRevoke,omitempty"`
}

// WildMark 是一只稀有野生宠的标记。
type WildMark struct {
	ID     string   `json:"id"`            // actor_id;uint64 超出 JS 安全整数,用字符串
	Name   string   `json:"n"`             // 形态名(珀尔鼬…);表里查不到时为空
	Img    string   `json:"img,omitempty"` // 头像相对路径 HeadIcon/<n>.webp
	Kinds  []string `json:"kinds"`         // 命中的类别:colorful / shiny / pollution / big / small / high / low
	U      float64  `json:"u"`
	V      float64  `json:"v"`
	X      int32    `json:"x"`
	Y      int32    `json:"y"`
	Z      int32    `json:"z"`
	Lv     int32    `json:"lv,omitempty"`
	Voice  int32    `json:"voice"`
	Height int32    `json:"height,omitempty"`
	Weight int32    `json:"weight,omitempty"`
	// 体重在本形态取值范围内的百分位(0-100),与宠物列表/事件页的「W xx%」同一口径
	// (pet.SizePercentile);形态范围缺失时为 nil。
	WeightPct *float64 `json:"weightPct,omitempty"`
	GlassType int32    `json:"glassType,omitempty"` // 炫彩类型(1=普通 / 2=隐藏;0=无炫彩)
	Glass     string   `json:"glass,omitempty"`     // 炫彩外观描述(暗夜拾光 / 四角星·亮X暗 - 浅紫橙);空=非炫彩
	// 炫彩数值(普通=(粒子id<<20)|配色id;隐藏=1/2/3 赛季、1000);前端据此渲染色卡
	GlassValue int32 `json:"glassValue,omitempty"`
	Mutation   int32 `json:"mutation,omitempty"` // 原始 mutation_type 位标志(排查用)
	Stale      bool  `json:"stale,omitempty"`    // 已离开 AOI:位置是最后所见,前端置灰
	// Inject 标记这是管理员投放的假精灵(非游戏真实流量),前端据此显示撤销按钮与视觉提示。
	// 仅出现在注入的标记上。
	Inject bool `json:"inject,omitempty"`
}

// WildAllMark 是一只普通野生宠的标记(全量通道之外)。
type WildAllMark struct {
	ID    string  `json:"id"`
	Name  string  `json:"n,omitempty"`   // 形态名(查表失败为空)
	Img   string  `json:"img,omitempty"` // 头像相对路径 HeadIcon/<n>.webp
	U     float64 `json:"u"`
	V     float64 `json:"v"`
	Stale bool    `json:"stale,omitempty"`
}

// HomePayload 是家园小窝图层。
type HomePayload struct {
	Account string     `json:"account"`
	Nests   []NestMark `json:"nests"` // 不在家园时为空数组(不是 null)

	// Meta 是「玩家确实在家园」时才有的四个字段,**四个同进同退**:
	// 不在家园(pipeline/home.go 的早退分支)时整体缺席,在家园时四个都下发。
	//
	// 用内嵌指针而非四个 omitempty 字段是有意的:原先是 map,这四个键在早退分支
	// 根本不写、在家园分支则无条件写(哪怕值为 0/false)。若改成四个
	// `omitempty` 字段,零值会被省掉 —— 比如 couplesStale=false 就从响应里消失了,
	// 与改造前不等价。内嵌指针既让 JSON 保持扁平(四个键仍平级),
	// 又保证「要么全在、要么全不在」。
	//
	// 这个差异是阶段 3 实测抓到的:对比改造前后的 SSE 输出,home 少了 couplesStale
	// —— 而 golden 没报警,因为它是在改动后重新生成的,没有基线可比对。
	*HomeMeta `json:",omitempty"`
}

// HomeMeta 是家园的元信息,见 HomePayload.Meta 的说明。
type HomeMeta struct {
	SceneResID   int32  `json:"sceneResId"`   // 家园场景资源 id
	Level        uint32 `json:"level"`        // 家园等级(决定底图取 <res>_<lv>)
	RoomLevel    uint32 `json:"roomLevel"`    // 房间等级
	CouplesStale bool   `json:"couplesStale"` // 配对信息是否已过期
}

// NestMark 是一个精灵小窝。
type NestMark struct {
	ID   string  `json:"id"` // furniture_guid(uint64 超出 JS 安全整数,用字符串)
	U    float64 `json:"u"`
	V    float64 `json:"v"`
	X    int32   `json:"x"`
	Y    int32   `json:"y"`
	Name string  `json:"name"` // 家具名(精灵小窝)
	// Pet 为空即空窝。Pet 与 Egg 互斥。
	Pet *NestPet `json:"pet,omitempty"`
	Egg *NestEgg `json:"egg,omitempty"`
}

// NestPet 是窝里那只宠物的简要信息(悬浮显示;点击看详情走 /api/pets/{gid})。
type NestPet struct {
	Gid       uint32   `json:"gid"`
	Name      string   `json:"name"`
	Species   string   `json:"species,omitempty"`
	Img       string   `json:"img,omitempty"`
	Gender    string   `json:"gender,omitempty"`
	Level     uint32   `json:"level,omitempty"`
	HeightM   float64  `json:"heightM,omitempty"`
	WeightKg  float64  `json:"weightKg,omitempty"`
	HeightPct *float64 `json:"heightPct,omitempty"`
	WeightPct *float64 `json:"weightPct,omitempty"`
	Voice     int32    `json:"voice"`
	Nature    string   `json:"nature,omitempty"`
	Talent    string   `json:"talentRank,omitempty"`
	FeedRound uint32   `json:"feedRound,omitempty"`
	// Mates 是与它配对的另一半(母本看到候选父本,父本看到它配的母本);多于一个即串窝。
	Mates []NestMate `json:"mates,omitempty"`
}

// NestMate 是配对里的另一半(只给名字与 gid,详情点开另一个窝即可)。
type NestMate struct {
	Gid  uint32 `json:"gid"`
	Name string `json:"name"`
}

// NestEgg 是趴在窝上、还没收的蛋。
type NestEgg struct {
	ItemID uint32 `json:"itemId"`
	Name   string `json:"name"`
	Icon   string `json:"icon,omitempty"`
}
