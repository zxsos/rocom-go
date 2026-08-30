import { useState, useEffect, useRef, useCallback } from 'react'
import { useStoredJSON } from '../../hooks/useStoredState'
import { renderToCanvas, readPalette } from './pipDraw'
import { frameSig, clampZoom } from './pipGeom'

// —— 画中画(PiP)生命周期 ——
// 把地图实时画到一张离屏 canvas,经 captureStream → video → requestPictureInPicture
// 投成系统悬浮窗。开启后可以切到本站其它页面、切出浏览器,小窗照常更新。
//
// 三个容易踩的坑,都在下面正面处理了:
//  1. **不能用 requestAnimationFrame 驱动**:页面切后台/最小化时 RAF 被浏览器暂停,
//     小窗会当场冻死——这正是「开 PiP 就是为了切出去」的场景。故用 setInterval,
//     后台被节流到 ~1s 时小窗降频但仍在更新。
//  2. **必须手动推帧**:captureStream(0) 取手动模式,画完调 track.requestFrame()。
//     自动模式会强制按固定帧率重绘,玩家站着不动时也白烧 CPU。
//  3. **requestPictureInPicture 必须在用户手势内调用**:放在 effect 或 setTimeout 里
//     会被浏览器直接拒绝。

// 画布尺寸:系统悬浮窗实际显示通常不到 300px,512 已经留出余量;再大只是徒增
// 底图缩放采样的开销(每帧一次 drawImage 是这个链条上的主要成本)。
const SIZE = 512
// 帧间隔:移动中 100ms(10fps),静止 500ms(2fps)。
const TICK_MOVING = 100
const TICK_IDLE = 500
// 静止判定:连续 N 帧签名没变就降到慢速档。取 20 帧 ≈ 2 秒,避免玩家短暂停止时
// 频繁在两档之间来回切(切档本身要重建 interval)。
const IDLE_FRAMES = 20

// 能力检测:三个条件缺一不可。iOS Safari 对 canvas.captureStream 支持有限,
// 桌面 Chrome / 安卓 Chrome 可用;不支持时按钮置灰并给出原因(见 MapViz)。
function detect() {
  if (typeof document === 'undefined') return { ok: false, why: '当前环境不支持' }
  if (!document.pictureInPictureEnabled) return { ok: false, why: '浏览器不支持画中画' }
  if (typeof HTMLCanvasElement === 'undefined'
    || typeof HTMLCanvasElement.prototype.captureStream !== 'function') {
    return { ok: false, why: '浏览器不支持 canvas 推流(captureStream)' }
  }
  const v = document.createElement('video')
  if (typeof v.requestPictureInPicture !== 'function') {
    return { ok: false, why: '浏览器不支持请求画中画' }
  }
  return { ok: true, why: '' }
}

// buildSnap 从引擎读一份本帧的绘制快照。全部读 ref/当前值,不触发重渲染。
// 引擎对象每次渲染都是新字面量,故必须读**最新的 engineRef.current**,
// 否则闭包里永远是首帧那份引擎、图层数据永不更新。
function buildSnap(engine, zoom, palette, icons) {
  const pos = engine.pos
  const paint = engine.paint
  const grid = paint?.gridRef?.current || { w: 0, h: 0 }
  const player = engine.frameStateRef.current // { u, v, heading }
  const hasMap = engine.hasMap && pos && pos.img

  const snap = {
    w: SIZE, h: SIZE, zoom,
    // 焦点:优先玩家当前位置(PiP 常跟随玩家);没有位置数据时退回地图中心。
    focus: player ? { u: player.u, v: player.v } : { u: 0.5, v: 0.5 },
    palette, icons,
    sceneImg: hasMap ? pos.img : '',
    layerImg: pos && pos.layer ? pos.layer.img : '',
    layerRect: pos && pos.layer ? pos.layer : null,
    pois: engine.pois ? engine.pois.marks : null,
    wilds: engine.wilds ? engine.wilds.marks : null,
    nests: engine.home ? engine.home.marks : null,
    routes: engine.routes ? engine.routes.marks : null,
    paint: paint && paint.on && paint.ready
      ? { on: true, bits: paint.bitsRef.current, gw: grid.w, gh: grid.h, edge: paint.edge, ver: paint.ver }
      : { on: false },
    player: hasMap ? player : null,
    nomap: pos ? { name: pos.sceneName, x: pos.x, y: pos.y, z: pos.z } : null,
  }
  // 参与签名的标记数组:引用不变即视为内容未变(它们都是 useMemo 产物)。
  snap.sigMarks = [snap.pois, snap.wilds, snap.nests, snap.routes]
  return snap
}

