// 顶栏账号下拉布局验收:称号 → 锁 → 昵称 → UID;手机端 UID 换到第二行;宽度按内容且有上限。
//
//   npm run build && 启动后端
//   node scripts/verify-account-width.mjs [后端地址]
//
// 判据(都是用户明确提过的约束,漏了看得见):
//   1. 元素顺序必须是 称号 → 昵称 → UID;
//   2. 短于 5 个字的昵称,下拉宽度**一致** —— 否则切换账号时下拉一档一档变宽变窄,
//      顶栏跟着抖(右侧就是全屏/主题按钮);
//   3. 手机端 UID **换到第二行**(不是藏掉、也不是挤在同一行);
//   4. 超长昵称要截断出省略号,而不是把顶栏撑破或把昵称挤到下一行。
//
// 四个坑,都踩过:
//   1. 5 字基准要落在**昵称**(.acct-name)上,不能落在 .account-trigger-name 上。
//      桌面端后者还含 UID 那截(约 114px),给它设 5em 等于把 UID 也算进 5 个字里,
//      短昵称照样一档一档变宽(实测 1/2/3/5 字 → 239/253/267/295)。
//   2. 截断看的是**外层** .account-trigger-name 的 scrollWidth/clientWidth,
//      不是内层 .acct-name —— 内层是 inline-block,永远撑满内容宽、自身从不截断,
//      拿它当探针会得到「16 字也不截断」的假结论。
//   3. flex-basis 必须是 **0** 而不是 auto:手机端为了换行开了 flex-wrap,而 wrap
//      容器里**换行优先于收缩** —— basis:auto 时放不下就直接换行,省略号永远不出现
//      (实测 16 字时 trigger 高从 81 涨到 105、昵称被整条挤到第二行)。
//   4. 跑之前确认后端提供的是**最新产物**(比对 bundle 文件名)。曾有一次变异验证
//      因旧进程仍占端口、读到旧 CSS,得出「变异未被抓到」的错误结论。
//
// 三个视口都测:桌面单行、手机两行,宽度构成不同(坑 1、3 都只在特定端暴露)。

import { createRequire } from 'node:module'
const require = createRequire(import.meta.url)
const { chromium } = require('playwright')

const BASE = process.argv[2] || process.env.E2E_BASE || 'http://localhost:4939'
// 覆盖「短于基准 / 等于基准 / 长于基准」三档
const NAMES = ['一', '俩字', '仨字呀', '一二三四五', '一二三四五六七八', '一二三四五六七八九十十一']

const results = []
const check = (name, ok, detail) => {
  results.push({ name, ok })
  console.log(`${ok ? '  ok  ' : ' FAIL '} ${name}${detail ? '  — ' + detail : ''}`)
}

const browser = await chromium.launch({ args: ['--no-sandbox', '--disable-dev-shm-usage'] })

