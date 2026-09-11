// 「长按 → 蛋组」这条链在**页面层**的验收:宠物列表菜单 + 筛选面板 + 培育页深链。
//
//   node scripts/verify-egggroup-ui.mjs
//
// 为什么纯函数脚本(verify-ctx-menu / verify-breeding-newlink)之外还要这一层:纯函数对了不等于
// **接上了**。这一层真渲染 PetList / Breeding,验的正是接线:
//   - 右键宠物卡弹出的菜单里到底有什么,点「筛选相同蛋组」之后**发出去的请求**带的是不是
//     eggGroupsExact=官方组名(只在内存里改个状态、参数名发错,纯函数都测不出来);
//   - 面板上真的出现这条精确条件、能一键清除,且界面上已经找不到「未发现」这个词;
//   - /breeding?new=<gid> 进去后新建表单已展开、品种候选被收窄、♀ 的品种已预选、能解除限制。
//
// 这些错都是「点了没反应 / 发错参数」型的:不报错、不红,只有渲染出来并盯住请求才看得见。
//
// ⚠️ 已做变异测试(每条断言都验证过会红):
//    - ContextMenu 发 eggGroups(OR)而不是 eggGroupsExact → 请求参数断言红
//    - BreedRule 的收窄不过滤                         → 「候选被收窄」断言红
//    - 面板不渲染 .filter-exact                        → 精确条件可见/可清除断言红
//    - PetCard/badges 不映射「无蛋组」                  → 「界面上没有未发现」断言红
import { readFileSync } from 'node:fs'
import { createServer } from 'vite'
import { JSDOM } from 'jsdom'

const golden = (n) => JSON.parse(
  readFileSync(new URL(`../../internal/server/testdata/contract/${n}.json`, import.meta.url), 'utf8'),
)

// —— jsdom 环境 ——
const dom = new JSDOM('<!doctype html><html><body><div id="root"></div></body></html>', {
  url: 'http://localhost:5173/', pretendToBeVisual: true,
})
const win = dom.window
const KEYS = ['window', 'document', 'navigator', 'localStorage', 'sessionStorage', 'HTMLInputElement',
  'HTMLElement', 'Element', 'Node', 'MouseEvent', 'Event', 'getComputedStyle',
  'requestAnimationFrame', 'cancelAnimationFrame']
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
globalThis.IS_REACT_ACT_ENVIRONMENT = false
class FakeES { constructor(url) { this.url = url } addEventListener() {} removeEventListener() {} close() {} }
globalThis.EventSource = FakeES
// jsdom 缺口:下拉量空间要用 elementFromPoint,高亮项要 scrollIntoView(见 verify-breeding-page.mjs)。
win.document.elementFromPoint = () => null
win.Element.prototype.scrollIntoView = () => {}
// api.js 在模块加载时就读它(见 api.js 顶部),故必须在 ssrLoadModule 之前写好。
win.localStorage.setItem('account', 'UID:1')

// —— 假后端:按路径给数据,并记录每次请求的 URL ——
const basePets = golden('pets').pets
const PET_EGGY = { ...basePets[0], eggGroups: [{ id: 4, name: '天空', desc: '天空' }, { id: 13, name: '龙', desc: '龙' }] }
const PET_NONE = { ...basePets[1], gid: 1002, eggGroups: [{ id: 1, name: '未发现', desc: '未发现' }] }
const PET_BARE = { ...basePets[1], gid: 1003, name: '超进化的样子', eggGroups: [] }
const PETS = { total: 3, pets: [PET_EGGY, PET_NONE, PET_BARE] }
// 培育页的品种候选:一条与「天空/龙」共组、一条完全不搭、一条链首没配蛋组(宁多勿漏要保留)。
const CHAINS = {
  chains: [
    { evo: 3003, species: '火花', base: 3003, label: '火花（焰火/火神/烈火战神）', img: 'HeadIcon/3003.webp', count: 1, eggGroups: ['天空', '龙'] },
    { evo: 4001, species: '水蓝蓝', base: 4001, label: '水蓝蓝（波波拉/水灵）', img: 'HeadIcon/4001.webp', count: 2, eggGroups: ['海洋'] },
    { evo: 5001, species: '无组猫', base: 5001, label: '无组猫', img: 'HeadIcon/5001.webp', count: 1 },
  ],
}
// 长按的那只:♀ 火神(当前形态 petbase 3006),它的进化链阶段表用于认品种(链首 3003)。
const BREED_PET = { ...PET_EGGY, gid: 1001, gender: '♀', baseConfId: 3006 }
const STEPS = [
  { petbase: 3003, name: '火花', stage: 1, book: 5 },
  { petbase: 3032, name: '焰火', stage: 2, book: 6 },
  { petbase: 3006, name: '火神', stage: 3, book: 7 },
]

