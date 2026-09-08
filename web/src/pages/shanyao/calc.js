// 伤害估算的规则层:**常量 + 纯函数**,不含公式主体(下期接)。
//
// 口径全部对齐 roco-calculator(https://github.com/Evenstar-tools/roco-calculator):
//   - 面板六维:`round((isHP?1.7:1.1) × (race + 3×个体/6)) + (isHP?70:10)` 得 baseValue,
//     面板值 = `round(baseValue × 性格) + (isHP?100:50)`(协议里就是 effortAdd)。
//   - 等级系数:`(level×45/100 + 10) / 41`(60 级 = 0.902439)。
//   - 取整:见 ROUNDING。
// 这些数字看着像魔法数,改任何一个之前先跑 npm run verify:calc(对拍脚本)。

// 六维键的固定顺序 —— 与后端 /api/calc-rules 的 statKeys、pets[].stats 数组**同一套序**。
// 三处必须一致:后端 gamedata.CalcStatKeys()、/api/calc-rules、这里。
export const STAT_KEYS = ['hp', 'physicalAttack', 'magicalAttack', 'physicalDefense', 'magicalDefense', 'speed']

export const STAT_LABELS = {
  hp: '生命',
  physicalAttack: '物攻',
  magicalAttack: '魔攻',
  physicalDefense: '物防',
  magicalDefense: '魔防',
  speed: '速度',
}

// 默认个体(displayIv)。协议里的 talent=60 就是这个口径(roco 的 DEFAULT_DISPLAY_IV)。
export const DEFAULT_DISPLAY_IV = 60

// 性格系数:加成 +1.2、减成 −0.9(**不是** 1.1/0.9,别凭直觉改)。
export const NATURE_MULT = { up: 1.2, down: 0.9 }

// 取整策略(逐条对应 roco 的 DAMAGE_ROUNDING_POLICY)。
// 写在这里是为了让「哪一步取整、怎么取整」只有一个来源 —— 取整时机错了,
// 结果差 1~2 点,肉眼几乎看不出,只有对拍能发现。
export const ROUNDING = {
  effectiveSkillPower: 'floor',
  displayedPower: 'round-half-up',
  damageNumerator: 'round-half-up',
  oneHitDamage: 'floor',
  finalOneHitDamage: 'floor',
  hitCount: 'floor-then-multiply',
}

// 四行算式槽位(顺序即展示顺序,对齐 roco 的展示规范)。
export const STEP_LABELS = ['技能威力', '显示威力', '每段伤害', '总伤害']

export const NOT_IMPLEMENTED = 'not-implemented'

// 等级系数:60 级 = 37/41 ≈ 0.902439。
export function levelCoefficient(level = 60) {
  return (level * 45 / 100 + 10) / 41
}

// 个体归一:displayIv(0..100)/6。60 → 10。
export function normalizeIv(displayIv = DEFAULT_DISPLAY_IV) {
  const n = Number(displayIv)
  return Math.min(100, Math.max(0, Number.isFinite(n) ? n : 0)) / 6
}

// roundScaledStat:一般的四舍五入;个体为 0 且恰好 .5 时取偶(roco 的特殊处理,
// 目的是让「0 个体」这一档不因 .5 全部向上取整而整体偏高)。
function roundScaledStat(value, iv) {
  const floor = Math.floor(value)
  const tie = iv === 0 && Math.abs(value - floor - 0.5) < Number.EPSILON * Math.max(100, Math.abs(value))
  if (!tie) return Math.round(value)
  return floor % 2 === 0 ? floor + 1 : floor
}

// statBaseValue 由种族值与个体推出协议里的 baseValue。
//
// 已用抓包核对(小花仙:种族 122/67/72/84/94/100,个体 60/0/0/60/60/0):
//   HP 1.7×(122+30)=258.4→258, +70 = 328 ✓
//   物攻 1.1×(67+0)=73.7→74,  +10 = 84  ✓(个体为 0 时的取整也能对上)
export function statBaseValue({ kind, race, displayIv = DEFAULT_DISPLAY_IV }) {
  const iv = normalizeIv(displayIv)
  const isHp = kind === 'hp'
  const scaled = roundScaledStat((isHp ? 1.7 : 1.1) * (Number(race) + 3 * iv), iv)
  return scaled + (isHp ? 70 : 10)
}

