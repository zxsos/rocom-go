// 画中画(PiP)的纯几何与工具:世界坐标 → 屏幕坐标、视口裁剪、内容签名、涂地边界解析。
//
// 本文件**不依赖 React、不碰 DOM**(除 snap 的可选 dpr 读取由调用方传入),以便
// web/scripts/verify-pip.mjs 直接 import 校验 —— 地图绘制的数学错了很难靠肉眼在小窗里
// 看出来(差几个像素就是「标记对不上底图」),必须可断言。
//
// 坐标系约定与 DOM 版一致(见 useMapEngine.jsx 的 applyFrame):
//   世界 = 底图归一化坐标 u/v ∈ [0,1];mapPx = min(视口宽, 视口高) × zoom;
//   left = 视口宽/2 − focus.u × mapPx,top = 视口高/2 − focus.v × mapPx;
//   故屏幕坐标 sx = 视口宽/2 + (u − focus.u) × mapPx。
// 归一化坐标带来一个好处:PiP 窗口尺寸与主地图不同也无需换算,同一套公式直接套。

// —— 标记尺寸:照抄 map.css,改那里记得改这里 ——
// 单位是 CSS 像素(画在 512×512 画布上就是画布像素)。PiP 画布比主地图视口小,
// 但这些尺寸是**屏幕像素恒定**的(主地图里标记的 width/height 也不随缩放变大),
// 故直接照搬,不按 PiP 尺寸缩放——否则小窗里标记会小到看不见。
export const SIZES = {
  poi: 26,           // .map-poi:height(宽按图比例自适应)
  wild: 30,          // .map-wild
  wildRare: 40,      // .map-wild.rare
  wildAll: 22,       // .map-wild-all
  nest: 34,          // .map-nest
  nestEmpty: 26,     // .map-nest.empty
  nestEgg: 20,       // .map-nest-egg(挂右上,略微出框)
  arrow: 30,         // .map-arrow 内的 svg
  markBadge: 16,     // .map-wild-mark 角标圆盘
  markBadgeIcon: 10, // 角标内的图标
  routeWidth: 2.5,   // .map-routes path stroke-width
  routeStart: 6,     // 起点圆
  routeArrived: 5.5, // 跟走中:已到达点
  routeNext: 9,      // 下一目标点
  routeNextCore: 3,  // 下一目标点的白心
  routeEnd: 5,       // 终点圆
  routeTeleport: 10, // 传送落点菱形(10×10 rotate 45)
  paintEdge: 1.2,    // .map-paint-edge path stroke-width(non-scaling-stroke)
  // 头像边缘羽化到圆周处**保留**的不透明度(照抄 .map-wild-face 的 mask 终点 alpha):
  // 中心 60% 全实,往外渐隐到这里就不再继续变透。0.5 = 周围那圈是 50% 半透明,
  // 既透出底图又留得住轮廓;取 0 则是彻底渐隐、边缘会消失。
  featherEdge: 0.5,
}

// 跑图路线专用常量(见 useRoutes.jsx):路线数据是 8192×8192 画布,除 GRID 即归一化坐标;
// 相邻点距离超过 TELEPORT 判为传送,断开不画直线。
export const ROUTE_GRID = 8192
export const ROUTE_TELEPORT = 300

// worldToScreen 把底图归一化坐标换算成屏幕(画布)坐标。focus 是视口中心对应的地图坐标。
export function worldToScreen(u, v, focus, mapPx, w, h) {
  return {
    x: w / 2 + (u - focus.u) * mapPx,
    y: h / 2 + (v - focus.v) * mapPx,
  }
}

// mapPxOf 与 useMapEngine.applyFrame 同源:地图边长 = 视口短边 × zoom。
export function mapPxOf(w, h, zoom) {
  return (Math.min(w, h) || 1) * zoom
}

// inView 屏幕坐标是否还在画布内(pad 为外扩容差)。标记是「以锚点为中心」画的
// (DOM 里靠 translate(-50%,-50%)),故留出一个标记尺寸的余量:刚好擦边的标记
// 若被裁掉,小窗边缘会出现半张脸。
export function inView(x, y, w, h, pad = 40) {
  return x >= -pad && y >= -pad && x <= w + pad && y <= h + pad
}

// —— 内容签名:内容没变就跳过重绘 ——
// PiP 是手动推帧(captureStream(0) + requestFrame),画一次才推一帧;玩家站着不动时
// 每秒重画 10 次同样的画面纯属浪费 CPU。故每帧算一个签名,与上一帧相同就完全不画。
//
// 标记数组用**引用**而非内容参与签名:pois/wilds/routes 的 marks 都是 useMemo 产物,
// 数据没变引用就不变(见 usePois/useWildPets/useRoutes);内容变了引用必然变。
// 这样既零成本(不用遍历几百个标记)又不会漏——比按内容逐项哈希更可靠。
// 用 WeakMap 给数组分配稳定 id:同一个数组两次调用得到相同 id,不同数组得到不同 id。
const refIds = new WeakMap()
let nextRefId = 1
function refId(o) {
  if (!o) return 0
  let id = refIds.get(o)
  if (id === undefined) { id = nextRefId++; refIds.set(o, id) }
  return id
}

