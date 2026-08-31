import React, { useState, useEffect, useMemo, useRef, useCallback } from 'react'
import { imgURL } from '../../components/icons'

// —— 跑图收集路线图层(仅 10003 卡洛西亚大陆)——
// 路线数据是 B站泽口博士的收集路线,静态 JSON 在 web/public/route-map/data/:
//   index.json(文件名列表) + 各路线 JSON。
// 路线坐标为 8192x8192 画布,归一化 u=x/8192 后与底图投影对齐,前端只负责开关与摆放。
// 开关选择存 localStorage,与 POI 图层记忆互不干扰。
// 跟走模式:玩家位置(pos 世界坐标,cm)换算回 8192 画布,与路线点比距离;
// 到达某点(半径 NEAR)后该点之前的线隐藏,只显示剩余路线 + 下一目标点。
const ROUTES_LS_KEY = 'map.routeLayers'
const FOLLOW_LS_KEY = 'map.routeFollow'
const PROGRESS_LS_KEY = 'map.routeProgress'
const NEAR_M_LS_KEY = 'map.routeNearM'
const COLORS_LS_KEY = 'map.routeColors'
const SCENE = 10003 // 卡洛西亚大陆(路线数据只在该场景有意义)
const GRID = 8192
// 10003 底图投影参数(与 names.json maps[10003] 一致):世界坐标 cm → 画布坐标
const OX = 306000
const OY = 408000
const SIDE = 408000
// 到达判定半径由用户调节:范围 10~50m(米 → 画布单位 ×2,即 50m=100 画布单位)
const NEAR_MIN_M = 10
const NEAR_MAX_M = 50
const LOOKAHEAD = 30 // 前瞻窗口:传送跨点时从当前进度向后扫多少个点找最近点
const TELEPORT = 300 // 相邻点距离超过该画布单位(≈150m)判定为直接传送,路线在此断开不画直线

// 路线随机配色。池子人工筛过,色相避开地图上已有标记:
// 玩家箭头红 / 奖牌四色(大块头红 ff5252、小不点橙 ff9100、婉转声绿 2e7d32、
// 粗嗓门浅蓝 40c4ff) / 野生污染紫 c792ea / 异色白。#HUE_BAN 再兜底过滤一遍。
const PALETTE = [
  // 青绿/青(避开粗嗓门浅蓝 180-220 色相,取更绿或更暗的青)
  '#00695c', '#00796b', '#00897b', '#26a69a', '#4db6ac', '#1de9b6', '#80cbc4', '#b2dfdb',
  // 蓝/靛(色相 >220,避开浅蓝禁区)
  '#283593', '#3949ab', '#3f51b5', '#5c6bc0', '#7986cb', '#9fa8da',
  // 玫红/粉(色相 320-345,偏紫,与正红箭头/大块头红可区分)
  '#ec407a', '#f06292', '#f48fb1', '#e91e63', '#d81b60',
  // 黄绿/黄(色相 48-95,低于婉转声绿 95-155 禁区)
  '#aeea00', '#c0ca33', '#cddc39', '#d4e157', '#ffeb3b', '#fff176', '#fff59d',
  // 深色(色相/亮度都不进禁区):地图背景偏亮(草原/沙地)时提供强对比
  '#212121', '#1a237e', '#004d40', '#880e4f',
]
// 地图已有标记的色相禁区(度,含边界):红(箭头/大块头)、橙(小不点)、绿(婉转声)、浅蓝(粗嗓门)、紫(污染)
const HUE_BAN = [[345, 360], [0, 22], [22, 48], [95, 155], [180, 220], [250, 290]]
const hexHue = (hex) => {
  const m = /^#?([0-9a-f]{6})$/i.exec(hex)
  if (!m) return -1
  const n = parseInt(m[1], 16)
  const r = n >> 16 & 255, g = n >> 8 & 255, b = n & 255
  const max = Math.max(r, g, b), min = Math.min(r, g, b)
  if (max === min) return -1 // 灰/白/黑无色相
  const d = max - min
  let h
  if (max === r) h = (g - b) / d + (g < b ? 6 : 0)
  else if (max === g) h = (b - r) / d + 2
  else h = (r - g) / d + 4
  return h * 60
}
const ROUTE_COLORS = PALETTE.filter((c) => {
  const h = hexHue(c)
  return !HUE_BAN.some(([a, b]) => h >= a && h <= b)
})
// Fisher-Yates 洗牌:每次进场景随机顺序分配,同时开多条路线颜色互不相同且不撞地图标记
const shuffle = (arr) => {
  const a = [...arr]
  for (let i = a.length - 1; i > 0; i--) {
    const j = Math.floor(Math.random() * (i + 1))
    ;[a[i], a[j]] = [a[j], a[i]]
  }
  return a
}

