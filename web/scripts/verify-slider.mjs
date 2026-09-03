// 双滑块的回归测试(jsdom,不需要后端)。
//
//   node scripts/verify-slider.mjs
//
// 两组断言:
//
// 1) 「拖一端不会翻转区间」—— clampRange 是纯函数,verify-rules.mjs 测得到;
//    但**组件有没有把它接上**测不到 —— 两个 range input 各自独立,若 onChange 直接
//    把值写回(不经过 clampRange),下限就能被拖到上限右边,区间翻掉:高亮段消失、
//    读数变成「80~60」,而纯函数测试照样全绿。
//
// 2) 「变焦刻度接上了」—— 轨道是非线性的(见 utils/rules.js 的 rangeScale),
//    两个 range input 走的是**位置空间**(0~1000)而非取值域。若哪天有人把
//    min/max 改回取值域,浏览器就会按均匀分布铺轨道,重点区放大全部失效,
//    而页面上**没有任何报错** —— 只是奖牌区又变回几像素宽。这种回归只有断言能抓。
//
// 尺子本身的数值性质(单调性、量化、磁吸)在 verify-rules.mjs 里测;这里只管接线。
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
const { DEFAULT_RANGE_RULES, DIM_BY_K, rangeScale } = await server.ssrLoadModule('/src/utils/rules.js')

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
//
// 注意 value 是**轨道位置**(0~1000),不是取值 —— 见文件头的说明。
const drag = async (index, edge, pos) => {
  const rows = document.querySelectorAll('.rrule')
  const inputs = rows[index].querySelectorAll('.rslider-input')
  const el = edge === 'min' ? inputs[0] : inputs[1]
  await act(async () => {
    // React 用 value 的 setter 追踪,直接赋值会漏掉更新 —— 走原型上的 setter
    const setter = Object.getOwnPropertyDescriptor(win.HTMLInputElement.prototype, 'value').set
    setter.call(el, String(pos))
    el.dispatchEvent(new win.Event('input', { bubbles: true }))
  })
}

const STEPS = 1000
const wScale = rangeScale(DIM_BY_K.weightPct)
const vScale = rangeScale(DIM_BY_K.voice)

console.log('\n[1] 渲染与结构')
{
  const rows = document.querySelectorAll('.rrule')
  ok('渲染出 4 条默认规则', rows.length === 4, `实际 ${rows.length}`)
  const inputs = rows[0].querySelectorAll('.rslider-input')
  ok('每条规则有两个滑块(下限/上限)', inputs.length === 2, `实际 ${inputs.length}`)
  ok('滑块走位置空间(min=0, max=1000)',
    inputs[0].min === '0' && inputs[0].max === '1000',
    `实际 ${inputs[0].min}~${inputs[0].max}`)
  // 大块头 [98,100] 的位置由尺子算出,而非 98/100 这两个数本身
  const want = [wScale.toPos(98), wScale.toPos(100)].map((p) => String(Math.round(p * STEPS)))
  ok('大块头:滑块位置 = 尺子算出的位置(不是取值)',
    inputs[0].value === want[0] && inputs[1].value === want[1],
    `实际 ${inputs[0].value}/${inputs[1].value}，期望 ${want.join('/')}`)
  ok('维度切换按钮带上了刻度说明',
    /刻度不均匀/.test(rows[0].querySelector('.rrule-dim').title || ''))
}

console.log('\n[2] 变焦刻度画在了轨道上')
{
  const rows = document.querySelectorAll('.rrule')
  const zones = rows[0].querySelectorAll('.rslider-zone')
  const ticks = rows[0].querySelectorAll('.rslider-tick')
  ok('画出了 2 段重点区(两个放大镜)', zones.length === 2, `实际 ${zones.length}`)
  ok('画出了 4 条奖牌刻度线', ticks.length === 4, `实际 ${ticks.length}`)
  // 重点区应当吃掉大部分轨道:两段各 35%
  const width = [...zones].reduce((n, z) => n + parseFloat(z.style.width), 0)
  ok('重点区合计约占 70% 轨道', Math.abs(width - 70) < 1, `实际 ${width.toFixed(1)}%`)
  // 刻度线的位置必须与尺子一致,否则画出来的刻度是骗人的
  const wantPos = wScale.marks.map((m) => `${(m.pos * 100).toFixed(4)}%`)
  const gotPos = [...ticks].map((t) => t.style.left)
  const near = wantPos.every((w, i) => Math.abs(parseFloat(w) - parseFloat(gotPos[i])) < 0.01)
  ok('刻度线位置 = 尺子算出的奖牌边界', near, `实际 ${gotPos.join(' ')}，期望 ${wantPos.join(' ')}`)
}