// defaultEffortAdd 是努力值加成:HP +100、其余 +50(协议里的 effort_add)。
export const defaultEffortAdd = (kind) => (kind === 'hp' ? 100 : 50)

// panelStat 由 baseValue 得面板值:round(base × 性格) + effortAdd。
export function panelStat({ baseValue, effortAdd, natureMultiplier = 1 }) {
  return Math.round(Number(baseValue) * natureMultiplier) + Number(effortAdd)
}

// estimateStats 在**协议没给六维**时(对手)按种族值推算六维。
//
// 返回 { values, estimated: true };种族值缺失返回 null —— 缺数据就认,
// 拿一个默认值算出来的数字比「算不出」更害人。
export function estimateStats({ race, talents, natureMultipliers }) {
  if (!race || STAT_KEYS.some((k) => !Number.isFinite(Number(race[k])))) return null
  const values = {}
  for (const kind of STAT_KEYS) {
    const talent = talents?.[kind]
    const base = statBaseValue({ kind, race: race[kind], displayIv: Number.isFinite(Number(talent)) ? talent : DEFAULT_DISPLAY_IV })
    values[kind] = panelStat({
      baseValue: base,
      effortAdd: defaultEffortAdd(kind),
      natureMultiplier: natureMultipliers?.[kind] ?? 1,
    })
  }
  return { values, estimated: true }
}

// statsFromPayload 把 /api/shanyao 的宠物六维整理成一致形状,供 UI 与后续公式使用。
//
// 三种结果必须区分清楚(UI 要照此标注):
//   - source 'protocol':协议给的精确值(我方);
//   - source 'estimated':按静态种族值推算(对手);
//   - null:连种族值都没有,真的算不出。
export function statsFromPayload(pet) {
  if (!pet) return null
  // 性格系数:后端给的定序数组(与 STAT_KEYS 同序),缺则按无修正。
  const natures = {}
  if (Array.isArray(pet.natureMult)) STAT_KEYS.forEach((kind, i) => { natures[kind] = pet.natureMult[i] })

  if (pet.stats && Array.isArray(pet.stats.baseValue)) {
    const values = {}
    STAT_KEYS.forEach((kind, i) => {
      values[kind] = panelStat({
        baseValue: pet.stats.baseValue[i],
        effortAdd: pet.stats.effortAdd?.[i] ?? defaultEffortAdd(kind),
        natureMultiplier: natures[kind] ?? 1,
      })
    })
    return { values, estimated: false, source: 'protocol' }
  }
  if (Array.isArray(pet.raceStats) && pet.raceStats.length === STAT_KEYS.length) {
    const race = {}
    STAT_KEYS.forEach((kind, i) => { race[kind] = pet.raceStats[i] })
    const est = estimateStats({ race, natureMultipliers: natures })
    return est ? { ...est, source: 'estimated' } : null
  }
  return null
}

// typeMultiplier 计算属性克制倍率(攻方系别 → 守方 1~2 个系别)。
//
// 单系倍率取 matrix[攻][守];双系相乘后钳制:raw>=4 记 3、raw<=0.25 记 0.25
// (roco 的 getTypeMultiplier)。注意**不是**简单相乘。
// rules 缺数据时返回 null(UI 显示「缺数据」)。
export function typeMultiplier(attackType, defenderTypes, rules) {
  if (!rules || !Array.isArray(rules.types) || !Array.isArray(rules.matrix)) return null
  const types = Array.from(new Set((Array.isArray(defenderTypes) ? defenderTypes : [defenderTypes]).filter(Boolean)))
  const single = (atk, def) => {
    const i = rules.types.indexOf(atk)
    const j = rules.types.indexOf(def)
    const v = rules.matrix?.[i]?.[j]
    return Number.isFinite(v) && v >= 0 ? v : 1
  }
  const raw = types.reduce((m, d) => m * single(attackType, d), 1)
  if (raw === 0) return 0
  const max = rules.clamp?.max ?? 3
  const min = rules.clamp?.min ?? 0.25
  if (raw >= max) return max
  if (raw <= min) return min
  return raw
}

