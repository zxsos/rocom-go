import React, { useState, useEffect, useRef, useCallback, useLayoutEffect } from 'react'
import { subscribe, getPosition } from '../../api'
import { IconsContext } from '../../context'
import { imgURL } from '../../components/icons'
import { ZOOM_FALLBACK, defaultZoom, SMOOTH_TAU, snap, posAt, makeAnchor } from './motion'
import { usePanZoom } from './usePanZoom'
import { usePois } from './usePois'
import { useWildPets, wildTags } from './useWildPets'
import { useHomeNests, nestTitle } from './useHomeNests'
import { usePaint } from './usePaint'
import { PetDetailModal } from '../../components/PetDetailModal'

// wildTitle 组一条野生宠物标记的悬停说明(见 MapPage 原实现)。
export function wildTitle(p) {
  const head = [p.n || '野生宠物']
  if (p.lv) head.push('Lv.' + p.lv)
  head.push(...wildTags(p.kinds))
  const w = p.weightPct != null ? `${Math.round(p.weightPct * 10) / 10}%` : '-'
  let s = `${head.join(' ')} W ${w} V ${p.voice}`
  if (p.stale) s += ' (已离开视野)'
  return s
}

// useMapEngine 抽离自 MapPage:地图引擎内核——位置/外推/RAF/视图状态 + 图层数据订阅。
// 浮窗与主页面共用此 hook,各自渲染外壳(MapViz),省一份逻辑拷贝。
// floating 标记当前是否运行在浮窗模式(预留:PiP 关闭回调、视口尺寸取自浮窗容器等)。
export function useMapEngine(account, opts = {}) {
  const { floating = false } = opts
  const [pos, setPos] = useState(null)
  const [imgError, setImgError] = useState(false)
  const [layerError, setLayerError] = useState(false)
  const sceneRef = useRef(null)
  const layerRef = useRef(null)

  const [detailGid, setDetailGid] = useState(null)
  const [wildTip, setWildTip] = useState(null)
  const [wildDist, setWildDist] = useState(null)
  const posRef = useRef(null)
  const wildsRef = useRef(null)

  const onTap = useCallback((target) => {
    const gid = target.closest?.('.map-nest')?.dataset.gid
    if (gid) { setDetailGid(Number(gid)); setWildTip(null); setWildDist(null); return }
    const wildEl = target.closest?.('.map-wild')
    if (wildEl?.classList.contains('map-wild-all')) {
      setWildTip(null); setWildDist(null); return
    }
    const wid = wildEl?.dataset.id
    setWildTip((cur) => (wid ? (wid === cur ? null : wid) : null))
    if (wid) {
      const w = (wildsRef.current || []).find((x) => x.id === wid)
      const p0 = posRef.current
      if (w && p0 && w.x != null && p0.x != null) {
        const dx = w.x - p0.x, dy = w.y - p0.y, dz = (w.z || 0) - (p0.z || 0)
        setWildDist(Math.round(Math.hypot(dx, dy, dz) / 100))
      } else {
        setWildDist(null)
      }
    } else {
      setWildDist(null)
    }
  }, [])

  const hasMap = !!(pos && pos.u != null && pos.img && !imgError)
  posRef.current = pos
  const view = usePanZoom(hasMap, onTap)
  const { focusRef, stRef } = view
  const pois = usePois(account, pos && pos.sceneResId)
  const wilds = useWildPets(account)
  wildsRef.current = wilds.marks
  const home = useHomeNests(account)
  const paint = usePaint(account, pos && pos.sceneResId, pos && pos.layer && pos.layer.id, pos && pos.paintable)

  const anchorRef = useRef(null)
  const dispRef = useRef(null)
  const worldRef = useRef(null)
  const arrowRef = useRef(null)
  const lastFrameRef = useRef(null)

  const applyFrame = useCallback(() => {
    const a = anchorRef.current
    const { zoom: z, follow: fl, vp: v } = stRef.current
    if (!a || !worldRef.current) return
    const dt = (performance.now() - a.t0) / 1000
    const decay = Math.exp(-dt / SMOOTH_TAU)
    const p = posAt(a, dt)
    const u = p.u + a.cu * decay
    const w = p.v + a.cv * decay
    const heading = a.heading + a.dh * decay
    dispRef.current = { u, v: w, heading }
    if (fl) focusRef.current = { u, v: w }

    const f = focusRef.current
    const px = (Math.min(v.w, v.h) || 1) * z
    const left = snap(v.w / 2 - f.u * px)
    const top = snap(v.h / 2 - f.v * px)
    const ax = snap(left + u * px)
    const ay = snap(top + w * px)
    const world = `translate3d(${left}px, ${top}px, 0)`
    const arrow = `translate3d(${ax}px, ${ay}px, 0) translate(-50%,-50%) rotate(${heading + 90}deg)`
    const last = lastFrameRef.current
    if (last && last.world === world && last.arrow === arrow) return
    lastFrameRef.current = { world, arrow }
    worldRef.current.style.transform = world
    if (arrowRef.current) arrowRef.current.style.transform = arrow
  }, [stRef, focusRef])

  useEffect(() => {
    let raf = 0
    const tick = () => { applyFrame(); raf = requestAnimationFrame(tick) }
    raf = requestAnimationFrame(tick)
    return () => cancelAnimationFrame(raf)
  }, [applyFrame])
  useLayoutEffect(applyFrame)

  const applyPos = useCallback((p) => {
    if (p.layerOnly) {
      const li = p.layer ? p.layer.img : ''
      if (li !== layerRef.current) {
        layerRef.current = li
        setLayerError(false)
      }
      setPos((prev) => (prev ? { ...prev, layer: p.layer || null, sceneName: p.sceneName || prev.sceneName } : prev))
      return
    }
    setPos(p)
    const sceneChanged = p.img !== sceneRef.current
    if (sceneChanged) {
      sceneRef.current = p.img
      setImgError(false)
      view.setZoom(defaultZoom(p))
      view.setFollow(true)
      lastFrameRef.current = null
    }
    const li = p.layer ? p.layer.img : ''
    if (li !== layerRef.current) {
      layerRef.current = li
      setLayerError(false)
    }
    if (p.u == null) {
      anchorRef.current = null
      dispRef.current = null
      return
    }
    anchorRef.current = makeAnchor(p, sceneChanged ? null : dispRef.current, sceneChanged)
    if (sceneChanged || !dispRef.current) focusRef.current = { u: p.u, v: p.v }
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    let alive = true
    sceneRef.current = null
    layerRef.current = null
    anchorRef.current = null
    dispRef.current = null
    lastFrameRef.current = null
    setPos(null); setImgError(false); setLayerError(false); view.setFollow(true); view.setZoom(ZOOM_FALLBACK)
    getPosition().then((p) => { if (alive && p) applyPos(p) }).catch(() => {})
    return () => { alive = false }
  }, [account, applyPos]) // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => subscribe((m) => { if (m.type === 'position') applyPos(m.data) }), [account, applyPos])

  return {
    pos, hasMap, imgError, layerError, setImgError, setLayerError,
    view, worldRef, arrowRef, applyFrame,
    pois, wilds, home, paint,
    detailGid, setDetailGid, wildTip, setWildTip, wildDist, setWildDist, onTap,
    floating,
  }
}

