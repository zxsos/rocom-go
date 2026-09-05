// hatchProgress 的回归测试(纯 Node 跑:node web/src/pages/eggs/hatch.test.mjs)。
//
// 本模块现在只做一件事:**按后端给的倍率外推进度**。倍率本身由后端按加速日时间表
// 算出(北京时间周五 04:00 ~ 周一 04:00 → 5 倍,否则 1 倍),前端不估 —— 早先那套
// 「对相邻两次进度下发做差分、取跨蛋中位数」已废弃,它把活动倍率(固定时间表)与
// 在线加成(移动/风场)混成一个数去测,结果样本被移动时段污染、离线那还算不了。
//
// 这里守住的是外推本身的三条:无采样不外推、进度不倒退、孵化秒与真实秒不混。
import assert from 'node:assert/strict'
import { hatchProgress, remainRealSecs, clampRate } from './hatch.js'

const NOW = 1788000000 * 1000 // 固定"当前时刻",避免依赖真实时钟

// 不在孵蛋器里 → null
assert.equal(hatchProgress({ hatching: false, maxSecs: 3600 }, NOW, 5), null)
// 缺 maxSecs(配置缺失)→ null,不崩
assert.equal(hatchProgress({ hatching: true, maxSecs: 0, hatchUpdate: 100 }, NOW, 5), null)

// —— 核心用例:从未有过进度采样(hatchUpdate=0)——
// 必须返回 null(进度未知),绝不能外推成 100%。
const fresh = { gid: 4405, hatching: true, maxSecs: 3600, hatchedSecs: 0, hatchUpdate: 0 }
assert.equal(hatchProgress(fresh, NOW, 5), null, '无采样时必须返回 null,不能外推成 100%')

// 有采样:1 倍速下 100 秒应推进约 100 秒(600 → 700)
const sampled = { gid: 4406, hatching: true, maxSecs: 3600, hatchedSecs: 600, hatchUpdate: NOW / 1000 - 100 }
const p1 = hatchProgress(sampled, NOW, 1)
assert.ok(p1 && p1.pct > 0 && p1.pct < 100, `有采样应给出中间进度,实际 ${JSON.stringify(p1)}`)
assert.ok(Math.abs(p1.secs - 700) <= 2, `1 倍下应外推到约 700 秒,实际 ${p1.secs}`)

// 5 倍速:同样 100 秒应推进 500 秒(600 → 1100)
const p5 = hatchProgress(sampled, NOW, 5)
assert.ok(Math.abs(p5.secs - 1100) <= 2, `5 倍下应外推到约 1100 秒,实际 ${p5.secs}`)
// 倍率要一并返回,否则 ETA 无从把孵化秒折算成真实秒
assert.equal(p5.rate, 5, '外推用的倍率要一并返回')

// 进度已满:仍要如实返回 100(由调用方决定是否隐藏,不是这里假装没满)
const done = { gid: 4407, hatching: true, maxSecs: 3600, hatchedSecs: 3600, hatchUpdate: NOW / 1000 }
assert.equal(hatchProgress(done, NOW, 5).pct, 100)

// 时钟回拨(hatchUpdate 晚于 now)不应产生负数进度
const skew = { gid: 4408, hatching: true, maxSecs: 3600, hatchedSecs: 600, hatchUpdate: NOW / 1000 + 999 }
assert.ok(hatchProgress(skew, NOW, 5).secs >= 600, '时钟回拨不该把进度往回推')

