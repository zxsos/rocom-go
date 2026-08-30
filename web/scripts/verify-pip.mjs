// 画中画(PiP)几何函数的校验。
//
// 为什么单独校验:canvas 手绘的地图**错了看不出来**。坐标换算差一点,标记就整体相对
// 底图偏移;视口裁剪差一点,小窗边缘就缺半只稀有宠。而 PiP 是系统悬浮窗,没有 DevTools
// 可 inspect,靠肉眼在小窗里比对底图几乎不可能。故把这部分抽成纯函数(见
// web/src/pages/map/pipGeom.js),在这里直接断言。
//
// 用法: node scripts/verify-pip.mjs   (退出码非 0 即有问题)

import { dirname } from 'node:path'
import { fileURLToPath } from 'node:url'
import {
  worldToScreen, mapPxOf, inView, frameSig, parseEdge, edgeToScreen, markerScale,
  SIZES, ROUTE_GRID, ROUTE_TELEPORT,
} from '../src/pages/map/pipGeom.js'

const DIR = dirname(fileURLToPath(import.meta.url))
let bad = 0
const fail = (msg) => { bad++; console.log('❌ ' + msg) }
const ok = (msg) => console.log('✅ ' + msg)
const near = (a, b, eps = 1e-9) => Math.abs(a - b) <= eps

// —— 1) 坐标换算:与 useMapEngine.applyFrame 的公式同源 ——
// applyFrame: left = w/2 − focus.u × px,ax = left + u × px
// 故 sx 必须等于 w/2 + (u − focus.u) × px。这里用几组数交叉验证。
{
  const w = 512, h = 512, zoom = 5
  const mapPx = mapPxOf(w, h, zoom)
  if (!near(mapPx, 2560)) fail(`mapPxOf(512,512,5) 应为 2560,实得 ${mapPx}`)
  else ok('mapPxOf 取短边 × zoom')

  // focus 在地图正中:地图中心应落在画布中心
  const c = worldToScreen(0.5, 0.5, { u: 0.5, v: 0.5 }, mapPx, w, h)
  if (!near(c.x, 256) || !near(c.y, 256)) fail(`focus 居中时 (0.5,0.5) 应画在画布中心 (256,256),实得 (${c.x},${c.y})`)
  else ok('focus 与点重合时落在画布中心')

  // focus 在左上 (0,0):地图左上角应落在画布中心
  const o = worldToScreen(0, 0, { u: 0, v: 0 }, mapPx, w, h)
  if (!near(o.x, 256) || !near(o.y, 256)) fail(`focus=(0,0) 时世界原点应落在画布中心,实得 (${o.x},${o.y})`)
  else ok('focus 在原点时原点落在画布中心')

  // 相对关系:两点屏幕距离 = 世界距离 × mapPx(缩放的线性性)
  const p1 = worldToScreen(0.2, 0.3, { u: 0.5, v: 0.5 }, mapPx, w, h)
  const p2 = worldToScreen(0.4, 0.3, { u: 0.5, v: 0.5 }, mapPx, w, h)
  if (!near(p2.x - p1.x, 0.2 * mapPx, 1e-6)) fail(`世界距离 0.2 在屏幕上应为 ${0.2 * mapPx}px,实得 ${p2.x - p1.x}`)
  else ok('世界距离与屏幕距离成线性(mapPx)')

  // 非正方形画布:仍以短边定 mapPx,但居中用各自宽高
  const n = worldToScreen(0.5, 0.5, { u: 0.5, v: 0.5 }, mapPxOf(800, 400, 2), 800, 400)
  if (!near(n.x, 400) || !near(n.y, 200)) fail(`非正方形画布中心应为 (400,200),实得 (${n.x},${n.y})`)
  else ok('非正方形画布按各自宽高居中')

  // 玩家在 focus 右下方:屏幕坐标应在中心右下
  const d = worldToScreen(0.6, 0.7, { u: 0.5, v: 0.5 }, mapPx, w, h)
  if (!(d.x > 256 && d.y > 256)) fail(`u/v 大于 focus 时应画在中心右下,实得 (${d.x},${d.y})`)
  else ok('方向正确(u/v 增大 → 屏幕右下)')
}

// —— 2) 视口裁剪 ——
{
  const w = 512, h = 512
  if (!inView(256, 256, w, h)) fail('画布正中的点应判定可见')
  else if (inView(-100, 256, w, h)) fail('画布左侧 100px 外的点(超出默认 pad 40)应判定不可见')
  else if (inView(256, 600, w, h)) fail('画布下方 88px 外的点应判定不可见')
  else ok('inView 基本判定正确')

  // pad 必须真的生效:标记是「以锚点为中心」画的,擦边的标记留半张脸也要画
  if (!inView(-30, 256, w, h)) fail('pad=40 时,左侧 30px 外的点(标记半张脸)应判定可见')
  else ok('inView 的 pad 容差生效(擦边标记不被裁)')

  if (inView(-41, 0, w, h)) fail('刚超出 pad 应不可见')
  else ok('inView 边界外即不可见')

  // 自定义 pad
  if (!inView(-80, 0, w, h, 100)) fail('pad=100 时左侧 80px 应可见')
  else ok('inView 支持自定义 pad')
}

