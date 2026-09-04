// 页面启动(白屏/黑屏)验收。
//
//   npm run build && 启动后端
//   node scripts/verify-page-boot.mjs [后端地址]
//
// 存在理由:2026-09-04 线上黑屏,排查耗时很久,而根因**服务端完全看不出来**。
// 某个 assets chunk 不在 embed 里时:
//
//   1. `//go:embed all:web` 对缺失文件**不报错**,二进制照常编出来;
//   2. handleStatic 找不到该文件,按 SPA fallback 返回 index.html —— 200 + text/html;
//   3. 浏览器按 HTML 规范**拒绝**把 HTML 当 module script 执行(严格 MIME 检查);
//   4. React 从未挂载 → #root 恒空 → 页面全黑。
//
// 全程:后端返回 200、无错误日志、接口全正常(实测 /api/accounts 照样 200)。
// 只有浏览器控制台会留下一句 MIME 报错,不打开 DevTools 就一无所知。
// 本脚本把这条错误变成一条可执行、会红的断言。
//
// 判据(四条独立,缺一不可):
//   1. #root 必须真的挂载出内容 —— 这是「没黑屏」的唯一硬证据;
//   2. 不能有 pageerror(未捕获异常);
//   3. 不能有资源加载失败 / HTTP 4xx·5xx;
//   4. **控制台不能出现 module script 的 MIME 报错** —— 缺件特有,单独列出来
//      是为了让失败信息直接指向根因,而不是让人对着黑屏猜。
//
// 顺带核对 index.html 引用的每个 assets/* 都能取到且 MIME 正确:
// 比等浏览器报错更早一步定位到**具体是哪个文件**缺了。

import { createRequire } from 'node:module'
const require = createRequire(import.meta.url)
const { chromium } = require('playwright')

const BASE = process.argv[2] || process.env.E2E_BASE || 'http://localhost:4939'

const results = []
const check = (name, ok, detail) => {
  results.push({ name, ok })
  console.log(`${ok ? '  ok  ' : ' FAIL '} ${name}${detail ? '  — ' + detail : ''}`)
}

// 先静态核对 index.html 引用的资源:能直接点名是哪个文件缺了,比看浏览器报错快。
{
  const html = await fetch(BASE + '/').then((r) => r.text())
  const refs = [...new Set([...html.matchAll(/assets\/[A-Za-z0-9._-]+/g)].map((m) => m[0]))]
  check('index.html 引用了前端资源', refs.length > 0, `${refs.length} 个`)

  const broken = []
  for (const r of refs) {
    const res = await fetch(BASE + '/' + r)
    const type = res.headers.get('content-type') || ''
    // 资源路径必须是 JS/CSS 等,绝不能是 text/html —— 那就是 fallback 成了 index.html
    if (!res.ok || type.includes('text/html')) {
      broken.push(`${r} → HTTP ${res.status} ${type}`)
    }
  }
  check('引用的资源都存在且 MIME 正确', broken.length === 0,
    broken.length ? broken.join('; ') : `${refs.length} 个全部正常`)
}

const browser = await chromium.launch({ args: ['--no-sandbox', '--disable-dev-shm-usage'] })
const page = await browser.newPage({ viewport: { width: 1280, height: 800 } })

const pageErrors = []
const failed = []
const consoleErrors = []
page.on('pageerror', (e) => pageErrors.push(e.message))
// SSE 是长连接:关闭页面时它必然被中断(net::ERR_ABORTED),那是收尾不是故障。
// 不排除的话这条永远红 —— 一个恒红的断言会让人养成忽略它的习惯,还不如没有。
const isSSE = (url) => url.includes('/api/stream')
page.on('requestfailed', (r) => {
  if (isSSE(r.url())) return
  failed.push(`${r.url()} — ${r.failure()?.errorText}`)
})
page.on('response', (r) => { if (r.status() >= 400) failed.push(`HTTP ${r.status()} ${r.url()}`) })
page.on('console', (m) => { if (m.type() === 'error') consoleErrors.push(m.text()) })

try {
  await page.goto(BASE + '/', { waitUntil: 'load', timeout: 30000 })
  await page.waitForTimeout(4000) // 等 React 挂载 + 首屏接口回来

  const root = await page.evaluate(() => {
    const el = document.getElementById('root')
    return { exists: !!el, len: el ? el.innerHTML.length : -1 }
  })
  check('#root 存在', root.exists)
  check('#root 已挂载出内容(未黑屏)', root.len > 0,
    root.len > 0 ? `${root.len} 字节` : '#root 是空的 —— React 没挂载')

  check('无未捕获异常', pageErrors.length === 0, pageErrors.join(' | '))
  check('无资源加载失败', failed.length === 0, failed.join(' | '))

  // 缺件的特征报错,单列出来直接指向根因
  const mimeErr = consoleErrors.filter((t) => /MIME type|module script/i.test(t))
  check('无 module script MIME 报错(前端产物不缺件)', mimeErr.length === 0,
    mimeErr.join(' | ') || '')
  check('控制台无其它 error', consoleErrors.length === mimeErr.length,
    consoleErrors.filter((t) => !/MIME type|module script/i.test(t)).join(' | ') || '')
} catch (e) {
  check('页面启动 执行完成', false, String(e).split('\n')[0])
} finally {
  await page.close()
}

await browser.close()

const bad = results.filter((r) => !r.ok)
console.log(`\n${results.length - bad.length}/${results.length} 项通过`)
if (bad.length) {
  console.log('\n若报「MIME 报错」或「引用的资源 MIME 不对」,即前端产物缺件:')
  console.log('  git checkout -- internal/server/web/ && cd web && npm run build')
  console.log('  然后重新编译部署(go build 的 embed 发生在编译期,必须重编)')
}
process.exit(bad.length ? 1 : 0)
