// 顶栏账号切换器布局验收。
//
//   npm run build && 启动后端
//   node scripts/verify-account-width.mjs [后端地址]
//
// 触发条的内容**两端不同**,改任何一端都要看这个脚本的对应分支:
//   桌面:徽章 → 称号 → 昵称 → UID
//   手机:徽章 → 称号 → UID(昵称不渲染,顶栏横向放不下;昵称进了 title/aria-label,
//        点开 sheet 每行也写得很清楚)
// PIN 锁不在这一层 —— 它挂在头像左下角(AccountAvatar 的 pin 参数)。
//
// 判据(都是用户明确提过的约束,漏了看得见):
//   1. 桌面端触发条 DOM 顺序必须是 徽章 → 称号 → 昵称 → UID;
//   2. 短于 5 个字的昵称,触发条宽度**一致** —— 否则切换账号时顶栏一档一档变宽变窄,
//      紧邻的全屏/主题按钮跟着抖;
//   3. 手机端触发条**不渲染昵称**但**渲染 UID**(早期版本是反过来的:
//      显示昵称、把 UID 藏了;那版 UID 换行会把 sticky 顶栏撑高,各页高度不一致);
//   4. 手机端触发条高度**不随内容长度变化**(顶栏高度稳定);
//   5. 超长昵称(桌面端)要截断出省略号,而不是把顶栏撑破;
//   6. 徽章头像渲染出来了,且带 .privacy(截图防泄)而在线小点不带。
//
// 四个坑,都踩过:
//   1. 5 字基准要落在**昵称**(.acct-name)上,不能落在 .account-trigger-name 上。
//      桌面端后者还含 UID 那截(约 114px),给它设 5em 等于把 UID 也算进 5 个字里,
//      短昵称照样一档一档变宽(实测 1/2/3/5 字 → 239/253/267/295)。
//   2. 截断看的是**外层** .account-trigger-name 的 scrollWidth/clientWidth,
//      不是内层 .acct-name —— 内层是 inline-block,永远撑满内容宽、自身从不截断,
//      拿它当探针会得到「16 字也不截断」的假结论。
//   3. 顺序按 **DOM 顺序** 判,不能按 left 比大小:徽章/称号都是小元素,
//      几何位置判容易误报。这里用 compareDocumentPosition 确认先后关系。
//   4. 跑之前确认后端提供的是**最新产物**(比对 bundle 文件名)。曾有一次变异验证
//      因旧进程仍占端口、读到旧 CSS,得出「变异未被抓到」的错误结论。
//
// 三个视口都测:桌面与手机的宽度构成不同(坑 1、3 都只在特定端暴露)。

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
        // 顺序按 DOM 顺序判(见坑 3)
        const ordered = (() => {
          const avatar = q('.account-trigger .acct-avatar')
          const titleEl = q('.account-trigger .rank-title')
          const nameItem = q('.acct-name')
          const uidEl = q('.account-trigger .acct-uid')
          if (!avatar) return null
          // 手机端不渲染昵称,顺序退化为 徽章 → [称号] → UID
          const last = nameItem || uidEl
          if (!last) return null
          const before = (a, b) => !!(a.compareDocumentPosition(b) & Node.DOCUMENT_POSITION_FOLLOWING)
          let ok = before(avatar, last)
          if (titleEl) ok = ok && before(avatar, titleEl) && before(titleEl, last)
          // 昵称与 UID 同时存在时(桌面端),UID 必须排在昵称之后
          if (nameItem && uidEl) ok = ok && before(nameItem, uidEl)
          return { ok, hasUid: !!uidEl, hasName: !!nameItem }
        })()
        return {
          len: n.length,
          sel: Math.round(q('.account-select').getBoundingClientRect().width),
          triggerH: Math.round(q('.account-trigger').getBoundingClientRect().height),
          barH: Math.round(bar.getBoundingClientRect().height),
          ordered,
          truncated: nameEl ? nameEl.scrollWidth > nameEl.clientWidth + 1 : null,
          overflow: bar.scrollWidth > bar.clientWidth + 1,
          brand: Math.round(q('.brand').getBoundingClientRect().width),
        }
      }, name))
    }

    check(`${vp.n} 元素顺序为 徽章→[称号]→[昵称]→[UID]`,
      rows.every((r) => r.ordered && r.ordered.ok),
      rows[0].ordered ? '' : '(无徽章,跳过)')

    if (vp.mobile) {
      // 手机端:**不渲染昵称、渲染 UID**。
      // 早期版本是反过来的(显示昵称、藏 UID),那版 UID 换行会把 sticky 顶栏撑高,
      // 各页高度不一致、滚动时有跳动感,故改成藏昵称留 UID。
      check(`${vp.n} 触发条不渲染昵称`,
        rows.every((r) => r.ordered && !r.ordered.hasName),
        rows[0].ordered?.hasName ? '仍渲染了昵称' : '')
      check(`${vp.n} 触发条渲染 UID(唯一的文本标识)`,
        rows.every((r) => r.ordered && r.ordered.hasUid),
        rows[0].ordered?.hasUid ? '' : 'UID 没渲染')
      const heights = new Set(rows.map((r) => r.triggerH))
      const barHeights = new Set(rows.map((r) => r.barH))
      check(`${vp.n} 顶栏高度不随内容长度变化`,
        heights.size === 1 && barHeights.size === 1,
        `trigger ${[...heights].join('/')} · topbar ${[...barHeights].join('/')}`)
    } else {
      // 下面两条**只在桌面端有意义**:手机端不渲染昵称,改 .acct-name 的文本
      // 不会引起任何宽度变化,两条都会恒绿 —— 假绿灯比不测更糟(它让人以为验过了)。
      const shortW = rows.filter((r) => r.len <= 5).map((r) => r.sel)
      const uniq = [...new Set(shortW)]
      check(`${vp.n} 短昵称(≤5字)宽度一致`, uniq.length === 1,
        uniq.length === 1 ? `${uniq[0]}px` : shortW.join('/'))

      const w5 = rows.find((r) => r.len === 5).sel
      const w8 = rows.find((r) => r.len === 8).sel
      check(`${vp.n} 长于 5 字能自适应增长`, w8 >= w5, `5字 ${w5} → 8字 ${w8}`)

      check(`${vp.n} 昵称与 UID 同排(桌面端都保留)`,
        rows.every((r) => r.ordered?.hasName && r.ordered?.hasUid),
        `hasName=${rows[0].ordered?.hasName} hasUid=${rows[0].ordered?.hasUid}`)
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

// 超长昵称必须截断出省略号 —— 单独测,因为上面的样本最长才 11 字。
//
// 字数要取**足够长**:桌面端触发条的可用宽比手机端大得多(实测 258px vs 130px),
// 16 个字在桌面只有 224px、根本撑不破 —— 那条会假绿成「不截断」(曾经就这么误报过)。
// 取 40 字:远超桌面端可用宽,测到的才是真截断。
//
// 只测桌面端:手机端压根不渲染昵称(见上面 mobile 分支),这里 querySelector
// 拿不到元素,断言会直接抛;而手机端真正要防的是「UID 把顶栏撑破」,
// 那由上面的「顶栏不溢出 / brand 未被挤」覆盖。
const LONG_NAME = '啊'.repeat(40)
for (const vp of [{ w: 1280, h: 800, n: '桌面 1280' }]) {
  const page = await browser.newPage({ viewport: { width: vp.w, height: vp.h }, deviceScaleFactor: 2 })
  try {
    await page.goto(BASE + '/#/pets', { waitUntil: 'domcontentloaded' })
    await page.waitForSelector('.account-select', { timeout: 20000 })
    await page.waitForTimeout(2500)
    await page.evaluate((n) => {
      const el = document.querySelector('.acct-name')
      if (el) el.textContent = n
    }, LONG_NAME)
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
      }
    })
    check(`${vp.n} 超长昵称(${LONG_NAME.length}字)截断且省略号三件套齐全`,
      m.truncated && m.ellipsis, `可见 ${m.visible} / 需 ${m.need}, 三件套 ${m.ellipsis ? '齐' : '缺'}`)
    check(`${vp.n} 超长昵称不撑破顶栏`, !m.overflow)
  } catch (e) {
    check(`${vp.n} 超长昵称 执行完成`, false, String(e).split('\n')[0])
  } finally {
    await page.close()
  }
}

