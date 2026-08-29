// —— 野生宠物图层(异色/炫彩 · 污染 · 奖牌四件套:大块头/小不点/婉转声/粗嗓门)——
// 与 POI 图层不同,这几类**不是固定点位**:野生宠会刷新、被别人抓走,只有走进 AOI 才知道它在。
// 后端从周边实体快照与 AOI 通知里挑出这几类推过来(见 internal/pipeline/wildpets.go),
// 前端只管开关与摆放。判定依据(捕捉前后一致的属性)见 docs/data.md 3.5。
//
// 存储键带版本号:这一版把「奖牌四件套」从开关(开/关二态)换成**只严不宽的阈值滑块**
// (默认=奖牌边界,只能往更极端拖),沿用旧键会让旧选择错位,故 bump 到 v5;
// 之后又给 4 条各加了开关(medalOn 字段,旧数据缺该字段时默认全开,不必再 bump)。
// 再之后又加了「全部野生」图层开关(all 字段),旧数据缺时默认关——这里 bump 到 v6 以
// 隔离旧选择(旧 v5 没有 all 键,loadState 会按默认补全)。
// 再之后又加了「双牌」筛选(dual 字段):缺字段时默认关(=旧行为,只显示命中任一张奖牌
// 的宠),不必再 bump。
// 之后双牌从纯开关升级为**带独立阈值**的结构 { on, medals }:on 控开关,medals 是 4 条
// 奖牌各一个**双牌专用阈值**(范围=[单牌当前阈值, 极端值],默认=单牌当前值,只严不宽)。
// 双牌开后奖牌段改用双牌阈值判 ≥2 张,单牌阈值不再参与该段;旧 bool dual 自动迁移
// (true→{on:true,medals:默认}, false/缺→{on:false,...}),不必 bump。
export const LS_KEY = 'map.wildLayers.v6'

// 与数值无关的开关图层:一个开关可覆盖后端 kinds 里的**多个**类别(异色与炫彩合成一个);
// 按稀有度从高到低排,color 同时用作侧栏色点与地图标记描边(见 wildRing)。
// all 是「全部野生」图层:kinds 为空,不按稀有类别命中,而是用后端推送的 allPets 数据源
// (普通野生宠,无稀有标记)。默认关:满地都是的普通宠开启后会糊住地图。
export const WILD_LAYERS = [
  { k: 'all', n: '全部野生', kinds: [], color: 'var(--fg-dim)' },
  { k: 'mutation', n: '异色/炫彩', kinds: ['shiny', 'colorful'], color: '#fff', on: true },
  { k: 'pollution', n: '污染', kinds: ['pollution'], color: '#c792ea' },
]
// 奖牌四件套:滑块是**单值阈值**、范围=奖牌边界~极端(只严不宽),默认=奖牌边界——与后端
// kinds 标签(big/small/high/low)同口径,拖动只能往更严格方向走。dim 是标记上的数值字段
// (weightPct 体重百分位 / voice 嗓音原值),dir 是判定方向,滑块值即阈值本身;计数随阈值
// 实时变化,与图上标记一一对应。整体收在「奖牌筛选」按钮下(见 LayerPanel)。
// step 是滑块步进:体重百分位按十分位调(与地图显示同精度,见 MapPage 的 round1),voice
// 是整数原值,维持原 0.5(等价于只影响相邻整数刻度),留默认即 0.5。
// color 同时用作侧栏色点与地图标记描边:块头类(体重)用红橙暖色、声音类用紫蓝冷色,
// 两族互成对比,且都比早先的浅色更饱和——深色底图上更跳眼(圆环加粗与光晕见 wildRing)。
export const MEDAL_FILTERS = [
  { k: 'big', n: '大块头', dim: 'weightPct', dir: '>=', lo: 98, hi: 100, def: 98, step: 0.1, color: '#ff5252' },
  { k: 'small', n: '小不点', dim: 'weightPct', dir: '<=', lo: 0, hi: 2, def: 2, step: 0.1, color: '#ff9100' },
  { k: 'high', n: '婉转声', dim: 'voice', dir: '>=', lo: 96, hi: 100, def: 96, color: '#2e7d32' },
  { k: 'low', n: '粗嗓门', dim: 'voice', dir: '<=', lo: -100, hi: -96, def: -96, color: '#40c4ff' },
]
export const DEFAULT_MEDALS = Object.fromEntries(MEDAL_FILTERS.map((m) => [m.k, m.def]))
export const SWITCH_KEYS = new Set(WILD_LAYERS.map((l) => l.k))
export const DEFAULT_SWITCHES = WILD_LAYERS.filter((l) => l.on).map((l) => l.k)
// 奖牌开关:每条默认全开(与无开关时代的旧行为一致),用户可单独关掉某项。
export const MEDAL_KEYS = new Set(MEDAL_FILTERS.map((m) => m.k))
export const DEFAULT_MEDAL_ON = MEDAL_FILTERS.map((m) => m.k)

// 双牌专用阈值的默认值 = 各奖牌单牌默认值(=奖牌边界)。双牌开后,4 条奖牌各有一个独立
// 阈值,范围=[单牌当前阈值, 极端值](只严不宽,同方向),默认=单牌当前值——即"双牌不额外
// 收紧",与纯开关时代的擦边双牌行为一致。用户拖严双牌子滑块时,双牌判定比单牌更严,
// 但单牌阈值不受影响(两者解耦)。
export const DEFAULT_DUAL_MEDALS = { ...DEFAULT_MEDALS }

// clampDual 把双牌阈值钳到合法范围 [单牌当前值, 极端值]内,保证双牌永远不比单牌宽。
// dir>=': 阈值越大越严,双牌下限=单牌值,上限=hi;dir<=': 阈值越小越严,双牌上限=单牌值,
// 下限=lo。单牌值变化时(用户拖严单牌),双牌若被越过会自动跟上,维持只严不宽。
export const clampDual = (m, dualVal, singleVal) =>
  m.dir === '>='
    ? Math.min(Math.max(dualVal, singleVal), m.hi)
    : Math.max(Math.min(dualVal, singleVal), m.lo)
