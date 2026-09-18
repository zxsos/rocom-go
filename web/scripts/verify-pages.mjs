// 用 jsdom + Vite SSR 真实渲染前端页面,验收重构后各页能否正常挂载与出数据。
// 比只看「接口 200」可靠:组件里的运行时异常(undefined 访问、hook 误用)只有真跑才暴露。
//
//   node scripts/verify-pages.mjs [页面路径...]
//
// 默认渲染全部路由。后端需已运行(默认 http://localhost:4939)。
// 说明:jsdom 无 canvas/EventSource/matchMedia,本脚本做了最小 mock(见 installMocks),
// 故**通过不等于浏览器里一定没问题**,但能挡住绝大多数运行时错误。

import { createServer } from 'vite'
import { JSDOM } from 'jsdom'

const BACKEND = process.env.VERIFY_BACKEND || 'http://localhost:4939'

// 要渲染的路由(hash 路由,对应 main.jsx 里的 Routes)
const ROUTES = process.argv.slice(2).length
  ? process.argv.slice(2)
  : ['/pets', '/eggs', '/handbook', '/events', '/map', '/flowers', '/merchant', '/leaderboard', '/admin', '/debug']

// 路由表:与 src/main.jsx 的 <Routes> 一一对应(值 = /src/pages/ 下的路径)。
const PAGES = {
  '/pets': 'pet-list/PetList',
  '/events': 'events/Events',
  '/eggs': 'eggs/EggList',
  '/merchant': 'merchant/Merchant',
  '/map': 'map/MapPage',
  '/flowers': 'flowers/Flowers',
  '/handbook': 'handbook/HandbookGlasses',
  '/leaderboard': 'leaderboard/Leaderboard',
  '/debug': 'debug/Debug',
  '/admin': 'admin/Admin',
}

// 每个路由渲染后必须出现的关键文案。用于区分「渲染成功」与「渲染了空壳」——
// 接口 500 时页面也会正常挂载,只有查内容才能发现。
// 取值来自本次 pcap 回放的真实数据(账号 示例玩家 / UID:100000002)。
const CONTENT_CHECKS = {
  '/pets': ['示例玩家'], // 顶栏账号名(AccountContext 生效)
  '/eggs': ['精灵蛋'],
  '/handbook': ['炫彩'],
  '/events': ['捕获'],
  '/flowers': ['花种'],
  '/leaderboard': ['排行'],
  // 未登录时应显示登录卡(「管理员登录」/「设置管理员密码」二选一,取决于是否配过密码)
  '/admin': ['管理员'],
  '/debug': ['opcode'],
  // /map 依赖 canvas,jsdom 下画不出底图,只验挂载不报错
  // /merchant 也只验挂载:它渲染的是当前营业日的轮次,内容随一天四个整点与打烊变化,
  //   写死内容断言会随时钟飘。(该页已不依赖任何服务端令牌配置,见 api_merchant.go)
}

// —— 浏览器 API mock ——
// jsdom 缺这些;缺了组件会在挂载时抛错,导致假阳性。
function installMocks(win) {
  // matchMedia:主题跟随系统用(useTheme)
  win.matchMedia = (q) => ({
    matches: false, media: q, onchange: null,
    addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {},
    dispatchEvent: () => false,
  })
  // Notification:野生宠通知用(useWildPets)
  win.Notification = class { static permission = 'default'; static requestPermission = async () => 'default' }
  // canvas.getContext:地图底图/炫彩合成用。给个记录调用的空壳,不真绘制。
  const ctx2d = new Proxy({}, {
    get: (t, k) => {
      if (k === 'canvas') return { width: 800, height: 600 }
      if (k === 'getImageData') return () => ({ data: new Uint8ClampedArray(4) })
      if (k === 'createLinearGradient' || k === 'createRadialGradient') {
        return () => ({ addColorStop() {} })
      }
      if (k === 'measureText') return () => ({ width: 10 })
      return () => {}
    },
    set: () => true,
  })
  win.HTMLCanvasElement.prototype.getContext = () => ctx2d
  win.HTMLCanvasElement.prototype.toDataURL = () => 'data:image/png;base64,'
  win.HTMLCanvasElement.prototype.toBlob = (cb) => cb(new win.Blob())
  // IntersectionObserver / ResizeObserver:滚动懒加载用
  for (const name of ['IntersectionObserver', 'ResizeObserver']) {
    win[name] = class { observe() {} unobserve() {} disconnect() {} takeRecords() { return [] } }
  }
  // EventSource:连真实后端 SSE,把事件派发给前端(这样才能验实时刷新)
  win.EventSource = makeEventSource()
}

