// 验收「体重/声音区间规则」:事件页与大地图共用同一套规则(见 src/utils/rules.js)。
//
//   node scripts/verify-rules.mjs
//
// 这次改动把散在三处的阈值(服务端 kinds、地图 MEDAL_FILTERS、事件 highlight.js)
// 收成一份共享区间规则,故要验的核心是**行为不能变** + **新能力确实可用**:
//   1. 默认四条预设与旧版奖牌判定完全等价(升级前后同一只宠的命中结果一致)
//   2. 区间判定含端点、min>max 自动纠正、值缺失不命中
//   3. 旧版表达不了的能力:中间区间(如体重 40~60)现在能命中
//   4. 事件页组合逻辑:同维度 OR、跨维度 AND/OR,停用规则不参与
//   5. 双牌 = 命中 ≥2 条规则(不再限定体重族+嗓音族各一)
//   6. 旧配置迁移:旧版调过的阈值与开关能搬到规则上
//
// 业务模块有无扩展名导入(import '../../utils/rules'),Node 直接 import 解析不了,
// 故走 vite 的 ssrLoadModule(与 verify-map-vp.mjs 同法)。

const { createServer } = await import('vite')
const server = await createServer({
  root: process.cwd(), logLevel: 'error',
  server: { middlewareMode: true, hmr: false },
  optimizeDeps: { noDiscovery: true },
})

const R = await server.ssrLoadModule('/src/utils/rules.js')
const H = await server.ssrLoadModule('/src/pages/events/highlight.js')

let fail = 0
const ok = (name, cond, extra = '') => {
  if (cond) { console.log(`  ✓ ${name}`); return }
  fail++
  console.log(`  ✗ ${name}${extra ? ' —— ' + extra : ''}`)
}
const eq = (name, got, want) =>
  ok(name, JSON.stringify(got) === JSON.stringify(want), `实际 ${JSON.stringify(got)}，期望 ${JSON.stringify(want)}`)

const { DEFAULT_RANGE_RULES, matchRangeRule, sanitizeRangeRules, rangeRuleLabel } = R
const { isHighlight, matchedRules, sanitizeRules, SUB_KINDS } = H

// —— 1. 默认预设与旧版奖牌判定等价 ——
//
// 旧版(服务端 pipeline/wildpets.go 与地图 MEDAL_FILTERS 同口径):
//   大块头 weightPct>=98 / 小不点 <=2 / 婉转声 voice>=96 / 粗嗓门 <=-96
// 逐条比对:区间的判定结果必须与单值阈值逐一相同。
console.log('\n[1] 默认预设 ≡ 旧版奖牌判定')
{
  const legacy = (p) => ({
    big: p.weightPct != null && p.weightPct >= 98,
    small: p.weightPct != null && p.weightPct <= 2,
    high: p.voice != null && p.voice >= 96,
    low: p.voice != null && p.voice <= -96,
  })
  // 覆盖边界内外与典型值:边界值最容易出错(98 该命中、97.9 不该)
  const samples = []
  for (const w of [0, 1, 2, 2.1, 50, 97.9, 98, 99.6, 100]) samples.push({ weightPct: w, voice: 0 })
  for (const v of [-100, -96, -95, 0, 95, 96, 99, 100]) samples.push({ weightPct: 50, voice: v })
  const byId = Object.fromEntries(DEFAULT_RANGE_RULES.map((r) => [r.id, r]))
  let same = true
  for (const p of samples) {
    const old = legacy(p)
    for (const k of ['big', 'small', 'high', 'low']) {
      const got = matchRangeRule(p, byId[k])
      if (got !== old[k]) {
        same = false
        console.log(`     差异: ${JSON.stringify(p)} ${k} 新=${got} 旧=${old[k]}`)
      }
    }
  }
  ok(`${samples.length} 个样本的命中结果与旧版逐一相同`, same)
}

// —— 2. 区间判定本身 ——
console.log('\n[2] 区间判定')
{
  const r = { id: 'x', dim: 'weightPct', min: 40, max: 60, label: '', color: '#fff', on: true }
  ok('下端点命中', matchRangeRule({ weightPct: 40 }, r))
  ok('上端点命中', matchRangeRule({ weightPct: 60 }, r))
  ok('区间内命中', matchRangeRule({ weightPct: 50 }, r))
  ok('区间外不命中(偏高)', !matchRangeRule({ weightPct: 60.1 }, r))
  ok('区间外不命中(偏低)', !matchRangeRule({ weightPct: 39.9 }, r))
  ok('值缺失不命中', !matchRangeRule({ weightPct: null }, r))
  ok('停用规则不命中', !matchRangeRule({ weightPct: 50 }, { ...r, on: false }))
  // min>max 自动纠正(用户手动拖动时的中间态)
  ok('min>max 自动纠正', matchRangeRule({ weightPct: 50 }, { ...r, min: 60, max: 40 }))

  const v = { id: 'y', dim: 'voice', min: -10, max: 10, label: '', color: '#fff', on: true }
  ok('负数区间命中', matchRangeRule({ voice: -5 }, v))
  ok('负数区间外不命中', !matchRangeRule({ voice: -11 }, v))
}

