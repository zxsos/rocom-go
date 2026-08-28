import React, { useState, useEffect, useRef, useCallback, useLayoutEffect, useMemo } from 'react'
import { subscribe, getPosition } from '../../api'
import { IconsContext } from '../../context'
import { imgURL } from '../../components/icons'
import { ZOOM_FALLBACK, defaultZoom, SMOOTH_TAU, SMOOTH_CUTOFF, snap, posAt, makeAnchor } from './motion'
import { usePanZoom } from './usePanZoom'
import { usePois } from './usePois'
import { useRoutes, RouteLayer } from './useRoutes.jsx'
import { useWildPets, wildTags } from './useWildPets'
import { useHomeNests, nestTitle } from './useHomeNests'
import { usePaint } from './usePaint'
import { PetDetailModal } from '../../components/PetDetailModal'
import { GlassChip } from '../../components/badges'

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
export function useMapEngine(account) {
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
  // posRef 同步最新位置数据:setPos 路径(场景/层图变化)由下面的 applyPos 中赋;
  // 不 setPos 的纯位置推送也由 applyPos 中手动赋(见 needRender 判断)。
  // 不在此行赋值 posRef.current = pos——否则其它重渲染(如 wilds 刷新)会覆盖 applyPos
  // 中赋的最新位置,导致 onTap 距离计算用到旧坐标。
  const view = usePanZoom(hasMap, onTap)
  const { focusRef, stRef } = view
  const pois = usePois(account, pos && pos.sceneResId)
  const wilds = useWildPets(account)
  wildsRef.current = wilds.marks
  const home = useHomeNests(account)
  const paint = usePaint(account, pos && pos.sceneResId, pos && pos.layer && pos.layer.id, pos && pos.paintable)
  const routes = useRoutes(account, pos && pos.sceneResId)

  const anchorRef = useRef(null)
  const dispRef = useRef(null)
  const worldRef = useRef(null)
  const arrowRef = useRef(null)
  const lastFrameRef = useRef(null)
  // draggingRef:指针是否按住拖动中。静止判定见下:玩家静止时 RAF 会停,
  // 但拖动期间 focusRef 持续被平移更新,必须保持 RAF 运行才能逐帧消费,否则单指拖不动
  // (双指缩放因每次 setZoom 都触发重渲染+layoutEffect 画帧,不受影响)。
  const draggingRef = useRef(false)
  // rafRef 持有「确保 RAF 在跑」的函数:applyPos 写入新锚点后调一次,若 RAF 已因静止停止则重启。
  // tick 静止退出前把 rafRef.current 置为重启函数;applyPos 调用它即恢复循环。
  const rafRef = useRef(null)

  const applyFrame = useCallback(() => {
    const a = anchorRef.current
    const { zoom: z, follow: fl, vp: v } = stRef.current
    if (!a || !worldRef.current) return
    const dt = (performance.now() - a.t0) / 1000
    // decay 在 dt 超过 SMOOTH_CUTOFF 后直接归零(不再用 e^(-dt/τ) 的亚像素小数):
    // 否则玩家静止时 cu/cv 的极小残差经 snap 的 Math.round 在整数边界反复跳,箭头/地图每帧抖 1px。
    const decay = dt >= SMOOTH_CUTOFF ? 0 : Math.exp(-dt / SMOOTH_TAU)
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
    // DOM 节点变了(浮窗开关后 MapViz 重挂,worldRef/arrowRef 指向新节点)→ 强制重写,
    // 否则旧 lastFrameRef 的字符串与新算出的相同会跳过写入,新 DOM 没设 transform 停在左上角。
    if (last && last.world === world && last.arrow === arrow
      && last.wNode === worldRef.current && last.aNode === arrowRef.current) return
    lastFrameRef.current = { world, arrow, wNode: worldRef.current, aNode: arrowRef.current }
    worldRef.current.style.transform = world
    if (arrowRef.current) arrowRef.current.style.transform = arrow
  }, [stRef, focusRef])

  useEffect(() => {
    let raf = 0
    const tick = () => {
      applyFrame()
      const a = anchorRef.current
      // 锚点尚未就绪(尚未收到位置包):applyFrame 已判空 return,这里同样跳过静止判定,
      // 续跑 RAF 等 applyPos 写入锚点。否则 const dt = ... - a.t0 直接抛 TypeError,RAF 链断掉,
      // 之后 rafRef.current?.() 重启 tick 又抛——整个 RAF 永远起不来,地图不渲染,页面黑屏。
      if (!a) { raf = requestAnimationFrame(tick); return }
      // 静止判定:decay 已饱和(误差收敛完毕,dt 超过 SMOOTH_CUTOFF)、且锚点无速度也无轨迹回放。
      // 满足这三条后画面值不再随 dt 变化,继续跑 RAF 只是无谓计算 + 字符串比较,且亚像素边界
      // 抖动可能被放大。此时停 RAF,等 applyPos 写新锚点后经 rafRef 重启。
      // 注意:有速度(vu/vv≠0)或轨迹回放(dt<GLIDE)时不能停,否则玩家在动画面会冻住。
      const dt = (performance.now() - a.t0) / 1000
      const moving = (a.vu || 0) !== 0 || (a.vv || 0) !== 0 || (a.cum && dt < 0.45)
      if (dt >= SMOOTH_CUTOFF && !moving && !draggingRef.current) {
        raf = 0 // 已停,标记给 ensureRaf 知道下次需重启
        return
      }
      raf = requestAnimationFrame(tick)
    }
    // ensureRaf:若 RAF 没在跑则启动一帧。applyPos 写新锚点后调用——新锚点要么有速度(移动中)、
    // 要么 dt 从 0 起算,都不满足静止条件,故 tick 会自然续跑。
    const ensureRaf = () => { if (!raf) raf = requestAnimationFrame(tick) }
    rafRef.current = ensureRaf
    raf = requestAnimationFrame(tick)
    return () => { if (raf) cancelAnimationFrame(raf); rafRef.current = null }
  }, [applyFrame])
  useLayoutEffect(applyFrame)

  const applyPos = useCallback((p) => {
    if (p.layerOnly) {
      const li = p.layer ? p.layer.img : ''
      if (li !== layerRef.current) {
        layerRef.current = li
        setLayerError(false)
      }
      // 同步 posRef 的 layer/sceneName(位置坐标不变)
      if (posRef.current) posRef.current = { ...posRef.current, layer: p.layer || null, sceneName: p.sceneName || posRef.current.sceneName }
      setPos((prev) => (prev ? { ...prev, layer: p.layer || null, sceneName: p.sceneName || prev.sceneName } : prev))
      return
    }
    const sceneChanged = p.img !== sceneRef.current
    const li = p.layer ? p.layer.img : ''
    const layerChanged = li !== layerRef.current
    // 性能优化:位置推送约 8 条/秒,绝大多数 p 与上一包同场景、同层。此时底图/层图/控件都不需要
    // 重渲染——它们只随场景或层图变化而变。位置坐标走锚点 ref + RAF 逐帧外推(见 applyFrame),
    // 不进 React state。只在场景切换、层图切换、或从 null/无底图状态变化时才 setPos 触发重渲染,
    // 省去每秒 8 次的 MapViz 整树 JSX 构造与 React reconciliation(WildLayer/PoiLayer/NestLayer 虽
    // 被 memo 跳过,但父函数体 + 各 memo 比较函数的开销在高频位置推送下仍可观)。
    const noMap = p.u == null
    // 无底图场景(noMap)下 pos.x/y/z 是唯一的可见信息,必须每次更新;
    // 有底图时位置走锚点 ref+RAF,只在场景/层图/有底图状态变化时才 setPos。
    const needRender = noMap || sceneChanged || layerChanged || !posRef.current || (posRef.current && posRef.current.u == null) !== !noMap
    posRef.current = p // 始终同步:无论是否 setPos,onTap 距离计算都需最新坐标
    if (needRender) setPos(p)
    if (sceneChanged) {
      sceneRef.current = p.img
      setImgError(false)
      view.setZoom(defaultZoom(p))
      view.setFollow(true)
      lastFrameRef.current = null
    }
    if (layerChanged) {
      layerRef.current = li
      setLayerError(false)
    }
    if (noMap) {
      anchorRef.current = null
      dispRef.current = null
      return
    }
    anchorRef.current = makeAnchor(p, sceneChanged ? null : dispRef.current, sceneChanged)
    if (sceneChanged || !dispRef.current) focusRef.current = { u: p.u, v: p.v }
    // 新锚点已写入:若 RAF 因上一段静止而停了,这里重启一帧;tick 检测到新锚点(dt 重置、
    // 或有速度)不满足静止条件会自然续跑。layerOnly 分支不动锚点,故无需重启。
    rafRef.current?.()
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
    pois, wilds, home, paint, routes,
    detailGid, setDetailGid, wildTip, setWildTip, wildDist, setWildDist, onTap,
    // canvas PiP 用:暴露当前帧的渲染参数(供 renderToCanvas 画到外部 canvas)
    // 这些 ref 在 applyFrame 里每帧更新,canvas 渲染循环直接读,不触发 React 重渲染。
    frameStateRef: dispRef, // { u, v, heading } 当前玩家显示位置
    stRef, // { zoom, follow, vp } 视图状态
    focusRef, // { u, v } 视口中心对应的地图坐标
    sceneRef, layerRef, // 当前场景底图名/层图名
    // 拖动修复:拖动中置 draggingRef=true 让 RAF 不因静止停止;pokeFrame 重启已停的 RAF。
    draggingRef,
    pokeFrame: () => rafRef.current?.(),
  }
}

// MapViz 地图本体渲染:从 .map-vp 到标记层、控制按钮。
// engine 由 useMapEngine 产出(内部持有 sceneRef/layerRef/anchorRef 等,这里只读 pos)。
export function MapViz({ engine, layersActive, onToggleLayers }) {
  const { pos, hasMap, imgError, layerError, setImgError, setLayerError,
    view, worldRef, arrowRef, pois, wilds, home, paint, routes,
    detailGid, setDetailGid, wildTip, setWildDist, setWildTip, wildDist, onTap,
    draggingRef, pokeFrame } = engine
  const { focusRef, stRef } = view
  const mapPx = (Math.min(view.vp.w, view.vp.h) || 1) * view.zoom
  // 包装 usePanZoom 的指针 handlers:拖动期间置 draggingRef(RAF 静止判定因此不停),
  // 按下时 pokeFrame 重启可能已停的 RAF。否则玩家静止时 RAF 停止,单指平移只更新
  // focusRef 没有帧循环消费,画面不动(双指缩放因 setZoom 每次触发重渲染画帧而不受影响)。
  const handlers = useMemo(() => {
    const h = view.handlers
    return {
      ...h,
      onPointerDown: (e) => { draggingRef.current = true; pokeFrame(); h.onPointerDown?.(e) },
      onPointerMove: (e) => { h.onPointerMove?.(e) },
      onPointerUp: (e) => { h.onPointerUp?.(e); draggingRef.current = false },
      onPointerCancel: (e) => { h.onPointerCancel?.(e); draggingRef.current = false },
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [view.handlers, draggingRef, pokeFrame])

  // 控制组(图层/放大/缩小/居中)浮在视口右上角。无论地图是否就绪都渲染——
  // 这样「等待位置数据」/「无底图场景」下也能点 ☰ 打开图层栏(否则侧栏入口消失)。
  // 地图未就绪时放大/缩小/居中无意义,置灰禁用。
  const zoomReady = !!(pos && hasMap)
  const ctrl = (
    <div className="map-ctrl">
      <button className={'map-btn map-layers-toggle' + (layersActive ? ' on' : '')} title="图层栏"
        onClick={onToggleLayers}>☰</button>
      <button className="map-btn" title="放大" disabled={!zoomReady}
        onClick={() => view.zoomAround(1.4, view.vp.w / 2, view.vp.h / 2)}>＋</button>
      <button className="map-btn" title="缩小" disabled={!zoomReady}
        onClick={() => view.zoomAround(1 / 1.4, view.vp.w / 2, view.vp.h / 2)}>－</button>
      <button className={'map-btn' + (view.follow ? ' on' : '')} title="回到当前位置"
        disabled={!zoomReady} onClick={() => view.setFollow(true)}>◎</button>
    </div>
  )

  return (
    <>
      {!pos && <div className="empty">等待位置数据…(需后端正在抓包/回放,且玩家已登录并移动过)</div>}
      {pos && (hasMap ? (
        <div className="map-vp" ref={view.vpRef} {...handlers}>
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
            <RouteLayer marks={routes.marks} mapPx={mapPx} />
            <PoiLayer marks={pois.marks} mapPx={mapPx} />
            <NestLayer marks={home.marks} mapPx={mapPx} />
            <WildLayer marks={wilds.marks} mapPx={mapPx} wildTip={wildTip} dist={wildDist} />
          </div>
          <div className="map-arrow" ref={arrowRef}>
            <svg viewBox="0 0 24 24" width="30" height="30">
              <path d="M12 2 L20 21 L12 16 L4 21 Z" fill="var(--red)" stroke="#fff" strokeWidth="1.5" strokeLinejoin="round" />
            </svg>
          </div>
          {ctrl}
        </div>
      ) : (
        <div className="map-nomap">
          <div className="map-nomap-name">{pos.sceneName || '未知场景'}</div>
          <div className="muted">该场景无底图,仅显示坐标</div>
          <div className="map-coords">X {pos.x} · Y {pos.y} · Z {pos.z}</div>
          {ctrl}
        </div>
      ))}
      {!pos && ctrl}
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
            {/* 炫彩/异色炫彩:悬浮面板里展示完整色卡(角标圆盘太小看不清,点开可细看配色)。
                后端在 glassType != 空 时才带这两个字段,故此处判断即可;异色(仅 shiny)无炫彩数据不显示。 */}
            {p.glassType > 0 && p.glassValue > 0 && (
              <div className="twg"><GlassChip p={p} className="map-wild-tip-chip" /></div>
            )}
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