// 最小 SSE 客户端:用 fetch 读流,按 \n\n 切事件派发。
// 只实现前端用到的部分:onopen / addEventListener(type) / close。
function makeEventSource() {
  return class {
    constructor(url) {
      this.url = url
      this.listeners = new Map()
      this.readyState = 0
      this._closed = false
      this.onopen = null
      this.onerror = null
      this._run()
    }
    addEventListener(type, fn) {
      if (!this.listeners.has(type)) this.listeners.set(type, [])
      this.listeners.get(type).push(fn)
    }
    removeEventListener(type, fn) {
      const l = this.listeners.get(type)
      if (l) this.listeners.set(type, l.filter((f) => f !== fn))
    }
    close() { this._closed = true; this.readyState = 2; this._ctl?.abort() }
    _emit(type, data) {
      for (const fn of this.listeners.get(type) || []) fn({ data, type })
      for (const fn of this.listeners.get('message') || []) fn({ data, type })
    }
    async _run() {
      try {
        this._ctl = new AbortController()
        const r = await fetch(BACKEND + this.url, {
          signal: this._ctl.signal,
          headers: { Accept: 'text/event-stream' },
        })
        if (!r.ok) { this.onerror?.({}); return }
        this.readyState = 1
        this.onopen?.({})
        // 前端 api.js 在 onopen 时自己造 stream-open,这里只转发后端真实事件
        let buf = ''
        for await (const chunk of r.body) {
          if (this._closed) break
          buf += Buffer.from(chunk).toString('utf8')
          let i
          while ((i = buf.indexOf('\n\n')) >= 0) {
            const raw = buf.slice(0, i)
            buf = buf.slice(i + 2)
            const ev = {}
            for (const line of raw.split('\n')) {
              const m = /^([a-z]+):\s*(.*)$/.exec(line)
              if (m) ev[m[1]] = m[2]
            }
            if (ev.event) this._emit(ev.event, ev.data || '')
          }
        }
      } catch {
        if (!this._closed) this.onerror?.({})
      }
    }
  }
}

// 把 jsdom 的 window 装成 Node 全局,让 react-dom 以为在浏览器里。
function globalsFrom(win) {
  const keys = ['window', 'document', 'navigator', 'location', 'history', 'localStorage',
    'sessionStorage', 'HTMLElement', 'Element', 'Node', 'Event', 'CustomEvent', 'MouseEvent',
    'KeyboardEvent', 'PointerEvent', 'getComputedStyle', 'requestAnimationFrame',
    'cancelAnimationFrame', 'matchMedia', 'Notification', 'EventSource', 'Blob', 'URL',
    'IntersectionObserver', 'ResizeObserver', 'DOMParser', 'Image', 'HTMLCanvasElement']
  const saved = {}
  for (const k of keys) {
    if (win[k] === undefined) continue
    saved[k] = globalThis[k]
    // navigator 等在 Node 22 里是只读 getter,直接赋值会抛错,得走 defineProperty
    try {
      globalThis[k] = win[k]
    } catch {
      Object.defineProperty(globalThis, k, {
        value: win[k], configurable: true, writable: true,
      })
    }
  }
  globalThis.self = win
  // 相对路径 fetch → 补成后端地址(组件里都写 /api/xxx)
  const realFetch = globalThis.fetch
  globalThis.fetch = (url, opts) =>
    realFetch(typeof url === 'string' && url.startsWith('/') ? BACKEND + url : url, opts)
  return () => { for (const k of keys) globalThis[k] = saved[k] }
}

