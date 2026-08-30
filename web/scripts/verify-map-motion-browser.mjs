// 地图箭头移动平滑度：真浏览器逐帧验收。
//
//   npm run build && 启动后端(需 -pcap 回放,让 REST 侧有数据)
//   node scripts/verify-map-motion-browser.mjs [fixture 名]
//
// 存在理由:verify-motion.mjs 是**仿真** —— 它在 Node 里接管 performance.now(),
// 按固定 1/60s 步进驱动 motion.js。那验证的是算法,不是浏览器里的真实表现:
// 真实 RAF 间隔会抖动(掉帧、后台节流),DOM 写入与合成也有开销,这些仿真都看不到。
// 本脚本在真浏览器里用 RAF 逐帧抓 .map-arrow 实际写入的 transform,量真实帧间隔与
// 真实每帧位移。
//
// **为什么必须自己喂数据**:后端 -pcap 回放是「尽快」模式(见 capture.RunOffline),
// 268s 的包几秒就放完,前端会在一瞬间收到全部位置 —— 测不出真实节奏。故这里 stub
// 掉 EventSource,把 fixture 整份传进页面,由**页面内部**按 1x 节拍发射 ——
// 时序全在浏览器里,不经 Node↔浏览器往返(那有毫秒级抖动,会污染测量)。
//
// 测什么,与 verify-motion.mjs 的分工(**谁也替不了谁**):
//   - 本脚本 = **平滑度**:帧间隔、每帧位移、静止期抖动。这些只有真浏览器测得到。
//   - verify-motion.mjs = **准确度**:箭头离真实轨迹多远。仿真足够,且能按 p95/p99 细看。
//   实测印证了这个划分:把外推退回成「全时程衰减」(P0 旧版)时,本脚本 10/10 通过
//   —— 它确实平滑(每帧最大 2.6m),只是**不准**(偏差 4.2m vs 2.6m,由那边抓到)。
//
// 具体指标:
//   - 帧间隔:RAF 是否真的 ~16.7ms(掉帧会直接表现为卡顿)
//   - 每帧位移:分布与**绝对上限**(单次大跳 = 抽搐/瞬移)
//   - 静止期抖动:玩家不动时地图是否在抖(snap 的取整边界问题会在这里暴露)
//
// **量 .map-world 而不是 .map-arrow**:跟随模式(默认)下 focus 跟着玩家走,箭头被钉在
// 视口正中几乎不动,真正滚动的是地图 —— 用户看到的「移动」就是它。只量箭头会得到
// 「每帧位移 0」的假象(我第一版就这么写错了)。

import fs from 'node:fs'
import { chromium } from 'playwright'

const FIXTURE = process.argv[2] || 'tap-move'
const BASE = process.env.E2E_BASE || 'http://localhost:4939'
const SPEED = Number(process.env.E2E_SPEED || 1) // 1 = 实时;非 1 用于快速试跑

const raw = JSON.parse(fs.readFileSync(`scripts/fixtures/${FIXTURE}.json`, 'utf8'))
const SIDE = raw.side
const pkts = raw.pkts
const DURATION = pkts[pkts.length - 1].t

const accounts = await fetch(BASE + '/api/accounts').then((r) => r.json())
if (!accounts.length) {
  console.error('后端无账号数据,先带 -pcap 启动后端')
  process.exit(1)
}
const ACCOUNT = accounts[0].account

// 把 fixture 的一条转成后端推送的 PositionPayload 形状(字段名见 internal/server/payload.go)。
// x/y/z 只用于无底图场景的坐标展示,箭头定位用 u/v,故这里置 0 不影响测量。
const toPayload = (p) => {
  const o = {
    account: ACCOUNT,
    sceneResId: raw.res,
    sceneCfgId: 0,
    sceneName: '卡洛西亚大陆',
    img: String(raw.res),
    x: 0, y: 0, z: 0,
    heading: 0,
    stop: !!p.stop,
    paintable: false,
    ts: 0,
    tsMs: Date.now(), // 前端只在 GET 快照时判过期,SSE 路径不读,给当前时刻即可
    u: p.u, v: p.v,
  }
  if (!p.stop && (p.vu || p.vv)) { o.vu = p.vu; o.vv = p.vv }
  if (p.path && p.path.length >= 2) o.path = p.path.map((q) => ({ u: q.u, v: q.v }))
  return o
}

const results = []
const check = (name, ok, detail) => {
  results.push({ name, ok })
  console.log(`${ok ? '  ok  ' : ' FAIL '} ${name}${detail ? '  — ' + detail : ''}`)
}