for (const vp of [
  { w: 1280, h: 800, n: '桌面 1280', mobile: false },
  { w: 360, h: 800, n: '安卓 360', mobile: true },
  { w: 390, h: 844, n: 'iPhone 390', mobile: true },
]) {
  const page = await browser.newPage({ viewport: { width: vp.w, height: vp.h }, deviceScaleFactor: 2 })
  try {
    await page.goto(BASE + '/#/pets', { waitUntil: 'domcontentloaded' })
    await page.waitForSelector('.account-select', { timeout: 20000 })
    await page.waitForTimeout(2500)

    const rows = []
    for (const name of NAMES) {
      await page.evaluate((n) => {
        const el = document.querySelector('.acct-name')
        if (el) el.textContent = n
      }, name)
      await page.waitForTimeout(200)
      rows.push(await page.evaluate((n) => {
        const q = (s) => document.querySelector(s)
        const bar = q('.topbar')
        const nameEl = q('.account-trigger-name') // 截断看外层(见坑 2)
        const nb = q('.acct-name')?.getBoundingClientRect()
        const ub = q('.acct-uid')?.getBoundingClientRect()
        return {
          len: n.length,
          sel: Math.round(q('.account-select').getBoundingClientRect().width),
          triggerH: Math.round(q('.account-trigger').getBoundingClientRect().height),
          // 顺序按 **DOM 顺序** 判,不能按 left 比大小:手机端 UID 换行后 left 回到行首
          // (实测 146 < 昵称的 223),按几何位置判会误报。这里用 compareDocumentPosition
          // 确认 称号→昵称→UID 的先后关系。
          ordered: (() => {
            const titleEl = q('.rank-title')
            const nameItem = q('.acct-name')
            const uidEl = q('.acct-uid')
            if (!titleEl || !nameItem || !uidEl) return null
            const before = (a, b) =>
              !!(a.compareDocumentPosition(b) & Node.DOCUMENT_POSITION_FOLLOWING)
            return before(titleEl, nameItem) && before(nameItem, uidEl)
          })(),
          // 换行:UID 的 top 明显低于昵称的 top
          wrapped: ub && nb ? ub.top > nb.top + 4 : null,
          truncated: nameEl ? nameEl.scrollWidth > nameEl.clientWidth + 1 : null,
          overflow: bar.scrollWidth > bar.clientWidth + 1,
          brand: Math.round(q('.brand').getBoundingClientRect().width),
        }
      }, name))
    }

    const shortW = rows.filter((r) => r.len <= 5).map((r) => r.sel)
    const uniq = [...new Set(shortW)]
    check(`${vp.n} 短昵称(≤5字)宽度一致`, uniq.length === 1,
      uniq.length === 1 ? `${uniq[0]}px` : shortW.join('/'))

    const w5 = rows.find((r) => r.len === 5).sel
    const w8 = rows.find((r) => r.len === 8).sel
    check(`${vp.n} 长于 5 字能自适应增长`, w8 >= w5, `5字 ${w5} → 8字 ${w8}`)

    check(`${vp.n} 元素顺序为 称号→昵称→UID`,
      rows.every((r) => r.ordered !== false),
      rows[0].ordered === null ? '(本份数据无称号,跳过)' : '')

    if (vp.mobile) {
      const heights = new Set(rows.map((r) => r.triggerH))
      check(`${vp.n} UID 换到第二行(且昵称不被挤下去)`,
        rows.every((r) => r.wrapped) && heights.size === 1,
        `trigger 高 ${[...heights].join('/')}`)
    } else {
      check(`${vp.n} UID 与昵称同排(不换行)`,
        rows.every((r) => !r.wrapped), `trigger 高 ${rows[0].triggerH}`)
    }

    check(`${vp.n} 顶栏不溢出 / brand 未被挤`,
      rows.every((r) => !r.overflow) && new Set(rows.map((r) => r.brand)).size === 1,
      `brand ${rows[0].brand}px`)
  } catch (e) {
    check(`${vp.n} 执行完成`, false, String(e).split('\n')[0])
  } finally {
    await page.close()
  }
}

// 超长昵称(16 字)必须截断出省略号 —— 单独测,因为上面的样本最长才 11 字
{
  const page = await browser.newPage({ viewport: { width: 360, height: 800 }, deviceScaleFactor: 2 })
  try {
    await page.goto(BASE + '/#/pets', { waitUntil: 'domcontentloaded' })
    await page.waitForSelector('.account-select', { timeout: 20000 })
    await page.waitForTimeout(2500)
    await page.evaluate(() => {
      const el = document.querySelector('.acct-name')
      if (el) el.textContent = '啊'.repeat(16)
    })
    await page.waitForTimeout(400)
    const m = await page.evaluate(() => {
      const outer = document.querySelector('.account-trigger-name')
      const bar = document.querySelector('.topbar')
      const s = getComputedStyle(outer)
      return {
        truncated: outer.scrollWidth > outer.clientWidth + 1,
        need: outer.scrollWidth, visible: outer.clientWidth,
        ellipsis: s.textOverflow === 'ellipsis' && s.whiteSpace === 'nowrap' && s.overflow === 'hidden',
        overflow: bar.scrollWidth > bar.clientWidth + 1,
        triggerH: Math.round(document.querySelector('.account-trigger').getBoundingClientRect().height),
      }
    })
    check('超长昵称(16字)截断且省略号三件套齐全',
      m.truncated && m.ellipsis, `可见 ${m.visible} / 需 ${m.need}, 三件套 ${m.ellipsis ? '齐' : '缺'}`)
    check('超长昵称不撑破顶栏、不把昵称挤到下一行',
      !m.overflow && m.triggerH < 100, `trigger 高 ${m.triggerH}`)
  } catch (e) {
    check('超长昵称 执行完成', false, String(e).split('\n')[0])
  } finally {
    await page.close()
  }
}

await browser.close()

const bad = results.filter((r) => !r.ok)
console.log(`\n${results.length - bad.length}/${results.length} 项通过`)
process.exit(bad.length ? 1 : 0)