// —— 换色:点击路线色点切换颜色,自动偏向与地图背景/旧色/其它已开路线对比明显的色 ——
// hex → {h,s,l}(h 度,-1 表示灰/白/黑),供打分用
const hexHsl = (hex) => {
  const m = /^#?([0-9a-f]{6})$/i.exec(hex)
  if (!m) return null
  const n = parseInt(m[1], 16)
  const r = n >> 16 & 255, g = n >> 8 & 255, b = n & 255
  const max = Math.max(r, g, b), min = Math.min(r, g, b)
  const l = (max + min) / 510
  if (max === min) return { h: -1, s: 0, l }
  const d = max - min
  const s = d / (max + min < 510 ? max + min : 510 - (max + min))
  let h
  if (max === r) h = (g - b) / d + (g < b ? 6 : 0)
  else if (max === g) h = (b - r) / d + 2
  else h = (r - g) / d + 4
  return { h: h * 60, s, l }
}
// 色相环最短距离(0~180)
const hueDist = (a, b) => {
  const d = Math.abs(a - b)
  return Math.min(d, 360 - d)
}
const rgbToHex = (r, g, b) => '#' + [r, g, b].map((v) => v.toString(16).padStart(2, '0')).join('')
// —— 底图采样:整图缩到 SAMPLE×SAMPLE 存一份像素,按归一化 uv 取色 ——
// 为什么不采「整图平均色」:一条路线横跨草原/雪地/沙漠/水面,整图平均是个谁都不像的
// 中间色,拿它选色等于没选,换完照样撞背景。故改成**沿路线折线采样**:只统计这条路线
// 真正压过去的像素,得到它自己的背景色,再挑与它对比最大的颜色。
const SAMPLE = 256
const SAMPLES_PER_ROUTE = 96
let sampleCache = null   // { img, px: Uint8ClampedArray }
let samplePromise = null
const loadSample = (img) => {
  if (!img) return Promise.resolve(null)
  if (sampleCache && sampleCache.img === img) return Promise.resolve(sampleCache)
  if (sampleCache && sampleCache.img !== img) samplePromise = null // 换场景:旧采样作废
  if (samplePromise) return samplePromise
  samplePromise = new Promise((resolve) => {
    const im = new Image()
    im.crossOrigin = 'anonymous'
    im.onload = () => {
      try {
        const c = document.createElement('canvas')
        c.width = SAMPLE; c.height = SAMPLE
        const ctx = c.getContext('2d', { willReadFrequently: true })
        ctx.drawImage(im, 0, 0, SAMPLE, SAMPLE)
        sampleCache = { img, px: ctx.getImageData(0, 0, SAMPLE, SAMPLE).data }
      } catch { sampleCache = null } // 解码失败/跨域:退化成只用旧色与它色对比
      resolve(sampleCache)
    }
    im.onerror = () => { sampleCache = null; resolve(null) }
    im.src = imgURL(`bigmap/${img}.webp`)
  })
  return samplePromise
}
// 沿路线折线按**等弧长**取 SAMPLES_PER_ROUTE 个点读底图像素,平均成该路线自己的背景色。
// 等弧长而非逐点:路线点疏密不均(转弯密、直道稀),逐点平均会被密处的地形带偏。
// 传送段(相邻点跳变 > TELEPORT)不计入——那是瞬移,玩家并不从中间地上走过。
const routeBgHsl = (sample, points) => {
  if (!sample || !points || points.length < 2) return null
  const seg = []
  let total = 0
  for (let i = 1; i < points.length; i++) {
    const d = Math.hypot(points[i].x - points[i - 1].x, points[i].y - points[i - 1].y)
    seg.push(d > TELEPORT ? 0 : d) // 0 = 传送段,不在其上取样
    total += seg[seg.length - 1]
  }
  if (total <= 0) return null
  const px = sample.px
  const step = total / (SAMPLES_PER_ROUTE - 1)
  let r = 0, g = 0, b = 0, n = 0
  let si = 0, acc = 0 // 当前段下标、该段起点之前的累计弧长
  for (let k = 0; k < SAMPLES_PER_ROUTE; k++) {
    const target = k * step
    while (si < seg.length - 1 && acc + seg[si] < target) { acc += seg[si]; si++ }
    const t = seg[si] > 0 ? (target - acc) / seg[si] : 0
    const p0 = points[si], p1 = points[si + 1]
    const x = Math.min(SAMPLE - 1, Math.max(0, Math.round((p0.x + (p1.x - p0.x) * t) / GRID * SAMPLE)))
    const y = Math.min(SAMPLE - 1, Math.max(0, Math.round((p0.y + (p1.y - p0.y) * t) / GRID * SAMPLE)))
    const i = (y * SAMPLE + x) * 4
    r += px[i]; g += px[i + 1]; b += px[i + 2]; n++
  }
  return n ? hexHsl(rgbToHex(Math.round(r / n), Math.round(g / n), Math.round(b / n))) : null
}
// 从候选池挑色:评分 = 与该路线背景的色相/亮度对比(权重最高,防背景相近)
//  + 与旧色对比(换色后明显不同) + 与其它已开路线的最小色相距离(路线间可区分)。
// 取 top3 随机,避免固定两个色反复横跳。
const pickColor = (candidates, { oldColor, others, bgHsl }) => {
  const old = oldColor ? hexHsl(oldColor) : null
  const othersHsl = others.map(hexHsl).filter(Boolean)
  const scored = candidates.map((c) => {
    const h = hexHsl(c)
    let score = 0
    if (bgHsl && h && h.h >= 0) {
      score += hueDist(h.h, bgHsl.h) / 180 * 1.5
      score += Math.abs(h.l - bgHsl.l) * 1.0
    }
    if (h && old && old.h >= 0) score += hueDist(h.h, old.h) / 180 * 0.8
    if (h) {
      let minD = Infinity
      for (const o of othersHsl) if (o.h >= 0) minD = Math.min(minD, hueDist(h.h, o.h))
      score += (minD === Infinity ? 180 : minD) / 180 * 0.6
    }
    return { c, score }
  }).sort((a, b) => b.score - a.score)
  const top = scored.slice(0, 3)
  return top[Math.floor(Math.random() * top.length)].c
}

