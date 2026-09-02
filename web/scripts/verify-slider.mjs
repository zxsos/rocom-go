// 双滑块「拖一端不会翻转区间」的回归测试(jsdom,不需要后端)。
//
//   node scripts/verify-slider.mjs
//
// 存在理由:utils/rules.js 里的 clampRange 是纯函数,verify-rules.mjs 测得到;
// 但**组件有没有把它接上**测不到 —— 两个 range input 各自独立,若 onChange 直接
// 把值写回(不经过 clampRange),下限就能被拖到上限右边,区间翻掉:高亮段消失、
// 读数变成「80~60」,而纯函数测试照样全绿。
//
// 故这里渲染真实的 RangeRules,模拟拖动,断言结果区间始终 min ≤ max。
//
// jsdom 覆盖不到的部分(别误以为通过了就等于没问题):
// 两个 range 的**叠放命中**依赖 CSS pointer-events(容器 none / thumb auto),
// jsdom 不做样式命中测试,故「上层轨道盖住下层导致某个端点拖不动」测不出来。
// 那部分只能靠真浏览器验证(或人工点一下两个端点)。

import { JSDOM } from 'jsdom'

const dom = new JSDOM('<!doctype html><html><body><div id="root"></div></body></html>', {
  url: 'http://localhost/',
  pretendToBeVisual: true,
})
const win = dom.window
for (const k of ['window', 'document', 'navigator', 'HTMLElement', 'Element', 'Node', 'Event',
  'requestAnimationFrame', 'cancelAnimationFrame', 'getComputedStyle', 'MouseEvent', 'InputEvent']) {
  try {
    globalThis[k] = win[k]
  } catch {
    Object.defineProperty(globalThis, k, { value: win[k], configurable: true, writable: true })
  }
}
globalThis.self = win
globalThis.IS_REACT_ACT_ENVIRONMENT = true

const { createServer } = await import('vite')
const server = await createServer({
  root: process.cwd(), logLevel: 'error',
  server: { middlewareMode: true, hmr: false },
  optimizeDeps: { noDiscovery: true },
})
const React = (await import('react')).default
const { createRoot } = await import('react-dom/client')
const { act, useState } = await import('react')
const { default: RangeRules } = await server.ssrLoadModule('/src/components/RangeRules.jsx')
const { DEFAULT_RANGE_RULES, DIM_BY_K } = await server.ssrLoadModule('/src/utils/rules.js')

let fail = 0
const ok = (name, cond, extra = '') => {
  if (cond) { console.log(`  ✓ ${name}`); return }
  fail++
  console.log(`  ✗ ${name}${extra ? ' —— ' + extra : ''}`)
}

// 受控 wrapper:把当前规则暴露到外部,便于断言
let current = DEFAULT_RANGE_RULES.map((r) => ({ ...r }))
function Harness() {
  const [rs, setRs] = useState(current)
  current = rs
  return React.createElement(RangeRules, { rules: rs, setRules: setRs, counts: {} })
}

const root = createRoot(document.getElementById('root'))
await act(async () => { root.render(React.createElement(Harness)) })

// 拖某个 input:改 value 再派发 input 事件(React 的 onChange 监听它)
const drag = async (index, edge, value) => {
  const rows = document.querySelectorAll('.rrule')
  const inputs = rows[index].querySelectorAll('.rslider-input')
  const el = edge === 'min' ? inputs[0] : inputs[1]
  await act(async () => {
    // React 用 value 的 setter 追踪,直接赋值会漏掉更新 —— 走原型上的 setter
    const setter = Object.getOwnPropertyDescriptor(win.HTMLInputElement.prototype, 'value').set
    setter.call(el, String(value))
    el.dispatchEvent(new win.Event('input', { bubbles: true }))
  })
}

console.log('\n[1] 渲染与结构')
{
  const rows = document.querySelectorAll('.rrule')
  ok('渲染出 4 条默认规则', rows.length === 4, `实际 ${rows.length}`)
  const inputs = rows[0].querySelectorAll('.rslider-input')
  ok('每条规则有两个滑块(下限/上限)', inputs.length === 2, `实际 ${inputs.length}`)
  ok('大块头:下限=98 上限=100',
    inputs[0].value === '98' && inputs[1].value === '100',
    `实际 ${inputs[0].value}/${inputs[1].value}`)
  ok('有维度切换按钮', rows[0].querySelector('.rrule-dim') !== null)
  ok('有整套方案区', document.querySelectorAll('.rrule-schemes .chip').length >= 3)
}

console.log('\n[2] 拖一端不翻转区间(核心)')
{
  // 大块头 [98,100]:把下限拖到 100 以上 → 应被钳到 100,不能变成 [150,100]
  await drag(0, 'min', 150)
  const r0 = current[0]
  ok('下限拖过上限 → 钳住(不翻转)',
    r0.min <= r0.max, `min=${r0.min} max=${r0.max}`)
  ok('下限钳到取值域上界 100', r0.max === 100, `max=${r0.max}`)

  // 继续:把上限拖到下限左边 → 钳到下限
  await drag(0, 'max', 0)
  const r1 = current[0]
  ok('上限拖过下限 → 钳住(不翻转)',
    r1.min <= r1.max, `min=${r1.min} max=${r1.max}`)
}

console.log('\n[3] 拖动后区间始终合法(随机抽查)')
{
  const vals = [0, 25, 50, 75, 100, -50, 150]
  let bad = []
  for (const v of vals) {
    await drag(1, 'min', v)   // 小不点(体重)
    await drag(1, 'max', v)
    const r = current[1]
    if (!(r.min <= r.max)) bad.push(`min拖${v}后 ${r.min}~${r.max}`)
  }
  eq('体重维度:各种拖动后 min ≤ max', bad, [])

  bad = []
  for (const v of [-200, -100, -50, 0, 50, 100, 200]) {
    await drag(2, 'min', v)   // 婉转声(嗓音,含负数域)
    await drag(2, 'max', v)
    const r = current[2]
    if (!(r.min <= r.max)) bad.push(`min拖${v}后 ${r.min}~${r.max}`)
  }
  eq('嗓音维度(负数域):各种拖动后 min ≤ max', bad, [])

  // 区间必须始终落在维度取值域内
  bad = []
  for (const r of current) {
    const d = DIM_BY_K[r.dim]
    if (r.min < d.min || r.max > d.max) bad.push(`${r.label}:${r.min}~${r.max}`)
  }
  eq('所有规则都在取值域内', bad, [])
}

function eq(name, got, want) {
  ok(name, JSON.stringify(got) === JSON.stringify(want),
    `实际 ${JSON.stringify(got)}，期望 ${JSON.stringify(want)}`)
}

await server.close()
console.log(fail === 0 ? '\n✓ 全部通过' : `\n✗ ${fail} 项未通过`)
process.exit(fail === 0 ? 0 : 1)
