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
	ID   string `json:"id"`            // actor_id;uint64 超出 JS 安全整数,用字符串
	Name string `json:"n"`             // 形态名(珀尔鼬…);表里查不到时为空
	Img  string `json:"img,omitempty"` // 头像相对路径 HeadIcon/<n>.webp
	// 形态编号(petbase id),用于「在 rkpet 看 3D 效果」的外链 —— 大地图与宠物列表
	// 的色卡弹出预览里那条按钮靠它拼 URL,没有它按钮就不显示。
	//
	// ⚠️ 不要从 Img 的 HeadIcon/<n>.webp 反推这个号 —— 那只是图片**文件名**,与形态
	// 编号之间没有换算关系(多个形态常共用同一张素材):实测 names.json 的 images 里
	// 1112 个形态中 461 个的 h 与形态编号不同(如 3242 的图是 3012),220 个有异色头像
	// 的里 75 个 sh 不以形态编号开头。反推会静默地把外链指向别的宠物,页面却一切正常。
	BaseConfID uint32 `json:"baseConfId,omitempty"`
	// 是否异色。rkpet 外链靠它加 shiny=1,否则 3D 模型是普通配色 —— 而异色炫彩恰恰
	// 是炫彩里最常见的情况,缺了它链接就指向错配色。与 PetData.Shiny 同一口径。
	//
	// ⚠️ 与那边一样:前端**不可**拿 Img 反推异色(多个形态共用素材,普通图与异色图
	// 文件名没有可靠对应关系,见 BaseConfID 的注释),只看这个字段。
	Shiny  bool     `json:"shiny,omitempty"`
	Kinds  []string `json:"kinds"` // 命中的类别:colorful / shiny / pollution / big / small / high / low
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

// TrialPayload 是草系徽章试炼的实时状态(见 internal/trial 与 docs/pcap-20260831-grass-trial.md)。
//
// Active 恒在下发(是否正在进行一局);Run 只在握有某一局的数据时出现(结束后仍保留,
// 带 Result);History 只在收到过 0x1975 账号档案后出现。三者都由指针表达「有没有」,
// 不用 omitempty 值类型 —— 同 HomePayload.Meta 的理由:零值该出现时不能消失。
type TrialPayload struct {
	Account string        `json:"account"`
	Ts      int64         `json:"ts"`     // 本份快照的 Unix 秒
	Active  bool          `json:"active"` // 是否正在进行一局
	Run     *TrialRun     `json:"run,omitempty"`
	History *TrialHistory `json:"history,omitempty"`
}

// TrialRun 是一局的镜像。
type TrialRun struct {
	TrialID     uint32          `json:"trialId"`
	SlotID      uint32          `json:"slotId"` // 属性系图鉴槽位(1000=普、1001=草…)
	SlotName    string          `json:"slotName,omitempty"`
	ChapterID   uint32          `json:"chapterId"`  // 服务器下发(3000/3001/3002)
	ChapterIdx  uint32          `json:"chapterIdx"` // 第几章(1 起),由 chapters 次序推出
	NodeIndex   uint32          `json:"nodeIndex"`  // 本章第几个节点(0 起)
	Coin        uint32          `json:"coin"`
	Chapters    []uint32        `json:"chapters,omitempty"`
	Effects     []uint32        `json:"effects,omitempty"` // 本局生效的试炼词条
	Boss        bool            `json:"boss,omitempty"`    // 已进 BOSS 战
	Pet         *TrialPet       `json:"pet,omitempty"`
	Options     []TrialOption   `json:"options,omitempty"` // 当前节点的候选事件
	RefreshCost uint32          `json:"refreshCost,omitempty"`
	Bless       *TrialBless     `json:"bless,omitempty"`
	Reward      *TrialReward    `json:"reward,omitempty"` // 待处理的节点奖励
	Shop        []TrialShopItem `json:"shop,omitempty"`
	Result      *TrialResult    `json:"result,omitempty"` // 上一局结算(Active=false 时)
	Log         []TrialLogEntry `json:"log,omitempty"`    // 操作流水(最新在前)

	// 以下三项来自**静态配置**(wiki,见 gamedata/trial.go),协议不发这些:
	//   Floor      当前节点是什么(普通/首领/商人/NPC…)
	//   ChapterName 章节名(如「记忆中的索米亚草原」)
	//   Opponents  第 7 层的候选 NPC 阵容;其余层为 nil
	// 静态配置缺失时(如数据没生成)两项为空、Opponents 为 nil,不是错误。
	Floor       string          `json:"floor,omitempty"`       // start/normal/boss/merchant/npc
	FloorLabel  string          `json:"floorLabel,omitempty"`  // 中文名(普通/首领/商人/NPC)
	ChapterName string          `json:"chapterName,omitempty"` // 章节名
	Opponents   []TrialOpponent `json:"opponents,omitempty"`   // 第 7 层候选阵容
}

