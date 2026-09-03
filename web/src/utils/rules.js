// —— 体重 / 声音的「区间规则」(事件页与大地图共用)——
//
// 存在理由:这两项此前各写一份、且都写死 ——
//   - 事件页 highlight.js:只能选「大块头/小不点/婉转声/粗嗓门」四个死标签,阈值硬编码;
//   - 大地图 wildConfig.js:MEDAL_FILTERS 滑块**只严不宽**(范围框死在极值区),
//     想筛「体重 40~60 的中等体型」做不到;
//   - 服务端 pipeline/wildpets.go 另有一份同口径的 kinds 标签。
// 三处各自维护,改一处不改其余就会出现「图上描了圈却被过滤掉」这类不一致。
//
// 现在统一成一份用户自定义区间,两处共用:配一次两边生效,口径只有这一个来源。
//
// 规则形状:
//   { id, dim, min, max, label, color, on }
//     dim  : 'weightPct'(体重百分位 0~100) | 'voice'(嗓音原值 -100~100)
//     min/max : 闭区间,含端点;min > max 时按 [max, min] 处理
//     label: 显示名,空则显示区间本身(「98% ~ 100%」)
//     color: 事件页高亮 / 地图描边的取色
//     on   : 启停 —— 关掉规则但保留配置,不必删了重建
//
// 判定只用**原始数值**,不看后端 kinds 标签:后端按奖牌边界(98/2/96/-96)打
// big/small/high/low 标签,那是固定口径;用户区间可能比它宽(如体重 40~60),
// 只有原始数值判得了。两个页面都已下发这两个字段 —— 事件经 FillSizePercentile
// 后带 weightPct,地图 WildMark 带 weightPct / voice。

// RANGE_DIMS 两个可自定义的维度。min/max 是该维度的**取值域**(不是默认值),
// step 是输入步进:体重百分位按十分位(与地图显示、滑块同精度),嗓音是整数。
export const RANGE_DIMS = [
  { k: 'weightPct', n: '体重', unit: '%', min: 0, max: 100, step: 0.1, color: '#ff5252', hint: '形态内的体重百分位(0~100)' },
  { k: 'voice', n: '声音', unit: '', min: -100, max: 100, step: 1, color: '#40c4ff', hint: '嗓音原值(-100~100)' },
]
export const DIM_BY_K = Object.fromEntries(RANGE_DIMS.map((d) => [d.k, d]))
export const RANGE_DIM_KEYS = new Set(RANGE_DIMS.map((d) => d.k))

// 旧版事件高亮里体重/声音用的字段名(与 RANGE_DIM_KEYS 不完全重合:老键是
// 'weight' 而现在是 'weightPct'),读取时据此剔除历史遗留规则 —— 它们已由下面的
// 预设区间取代,留着只会变成点不掉也判不中的死标签。
export const LEGACY_RANGE_FIELDS = new Set(['weight', 'voice'])

// 默认四条 = 游戏奖牌四件套的边界(与后端 kinds 同口径):
//   大块头 weightPct>=98 / 小不点 <=2 / 婉转声 voice>=96 / 粗嗓门 <=-96。
// 单值阈值是区间的退化形式 —— >=98 即 [98,100],<=2 即 [0,2],故改用区间后
// 默认行为与改前完全一致,用户可在此基础上随意改宽改窄或增删。
export const DEFAULT_RANGE_RULES = [
  { id: 'big', dim: 'weightPct', min: 98, max: 100, label: '大块头', color: '#ff5252', on: true },
  { id: 'small', dim: 'weightPct', min: 0, max: 2, label: '小不点', color: '#ff9100', on: true },
  { id: 'high', dim: 'voice', min: 96, max: 100, label: '婉转声', color: '#3fb950', on: true },
  { id: 'low', dim: 'voice', min: -100, max: -96, label: '粗嗓门', color: '#40c4ff', on: true },
]

