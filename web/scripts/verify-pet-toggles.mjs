// 精灵列表筛选面板的「按键式筛选开关」验收(需要 playwright + chromium)。
//
//   node scripts/verify-pet-toggles.mjs      (npm run verify:browser 已带上)
//
// 守的是三条契约:
//
//   1. **整块按键都是命中区**。原生 checkbox/radio 的命中区只有 ~13px,远低于
//      项目的 36px 触控基线;改成按键后整块 ≥32px(移动端 36px)都要能点。
//   2. **保留原生语义**。input 只是视觉隐藏,Tab 可达、空格可切换、读屏会播报
//      「已选中」—— 自制 div 开关最常见的可访问性缺失就是丢这些。
//   3. **选中态可读**。由 React 显式加 .on,不依赖 :has() —— 老浏览器上最多个
//      焦点环不显示,不会把「选中了没」整个显示错(那是信息错误,严重得多)。
//
// 为什么必须用**真实组件**渲染:这些是 React 受控组件(input 的 checked 由
// props 决定),手造 DOM 拿不到真实的选中态与类型分布。
//
// ⚠️ 两条坑(都曾误报失败,已修正并写进注释):
//   1. input 是 absolute inset:0,相对 **padding box** 定位,而
//      getBoundingClientRect 取 border-box —— 按键有 1px 边框,天然差 2px。
//      断言要容这 2px,否则永远红。
//   2. 量尺寸不如真点击可靠。故本脚本额外用 mouse.click 点按键左右边缘,
//      验证真的能切换(这才是不变量本身)。
import { chromium } from 'playwright'
import { readFileSync } from 'node:fs'
import { createServer } from 'vite'
import { renderToStaticMarkup } from 'react-dom/server'

const React = (await import('react')).default

const server = await createServer({
  root: process.cwd(), logLevel: 'error',
  server: { middlewareMode: true, hmr: false }, optimizeDeps: { noDiscovery: true },
})
const { IconsContext } = await server.ssrLoadModule('/src/context.js')
const FilterPanel = (await server.ssrLoadModule('/src/pages/pet-list/FilterPanel.jsx')).default

// 三个选中 + 若干未选,覆盖两种状态
const filter = {
  page: 1, pageSize: 20, sort: 'boxpos', order: 'asc',
  shiny: '1', gender: '♂', medalBig: '1',
}
const panelHTML = renderToStaticMarkup(
  React.createElement(IconsContext.Provider, { value: {} },
    React.createElement(FilterPanel, {
      filter,
      // natureMatrix 必须给 6×6 结构:NatureMatrix 会直接下标访问 matrix[i][j],
      // 传空数组会在渲染时抛 TypeError(曾踩过)。
      options: {
        natureMatrix: Array.from({ length: 6 }, () => new Array(6).fill('')),
        box: [], talentRank: [], speciality: [], medal: [],
      },
      total: 983, collapsed: false, onClose: () => {},
      set: () => {}, toggleType: () => {}, reset: () => {},
    })),
)
await server.close()

const css = ['base', 'pet', 'list', 'panel', 'dropdown']
  .map((f) => readFileSync(`src/styles/${f}.css`, 'utf8')).join('\n')

let fail = 0
const ok = (c, l, x = '') => {
  console.log(`  ${c ? 'OK  ' : 'FAIL'} ${l}${x ? ' — ' + x : ''}`)
  if (!c) fail++
}

// 期望的开关构成:变异 2(多选) + 性别 3(单选) + 奖牌特征 4(多选)
const EXPECT = { total: 9, checked: 3, checkbox: 6, radio: 3 }

const browser = await chromium.launch()

