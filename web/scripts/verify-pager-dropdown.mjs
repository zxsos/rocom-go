// 测量:移动端(≤760px)展开分页档位下拉时,菜单项是否被底栏(.bottomnav)遮挡。
//
//   node scripts/verify-pager-dropdown.mjs
//
// 浏览器实测而非目测:遮挡是**几何 + 层叠**两个因素共同的结果 ——
// 光看 z-index 不够(菜单可能与底栏压根不重叠),光看位置也不够
// (重叠了但若菜单层级更高就没问题)。两者都要量化。
//
// 判据:每个菜单项的**中心点**做 elementFromPoint 命中测试 ——
// 命中的不是菜单项即被遮挡(这模拟手指点下去实际点到谁)。
//
// 为什么用 esbuild 预打包而**不用** vite dev server:
// dev server 会经 @vitejs/plugin-react,它对同一模块重复注入 $RefreshReg$
// (HMR 运行时),在本探针这种「入口与源码同目录」的布局下必报
// "The symbol $RefreshReg$ has already been declared" → 500 → 白屏。
// esbuild 直接打成 IIFE 则没有 HMR 注入,与生产构建同构,且无需起服务。
//
// ⚠️ 已做变异测试(见文件末尾注释)。
import { chromium } from 'playwright'
import { build } from 'esbuild'
import { writeFileSync, mkdirSync, rmSync } from 'node:fs'
import { join, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..')
// 探针必须落在**项目内**(不能放 /tmp):esbuild 从入口所在目录向上找 node_modules,
// 在 /tmp 下解析不到 react / react-dom,报 "Could not resolve"。
// 放 scripts/ 下并以前缀 __ 标记,测完即删(见文件末尾的 rmSync)。
const TMP = join(ROOT, 'scripts', '__probe-pager')

// 探针:复刻 App.jsx 的骨架 .app > .content(滚动区) + .bottomnav(sticky 底),
// 内容区底部放真实的分页条(与 PetList.jsx 的结构一致)。
mkdirSync(TMP, { recursive: true })
const PROBE = join(TMP, 'probe.jsx')
writeFileSync(PROBE, `
import React from 'react'
import { createRoot } from 'react-dom/client'
import Dropdown from '${join(ROOT, 'src/components/Dropdown.jsx')}'
import '${join(ROOT, 'src/styles/base.css')}'
import '${join(ROOT, 'src/styles/dropdown.css')}'
import '${join(ROOT, 'src/styles/shell.css')}'
import '${join(ROOT, 'src/styles/list.css')}'

// 分页条:与 PetList.jsx 第 297-310 行同构
function Pager() {
  return (
    <div className="pager">
      <button className="btn">首页</button>
      <button className="btn">上一页</button>
      <span className="muted">3 / 12</span>
      <button className="btn">下一页</button>
      <button className="btn">尾页</button>
      <Dropdown
        className="pager-size" small value={20}
        options={[10, 20, 30, 60, 100].map((n) => ({ value: n, label: n + ' 条/页' }))}
        onChange={() => {}}
      />
    </div>
  )
}
function App() {
  return (
    <div className="app" style={{ display: 'flex', flexDirection: 'column', height: '100vh' }}>
      <div className="content" style={{ flex: 1, overflow: 'auto', padding: 16 }}>
        <div style={{ height: 2400 }} />
        <Pager />
        {/* 分页条**后面**也要留空间:它是 .content 的最后一个子元素时,
            滚到底它也停在视口底部(下方没有余量),桌面那组「滚到视口中部、
            下方应有空间、不该翻转」的反例场景就构造不出来。 */}
        <div style={{ height: 600 }} />
      </div>
      <nav className="bottomnav">
        <button className="tab"><span className="tab-icon">B</span><span className="tab-label">背包</span></button>
      </nav>
    </div>
  )
}
createRoot(document.getElementById('root')).render(<App />)
`)

const OUT = join(TMP, 'bundle.js')
// ⚠️ esbuild 把 import 的 CSS 输出到**独立的 .css 文件**(与 outfile 同名,
// 即 bundle.css),不会内联进 JS。HTML 必须两个都引 ——
// 只引 JS 的话页面是完全没样式的裸 DOM,测出来的「遮挡」是假阳性
// (裸 <ul> 溢出容器被后续元素盖住,与真实的浮层层级无关)。
await build({
  entryPoints: [PROBE], bundle: true, outfile: OUT,
  format: 'iife', platform: 'browser', jsx: 'automatic',
  loader: { '.css': 'css' },
  // 字体 url 是根绝对路径(/fonts/…,由 Go 服务或 vite public 提供),
  // esbuild 会当本地文件解析而报 "Could not resolve"。本探针不加载真实字体,
  // 标为 external 让它原样留在 CSS 里 —— 字体缺失只影响字形,不影响被测的几何。
  external: ['/fonts/*'],
  define: { 'process.env.NODE_ENV': '"production"' },
  logLevel: 'error',
})

const HTML = join(TMP, 'index.html')
writeFileSync(HTML, `<!doctype html><html data-theme="dark"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<link rel="stylesheet" href="./bundle.css"></head>
<body style="margin:0"><div id="root"></div><script src="./bundle.js"></script></body></html>`)

const browser = await chromium.launch()
let bad = 0

// —— 反向断言:桌面端(宽度 > 760,无底栏)的下拉**不得**翻转 ——
// 翻转判据是通用逻辑,若写得过宽(比如只看视口下半就翻),桌面端所有
// 下拉都会朝上弹 —— 那比被遮挡更糟:向下展开是符合预期的默认行为。
// 这一条守住「该翻才翻」。
{
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 } })
  await page.goto('file://' + HTML)
  await page.waitForSelector('.pager-size .dropdown-trigger')
  // 滚到分页条**位于视口中部**:下方空间充足,不该翻。
  // 直接不滚动的话分页条还在视口外(内容区高 900 > 视口 900 - 其它),
  // 那时翻转是正确行为,拿它当反例反而会误判。
  await page.evaluate(() => {
    // 手动算 scrollTop 而非 scrollIntoView:后者在嵌套滚动容器(.content)里
    // 表现不稳定(实测未生效,pager 仍停在视口底部),而这里需要精确居中。
    const content = document.querySelector('.content')
    const pager = document.querySelector('.pager')
    const c = content.getBoundingClientRect()
    const p = pager.getBoundingClientRect()
    content.scrollTop += (p.top + p.height / 2) - (c.top + c.height / 2)
  })
  await page.click('.pager-size .dropdown-trigger')
  await page.waitForSelector('.dropdown-menu', { state: 'visible' })
  const r = await page.evaluate(() => {
    const m = document.querySelector('.dropdown-menu').getBoundingClientRect()
    const t = document.querySelector('.pager-size .dropdown-trigger').getBoundingClientRect()
    return {
      up: m.bottom <= t.top + 1, below: m.top >= t.bottom - 1,
      triggerBottom: +t.bottom.toFixed(0), vh: window.innerHeight,
    }
  })
  console.log(`        (trigger 底边 ${r.triggerBottom} / 视口 ${r.vh} —— 下方尚余 ${r.vh - r.triggerBottom}px)`)
  const ok = !r.up && r.below
  if (!ok) bad++
  console.log(`1440px(桌面) 展开方向: ${r.up ? '向上' : '向下'}  ${ok ? '✓ 未误翻' : '✗ 不应翻转'}`)
  await page.close()
}

