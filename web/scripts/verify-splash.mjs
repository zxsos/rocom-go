// 开屏加载动画的回归测试(jsdom,不需要后端)。
//
//   node scripts/verify-splash.mjs
//
// 为什么需要它:开屏是首屏唯一的「拦路虎」—— 它自己写错,表现是整个应用进不去,
// 而构建、lint 与其余 verify 脚本都发现不了。四个**静默失效**的坑:
//
//   1. 门控失效(ready 忘了判)—— 数据还在路上就播收尾,主界面顶栏先空一块再补上;
//      用户看不到报错,只觉得「这站闪了一下」。
//   2. 退场时长与 CSS 不同步 —— 组件按时间卸载,只改一边的话动画没播完就被摘掉
//      (半透明的书被硬切),或者白等一段。见 [4] 与 [6] 的 EXIT_MS 断言。
//   3. 素材少一张 —— 进度条上不去,开屏变成死等;而少的那张图只在浏览器 Network
//      里可见,jsdom 与构建都当无事发生。见 [2] 与磁盘清单的比对。
//   4. 无障碍降级漏了 —— prefers-reduced-motion 下仍播 1.7s 播片,不报错。
//
// 覆盖:[1] 结构 / [2] 进度条由图片加载驱动 + 素材清单 / [3] ready 门控 /
//       [4] 退场时序与卸载 / [5] reduced-motion 降级 / [6] 与 CSS、index.html 的静态一致性。
//
// ⚠️ jsdom 覆盖不到的部分(别误以为通过了就等于没问题):
//   - **位置与观感**:jsdom 不做布局(getBoundingClientRect 恒为 0),叶片摆在哪、
//     书有没有居中、动画好不好看,只能靠真浏览器截图(坐标见 splash.css 的注释)。
//     「整层被顶到视口外」这类 bug 同样测不到 —— 踩过一次,见 [6] 的 inset 断言。
//   - **图片真加载**:下面用替身 Image,赋 src 即算成功 —— 真实 404 的表现测不到。
//     不过组件的兜底在 onerror 上也挂了同一回调,且另有 MAX_MS 硬超时,故风险可控。

import { readFileSync, readdirSync } from 'node:fs'
import { JSDOM } from 'jsdom'

const dom = new JSDOM('<!doctype html><html><body></body></html>', {
  url: 'http://localhost/',
  pretendToBeVisual: true,
})
const win = dom.window
for (const k of ['window', 'document', 'navigator', 'HTMLElement', 'Element', 'Node', 'Event',
  'requestAnimationFrame', 'cancelAnimationFrame', 'getComputedStyle', 'MouseEvent']) {
  try {
    globalThis[k] = win[k]
  } catch {
    Object.defineProperty(globalThis, k, { value: win[k], configurable: true, writable: true })
  }
}
globalThis.self = win
globalThis.IS_REACT_ACT_ENVIRONMENT = true

// jsdom 不真的加载图片:onload/onerror 永不触发,进度条就永远是 0%,
// 「进度由图片加载驱动」这条核心逻辑也就测不到。故换掉 Image 替身:
//   - 默认赋 src 即排一个宏任务回调 onload(真实浏览器里也是并行 + 很快返回);
//   - holdImages 打开时改为攒进 pendingImgs,由 flush() 手动喂 —— 这样能停在
//     「加载到一半」的状态上断言进度条的位置,而不是只能看 0% 和 100%。
const loadedSrcs = []
const pendingImgs = []
let holdImages = false
class FakeImage {
  constructor() {
    this.onload = null
    this.onerror = null
    this.complete = false
    this.naturalWidth = 1
  }
  set src(v) {
    this._src = v
    loadedSrcs.push(v)
    if (holdImages) {
      pendingImgs.push(this)
      return
    }
    setTimeout(() => { this.complete = true; this.onload?.() }, 0)
  }
  get src() { return this._src }
}
globalThis.Image = FakeImage
win.Image = FakeImage

const { createServer } = await import('vite')
const server = await createServer({
  root: process.cwd(), logLevel: 'error',
  server: { middlewareMode: true, hmr: false },
  optimizeDeps: { noDiscovery: true },
})
const React = (await import('react')).default
const { createRoot } = await import('react-dom/client')
const { act, useState } = await import('react')
// react / react-dom 是 CJS,不能经 vite 的 ssrLoadModule(会报 module is not defined),
// 直接在 Node 侧 import;只有业务代码(含 JSX)走 vite 转换。
const { default: Splash } = await server.ssrLoadModule('/src/components/Splash.jsx')

let fail = 0
const ok = (name, cond, extra = '') => {
  if (cond) { console.log(`  ✓ ${name}`); return }
  fail++
  console.log(`  ✗ ${name}${extra ? ' —— ' + extra : ''}`)
}

