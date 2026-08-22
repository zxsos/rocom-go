import React, { useState, useEffect, useRef, useContext, useCallback, useLayoutEffect } from 'react'
import { subscribe, getPosition } from '../../api'
import { AccountContext, IconsContext } from '../../context'
import { imgURL } from '../../components/icons'
import { ZOOM_FALLBACK, defaultZoom, SMOOTH_TAU, snap, posAt, makeAnchor } from './motion'
import { usePanZoom } from './usePanZoom'
import { usePois } from './usePois'
import { useWildPets, wildTags } from './useWildPets'
import { useHomeNests, nestTitle } from './useHomeNests'
import { usePaint } from './usePaint'
import { PetDetailModal } from '../../components/PetDetailModal'
import LayerPanel from './LayerPanel'

// wildTitle 组一条野生宠物标记的悬停说明,格式:
//   {种类} Lv.44 异色炫彩 W 19.6% V -55
// W 是体重在本形态取值范围内的百分位(后端算好,大世界地图用十分位显示,与奖牌滑块同精度;
// 宠物列表/事件页仍显示整数百分位),V 是嗓音原值——与事件页那行保持一致,一眼能对上。
function wildTitle(p) {
  const head = [p.n || '野生宠物']
  if (p.lv) head.push('Lv.' + p.lv)
  head.push(...wildTags(p.kinds))
  const w = p.weightPct != null ? `${Math.round(p.weightPct * 10) / 10}%` : '-'
  let s = `${head.join(' ')} W ${w} V ${p.voice}`
  if (p.stale) s += ' (已离开视野)'
  return s
}

