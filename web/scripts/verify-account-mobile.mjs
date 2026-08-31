// 手机端账号 sheet 验收(AccountSheet):从底部升起、portal 到 body、大热区、三种退出方式。
//
//   npm run build && 启动后端
//   node scripts/verify-account-mobile.mjs [后端地址]
//
// 判据(都是方案里明确写下的约束,漏了看得见):
//   1. sheet 挂在 document.body 下,**不在 .topbar 内** ——
//      .topbar 有 backdrop-filter,会成为 fixed 后代的包含块,留在里面会相对顶栏定位;
//   2. 面板贴底、宽度撑满视口、顶部圆角;
//   3. 账号行高 ≥ 52px(移动端触控基线之上再放一档);
//   4. 点遮罩 / 点关闭按钮 / 按 Esc 三种方式都能退出;
//   5. 打开期间 documentElement 的 overflow 被锁成 hidden,关闭后**还原**;
//   6. 键盘 ↑↓ + Enter 能切账号,切完顶栏昵称跟着变;
//   7. 桌面视口(>760px)**不**出 sheet,仍是锚定浮层。
//
// 三个易错点:
//   1. 测退出必须**等卸载**:sheet 有 180ms 退出动画,点完立刻断言还在 DOM 里是正常现象,
//      但 300ms 后必须消失。曾据此误判「关不掉」。
//   2. 键盘事件要派发到 .account-wrap 内的可聚焦元素上:onKeyDown 挂在 .account-wrap,
//      React 事件沿 React 树冒泡,portal 出去的 sheet 里的元素照样能触发 —— 但必须先
//      让焦点落在 .account-wrap 内部(点触发条即可)。
//   3. 跑之前确认后端提供的是**最新产物**(比对 bundle 文件名)。曾因旧进程仍占端口、
//      读到旧 CSS,得出「变异未被抓到」的错误结论。

import { createRequire } from 'node:module'
const require = createRequire(import.meta.url)
const { chromium } = require('playwright')

const BASE = process.argv[2] || process.env.E2E_BASE || 'http://localhost:4939'

const results = []
const check = (name, ok, detail) => {
  results.push({ name, ok })
  console.log(`${ok ? '  ok  ' : ' FAIL '} ${name}${detail ? '  — ' + detail : ''}`)
}
// skip 用于「前置数据不满足」(如库里没有设 PIN 的账号、只有一个账号):
// 那不是产品缺陷,不该染红 CI;但必须**显式报出来**,否则会让人误以为验过了。
const skipped = []
const skip = (name, detail) => {
  skipped.push(name)
  console.log(` skip  ${name}${detail ? '  — ' + detail : ''}`)
}

const browser = await chromium.launch({ args: ['--no-sandbox', '--disable-dev-shm-usage'] })
// iPhone 尺寸 + 触屏:sheet 分支由 matchMedia('(max-width: 760px)') 决定,与触屏无关
const page = await browser.newPage({ viewport: { width: 390, height: 844 }, deviceScaleFactor: 2, hasTouch: true })

const openSheet = async () => {
  await page.click('.account-trigger')
  await page.waitForSelector('.account-sheet', { timeout: 5000 })
  await page.waitForTimeout(350) // 等上滑动画(.28s)跑完,否则测到的是动画中的位置
}
const sheetGone = async () => {
  await page.waitForTimeout(320) // 等退出动画(.18s)+ 卸载
  return (await page.locator('.account-sheet').count()) === 0
}