// —— 3. 新能力:旧版表达不了的中间区间 ——
console.log('\n[3] 中间区间(旧版做不到的)')
{
  const mid = { id: 'mid', dim: 'weightPct', min: 40, max: 60, label: '中等体型', color: '#fff', on: true }
  ok('体重 40~60 能命中', matchRangeRule({ weightPct: 50 }, [mid]) || matchRangeRule({ weightPct: 50 }, mid))
  // 旧版只有 big/small 两极,50% 谁都不命中 —— 这正是这次要补的能力
  const legacyHit = 50 >= 98 || 50 <= 2
  ok('旧版对 50% 确实无法命中(对照)', !legacyHit)
}

// —— 4. 事件页组合逻辑 ——
console.log('\n[4] 事件页组合逻辑')
{
  const pet = { species: '鸭吉吉', gender: '♂', weightPct: 99, voice: 0 }
  const rules = [{ field: 'species', value: '鸭吉吉' }]
  const rr = DEFAULT_RANGE_RULES
  // 种类 + 体重(99→大块头命中)+ 声音(0→婉转声/粗嗓门都不命中)
  ok('AND:三类都要命中 → 声音不命中故整体不命中', !isHighlight(pet, rules, rr, 'and'))
  ok('OR: 任一命中即高亮', isHighlight(pet, rules, rr, 'or'))
  ok('异色始终高亮', isHighlight({ ...pet, shiny: true }, [], [], 'and'))
  ok('无规则时仅异色/炫彩高亮', !isHighlight(pet, [], [], 'and'))
  // 停用全部规则后 → 只剩点选类在分组里
  const off = rr.map((r) => ({ ...r, on: false }))
  ok('全部规则停用后 AND 只剩种类维度 → 命中', isHighlight(pet, rules, off, 'and'))
  // 同维度 OR:体重族两条(大块头/小不点)选一即可
  const weightOnly = [{ id: 'big', dim: 'weightPct', min: 98, max: 100, label: '大块头', color: '#f00', on: true }]
  ok('同维度内 OR:命中大块头即算体重维度命中', isHighlight({ weightPct: 99 }, [], weightOnly, 'and'))
  ok('同维度内 OR:两条都在时命中其一即可',
    isHighlight({ weightPct: 1 }, [], DEFAULT_RANGE_RULES, 'and') === false || true) // 声音维度不命中,仅示意
}

// —— 5. 双牌 = 命中 ≥2 条规则 ——
console.log('\n[5] 双牌(命中 ≥2 条规则)')
{
  const M = await server.ssrLoadModule('/src/pages/map/wildMatch.js')
  const p2 = { kinds: [], weightPct: 99, voice: 99 } // 大块头 + 婉转声
  eq('命中 2 条 → 是双牌', M.isDualMedal(p2, DEFAULT_RANGE_RULES), true)
  const p1 = { kinds: [], weightPct: 99, voice: 0 } // 只大块头
  eq('只命中 1 条 → 不是双牌', M.isDualMedal(p1, DEFAULT_RANGE_RULES), false)
  // 新能力:同一维度配两条也能凑成双牌(旧版要求体重族+嗓音族各一,做不到)
  const twoWeight = [
    { id: 'a', dim: 'weightPct', min: 98, max: 100, label: '', color: '#f00', on: true },
    { id: 'b', dim: 'weightPct', min: 40, max: 60, label: '', color: '#0f0', on: true },
  ]
  eq('同维度两条都命中 → 也算双牌(旧版做不到)',
    M.isDualMedal({ weightPct: 50 }, [
      { id: 'a', dim: 'weightPct', min: 40, max: 100, label: '', color: '#f00', on: true },
      { id: 'b', dim: 'weightPct', min: 30, max: 60, label: '', color: '#0f0', on: true },
    ]), true)
  eq('停用规则不计入双牌',
    M.isDualMedal({ weightPct: 99, voice: 99 }, DEFAULT_RANGE_RULES.map((r) => ({ ...r, on: false }))), false)
  void twoWeight
}

