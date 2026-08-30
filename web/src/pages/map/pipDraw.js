// 画中画(PiP)的 canvas 绘制:把地图按与主地图一致的层序手绘到一张离屏画布上。
//
// 为什么必须手绘而不能「截图」DOM:系统画中画只吃 video,而 video 的帧只能来自
// canvas.captureStream() 或 MediaRecorder;DOM 无法直接变成视频流(html-to-image 那套
// 是逐次截图,做不到每秒十帧且不掉帧)。故这里把 MapViz 的 DOM 渲染**平行重写**一遍:
// 同样的坐标、同样的层序、同样的尺寸与配色,只是输出目标是 canvas 2D。
//
// 两条渲染链互不干扰:本文件只读 ref 快照,不触发任何 React 重渲染,也不引用任何
// React API;几何计算全在 pipGeom.js(已被 scripts/verify-pip.mjs 断言)。
//
// 层序与 MapViz 一致(见 useMapEngine.jsx 的 MapViz):
//   底图 → 洞穴层图 → 涂地(填充+边界)→ 跑图路线 → POI → 家园小窝 → 野生宠 → 玩家箭头
//
// 刻意省略的:所有 CSS 动画(稀有光环旋转、呼吸脉动、小窝 bob)。PiP 是静态逐帧渲染,
// 动画要么每帧重算(白烧 CPU),要么冻在中途更难看,故一律画成静止的等价形态。

import { imgURL } from '../../components/icons'
import {
  SIZES, ROUTE_GRID, ROUTE_TELEPORT,
  worldToScreen, mapPxOf, inView, parseEdge, edgeToScreen,
} from './pipGeom'

const TAU = Math.PI * 2

// —— 图标缓存 ——
// 图标与底图都是 /img/... 同域静态资源(见 components/icons.jsx 的 imgURL),
// 画进 canvas 不会有 CORS 污染。缓存按 URL 复用 Image 对象,避免每帧新建。
const iconCache = new Map()

// loadIcon 取一个已加载完成的 Image;未加载完返回 null(下一帧自然补上,
// 不做等待——绘制是逐帧的,缺一张图不该阻塞整帧)。
function loadIcon(path) {
  if (!path) return null
  const url = imgURL(path)
  let im = iconCache.get(url)
  if (im === undefined) {
    im = new Image()
    im.decoding = 'async'
    im.src = url
    iconCache.set(url, im)
  }
  // complete 为 true 也可能是加载失败,须再看 naturalWidth。
  return im.complete && im.naturalWidth ? im : null
}

// —— 配色:从 CSS 变量读,随主题变化 ——
// canvas 需要具体色值,拿不到 var()。故启动时与主题切换后各读一次 computed style,
// 缓存成 palette(见 usePip.js)。兜底色取自 base.css 的暗色主题值。
export function readPalette() {
  const cs = getComputedStyle(document.documentElement)
  const v = (k, fb) => (cs.getPropertyValue(k) || '').trim() || fb
  return {
    bg: v('--bg', '#0e1116'),
    bg1: v('--bg-1', '#161b22'),
    line: v('--line', '#2a3240'),
    fg: v('--fg', '#e6edf3'),
    fgDim: v('--fg-dim', '#9aa7b4'),
    gold: v('--gold', '#f5b942'),
    red: v('--red', '#f85149'),
    star: v('--c-star', '#ffd54a'),
    medal: v('--c-medal', '#ffc107'),
    portal: v('--c-portal', '#4a3f8a'),
    flowerD: v('--c-flower-d', '#6b2f50'),
    shiny: v('--c-shiny', '#c9b6ff'),
  }
}

