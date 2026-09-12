// 主题切换「从按钮处扩散」的真浏览器验收(web/dist 构建产物 + Chromium)。
//
//   npm run build
//   node scripts/verify-theme-spread-browser.mjs
//
// 存在理由:静态版(verify-theme-spread.mjs)只能证明「代码写对了」,
// 证明不了浏览器**真的跑起来**了。这条链路有三件事只有真浏览器说了算:
//
//   1. `html.theme-spread::view-transition-new(root)` 这个选择器**匹配得上吗**。
//      view-transition 的伪元素树挂在根元素上,能否被带 class 的后代选择器选中
//      是纯 UA 行为 —— 匹配不上时动画不播、不报错,页面只是「瞬间变个色」,
//      和没做这个功能长得一模一样。这是本次改动最可能的静默失效点。
//   2. clip-path 是否真的在**逐帧插值**(0 → 覆盖整屏),而不是一步到位。
//   3. 三条退化路径:不支持 API / 开了减少动效 / 连续快速点击,
//      是否都还能把主题**切过去**(动画可以没有,功能不能丢)。
//
// 判据取「过渡中途的 clip-path 半径介于 0 与满半径之间」,它同时盖住 1 和 2:
// 选择器没匹配上时根本没有这条动画,中途取样拿到的会是 none 或满半径。
//
// 不依赖后端数据:只点顶栏的**版本号**(主题菜单入口)与主题图标,壳渲染出来即可。

import { chromium } from 'playwright'
import { createServer } from 'node:http'
import { readFile } from 'node:fs/promises'
import { extname, join, normalize } from 'node:path'
import { fileURLToPath } from 'node:url'

const here = fileURLToPath(new URL('..', import.meta.url))          // web/
const DIST = join(here, '..', 'internal', 'server', 'web')          // Go embed 的构建产物
const PORT = Number(process.env.PORT || 4956)

const MIME = {
  '.html': 'text/html; charset=utf-8', '.js': 'text/javascript; charset=utf-8',
  '.css': 'text/css; charset=utf-8', '.json': 'application/json',
  '.woff2': 'font/woff2', '.svg': 'image/svg+xml', '.webp': 'image/webp', '.png': 'image/png',
}

const server = createServer(async (req, res) => {
  const url = decodeURIComponent((req.url || '/').split('?')[0])
  const rel = normalize(url === '/' ? '/index.html' : url).replace(/^(\.\.[/\\])+/, '')
  try {
    const buf = await readFile(join(DIST, rel))
    res.writeHead(200, { 'content-type': MIME[extname(rel)] || 'application/octet-stream' })
    res.end(buf)
  } catch {
    const buf = await readFile(join(DIST, 'index.html'))
    res.writeHead(200, { 'content-type': MIME['.html'] })
    res.end(buf)
  }
})
await new Promise((r) => server.listen(PORT, r))

const results = []
const check = (name, ok, detail) => {
  results.push(ok)
  console.log(`  ${ok ? 'ok  ' : 'FAIL'} ${name}${detail ? '  — ' + detail : ''}`)
}
// 等页面真正就绪:**主题按钮出现** + **开屏层已卸载**。
//
// 为什么要等开屏:它是全屏覆盖层(z-index 最高),在它退场前:
//   · [9] 的像素证据采到的是开屏的深蓝底与金色水晶,而不是主题底色 ——
//     实测就是栽在这里:切换前采到 rgb(31,31,45)(开屏底色),中途采到 rgb(180,165,53)
//     (淡出中的金色水晶),于是「圆内已是新主题」判失败,而主题本身完全正常;
//   · 主题按钮虽然可点,但 View Transition 的整屏快照里也带着开屏。
// 判据用「#loading 消失」而不是等固定时长:开屏在首屏数据就绪后 1.7s 自行卸载,
// 慢机器上更久;等元素消失既准确,又不会在快机器上白等。
const pageReady = async (page) => {
  await page.waitForSelector('.topbar-theme-icon', { timeout: 10000 })
  await page.waitForFunction(() => !document.getElementById('loading'), null, { timeout: 15000 })
}