// 取色板:新规则按顺序取,用户可改。颜色要在深色底图上跳眼且与既有语义色区分。
export const RULE_PALETTE = [
  '#ff5252', '#ff9100', '#ffc107', '#3fb950',
  '#40c4ff', '#4c8dff', '#c792ea', '#f5b942',
]

// RULE_PRESETS 常用区间模板,「添加」时直接点选,不必从零拖滑块。
//
// 存在理由:完全自由的区间在**第一次**配的时候最累 —— 想加个「中等体型」,
// 得先想清楚该是多少到多少,再分别调两个端点。给一组现成的档位,一点即用,
// 之后要微调再拖,比凭空填数字省事得多。
//
// 每档的区间不重叠且覆盖整个取值域:体重按「小不点 < 偏轻 < 中等 < 偏重 < 大块头」
// 从低到高排,声音按「粗嗓门 < 偏低 < 中性 < 偏高 < 婉转声」从低到高排,
// 视觉上对称,也避免用户不小心选出两个含义打架的区间。
// 中间那几档(偏轻/中等/偏重、偏低/中性/偏高)是**旧版完全给不出**的:旧模型
// 只有两极(极值边界),没有「中等」这种说法。
export const RULE_PRESETS = [
  // 体重百分位由低到高
  { g: '体重', label: '小不点', dim: 'weightPct', min: 0, max: 2, color: '#ff9100' },
  { g: '体重', label: '偏轻', dim: 'weightPct', min: 0, max: 30, color: '#ffab00' },
  { g: '体重', label: '中等', dim: 'weightPct', min: 40, max: 60, color: '#ffc107' },
  { g: '体重', label: '偏重', dim: 'weightPct', min: 70, max: 100, color: '#ff7043' },
  { g: '体重', label: '大块头', dim: 'weightPct', min: 98, max: 100, color: '#ff5252' },
  // 嗓音原值由低到高
  { g: '声音', label: '粗嗓门', dim: 'voice', min: -100, max: -96, color: '#40c4ff' },
  { g: '声音', label: '偏低', dim: 'voice', min: -100, max: -30, color: '#29b6f6' },
  { g: '声音', label: '中性', dim: 'voice', min: -30, max: 30, color: '#4dd0e1' },
  { g: '声音', label: '偏高', dim: 'voice', min: 30, max: 100, color: '#66bb6a' },
  { g: '声音', label: '婉转声', dim: 'voice', min: 96, max: 100, color: '#3fb950' },
]

// RULE_SCHEMES 整套规则的快捷方案,点一下替换当前全部规则。
//
// 面向「不想逐条配」的场景:多数人只关心某几类,直接选一个现成的组合。
// ids 引用 RULE_PRESETS 的 label(同名),故方案里的规则带预设的区间与配色,
// 与手动添加的结果完全一致 —— 不存在「方案配的」和「自己配的」两套东西。
export const RULE_SCHEMES = [
  { k: 'medal', n: '奖牌四件套', ids: ['大块头', '小不点', '婉转声', '粗嗓门'] },
  { k: 'extreme', n: '只看极值', ids: ['大块头', '婉转声'] },
  { k: 'weight', n: '只看体型', ids: ['大块头', '小不点'] },
  { k: 'voice', n: '只看声音', ids: ['婉转声', '粗嗓门'] },
  { k: 'none', n: '清空', ids: [] },
]

// schemeRules 把方案展开成规则数组(带新 id,避免与既有规则撞 id)。
export function schemeRules(scheme) {
  const presets = RULE_PRESETS.filter((p) => scheme.ids.includes(p.label))
  return presets.map((p, i) => ({
    id: `s${i}_${p.label}`,
    dim: p.dim, min: p.min, max: p.max,
    label: p.label, color: p.color, on: true,
  }))
}

// round1 取整到十分位。与地图显示(MapPage 的 wildTitle/资料卡)、滑块值同口径 ——
// 否则 99.6 显示成「100%」的满格个体,阈值 100 时却因 99.6>=100 为假被误筛掉。
const round1 = (v) => Math.round(v * 10) / 10