for (const [w, dpr, minH] of [[1200, 1, 32], [390, 2, 36]]) {
  const page = await browser.newPage({ viewport: { width: w, height: 900 }, deviceScaleFactor: dpr })
  await page.setContent(`<!doctype html><html><head><meta charset="utf-8"><style>${css}</style></head>
<body style="margin:0;padding:12px"><div style="width:${w}px">${panelHTML}</div></body></html>`)

  const r = await page.evaluate(() => {
    const ts = [...document.querySelectorAll('.toggle')]
    return {
      count: ts.length,
      boxes: ts.map((t) => {
        const b = t.getBoundingClientRect()
        const inp = t.querySelector('input')
        const ib = inp.getBoundingClientRect()
        return {
          txt: t.textContent.trim(),
          h: +b.height.toFixed(0), w: +b.width.toFixed(0),
          on: t.classList.contains('on'),
          type: inp.type,
          // 容 2px:absolute inset:0 相对 padding box,border-box 天然多 2px(见文件头)
          cover: ib.width >= b.width - 2.5 && ib.height >= b.height - 2.5,
          hidden: getComputedStyle(inp).opacity === '0',
        }
      }),
    }
  })
  const min = Math.min(...r.boxes.map((b) => b.h))
  console.log(`\n=== 视口 ${w} (触控基线 ${minH}px) ===`)
  console.log(`  ${r.boxes.map((b) => `${b.txt}${b.w}×${b.h}${b.on ? '✓' : ''}`).join('  ')}`)
  ok(r.count === EXPECT.total, `开关数 ${EXPECT.total}`, String(r.count))
  ok(min >= minH, `最小高度 ≥ ${minH}px`, `实测 ${min}px`)
  ok(r.boxes.every((b) => b.cover), 'input 覆盖整块(整块可点)')
  ok(r.boxes.every((b) => b.hidden), 'input 视觉隐藏')
  ok(r.boxes.filter((b) => b.on).length === EXPECT.checked, `选中 ${EXPECT.checked} 个`, String(r.boxes.filter((b) => b.on).length))
  ok(r.boxes.filter((b) => b.type === 'checkbox').length === EXPECT.checkbox
    && r.boxes.filter((b) => b.type === 'radio').length === EXPECT.radio,
  `类型分布 checkbox ${EXPECT.checkbox} / radio ${EXPECT.radio}`)

  // 真实点击:左右边缘都要能切换
  const bx = await page.evaluate(() => {
    const b = document.querySelector('.toggle').getBoundingClientRect()
    return { x: b.x, y: b.y, w: b.width, h: b.height }
  })
  const s0 = await page.evaluate(() => document.querySelector('.toggle input').checked)
  await page.mouse.click(bx.x + 2, bx.y + bx.h / 2)
  const s1 = await page.evaluate(() => document.querySelector('.toggle input').checked)
  await page.mouse.click(bx.x + bx.w - 2, bx.y + bx.h / 2)
  const s2 = await page.evaluate(() => document.querySelector('.toggle input').checked)
  ok(s1 !== s0 && s2 !== s1, '左右边缘点击都能切换', `${s0}→${s1}→${s2}`)

  await page.close()
}

// —— 键盘可达性(原生语义是否还在)——
{
  const page = await browser.newPage({ viewport: { width: 1200, height: 900 } })
  await page.setContent(`<!doctype html><html><head><meta charset="utf-8"><style>${css}</style></head>
<body style="margin:0;padding:12px"><div style="width:1200px">${panelHTML}</div></body></html>`)
  console.log('\n=== 键盘可达性(原生语义) ===')
  const f = await page.evaluate(() => {
    const inp = document.querySelector('.toggle input')
    inp.focus()
    // radio 需要 name 才能成组;checkbox 不需要
    return { focused: document.activeElement === inp, tabIndex: inp.tabIndex }
  })
  ok(f.focused, 'input 可获得焦点(Tab 可达)')
  ok(f.tabIndex >= 0, 'input 在 Tab 序列中')
  const before = await page.evaluate(() => document.querySelector('.toggle input').checked)
  await page.keyboard.press('Space')
  const after = await page.evaluate(() => document.querySelector('.toggle input').checked)
  ok(before !== after, '空格键可切换', `${before} → ${after}`)
  // 单选组:name 相同才是"一组",否则三个 radio 能同时选中
  const names = await page.evaluate(() =>
    [...document.querySelectorAll('.toggle input[type=radio]')].map((i) => i.name))
  ok(names.length === 3 && new Set(names).size === 1, '三个 radio 同属一组(name 相同)', names.join(','))
  await page.close()
}

await browser.close()
console.log(fail ? `\n=== ${fail} 项失败 ===` : '\n=== 全部通过 ===')
process.exit(fail ? 1 : 0)