console.log('\n[3] 重点区被放大 / 中间段被压扁(核心)')
{
  // 奖牌窗口 [98,100] 只占取值域的 2%,但摊到的轨道必须明显更多 —— 这就是
  // 「在需要的区域太小了」的解法。小了就说明尺子没接上。
  const medal = wScale.toPos(100) - wScale.toPos(98)
  ok('奖牌窗口占轨道比按比例放大 2.5 倍以上',
    medal > (2 / 100) * 2.5, `实际 ${(medal * 100).toFixed(1)}%(按比例只有 2%)`)
  // 反过来:中间一段应被压扁,否则"变焦"就没变
  const mid = wScale.toPos(60) - wScale.toPos(40)
  ok('中间段(体重 40~60)占轨道比按比例更小',
    mid < 20 / 100, `实际 ${(mid * 100).toFixed(1)}%(按比例是 20%)`)
  // 拖动灵敏度:同样 1% 轨道,中间段走的取值必须远多于重点区
  const perFocus = wScale.toValue(0.20) - wScale.toValue(0.15)
  const perGap = wScale.toValue(0.55) - wScale.toValue(0.50)
  ok('同样拖 5% 轨道,中间段走的取值更多',
    perGap > perFocus * 3, `重点区 ${perFocus.toFixed(2)} / 中间段 ${perGap.toFixed(2)}`)
}

console.log('\n[4] 磁吸:奖牌边界拖得准,又拖得开')
{
  // 大块头 min=98,其位置在 93%。拖到 92%(略偏左)应被吸附回 98 ——
  // 奖牌是截断判定,97.9 筛不出大块头,手拖不稳就得靠吸附。
  await drag(0, 'min', Math.round(0.92 * STEPS))
  ok('拖到奖牌边界附近 → 吸附到 98', current[0].min === 98, `实际 ${current[0].min}`)
  // 但吸附必须能脱离:再往左拖远一点(88%),就该拿到真正对应的值,而不是还粘在 98
  await drag(0, 'min', Math.round(0.88 * STEPS))
  ok('拖离边界 → 脱离吸附(能放宽标准)', current[0].min < 98, `实际 ${current[0].min}`)
}

console.log('\n[5] 拖一端不翻转区间(核心)')
{
  // 拖到轨道最右端 → 取值 100,应被钳到上限 100,不能翻成 [100, 98]
  await drag(0, 'min', STEPS)
  const r0 = current[0]
  ok('下限拖过上限 → 钳住(不翻转)', r0.min <= r0.max, `min=${r0.min} max=${r0.max}`)
  ok('下限钳到取值域上界 100', r0.max === 100, `max=${r0.max}`)

  // 反向:把上限拖到轨道最左端 → 钳到下限
  await drag(0, 'max', 0)
  const r1 = current[0]
  ok('上限拖过下限 → 钳住(不翻转)', r1.min <= r1.max, `min=${r1.min} max=${r1.max}`)
}

console.log('\n[6] 拖动后区间始终合法(遍历整条轨道)')
{
  const bad = []
  // 遍历整条轨道而不只是几个点:量化/磁吸/分段都是位置的函数,只抽查几个点
  // 会漏掉「某一段算出的值越界」这类只在局部出现的错误。
  for (let i = 1; i < STEPS; i++) {
    await drag(1, 'min', i)
    await drag(1, 'max', i)
    const r = current[1]
    const d = DIM_BY_K[r.dim]
    if (!(r.min <= r.max)) bad.push(`位置 ${i}: 翻转 ${r.min}~${r.max}`)
    if (r.min < d.min || r.max > d.max) bad.push(`位置 ${i}: 越界 ${r.min}~${r.max}`)
    if (bad.length > 3) break
  }
  ok('体重维度:整条轨道拖过去都合法', bad.length === 0, bad.join('; '))

  const badV = []
  for (let i = 1; i < STEPS; i++) {
    await drag(2, 'min', i)
    await drag(2, 'max', i)
    const r = current[2]
    const d = DIM_BY_K[r.dim]
    if (!(r.min <= r.max)) badV.push(`位置 ${i}: 翻转 ${r.min}~${r.max}`)
    if (r.min < d.min || r.max > d.max) badV.push(`位置 ${i}: 越界 ${r.min}~${r.max}`)
    if (badV.length > 3) break
  }
  ok('嗓音维度(负数域):整条轨道拖过去都合法', badV.length === 0, badV.join('; '))

  const out = []
  for (const r of current) {
    const d = DIM_BY_K[r.dim]
    if (r.min < d.min || r.max > d.max) out.push(`${r.label}:${r.min}~${r.max}`)
  }
  ok('所有规则都在取值域内', out.length === 0, out.join('; '))
}

console.log('\n[7] 嗓音维度的重点区(负数域)')
{
  // 粗嗓门 [-100,-96] 是负数域的奖牌窗口,同样要被放大
  const medal = vScale.toPos(-96) - vScale.toPos(-100)
  ok('嗓音奖牌窗口同样被放大',
    medal > (4 / 200) * 2.5, `实际 ${(medal * 100).toFixed(1)}%(按比例只有 2%)`)
  ok('嗓音也画出了 2 段重点区',
    document.querySelectorAll('.rrule')[2].querySelectorAll('.rslider-zone').length === 2)
}

await server.close()
console.log(fail === 0 ? '\n✓ 全部通过' : `\n✗ ${fail} 项未通过`)
process.exit(fail === 0 ? 0 : 1)