// 所有等待都必须切成小步 act,不能写成一次 `act(async () => sleep(1500))`:
// React 18 的 act 把期间发生的 setState 攒在 act 队列里,**到这次 act 退出才落地**,
// 于是「睡 1.5 秒再看」读到的是 1.5 秒前的 DOM —— 组件明明已经收尾,断言却是空的。
// (这条踩过:单次长 act 下 [5] 全红,切成小步后立刻正常。)
// 小步走还顺带把 setTimeout 回调圈在 act 作用域内,不刷 act 警告。
const TICK_MS = 100
const step = (n = 1) => act(async () => { await new Promise((r) => setTimeout(r, TICK_MS * n)) })

// 轮询等待条件成立,返回从开始等到成立所花的毫秒数;超时返回 -1。
// 用它而不是「等一个固定时长」:轮询值本身就是被测的时序证据(见 [5] 的退场时长断言)。
const waitFor = async (pred, timeoutMs) => {
  for (let waited = 0; waited <= timeoutMs; waited += TICK_MS) {
    if (pred()) return waited
    await step(1)
  }
  return -1
}

const css = readFileSync('src/styles/splash.css', 'utf8')
const jsx = readFileSync('src/components/Splash.jsx', 'utf8')
const html = readFileSync('index.html', 'utf8')

const LOADING = () => document.getElementById('loading')
const barPct = () => {
  const el = document.querySelector('#loading .progress .box > div')
  return el ? parseFloat(el.style.height) || 0 : NaN
}
const classes = () => (LOADING() ? LOADING().className.split(/\s+/).filter(Boolean) : [])

// 挂一层受控 Harness:把 setReady 暴露到外面,好在「进度已满但数据没到」的时刻切 ready。
const mount = async (initialReady) => {
  const host = document.createElement('div')
  document.body.appendChild(host)
  const root = createRoot(host)
  let setReady = null
  function Harness() {
    const [ready, r] = useState(initialReady)
    setReady = r
    return React.createElement(Splash, { ready })
  }
  await act(async () => { root.render(React.createElement(Harness)) })
  return { root, setReady: (v) => act(async () => { setReady(v) }) }
}

console.log('=== 开屏动画校验 Splash ===')

// —— 1. 结构 ——
console.log('\n[1] 结构与层叠顺序')
holdImages = true // 停在加载态,便于断言
const app = await mount(false)

const STATIC_LEAVES = ['leftTop2', 'leftTop1', 'left2', 'left1', 'top4', 'top3', 'top2', 'top1',
  'rightTopLeaf3', 'rightTopLeaf2', 'rightTopLeaf1', 'rightLeaf']
const FLY_LEAVES = ['flyLeaf1', 'flyLeaf2', 'flyLeaf3', 'flyLeaf4', 'flyLeaf5', 'flyLeaf6', 'flyLeaf7', 'flyLeaf8']

ok('渲染出全屏遮罩 #loading', !!LOADING())
ok('#loading 自带 role=status 与可读名', LOADING()?.getAttribute('role') === 'status' && !!LOADING()?.getAttribute('aria-label'))
ok('两层 .center(书 / 光点各一层)', document.querySelectorAll('#loading > .center').length === 2,
  `实际 ${document.querySelectorAll('#loading > .center').length}`)

const center1 = document.querySelectorAll('#loading > .center')[0]
const center2 = document.querySelectorAll('#loading > .center')[1]
ok('第一层 24 个子元素(12 叶 + 书底 + 进度条 + 书页框 + 8 飞叶 + 光效)',
  center1.children.length === 24, `实际 ${center1.children.length}`)
const missingStatic = STATIC_LEAVES.filter((c) => center1.querySelectorAll(':scope > .' + c).length !== 1)
ok('12 片定位叶齐全', missingStatic.length === 0, missingStatic.join(' '))
const missingFly = FLY_LEAVES.filter((c) => center1.querySelectorAll(':scope > .' + c).length !== 1)
ok('8 片飞叶齐全', missingFly.length === 0, missingFly.join(' '))
ok('书底 back / 书页框 front 就位',
  !!center1.querySelector(':scope > .back > img') && !!center1.querySelector(':scope > .front > img'))
ok('进度条两根(底图 + 高亮)',
  !!center1.querySelector('.progress .box img.loadingProgress') &&
  !!center1.querySelector('.progress .box img.finishProgress'))
ok('光效容器 .light > div 存在(背景逐帧,不是 img)', !!center1.querySelector(':scope > .light > div'))

