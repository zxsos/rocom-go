import React, { useState, useEffect, useMemo, useRef, useCallback } from 'react'

// —— 跑图收集路线图层(仅 10003 卡洛西亚大陆)——
// 路线数据是 B站泽口博士的收集路线,与页面 /route-map/ 共用同一份静态 JSON:
//   web/public/route-map/data/index.json(文件名列表) + 各路线 JSON。
// 路线坐标为 8192x8192 画布,归一化 u=x/8192 后与底图投影对齐,前端只负责开关与摆放。
// 开关选择存 localStorage,与 POI 图层记忆互不干扰。
// 跟走模式:玩家位置(pos 世界坐标,cm)换算回 8192 画布,与路线点比距离;
// 到达某点(半径 NEAR)后该点之前的线隐藏,只显示剩余路线 + 下一目标点。
const ROUTES_LS_KEY = 'map.routeLayers'
const FOLLOW_LS_KEY = 'map.routeFollow'
const PROGRESS_LS_KEY = 'map.routeProgress'
const NEAR_M_LS_KEY = 'map.routeNearM'
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

  useEffect(() => {
    onRef.current = loadKeys() // 每次进场景重新读一次用户记忆
    lastCheckRef.current = 0
    if (res !== SCENE) { setRoutes([]); return }
    let alive = true
    fetch('/route-map/data/index.json', { cache: 'no-store' })
      .then((r) => r.json())
      .then(async (names) => {
        const colors = shuffle(ROUTE_COLORS) // 每次进场景随机色序,避免固定分配
        const list = await Promise.all(names.map(async (name, i) => {
          const d = await fetch('/route-map/data/' + encodeURIComponent(name), { cache: 'no-store' }).then((r) => r.json())
          return {
            name,
            short: name.replace(/\.json$/, ''),
            count: d.points.length,
            points: d.points,
            color: colors[i % colors.length],
            on: onRef.current.has(name),
          }
        }))
        if (alive) setRoutes(list)
      })
      .catch(() => {})
    return () => { alive = false }
  }, [res, account])

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
      if (next) localStorage.setItem(PROGRESS_LS_KEY, JSON.stringify(next))
      return next || prev
    })
  }, [pos, follow, routes, res, nearM])

  const toggle = useCallback((name) => {
    setRoutes((prev) => {
      const on = !prev.find((r) => r.name === name).on
      const next = new Set(onRef.current)
      on ? next.add(name) : next.delete(name)
      onRef.current = next
      localStorage.setItem(ROUTES_LS_KEY, JSON.stringify([...next]))
      return prev.map((r) => (r.name === name ? { ...r, on } : r))
    })
  }, [])

  const toggleFollow = useCallback(() => {
    setFollow((f) => {
      const nf = !f
      localStorage.setItem(FOLLOW_LS_KEY, JSON.stringify(nf))
      return nf
    })
  }, [])

  const resetProgress = useCallback(() => {
    setProgress({})
    localStorage.removeItem(PROGRESS_LS_KEY)
  }, [])

  // 判定半径(米):夹取到 10~50,持久化
  const setNearM = useCallback((m) => {
    const v = Math.min(NEAR_MAX_M, Math.max(NEAR_MIN_M, m))
    setNearMState(v)
    localStorage.setItem(NEAR_M_LS_KEY, JSON.stringify(v))
  }, [])

  const kinds = useMemo(() => routes.map((r) => ({ ...r, progress: progress[r.name] ?? -1 })), [routes, progress])
  const marks = useMemo(() => routes.filter((r) => r.on).map((r) => ({
    ...r,
    progress: progress[r.name] ?? -1,
    follow,
  })), [routes, progress, follow])

  return { kinds, marks, open, toggleOpen: () => setOpen((o) => !o), toggle, follow, toggleFollow, resetProgress, nearM, setNearM }
}

// RouteLayer 把路线画进 .map-world:一条路线一个折线 <path> + 起终点圆。
// SVG 无 viewBox,width/height=mapPx,用户坐标即底图像素(与其它标记同一坐标系)。
// 跟走模式下只画 progress 之后的线:起点圆换成「已到达点」,下一目标点画高亮大圆。
// 传送打断:相邻点距离 > TELEPORT 判定为直接传送(不画那条笔直长线),在传送落点用
// 路线同色画一个「下一起点」菱形标记,提示从这里继续走。
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
      {geo.map((g) => (
        <g key={g.key}>
          {g.d && (
            <path d={g.d} fill="none" stroke={g.color} strokeWidth={2.5}
              strokeLinejoin="round" strokeLinecap="round" opacity={0.9} />
          )}
          {/* 传送落点:路线色菱形 + 白描边 + 白点,表示「从上一段直接传送到此,从这继续走」 */}
          {g.teleports.map((t, i) => (
            <g key={i} transform={`translate(${t.x} ${t.y}) rotate(45)`}>
              <rect x={-5} y={-5} width={10} height={10} fill={g.color} stroke="#fff" strokeWidth={1.8} opacity={0.95} />
              <rect x={-1.5} y={-1.5} width={3} height={3} fill="#fff" />
            </g>
          ))}
          {g.showStart && (
            <circle cx={g.sx} cy={g.sy} r={6} fill="#fff" stroke="#000" strokeWidth={1.5} />
          )}
          {!g.showStart && (
            /* 已到达点:路线色圆 + 白描边 */
            <circle cx={g.sx} cy={g.sy} r={5.5} fill={g.color} stroke="#fff" strokeWidth={1.5} />
          )}
          {g.nx && (
            /* 下一目标点:路线色大圆 + 白描边 + 白心 */
            <g>
              <circle cx={g.nx.x} cy={g.nx.y} r={9} fill={g.color} stroke="#fff" strokeWidth={2} opacity={0.95} />
              <circle cx={g.nx.x} cy={g.nx.y} r={3} fill="#fff" />
            </g>
          )}
          {/* 终点:路线色圆 + 白描边 */}
          <circle cx={g.ex} cy={g.ey} r={5} fill={g.color} stroke="#fff" strokeWidth={1.2} />
        </g>
      ))}
    </svg>
  )
})