// —— 涂地填充的位图缓存 ——
// 与 usePaint 的 draw 同一算法(按位写字 → ImageData),但画到一张 gw×gh 的离屏画布上,
// 再由 drawImage 缩放到屏幕尺寸——等价于 DOM 里 canvas 位图被 CSS 拉到底图尺寸的效果
// (放大后被插值柔化,正好把方格化开)。
// 只在「格数或世代号变了」时重画:涂地增量每秒可能来几批,但整张重铺是几万次遍历。
let paintCanvas = null
let paintKey = ''
function paintFill(bits, gw, gh, ver) {
  const key = `${gw}x${gh}|${ver}`
  if (paintKey === key && paintCanvas) return paintCanvas
  if (!paintCanvas) paintCanvas = document.createElement('canvas')
  paintCanvas.width = gw
  paintCanvas.height = gh
  const c = paintCanvas.getContext('2d')
  if (c && bits) {
    const img = c.createImageData(gw, gh)
    const d = img.data
    // 配色与 usePaint 的 FILL_RGBA 一致(#a855f7 约 32%)
    for (let b = 0, n = bits.length; b < n; b++) {
      if (bits[b] === 0) continue // 整字节没涂(绝大多数):8 格一起跳过
      for (let k = 0; k < 8; k++) {
        if ((bits[b] & (1 << k)) === 0) continue
        const o = ((b << 3) | k) * 4
        d[o] = 168; d[o + 1] = 85; d[o + 2] = 247; d[o + 3] = 82
      }
    }
    c.putImageData(img, 0, 0)
  }
  paintKey = key
  return paintCanvas
}

// —— 头像四周羽化 ——
// 与 map.css 的 .map-wild-face / .map-nest img 同一视觉:径向遮罩,中心 60% 全不透明、
// 到圆周渐变到透明,让头像融入底图而不是硬切一个圆。
//
// canvas 没有 mask,故在离屏画布上先画头像、再用 destination-out 擦掉边缘。
// **不能直接对主画布做 destination-out** —— 那会把已经画好的底图一起擦出洞,
// 故必须先在离屏画布上合成好,再整体 drawImage 过去。
const FEATHER = 64 // 离屏画布边长:固定值,不随标记尺寸变,免得每次重设尺寸重新分配
let featherCanvas = null
function featheredAvatar(im, scale) {
  if (!featherCanvas) {
    featherCanvas = document.createElement('canvas')
    featherCanvas.width = featherCanvas.height = FEATHER
  }
  const c = featherCanvas.getContext('2d')
  if (!c) return im // 拿不到 2d 上下文(极罕见):退回原图,不羽化
  c.clearRect(0, 0, FEATHER, FEATHER)
  const r = FEATHER / 2
  c.save()
  c.beginPath(); c.arc(r, r, r, 0, TAU); c.clip()
  const dw = FEATHER * scale
  c.drawImage(im, (FEATHER - dw) / 2, (FEATHER - dw) / 2, dw, dw)
  // 擦边缘:内圈(60% 半径)alpha 0 不擦,到圆周 alpha 1 全擦,中间线性过渡
  c.globalCompositeOperation = 'destination-out'
  const g = c.createRadialGradient(r, r, r * 0.6, r, r, r)
  g.addColorStop(0, 'rgba(0,0,0,0)')
  g.addColorStop(1, 'rgba(0,0,0,1)')
  c.fillStyle = g
  c.fillRect(0, 0, FEATHER, FEATHER) // 被 clip 限制在圆内,圆外本就透明
  c.restore()
  return featherCanvas
}

// —— 外环解析:把 wildRing 产出的 box-shadow 翻成可画的圆环 ——
// wildRing(见 wildMatch.js)把叠加的类别描边写成 box-shadow,形如:
//   "0 0 0 3px #ff5252, 0 0 0 6px #40c4ff, 0 0 8px 1px #ff5252"
// 前两档是实心外扩环(blur=0),最后一档是柔光(blur>0)。
// 按 box-shadow 语法 offsetX offsetY blur spread color 依次解析长度值。
export function parseRings(boxShadow) {
  const out = []
  if (!boxShadow) return out
  for (const seg of String(boxShadow).split(',')) {
    const nums = [...seg.matchAll(/(-?[\d.]+)px/g)]
    if (!nums.length) continue
    const last = nums[nums.length - 1]
    const spread = nums.length > 3 ? parseFloat(nums[3][1]) : 0
    const blur = nums.length > 2 ? parseFloat(nums[2][1]) : 0
    const color = seg.slice(last.index + last[0].length).trim()
    if (color) out.push({ spread, blur, color })
  }
  return out
}