const loadKeys = () => {
  try {
    const v = JSON.parse(localStorage.getItem(ROUTES_LS_KEY))
    return Array.isArray(v) ? new Set(v) : new Set()
  } catch { return new Set() }
}
const loadJSON = (k, fallback) => {
  try {
    const v = JSON.parse(localStorage.getItem(k))
    return v == null ? fallback : v
  } catch { return fallback }
}

// 世界坐标(cm) → 8192 画布坐标(与底图投影 u=(x-ox)/side 自洽)
const toCanvas = (x, y) => [(x - OX) / SIDE * GRID, (y - OY) / SIDE * GRID]

// useRoutes 管理跑图路线图层:仅当场景是 10003 时拉取路线清单与全部点数据;
// 返回 kinds(可开关的路线列表,含点数/颜色/进度)、marks(已开启、可绘制的路线)
// 与跟走模式状态(follow/toggleFollow/resetProgress)。
export function useRoutes(account, pos) {
  const res = pos && pos.sceneResId
  const [routes, setRoutes] = useState([]) // [{name, count, points, color, on}]
  const [open, setOpen] = useState(false)  // 图层面板展开
  const [follow, setFollow] = useState(() => loadJSON(FOLLOW_LS_KEY, false))
  const [progress, setProgress] = useState(() => loadJSON(PROGRESS_LS_KEY, {}))
  const [nearM, setNearMState] = useState(() => {
    const v = loadJSON(NEAR_M_LS_KEY, NEAR_MAX_M)
    return Math.min(NEAR_MAX_M, Math.max(NEAR_MIN_M, v))
  })
  const onRef = useRef(loadKeys())
  const lastCheckRef = useRef(0) // 上次判定时间戳,时间节流用

  const img = pos && pos.img // 底图名:用于采样路线沿途地形色
  useEffect(() => {
    onRef.current = loadKeys() // 每次进场景重新读一次用户记忆
    lastCheckRef.current = 0
    if (res !== SCENE) { setRoutes([]); return }
    let alive = true
    fetch('/route-map/data/index.json', { cache: 'no-store' })
      .then((r) => r.json())
      .then(async (names) => {
        const list = await Promise.all(names.map(async (name) => {
          const d = await fetch('/route-map/data/' + encodeURIComponent(name), { cache: 'no-store' }).then((r) => r.json())
          return {
            name,
            short: name.replace(/\.json$/, ''),
            count: d.points.length,
            points: d.points,
            color: '',
            on: onRef.current.has(name),
          }
        }))
        // 配色:用户手动换过色的沿用;其余按「这条路线沿途的地形色 + 已分出去的色」
        // 逐条贪心挑对比最大的。候选池先洗牌再评分(sort 稳定),保留进场景的随机性,
        // 又不至于像纯随机那样分到与地形同色的号。
        const savedColors = loadJSON(COLORS_LS_KEY, {}) || {}
        const sample = await loadSample(img)
        const used = []
        for (const r of list) {
          if (savedColors[r.name]) { r.color = savedColors[r.name]; used.push(r.color); continue }
          const avail = shuffle(ROUTE_COLORS.filter((c) => !used.includes(c)))
          r.color = pickColor(avail.length ? avail : shuffle(ROUTE_COLORS), {
            others: used,
            bgHsl: routeBgHsl(sample, r.points),
          })
          used.push(r.color)
        }
        if (alive) setRoutes(list)
      })
      .catch(() => {})
    return () => { alive = false }
  }, [res, account, img])

  // 跟走进度:玩家位置变化时,对每条开启路线做「顺序推进 + 前瞻窗口」判定。
  // 只进不退:从 cur+1 起往后扫 LOOKAHEAD 个点,遇到第一个距离 < NEAR 的点就推进到那,
  // 支持传送跨点,又不会因路线绕圈回起点而误判。
  useEffect(() => {
    if (!follow || res !== SCENE || !pos || pos.x == null || pos.y == null) return
    // 时间节流:2 秒内最多判定一次。不用位移防抖——玩家在点周围绕圈/折返时
    // 直线位移很小会被一直拦截,导致「走了好久才触发」;时间节流保证原地徘徊
    // 也会周期性检查,只要在 NEAR 半径内就推进。
    const now = Date.now()
    if (now - lastCheckRef.current < 2000) return
    lastCheckRef.current = now
    const px = pos.x, py = pos.y
    const pc = toCanvas(px, py)
    setProgress((prev) => {
      let next = null
      for (const r of routes) {
        if (!r.on || r.points.length < 2) continue
        const cur = prev[r.name] ?? -1
        const from = cur + 1
        const end = Math.min(r.points.length, from + LOOKAHEAD)
        for (let i = from; i < end; i++) {
          const p = r.points[i]
          if (Math.hypot(p.x - pc[0], p.y - pc[1]) < nearM * 2) { // 米 → 画布单位(1m = 2 单位)
            if (i !== cur) (next ??= { ...prev })[r.name] = i
            break
          }
        }
      }
      return next || prev
    })
  }, [pos, follow, routes, res, nearM])

  const toggle = useCallback((name) => {
    setRoutes((prev) => {
      const on = !prev.find((r) => r.name === name).on
      const next = new Set(onRef.current)
      on ? next.add(name) : next.delete(name)
      onRef.current = next
      return prev.map((r) => (r.name === name ? { ...r, on } : r))
    })
  }, [])

  // 一键全开/全关:全开 = 勾选所有路线,全关 = 全部取消,均持久化
  const setAll = useCallback((on) => {
    setRoutes((prev) => {
      const next = new Set(on ? prev.map((r) => r.name) : [])
      onRef.current = next
      return prev.map((r) => ({ ...r, on }))
    })
  }, [])

  // 换色:点击路线色点触发。采样**这条路线沿途**的底图色后,从池中选「与该背景、
  // 旧色、其它路线对比明显」的新色,更新状态并持久化(map.routeColors),刷新后沿用。
  const cycleColor = useCallback((name) => {
    const r = routes.find((x) => x.name === name)
    if (!r) return
    const others = routes.filter((x) => x.on && x.name !== name).map((x) => x.color)
    const candidates = shuffle(ROUTE_COLORS.filter((c) => c !== r.color))
    loadSample(pos && pos.img).then((sample) => {
      const color = pickColor(candidates, { oldColor: r.color, others, bgHsl: routeBgHsl(sample, r.points) })
      setRoutes((prev) => prev.map((x) => (x.name === name ? { ...x, color } : x)))
    })
  }, [routes, pos])

  const toggleFollow = useCallback(() => setFollow((f) => !f), [])

  const resetProgress = useCallback(() => setProgress({}), [])

  // 判定半径(米):夹取到 10~50,持久化
  const setNearM = useCallback((m) => {
    setNearMState(Math.min(NEAR_MAX_M, Math.max(NEAR_MIN_M, m)))
  }, [])

  // —— 持久化:全部收口到这几个 effect ——
  // 原先散在各 setValue 的 updater 内;StrictMode(见 main.jsx)会把 updater 调用两次,
  // 副作用写在里面会重复执行。这里统一按依赖落盘,语义等价且不重复。
  useEffect(() => {
    try {
      if (Object.keys(progress).length) localStorage.setItem(PROGRESS_LS_KEY, JSON.stringify(progress))
      else localStorage.removeItem(PROGRESS_LS_KEY)
    } catch { /* 隐私模式下忽略 */ }
  }, [progress])

  useEffect(() => {
    try {
      localStorage.setItem(ROUTES_LS_KEY, JSON.stringify(routes.filter((r) => r.on).map((r) => r.name)))
    } catch { /* 同上 */ }
  }, [routes])

  // 路线配色按 name 存(与 routes 分离:换场景重建 routes 时不能丢用户挑的颜色)
  useEffect(() => {
    try {
      const store = loadJSON(COLORS_LS_KEY, {}) || {}
      for (const r of routes) if (r.color) store[r.name] = r.color
      localStorage.setItem(COLORS_LS_KEY, JSON.stringify(store))
    } catch { /* 同上 */ }
  }, [routes])

  useEffect(() => {
    try { localStorage.setItem(FOLLOW_LS_KEY, JSON.stringify(follow)) } catch { /* 同上 */ }
  }, [follow])

  useEffect(() => {
    try { localStorage.setItem(NEAR_M_LS_KEY, JSON.stringify(nearM)) } catch { /* 同上 */ }
  }, [nearM])

  const kinds = useMemo(() => routes.map((r) => ({ ...r, progress: progress[r.name] ?? -1 })), [routes, progress])
  const marks = useMemo(() => routes.filter((r) => r.on).map((r) => ({
    ...r,
    progress: progress[r.name] ?? -1,
    follow,
  })), [routes, progress, follow])

  return { kinds, marks, open, toggleOpen: () => setOpen((o) => !o), toggle, setAll, cycleColor, follow, toggleFollow, resetProgress, nearM, setNearM }
}

