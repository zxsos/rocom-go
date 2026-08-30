import { useState, useLayoutEffect, useRef, useCallback, useMemo } from 'react'
import { ZOOM_MIN, ZOOM_MAX, ZOOM_FALLBACK, clamp } from './motion'

// tapSlop 是「按下到抬起」还算点一下的位移上限(px):超过就当在拖地图。
const tapSlop = 6

// usePanZoom 地图视图状态与手势:zoom 缩放、follow 跟随玩家(玩家居中)、
// 指针拖动(单指/鼠标)平移、双指捏合与滚轮缩放。
// 视口中心对应的地图归一化坐标放 focusRef(跟随时每帧跟着玩家走,不进 state,否则每帧重渲染整页);
// zoom/follow/vp 同时放进 stRef 供指针回调与逐帧循环即时读取,避免闭包过期。
// 尺寸的测量不依赖任何外部标志位:视口元素的挂载/卸载由 MapViz(当前是否在地图页、
// 有没有底图)决定,与本 hook 解耦,故这里用回调 ref 跟着元素本身走(见 attachVp)。
// onTap(target) = 在地图上点了一下(没拖动)时回调,target 是**按下那一刻**的元素:
// 地图内的标记不能用普通 onClick——平移要 setPointerCapture,pointerup 被重定向到视口后
// 浏览器就不再往标记上派发 click 了(桌面端点头像没反应正是这个原因),故这里自己判点击。
export function usePanZoom(onTap) {
  const vpRef = useRef(null)
  const [vpEl, setVpEl] = useState(null) // 视口元素本身:它随路由挂载/卸载,与引擎状态无关
  const [vp, setVp] = useState({ w: 0, h: 0 }) // 视口尺寸(归一化坐标 → 像素;地图边长 = min(w,h)*zoom)
  const [zoom, setZoom] = useState(ZOOM_FALLBACK)
  const [follow, setFollow] = useState(true)
  const focusRef = useRef({ u: 0.5, v: 0.5 })
  const stRef = useRef({ zoom, follow, vp })
  stRef.current = { zoom, follow, vp }

  // 测量必须跟着**元素**走,不能跟着 active 走。
  //
  // 引擎常驻在 App 层(画中画要跨页面存活),位置数据常在用户还不在地图页时就已到货 ——
  // 那时 .map-vp 尚未挂载,以 [active] 为依赖的 effect 拿不到元素便空转;等用户切到 /map,
  // 元素挂上了,而 active 早已是 true、不再变化,effect 永远不重挂 ResizeObserver,
  // vp 恒为 {0,0}。后果:mapPx = (min(0,0)||1) * zoom ≈ 5px,**所有图标挤在左上角、
  // 底图缩成几像素即黑色背景**;手动刷新能好,是因为刷新时人已在 /map,active 翻转那一刻
  // 元素就在,一次挂上。故这里用回调 ref 把元素本身收进 state,以它为依赖重挂。
  const attachVp = useCallback((el) => {
    vpRef.current = el
    setVpEl(el)
  }, [])

  // 用 layout effect 而非 effect:首帧就该带着真实尺寸出图,否则会先闪一帧塌掉的地图。
  useLayoutEffect(() => {
    if (!vpEl) return
    const measure = () => setVp({ w: vpEl.clientWidth, h: vpEl.clientHeight })
    const ro = new ResizeObserver(measure)
    ro.observe(vpEl)
    measure()
    return () => ro.disconnect()
  }, [vpEl])

  // 以视口某点(px,py,相对视口左上)为锚缩放:保持该点下的地图坐标不动。
  const zoomAround = useCallback((factor, px, py) => {
    const { zoom: z, vp: v } = stRef.current
    const f = focusRef.current
    const nz = clamp(z * factor, ZOOM_MIN, ZOOM_MAX)
    if (nz === z || !v.w) return
    const base = Math.min(v.w, v.h)
    const mapU = f.u + (px - v.w / 2) / (base * z)
    const mapV = f.v + (py - v.h / 2) / (base * z)
    setFollow(false)
    setZoom(nz)
    focusRef.current = { u: mapU - (px - v.w / 2) / (base * nz), v: mapV - (py - v.h / 2) / (base * nz) }
  }, [])

  const ptrs = useRef(new Map())
  const pinch = useRef(0)
  const tap = useRef(null) // 本次按下是否还够得上「点一下」(拖过/多指即作废)
  const tapCb = useRef(onTap)
  tapCb.current = onTap
  const onPointerDown = useCallback((e) => {
    // 点在缩放/回中控件上:不捕获指针、不启动平移,否则 setPointerCapture 会把 pointerup
    // 重定向到视口,桌面端按钮的 click 事件就不触发(移动端触摸 click 合成方式不同,不受影响)。
    if (e.target.closest?.('.map-ctrl')) return
    vpRef.current.setPointerCapture?.(e.pointerId)
    ptrs.current.set(e.pointerId, { x: e.clientX, y: e.clientY })
    // 记下按下那一刻的元素:之后指针被捕获,move/up 的 target 一律是视口。
    tap.current = ptrs.current.size > 1 ? null
      : { id: e.pointerId, x: e.clientX, y: e.clientY, target: e.target }
  }, [])
  const onPointerMove = useCallback((e) => {
    const p = ptrs.current.get(e.pointerId)
    if (!p) return
    const t = tap.current
    if (t && (Math.abs(e.clientX - t.x) > tapSlop || Math.abs(e.clientY - t.y) > tapSlop)) {
      tap.current = null // 拖开了,这次不算点击
    }
    const prev = { x: p.x, y: p.y }
    p.x = e.clientX; p.y = e.clientY
    const pts = [...ptrs.current.values()]
    if (pts.length >= 2) {
      tap.current = null
      // 捏合:按两指距离变化缩放,锚点为两指中点(相对视口)。
      const [a, b] = pts
      const dist = Math.hypot(a.x - b.x, a.y - b.y)
      if (pinch.current) {
        const rect = vpRef.current.getBoundingClientRect()
        zoomAround(dist / pinch.current, (a.x + b.x) / 2 - rect.left, (a.y + b.y) / 2 - rect.top)
      }
      pinch.current = dist
    } else {
      // 平移:把屏幕位移换算成归一化坐标偏移(下一帧 applyFrame 即生效)。
      const { zoom: z, vp: v } = stRef.current
      const base = Math.min(v.w, v.h) || 1
      const f = focusRef.current
      setFollow(false)
      // 立即同步 stRef.current.follow:setFollow 走 setState 要等下次渲染,
      // 而 RAF 可能在这之前 tick(拖动中 RAF 保持运行),applyFrame 读到 follow=true
      // 会把 focus 拉回玩家,画面抖动/拖不动。手动同步后 RAF 立即看到 false。
      stRef.current.follow = false
      focusRef.current = { u: f.u - (e.clientX - prev.x) / (base * z), v: f.v - (e.clientY - prev.y) / (base * z) }
    }
  }, [zoomAround])
  const onPointerUp = useCallback((e) => {
    ptrs.current.delete(e.pointerId)
    if (ptrs.current.size < 2) pinch.current = 0
    const t = tap.current
    tap.current = null
    if (t && t.id === e.pointerId && e.type === 'pointerup') {
      tapCb.current?.(t.target)
    }
  }, [])
  const onWheel = useCallback((e) => {
    const rect = vpRef.current.getBoundingClientRect()
    zoomAround(e.deltaY < 0 ? 1.15 : 1 / 1.15, e.clientX - rect.left, e.clientY - rect.top)
  }, [zoomAround])

  // handlers 整体 useMemo 稳定:MapPage 随位置推送高频重渲染,若每次重建 handlers,.map-vp
  // 的 props 每次都变;稳定后视口元素本身也跳过无谓的重协调。
  // 四个回调都只读 refs(stRef/focusRef/vpRef/tapCb/指针表),无闭包过期问题。
  const handlers = useMemo(() => ({
    onPointerDown, onPointerMove, onPointerUp, onPointerCancel: onPointerUp, onWheel,
  }), [onPointerDown, onPointerMove, onPointerUp, onWheel])

  return {
    vpRef, attachVp, vp, zoom, setZoom, follow, setFollow, focusRef, stRef, zoomAround,
    handlers,
  }
}