// MapViz 地图本体渲染:从 .map-vp 到标记层、控制按钮,主页面与浮窗共用。
// engine 由 useMapEngine 产出(内部持有 sceneRef/layerRef/anchorRef 等,这里只读 pos)。
export function MapViz({ engine, onOpenFloat, floatMode, sidebarOpen, onToggleLayers }) {
  const { pos, hasMap, imgError, layerError, setImgError, setLayerError,
    view, worldRef, arrowRef, pois, wilds, home, paint,
    detailGid, setDetailGid, wildTip, setWildDist, setWildTip, wildDist, onTap } = engine
  const { focusRef, stRef } = view
  const mapPx = (Math.min(view.vp.w, view.vp.h) || 1) * view.zoom

  return (
    <>
      {!pos && <div className="empty">等待位置数据…(需后端正在抓包/回放,且玩家已登录并移动过)</div>}
      {pos && (hasMap ? (
        <div className="map-vp" ref={view.vpRef} {...view.handlers}>
          <div className="map-world" ref={worldRef} style={{ width: mapPx, height: mapPx }}>
            <img className="map-base" src={imgURL(`bigmap/${pos.img}.webp`)} alt={pos.sceneName}
              draggable={false} onError={() => setImgError(true)} />
            {pos.layer && !layerError && (
              <img className="map-layer" src={imgURL(`bigmap/${pos.layer.img}.webp`)} alt="" draggable={false}
                onError={() => setLayerError(true)}
                style={{
                  left: pos.layer.u0 * mapPx, top: pos.layer.v0 * mapPx,
                  width: (pos.layer.u1 - pos.layer.u0) * mapPx, height: (pos.layer.v1 - pos.layer.v0) * mapPx,
                }} />
            )}
            {paint.on && paint.ready && (<>
              <canvas className="map-paint" ref={paint.attach}
                width={paint.w} height={paint.h}
                style={{ width: mapPx, height: mapPx }} />
              <svg className="map-paint-edge" viewBox={`0 0 ${paint.w} ${paint.h}`}
                preserveAspectRatio="none" style={{ width: mapPx, height: mapPx }}>
                <path d={paint.edge} />
              </svg>
            </>)}
            <PoiLayer marks={pois.marks} mapPx={mapPx} />
            <NestLayer marks={home.marks} mapPx={mapPx} />
            <WildLayer marks={wilds.marks} mapPx={mapPx} wildTip={wildTip} dist={wildDist} />
          </div>
          <div className="map-arrow" ref={arrowRef}>
            <svg viewBox="0 0 24 24" width="30" height="30">
              <path d="M12 2 L20 21 L12 16 L4 21 Z" fill="var(--red)" stroke="#fff" strokeWidth="1.5" strokeLinejoin="round" />
            </svg>
          </div>
          {!floatMode && (
            <div className="map-ctrl">
              <button className="map-btn map-float-open" title="开浮窗(置顶小窗)"
                onClick={onOpenFloat}>◰</button>
              <button className={'map-btn map-layers-toggle' + (sidebarOpen ? ' on' : '')} title="图层栏"
                onClick={onToggleLayers}>☰</button>
              <button className="map-btn" title="放大" onClick={() => view.zoomAround(1.4, view.vp.w / 2, view.vp.h / 2)}>＋</button>
              <button className="map-btn" title="缩小" onClick={() => view.zoomAround(1 / 1.4, view.vp.w / 2, view.vp.h / 2)}>－</button>
              <button className={'map-btn' + (view.follow ? ' on' : '')} title="回到当前位置" onClick={() => view.setFollow(true)}>◎</button>
            </div>
          )}
          {floatMode && (
            <div className="map-ctrl">
              <button className="map-btn" title="放大" onClick={() => view.zoomAround(1.4, view.vp.w / 2, view.vp.h / 2)}>＋</button>
              <button className="map-btn" title="缩小" onClick={() => view.zoomAround(1 / 1.4, view.vp.w / 2, view.vp.h / 2)}>－</button>
              <button className={'map-btn' + (view.follow ? ' on' : '')} title="回到当前位置" onClick={() => view.setFollow(true)}>◎</button>
            </div>
          )}
        </div>
      ) : (
        <div className="map-nomap">
          <div className="map-nomap-name">{pos.sceneName || '未知场景'}</div>
          <div className="muted">该场景无底图,仅显示坐标</div>
          <div className="map-coords">X {pos.x} · Y {pos.y} · Z {pos.z}</div>
        </div>
      ))}
      {detailGid != null && <PetDetailModal gid={detailGid} onClose={() => setDetailGid(null)} />}
    </>
  )
}

