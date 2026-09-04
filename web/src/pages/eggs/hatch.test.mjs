// hatchProgress 的回归测试(纯 Node 跑:node web/src/pages/eggs/hatch.test.mjs)。
//
// 为什么要有它:孵化进度是**本地外推**的,而外推的输入(hatchUpdate/hatchedSecs)
// 可能压根不存在 —— 登录包 0x0102 只给「哪些蛋在孵」,逐蛋进度要等开孵蛋器 0x0312
// 或开背包 0x1344 才下发。早先 hatchProgress 对 hatchUpdate=0 也照常外推,算出
// 「已过十几亿秒」→ 进度顶满 → EggList 把「在孵且进度满」的蛋两栏都不显示,
// 蛋凭空消失 —— 玩家看到的就是「登录后孵蛋器空的 / 没刷新」。
import assert from 'node:assert/strict'
import { hatchProgress, clampRate, gatherRates } from './hatch.js'

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

// —— 实测回归:2026-09 三份 pcap 的 15 个采样点 ——
//
// 这是本次最重要的断言,守三条由实测得出的结论:
//   1. 差分法(主口径)在全部 6 个差分点上精确 5.00;
//   2. 倍率是**全局**的:同一时刻不同 maxSecs 的蛋增速一致(故可跨蛋聚合);
//   3. 单点法会因玩家跑动高估到 8~9 倍 —— 故**不得**用它兜底。
// 数据来自 PCAPdroid_04_9月_17_33_10 / 05_9月_01_23_51 / 05_9月_01_26_10。

// 蛋 4475:刚入孵(23:51 那份),(hatchUpdate, hatchedSecs)
const E4475_A = [
  { t: 1788542707, v: 50 }, { t: 1788542717, v: 100 },
  { t: 1788542721, v: 120 }, { t: 1788542725, v: 140 },
]
// 蛋 4475 在另一份(26:10),已孵较久
const E4475_B = [{ t: 1788542818, v: 980 }, { t: 1788542820, v: 990 }]
const E4473 = [{ t: 1788542818, v: 860 }, { t: 1788542820, v: 870 }]
const E4281 = [{ t: 1788542818, v: 840 }, { t: 1788542820, v: 850 }] // 16h 蛋
// 蛋 4474 / 4486(04_9月_17_33_10,刚入孵)
const E4474 = [
  { t: 1788514551, v: 25 }, { t: 1788514552, v: 30 }, { t: 1788514562, v: 80 },
  { t: 1788514565, v: 95 }, { t: 1788514567, v: 105 },
]
const E4486 = [
  { t: 1788514552, v: 5 }, { t: 1788514562, v: 55 },
  { t: 1788514565, v: 70 }, { t: 1788514567, v: 80 },
]

// [1] 差分法:全部相邻采样对必须精确 5.00
//
// 六组共 19 个采样点(4+2+2+2+5+4),相邻对 = 19 − 6 = **13**。
// 显式核对总数:少算会让断言漏测(假绿)。
{
  let n = 0
  for (const [gid, pts] of [[4475, E4475_A], [4475, E4475_B], [4473, E4473],
    [4281, E4281], [4474, E4474], [4486, E4486]]) {
    for (let i = 1; i < pts.length; i++) {
      const dt = pts[i].t - pts[i - 1].t
      const dv = pts[i].v - pts[i - 1].v
      assert.ok(Math.abs(dv / dt - 5) < 1e-9,
        `蛋 ${gid} 差分应精确 5.00,实际 ${(dv / dt).toFixed(3)} (dt=${dt} dv=${dv})`)
      n++
    }
  }
  assert.equal(n, 13, `应有 13 个差分采样对,实际 ${n}`)
}

// [2] 倍率全局:同一时刻不同 maxSecs 的三颗蛋增速一致
{
  const r = (p) => (p[1].v - p[0].v) / (p[1].t - p[0].t)
  const a = r(E4475_B), b = r(E4473), c = r(E4281)
  assert.ok(Math.abs(a - b) < 1e-9 && Math.abs(b - c) < 1e-9,
    `同刻三蛋倍率应一致(8h/8h/16h),实际 ${a}/${b}/${c}`)
}

// [3] 单点法会高估 —— 这是「不得用它兜底」的实证,钉住免得将来有人改回去
{
  const st = 1788542697 // 蛋 4475 的 start_hatch_time
  const early = E4475_A[0].v / (E4475_A[0].t - st)      // 刚放入
  const later = E4475_B[0].v / (E4475_B[0].t - st)      // 跑动过之后
  assert.ok(Math.abs(early - 5) < 1e-9, `刚放入时单点应为 5.00,实际 ${early}`)
  assert.ok(later > 8 && later < 9.1,
    `跑动后单点应高估到 8~9 倍(故不可用),实际 ${later.toFixed(2)}`)
}

