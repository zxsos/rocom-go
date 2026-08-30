// admin 面板「失效令牌」验收:真浏览器打开 #/admin,验证令牌失效时**踢回登录页**而不是黑屏。
//
//   npm run build && 启动后端
//   node scripts/verify-admin-browser.mjs [后端地址]
//
// 背景(用户报的「面板闪一下就黑屏、无法进入」):
// 令牌是服务端内存态,重启即失效;浏览器 localStorage 里那份还在,于是页面一进来就认定已登录、
// 先把面板渲染出来(「闪一下」),随后 7 个 admin 接口齐刷刷返 401。
// useAdminFetch 通知上层踢回登录页时若漏传 error,Admin 的 kickIfUnauthed(err) 读 err.status
// 就抛 TypeError;没有 error boundary,整棵 React 树被卸载 → root 变空 → 深色主题下即黑屏。
// 更要命的是刷新也没用(令牌仍在 localStorage),管理员被永久锁死在外面。
//
// 判据不用「有没有报错」这种软指标,而看**用户能不能自救**:
//   - root 必须非空(没有整树卸载);
//   - 必须出现登录卡(能被踢回来),而不是停在一个全是 401 的面板上。
// 这两条任一条缺失,管理员就真的进不去了。

import { createRequire } from 'node:module'
const require = createRequire(import.meta.url)
const { chromium } = require('playwright')

const BASE = process.argv[2] || process.env.E2E_BASE || 'http://localhost:4939'
// 一个服务端不可能认的假令牌:足以触发 401,与「服务重启后旧令牌失效」完全同构。
const STALE = 'stale-token-for-e2e'

const results = []
const check = (name, ok, detail) => {
  results.push({ name, ok })
  console.log(`${ok ? '  ok  ' : ' FAIL '} ${name}${detail ? '  — ' + detail : ''}`)
}

const browser = await chromium.launch({ args: ['--no-sandbox', '--disable-dev-shm-usage'] })
const ctx = await browser.newContext({ viewport: { width: 1280, height: 900 } })
// 必须在任何页面脚本之前写入,才能复现「用户之前登录过」的状态。
await ctx.addInitScript((t) => { localStorage.setItem('adminToken', t) }, STALE)
const page = await ctx.newPage()

const crashes = []
page.on('pageerror', (e) => crashes.push(String(e).split('\n')[0]))

try {
  await page.goto(BASE + '/#/admin', { waitUntil: 'domcontentloaded' })
  await page.waitForTimeout(4000)

  const st = await page.evaluate(() => {
    const root = document.getElementById('root')
    const text = (root?.textContent || '').replace(/\s+/g, '')
    return {
      len: root ? root.innerHTML.length : -1,
      // 登录卡的标题文案(未配密码时是「设置管理员密码」,已配是「管理员登录」)
      hasLogin: text.includes('管理员登录') || text.includes('设置管理员密码'),
      hasLogout: text.includes('退出登录'), // 还停在面板上的标志
    }
  })

  check('没有整树卸载(root 非空)', st.len > 0, `DOM ${st.len}B`)
  check('已踢回登录卡', st.hasLogin && !st.hasLogout,
    st.hasLogout ? '仍停在面板上(没踢回来)' : (st.hasLogin ? '' : '未出现登录卡'))
  check('无未捕获异常', crashes.length === 0, crashes.slice(0, 2).join(' | '))
} catch (e) {
  check('执行完成', false, String(e).split('\n')[0])
} finally {
  await browser.close()
}

const bad = results.filter((r) => !r.ok)
console.log(`\n${results.length - bad.length}/${results.length} 项通过`)
process.exit(bad.length ? 1 : 0)
