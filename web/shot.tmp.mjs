import { chromium } from 'playwright'

const url = process.argv[2] || 'http://localhost:5173/mock.html'
const out = process.argv[3] || '/tmp/breeding-10gen.png'
const width = Number(process.argv[4] || 1440)

const b = await chromium.launch()
const p = await b.newPage({ viewport: { width, height: 1000 }, deviceScaleFactor: 1 })
const errs = []
p.on('console', (m) => { if (m.type() === 'error') errs.push(m.text()) })
p.on('pageerror', (e) => errs.push('pageerror: ' + e.message))
await p.goto(url, { waitUntil: 'networkidle' })
await p.waitForTimeout(1200)
// 把主题变量落到亮色:base.css 可能依赖 prefers-color-scheme
await p.screenshot({ path: out, fullPage: true })
console.log('saved', out)
console.log('控制台错误:', errs.length ? errs.slice(0, 8).join('\n') : '无')
await b.close()
