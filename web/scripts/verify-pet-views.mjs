// 宠物列表两个视图(陈列 / 表格)的渲染验收:用 vite SSR + react-dom/server 真渲染,
// 覆盖「全字段 / 极简老数据 / 空列表」三种数据形态。
//
//   node scripts/verify-pet-views.mjs
//
// **不需要后端** —— 这是它相对 verify-pages.mjs 的价值:数据走内置 fixture,
// 任何时候都能跑,挡的是字段改名/缺失导致的 undefined 访问(渲染期就抛,
// 构建期看不出来)与「渲染了但数字画错」(见下方 width/height 断言)。
//
// 覆盖极限形态的理由:bare 那只对应**老库与未上线形态的真实形状** —— 无图、
// 无位置、无百分位、六维全 0。这三只里任意一只渲染失败,线上就是整个列表白屏。
//
// ⚠️ 已做变异测试(改一处、确认测试红):
//    - pct.toFixed(1) → (0)                    → 「陈列:百分位」失败
//    - STAT_MAX 500 → 5000                     → 「六维定标条/微柱高度」失败
//    - <SixBars/> → <span/>                    → 「表格:六维微柱」失败
//    - {p.name || p.species} → {p.species}     → 「陈列:昵称」失败
//   断言收紧过一次:原先写 g1.includes('99.2%'),会被百分位游标的内联样式
//   style="left:99.2%" 命中 —— 数字渲染错了测试照样绿。故一律带 > < 边界。
import { createServer } from 'vite'

const server = await createServer({
  root: process.cwd(), logLevel: 'error',
  server: { middlewareMode: true, hmr: false }, optimizeDeps: { noDiscovery: true },
})

const React = (await import('react')).default
const { renderToStaticMarkup } = await import('react-dom/server')
const { IconsContext } = await server.ssrLoadModule('/src/context.js')
const PetGallery = (await server.ssrLoadModule('/src/pages/pet-list/PetGallery.jsx')).default
const PetTable = (await server.ssrLoadModule('/src/pages/pet-list/PetTable.jsx')).default

const st = (value, talentLv = 0, nature = 0) => ({ value, talentLv, nature })

// 完整数据的一只:所有可选字段都给值(走"全字段"路径)
const full = {
  gid: 1, confId: 10, baseConfId: 10, species: '火花', book: 31, form: '夏日', stage: 2,
  name: '小火花', level: 42, natureId: 1, nature: '急躁', gender: '♂',
  types: ['火', '龙'], typeIcons: ['t/1.png', 't/2.png'],
  bloodId: 3, blood: '火', bloodIcon: 'b/3.png',
  eggGroups: [{ id: 1, name: '陆上', desc: '陆上组' }],
  heightM: 1.32, weightKg: 24.3,
  heightMin: 1.1, heightMax: 1.5, heightPct: 41.02,
  weightMin: 20, weightMax: 26, weightPct: 99.2,
  voice: 96, talentRank: '了不起的天分', medal: '飞跃', medalDesc: '体重奖牌', medalIcon: 'm/1.png',
  wearMedalConfId: 5, medalIds: [1, 2], partnerMark: '心仪', partnerMarkIcon: 'p/1.png',
  speciality: '拾荒', specialityId: 7, catchTime: 1758700000,
  shiny: true, colorful: true, glassType: 1, glassValue: 3,
  image: { head: 'HeadIcon/1.webp', bigHead: 'BigHeadIcon256/1.webp', portraitSmall: 'Pet256/1.webp' },
  box: { boxId: 2, boxName: '性格1', slot: 14 }, team: null,
  hp: st(118, 3, 1), attack: st(92, 0, 0), defense: st(74, 0, -1),
  spAttack: st(88, 1, 0), spDefense: st(70, 0, 0), speed: st(101, 5, 1),
}

// 极简数据的一只:老库/未上线形态的真实形态 —— 无图、无位置、无百分位、无炫彩
const bare = {
  gid: 2, confId: 11, baseConfId: 11, species: '未知名', book: 0, form: '', stage: 1,
  name: '', level: 1, natureId: 0, nature: '', gender: '',
  types: [], typeIcons: [],
  heightM: 0, weightKg: 0, voice: 0,
  talentRank: '', medal: '', medalDesc: '', medalIcon: '',
  wearMedalConfId: 0, medalIds: [], partnerMark: '无', partnerMarkIcon: '',
  speciality: '', specialityId: 0, catchTime: 0,
  shiny: false, colorful: false, glassType: 0, glassValue: 0,
  image: {}, box: null, team: null,
  hp: st(0), attack: st(0), defense: st(0), spAttack: st(0), spDefense: st(0), speed: st(0),
}

// 在队里的一只(team 而非 box),外加 0 值维度与极值声音
const inTeam = {
  ...full, gid: 3, box: null, team: { teamIdx: 1, pos: 3 },
  voice: -100, heightPct: 0, weightPct: 100,
}

const pets = [full, bare, inTeam]
const noop = () => {}
const itemProps = () => ({
  onClick: noop, onDoubleClick: noop, onContextMenu: noop,
  onTouchStart: noop, onTouchMove: noop, onTouchEnd: noop,
})

let fail = 0
const check = (label, fn) => {
  try {
    const html = renderToStaticMarkup(
      React.createElement(IconsContext.Provider, { value: {} }, fn()),
    )
    console.log(`OK   ${label.padEnd(28)} ${String(html.length).padStart(6)}B`)
    return html
  } catch (e) {
    fail++
    console.log(`FAIL ${label}\n     ${e && e.stack ? e.stack.split('\n').slice(0, 3).join('\n     ') : e}`)
    return ''
  }
}