// 抑制 React 的 act/警告噪音,只留真错误
function captureErrors(win) {
  const errors = []
  const origErr = console.error
  console.error = (...a) => { errors.push(a.map(String).join(' ')) }
  win.addEventListener('error', (e) => errors.push('window.onerror: ' + (e.message || e)))
  win.addEventListener('unhandledrejection', (e) => errors.push('unhandledrejection: ' + e.reason))
  return { errors, restore: () => { console.error = origErr } }
}

const results = []
const server = await createServer({
  root: process.cwd(),
  logLevel: 'error',
  server: { middlewareMode: true, hmr: false },
  optimizeDeps: { noDiscovery: true },
})

for (const route of ROUTES) {
  const dom = new JSDOM('<!doctype html><html><body><div id="root"></div></body></html>', {
    url: 'http://localhost:5173/#' + route,
    pretendToBeVisual: true,
  })
  const win = dom.window
  installMocks(win)
  const restoreGlobals = globalsFrom(win)
  const { errors, restore } = captureErrors(win)

  let html = ''
  let text = ''
  try {
    // react / react-dom 是 CJS,不能经 vite 的 ssrLoadModule(会报 module is not defined),
    // 直接在 Node 侧 import;只有业务代码(含 JSX)走 vite 转换。
    const React = (await import('react')).default
    const { createRoot } = await import('react-dom/client')
    const { MemoryRouter, Routes, Route } = await import('react-router-dom')
    // 路由表与 src/main.jsx 保持一致(App 是布局路由,靠 <Outlet> 渲染子页);
    // 用 MemoryRouter 而非 HashRouter:jsdom 改 hash 不会同步给已创建的 history,
    // 会导致每个路由都渲染成同一页。
    const pages = {}
    for (const [p, name] of Object.entries(PAGES)) {
      pages[p] = (await server.ssrLoadModule(`/src/pages/${name}`)).default
    }
    const App = (await server.ssrLoadModule('/src/App.jsx')).default
    const root = createRoot(win.document.getElementById('root'))
    root.render(React.createElement(MemoryRouter, { initialEntries: [route] },
      React.createElement(Routes, null,
        React.createElement(Route, { element: React.createElement(App) },
          Object.entries(pages).map(([p, C]) =>
            React.createElement(Route, { key: p, path: p.slice(1), element: React.createElement(C) }))))))
    // 等数据落地:接口 + 一轮渲染
    await new Promise((r) => setTimeout(r, 2500))
    // 必须在 unmount 前取 DOM:unmount 会清空容器,之后再读恒为空。
    const el = win.document.getElementById('root')
    html = el.innerHTML
    text = el.textContent || ''
    root.unmount()
  } catch (e) {
    errors.push('渲染异常: ' + (e && e.stack ? e.stack.split('\n').slice(0, 3).join(' | ') : e))
  }

  // 内容校验:只「不报错」不够——接口挂了也会渲染出空壳,故再查关键文案。
  const missing = (CONTENT_CHECKS[route] || []).filter((c) => !text.includes(c))

  const real = errors.filter((e) =>
    !/not wrapped in act|Warning: ReactDOM|useLayoutEffect does nothing|React Router Future Flag/.test(e))
  const ok = real.length === 0 && missing.length === 0
  results.push({ route, ok, errors: real, missing, len: html.length })
  restore()
  restoreGlobals()
  dom.window.close()

  console.log(`${ok ? '✅' : '❌'} ${route.padEnd(14)} DOM ${String(html.length).padStart(7)}B`
    + (real.length ? `  错误 ${real.length}` : '')
    + (missing.length ? `  缺内容 ${missing.join('/')}` : ''))
  for (const e of real.slice(0, 3)) console.log('     ' + e.slice(0, 300))
}

await server.close()

const bad = results.filter((r) => !r.ok)
console.log(`\n${results.length - bad.length}/${results.length} 个路由渲染无错误`)
process.exit(bad.length ? 1 : 0)
