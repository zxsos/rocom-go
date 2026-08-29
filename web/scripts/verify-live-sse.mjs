// 验收 SSE 实时刷新的**完整链路**:真实事件 → 前端 subscribe 分发 → 页面更新。
//
// 背景:[A9] 把「SSE 实时刷新」标为未验,因为 pcap 回放一次性放完(约 40ms),
// 前端连上时事件早已结束。后端 B 在 [B10] 提供了 scripts/capture_sse.sh——
// 先挂 SSE 再启动回放,把真实推送落盘。本脚本消费那份捕获,喂给**真实前端组件**。
//
// 与 verify-subscribe.mjs 的区别:
//   - verify-subscribe 用的是我手写的假事件,验「分发逻辑」
//   - 本脚本用**后端真实推送的字节**,验「真实载荷能否被前端正确消费」
//
//   node scripts/verify-live-sse.mjs [捕获文件]
//
// 前置:先跑 `bash scripts/capture_sse.sh <pcap> /tmp/sse-live.txt 4942` 生成捕获。

import { createServer } from 'vite'
import { JSDOM } from 'jsdom'
import { readFileSync } from 'node:fs'

const CAPTURE = process.argv[2] || '/tmp/sse-live.txt'

// 解析 SSE 流。后端用的是**裸 data: 行**(不是 `event:` 行 + `data:` 行),
// 消息类型放在 payload 的 type 字段里 —— 与 api.js 的 es.onmessage 单一入口一致。
// 形如: data: {"type":"wildpets","account":"...","data":{...}}
function parseSSE(text) {
  const out = []
  for (const line of text.split('\n')) {
    if (!line.startsWith('data:')) continue
    const raw = line.slice(5).trim()
    if (!raw) continue
    try {
      const payload = JSON.parse(raw)
      if (payload && payload.type) out.push(payload)
    } catch { /* 非 JSON,跳过 */ }
  }
  return out
}

let events
try {
  events = parseSSE(readFileSync(CAPTURE, 'utf8'))
} catch {
  console.error('读不到捕获文件:', CAPTURE, '\n请先跑: bash scripts/capture_sse.sh <pcap> ' + CAPTURE + ' 4942')
  process.exit(2)
}
if (!events.length) { console.error('捕获文件里没有事件'); process.exit(2) }

const kinds = events.reduce((m, e) => (m[e.type] = (m[e.type] || 0) + 1, m), {})
console.log('① 捕获的真实事件:', Object.entries(kinds).map(([k, v]) => `${k}×${v}`).join(' '))

// —— jsdom + 真实前端组件 ——
// 把捕获到的事件按后端同样的格式(类型在 payload 的 type 字段里)投喂给 EventSource
function installMocks(win, queue) {
  win.matchMedia = (q) => ({ matches: false, media: q, addEventListener() {}, removeEventListener() {} })
  win.Notification = class { static permission = 'default'; static requestPermission = async () => 'default' }
  const ctx = new Proxy({}, { get: (t, k) => (k === 'measureText' ? () => ({ width: 10 }) : () => {}), set: () => true })
  win.HTMLCanvasElement.prototype.getContext = () => ctx
  for (const n of ['IntersectionObserver', 'ResizeObserver']) {
    win[n] = class { observe() {} unobserve() {} disconnect() {} }
  }
  win.HTMLElement.prototype.scrollIntoView = function () {}
  win.HTMLElement.prototype.scrollTo = function () {}
  win.EventSource = class {
    constructor(url) { this.url = url; queue.push(this) }
    addEventListener() {} removeEventListener() {} close() {}
  }
}

const dom = new JSDOM('<!doctype html><html><body><div id="root"></div></body></html>',
  { url: 'http://localhost:5173', pretendToBeVisual: true })
const win = dom.window
const sources = []
installMocks(win, sources)
// 后端不需要真的连:捕获里已有真实事件,REST 快照走本地文件或空响应即可
const reqs = []
const realFetch = globalThis.fetch
globalThis.fetch = async (u, o) => {
  const url = typeof u === 'string' ? u : u.url
  reqs.push(url.split('?')[0])
  return realFetch(url.startsWith('/') ? 'http://localhost:4939' + url : url, o)
}
for (const k of ['window', 'document', 'navigator', 'localStorage', 'sessionStorage', 'HTMLElement',
  'Element', 'Node', 'getComputedStyle', 'requestAnimationFrame', 'cancelAnimationFrame',
  'matchMedia', 'Notification', 'EventSource', 'Blob', 'URL', 'IntersectionObserver',
  'ResizeObserver', 'HTMLCanvasElement']) {
  if (win[k] === undefined) continue
  try { globalThis[k] = win[k] } catch { Object.defineProperty(globalThis, k, { value: win[k], configurable: true }) }
}