let FIXTURES = {}
let REQS = []
globalThis.fetch = async (url) => {
  const raw = String(url)
  REQS.push(raw)
  const path = raw.split('?')[0]
  const body = FIXTURES[path]
  if (!body) return { ok: false, status: 404, json: async () => ({}), text: async () => 'not found' }
  return { ok: true, status: 200, json: async () => body, text: async () => JSON.stringify(body) }
}

const R = []
const check = (name, ok, detail = '') => {
  R.push({ name, ok, detail })
  console.log(`${ok ? '✓' : '✗'} ${name}${ok || !detail ? '' : `  —— ${detail}`}`)
}
const tick = (ms = 250) => new Promise((r) => setTimeout(r, ms))

const server = await createServer({
  root: process.cwd(), logLevel: 'error',
  server: { middlewareMode: true, hmr: false }, optimizeDeps: { noDiscovery: true },
})
const React = (await import('react')).default
const { createRoot } = await import('react-dom/client')
const { MemoryRouter, Routes, Route } = await import('react-router-dom')
const { AccountContext, IconsContext } = await server.ssrLoadModule('/src/context.js')
const PetList = (await server.ssrLoadModule('/src/pages/pet-list/PetList.jsx')).default
const Breeding = (await server.ssrLoadModule('/src/pages/breeding/Breeding.jsx')).default

const doc = win.document
const text = () => doc.body.textContent || ''
const ctxMenuItems = () => [...doc.querySelectorAll('.ctx-menu .ctx-item')].map((el) => el.textContent.trim())
const clickText = (sel, label) => {
  const el = [...doc.querySelectorAll(sel)].find((x) => x.textContent.trim() === label)
  if (el) el.dispatchEvent(new win.MouseEvent('click', { bubbles: true }))
  return !!el
}
const openMenuOn = (card) => card.dispatchEvent(new win.MouseEvent('contextmenu', {
  bubbles: true, cancelable: true, clientX: 40, clientY: 40,
}))

// ================= A. 宠物列表:菜单 + 面板 =================
FIXTURES = {
  '/api/pets': PETS,
  '/api/filter-options': golden('filter-options'),
  '/api/name-options': golden('name-options'),
  '/api/boxes': golden('boxes'),
  '/api/teams': golden('teams'),
}
const rootA = createRoot(doc.getElementById('root'))
rootA.render(React.createElement(AccountContext.Provider, { value: 'UID:1' },
  React.createElement(IconsContext.Provider, { value: { stat: {} } },
    React.createElement(MemoryRouter, { initialEntries: ['/'] },
      React.createElement(Routes, null,
        React.createElement(Route, { path: '/', element: React.createElement(PetList) }))))))
await tick(400)

const cards = [...doc.querySelectorAll('.pet-card')]
check('列表渲染出三只宠物', cards.length === 3, `找到 ${cards.length} 张卡`)

// 「无蛋组」的显示口径:卡片上写着「蛋组 无蛋组」,而官方名「未发现」不该出现在界面上。
check('卡片把「未发现」显示成「无蛋组」', text().includes('蛋组 无蛋组'), doc.querySelector('.pt-meta')?.textContent)
check('界面上找不到「未发现」', !text().includes('未发现'), '页面正文里还有「未发现」')
const chipTexts = [...doc.querySelectorAll('.chip-egg')].map((el) => el.textContent.trim())
check('筛选面板有「无蛋组」chip', chipTexts.includes('无蛋组'), chipTexts.join('/'))
check('筛选面板没有「未发现」chip', !chipTexts.includes('未发现'), chipTexts.join('/'))
check('蛋组 chip 仍是 15 个', chipTexts.length === 15, `${chipTexts.length} 个`)

// 右键有蛋组的宠物:菜单里应有蛋组两项,且没有已经删掉的「复制编号」。
openMenuOn(cards[0])
await tick(60)
const items = ctxMenuItems()
check('菜单含「筛选相同蛋组」「孵蛋配种」', items.includes('筛选相同蛋组') && items.includes('孵蛋配种'), items.join('/'))
check('菜单里没有「复制编号」', !items.includes('复制编号'), items.join('/'))

// 点「筛选相同蛋组」:写进筛选的必须是**精确口径 + 官方组名**,请求也得带出去。
REQS = []
clickText('.ctx-menu .ctx-item', '筛选相同蛋组')
await tick(300)
const stored = JSON.parse(win.sessionStorage.getItem('petListFilter') || '{}')
check('筛选状态写入 eggGroupsExact(官方组名)',
  JSON.stringify(stored.eggGroupsExact) === JSON.stringify(['天空', '龙']) && (stored.eggGroups || []).length === 0,
  JSON.stringify(stored))
