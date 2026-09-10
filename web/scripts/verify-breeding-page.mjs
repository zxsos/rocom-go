// 用 jsdom + Vite SSR 真实渲染培育页,验三件只能「看内容」才能发现的事:
//   1. 品种是**可输入**的组合下拉,且候选是**整条进化链**(「火花（焰火/火神/烈火战神）」)——
//      打字即弹匹配项,不匹配的被滤掉
//   2. 目标性格支持「性格正面加某维」—— 6 个维度 chip,且契约样本那条线的目标落在「加物攻」上
//   3. 前 5 对里每只亲本都带自己的性格(选种时要看的第三个维度)
//   4. 卡片头像:一代都还没有的线退回这个品种的蛋(此前是留一个空占位框)
//
// 为什么用契约 golden 而不是真后端:这三条都是「渲染出来了、但内容不对」看不见的那类 ——
// 页面多一个字少一个字都不报错,只有比对内容才能验;而 golden 正是后端真实样本(见
// docs/api/README.md,它同时也是生成本仓库 fields.json 的输入),不需要跑后端、不需要数据。
//
//   node scripts/verify-breeding-page.mjs

import { readFileSync } from 'node:fs'
import { createServer } from 'vite'
import { JSDOM } from 'jsdom'

const golden = (n) => JSON.parse(
  readFileSync(new URL(`../../internal/server/testdata/contract/${n}.json`, import.meta.url), 'utf8'),
)
const RESPONSES = {
  '/api/breeding': golden('breeding'),
  '/api/name-options': golden('name-options'),
  '/api/filter-options': golden('filter-options'),
}

// —— jsdom 环境 ——
const dom = new JSDOM('<!doctype html><html><body><div id="root"></div></body></html>', {
  url: 'http://localhost:5173/', pretendToBeVisual: true,
})
const win = dom.window
// api.js 在模块加载时就读 localStorage,故必须先接上再 ssrLoadModule。
const KEYS = ['window', 'document', 'navigator', 'localStorage', 'HTMLInputElement',
  'MouseEvent', 'Event', 'getComputedStyle', 'requestAnimationFrame', 'cancelAnimationFrame']
for (const k of KEYS) {
  if (win[k] === undefined) continue
  try {
    globalThis[k] = win[k]
  } catch {
    Object.defineProperty(globalThis, k, { value: win[k], configurable: true, writable: true })
  }
}
win.matchMedia = (q) => ({
  matches: false, media: q, addEventListener() {}, removeEventListener() {},
  addListener() {}, removeListener() {}, dispatchEvent: () => false,
})
globalThis.matchMedia = win.matchMedia
// 培育页只订阅 breeding 那一路推送;这里给个不连任何东西的空壳(不连就不会触发刷新)。
class FakeES { constructor(url) { this.url = url } addEventListener() {} removeEventListener() {} close() {} }
globalThis.EventSource = FakeES
// jsdom 缺这两个,而 useDropdown 各用一处:量「下方空间够不够」要 elementFromPoint,
// 高亮项滚动要 scrollIntoView。缺了会在 layout effect 里直接抛错 —— 菜单整个渲染不出来,
// 看着像「输入框没反应」。这是 jsdom 的缺口,不是页面的问题。
win.document.elementFromPoint = () => null
win.Element.prototype.scrollIntoView = () => {}

// 相对路径的 /api/* 打到这里来;未知路径返回 404(页面会显示错误态,正好能被断言抓到)。
globalThis.fetch = async (url) => {
  const path = String(url).split('?')[0]
  const body = RESPONSES[path]
  if (!body) return { ok: false, status: 404, json: async () => ({}), text: async () => 'not found' }
  return { ok: true, status: 200, json: async () => body, text: async () => JSON.stringify(body) }
}

// React 的报错只在 console.error 里,不接住就会「渲染成空壳但脚本全绿」。
const errors = []
const origErr = console.error
console.error = (...a) => { errors.push(a.map(String).join(' ')) }

const results = []
const check = (name, cond, detail = '') => {
  results.push({ name, ok: cond, detail })
  console.log(`${cond ? '✅' : '❌'} ${name}${detail ? '  ' + detail : ''}`)
}
const tick = (ms = 60) => new Promise((r) => setTimeout(r, ms))
const textOf = (el) => (el ? (el.textContent || '').trim() : '')

