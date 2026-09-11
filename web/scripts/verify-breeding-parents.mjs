// 培育线的「亲本」(种母 × 种公)在页面层的验收:建线表单可选、长按预置、详情成对展示、补录默认值。
//
//   node scripts/verify-breeding-parents.mjs
//
// 为什么要有这一层(纯函数与接口测试管不到这些):
//   - 建线表单里那两位现在**顺序不设限**(没定品种也能先挑),候选来自全库还是某个品种,
//     只有真渲染出来看下拉里有什么才知道;
//   - 选了种母有没有把「品种(蛋)」自动带出来?——蛋随母本是事实,漏了这一步玩家得自己再找一遍;
//   - 种公候选有没有按所选母本的蛋组收窄?——收窄错了不会报错,只会列出一堆配不上的公;
//   - 点「建线」发出去的 body 到底带没带 motherGid / fatherGid?——前端拼错一个键名,
//     后端只会当没传,线建出来就是「两位都没指定」,而页面上完全看不出来;
//   - 长按「孵蛋配种」进来时,那只宠物有没有真的占住对应的一侧并标注「你长按的那只」;
//   - 「学院小窝」勾选有没有真的落在亲本卡上、勾另一张卡会不会把窝换过去(小窝全库唯一)。
//
// ⚠️ 已做变异测试(每条断言都验证过会红):
//   - 建线 body 里把 fatherGid 去掉            → 提交断言红
//   - 种公候选不按母本蛋组收窄(fatherCandidates 退化成不过滤) → 收窄断言红
//   - 长按预置只处理 ♀(♂ 不占位)              → ♂ 预置断言红
//   - 详情里 father 快照不读 fatherValue        → 成对展示断言红
//   - 选种母后不写 newChain                     → 「品种自动填好」断言红
//   - 详情勾选改回**仍调 POST /api/nest**(不写计划值)→ 「详情勾种母带 nestPlanGid」「勾选
//     不再调 /api/nest」「写明按计划试算」「换勾种公带 2003」4 条断言红(2026-09 实变)
//   - 状态行态 4 丢掉计划名(只剩真值那句)→ 「状态行同现真值 + 计划名」「计划已不在库写
//     已不在库的那只」2 条断言红(2026-09 实变)
//   - 去掉真值锁定的禁用(lockedByTruth 恒 false)→ 「真值那只的勾被禁用」断言红(2026-09 实变)
//   - 表单里状态行也总给「记为游戏真值」(去掉 onCommitTruth 判断)→ 「建线表单的状态行不给
//     记为游戏真值」断言红(2026-09 实变)
//   - 选中时不给卡片加 nest-picked           → 「勾上的那张卡带 nest-picked」「真值那只同样
//     长草」2 条断言红(2026-09 实变)
//   - 不给 PetPicker 传 avFrame(头像不挂草环)→ 「草环挂在头像容器里」「草环是三股绳 + 一圈
//     纤维」2 条断言红(2026-09 实变)
//   - 改掉结论那行的类名(br-nest-note → 别的)→ 「结论那句在按钮外面」断言红(2026-09 实变;
//     它守的是「那句话不能塞回胶囊里」,塞回去胶囊就被撑成整条横幅了)
import { createServer } from 'vite'
import { JSDOM } from 'jsdom'