try {
  await page.goto(BASE + '/#/pets', { waitUntil: 'domcontentloaded' })
  await page.waitForSelector('.account-select', { timeout: 20000 })
  await page.waitForTimeout(2500)

  const accountCount = await page.locator('.account-trigger').count()
  if (!accountCount) throw new Error('顶栏无账号切换器(后端没返回账号?)')

  // ---- 打开后:结构 ----
  await openSheet()

  const geo = await page.evaluate(() => {
    const sheet = document.querySelector('.account-sheet')
    const scrim = document.querySelector('.account-scrim')
    const bar = document.querySelector('.topbar')
    const r = sheet.getBoundingClientRect()
    const items = [...document.querySelectorAll('.account-sheet .account-item')]
    const cs = getComputedStyle(sheet)
    return {
      // 挂在 body 下、不在 topbar 内 —— 这是 portal 的核心目的
      inBody: sheet.parentElement === document.body || sheet.closest('.account-sheet-root')?.parentElement === document.body,
      inTopbar: !!sheet.closest('.topbar'),
      pos: cs.position,
      fullWidth: Math.round(r.width),
      vw: window.innerWidth,
      // 贴底:底边与视口底齐平(允许 1px 舍入)
      atBottom: Math.abs(r.bottom - window.innerHeight) <= 1,
      radiusTop: cs.borderTopLeftRadius,
      radiusBottom: cs.borderBottomLeftRadius,
      sheetZ: Number(cs.zIndex),
      scrimZ: scrim ? Number(getComputedStyle(scrim).zIndex) : null,
      barZ: bar ? Number(getComputedStyle(bar).zIndex) : null,
      rowH: items.length ? Math.round(Math.min(...items.map((i) => i.getBoundingClientRect().height))) : 0,
      rowCount: items.length,
      hasGrip: !!document.querySelector('.account-sheet-grip'),
      hasClose: !!document.querySelector('.account-sheet-close'),
      hasActions: !!document.querySelector('.account-sheet .account-actions'),
      // sheet 必须盖住顶栏与底部 tab
      coversBars: Number(cs.zIndex) > (bar ? Number(getComputedStyle(bar).zIndex) : 0),
    }
  })

  check('sheet portal 到 document.body(不在 .topbar 内)', geo.inBody && !geo.inTopbar,
    `inBody=${geo.inBody} inTopbar=${geo.inTopbar}`)
  check('panel 用 fixed 定位', geo.pos === 'fixed', geo.pos)
  check('panel 宽度撑满视口且贴底',
    geo.fullWidth === geo.vw && geo.atBottom, `宽 ${geo.fullWidth}/${geo.vw} · 贴底 ${geo.atBottom}`)
  check('panel 只有顶部圆角', geo.radiusTop !== '0px' && geo.radiusBottom === '0px',
    `top ${geo.radiusTop} / bottom ${geo.radiusBottom}`)
  check('sheet 盖住顶栏/底栏(z-index 高于 .topbar)',
    geo.coversBars, `sheet ${geo.sheetZ} > topbar ${geo.barZ} · scrim ${geo.scrimZ}`)
  check('账号行高 ≥ 52px', geo.rowH >= 52, `${geo.rowH}px × ${geo.rowCount} 项`)
  check('有抓手条与关闭按钮', geo.hasGrip && geo.hasClose,
    `grip=${geo.hasGrip} close=${geo.hasClose}`)
  check('底部有「管理 PIN / 删除账号」', geo.hasActions)

  // ---- 填充:列表通栏 + 底部按钮均分 ----
  // 两条都是「看着差一截」类的缺陷,只能靠几何量出来,肉眼在截图里容易看漏。
  const fill = await page.evaluate(() => {
    const list = document.querySelector('.account-sheet .account-list')
    const btns = [...document.querySelectorAll('.account-sheet .account-action-btn')]
    const lr = list.getBoundingClientRect()
    return {
      // 列表必须通栏:抵消 sheet 的 12px 内边距,让行高亮延伸到 sheet 边缘
      listL: Math.round(lr.left), listR: Math.round(lr.right), vw: window.innerWidth,
      // 按钮必须均分满宽。曾因 .account-actions(<li>)漏写 display:flex 而失效:
      // 它是 list-item,不是 flex 容器,按钮的 flex:1 形同虚设,两个按钮按内容宽
      // 挤在左边(实测 366px 的行里只占 137px)。
      btnW: btns.map((b) => Math.round(b.getBoundingClientRect().width)),
      btnSpan: btns.length ? Math.round(btns[btns.length - 1].getBoundingClientRect().right -
        btns[0].getBoundingClientRect().left) : 0,
      liDisplay: getComputedStyle(document.querySelector('.account-sheet .account-actions')).display,
    }
  })
  check('账号列表左右通栏(行高亮延伸到 sheet 边缘)',
    fill.listL === 0 && fill.listR === fill.vw,
    `list ${fill.listL}~${fill.listR} / 视口 0~${fill.vw}`)
  check('底部两个按钮均分满宽',
    fill.btnW.length === 2 && fill.liDisplay === 'flex' &&
    Math.abs(fill.btnW[0] - fill.btnW[1]) <= 1 && fill.btnSpan >= fill.vw - 40,
    `${fill.btnW.join('/')} · 跨度 ${fill.btnSpan} · li.display=${fill.liDisplay}`)

  // 点 sheet **内部空白处**不该关。
  // 这是 extraRef 的守护项:sheet 用 createPortal 挂到了 body,不是 .account-wrap 的 DOM
  // 后代 —— useOutsideClick 若只判 rootRef,点这里会被当成「点外部」而收起。
  // 不能用「点条目能切账号」来守:条目的选中走 onClick,即使被误关也仍在 180ms 的
  // 卸载窗口内触发,照样切得动 —— 那条会绿,但这个缺陷还在。
  await page.click('.account-sheet-title')
  await page.waitForTimeout(350)
  check('点 sheet 内部空白处不会关闭',
    (await page.locator('.account-sheet').count()) === 1)

  // **滚动后仍要贴底** —— 这条比上面那条更能说明问题,务必保留。
  // .account-sheet 若写成 position:absolute,portal 根是静态定位,会以**初始包含块**
  // (锚在文档原点的视口大小矩形)为基准 —— 页面滚一段再打开,sheet 会整体上移同样的
  // 距离、悬在屏幕中间。而页面在顶部时 absolute 与 fixed 表现**完全一致**,
  // 故「贴底」这条判据在顶部测永远绿,抓不到这个 bug(实测变异验证:改回 absolute 后
  // 「贴底」仍 ok,只有本条和 fixed 判据会红)。顶栏是 sticky 的,滚到哪儿都点得开,
  // 所以这个偏移在真机上必然复现。
  // 注意先后顺序:必须先关掉 sheet 再滚 —— 打开期间 useScrollLock 把 <html> 锁成
  // overflow:hidden,此时 scrollTo 无效,且 scrim(fixed inset:0)会挡住触发条,
  // openSheet() 的点击会一直等到 playwright 30s 超时(报的是「点不到触发条」,
  // 看不出真因是上面的 sheet 没关)。
  //
  // 关闭走**关闭按钮**而不是 Esc:Esc 靠 React 的 onKeyDown 从 .account-wrap 冒泡,
  // 要求焦点落在 .account-wrap 内的元素上;而上一步刚点过 .account-sheet-title
  // (不可聚焦元素),焦点已回到 body,此时按 Esc 是没反应的。
  await page.click('.account-sheet-close')
  await sheetGone()
  await page.evaluate(() => window.scrollTo(0, 600))
  await page.waitForTimeout(300)
  const scrolled = await page.evaluate(() => window.scrollY)
  await openSheet()
  const atBottomScrolled = await page.evaluate(() => {
    const r = document.querySelector('.account-sheet').getBoundingClientRect()
    return Math.abs(r.bottom - window.innerHeight) <= 1
  })
  check(`滚动 ${scrolled}px 后打开 sheet 仍贴底`,
    scrolled > 0 && atBottomScrolled, `scrollY=${scrolled} · 贴底=${atBottomScrolled}`)

  // ---- 打开期间:滚动锁定 ----
  // 读的是「当前 sheet 打开着」这一刻的 <html> overflow —— 上面那条判据结束时 sheet
  // 仍是开着的,直接用即可,不要再开关一次(多一次开关就多一个出错点)。
  const locked = await page.evaluate(() => getComputedStyle(document.documentElement).overflow)
  check('打开时锁住 <html> 滚动', locked === 'hidden', `overflow=${locked}`)

  // 关掉并回到页顶:后面的判据(三种退出方式)都从页顶开始,免得互相干扰
  await page.click('.account-sheet-close')
  await sheetGone()
  await page.evaluate(() => window.scrollTo(0, 0))
  await page.waitForTimeout(200)

  // 截图遮罩:展开时**临时**解除,收起(切成功或取消)即恢复 —— 见 hooks/usePrivacy。
  // 判据取 **html 上有没有 data-privacy**,不读 CSS 的 filter 值:
  // 后者在 .12s 过渡期间读到的是中间态,断言会莫名飘。
  //
  // 下面三种退出方式(关闭按钮 / 点遮罩 / Esc)各自验一遍恢复,另加「切换成功」。
  // 收起共有五条路径(还有「点外部」),实现上收敛在 open 的变化里
  // (见 AccountSelect 的 useEffect),只要那条链路在就不会漏;但反过来,
  // 若哪天有人改成在退出路径上散着调,漏一条遮罩就永远留着了 —— 这些判据守的就是这个。
  const privacyOn = () => page.evaluate(() => document.documentElement.hasAttribute('data-privacy'))
  check('展开前遮罩是开的', await privacyOn())

  // ---- 退出 1:点关闭按钮 ----
  // 重新打开:上面那步为了回页顶已经把 sheet 关了,退出方式的判据各自需要一个开着的 sheet。
  await openSheet()
  await page.click('.account-sheet-close')
  check('点关闭按钮能退出', await sheetGone())
  const restored1 = await page.evaluate(() => document.documentElement.style.overflow)
  check('关闭后还原 <html> 的 overflow(非空串即还原原值)',
    restored1 === '' || restored1 === 'visible', `inline overflow="${restored1}"`)
  check('取消(关闭按钮)后遮罩恢复', await privacyOn())

  // ---- 退出 2:点遮罩 ----
  await openSheet()
  check('展开 sheet 时临时解除遮罩', !(await privacyOn()))
  await page.click('.account-scrim', { position: { x: 195, y: 60 } })
  check('点遮罩能退出', await sheetGone())
  check('取消(点遮罩)后遮罩恢复', await privacyOn())

  // ---- 退出 3:Esc ----
  await openSheet()
  await page.keyboard.press('Escape')
  check('按 Esc 能退出', await sheetGone())
  check('取消(Esc)后遮罩恢复', await privacyOn())

  // ---- 键盘导航:↑↓ + Enter 切账号 ----
  // 目标必须是「非当前 + 未设 PIN」的账号:设了 PIN 的会被拦去输 PIN(那是另一条路径,
  // 下面单独验)。这里若直接挑第一个不同的项,会撞上 PIN 弹窗、顶栏标识自然不变化 ——
  // 表现为「切不动」,其实是 PIN 拦截生效了。
  //
  // 锁的选择器是 **.acct-avatar-pin**(头像左下角),不是 .account-item-pin ——
  // 锁从「行内一个独立 span」挪到头像上了(见 AccountAvatar 的 pin 参数),
  // 沿用旧选择器会**一个都找不到**,于是「没有设 PIN 的账号」→ 3 项全 skip,
  // 看着是数据不足,其实是选择器失效(踩过一次)。
  //
  // 「当前账号」的探针用 **UID** 而不是昵称:手机端触发条不渲染昵称(见
  // verify-account-width.mjs 的判据 3),取 .acct-name 会得到 null。
  const pickTarget = async () => page.evaluate(() => {
    const items = [...document.querySelectorAll('.account-sheet .account-item')]
    const cur = document.querySelector('.account-trigger .acct-uid')?.textContent || ''
    const curId = cur.replace(/^UID:/, '')
    return items.findIndex((el) =>
      !el.classList.contains('cur') &&
      !el.querySelector('.acct-avatar-pin') &&
      !(el.querySelector('.account-item-name')?.textContent || '').includes(curId))
  })

  // 先验 PIN 拦截:切到设了 PIN 的账号应弹 PIN 窗、顶栏标识**不变**
  await openSheet()
  const pinIdx = await page.evaluate(() => {
    const items = [...document.querySelectorAll('.account-sheet .account-item')]
    return items.findIndex((el) => !el.classList.contains('cur') && el.querySelector('.acct-avatar-pin'))
  })
  if (pinIdx >= 0) {
    const before = await page.textContent('.account-trigger .acct-uid')
    await page.locator('.account-sheet .account-item').nth(pinIdx).click()
    await page.waitForTimeout(1200)
    const after = await page.textContent('.account-trigger .acct-uid')
    const pinShown = await page.locator('.pin-dialog').count()
    check('切到设了 PIN 的账号:弹 PIN 窗且不直接切',
      pinShown === 1 && before === after, `PIN 窗 ${pinShown} 个 · ${before} → ${after}`)
    // 关掉弹窗,免得挡住后面的点击。
    // verify 模式的 PinDialog **没有取消按钮**(只有输入框 + 确认),且它自己不处理 Esc ——
    // 只能点遮罩空白处。注意不能直接 page.click('.pin-backdrop'):那会点在正中央,
    // 也就是弹窗本体上,而它的关闭判据是 e.target === e.currentTarget。
    await page.mouse.click(8, 8)
    await page.waitForTimeout(600)
  } else {
    skip('切到设了 PIN 的账号:弹 PIN 窗且不直接切', '库里没有设 PIN 的账号')
    // **必须把 sheet 关掉**:下面紧接着要 openSheet(),而 openSheet 会点触发条 ——
    // sheet 的 scrim 是 fixed inset:0 盖在整屏上,留着它,那一下点击会被拦截,
    // playwright 等到 30s 超时(且报的是「点不到触发条」,看不出真因)。
    await page.keyboard.press('Escape')
    await sheetGone()
  }

  await openSheet()
  const targetIdx = await pickTarget()
  if (targetIdx >= 0) {
    const before = await page.textContent('.account-trigger .acct-uid')
    // 从当前高亮出发,按 ↓ 直到高亮落在目标项上(高亮初值是当前选中项,见 useDropdown)
    for (let i = 0; i <= geo.rowCount; i++) {
      const hiIdx = await page.evaluate(() => [...document.querySelectorAll('.account-sheet .account-item')]
        .findIndex((el) => el.classList.contains('hi')))
      if (hiIdx === targetIdx) break
      await page.keyboard.press('ArrowDown')
      await page.waitForTimeout(90)
    }
    const hiIdx = await page.evaluate(() => [...document.querySelectorAll('.account-sheet .account-item')]
      .findIndex((el) => el.classList.contains('hi')))
    await page.keyboard.press('Enter')
    await page.waitForTimeout(2500) // 切账号会触发全站重取
    const after = await page.textContent('.account-trigger .acct-uid')
    check('键盘 ↑↓ 能把高亮移到目标项', hiIdx === targetIdx, `高亮 ${hiIdx} / 目标 ${targetIdx}`)
    check('键盘 Enter 能切账号(顶栏 UID 变化)', before !== after, `${before} → ${after}`)
    check('切完自动收起', await sheetGone())
    check('切换成功后遮罩恢复', await privacyOn())
  } else {
    skip('键盘切账号', '库里只有 1 个账号(需 ≥2 个才能切)')
    await page.keyboard.press('Escape')
    await sheetGone()
  }

  // ---- 点击切换(触屏路径:onClick 而非 onMouseDown)----
  await openSheet()
  const clickIdx = await pickTarget()
  if (clickIdx >= 0) {
    const before = await page.textContent('.account-trigger .acct-uid')
    await page.locator('.account-sheet .account-item').nth(clickIdx).click()
    await page.waitForTimeout(2500)
    const after = await page.textContent('.account-trigger .acct-uid')
    check('点条目能切账号', before !== after, `${before} → ${after}`)
    check('点完自动收起', await sheetGone())
  } else {
    skip('点条目能切账号', '库里只有 1 个账号(需 ≥2 个才能切)')
    await page.keyboard.press('Escape')
    await sheetGone()
  }

  // ---- 多账号徽章色相不同 ----
  await openSheet()
  const hues = await page.evaluate(() =>
    [...document.querySelectorAll('.account-sheet .acct-avatar')]
      .map((el) => getComputedStyle(el).getPropertyValue('--acct-h').trim()))
  await page.keyboard.press('Escape')
  await page.waitForTimeout(320)
  check('各账号徽章色相已派生(非空且尽量不同)',
    hues.length > 0 && hues.every((h) => h !== ''),
    hues.join('/'))

  // ---- 桌面视口不出 sheet ----
  const desk = await browser.newPage({ viewport: { width: 1280, height: 800 }, deviceScaleFactor: 2 })
  try {
    await desk.goto(BASE + '/#/pets', { waitUntil: 'domcontentloaded' })
    await desk.waitForSelector('.account-select', { timeout: 20000 })
    await desk.waitForTimeout(2500)
    await desk.click('.account-trigger')
    await desk.waitForTimeout(400)
    const d = await desk.evaluate(() => ({
      sheet: document.querySelectorAll('.account-sheet').length,
      pop: document.querySelectorAll('.account-dropdown').length,
      // 桌面浮层必须留在 .topbar 内(它不需要 portal)
      popInTopbar: !!document.querySelector('.account-dropdown')?.closest('.topbar'),
    }))
    check('桌面视口不出 sheet,用锚定浮层',
      d.sheet === 0 && d.pop === 1 && d.popInTopbar,
      `sheet=${d.sheet} popover=${d.pop} inTopbar=${d.popInTopbar}`)
  } finally {
    await desk.close()
  }
} catch (e) {
  check('执行完成', false, String(e).split('\n')[0])
} finally {
  await page.close()
}

await browser.close()

const bad = results.filter((r) => !r.ok)
console.log(`\n${results.length - bad.length}/${results.length} 项通过` +
  (skipped.length ? `,${skipped.length} 项因数据不足跳过(${skipped.join('/')})` : ''))
// 跳过项不计入失败:那是「库里没有第二个账号/没有设 PIN 的账号」这类前置数据问题,
// 不是产品缺陷。想真正验到它们,得回放一份含多账号 + 设过 PIN 的 pcap。
process.exit(bad.length ? 1 : 0)
