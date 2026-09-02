// —— 野生宠物图层(异色/炫彩 · 污染 · 体重/声音区间规则)——
// 与 POI 图层不同,这几类**不是固定点位**:野生宠会刷新、被别人抓走,只有走进 AOI 才知道它在。
// 后端从周边实体快照与 AOI 通知里挑出这几类推过来(见 internal/pipeline/wildpets.go),
// 前端只管开关与摆放。判定依据(捕捉前后一致的属性)见 docs/data.md 3.5。
//
// 存储键带版本号。v7 的变化:**体重/声音的四项奖牌阈值移出本文件**,改为事件页与大地图
// 共用的「区间规则」(见 utils/rules.js、hooks/useRangeRules)。原因是原先三处各写一份
// (服务端 kinds、本文件的 MEDAL_FILTERS、事件页 highlight.js),改一处不改其余就会出现
// 「图上描了圈却被过滤掉」;而且滑块**只严不宽**,想筛「体重 40~60 的中等体型」做不到。
// 旧的 medals / medalOn / dual.medals 在首次加载时迁移到规则里(见 useWildPets)。
export const LS_KEY = 'map.wildLayers.v7'
export const LEGACY_LS_KEY = 'map.wildLayers.v6'

// 与数值无关的开关图层:一个开关可覆盖后端 kinds 里的**多个**类别(异色与炫彩合成一个);
// 按稀有度从高到低排,color 同时用作侧栏色点与地图标记描边(见 wildRing)。
// all 是「全部野生」图层:kinds 为空,不按稀有类别命中,而是用后端推送的 allPets 数据源
// (普通野生宠,无稀有标记)。默认关:满地都是的普通宠开启后会糊住地图。
export const WILD_LAYERS = [
  { k: 'all', n: '全部野生', kinds: [], color: 'var(--fg-dim)' },
  { k: 'mutation', n: '异色/炫彩', kinds: ['shiny', 'colorful'], color: '#fff', on: true },
  { k: 'pollution', n: '污染', kinds: ['pollution'], color: '#c792ea' },
]
export const SWITCH_KEYS = new Set(WILD_LAYERS.map((l) => l.k))
export const DEFAULT_SWITCHES = WILD_LAYERS.filter((l) => l.on).map((l) => l.k)

// LEGACY_MEDALS 旧版「奖牌四件套」的定义,**仅供迁移使用**(见 useWildPets)。
//
// 不再参与任何判定:现在判定走 matchRangeRule(utils/rules.js),而这四项只是那条通路的
// 四个默认预设(id 与规则 id 同名,故能对号入座)。保留定义是为了把用户旧版调过的阈值
// 与开关搬过去 —— 直接丢弃的话,升级后用户精心拖过的边界会悄悄回到默认值。
// dir 是旧的单值阈值方向:>='=越大越严,转成区间即 [阈值, hi];<='=越小越严,即 [lo, 阈值]。
export const LEGACY_MEDALS = [
  { k: 'big', dim: 'weightPct', dir: '>=', lo: 98, hi: 100, def: 98 },
  { k: 'small', dim: 'weightPct', dir: '<=', lo: 0, hi: 2, def: 2 },
  { k: 'high', dim: 'voice', dir: '>=', lo: 96, hi: 100, def: 96 },
  { k: 'low', dim: 'voice', dir: '<=', lo: -100, hi: -96, def: -96 },
]
export const LEGACY_MEDAL_KEYS = new Set(LEGACY_MEDALS.map((m) => m.k))

// migrateLegacyMedals 把旧版奖牌的阈值与开关搬到区间规则上,rule id 与旧奖牌 key 同名。
//
// 抽成纯函数(而不是写在 useWildPets 的 effect 里)是为了**可测**:这段最容易写错的是
// 方向(>= 与 <= 搞反会让区间整个颠倒,大块头变成「98 以下」),而放在 hook 里
// verify-rules.mjs 够不着 —— 先前就因此漏测过一次,改坏了方向测试照样全绿。
export function migrateLegacyMedals(rules, legacy) {
  if (!legacy || !Array.isArray(rules)) return rules
  const { medals, medalOn } = legacy
  return rules.map((r) => {
    const m = LEGACY_MEDALS.find((x) => x.k === r.id)
    if (!m) return r
    const onOff = Array.isArray(medalOn) ? medalOn.includes(r.id) : r.on
    const th = medals && typeof medals[m.k] === 'number' ? medals[m.k] : null
    // 旧的是单值阈值 + 方向:>=' 越大越严 → [阈值, hi];<=' 越小越严 → [lo, 阈值]。
    const min = th == null ? r.min : (m.dir === '>=' ? th : m.lo)
    const max = th == null ? r.max : (m.dir === '>=' ? m.hi : th)
    return { ...r, on: onOff, min: Math.min(min, max), max: Math.max(min, max) }
  })
}
