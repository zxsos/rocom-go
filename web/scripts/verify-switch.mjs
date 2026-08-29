// 验收「切账号」路径:去掉 <main key={account}> 后,切账号应只刷新数据、不重建组件树。
// 这是阶段 6 的核心变更——原来靠卸载重建刷新,现在靠 useAsyncData 的 reloadKey 驱动。
//
//   node scripts/verify-switch.mjs
//
// 检查三件事:
//   1. 切账号后各页数据确实变了(账号隔离生效)
//   2. 请求数不增加(原来挂载+effect 各触发一次,StrictMode 下更甚)
//   3. 切账号时组件实例未被销毁重建(UI 态保留)

import { createServer } from 'vite'
import { JSDOM } from 'jsdom'

const BACKEND = process.env.VERIFY_BACKEND || 'http://localhost:4939'

// 记录所有发出的请求,用于核对请求数
const requests = []
const realFetch = globalThis.fetch
globalThis.fetch = (url, opts) => {
  const u = typeof url === 'string' ? url : url.url
  requests.push(u)
  return realFetch(u.startsWith('/') ? BACKEND + u : u, opts)
}

function installMocks(win) {
  win.matchMedia = (q) => ({ matches: false, media: q, addEventListener() {}, removeEventListener() {} })
  win.Notification = class { static permission = 'default'; static requestPermission = async () => 'default' }
  const ctx = new Proxy({}, { get: (t, k) => (k === 'measureText' ? () => ({ width: 10 }) : () => {}), set: () => true })
  win.HTMLCanvasElement.prototype.getContext = () => ctx
  for (const n of ['IntersectionObserver', 'ResizeObserver']) {
    win[n] = class { observe() {} unobserve() {} disconnect() {} }
  }
  win.EventSource = class {
    constructor() { this.readyState = 0 }
    addEventListener() {} removeEventListener() {} close() {}
  }
  // jsdom 未实现这两个,补空实现(浏览器里有,不是代码问题)
  win.HTMLElement.prototype.scrollIntoView = function () {}
  win.HTMLElement.prototype.scrollTo = function () {}
}

const dom = new JSDOM('<!doctype html><html><body><div id="root"></div></body></html>',
  { url: 'http://localhost:5173', pretendToBeVisual: true })
const win = dom.window
installMocks(win)
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
const PetList = (await server.ssrLoadModule('/src/pages/pet-list/PetList.jsx')).default

// 统计 PetList 的挂载/卸载次数:切账号若重建组件树,unmount 会 +1
let mounted = 0
let unmounted = 0
const Probe = () => {
  React.useEffect(() => { mounted++; return () => { unmounted++ } }, [])
  return React.createElement(PetList)
}

const root = createRoot(win.document.getElementById('root'))
root.render(React.createElement(MemoryRouter, { initialEntries: ['/pets'] },
  React.createElement(Routes, null,
    React.createElement(Route, { element: React.createElement(App) },
      React.createElement(Route, { path: 'pets', element: React.createElement(Probe) })))))

const wait = (ms) => new Promise((r) => setTimeout(r, ms))
await wait(3000)

const t1 = win.document.getElementById('root').textContent || ''
// 账号 1(示例玩家,862 只宠物)的洛克贝数字,用于确认切前状态
const coins1 = t1.includes('247,492,719') || t1.includes('示例玩家')
console.log('① 初始渲染:账号可见 =', coins1, '| DOM', t1.length, 'B')

// —— 切账号:走真实交互路径 ——
// 不重新挂载(那正是要验的「不重建」),而是点击顶栏账号下拉里的另一项,
// 让 App 的 switchAccount → useAccounts.selectAccount 自然流转。
requests.length = 0
const before = { mounted, unmounted }

// 1) 点开账号下拉
const trigger = win.document.querySelector('.account-trigger')
trigger.dispatchEvent(new win.MouseEvent('mousedown', { bubbles: true }))
trigger.dispatchEvent(new win.MouseEvent('mouseup', { bubbles: true }))
trigger.dispatchEvent(new win.MouseEvent('click', { bubbles: true }))
await wait(300)

// 2) 在下拉里找「测试二号」那一项并点击
const items = [...win.document.querySelectorAll('.account-item')]
const target = items.find((li) => (li.textContent || '').includes('测试二号'))
console.log('② 下拉项数', items.length, '| 找到测试二号 =', !!target)
if (target) {
  target.dispatchEvent(new win.MouseEvent('mousedown', { bubbles: true }))
  await wait(3000)
}

const t2 = win.document.getElementById('root').textContent || ''
console.log('③ 切换后:测试二号可见 =', t2.includes('测试二号'),
  '| 示例玩家仍可见 =', t2.includes('示例玩家'), '| DOM', t2.length, 'B')
console.log('   挂载/卸载增量:', mounted - before.mounted, '/', unmounted - before.unmounted,
  '(0/0 表示组件树未被重建)')

console.log('\n挂载次数', mounted, '卸载次数', unmounted)
console.log('切账号期间请求数', requests.length, ':', [...new Set(requests.map((u) => u.split('?')[0]))].join(' '))

root.unmount()
await server.close()
process.exit(0)