const petsReq = [...REQS].reverse().find((u) => u.startsWith('/api/pets'))
check('请求带上 eggGroupsExact=天空,龙',
  !!petsReq && decodeURIComponent(petsReq).includes('eggGroupsExact=天空,龙'),
  petsReq || '(没有发请求)')

// 面板上这条精确条件要看得见、清得掉。
const exactEl = doc.querySelector('.filter-exact')
check('面板显示「蛋组完全相同」条件', !!exactEl && exactEl.textContent.includes('天空') && exactEl.textContent.includes('龙'),
  exactEl ? exactEl.textContent.trim() : '(没有这条)')
check('精确条件计入已选计数', (doc.querySelector('.filter-status')?.textContent || '').includes('已选 1 项'),
  doc.querySelector('.filter-status')?.textContent)
REQS = []
check('条件上有清除入口', clickText('.filter-exact .fe-clear', '清除'))
await tick(300)
check('清除后面板不再显示该条件', !doc.querySelector('.filter-exact'), doc.querySelector('.filter-exact')?.textContent)
const afterClear = [...REQS].reverse().find((u) => u.startsWith('/api/pets'))
check('清除后请求不再带 eggGroupsExact', !!afterClear && !afterClear.includes('eggGroupsExact'), afterClear || '(没有发请求)')

// 无蛋组的宠物:两种情形都不该出现蛋组两项(这是「长按没有这个选项」那条需求)。
for (const [idx, label] of [[1, '蛋组是「未发现」'], [2, '没有蛋组']]) {
  openMenuOn([...doc.querySelectorAll('.pet-card')][idx])
  await tick(60)
  const m = ctxMenuItems()
  check(`${label}:菜单不出蛋组两项`, !m.includes('筛选相同蛋组') && !m.includes('孵蛋配种'), m.join('/'))
  check(`${label}:其余项照旧`, m.length === 4 && m[0] === '查看详情', m.join('/'))
  doc.dispatchEvent(new win.MouseEvent('click', { bubbles: true }))
  await tick(30)
}
rootA.unmount()

// ================= B. 培育页深链 ?new=<gid> =================
doc.getElementById('root').innerHTML = ''
FIXTURES = {
  '/api/breeding': golden('breeding'),
  '/api/name-options': golden('name-options'),
  '/api/filter-options': CHAINS,
  '/api/pets/1001': BREED_PET,
  '/api/evolution': STEPS,
}
const rootB = createRoot(doc.getElementById('root'))
rootB.render(React.createElement(AccountContext.Provider, { value: 'UID:1' },
  React.createElement(IconsContext.Provider, { value: { stat: {} } },
    React.createElement(MemoryRouter, { initialEntries: ['/?new=1001'] },
      React.createElement(Routes, null,
        React.createElement(Route, { path: '/', element: React.createElement(Breeding) }))))))
await tick(500)

check('深链进来就展开了「新建培育线」', !!doc.querySelector('.br-new'), '表单没展开')
const hint = [...doc.querySelectorAll('.br-hint')].map((el) => el.textContent.trim()).find((t) => t.includes('已按蛋组收窄'))
check('表单说明写出了收窄的蛋组', !!hint && hint.includes('天空') && hint.includes('龙'), hint || '(没有这句)')
check('表单说明写出了剩下几个品种', !!hint && hint.includes('共 2 个'), hint || '(没有这句)')
// 2 个 = 共蛋组的「火花」 + 链首没配蛋组的「无组猫」(宁多勿漏),不搭的「水蓝蓝」被滤掉。
check('不搭的品种已从候选里滤掉', !hint || !hint.includes('共 3 个'), hint || '')
check('♀ 的品种已预选好', (doc.querySelector('.combo-input')?.value || '') === '火花（焰火/火神/烈火战神）',
  doc.querySelector('.combo-input')?.value || '(输入框为空)')
check('限制可一键解除', clickText('.br-new .btn', '不限蛋组'))
await tick(200)
const hintGone = ![...doc.querySelectorAll('.br-hint')].map((el) => el.textContent.trim()).some((t) => t.includes('已按蛋组收窄'))
check('解除后收窄说明消失', hintGone, '说明还在')
check('解除后预选的品种还在(蛋随母本,不该被抹掉)',
  (doc.querySelector('.combo-input')?.value || '') === '火花（焰火/火神/烈火战神）',
  doc.querySelector('.combo-input')?.value || '(输入框为空)')
rootB.unmount()

await server.close()
let bad = R.filter((r) => !r.ok).length
console.log(`\n${bad ? `✗ ${bad}/${R.length} 条不符` : `✓ 全部 ${R.length} 条通过`}`)
process.exit(bad ? 1 : 0)