// —— 主绘制入口 ——
// snap 由 usePip 每帧从引擎 ref 组装;字段说明见 usePip.js 的 buildSnap。
export function renderToCanvas(ctx, snap) {
  const { w, h, zoom, focus, palette } = snap
  // scale 是标记的尺寸缩放系数(按小窗与主地图的像素密度比,见 pipGeom.markerScale)。
  // 所有 SIZES.* 用到时都乘它;线宽同理 —— 否则标记小了描边还那么粗,看着就脏。
  const scale = snap.scale || 1
  const mapPx = mapPxOf(w, h, zoom)
  // 世界原点(u=v=0,底图左上角)在屏幕上的位置:之后所有图层都以它为基准平移。
  const origin = worldToScreen(0, 0, focus, mapPx, w, h)

  ctx.clearRect(0, 0, w, h)
  ctx.fillStyle = palette.bg
  ctx.fillRect(0, 0, w, h)

  if (!snap.sceneImg) { drawNoMap(ctx, snap); return }

  const base = loadIcon(`bigmap/${snap.sceneImg}.webp`)
  if (base) ctx.drawImage(base, origin.x, origin.y, mapPx, mapPx)

  if (snap.layerImg && snap.layerRect) drawLayer(ctx, snap, origin, mapPx)
  if (snap.paint && snap.paint.on) drawPaint(ctx, snap, origin, mapPx, scale)
  if (snap.routes) drawRoutes(ctx, snap, focus, mapPx, w, h, scale)
  if (snap.pois) drawPois(ctx, snap, focus, mapPx, w, h, palette, scale)
  if (snap.nests) drawNests(ctx, snap, focus, mapPx, w, h, palette, scale)
  if (snap.wilds) drawWilds(ctx, snap, focus, mapPx, w, h, palette, scale)
  if (snap.player) drawArrow(ctx, snap, focus, mapPx, w, h, palette, scale)
}

// 无底图场景(洞穴/家园室内等):照抄 .map-nomap —— 场景名 + 一行坐标。
function drawNoMap(ctx, snap) {
  const { w, h, palette, nomap } = snap
  ctx.textAlign = 'center'
  ctx.fillStyle = palette.fg
  ctx.font = '600 18px system-ui, -apple-system, "PingFang SC", sans-serif'
  ctx.fillText(nomap?.name || '等待位置数据…', w / 2, h / 2 - 12)
  ctx.fillStyle = palette.fgDim
  ctx.font = '13px system-ui, -apple-system, "PingFang SC", sans-serif'
  const line = nomap
    ? `X ${nomap.x} · Y ${nomap.y} · Z ${nomap.z}`
    : '需后端正在抓包/回放,且玩家已登录并移动过'
  ctx.fillText(line, w / 2, h / 2 + 14)
  ctx.textAlign = 'start'
}

// 洞穴/地下层切片:按后端给的 u0/v0/u1/v1 矩形贴到底图对应位置。
function drawLayer(ctx, snap, origin, mapPx) {
  const im = loadIcon(`bigmap/${snap.layerImg}.webp`)
  if (!im) return
  const { u0, v0, u1, v1 } = snap.layerRect
  ctx.drawImage(im,
    origin.x + u0 * mapPx, origin.y + v0 * mapPx,
    (u1 - u0) * mapPx, (v1 - v0) * mapPx)
}

// 涂地:淡填充(位图缩放)+ 边界细线(按屏幕像素画,不随缩放变粗)。
function drawPaint(ctx, snap, origin, mapPx, scale) {
  const { bits, gw, gh, edge, ver } = snap.paint
  if (bits && gw > 0 && gh > 0) {
    const fill = paintFill(bits, gw, gh, ver)
    if (fill) ctx.drawImage(fill, origin.x, origin.y, mapPx, mapPx)
  }
  if (!edge) return
  // 线宽恒为屏幕上的 1.2px,等价于 SVG 的 vector-effect:non-scaling-stroke。
  // 位图描边一放大就成了毛边,当年描边从位图改成 SVG 正是为这个(见 map.css:88-99)。
  ctx.save()
  ctx.strokeStyle = 'rgba(237, 233, 254, .95)'
  ctx.lineWidth = SIZES.paintEdge * scale
  ctx.beginPath()
  let started = false
  for (const seg of parseEdge(edge)) {
    const p = edgeToScreen(seg, origin.x, origin.y, mapPx, gw, gh)
    if (seg.type === 'M') { ctx.moveTo(p.x, p.y); started = true }
    else if (started) ctx.lineTo(p.x, p.y)
  }
  ctx.stroke()
  ctx.restore()
}