// matchRangeRule 判定一只宠是否落在规则区间内。
//
// 值缺失(null/undefined)时**不命中**:形态范围缺失时后端不推 weightPct,
// 无从判断,不能因为 min=0 就把「不知道」当成「落在 [0,2] 里」。
export function matchRangeRule(pet, rule) {
  if (!pet || !rule || rule.on === false) return false
  const v = pet[rule.dim]
  if (v == null) return false
  const t = round1(v)
  const lo = round1(Math.min(rule.min, rule.max))
  const hi = round1(Math.max(rule.min, rule.max))
  return t >= lo && t <= hi
}

// rangeRuleLabel 规则的显示名:有自定义名用自定义名,否则显示区间本身。
export function rangeRuleLabel(rule) {
  if (!rule) return ''
  if (rule.label) return rule.label
  const dim = DIM_BY_K[rule.dim]
  const u = dim?.unit || ''
  return `${rule.min}${u} ~ ${rule.max}${u}`
}

// sanitizeRangeRules 清洗存储里的规则:逐条校验,非法项丢弃而非抛错。
//
// 允许空数组 —— 用户把规则全删了就该是全不命中(on 全关时同样),不能悄悄把
// 默认四条塞回去(那样「删除」这个操作等于无效)。
// 值越界一律**钳回**维度取值域:旧版本区间更窄时留下来的数不至于让规则失效。
export function sanitizeRangeRules(v) {
  if (!Array.isArray(v)) return []
  const out = []
  const seen = new Set()
  for (const r of v) {
    if (!r || typeof r !== 'object') continue
    const dim = DIM_BY_K[r.dim]
    if (!dim) continue
    let min = Number(r.min)
    let max = Number(r.max)
    if (!Number.isFinite(min) || !Number.isFinite(max)) continue
    min = Math.min(Math.max(min, dim.min), dim.max)
    max = Math.min(Math.max(max, dim.min), dim.max)
    let id = typeof r.id === 'string' && r.id ? r.id : `r${out.length + 1}`
    while (seen.has(id)) id += '_'
    seen.add(id)
    out.push({
      id,
      dim: dim.k,
      min: Math.min(min, max),
      max: Math.max(min, max),
      label: typeof r.label === 'string' ? r.label.slice(0, 12) : '',
      color: typeof r.color === 'string' && r.color ? r.color : dim.color,
      on: r.on !== false,
    })
  }
  return out
}

// newRuleId 生成规则 id。不追求全局唯一,只需在同一份列表内不撞(撞了会被
// sanitizeRangeRules 改名,React key 也不会因此重复)。
export const newRuleId = () =>
  'r' + Math.random().toString(36).slice(2, 8) + Date.now().toString(36).slice(-3)

// clampRange 把区间的一端拖动后钳成合法区间,返回 [min, max]。
//
// 双滑块是两个独立的 range input,各自的原生行为不受另一端约束 —— 不钳的话
// 下限能被拖到上限右边,区间就「翻」了(界面上高亮段消失,数值也看着不对)。
//
// 抽成纯函数而非写在组件里:这段是最容易写错的(方向、边界),而组件里的逻辑
// verify-rules.mjs 够不着。
export function clampRange(edge, value, other, dim) {
  const v = Math.min(Math.max(Number(value), dim.min), dim.max)
  const o = Math.min(Math.max(Number(other), dim.min), dim.max)
  return edge === 'min' ? [Math.min(v, o), o] : [o, Math.max(v, o)]
}

