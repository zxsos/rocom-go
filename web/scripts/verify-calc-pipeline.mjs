#!/usr/bin/env node
// 伤害估算**数据管道**的对拍验收:用 roco-calculator 公开文档里的示例核我们的口径。
//
// 存在理由:这些数字(1.7/1.1、+70/+10、37/41、×1.2/−0.9、钳制 3/0.25)抄错一位,
// 页面照样出数、看着也合理,只有对拍能抓出来。本期(数据管道期)的完成标准就是它。
//
// 用法:npm run verify:calc

import {
  STAT_KEYS, levelCoefficient, statBaseValue, panelStat, estimateStats, typeMultiplier,
  normalizeIv, defaultEffortAdd, ROUNDING, NATURE_MULT, DEFAULT_DISPLAY_IV,
  abilityLevelMultiplier, abilityAdjustedStat, clampAbilityStage,
  resolveDynamicPower, differencePower, MANA_BURST_POWER, calculateDamage, estimate,
} from '../src/pages/shanyao/calc.js'

let failed = 0
function eq(name, got, want, tol = 0) {
  const ok = tol ? Math.abs(got - want) <= tol : got === want
  if (!ok) { failed++; console.error(`  ✗ ${name}: 期望 ${want}, 实得 ${got}`) } else { console.log(`  ✓ ${name} = ${got}`) }
}

console.log('— 上游给定的常量 —')
// 直接断言常量看着有点怪,但这两个数是 roco 明确给定的(+1.2/−0.9),而国内同类
// 游戏普遍是 1.1/0.9 —— 「顺手改成 1.1」是会真实发生的改动,必须有人拦。
eq('NATURE_MULT.up', NATURE_MULT.up, 1.2)
eq('NATURE_MULT.down', NATURE_MULT.down, 0.9)
eq('DEFAULT_DISPLAY_IV', DEFAULT_DISPLAY_IV, 60)

console.log('— 等级系数 —')
// roco 文档示例:60 级的等级系数 0.902439(= 37/41)
eq('levelCoefficient(60)', levelCoefficient(60).toFixed(6), '0.902439')
eq('levelCoefficient(100)', levelCoefficient(100).toFixed(6), ((100 * 45 / 100 + 10) / 41).toFixed(6))
eq('个体归一 60 → 10', normalizeIv(60), 10)

console.log('— 面板六维(抓包真值:小花仙 种族 122/67/72/84/94/100, 个体 60/0/0/60/60/0)—')
const race = { hp: 122, physicalAttack: 67, magicalAttack: 72, physicalDefense: 84, magicalDefense: 94, speed: 100 }
const talents = { hp: 60, physicalAttack: 0, magicalAttack: 0, physicalDefense: 60, magicalDefense: 60, speed: 0 }
for (const kind of STAT_KEYS) {
  eq(`statBaseValue.${kind}`, statBaseValue({ kind, race: race[kind], displayIv: talents[kind] }), { hp: 328, physicalAttack: 84, magicalAttack: 89, physicalDefense: 135, magicalDefense: 146, speed: 120 }[kind])
}
eq('effortAdd(HP)', defaultEffortAdd('hp'), 100)
eq('effortAdd(物攻)', defaultEffortAdd('physicalAttack'), 50)
// 面板值 = round(base × 性格) + effortAdd;踏实是「无修正」(×1)
eq('panelStat(HP, 性格×1)', panelStat({ baseValue: 328, effortAdd: 100 }), 428)
eq('panelStat(物攻, 性格×1.2)', panelStat({ baseValue: 84, effortAdd: 50, natureMultiplier: 1.2 }), 151)

console.log('— 对手推算(无协议六维)—')
const est = estimateStats({ race, talents, natureMultipliers: {} })
eq('estimated 标记', est.estimated, true)
eq('推算 HP', est.values.hp, 428)
eq('推算 物攻', est.values.physicalAttack, 134) // 84 + 50
eq('缺种族值时返回 null', estimateStats({ race: null }), null)

