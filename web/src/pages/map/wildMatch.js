import { WILD_LAYERS, MEDAL_FILTERS, MEDAL_KEYS } from './wildConfig'

// 野生宠物的判定口径:标签翻译、奖牌阈值匹配、是否显示、是否双牌、描边样式。
// 这几个函数是**同一口径**的不同投影(能提醒的必然画得出环,画得出环的必然在 marks 里),
// 改动其中一个务必同步其余,否则会出现「图上描了圈却被过滤掉」这类不一致。

// wildTags 把一只宠物命中的类别翻成悬浮提示上的标签(比图层名更细:图层把异色/炫彩合成
// 一个开关,提示里仍分开说)。异色 + 炫彩兼具时游戏自己有个合称「异色炫彩」
// (见 gen_gamedata.py 的 STATIC_ICONS),用它比并列两个词自然。
export function wildTags(kinds = []) {
  const has = (k) => kinds.includes(k)
  const out = []
  if (has('shiny') && has('colorful')) out.push('异色炫彩')
  else if (has('shiny')) out.push('异色')
  else if (has('colorful')) out.push('炫彩')
  if (has('pollution')) out.push('污染')
  if (has('big')) out.push('大块头')
  if (has('small')) out.push('小不点')
  if (has('high')) out.push('婉转声')
  else if (has('low')) out.push('粗嗓门')
  return out
}
// medalMatch 判定某奖牌滑块是否命中一只宠物:按标记上的数值字段(weightPct 体重百分位 /
// voice 嗓音原值)与当前阈值比。形态范围缺失时后端不推 weightPct,无从判,不命中。
// 数值与阈值都先取整到十分位再比,与地图显示的百分位(MapPage 的 wildTitle/资料卡、滑块
// 值都是 round1)同口径:否则 99.6 显示成「100%」的满格个体,滑块拉到 100 时却因
// 99.6 >= 100 为假被误筛掉;voice 本就是整数,取整到十分位无副作用。
export function medalMatch(m, p, medals) {
  const v = p[m.dim]
  if (v == null) return false
  const t = Math.round(v * 10) / 10
  const th = Math.round(medals[m.k] * 10) / 10
  return m.dir === '>=' ? t >= th : t <= th
}

// wildShown 判定一只宠当前能否在地图上「带环显示」——marks 过滤与稀有宠提醒共用的同一
// 口径:开关图层(kinds 标签命中且该图层开关开)或奖牌(开关开且滑块数值阈值命中)任一命中
// 即算。与 wildRing 的描边条件完全一致:能提醒的宠必然画得出环,画不出环的必不提醒。
// dual 是双牌筛选状态 { on, medals }:
//   - on=false:奖牌段用单牌阈值 medals 判 ≥1 张(旧行为);
//   - on=true :奖牌段改用双牌阈值 dual.medals 判 ≥2 张,单牌阈值**不参与**该段——双牌阈值
//     默认=单牌值(擦边双牌),拖严后双牌比单牌更严,但两者解耦,互不影响。
// 体重族(大块头/小不点)与嗓音族(婉转声/粗嗓门)各自互斥,命中数上限就是 2。双牌只收紧
// 奖牌段,不拦开关图层——异色/炫彩、污染照常显示。
export function wildShown(p, on, medals, medalOn, dual) {
  const kinds = p.kinds || []
  const th = dual && dual.on ? dual.medals : medals
  const minHits = dual && dual.on ? 2 : 1
  const medalHits = MEDAL_FILTERS.reduce(
    (n, m) => n + (medalOn.has(m.k) && medalMatch(m, p, th) ? 1 : 0), 0)
  return (
    WILD_LAYERS.some((l) => on.has(l.k) && l.kinds.some((k) => kinds.includes(k))) ||
    medalHits >= minHits
  )
}

// isDualMedal 判定一只宠是否「双牌」:用双牌阈值口径(dual.on 时用 dual.medals,否则用单牌
// 阈值)判奖牌命中数 ≥2。与 wildShown 双牌开启时的奖牌段判定同口径,供「仅双牌时提醒」用:
// 用户拖严双牌滑块后,提醒也按同样阈值判,不会图上不画双牌环却还提醒。体重族与嗓音族各一,
// 命中数上限就是 2。
export function isDualMedal(p, medals, medalOn, dual) {
  const th = dual && dual.on ? dual.medals : medals
  const medalHits = MEDAL_FILTERS.reduce(
    (n, m) => n + (medalOn.has(m.k) && medalMatch(m, p, th) ? 1 : 0), 0)
  return medalHits >= 2
}

// wildRing 把一只宠物当前「开着且命中」的类别翻成描边样式,与 marks 过滤**同一口径**:
//   - 开关图层(异色/炫彩、污染):看 kinds 标签,且该图层开关开着才描(关了图层就不该标);
//   - 奖牌四件套:看数值阈值 medalMatch(只看 kinds 标签的话,滑块拖严后边界会对不上:
//     后端按固定边界打标,前端滑块只严不宽,weightPct 98.5 拖到 99 后不该再标大块头)。
// 最稀有的类别上主描边,其余依次向外叠外环。一只可同时命中多类(如炫彩 + 大块头 +
// 婉转声,最多 4 层),靠 CSS 类组合会指数爆炸,故按数据算。返回空对象 = 无圈。
// 可见度:命中奖牌(大块头/小不点/婉转声/粗嗓门)或异色/炫彩(全场最稀有,白色圆环)时
// 描边加粗到 3px(普通图层仍走 CSS 默认 2px),并在最外圈补一圈主色柔光(0 0 8px 1px),
// 让它们在深色底图上更跳眼;外环起点也随加粗整体外移一格,各环间距不变。
export function wildRing(p, on, medals, medalOn, dual) {
  const kinds = p.kinds || []
  // 描边阈值与 wildShown 同口径:双牌开时用双牌阈值,否则用单牌阈值。保证"画得出环的必然
  // 在 marks 里,marks 里的必然画得出环"——双牌开时单牌阈值不参与判定,描边也不该按单牌
  // 阈值画(否则会出现图上描了圈却被过滤掉的宠)。
  const th = dual && dual.on ? dual.medals : medals
  const layers = [
    ...WILD_LAYERS.filter((l) => on.has(l.k) && l.kinds.some((k) => kinds.includes(k))),
    ...MEDAL_FILTERS.filter((m) => medalOn.has(m.k) && medalMatch(m, p, th)),
  ]
  if (layers.length === 0) return {}
  const medal = layers.some((l) => MEDAL_KEYS.has(l.k))
  // 异色/炫彩在 WILD_LAYERS 首位、且图层先于奖牌拼入 layers,命中时必是主描边层。
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
    // 纯异色/炫彩(无奖牌叠加):不需要描边与柔光,光环已足够突出。
    style.borderWidth = '0'
  }
  if (rings.length) style.boxShadow = rings.join(', ')
  return style
}
