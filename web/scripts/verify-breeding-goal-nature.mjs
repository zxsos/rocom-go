// 验收培育页「目标性格」的口径(src/pages/breeding/pets.js):
//   1. 「性格正面加某维」按方阵的**整行**展开 —— 行序必须等于后端 nature_effect 的维度编号
//   2. 一组名字 == 某一行 → 显示成「加物攻」;对不上时退化成「N 种性格」,不冒充某一维
//   3. parseGoal / goalToInput / sameGoal 对 natureIn 的往返(去重、空名、顺序无关)
//
// 为什么值得单独验:这一层是**纯映射**,写错了页面照样渲染、后端照样算分,只是「加物攻」
// 悄悄变成别的维度(或反过来,把一维存成另一维的 5 个名字)—— 没有任何报错,只有刷了几代
// 之后发现子代性格都不对。行序那条尤其只能拿**真实方阵**验(见下面读的 golden),
// 前端自己的 NATURE_DIMS 与它同源,拿它自证等于什么也没验。
//
//   node scripts/verify-breeding-goal-nature.mjs

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

// —— 1. 真实方阵的形状(前端按行取「正面加某维」,行序错了整件事就错了)——
check('方阵是 6×6', Array.isArray(matrix) && matrix.length === 6 && matrix.every((r) => r.length === 6))
check('每行 5 个非空性格(对角线为空)', matrix.every((r) => r.filter(Boolean).length === 5))
const all = matrix.flat().filter(Boolean)
check('30 个性格不重复', new Set(all).size === 30, `${all.length} 个 / 去重后 ${new Set(all).size}`)
// 物攻↑ 那一行的 5 个名字(取值来自真实方阵:固执 = +物攻 −魔攻)。**写死**在这里 ——
// 拿它当输入才能验出「行序 = 维度编号」。反过来从 dims 里按 label 反查一行、再断言它叫
// 「物攻」是同义反复(常量整体错位时两边一起错,照样全绿 —— 变异测试实测过)。
const ATK_ROW = ['逞强', '固执', '大胆', '调皮', '勇敢']
check('2. 方阵第 2 行就是物攻行', JSON.stringify(matrix[1].filter(Boolean)) === JSON.stringify(ATK_ROW),
  `matrix[1] = ${matrix[1].filter(Boolean).join('/')}`)
check('2. NATURE_DIMS[1] = 物攻', pets.NATURE_DIMS[1] === '物攻', pets.NATURE_DIMS.join('/'))

// —— 2. 「正面加某维」的展开与显示 ——
const dims = pets.natureDimRows(matrix)
check('natureDimRows 给 6 维', dims.length === 6, dims.map((r) => r.label).join('/'))
check('维度编号是 1..6', dims.every((r, i) => r.dim === i + 1))
check('每维 5 个名字', dims.every((r) => r.names.length === 5))
check('「物攻」这一维展开的就是那一行',
  JSON.stringify((dims.find((r) => r.label === '物攻') || {}).names) === JSON.stringify(ATK_ROW))

check('一整行 → 「加物攻」', pets.goalNatureLabel({ natureIn: ATK_ROW }, matrix) === '加物攻',
  pets.goalNatureLabel({ natureIn: ATK_ROW }, matrix))
check('确切一个性格 → 原样显示名字', pets.goalNatureLabel({ nature: '固执' }, matrix) === '固执')
check('非整行的集合 → 「N 种性格」',
  pets.goalNatureLabel({ natureIn: ['固执', '胆小'] }, matrix) === '2 种性格',
  pets.goalNatureLabel({ natureIn: ['固执', '胆小'] }, matrix))
// 并集(行外的 1 个 + 整行 5 个 = 6 个)覆盖不了任何一行(每行 5 个)→ 不该显示成「加某维」
check('并集凑不出整行时不说自己是某一维',
  pets.goalNatureLabel({ nature: '胆小', natureIn: ATK_ROW }, matrix) === '6 种性格',
  pets.goalNatureLabel({ nature: '胆小', natureIn: ATK_ROW }, matrix))
check('没填性格 → 空串', pets.goalNatureLabel({}, matrix) === '' && pets.goalNatureLabel({ nature: '' }, matrix) === '')
check('hasNatureGoal:一组名字算填了', pets.hasNatureGoal({ natureIn: ATK_ROW }) === true)
check('hasNatureGoal:空名不算', pets.hasNatureGoal({ nature: '', natureIn: ['', '  '] }) === false)
check('hasGoal 认 natureIn', pets.hasGoal({ natureIn: ATK_ROW }) === true)

// —— 3. 提交前的归一(parseGoal / goalToInput / sameGoal)——
const goal = { voice: 96, weightPct: 98, natureIn: ATK_ROW }
const back = pets.parseGoal(pets.goalToInput(goal))
check('往返:集合保序', JSON.stringify(back.natureIn) === JSON.stringify(ATK_ROW), JSON.stringify(back.natureIn))
check('往返:数值项不丢', back.voice === 96 && back.weightPct === 98)
check('空名与重复被丢掉',
  JSON.stringify(pets.parseGoal({ natureIn: ['固执', '', '固执', '  '] }).natureIn) === JSON.stringify(['固执']))
check('显式的空 natureIn 不写进目标', !('natureIn' in pets.parseGoal({ natureIn: [] })))
// 顺序无关:并集合并后的顺序与方阵行来的顺序未必相同,而它说的是同一件事
const shuffled = [ATK_ROW[2], ATK_ROW[0], ATK_ROW[4], ATK_ROW[1], ATK_ROW[3]]
check('sameGoal:同一集合的顺序不同算没改', pets.sameGoal({ ...goal, natureIn: shuffled }, goal) === true)
check('sameGoal:少一个名字算改了',
  pets.sameGoal({ natureIn: ATK_ROW.slice(1) }, goal) === false)
check('sameGoal:确切性格与它的一组同名名字等价',
  pets.sameGoal({ natureIn: ['固执'], nature: '固执' }, { nature: '固执' }) === true)

await server.close()
const bad = results.filter((r) => !r.ok)
console.log(`\n${results.length - bad.length}/${results.length} 项通过`)
process.exit(bad.length ? 1 : 0)