// ——— 能力等级(±N 档)———
// 每档 ±10%(roco 的 clampAbilityStage 夹在 ±99)。攻/守的档位合成**一个**系数,
// 而不是各自乘一次:正向加成进分子、负向加成进分母(roco 的 abilityLevelMultiplier)。
export const clampAbilityStage = (v) => Math.min(99, Math.max(-99, Math.floor(Number(v) || 0)))

export function abilityLevelMultiplier(attackStage = 0, defenseStage = 0) {
  const a = clampAbilityStage(attackStage) * 10
  const d = clampAbilityStage(defenseStage) * 10
  const numerator = 1 + Math.max(a, 0) / 100 + Math.max(-d, 0) / 100
  const denominator = 1 + Math.max(-a, 0) / 100 + Math.max(d, 0) / 100
  return numerator / denominator
}

// abilityAdjustedStat 把**单项**能力值按档位调整(速度差/物防差这类比较要用它)。
export function abilityAdjustedStat(value, stage = 0) {
  const pct = clampAbilityStage(stage) * 10
  return (Number(value) * (1 + Math.max(pct, 0) / 100)) / (1 + Math.max(-pct, 0) / 100)
}

// ——— 动态威力规则(快照里只有 3 个技能带 ruleId)———
// mana_burst(魔能爆):按**当前能量**查表,能量 0..10。
export const MANA_BURST_POWER = [45, 70, 90, 110, 135, 155, 165, 180, 190, 200, 210]

// DIFFERENCE_POWER_TABLE:速度差(闪击)与物防差(鸣沙陷阱)**共用**同一张表,
// 只是比较的字段不同。查的是「攻方值 − 守方值」落在哪一档。
export const DIFFERENCE_POWER_TABLE = [
  { min: -Infinity, max: 0, power: 60 },
  { min: 1, max: 30, power: 80 },
  { min: 31, max: 60, power: 100 },
  { min: 61, max: 90, power: 120 },
  { min: 91, max: 120, power: 140 },
  { min: 121, max: 150, power: 150 },
  { min: 151, max: 180, power: 160 },
  { min: 181, max: 210, power: 170 },
  { min: 211, max: 240, power: 180 },
  { min: 241, max: 270, power: 190 },
  { min: 271, max: Infinity, power: 200 },
]

export function differencePower(diff) {
  const row = DIFFERENCE_POWER_TABLE.find((r) => diff >= r.min && diff <= r.max)
  return row ? row.power : null
}

// hasNum:null / undefined / 空串一律算「没给」—— 直接 Number(v) 会把 null 当成 0,
// 而 0 在能量与能力值上都是合法值(能量耗尽、速度为 0),会被误当成有效输入。
const hasNum = (v) => v !== null && v !== undefined && v !== '' && Number.isFinite(Number(v))

// resolveDynamicPower 按 ruleId 重算威力。
//
// 返回 { power, expr, missing }:
//   - power 为 null 且 missing 非空 → **算不出**,调用方必须显示缺什么,不能拿 basePower 顶。
//   - 没有 ruleId → 直接用传入的 basePower(常规技能)。
export function resolveDynamicPower({ ruleId, basePower, energy, attackerValue, defenderValue }) {
  if (!ruleId) return { power: basePower ?? null, expr: null, missing: [] }
  if (ruleId === 'mana_burst') {
    if (!hasNum(energy)) {
      return { power: null, expr: null, missing: ['当前能量'] }
    }
    const e = Math.min(10, Math.max(0, Math.floor(Number(energy))))
    return { power: MANA_BURST_POWER[e], expr: `能量 ${e} → 威力 ${MANA_BURST_POWER[e]}`, missing: [] }
  }
  if (ruleId === 'speed_difference' || ruleId === 'physical_defense_difference') {
    if (!hasNum(attackerValue) || !hasNum(defenderValue)) {
      const what = ruleId === 'speed_difference' ? '双方速度' : '双方物防'
      return { power: null, expr: null, missing: [what] }
    }
    const diff = Number(attackerValue) - Number(defenderValue)
    const p = differencePower(diff)
    const label = ruleId === 'speed_difference' ? '速度差' : '物防差'
    return { power: p, expr: `${label} ${diff} → 威力 ${p}`, missing: [] }
  }
  // 未知规则:不做猜测,按常规威力处理并标出未支持。
  return { power: basePower ?? null, expr: null, missing: [], unsupported: ruleId }
}

