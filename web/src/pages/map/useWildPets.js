import { useState, useEffect } from 'react'
import { getWildPets, subscribe } from '../../api'

// —— 野生宠物图层(异色/炫彩 · 污染 · 奖牌四件套:大块头/小不点/婉转声/粗嗓门)——
// 与 POI 图层不同,这几类**不是固定点位**:野生宠会刷新、被别人抓走,只有走进 AOI 才知道它在。
// 后端从周边实体快照与 AOI 通知里挑出这几类推过来(见 internal/pipeline/wildpets.go),
// 前端只管开关与摆放。判定依据(捕捉前后一致的属性)见 docs/data.md 3.5。
//
// 存储键带版本号:这一版把「奖牌四件套」从开关(开/关二态)换成**只严不宽的阈值滑块**
// (默认=奖牌边界,只能往更极端拖),沿用旧键会让旧选择错位,故 bump 到 v5;
// 之后又给 4 条各加了开关(medalOn 字段,旧数据缺该字段时默认全开,不必再 bump)。
const LS_KEY = 'map.wildLayers.v5'

// 与数值无关的开关图层:一个开关可覆盖后端 kinds 里的**多个**类别(异色与炫彩合成一个);
// 按稀有度从高到低排,color 同时用作侧栏色点与地图标记描边(见 wildRing)。
export const WILD_LAYERS = [
  { k: 'mutation', n: '异色/炫彩', kinds: ['shiny', 'colorful'], color: '#7ad3ff', on: true },
  { k: 'pollution', n: '污染', kinds: ['pollution'], color: '#c792ea' },
]

// 奖牌四件套:滑块是**单值阈值**、范围=奖牌边界~极端(只严不宽),默认=奖牌边界——与后端
// kinds 标签(big/small/high/low)同口径,拖动只能往更严格方向走。dim 是标记上的数值字段
// (weightPct 体重百分位 / voice 嗓音原值),dir 是判定方向,滑块值即阈值本身;计数随阈值
// 实时变化,与图上标记一一对应。整体收在「奖牌筛选」按钮下(见 LayerPanel)。
export const MEDAL_FILTERS = [
  { k: 'big', n: '大块头', dim: 'weightPct', dir: '>=', lo: 98, hi: 100, def: 98, color: '#ff8a65' },
  { k: 'small', n: '小不点', dim: 'weightPct', dir: '<=', lo: 0, hi: 2, def: 2, color: '#4db6ac' },
  { k: 'high', n: '婉转声', dim: 'voice', dir: '>=', lo: 96, hi: 100, def: 96, color: '#ffb74d' },
  { k: 'low', n: '粗嗓门', dim: 'voice', dir: '<=', lo: -100, hi: -96, def: -96, color: '#aed581' },
]
const DEFAULT_MEDALS = Object.fromEntries(MEDAL_FILTERS.map((m) => [m.k, m.def]))
const SWITCH_KEYS = new Set(WILD_LAYERS.map((l) => l.k))
const DEFAULT_SWITCHES = WILD_LAYERS.filter((l) => l.on).map((l) => l.k)
// 奖牌开关:每条默认全开(与无开关时代的旧行为一致),用户可单独关掉某项。
const MEDAL_KEYS = new Set(MEDAL_FILTERS.map((m) => m.k))
const DEFAULT_MEDAL_ON = MEDAL_FILTERS.map((m) => m.k)

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

// wildRing 把一只宠物命中的类别翻成描边样式:最稀有的那层上主描边,次一层再加一圈外环。
// (一只可以同时命中多类,如炫彩 + 大块头,靠 CSS 类组合会指数爆炸,故按数据算。)
export function wildRing(kinds = []) {
  const hit = WILD_LAYERS.filter((l) => l.kinds.some((k) => kinds.includes(k)))
  if (hit.length === 0) return {}
  const style = { borderColor: hit[0].color }
  if (hit.length > 1) style.boxShadow = `0 0 0 2px ${hit[1].color}`
  return style
}

