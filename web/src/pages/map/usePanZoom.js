import { useState, useEffect, useRef, useCallback } from 'react'
import { ZOOM_MIN, ZOOM_MAX, ZOOM_FALLBACK, clamp } from './motion'

// usePanZoom 地图视图状态与手势:zoom 缩放、follow 跟随玩家(玩家居中)、
// 指针拖动(单指/鼠标)平移、双指捏合与滚轮缩放。
// 视口中心对应的地图归一化坐标放 focusRef(跟随时每帧跟着玩家走,不进 state,否则每帧重渲染整页);
// zoom/follow/vp 同时放进 stRef 供指针回调与逐帧循环即时读取,避免闭包过期。
// active=视口元素当前是否渲染(随 hasMap 出现/消失,重挂 ResizeObserver)。
export function usePanZoom(active) {
  const vpRef = useRef(null)
  const [vp, setVp] = useState({ w: 0, h: 0 }) // 视口尺寸(归一化坐标 → 像素;地图边长 = min(w,h)*zoom)
  const [zoom, setZoom] = useState(ZOOM_FALLBACK)
  const [follow, setFollow] = useState(true)
  const focusRef = useRef({ u: 0.5, v: 0.5 })
  const stRef = useRef({ zoom, follow, vp })
  stRef.current = { zoom, follow, vp }

  // 测量视口尺寸(视口元素随 active 出现/消失)。
  useEffect(() => {
    const el = vpRef.current
    if (!el) return
    const ro = new ResizeObserver(() => setVp({ w: el.clientWidth, h: el.clientHeight }))
    ro.observe(el)
    setVp({ w: el.clientWidth, h: el.clientHeight })
    return () => ro.disconnect()
  }, [active])

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
  const onPointerDown = (e) => {
    // 点在缩放/回中控件上:不捕获指针、不启动平移,否则 setPointerCapture 会把 pointerup
    // 重定向到视口,桌面端按钮的 click 事件就不触发(移动端触摸 click 合成方式不同,不受影响)。
    if (e.target.closest?.('.map-ctrl')) return
    vpRef.current.setPointerCapture?.(e.pointerId)
    ptrs.current.set(e.pointerId, { x: e.clientX, y: e.clientY })
  }
  const onPointerMove = (e) => {
    const p = ptrs.current.get(e.pointerId)
    if (!p) return
    const prev = { x: p.x, y: p.y }
    p.x = e.clientX; p.y = e.clientY
    const pts = [...ptrs.current.values()]
    if (pts.length >= 2) {
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
      focusRef.current = { u: f.u - (e.clientX - prev.x) / (base * z), v: f.v - (e.clientY - prev.y) / (base * z) }
    }
  }
  const onPointerUp = (e) => {
    ptrs.current.delete(e.pointerId)
    if (ptrs.current.size < 2) pinch.current = 0
  }
  const onWheel = (e) => {
    const rect = vpRef.current.getBoundingClientRect()
    zoomAround(e.deltaY < 0 ? 1.15 : 1 / 1.15, e.clientX - rect.left, e.clientY - rect.top)
  }

  return {
    vpRef, vp, zoom, setZoom, follow, setFollow, focusRef, stRef, zoomAround,
    handlers: { onPointerDown, onPointerMove, onPointerUp, onPointerCancel: onPointerUp, onWheel },
  }
}