// —— 3) 内容签名:内容变了必须变,没变必须不变 ——
{
  const base = {
    sceneImg: '10003', layerImg: '', w: 512, h: 512, zoom: 5,
    player: { u: 0.5, v: 0.5, heading: 90 },
    paintVer: 3,
    sigMarks: [[], [], [], []],
  }
  const s0 = frameSig(base)
  if (typeof s0 !== 'string' || !s0.length) fail('frameSig 应返回非空字符串')
  else ok('frameSig 返回可比较的字符串')

  // 同一份输入两次调用必须相同(WeakMap 的 refId 分配必须稳定)
  if (frameSig(base) !== s0) fail('同一份输入两次 frameSig 结果不同 —— refId 不稳定会导致每帧都白重画')
  else ok('frameSig 对同一输入稳定(不会每帧误判为变化)')

  const marks = base.sigMarks
  const withSameMarks = { ...base, sigMarks: marks }
  if (frameSig(withSameMarks) !== s0) fail('marks 数组引用未变时签名不应变')
  else ok('marks 引用不变 → 签名不变(静止时不重画)')

  // 引用变了(React 重算了 marks)必须变
  if (frameSig({ ...base, sigMarks: [[], [], [], []] }) === s0) fail('marks 换成新数组后签名应变化,否则新数据不会画出来')
  else ok('marks 引用变化 → 签名变化(新数据会重画)')

  // 玩家位置变了必须变
  if (frameSig({ ...base, player: { u: 0.51, v: 0.5, heading: 90 } }) === s0) fail('玩家移动后签名应变化')
  else ok('玩家移动 → 签名变化')

  // 玩家消失/出现必须变
  if (frameSig({ ...base, player: null }) === s0) fail('player 变 null 后签名应变化')
  else ok('玩家位置丢失 → 签名变化')

  // 场景/层图切换必须变
  if (frameSig({ ...base, sceneImg: '10018' }) === s0) fail('换场景后签名应变化')
  else if (frameSig({ ...base, layerImg: 'cave1' }) === s0) fail('换洞穴层后签名应变化')
  else ok('换场景/换层 → 签名变化')

  // 缩放变化必须变
  if (frameSig({ ...base, zoom: 8 }) === s0) fail('缩放变化后签名应变化')
  else ok('缩放变化 → 签名变化')

  // 涂地版本变化必须变(涂地增量只改 ref,靠 ver 判定)
  if (frameSig({ ...base, paintVer: 4 }) === s0) fail('涂地版本变化后签名应变化')
  else ok('涂地版本变化 → 签名变化')

  // 微小移动(0.000001,约 0.004 底图像素)不应触发重画 —— 量化精度的意义
  if (frameSig({ ...base, player: { u: 0.500001, v: 0.5, heading: 90 } }) !== s0) {
    fail('亚量化精度的抖动不应触发重画(否则玩家静止时也在白画)')
  } else ok('亚量化抖动被吸收(静止时零重绘)')

  // 朝向微调 1° 不应触发重画(量化到 3°)
  if (frameSig({ ...base, player: { u: 0.5, v: 0.5, heading: 91 } }) !== s0) {
    fail('朝向变化 1° 不应触发重画')
  } else ok('朝向微调 1° 被吸收')

  // 朝向变化 3° 应触发重画
  if (frameSig({ ...base, player: { u: 0.5, v: 0.5, heading: 93 } }) === s0) {
    fail('朝向变化 3° 应触发重画,否则箭头会卡住不转')
  } else ok('朝向变化 3° → 重画')

  // 无底图场景的实时坐标必须参与签名,否则小窗会停在旧坐标不动
  if (frameSig({ ...base, nomap: { name: 'x', x: 1, y: 2, z: 3 } }) === s0) {
    fail('无底图场景的坐标应参与签名(否则小窗坐标不刷新)')
  } else ok('无底图坐标变化 → 重画')

  const withCoord = { ...base, nomap: { name: 'x', x: 1, y: 2, z: 3 } }
  if (frameSig({ ...withCoord, nomap: { name: 'x', x: 1, y: 5, z: 3 } }) === frameSig(withCoord)) {
    fail('仅坐标变化时签名应变化')
  } else ok('坐标变化 → 签名变化')

  // 缺字段不应崩
  if (typeof frameSig({}) !== 'string') fail('frameSig({}) 不应抛错')
  else ok('frameSig 对空输入健壮')
}

