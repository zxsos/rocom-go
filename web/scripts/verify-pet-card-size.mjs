// 宠物陈列卡的图片尺寸验收(需要 playwright + chromium)。
//
//   node scripts/verify-pet-card-size.mjs      (npm run verify:browser 已带上)
//
// 锁住两条不变量:
//
//   1. **图片不得被放大显示** —— 源是 256×256(Pet256,实测 734/737 张),放大就糊:
//      有损 webp(gen_images.py 的 QUALITY=90)的边缘振铃会被放大效应一并凸显。
//   2. **图片不得溢出图区** —— 溢出会顶破卡片、压到相邻行。
//
// 为什么必须用**真实组件**测:初版这里用手造 DOM(只造 .pt-media),测出来
// 完全不是真实卡片的数 —— 手造 DOM 下 img 是 256×256(溢出 104 的图区),
// 真实组件下是 241×103。若照那份数据下结论,等于测了个不存在的页面。
//
// 为什么必须实测而非看 CSS:图片尺寸链踩过三个**静默**的坑,看代码全看不出来:
//   1. aspect-ratio 4/3 与 img 的 height:100% 循环依赖,浏览器回退到图片固有
//      比例,声明的比例根本不生效;
//   2. grid 的隐式行高按内容算,同样形成循环 → 图片解析成固有高度 256,
//      **溢出固定高度 104 的图区**(改 flex 解决,见 .pt-media 注释);
//   3. width/height:auto 会让盒子依赖图片**是否已加载** —— lazy 未加载时
//      塌陷成 16×16,加载完才 103。同一份 CSS 两种结果,随缓存状况漂移。
//
// 故本脚本对「已加载」与「未加载」两种情形都测,确保结果与加载时机无关。
//
// ⚠️ 已做变异测试(每条断言都验证过会红,且报错点出具体容器宽度):
//    - 去掉 .pt-img 的 max-*              → 放大 1.08×
//    - 图区调 400 且去掉 max-*:256        → 放大 1.08×
//    - .pt-media 改回 grid                → 溢出 + 加载时机
//    - 图片盒子改 auto                    → 加载时机(该断言的专属用例)
//    - 量测改回并排                       → 量测行溢出
//    - 四角徽标侧栏上限 62→30px           → 徽标截断
//    - 四角改回独立 absolute(非两栏)     → 右栏上下重叠 + 截断
//
//    注意「栏内重叠」这一条:它守的是**两栏 space-between 这个结构决策**。
//    空间不足时 space-between 会压缩而不是重叠,故只有真改回四个独立
//    absolute 角才触发 —— 那正是最初踩到的坑(右上 70px + 右下 32px > 图区 104px)。
import { chromium } from 'playwright'
import { readFileSync } from 'node:fs'
import { createServer } from 'vite'
import { renderToStaticMarkup } from 'react-dom/server'

const React = (await import('react')).default

// 256×256 纯色 PNG(内联,免依赖仓库资源):固有尺寸必须与被测源一致,
// 否则测的是另一张图(见上面坑 1)。
const PX = 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAQAAAAEACAIAAADTED8xAAAB/0lEQVR42u3TQREAMAjAsDH9iEEFunijgURC7xpd+eCqLwEGAAOAAcAAYAAwABgADAAGAAOAAcAAYAAwABgADAAGAAOAAcAAYAAwABgADAAGAAOAAcAAYAAwABgADAAGAAOAAcAAYAAwABgADAAGAAOAAcAAYAAwABgADAAGAAOAAcAAYAAwABgADAAGAAOAAcAAYAAwABgADAAGAAOAATAAGAAMAAYAA4ABwABgADAAGAAMAAYAA4ABwABgADAAGAAMAAYAA4ABwABgADAAGAAMAAYAA4ABwABgADAAGAAMAAYAA4ABwABgADAAGAAMAAYAA4ABwABgADAAGAAMAAYAA4ABwABgADAAGAAMAAYAA4ABwABgADAAGAAMAAbAAGAAMAAYAAwABgADgAHAAGAAMAAYAAwABgADgAHAAGAAMAAYAAwABgADgAHAAGAAMAAYAAwABgADgAHAAGAAMAAYAAwABgADgAHAAGAAMAAGAAOAAcAAYAAwABgADAAGAAOAAcAAYAAwABgADAAGAAOAAcAAYAAwABgADAAGAAOAAcAAYAAwABgADAAGAAOAAcAAYAAwABgADAAGAAOAAcAAYAAwABgADAAGAAOAAcAAGAAMAAYAA4ABwABgADAAGAAMAAYAA4ABwABgADAAGAAMAAYAA4ABwABgADAAbAOG6gQCnuJhgQAAAABJRU5ErkJggg=='

