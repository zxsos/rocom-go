// 验收培育页「离目标差距」的口径(src/pages/breeding/pets.js):
//   1. 达标是**方向化**的:目标偏高(嗓音 > 0、体重 > 50%)时「≥ 目标」算到、偏低时「≤ 目标」
//      算到、正中间精确相等。子代 97 对目标 96 是达标,不再显示「差 1」
//   2. 体重**没有** ±2pp 容差:目标 98 只认 ≥98(97.5 不算)
//   3. 未达标才说差多少,且**不区分**超出 / 不足(只有「够到」与「差多少」两态)
//   4. 性格维度有形目:命中 → 达标,未命中 → 未命中
//   5. bestChild 选出「同一只」里命中项最多、距离最近的一代 —— 与「各维最佳」是两回事
//
//   node scripts/verify-breeding-goal-progress.mjs
//
// 为什么值得单独验:这一层是**纯映射**(必须与后端 internal/pet/breeding.go 的 reached 逐字
// 一致),写错了页面照样渲染、后端照样算分,没有报错 —— 只有把具体数值喂进去才能发现
// 「重了 1.6」被写成「差 1.6」、或方向判反导致「页面写达标、状态不翻」这类静默误导。
// 第 1 组用例里的 97 vs 96、99.6 vs 98 就来自报告中的真实数据。

import { readFileSync } from 'node:fs'
import { createServer } from 'vite'

const GOLDEN = new URL('../../internal/server/testdata/contract/name-options.json', import.meta.url)
const matrix = JSON.parse(readFileSync(GOLDEN, 'utf8')).nature

const server = await createServer({ root: process.cwd(), logLevel: 'error', server: { middlewareMode: true, hmr: false } })
const pets = await server.ssrLoadModule('/src/pages/breeding/pets.js')

const results = []
const check = (name, cond, detail = '') => {
  results.push({ name, ok: cond, detail })
  console.log(`${cond ? '✅' : '❌'} ${name}${detail ? '  ' + detail : ''}`)
}

// —— 0. 方向化判定(goalReached)——
const gr = pets.goalReached
check('高嗓音:97 ≥ 96 → 达标', gr(97, 96, 'voice') === true)
check('高嗓音:96 = 96 → 达标', gr(96, 96, 'voice') === true)
check('高嗓音:95 < 96 → 不达标', gr(95, 96, 'voice') === false)
check('低嗓音:-100 ≤ -96 → 达标', gr(-100, -96, 'voice') === true)
check('低嗓音:-95 > -96 → 不达标', gr(-95, -96, 'voice') === false)
check('中心嗓音:精确相等才算', gr(0, 0, 'voice') === true && gr(1, 0, 'voice') === false)
check('高体重:99.6 ≥ 98 → 达标', gr(99.6, 98, 'weight') === true)
check('去容差:97.5 < 98 → 不达标(旧 ±2pp 已取消)', gr(97.5, 98, 'weight') === false)
check('去容差:96 < 98 → 不达标', gr(96, 98, 'weight') === false)
check('低体重:5 ≤ 10 → 达标', gr(5, 10, 'weight') === true)
check('低体重:12 > 10 → 不达标', gr(12, 10, 'weight') === false)
check('轴:嗓音 -100~100、体重 0~100',
  pets.GOAL_AXES.voice.lo === -100 && pets.GOAL_AXES.voice.hi === 100
  && pets.GOAL_AXES.weight.lo === 0 && pets.GOAL_AXES.weight.hi === 100,
  JSON.stringify(pets.GOAL_AXES))

// —— 1. 单维文案(goalItem)——
const gi = pets.goalItem
check('嗓音 97 vs 96 → 达标(不再写差 1)',
  gi(97, 96, 'voice').hit === true && gi(97, 96, 'voice').text.includes('达标'), gi(97, 96, 'voice').text)
check('嗓音 95 vs 96 → 差 1',
  gi(95, 96, 'voice').hit === false && gi(95, 96, 'voice').text.includes('差 1'), gi(95, 96, 'voice').text)
check('体重 99.6 vs 98 → 达标(不再写差 1.6)',
  gi(99.6, 98, 'weight').hit === true && gi(99.6, 98, 'weight').text.includes('达标'), gi(99.6, 98, 'weight').text)
check('体重 97.5 vs 98 → 差 0.5pp',
  gi(97.5, 98, 'weight').hit === false && gi(97.5, 98, 'weight').text.includes('差 0.5pp'), gi(97.5, 98, 'weight').text)
check('低嗓音 -100 vs -96 → 达标', gi(-100, -96, 'voice').hit === true, gi(-100, -96, 'voice').text)
check('没填目标 / 值未知 → null', gi(96, null, 'voice') === null && gi(null, 96, 'voice') === null)
// 不区分超出 / 不足:同一个距离的两侧文案只该差数值,不含方向词
const overTxt = gi(100.5, 98, 'weight').text
const underTxt = gi(95.5, 98, 'weight').text
check('不出现「超出 / 高 / 低」方向词',
  !['超', '高', '低'].some((w) => overTxt.includes(w) || underTxt.includes(w)), `${overTxt} | ${underTxt}`)