const g1 = check('陈列视图(全量数据)', () =>
  React.createElement(PetGallery, { pets, selected: 1, itemProps }))
const t1 = check('表格视图(全量数据)', () =>
  React.createElement(PetTable, { pets, selected: 2, sort: 'gid', order: 'asc', onSort: noop, itemProps }))
check('陈列视图(空列表)', () =>
  React.createElement(PetGallery, { pets: [], selected: null, itemProps }))
check('表格视图(空列表)', () =>
  React.createElement(PetTable, { pets: [], selected: null, sort: '', order: '', onSort: noop, itemProps }))

// 视图默认值的规整:回落方向写反是静默故障(页面照常显示,只是每个用户的视图被
// 强制翻到另一个),故单独断言四类输入。
//
// ⚠️ 断言一律**语义驱动**、用 DEFAULT_VIEW 表达,不硬编码 'table'/'gallery':
// 初版把期望值写死成 'table',于是这次默认值改成 gallery 时五条断言全红 ——
// 而那次改动本身是**正确**的,红的是过时的断言。断言守的是契约(两个合法值
// 都保留、非法值回落默认),不是"当前默认值恰好是哪个"。
{
  const { DEFAULT_VIEW, sanitizeView } = await server.ssrLoadModule('/src/pages/pet-list/filters.js')
  const VIEWS = ['table', 'gallery']
  const cases = [
    ['默认值是合法视图之一', VIEWS.includes(DEFAULT_VIEW)],
    ['无记录/空串 → 默认', [undefined, null, ''].every((v) => sanitizeView(v) === DEFAULT_VIEW)],
    // 最关键的一条:两个合法值都**原样保留**,不能被默认值"吸收"。
    // 写成 `v === 'x' ? 'x' : DEFAULT_VIEW` 的那半个,改默认值就会静默翻掉另一半。
    ['两个合法值都保留(不被默认值吸收)', VIEWS.every((v) => sanitizeView(v) === v)],
    ['垃圾值 → 默认', ['GALLERY', 'Table', 42, {}, true].every((v) => sanitizeView(v) === DEFAULT_VIEW)],
  ]
  console.log(`\n  (当前 DEFAULT_VIEW = ${DEFAULT_VIEW})`)
  for (const [label, ok] of cases) {
    console.log(`  ${ok ? 'OK  ' : 'FAIL'} 视图默认值: ${label}`)
    if (!ok) fail++
  }
}

// 内容校验:渲染成功不等于渲染对了 —— 关键数据必须出现在 HTML 里
const must = [
  ['陈列:昵称', g1.includes('小火花')],
  ['陈列:等级', g1.includes('Lv.42')],
  ['陈列:六维值', g1.includes('118')],
  // 断言写 >99.2%< 而非 99.2%:后者会被百分位标尺游标的内联样式
  // style="left:99.2%" 命中 —— 那样即使数字渲染错了测试也是绿的。
  ['陈列:百分位', g1.includes('>99.2%<')],
  // 定标条的实际长度:生命 118 / 上限 500 = 23.6%。数值画成条是这次的核心改动,
  // 只断言"118 出现了"挡不住条长算错(除以 5000 也能过)。
  ['陈列:六维定标条', g1.includes('width:23.6%')],
  ['表格:微柱高度', t1.includes('height:23.6%')],
  ['陈列:全身像', g1.includes('Pet256/1.webp')],
  ['陈列:无图回退', g1.includes('🐾') || g1.includes('✨')],
  ['陈列:位置待同步', g1.includes('位置待同步')],
  // 蛋组显示**组名**。曾误写成显示组数(「蛋组 2」),数字不含任何可用信息 ——
  // 玩家要的是「陆上/天空」这些组名,它们对应繁殖范围。
  //
  // ⚠️ 正则必须锚定 `>` 开头(`>\s*蛋组...`),只吃**标签的文本内容**。
  // 初版写成 /蛋组\s*([^<]*)/ 会先命中 title 属性里的「蛋组：陆上(陆上组)」——
  // 那里始终带组名,于是即使标签文本退化成数字,断言依然是绿的(变异测试
  // 抓到过这个假阳性)。形态(显示名还是数字)才是契约,故不钉死具体组名。
  ['陈列:蛋组显示组名', (() => {
    const m = />\s*蛋组\s*([^<]*)</.exec(g1)
    return !!m && m[1].trim().length > 0 && !/^\d+$/.test(m[1].trim())
  })()],
  ['陈列:名字与系别同排(pt-head 容器)', g1.includes('pt-head') && g1.includes('pt-head-text')],
  ['表格:种类', t1.includes('火花')],
  ['表格:六维微柱', t1.includes('sixbars-b')],
  ['表格:排序态 aria-sort', t1.includes('aria-sort="ascending"')],
  ['表格:百分位', t1.includes('99.20%') || t1.includes('99.2%')],
]
console.log('')
for (const [label, ok] of must) {
  console.log(`  ${ok ? 'OK  ' : 'FAIL'} ${label}`)
  if (!ok) fail++
}

await server.close()
console.log(fail ? `\n=== ${fail} 项失败 ===` : '\n=== 全部通过 ===')
process.exit(fail ? 1 : 0)
