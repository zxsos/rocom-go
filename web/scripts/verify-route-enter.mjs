// 路由过渡包装层(.route-enter)的布局链路校验 —— 真浏览器 + 真 CSS 级联。
//
//   npm run build
//   node scripts/verify-route-enter.mjs            # 自带静态服务(默认 4955)
//   PORT=4960 node scripts/verify-route-enter.mjs
//
// 存在理由:P5.4.1 在 .content 与页面根之间插了一层 .route-enter(用于触发进场动画),
// 这一层是**布局敏感**的:map.css 的高度链是
//     .content:has(.map-page)  → flex 列
//     .map-page                → flex:1 1 auto 撑满
// 多一层普通块级 div 后,flex:1 会落在一个非 flex 容器里失效 —— 地图视口撑不满高度,
// 表现为「地图只剩顶部一条」。jsdom 版的 verify 测不出(它不跑真实布局),
// 只有真浏览器量高度才抓得到,故单列此脚本。
//
// 做法:让真实 SPA 起来(拿到真实的 .app / .content 级联),再往 .content 里注入
// .route-enter > .map-page 探针,量它的高度是否吃满 .content 的可用高度。
// 不依赖后端数据:地图页要等位置推送才渲染 .map-page,故这里直接造 DOM 测 CSS。

import { chromium } from 'playwright'
import { createServer } from 'node:http'
import { readFile } from 'node:fs/promises'
import { extname, join, normalize } from 'node:path'
import { fileURLToPath } from 'node:url'

const here = fileURLToPath(new URL('..', import.meta.url))          // web/
const DIST = join(here, '..', 'internal', 'server', 'web')          // Go embed 的构建产物
const PORT = Number(process.env.PORT || 4955)

const MIME = {
  '.html': 'text/html; charset=utf-8', '.js': 'text/javascript; charset=utf-8',
  '.css': 'text/css; charset=utf-8', '.json': 'application/json',
  '.woff2': 'font/woff2', '.svg': 'image/svg+xml', '.webp': 'image/webp', '.png': 'image/png',
}

// 极简静态服务:只为把构建产物喂给浏览器(不引入 express 之类的运行时依赖)。
const server = createServer(async (req, res) => {
  const url = decodeURIComponent((req.url || '/').split('?')[0])
  // 只服务 DIST 内的文件(防目录穿越);找不到一律回 index.html(SPA fallback)。
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

const browser = await chromium.launch({ args: ['--no-sandbox', '--disable-dev-shm-usage'] })
const page = await browser.newPage({ viewport: { width: 1280, height: 800 } })
try {
  await page.goto(`http://localhost:${PORT}/`, { waitUntil: 'networkidle' })
  // 等真实壳渲染出来(.content 是 App.jsx 里的 <main>)
  await page.waitForSelector('.content', { timeout: 10000 })

  const got = await page.evaluate(() => {
    const content = document.querySelector('.content')
    // 先隐藏既有子节点:它们是真实页面内容(PetList 等),会占满 .content 的自由空间,
    // 导致探针的 flex-grow 无空间可分配、量出来恒为 0 —— 那是**测法的假阳性**,
    // 不是高度链断了。故只留探针一个在流内的子节点。
    const hidden = [...content.children].map((c) => [c, c.style.display])
    hidden.forEach(([c]) => { c.style.display = 'none' })

    // 探针:复刻 MapPage 渲染出的结构 .content > .route-enter > .map-page
    const wrap = document.createElement('div')
    wrap.className = 'route-enter'
    const mp = document.createElement('div')
    mp.className = 'map-page'
    wrap.appendChild(mp)
    content.appendChild(wrap)

    const cs = getComputedStyle(content)
    const avail = content.clientHeight
      - parseFloat(cs.paddingTop) - parseFloat(cs.paddingBottom)
    const h = mp.getBoundingClientRect().height
    const out = { avail, h, contentDisplay: cs.display, wrapDisplay: getComputedStyle(wrap).display }
    wrap.remove()
    hidden.forEach(([c, d]) => { c.style.display = d })
    return out
  })

  // 链路生效的判据:.content 在地图页变 flex 列,且 .map-page 吃满可用高度
  check('地图页 .content 为 flex 列(:has 生效)', got.contentDisplay.includes('flex'), got.contentDisplay)
  check('.route-enter 接住 flex 链(自身也是 flex 列)', got.wrapDisplay.includes('flex'), got.wrapDisplay)
  // 留 1px 容差:亚像素取整
  check('.map-page 撑满 .content 可用高度', Math.abs(got.h - got.avail) <= 1,
    `map-page ${got.h.toFixed(1)}px / 可用 ${got.avail.toFixed(1)}px`)
} finally {
  await browser.close()
  server.close()
}

const bad = results.filter((r) => !r).length
console.log(bad ? `\n✗ ${bad} 项未通过` : '\n✓ 路由过渡层未破坏布局链路')
process.exit(bad ? 1 : 0)