// TrialOpponent 是第 7 层的一个候选 NPC 阵容。
//
// ⚠️ **这是候选池,不是「当前遭遇的对手」**:wiki 的 opponent id 与协议里的
// npc_id 不是同一套编号(实测协议 npc_id=86023),无从绑定。前端应照此表述,
// 不要写成「对面就是这几只」。
type TrialOpponent struct {
	ID   uint32        `json:"id"`   // wiki 的 opponent 编号(300xxx 等),仅作标识
	Name string        `json:"name"` // NPC 名(研究员/易西/罗兰)
	Pets []TrialOppPet `json:"pets"` // 阵容:每只带名字与头像
}

// TrialOppPet 是候选阵容里的一只精灵。
//
// 带名字与头像而不只给 petbase id:这两样都要查 gamedata(且**并非每个形态都有
// 头像** —— 缺图时 Img 为空,由前端决定占位),后端查比前端再查一遍省事,
// 也避免前端硬编码「HeadIcon/<id>.webp」这种路径约定。
type TrialOppPet struct {
	Base uint32 `json:"base"`          // petbase id
	Name string `json:"name"`          // 形态全名(未知时为空)
	Img  string `json:"img,omitempty"` // HeadIcon/<n>.webp;无图时缺失
}

// TrialPet 是试炼里的宠物副本。
//
// 技能只有 id 与融合后的数值,**没有名字**:游戏技能名表尚未接入本项目
// (names.json 里没有,见 docs/data.md「待校准」),故前端按 id 展示。
type TrialPet struct {
	Gid      uint32       `json:"gid"`
	Name     string       `json:"name"`
	Species  string       `json:"species,omitempty"`
	Img      string       `json:"img,omitempty"` // 头像相对路径 HeadIcon/<n>.webp
	Level    uint32       `json:"level"`
	HP       uint32       `json:"hp"`
	MaxHP    uint32       `json:"maxHp"`
	Energy   uint32       `json:"energy"` // 能量上限
	Growth   uint32       `json:"growth"`
	Skills   []TrialSkill `json:"skills,omitempty"`
	Features []uint32     `json:"features,omitempty"` // 已获特性(288xxx)
	// 下面两组是 Features 的拆分,**只有能拿到天生特性时才给**:
	//   InnateFeatures = 宠物天生的(局级 initial_feature_ids)
	//   GainedFeatures = 试炼中获得的(已获 - 天生)
	// 拿不到时两者都缺席、Features 仍在 —— 前端据此回退到「不区分」的展示。
	// 刻意不猜:标错比不标更糟,用户会把「天生」当成确定的事实。
	InnateFeatures []uint32 `json:"innateFeatures,omitempty"`
	GainedFeatures []uint32 `json:"gainedFeatures,omitempty"`
	// FeatureNames 是查到名字的特性:id -> 名字。
	//
	// 名字**不是**游戏配置给的,而是用 wiki 的「精灵 → 特性」表桥接出来的:
	// 按形态全名查到特性名,再绑到该精灵天生的那条 id 上(原理与覆盖率见
	// gamedata.FeatureNameOfBase)。只对天生特性成立 —— 试炼中获得的特性
	// 是节点随机给的,拿精灵的特性名去绑必然是错的,故那些 id 不进这张表。
	FeatureNames map[uint32]string `json:"featureNames,omitempty"`
	Shards       []uint32          `json:"shards,omitempty"`   // 已获碎片(20xx/30xx)
	Equipped     []uint32          `json:"equipped,omitempty"` // 出战技能槽位
	// 下面四项是**玩家自己的精灵**带进试炼的外观:异色 / 炫彩 / 炫彩配色。
	// 取自内嵌 PetData 的 mutation_type(bit0=异色、bit3=炫彩)与 glass_info。
	//
	// 三者都会影响显示:异色换头像(且 img 已按异色取过图)、炫彩加色卡。
	// ⚠️ 异色头像**不是每只精灵都有素材** —— 没有时 gamedata 静默回退普通图,
	// 此时 shiny 仍为 true 而 img 是普通图的路径。前端**不可**拿 img 反推 shiny:
	// 那样没素材的异色精灵会被当成普通的。要判断异色,只看 shiny 这个字段。
	Shiny      bool   `json:"shiny"`
	Colorful   bool   `json:"colorful"`
	GlassType  int32  `json:"glassType,omitempty"`
	GlassValue uint32 `json:"glassValue,omitempty"`
}