// 徽章头像:渲染出来了、首字带 .privacy(参与截图防泄)、在线小点不带
{
  const page = await browser.newPage({ viewport: { width: 1280, height: 800 }, deviceScaleFactor: 2 })
  try {
    await page.goto(BASE + '/#/pets', { waitUntil: 'domcontentloaded' })
    await page.waitForSelector('.account-select', { timeout: 20000 })
    await page.waitForTimeout(2500)
    const m = await page.evaluate(() => {
      const av = document.querySelector('.account-trigger .acct-avatar')
      if (!av) return { missing: true }
      const txt = av.querySelector('.acct-avatar-txt')
      const dot = av.querySelector('.acct-avatar-dot')
      const r = av.getBoundingClientRect()
      return {
        size: Math.round(r.width),
        round: getComputedStyle(av).borderRadius,
        hue: getComputedStyle(av).getPropertyValue('--acct-h').trim(),
        initial: txt?.textContent || '',
        // 首字必须带 .privacy(昵称首字同样是可识别信息,要跟着全局遮罩一起糊);
        // 在线小点必须**不带**(不敏感,且是扫视时最先要确认的东西)
        txtPrivacy: !!txt && txt.classList.contains('privacy'),
        dotPrivacy: !!dot && dot.classList.contains('privacy'),
      }
    })
    check('徽章头像已渲染(圆形、有色相、有首字)',
      !m.missing && m.size >= 20 && m.round.includes('50%') && m.hue !== '' && m.initial !== '',
      m.missing ? '未渲染' : `${m.size}px · --acct-h=${m.hue} · 首字「${m.initial}」`)
    check('首字带 .privacy、在线小点不带', m.txtPrivacy && !m.dotPrivacy,
      `首字 ${m.txtPrivacy ? '有' : '无'} / 小点 ${m.dotPrivacy ? '有(不该有)' : '无'}`)
  } catch (e) {
    check('徽章头像 执行完成', false, String(e).split('\n')[0])
  } finally {
    await page.close()
  }
}

await browser.close()

const bad = results.filter((r) => !r.ok)
console.log(`\n${results.length - bad.length}/${results.length} 项通过`)
process.exit(bad.length ? 1 : 0)