// 跑图路线:折线 + 起终点/到达点/下一目标/传送落点。几何口径照抄 RouteLayer
// (useRoutes.jsx:331-394):路线点是 8192 画布坐标,除 GRID 得归一化坐标。
function drawRoutes(ctx, snap, focus, mapPx, w, h, scale) {
  for (const r of snap.routes) {
    const pts = r.points
    if (!pts || pts.length < 2) continue
    const from = r.follow && r.progress >= 0 ? Math.min(r.progress, pts.length - 1) : 0
    const rest = pts.slice(from)
    // 传送断点:相邻点距离超过 TELEPORT 判为直接传送,断开不画那条笔直长线。
    const tele = new Set()
    for (let i = 1; i < rest.length; i++) {
      if (Math.hypot(rest[i].x - rest[i - 1].x, rest[i].y - rest[i - 1].y) > ROUTE_TELEPORT) tele.add(i)
    }
    const at = (p) => worldToScreen(p.x / ROUTE_GRID, p.y / ROUTE_GRID, focus, mapPx, w, h)

    ctx.save()
    ctx.strokeStyle = r.color
    ctx.lineWidth = SIZES.routeWidth * scale
    ctx.globalAlpha = 0.9
    ctx.lineJoin = 'round'
    ctx.lineCap = 'round'
    ctx.beginPath()
    let pen = false
    for (let i = 0; i < rest.length; i++) {
      const p = at(rest[i])
      // 断点处重起一条:只在大概率可见时才画,长线即便完全出界也只是一段直线。
      if (i === 0 || tele.has(i)) { ctx.moveTo(p.x, p.y); pen = true }
      else if (pen) ctx.lineTo(p.x, p.y)
    }
    ctx.stroke()
    ctx.globalAlpha = 1

    // 传送落点:路线色菱形(10×10 rotate 45)+ 白描边 + 白心
    for (const i of tele) {
      const p = at(rest[i])
      if (!inView(p.x, p.y, w, h)) continue
      dot(ctx, p.x, p.y, SIZES.routeTeleport * scale, r.color, '#fff', 1.8 * scale, true)
      dot(ctx, p.x, p.y, 3 * scale, '#fff', null, 0)
    }

    const s = at(rest[0])
    const e = at(rest[rest.length - 1])
    // 跟走开始后起点圆消失,改由「已到达点」(路线色)标记;否则画白心起点圆。
    const showStart = !r.follow || r.progress < 0
    if (showStart && inView(s.x, s.y, w, h)) {
      dot(ctx, s.x, s.y, SIZES.routeStart * scale, '#fff', '#000', 1.5 * scale)
    }
    if (!showStart && inView(s.x, s.y, w, h)) {
      dot(ctx, s.x, s.y, SIZES.routeArrived * scale, r.color, '#fff', 1.5 * scale)
    }
    if (inView(e.x, e.y, w, h)) {
      dot(ctx, e.x, e.y, SIZES.routeEnd * scale, r.color, '#fff', 1.2 * scale)
    }

    // 下一目标点:路线色大圆 + 白描边 + 白心(跟走模式下才存在)
    if (r.follow && r.progress >= 0 && r.progress + 1 < pts.length) {
      const nx = at(pts[r.progress + 1])
      if (inView(nx.x, nx.y, w, h)) {
        dot(ctx, nx.x, nx.y, SIZES.routeNext * scale, r.color, '#fff', 2 * scale)
        dot(ctx, nx.x, nx.y, SIZES.routeNextCore * scale, '#fff', null, 0)
      }
    }
    ctx.restore()
  }
}