// —— jsdom 环境 ——
const dom = new JSDOM('<!doctype html><html><body><div id="root"></div></body></html>', {
  url: 'http://localhost:5173/', pretendToBeVisual: true,
})
const win = dom.window
for (const k of ['window', 'document', 'navigator', 'localStorage', 'sessionStorage', 'HTMLInputElement',
  'HTMLElement', 'Element', 'Node', 'MouseEvent', 'Event', 'getComputedStyle',
  'requestAnimationFrame', 'cancelAnimationFrame']) {
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
win.document.elementFromPoint = () => null
win.Element.prototype.scrollIntoView = () => {}
win.localStorage.setItem('account', 'UID:1')

// —— 假后端 ——
// 两条链:火花(天空/龙)、水蓝蓝(海洋)。候选池按品种给三类候选;其中父本有两只,
// 只有一只与「天空/龙」共组 —— 收窄与否在数量上就分得出来(2 只 vs 1 只)。
const CHAINS = {
  chains: [
    { evo: 3003, species: '火花', base: 3003, label: '火花（焰火/火神/烈火战神）', count: 1, eggGroups: ['天空', '龙'] },
    { evo: 4001, species: '水蓝蓝', base: 4001, label: '水蓝蓝（波波拉/水灵）', count: 2, eggGroups: ['海洋'] },
  ],
}
const mkCand = (gid, name, gender, eggGroups, evo, species, voice = 10) => ({
  gid, name, species, evo, gender, voice, nature: '固执', weightPct: 50, img: `HeadIcon/${gid}.webp`, eggGroups,
})
const FIRE_MOM_A = mkCand(2001, '小母甲', '♀', ['天空', '龙'], 3003, '火神', 40)
const FIRE_MOM_B = mkCand(2002, '小母乙', '♀', ['天空', '龙'], 3003, '火神', -20)
const FIRE_DAD_A = mkCand(2003, '小公乙', '♂', ['天空', '龙'], 3003, '火神', 60)
const FIRE_DAD_B = mkCand(2004, '小公丙', '♂', ['海洋'], 3003, '火神', 30)
const WATER_MOM = mkCand(2101, '水母甲', '♀', ['海洋'], 4001, '水蓝蓝', 10)
const WATER_DAD = mkCand(2102, '水公乙', '♂', ['海洋'], 4001, '水蓝蓝', 20)
// 「还没定品种」那一份:前端会把 evo=0、species 空串一起发出来(buildQuery 只跳过空串,0 会带上),
// 键就按它实际发出来的样子写 —— 写成 '' 的话请求会落进 404,候选池空掉而看不出原因。
const ALL_POOL = { evo: 0, species: '', mothers: [FIRE_MOM_A, FIRE_MOM_B, WATER_MOM], fathers: [FIRE_DAD_A, FIRE_DAD_B, WATER_DAD], kids: [] }
const POOLS = {
  'evo=0': ALL_POOL,
  'evo=3003': { evo: 3003, species: '火花', mothers: [FIRE_MOM_A, FIRE_MOM_B], fathers: [FIRE_DAD_A, FIRE_DAD_B], kids: [] },
  'evo=4001': { evo: 4001, species: '水蓝蓝', mothers: [WATER_MOM], fathers: [WATER_DAD], kids: [] },
}
const snap = (gid, name, gender) => ({ gid, name, species: '火神', gender, voice: 10, nature: '固执' })
// 学院小窝(全库唯一一只):{gid,name,nature},gid=0 表示空着。它跟着 /api/breeding 一起下发,
// 改动走 POST /api/nest —— 假后端也必须**只存一只**:要是收下两只,「勾另一张卡就是把窝换过去」
// 这条断言就成了假过(真后端那边由单行表保证,见 store/academy.go)。
// 它是**游戏真值**(家园管线自动维护),与「本线计划」(线的 nestPlanGid)是两回事,断言要分开看。
let NEST = { gid: 0 }
const NEST_NAMES = { 2001: '小母甲', 2003: '小公乙' }
const NATURE_OF = { 2001: '固执', 2003: '胆小' }
// 真值只有设了才带 name/nature(见下面 switchTruth);gid≠0 但查不到名字 = 那只已不在库。
const switchTruth = (on) => {
  NEST = on ? { gid: 2001, name: NEST_NAMES[2001], nature: NATURE_OF[2001] } : { gid: 0 }
}
// 计划那只的快照(与真实后端一样由 gid 派生):查得到给 snapshot,查不到置 nestPlanReleased。
const PLAN_GIDS = { 2001: snap(2001, '小母甲', '♀'), 2003: snap(2003, '小公乙', '♂'), 2101: snap(2101, '水母甲', '♀') }
const LINES = {
  lines: [
    {
      id: 'L-with-father', evo: 3003, species: '火神', confId: 3006, chainName: '火花（焰火/火神/烈火战神）',
      status: 'active', goal: {}, gens: [], pending: [],
      mother: snap(2001, '小母甲', '♀'), father: snap(2003, '小公乙', '♂'),
      createdAt: 1, updatedAt: 2,
    },
    {
      id: 'L-released-father', evo: 3003, species: '火神', confId: 3006, chainName: '火花（焰火/火神/烈火战神）',
      status: 'active', goal: {}, gens: [], pending: [],
      mother: snap(2002, '小母乙', '♀'), fatherGid: 2999, fatherReleased: true,
      createdAt: 1, updatedAt: 2,
    },
    // 这条线带着一个**存在**的计划(2003):用于「有计划、与真值不同」那几态。
    {
      id: 'L-plan', evo: 3003, species: '火神', confId: 3006, chainName: '火花（焰火/火神/烈火战神）',
      status: 'active', goal: {}, gens: [], pending: [], nestPlanGid: 2003,
      mother: snap(2001, '小母甲', '♀'), father: snap(2003, '小公乙', '♂'),
      createdAt: 1, updatedAt: 2,
    },
    // 这条线计划的那只已不在库(2999 不在 PLAN_GIDS 里):nestPlan 缺失但 nestPlanReleased=true,
    // 状态行必须写「已不在库的那只」而不是留空。
    {
      id: 'L-plan-released', evo: 3003, species: '火神', confId: 3006, chainName: '火花（焰火/火神/烈火战神）',
      status: 'active', goal: {}, gens: [], pending: [], nestPlanGid: 2999,
      mother: snap(2002, '小母乙', '♀'), father: snap(2003, '小公乙', '♂'),
      createdAt: 1, updatedAt: 2,
    },
  ],
}
// nestView 把线上存的 nestPlanGid 展开成真实后端那份「线 + nestPlan 快照 + nestPlanReleased」。
// 没有它,POST 改计划后 GET 读到的还是旧值,断言就会看到「勾了没生效」。
const nestView = (l) => {
  if (!l.nestPlanGid) return l
  const p = PLAN_GIDS[l.nestPlanGid]
  return p ? { ...l, nestPlan: p } : { ...l, nestPlanReleased: true }
}
const NAME_OPTS = { nature: Array.from({ length: 6 }, () => new Array(6).fill('固执')) }
// 长按入口的两种性别各给一只:♀ 与她那条链、♂ 只有他自己。
const PETS_BY_GID = {
  2001: {
    gid: 2001, species: '火神', name: '小母甲', gender: '♀', baseConfId: 3006, level: 30,
    eggGroups: [{ id: 4, name: '天空', desc: '天空' }, { id: 13, name: '龙', desc: '龙' }],
  },
  2003: {
    gid: 2003, species: '火神', name: '小公乙', gender: '♂', baseConfId: 3006, level: 30,
    eggGroups: [{ id: 4, name: '天空', desc: '天空' }, { id: 13, name: '龙', desc: '龙' }],
  },
}
const STEPS = [{ petbase: 3003, name: '火花', stage: 1 }, { petbase: 3006, name: '火神', stage: 3 }]

let POSTS = []
let POOL_REQS = []
globalThis.fetch = async (url, opts = {}) => {
  const raw = String(url)
  const [path, search = ''] = raw.split('?')
  if (opts.method === 'POST') POSTS.push({ path, body: JSON.parse(opts.body || '{}') })
  let body = null
  if (path === '/api/filter-options') body = CHAINS
  else if (path === '/api/name-options') body = NAME_OPTS
  else if (path === '/api/breeding/pool') {
    POOL_REQS.push(search)
    body = POOLS[search.split('&').filter((x) => x.startsWith('evo=')).join('')] || null
  } else if (path === '/api/breeding') {
    if (opts.method === 'POST') {
      // 整条覆盖写:把提交上来的 nestPlanGid 落进线上(GET 再读到时就反映新计划)。缺字段 = 没计划。
      const next = JSON.parse(opts.body || '{}')
      const l = LINES.lines.find((x) => x.id === next.id)
      if (l) {
        if (next.nestPlanGid) l.nestPlanGid = next.nestPlanGid
        else delete l.nestPlanGid
      }
      body = {}
    } else {
      body = { lines: LINES.lines.map(nestView), nest: NEST }
    }
  } else if (path === '/api/nest') {
    if (opts.method === 'POST') {
      const gid = JSON.parse(opts.body || '{}').gid || 0
      NEST = gid ? { gid, name: NEST_NAMES[gid] || '某只', nature: NATURE_OF[gid] || '固执' } : { gid: 0 }
    }
    body = NEST
  }
  else if (path.startsWith('/api/pets/')) body = PETS_BY_GID[Number(path.split('/').pop())] || null
  else if (path === '/api/evolution') body = STEPS
  if (!body) return { ok: false, status: 404, json: async () => ({}), text: async () => 'not found' }
  return { ok: true, status: 200, json: async () => body, text: async () => JSON.stringify(body) }
}

const server = await createServer({
  root: process.cwd(), logLevel: 'error',
  server: { middlewareMode: true, hmr: false }, optimizeDeps: { noDiscovery: true },
})
const React = (await import('react')).default
const { createRoot } = await import('react-dom/client')
const { MemoryRouter, Routes, Route } = await import('react-router-dom')
const { AccountContext, IconsContext } = await server.ssrLoadModule('/src/context.js')
const Breeding = (await server.ssrLoadModule('/src/pages/breeding/Breeding.jsx')).default

const R = []
const check = (name, ok, detail = '') => {
  R.push({ name, ok })
  console.log(`${ok ? '✓' : '✗'} ${name}${ok || !detail ? '' : `  —— ${detail}`}`)
}
const tick = (ms = 260) => new Promise((r) => setTimeout(r, ms))
const doc = win.document
const textOf = (el) => (el ? (el.textContent || '').trim() : '')
const clickEl = (el) => el && el.dispatchEvent(new win.MouseEvent('click', { bubbles: true }))
// 下拉项(ComboSelect / PetPicker)是 **mousedown** 提交的:两个组件都靠 preventDefault 保住输入框
// 焦点,用 click 派发不会触发选中 —— 这也正是它们用 onMouseDown 而不是 onClick 的原因。
const pickEl = (el) => el && el.dispatchEvent(new win.MouseEvent('mousedown', { bubbles: true, cancelable: true }))
const setNative = (el, v) => {
  const d = Object.getOwnPropertyDescriptor(win.HTMLInputElement.prototype, 'value')
  d.set.call(el, v)
  el.dispatchEvent(new win.Event('input', { bubbles: true }))
}
// 表单里那两张亲本卡按顺序:0 = 种母、1 = 种公。
const slot = (i) => [...doc.querySelectorAll('.br-new .br-parent')][i]
const openSlot = async (i) => { clickEl(slot(i).querySelector('.dropdown-trigger')); await tick(60) }
const slotItems = (i) => [...slot(i).querySelectorAll('.dropdown-item')].map(textOf)
const pickInSlot = async (i, name) => {
  await openSlot(i)
  pickEl([...slot(i).querySelectorAll('.dropdown-item')].find((el) => textOf(el).includes(name)))
  await tick(400)
}
const comboValue = () => doc.querySelector('.br-new .combo-input')?.value || ''
const pickChain = async (kw) => {
  setNative(doc.querySelector('.br-new .combo-input'), kw)
  await tick(140)
  pickEl([...doc.querySelectorAll('.br-new .dropdown-item')].find((el) => textOf(el).includes(kw + '（')))
  await tick(400)
}

// 每条线**初始**的计划值:POST /api/breeding 会把新计划写进 LINES(B 段还要读它),故每次
// render 前按这份快照还原 —— 否则 E 段那次勾选会把计划留在线上,A~D 段的「无计划」前提下一轮
// 渲染就不再成立(状态与断言会错位),而那种失败看起来像代码坏了,其实是夹具串了。
const PLAN0 = new Map(LINES.lines.map((l) => [l.id, l.nestPlanGid]))
const resetPlans = () => {
  for (const l of LINES.lines) {
    if (PLAN0.get(l.id)) l.nestPlanGid = PLAN0.get(l.id)
    else delete l.nestPlanGid
  }
}

let root = null
async function render(url) {
  if (root) { root.unmount(); doc.getElementById('root').innerHTML = '' }
  resetPlans()
  POSTS = []; POOL_REQS = []
  root = createRoot(doc.getElementById('root'))
  root.render(React.createElement(AccountContext.Provider, { value: 'UID:1' },
    React.createElement(IconsContext.Provider, { value: { stat: {} } },
      React.createElement(MemoryRouter, { initialEntries: [url] },
        React.createElement(Routes, null,
          React.createElement(Route, { path: '/', element: React.createElement(Breeding) }))))))
  await tick(500)
}
const openForm = async () => {
  clickEl([...doc.querySelectorAll('button')].find((b) => textOf(b) === '新建培育线'))
  await tick(200)
}

// ===== A. 先选种母:品种(蛋)随她自动填好;种公按她的蛋组收窄 =====
await render('/')
await openForm()
check('建线表单有一对亲本卡', [...doc.querySelectorAll('.br-new .br-parent')].length === 2)
// 还没定品种时前端发的是 evo=0(空品种),后端按空引用给全库雌雄(见 BreedCandidates)。
check('没定品种也拉了候选池(全库口径)', POOL_REQS.some((s) => s.startsWith('evo=0')), POOL_REQS.join(' / ') || '(没有请求)')
check('没定品种时种母位就能点(不是只读位)', !!slot(0).querySelector('.dropdown-trigger'))
const allMothers = await (async () => { await openSlot(0); return slotItems(0) })()
check('候选是全库的雌性(含别的品种)', allMothers.some((t) => t.includes('小母甲')) && allMothers.some((t) => t.includes('水母甲')),
  allMothers.join(' | '))
pickEl([...slot(0).querySelectorAll('.dropdown-item')].find((el) => textOf(el).includes('水母甲')))
await tick(500)
check('选种母后「品种(蛋)」自动填成她的链', comboValue().includes('水蓝蓝'), comboValue() || '(空)')
check('池子随之按这个品种重拉', POOL_REQS.some((s) => s.includes('evo=4001')), POOL_REQS.join(' / '))
check('换品种后她仍在候选里(同品种)', textOf(slot(0)).includes('水母甲'), textOf(slot(0)))

// 手动换回另一个品种:她不属于新品种 → 应被清掉并说明
await pickChain('火花')
const noteTxt = textOf(doc.querySelector('.br-new'))
check('换品种后不属于该品种的种母被清掉', textOf(slot(0)).includes('＋ 选种母'), textOf(slot(0)))
check('并给出了取消的原因', noteTxt.includes('不属于这个品种'), noteTxt.split('\n').filter((l) => l.includes('不属于')).join(' / '))

// 选种母(火花链) → 种公候选按她的蛋组收窄
await pickInSlot(0, '小母甲')
check('选中后卡上显示她的蛋组', textOf(slot(0)).includes('天空') && textOf(slot(0)).includes('龙'), textOf(slot(0)))
await openSlot(1)
const fatherItems = slotItems(1)
check('种公候选按母本蛋组收窄(只剩共组的)',
  fatherItems.some((t) => t.includes('小公乙')) && !fatherItems.some((t) => t.includes('小公丙')),
  fatherItems.join(' | '))
pickEl([...slot(1).querySelectorAll('.dropdown-item')].find((el) => textOf(el).includes('小公乙')))
await tick(200)

clickEl([...doc.querySelectorAll('.br-new button')].find((b) => textOf(b) === '建线'))
await tick(400)
const post = POSTS.find((p) => p.path === '/api/breeding')
check('建线 body 带上这对亲本的 gid',
  !!post && post.body.motherGid === 2001 && post.body.fatherGid === 2003,
  post ? JSON.stringify({ motherGid: post.body.motherGid, fatherGid: post.body.fatherGid }) : '(没有提交)')

// ===== B. 长按 ♂ 进来:他占「种公」位并标注来源 =====
await render('/?new=2003')
check('长按进入后表单已展开', !!doc.querySelector('.br-new'))
check('长按的是 ♂ → 占住种公位', textOf(slot(1)).includes('小公乙'), textOf(slot(1)))
check('被预置的那侧标注「你长按的那只」', textOf(slot(1)).includes('你长按的那只'), textOf(slot(1)))
check('另一侧(种母)仍空着', textOf(slot(0)).includes('＋ 选种母'), textOf(slot(0)))
check('♂ 不预选品种(蛋随母本,由玩家挑)', comboValue() === '', comboValue() || '(空)')

// ===== B2. 长按 ♀ 进来:她占「种母」位,连带把她这条链选成品种 =====
await render('/?new=2001')
check('长按的是 ♀ → 占住种母位', textOf(slot(0)).includes('小母甲'), textOf(slot(0)))
check('♀ 那侧标注「你长按的那只」', textOf(slot(0)).includes('你长按的那只'), textOf(slot(0)))
check('♀ 连带把她这条进化链选成品种(蛋随母本)', comboValue().includes('火花（'), comboValue() || '(空)')
check('♀ 进来时种公位仍空着(由玩家挑)', textOf(slot(1)).includes('＋ 选种公'), textOf(slot(1)))

// ===== C. 线详情:成对展示、种公可换、已不在库 =====
await render('/?line=L-with-father')
const dCards = [...doc.querySelectorAll('.br-parents-block .br-parent')]
check('详情里成对展示亲本', dCards.length === 2, `${dCards.length} 张`)
check('母本卡显示她的名字', textOf(dCards[0]).includes('小母甲'), textOf(dCards[0]))
check('种公卡显示他的名字', !!dCards[1] && textOf(dCards[1]).includes('小公乙'), textOf(dCards[1] || ''))
check('种母只读(不给下拉)、种公可换', !dCards[0].querySelector('.dropdown-trigger') && !!dCards[1].querySelector('.dropdown-trigger'))

await render('/?line=L-released-father')
const relCard = [...doc.querySelectorAll('.br-parents-block .br-parent')][1]
check('指定过但已不在库的种公写「已不在库」', textOf(relCard).includes('已不在库'), textOf(relCard))

// ===== D. 补录面板默认就是这对亲本 =====
await render('/?line=L-with-father')
clickEl([...doc.querySelectorAll('button')].find((b) => textOf(b) === '手动补录一代'))
await tick(200)
const recInputs = [...doc.querySelectorAll('.br-rec-form .dropdown-trigger')].map(textOf)
check('补录面板默认选中线上那对亲本',
  recInputs[0].includes('小母甲') && recInputs[1].includes('小公乙'), recInputs.join(' | '))

// ===== E. 学院小窝:勾选 = 记下**本线的计划**(写线,不写游戏真值)=====
//
// 守的事:
//   1. 勾选长在**亲本卡**上(入口就在种母 / 种公旁边);
//   2. 详情里的勾选走 **POST /api/breeding**(line.nestPlanGid),**不再**调 POST /api/nest ——
//      真值是游戏里的事实,工具里只该存「计划」;把它写进真值就是替游戏改事实;
//   3. 勾另一张卡 = 计划换过去(计划每条线各存一个),前端不自己维护「只能勾一个」;
//   4. 勾上以后卡上区分「按计划试算」与「游戏实况」,状态行把真值与计划并列。
await render('/?line=L-with-father')
const nestBox = (i) => [...doc.querySelectorAll('.br-parents-block .br-nest')][i]
const nestCard = (i) => [...doc.querySelectorAll('.br-parents-block .br-parent')][i]
const hint = () => textOf(doc.querySelector('.br-nest-hint'))
check('亲本卡上有「学院小窝」勾选', doc.querySelectorAll('.br-parents-block .br-nest').length === 2,
  `${doc.querySelectorAll('.br-parents-block .br-nest').length} 个`)
check('小窝空着且无计划时明说空着', hint().includes('空着'), hint())
check('还没选中时没有草环(绿草只属于选中态)',
  doc.querySelectorAll('.br-parents-block .ng-wreath').length === 0
  && !doc.querySelector('.br-parent.nest-picked'))

// 勾种母 → 记成本线的计划(2001),且**不碰** /api/nest
clickEl(nestBox(0).querySelector('input'))
await tick(500)
const putMom = POSTS.find((p) => p.path === '/api/breeding')
check('详情勾种母 → POST /api/breeding 带 nestPlanGid=2001',
  !!putMom && putMom.body.nestPlanGid === 2001,
  putMom ? JSON.stringify({ nestPlanGid: putMom.body.nestPlanGid }) : '(没有提交)')
check('勾选**不再**调 POST /api/nest(计划不是真值)',
  !POSTS.some((p) => p.path === '/api/nest'), POSTS.map((p) => p.path).join(' / ') || '(没有请求)')
check('勾上后她的卡上写着结论', textOf(nestCard(0)).includes('子代性格 100%') && textOf(nestCard(0)).includes('固执'),
  textOf(nestCard(0)))
check('勾上后写明这是「按计划试算」', textOf(nestCard(0)).includes('按计划试算'), textOf(nestCard(0)))
// 结论那句必须在**按钮外面**:塞进胶囊里,胶囊就会被这句话撑成整条横幅,那就不是一枚按钮了
// (4 倍截图上看出来的)。这条守的就是那条界线。
check('结论那句在按钮外面(不把胶囊撑成横幅)',
  !nestBox(0).querySelector('em') && !!nestCard(0).querySelector('.br-nest-note'),
  nestBox(0).innerHTML.slice(0, 80))
check('另一张卡没被跟着勾上', !nestBox(1).querySelector('input').checked)

// 换勾种公 → 计划换成他(2003),种母那张卡松开
clickEl(nestBox(1).querySelector('input'))
await tick(500)
const putDad = POSTS.filter((p) => p.path === '/api/breeding').pop()
check('换勾种公 → body 带 nestPlanGid=2003',
  !!putDad && putDad.body.nestPlanGid === 2003,
  putDad ? JSON.stringify({ nestPlanGid: putDad.body.nestPlanGid }) : '(没有提交)')
check('换计划后种母那张卡自己松开了', !nestBox(0).querySelector('input').checked)
check('种公那张卡是勾上的', !!nestBox(1).querySelector('input').checked)

// 视觉:选中那张卡要「长草」—— 卡带 nest-picked、头像容器里挂上草环(NestGrass.jsx),按钮上是
// 那株草芽。三样都是这套 UI 的可见结果,拆掉任何一样都不会报错、只是没效果,故必须由验收守住。
check('勾上的那张卡带 nest-picked 标记', nestCard(1).classList.contains('nest-picked'), nestCard(1).className)
check('草环挂在那张卡的**头像容器**里(挂错层就飘走)',
  !!nestCard(1).querySelector('.dropdown-av-wrap > .ng-wreath'))
check('没勾的那张卡既无标记也无草环',
  !nestCard(0).classList.contains('nest-picked') && !nestCard(0).querySelector('.ng-wreath'))
check('草环是「编」出来的:三股草绳 + 一圈纹理纤维',
  nestCard(1).querySelectorAll('.ng-wreath .ng-strand').length === 3
  && nestCard(1).querySelectorAll('.ng-wreath .ng-fiber').length >= 60,
  `${nestCard(1).querySelectorAll('.ng-wreath .ng-strand').length} 股绳 / `
  + `${nestCard(1).querySelectorAll('.ng-wreath .ng-fiber').length} 根纤维`)
check('按钮上带草芽图标', !!nestBox(1).querySelector('.ng-sprig'))

// ===== E2. 真值存在且无计划:真值那只勾着但**被禁用**,状态行写「游戏里小窝现在是 …」=====
switchTruth(true)
await render('/?line=L-with-father')
check('真值那只(2001=种母)勾着', !!nestBox(0).querySelector('input').checked)
check('真值那只的勾被禁用(工具里取消不了)', !!nestBox(0).querySelector('input').disabled,
  String(nestBox(0).querySelector('input').disabled))
check('状态行写「游戏里小窝现在是 小母甲」', hint().includes('游戏里小窝现在是') && hint().includes('小母甲'), hint())
check('另一张卡没被勾上、也没被禁(可勾计划)', !nestBox(1).querySelector('input').checked && !nestBox(1).querySelector('input').disabled)
// 禁用的只是「取消」这个动作:草环照挂 —— 它在游戏里就是小窝里那只,视觉上必须是满的。
check('真值那只(勾被禁用)同样长草',
  nestCard(0).classList.contains('nest-picked') && !!nestCard(0).querySelector('.ng-wreath'))

// ===== E3. 有计划且与真值不同:状态行同时出现真值名与计划名,「改用游戏真值」提交 0 =====
await render('/?line=L-plan')
check('状态行同时出现真值名(小母甲)与计划名(小公乙)',
  hint().includes('小母甲') && hint().includes('小公乙'), hint())
check('计划那只(2003=种公)勾着', !!nestBox(1).querySelector('input').checked)
check('真值那只(2001=种母)没被勾(生效值按计划)', !nestBox(0).querySelector('input').checked)
clickEl([...doc.querySelectorAll('.br-nest-hint button')].find((b) => textOf(b) === '改用游戏真值'))
await tick(500)
const clearPlan = POSTS.filter((p) => p.path === '/api/breeding').pop()
check('点「改用游戏真值」→ 提交 nestPlanGid: 0(清计划、回落真值)',
  !!clearPlan && clearPlan.body.nestPlanGid === 0,
  clearPlan ? JSON.stringify({ nestPlanGid: clearPlan.body.nestPlanGid }) : '(没有提交)')

// ===== E4. 计划那只已不在库:文案里 B 写「已不在库的那只」,不留空 =====
await render('/?line=L-plan-released')
check('计划那只已不在库时,状态行写着「已不在库的那只」', hint().includes('已不在库的那只'), hint())

switchTruth(false)

// ===== F. 建线表单:勾选只改本地、不发 /api/nest;「建线」时 body 带 nestPlanGid =====
await render('/')
await openForm()
await pickInSlot(0, '小母甲')
const formNestBox = (i) => [...doc.querySelectorAll('.br-new .br-nest')][i]
// 表单里勾种母 → 只改本地 state,一个请求都不该发
clickEl(formNestBox(0).querySelector('input'))
await tick(300)
check('建线表单勾选**不发**任何写请求(计划只记本地)',
  !POSTS.some((p) => p.path === '/api/nest' || p.path === '/api/breeding'),
  POSTS.map((p) => p.path).join(' / ') || '(没有请求)')
check('建线表单说明里写了「勾选只记下这条线的计划」',
  textOf(doc.querySelector('.br-new')).includes('勾选只记下这条线的计划'), textOf(doc.querySelector('.br-new')))
// 表单里还没有「线」,故状态行不该给「记为游戏真值」(它要 POST /api/nest 改全库真值):
// 一个点了没反应的按钮比不给更差。只该留「清掉计划」。
check('建线表单的状态行不给「记为游戏真值」(还没有线可兜底)',
  !textOf(doc.querySelector('.br-new')).includes('记为游戏真值'), textOf(doc.querySelector('.br-nest-hint')))
clickEl([...doc.querySelectorAll('.br-new button')].find((b) => textOf(b) === '建线'))
await tick(500)
const createPost = POSTS.find((p) => p.path === '/api/breeding')
check('点「建线」时 body 带 nestPlanGid',
  !!createPost && createPost.body.nestPlanGid === 2001,
  createPost ? JSON.stringify({ nestPlanGid: createPost.body.nestPlanGid }) : '(没有提交)')
check('建线**全程**不发 /api/nest', !POSTS.some((p) => p.path === '/api/nest'),
  POSTS.map((p) => p.path).join(' / ') || '(没有请求)')

root.unmount()
await server.close()
const bad = R.filter((r) => !r.ok).length
console.log(`\n${bad ? `✗ ${bad}/${R.length} 条不符` : `✓ 全部 ${R.length} 条通过`}`)
process.exit(bad ? 1 : 0)