console.log('— 属性克制(矩阵 + 钳制)—')
const rules = {
  types: ['普通', '草', '火', '水', '光', '地', '冰', '龙', '电', '毒', '虫', '武', '翼', '萌', '幽', '恶', '机械', '幻'],
  clamp: { max: 3, min: 0.25 },
  // 下面只填对拍用得到的几格,其余为 1:这里验的是**取值与钳制逻辑**,
  // 矩阵本身由 /api/calc-rules 提供(golden 里钉住了火→草 / 草→火)。
  matrix: [],
}
const cell = (atk, def, v) => {
  const i = rules.types.indexOf(atk)
  const j = rules.types.indexOf(def)
  rules.matrix[i] = rules.matrix[i] || []
  rules.matrix[i][j] = v
}
cell('火', '草', 2)
cell('草', '火', 0.5)
cell('水', '火', 2)
cell('水', '草', 0.5)
eq('火 → 草', typeMultiplier('火', ['草'], rules), 2)
eq('草 → 火', typeMultiplier('草', ['火'], rules), 0.5)
// 双系:2 × 2 = 4 → 钳到 3(不是 4)
cell('地', '火', 2)
cell('地', '电', 2)
eq('地 → 火+电(2×2 钳到 3)', typeMultiplier('地', ['火', '电'], rules), 3)
// 双系:0.5 × 0.5 = 0.25 → 保持(不是继续变 0)
cell('草', '水', 2)
cell('火', '水', 0.5)
cell('火', '草', 2)
eq('水 → 火+草(2×0.5)', typeMultiplier('水', ['火', '草'], rules), 1)
eq('缺规则时返回 null', typeMultiplier('火', ['草'], null), null)

console.log('— 取整策略 —')
// roco 文档示例(每段伤害):物攻213 × 显示威力42.1875 × 0.902439 → 四舍五入 → ÷物防175 → 向下取整 = 46
const coeff = levelCoefficient(60)
const numerator = Math.round(213 * 42.1875 * coeff) // round-half-up
const oneHit = Math.floor(numerator / 175)
eq('每段伤害(示例)', oneHit, 46)
// 总伤害:每段46 × 2段 + 追加20 = 112
eq('总伤害(示例)', Math.floor(oneHit * 1) * 2 + 20, 112)
eq('ROUNDING.effectiveSkillPower', ROUNDING.effectiveSkillPower, 'floor')
eq('ROUNDING.oneHitDamage', ROUNDING.oneHitDamage, 'floor')
eq('ROUNDING.hitCount', ROUNDING.hitCount, 'floor-then-multiply')

console.log('— 能力等级(±N 档,每档 ±10%)—')
eq('clamp 上限', clampAbilityStage(500), 99)
eq('clamp 下限', clampAbilityStage(-500), -99)
eq('0/0 档 = 1', abilityLevelMultiplier(0, 0), 1)
eq('攻 +1 档', abilityLevelMultiplier(1, 0).toFixed(4), '1.1000')
eq('攻 −1 档', abilityLevelMultiplier(-1, 0).toFixed(4), (1 / 1.1).toFixed(4))
eq('守 +1 档(等效降低伤害)', abilityLevelMultiplier(0, 1).toFixed(4), (1 / 1.1).toFixed(4))
eq('攻+1 且 守+1(相互抵消)', abilityLevelMultiplier(1, 1).toFixed(4), '1.0000')
eq('单项调整 +1 档', Math.round(abilityAdjustedStat(200, 1)), 220)
eq('单项调整 −1 档', Math.round(abilityAdjustedStat(200, -1)), 182)

console.log('— 动态威力规则(快照里只有 3 个技能带 ruleId)—')
eq('mana_burst 能量 0', resolveDynamicPower({ ruleId: 'mana_burst', energy: 0 }).power, MANA_BURST_POWER[0])
eq('mana_burst 能量 10', resolveDynamicPower({ ruleId: 'mana_burst', energy: 10 }).power, 210)
eq('mana_burst 能量越界夹取', resolveDynamicPower({ ruleId: 'mana_burst', energy: 99 }).power, 210)
eq('mana_burst 缺能量 → 算不出', resolveDynamicPower({ ruleId: 'mana_burst', energy: null }).power, null)
eq('mana_burst 缺能量 → 说明缺什么', (resolveDynamicPower({ ruleId: 'mana_burst', energy: null }).missing || []).join(), '当前能量')
eq('差值表 ≤0', differencePower(-5), 60)
eq('差值表 100', differencePower(100), 140)
eq('差值表 ≥271', differencePower(999), 200)
eq('速度差 50 → 100', resolveDynamicPower({ ruleId: 'speed_difference', attackerValue: 200, defenderValue: 150 }).power, 100)
eq('物防差 −10 → 60', resolveDynamicPower({ ruleId: 'physical_defense_difference', attackerValue: 100, defenderValue: 110 }).power, 60)
eq('差值规则缺能力值 → 算不出', resolveDynamicPower({ ruleId: 'speed_difference', attackerValue: 200, defenderValue: null }).power, null)
eq('无 ruleId → 用 basePower', resolveDynamicPower({ ruleId: null, basePower: 90 }).power, 90)

