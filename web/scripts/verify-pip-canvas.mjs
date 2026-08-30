// 画中画(PiP)canvas 绘制的校验。
//
//   node scripts/verify-pip-canvas.mjs
//
// 与 scripts/verify-pip.mjs 的分工:那个校验**纯几何**(pipGeom.js 的换算与常量),
// 这个校验**实际画出来的像素**(pipDraw.js 的绘制指令本身)。PiP 是系统悬浮窗,
// 没有 DevTools 可 inspect —— 绘制错了只能靠把帧 dump 出来逐像素判,故固化成脚本。
//
// 做法:在真浏览器里 import 真实的 pipDraw.js,喂一份**合成快照**,画到一张离屏画布上,
// 再读指定位置的像素。判据靠两条:
//   1. 底图用一张故意不存在的图 → 加载不出来 → 整块画布保持 palette.bg;
//   2. palette.bg / palette.bg1 给成辨识度极高的洋红 / 亮绿。
// 于是「标记内部那块区域是洋红还是亮绿」就能直接回答
// **头像背后有没有垫实心底色** —— 垫了就是绿色(不透明),没垫就是洋红(透出底图)。
//
// 另有一组**玩家箭头居中**的断言(见下方 ARROW 段):PiP 的焦点恒为玩家自身位置,
// 故箭头必须画在画布正中。判据取箭头的**像素重心**而非某个顶点 —— 箭头随 heading
// 旋转,顶点位置跟着转,重心不转;断言具体像素会随朝向误报。
//
// 第三组**头像边缘羽化**的断言(见下方 FEATHER 段)校验:中心保持全实、
// 到圆周只渐隐到 50% 而非全透。中心必须仍为全实这条尤其要紧 —— 曾有次改动
// 直接给头像整体加 opacity:.5,那会让**中心也变半透**(把主体也弄虚了),
// 与本需求「只改周围那圈」不符;断言中心 = 1.0 正好能把两种改法区分开。

import { chromium } from 'playwright'
import { createServer } from 'vite'
import { createServer as createHttp } from 'node:http'

const results = []
const check = (name, ok, detail) => {
  results.push({ name, ok })
  console.log(`${ok ? '  ok  ' : ' FAIL '} ${name}${detail ? '  — ' + detail : ''}`)
}

// FEATHER 段需要一张**实心不透明**的图当头像:真实宠物头像是透明背景的 webp,
// 那些区域 alpha 本就是 0,剖面分不清「图本身透明」还是「羽化渐隐」。
// 故另起一个内存 http server 提供纯色 SVG,再由 vite 把 /img 代理过去 ——
// 不留文件、不污染仓库,且路径仍匹配 imgURL 的 '/img/' + path 拼接规则。
// (试过用 configureServer 加 middleware,会被 vite 的 SPA fallback 抢先返回 index.html。)
const SOLID_PATH = '/img/__solid__.svg'
const SOLID_IMG = `<svg xmlns="http://www.w3.org/2000/svg" width="64" height="64"><rect width="64" height="64" fill="#ff0000"/></svg>`
// **只**响应这张测试图,其余一律 404:renderToCanvas 会把 snap.sceneImg 也走 /img 加载,
// 若这里来者不拒,底图就会被同一张红图铺满画布,alpha 剖面全成 255、羽化无从测起。
const imgSrv = createHttp((req, res) => {
  if (req.url !== SOLID_PATH) { res.statusCode = 404; res.end(); return }
  res.setHeader('Content-Type', 'image/svg+xml')
  res.end(SOLID_IMG)
})
await new Promise((r) => imgSrv.listen(5196, r))

// 业务模块有 JSX 与无扩展名导入,Node 直接 import 解析不了,故走 vite dev server。
const server = await createServer({
  root: process.cwd(),
  logLevel: 'error',
  server: { port: 5197, strictPort: true, proxy: { '/img': 'http://localhost:5196' } },
})
await server.listen()

const browser = await chromium.launch({ args: ['--no-sandbox', '--disable-dev-shm-usage'] })
const page = await browser.newPage()
const errors = []
page.on('pageerror', (e) => errors.push(String(e).split('\n')[0]))