export function usePip(engine, theme) {
  const [cap] = useState(detect)
  const [active, setActive] = useState(false)
  const [zoom, setZoomState] = useStoredJSON(localStorage, 'map.pipZoom', 5, (v) => clampZoom(v))

  // 引擎与主题的最新值:绘制循环要在 interval 里读最新的,不能闭包到首帧。
  const engineRef = useRef(engine)
  engineRef.current = engine
  const paletteRef = useRef(null)
  const iconsRef = useRef({})

  const canvasRef = useRef(null)
  const videoRef = useRef(null)
  const hostNodeRef = useRef(null) // pip-video-host 的 DOM 节点(见 hostRef)
  const streamRef = useRef(null)
  const timerRef = useRef(0)
  const sigRef = useRef('')
  const idleRef = useRef(0)
  const [videoEl, setVideoEl] = useState(null)

  // 主题变化后重读配色:canvas 拿不到 var(),只能读 computed style 后缓存。
  // 配色不进内容签名(它是绘制参数不是内容),故必须手动作废签名——否则换主题后
  // 小窗还是旧配色,直到下一次玩家移动才刷新。
  useEffect(() => {
    paletteRef.current = readPalette()
    sigRef.current = ''
  }, [theme])

  // video 的挂载点:App 根部放一个 <div ref={hostRef}>,video 由这里 appendChild 进去。
  // 为什么不直接渲染 <video> 标签:video 元素必须在 PiP 开启前就存在于 DOM 且准备好,
  // 而 React 渲染时机受 PiP 开关状态影响,会导致首次点击时元素还没挂上、手势白费。
  //
  // 注意顺序:**ref 回调先于 useEffect 执行**,而 video 是在 effect 里创建的。
  // 故挂载只靠 ref 回调是不够的(首次回调时 video 还不存在),两边都要挂一次:
  // ref 回调挂「后建先挂」的情况(effect 已跑过),effect 挂「先建后挂」的首帧。
  const hostRef = useCallback((node) => {
    hostNodeRef.current = node
    if (!node) return
    const v = videoRef.current
    if (v && v.parentNode !== node) node.appendChild(v)
  }, [])

  // 离屏 canvas + video:只建一次,与 PiP 开关无关(video 元素须常驻 DOM 才能
  // 立刻响应手势;用 ref 挂载而非 JSX,避免受 PiP 开关状态影响而反复重建)。
  useEffect(() => {
    const cv = document.createElement('canvas')
    cv.width = SIZE
    cv.height = SIZE
    canvasRef.current = cv
    return () => { canvasRef.current = null }
  }, [])

  // video 元素:由 App 根部的 <div ref={videoHostRef}> 挂载(见 App.jsx)。
  // 用 state 存元素以便渲染到 React 树;muted + playsInline 是自动播放的前提。
  useEffect(() => {
    const v = document.createElement('video')
    v.muted = true
    v.defaultMuted = true
    v.playsInline = true
    v.autoplay = true
    v.className = 'pip-video'
    setVideoEl(v)
    videoRef.current = v
    // 首帧时 hostRef 的回调已经跑过了(那时 video 还不存在),这里补挂。
    if (hostNodeRef.current) hostNodeRef.current.appendChild(v)
    return () => {
      // StrictMode 下 effect 会跑两次,这里必须彻底拆掉,否则残留第二条流。
      stop()
      if (streamRef.current) {
        for (const t of streamRef.current.getTracks()) t.stop()
        streamRef.current = null
      }
      v.srcObject = null
      v.remove()
      videoRef.current = null
      setVideoEl(null)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // 画一帧并手动推流。签名没变则完全跳过(不画、不推帧)。
  const drawFrame = useCallback(() => {
    const cv = canvasRef.current
    const eng = engineRef.current
    if (!cv || !eng) return
    if (!paletteRef.current) paletteRef.current = readPalette()
    const snap = buildSnap(eng, zoom, paletteRef.current, iconsRef.current)
    const sig = frameSig(snap)
    if (sig === sigRef.current) { idleRef.current++; return }
    sigRef.current = sig
    idleRef.current = 0
    const ctx = cv.getContext('2d')
    if (!ctx) return
    renderToCanvas(ctx, snap)
    // 手动模式:画完才推这一帧。
    const track = streamRef.current && streamRef.current.getVideoTracks()[0]
    if (track && typeof track.requestFrame === 'function') {
      try { track.requestFrame() } catch { /* stream 已停,忽略 */ }
    }
  }, [zoom])

  // 帧循环:按当前是否静止在快慢两档之间切换;静止时几乎不耗 CPU。
  useEffect(() => {
    if (!active) return
    let cur = TICK_MOVING
    const tick = () => {
      drawFrame()
      // 连续多帧内容没变 → 玩家站着不动,降到慢速档;一旦有变化 drawFrame 会清零。
      const want = idleRef.current >= IDLE_FRAMES ? TICK_IDLE : TICK_MOVING
      if (want !== cur) { cur = want; schedule() }
    }
    const schedule = () => {
      clearInterval(timerRef.current)
      timerRef.current = setInterval(tick, cur)
    }
    schedule()
    return () => clearInterval(timerRef.current)
  }, [active, drawFrame])

  // 退出清理:用户点小窗的 ✕、或切了别的视频进 PiP,都会触发 leavepictureinpicture。
  useEffect(() => {
    const v = videoRef.current
    if (!v) return
    const onLeave = () => { stop() }
    v.addEventListener('leavepictureinpicture', onLeave)
    return () => v.removeEventListener('leavepictureinpicture', onLeave)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [videoEl])

  const stop = useCallback(() => {
    clearInterval(timerRef.current)
    timerRef.current = 0
    setActive(false)
    const v = videoRef.current
    if (v && document.pictureInPictureElement === v) {
      document.exitPictureInPicture().catch(() => {})
    }
  }, [])

  // 开启:**必须**在 click 回调内同步调 requestPictureInPicture,不能 defer。
  const start = useCallback(() => {
    if (!cap.ok) return
    const cv = canvasRef.current
    const v = videoRef.current
    if (!cv || !v) return
    // 先出一帧,免得进 PiP 的第一瞬间是黑的。
    drawFrame()
    if (!streamRef.current) {
      streamRef.current = cv.captureStream(0) // 0 = 手动推帧
      v.srcObject = streamRef.current
    }
    v.play().catch(() => {}) // 自动播放被拒不影响 PiP(悬浮窗会自己播)

    // —— 必须等元数据就绪才能进 PiP,否则浏览器直接拒绝 ——
    // 实测(Chromium):刚把 captureStream 挂到 srcObject 就调 requestPictureInPicture,
    // video.readyState 还是 0,抛 InvalidStateError "Metadata for the video element are
    // not loaded yet" —— 表现为「点了按钮没反应」。
    // 修复:先等 loadedmetadata。用户手势已在本次 click 中消耗,而手势的「粘性」
    // 允许在同步发起的异步回调里继续调该方法,故等一帧是安全的。
    const enter = () => {
      v.requestPictureInPicture().then(() => {
        setActive(true)
      }).catch(() => {
        // 手势过期/被拒:清掉流,下次点击重新建,不残留半开状态。
        if (streamRef.current) {
          for (const t of streamRef.current.getTracks()) t.stop()
          streamRef.current = null
          v.srcObject = null
        }
      })
    }
    if (v.readyState >= 1) enter()
    else v.addEventListener('loadedmetadata', enter, { once: true })
  }, [cap.ok, drawFrame])

  const toggle = useCallback(() => (active ? stop() : start()), [active, start, stop])

  // 缩放:PiP 窗口不可交互,故缩放只能从主页面调,存 localStorage 下次沿用。
  const zoomBy = useCallback((factor) => {
    setZoomState((z) => clampZoom(z * factor))
    // 强制重画一帧:只改缩放而内容签名没变时,画面不会自己更新。
    sigRef.current = ''
  }, [setZoomState])

  // 图标(异色/炫彩角标)由 App 从 IconsContext 拿到后注入。
  const setIcons = useCallback((icons) => { iconsRef.current = icons || {} }, [])

  return { cap, active, start, stop, toggle, zoom, zoomBy, hostRef, setIcons }
}