const server = await createServer({
  root: process.cwd(), logLevel: 'error',
  server: { middlewareMode: true, hmr: false },
  optimizeDeps: { noDiscovery: true },
})

// react / react-dom / router 是 CJS,只能直接在 Node 侧 import;业务代码(含 JSX)走 vite。
const React = (await import('react')).default
const { createRoot } = await import('react-dom/client')
const { MemoryRouter, Routes, Route } = await import('react-router-dom')
const Breeding = (await server.ssrLoadModule('/src/pages/breeding/Breeding.jsx')).default
const { AccountContext } = await server.ssrLoadModule('/src/context.js')

// 直接开契约样本那条线的详情(跳过列表页):建议、目标编辑器、回交对比都在这一屏。
const root = createRoot(win.document.getElementById('root'))
root.render(
  React.createElement(AccountContext.Provider, { value: 'UID:1' },
    React.createElement(MemoryRouter, { initialEntries: ['/?line=contract-line'] },
      React.createElement(Routes, null,
        React.createElement(Route, { path: '/', element: React.createElement(Breeding) })))),
)
await new Promise((r) => setTimeout(r, 1500))
const doc = win.document

// —— 3. 前 5 对里每只亲本自己的性格 ——
// 契约样本里三组建议 × 母/父两张卡 = 6 个性格,且四只宠都是「固执」。
const sugNats = [...doc.querySelectorAll('.br-sug-nat')]
check('建议行里每只亲本都带性格', sugNats.length === 6, `找到 ${sugNats.length} 个 .br-sug-nat`)
check('亲本性格取自快照', sugNats.every((el) => textOf(el) === '固执'),
  [...new Set(sugNats.map(textOf))].join('/'))

// —— 2. 目标性格的「正面加某维」 ——
const dims = [...doc.querySelectorAll('.br-natdim')]
check('性格维度 chip 有 6 个', dims.length === 6, dims.map(textOf).join(' '))
check('维度标签按 6 维顺序', dims.map(textOf).join('') === '+生命+物攻+魔攻+物防+魔防+速度',
  dims.map(textOf).join(' '))
// 契约样本的 goal 是 natureIn = 物攻那一行 → 该 chip 应处于选中的 on 态,且只有一个
const onDims = dims.filter((el) => el.className.includes('on'))
check('样本目标(物攻那一行)对应维度被点亮',
  onDims.length === 1 && textOf(onDims[0]) === '+物攻',
  onDims.map(textOf).join(' ') || '一个都没点亮')
// 建议行与回交对比里的性格目标都应显示成「加物攻」而不是 5 个名字
check('目标性格显示成「加物攻」', (doc.body.textContent || '').includes('加物攻'),
  (doc.body.textContent || '').includes('加物攻') ? '' : '页面上找不到「加物攻」')
check('后端没有把一组名字散在页面上', !doc.querySelector('.br-sug-nature')?.textContent.includes('勇敢'),
  textOf(doc.querySelector('.br-sug-nature')))

// —— 1. 品种是可输入的组合下拉(在**列表页**的新建表单里)——
// 详情页的工具栏只有「← 全部培育线」,「新建培育线」在列表页 —— 换个路由重挂一次。
root.unmount()
createRoot(win.document.getElementById('root')).render(
  React.createElement(AccountContext.Provider, { value: 'UID:1' },
    React.createElement(MemoryRouter, { initialEntries: ['/'] },
      React.createElement(Routes, null,
        React.createElement(Route, { path: '/', element: React.createElement(Breeding) })))),
)
await new Promise((r) => setTimeout(r, 300))
const byText = (sel, t) => [...doc.querySelectorAll(sel)].find((el) => textOf(el).includes(t))
byText('button', '新建培育线')?.dispatchEvent(new win.MouseEvent('click', { bubbles: true }))
await tick()
const combo = doc.querySelector('.combo-input')
check('品种是输入框(不再是只能滚的下拉)', !!combo, combo ? `placeholder=${combo.placeholder}` : '没找到 .combo-input')