const SRC_PX = 256 // Pet256 的源边长,见 metrics.jsx 的 STAT_MAX 注释旁的同源说明
// 360 是最窄的常见移动视口(< 760 走横排布局),1500 是宽屏;两端都要覆盖 ——
// 图区在两端形态完全不同(桌面 204~241px 宽 × 104 高 / 移动端 132 宽 × ~197 高),
// 只测一端会漏掉另一端的溢出与重叠。
const WIDTHS = [360, 480, 600, 700, 800, 1000, 1200, 1500]

// ---- 用真实 PetCard 组件渲染出静态 HTML ----
const server = await createServer({
  root: process.cwd(), logLevel: 'error',
  server: { middlewareMode: true, hmr: false }, optimizeDeps: { noDiscovery: true },
})
const { IconsContext } = await server.ssrLoadModule('/src/context.js')
const PetGallery = (await server.ssrLoadModule('/src/pages/pet-list/PetGallery.jsx')).default

const st = (v, t = 0, n = 0) => ({ value: v, talentLv: t, nature: n })
// fixture 用**极端数据**(长形态名 + 双系别 + 血脉 + 异色炫彩 + 双蛋组 + 长奖牌):
// 四角徽标是叠在图片上的浮层,只有内容撑到最满才会暴露越界/重叠/截断 ——
// 用「小火花」这种短数据测,布局永远是绿的,而真实数据里长形态名(「夏日的样子」)
// 与异色炫彩一起出现时就会崩(用户报的「显示不全」正是被短数据掩盖的)。
const pet = {
  gid: 1, confId: 10, baseConfId: 10, species: '火花兽', book: 31, form: '夏日的样子', stage: 2,
  name: '我的小火火花兽', level: 100, natureId: 1, nature: '顽皮', gender: '♂',
  types: ['火', '龙'], typeIcons: [], bloodId: 3, blood: '烈焰', bloodIcon: '',
  eggGroups: [{ id: 6, name: '陆上' }, { id: 9, name: '天空' }],
  heightM: 1.32, weightKg: 24.3, heightMin: 1.1, heightMax: 1.5, heightPct: 41.02,
  weightMin: 20, weightMax: 26, weightPct: 99.2, voice: 96, talentRank: '了不起的天分',
  medal: '了不起的大块头', medalDesc: '体重奖牌', medalIcon: '', wearMedalConfId: 5, medalIds: [1],
  partnerMark: '心仪', partnerMarkIcon: '', speciality: '拾荒高手', specialityId: 7,
  catchTime: 1758700000, shiny: true, colorful: true, glassType: 1, glassValue: 3,
  image: { head: 'a', bigHead: 'b', portraitSmall: 'c' },
  box: { boxId: 2, boxName: '性格1', slot: 14 }, team: null,
  hp: st(118, 3, 1), attack: st(92), defense: st(74, 0, -1),
  spAttack: st(88, 1), spDefense: st(70), speed: st(101, 5, 1),
}
const pets = Array.from({ length: 6 }, (_, i) => ({ ...pet, gid: i + 1 }))
const cardHTML = renderToStaticMarkup(
  React.createElement(IconsContext.Provider, { value: {} },
    React.createElement(PetGallery, { pets, selected: null, itemProps: () => ({}) })),
)
await server.close()

const css = ['base', 'pet', 'list'].map((f) => readFileSync(`src/styles/${f}.css`, 'utf8')).join('\n')

const browser = await chromium.launch()