const browser = await chromium.launch({ args: ['--no-sandbox', '--disable-dev-shm-usage'] })
const page = await browser.newPage({ viewport: { width: 1280, height: 800 } })
const errors = []
page.on('pageerror', (e) => errors.push(String(e).split('\n')[0]))
page.on('console', (m) => { if (m.type() === 'error') errors.push(m.text().slice(0, 200)) })

// EventSource 换成可控桩。整份数据一次传进去,由页面内部按 t 精确发射。
await page.addInitScript(() => {
  window.__ACC = null
  window.__inst = null
  window.EventSource = class {
    constructor(url) {
      this.url = url
      this.onmessage = null; this.onopen = null; this.onerror = null
      window.__inst = this
      setTimeout(() => this.onopen?.({}), 0)
    }
    close() { if (window.__inst === this) window.__inst = null }
    addEventListener() {} removeEventListener() {}
  }
  window.__emit = (type, data) => {
    const es = window.__inst
    if (!es || !es.onmessage) return false
    es.onmessage({ data: JSON.stringify({ type, account: window.__ACC, data }) })
    return true
  }
  // 逐帧记录实际写入的 transform。
  // **量 .map-world 而不是 .map-arrow**:跟随模式(默认)下 focus 跟着玩家走,
  // 箭头被钉在视口正中几乎不动,真正滚动的是地图本身 —— 用户看到的「移动」就是它。
  // 只量箭头会得到「每帧位移 0」的假象。两者都记,便于对照。
  window.__cap = []
  window.__startCap = () => {
    window.__cap = []
    const tick = (now) => {
      const w = document.querySelector('.map-world')
      const a = document.querySelector('.map-arrow')
      if (w) window.__cap.push({ t: now, tr: w.style.transform, arr: a ? a.style.transform : '' })
      window.__raf = requestAnimationFrame(tick)
    }
    window.__raf = requestAnimationFrame(tick)
  }
  window.__stopCap = () => { cancelAnimationFrame(window.__raf); return window.__cap }
  // 按 1x 节拍发射:全部用页面内的 setTimeout 排队,不经 Node 往返
  window.__feed = (list, speed) => {
    window.__drop = 0
    for (const it of list) {
      const payload = typeof it.data === 'string' ? JSON.parse(it.data) : it.data
      setTimeout(() => { if (!window.__emit('position', payload)) window.__drop++ }, (it.t * 1000) / speed)
    }
  }
})

// 初始快照也用 fixture 的第一条,免得后端缓存的「最后一包」把起点带偏
const first = toPayload(pkts[0])
await page.route('**/api/position**', (route) =>
  route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(first) }))