// TrialSkill 是试炼宠物的一个技能槽(融合体)。
type TrialSkill struct {
	ID     uint32   `json:"id"`               // base_skill_id
	Name   string   `json:"name,omitempty"`   // 技能中文名;查不到时缺失(见下)
	Power  uint32   `json:"power"`            // 融合后威力
	Cost   uint32   `json:"cost"`             // 融合后能耗
	Fusion uint32   `json:"fusion,omitempty"` // 融合次数(0=未融合)
	Slot   uint32   `json:"slot"`             // 槽位(1 起)
	Merged []uint32 `json:"merged,omitempty"` // 被融合进来的技能 id
}

// TrialReward 的 id 也有中文名(技能/特性),但特性(id 288xxx)目前无名称表,
// 故只在能查到时才带 name —— 前端回退显示 id。

// TrialOption 是当前节点的一个候选事件。
//
// 一个事件 = 一只精灵 + 从它身上抽的一个奖励。两种重掷都花金币,含义不同:
//
//	换奖励(rewardCost):在这只精灵的抽取池(Pool)里重抽一个;
//	换事件(eventCost) :整只精灵换掉,抽取池随之变成新精灵的那一套。
type TrialOption struct {
	Slot   uint32 `json:"slot"`
	Event  uint32 `json:"event"`  // event_conf_id
	Reward uint32 `json:"reward"` // reward_id(技能 7xxxxx / 特性 288xxx / 碎片 20xx-30xx)
	// Names 是本卡片里查得到中文名的 id -> 名字,覆盖 Reward / Pool / Extra。
	//
	// 技能名来自 skills.json(覆盖率 98.7%);特性名则要**先标出精灵**才能查 ——
	// 走「精灵 → 特性」表(见 gamedata.FeatureNameOfBase),这正是标注精灵省下的
	// 那一步:池里那条 288xxx 不用玩家再标一次。查不到的 id 不进这张表,
	// 前端显示裸 id 并给标注入口。
	Names      map[uint32]string `json:"names,omitempty"`
	Level      uint32            `json:"level,omitempty"`
	EventCost  uint32            `json:"eventCost,omitempty"`  // 重掷该事件的报价
	RewardCost uint32            `json:"rewardCost,omitempty"` // 重掷奖励的报价
	Extra      []uint32          `json:"extra,omitempty"`      // 额外奖励(多是碎片)
	// Pool 是本事件的抽取池:该精灵「1 个自身特性 + 4 个技能」(协议 random_skills[])。
	//
	// 换奖励就是从这 5 个里重抽一个 —— 只看当前那条 Reward 无法预判重掷会出什么,
	// 故整池下发:玩家能提前把 5 个 id 都标注好,也能看出重掷还可能出什么。
	// 字段名沿用协议的 random_skills,它是 repeated,长度待更多样本确认(预期 5)。
	Pool []uint32 `json:"pool,omitempty"`
	// Used 是本节点该槽位**已经抽过**的奖励(协议 used_reward_ids[]):重掷时服务器
	// 会排除它们,故 Pool 减去 Used 才是下一次重掷的真实候选。
	Used []uint32 `json:"used,omitempty"`
	// Pet 是本事件对应的精灵(头像+名字)。
	//
	// ⚠️ 协议**不下发**它:GrassTrialNodeEvent 只有 event_conf_id,到精灵的映射
	// 表在游戏配置里(未解包)。故这一项只能靠**标注**补(标注类型 event,
	// 玩法见 internal/server/api_annotations.go)—— 标注里填的精灵名经
	// gamedata.PetByName 反查成形态 id,取到头像才带;没标注就缺失,
	// 前端显示占位并给标注入口。别把它当成"服务器说是这只"。
	Pet *TrialOppPet `json:"pet,omitempty"`
}