// 测两种**明确构造**的状态,而不是靠 loading=lazy 的时序:
//
//   未加载:用 route 拦截全部图片请求并 abort。组件 src 指向不存在的路径,
//   拦截后图片永远处于未加载态 —— 稳定、可复现。
//   已加载:换成内联 data URI(不经过网络)并等 complete。
//
// 初版这里用「lazy vs 去掉 lazy」对比,结果是**不稳定的**:lazy 图片是否已加载
// 取决于它是否在视口内与调度时机,同一份 CSS 连跑两次结论不同(还原后复跑
// 都曾报「容器 1500 不一致」)。不稳定的断言比没有更糟 —— 它会让人习惯性忽略红灯。
// 一个 dpr 用**一个**页面跑完所有宽度:初版每个宽度都 newPage,8 宽度 × 2 dpr
// = 16 次建页 + 16 次 setContent(内含 ~200KB CSS),慢到脚本会被判定超时跳过 ——
// 跑不完的校验等于没有。改成建页一次、容器内改宽度、来回切 src 即可。
async function measureAll(dpr) {
  const page = await browser.newPage({ viewport: { width: 1700, height: 900 }, deviceScaleFactor: dpr })
  await page.setContent(`<!doctype html><html><head><meta charset="utf-8"><style>${css}</style></head>
<body style="margin:0;padding:0"><div id="host" style="width:1000px">${cardHTML}</div></body></html>`)
  await page.waitForSelector('.pt-img')

  const out = {}
  for (const w of WIDTHS) {
    await page.evaluate((w) => { document.getElementById('host').style.width = w + 'px' }, w)

    // —— 状态 A:未加载 ——
    // 不需要 route 拦截:去掉 src 就是确定未加载(改宽度时图片已加载过,
    // 故每次先摘掉),且不涉及网络时序,比 abort 更快也更稳。
    await page.evaluate(() => {
      for (const im of document.querySelectorAll('.pt-img')) im.removeAttribute('src')
    })
    const unloaded = await box(page)

    // —— 状态 B:已加载(内联 data URI,不经过网络)——
    await page.evaluate((px) => {
      for (const im of document.querySelectorAll('.pt-img')) im.src = px
      return Promise.all([...document.querySelectorAll('.pt-img')].map((im) =>
        im.complete ? null : new Promise((r) => { im.onload = r; im.onerror = r })))
    }, PX)
    const lb = await box(page)

    const badges = await readBadges(page)
    const measures = await readMeasures(page)
    out[w] = { unloaded, loaded: lb, measures, badges }
  }
  await page.close()
  return out
}

// 四角徽标(左右两栏)——
//
// 来自实际故障:属性徽标原先挤在数据区一行胶囊里(nowrap + 截断),
// 形态(「夏日的样子」)与奖牌被切掉。现改到图上四角、分左右两栏。
// 要断言的是**布局不崩**:两栏不越界、不互相重叠、栏内上下不重叠、
// 文字不被截断 —— 这些都只在真实布局下才测得出来。
const readBadges = (page) => page.evaluate(() => {
  const mb = document.querySelector('.pt-media').getBoundingClientRect()
  const box = (s) => {
    const e = document.querySelector(s)
    if (!e) return null
    const b = e.getBoundingClientRect()
    return { x: b.x, y: b.y, r: b.right, bt: b.bottom }
  }
  const L = box('.pt-side.left'), R = box('.pt-side.right')
  const tr = box('.pt-corner.tr'), br = box('.pt-corner.br')
  return {
    outside: [['left', L], ['right', R]].filter(([, c]) => c
      && (c.x < mb.x - 1 || c.r > mb.right + 1 || c.y < mb.y - 1 || c.bt > mb.bottom + 1)).map(([k]) => k),
    sideOverlap: !!(L && R && L.r > R.x + 1),
    colOverlap: !!(tr && br && tr.bt > br.y + 1),
    // 徽标文本被截断(scrollWidth 超出可视宽度)
    clipped: [...document.querySelectorAll('.pt-chip, .pt-meta-i')]
      .filter((e) => e.scrollWidth > e.clientWidth + 1).map((e) => e.textContent.trim()),
  }
})

// 量测行(体重/身高)——
//
// 来自实际故障:体重/身高曾并排显示,每半只有 ~105px,而一行要放
// 「标签+值+标尺+百分位」四项 ≈131px —— 内容溢出、百分位数字被标尺压住
// 重合,而页面看上去只是"挤了一点"。现已改回各占一行。
const readMeasures = (page) => page.evaluate(() =>
  [...document.querySelectorAll('.measure')].map((m) => {
    const bar = m.querySelector('.pctbar')
    const pct = m.querySelector('.measure-pct')
    const bb = bar && bar.getBoundingClientRect()
    const pb = pct && pct.getBoundingClientRect()
    return {
      label: m.querySelector('.measure-lb').textContent,
      overflow: m.scrollWidth > m.clientWidth + 1,
      // 标尺右边界与百分位左边界的间隙,负值即重合
      gap: bb && pb ? +(pb.x - bb.right).toFixed(1) : null,
    }
  }))

// box 量图区高度与图片盒子尺寸。object-fit:contain 下绘制边长 = 内容区较短的一边
// (clientWidth/Height 含 padding,故减 16 = 上下/左右各 8)。
const box = (page) => page.evaluate(() => {
  const m = document.querySelector('.pt-media')
  const im = document.querySelector('.pt-img')
  return {
    media: +m.getBoundingClientRect().height.toFixed(0),
    box: `${im.clientWidth}×${im.clientHeight}`,
    shown: +Math.min(im.clientWidth - 16, im.clientHeight - 16).toFixed(0),
  }
})

// dpr=1 用于「放大/溢出/一致」三项断言;dpr=2 只算物理放大供参考。
const [all1, all2] = [await measureAll(1), await measureAll(2)]
const rows = WIDTHS.map((w) => ({ w, d1: all1[w], d2: all2[w] }))

