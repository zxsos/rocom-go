import { WILD_LAYERS } from './wildConfig'
import { matchRangeRule, rangeRuleLabel } from '../../utils/rules'

// 野生宠物的判定口径:标签翻译、规则命中、是否显示、是否双牌、描边样式。
// 这几个函数是**同一口径**的不同投影(能提醒的必然画得出环,画得出环的必然在 marks 里),
// 改动其中一个务必同步其余,否则会出现「图上描了圈却被过滤掉」这类不一致。

// ruleHits 返回一只宠命中的区间规则(只算启用的)。
//
// 判定用**原始数值**而非后端 kinds 标签:后端按固定奖牌边界打 big/small/high/low,
// 用户自定义的区间可能比它宽(如体重 40~60),那种区间在后端根本没有对应标签。
export const ruleHits = (p, rangeRules = []) =>
  rangeRules.filter((r) => matchRangeRule(p, r))

// wildTags 把一只宠命中的类别翻成悬浮提示上的标签(比图层名更细:图层把异色/炫彩合成
// 一个开关,提示里仍分开说)。异色 + 炫彩兼具时游戏自己有个合称「异色炫彩」
// (见 gen_gamedata.py 的 STATIC_ICONS),用它比并列两个词自然。
//
// 区间规则命中项用规则自己的名字:自定义区间(如「体重 40~60」)后端没有标签,
// 只有前端补得出,否则悬停提示上看不出它为什么被圈出来。
export function wildTags(p, rangeRules = []) {
  const kinds = p?.kinds || []
  const has = (k) => kinds.includes(k)
  const out = []
  if (has('shiny') && has('colorful')) out.push('异色炫彩')
  else if (has('shiny')) out.push('异色')
  else if (has('colorful')) out.push('炫彩')
  if (has('pollution')) out.push('污染')
  for (const r of ruleHits(p, rangeRules)) out.push(rangeRuleLabel(r))
  return out
}

// wildShown 判定一只宠当前能否在地图上「带环显示」——marks 过滤与稀有宠提醒共用的同一
// 口径:开关图层(kinds 标签命中且该图层开关开)或区间规则命中(≥1 条;双牌开则 ≥2 条)
// 任一命中即算。与 wildRing 的描边条件完全一致:能提醒的宠必然画得出环,画不出环的必不提醒。
//
// dual 是双牌开关(布尔):由用户选定的「命中 ≥2 条规则」——规则是他自己配的,
// 故「双牌」= 命中其中任意两条,不再限定「体重族+嗓音族各一」。
export function wildShown(p, on, rangeRules, dual) {
  const kinds = p.kinds || []
  const minHits = dual ? 2 : 1
  return (
    WILD_LAYERS.some((l) => on.has(l.k) && l.kinds.some((k) => kinds.includes(k))) ||
    ruleHits(p, rangeRules).length >= minHits
  )
}

// isDualMedal 判定一只宠是否「双牌」:命中的启用规则 ≥2 条。与 wildShown 双牌开启时
// 的判定同口径,供「仅双牌时提醒」用 —— 不会图上不画双牌环却还提醒。
export const isDualMedal = (p, rangeRules) => ruleHits(p, rangeRules).length >= 2

// wildRing 把一只宠物当前「开着且命中」的类别翻成描边样式,与 marks 过滤**同一口径**:
//   - 开关图层(异色/炫彩、污染):看 kinds 标签,且该图层开关开着才描(关了图层就不该标);
//   - 区间规则:看数值区间 matchRangeRule,描边色取规则自带的颜色(与事件页高亮同色,
//     同一条规则在两个页面认得出是同一条)。
// 最稀有的类别上主描边,其余依次向外叠外环。一只可同时命中多类(如炫彩 + 大块头 + 婉转声),
// 靠 CSS 类组合会指数爆炸,故按数据算。返回空对象 = 无圈。
// 可见度:命中规则(原奖牌类)或异色/炫彩(全场最稀有,白色圆环)时描边加粗到 3px
// (普通图层仍走 CSS 默认 2px),并在最外圈补一圈主色柔光(0 0 8px 1px),
// 让它们在深色底图上更跳眼;外环起点也随加粗整体外移一格,各环间距不变。
export function wildRing(p, on, rangeRules) {
  const kinds = p.kinds || []
  const layerHits = WILD_LAYERS.filter((l) => on.has(l.k) && l.kinds.some((k) => kinds.includes(k)))
  // 画**所有**命中的规则,与双牌无关:双牌只决定这只宠显不显示(minHits,见 wildShown);
  // 一旦决定显示,它命中了几条就该叠几个环 —— 只画一条的话,看不出它为什么算双牌。
  const ruleLayers = ruleHits(p, rangeRules).map((r) => ({
    k: r.id, n: rangeRuleLabel(r), color: r.color,
  }))
  const layers = [...layerHits, ...ruleLayers]
  if (layers.length === 0) return {}
  const medal = ruleLayers.length > 0
  // 异色/炫彩在 WILD_LAYERS 里先于奖牌拼入 layers,命中时必是主描边层。
  const shiny = layers[0].k === 'mutation'
  const style = {}
  if (!shiny) {
    // 异色/炫彩走专属「稀有光环」视觉(见 map.css .map-wild.rare),不再画白色主描边,
    // 否则白圈会与旋转光环打架;普通类别仍走 borderColor 主描边。
    style.borderColor = layers[0].color
  }
  const rings = []
  let spread = medal || shiny ? 3 : 2 // 描边宽度,兼作外环起点,保持环间距均匀
  for (const l of layers.slice(1)) {
    rings.push(`0 0 0 ${spread}px ${l.color}`)
    spread += 3
  }
  if (medal) {
    style.borderWidth = '3px'
    rings.push(`0 0 8px 1px ${layers[0].color}`)
  } else if (shiny) {
    // 纯异色/炫彩(无规则叠加):不需要描边与柔光,光环已足够突出。
    style.borderWidth = '0'
  }
  if (rings.length) style.boxShadow = rings.join(', ')
  return style
}
