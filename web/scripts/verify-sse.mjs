// 验收 SSE 实时刷新:subscribe(type, onData, {onOpen}) 改造后,各页应仍能收到推送并更新。
// 阶段 2 把事件过滤/账号过滤/断线补拉都下沉到了 api.js,这里验证下沉后语义没丢。
//
//   node scripts/verify-sse.mjs
//
// 后端需已运行。用真实 EventSource 客户端(见 installMocks)连 /api/stream,
// 因此能验到「连上→收事件→前端更新」整条链路。

import { createServer } from 'vite'
import { JSDOM } from 'jsdom'

const BACKEND = process.env.VERIFY_BACKEND || 'http://localhost:4939'

// 记录 EventSource 的连接与收到的事件,用于确认前端确实订阅并消费了
const sseLog = []

function installMocks(win) {
  win.matchMedia = (q) => ({ matches: false, media: q, addEventListener() {}, removeEventListener() {} })
  win.Notification = class { static permission = 'default'; static requestPermission = async () => 'default' }
  const ctx = new Proxy({}, { get: (t, k) => (k === 'measureText' ? () => ({ width: 10 }) : () => {}), set: () => true })
  win.HTMLCanvasElement.prototype.getContext = () => ctx
  for (const n of ['IntersectionObserver', 'ResizeObserver']) {
    win[n] = class { observe() {} unobserve() {} disconnect() {} }
  }
  win.HTMLElement.prototype.scrollIntoView = function () {}
  win.HTMLElement.prototype.scrollTo = function () {}

  // 真实 SSE 客户端:连后端 /api/stream,把事件派发给前端
  win.EventSource = class {
    constructor(url) {
      this.url = url
      this.l = new Map()
      this.readyState = 0
      this.closed = false
      sseLog.push('connect ' + url)
      this._run()
    }
    addEventListener(t, fn) { (this.l.get(t) || this.l.set(t, []).get(t)).push(fn) }
    removeEventListener() {}
    close() { this.closed = true; this.readyState = 2; this.ctl?.abort(); sseLog.push('close') }
    _emit(t, data) { for (const fn of this.l.get(t) || []) fn({ data, type: t }) }
    async _run() {
      try {
        this.ctl = new AbortController()
        const r = await fetch(BACKEND + this.url, { signal: this.ctl.signal, headers: { Accept: 'text/event-stream' } })
        if (!r.ok) return
        this.readyState = 1
        if (this.onopen) this.onopen({})
        sseLog.push('open')
        let buf = ''
        for await (const c of r.body) {
          if (this.closed) break
          buf += Buffer.from(c).toString('utf8')
          let i
          while ((i = buf.indexOf('\n\n')) >= 0) {
            const raw = buf.slice(0, i); buf = buf.slice(i + 2)
            const ev = {}
            for (const line of raw.split('\n')) {
              const m = /^([a-z]+):\s*(.*)$/.exec(line)
              if (m) ev[m[1]] = m[2]
            }
            if (ev.event) { sseLog.push('event ' + ev.event); this._emit(ev.event, ev.data || '') }
          }
        }
      } catch { /* 关闭时忽略 */ }
    }
  }
}

const dom = new JSDOM('<!doctype html><html><body><div id="root"></div></body></html>',
  { url: 'http://localhost:5173', pretendToBeVisual: true })
const win = dom.window
installMocks(win)
const realFetch = globalThis.fetch
globalThis.fetch = (u, o) => realFetch(typeof u === 'string' && u.startsWith('/') ? BACKEND + u : u, o)
for (const k of ['window', 'document', 'navigator', 'localStorage', 'sessionStorage',
  'HTMLElement', 'Element', 'Node', 'getComputedStyle', 'requestAnimationFrame',
  'cancelAnimationFrame', 'matchMedia', 'Notification', 'EventSource', 'Blob', 'URL',
  'IntersectionObserver', 'ResizeObserver', 'HTMLCanvasElement']) {
  if (win[k] === undefined) continue
  try { globalThis[k] = win[k] } catch { Object.defineProperty(globalThis, k, { value: win[k], configurable: true }) }
}

const server = await createServer({ root: process.cwd(), logLevel: 'error', server: { middlewareMode: true, hmr: false } })
const React = (await import('react')).default
const { createRoot } = await import('react-dom/client')
const { MemoryRouter, Routes, Route } = await import('react-router-dom')

const App = (await server.ssrLoadModule('/src/App.jsx')).default
const pages = {
  '/map': 'map/MapPage',
  '/events': 'events/Events',
  '/eggs': 'eggs/EggList',
  '/flowers': 'flowers/Flowers',
}
const comps = {}
for (const [p, f] of Object.entries(pages)) {
  comps[p] = (await server.ssrLoadModule(`/src/pages/${f}`)).default
}

let pass = 0
for (const [route, C] of Object.entries(comps)) {
  sseLog.length = 0
  const host = win.document.createElement('div')
  win.document.body.appendChild(host)
  const root = createRoot(host)
  root.render(React.createElement(MemoryRouter, { initialEntries: [route] },
    React.createElement(Routes, null,
      React.createElement(Route, { element: React.createElement(App) },
        React.createElement(Route, { path: route.slice(1), element: React.createElement(C) })))))
  await new Promise((r) => setTimeout(r, 3000))

  const connected = sseLog.includes('open')
  const events = sseLog.filter((s) => s.startsWith('event ')).length
  const domLen = (host.textContent || '').length
  const ok = connected && domLen > 100
  if (ok) pass++
  console.log(`${ok ? '✅' : '❌'} ${route.padEnd(10)} SSE连接=${connected} 收到事件=${events} DOM=${domLen}B`)
  root.unmount()
  host.remove()
}

await server.close()
console.log(`\n${pass}/${Object.keys(comps).length} 个页面 SSE 连接正常`)
process.exit(pass === Object.keys(comps).length ? 0 : 1)