// ——— 主公式 ———
// 与 roco 的 calculateDamage 逐步对齐;steps 即四行算式(供 UI 展示与人工核对)。
//
// 取整:显示威力四舍五入、分子四舍五入、每段与最终每段向下取整、段数先取整再乘。
export function calculateDamage({
  attackerStat,
  displayedPower,
  defenderDefense,
  reduction = 1,
  finalMultiplier = 1,
  hitCount = 1,
  extraDamage = 0,
  level = 60,
}) {
  const coefficient = levelCoefficient(level)
  const hits = Math.max(1, Math.floor(Number(hitCount) || 1))
  const def = Number(defenderDefense)
  if (!(def > 0)) return { ok: false, reason: 'missing-defense', steps: [] }

  const unrounded = Number(attackerStat) * Number(displayedPower) * coefficient
  const numerator = Math.round(unrounded) // 四舍五入
  const unroundedOneHit = (numerator / def) * Math.max(0, Number(reduction))
  const oneHit = Math.floor(unroundedOneHit) // 向下取整
  const finalOneHit = Math.floor(oneHit * Math.max(0, Number(finalMultiplier)))
  const total = finalOneHit * hits + Math.max(0, Number(extraDamage) || 0)

  const steps = [
    { label: STEP_LABELS[1], expr: null, value: Math.round(Number(displayedPower)) }, // 显示威力由调用方填
    { label: STEP_LABELS[2], expr: `round(${attackerStat} × ${displayedPower} × ${coefficient.toFixed(6)}) = ${numerator} ÷ ${def}${reduction !== 1 ? ` × 减伤${reduction}` : ''} → 向下取整`, value: oneHit },
    { label: STEP_LABELS[3], expr: `${oneHit} × ${hits} 段${extraDamage ? ` + 追加 ${extraDamage}` : ''}`, value: total },
  ]
  return { ok: true, coefficient, numerator, oneHit, finalOneHit, hits, total, steps }
}

// damageInput 归一一次预估的输入。
export function damageInput({ attacker, defender, skill, rules = null, buffs = [], conditions = {} } = {}) {
  // rules(克制表)也进 ctx:它参与计算,且缺它时算出的克制倍率是「假精确」的 1。
  return { attacker, defender, skill, rules, buffs, conditions }
}

// newSteps 生成四行算式的空槽(值都是 null),公式落地后逐行填 expr/value。
export const newSteps = () => STEP_LABELS.map((label) => ({ label, expr: null, value: null }))