if (combo) {
  // React 受控输入:必须走原生 setter 再派发 input 事件,直接改 value 不会被 React 认到。
  const setValue = Object.getOwnPropertyDescriptor(win.HTMLInputElement.prototype, 'value').set
  setValue.call(combo, '火')
  combo.dispatchEvent(new win.Event('input', { bubbles: true }))
  await tick()
  const items = [...doc.querySelectorAll('.dropdown-menu .dropdown-item')].map(textOf)
  check('输入关键字即弹候选', items.length > 0, items.join(' / '))
  // 候选是**整条链**,不是单个形态名:选一次就覆盖链上全部阶段(见 gamedata.ChainOptions),
  // 故这里断言「含火神的那条链在、其余(喵喵那条)被滤掉」—— 只断言文字里有「火神」的话,
  // 退回按形态名列(升级前的口径)也照样绿。
  check('候选按关键字过滤,且候选是整条链(含火神的链留下、喵喵的链滤掉)',
    items.length > 0 && items.every((t) => t.includes('火')) && items.some((t) => t.includes('火神')),
    items.join(' / '))
  // 候选行左侧的图必须是**这个品种的蛋**(不是链首头像,见 Breeding.jsx 的 chainItems):
  // 同名多形态的品种文字完全一样,图是唯一能确认选的是哪一条的东西,而它同时就是这条线要孵的蛋。
  // 期望值取自 golden 里**这几个候选**各自的 egg 字段(不写死文件名,否则换数据这条断言就成噪音);
  // 头像 src 是 HeadIcon,与它不等 —— 拿错图(退回头像)会被抓出来。
  const shownGold = golden('filter-options').chains.filter(
    (c) => c.egg && items.some((t) => t.includes(c.label)),
  )
  const avs = [...doc.querySelectorAll('.dropdown-menu .dropdown-item img.dropdown-av')]
  check('候选行带图,且图就是这个品种的蛋',
    shownGold.length > 0 && items.length > 0 && avs.length === items.length
    && avs.every((im) => shownGold.some((c) => im.getAttribute('src') === '/img/' + c.egg)),
    avs.map((im) => im.getAttribute('src')).join(' ') || '一个 .dropdown-av 都没渲染')
  setValue.call(combo, '不存在')
  combo.dispatchEvent(new win.Event('input', { bubbles: true }))
  await tick()
  check('搜不到时的空态不是空白', (textOf(doc.querySelector('.dropdown-empty')) || '').includes('不存在'),
    textOf(doc.querySelector('.dropdown-empty')))
}

// —— 4. 卡片头像:一代都还没有的线退回这个品种的蛋 ——
// 手动建的线没有亲本快照可拿,此前留一个空的占位框,看着像这条线没建全(见 pets.lineAvatar)。
// 卡片此刻被新建表单盖住了(上面刚点开),故直接验那个纯函数 —— 卡片要用的就是它返回的那张图。
const { lineAvatar } = await server.ssrLoadModule('/src/pages/breeding/pets.js')
const gChains = golden('filter-options').chains
const c0 = gChains[0]
const emptyLine = { evo: c0.evo, species: c0.species, gens: [], pending: [] }
check('代数为空的线,卡片头像用这个品种的蛋',
  !!c0 && lineAvatar(emptyLine, gChains) === c0.egg,
  c0 ? String(lineAvatar(emptyLine, gChains)) : '样本里没有品种')
// 有母本快照的线仍用母本:那才是这条线真正在用的种母,比一颗蛋具体得多。
const momLine = { evo: 0, species: '无此品种', gens: [{ mother: { img: 'HeadIcon/1.webp' } }] }
check('有母本快照的线仍用母本头像', lineAvatar(momLine, gChains) === 'HeadIcon/1.webp',
  String(lineAvatar(momLine, gChains)))

check('渲染过程没有 React 报错', errors.length === 0, errors.slice(0, 2).join(' | '))

console.error = origErr
await server.close()
const bad = results.filter((r) => !r.ok)
console.log(`\n${results.length - bad.length}/${results.length} 项通过`)
process.exit(bad.length ? 1 : 0)