// TrialBless 是祝福节点(先选选项,再在候选技能里挑一个)。
type TrialBless struct {
	Event      uint32   `json:"event"`
	Options    []uint32 `json:"options,omitempty"`    // option_conf_id
	Effect     uint32   `json:"effect,omitempty"`     // 下一步要做什么:0=选技能 9=合并技能槽
	Candidates []uint32 `json:"candidates,omitempty"` // 候选技能 id
}

// TrialReward 是刚到账、等待玩家处理的奖励。
type TrialReward struct {
	Event uint32   `json:"event"`
	ID    uint32   `json:"id"`
	Extra []uint32 `json:"extra,omitempty"`
	Coin  uint32   `json:"coin"`
}

// TrialShopItem 是章末商店的一件商品。
type TrialShopItem struct {
	Type   uint32 `json:"type"` // 2=特性 3=碎片
	ID     uint32 `json:"id"`
	Price  uint32 `json:"price"`
	Index  uint32 `json:"index"`
	Bought bool   `json:"bought"`
}

// TrialResult 是一局结算。
type TrialResult struct {
	Victory   bool   `json:"victory"`
	Duration  uint32 `json:"duration"` // 用时(秒)
	PetBaseID uint32 `json:"petBaseId"`
	PetLevel  uint32 `json:"petLevel"`
	SettleAt  int64  `json:"settleAt"`
	Score     uint32 `json:"score,omitempty"`
}

// TrialLogEntry 是操作流水里的一条(时间倒序)。
type TrialLogEntry struct {
	Ts     int64    `json:"ts"`
	Kind   string   `json:"kind"`             // node/refresh/battle/bless/reward/shop/boss/settle/start
	Label  string   `json:"label"`            // 中文简述
	IDs    []uint32 `json:"ids,omitempty"`    // 相关 id(随 kind 而异)
	Action uint32   `json:"action,omitempty"` // 仅 reward:c2s 的处理动作
	Coin   uint32   `json:"coin,omitempty"`
}

// TrialHistory 是账号级试炼档案(0x1975 的聚合视图)。
type TrialHistory struct {
	ChallengeInc uint32         `json:"challengeInc"` // 累计挑战次数
	Total        uint32         `json:"total"`        // 服务器保留的战绩条数(上限 250)
	Wins         uint32         `json:"wins"`
	Cleared      []uint32       `json:"cleared,omitempty"` // 已通关的 trial_conf_id
	Recent       []TrialReview  `json:"recent,omitempty"`  // 最近战绩(倒序)
	TopPets      []TrialTopPet  `json:"topPets,omitempty"` // 常用形态
	Slots        []TrialSlot    `json:"slots,omitempty"`   // 各属性系槽位进度
	Logs         []TrialLogBook `json:"logs,omitempty"`    // 见闻录三册
}

// TrialReview 是一条历史战绩。
type TrialReview struct {
	SettleAt  int64  `json:"settleAt"`
	PetBaseID uint32 `json:"petBaseId"`
	PetName   string `json:"petName,omitempty"`
	PetLevel  uint32 `json:"petLevel"`
	TrialID   uint32 `json:"trialId"`
	Victory   bool   `json:"victory"`
	Duration  uint32 `json:"duration"`
	SlotID    uint32 `json:"slotId"`
	Mutation  uint32 `json:"mutation,omitempty"`
}

