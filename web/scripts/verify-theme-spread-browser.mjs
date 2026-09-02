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
// 不依赖后端数据:只点顶栏的主题按钮,壳渲染出来即可。

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
const BASE = `http://localhost:${PORT}/`

// 页面内脚本:点主题按钮,把「点击当下 / 过渡中途 / 过渡结束」三处状态一次采回来。
// 用 btn.click() 而不是 Playwright 的 locator.click():后者是异步的,
// 等它返回时 620ms 的过渡早跑完了,抓不到中途那一帧。
//
// **取样按动画自己的 currentTime 推进,不用墙钟 sleep**:headless 下 rAF 与定时器
// 都可能被滞后(实测用 rAF 等过渡建立时,整条取样时间轴被推后了几百毫秒,
// 取到的「中途」其实是动画的第 0 帧 —— 圆半径 0,看着像动画没跑)。
const probe = async () => {
  const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
  const root = document.documentElement
  const btn = [...document.querySelectorAll('.topbar-fs')]
    .find((b) => b.querySelector('.topbar-theme-icon'))
  if (!btn) return { error: '找不到主题按钮' }

  const dur = parseFloat(getComputedStyle(root).getPropertyValue('--dur-theme')) || 620
  const box = btn.getBoundingClientRect()
  const wantX = box.left + box.width / 2
  const wantY = box.top + box.height / 2
  const before = root.getAttribute('data-theme')

  btn.click()
  // ① 点击当下:圆心变量与 class 必须已经注入(拿不到圆心就没法从按钮扩散)
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
  // 固定初始态:主题存 auto + 浏览器深色 → 生效色是 dark。
  // 必须这样钉死:若生效色本来就是 light,点一下(→ light)颜色没变,
  // 走的是「换了模式但颜色不变」的瞬时分支(见下面 [8]),这里就测不到扩散了。
  const page = await browser.newPage({ viewport: { width: 1280, height: 800 }, colorScheme: 'dark' })
  await page.addInitScript(() => localStorage.setItem('theme', '"auto"'))
  await page.goto(BASE, { waitUntil: 'networkidle' })
  await page.waitForSelector('.topbar-theme-icon', { timeout: 10000 })

  const a = await page.evaluate(probe)
  if (a.error) {
    check('找到主题按钮', false, a.error)
  } else if (a.noAnim) {
    check('过渡里跑着 theme-spread 动画', false,
      `没找到(伪元素选择器没匹配上?)。实际动画: ${(a.seen || []).join(' | ') || '无'}`)
  } else {
    console.log('\n[1] 点击当下:圆心取自按钮')
    check('theme-spread class 已挂上', a.at0.cls)
    // 亚像素容差 1px:注入的是未取整的中心坐标
    check('--theme-x/y = 按钮中心',
      Math.abs(a.at0.x - a.wantX) <= 1 && Math.abs(a.at0.y - a.wantY) <= 1,
      `注入 (${a.at0.x}, ${a.at0.y}) / 按钮中心 (${a.wantX}, ${a.wantY})`)
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

    // —— 连续快速点击:不能留下摘不掉的 class / 变量 ——
    console.log('\n[5] 连点两次(第二次会抢占第一次的过渡)')
    await page.evaluate(async () => {
      const btn = [...document.querySelectorAll('.topbar-fs')]
        .find((b) => b.querySelector('.topbar-theme-icon'))
      btn.click()
      await new Promise((r) => setTimeout(r, 80)) // 过渡还没结束就再点一次
      btn.click()
    })
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
    localStorage.setItem('theme', '"auto"')
    delete Document.prototype.startViewTransition
  })
  await p2.goto(BASE, { waitUntil: 'networkidle' })
  await p2.waitForSelector('.topbar-theme-icon', { timeout: 10000 })
  const b = await p2.evaluate(async () => {
    const root = document.documentElement
    const btn = [...document.querySelectorAll('.topbar-fs')]
      .find((el) => el.querySelector('.topbar-theme-icon'))
    const before = root.getAttribute('data-theme')
    btn.click()
    await new Promise((r) => setTimeout(r, 120))
    return { before, after: root.getAttribute('data-theme'), cls: root.classList.contains('theme-spread') }
  })
  check('主题切换成功(无异常)', b.before !== b.after, `${b.before} → ${b.after}`)
  check('没有挂 class(不播动画)', !b.cls)
  await p2.close()

  // —— 退化路径 2:用户开了「减少动态效果」——
  console.log('\n[7] 退化:prefers-reduced-motion: reduce')
  const p3 = await browser.newPage({ viewport: { width: 1280, height: 800 }, reducedMotion: 'reduce', colorScheme: 'dark' })
  await p3.addInitScript(() => localStorage.setItem('theme', '"auto"'))
  await p3.goto(BASE, { waitUntil: 'networkidle' })
  await p3.waitForSelector('.topbar-theme-icon', { timeout: 10000 })
  const c = await p3.evaluate(async () => {
    const root = document.documentElement
    const btn = [...document.querySelectorAll('.topbar-fs')]
      .find((el) => el.querySelector('.topbar-theme-icon'))
    const before = root.getAttribute('data-theme')
    btn.click()
    await new Promise((r) => setTimeout(r, 120))
    return { before, after: root.getAttribute('data-theme'),
      cls: root.classList.contains('theme-spread'), anims: document.getAnimations().length }
  })
  check('主题切换成功', c.before !== c.after, `${c.before} → ${c.after}`)
  check('没有播扩散动画', !c.cls)
  await p3.close()

  // —— 退化路径 3:换了模式但**颜色没变**(浅色浏览器里 auto → light)——
  // 这时不能为它冻屏 620ms:全程看不到任何变化,观感是「点了没反应」
  // (过渡期间页面是快照,连按钮图标都要等过渡结束才换)。
  console.log('\n[8] 退化:生效颜色不变时不冻屏(浅色浏览器 auto → light)')
  const p4 = await browser.newPage({ viewport: { width: 1280, height: 800 }, colorScheme: 'light' })
  await p4.addInitScript(() => localStorage.setItem('theme', '"auto"'))
  await p4.goto(BASE, { waitUntil: 'networkidle' })
  await p4.waitForSelector('.topbar-theme-icon', { timeout: 10000 })
  const d = await p4.evaluate(async () => {
    const root = document.documentElement
    const btn = [...document.querySelectorAll('.topbar-fs')]
      .find((el) => el.querySelector('.topbar-theme-icon'))
    const before = root.getAttribute('data-theme')
    btn.click()
    await new Promise((r) => setTimeout(r, 100))
    return { before, after: root.getAttribute('data-theme'),
      cls: root.classList.contains('theme-spread'), mode: localStorage.getItem('theme') }
  })
  check('生效色不变,故不播扩散', !d.cls)
  check('但模式确实切过去了', d.mode === '"light"', `theme=${d.mode} / ${d.before} → ${d.after}`)
  // 再点一次(light → dark,颜色真的变了)必须恢复正常扩散 ——
  // 上面那条分支不能把后续点击也带成瞬时切换。
  const again = await p4.evaluate(async () => {
    const root = document.documentElement
    const btn = [...document.querySelectorAll('.topbar-fs')]
      .find((el) => el.querySelector('.topbar-theme-icon'))
    btn.click()
    await new Promise((r) => setTimeout(r, 60))
    return { cls: root.classList.contains('theme-spread'), theme: root.getAttribute('data-theme') }
  })
  check('下一次点击(颜色真的变)恢复扩散', again.cls, `data-theme=${again.theme}`)
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
  await p5.addInitScript(() => localStorage.setItem('theme', '"auto"'))
  await p5.goto(BASE, { waitUntil: 'networkidle' })
  await p5.waitForSelector('.topbar-theme-icon', { timeout: 10000 })

  const pts = await p5.evaluate(() => {
    const btn = [...document.querySelectorAll('.topbar-fs')]
      .find((b) => b.querySelector('.topbar-theme-icon'))
    const r = btn.getBoundingClientRect()
    return { A: [Math.round(r.left + r.width / 2), Math.round(r.top + r.height / 2 + 150)],
      B: [window.innerWidth - 40, window.innerHeight - 40] }
  })
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
  const t0 = Date.now()
  await p5.evaluate(() => {
    document.documentElement.style.setProperty('--dur-theme', '6000ms')
    const btn = [...document.querySelectorAll('.topbar-fs')]
      .find((b) => b.querySelector('.topbar-theme-icon'))
    btn.click()
  })
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
} finally {
  await browser.close()
  server.close()
}

const bad = results.filter((r) => !r).length
console.log(bad ? `\n✗ ${bad} 项未通过` : `\n✓ 主题扩散在真浏览器中生效(${results.length} 项)`)
process.exit(bad ? 1 : 0)