// 光点必须在**第二个** .center 里:19 个独立元素,各有关键帧 round1-19
const roundNames = Array.from({ length: 19 }, (_, i) => `round${i + 1}`)
ok('19 个光点都在第二层 .center 内', center2.children.length === 19, `实际 ${center2.children.length}`)
const missingRound = roundNames.filter((c) => center2.querySelectorAll(':scope > .' + c).length !== 1)
ok('光点类名是 round1~round19 且无重复', missingRound.length === 0, missingRound.join(' '))
ok('19 个光点共用同一张 round.png',
  new Set([...center2.querySelectorAll('img')].map((i) => i.getAttribute('src'))).size === 1)

const imgs = [...document.querySelectorAll('#loading img')]
ok('图片总数 43(12 叶 + 4 书 + 8 飞叶 + 19 光点)', imgs.length === 43, `实际 ${imgs.length}`)
const badSrc = imgs.map((i) => i.getAttribute('src')).filter((s) => !s.startsWith('/loading/'))
ok('全部图片走 /loading/ 绝对路径', badSrc.length === 0, badSrc.join(' '))
ok('进度条初始为 0%', barPct() === 0, `实际 ${barPct()}%`)
ok('加载中不带 .hide/.finish(还没到收尾)',
  !classes().includes('hide') && !classes().includes('finish'), classes().join(' '))

// —— 2. 进度条由图片加载驱动 + 素材清单与磁盘一致 ——
console.log('\n[2] 进度由图片预加载驱动')
const diskImgs = [
  ...readdirSync('public/loading').filter((f) => f.endsWith('.png')).map((f) => '/loading/' + f),
  ...readdirSync('public/loading/leaf').filter((f) => f.endsWith('.png')).map((f) => '/loading/leaf/' + f),
]
ok('预加载请求数 = 磁盘上的素材数(不多不少)', loadedSrcs.length === diskImgs.length,
  `请求 ${loadedSrcs.length} 张 / 磁盘 ${diskImgs.length} 张`)
const absent = diskImgs.filter((p) => !loadedSrcs.includes(p))
ok('预加载清单与 public/loading/ 逐张对应',
  absent.length === 0 && new Set(loadedSrcs).size === loadedSrcs.length,
  absent.join(' ') || '有重复请求')

const flush = async (n) => {
  const batch = pendingImgs.splice(0, n)
  await act(async () => { batch.forEach((im) => { im.complete = true; im.onload?.() }) })
}
await flush(9)
ok('喂到一半 → 进度条约在 50%', barPct() > 40 && barPct() < 60, `实际 ${barPct().toFixed(1)}%`)
await flush(9)
ok('全部加载完 → 进度条 100%', barPct() === 100, `实际 ${barPct()}%`)

// —— 3. ready 门控(核心)——
console.log('\n[3] 数据未就绪时不进场(核心)')
await step(15) // 1.5s,远超 MIN_MS(900) + HOLD_MS(340)
ok('ready=false 时进度满了也不播收尾',
  classes().length === 0 && !!LOADING(), `className="${classes().join(' ')}"`)
await app.setReady(true)
await waitFor(() => classes().includes('finish'), 2000)
ok('ready 转 true 后进入收尾(.hide + .finish)',
  classes().includes('hide') && classes().includes('finish'), classes().join(' '))

// —— 4. 退场时序与卸载 ——
// 这里的三个时间点卡住 EXIT_MS 的两侧:太短 → 第一条就红(动画没播完就摘),
// 太长 → 第三条红(白等)。等待都在 [3] 末尾「检测到 .finish」之后开始,
// 故「距起播」有一格(100ms)的误差,不影响判定。
console.log('\n[4] 退场时序与卸载')
await step(5) // ≈0.5s
ok('起播 0.5s 后仍在 DOM(退场未结束,不能提前摘)', !!LOADING())
await step(6) // ≈1.1s
ok('起播 1.1s 后仍在 DOM(0.7s 延时 + 1s 淡出还没走完)', !!LOADING())
await step(8) // ≈1.9s,累计 ≈2.7s > 1.7s
ok('起播 2.7s 后已自行卸载(EXIT_MS 到点)', !LOADING())
await act(async () => { app.root.unmount() })

// —— 5. reduced-motion 降级 ——
console.log('\n[5] prefers-reduced-motion 降级')
win.matchMedia = () => ({ matches: true, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {} })
holdImages = false // 这次让替身自动 onload,走「素材与数据都就绪」的最短路径
const rm = await mount(true)
await waitFor(() => classes().includes('hide'), 2000)
ok('降级时不加 .finish(整段播片跳过)', classes().includes('hide') && !classes().includes('finish'),
  classes().join(' '))