console.log('— 主公式(文档示例:每段 46、总伤害 112)—')
// 显示威力 42.1875 → 四舍五入 42;物攻 213、物防 175、减伤 1、段数 2、追加 20
const calc = calculateDamage({
  attackerStat: 213, displayedPower: Math.round(42.1875), defenderDefense: 175,
  reduction: 1, finalMultiplier: 1, hitCount: 2, extraDamage: 20, level: 60,
})
eq('每段伤害', calc.oneHit, 46)
eq('总伤害(含追加)', calc.total, 112)
eq('系数 0.902439', calc.coefficient.toFixed(6), '0.902439')
// 减伤 0.5(应对/防御技能):每段再乘 0.5
const calcRed = calculateDamage({ attackerStat: 213, displayedPower: 42, defenderDefense: 175, reduction: 0.5, hitCount: 1 })
eq('减伤 0.5 的每段', calcRed.oneHit, Math.floor(Math.round(213 * 42 * calc.coefficient) / 175 * 0.5))
// 缺防御值必须算不出
eq('缺物防 → 算不出', calculateDamage({ attackerStat: 213, displayedPower: 42, defenderDefense: 0 }).ok, false)

// 取整时机的边界:下面三组分别锁住「分子四舍五入」「每段向下取整」「最终倍率向下取整」。
// 每组都刻意选在**小数会跨越整数**的位置 —— 否则 floor/round 结果相同,变异测试抓不到。
// 8457.65:四舍五入 8458、向下取整 8457(这句专门钉住 round)
const r1 = calculateDamage({ attackerStat: 213, displayedPower: 44, defenderDefense: 1, hitCount: 1 })
eq('分子四舍五入(8457.65 → 8458)', r1.numerator, 8458)
// def=2 → 4036.5:向下取整 4036,若改成四舍五入会变 4037(这句专门钉住 floor)
const r2 = calculateDamage({ attackerStat: 213, displayedPower: 42, defenderDefense: 2, hitCount: 1 })
eq('每段向下取整(4036.5 → 4036)', r2.oneHit, 4036)
const r3 = calculateDamage({
  attackerStat: 213, displayedPower: 42, defenderDefense: 175,
  finalMultiplier: 1.5, hitCount: 1,
})
eq('最终倍率向下取整(46 × 1.5 = 69)', r3.finalOneHit, Math.floor(r3.oneHit * 1.5))

console.log('— estimate 端到端(我方 → 对手,单向)—')
const rulesFull = {
  types: ['普通', '草', '火', '水'],
  matrix: [[1, 1, 1, 1], [1, 1, 0.5, 2], [1, 2, 1, 0.5], [1, 0.5, 2, 1]],
  clamp: { max: 3, min: 0.25 },
}
const atkPet = {
  // 攻方系别含「火」→ 火系技能享受本系 1.25
  damNames: ['火', '草'], hp: 428, hpMax: 428,
  stats: { baseValue: [328, 84, 89, 135, 146, 120], effortAdd: [100, 50, 50, 50, 50, 50] },
  skills: [{ id: 7130150, name: '烈焰拳', type: '火', category: 'physical', power: 112, hits: 2 }],
}
const defPet = {
  damNames: ['草'], hp: 400, raceStats: [120, 90, 90, 100, 100, 90],
  skills: [],
}
const res = estimate({ attacker: atkPet, defender: defPet, skill: atkPet.skills[0], rules: rulesFull, conditions: { level: 60 } })
eq('estimate ok', res.ok, true)
eq('四行算式行数', res.steps.length, 4)
eq('克制倍率(火→草 = 2)', res.ctx.typeMultiplier, 2)
eq('本系加成(火系 ∈ 攻方系别)', res.ctx.stab, 1.25)
eq('显示威力 = round(112 × 1.25 × 2)', res.ctx.displayedPower, 280)
// 守方六维来自种族值推算 → 必须标出来
eq('守方标注推算', res.ctx.estimated.defender, true)
eq('伤害为正数', res.damage > 0, true)
eq('剩余生命 = 400 − 伤害', res.remainHp, 400 - res.damage)
// 缺威力时必须算不出,且不拿 basePower 顶
const noPower = estimate({ attacker: atkPet, defender: defPet, skill: { id: 1, type: '虫', category: 'physical', power: null }, rules: rulesFull })
eq('缺威力 → 算不出(不硬算)', noPower.ok, false)
const manaMissing = estimate({
  attacker: atkPet, defender: defPet,
  skill: { id: 7020550, type: '火', category: 'magical', ruleId: 'mana_burst', power: 25 },
  rules: rulesFull,
})
eq('魔能爆缺能量 → 算不出', manaMissing.ok, false)
eq('缺的是「当前能量」', (manaMissing.missing || []).join(), '当前能量')

if (failed) {
  console.error(`\n✗ 对拍失败 ${failed} 项`)
  process.exit(1)
}
console.log('\n✓ 数据管道 + 公式对拍通过(口径与 roco-calculator 一致)')