// —— 存储:v5 起为一个对象 { on: 开关键数组, medals: {big:…}, open: 奖牌筛选是否展开,
//   medalOn: 奖牌开关数组 } ——
// null = 用户从没手动选过(或旧格式),按默认值;数组只可能是旧键,同样回默认。
// medalOn 是后加的字段,旧数据缺失时按默认(全开)处理,与无开关时代的旧行为一致。
const loadState = () => {
  const base = { on: new Set(DEFAULT_SWITCHES), medals: { ...DEFAULT_MEDALS }, open: false, medalOn: new Set(DEFAULT_MEDAL_ON) }
  try {
    const v = JSON.parse(localStorage.getItem(LS_KEY))
    if (!v || typeof v !== 'object' || Array.isArray(v)) return base
    const on = new Set(Array.isArray(v.on) ? v.on.filter((k) => SWITCH_KEYS.has(k)) : DEFAULT_SWITCHES)
    const medals = { ...DEFAULT_MEDALS }
    if (v.medals && typeof v.medals === 'object') {
      for (const m of MEDAL_FILTERS) {
        const t = v.medals[m.k]
        if (typeof t === 'number' && t >= m.lo && t <= m.hi) medals[m.k] = t
      }
    }
    const medalOn = new Set(Array.isArray(v.medalOn) ? v.medalOn.filter((k) => MEDAL_KEYS.has(k)) : DEFAULT_MEDAL_ON)
    return { on, medals, open: !!v.open, medalOn }
  } catch { return base }
}
const persist = (on, medals, open, medalOn) => {
  localStorage.setItem(LS_KEY, JSON.stringify({ on: [...on], medals, open, medalOn: [...medalOn] }))
}

// useWildPets 管理野生宠物图层:订阅后端推送、按「开关 + 奖牌阈值」筛出可绘制的标记。
export function useWildPets(account) {
  const [pets, setPets] = useState([])
  const [st] = useState(loadState) // 初始快照只取一次(含 localStorage 的旧选择)
  const [on, setOn] = useState(st.on)
  const [medals, setMedals] = useState(st.medals)
  const [open, setOpen] = useState(st.open)
  const [medalOn, setMedalOn] = useState(st.medalOn)

  useEffect(() => {
    let alive = true
    setPets([])
    getWildPets().then((d) => { if (alive && d) setPets(d.pets || []) }).catch(() => {})
    return () => { alive = false }
  }, [account])

  // 后端每次成员/状态变化都推全量列表(实体进出 AOI 是低频事件),直接替换即可。
  useEffect(() => subscribe((m) => {
    if (m.type === 'wildpets') setPets(m.data.pets || [])
  }), [account])

  const toggle = (k) => {
    setOn((prev) => {
      const next = new Set(prev)
      next.has(k) ? next.delete(k) : next.add(k)
      persist(next, medals, open, medalOn)
      return next
    })
  }

  const setThreshold = (k, v) => {
    setMedals((prev) => {
      const next = { ...prev, [k]: v }
      persist(on, next, open, medalOn)
      return next
    })
  }

  const toggleMedal = (k) => {
    setMedalOn((prev) => {
      const next = new Set(prev)
      next.has(k) ? next.delete(k) : next.add(k)
      persist(on, medals, open, next)
      return next
    })
  }

  const toggleOpen = () => {
    setOpen((prev) => {
      persist(on, medals, !prev, medalOn)
      return !prev
    })
  }

  // 开着的开关图层覆盖哪些后端类别;奖牌滑块按数值阈值判(标记上带 weightPct/voice)。
  const shownKinds = new Set(WILD_LAYERS.filter((l) => on.has(l.k)).flatMap((l) => l.kinds))
  const medalHit = (m, p) => {
    const v = p[m.dim]
    if (v == null) return false // 形态范围缺失时后端不推 weightPct,无从判,不画
    return m.dir === '>=' ? v >= medals[m.k] : v <= medals[m.k]
  }
  const marks = pets.filter((p) =>
    (p.kinds || []).some((k) => shownKinds.has(k)) ||
    MEDAL_FILTERS.some((m) => medalOn.has(m.k) && medalHit(m, p)))
  // 图层行上的计数与地图上画出的标记一一对应:灰点(已离开视野的最后所见)也画在图上,
  // 故也计入——否则侧栏显示 0 而图上还挂着几个,只会让人以为标记出错了。
  // 另单算其中的灰点数,供侧栏悬浮说明拆开「视野内 / 已离开」(见 LayerPanel)。
  const hit = (l, p) => l.kinds
    ? (p.kinds || []).some((k) => l.kinds.includes(k))
    : medalOn.has(l.k) && medalHit(l, p)
  const count = (l, pick) => pets.filter((p) => pick(p) && hit(l, p)).length
  const num = Object.fromEntries([...WILD_LAYERS, ...MEDAL_FILTERS].map((l) => [l.k, count(l, () => true)]))
  const numStale = Object.fromEntries([...WILD_LAYERS, ...MEDAL_FILTERS].map((l) => [l.k, count(l, (p) => p.stale)]))

  return { marks, num, numStale, on, toggle, medals, setThreshold, open, toggleOpen, medalOn, toggleMedal }
}
