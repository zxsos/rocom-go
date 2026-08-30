// 生成地图箭头「准确度」的可视化报告(真浏览器实测)。
//
//   npm run build && 启动后端(需 -pcap 回放)
//   node scripts/motion-report.mjs [fixture] [输出.html]
//
// 与另两个脚本的分工:
//   - verify-motion.mjs          : **准确度**(仿真)—— 偏差数字,能按 p95/p99 细看
//   - verify-map-motion-browser.mjs : **平滑度**(真浏览器)—— 帧率、抖动、瞬移
//   - 本脚本                      : **准确度**(真浏览器)+ 可视化 —— 把偏差摊到时间线上看
//
// 为什么还要在浏览器里再测一次准确度:仿真里时钟是我接管的理想 1/60s 步进,真实浏览器
// 有掉帧与写入开销。这里从**实际写入 DOM 的 transform** 反推出箭头位置,是最贴近
// 用户所见的一手数据。
//
// 反推方法:applyFrame 写的是
//     world: translate3d(left, top, 0),  left = snap(vw/2 - focus.u * px)
//     arrow: translate3d(ax,   ay,  0),  ax   = snap(left + u * px)
// 故 **u = (ax - left) / px**,且这个式子与 follow 状态无关(ax 本身就含 left)。
// 代价是 snap 的 ±0.5px 量化(两个值各一次,合 ±1px):px 越大误差越小,所以先把地图
// 放大到 ZOOM_MAX(32)再测 —— 707px 视口下 px≈22624,量化误差 ±0.09m;
// 若用默认 5 倍则 px≈3535,误差 ±0.58m,相对 7m 的平均偏差就太吵了。

import fs from 'node:fs'
import { chromium } from 'playwright'

const FIXTURE = process.argv[2] || 'tap-move'
const OUT = process.argv[3] || `/tmp/rocom-motion-${FIXTURE}.html`
const BASE = process.env.E2E_BASE || 'http://localhost:4939'

const raw = JSON.parse(fs.readFileSync(`scripts/fixtures/${FIXTURE}.json`, 'utf8'))
const SIDE = raw.side
const pkts = raw.pkts
const DURATION = pkts[pkts.length - 1].t

// —— 真值:包位置 + 补报轨迹点,按时间线性插值(与 verify-motion.mjs 同一套)——
function buildTruth() {
  const pts = []
  for (const p of pkts) {
    if (p.path && p.path.length >= 2) {
      let lastT = -Infinity
      for (const q of p.path) {
        const t = Math.max(q.t, lastT + 1e-3)
        pts.push({ t, u: q.u, v: q.v })
        lastT = t
      }
      pts.push({ t: Math.max(p.t, lastT + 0.05), u: p.u, v: p.v })
    } else {
      pts.push({ t: p.t, u: p.u, v: p.v })
    }
  }
  pts.sort((a, b) => a.t - b.t)
  for (let i = 1; i < pts.length; i++) if (pts[i].t <= pts[i - 1].t) pts[i].t = pts[i - 1].t + 1e-3
  let i = 0
  return (t) => {
    if (t <= pts[0].t) return { u: pts[0].u, v: pts[0].v }
    if (t >= pts[pts.length - 1].t) { const l = pts[pts.length - 1]; return { u: l.u, v: l.v } }
    while (i < pts.length - 2 && pts[i + 1].t < t) i++
    while (i > 0 && pts[i].t > t) i--
    const a = pts[i], b = pts[i + 1]
    const f = (t - a.t) / (b.t - a.t)
    return { u: a.u + (b.u - a.u) * f, v: a.v + (b.v - a.v) * f }
  }
}
const truthAt = buildTruth()

const accounts = await fetch(BASE + '/api/accounts').then((r) => r.json())
if (!accounts.length) { console.error('后端无账号数据'); process.exit(1) }
const ACCOUNT = accounts[0].account

const toPayload = (p) => {
  const o = {
    account: ACCOUNT, sceneResId: raw.res, sceneCfgId: 0, sceneName: '卡洛西亚大陆',
    img: String(raw.res), x: 0, y: 0, z: 0, heading: 0, stop: !!p.stop,
    paintable: false, ts: 0, tsMs: Date.now(), u: p.u, v: p.v,
  }
  if (!p.stop && (p.vu || p.vv)) { o.vu = p.vu; o.vv = p.vv }
  if (p.path && p.path.length >= 2) o.path = p.path.map((q) => ({ u: q.u, v: q.v }))
  return o
}