// TrialTopPet 是战绩里出现最多的形态。
type TrialTopPet struct {
	PetBaseID uint32 `json:"petBaseId"`
	Name      string `json:"name,omitempty"`
	Img       string `json:"img,omitempty"`
	Count     uint32 `json:"count"`
}

// TrialSlot 是图鉴里某属性系槽位的通关情况(每个系 3 个难度)。
type TrialSlot struct {
	SlotID  uint32 `json:"slotId"`
	DamType uint32 `json:"damType"`
	DamName string `json:"damName,omitempty"`
	Cleared uint32 `json:"cleared"`
}

// TrialEncountersPayload 是草系试炼的「遇见记录」:三章各一张图,列出本章可能
// 遇到的精灵,遇到过(该章打过照面)的置灰。
//
// 每张图分两组:普通池(第 1/2/3/5 层)与首领(第 4 层的 22 人名单,三章共用)。
// 二者是**独立来源**,故分开列出而非合并成一个大网格。
//
// 关键口径:**每章独立计算** —— 与 wiki 一致(页面注明「3 章首领按章节独立计算」)。
// 同一只精灵在第 1 章遇到过,第 2 章的图里仍算未遇见。这样三张图的进度各自真实。
type TrialEncountersPayload struct {
	Account  string               `json:"account"`
	Ts       int64                `json:"ts"`
	Chapters []TrialEncounterBook `json:"chapters,omitempty"`
	Updated  string               `json:"updated,omitempty"` // 静态配置的更新时间(数据可能已过期)
}

// TrialEncounterBook 是某一章的一张图。
type TrialEncounterBook struct {
	Chapter uint32              `json:"chapter"`        // 1 起
	Name    string              `json:"name,omitempty"` // 章节名(如「记忆中的索米亚草原」)
	Total   uint32              `json:"total"`          // 本章精灵总数(普通池 + 首领)
	Seen    uint32              `json:"seen"`           // 已遇见数
	Normal  []TrialEncounterPet `json:"normal"`         // 普通池
	Boss    []TrialEncounterPet `json:"boss,omitempty"` // 22 名首领
	// Extra 是**见过但不在上面两组里**的精灵(NPC 战 / 最终 BOSS 等)。
	//
	// 静态配置没有第 7 层 NPC 与最终 BOSS 的精灵池(只有普通池与 22 名首领),
	// 这些遭遇无处安放。宁可单列也不丢弃 —— 用户明明打过照面,图上却显示
	// 未遇见,比少一个分组糟糕得多。
	//
	// **不计入 Total/Seen**:那两个字段的口径是「池子里还剩多少」,把来源
	// 不明的条目塞进分母会让进度百分比失去意义。故单独展示。
	Extra []TrialEncounterPet `json:"extra,omitempty"`
}

// TrialEncounterPet 是图里的一只精灵。
//
// Kind/Time 用**指针**而非 omitempty 值类型:普通战的 Kind 就是 0,用值类型加
// omitempty 会被 JSON 抹掉,前端便分不清「普通战遇到的」与「没遇到」——
// 与 AGENTS.md 里 u/v 指针那条约定是同一个坑。未遇见时二者为 nil(键不出现)。
type TrialEncounterPet struct {
	Base uint32  `json:"base"`
	Name string  `json:"name"`          // 形态全名(查不到时为空)
	Img  string  `json:"img,omitempty"` // 头像;并非每个形态都有图
	Seen bool    `json:"seen"`          // 本章是否已遇见
	Kind *uint32 `json:"kind,omitempty"`
	Time *int64  `json:"time,omitempty"`
}

// TrialLogBook 是见闻录的一册。
type TrialLogBook struct {
	LogConfID  uint32 `json:"logConfId"`
	Discovered uint32 `json:"discovered"`
	Total      uint32 `json:"total"`
	Unlocked   bool   `json:"unlocked"`
}
