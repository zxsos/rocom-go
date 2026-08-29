// 验收 SSE 分发层的语义:subscribe(type, onData, {onOpen}) 下沉到 api.js 后,
//   1. 类型过滤:只把匹配类型的消息交给回调
//   2. 账号过滤:非当前账号的消息被拦下(切账号瞬间的在途消息)
//   3. stream-open:只进 onOpen,不进 onData(它是「断线期间数据已丢」的信号)
//
// 为什么用注入验证:后端 pcap 回放是一次性的(约 40ms 放完),结束后不再推业务事件,
// 真实事件流在当前环境拿不到。这里直接驱动 api.js 内部的分发函数,验的是**前端逻辑本身**。
//
//   node scripts/verify-subscribe.mjs

import { createServer } from 'vite'
import { JSDOM } from 'jsdom'

const dom = new JSDOM('<!doctype html><html><body></body></html>', { url: 'http://localhost:5173' })
const win = dom.window
try { globalThis.window = win } catch { Object.defineProperty(globalThis, 'window', { value: win, configurable: true }) }
try { globalThis.localStorage = win.localStorage } catch {
  Object.defineProperty(globalThis, 'localStorage', { value: win.localStorage, configurable: true })
}

// 捕获前端创建的 EventSource,以便手动投喂事件。
// 注意 api.js 里的 syncStream 用的是**模块作用域的全局** EventSource(不是 window.EventSource),
// 故必须挂到 globalThis 上,只设 window.EventSource 抓不到。
let es = null
const FakeES = class {
  constructor(url) { this.url = url; this.l = new Map(); es = this }
  addEventListener(t, fn) { if (!this.l.has(t)) this.l.set(t, []); this.l.get(t).push(fn) }
  removeEventListener() {}
  close() {}
  // 后端的消息形状:{type, account, data}(类型在 payload 的 type 字段里,
  // 不是 SSE 的 event: 名),统一走 es.onmessage 这一个入口。
  fire(type, payload, account) {
    this.onmessage?.({ data: JSON.stringify({ type, account, data: payload }) })
  }
}
win.EventSource = FakeES
try { globalThis.EventSource = FakeES } catch {
  Object.defineProperty(globalThis, 'EventSource', { value: FakeES, configurable: true })
}

const server = await createServer({ root: process.cwd(), logLevel: 'error', server: { middlewareMode: true, hmr: false } })
const api = await server.ssrLoadModule('/src/api.js')

const results = []
const check = (name, cond, detail = '') => {
  results.push({ name, ok: cond, detail })
  console.log(`${cond ? '✅' : '❌'} ${name}${detail ? '  ' + detail : ''}`)
}

// —— 1. 类型过滤 + onOpen 分离 ——
// 当前无选中账号(api.js 的 currentAccount 为空),账号过滤不生效,单验类型过滤
const got = []
let opened = 0
const off1 = api.subscribe('pet', (d) => got.push(d), { onOpen: () => opened++ })

check('订阅后建立了 SSE 连接', !!es, es ? es.url : '未创建')
es.onopen?.() // 前端在 es.onopen 里造 stream-open
check('stream-open 触发 onOpen', opened === 1, `opened=${opened}`)

es.fire('pet', { gid: 1 })
es.fire('event', { gid: 2 }) // 不匹配,应被拦下
es.fire('pet', { gid: 3 })
check('只收到匹配类型的消息', got.length === 2, `收到 ${got.length} 条: ${got.map((d) => d.gid).join(',')}`)
check('stream-open 未进 onData', got.every((d) => d.gid !== undefined))
off1()

// —— 2. 账号过滤 ——
// 设当前账号后,其它账号的消息应被拦下
api.setCurrentAccount('UID:10002')
const got2 = []
const off2 = api.subscribe('pet', (d) => got2.push(d))
es.fire('pet', { gid: 10 }, 'UID:10002')      // 同账号 → 放行
es.fire('pet', { gid: 11 }, 'UID:100000002')  // 别的账号 → 拦下
es.fire('pet', { gid: 12 })                   // 无账号字段(全局消息) → 放行
check('账号过滤:同账号放行、异账号拦截、无账号放行',
  got2.length === 2 && got2[0].gid === 10 && got2[1].gid === 12,
  `收到 ${got2.map((d) => d.gid).join(',')}`)
off2()

// —— 3. 多类型订阅 ——
const got3 = []
const off3 = api.subscribe(['stars', 'starzones'], (d, t) => got3.push(t))
es.fire('stars', {})
es.fire('starzones', {})
es.fire('pet', {}) // 不在列表里
check('数组类型订阅', got3.length === 2 && got3.join(',') === 'stars,starzones', got3.join(','))
off3()

// —— 4. 取消订阅后不再收到 ——
const got4 = []
const off4 = api.subscribe('pet', (d) => got4.push(d))
es.fire('pet', {})
off4()
es.fire('pet', {})
check('取消订阅后不再收到', got4.length === 1, `收到 ${got4.length} 条`)

await server.close()
const bad = results.filter((r) => !r.ok)
console.log(`\n${results.length - bad.length}/${results.length} 项通过`)
process.exit(bad.length ? 1 : 0)
