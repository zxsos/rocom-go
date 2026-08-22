import React, { useState, useEffect, useRef, useCallback, useContext } from 'react'
import { createPortal } from 'react-dom'
import { AccountContext, IconsContext } from '../context'
import { useMapEngine, MapViz } from '../pages/map/useMapEngine.jsx'
import { imgURL } from './icons'
import LayerPanel from '../pages/map/LayerPanel'
import { pipSupported, setFloatState } from '../pages/map/floatState'

// FloatingMap 浮窗:两档,按设备能力自动选。
//   1. documentPictureInPicture(桌面 Chrome/Edge/Firefox 151+):任意 DOM 进 OS 级浮窗,可移出浏览器;
//   2. video PiP(Android Chrome 8.0+ 等):地图 canvas → captureStream → video → requestPictureInPicture,
//      系统级浮窗(可移出浏览器、置顶),但只读(不能点标记/拖地图,只能看)。
// 不支持以上两者的环境(如 iOS Safari)不弹浮窗:点"开浮窗"会提示当前浏览器不支持。
// 浮窗与主页面互斥:开启时主页面 MapPage 渲染占位(见 MapPage 的 floatOpen 检测)。
// 复用同一条 SSE 连接(全局单例),复用同套图层数据 hook(各自 useMapEngine 实例)。

// videoPiPSupported:检测 video requestPictureInPicture 是否可用(Android Chrome 等)。
// 注意:PiP API 仅在安全上下文(HTTPS/localhost)真正可用。非安全上下文下
//   pictureInPictureEnabled 仍可能是 true(feature detection 不拦),但实际调
//   requestPictureInPicture() 会 reject 或进 PiP 后黑屏。所以这里加 isSecureContext 判断。
const videoPiPSupported = typeof document !== 'undefined'
  && 'pictureInPictureEnabled' in document
  && document.pictureInPictureEnabled
  && (typeof window !== 'undefined' ? window.isSecureContext : true)

// PIP_AUTO_KEY:记录用户是否偏好"开浮窗即自动进入系统画中画"(video-pip 模式)。
// Android 上每次都要用户手势触发 requestPictureInPicture,但首次授权后,之后可自动进。
// 默认 false(避免首次就自动调被拦截);用户成功进过一次后改为 true。
const PIP_AUTO_KEY = 'map.pipAuto'

export default function FloatingMap() {
  // 模式选择:documentPiP(桌面) > videoPiP(Android) > none(不支持)
  const [mode, setMode] = useState(() => {
    if (pipSupported) return 'pip'
    if (videoPiPSupported) return 'video-pip'
    return 'none'
  })
  const [pipWin, setPipWin] = useState(null)

  const close = useCallback(() => {
    if (mode === 'pip' && pipWin) { try { pipWin.close() } catch { /* ignore */ } }
    // video-pip 退出 PiP:document.exitPictureInPicture() 在卸载时由 effect 清理
    if (mode === 'video-pip' && document.pictureInPictureElement) {
      try { document.exitPictureInPicture() } catch { /* ignore */ }
    }
    setFloatState({ open: false, mode: null })
  }, [mode, pipWin])

  // documentPiP 模式:开窗口。请求失败回退到 none。
  useEffect(() => {
    if (mode !== 'pip') return
    let cancelled = false
    const container = document.createElement('div')
    container.style.cssText = 'width:100vw;height:100vh;margin:0;padding:0;'
    ;(async () => {
      try {
        const w = await window.documentPictureInPicture.requestWindow({ width: 420, height: 420 })
        if (cancelled) { try { w.close() } catch {} return }
        copyStylesInto(w.document)
        w.document.body.style.cssText = 'margin:0;padding:0;background:#0b0e13;overflow:hidden;font-family:inherit;'
        w.document.body.appendChild(container)
        w.addEventListener('pagehide', () => {
          setFloatState({ open: false, mode: null })
          setPipWin(null)
        })
        setPipWin(w)
      } catch {
        setMode('none')
      }
    })()
    return () => { cancelled = true }
  }, [mode])

  if (mode === 'pip') {
    if (!pipWin) return null
    return createPortal(
      <FloatingContent onClose={close} floatMode="pip" />,
      pipWin.document.body.firstChild || pipWin.document.body,
    )
  }
  if (mode === 'video-pip') {
    return createPortal(
      <FloatingContent onClose={close} floatMode="video-pip" />,
      document.body,
    )
  }
  // 不支持任何 PiP:给个提示卡,用户可关闭浮窗回到主页面地图。
  return createPortal(
    <div className="map-float-unsupported">
      <div className="map-float-unsupported-card">
        <div className="map-float-unsupported-ic">🚫</div>
        <div className="map-float-unsupported-text">当前环境不支持地图浮窗</div>
        <div className="muted">
          系统画中画要求 HTTPS 或 localhost 访问
          (当前 {typeof location !== 'undefined' ? location.protocol + '//' + location.host : '—'} 不是安全上下文)。
          桌面 Chrome/Edge/Firefox 或 Android Chrome 走 HTTPS 即可启用;iOS 暂不支持。
        </div>
        <button className="btn primary" onClick={close}>关闭</button>
      </div>
    </div>,
    document.body,
  )
}

