// 地图视口尺寸：真浏览器端到端验收。
//
//   npm run build            # 先出产物(Go 侧 embed 的是它)
//   启动后端(需带位置数据,可 -pcap 离线回放)
//   node scripts/verify-map-vp-browser.mjs
//
// 存在理由:jsdom 版的 verify-map-vp.mjs 有**两处覆盖不到**(它把 ResizeObserver mock 成
// 空操作,且 act() 会抹平 layout effect 的时序):
//   1. 测量用 useLayoutEffect 是否真让首帧带正确尺寸(换成 useEffect 会闪一帧塌图);
//   2. 忘记 disconnect 会不会残留旧 ResizeObserver(每次切页泄漏一个)。
// 这两项只有**真浏览器 + 真 ResizeObserver** 验得了。实测(把修复退回去):
//   - 退回 useEffect → 抓到 .map-world 宽度序列 `5, 3535`(先写一帧塌陷)
//   - 不 disconnect   → 抓到存活观察器 `1 → 4`(切三次页泄漏三个)
//   - 依赖为空数组     → 抓到 .map-world 宽 5px(即用户看到的故障形态)
//
// 测的是**真实部署形态**:Go 后端 embed 的构建产物(不是 vite dev server),
// 故本脚本同时覆盖了构建产物是否与源码一致。

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

// 探针要在页面脚本之前装好:统计 ResizeObserver 创建/断开,并记录 .map-world 被写入的宽度序列
// (第一次写入即首帧尺寸 —— 用 useEffect 的话会先写一帧塌陷的宽度)。
await page.addInitScript(() => {
  window.__ro = { made: 0, killed: 0 }
  window.__worldW = []
  const RO = window.ResizeObserver
  window.ResizeObserver = class extends RO {
    constructor(cb) { super(cb); window.__ro.made++ }
    disconnect() { window.__ro.killed++; return super.disconnect() }
  }
  const obs = new MutationObserver(() => {
    const w = document.querySelector('.map-world')
    if (w && w.style.width) window.__worldW.push(parseFloat(w.style.width))
  })
  document.addEventListener('DOMContentLoaded', () =>
    obs.observe(document.body, { subtree: true, attributes: true, attributeFilter: ['style'] }))
})

try {
  // 先在**别的页面**停一会儿:引擎常驻在 App 层,位置数据此时就到货了 ——
  // 这正是用户遇到故障的路径(人不在地图页,数据先到)。
  await page.goto(BASE + '/#/pets', { waitUntil: 'domcontentloaded' })
  await page.waitForTimeout(2500)

  await page.evaluate(() => { location.hash = '#/map' })
  await page.waitForSelector('.map-vp', { timeout: 15000 })
  await page.waitForTimeout(1500)

  const vp = await page.$eval('.map-vp', (el) => ({ w: el.clientWidth, h: el.clientHeight }))
  check('切到地图页后视口量到尺寸', vp.w > 100 && vp.h > 100, `vp=${vp.w}×${vp.h}`)

  const world = await page.$eval('.map-world', (el) => parseFloat(el.style.width) || 0)
  check('底图边长按视口算出(≈min(vp)×zoom)', world >= 2000, `.map-world 宽 ${Math.round(world)}px`)

  // —— 覆盖点 1:首帧无塌陷 ——
  const widths = await page.evaluate(() => window.__worldW)
  const firstBad = widths.find((w) => w > 0 && w < 1000)
  check('首帧无塌陷(未写过 <1000px 的宽度)',
    widths.length > 0 && firstBad === undefined,
    `宽度序列前 5 项: ${widths.slice(0, 5).map(Math.round).join(', ')}`)

  // —— 覆盖点 2:反复切页不堆积观察器 ——
  const roBefore = await page.evaluate(() => window.__ro.made - window.__ro.killed)
  for (let i = 0; i < 3; i++) {
    await page.evaluate(() => { location.hash = '#/pets' })
    await page.waitForTimeout(400)
    await page.evaluate(() => { location.hash = '#/map' })
    await page.waitForSelector('.map-vp', { timeout: 15000 })
    await page.waitForTimeout(400)
  }
  const roAfter = await page.evaluate(() => window.__ro.made - window.__ro.killed)
  check('反复切页不堆积观察器', roAfter <= roBefore + 1, `存活数 ${roBefore} → ${roAfter}`)

  const vp2 = await page.$eval('.map-vp', (el) => el.clientWidth)
  check('切回后视口仍有尺寸', vp2 > 100, `w=${vp2}`)

  check('页面无 JS 错误', errors.length === 0, errors.slice(0, 2).join(' | '))
} catch (e) {
  check('执行完成', false, String(e).split('\n')[0])
} finally {
  await browser.close()
}

const bad = results.filter((r) => !r.ok)
console.log(`\n${results.length - bad.length}/${results.length} 项通过`)
process.exit(bad.length ? 1 : 0)
