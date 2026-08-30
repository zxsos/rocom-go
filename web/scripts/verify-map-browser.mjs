// 地图页真浏览器端到端验收:视口尺寸 + 图标定位 + 底图加载。
//
//   npm run build && 启动后端(需位置数据,可 -pcap 回放)
//   node scripts/verify-map-browser.mjs
//
// 与 verify-map-vp-browser.mjs 的分工:那个专测**视口尺寸的测量时机**(含 jsdom 覆盖不到的
// layout effect 时序与观察器泄漏);这个测**页面最终画出来对不对** —— 图标有没有散落在
// 该在的位置、底图是否真的加载出来了。两者合起来才覆盖用户报的
// 「图标全缩到左上角 + 地图背景黑」。
//
// 判据取自真实几何:图标按 left = u*mapPx 定位,若视口没量到尺寸,mapPx 塌成 ~5px,
// 所有图标的 left/top 都会挤在 0 附近 —— 用「图标坐标的离散度」区分正常与塌缩,
// 比断言某个具体数值稳(坐标随抓包数据变)。

import { chromium } from 'playwright'

const BASE = process.env.E2E_BASE || 'http://localhost:4939'
const results = []
const check = (name, ok, detail) => {
  results.push({ name, ok })
  console.log(`${ok ? '  ok  ' : ' FAIL '} ${name}${detail ? '  — ' + detail : ''}`)
}

const browser = await chromium.launch({ args: ['--no-sandbox', '--disable-dev-shm-usage'] })
const page = await browser.newPage({ viewport: { width: 1280, height: 800 } })
const errors = []
page.on('pageerror', (e) => errors.push(String(e).split('\n')[0]))
page.on('console', (m) => { if (m.type() === 'error') errors.push(m.text().slice(0, 200)) })

try {
  // 先在别页停留:复现「人不在地图页、位置数据已到货」的路径
  await page.goto(BASE + '/#/pets', { waitUntil: 'domcontentloaded' })
  await page.waitForTimeout(2500)
  await page.evaluate(() => { location.hash = '#/map' })
  await page.waitForSelector('.map-vp', { timeout: 15000 })
  await page.waitForTimeout(2000)

  // —— 底图:真的加载出来了吗(用户描述是「背景黑」)——
  const img = await page.$eval('.map-base', (el) => ({
    complete: el.complete, w: el.naturalWidth, h: el.naturalHeight, src: el.getAttribute('src'),
  })).catch(() => null)
  check('底图已加载', !!img && img.complete && img.w > 0, img ? `${img.w}×${img.h}` : '没有 .map-base')

  const shown = await page.$eval('.map-base', (el) => {
    const r = el.getBoundingClientRect()
    return { w: Math.round(r.width), h: Math.round(r.height) }
  }).catch(() => null)
  check('底图绘制尺寸与 mapPx 相符(数千 px)', !!shown && shown.w >= 1000, shown ? `${shown.w}×${shown.h}` : '-')

  // —— 图标:散落还是挤在左上角 ——
  const marks = await page.evaluate(() => {
    const sel = ['.map-poi', '.map-wild', '.map-nest']
    const out = {}
    for (const s of sel) {
      const els = [...document.querySelectorAll(s)]
      if (!els.length) { out[s] = null; continue }
      const pts = els.map((e) => ({ x: parseFloat(e.style.left) || 0, y: parseFloat(e.style.top) || 0 }))
      const xs = pts.map((p) => p.x), ys = pts.map((p) => p.y)
      out[s] = {
        n: els.length,
        xSpan: Math.max(...xs) - Math.min(...xs),
        ySpan: Math.max(...ys) - Math.min(...ys),
        maxX: Math.max(...xs),
      }
    }
    return out
  })
  const present = Object.entries(marks).filter(([, v]) => v && v.n > 0)
  check('至少有一类地图标记渲染出来', present.length > 0,
    Object.entries(marks).map(([k, v]) => `${k}=${v ? v.n : 0}`).join(' '))
  // 有标记且数量 >=3 才判离散度:两颗点也可能天然靠得近
  const spread = present.filter(([, v]) => v.n >= 3)
  if (spread.length === 0) {
    check('标记坐标离散度(需 >=3 个同类标记)', true, '本份数据不足,跳过')
  } else {
    for (const [sel, v] of spread) {
      check(`${sel} 未塌缩到一点`, (v.xSpan + v.ySpan) > 50,
        `n=${v.n} x跨度=${Math.round(v.xSpan)} y跨度=${Math.round(v.ySpan)}`)
    }
  }

  // —— 箭头(玩家标记)——
  const arrow = await page.$eval('.map-arrow', (el) => el.style.transform || '').catch(() => '')
  check('玩家箭头已定位', /translate3d/.test(arrow), arrow.slice(0, 60))

  // —— 头像标记无实心底色 ——
  // 头像靠 .map-wild-face 的 radial-gradient 羽化边缘融入底图,但前提是**背后没有实心底色**:
  // 否则边缘是渐隐到那块底色上,看起来仍是一整块不透明圆片(即「周边不透明」)。
  // 故这里断言计算后的背景是透明的 —— 改回 background: var(--bg-1) 会被抓到。
  // 只查实际渲染出来的元素(本份抓包没有小窝/全部野生,缺样本则跳过,不误报)。
  const opaque = await page.evaluate(() => {
    const out = []
    for (const sel of ['.map-wild', '.map-nest']) {
      for (const el of document.querySelectorAll(sel)) {
        const bg = getComputedStyle(el).backgroundColor
        // 透明色的规范形式:rgba(0, 0, 0, 0) / transparent
        const a = /^rgba?\(([^)]+)\)$/.exec(bg)
        const alpha = a ? (a[1].split(',')[3] != null ? parseFloat(a[1].split(',')[3]) : 1) : 1
        if (alpha > 0.01) out.push(`${sel} 背景 ${bg}`)
      }
    }
    return out
  })
  const sample = await page.evaluate(() =>
    [...document.querySelectorAll('.map-wild, .map-nest')].length)
  if (sample === 0) {
    check('头像标记无实心底色', true, '本份数据无样本,跳过')
  } else {
    check('头像标记无实心底色', opaque.length === 0, opaque.slice(0, 2).join(' | ') || `已查 ${sample} 个标记`)
  }

  check('页面无 JS 错误', errors.length === 0, errors.slice(0, 2).join(' | '))
} catch (e) {
  check('执行完成', false, String(e).split('\n')[0])
} finally {
  await browser.close()
}

const bad = results.filter((r) => !r.ok)
console.log(`\n${results.length - bad.length}/${results.length} 项通过`)
process.exit(bad.length ? 1 : 0)