// estimate 估算一次技能伤害(单向:攻方技能 → 守方)。
//
// 四行算式:技能威力 → 显示威力(本系 × 克制)→ 每段伤害 → 总伤害(段数 + 追加)。
// 任一输入缺失就返回 ok:false 并列出**缺什么** —— 拿默认值算出来的数字比「算不出」更害人。
export function estimate(input) {
  const ctx = damageInput(input)
  const { attacker, defender, skill, rules } = ctx
  const cond = ctx.conditions || {}
  const missing = []
  const steps = newSteps()

  if (!attacker || !defender || !skill) {
    return fail(['攻方 / 守方 / 技能'], ctx, steps)
  }

  const atk = statsFromPayload(attacker)
  const def = statsFromPayload(defender)
  if (!atk) missing.push('攻方六维')
  if (!def) missing.push('守方六维')
  if (missing.length) return fail(missing, ctx, steps, { attackerStats: atk, defenderStats: def })

  // 1) 能力等级:攻方攻击档 + 守方防御档合成一个系数,乘在攻击方能力值上。
  const stageMult = abilityLevelMultiplier(cond.attackStage, cond.defenseStage)
  const category = skill.category === 'magical' ? 'magicalAttack' : 'physicalAttack'
  const defKey = skill.category === 'magical' ? 'magicalDefense' : 'physicalDefense'
  // 档位已合成进 stageMult(守方防御档也在其中),故防御值不再重复调整。
  const atkStat = atk.values[category] * stageMult
  const defStat = def.values[defKey]

  // 2) 技能威力:静态/对局内的采用值,带 ruleId 时按动态规则重算。
  const bySpeed = skill.ruleId === 'speed_difference'
  const dyn = resolveDynamicPower({
    ruleId: skill.ruleId,
    basePower: skill.power,
    energy: attacker.energy,
    attackerValue: bySpeed ? atk.values.speed : atk.values.physicalDefense,
    defenderValue: bySpeed ? def.values.speed : def.values.physicalDefense,
  })
  if (dyn.power == null) return fail(dyn.missing, ctx, steps, { attackerStats: atk, defenderStats: def })
  steps[0] = { label: STEP_LABELS[0], expr: dyn.expr ?? `威力 ${dyn.power}`, value: dyn.power }

  // 3) 显示威力 = 技能威力 × 本系(1.25)× 克制;四舍五入(ROUNDING.displayedPower)。
  const stab = (attacker.damNames || []).includes(skill.type) ? 1.25 : 1
  const mult = typeMultiplier(skill.type, defender.damNames, rules) ?? 1
  const displayed = Math.round(dyn.power * stab * mult)
  steps[1] = {
    label: STEP_LABELS[1],
    expr: `${dyn.power}${stab !== 1 ? ' × 本系1.25' : ''}${mult !== 1 ? ` × 克制${mult}` : ''} → 四舍五入`,
    value: displayed,
  }

  // 4) 每段伤害与总伤害。
  const calc = calculateDamage({
    attackerStat: atkStat,
    displayedPower: displayed,
    defenderDefense: defStat,
    reduction: cond.reduction ?? 1,
    finalMultiplier: cond.finalMultiplier ?? 1,
    hitCount: skill.hits || 1,
    extraDamage: cond.extraDamage ?? 0,
    level: cond.level ?? 60,
  })
  if (!calc.ok) return fail([calc.reason === 'missing-defense' ? '守方防御值' : '守方防御值'], ctx, steps)
  steps[2] = calc.steps[1]
  steps[3] = calc.steps[2]

  const maxHp = def.values.hp
  const hpPct = maxHp > 0 ? (calc.total / maxHp) * 100 : null
  const remainHp = defender.hp != null ? Math.max(0, defender.hp - calc.total) : null

  return {
    ok: true,
    reason: null,
    steps,
    damage: calc.total,
    hpPct,
    remainHp,
    ctx: {
      ...ctx,
      attackerStats: atk,
      defenderStats: def,
      abilityStageMultiplier: stageMult,
      typeMultiplier: mult,
      stab,
      displayedPower: displayed,
      levelCoefficient: calc.coefficient,
      estimated: { attacker: atk.estimated, defender: def.estimated },
    },
  }
}

function fail(missing, ctx, steps, extra = {}) {
  return {
    ok: false,
    reason: 'missing-input',
    missing,
    steps,
    damage: null,
    hpPct: null,
    remainHp: null,
    ctx: { ...ctx, ...extra },
  }
}

// petLabel 给下拉框用的精灵标签:名字 + 等级;没有等级信息(对手未出场)时只给名字。
export function petLabel(pet) {
  if (!pet) return '—'
  const name = pet.name || pet.species || `形态 ${pet.baseConfId}`
  return pet.level ? `${name} Lv${pet.level}` : name
}