// 端点/线共用的深色外环。底图地形色不可控(草原绿 / 雪地白 / 沙漠黄 / 洞穴暗),
// 单靠路线色总有一段糊进背景;外环保证亮背景上也有轮廓(暗背景靠白描边那道)。
const HALO = 'rgba(0, 0, 0, .5)'
const HALO_W = 1.6

// RouteLayer 把路线画进 .map-world:一条路线一个折线 <path> + 起终点圆。
// SVG 无 viewBox,width/height=mapPx,用户坐标即底图像素(与其它标记同一坐标系)。
// 跟走模式下只画 progress 之后的线:起点圆换成「已到达点」,下一目标点画高亮大圆。
// 传送打断:相邻点距离 > TELEPORT 判定为直接传送(不画那条笔直长线),在传送落点用
// 路线同色画一个「下一起点」菱形标记,提示从这里继续走。
//
// —— 可见性:三层描边(casing)——
// 颜色再怎么挑也扛不住地形:一条路线横穿草原/雪地/水面,总有某段与背景同色。
// 故不指望单色解决,改给线加 casing:深色外圈 + 白色内圈 + 彩色线,三趟画
// (宽度见 styles/map.css 的 .map-route-casing-*;改那里记得改 pipGeom.js 的
// SIZES.routeCasingDark/Light)。亮背景靠深色外圈勾边,暗背景靠白色内圈,
// 路线色始终浮在地形之上。端点同理:白描边外再套一圈深色环。
// 三趟按「全部深色 → 全部白色 → 全部彩色」而非「一条路线画完三层再画下一条」——
// 否则后画路线的深色描边会压在先画路线的彩线上,交叉处看着像被切断。
export const RouteLayer = React.memo(({ marks, mapPx }) => {
  const geo = React.useMemo(() => marks.map((r) => {
    const from = r.follow && r.progress >= 0 ? Math.min(r.progress, r.points.length - 1) : 0
    const pts = r.points.slice(from)
    // 传送断点:相邻两点距离 > TELEPORT。断点 i 表示 pts[i] 是传送落点(新一段的起点)
    const teleports = []
    for (let i = 1; i < pts.length; i++) {
      if (Math.hypot(pts[i].x - pts[i - 1].x, pts[i].y - pts[i - 1].y) > TELEPORT) teleports.push(i)
    }
    // 折线:在断点处断开重起 M(不画传送那条笔直长线),正常段连 L
    const teleSet = new Set(teleports)
    const d = pts.map((p, i) =>
      `${i === 0 || teleSet.has(i) ? 'M' : 'L'}${(p.x / GRID * mapPx).toFixed(1)} ${(p.y / GRID * mapPx).toFixed(1)}`).join('')
    const s = pts[0]
    const e = pts[pts.length - 1]
    const nx = r.follow && r.progress >= 0 && r.progress + 1 < r.points.length ? r.points[r.progress + 1] : null
    return {
      key: r.name,
      d,
      color: r.color,
      sx: s.x / GRID * mapPx, sy: s.y / GRID * mapPx,
      ex: e.x / GRID * mapPx, ey: e.y / GRID * mapPx,
      teleports: teleports.map((i) => ({ x: pts[i].x / GRID * mapPx, y: pts[i].y / GRID * mapPx })),
      nx: nx && { x: nx.x / GRID * mapPx, y: nx.y / GRID * mapPx },
      showStart: !r.follow || r.progress < 0, // 跟走开始后起点圆消失,改由到达点标记
    }
  }), [marks, mapPx])
  if (!geo.length) return null
  return (
    <svg className="map-routes" width={mapPx} height={mapPx}>
      {/* 三层描边按「全部深色 → 全部白色 → 全部彩色」分趟画,见文件头说明 */}
      <g className="map-route-casing-dark">
        {geo.map((g) => g.d && <path key={g.key} d={g.d} />)}
      </g>
      <g className="map-route-casing-light">
        {geo.map((g) => g.d && <path key={g.key} d={g.d} />)}
      </g>
      <g className="map-route-line">
        {geo.map((g) => g.d && <path key={g.key} d={g.d} stroke={g.color} />)}
      </g>
      {geo.map((g) => (
        <g key={g.key}>
          {/* 传送落点:路线色菱形 + 白描边 + 白点,表示「从上一段直接传送到此,从这继续走」 */}
          {g.teleports.map((t, i) => (
            <g key={i} transform={`translate(${t.x} ${t.y}) rotate(45)`}>
              <rect x={-6.7} y={-6.7} width={13.4} height={13.4} fill="none" stroke={HALO} strokeWidth={HALO_W} />
              <rect x={-5} y={-5} width={10} height={10} fill={g.color} stroke="#fff" strokeWidth={1.8} opacity={0.95} />
              <rect x={-1.5} y={-1.5} width={3} height={3} fill="#fff" />
            </g>
          ))}
          {g.showStart && (
            /* 起点:白心 + 黑描边,本身就是双色,任何底图上都看得见 */
            <circle cx={g.sx} cy={g.sy} r={6} fill="#fff" stroke="#000" strokeWidth={1.5} />
          )}
          {!g.showStart && (
            /* 已到达点:深色外环 + 路线色圆 + 白描边 */
            <>
              <circle cx={g.sx} cy={g.sy} r={7.05} fill="none" stroke={HALO} strokeWidth={HALO_W} />
              <circle cx={g.sx} cy={g.sy} r={5.5} fill={g.color} stroke="#fff" strokeWidth={1.5} />
            </>
          )}
          {g.nx && (
            /* 下一目标点:深色外环 + 路线色大圆 + 白描边 + 白心 */
            <g>
              <circle cx={g.nx.x} cy={g.nx.y} r={10.9} fill="none" stroke={HALO} strokeWidth={1.8} />
              <circle cx={g.nx.x} cy={g.nx.y} r={9} fill={g.color} stroke="#fff" strokeWidth={2} opacity={0.95} />
              <circle cx={g.nx.x} cy={g.nx.y} r={3} fill="#fff" />
            </g>
          )}
          {/* 终点:深色外环 + 路线色圆 + 白描边 */}
          <circle cx={g.ex} cy={g.ey} r={6.4} fill="none" stroke={HALO} strokeWidth={HALO_W} />
          <circle cx={g.ex} cy={g.ey} r={5} fill={g.color} stroke="#fff" strokeWidth={1.2} />
        </g>
      ))}
    </svg>
  )
})
