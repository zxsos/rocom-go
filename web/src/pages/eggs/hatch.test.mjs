// hatchProgress 的回归测试(纯 Node 跑:node web/src/pages/eggs/hatch.test.mjs)。
//
// 为什么要有它:孵化进度是**本地外推**的,而外推的输入(hatchUpdate/hatchedSecs)
// 可能压根不存在 —— 登录包 0x0102 只给「哪些蛋在孵」,逐蛋进度要等开孵蛋器 0x0312
// 或开背包 0x1344 才下发。早先 hatchProgress 对 hatchUpdate=0 也照常外推,算出
// 「已过十几亿秒」→ 进度顶满 → EggList 把「在孵且进度满」的蛋两栏都不显示,
// 蛋凭空消失 —— 玩家看到的就是「登录后孵蛋器空的 / 没刷新」。
import assert from 'node:assert/strict'
import { hatchProgress } from './hatch.js'

const NOW = 1788000000 * 1000 // 固定"当前时刻",避免依赖真实时钟

// 不在孵蛋器里 → null
assert.equal(hatchProgress({ hatching: false, maxSecs: 3600 }, NOW), null)
// 缺 maxSecs(配置缺失)→ null,不崩
assert.equal(hatchProgress({ hatching: true, maxSecs: 0, hatchUpdate: 100 }, NOW), null)

// —— 核心用例:从未有过进度采样(hatchUpdate=0)——
// 必须返回 null(进度未知),绝不能外推成 100%。
const fresh = { gid: 4405, hatching: true, maxSecs: 3600, hatchedSecs: 0, hatchUpdate: 0 }
assert.equal(hatchProgress(fresh, NOW), null, '无采样时必须返回 null,不能外推成 100%')

// 有采样:正常外推
const sampled = { gid: 4406, hatching: true, maxSecs: 3600, hatchedSecs: 600, hatchUpdate: NOW / 1000 - 100 }
const p = hatchProgress(sampled, NOW)
assert.ok(p && p.pct > 0 && p.pct < 100, `有采样应给出中间进度,实际 ${JSON.stringify(p)}`)
// 1 倍速下 100 秒应推进约 100 秒(600 → 700)
assert.ok(Math.abs(p.secs - 700) <= 2, `应外推到约 700 秒,实际 ${p.secs}`)

// 进度已满:仍要如实返回 100(由调用方决定是否隐藏,不是这里假装没满)
const done = { gid: 4407, hatching: true, maxSecs: 3600, hatchedSecs: 3600, hatchUpdate: NOW / 1000 }
assert.equal(hatchProgress(done, NOW).pct, 100)

// 时钟回拨(hatchUpdate 晚于 now)不应产生负数进度
const skew = { gid: 4408, hatching: true, maxSecs: 3600, hatchedSecs: 600, hatchUpdate: NOW / 1000 + 999 }
assert.ok(hatchProgress(skew, NOW).secs >= 600, '时钟回拨不该把进度往回推')

console.log('hatch.test.mjs: 全部通过')