console.log(`\n源图 ${SRC_PX}×${SRC_PX}(Pet256)。放大 = 显示边长 ÷ ${SRC_PX},>1 即糊。\n`)
console.log('容器   图区  img盒      显示  放大  │ 未加载/已加载一致 │ DPR=2 物理  物理放大')
let worst = 0, worstPhys = 0
const bad = { 放大: [], 溢出: [], 不一致: [] }
for (const { w, d1, d2 } of rows) {
  const a = d1.loaded
  const ratio = a.shown / SRC_PX
  const phys = d2.loaded.shown * 2, rP = phys / SRC_PX
  worst = Math.max(worst, ratio)
  worstPhys = Math.max(worstPhys, rP)
  const same = d1.unloaded.box === d1.loaded.box
  // 图片盒子高度不得超出图区(+1 容 border 舍入)
  const overflow = a.media > 0 && parseInt(a.box.split('×')[1], 10) > a.media + 1
  if (ratio > 1.001) bad.放大.push(w)
  if (overflow) bad.溢出.push(w)
  if (!same) bad.不一致.push(w)
  console.log(
    String(w).padStart(4) + '  ' + String(a.media).padStart(4) + '  ' + a.box.padStart(9) + '  '
    + String(a.shown).padStart(4) + '  ' + ratio.toFixed(2) + (ratio > 1.001 ? '✗' : ' ') + ' │ '
    + (same ? '        ✓       ' : `  ✗ 未加载=${d1.unloaded.box} `) + ' │ '
    + String(phys).padStart(8) + '  ' + rP.toFixed(2) + (rP > 1.001 ? '✗' : ' '),
  )
}

console.log(`\n最大放大 ${worst.toFixed(2)}×  ${worst > 1.001 ? '✗ 会放大' : '✓ 全程不放大'}`)
console.log(`DPR=2 最大放大 ${worstPhys.toFixed(2)}×  ${worstPhys > 1.001 ? '(源分辨率天花板,需 Pet1024)' : '(连高倍屏也不放大 —— 显示尺寸已小于源)'}`)
// max-* 的 256 那一档在图区高度(104)远小于 256 时**不会触发**,它是给
// 「将来把图区调高」留的保险。明确说出来,免得误以为这条断言在守它。
console.log(`注:显示边长 ${rows[0].d1.loaded.shown}px 已远小于源 ${SRC_PX}px,故 max-*:256px 当前不参与约束;它由 .pt-media 的固定高度兜住。`)

await browser.close()

const problems = []
if (bad.放大.length) problems.push(`图片被放大(容器 ${bad.放大.join('/')}px)—— 检查 .pt-img 的 max-*`)
if (bad.溢出.length) problems.push(`图片溢出图区(容器 ${bad.溢出.join('/')}px)—— 检查 .pt-media 是否还是 grid`)
if (bad.不一致.length) problems.push(`加载时机影响布局(容器 ${bad.不一致.join('/')}px)—— 图片尺寸不能由内容撑`)

// 量测行:溢出与重合。任一容器宽度下出现即失败(见 measure() 里的注释)。
const badMes = []
for (const { w, d1 } of rows) {
  for (const m of d1.measures || []) {
    if (m.overflow) badMes.push(`容器 ${w}: ${m.label} 内容溢出`)
    if (m.gap !== null && m.gap < 0) badMes.push(`容器 ${w}: ${m.label} 标尺与百分位重合 ${m.gap}px`)
  }
}
if (badMes.length) problems.push('量测行异常 —— ' + badMes.slice(0, 4).join('; '))

// 四角徽标:越界 / 重叠 / 截断。任一容器宽度下出现即失败(见 measure() 内注释)。
const badBadge = []
for (const { w, d1 } of rows) {
  const g = d1.badges
  if (!g) continue
  if (g.outside.length) badBadge.push(`容器 ${w}: ${g.outside.join('/')} 栏越出图区`)
  if (g.sideOverlap) badBadge.push(`容器 ${w}: 左右两栏重叠`)
  if (g.colOverlap) badBadge.push(`容器 ${w}: 右栏上下重叠`)
  if (g.clipped.length) badBadge.push(`容器 ${w}: 徽标截断 ${g.clipped.slice(0, 2).join('|')}`)
}
if (badBadge.length) problems.push('四角徽标异常 —— ' + badBadge.slice(0, 4).join('; '))
if (problems.length) {
  console.error('\n✗ ' + problems.join('\n✗ '))
  process.exit(1)
}
console.log('\n✓ 不放大 / 不溢出 / 与加载时机无关 / 量测行不溢出不重合 / 四角徽标不越界不重叠不截断')
