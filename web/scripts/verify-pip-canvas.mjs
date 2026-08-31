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
//
// DOM 侧的同一套判据见 FEATHER 段末尾:主地图走 CSS(map.css 的 .map-wild-face),
// 是**独立于 canvas 的另一条渲染链**,两边参数靠注释互指同步,注释会失效 ——
// 故用真实 CSS 渲染一个标记、截图逐像素量一遍,把「两条链一致」变成断言。

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

  // —— DOM 侧头像羽化:与上面 canvas 段同一套判据,但走真实 CSS ——
  // 上面那组只证明 canvas 版画对了,DOM 版(map.css 的 .map-wild-face)是**另一条
  // 渲染链**,改错了它照样错。二者的参数靠注释互指保持同步,而注释会失效 —— 故直接
  // 用真实 CSS 渲染一个标记、截图逐像素量一遍,把「两条链一致」变成可执行断言。
  //
  // 这条尤其要紧:mask 写 radial-gradient(circle at 50% 50%, ...) 时,circle 默认按
  // **farthest-corner** 定尺寸,而头像是正方形 —— 渐变 100% 落在角上(距中心 1.414×
  // 半径),可见圆边只在 0.707 处,半透明那一段大半落在被 border-radius 裁掉的角里,
  // 实测圆周 alpha 只剩 .83,需求「周围那圈半透」等于没生效。必须 closest-side。
  //
  // 量法:纯红实心图叠在**白底**上截图,红叠白后 G = 255×(1-alpha),故
  // alpha = 1 − G/255。沿水平中线从中心扫到圆周,取归一化半径 0.6 / 1.0 两点。
  const FBOX = 60 // 放大到 60px 采样,与 30px 几何比例一致
  await page.evaluate(async ({ size, img }) => {
    // 清掉 app 自己渲染的 UI:取样框就放在 (0,0),顶栏/图标会盖上去,截图会把
    // 别人的像素一起拍进来。只清 body、保留 <head> —— map.css 是 app 在 <head>
    // 里注入的(vite dev 把 CSS 转成 JS 注入样式表),连 head 一起换掉就量不到真样式了
    // (试过 setContent 空页再 import('/src/styles/map.css'),样式表数为 0,没生效)。
    document.body.innerHTML = ''
    document.body.style.cssText = 'margin:0;background:#fff'
    const box = document.createElement('div')
    box.id = '__feather_dom__'
    box.className = 'map-wild'
    // 去掉 translate(-50%,-50%) 与描边:前者会把标记挪出视口,后者的灰色环
    // 会混进边缘采样。这里只关心 .map-wild-face 的遮罩,不关心外框装饰。
    box.style.cssText = `position:absolute;left:0;top:0;width:${size}px;height:${size}px;
      transform:none;border:0;filter:none`
    const face = document.createElement('img')
    face.id = '__feather_face__'
    face.className = 'map-wild-face'
    // min-width/height:0 不能省:.map-wild 是 flex 容器,而 flex 子项的自动最小尺寸
    // 是**内容固有尺寸** —— img 会撑到测试图的固有 64px 而不吃 width:100%,
    // 取样就不再是正方形。(真实页面有全局 img 重置兜着,这里空页面没有。)
    face.style.cssText = 'min-width:0;min-height:0'
    face.src = img
    box.appendChild(face)
    document.body.appendChild(box)
    await face.decode()
  }, { size: FBOX, img: SOLID_PATH })

  const shotBuf = await page.locator('#__feather_face__').screenshot()
  if (process.env.DUMP_FEATHER) {
    const { writeFileSync } = await import('node:fs')
    writeFileSync(process.env.DUMP_FEATHER, shotBuf)
  }
  const shot = shotBuf.toString('base64')
  const domProfile = await page.evaluate(async (b64) => {
    const im = new Image()
    im.src = 'data:image/png;base64,' + b64
    await im.decode()
    const c = document.createElement('canvas')
    c.width = im.width; c.height = im.height
    const ctx = c.getContext('2d', { willReadFrequently: true })
    ctx.drawImage(im, 0, 0)
    const d = ctx.getImageData(0, 0, im.width, im.height).data
    const y = Math.floor(im.height / 2)
    const cx = im.width / 2
    const r = im.width / 2 // 内切圆半径 = 半边长
    const at = (t) => { // t 为归一化半径,取最接近的那一列
      const x = Math.min(im.width - 1, Math.round(cx + t * r) - 1)
      const i = (y * im.width + x) * 4
      // 红叠白:R 恒 255,G = 255×(1-alpha)
      return { a: d[i + 3] === 0 ? 0 : 1 - d[i + 1] / 255, rgba: [d[i], d[i + 1], d[i + 2], d[i + 3]] }
    }
    const c0 = at(0), e0 = at(0.97)
    return {
      center: c0.a, inner: at(0.55).a, edge: e0.a, w: im.width, h: im.height,
    }
  }, shot)

  if (domProfile.w !== FBOX) {
    check('DOM 头像羽化可测量', false, `截图尺寸 ${domProfile.w},期望 ${FBOX}`)
  } else {
    check('DOM 头像边缘羽化到 50%(圆周处半透)',
      Math.abs(domProfile.edge - 0.5) < 0.12,
      `圆周 alpha=${domProfile.edge.toFixed(3)},期望 0.5±0.12`)
    check('DOM 头像中心仍是全实(未被整体弄半透)', domProfile.center > 0.97,
      `中心 alpha=${domProfile.center.toFixed(3)},期望 1.0`)
    // 与 canvas 侧对齐:两条渲染链同一套视觉,差太多就说明有一边改漏了
    check('DOM 与 canvas 羽化一致(两条渲染链)',
      feather.error ? false : Math.abs(domProfile.edge - feather.edge / 255) < 0.12,
      feather.error ? 'canvas 侧未测出,无法比对' : `DOM ${domProfile.edge.toFixed(3)} vs canvas ${(feather.edge / 255).toFixed(3)}`)
  }
  await page.evaluate(() => document.getElementById('__feather_dom__')?.remove())

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
