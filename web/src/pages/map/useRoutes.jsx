import React, { useState, useEffect, useMemo, useRef, useCallback } from 'react'

// —— 跑图收集路线图层(仅 10003 卡洛西亚大陆)——
// 路线数据是 B站泽口博士的收集路线,与页面 /route-map/ 共用同一份静态 JSON:
//   web/public/route-map/data/index.json(文件名列表) + 各路线 JSON。
// 路线坐标为 8192x8192 画布,归一化 u=x/8192 后与底图投影对齐,前端只负责开关与摆放。
// 开关选择存 localStorage,与 POI 图层记忆互不干扰。
const ROUTES_LS_KEY = 'map.routeLayers'
const SCENE = 10003 // 卡洛西亚大陆(路线数据只在该场景有意义)
const GRID = 8192

const PALETTE = [
  '#e63946', '#499ed5', '#26890c', '#ffb000', '#9c27b0', '#00897b', '#ff5722',
  '#795548', '#3f51b5', '#00796b', '#e91e63', '#18ffff', '#8bc34a', '#fff176',
  '#ab47bc', '#607d8b', '#ff7043', '#1de9b6', '#d500f9', '#c5e1a5', '#ff6f00',
  '#00bcd4', '#d32f2f', '#76ff03', '#7c4dff', '#26a69a', '#ef5350', '#5c6bc0',
  '#ffca28', '#42a5f5', '#ef6c00', '#66bb6a', '#ec407a', '#29b6f6',
]
const loadKeys = () => {
  try {
    const v = JSON.parse(localStorage.getItem(ROUTES_LS_KEY))
    return Array.isArray(v) ? new Set(v) : new Set()
  } catch { return new Set() }
}

// useRoutes 管理跑图路线图层:仅当场景是 10003 时拉取路线清单与全部点数据;
// 返回 kinds(可开关的路线列表,含点数/颜色)与 marks(已开启、可绘制的路线)。
export function useRoutes(account, res) {
  const [routes, setRoutes] = useState([]) // [{name, count, points, color, on}]
  const [open, setOpen] = useState(false)  // 图层面板展开
  const onRef = useRef(loadKeys())

  useEffect(() => {
    onRef.current = loadKeys() // 每次进场景重新读一次用户记忆
    if (res !== SCENE) { setRoutes([]); return }
    let alive = true
    fetch('/route-map/data/index.json', { cache: 'no-store' })
      .then((r) => r.json())
      .then(async (names) => {
        const list = await Promise.all(names.map(async (name, i) => {
          const d = await fetch('/route-map/data/' + encodeURIComponent(name), { cache: 'no-store' }).then((r) => r.json())
          return {
            name,
            short: name.replace(/\.json$/, ''),
            count: d.points.length,
            points: d.points,
            color: PALETTE[i % PALETTE.length],
            on: onRef.current.has(name),
          }
        }))
        if (alive) setRoutes(list)
      })
      .catch(() => {})
    return () => { alive = false }
  }, [res, account])

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

  const marks = useMemo(() => routes.filter((r) => r.on), [routes])

  return { kinds: routes, marks, open, toggleOpen: () => setOpen((o) => !o), toggle }
}

// RouteLayer 把路线画进 .map-world:一条路线一个折线 <path> + 起终点圆。
// SVG 无 viewBox,width/height=mapPx,用户坐标即底图像素(与其它标记同一坐标系)。
export const RouteLayer = React.memo(({ marks, mapPx }) => {
  const geo = React.useMemo(() => marks.map((r) => {
    const d = r.points.map((p, i) =>
      `${i ? 'L' : 'M'}${(p.x / GRID * mapPx).toFixed(1)} ${(p.y / GRID * mapPx).toFixed(1)}`).join('')
    const s = r.points[0]
    const e = r.points[r.points.length - 1]
    return {
      key: r.name,
      d,
      color: r.color,
      sx: s.x / GRID * mapPx, sy: s.y / GRID * mapPx,
      ex: e.x / GRID * mapPx, ey: e.y / GRID * mapPx,
    }
  }), [marks, mapPx])
  if (!geo.length) return null
  return (
    <svg className="map-routes" width={mapPx} height={mapPx}>
      {geo.map((g) => (
        <g key={g.key}>
          <path d={g.d} fill="none" stroke={g.color} strokeWidth={2.5}
            strokeLinejoin="round" strokeLinecap="round" opacity={0.9} />
          {/* 起点:白底深描边实心圆 */}
          <circle cx={g.sx} cy={g.sy} r={6} fill="#fff" stroke="#000" strokeWidth={1.5} />
          {/* 终点:路线色圆 + 白描边 */}
          <circle cx={g.ex} cy={g.ey} r={5} fill={g.color} stroke="#fff" strokeWidth={1.2} />
        </g>
      ))}
    </svg>
  )
})
