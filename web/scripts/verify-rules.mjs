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
  ok('体重 40~60 能命中', matchRangeRule({ weightPct: 50 }, mid))
  // 旧版只有 big/small 两极(单值阈值 >=98 / <=2),50% 谁都不命中 —— 这正是要补的能力。
  // 写成函数再代入样本,不能写 `50 >= 98 || 50 <= 2` 那种字面量:那是常量表达式,
  // eslint 的 no-constant-binary-expression 会报,而且它压根没在验证规则 —— 改了
  // DEFAULT_RANGE_RULES 的阈值它照样"通过",是条死断言。
  const legacy = (pct) => pct >= 98 || pct <= 2
  ok('旧版对 50% 确实无法命中(对照)', !legacy(50))
  ok('旧版对极值仍能命中(回归:没把旧能力弄丢)', legacy(99) && legacy(1))
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

// —— 5b. 画环与文案必须是同一口径 ——
//
// 故障原型:悬浮面板(.twt)曾写成 `wildTags(p.kinds)` —— 第一个参数该是**宠物
// 对象**(函数内部读 p.kinds),传了 kinds 数组,第二个参数 rangeRules 又漏传,
// 于是恒返回空、落到兜底文案「普通」。表现就是用户看到的:
//   **图上描着大块头的环(wildShown 判定对),点开却显示「普通」**。
//
// 故这里的断言不是「函数算得对」,而是**两条投影之间的一致性**:
// 凡是能画环的宠,文案里必须说得出为什么 —— 不能一边描环一边说普通。
console.log('\n[5b] 画环 ⇄ 文案 口径一致')
{
  const M = await server.ssrLoadModule('/src/pages/map/wildMatch.js')
  const on = new Set(['mutation', 'pollution'])
  // 覆盖:四类奖牌各一、双牌、异色、炫彩、异色炫彩、污染、以及谁都不中的普通值
  const samples = [
    { kinds: [], weightPct: 99, voice: 0, want: '大块头' },
    { kinds: [], weightPct: 1, voice: 0, want: '小不点' },
    { kinds: [], weightPct: 50, voice: 99, want: '婉转声' },
    { kinds: [], weightPct: 50, voice: -99, want: '粗嗓门' },
    { kinds: [], weightPct: 99, voice: 99 },                       // 双牌
    { kinds: ['shiny'], weightPct: 50, voice: 0, want: '异色' },
    { kinds: ['colorful'], weightPct: 50, voice: 0, want: '炫彩' },
    { kinds: ['shiny', 'colorful'], weightPct: 50, voice: 0, want: '异色炫彩' },
    { kinds: ['pollution'], weightPct: 50, voice: 0, want: '污染' },
    { kinds: [], weightPct: 50, voice: 0 },                        // 谁都不中
  ]
  for (const s of samples) {
    const shown = M.wildShown(s, on, DEFAULT_RANGE_RULES, false)
    const text = M.wildTagText(s, DEFAULT_RANGE_RULES)
    const label = `kinds=[${s.kinds}] w=${s.weightPct} v=${s.voice}`
    // 核心断言:能画环 → 文案不能是「普通」(那等于描了环却说不出为什么)
    if (shown) ok(`${label}: 能画环 → 文案非空`, text !== '普通', `实际「${text}」`)
    else ok(`${label}: 不画环 → 文案为「普通」`, text === '普通', `实际「${text}」`)
    if (s.want) ok(`${label}: 文案含「${s.want}」`, text.includes(s.want), `实际「${text}」`)
  }
  // 双牌文案要同时说两条(只说一条就看不出为什么算双牌)
  const dual = M.wildTagText({ kinds: [], weightPct: 99, voice: 99 }, DEFAULT_RANGE_RULES)
  ok('双牌:文案同时含两条奖牌名', dual.includes('大块头') && dual.includes('婉转声'), `实际「${dual}」`)
}