// sliderTop 决定两个端点重合时哪个滑块在上层(接收指针事件)。
//
// 重合时上面的 input 会盖住下面的,总有一个端点拖不动。判据取「区间落在取值域的
// 哪一半」:不偏右时让**上限**在上(用户多半想往右扩),偏右时让**下限**在上
// (想往左缩)。这样两个方向都够得着,不必先拆开再调。
//
// 用 <= 而非 <:正好居中时(如嗓音 [-10,10],中心恰为取值域中点)归到「不偏右」,
// 与偏左同侧处理。此时两个端点本就分开、谁在上都无所谓,但判据要有确定的归属,
// 免得同一个值落进两个分支、行为随浮点误差漂移。
export function sliderTop(min, max, dim) {
  const mid = (dim.min + dim.max) / 2
  return (min + max) / 2 <= mid ? 'max' : 'min'
}

// —— 滑块的「变焦刻度」——
//
// 体重/声音的取值域绝大部分用不上:真正在意的是两头的奖牌窗口 ——
// 体重 [0,2] 与 [98,100]、嗓音 [-100,-96] 与 [96,100](见 DEFAULT_RANGE_RULES);
// 嗓音那两段合起来也只占取值域的 4%(8/200)。按比例铺在 200px 的轨道上两头各
// 4~8px —— 想停在 98.0 而不是 98.3 基本靠运气,而中间那 90% 几乎没人会去拖。
//
// 故把轨道**非线性地**分给重点区:两个重点区各占一大块,中间那段压扁。拖动同样
// 一像素,在中间走得多、在两头走得少 —— 也就是「非奖牌区 step 更大」。
// 轨道仍是一条连贯的线,只是刻度不均匀,所以 RangeSlider 会把重点区与奖牌边界
// 画在轨道上(见 .rslider-zone / .rslider-tick):不画出来的话,用户只会觉得
// 「拖动不准」,而不知道刻度是故意不均匀的。

// FOCUS_ZONES 需要放大的区间(闭区间)。取「奖牌窗口 + 一点余量」——
// 余量只求「偶尔放宽标准」够得着:给太多,两头就被摊薄得不够用了。
export const FOCUS_ZONES = {
  weightPct: [[0, 10], [90, 100]],
  voice: [[-100, -88], [88, 100]],
}

// FOCUS_SHARE 两个重点区**合计**占轨道的比例,剩下的按值域跨度分给中间段。
// 0.7 是手感值:体重每个重点区 10 个单位摊到 70px(200px 轨道),奖牌窗口那
// 2 个单位有 14px;中间 80 个单位只剩 60px —— 约 9 倍的差。
const FOCUS_SHARE = 0.7

// COARSE_MUL 中间段的步进放大倍数。中间段一像素能走好几个单位,再给重点区那档
// 精度只会拖出 43.7 这种没有意义的数。
const COARSE_MUL = 10

// MAGNET_POS 磁吸半径(按轨道比例):拖到奖牌边界附近 ±1.5% 轨道内,直接吸附到
// 边界值。奖牌是截断判定(97.9 不算大块头),差 0.1 就筛不出来,手拖不稳;
// 半径刻意留得小,想放宽标准往外多拖一点就脱离吸附了。
const MAGNET_POS = 0.015