const browser = await chromium.launch({ args: ['--no-sandbox', '--disable-dev-shm-usage'] })
const page = await browser.newPage({ viewport: { width: 1280, height: 800 } })

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
  window.__cap = []
  window.__startCap = () => {
    window.__cap = []
    const tick = (now) => {
      const w = document.querySelector('.map-world')
      const a = document.querySelector('.map-arrow')
      if (w && a) window.__cap.push({ t: now, w: w.style.transform, a: a.style.transform })
      window.__raf = requestAnimationFrame(tick)
    }
    window.__raf = requestAnimationFrame(tick)
  }
  window.__stopCap = () => { cancelAnimationFrame(window.__raf); return window.__cap }
  window.__feed = (list) => {
    window.__drop = 0
    for (const it of list) {
      const payload = typeof it.data === 'string' ? JSON.parse(it.data) : it.data
      setTimeout(() => { if (!window.__emit('position', payload)) window.__drop++ }, it.t * 1000)
    }
  }
})

const first = toPayload(pkts[0])
await page.route('**/api/position**', (r) =>
  r.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(first) }))

console.log(`打开地图页(${FIXTURE})…`)
await page.goto(BASE + '/#/map', { waitUntil: 'domcontentloaded' })
await page.waitForSelector('.map-arrow', { timeout: 20000 })
await page.evaluate((a) => { window.__ACC = a }, ACCOUNT)
await page.waitForTimeout(500)

// 放大到最大:降低 snap 量化误差(见文件头)
for (let i = 0; i < 8; i++) await page.click('.map-ctrl .map-btn:nth-child(2)').catch(() => {})
await page.waitForTimeout(300)

const geo = await page.evaluate(() => {
  const w = document.querySelector('.map-world')
  const vp = document.querySelector('.map-vp')
  return {
    mapPx: parseFloat(w.style.width) || 0,
    vw: vp.clientWidth, vh: vp.clientHeight,
  }
})
if (!geo.mapPx) { console.error('拿不到 mapPx'); await browser.close(); process.exit(1) }
const quantM = (SIDE / 100 / geo.mapPx).toFixed(3)
console.log(`  放大后 mapPx=${Math.round(geo.mapPx)}px,量化误差 ±${quantM}m`)

await page.evaluate(() => window.__startCap())
await page.evaluate((list) => window.__feed(list),
  pkts.map((p) => ({ t: p.t, data: JSON.stringify(toPayload(p)) })))
console.log(`  回放 ${DURATION.toFixed(1)}s…`)
await page.waitForTimeout((DURATION + 3) * 1000)
const cap = await page.evaluate(() => window.__stopCap())
const dropped = await page.evaluate(() => window.__drop)
await browser.close()