// dot 画一个带描边的圆点(路线图的各种端点共用)。diamond=true 时画菱形(传送落点)。
// 半径与描边宽都按 scale 缩放(见 renderToCanvas 的 scale 说明)。
function dot(ctx, x, y, r, fill, stroke, strokeW, diamond = false) {
  if (r <= 0) return
  ctx.beginPath()
  if (diamond) {
    ctx.save()
    ctx.translate(x, y)
    ctx.rotate(Math.PI / 4)
    ctx.rect(-r / 2, -r / 2, r, r)
    ctx.restore()
  } else {
    ctx.arc(x, y, r, 0, TAU)
  }
  if (fill) { ctx.fillStyle = fill; ctx.fill() }
  if (stroke && strokeW) { ctx.lineWidth = strokeW; ctx.strokeStyle = stroke; ctx.stroke() }
}

// POI 标记:高度固定 26px、宽度按图标原始比例自适应(解包图标不是方的,
// 写死等宽高会把竖长图标横向拉伸——见 map.css:163-166)。
function drawPois(ctx, snap, focus, mapPx, w, h, palette, scale) {
  for (const p of snap.pois) {
    const s = worldToScreen(p.u, p.v, focus, mapPx, w, h)
    if (!inView(s.x, s.y, w, h)) continue
    const im = loadIcon(p.icon)
    const hgt = SIZES.poi * scale
    if (!im) { // 图没加载完:先画个占位圆,下一帧自然补上
      dot(ctx, s.x, s.y, hgt / 2, palette.bg1, palette.line, scale)
      continue
    }
    const wid = hgt * (im.naturalWidth / im.naturalHeight)
    ctx.save()
    // 收集模式下「已确认还在」的点:金色外发光(对应 .map-poi.sure)
    if (p.sure) { ctx.shadowColor = palette.star; ctx.shadowBlur = 6 * scale }
    else {
      ctx.shadowColor = 'rgba(0,0,0,.55)'; ctx.shadowBlur = 2 * scale
      ctx.shadowOffsetY = scale
    }
    ctx.drawImage(im, s.x - wid / 2, s.y - hgt / 2, wid, hgt)
    ctx.restore()
  }
}

// 家园小窝:圆形头像 + 金边;空窝是虚线圈 + 「空」字(背景半透明,相邻两窝只隔几十
// 像素,实心底色会糊成一团)。窝上没收的蛋挂右上角。
function drawNests(ctx, snap, focus, mapPx, w, h, palette, scale) {
  for (const n of snap.nests) {
    const s = worldToScreen(n.u, n.v, focus, mapPx, w, h)
    if (!inView(s.x, s.y, w, h)) continue
    const empty = !n.pet
    const size = (empty ? SIZES.nestEmpty : SIZES.nest) * scale
    const r = size / 2

    ctx.save()
    if (empty) {
      ctx.fillStyle = 'rgba(11, 14, 19, .35)'
      ctx.beginPath(); ctx.arc(s.x, s.y, r, 0, TAU); ctx.fill()
      ctx.setLineDash([3 * scale, 3 * scale])
      ctx.lineWidth = 2 * scale
      ctx.strokeStyle = palette.fgDim
      ctx.stroke()
      ctx.setLineDash([])
      ctx.fillStyle = palette.fgDim
      ctx.font = `${Math.round(11 * scale)}px system-ui, -apple-system, "PingFang SC", sans-serif`
      ctx.textAlign = 'center'
      ctx.textBaseline = 'middle'
      ctx.fillText('空', s.x, s.y)
    } else {
      // 住户头像:裁圆后铺满(canvas 没有 object-fit,手动 clip)。
      // **不铺实心底色**(与 map.css 的 .map-nest 一致):头像由 featheredAvatar 羽化
      // 边缘,底色会把它糊回一块不透明圆片,掩盖住羽化效果。
      const im = n.pet.img ? loadIcon(n.pet.img) : null
      if (im) {
        // 头像资源四周自带透明留白,放大 1.3 撑满内圆(与 .map-wild.rare 的头像同处理);
        // 羽化同样作用于住户头像(与 CSS 一致),蛋图标不在此列(它是单独画的)。
        const cr = r - 2 * scale
        ctx.drawImage(featheredAvatar(im, 1.3), s.x - cr, s.y - cr, cr * 2, cr * 2)
      }
      ctx.lineWidth = 2 * scale
      ctx.strokeStyle = palette.gold
      ctx.beginPath(); ctx.arc(s.x, s.y, r, 0, TAU); ctx.stroke()
    }

    // 窝上的蛋:右上角略微出框
    if (n.egg && n.egg.icon) {
      const egg = loadIcon(n.egg.icon)
      if (egg) {
        ctx.save()
        ctx.shadowColor = 'rgba(0,0,0,.8)'
        ctx.shadowBlur = 2 * scale
        ctx.shadowOffsetY = scale
        const es = SIZES.nestEgg * scale
        ctx.drawImage(egg, s.x + r - 8 * scale, s.y - r - 8 * scale, es, es)
        ctx.restore()
      }
    }
    ctx.restore()
  }
}

