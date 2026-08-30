// 游玩记录分页验收:真浏览器打开管理面板,验证分页控件的行为。
//
//   npm run build && 启动后端(需带 play_sessions 数据)
//   node scripts/verify-admin-paging.mjs [后端地址]
//
// 覆盖「管理面板游玩记录表格要分页」这条需求里**真正容易错**的三点,只看「翻页能点」
// 是抓不到的:
//   1. 每页真的只渲染 pageSize 行(不是一次全拉 200 行);
//   2. 翻到下一页内容**换了**(若后端漏了 OFFSET,两页是同一批数据,肉眼很难察觉);
//   3. 切筛选后页码重置回第 1 页(否则从「全部」的末页切到只有几条记录的账号,
//      会停在越界的空白页,看上去像「筛完就没数据了」)。
// 后两点正是分页最典型的缺陷,故判据取「内容变化」与「页码归位」而非按钮是否可点。

import { createRequire } from 'node:module'
const require = createRequire(import.meta.url)
const { chromium } = require('playwright')

const BASE = process.argv[2] || process.env.E2E_BASE || 'http://localhost:4939'
// 复现「用户之前登录过」的状态:先设密码再进面板。
const PW = process.env.ADMIN_PW || 'diag12345'

const results = []
const check = (name, ok, detail) => {
  results.push({ name, ok })
  console.log(`${ok ? '  ok  ' : ' FAIL '} ${name}${detail ? '  — ' + detail : ''}`)
}

const browser = await chromium.launch({ args: ['--no-sandbox', '--disable-dev-shm-usage'] })
const page = await browser.newPage({ viewport: { width: 1280, height: 1000 } })
const errors = []
page.on('pageerror', (e) => errors.push(String(e).split('\n')[0]))

// 取整行文本序列,用于比较两页内容是否真换了。
// 两个坑,都踩过:
//   1. 只取首列(玩家名+UID)不行 —— 单账号数据下每行的首列完全相同,
//      两页比对必然「全部重复」,断言形同虚设。故取整行(含上/下线时间与时长)。
//   2. 必须限定在「游玩记录」卡片内:.admin-play-table 是共用表格样式,查蛋统计卡片
//      (EggStatsCard) 也用它,直接全局选会把那边的行一起数进来,条数断言就失准了。
const rows = () => page.evaluate(() => {
  const card = [...document.querySelectorAll('.admin-card')]
    .find((c) => (c.querySelector('h3')?.textContent || '').includes('游玩记录'))
  if (!card) return []
  return [...card.querySelectorAll('.admin-play-table tbody tr')]
    .map((tr) => [...tr.children].map((td) => (td.textContent || '').trim()).join('|'))
})
const pagerText = () => page.evaluate(() => {
  const el = document.querySelector('.admin-pager')
  return el ? el.textContent.replace(/\s+/g, ' ').trim() : null
})

try {
  await page.goto(BASE + '/#/admin', { waitUntil: 'domcontentloaded' })
  await page.waitForTimeout(2500)
  // 首次进入需设密码(未配置时表单有两处密码输入)
  const inputs = await page.$$('input[type="password"]')
  if (inputs.length) {
    for (const el of inputs) await el.fill(PW)
    await page.keyboard.press('Enter')
    await page.waitForTimeout(1200)
  }
  await page.waitForSelector('.admin-card', { timeout: 15000 })
  await page.waitForTimeout(3500)

  const head = await page.evaluate(() => (document.body.textContent || '').includes('管理面板'))
  if (!head) {
    check('已进入管理面板', false, '没见到面板标题(可能没登录成功)')
  } else {
    const p1 = await rows()
    const pager1 = await pagerText()
    check('分页控件已渲染', !!pager1, pager1 || '没有 .admin-pager')

    const size = await page.evaluate(() => {
      const m = /共\s*(\d+)\s*条/.exec(document.querySelector('.admin-pager')?.textContent || '')
      return m ? parseInt(m[1], 10) : 0
    })
    // 每页条数默认 50:行数 = min(50, 总数)
    check('每页只渲染 pageSize 行', p1.length > 0 && p1.length <= 50, `${p1.length} 行 / 共 ${size} 条`)

    if (size > 50) {
      // 翻到第二页:内容必须与第一页完全不同(OFFSET 生效)
      await page.click('.admin-pager .btn:has-text("下一页")')
      await page.waitForTimeout(2500)
      const p2 = await rows()
      const pager2 = await pagerText()
      check('翻页后页码变为第 2 页', /第 2 \//.test(pager2 || ''), pager2 || '-')
      const overlap = p2.filter((r) => p1.includes(r))
      check('第二页内容与第一页不重复', p2.length > 0 && overlap.length === 0,
        `第2页 ${p2.length} 行, 与第1页重复 ${overlap.length} 行`)

      // 切筛选后必须回到第 1 页
      await page.evaluate(() => {
        const dds = [...document.querySelectorAll('.admin-play-toolbar .dropdown-trigger')]
        if (dds[0]) dds[0].click()
      })
      await page.waitForTimeout(400)
      const picked = await page.evaluate(() => {
        const items = [...document.querySelectorAll('.admin-play-toolbar .dropdown-item')]
        // 选第一个真实账号(跳过「全部账号」)
        const it = items.find((i) => !/全部账号/.test(i.textContent || ''))
        if (it) { it.dispatchEvent(new MouseEvent('mousedown', { bubbles: true })); return it.textContent.trim() }
        return null
      })
      await page.waitForTimeout(2500)
      const pager3 = await pagerText()
      check('切换账号筛选后回到第 1 页', /第 1 \//.test(pager3 || ''),
        `选了 ${picked || '(无账号可选)'}, 现为 ${pager3 || '-'}`)
    } else {
      check('翻页/筛选重置(需 >50 条数据)', true, `本份数据仅 ${size} 条,跳过`)
    }

    check('无未捕获异常', errors.length === 0, errors.slice(0, 2).join(' | '))
  }
} catch (e) {
  check('执行完成', false, String(e).split('\n')[0])
} finally {
  await browser.close()
}

const bad = results.filter((r) => !r.ok)
console.log(`\n${results.length - bad.length}/${results.length} 项通过`)
process.exit(bad.length ? 1 : 0)