// rangeScale 造一把分段线性的尺子,在**取值**与**轨道比例(0~1)**之间换算。
//
// 返回 { segs, toPos, toValue, marks }:
//   - segs  分段结果(含各自占的轨道起止),给 RangeSlider 画重点区底色;
//   - marks 奖牌边界在轨道上的位置,画刻度线、同时是磁吸点;
//   - toPos / toValue 互逆(量化与磁吸除外)。
//
// 为什么是分段线性而不是一条平滑曲线:重点区与中间段的边界要的是**硬拐点**,
// 这样每个重点区内部的刻度才是均匀的 —— 用户在一个区内的手感一致,不会出现
// 「越靠近奖牌越慢」这种难以预期的变化。
export function rangeScale(dim) {
  const zones = (FOCUS_ZONES[dim.k] || [])
    .map(([a, b]) => [Math.max(a, dim.min), Math.min(b, dim.max)])
    .filter(([a, b]) => b > a)
    .sort((x, y) => x[0] - y[0])

  // 切成交替的段,铺满整个取值域(重点区之间、以及两端之外自动成为「中间段」)。
  const segs = []
  let cur = dim.min
  for (const [a, b] of zones) {
    if (a > cur) segs.push({ focus: false, a: cur, b: a })
    if (b > cur) segs.push({ focus: true, a: Math.max(a, cur), b })
    cur = Math.max(cur, b)
  }
  if (cur < dim.max) segs.push({ focus: false, a: cur, b: dim.max })
  if (segs.length === 0) segs.push({ focus: false, a: dim.min, b: dim.max })

  // 分轨道:重点区均分 FOCUS_SHARE,中间段按值域跨度分剩下的。
  const nf = segs.filter((s) => s.focus).length
  const gapSpan = segs.reduce((n, s) => n + (s.focus ? 0 : s.b - s.a), 0)
  const focusShare = nf === 0 ? 0 : gapSpan === 0 ? 1 : FOCUS_SHARE
  let at = 0
  for (const s of segs) {
    const w = s.focus
      ? focusShare / nf
      : gapSpan === 0 ? 0 : ((1 - focusShare) * (s.b - s.a)) / gapSpan
    s.start = at
    s.end = at + w
    at = s.end
  }
  segs[segs.length - 1].end = 1 // 收掉浮点误差,保证满量程正好落在 1

  const last = segs[segs.length - 1]
  // 边界值归入**后**一段:两侧的 t 分别算出 1 和 0,落点是同一个位置,故不影响连续性。
  const segByValue = (x) => segs.find((s, i) => x < s.b || i === segs.length - 1) || last
  const segByPos = (p) => segs.find((s, i) => p < s.end || i === segs.length - 1) || last

  const toPos = (v) => {
    const x = Math.min(dim.max, Math.max(dim.min, v))
    const s = segByValue(x)
    const t = s.b === s.a ? 0 : (x - s.a) / (s.b - s.a)
    return s.start + t * (s.end - s.start)
  }

  // 奖牌边界 = 默认规则的端点(DEFAULT_RANGE_RULES 是阈值的唯一来源):
  // 既是刻度线的位置,也是磁吸点。默认规则一改,这两处跟着走。
  const marks = []
  for (const r of DEFAULT_RANGE_RULES) {
    if (r.dim !== dim.k) continue
    for (const v of [r.min, r.max]) if (!marks.some((m) => m.v === v)) marks.push({ v, pos: toPos(v) })
  }
  marks.sort((a, b) => a.v - b.v)

  const toValue = (p) => {
    const x = Math.min(1, Math.max(0, p))
    const s = segByPos(x)
    const t = s.end === s.start ? 0 : (x - s.start) / (s.end - s.start)
    const raw = s.a + t * (s.b - s.a)
    // 量化到所在段的步进:中间段放大一档,重点区保持维度自身的精度。
    // 取整走**全局网格**(round(v/step)*step)而不是「段内相对」,这样中间段出来的
    // 是 -20 / 0 / 60 这类整数,而非 -18 / -8 / 2 这种看着像随手填的数。
    const step = s.focus ? dim.step : dim.step * COARSE_MUL
    let out = round1(Math.round(raw / step) * step)
    // 夹回本段:网格点可能落到段外(如嗓音中间段 step=10,靠左端会算出 -90,
    // 而 -90 属于左边的重点区),夹回来才能保证段边界是够得到的。
    out = Math.min(s.b, Math.max(s.a, out))
    // 磁吸:取**最近**的边界,而不是第一个够近的。
    let best = -1
    let bd = MAGNET_POS
    for (let i = 0; i < marks.length; i++) {
      const d = Math.abs(x - marks[i].pos)
      if (d <= bd) { bd = d; best = i }
    }
    if (best >= 0) out = marks[best].v
    return Math.min(dim.max, Math.max(dim.min, out))
  }

  return { segs, toPos, toValue, marks }
}
