// 事件高亮规则:维度定义、持久化读取与命中判定。
//
// 两类规则,分开存:
//   - 点选类(种类/性别/性格/特长):{ field, value },本页 hlRules 里;
//   - 区间类(体重/声音):{ id, dim, min, max, label, color, on },**与大地图共用**
//     (见 utils/rules.js 与 hooks/useRangeRules)。
//
// 体重/声音原本也是点选四个死标签(大块头/小不点/婉转声/粗嗓门)且阈值硬编码在
// matchRule 里,现已由区间规则取代 —— 两者共用一套判定,改一处两边生效。
import { matchRangeRule, rangeRuleLabel, LEGACY_RANGE_FIELDS } from '../../utils/rules'

// 高亮规则维度。仅「种类」为自由输入(种类繁多且无全表点选),其余点选条目。
// 体重/声音不在此列:它们是区间规则,由 RangeRules 组件编辑(见 RulePanel)。
export const FIELDS = [
  { k: 'species', label: '种类' },
  { k: 'gender', label: '性别' },
  { k: 'nature', label: '性格' },
  { k: 'speciality', label: '特长' },
]

// 性别按符号点选(与宠物列表筛选同一套:♂ 雄、♀ 雌,数据里直接就是这个符号)。
export const GENDER_OPTS = ['♂', '♀']

// 事件流里值得单独标出的稀有血脉(元素系血脉几乎人人有,不展示以免刷屏)
export const NOTABLE_BLOODS = ['污染', '奇异']

// 事件来源(server catchWayName 的四种取值;统计卡片里也是这个顺序)。
export const SUB_KINDS = ['捕捉', '孵蛋', '赠送获得', '获得']

// 异色/炫彩始终高亮、系别与奖牌已废弃,读取时顺手剔除这些历史遗留规则。
// weight/voice 一并剔除:它们是旧版体重/声音的死标签(如「大块头」),已由区间
// 规则取代 —— 留着只会变成判不中也点不掉的僵尸规则。
export function sanitizeRules(v) {
  const dropped = ['shiny', 'colorful', 'type', 'medal']
  return Array.isArray(v)
    ? v.filter((x) => !dropped.includes(x.field) && !LEGACY_RANGE_FIELDS.has(x.field))
    : []
}

function matchRule(pet, rule) {
  if (!pet) return false
  return String(pet[rule.field] || '') === rule.value
}

// hitOne 判定单条规则:区间类走 matchRangeRule(dim),点选类走字符串相等(field)。
const hitOne = (pet, r) => (r.dim ? matchRangeRule(pet, r) : matchRule(pet, r))

// dimKey 分组键:区间类按 dim、点选类按 field,同维度内任一条命中即算该维度命中(OR)。
const dimKey = (r) => r.dim || r.field

// isHighlight 异色/炫彩始终高亮;此外按维度分组:同维度内任一条目命中即算该维度命中,
// 维度之间按 mode 组合——'and'=每个维度都命中、'or'=任一维度命中。
//
// 区间规则只算 on 的那些(停用等同不存在),否则停用后高亮不消失,开关就成了摆设。
// 无规则时仅异色/炫彩高亮(避免 and 下 every([]) 恒真把全部点亮)。
export function isHighlight(pet, rules, rangeRules, mode) {
  if (!pet) return false
  if (pet.shiny || pet.colorful) return true
  const groups = new Map()
  for (const r of rules) {
    if (!groups.has(dimKey(r))) groups.set(dimKey(r), [])
    groups.get(dimKey(r)).push(r)
  }
  for (const r of rangeRules) {
    if (!r.on) continue
    if (!groups.has(r.dim)) groups.set(r.dim, [])
    groups.get(r.dim).push(r)
  }
  const g = [...groups.values()]
  if (g.length === 0) return false
  return mode === 'or'
    ? g.some((rs) => rs.some((r) => hitOne(pet, r)))
    : g.every((rs) => rs.some((r) => hitOne(pet, r)))
}

// matchedRules 列出一只宠命中了**哪些**规则,供事件行标出「为什么亮了」。
//
// isHighlight 只回一个布尔,看不出是体重还是声音命中的 —— 高亮一多就分不清,
// 这个值就是给 UI 补上那层解释。点选类统一用主色(它们没有自己的颜色概念)。
export function matchedRules(pet, rules, rangeRules) {
  if (!pet) return []
  const out = []
  for (const r of rules) {
    if (hitOne(pet, r)) {
      out.push({ id: `${r.field}:${r.value}`, label: r.value, color: 'var(--accent)' })
    }
  }
  for (const r of rangeRules) {
    if (!r.on) continue
    if (matchRangeRule(pet, r)) {
      out.push({ id: r.id, label: rangeRuleLabel(r), color: r.color })
    }
  }
  return out
}