// —— 反推 disp,与真值比对 ——
const num = (s) => parseFloat(s)
const samples = []
for (const c of cap) {
  const mw = /translate3d\(([-\d.]+)px,\s*([-\d.]+)px/.exec(c.w)
  const ma = /translate3d\(([-\d.]+)px,\s*([-\d.]+)px/.exec(c.a)
  if (!mw || !ma) continue
  const left = num(mw[1]), top = num(mw[2])
  const ax = num(ma[1]), ay = num(ma[2])
  samples.push({ t: c.t, u: (ax - left) / geo.mapPx, v: (ay - top) / geo.mapPx })
}
if (samples.length < 50) { console.error('帧太少'); process.exit(1) }

const t0 = samples[0].t
const errs = []
for (const s of samples) {
  const ts = (s.t - t0) / 1000
  const T = truthAt(ts)
  errs.push({
    ts,
    err: Math.hypot(s.u - T.u, s.v - T.v) * SIDE / 100,
    du: s.u, dv: s.v, tu: T.u, tv: T.v,
  })
}
// 时间戳可能比 fixture 略长(启动开销),裁掉尾部越界的部分
const valid = errs.filter((e) => e.ts >= 0 && e.ts <= DURATION + 0.5)

const errVals = valid.map((e) => e.err)
const q = (xs, p) => { const s = [...xs].sort((a, b) => a - b); return s[Math.min(s.length - 1, Math.floor(p * s.length))] }
const stat = {
  mean: errVals.reduce((a, b) => a + b, 0) / errVals.length,
  p50: q(errVals, .5), p95: q(errVals, .95), p99: q(errVals, .99), max: Math.max(...errVals),
}
console.log(`\n  偏差 均值 ${stat.mean.toFixed(2)}m  p50 ${stat.p50.toFixed(2)}  p95 ${stat.p95.toFixed(2)}  p99 ${stat.p99.toFixed(2)}  最大 ${stat.max.toFixed(2)}`)
console.log(`  帧数 ${valid.length},丢包 ${dropped}`)

// —— 渲染报告 ——
const W = 1100, H = 260
const pad = { l: 52, r: 16, t: 14, b: 26 }
const innerW = W - pad.l - pad.r
const innerH = H - pad.t - pad.b

// 均匀降采样到 ~1500 点,避免 SVG 过长
const step = Math.max(1, Math.floor(valid.length / 1500))
const draw = valid.filter((_, i) => i % step === 0)

const maxErr = Math.max(stat.p99 * 1.15, 0.5)
const X = (ts) => pad.l + (ts / DURATION) * innerW
const Y = (m) => pad.t + innerH - Math.min(m, maxErr) / maxErr * innerH

// 图1:偏差-时间曲线
const line = draw.map((e, i) => `${i ? 'L' : 'M'}${X(e.ts).toFixed(1)},${Y(e.err).toFixed(1)}`).join('')
// stop 时刻竖线
const stops = pkts.filter((p) => p.stop).map((p) => p.t)

let svg1 = `<svg viewBox="0 0 ${W} ${H}" width="${W}" height="${H}" style="font:11px system-ui">`
svg1 += `<rect x="${pad.l}" y="${pad.t}" width="${innerW}" height="${innerH}" fill="#fbfbfa" stroke="#e3e2df"/>`
for (const g of [0.25, 0.5, 0.75]) {
  const y = pad.t + innerH * g
  svg1 += `<line x1="${pad.l}" y1="${y}" x2="${W - pad.r}" y2="${y}" stroke="#eee"/>`
}
for (const s of stops) {
  svg1 += `<line x1="${X(s).toFixed(1)}" y1="${pad.t}" x2="${X(s).toFixed(1)}" y2="${pad.t + innerH}" stroke="#f0b429" stroke-width="1" opacity=".45"/>`
}
for (let i = 0; i <= 4; i++) {
  const m = maxErr * i / 4
  svg1 += `<text x="${pad.l - 6}" y="${Y(m) + 3.5}" text-anchor="end" fill="#888">${m.toFixed(1)}</text>`
}
svg1 += `<path d="${line}" fill="none" stroke="#c0392b" stroke-width="1.1"/>`
for (let i = 0; i <= 6; i++) {
  const ts = DURATION * i / 6
  svg1 += `<text x="${X(ts).toFixed(1)}" y="${H - 8}" text-anchor="middle" fill="#888">${ts.toFixed(0)}s</text>`
}
svg1 += `<text x="${pad.l}" y="${pad.t - 3}" fill="#666">偏差(米)·橙线=收到 stop 包的时刻</text></svg>`

// 图2:轨迹对比(真值 vs 箭头)
const TW = 520, TH = 520
const allU = draw.flatMap((e) => [e.du, e.tu])
const allV = draw.flatMap((e) => [e.dv, e.tv])
const uMin = Math.min(...allU), uMax = Math.max(...allU)
const vMin = Math.min(...allV), vMax = Math.max(...allV)
const span = Math.max(uMax - uMin, vMax - vMin, 1e-6) * 1.05
const uMid = (uMin + uMax) / 2, vMid = (vMin + vMax) / 2
const PX = (u) => (u - (uMid - span / 2)) / span * (TW - 40) + 20
const PY = (v) => (v - (vMid - span / 2)) / span * (TH - 40) + 20

const pathOf = (key) => draw.map((e, i) =>
  `${i ? 'L' : 'M'}${PX(e[key]).toFixed(1)},${PY(e[key.replace('u', 'v')]).toFixed(1)}`).join('')
let svg2 = `<svg viewBox="0 0 ${TW} ${TH}" width="${TW}" height="${TH}" style="font:11px system-ui">`
svg2 += `<rect width="${TW}" height="${TH}" fill="#fbfbfa" stroke="#e3e2df"/>`
svg2 += `<path d="${pathOf('tu')}" fill="none" stroke="#2c7a7b" stroke-width="1.6" opacity=".85"/>`
svg2 += `<path d="${pathOf('du')}" fill="none" stroke="#c0392b" stroke-width="1.2" opacity=".9"/>`
svg2 += `<circle cx="${PX(draw[0].du).toFixed(1)}" cy="${PY(draw[0].dv).toFixed(1)}" r="3" fill="#c0392b"/>`
svg2 += `<text x="10" y="16" fill="#2c7a7b">真实轨迹</text><text x="10" y="30" fill="#c0392b">箭头</text>`
svg2 += `<text x="10" y="${TH - 8}" fill="#999">跨度约 ${(span * SIDE / 100).toFixed(0)}m</text></svg>`

const html = `<!doctype html><meta charset="utf-8"><title>地图箭头准确度 · ${FIXTURE}</title>
<style>body{font:14px system-ui;margin:24px;color:#222;max-width:1140px}
table{border-collapse:collapse;margin:12px 0}td,th{border:1px solid #e3e2df;padding:5px 12px;text-align:right}
th{background:#f6f5f3}td:first-child,th:first-child{text-align:left}
.note{color:#777;font-size:12px;line-height:1.7}
h2{font-size:15px;margin:22px 0 6px}</style>
<h1>地图箭头准确度报告 · ${FIXTURE}</h1>
<p class="note">
真浏览器实测(Chromium headless · 60fps · RAF 逐帧抓取)。
数据来自<b>实际写入 DOM 的 transform</b>反推,最接近用户所见。<br>
fixture: ${pkts.length} 包 / ${DURATION.toFixed(1)}s · 有效帧 ${valid.length} · 丢包 ${dropped}<br>
放大倍数 ${(geo.mapPx / Math.min(geo.vw, geo.vh)).toFixed(1)}×(mapPx ${Math.round(geo.mapPx)}px),故 snap 量化误差约 <b>±${quantM}m</b>。<br>
每条 <code>stop</code> 竖线后客户端即沉默,直到下一个移动包(实测中位 1.8s)—— 那段没有任何
新信息进来,箭头只能等,这是上报节奏造成的下限,换任何画法都提前不了。
</p>
<h2>偏差统计</h2>
<table>
<tr><th>指标</th><th>均值</th><th>p50</th><th>p95</th><th>p99</th><th>最大</th></tr>
<tr><td>偏差(米)</td><td>${stat.mean.toFixed(2)}</td><td>${stat.p50.toFixed(2)}</td>
<td>${stat.p95.toFixed(2)}</td><td>${stat.p99.toFixed(2)}</td><td>${stat.max.toFixed(2)}</td></tr>
</table>
<h2>偏差随时间</h2>
${svg1}
<p class="note">
纵轴截断在 p99×1.15 = ${maxErr.toFixed(1)}m(峰值 ${stat.max.toFixed(1)}m 会顶到框顶)。
橙色竖线是服务器下发 <code>stop_move</code> 包的时刻 —— 玩家真正停下、客户端随即沉默的那一段。
看这些竖线<b>右侧</b>的曲线:若偏差冲高再回落,就是「停下后箭头还在动」;
若下一段起步时偏差持续偏高,就是「箭头没跟上」。
</p>
<h2>轨迹对比</h2>
${svg2}
<p class="note">
青色是真实轨迹,红色是箭头画出的轨迹。两者重合度越高越好。
注意这是<b>点触式走走停停</b>的数据时,短距离往返会让两条线交错 —— 那是正常的;
重点看箭头有没有系统性地「抄近路」或「拉长」。
</p>`

fs.writeFileSync(OUT, html)
console.log(`\n报告已写入: ${OUT}`)