// 野生宠物:圆形头像 + 类别描边(含叠加外环)+ 异色/炫彩角标。
// 尺寸与降级样式照抄 map.css:稀有 40px、普通稀有 30px、「全部野生」22px;
// 已离开视野(stale)压暗降饱和。
function drawWilds(ctx, snap, focus, mapPx, w, h, palette, scale) {
  for (const p of snap.wilds) {
    const s = worldToScreen(p.u, p.v, focus, mapPx, w, h)
    if (!inView(s.x, s.y, w, h)) continue
    const kinds = p.kinds || []
    const rare = kinds.includes('shiny') || kinds.includes('colorful')
    const all = !!p.all
    const size = (all ? SIZES.wildAll : rare ? SIZES.wildRare : SIZES.wild) * scale
    const r = size / 2
    const st = p.style || {}

    ctx.save()
    // 降级与失效:全部野生 opacity .55 + grayscale .4;已离开视野 opacity .4 + grayscale .6
    if (all) { ctx.globalAlpha = 0.55; ctx.filter = 'grayscale(.4)' }
    if (p.stale) { ctx.globalAlpha = all ? 0.25 : 0.4; ctx.filter = 'grayscale(.6)' }

    // 头像:裁圆铺满。稀有宠的头像再放大 1.3 裁掉资源留白。
    // **不铺实心底色**(与 map.css 的 .map-wild 一致):羽化边缘要能透出底图。
    const im = p.img ? loadIcon(p.img) : null
    const clipR = rare ? r : r - 2 * scale
    ctx.save()
    ctx.beginPath(); ctx.arc(s.x, s.y, clipR, 0, TAU); ctx.clip()
    if (im) {
      // 羽化后的头像自带圆形(边缘已渐隐),无需再 clip 裁圆
      ctx.drawImage(featheredAvatar(im, rare ? 1.3 : 1),
        s.x - clipR, s.y - clipR, clipR * 2, clipR * 2)
    } else {
      // 缺图回退 emoji(与 .map-wild-face-fallback 一致)
      ctx.fillStyle = palette.fg
      ctx.font = `${Math.round(size * 0.53)}px system-ui, sans-serif`
      ctx.textAlign = 'center'
      ctx.textBaseline = 'middle'
      ctx.fillText('🐾', s.x, s.y)
    }
    ctx.restore()

    if (rare) {
      // 稀有光环:金色细环 + 外发光。CSS 里是两道反向旋转的 conic 渐变环,
      // 这里画成静态的单圈金环 + 柔光——小窗里靠「金色粗细」认稀有度已足够。
      ctx.save()
      ctx.shadowColor = 'rgba(255, 213, 74, .55)'
      ctx.shadowBlur = 12 * scale
      ctx.lineWidth = 3 * scale
      ctx.strokeStyle = palette.star
      ctx.beginPath(); ctx.arc(s.x, s.y, r + 1.5 * scale, 0, TAU); ctx.stroke()
      ctx.restore()
    } else if (st.borderColor) {
      // 普通类别:主描边(奖牌命中时加粗到 3px)
      const bw = (parseFloat(st.borderWidth) || 2) * scale
      ctx.lineWidth = bw
      ctx.strokeStyle = st.borderColor
      ctx.beginPath(); ctx.arc(s.x, s.y, r - bw / 2, 0, TAU); ctx.stroke()
    }

    // 叠加外环:wildRing 把多类别叠成 box-shadow,逐环外扩描边还原。
    // 带 blur 的那一档是柔光,用 shadow 画在最后一圈外。
    // spread/blur 都是 CSS 像素,同样按 scale 换算。
    let prev = 0
    for (const ring of parseRings(st.boxShadow)) {
      if (ring.blur > 0) {
        ctx.save()
        ctx.shadowColor = ring.color
        ctx.shadowBlur = ring.blur * scale
        ctx.lineWidth = scale
        ctx.strokeStyle = ring.color
        ctx.beginPath(); ctx.arc(s.x, s.y, r + prev, 0, TAU); ctx.stroke()
        ctx.restore()
        continue
      }
      const spread = ring.spread * scale
      if (spread <= prev) continue
      const lw = spread - prev
      ctx.lineWidth = lw
      ctx.strokeStyle = ring.color
      ctx.beginPath(); ctx.arc(s.x, s.y, r + prev + lw / 2, 0, TAU); ctx.stroke()
      prev = spread
    }

    // 异色/炫彩角标:右上角的圆盘徽章(16px)+ 图标(10px)
    if (rare && !all && snap.icons) {
      const icon = (kinds.includes('shiny') && kinds.includes('colorful') && snap.icons.shinyColorful) ||
        (kinds.includes('shiny') && snap.icons.shiny) ||
        (kinds.includes('colorful') && snap.icons.colorful)
      if (icon) {
        const b = SIZES.markBadge * scale
        const bx = s.x + r - 4 * scale
        const by = s.y - r + 4 * scale
        ctx.save()
        ctx.fillStyle = kinds.includes('shiny') && kinds.includes('colorful')
          ? palette.shiny : kinds.includes('shiny') ? palette.portal : palette.flowerD
        ctx.beginPath(); ctx.arc(bx, by, b / 2, 0, TAU); ctx.fill()
        ctx.lineWidth = 1.5 * scale
        ctx.strokeStyle = '#fff'
        ctx.stroke()
        const ii = loadIcon(icon)
        const bs = SIZES.markBadgeIcon * scale
        if (ii) ctx.drawImage(ii, bx - bs / 2, by - bs / 2, bs, bs)
        ctx.restore()
      }
    }
    ctx.restore()
  }
}

