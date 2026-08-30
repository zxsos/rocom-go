// 地图视口尺寸测量的回归测试。
//
//   node scripts/verify-map-vp.mjs
//
// 存在理由:地图所有图层(底图、POI、野生宠、小窝、涂地)都按 `mapPx = min(vp.w,vp.h)*zoom`
// 定位 —— 视口量不到尺寸时 mapPx 塌成 `1*zoom`(约 5px),表现为**所有图标挤在左上角、
// 底图缩成几像素即黑色背景**。这类故障在 jsdom 里不报错、接口也 200,只能靠断言尺寸发现。
//
// 故障路径(真实操作):引擎常驻在 App 层(画中画要在离开地图页后继续更新),
// 位置数据可能在**用户还不在地图页时**就到货 —— 那时 .map-vp 尚未挂载,测量 effect 拿不到
// 元素;等用户切到 /map,元素挂上了,而测量 effect 只依赖 hasMap(没变)不会重跑,
// ResizeObserver 永远不挂,vp 恒为 {0,0}。手动刷新能好,是因为刷新时人已经在 /map,
// hasMap 翻转的那一刻元素就在,effect 一次挂上。
//
// 本测试直接驱动 usePanZoom 复现这条路径:先只有引擎(元素未挂),再挂上视口元素。
//
// **本测试覆盖不到的两项**(jsdom 的局限,别误以为通过了就等于没问题):
//   1. 测量用 useLayoutEffect(首帧就带真实尺寸出图,避免闪一帧塌掉的地图)。这个时序差异
//      在 act() 下会被抹平 —— 换成 useEffect 本测试照样通过,但浏览器里会闪一下。
//   2. ResizeObserver 被 mock 成空操作(jsdom 没有实现),故「忘记 disconnect 导致旧
//      观察器泄漏」测不出来。
// 这两项由 **scripts/verify-map-vp-browser.mjs**(真浏览器)覆盖 —— 实测它能在
// 「退回 useEffect」时抓到宽度序列 `5, 3535`(先写一帧塌陷再纠正),在「不 disconnect」
// 时抓到存活观察器 `1 → 4`。别因为有了它就把本脚本删掉:它跑得快、不需要后端,
// 适合改一行就验一下。

import { JSDOM } from 'jsdom'

// 视口 stub 尺寸:jsdom 的 clientWidth/clientHeight 恒为 0,故给 .map-vp 一个固定值,
// 让「量到了」与「没量到」可区分。
const VP_W = 700
const VP_H = 500

const dom = new JSDOM('<!doctype html><html><body><div id="root"></div></body></html>', {
  url: 'http://localhost/',
  pretendToBeVisual: true,
})
const win = dom.window

for (const k of ['window', 'document', 'navigator', 'HTMLElement', 'Element', 'Node', 'Event',
  'requestAnimationFrame', 'cancelAnimationFrame', 'getComputedStyle']) {
  try {
    globalThis[k] = win[k]
  } catch {
    Object.defineProperty(globalThis, k, { value: win[k], configurable: true, writable: true })
  }
}
globalThis.self = win
globalThis.IS_REACT_ACT_ENVIRONMENT = true
win.ResizeObserver = class { observe() {} unobserve() {} disconnect() {} takeRecords() { return [] } }
globalThis.ResizeObserver = win.ResizeObserver
// .map-vp 有尺寸,其余元素没有(与真实布局一致)
for (const [prop, val] of [['clientWidth', VP_W], ['clientHeight', VP_H]]) {
  Object.defineProperty(win.Element.prototype, prop, {
    configurable: true,
    get() { return this.classList?.contains('map-vp') ? val : 0 },
  })
}

// 业务模块有无扩展名导入(import './motion'),Node 直接 import 解析不了,
// 故走 vite 的 ssrLoadModule(与 verify-pages.mjs 同法)。
const { createServer } = await import('vite')
const server = await createServer({
  root: process.cwd(), logLevel: 'error',
  server: { middlewareMode: true, hmr: false },
  optimizeDeps: { noDiscovery: true },
})

const React = (await import('react')).default
const { createRoot } = await import('react-dom/client')
const { act } = await import('react')
const { usePanZoom } = await server.ssrLoadModule('/src/pages/map/usePanZoom.js')

// Harness 复刻真实挂载顺序:视口元素的挂载与「引擎有无位置」**互不绑定**。
let seen = null
function Harness({ showVp }) {
  const view = usePanZoom(() => {})
  seen = view.vp
  return showVp ? React.createElement('div', { className: 'map-vp', ref: view.attachVp }) : null
}

const root = createRoot(win.document.getElementById('root'))
const results = []
const check = (name, ok, detail) => {
  results.push({ name, ok, detail })
  console.log(`${ok ? '  ok  ' : ' FAIL '} ${name}${detail ? '  — ' + detail : ''}`)
}

// —— 场景 1:引擎先有位置(active=true)、地图页后挂载 —— 即用户报的故障路径 ——
await act(async () => {
  root.render(React.createElement(Harness, { showVp: false }))
})
await act(async () => {
  root.render(React.createElement(Harness, { showVp: true }))
})
check('切到地图页后视口被测量', seen && seen.w === VP_W && seen.h === VP_H,
  `vp=${JSON.stringify(seen)}, 期望 {w:${VP_W},h:${VP_H}}`)

// —— 场景 2:从地图页切走再切回(元素卸载后重挂)——
await act(async () => {
  root.render(React.createElement(Harness, { showVp: false }))
})
await act(async () => {
  root.render(React.createElement(Harness, { showVp: true }))
})
check('切走再切回后仍有尺寸', seen && seen.w === VP_W && seen.h === VP_H,
  `vp=${JSON.stringify(seen)}`)

// —— 场景 3:卸载后重新挂载(位置后到)—— 这条原本就正常,防回归 ——
await act(async () => { root.unmount() })
const root2 = createRoot(win.document.getElementById('root'))
await act(async () => {
  root2.render(React.createElement(Harness, { showVp: false }))
})
await act(async () => {
  root2.render(React.createElement(Harness, { showVp: true }))
})
check('重新挂载后正常测量', seen && seen.w === VP_W && seen.h === VP_H,
  `vp=${JSON.stringify(seen)}`)
await act(async () => { root2.unmount() })

await server.close()

const bad = results.filter((r) => !r.ok)
console.log(`\n${results.length - bad.length}/${results.length} 项通过`)
process.exit(bad.length ? 1 : 0)