// 位置的量化精度:底图 4096px,1e-5 ≈ 0.04 底图像素,放大到 zoom 32 也才 1.3 屏幕像素,
// 肉眼不可见;再粗就可能在小窗里看出箭头跳格。
const POS_QUANT = 1e5
// 朝向量化到 3°:箭头 30px,旋转 3° 时尖端位移约 0.8px,看不出差别。
const HEADING_STEP = 3

// frameSig 由一帧的绘制输入算出可比较的签名。snap 字段见 pipDraw 的约定;
// sigMarks 是参与签名的标记数组集合(pois/routes/nests/wilds 的 marks)。
export function frameSig(snap) {
  const p = snap.player
  // player 为空(等位置数据/无底图场景)也参与签名:从「有位置」变「没位置」画面要重画。
  const pos = p
    ? `${Math.round(p.u * POS_QUANT)},${Math.round(p.v * POS_QUANT)},${Math.round((p.heading || 0) / HEADING_STEP)}`
    : '-'
  // 无底图场景(洞穴/家园室内)画面上显示的是**实时坐标**,而坐标不进 player
  // (player 是有底图时的归一化位置)。不把它们放进签名,小窗就会停在旧坐标上不动。
  const nm = snap.nomap
  const coord = nm ? `${nm.x},${nm.y},${nm.z}` : ''
  return [
    snap.sceneImg || '', snap.layerImg || '',
    snap.w, snap.h, snap.zoom, pos, coord,
    snap.paintVer || 0,
    ...(snap.sigMarks ? snap.sigMarks.map(refId) : []),
  ].join('|')
}

// —— 涂地边界路径解析 ——
// usePaint 的 edgePath 只产出三种命令(见 usePaint.js:42,54):
//   `M${x} ${y}H${x2}`  横向一段   `M${x} ${y}V${y2}`  纵向一段
// 故这里只认 M / H / V,不实现完整 SVG 路径——画法刻意**不走 Path2D + ctx.scale**:
// 位图描边一放大就成毛边,当年描边从位图改成 SVG 就是为这个(见 map.css:88-99)。
// 这里把每段解析成格子坐标,绘制时自己换算到屏幕像素再画 1.2px 的线,
// 效果等价于 non-scaling-stroke:线宽恒定,不随地图缩放变粗。
// 返回 [{ type:'M'|'H'|'V', x, y }]。H/V 段**各自补齐了缺失的那一维**:H 线的 y 取自它
// 所属 M 的 y,V 线的 x 取自所属 M 的 x —— 这样每段都是完整坐标,绘制时可直接喂给
// edgeToScreen,不必让调用方自己维护「当前点」状态机。
export function parseEdge(path) {
  const out = []
  if (!path) return out
  // M 带两个参数,H/V 带一个;数值均非负整数(格子号),宽松匹配以防将来出现小数。
  const re = /([MHV])\s*(-?[\d.]+)(?:\s+(-?[\d.]+))?/g
  let mx = 0, my = 0 // 最近一条 M 的坐标(edgePath 的 M 一定先于其 H/V 出现)
  let m
  while ((m = re.exec(path)) !== null) {
    const cmd = m[1]
    const a = Number(m[2])
    const b = m[3] === undefined ? 0 : Number(m[3])
    if (!Number.isFinite(a) || !Number.isFinite(b)) continue
    if (cmd === 'M') {
      mx = a; my = b
      out.push({ type: 'M', x: a, y: b })
    } else if (cmd === 'H') {
      out.push({ type: 'H', x: a, y: my })
    } else {
      out.push({ type: 'V', x: mx, y: a })
    }
  }
  return out
}

// edgeToScreen 把解析出的格子坐标段换算成屏幕坐标。
// left/top 是世界原点(0,0)的屏幕坐标,mapPx 是底图在屏幕上的边长;
// 格子坐标除以格数(gw/gh)即归一化坐标——涂地是铺满整张底图的。
export function edgeToScreen(seg, left, top, mapPx, gw, gh) {
  const nx = gw > 0 ? seg.x / gw : 0
  const ny = gh > 0 ? seg.y / gh : 0
  return { x: left + nx * mapPx, y: top + ny * mapPx }
}

// markerScale 算小窗里标记该画多大。
//
// PiP 与主地图**用同一个 zoom**,但画布尺寸不同(小窗 512px vs 主地图视口短边),
// 同样的标记像素数在小窗里会覆盖更大的地图范围 —— 缩小看全图时标记就会糊成一片。
// 故按像素密度比缩放:标记尺寸 × (小窗边长 / 主地图视口短边),
// 使小窗里每个标记覆盖的**地图范围**与主地图一致,视觉上小窗就是主地图的缩小版。
//
//   推导:标记覆盖的地图归一化的量 = 尺寸 / mapPx = 尺寸 / (边长 × zoom)
//     主地图: 30 / (vp短 × zoom)
//     小窗  : (30 × s) / (512 × zoom)   令两者相等 → s = 512 / vp短
//
// 再夹到 [0.5, 1.5]:4K 屏视口短边可达 2000+,照算标记只剩 7px,小窗里根本看不见;
// 手机上视口短边 340 时又放大到 1.5 倍,标记会挤爆小窗。两端各留一档,
// 宁可牺牲一点「严格等比」,也要保证小窗可读。
// vpShort 为 0(视口尚未测量)时回退 1,即不缩放。
export function markerScale(vpShort, pipSize) {
  if (!Number.isFinite(vpShort) || vpShort <= 0) return 1
  const s = pipSize / vpShort
  return Math.max(0.5, Math.min(1.5, s))
}