// copyStylesInto 把主页的 <style> 与 <link> 复制到 PiP 窗口,让浮窗内样式一致。
function copyStylesInto(targetDoc) {
  try {
    document.querySelectorAll('style, link[rel="stylesheet"]').forEach((node) => {
      try { targetDoc.head.appendChild(node.cloneNode(true)) } catch {}
    })
  } catch {}
}

// FloatingContent 浮窗内容本体:自带 useMapEngine(独立 RAF,主页面已卸载)。
// - pip 模式:渲染 MapViz(DOM)到 documentPiP 窗口。
// - video-pip 模式:渲染 MapCanvasViz(canvas)到页内底部小窗 + 隐藏 video,用户点按钮进系统 PiP。
function FloatingContent({ onClose, floatMode }) {
  const account = useContext(AccountContext)
  const engine = useMapEngine(account, { floating: true })
  const [layersOpen, setLayersOpen] = useState(false)
  // video-pip 专属
  const canvasRef = useRef(null)
  const videoRef = useRef(null)
  const [pipActive, setPipActive] = useState(false)
  const [pipError, setPipError] = useState('')

  // video-pip:canvas → captureStream → video.srcObject → play。
  // 非安全上下文(非 HTTPS/非 localhost)下不创建 video 元素,此 effect 提前返回。
  useEffect(() => {
    if (floatMode !== 'video-pip') return
    if (typeof window !== 'undefined' && !window.isSecureContext) return
    const cv = canvasRef.current, vd = videoRef.current
    if (!cv || !vd) return
    cv.width = 480; cv.height = 480
    let stream
    try { stream = cv.captureStream(15) } catch { setPipError('captureStream 不可用'); return }
    vd.srcObject = stream
    vd.muted = true
    vd.play().catch(() => setPipError('video 播放被拦截(需用户手势)'))
    const onLeave = () => setPipActive(false)
    vd.addEventListener('leavepictureinpicture', onLeave)
    // 若用户之前成功进过 PiP 且偏好自动,则尝试自动进入(仍需 video 已 play,失败静默)
    const auto = localStorage.getItem(PIP_AUTO_KEY) === '1'
    if (auto) {
      vd.play().then(() => vd.requestPictureInPicture()
        .then(() => { setPipActive(true); setPipError('') })
        .catch(() => {})).catch(() => {})
    }
    return () => {
      vd.removeEventListener('leavepictureinpicture', onLeave)
      try { vd.pause() } catch {}
      if (document.pictureInPictureElement) { try { document.exitPictureInPicture() } catch {} }
    }
  }, [floatMode])

  // enterPiP:用户手势触发进入系统画中画(移动端必须手势)。
  // 黑屏根因修复:Android Chrome 的 PiP 取 video 渲染区域作为初始帧,
  //   1) video 必须有真实可见尺寸(不能 display:none / 1px);
  //   2) video 必须真正 playing 且 readyState>=2;
  //   3) canvas 必须已画过有效帧(captureStream 才有非空流)。
  //   所以这里:先等 readyState,再 requestFrame 一帧,最后才 requestPictureInPicture。
  const enterPiP = useCallback(async () => {
    const vd = videoRef.current
    const cv = canvasRef.current
    if (!vd) return
    try {
      vd.muted = true
      await vd.play()
      // 等 video 拿到首帧数据(HAVE_CURRENT_DATA 及以上)
      if (vd.readyState < 2) {
        await new Promise((res) => {
          const t = setTimeout(res, 2000)
          vd.addEventListener('loadeddata', () => { clearTimeout(t); res() }, { once: true })
        })
      }
      // 强制 canvas 立刻画一帧并让 captureStream 取帧,避免 PiP 初始黑屏
      if (cv && typeof cv.captureStream === 'function') {
        try {
          const track = vd.srcObject && vd.srcObject.getVideoTracks()[0]
          if (track && typeof track.requestFrame === 'function') track.requestFrame()
        } catch {}
      }
      await vd.requestPictureInPicture()
      setPipActive(true)
      setPipError('')
      try { localStorage.setItem(PIP_AUTO_KEY, '1') } catch {}
    } catch (e) {
      setPipActive(false)
      setPipError(e?.message || '进入画中画失败')
    }
  }, [])

  return (
    <div className={'map-float map-float-' + floatMode}>
      <div className="map-float-title">
        <span className="map-float-title-ic">🗺️</span>
        <span className="map-float-title-text">实时地图浮窗</span>
        {floatMode === 'video-pip' && window.isSecureContext && (
          <button className={'map-float-btn map-float-pip-btn' + (pipActive ? ' on' : '')}
            title={pipActive ? '已在系统画中画(点此恢复页内预览)' : '进入系统画中画(可移出浏览器、置顶)'}
            onClick={(e) => { e.stopPropagation(); pipActive ? document.exitPictureInPicture?.() : enterPiP() }}>
            {pipActive ? '▣' : '⊞'}
          </button>
        )}
        <button className="map-float-btn" title="图层"
          onClick={(e) => { e.stopPropagation(); setLayersOpen((o) => !o) }}>☰</button>
        <button className="map-float-btn map-float-close" title="关闭浮窗"
          onClick={(e) => { e.stopPropagation(); onClose() }}>✕</button>
      </div>
      <div className="map-float-body">
        {floatMode === 'video-pip' ? (
          <MapCanvasViz engine={engine} canvasRef={canvasRef} videoRef={videoRef}
            pipActive={pipActive} pipError={pipError} onEnterPip={enterPiP} />
        ) : (
          <MapViz engine={engine} floatMode={floatMode}
            sidebarOpen={false} onToggleLayers={() => setLayersOpen((o) => !o)} />
        )}
      </div>
      {layersOpen && (
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

// MapCanvasViz:纯 canvas 2D 渲染地图,供 video PiP 用(captureStream → video → 系统画中画)。
// 画的内容:底图 + 层图(洞穴) + 标记层(简化为点) + 箭头(玩家位置)。
// 不复用 MapViz 的 DOM 渲染——DOM 无法被 captureStream,必须画到 canvas 上。
// 渲染参数从 engine 的 refs 读取,与 applyFrame 同源,保证与主画面一致。
function MapCanvasViz({ engine, canvasRef, videoRef, pipActive, pipError, onEnterPip }) {
  const { pos, hasMap, frameStateRef, stRef, focusRef, view, pois, wilds, home } = engine
  const imgCacheRef = useRef({})
  // 容器 ref:测实际尺寸写入 stRef.vp(video-pip 不走 MapViz,vpRef 没挂,vp 是 {0,0},
  // 导致 applyFrame 的 px=0、地图画在左上角而非居中)。这里自己测容器,同步给 engine。
  const vpRef = useRef(null)

  // 测容器尺寸 → 写入 stRef.vp + view.setVp(触发 applyFrame 重算);同时强制 follow=true(只读模式必跟随)。
  useEffect(() => {
    const el = vpRef.current
    if (!el) return
    const ro = new ResizeObserver(() => {
      const w = el.clientWidth, h = el.clientHeight
      if (w > 0 && h > 0) {
        view.setVp({ w, h })
        if (!stRef.current.follow) view.setFollow(true)
      }
    })
    ro.observe(el)
    // 立即测一次(ResizeObserver 首次回调可能延迟一帧)
    const w = el.clientWidth, h = el.clientHeight
    if (w > 0 && h > 0) { view.setVp({ w, h }); view.setFollow(true) }
    return () => ro.disconnect()
  }, [view, stRef])

  // canvas 尺寸跟随容器最小边,保证地图正方形且与视口一致。
  useEffect(() => {
    const cv = canvasRef.current
    if (!cv) return
    const { vp } = stRef.current
    const size = Math.min(vp.w, vp.h) || 480
    if (cv.width !== size) cv.width = size
    if (cv.height !== size) cv.height = size
  })

  // RAF 渲染循环:每帧把地图画到 canvas。
  useEffect(() => {
    let raf = 0
    const draw = () => {
      renderCanvasFrame(canvasRef.current, engine, imgCacheRef.current)
      raf = requestAnimationFrame(draw)
    }
    raf = requestAnimationFrame(draw)
    return () => cancelAnimationFrame(raf)
  }, [engine, canvasRef])

  if (!pos) return <div className="map-canvas-vp" ref={vpRef}><div className="empty">等待位置数据…</div></div>
  if (!hasMap) return <div className="map-canvas-vp" ref={vpRef}><div className="empty">该场景无底图</div></div>

  return (
    <div className="map-canvas-vp" ref={vpRef}>
      <canvas ref={canvasRef} className="map-canvas-el" />
      {window.isSecureContext && (
        <video ref={videoRef} playsInline muted autoPlay className="map-canvas-video" />
      )}
      {!pipActive && window.isSecureContext && (
        <div className="map-canvas-pip-hint">
          <div className="map-canvas-pip-hint-text">点下方按钮把地图悬浮到系统顶层</div>
          <button className="btn primary map-canvas-pip-btn" onClick={onEnterPip}>
            ⊞ 进入系统画中画
          </button>
          {pipError && <div className="map-canvas-pip-err">{pipError}</div>}
        </div>
      )}
    </div>
  )
}

// renderCanvasFrame:把当前帧的地图画到 canvas。纯函数。
function renderCanvasFrame(cv, engine, imgCache) {
  if (!cv) return
  const ctx = cv.getContext('2d')
  if (!ctx) return
  const { pos, frameStateRef, stRef, focusRef, pois, wilds, home } = engine
  if (!pos || !pos.img) { ctx.clearRect(0, 0, cv.width, cv.height); return }

  const { zoom: z } = stRef.current
  const size = Math.min(cv.width, cv.height)
  const mapPx = size * z
  const f = focusRef.current
  const left = (cv.width / 2 - f.u * mapPx) | 0
  const top = (cv.height / 2 - f.v * mapPx) | 0

  ctx.fillStyle = '#0b0e13'
  ctx.fillRect(0, 0, cv.width, cv.height)

  // 底图
  const baseImg = getImg(imgURL('bigmap/' + pos.img + '.webp'), imgCache)
  if (baseImg && baseImg.complete && baseImg.naturalWidth) {
    ctx.drawImage(baseImg, left, top, mapPx, mapPx)
  }
  // 层图(洞穴)
  if (pos.layer) {
    const li = getImg(imgURL('bigmap/' + pos.layer.img + '.webp'), imgCache)
    if (li && li.complete && li.naturalWidth) {
      ctx.drawImage(li,
        left + pos.layer.u0 * mapPx, top + pos.layer.v0 * mapPx,
        (pos.layer.u1 - pos.layer.u0) * mapPx, (pos.layer.v1 - pos.layer.v0) * mapPx)
    }
  }
  // 标记层:简化为点
  drawMarks(ctx, pois.marks, left, top, mapPx, '#5fd0ff')
  drawMarks(ctx, wilds.marks, left, top, mapPx, '#fff', true)
  drawMarks(ctx, (home?.marks) || [], left, top, mapPx, '#ff9100')

  // 玩家箭头
  const disp = frameStateRef.current
  if (disp) {
    ctx.save()
    ctx.translate((left + disp.u * mapPx) | 0, (top + disp.v * mapPx) | 0)
    ctx.rotate((disp.heading + 90) * Math.PI / 180)
    ctx.fillStyle = '#f5365c'
    ctx.strokeStyle = '#fff'
    ctx.lineWidth = 1.5
    ctx.beginPath()
    ctx.moveTo(0, -10); ctx.lineTo(7, 8); ctx.lineTo(0, 4); ctx.lineTo(-7, 8)
    ctx.closePath(); ctx.fill(); ctx.stroke()
    ctx.restore()
  }
}

function getImg(src, cache) {
  if (!cache[src]) { const img = new Image(); img.src = src; cache[src] = img }
  return cache[src]
}

function drawMarks(ctx, marks, left, top, mapPx, color, rare) {
  if (!marks || !marks.length) return
  ctx.fillStyle = color
  ctx.strokeStyle = 'rgba(0,0,0,.5)'
  ctx.lineWidth = 1
  for (const m of marks) {
    if (m.u == null) continue
    ctx.beginPath()
    ctx.arc((left + m.u * mapPx) | 0, (top + m.v * mapPx) | 0, rare ? 4 : 3, 0, Math.PI * 2)
    ctx.fill(); ctx.stroke()
  }
}