// 玩家方向箭头:形状与配色照抄 .map-arrow 的 svg(M12 2 L20 21 L12 16 L4 21 Z,
// 30px,fill var(--red) + 白描边 1.5),并按朝向旋转(heading + 90,与 DOM 一致)。
// 呼吸光晕在 CSS 里是 ::before 的 radial-gradient,这里画一个半透明红晕等价物。
function drawArrow(ctx, snap, focus, mapPx, w, h, palette, scale) {
  const p = snap.player
  const s = worldToScreen(p.u, p.v, focus, mapPx, w, h)
  const arrowSize = SIZES.arrow * scale
  ctx.save()
  // 光晕
  const g = ctx.createRadialGradient(s.x, s.y, 0, s.x, s.y, arrowSize)
  g.addColorStop(0, 'rgba(248, 81, 73, .75)')
  g.addColorStop(0.7, 'rgba(248, 81, 73, 0)')
  ctx.fillStyle = g
  ctx.beginPath(); ctx.arc(s.x, s.y, arrowSize, 0, TAU); ctx.fill()

  ctx.translate(s.x, s.y)
  ctx.rotate(((p.heading || 0) + 90) * Math.PI / 180)
  // 原 svg 是 24×24 视口里的 30px 图形
  const k = arrowSize / 24
  ctx.scale(k, k)
  ctx.beginPath()
  ctx.moveTo(12, 2); ctx.lineTo(20, 21); ctx.lineTo(12, 16); ctx.lineTo(4, 21)
  ctx.closePath()
  ctx.fillStyle = palette.red
  ctx.fill()
  // 描边随 k 一起放大(与 svg 里按 30px 显示时的实际宽度成比例)
  ctx.lineWidth = 1.5 * scale
  ctx.strokeStyle = '#fff'
  ctx.lineJoin = 'round'
  ctx.stroke()
  ctx.restore()
}