// 390 = iPhone 常见;360 = 安卓窄屏;414 = 大屏手机;760 = 底栏出现的临界宽度
for (const W of [360, 390, 414, 760]) {
  const page = await browser.newPage({ viewport: { width: W, height: 780 }, deviceScaleFactor: 1 })
  page.on('pageerror', (e) => console.log('    [pageerror]', String(e).slice(0, 160)))
  await page.goto('file://' + HTML)
  await page.waitForSelector('.pager-size .dropdown-trigger')
  // 自检:样式必须真的生效,否则后面所有几何测量都是裸 DOM 的假结果。
  // 曾踩过 —— 只引 bundle.js 没引 bundle.css,页面无样式,测出的「被挡」
  // 其实是裸 <ul> 溢出容器,与真实浮层层级无关(详见本脚本头部注释)。
  //
  // 判据取「CSS 变量能否解析」而非某个元素的 position:菜单此刻还没展开
  // (.dropdown-menu 不存在),拿它判断会误报。--z-dropdown 来自 base.css 的
  // :root,解析得到即说明整份样式表已生效。
  const styled = await page.evaluate(() =>
    getComputedStyle(document.documentElement).getPropertyValue('--z-dropdown').trim())
  if (styled !== '60') {
    console.error(`✗ 样式未生效(--z-dropdown=${styled || '空'})—— 本脚本的几何结论不可信,请检查 bundle.css 是否被引用`)
    process.exit(2)
  }
  // 把分页条滚到**紧贴底栏上方** —— 正是用户报的位置。
  // 不能简单 scrollTop = 9999:探针在分页条后面留了 600px(桌面反例需要),
  // 滚到底后分页条会跑到视口中上部、离底栏很远,那是另一个场景。
  await page.evaluate(() => {
    const content = document.querySelector('.content')
    const pager = document.querySelector('.pager')
    const nav = document.querySelector('.bottomnav')
    content.scrollTop += pager.getBoundingClientRect().bottom + 8 - nav.getBoundingClientRect().top
  })
  await page.click('.pager-size .dropdown-trigger')
  await page.waitForSelector('.dropdown-menu', { state: 'visible' })

  const r = await page.evaluate(() => {
    const m = document.querySelector('.dropdown-menu').getBoundingClientRect()
    const nav = document.querySelector('.bottomnav').getBoundingClientRect()
    const overlap = Math.max(0, Math.min(m.bottom, nav.bottom) - Math.max(m.top, nav.top))
    const dd = document.querySelector('.pager-size')
    // 逐项命中测试
    const hits = [...document.querySelectorAll('.dropdown-item')].map((it) => {
      const b = it.getBoundingClientRect()
      const el = document.elementFromPoint(b.left + b.width / 2, b.top + b.height / 2)
      return { label: it.textContent.trim(), hit: el ? String(el.className || el.tagName).slice(0, 20) : 'null' }
    })
    return {
      menu: `${m.top.toFixed(0)}~${m.bottom.toFixed(0)}`,
      nav: `${nav.top.toFixed(0)}~${nav.bottom.toFixed(0)}`,
      overlapPx: +overlap.toFixed(0),
      hits,
      zMenu: getComputedStyle(document.querySelector('.dropdown-menu')).zIndex,
      zNav: getComputedStyle(document.querySelector('.bottomnav')).zIndex,
      // up=翻转是否生效;不生效时这一行能直接看出是判据没触发
      up: dd.className.includes(' up'),
    }
  })
  // 菜单项中心点若命中的不是 dropdown 内部元素,即为被遮挡
  const blocked = r.hits.filter((h) => !h.hit.includes('dropdown'))
  if (blocked.length) bad++
  console.log(
    `${String(W).padStart(4)}px  菜单 ${r.menu}  底栏 ${r.nav}  重叠 ${String(r.overlapPx).padStart(3)}px  `
    + `z=${r.zMenu}/${r.zNav} up=${r.up ? 'Y' : 'N'}  ${blocked.length ? `✗ ${blocked.length}/${r.hits.length} 项被挡` : `✓ ${r.hits.length} 项全可点`}`
    + (blocked.length ? `\n         被挡: ${blocked.map((h) => h.label + '→' + h.hit).join(', ')}` : ''),
  )
  await page.close()
}
await browser.close()
rmSync(TMP, { recursive: true, force: true })

console.log(bad ? `\n✗ ${bad} 个视口下菜单被底栏遮挡` : '\n✓ 全部视口:菜单项均可点')
process.exit(bad ? 1 : 0)
