import React, { useState, useEffect, useRef, useCallback, useContext } from 'react'
import { createPortal } from 'react-dom'
import { AccountContext, IconsContext } from '../context'
import { useMapEngine, MapViz } from '../pages/map/useMapEngine.jsx'
import LayerPanel from '../pages/map/LayerPanel'
import { pipSupported, setFloatState } from '../pages/map/floatState'

// FloatingMap 浮窗:运行时检测 documentPictureInPicture,
//   - 支持(桌面 Chrome/Edge):把地图 DOM 搬进浏览器原生画中画窗口,可拖出浏览器/跨屏/OS 置顶;
//   - 不支持(手机/旧浏览器):在页面内挂一个 fixed 可拖拽缩放的 Web 浮窗。
// 浮窗与主页面互斥:开启时主页面 MapPage 渲染占位(见 MapPage 的 floatOpen 检测)。
// 复用同一条 SSE 连接(全局单例),复用同套图层数据 hook(各自 useMapEngine 实例)。

const WEB_FLOAT_KEY = 'map.webFloatBox' // { x, y, w, h, collapsed }

// loadBox 读本地保存的 Web 浮窗位置/尺寸(无记录给默认)。
function loadBox() {
  try {
    const v = JSON.parse(localStorage.getItem(WEB_FLOAT_KEY))
    if (v && typeof v === 'object' && !Array.isArray(v)) {
      return {
        x: typeof v.x === 'number' ? v.x : 40,
        y: typeof v.y === 'number' ? v.y : 80,
        w: typeof v.w === 'number' ? Math.max(280, v.w) : 380,
        h: typeof v.h === 'number' ? Math.max(280, v.h) : 380,
      }
    }
  } catch { /* ignore */ }
  return { x: 40, y: 80, w: 380, h: 380 }
}

// PiP 模式:用 documentPictureInPicture 开一个独立窗口,把浮窗内容渲染进去。
// React 18 的 createPortal 可以目标到外部 document 的节点。
export default function FloatingMap() {
  const [mode, setMode] = useState(() => (pipSupported ? 'pip' : 'web'))
  // PiP 窗口:持有 window 对象;关闭时 null。
  const [pipWin, setPipWin] = useState(null)
  // Web 浮窗容器节点(PiP 不可用时用)。
  const webRootRef = useRef(null)

  // 关闭浮窗:PiP 先关窗口,Web 直接卸载。
  const close = useCallback(() => {
    if (mode === 'pip' && pipWin) { try { pipWin.close() } catch { /* ignore */ } }
    setFloatState({ open: false, mode: null })
  }, [mode, pipWin])

  // PiP 模式:开窗口。请求失败(权限/不支持)时回退到 Web 浮窗。
  useEffect(() => {
    if (mode !== 'pip') return
    let cancelled = false
    const container = document.createElement('div')
    container.style.cssText = 'width:100vw;height:100vh;margin:0;padding:0;'
    ;(async () => {
      try {
        const w = await window.documentPictureInPicture.requestWindow({
          width: 420, height: 420,
        })
        if (cancelled) { try { w.close() } catch { /* ignore */ } return }
        // 复制基础样式:让浮窗内主题色/字体与主页一致。
        copyStylesInto(w.document)
        w.document.body.style.cssText = 'margin:0;padding:0;background:#0b0e13;overflow:hidden;font-family:inherit;'
        w.document.body.appendChild(container)
        w.addEventListener('pagehide', () => {
          setFloatState({ open: false, mode: null })
          setPipWin(null)
        })
        setPipWin(w)
      } catch {
        // 请求失败:回退 Web 浮窗。
        setMode('web')
      }
    })()
    return () => { cancelled = true }
  }, [mode])

  // Web 浮窗模式:渲染到 body 下的一个 fixed 容器。
  if (mode === 'pip') {
    if (!pipWin) return null // 等待窗口开启
    return createPortal(
      <FloatingContent onClose={close} floatMode="pip" />,
      pipWin.document.body.firstChild || pipWin.document.body,
    )
  }
  // Web 浮窗
  return createPortal(
    <FloatingContent onClose={close} floatMode="web" />,
    document.body,
  )
}

// copyStylesInto 把主页的 <style> 与 <link> 复制到 PiP 窗口,让浮窗内样式一致。
// embed 的 CSS 经 Vite 打包成 <style> 或 <link>,逐一克隆即可。
function copyStylesInto(targetDoc) {
  try {
    document.querySelectorAll('style, link[rel="stylesheet"]').forEach((node) => {
      try {
        const clone = node.cloneNode(true)
        targetDoc.head.appendChild(clone)
      } catch { /* 个别节点跨 document 克隆失败,忽略 */ }
    })
  } catch { /* ignore */ }
}