try {
  await page.goto(BASE + '/#/map', { waitUntil: 'domcontentloaded' })
  await page.waitForSelector('.map-arrow', { timeout: 20000 })
  await page.evaluate((a) => { window.__ACC = a }, ACCOUNT)
  await page.waitForTimeout(500) // 让初始快照落地、场景稳定

  const geo = await page.evaluate(() => {
    const w = document.querySelector('.map-world')
    return { mapPx: parseFloat(w.style.width) || 0 }
  })
  check('底图边长已就绪', geo.mapPx > 1000, `mapPx=${Math.round(geo.mapPx)}px`)

  await page.evaluate(() => window.__startCap())
  await page.evaluate((arg) => window.__feed(arg.list, arg.speed), {
    list: pkts.map((p) => ({ t: p.t, data: JSON.stringify(toPayload(p)) })),
    speed: SPEED,
  })

  const waitMs = (DURATION / SPEED + 3) * 1000
  console.log(`  … 回放 ${DURATION.toFixed(1)}s(${SPEED}x),等待 ${(waitMs / 1000).toFixed(0)}s`)
  await page.waitForTimeout(waitMs)

  const cap = await page.evaluate(() => window.__stopCap())
  const dropped = await page.evaluate(() => window.__drop)

  check('抓到帧', cap.length > 100, `${cap.length} 帧`)
  check('所有位置包都送到了前端', dropped === 0, dropped ? `丢失 ${dropped} 条` : '')

  // 解析 transform:translate3d(Xpx, Ypx, 0) ...
  const parse = (key) => {
    const out = []
    for (const c of cap) {
      const m = /translate3d\(([-\d.]+)px,\s*([-\d.]+)px/.exec(c[key] || '')
      if (m) out.push({ t: c.t, x: parseFloat(m[1]), y: parseFloat(m[2]) })
    }
    return out
  }
  const pts = parse('tr')     // 地图(跟随模式下真正滚动的元素)
  const arr = parse('arr')    // 箭头(跟随模式下应基本不动)
  check('transform 可解析', pts.length === cap.length && pts.length > 100, `${pts.length}/${cap.length}`)

  const dts = []
  const moves = []
  for (let i = 1; i < pts.length; i++) {
    dts.push(pts[i].t - pts[i - 1].t)
    moves.push(Math.hypot(pts[i].x - pts[i - 1].x, pts[i].y - pts[i - 1].y))
  }
  const q = (xs, p) => { const s = [...xs].sort((a, b) => a - b); return s[Math.min(s.length - 1, Math.floor(p * s.length))] }
  const mean = (xs) => xs.reduce((a, b) => a + b, 0) / xs.length

  // px → 米:mapPx 像素对应 SIDE 厘米
  const pxToM = SIDE / 100 / geo.mapPx
  const movesM = moves.map((m) => m * pxToM)

  console.log(`\n  帧间隔  中位 ${q(dts, .5).toFixed(1)}ms  p95 ${q(dts, .95).toFixed(1)}ms  p99 ${q(dts, .99).toFixed(1)}ms  最大 ${Math.max(...dts).toFixed(0)}ms`)
  console.log(`  每帧位移 中位 ${(q(movesM, .5)).toFixed(2)}m  p95 ${q(movesM, .95).toFixed(2)}m  p99 ${q(movesM, .99).toFixed(2)}m  最大 ${Math.max(...movesM).toFixed(2)}m`)
  console.log(`  等效帧率 ${(1000 / mean(dts)).toFixed(1)} fps`)

  // —— 断言 1:帧率。掉帧严重会直接表现为卡顿 ——
  const fps = 1000 / mean(dts)
  check('帧率接近 60fps', fps > 50, `${fps.toFixed(1)} fps`)

  // —— 断言 2:无瞬移。要**同时**看绝对上限与比例 —— 只查比例会漏。
  //
  // 实测教训:只断言「>2m 的帧占比 <0.5%」时,把修复退回成硬截断(原实现)竟然 9/9 通过
  // —— 21m 的那一次跳变只占 1/5015,占比 0.02% 远低于阈值。可那正是用户报的「乱动」
  // (21m ≈ 18px,肉眼可见的瞬移)。故必须再卡绝对上限。
  //
  // 阈值 6m 的来路:本场景真实峰值约 30m/s,60fps 下理想单帧步长 0.5m;误差修正的峰值
  // 速度是实际速度的 3 倍(见 motion.js 的 TAU_RATIO),即 1.5m/帧,再容一次掉帧 → 约 3m。
  // 正常实测 2.6m,给到 6m 是宽裕的;而硬截断的 21m 远超它。
  const maxMove = Math.max(...movesM)
  check('无瞬移(单帧位移 <6m)', maxMove < 6, `最大 ${maxMove.toFixed(2)}m`)
  const big = movesM.filter((m) => m > 2).length
  check('大位移帧占比 <0.5%', big / movesM.length < 0.005, `${big}/${movesM.length} 帧`)

  // —— 断言 3:静止不抖。取位移最小的 20% 帧(玩家基本不动时),若它们的位移
  //     显著 >0 说明在原地抖(snap 取整边界反复跳会在这里暴露)——
  const still = movesM.slice().sort((a, b) => a - b).slice(0, Math.floor(movesM.length * 0.2))
  check('静止期无亚像素抖动', mean(still) < 0.05, `最安静 20% 帧的平均位移 ${mean(still).toFixed(3)}m`)

  // —— 断言 4:跟随模式下箭头应稳在视口中心(若它在大幅移动,说明焦点没跟上)——
  const arrMoves = []
  for (let i = 1; i < arr.length; i++) {
    arrMoves.push(Math.hypot(arr[i].x - arr[i - 1].x, arr[i].y - arr[i - 1].y))
  }
  const arrMax = arrMoves.length ? Math.max(...arrMoves) : 0
  check('跟随模式下箭头稳定居中', arrMax < 60, `箭头单帧最大位移 ${arrMax.toFixed(1)}px`)

  check('页面无 JS 错误', errors.length === 0, errors.slice(0, 2).join(' | '))
} catch (e) {
  check('执行完成', false, String(e).split('\n')[0])
} finally {
  await browser.close()
}

const bad = results.filter((r) => !r.ok)
console.log(`\n${results.length - bad.length}/${results.length} 项通过`)
process.exit(bad.length ? 1 : 0)
