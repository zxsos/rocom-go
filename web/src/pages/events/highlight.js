// 事件高亮规则:维度定义、持久化读取与命中判定。

// 高亮规则维度。仅「种类」为自由输入(种类繁多且无全表点选),其余点选条目。
export const FIELDS = [
  { k: 'species', label: '种类' },
  { k: 'nature', label: '性格' },
  { k: 'speciality', label: '特长' },
  { k: 'weight', label: '体重' },
  { k: 'voice', label: '声音' },
]

// 体重/声音按「在自身范围内的百分位」判定(非奖牌拥有):体重百分位 weightPct(0-100)、
// 声音 voice(-100~100)。阈值与宠物列表的极值高亮一致。大块头=体型最大、小不点=最小;
// 婉转声=声音最高昂、粗嗓门=最低沉(见奖牌定义)。
export const WEIGHT_OPTS = ['大块头', '小不点']
export const VOICE_OPTS = ['婉转声', '粗嗓门']

// 事件流里值得单独标出的稀有血脉(元素系血脉几乎人人有,不展示以免刷屏)
export const NOTABLE_BLOODS = ['污染', '奇异']

// 异色/炫彩始终高亮、系别与奖牌已废弃,读取时顺手剔除这些历史遗留规则。
export function sanitizeRules(v) {
  const dropped = ['shiny', 'colorful', 'type', 'medal']
  return Array.isArray(v) ? v.filter((x) => !dropped.includes(x.field)) : []
}

function matchRule(pet, rule) {
  if (!pet) return false
  // 体重/声音按百分位实际判定,不依赖奖牌是否已获得。
  if (rule.field === 'weight') {
    const p = pet.weightPct // 0-100:接近上/下限即体型最大/最小
    return p != null && (rule.value === '大块头' ? p >= 98 : p <= 2)
  }
  if (rule.field === 'voice') {
    const v = pet.voice // -100~100:最高昂/最低沉
    return v != null && (rule.value === '婉转声' ? v >= 96 : v <= -96)
  }
  return String(pet[rule.field] || '') === rule.value
}

// 异色/炫彩始终高亮;此外按维度分组:同维度内任一条目命中即算该维度命中(OR),
// 维度之间按 mode 组合——'and'=每个维度都命中、'or'=任一维度命中(等价于任一规则命中)。
// 各维度均为单值(体重/声音取百分位区间),同维度取或可避免同选两极永不命中。
// 无规则时仅异色/炫彩高亮(避免 and 下 every([]) 恒真把全部点亮)。
export function isHighlight(pet, rules, mode) {
  if (!pet) return false
  if (pet.shiny || pet.colorful) return true
  if (rules.length === 0) return false
  const groups = new Map() // field -> rules[]
  for (const r of rules) {
    if (!groups.has(r.field)) groups.set(r.field, [])
    groups.get(r.field).push(r)
  }
  const groupHit = (rs) => rs.some((r) => matchRule(pet, r))
  const g = [...groups.values()]
  return mode === 'or' ? g.some(groupHit) : g.every(groupHit)
}