// 切主题是**两步**:先开菜单、再点选项(入口现在弹菜单,不再是点一下就换)。
// 这里的两次 click 走 Playwright 而不是页面内 btn.click():菜单由 React 渲染,
// 点完触发按钮得等它挂到 DOM 上才能点里面的选项 —— 页面内同步 click 拿不到刚渲染出来的节点。
// (帧级取样的那条路径相反,必须留在页面内才能在同一帧读到 --theme-x/y,见 probe。)
const pickTheme = async (page, label) => {
  await page.click('.ver-wrap .topbar-ver') // 版本号是唯一的菜单入口(图标是纯切换)
  // 用 :has-text 而不是 :text-is:选项里有个 <span class="tm-text"> 包着文字,
  // 而 :text-is 要求「**最小的**含该文本元素」等于它 —— 那个 span 没有 tm-opt 类,
  // 于是选择器永远匹配不上(实测表现为点击一直等到超时,而不是报找不到选项)。
  await page.click(`.theme-menu .tm-opt:has-text("${label}")`)
}

const BASE = `http://localhost:${PORT}/`

// 页面内脚本:开主题菜单 → 选某一项,把「点击当下 / 过渡中途 / 过渡结束」三处状态一次采回来。
//
// ⚠️ 这里必须用页面内 click 而不是 Playwright 的 locator.click():后者是异步的,
// 等它返回时 620ms 的过渡早跑完了,抓不到中途那一帧。也正因为要留在页面内,
// 「先开菜单再点选项」得自己轮询等菜单挂上 React 渲染的节点(下面那个 for 循环)。
//
// **取样按动画自己的 currentTime 推进,不用墙钟 sleep**:headless 下 rAF 与定时器
// 都可能被滞后(实测用 rAF 等过渡建立时,整条取样时间轴被推后了几百毫秒,
// 取到的「中途」其实是动画的第 0 帧 —— 圆半径 0,看着像动画没跑)。
const probe = async ({ pick }) => {
  const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
  const root = document.documentElement
  // ⚠️ 菜单入口是**版本号**,不是顶栏那个主题图标 —— 图标现在是纯切换、不弹菜单
  // (它只动明暗,见 [10])。这里曾点图标去找菜单,失败信息是「菜单里没有「浅色」这个选项」,
  // 读起来像菜单坏了,其实是探针点错了入口。
  const entry = document.querySelector('.ver-wrap .topbar-ver')
  if (!entry) return { error: '找不到主题菜单入口(版本号)' }

  const dur = parseFloat(getComputedStyle(root).getPropertyValue('--dur-theme')) || 620
  const before = root.getAttribute('data-theme')

  // 先开菜单、等选项渲染出来,再点选项。
  // 扩散圆心应当是被点的那个**选项**的中心 ——
  // 事件链:选项 onClick → onPick(next, e) → App 的 pickTheme → choose(next, e),
  // 任一环丢掉 e 都会让圆心退回…没有圆心,这条断言就会先红。
  entry.click()
  let opt = null
  for (let i = 0; i < 200 && !opt; i++) {
    opt = [...document.querySelectorAll('.theme-menu .tm-opt')]
      .find((o) => o.textContent.trim() === pick) || null
    if (!opt) await sleep(10)
  }
  if (!opt) return { error: `菜单里没有「${pick}」这个选项`, menu: !!document.querySelector('.theme-menu') }
  const box = opt.getBoundingClientRect()
  const wantX = box.left + box.width / 2
  const wantY = box.top + box.height / 2

  opt.click()
  // ① 点击当下:圆心变量与 class 必须已经注入(拿不到圆心就没法从选项扩散)
  const at0 = {
    cls: root.classList.contains('theme-spread'),
    x: parseFloat(root.style.getPropertyValue('--theme-x')),
    y: parseFloat(root.style.getPropertyValue('--theme-y')),
    r: parseFloat(root.style.getPropertyValue('--theme-r')),
  }
  // ② 等过渡建立(伪元素要下一帧才出现)—— 轮询,别用 rAF 等(见上面注释)
  let spread = null
  for (let i = 0; i < 300 && !spread; i++) {
    spread = document.getAnimations().find((a) => a.animationName === 'theme-spread') || null
    if (!spread) await sleep(10)
  }
  // 找不到动画就直接返回,别往下走 —— 否则会在读 spread.currentTime 时抛 TypeError,
  // 报出一个「脚本崩了」而不是「动画没挂上」,真正的病因反而看不出来。
  // 这正是文件头说的**最可能的静默失效点**:伪元素选择器没匹配上。
  if (!spread) {
    return { noAnim: true, at0, before,
      seen: document.getAnimations().map((a) => `${a.effect?.pseudoElement || '-'} ${a.animationName}`) }
  }
  const anims = document.getAnimations()
    .filter((a) => (a.effect?.pseudoElement || '').includes('view-transition'))
  const oldBlend = getComputedStyle(root, '::view-transition-old(root)').mixBlendMode

  // ③ 过渡中途:clip-path 的半径应在 (0, 满半径) 之间 —— 这条最要紧,见文件头
  const waitAnimTime = async (t) => {
    for (let i = 0; i < 300; i++) {
      if (spread.currentTime >= t) return
      await sleep(5)
    }
  }
  await waitAnimTime(dur * 0.45)
  const midRaw = getComputedStyle(root, '::view-transition-new(root)').clipPath
  const midR = parseFloat(/circle\(([\d.]+)px/.exec(midRaw)?.[1] ?? 'NaN')

  // ④ 过渡结束:class 与三个变量必须清干净,主题必须真的变了
  //
  //    等清理要**轮询**而不是 sleep 一段固定时长:view-transition 的 finished 比动画
  //    自身的 finished 晚一两帧(它还要拆掉伪元素树),而 headless 下帧节奏不稳,
  //    固定 sleep 会偶发地早于清理完成 —— 那是测法的假阳性,不是代码没清。
  //    轮询给足 3s 上限:真没清干净时照样判失败。
  await Promise.race([spread.finished.catch(() => {}), sleep(dur * 3)])
  let cleaned = false
  for (let i = 0; i < 300; i++) {
    if (!root.classList.contains('theme-spread')) { cleaned = true; break }
    await sleep(10)
  }
  const end = {
    cls: cleaned ? false : root.classList.contains('theme-spread'),
    leftover: root.getAttribute('style') || '',
    theme: root.getAttribute('data-theme'),
  }
  return { dur, wantX, wantY, before, at0, pseudo: spread?.effect.pseudoElement || null,
    animCount: anims.length, oldBlend, midRaw, midR, end }
}

const browser = await chromium.launch({ args: ['--no-sandbox', '--disable-dev-shm-usage'] })
try {
  // —— 主路径:支持 View Transitions 的浏览器 ——
  // 固定初始态:洛克 + 跟随系统 + 浏览器深色 → 生效色是 handbook-dark。
  // 必须这样钉死:选的这一项**一定要真的换色**,否则走的是「生效色不变」的瞬时分支
  // (见下面 [8]),这里就测不到扩散了 —— 故选「浅色」(handbook-dark → handbook)。
  const page = await browser.newPage({ viewport: { width: 1280, height: 800 }, colorScheme: 'dark' })
  await page.addInitScript(() => localStorage.setItem('theme', '"roco:auto"'))
  await page.goto(BASE, { waitUntil: 'networkidle' })
  await pageReady(page)

  const a = await page.evaluate(probe, { pick: '浅色' })
  if (a.error) {
    check('找到主题按钮', false, a.error)
  } else if (a.noAnim) {
    check('过渡里跑着 theme-spread 动画', false,
      `没找到(伪元素选择器没匹配上?)。实际动画: ${(a.seen || []).join(' | ') || '无'}`)
  } else {
    console.log('\n[1] 点击当下:圆心取自按钮')
    check('theme-spread class 已挂上', a.at0.cls)
    // 亚像素容差 1px:注入的是未取整的中心坐标
    check('--theme-x/y = 被点选项的中心',
      Math.abs(a.at0.x - a.wantX) <= 1 && Math.abs(a.at0.y - a.wantY) <= 1,
      `注入 (${a.at0.x}, ${a.at0.y}) / 选项中心 (${a.wantX}, ${a.wantY})`)
    const need = Math.hypot(Math.max(a.wantX, 1280 - a.wantX), Math.max(a.wantY, 800 - a.wantY))
    check('--theme-r 覆盖到最远角', a.at0.r >= need - 1, `${a.at0.r} ≥ ${need.toFixed(1)}`)

    console.log('\n[2] 动画确实挂在新快照上(选择器匹配得上)')
    check('存在 theme-spread 动画', !!a.pseudo, `pseudo=${a.pseudo}`)
    check('裁的是 ::view-transition-new(root)',
      a.pseudo === '::view-transition-new(root)', String(a.pseudo))
    check('old 快照 mix-blend-mode: normal(默认交叉淡入已关)', a.oldBlend === 'normal', a.oldBlend)

    console.log('\n[3] 中途取样:圆在逐帧长大(不是瞬变)')
    check('中途 clip-path 是 circle()', /^circle\(/.test(a.midRaw), a.midRaw)
    check('中途半径介于 0 与满半径之间', a.midR > 0 && a.midR < a.at0.r,
      `中途 ${a.midR}px / 满 ${a.at0.r}px`)

    console.log('\n[4] 收尾:清理干净 + 主题确实切了')
    check('class 已摘除', !a.end.cls)
    check('--theme-* 内联变量已清除', !/--theme-[xyr]/.test(a.end.leftover), a.end.leftover || '(空)')
    check('data-theme 变了', a.end.theme !== a.before, `${a.before} → ${a.end.theme}`)

    // —— 连续快速选两次:不能留下摘不掉的 class / 变量 ——
    console.log('\n[5] 连选两次(第二次会抢占第一次的过渡)')
    await pickTheme(page, '深色')
    await page.waitForTimeout(80) // 过渡还没结束就再选一次
    await pickTheme(page, '浅色')
    await page.waitForTimeout(1400)
    const after = await page.evaluate(() => ({
      cls: document.documentElement.classList.contains('theme-spread'),
      style: document.documentElement.getAttribute('style') || '',
    }))
    check('无残留 class', !after.cls)
    check('无残留 --theme-* 变量', !/--theme-[xyr]/.test(after.style), after.style || '(空)')
  }
  await page.close()

  // —— 退化路径 1:浏览器不支持 View Transitions(老 Firefox / Safari 17)——
  console.log('\n[6] 退化:没有 startViewTransition 时仍能切主题')
  const p2 = await browser.newPage({ viewport: { width: 1280, height: 800 }, colorScheme: 'dark' })
  await p2.addInitScript(() => {
    localStorage.setItem('theme', '"roco:auto"')
    delete Document.prototype.startViewTransition
  })
  await p2.goto(BASE, { waitUntil: 'networkidle' })
  await pageReady(p2)
  const bBefore = await p2.evaluate(() => document.documentElement.getAttribute('data-theme'))
  await pickTheme(p2, '浅色')
  const b = await p2.evaluate(() => ({
    after: document.documentElement.getAttribute('data-theme'),
    cls: document.documentElement.classList.contains('theme-spread'),
  }))
  check('主题切换成功(无异常)', bBefore !== b.after, `${bBefore} → ${b.after}`)
  check('没有挂 class(不播动画)', !b.cls)
  await p2.close()

  // —— 退化路径 2:用户开了「减少动态效果」——
  console.log('\n[7] 退化:prefers-reduced-motion: reduce')
  const p3 = await browser.newPage({ viewport: { width: 1280, height: 800 }, reducedMotion: 'reduce', colorScheme: 'dark' })
  await p3.addInitScript(() => localStorage.setItem('theme', '"roco:auto"'))
  await p3.goto(BASE, { waitUntil: 'networkidle' })
  await pageReady(p3)
  const cBefore = await p3.evaluate(() => document.documentElement.getAttribute('data-theme'))
  await pickTheme(p3, '浅色')
  const c = await p3.evaluate(() => ({
    after: document.documentElement.getAttribute('data-theme'),
    cls: document.documentElement.classList.contains('theme-spread'),
  }))
  check('主题切换成功', cBefore !== c.after, `${cBefore} → ${c.after}`)
  check('没有播扩散动画', !c.cls)
  await p3.close()

  // —— 退化路径 3:换了组合但**生效色没变**(亮系统里「洛克·跟随系统」→「洛克·浅色」)——
  // 这时不能为它冻屏 620ms:全程看不到任何变化,观感是「点了没反应」
  // (过渡期间页面是快照,连菜单都要等过渡结束才更新)。
  //
  // ⚠️ 这两档之所以同色,是因为「洛克 + 跟随系统」在亮系统下解析成 handbook,
  // 而「洛克 + 浅色」也是 handbook —— 换的是**轴的取值**,不是画面。
  // 改 resolveTheme 或默认风格时要同步这里,否则这条会变成「随手点了个不同色的选项」,
  // 而它本来要验的东西(瞬时分支)就没人测了。
  console.log('\n[8] 退化:生效色不变时不冻屏(洛克·跟随系统 → 洛克·浅色)')
  const p4 = await browser.newPage({ viewport: { width: 1280, height: 800 }, colorScheme: 'light' })
  await p4.addInitScript(() => localStorage.setItem('theme', '"roco:auto"'))
  await p4.goto(BASE, { waitUntil: 'networkidle' })
  await pageReady(p4)
  const dBefore = await p4.evaluate(() => document.documentElement.getAttribute('data-theme'))
  await pickTheme(p4, '浅色')
  const d = await p4.evaluate(() => ({
    after: document.documentElement.getAttribute('data-theme'),
    cls: document.documentElement.classList.contains('theme-spread'),
    stored: localStorage.getItem('theme'),
  }))
  check('生效色不变,故不播扩散', !d.cls)
  check('但组合确实切过去了', d.stored === '"roco:light"', `stored=${d.stored} / ${dBefore} → ${d.after}`)
  // 再选一次(洛克·浅色 → 洛克·深色,颜色真的变了)必须恢复正常扩散 ——
  // 上面那条分支不能把后续选择也带成瞬时切换。
  await pickTheme(p4, '深色')
  const again = await p4.evaluate(() => ({
    cls: document.documentElement.classList.contains('theme-spread'),
    theme: document.documentElement.getAttribute('data-theme'),
  }))
  check('下一次选择(颜色真的变)恢复扩散', again.cls, `data-theme=${again.theme}`)
  await p4.close()

  // —— 像素证据:过渡中途,圆**内**已经是新主题、圆**外**还是旧主题 ——
  //
  //    为什么还要这一条:前面所有断言量的都是「圆有没有在长大」,量不到
  //    **圆里面铺开的是不是新主题**。若 startViewTransition 的回调没能同步把
  //    data-theme 落下去(React 18 批处理会把它推到微任务 —— 正是 flushSync 要防的),
  //    新旧两张快照会一模一样:圆照常张开、半径照常插值,上面的检查全绿,
  //    但屏幕上什么颜色变化都看不到。这一类「动画在跑、内容错了」只有像素说了算。
  //
  //    做法:把动画拖慢到 6s(内联覆写 --dur-theme),在圆张到一半时截一张图,
  //    取两个点到**三张截图**(切换前 / 中途 / 切换后)里分别取色:
  //      A 圆心附近(距圆心 ~150px)—— 中途应已等于「切换后」的颜色;
  //      B 屏幕远角(距圆心 ~1000px)—— 中途应仍等于「切换前」的颜色。
  //    同一坐标在三张图里取色,内容位置不变,故可比(不要求该点是纯背景)。
  console.log('\n[9] 像素证据:圆内是新主题、圆外还是旧主题')
  const p5 = await browser.newPage({ viewport: { width: 1280, height: 800 }, colorScheme: 'dark' })
  await p5.addInitScript(() => localStorage.setItem('theme', '"roco:auto"'))
  await p5.goto(BASE, { waitUntil: 'networkidle' })
  await pageReady(p5)

  // 圆心是**被点的那个选项**(菜单挂在版本号下方),故先开一次菜单量出「浅色」的中心,
  // 再把 A 取在它下方 150px:6s 拖慢后中途半径约 350px,故 A 稳稳在圆内、B(远角)在圆外。
  // 量完按 Esc 收起菜单 —— 「切换前」那张图必须是干净页面(菜单本身会挡住取样点)。
  await p5.click('.ver-wrap .topbar-ver')
  const pts = await p5.evaluate(() => {
    const opt = [...document.querySelectorAll('.theme-menu .tm-opt')]
      .find((o) => o.textContent.trim() === '浅色')
    const r = (opt || document.querySelector('.ver-wrap .topbar-ver')).getBoundingClientRect()
    return { A: [Math.round(r.left + r.width / 2), Math.round(r.top + r.height / 2 + 150)],
      B: [window.innerWidth - 40, window.innerHeight - 40] }
  })
  await p5.keyboard.press('Escape')
  // 取色:截图交给页面自己画到 canvas 上再读像素(页面内没有解码器,浏览器就是解码器)
  const sample = async () => {
    const b64 = (await p5.screenshot({ animations: 'allow' })).toString('base64')
    return p5.evaluate(async ({ b64, pts }) => {
      const img = new Image()
      img.src = 'data:image/png;base64,' + b64
      await img.decode()
      const c = document.createElement('canvas')
      c.width = img.width
      c.height = img.height
      const ctx = c.getContext('2d')
      ctx.drawImage(img, 0, 0)
      const at = ([x, y]) => [...ctx.getImageData(x, y, 1, 1).data].slice(0, 3)
      return { A: at(pts.A), B: at(pts.B) }
    }, { b64, pts })
  }
  const before9 = await sample()
  await p5.evaluate(() => document.documentElement.style.setProperty('--dur-theme', '6000ms'))
  await p5.click('.ver-wrap .topbar-ver') // 先开菜单(这两步耗时相对 6s 可忽略)
  const t0 = Date.now()
  await p5.click('.theme-menu .tm-opt:has-text("浅色")') // 选 → 起过渡,此刻才是动画的 0 点
  await p5.waitForTimeout(600) // 6s 里的 10%:圆半径约 350px,A(150)在内、B(~1000)在外
  const mid9 = await sample()
  const midAtMs = Date.now() - t0 // 中途那张图真正落地的时刻(截图本身有耗时)
  await p5.waitForFunction(() => !document.documentElement.classList.contains('theme-spread'),
    null, { timeout: 12000 })
  const after9 = await sample()

  const eq = (a, b) => a.every((v, i) => Math.abs(v - b[i]) <= 2) // 2 的抗锯齿容差
  const rgb = (c) => `rgb(${c.join(',')})`
  check('主题确实变了(切换前后 A 点颜色不同)', !eq(before9.A, after9.A),
    `${rgb(before9.A)} → ${rgb(after9.A)}`)
  check('中途:A 点(圆内)已是新主题', eq(mid9.A, after9.A),
    `中途 ${rgb(mid9.A)} / 切换后 ${rgb(after9.A)}`)
  check('中途:B 点(圆外)仍是旧主题', eq(mid9.B, before9.B),
    `中途 ${rgb(mid9.B)} / 切换前 ${rgb(before9.B)}`)
  check('中途画面上确实存在新旧分界', !eq(mid9.A, mid9.B),
    `A ${rgb(mid9.A)} vs B ${rgb(mid9.B)}(中途截图于点击后 ${midAtMs}ms)`)
  await p5.close()

  // —— 面板必须落在视口内(两个入口 × 桌面/手机)——
  // 为什么值得单列一条:面板是贴着触发元素展开的,而两个入口分处顶栏左右,
  // 手机上「还没有账号」时图标还会被挤到中间 —— 纯 CSS 锚点在这些排布下都会让面板探出视口
  // (实测:1280px 下版本号那侧探出 99px、390px 下图标那侧探出 26px,而菜单里一半的选项
  //  因此点不到)。ThemeMenu 里那次量尺 + translateX 就是为此,这条是它的护栏。
  console.log('\n[10] 面板夹在视口内 + 图标是纯切换')
  for (const vp of [{ width: 1280, height: 800 }, { width: 390, height: 844 }]) {
    const p6 = await browser.newPage({ viewport: vp })
    await p6.goto(BASE, { waitUntil: 'networkidle' })
    await pageReady(p6)
    // 版本号:唯一的菜单入口,面板必须整块落在视口内(窄屏上它左边还有品牌,最容易被挤出去)
    await p6.click('.ver-wrap .topbar-ver')
    const r = await p6.evaluate(() => {
      const el = document.querySelector('.theme-menu')
      if (!el) return null
      const b = el.getBoundingClientRect()
      return { left: Math.round(b.left), right: Math.round(b.right), vw: window.innerWidth }
    })
    check(`${vp.width}px 版本号:面板在视口内`, !!r && r.left >= 0 && r.right <= r.vw,
      r ? `left=${r.left} right=${r.right} / 视口 ${r.vw}` : '面板没打开')
    await p6.keyboard.press('Escape')
    // 图标:不该弹菜单,而且要在**同一风格内**把明暗翻过去
    const beforeT = await p6.evaluate(() => ({
      theme: document.documentElement.getAttribute('data-theme'),
      stored: localStorage.getItem('theme'),
    }))
    await p6.click('.fs-wrap .topbar-fs, .topbar-fs:has(.topbar-theme-icon)')
    await p6.waitForTimeout(200)
    const afterT = await p6.evaluate(() => ({
      theme: document.documentElement.getAttribute('data-theme'),
      stored: localStorage.getItem('theme'),
      menu: !!document.querySelector('.theme-menu'),
    }))
    check(`${vp.width}px 图标:不弹菜单`, !afterT.menu)
    check(`${vp.width}px 图标:确实切了明暗`, afterT.theme !== beforeT.theme,
      `${beforeT.theme} → ${afterT.theme}`)
    // 风格不变:两边要么都带 handbook,要么一个是 light 一个是 dark(经典族)。
    const famOf = (t) => (t.startsWith('handbook') ? 'roco' : 'classic')
    check(`${vp.width}px 图标:只动明暗、风格不变`, famOf(afterT.theme) === famOf(beforeT.theme),
      `${beforeT.theme} → ${afterT.theme}`)
    await p6.close()
  }
} finally {
  await browser.close()
  server.close()
}

const bad = results.filter((r) => !r).length
console.log(bad ? `\n✗ ${bad} 项未通过` : `\n✓ 主题扩散在真浏览器中生效(${results.length} 项)`)
process.exit(bad ? 1 : 0)