// [4] hatchRate 走差分并钳制:喂入实测序列,应得到 5 且被钳进 [1,20]
{
  const seq = [
    { gid: 9001, hatching: true, maxSecs: 28800, hatchedSecs: 50, hatchUpdate: 1788542707 },
    { gid: 9001, hatching: true, maxSecs: 28800, hatchedSecs: 100, hatchUpdate: 1788542717 },
  ]
  hatchProgress(seq[0], 1788542707 * 1000)  // 第一次采样(无前值 → 1 倍)
  const p2 = hatchProgress(seq[1], 1788542717 * 1000)
  assert.ok(p2, '第二次采样应给出进度')
  // 第二次采样后倍率已收敛到 5,此刻 elapsed=0,进度 = 100
  const p3 = hatchProgress(seq[1], 1788542717 * 1000 + 10 * 1000)
  assert.ok(Math.abs(p3.secs - 150) < 1e-6,
    `按 5 倍外推 10 秒应到 150s(100+50),实际 ${p3.secs}`)
}

// [6] gatherRates:跨蛋聚合(倍率全局统一,故可用中位数抗单蛋抖动)
{
  // 先让每颗蛋各喂一次采样,建立 prev
  for (const [gid, p] of [[9101, E4475_A[0]], [9102, E4473[0]], [9103, E4281[0]]]) {
    hatchProgress({ gid, hatching: true, maxSecs: 28800, hatchedSecs: p.v, hatchUpdate: p.t }, p.t * 1000)
  }
  // 第二次采样:三颗蛋同刻各 +10s(实测形态)
  const eggs = [
    { gid: 9101, hatching: true, maxSecs: 28800, hatchedSecs: E4475_B[1].v, hatchUpdate: E4475_B[1].t },
    { gid: 9102, hatching: true, maxSecs: 28800, hatchedSecs: E4473[1].v, hatchUpdate: E4473[1].t },
    { gid: 9103, hatching: true, maxSecs: 57600, hatchedSecs: E4281[1].v, hatchUpdate: E4281[1].t },
  ]
  const r = gatherRates(eggs)
  assert.ok(Math.abs(r - 5) < 1e-9, `跨蛋中位数倍率应为 5,实际 ${r}`)
  assert.equal(gatherRates([]), null, '无可用采样时应返回 null')
  assert.equal(gatherRates(null), null, '入参为 null 时应返回 null')
}

// [7] clampRate 边界
{
  assert.equal(clampRate(0.5), 1, '低于 1 应钳到 1')
  assert.equal(clampRate(-3), 1, '负值应钳到 1')
  assert.equal(clampRate(NaN), 1, 'NaN 应钳到 1')
  assert.equal(clampRate(Infinity), 20, '无穷大应钳到 20')
  assert.equal(clampRate(999), 20, '过大应钳到 20')
  assert.equal(clampRate(5), 5, '区间内的值原样返回')
  assert.equal(clampRate(1), 1)
  assert.equal(clampRate(20), 20)
}

// [5] 钳制:异常倍率不得吹飞进度(端到端,验证 hatchRate 真的调了 clampRate)
//
// ⚠️ 用例必须能**区分**钳制与否,否则是死断言。初版用 hatchedSecs:999999 构造
// 超大倍率,结果它本身就先被 maxSecs 钳住 —— 钳制与否都得到 maxSecs,断言恒真
// (变异测试:去掉 clampRate 后照样全绿)。
//
// 正确构造:让两次采样的差分大得异常、但 v2 本身仍远小于 maxSecs,
// 这样「外推增量」才是唯一变量:
//   差分 1000s/10s = 100 倍(异常),钳制后应为 20 → 外推 100s 只推进 2000s。
{
  const s1 = { gid: 9002, hatching: true, maxSecs: 28800, hatchedSecs: 0, hatchUpdate: 1000 }
  const s2 = { gid: 9002, hatching: true, maxSecs: 28800, hatchedSecs: 1000, hatchUpdate: 1010 }
  hatchProgress(s1, 1000 * 1000)
  const p = hatchProgress(s2, 1110 * 1000) // elapsed = 100s
  const want = 1000 + 100 * 20 // 倍率钳到 20
  assert.ok(Math.abs(p.secs - want) < 1e-6,
    `倍率应被钳到 20 → 外推 100s 得 ${want}s,实际 ${p.secs}s(未钳制会是 11000)`)

  // 下限同理:差分 1s/10s = 0.1 倍(异常慢),钳制后应为 1
  const t1 = { gid: 9003, hatching: true, maxSecs: 28800, hatchedSecs: 1000, hatchUpdate: 2000 }
  const t2 = { gid: 9003, hatching: true, maxSecs: 28800, hatchedSecs: 1001, hatchUpdate: 2010 }
  hatchProgress(t1, 2000 * 1000)
  const q = hatchProgress(t2, 2110 * 1000)
  const want2 = 1001 + 100 * 1 // 倍率钳到 1
  assert.ok(Math.abs(q.secs - want2) < 1e-6,
    `倍率应被钳到 1 → 外推 100s 得 ${want2}s,实际 ${q.secs}s(未钳制会是 1011)`)
}

console.log('hatch.test.mjs: 全部通过')
