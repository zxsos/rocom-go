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

import { chromium } from 'playwright'
import { createServer } from 'vite'

const results = []
const check = (name, ok, detail) => {
  results.push({ name, ok })
  console.log(`${ok ? '  ok  ' : ' FAIL '} ${name}${detail ? '  — ' + detail : ''}`)
}

// 业务模块有 JSX 与无扩展名导入,Node 直接 import 解析不了,故走 vite dev server。
const server = await createServer({ root: process.cwd(), logLevel: 'error', server: { port: 5197, strictPort: true } })
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
  check('绘制过程无 JS 错误', errors.length === 0, errors.slice(0, 2).join(' | '))
} catch (e) {
  check('执行完成', false, String(e).split('\n')[0])
} finally {
  await browser.close()
  await server.close()
}

const bad = results.filter((r) => !r.ok)
console.log(`\n${results.length - bad.length}/${results.length} 项通过`)
process.exit(bad.length ? 1 : 0)