// —— 刚放进孵蛋器的蛋:必须有进度条 ——
//
// 后端对「进度为 0」的蛋会把 hatchUpdate 一起清零(见 store/egg.go 的 UpsertEggs),
// 而刚放进去的蛋 hatchedSecs 就是 0。若这里照 hatchUpdate=0 返回 null,页面上这颗蛋
// 就**连进度条都没有** —— 玩家刚放上蛋看不到 0%、也看不到预计时间,要手动重开一次
// 孵蛋器/背包(服务器重算进度)才出现。
//
// 但此刻进度是**确定的 0**,入孵时刻(startHatch)也是准确的,故从 startHatch 外推即可。
// 这与被实测否掉的「单点法」不是一回事:单点法是拿 v/elapsed **反推倍率**(跑动那段
// 被平摊进去,虚报成 8~9 倍);这里倍率是后端给的,只是从一个确定的零点起算。
{
  const T = NOW / 1000 - 120 // 两分钟前放进去的
  const fresh = { gid: 4409, hatching: true, maxSecs: 28800, hatchedSecs: 0, hatchUpdate: 0, startHatch: T }
  const p = hatchProgress(fresh, NOW, 5)
  assert.ok(p, '刚放入的蛋(进度 0、无采样时刻)也该有进度,不能返回 null')
  // 5 倍 × 120 秒 = 600 孵化秒
  assert.ok(Math.abs(p.secs - 600) < 2, `应从入孵时刻外推到约 600 秒,实际 ${p.secs}`)
  assert.equal(p.pct, 2, '8h 蛋孵了 600/28800 应约 2%')

  // 已取出的蛋 startHatch 是残留值,但 hatching 会被权威列表清成 false,
  // 故走不到这个分支(函数开头已返回 null)。这里守住它别被误用。
  const takenOut = { gid: 4410, hatching: false, maxSecs: 28800, hatchedSecs: 0, hatchUpdate: 0, startHatch: T }
  assert.equal(hatchProgress(takenOut, NOW, 5), null, '不在孵的蛋不得拿残留 startHatch 外推')
}

// —— 孵化秒 ≠ 真实秒:这是「加速日 ETA 报成 5 倍远」的病根 ——
//
// secs 是进度条的量纲(与 maxSecs 同尺子),要算「还要多久」必须除以倍率。
// 8h 蛋已孵 100 孵化秒 → 剩 28700 孵化秒;5 倍下真实只需 5740 秒,
// 漏了这一步就会说成 28700 秒(把完成时刻报成 5 倍远)。
{
  const egg = { gid: 4502, hatching: true, maxSecs: 28800, hatchedSecs: 100, hatchUpdate: NOW / 1000 }
  const p = hatchProgress(egg, NOW, 5)
  assert.ok(Math.abs(remainRealSecs(egg, p) - (28800 - 100) / 5) < 1e-6,
    `5 倍下应剩 ${(28800 - 100) / 5} 真实秒,实际 ${remainRealSecs(egg, p)}` +
    '(不除倍率会得 28700)')
  // 1 倍时两者相等 —— 这正是这个 bug 平时看不出来的原因
  const q = hatchProgress(egg, NOW, 1)
  assert.equal(remainRealSecs(egg, q), 28700, '1 倍时孵化秒 == 真实秒')
}

// 已孵满 / 参数缺失时剩余都得是 0,不能是 NaN 或负数
{
  const egg = { gid: 4601, hatching: true, maxSecs: 3600, hatchedSecs: 3600, hatchUpdate: NOW / 1000 }
  assert.equal(remainRealSecs(egg, hatchProgress(egg, NOW, 5)), 0, '已孵满应剩 0')
  assert.equal(remainRealSecs(null, null), 0, '无进度(null)应剩 0,不崩')
}

// clampRate 只作防御:后端异常数据不该让进度条拖死或吹飞
{
  assert.equal(clampRate(0), 1, '0 应钳到 1(字段缺失时别让进度停住)')
  assert.equal(clampRate(-3), 1, '负值应钳到 1')
  assert.equal(clampRate(NaN), 1, 'NaN 应钳到 1')
  assert.equal(clampRate(5), 5, '正常值原样返回')
  assert.equal(clampRate(1), 1)
  // 倍率缺省(未传 / undefined)按 1 处理,而不是 undefined 参与运算变成 NaN
  assert.equal(hatchProgress(sampled, NOW, undefined).rate, 1, '倍率缺省应按 1 倍')
  assert.equal(hatchProgress(sampled, NOW, 0).rate, 1, '倍率为 0 应按 1 倍')
}

console.log('hatch.test.mjs: 全部通过')