// —— 4) 涂地边界解析(只认 M / H / V)——
{
  // 与 usePaint.edgePath 的实际产出格式一致:每个片段是「M x y」紧跟一条 H 或 V,
  // 故 3 个片段 = 6 条命令(见 usePaint.js:42,54)。
  const path = 'M0 0H5M3 1V4M10 10H20'
  const segs = parseEdge(path)
  if (segs.length !== 6) fail(`parseEdge 应解析出 6 条命令,实得 ${segs.length}`)
  else if (segs[0].type !== 'M' || segs[0].x !== 0 || segs[0].y !== 0) fail('第 1 条应为 M 0 0')
  else if (segs[1].type !== 'H' || segs[1].x !== 5) fail(`第 1 段的 H 应为 x=5,实得 ${JSON.stringify(segs[1])}`)
  else if (segs[2].type !== 'M' || segs[2].x !== 3 || segs[2].y !== 1) fail('第 2 段应为 M 3 1')
  else if (segs[3].type !== 'V' || segs[3].y !== 4) fail(`第 2 段的 V 应为 y=4,实得 ${JSON.stringify(segs[3])}`)
  else if (segs[4].type !== 'M' || segs[4].x !== 10 || segs[4].y !== 10) fail('第 3 段应为 M 10 10')
  else if (segs[5].type !== 'H' || segs[5].x !== 20) fail(`第 3 段的 H 应为 x=20,实得 ${JSON.stringify(segs[5])}`)
  else ok('parseEdge 正确解析 M/H/V 三种命令')

  // H/V 必须补齐缺失的那一维(取自所属 M),否则绘制时无法直接换算成屏幕坐标
  if (!near(segs[1].y, 0)) fail(`H 段的 y 应取自所属 M 的 y(0),实得 ${segs[1].y}`)
  else if (!near(segs[3].x, 3)) fail(`V 段的 x 应取自所属 M 的 x(3),实得 ${segs[3].x}`)
  else if (!near(segs[5].y, 10)) fail(`H 段的 y 应取自所属 M 的 y(10),实得 ${segs[5].y}`)
  else ok('H/V 段补齐所属 M 的另一维坐标')

  // M 后面必须紧跟 H 或 V(edgePath 的产出形态),顺序不能乱
  const seq = segs.map((s) => s.type).join('')
  if (seq !== 'MHMVMH') fail(`命令序列应为 MHMVMH,实得 ${seq}`)
  else ok('命令序列 = M 后紧跟 H/V')

  if (parseEdge('').length !== 0) fail('空路径应返回空数组')
  else if (parseEdge(null).length !== 0) fail('null 路径应返回空数组')
  else ok('空路径健壮')

  // —— edgeToScreen:格子坐标 → 屏幕坐标 ——
  // 涂地铺满整张底图:格子 (0,0) = 底图左上角,格子 (gw,gh) = 底图右下角。
  const left = 100, top = 50, mapPx = 1000, gw = 500, gh = 500
  const p0 = edgeToScreen({ type: 'M', x: 0, y: 0 }, left, top, mapPx, gw, gh)
  if (!near(p0.x, left) || !near(p0.y, top)) fail(`格子原点应画在世界原点 (${left},${top}),实得 (${p0.x},${p0.y})`)
  else ok('edgeToScreen:格子原点对齐世界原点')

  const pMax = edgeToScreen({ type: 'M', x: gw, y: gh }, left, top, mapPx, gw, gh)
  if (!near(pMax.x, left + mapPx) || !near(pMax.y, top + mapPx)) fail(`格子 (gw,gh) 应画在底图右下角 (${left + mapPx},${top + mapPx})`)
  else ok('edgeToScreen:格子满值对齐底图右下角')

  const pHalf = edgeToScreen({ type: 'M', x: gw / 2, y: gh / 2 }, left, top, mapPx, gw, gh)
  if (!near(pHalf.x, left + mapPx / 2)) fail(`格子中线应落在底图中线,实得 x=${pHalf.x}`)
  else ok('edgeToScreen:线性映射正确')

  // 非方形格子图(不同场景格数不同)仍按各自格数归一化
  const pRect = edgeToScreen({ type: 'M', x: 100, y: 100 }, 0, 0, 1000, 200, 400)
  if (!near(pRect.x, 500) || !near(pRect.y, 250)) fail(`非方形格子图应按各自格数归一化,实得 (${pRect.x},${pRect.y})`)
  else ok('edgeToScreen:非方形格数按各自归一化')

  // 格数为 0(无涂地数据)不应崩
  if (!Number.isFinite(edgeToScreen({ type: 'M', x: 1, y: 1 }, 0, 0, 1000, 0, 0).x)) {
    fail('格数为 0 时 edgeToScreen 应返回有限值(除零兜底)')
  } else ok('edgeToScreen:格数为 0 有兜底')
}