// —— 2. 整条线的进度行(goalProgress)——
// 目标 V96 / W98 / 固执:1 代 V96 / W99.6 / 固执
const line = {
  goal: { voice: 96, weightPct: 98, nature: '固执' },
  gens: [{ gen: 1, child: { voice: 96, weightPct: 99.6, nature: '固执', name: '子' } }],
}
const bars = pets.goalProgress(line, matrix)
check('三项目标 → 三行进度(含性格)', bars.length === 3, bars.map((b) => b.k).join(''))
check('V 行达标', (bars.find((b) => b.k === 'V') || {}).hit === true, (bars.find((b) => b.k === 'V') || {}).text)
check('W 行达标(不再是差 1.6)', (bars.find((b) => b.k === 'W') || {}).hit === true, (bars.find((b) => b.k === 'W') || {}).text)
const natBar = bars.find((b) => b.k === '性')
check('性 行出现且达标', !!natBar && natBar.hit === true, natBar && natBar.text)
// 报告里的另一类线:目标 96、子代 97 → 现在也算达标
const up = pets.goalProgress({ goal: { voice: 96 }, gens: [{ gen: 1, child: { voice: 97, weightPct: null, nature: '' } }] }, matrix)
check('子代 97 对目标 96 → V 行达标', up[0].hit === true, up[0].text)
// 只填嗓音 → 只有一行(没填的维度不参与,与后端「只对填了的项计分」同口径)
check('只填嗓音 → 一行', pets.goalProgress({ goal: { voice: 96 }, gens: line.gens }, matrix).length === 1)

// 性格未命中:线卡在「进行中」的真实原因,必须显示出来
const missLine = { goal: { nature: '固执' }, gens: [{ gen: 1, child: { voice: 0, weightPct: null, nature: '开朗' } }] }
const missBar = pets.goalProgress(missLine, matrix).find((b) => b.k === '性')
check('性格未命中 → 未命中', !!missBar && missBar.hit === false && missBar.text === '未命中', missBar && missBar.text)
check('natureHit:命中为 true', pets.natureHit(line, line.goal) === true)
check('natureHit:未命中为 false', pets.natureHit(missLine, missLine.goal) === false)

// —— 3. 同一只达标(bestChild)——
const twoGens = {
  goal: { voice: 96, weightPct: 98 },
  gens: [
    { gen: 1, child: { voice: 96, weightPct: 90, nature: '固执' } },
    { gen: 2, child: { voice: 90, weightPct: 98, nature: '固执' } },
  ],
}
const bc = pets.bestChild(twoGens)
check('bestChild 只挑同一只(最多命中 1 项)', !!bc && bc.miss === 1, bc && `第 ${bc.gen} 代 miss=${bc.miss}`)
const both = {
  goal: { voice: 96, weightPct: 98 },
  gens: [
    { gen: 1, child: { voice: 96, weightPct: 90, nature: '' } },
    { gen: 2, child: { voice: 96, weightPct: 99, nature: '' } },
  ],
}
check('bestChild 全项命中时 miss=0', (pets.bestChild(both) || {}).miss === 0, String((pets.bestChild(both) || {}).miss))
check('没填目标 → bestChild 为 null', pets.bestChild({ goal: {}, gens: line.gens }) === null)
check('没有子代 → bestChild 为 null', pets.bestChild({ goal: { voice: 1 }, gens: [{ gen: 1 }] }) === null)

// 契约样本那条线(目标 V96/W98/加物攻 + 子代 V88/W0/固执):应挑中第 1 代且还差 2 项
const contractLine = {
  goal: { voice: 96, weightPct: 98, nature: '固执', natureIn: ['逞强', '固执', '大胆', '调皮', '勇敢'] },
  gens: [{ gen: 1, child: { voice: 88, weightPct: 0, nature: '固执' } }],
}
const cb = pets.bestChild(contractLine)
check('契约样本:性格命中、V/W 未命中 → 还差 2 项', !!cb && cb.miss === 2 && cb.hits.nature === true && cb.hits.voice === false,
  cb && `miss=${cb.miss} nature=${cb.hits.nature}`)

// 「历代各维最佳」达标优先:已达标的 100 不该被只差 1 却没达标的 95 挤掉(与后端 LineStats 同口径)
const best = pets.lineStats({
  goal: { voice: 96 },
  gens: [
    { gen: 1, child: { voice: 95, weightPct: null, nature: '' } },
    { gen: 2, child: { voice: 100, weightPct: null, nature: '' } },
  ],
})
check('历代最佳达标优先:取 100 而非更近的 95', best.bestVoice === 100, String(best.bestVoice))

await server.close()
const bad = results.filter((r) => !r.ok)
console.log(`\n${results.length - bad.length}/${results.length} 项通过`)
process.exit(bad.length ? 1 : 0)