try {
  // 挂到 vite dev server 的根:只为拿到模块图,随后用动态 import 取 pipDraw
  await page.goto('http://localhost:5197/', { waitUntil: 'domcontentloaded' })

  const got = await page.evaluate(async () => {
    const m = await import('/src/pages/map/pipDraw.js')
    const SIZE = 512
    const cv = document.createElement('canvas')
    cv.width = cv.height = SIZE
    const ctx = cv.getContext('2d')
    // bg = 洋红(底图),bg1 = 亮绿(实心底色):两者必须完全不同,否则判不出
    const palette = {
      bg: '#ff00ff', bg1: '#00ff00', line: '#0000ff',
      fg: '#ffffff', fgDim: '#808080', gold: '#ffd54a', star: '#ffd54a',
      portal: '#c9b6ff', shiny: '#ffd54a', flowerD: '#ff5fa2', flower: '#ffb6e0',
    }
    // 小窝放在野生宠右侧 100px:mapPx = 512 × zoom(2) = 1024,故 Δu = 100/1024
    const du = 100 / 1024
    const snap = {
      w: SIZE, h: SIZE, zoom: 2, scale: 1,
      focus: { u: 0.5, v: 0.5 },
      palette,
      sceneImg: '__no_such_scene__', // 加载不出 → 底图不画,整块保持 palette.bg
      wilds: [{ u: 0.5, v: 0.5, kinds: [], style: {}, img: null }],
      nests: [{ u: 0.5 + du, v: 0.5, pet: { img: null } }],
    }
    m.renderToCanvas(ctx, snap)

    const px = (x, y) => {
      const d = ctx.getImageData(Math.round(x), Math.round(y), 1, 1).data
      return `rgb(${d[0]}, ${d[1]}, ${d[2]})`
    }
    const cx = SIZE / 2, cy = SIZE / 2
    return {
      // 标记内部、但避开中央的 🐾 字形(字号约 0.53×size,纵向占 ±8px)
      wild: px(cx, cy - 11),   // .map-wild 30px → r=15
      nest: px(cx + 100, cy - 13), // .map-nest 34px → r=17
      bg: px(20, 20),          // 画布角落:必然是底图色
    }
  })

  check('画布底色按 palette.bg 铺满', got.bg === 'rgb(255, 0, 255)', `角落 ${got.bg}`)
  // 透出底图 = 洋红;垫了实心底色 = 亮绿
  check('野生宠头像无实心底色(透出底图)', got.wild === 'rgb(255, 0, 255)', `标记内 ${got.wild}`)
  check('小窝住户头像无实心底色(透出底图)', got.nest === 'rgb(255, 0, 255)', `标记内 ${got.nest}`)

  // —— 玩家箭头必须画在画布正中 ——
  // 背景:PiP 的 focus 恒取玩家位置(见 usePip.buildSnap),故玩家屏幕坐标 = 画布中心。
  // 曾漏了 DOM 版 translate(-50%,-50%) 的对应补偿,箭头整体偏移 (12,12)×k
  // (scale=1 时约 22px),且偏移随 heading 一起旋转 —— 表现为「箭头绕着中心打转」。
  // 多个朝向量一遍:只测一个朝向,恰好朝向与偏移同向时也可能蒙混过关。
  const arrows = await page.evaluate(async () => {
    const m = await import('/src/pages/map/pipDraw.js')
    const SIZE = 512
    const out = []
    for (const heading of [0, 90, 180, 270]) {
      const cv = document.createElement('canvas')
      cv.width = cv.height = SIZE
      const ctx = cv.getContext('2d')
      // 底图白、箭头纯红:只统计「红得发黑」的像素即可把箭头从背景里挑出来。
      // 光晕是半透明红叠在白底上(G 被抬到 ~120),不会被误计入。
      const palette = {
        bg: '#ffffff', bg1: '#ffffff', line: '#000000', fg: '#000000',
        fgDim: '#808080', gold: '#ffff00', star: '#ffff00', portal: '#c9b6ff',
        shiny: '#ffff00', flowerD: '#ff5fa2', flower: '#ffb6e0', red: '#ff0000',
      }
      const u = 0.5, v = 0.5
      m.renderToCanvas(ctx, {
        w: SIZE, h: SIZE, zoom: 2, scale: 1,
        focus: { u, v }, palette,
        sceneImg: '__no_such_scene__',
        player: { u, v, heading },
      })
      const d = ctx.getImageData(0, 0, SIZE, SIZE).data
      let n = 0, sx = 0, sy = 0
      for (let y = 0; y < SIZE; y++) {
        for (let x = 0; x < SIZE; x++) {
          const i = (y * SIZE + x) * 4
          if (d[i] > 200 && d[i + 1] < 40 && d[i + 2] < 40) { n++; sx += x; sy += y }
        }
      }
      out.push({ heading, n, dist: n ? Math.hypot(sx / n - SIZE / 2, sy / n - SIZE / 2) : -1 })
    }
    return out
  })
  // 阈值 4px:形状自身不对称(三角形重心在 viewBox 的 (12,12.7) 而非 (12,12))会留下
  // 约 1px 残余,与 DOM 版行为一致;留 4px 余量既不误报,也足以抓住当年的 22px 偏移。
  const off = arrows.filter((a) => a.n === 0 || a.dist > 4)
  check('玩家箭头居中(4 个朝向重心均贴近画布中心)', off.length === 0,
    off.length ? off.map((a) => `${a.heading}° 偏 ${a.dist.toFixed(1)}px`).join(', ')
      : arrows.map((a) => `${a.heading}° ${a.dist.toFixed(1)}px`).join(' '))

  // —— 头像边缘羽化:中心全实,到圆周只渐隐到 50% ——
  // 需求是「头像**周围那圈**半透」,不是整个头像半透。故两条一起判:
  //   1. 外缘 alpha ≈ 0.5(透出底图);
  //   2. 中心 alpha = 1.0(主体没被一起弄虚)。
  // 第 2 条是关键护栏:曾有误改直接给头像加 opacity:.5,那会把主体也变半透,
  // 与需求不符却也能让第 1 条通过 —— 两条合起来才卡得住。
  const feather = await page.evaluate(async () => {
    const m = await import('/src/pages/map/pipDraw.js')
    const SIZE = 256
    const IMG = '__solid__.svg'

    // loadIcon 首次返回 null(异步加载、下一帧才补上),故先预拉进缓存再轮询渲染。
    await new Promise((res) => {
      const im = new Image()
      im.onload = im.onerror = () => res()
      im.src = '/img/' + IMG
    })

    const cv = document.createElement('canvas')
    cv.width = cv.height = SIZE
    const ctx = cv.getContext('2d')
    // bg 给透明:这样画上去的头像 alpha 不会被底图填充盖掉,读到的就是羽化后的真实 alpha。
    const palette = {
      bg: 'rgba(0,0,0,0)', bg1: 'rgba(0,0,0,0)', line: '#000000', fg: '#000000',
      fgDim: '#808080', gold: '#ffff00', star: '#ffff00', portal: '#c9b6ff',
      shiny: '#ffff00', flowerD: '#ff5fa2', flower: '#ffb6e0', red: '#ff0000',
    }
    const x = SIZE / 2, y = SIZE / 2
    const snap = {
      w: SIZE, h: SIZE, zoom: 2, scale: 1,
      focus: { u: 0.5, v: 0.5 }, palette,
      sceneImg: '__no_such_scene__',
      wilds: [{ u: 0.5, v: 0.5, kinds: [], style: {}, img: IMG }],
    }
    let loaded = false
    for (let i = 0; i < 40 && !loaded; i++) {
      ctx.clearRect(0, 0, SIZE, SIZE)
      m.renderToCanvas(ctx, snap)
      // 判定「头像真的画上去了」要看**颜色**(测试图是纯红 #ff0000,emoji 兜底是
      // palette.fg 的黑色),不能看 alpha —— 本节断言的正是 alpha,若拿 alpha 判加载,
      // 一旦有人把头像整体改半透,这里会误报「图未加载」,把真实缺陷引到错误方向。
      // getImageData 返回非预乘 RGBA,故红色半透时 RGB 仍是 (255,0,0)。
      const p = ctx.getImageData(x, y, 1, 1).data
      loaded = p[0] > 200 && p[1] < 60 && p[2] < 60
      if (!loaded) await new Promise((r) => setTimeout(r, 50))
    }
    if (!loaded) return { error: '测试图未加载(走了 emoji 兜底)' }

    const d = ctx.getImageData(0, 0, SIZE, SIZE).data
    const alphaAt = (dx) => d[(y * SIZE + (x + dx)) * 4 + 3]
    // clipR = SIZES.wild/2 - 2 = 13,圆周内最外一圈取 12(13 已在 clip 边界上会被切掉)
    return { center: alphaAt(0), edge: alphaAt(12) }
  })

  if (feather.error) {
    check('头像边缘羽化到 50%', false, feather.error)
  } else {
    const edgeRatio = feather.edge / 255
    check('头像边缘羽化到 50%(圆周处半透)', Math.abs(edgeRatio - 0.5) < 0.12,
      `圆周 alpha=${feather.edge} (${edgeRatio.toFixed(3)}), 期望 0.5±0.12`)
    check('头像中心仍是全实(未被整体弄半透)', feather.center > 250,
      `中心 alpha=${feather.center}, 期望 255`)
  }

  check('绘制过程无 JS 错误', errors.length === 0, errors.slice(0, 2).join(' | '))
} catch (e) {
  check('执行完成', false, String(e).split('\n')[0])
} finally {
  await browser.close()
  await server.close()
  imgSrv.close()
}

const bad = results.filter((r) => !r.ok)
console.log(`\n${results.length - bad.length}/${results.length} 项通过`)
process.exit(bad.length ? 1 : 0)