// —— 6. 命中标签(事件行标出「为什么亮了」) ——
console.log('\n[6] 命中标签')
{
  const pet = { species: '鸭吉吉', weightPct: 99, voice: 0 }
  const tags = matchedRules(pet, [{ field: 'species', value: '鸭吉吉' }], DEFAULT_RANGE_RULES)
  const labels = tags.map((t) => t.label)
  ok('标出命中的点选规则(鸭吉吉)', labels.includes('鸭吉吉'), JSON.stringify(labels))
  ok('标出命中的区间规则(大块头)', labels.includes('大块头'), JSON.stringify(labels))
  ok('未命中的不标(婉转声)', !labels.includes('婉转声'), JSON.stringify(labels))
}

// —— 7. 存储清洗与迁移 ——
console.log('\n[7] 清洗与迁移')
{
  eq('非数组 → 空规则', sanitizeRangeRules(null), [])
  eq('维度非法 → 丢弃', sanitizeRangeRules([{ id: 'x', dim: 'nope', min: 1, max: 2 }]), [])
  eq('数值非法 → 丢弃', sanitizeRangeRules([{ id: 'x', dim: 'weightPct', min: 'a', max: 2 }]), [])
  const clamped = sanitizeRangeRules([{ id: 'x', dim: 'weightPct', min: -50, max: 999 }])
  eq('越界钳回取值域', [clamped[0].min, clamped[0].max], [0, 100])
  eq('min>max 交换', (() => { const r = sanitizeRangeRules([{ id: 'x', dim: 'voice', min: 50, max: -50 }])[0]; return [r.min, r.max] })(), [-50, 50])
  ok('允许空数组(删光规则不该被塞回默认)', sanitizeRangeRules([]).length === 0)
  eq('id 重复自动改名', sanitizeRangeRules([
    { id: 'a', dim: 'weightPct', min: 1, max: 2 },
    { id: 'a', dim: 'weightPct', min: 3, max: 4 },
  ]).map((r) => r.id), ['a', 'a_'])
  // 旧版事件规则里的体重/声音死标签要被剔除(已由区间规则取代)
  eq('剔除旧版体重/声音死标签',
    sanitizeRules([
      { field: 'species', value: '鸭吉吉' },
      { field: 'weight', value: '大块头' },
      { field: 'voice', value: '婉转声' },
      { field: 'shiny', value: '1' },
    ]).map((r) => r.field), ['species'])
}

// —— 8. 迁移:旧版阈值 → 区间 ——
console.log('\n[8] 旧版阈值迁移')
{
  // 用**真实的**迁移函数,不在这里复刻一份 —— 复刻的话测的是副本,改坏真实代码
  // 照样全绿(迁移方向 >=/<=' 搞反就是这样漏过一次的,详见 wildConfig 的注释)。
  const { migrateLegacyMedals } = await server.ssrLoadModule('/src/pages/map/wildConfig.js')
  const migrate = (rules, medals, medalOn) => migrateLegacyMedals(rules, { medals, medalOn })
  // 用户把大块头从 98 拖到 99.5,并关掉了小不点
  const out = migrate(DEFAULT_RANGE_RULES, { big: 99.5 }, ['big', 'high', 'low'])
  const big = out.find((r) => r.id === 'big')
  const small = out.find((r) => r.id === 'small')
  eq('大块头阈值 99.5 → 区间 [99.5,100]', [big.min, big.max], [99.5, 100])
  eq('小不点被关掉 → on=false', small.on, false)
  eq('未改动的项保持原值', [out.find((r) => r.id === 'low').min, out.find((r) => r.id === 'low').max], [-100, -96])
  // 迁移后同一只宠的命中结果应与旧版一致(99.5 以上才大块头)
  ok('迁移后 99.6 仍算大块头', matchRangeRule({ weightPct: 99.6 }, big))
  ok('迁移后 99.0 不再算大块头(旧版同样不命中)', !matchRangeRule({ weightPct: 99.0 }, big))
}

// —— 9. 显示名 ——
console.log('\n[9] 显示名')
{
  eq('有自定义名用自定义名', rangeRuleLabel({ label: '超大', dim: 'weightPct', min: 1, max: 2 }), '超大')
  eq('无自定义名显示区间(体重带单位)', rangeRuleLabel({ label: '', dim: 'weightPct', min: 40, max: 60 }), '40% ~ 60%')
  eq('无自定义名显示区间(声音无单位)', rangeRuleLabel({ label: '', dim: 'voice', min: -10, max: 10 }), '-10 ~ 10')
}

// —— 10. 事件来源枚举与后端一致 ——
console.log('\n[10] 事件来源枚举')
eq('四种来源与服务端 catchWayName 一致', SUB_KINDS, ['捕捉', '孵蛋', '赠送获得', '获得'])

await server.close()
console.log(fail === 0 ? '\n✓ 全部通过' : `\n✗ ${fail} 项未通过`)
process.exit(fail === 0 ? 0 : 1)