// FloatingContent 浮窗内容本体:自带 useMapEngine(独立 RAF,主页面已卸载,无冗余),
// 渲染 MapViz + 简化版图层入口(PiP 模式下窗口很小,图层栏放折叠按钮,点开浮层)。
function FloatingContent({ onClose, floatMode }) {
  const account = useContext(AccountContext)
  const engine = useMapEngine(account, { floating: true })
  // Web 浮窗的位置/尺寸/折叠态
  const [box, setBox] = useState(() => loadBox())
  const [collapsed, setCollapsed] = useState(false) // 收起成小条(只留标题栏)
  const [layersOpen, setLayersOpen] = useState(false) // 图层浮层(移动抽屉式)
  const dragRef = useRef(null) // 拖拽中:{ startX, startY, boxX, boxY, mode }

  // 持久化 Web 浮窗位置/尺寸。
  useEffect(() => {
    if (floatMode !== 'web') return
    const t = setTimeout(() => {
      try { localStorage.setItem(WEB_FLOAT_KEY, JSON.stringify(box)) } catch { /* ignore */ }
    }, 400)
    return () => clearTimeout(t)
  }, [box, floatMode])

  // 拖拽手柄(标题栏):按下后移动整个浮窗。Web 浮窗专属;PiP 窗口由 OS 拖。
  const onHandleDown = useCallback((e) => {
    if (floatMode !== 'web') return
    if (e.button != null && e.button !== 0) return
    const startX = e.clientX, startY = e.clientY
    const { x, y } = box
    dragRef.current = { startX, startY, x, y, mode: 'move' }
    const onMove = (ev) => {
      const d = dragRef.current
      if (!d || d.mode !== 'move') return
      const nx = Math.max(0, Math.min(window.innerWidth - 80, d.x + (ev.clientX - d.startX)))
      const ny = Math.max(0, Math.min(window.innerHeight - 40, d.y + (ev.clientY - d.startY)))
      setBox((b) => ({ ...b, x: nx, y: ny }))
    }
    const onUp = () => {
      dragRef.current = null
      window.removeEventListener('mousemove', onMove)
      window.removeEventListener('mouseup', onUp)
    }
    window.addEventListener('mousemove', onMove)
    window.addEventListener('mouseup', onUp)
    e.preventDefault()
  }, [box, floatMode])

  // 缩放手柄(右下角):拖动改变尺寸。
  const onResizeDown = useCallback((e) => {
    if (floatMode !== 'web') return
    if (e.button != null && e.button !== 0) return
    e.preventDefault()
    e.stopPropagation()
    const startX = e.clientX, startY = e.clientY
    const { w, h } = box
    dragRef.current = { startX, startY, w, h, mode: 'resize' }
    const onMove = (ev) => {
      const d = dragRef.current
      if (!d || d.mode !== 'resize') return
      const nw = Math.max(280, d.w + (ev.clientX - d.startX))
      const nh = Math.max(280, d.h + (ev.clientY - d.startY))
      setBox((b) => ({ ...b, w: nw, h: nh }))
    }
    const onUp = () => {
      dragRef.current = null
      window.removeEventListener('mousemove', onMove)
      window.removeEventListener('mouseup', onUp)
    }
    window.addEventListener('mousemove', onMove)
    window.addEventListener('mouseup', onUp)
  }, [box, floatMode])

  // PiP 模式:容器撑满 PiP 窗口;Web 模式:按 box 定位。
  const containerStyle = floatMode === 'pip'
    ? { position: 'fixed', inset: 0, display: 'flex', flexDirection: 'column' }
    : {
        position: 'fixed', left: box.x, top: box.y, width: box.w,
        height: collapsed ? 40 : box.h,
        display: 'flex', flexDirection: 'column',
        zIndex: 99998,
        transition: collapsed ? 'height .18s ease' : 'none',
      }

  return (
    <div className={'map-float' + (floatMode === 'web' ? ' map-float-web' : ' map-float-pip')} style={containerStyle}>
      <div className="map-float-title" onMouseDown={onHandleDown} onDoubleClick={() => setCollapsed((c) => !c)}>
        <span className="map-float-title-ic">🗺️</span>
        <span className="map-float-title-text">实时地图浮窗</span>
        <button className="map-float-btn" title="图层" onClick={(e) => { e.stopPropagation(); setLayersOpen((o) => !o) }}>☰</button>
        <button className="map-float-btn" title={collapsed ? '展开' : '收起'} onClick={(e) => { e.stopPropagation(); setCollapsed((c) => !c) }}>{collapsed ? '▢' : '▬'}</button>
        <button className="map-float-btn map-float-close" title="关闭浮窗" onClick={(e) => { e.stopPropagation(); onClose() }}>✕</button>
      </div>
      {!collapsed && (
        <div className="map-float-body">
          <MapViz engine={engine} floatMode={floatMode}
            sidebarOpen={false} onToggleLayers={() => setLayersOpen((o) => !o)} />
        </div>
      )}
      {floatMode === 'web' && !collapsed && (
        <div className="map-float-resize" onMouseDown={onResizeDown} />
      )}
      {/* 图层浮层:点击 ☰ 弹出,占满浮窗或贴合一边 */}
      {layersOpen && !collapsed && (
        <div className="map-float-layers-backdrop" onClick={() => setLayersOpen(false)}>
          <div className="map-float-layers" onClick={(e) => e.stopPropagation()}>
            <LayerPanel
              pois={engine.pois} wilds={engine.wilds} paint={engine.paint}
              collapsed={false}
              onClose={() => setLayersOpen(false)}
              onCollapseSidebar={() => setLayersOpen(false)}
            />
          </div>
        </div>
      )}
    </div>
  )
}