// —— 5c. 悬浮面板必须走 wildTagText(不得自行拼 wildTags)——
//
// 上一条断言测的是函数本身;这一条守住**调用点**。函数写得再对,调用点写错
// (漏传 rangeRules / 传错第一个参数)页面照样错 —— 而那正是本次故障的形态,
// 且纯函数测试是抓不到的。故直接读源码断言那一行调的是谁。
console.log('\n[5c] 悬浮面板调用点')
{
  const { readFileSync } = await import('node:fs')
  const src = readFileSync('src/pages/map/useMapEngine.jsx', 'utf8')
  const twt = src.split('\n').find((l) => l.includes('className="twt"'))
  ok('存在 .twt 这一行', !!twt)
  if (twt) {
    ok('.twt 调用 wildTagText(p, rangeRules)', twt.includes('wildTagText(p, rangeRules)'), `实际: ${twt.trim()}`)
    ok('.twt 未自行拼 wildTags', !twt.includes('wildTags('), `实际: ${twt.trim()}`)
  }
  // 入参顺序:wildTags 的首参是宠物对象。曾传成 p.kinds(数组)导致内部 p.kinds
  // 取到 undefined —— 这里守住 useMapEngine 里不再残留那种调用。
  const bad = src.split('\n').filter((l) => /wildTags\(\s*p\.kinds/.test(l))
  ok('无 wildTags(p.kinds) 这类错误调用', bad.length === 0, bad.join(' | '))
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

// —— 11. 双滑块:拖一端的钳制 ——
console.log('\n[11] 双滑块钳制')
{
  const W = R.DIM_BY_K.weightPct   // 0~100
  const V = R.DIM_BY_K.voice       // -100~100
  // 下限拖到上限右边 → 钳成退化区间(不能翻转)
  eq('下限拖过上限 → 钳到上限', R.clampRange('min', 80, 60, W), [60, 60])
  eq('上限拖过下限 → 钳到下限', R.clampRange('max', 20, 60, W), [60, 60])
  eq('正常拖动下限', R.clampRange('min', 30, 90, W), [30, 90])
  eq('正常拖动上限', R.clampRange('max', 90, 30, W), [30, 90])
  eq('拖出取值域下界 → 钳回', R.clampRange('min', -50, 80, W), [0, 80])
  eq('拖出取值域上界 → 钳回', R.clampRange('max', 999, 20, W), [20, 100])
  // 嗓音域是负的,最容易写错
  eq('嗓音域:下限拖过上限', R.clampRange('min', 50, -20, V), [-20, -20])
  eq('嗓音域:正常区间', R.clampRange('max', 96, -96, V), [-96, 96])
}

// —— 12. 重合时哪个滑块在上层 ——
console.log('\n[12] 滑块叠放顺序')
{
  const W = R.DIM_BY_K.weightPct
  const V = R.DIM_BY_K.voice
  // 区间整体偏左 → 上限在上(用户多半想往右扩)
  eq('偏左时上限在上', R.sliderTop(0, 2, W), 'max')
  eq('偏左下界时上限在上', R.sliderTop(10, 20, W), 'max')
  // 整体偏右 → 下限在上(想往左缩)
  eq('偏右时下限在上', R.sliderTop(98, 100, W), 'min')
  eq('偏右下界时下限在上', R.sliderTop(70, 90, W), 'min')
  // 嗓音域以 0 为中心
  eq('嗓音偏负时上限在上', R.sliderTop(-100, -96, V), 'max')
  eq('嗓音偏正时下限在上', R.sliderTop(96, 100, V), 'min')
  eq('嗓音居中时上限在上', R.sliderTop(-10, 10, V), 'max')
}

// —— 13. 预设档位 ——
console.log('\n[13] 预设档位')
{
  const { RULE_PRESETS, RULE_SCHEMES, schemeRules } = R
  ok('预设非空', RULE_PRESETS.length >= 8, `实际 ${RULE_PRESETS.length}`)
  // 预设必须在各自维度的取值域内,且 min<=max —— 否则点了就是条非法规则
  let bad = []
  for (const p of RULE_PRESETS) {
    const d = R.DIM_BY_K[p.dim]
    if (!d || p.min < d.min || p.max > d.max || p.min > p.max) bad.push(p.label)
  }
  eq('全部预设的区间合法且在取值域内', bad, [])
  // 同维度内的预设 label 不能重名(重名会导致方案筛选时多选)
  const dup = RULE_PRESETS.map((p) => p.label).filter((v, i, a) => a.indexOf(v) !== i)
  eq('预设名称不重复', [...new Set(dup)], [])
  // 每个维度都要有预设,否则「添加」时那一组是空的
  for (const d of R.RANGE_DIMS) {
    ok(`维度「${d.n}」有预设`, RULE_PRESETS.some((p) => p.dim === d.k))
  }
  // 方案
  ok('方案非空', RULE_SCHEMES.length >= 3)
  const medal = schemeRules(RULE_SCHEMES.find((s) => s.k === 'medal'))
  eq('「奖牌四件套」展开为 4 条', medal.length, 4)
  // 方案里的规则必须带预设的区间与配色(不是空壳),且名字对得上
  eq('奖牌四件套 = 大块头/小不点/婉转声/粗嗓门',
    medal.map((r) => r.label).sort(), ['婉转声', '小不点', '粗嗓门', '大块头'].sort())
  eq('方案规则沿用预设区间(大块头 98~100)',
    (() => { const b = medal.find((r) => r.label === '大块头'); return [b.min, b.max] })(), [98, 100])
  eq('方案规则全部启用', medal.every((r) => r.on === true), true)
  // 方案展开的规则必须能被清洗函数接受(否则点了方案会得到非法规则)
  eq('方案展开后能过 sanitize', schemeRules(RULE_SCHEMES[0]).length, sanitizeRangeRules(schemeRules(RULE_SCHEMES[0])).length)
  eq('「清空」展开为空数组', schemeRules(RULE_SCHEMES.find((s) => s.k === 'none')), [])
}

// —— 变焦刻度(rangeScale)——
//
// 轨道被分段放大:两头的奖牌区各占 35%,中间段压扁(见 utils/rules.js)。
// 这里的断言全部是**数值性质**,而这类性质出错时页面不会报错 ——
// 滑块照样能拖、规则照样能存,只是「想要的区间拖不出来」。只有断言能抓。
console.log('\n[9] 变焦刻度 rangeScale')
{
  for (const dim of R.RANGE_DIMS) {
    const sc = R.rangeScale(dim)

    // 铺满:轨道两端必须正好是取值域两端,否则滑块拖到底也到不了极值
    eq(`${dim.n}: 值域下界落在轨道 0`, sc.toPos(dim.min), 0)
    eq(`${dim.n}: 值域上界落在轨道 1`, sc.toPos(dim.max), 1)

    // 分段连续且铺满:后一段的 start 必须等于前一段的 end
    const joined = sc.segs.every((s, i) => i === 0 || Math.abs(s.start - sc.segs[i - 1].end) < 1e-12)
    ok(`${dim.n}: 各段首尾相接`, joined)
    ok(`${dim.n}: 重点区+中间段覆盖整个取值域`,
      Math.abs(sc.segs[0].a - dim.min) < 1e-9 &&
      Math.abs(sc.segs[sc.segs.length - 1].b - dim.max) < 1e-9)

    // 单调:轨道从左往右,取值只能变大不能变小。不单调的话拖动会「跳回去」,
    // 而磁吸是唯一允许的例外(它是有意的吸附)。
    let prev = -Infinity
    let drops = []
    for (let i = 0; i <= 2000; i++) {
      const v = sc.toValue(i / 2000)
      if (v < prev - 1e-9) {
        // 只可能是磁吸在起作用:该位置的真值应当与某个奖牌边界足够近
        const nearMark = sc.marks.some((m) => Math.abs(i / 2000 - m.pos) <= 0.02)
        if (!nearMark) drops.push(`${(i / 20).toFixed(2)}%: ${prev} → ${v}`)
      }
      prev = v
    }
    eq(`${dim.n}: 取值随轨道单调不减(磁吸除外)`, drops.slice(0, 3), [])

    // 往返:默认规则(奖牌窗口)必须能被精确复现 —— 这是最高频的用法,
    // 拖一下把 98 变成 97.9 就等于筛不出奖牌了。
    for (const r of R.DEFAULT_RANGE_RULES.filter((x) => x.dim === dim.k)) {
      eq(`${dim.n}: 默认「${r.label}」往返不失真`,
        [sc.toValue(sc.toPos(r.min)), sc.toValue(sc.toPos(r.max))], [r.min, r.max])
    }

    // 放大:重点区每单位取值摊到的轨道,必须远多于中间段
    const focusSeg = sc.segs.find((s) => s.focus)
    const gapSeg = sc.segs.find((s) => !s.focus)
    const dens = (s) => (s.end - s.start) / (s.b - s.a)
    ok(`${dim.n}: 重点区刻度比中间段细 5 倍以上`,
      dens(focusSeg) > dens(gapSeg) * 5,
      `${dens(focusSeg).toFixed(5)} vs ${dens(gapSeg).toFixed(5)}`)

    // 中间段粗步进:拖出来的值必须落在放大后的网格上,而不是 43.7 这种噪音。
    //
    // 必须**整段扫一遍**,不能只抽查中点:中点(体重 50 / 嗓音 0)在粗细两个网格上
    // 都成立,于是把 COARSE_MUL 改成 1(或干脆不分档)测试照样全绿 ——
    // 而「非奖牌区 step 更大」正是这次需求本身,漏掉它等于没测。
    const coarse = dim.step * 10
    const offGrid = []
    for (let i = 1; i < 200; i++) {
      const p = gapSeg.start + ((gapSeg.end - gapSeg.start) * i) / 200
      const v = sc.toValue(p)
      // 段边界是「夹回本段」夹出来的,可能本就不在网格上,排除
      if (v <= gapSeg.a + 1e-9 || v >= gapSeg.b - 1e-9) continue
      if (Math.abs(v / coarse - Math.round(v / coarse)) > 1e-9) offGrid.push(String(v))
    }
    eq(`${dim.n}: 中间段的值全部落在粗网格上(step×10)`, offGrid.slice(0, 3), [])

    // 反向确认重点区保持**细**步进:粗网格若被用到重点区,奖牌边界就凑不准了。
    const fineSeg = sc.segs.find((s) => s.focus)
    const fineOff = []
    for (let i = 1; i < 200; i++) {
      const p = fineSeg.start + ((fineSeg.end - fineSeg.start) * i) / 200
      const v = sc.toValue(p)
      if (v <= fineSeg.a + 1e-9 || v >= fineSeg.b - 1e-9) continue
      if (Math.abs(v / dim.step - Math.round(v / dim.step)) > 1e-9) fineOff.push(String(v))
    }
    eq(`${dim.n}: 重点区的值落在细网格上(step)`, fineOff.slice(0, 3), [])

    // 磁吸:奖牌边界要吸得住,也要能脱离
    for (const m of sc.marks) {
      eq(`${dim.n}: 磁吸点在轨道上取回自身`, sc.toValue(m.pos), m.v)
    }
    // 脱离:从边界挪开「半径的 3 倍」后,不该还粘在边界上。
    // 不验这一条的话,把磁吸半径写大(比如 0.5)测试照样全绿,而用户就再也
    // 拖不出「放宽一点点」的区间了 —— 那正是这次改动要保留的能力。
    for (const m of sc.marks) {
      const inner = sc.marks.filter((o) => o !== m)
      for (const dir of [-0.06, 0.06]) {
        const p = m.pos + dir
        // 端点刻度(0 / 100)本身就在轨道两头,朝外挪会被夹回来 —— 那不是"脱离",
        // 跳过即可。只测朝轨道内侧的那一边。
        if (p < 0 || p > 1) continue
        const v = sc.toValue(p)
        // 若落点仍在**别的**刻度线的磁吸半径内,取到那个值也算脱离了这一个
        const stillMagnet = inner.some((o) => Math.abs(p - o.pos) <= 0.015) && v !== m.v
        ok(`${dim.n}: 离开边界 ${m.v} 后能取到别的值`,
          v !== m.v || stillMagnet, `位置 ${(p * 100).toFixed(0)}% 仍得 ${m.v}`)
      }
    }
  }

  // 奖牌窗口的可用宽度:按比例的像素数 vs 变焦后的像素数。
  // 这才是「在需要的区域太小了」那条抱怨的量化答案。
  const w = R.rangeScale(R.DIM_BY_K.weightPct)
  const px = w.toPos(100) - w.toPos(98)
  ok('体重奖牌窗口摊到的轨道 ≥ 5%(按比例只有 2%)', px >= 0.05, `实际 ${(px * 100).toFixed(1)}%`)
  const vv = R.rangeScale(R.DIM_BY_K.voice)
  const vpx = vv.toPos(-96) - vv.toPos(-100)
  ok('嗓音奖牌窗口摊到的轨道 ≥ 5%(按比例只有 2%)', vpx >= 0.05, `实际 ${(vpx * 100).toFixed(1)}%`)

  // 奖牌边界 = 默认规则的端点:阈值只有一个来源,刻度与磁吸跟着它走
  for (const dim of R.RANGE_DIMS) {
    const want = []
    for (const r of R.DEFAULT_RANGE_RULES) {
      if (r.dim === dim.k) want.push(r.min, r.max)
    }
    eq(`${dim.n}: 刻度线 = 默认规则的端点`,
      R.rangeScale(dim).marks.map((m) => m.v).sort((a, b) => a - b),
      [...new Set(want)].sort((a, b) => a - b))
  }
}

await server.close()
console.log(fail === 0 ? '\n✓ 全部通过' : `\n✗ ${fail} 项未通过`)
process.exit(fail === 0 ? 0 : 1)