// —— 5) 标记缩放(小窗与主地图像素密度对齐)——
{
  // 视口短边与小窗同为 512 → 密度相同 → 不缩放
  if (markerScale(512, 512) !== 1) fail(`视口与小窗同尺寸时应为 1,实得 ${markerScale(512, 512)}`)
  else ok('markerScale:同密度 → 1(不缩放)')

  // 视口比小窗大一倍 → 小窗里标记应减半,才能覆盖同样的地图范围
  if (!near(markerScale(1024, 512), 0.5)) fail(`视口 1024 / 小窗 512 应为 0.5,实得 ${markerScale(1024, 512)}`)
  else ok('markerScale:视口大一倍 → 标记减半')

  // 视口比小窗小 → 标记放大(手机窄视口)
  if (!near(markerScale(341, 512), 1.5, 1e-9)) fail(`视口 341 / 小窗 512 应夹到上限 1.5,实得 ${markerScale(341, 512)}`)
  else ok('markerScale:窄视口 → 放大并夹到上限 1.5')

  // 4K 屏:视口短边 2160 时照算只剩 0.237,必须被下限兜住,否则小窗里标记看不见
  if (!near(markerScale(2160, 512), 0.5, 1e-9)) fail(`超大视口应夹到下限 0.5,实得 ${markerScale(2160, 512)}`)
  else ok('markerScale:超大视口 → 夹到下限 0.5(保可读性)')

  // 视口还没测量(0/NaN)→ 回退 1,不缩放
  if (markerScale(0, 512) !== 1) fail('视口未测量(0)应回退 1')
  else if (markerScale(NaN, 512) !== 1) fail('视口 NaN 应回退 1')
  else ok('markerScale:视口未测量 → 回退 1')

  // —— 核心性质:标记覆盖的「地图归一化范围」在主地图与小窗里必须相等 ——
  // 主地图: 30 / (vpShort × zoom);小窗: (30 × s) / (512 × zoom)
  const zoom = 5
  for (const vpShort of [600, 900, 1200]) {
    const s = markerScale(vpShort, 512)
    const mainCover = 30 / (vpShort * zoom)
    const pipCover = (30 * s) / (512 * zoom)
    // 被 [0.5,1.5] 夹过的档位允许偏差,未夹的必须严格相等
    const clamped = s <= 0.5 || s >= 1.5
    if (!clamped && !near(mainCover, pipCover, 1e-12)) {
      fail(`视口 ${vpShort}:标记覆盖的地图范围应与主地图一致(主 ${mainCover} vs 小窗 ${pipCover})`)
    }
  }
  ok('markerScale:标记覆盖的地图范围与主地图一致(密度对齐)')
}

// —— 6) 常量与 map.css / useRoutes 对齐(改了那边忘了这边会直接看出来)——
{
  if (ROUTE_GRID !== 8192) fail(`ROUTE_GRID 应与 useRoutes.jsx 的 GRID 一致(8192),实得 ${ROUTE_GRID}`)
  else ok('ROUTE_GRID 与 useRoutes 一致')
  if (ROUTE_TELEPORT !== 300) fail(`ROUTE_TELEPORT 应与 useRoutes.jsx 的 TELEPORT 一致(300),实得 ${ROUTE_TELEPORT}`)
  else ok('ROUTE_TELEPORT 与 useRoutes 一致')

  // 尺寸层级:稀有宠必须比普通宠大(小窗里靠大小分稀有度),普通野生必须最小
  if (!(SIZES.wildRare > SIZES.wild)) fail('稀有宠标记应大于普通稀有宠')
  else if (!(SIZES.wild > SIZES.wildAll)) fail('普通稀有宠应大于「全部野生」的降级标记')
  else ok('标记尺寸层级正确(稀有 > 普通 > 全部野生)')
  if (!(SIZES.nest > SIZES.nestEmpty)) fail('有住户的小窝应大于空窝')
  else ok('小窝尺寸层级正确(有住户 > 空窝)')
  if (!(SIZES.poi > 0 && SIZES.arrow > 0)) fail('POI 与箭头尺寸应为正数')
  else ok('POI/箭头尺寸为正')
}

console.log(`\n用完 ${DIR} 下的 pipGeom.js`)
if (bad) {
  console.log(`\n❌ ${bad} 项校验未通过`)
  process.exit(1)
}
console.log('\n✅ 画中画几何校验全部通过')