// —— 标记层(memo 子组件,原 MapPage 底部那三个,移到此共用)——
const PoiLayer = React.memo(({ marks, mapPx }) => (
  <>{marks.map((p, i) => (
    <img key={i} alt="" draggable={false}
      className={'map-poi' + (p.sure ? ' sure' : '')}
      src={imgURL(p.icon)} title={p.n}
      style={{ left: p.u * mapPx, top: p.v * mapPx }} />
  ))}</>
))

const NestLayer = React.memo(({ marks, mapPx }) => (
  <>{marks.map((n) => (
    <div key={n.id} title={nestTitle(n)}
      className={'map-nest' + (n.pet ? '' : ' empty')}
      data-gid={n.pet ? n.pet.gid : undefined}
      style={{ left: n.u * mapPx, top: n.v * mapPx }}>
      {n.pet
        ? (n.pet.img ? <img src={imgURL(n.pet.img)} alt="" draggable={false} /> : <span>🐾</span>)
        : <span className="map-nest-empty">空</span>}
      {n.egg && <img className="map-nest-egg" src={imgURL(n.egg.icon)} alt="" draggable={false} />}
    </div>
  ))}</>
))

const WildLayer = React.memo(({ marks, mapPx, wildTip, dist }) => {
  const icons = React.useContext(IconsContext)
  return (
    <>{marks.map((p) => {
      if (p.all) {
        return (
          <div key={p.id} data-id={p.id} title={p.n || '野生宠物'}
            className={'map-wild map-wild-all' + (p.stale ? ' stale' : '')}
            style={{ left: p.u * mapPx, top: p.v * mapPx }}>
            {p.img ? <img className="map-wild-face" src={imgURL(p.img)} alt="" draggable={false} /> : <span className="map-wild-face-fallback">🐾</span>}
          </div>
        )
      }
      const tip = wildTip === p.id
      const kinds = p.kinds || []
      const rare = kinds.includes('shiny') || kinds.includes('colorful')
      const mark = (kinds.includes('shiny') && kinds.includes('colorful') && icons.shinyColorful) ||
        (kinds.includes('shiny') && icons.shiny) ||
        (kinds.includes('colorful') && icons.colorful)
      const markKind = kinds.includes('shiny') && kinds.includes('colorful') ? 'shinyColorful'
        : kinds.includes('shiny') ? 'shiny'
        : kinds.includes('colorful') ? 'colorful'
        : ''
      return [
        <div key={p.id} data-id={p.id} title={wildTitle(p)}
          className={'map-wild' + (p.stale ? ' stale' : '') + (p.inject ? ' inject' : '') + (tip ? ' tip' : '') + (rare ? ' rare' : '')}
          style={{ left: p.u * mapPx, top: p.v * mapPx, ...p.style }}>
          <span className="map-wild-rare-halo" />
          {p.img ? <img className="map-wild-face" src={imgURL(p.img)} alt="" draggable={false} /> : <span className="map-wild-face-fallback">🐾</span>}
          {rare && mark && (
            <span className={'map-wild-mark map-wild-mark-' + markKind}>
              <img src={imgURL(mark)} alt="" draggable={false} />
            </span>
          )}
        </div>,
        tip && (
          <div key={p.id + '-tip'} className="map-wild-tip"
            style={{ left: p.u * mapPx, top: p.v * mapPx }}>
            <div className="twn">{p.n || '野生宠物'}{p.lv ? ' Lv.' + p.lv : ''}</div>
            <div className="twt">{wildTags(p.kinds).join(' ') || '普通'}</div>
            <div className="twr">体重 {p.weightPct != null ? Math.round(p.weightPct * 10) / 10 + '%' : '-'} · 嗓音 {p.voice}</div>
            <div className="twc">X {p.x} · Y {p.y} · Z {p.z}</div>
            <div className="twd">距离 {dist != null ? dist : '-'} 米</div>
            {p.stale && <div className="tws">已离开视野</div>}
          </div>
        ),
      ]
    })}</>
  )
})
