// 宠物列表长按/右键菜单的验收(见 pages/pet-list/ContextMenu.jsx + constants.js 的蛋组口径)。
//
//   node scripts/verify-ctx-menu.mjs
//
// 锁住三件事,它们的共同点是错了**都不会报错**:
//   1. 菜单里没有「复制编号」—— 那个操作在 http(非安全上下文)下 navigator.clipboard 根本不存在,
//      点了毫无反应(静默失败),故删掉;
//   2. 蛋组两项只在宠物**有可繁殖蛋组**时出现 —— 蛋组为空(超进化/分支形态)或就是「无蛋组」
//      (官方名「未发现」,不可繁殖)的宠物,点下去只会得到空列表 / 配不出伴;
//   3. 蛋组的**显示名**统一成「无蛋组」,而**发出去的仍是官方名**「未发现」—— 面板 chip 的取值与
//      长按筛选写进 filter 的 eggGroupsExact 都得是官方名,写成「无蛋组」后端一条都匹配不到
//      (筛选静默变空,面板上还高亮着)。
//
// ⚠️ 已做变异测试(每条断言都验证过会红):
//    - eggGroupLabel 直接返回 name(不映射)      → 显示名断言红
//    - ALL_EGG_GROUPS 里把 value 也改成「无蛋组」  → 取值断言红
//    - breedableEggGroups 不过滤「未发现」        → 无蛋组那两条「不出现」的断言红
//    - 菜单项里加回「复制编号」                    → 已删项的断言红
import { createServer } from 'vite'
import { JSDOM } from 'jsdom'

const dom = new JSDOM('<!doctype html><html><body><div id="root"></div></body></html>', {
  url: 'http://localhost:5173/', pretendToBeVisual: true,
})
const win = dom.window
for (const k of ['window', 'document', 'navigator', 'HTMLElement', 'Element', 'Node',
  'MouseEvent', 'Event', 'getComputedStyle', 'requestAnimationFrame', 'cancelAnimationFrame']) {
  if (win[k] === undefined) continue
  try {
    globalThis[k] = win[k]
  } catch {
    Object.defineProperty(globalThis, k, { value: win[k], configurable: true, writable: true })
  }
}
// 这里不用 act():菜单是纯展示组件、点击回调只把参数交给父级,渲染后的状态断言不依赖 React 的
// 批处理时机。关掉 act 环境标记,免得每次点击都往 stderr 刷一条与断言无关的告警。
globalThis.IS_REACT_ACT_ENVIRONMENT = false

const server = await createServer({
  root: process.cwd(), logLevel: 'error',
  server: { middlewareMode: true, hmr: false }, optimizeDeps: { noDiscovery: true },
})
const React = (await import('react')).default
const { createRoot } = await import('react-dom/client')
const { breedableEggGroups, eggGroupLabel, ALL_EGG_GROUPS, EGG_GROUP_UNKNOWN } =
  await server.ssrLoadModule('/src/constants.js')
const ContextMenu = (await server.ssrLoadModule('/src/pages/pet-list/ContextMenu.jsx')).default

const R = []
const eq = (name, got, want) => R.push({ name, got, want, ok: JSON.stringify(got) === JSON.stringify(want) })
const tick = (ms = 80) => new Promise((r) => setTimeout(r, ms))

// —— 1. 显示名/取值名 ——
eq('官方名「未发现」显示为「无蛋组」', eggGroupLabel(EGG_GROUP_UNKNOWN), '无蛋组')
eq('没蛋组(空)也显示「无蛋组」', eggGroupLabel(''), '无蛋组')
eq('普通蛋组原样显示', eggGroupLabel('天空'), '天空')
eq('选项仍是 15 个(id 1-15)', ALL_EGG_GROUPS.length, 15)
const noneOpt = ALL_EGG_GROUPS[ALL_EGG_GROUPS.length - 1]
eq('无蛋组排在末尾', noneOpt.label, '无蛋组')
// 取值必须是官方名:发「无蛋组」出去,egg_groups LIKE '%"无蛋组"%' 一条都匹配不到。
eq('无蛋组的取值仍是官方名', noneOpt.value, EGG_GROUP_UNKNOWN)
eq('没有别的选项叫无蛋组', ALL_EGG_GROUPS.filter((o) => o.label === '无蛋组').length, 1)
eq('选项的取值互不重复', new Set(ALL_EGG_GROUPS.map((o) => o.value)).size, 15)