// 实时地图页:地图软件式交互——方向箭头指示朝向、可缩放平移、默认放大跟随玩家。
// 位置来自 SSE position(玩家移动时逐包推送)+ 加载时 GET /api/position。仅自己。
// 另叠两类实时标记:POI 图层(固定点位)与野生宠物图层(附近刷出的稀有个体)。
// 注:组件名不能叫 Map——会遮蔽内置 Map 构造器。
export default function MapPage() {
  const account = useContext(AccountContext)
  const [pos, setPos] = useState(null) // 最近一个移动包(工具栏文字、底图选择);箭头位置另由 anchor 逐帧算出
  const [imgError, setImgError] = useState(false)
  const [layerError, setLayerError] = useState(false)
  const [collapsed, setCollapsed] = useState(true)  // 移动端图层抽屉(开合)
  const [sidebarOpen, setSidebarOpen] = useState(true) // 桌面图层侧栏(可折叠,折叠后地图全宽)
  const sceneRef = useRef(null) // 当前底图名(换底图=换场景/等级才重置缩放/跟随)
  const layerRef = useRef(null) // 当前叠加层图名(换层仅重试层图,不动缩放)

  const [detailGid, setDetailGid] = useState(null) // 点小窝里的宠物 → 宠物详情弹窗
  const [wildTip, setWildTip] = useState(null) // 点中的野生宠物标记 → 资料卡(点击触发悬浮)
  const [wildDist, setWildDist] = useState(null) // 点中时刻玩家↔宠物的直线距离(米;点击时的快照)
  // onTap 是 [] 闭包,读不到最新的 pos / marks,用 ref 每次渲染同步,点击时才取值。
  const posRef = useRef(null)
  const wildsRef = useRef(null)
  // 地图内标记的「点一下」由 usePanZoom 判定后回调(平移要捕获指针,标记收不到 click),
  // 故这里认按下时的元素:落在住了宠物的小窝上就开详情,落在野生宠物上弹资料卡
  // (再点同一个关掉,点别处也关)。桌面 hover 的 title 照旧,两种触发方式并存。
  const onTap = useCallback((target) => {
    const gid = target.closest?.('.map-nest')?.dataset.gid
    if (gid) { setDetailGid(Number(gid)); setWildTip(null); setWildDist(null); return }
    // 普通野生宠(.map-wild-all):不弹资料卡、不算距离,点它只当点地图(拖动/关资料卡)。
    const wildEl = target.closest?.('.map-wild')
    if (wildEl?.classList.contains('map-wild-all')) {
      setWildTip(null); setWildDist(null); return
    }
    const wid = wildEl?.dataset.id
    setWildTip((cur) => (wid ? (wid === cur ? null : wid) : null))
    // 距离:点中时刻玩家↔宠物世界坐标(厘米)的直线距离,÷100 取整米。资料卡是点击时
    // 的快照,不随玩家移动实时刷——否则位置推送会让 WildLayer 整层重渲染,得不偿失。
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

  // 右上角 ☰:窄屏开/关图层抽屉,桌面折叠/展开图层侧栏(折叠后地图全宽)。
  const toggleLayers = () => {
    if (window.matchMedia('(max-width: 760px)').matches) setCollapsed((c) => !c)
    else setSidebarOpen((o) => !o)
  }

  const hasMap = !!(pos && pos.u != null && pos.img && !imgError)
  posRef.current = pos // 渲染时同步最新位置/标记,onTap(空依赖闭包)点击时经 ref 取值
  const view = usePanZoom(hasMap, onTap)
  const { focusRef, stRef } = view
  const pois = usePois(account, pos && pos.sceneResId)
  const wilds = useWildPets(account)
  wildsRef.current = wilds.marks
  const home = useHomeNests(account)
  // 涂地:把「见到过野生宠物」的方向涂上色(玩家 ↔ 宠物之间那条带子),遍历找稀有个体时
  // 看哪片还没扫。分层地图与地表各涂各的,故要把当前层 id 一并给它。
  const paint = usePaint(account, pos && pos.sceneResId, pos && pos.layer && pos.layer.id, pos && pos.paintable)

  // 逐帧外推的锚点:最近一个移动包的位置/速度/朝向 + 收到它时与画面位置的落差(cu/cv/dh)。
  const anchorRef = useRef(null)
  const dispRef = useRef(null) // 当前画面上的位置/朝向(每帧算出,供下一个包算落差)
  const worldRef = useRef(null)
  const arrowRef = useRef(null)

  // applyFrame 按当前时刻把锚点外推成画面位置,并直接写 transform(不经 React,免每帧重渲染)。
  // 平移量与箭头位置都对齐整设备像素(见 motion.js snap),否则箭头会相对地图晃半个像素。
  // 用 lastFrameRef 缓存上一帧写下的 transform:玩家静止(外推量 0、落差已收敛)时算出的
  // left/top/箭头完全相同,跳过这次 DOM 写——静止或极低速巡航时省掉每帧无谓的合成器重排。
  const lastFrameRef = useRef(null)
  const applyFrame = useCallback(() => {
    const a = anchorRef.current
    const { zoom: z, follow: fl, vp: v } = stRef.current
    if (!a || !worldRef.current) return
    const dt = (performance.now() - a.t0) / 1000
    const decay = Math.exp(-dt / SMOOTH_TAU) // 与上一帧位置的落差随时间抹平
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
    // 世界 yaw(0=东/右,逆时针+)→ 默认朝上的箭头旋转 heading+90(CSS 顺时针,屏幕Y向下)。
    const arrow = `translate3d(${ax}px, ${ay}px, 0) translate(-50%,-50%) rotate(${heading + 90}deg)`
    const last = lastFrameRef.current
    if (last && last.world === world && last.arrow === arrow) return // 与上一帧一致,无需重写
    lastFrameRef.current = { world, arrow }
    worldRef.current.style.transform = world
    if (arrowRef.current) arrowRef.current.style.transform = arrow
  }, [stRef, focusRef])

  // 逐帧循环:即使没有新包也要跑——外推、落差收敛、跟随都是随时间连续变化的。
  useEffect(() => {
    let raf = 0
    const tick = () => { applyFrame(); raf = requestAnimationFrame(tick) }
    raf = requestAnimationFrame(tick)
    return () => cancelAnimationFrame(raf)
  }, [applyFrame])
  // 渲染后(缩放/视口/底图变化)立刻按新参数重画一帧,免得等到下一帧才对齐。
  useLayoutEffect(applyFrame)

  const applyPos = useCallback((p) => {
    // 只更新分层的消息(后端在区域进/出事件后推,如传送落地进洞时玩家还站着不动):
    // 只叠加/撤下切片图,不碰位置锚点——否则会用旧位置重置外推,箭头往回跳。
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
    // 底图变化(换场景、家园换等级)才重置缩放/跟随并重试底图;同底图内移动不打断手动缩放/平移。
    if (sceneChanged) {
      sceneRef.current = p.img
      setImgError(false)
      view.setZoom(defaultZoom(p))
      view.setFollow(true)
      lastFrameRef.current = null // 换底图,清掉上一帧缓存,避免首帧被误判为「无变化」
    }
    // 叠加层变化(进/出/换洞穴层)只重试层图,不动缩放/跟随——与外层保持一致。
    const li = p.layer ? p.layer.img : ''
    if (li !== layerRef.current) {
      layerRef.current = li
      setLayerError(false)
    }
    if (p.u == null) { // 该场景无底图:无从投影,也就无从外推
      anchorRef.current = null
      dispRef.current = null
      return
    }
    anchorRef.current = makeAnchor(p, sceneChanged ? null : dispRef.current, sceneChanged)
    if (sceneChanged || !dispRef.current) focusRef.current = { u: p.u, v: p.v } // 新场景:视口先对准玩家
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

  // 地图层尺寸只跟缩放/视口走(平移与箭头位置逐帧写 transform,不在渲染里算)。
  const mapPx = (Math.min(view.vp.w, view.vp.h) || 1) * view.zoom

  return (
    <div className="map-page">
      {/* 无工具栏:地图占满整页(场景名/坐标不再显示,位置看箭头即可);移动端的图层抽屉入口
          作为浮动控件挂在地图左下角。 */}
      <div className={'map-layout' + (sidebarOpen ? '' : ' closed')}>
        <LayerPanel pois={pois} wilds={wilds} paint={paint} collapsed={collapsed}
          onClose={() => setCollapsed(true)} onCollapseSidebar={() => setSidebarOpen(false)} />

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
            {/* 涂地:淡填充用一张格子位图(canvas,一格 8m),边界用 SVG 路径描细线——
                位图放大会糊,矢量则由浏览器按当前缩放重新栅格化,放到最大描边依旧是细线
                (vector-effect:non-scaling-stroke,见 usePaint 与 map.css)。
                两层都只在「有新格子」时重画一次;平移缩放全靠 .map-world 的 transform 与
                CSS 尺寸,不触发重绘。压在标记之下、层图之上。 */}
            {paint.on && paint.ready && (<>
              <canvas className="map-paint" ref={paint.attach}
                width={paint.w} height={paint.h}
                style={{ width: mapPx, height: mapPx }} />
              <svg className="map-paint-edge" viewBox={`0 0 ${paint.w} ${paint.h}`}
                preserveAspectRatio="none" style={{ width: mapPx, height: mapPx }}>
                <path d={paint.edge} />
              </svg>
            </>)}
            {/* POI / 小窝 / 野生三层标记都拆成 memo 子组件(见文件底部):位置推送不改变任何
                标记数据(marks 引用不变),三层整层跳过重渲染;marks 变化(开关/阈值/新数据)
                只重渲染对应那一层。 */}
            <PoiLayer marks={pois.marks} mapPx={mapPx} />
            <NestLayer marks={home.marks} mapPx={mapPx} />
            <WildLayer marks={wilds.marks} mapPx={mapPx} wildTip={wildTip} dist={wildDist} />
          </div>
          <div className="map-arrow" ref={arrowRef}>
            <svg viewBox="0 0 24 24" width="30" height="30">
              <path d="M12 2 L20 21 L12 16 L4 21 Z" fill="var(--red)" stroke="#fff" strokeWidth="1.5" strokeLinejoin="round" />
            </svg>
          </div>
          {/* 图层入口:窄屏显示在地图左下角(抽屉);桌面侧栏折叠后右上角的 ☰ 也可展开。
              带 .map-ctrl 类使点它不触发地图拖动。不复用 .filter-toggle——它的 display:none
              会被后定义的 .map-btn{display:flex} 盖掉。 */}
          <button className="map-btn map-ctrl map-layers-btn" title="图层"
            onClick={() => setCollapsed((c) => !c)}>☰</button>
          {/* 右上角控制组:图层侧栏(桌面)/放大/缩小/回中。跟随打开后,下一帧 applyFrame
              即把视口对准玩家。 */}
          <div className="map-ctrl">
            <button className={'map-btn map-layers-toggle' + (sidebarOpen ? ' on' : '')} title="图层栏"
              onClick={toggleLayers}>☰</button>
            <button className="map-btn" title="放大" onClick={() => view.zoomAround(1.4, view.vp.w / 2, view.vp.h / 2)}>＋</button>
            <button className="map-btn" title="缩小" onClick={() => view.zoomAround(1 / 1.4, view.vp.w / 2, view.vp.h / 2)}>－</button>
            <button className={'map-btn' + (view.follow ? ' on' : '')} title="回到当前位置" onClick={() => view.setFollow(true)}>◎</button>
          </div>
        </div>
        ) : (
          <div className="map-nomap">
            <div className="map-nomap-name">{pos.sceneName || '未知场景'}</div>
            <div className="muted">该场景无底图,仅显示坐标</div>
            <div className="map-coords">X {pos.x} · Y {pos.y} · Z {pos.z}</div>
          </div>
        ))}
      </div>
      {detailGid != null && <PetDetailModal gid={detailGid} onClose={() => setDetailGid(null)} />}
    </div>
  )
}

// —— 标记层(memo 子组件)——
// 三层都只依赖「标记数据(引用稳定) + mapPx(缩放/视口)」,与 position 推送无关:
// 玩家移动逐包推位置 → MapPage 重渲染,但这三层的 props 引用都不变,React.memo 整层跳过,
// 只有箭头/底图/跟随这些轻量元素参与。mapPx 随缩放/视口变化,那时三层本来就要重排,不亏。

// POI 标记:与底图同属 .map-world(一起平移,不会相对底图抖动);尺寸恒定不随缩放变大,
// 故位置用 left/top + translate(-50%,-50%) 定在锚点上。洞穴层的点也用底图投影,自然
// 落在层图上。sure(收集模式「已确认还在」高亮)已由 usePois 预计算好。
const PoiLayer = React.memo(({ marks, mapPx }) => (
  <>{marks.map((p, i) => (
    <img key={i} alt="" draggable={false}
      className={'map-poi' + (p.sure ? ' sure' : '')}
      src={imgURL(p.icon)} title={p.n}
      style={{ left: p.u * mapPx, top: p.v * mapPx }} />
  ))}</>
))

// 家园小窝:空窝画个虚线圈,住了宠物画头像;窝上有蛋则右上角挂个蛋图标。
// 悬浮看简要信息(见 nestTitle),点住户看宠物详情。同属 .map-world 一起平移。
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

// 野生宠物标记:圆头像 + 类别描边(异色/炫彩、污染、奖牌四件套),同属 .map-world 一起平移。
// 与 POI 同样尺寸恒定,故用 left/top + translate(-50%,-50%) 钉在锚点上。描边样式(style)
// 已在 useWildPets 的 marks 里按命中类别预计算,这里直接展开,不再逐标记调 wildRing。
// 异色/炫彩头像右上角再叠游戏标记图(兼具用合成的异色炫彩图,与 badges 的 Marks 同口径);
// 图标取自全局 IconsContext(启动拉一次,引用稳定,不影响本层 memo)。缺图时不叠加。
// 桌面 hover 有 title 悬浮;触屏没有 hover,点一下弹资料卡(wildTip),点中放大提亮。
// wildTip 是点选状态:只在它变化时(以及 marks/mapPx/dist 变化时)重渲染这一层。
// dist 是点击时算好的距离快照(标量),位置推送不改它,故本层仍不受高频位置推送打扰。
const WildLayer = React.memo(({ marks, mapPx, wildTip, dist }) => {
  const icons = React.useContext(IconsContext)
  return (
    <>{marks.map((p) => {
      // 普通野生宠(「全部野生」图层):只画小头像点,无描边/标记图/资料卡;title 只给名字。
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
      // 异色/炫彩是全场最稀有的类别,单独走「稀有特效」视觉(放大 + 光环 + 角标徽章)。
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