ok('降级时进度条照常走满(进度是实质信息,不降级)', barPct() === 100, `实际 ${barPct()}%`)
const goneAfter = await waitFor(() => !LOADING(), 3000)
ok('降级退场明显短于完整的 1.7s(EXIT_REDUCED_MS 生效)',
  goneAfter >= 0 && goneAfter < 1200, `实测起播后 ${goneAfter}ms 卸载`)
await act(async () => { rm.root.unmount() })
delete win.matchMedia // 还原:jsdom 本来没有它,组件也应能容忍缺失(上面 [1]~[4] 就是这种情形)

// —— 6. 与 CSS / index.html 的静态一致性 ——
console.log('\n[6] 与 splash.css / index.html 的一致性')
ok('splash.css 用项目层叠变量(不是裸 z-index: 40)', /#loading\s*\{[^}]*z-index:\s*var\(--z-loading\)/.test(css))
ok('light.png 用绝对路径(css 会被搬到 /assets/)', /url\("\/loading\/light\.png"\)/.test(css))
ok('退场期间放行指针(.hide 不再吞点击)', /#loading\.hide\s*\{[^}]*pointer-events:\s*none/.test(css))
// 这条守的是「整层跑到视口外」—— 实测踩过:position: fixed 不写 inset 时按**静态位置**落位,
// 而 <Splash> 挂在 .app 之后(App.jsx),整层就落到折叠线以下:DOM 在、43 张图全加载、
// 关键帧都挂上了,只有真浏览器截图能看出「什么都看不见」。
// jsdom 不做布局(getBoundingClientRect 恒为 0),故这里退化成样式文本断言。
ok('#loading 钉在视口上(inset: 0,不靠 DOM 顺序)', /#loading\s*\{[^}]*inset:\s*0/.test(css))

const base = /#loading\.hide\s*\{[^}]*animation:\s*loaded1\s+([\d.]+)s\s+([\d.]+)s/.exec(css)
const cssExit = base ? (parseFloat(base[1]) + parseFloat(base[2])) * 1000 : NaN
const jsExit = +(/const EXIT_MS = (\d+)/.exec(jsx)?.[1] || NaN)
ok('EXIT_MS 与 CSS 的「1s 淡出 + 0.7s 延时」同步(1.7s)', cssExit === jsExit, `CSS ${cssExit}ms / JS ${jsExit}ms`)

const rmStart = css.indexOf('@media (prefers-reduced-motion: reduce)')
const rmBody = rmStart < 0 ? '' : css.slice(rmStart, css.indexOf('\n}', rmStart))
ok('存在 reduced-motion 块', rmStart >= 0)
ok('降级块覆盖整层淡出与首层淡出',
  /#loading\.hide\s*\{/.test(rmBody) && /#loading\.hide \.center:first-child\s*\{/.test(rmBody))
ok('降级块还兜住了播片(万一有人手加了 .finish)',
  /#loading\.finish\.hide[\s\S]*animation:\s*none/.test(rmBody))

const jsReduced = +(/const EXIT_REDUCED_MS = (\d+)/.exec(jsx)?.[1] || NaN)
const rmAnim = /#loading\.hide\s*\{\s*animation:\s*loaded1\s+([\d.]+)s/.exec(rmBody)
const cssReduced = rmAnim ? parseFloat(rmAnim[1]) * 1000 : NaN
ok('EXIT_REDUCED_MS ≥ CSS 的 .3s 且留少量余量(动画播完再摘)',
  jsReduced >= cssReduced && jsReduced - cssReduced <= 100, `CSS ${cssReduced}ms / JS ${jsReduced}ms`)

// 缩放分档:窄屏沿用原站基线 0.5,桌面按视口放大。
// 这条守的是「别把桌面档位删掉」—— 原站是手机站,0.5 是给手机画布定的,桌面照搬时
// 合成体(设计尺寸约 364×516)只剩 182×258px,1920 宽的屏上像没加载完。
ok('保留原站基线 0.5(手机画布)',
  /#loading \.center\s*\{[^}]*transform:\s*scale\(0\.5\)/.test(css))
ok('桌面按视口放大(≥761px 主断点 + 高度分档)',
  /@media \(min-width: 761px\) and \(min-height: 620px\)/.test(css) &&
  /@media \(min-width: 761px\) and \(min-height: 880px\)/.test(css) &&
  /scale\(0\.72\)/.test(css) && /scale\(0\.86\)/.test(css))
const preloads = [...html.matchAll(/<link rel="preload" href="\/loading\/[^"]+" as="image"/g)]
ok('index.html preload 了门面三图(书底/书页框/进度条)', preloads.length === 3, `实际 ${preloads.length}`)

await server.close()
console.log(fail === 0 ? '\n✓ 全部通过' : `\n✗ ${fail} 项未通过`)
process.exit(fail === 0 ? 0 : 1)
