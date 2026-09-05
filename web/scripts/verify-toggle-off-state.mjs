// 开关按钮「关闭态不得带主色」的验收。
//
//   node scripts/verify-toggle-off-state.mjs
//
// 来自真实反馈:「取消后还有蓝色光圈在,不容易看出是否关闭」。
// 根因是 hover 与选中撞色 —— .toggle:hover / .rrule-mini:hover / .map-btn:hover
// 都写了 --accent,而 .on 也是 --accent。取消勾选后鼠标仍停在按键上,
// 边框依旧是蓝的,与开着只差一层 0.14 alpha 的底色,扫视时分不清。
// (.map-btn 更极端:它的 :hover 与 .on 样式**完全相同**。)
//
// 本脚本锁住的判据:**关闭态(未选中)的开关,即使正在悬停,也不得出现主色**。
// 主色必须是「选中」的专属语义。
//
// ⚠️ 交互顺序很关键:每个开关必须**先点两次(开→关)、再把鼠标移回它上面**,
// 才能测到「关闭 + 悬停」这个组合。曾漏掉 hover 那一步 —— 按 click 顺序
// 只有最后一个元素处于悬停态,前两个实际测的是非悬停态(本来就不是蓝的),
// 于是脚本永远绿、变异测试也测不出问题。
//
// ⚠️ 已做变异测试:把任一 .on:hover 改回 --accent,对应项立即报红。
import { chromium } from 'playwright'
import { build } from 'esbuild'
import { writeFileSync, mkdirSync, rmSync } from 'node:fs'
import { join, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..')
// 探针必须落在项目内:esbuild 从入口所在目录向上找 node_modules(详见
// verify-pager-dropdown.mjs 的同名注释)。
const TMP = join(ROOT, 'scripts', '__probe-toggle')
mkdirSync(TMP, { recursive: true })

const PROBE = join(TMP, 'p.jsx')
writeFileSync(PROBE, `
import React, { useState } from 'react'
import { createRoot } from 'react-dom/client'
import '${join(ROOT, 'src/styles/base.css')}'
import '${join(ROOT, 'src/styles/panel.css')}'
import '${join(ROOT, 'src/styles/rules.css')}'
import '${join(ROOT, 'src/styles/map.css')}'

// 复刻 FilterPanel.jsx 的 Toggle:原生 input 铺满并透明,视觉交给 label
function Toggle({ checked, onChange, children }) {
  return (
    <label className={'toggle' + (checked ? ' on' : '')}>
      <input type="checkbox" checked={checked} onChange={(e) => onChange(e.target.checked)} />
      <span>{children}</span>
    </label>
  )
}
function App() {
  // ⚠️ 初始值必须都是 false:点两次(开→关)后要落在**关闭态**。
  // 曾把 rule/layer 初始化成 true,点两次又回到开着 —— 那两项测的是
  // 「开着 + 悬停」(主色是应当的),判据 !on 不触发,于是假通过。
  const [t, setT] = useState(false)
  const [rule, setRule] = useState(false)
  const [layer, setLayer] = useState(false)
  return <div style={{ padding: 24, display: 'flex', gap: 24 }}>
    <div className="toggles">
      <Toggle checked={t} onChange={setT}>异色</Toggle>
    </div>
    <button className={'rrule-mini' + (rule ? ' on' : '')} onClick={() => setRule(!rule)} />
    <button className={'map-btn' + (layer ? ' on' : '')} onClick={() => setLayer(!layer)} />
  </div>
}
createRoot(document.getElementById('root')).render(<App />)
`)

const OUT = join(TMP, 'bundle.js')
await build({
  entryPoints: [PROBE], bundle: true, outfile: OUT,
  format: 'iife', platform: 'browser', jsx: 'automatic',
  loader: { '.css': 'css' }, external: ['/fonts/*'],
  define: { 'process.env.NODE_ENV': '"production"' }, logLevel: 'error',
})
const HTML = join(TMP, 'index.html')
writeFileSync(HTML, `<!doctype html><html data-theme="dark"><head><meta charset="utf-8">
<link rel="stylesheet" href="./bundle.css"></head>
<body style="margin:0"><div id="root"></div><script src="./bundle.js"></script></body></html>`)

const browser = await chromium.launch()
const page = await browser.newPage({ viewport: { width: 700, height: 220 } })
await page.goto('file://' + HTML)
await page.waitForSelector('.toggle')

// 自检:样式必须生效,否则颜色判断全是空值、结论不可信
const styled = await page.evaluate(() =>
  getComputedStyle(document.documentElement).getPropertyValue('--c-accent-rgb').trim())
if (!styled) {
  console.error('✗ 样式未生效(--c-accent-rgb 为空)—— 请检查 bundle.css 是否被引用')
  process.exit(2)
}

// 主色的 rgb 分量,用于判断边框/字色是否被染成主色
const ACC = await page.evaluate(() =>
  getComputedStyle(document.documentElement).getPropertyValue('--c-accent-rgb').trim())
const [ar, ag, ab] = ACC.split(',').map((s) => s.trim())

const CASES = [
  ['toggle', '.toggles .toggle'],
  ['rrule-mini', '.rrule-mini'],
  ['map-btn', '.map-btn'],
]

let bad = 0
for (const [name, sel] of CASES) {
  // 点两次:开 → 关(还原用户「取消勾选」的操作)
  await page.click(sel)
  await page.click(sel)
  // ⚠️ 必须再 hover 回来:点完下一个元素后鼠标就离开了这一个,
  // 不 hover 回来测到的就是非悬停态(本来就不是蓝的),等于没测。
  await page.hover(sel)
  await new Promise((r) => setTimeout(r, 200))

  const r = await page.evaluate((s) => {
    const el = document.querySelector(s)
    const cs = getComputedStyle(el)
    return {
      on: el.classList.contains('on'),
      border: cs.borderTopColor,
      color: cs.color,
      shadow: cs.boxShadow,
      hovered: el.matches(':hover'),
    }
  }, sel)

  const isAccent = (c) => {
    const m = c.match(/(\d+),\s*(\d+),\s*(\d+)/)
    if (!m) return false
    return Math.abs(+m[1] - ar) < 12 && Math.abs(+m[2] - ag) < 12 && Math.abs(+m[3] - ab) < 12
  }
  // 彩度 = RGB 极差。关闭态必须是**灰**(≈0),而不是"任何不是主色的颜色"。
  //
  // 只判"不含主色"太松 —— 浅蓝、青灰都能过,而那正是「一点也不明显」的来源:
  // 旧代码用带蓝调的 --line(#2a3240,极差 22)/ --fg-dim(#9aa7b4,极差 26),
  // 严格说也不是主色,却仍偏蓝,与 .on 的蓝边拉不开距离。
  //
  // 只判边框与字:map-btn 的底是半透明磨砂,混色后不具可比性。
  const chroma = (c) => {
    const m = c.match(/(\d+),\s*(\d+),\s*(\d+)/)
    if (!m) return 0
    const [r, g, b] = [+m[1], +m[2], +m[3]]
    return Math.max(r, g, b) - Math.min(r, g, b)
  }
  const offChroma = Math.max(chroma(r.border), chroma(r.color))
  // 阈值 18 —— 实测卡在新旧之间:
  //   新 --sw-off-* 最大 17(亮色 --sw-off-fg #8b939c)
  //   旧 --line/--fg-dim/--bg-2 最小 20(#1c2230)  主色则高达 179
  const GRAY = 18
  const bluish = !r.on && (isAccent(r.border) || isAccent(r.color) || /76,\s*141,\s*255/.test(r.shadow))
  const notGray = !r.on && offChroma > GRAY
  if (bluish || notGray) bad++
  console.log(
    `${name.padEnd(11)} on=${String(r.on).padEnd(5)} hover=${r.hovered ? 'Y' : 'N'}  `
    + `border=${r.border.padEnd(20)} color=${r.color.padEnd(20)} 彩度=${String(offChroma).padStart(3)}  `
    + `${bluish ? '✗ 关闭态仍带主色' : notGray ? `✗ 关闭态不够灰(彩度 ${offChroma} > ${GRAY})` : '✓'}`,
  )
}

await browser.close()
rmSync(TMP, { recursive: true, force: true })
console.log(bad ? `\n✗ ${bad} 个开关的关闭态不够"熄灭"(带主色或不够灰)` : '\n✓ 关闭态均为中性灰 —— 蓝只表示选中')
process.exit(bad ? 1 : 0)