const server = await createServer({ root: process.cwd(), logLevel: 'error', server: { middlewareMode: true, hmr: false } })
const React = (await import('react')).default
const { createRoot } = await import('react-dom/client')
const { MemoryRouter, Routes, Route } = await import('react-router-dom')

const App = (await server.ssrLoadModule('/src/App.jsx')).default
// 验三个会消费这些事件的页面:花种(flowers)、地图(paint/stars/starzones/home)、精灵蛋(eggs)
const Flowers = (await server.ssrLoadModule('/src/pages/flowers/Flowers.jsx')).default
const MapPage = (await server.ssrLoadModule('/src/pages/map/MapPage.jsx')).default
const EggList = (await server.ssrLoadModule('/src/pages/eggs/EggList.jsx')).default

// 内容判据:这些值**来自捕获到的真实事件**,若出现在 DOM 里,说明事件确实被消费并渲染。
// 花种页是直接更新型的代表 —— 广播带全量数据,setData 即可,不会再发请求,
// 故只能用内容判据验证(用「有没有补拉」会误判成没消费)。
const MARKERS = {
  '/flowers': ['食尘短绒'],
}

const wait = (ms) => new Promise((r) => setTimeout(r, ms))
const results = []

for (const [route, C, label] of [['/flowers', Flowers, '花种'], ['/map', MapPage, '地图'], ['/eggs', EggList, '精灵蛋']]) {
  sources.length = 0
  const host = win.document.createElement('div')
  win.document.body.appendChild(host)
  const errs = []
  const origErr = console.error
  console.error = (...a) => { const s = a.map(String).join(' '); if (!/act\(|React Router Future/.test(s)) errs.push(s) }

  const root = createRoot(host)
  root.render(React.createElement(MemoryRouter, { initialEntries: [route] },
    React.createElement(Routes, null,
      React.createElement(Route, { element: React.createElement(App) },
        React.createElement(Route, { path: route.slice(1), element: React.createElement(C) })))))
  await wait(2500)

  const before = host.innerHTML.length
  reqs.length = 0 // 只统计「投喂事件后」触发的补拉
  // 投喂:走 es.onmessage 单一入口,消息形状与后端一致({type, account, data})
  let delivered = 0
  for (const src of sources) {
    if (!src.onmessage) continue
    for (const payload of events) {
      try {
        src.onmessage({ data: JSON.stringify(payload) })
        delivered++
      } catch (e) { errs.push('投喂异常: ' + e.message) }
    }
  }
  await wait(2000)
  const after = host.innerHTML.length
  console.error = origErr

  // 关键判据:事件必须**真的驱动了重取**。若只是被过滤掉、不报错, delivered 也是满的,
  // 只判「不报错」不够——事件被过滤掉也照样不报错,那就等于没验。
  // 两种消费方式都是正确设计,分开判:
  //   - 触发重取型(eggs / pois):看有没有发出补拉请求
  //   - 直接更新型(flowers:广播带全量数据,setData 即可,不必再请求):看 DOM 是否出现事件里的数据
  const refetched = [...new Set(reqs)]
  const text = host.textContent || ''
  const markers = (MARKERS[route] || []).filter((m) => text.includes(m))
  const consumed = refetched.length > 0 || markers.length > 0
  const ok = errs.length === 0 && delivered > 0 && consumed
  if (ok) results.push(route)
  console.log(`${ok ? '✅' : '❌'} ${label.padEnd(4)} ${route.padEnd(9)} 投喂 ${delivered} 条, DOM ${before}→${after}B`
    + (refetched.length ? `  补拉: ${refetched.join(' ')}` : '')
    + (markers.length ? `  命中事件数据: ${markers.join(' ')}` : '')
    + (errs.length ? `  错误${errs.length}: ${errs[0].slice(0, 160)}` : ''))
  root.unmount()
  host.remove()
}

await server.close()
console.log(`\n${results.length}/3 个页面能正确消费真实 SSE 事件`)
process.exit(results.length === 3 ? 0 : 1)