// —— 2. 哪些宠物有可繁殖蛋组 ——
eq('没有蛋组 → 空', breedableEggGroups([]), [])
eq('蛋组是「未发现」→ 空', breedableEggGroups([{ name: EGG_GROUP_UNKNOWN }]), [])
eq('两个蛋组原样取出', breedableEggGroups([{ name: '天空' }, { name: '龙' }]), ['天空', '龙'])
// 候选池(/api/breeding/pool)给的是字符串数组,同一套规则也得认
eq('字符串形状也认', breedableEggGroups(['天空', EGG_GROUP_UNKNOWN]), ['天空'])
eq('undefined 不炸', breedableEggGroups(undefined), [])

// —— 3. 菜单项 ——
const mkPet = (eggGroups) => ({
  gid: 1001, species: '火神', nature: '固执', speciality: '暴击', eggGroups,
})
async function renderMenu(pet) {
  const host = win.document.createElement('div')
  win.document.body.appendChild(host)
  const patches = []
  const breeds = []
  const root = createRoot(host)
  root.render(React.createElement(ContextMenu, {
    menu: { gid: pet.gid, pet, x: 10, y: 10 },
    menuRef: null,
    onDetail: () => {},
    onFilterSame: (patch) => patches.push(patch),
    onBreed: (p) => breeds.push(p),
  }))
  await tick()
  const doc = win.document
  const items = [...host.querySelectorAll('.ctx-item')].map((el) => (el.textContent || '').trim())
  const click = (label) => {
    const el = [...host.querySelectorAll('.ctx-item')].find((x) => (x.textContent || '').trim() === label)
    if (el) el.dispatchEvent(new win.MouseEvent('click', { bubbles: true }))
  }
  return { host, items, patches, breeds, click, root }
}

const withEgg = await renderMenu(mkPet([{ name: '天空' }, { name: '龙' }]))
R.push({ name: '有蛋组:菜单含蛋组两项', got: withEgg.items.filter((x) => x.includes('蛋组') || x.includes('配种')), want: ['筛选相同蛋组', '孵蛋配种'], ok: withEgg.items.includes('筛选相同蛋组') && withEgg.items.includes('孵蛋配种') })
R.push({ name: '菜单已无「复制编号」', got: withEgg.items.includes('复制编号'), want: false, ok: !withEgg.items.includes('复制编号') })
R.push({ name: '原有四项照旧', got: withEgg.items.slice(0, 4), want: ['查看详情', '筛选相同种类', '筛选相同性格', '筛选相同特长'], ok: JSON.stringify(withEgg.items.slice(0, 4)) === JSON.stringify(['查看详情', '筛选相同种类', '筛选相同性格', '筛选相同特长']) })
// 点击「筛选相同蛋组」写进 filter 的 patch:精确口径 + 清掉 OR 口径(两套并存时后端以精确为准,
// 不清的话面板上那排 chip 还亮着却不生效)。
withEgg.click('筛选相同蛋组')
R.push({ name: '长按筛选写的是 eggGroupsExact(官方名)', got: withEgg.patches, want: [{ eggGroups: [], eggGroupsExact: ['天空', '龙'] }], ok: JSON.stringify(withEgg.patches) === JSON.stringify([{ eggGroups: [], eggGroupsExact: ['天空', '龙'] }]) })
withEgg.click('孵蛋配种')
R.push({ name: '孵蛋配种把整只宠物交给父级(带去 /breeding?new=gid)', got: withEgg.breeds.length, want: 1, ok: withEgg.breeds.length === 1 && withEgg.breeds[0].gid === 1001 })
withEgg.root.unmount()
withEgg.host.remove()

for (const [name, pet] of [
  ['无蛋组(未发现)', mkPet([{ name: EGG_GROUP_UNKNOWN }])],
  ['没有蛋组(空)', mkPet([])],
  ['蛋组字段缺失', mkPet(undefined)],
]) {
  const m = await renderMenu(pet)
  const has = m.items.some((x) => x === '筛选相同蛋组' || x === '孵蛋配种')
  R.push({ name: `${name}:不出现蛋组两项`, got: m.items, want: '无蛋组项', ok: !has && m.items.length === 4 })
  m.root.unmount()
  m.host.remove()
}

await server.close()
let bad = 0
for (const r of R) {
  if (r.ok) { console.log(`  ✓ ${r.name}`) } else {
    bad++
    console.log(`  ✗ ${r.name}  得到 ${JSON.stringify(r.got)}  期望 ${JSON.stringify(r.want)}`)
  }
}
console.log(`\n${bad ? `✗ ${bad} 条不符` : `✓ 全部 ${R.length} 条通过`}`)
process.exit(bad ? 1 : 0)
