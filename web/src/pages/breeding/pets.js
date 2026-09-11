// 培育页与宠物库之间的口径转换(纯函数,不碰 IO)。
//
// 这里每条规则都必须与后端一致(internal/pet/breeding.go):手动补录提交的是**快照**,
// 后端只做校验、不会替前端补字段 —— 前端少填一个 voice,那一代的记录就永远是 0。

// 嗓音的取值范围(与 pet.VoiceLow/VoiceHigh 同):差距归一化时当分母。
export const VOICE_SPAN = 200

// 目标所在的轴:嗓音 -100~100(中心 0)、体重百分位 0~100(中心 50)。
// 必须与后端一致(嗓音抄 pet.VoiceLow/VoiceHigh,体重就是百分位本身)。
export const GOAL_AXES = {
  voice: { lo: -100, hi: 100 },
  weight: { lo: 0, hi: 100 },
}

// goalReached 某一维是否达标:方向由目标偏向轴的哪一端决定 —— 目标偏高(> 中心)时「≥ 目标」
// 算到、偏低(< 中心)时「≤ 目标」算到、正好在中心时精确相等。
//
// ⚠️ 必须与后端 internal/pet/breeding.go 的 reached **逐字一致**:页面上的「达标」用的就是
// 后端置 done 的同一判定,两处不等会冒出「页面写着达标、状态还停在进行中」。
// 按方向而不是一律 ≥,是为了不毁掉「极限嗓音 -100 / 极限体重 0%」这种往低刷的目标 ——
// 一律 ≥ 会让它们对任何个体都成立(等于目标失效)。
export function goalReached(cur, goal, kind) {
  if (cur == null || goal == null) return false
  const { lo, hi } = GOAL_AXES[kind] || GOAL_AXES.voice
  const center = (lo + hi) / 2
  if (goal > center) return cur >= goal
  if (goal < center) return cur <= goal
  return cur === goal
}

// toParent 把库里的个体转成亲本 / 子代快照。字段口径对齐 pet.ParentSnapshot。
export function toParent(p) {
  if (!p) return null
  return {
    gid: p.gid, name: p.name, species: p.species, confId: p.confId,
    // 两种头像形状都要认:宠物列表(/api/pets)给嵌套的 image.head,候选池(/api/breeding/pool)
    // 为省 payload 直接给 img 字符串。少认一种,补录提交上去的快照就没有头像。
    img: (p.image && p.image.head) || p.img || '', gender: p.gender,
    heightM: p.heightM, weightKg: p.weightKg,
    heightPct: p.heightPct, weightPct: p.weightPct,
    voice: p.voice, nature: p.nature, talentRank: p.talentRank,
  }
}

// nextGen 下一代的号 = 已记代数与待认领代数取大者 +1(同 pet.NextGen)。
//
// 用「取大」而不是「已记代数 +1」:待认领的那一代早就占了号,按已记代数发号会让两代同号 ——
// 而认领是按代数定位的,那样会补到错误的一代上。
export function nextGen(line) {
  let gen = 0
  for (const g of [...((line && line.gens) || []), ...((line && line.pending) || [])]) {
    if (g.gen > gen) gen = g.gen
  }
  return gen + 1
}

// eggGroupNames 取一串蛋组的名字。两种形状都得认:/api/pets 给 [{name,desc}],而候选池
// (/api/breeding/pool)只给 ["龙"] —— 官方描述在选种上用不到,几百只每只两条长文案白占 payload。
// 只认一种形状的后果是静默的:另一种会被当成「没有名字的蛋组」滤掉,母本蛋组过滤后一只种公都不剩。
const eggGroupNames = (gs) =>
  (gs || []).map((x) => (typeof x === 'string' ? x : x && x.name)).filter(Boolean)

// eggGroupsMatch 两串蛋组是否有交集(同 pet.EggGroupsMatch);任一为空按「不知道」处理,一律不匹配。
export function eggGroupsMatch(a, b) {
  const as = eggGroupNames(a)
  const bs = eggGroupNames(b)
  if (!as.length || !bs.length) return false
  return as.some((x) => bs.includes(x))
}

// fatherCandidates 种公候选的**二次过滤**:雄性与所选母本共用蛋组的个体(父亲是谁不影响物种,
// 能配上就有意义)。候选池那一步已按该品种雌性的蛋组并集粗筛过,这里再按**这只**母本收一次 ——
// 同一品种的母本蛋组通常相同,但库里的蛋组是逐只注入的,可能有个体缺失。
//
// 母本未选、或其蛋组未知时不做蛋组过滤 —— 空列表会让玩家根本选不出父亲(见 BreedCandidates)。
export function fatherCandidates(pets, mother) {
  const males = (pets || []).filter((p) => p.gender === '♂')
  if (!mother || !eggGroupNames(mother.eggGroups).length) return males
  return males.filter((p) => eggGroupsMatch(mother.eggGroups, p.eggGroups))
}

// petPickerOption 把候选宠转成 PetPicker 的选项(见 components/PetPicker.jsx)。
//
// 名字单独给出来是给**过滤**用的;数值压成一行副文本。同品种同名的个体很常见(刷了一窝),
// 不带上嗓音 / 体重百分位 / 性格根本分不清选的是哪一只 —— 只给名字的候选列表在这里等于没用。
export function petPickerOption(p) {
  if (!p) return null
  const w = p.weightPct == null ? '' : `W${Math.round(p.weightPct)}%`
  return {
    value: String(p.gid),
    img: (p.image && p.image.head) || p.img || '',
    name: p.name || p.species || `#${p.gid}`,
    sub: [`V${p.voice}`, w, p.nature].filter(Boolean).join(' · '),
  }
}

// —— 进度汇总(与 pet.LineStats 同口径,后端只下发线本体,这里按需现算)—— 

// closerVoice 这一代的嗓音是否比已记录的最佳值更值得留下:**达标优先,其次离目标更近**
// (方向化后一个已达标的 97 不该被一个没达标的 96 挤掉,那正是「只取离目标最近」会露出的错);
// 没填目标取绝对值更大的(收集向玩法要的就是极端个体)。
function closerVoice(cur, best, target) {
  if (target != null) {
    const ch = goalReached(cur, target, 'voice')
    const bh = goalReached(best, target, 'voice')
    if (ch !== bh) return ch
    return Math.abs(cur - target) < Math.abs(best - target)
  }
  return Math.abs(cur) > Math.abs(best)
}

function closerWeight(cur, best, target) {
  if (target != null) {
    const ch = goalReached(cur, target, 'weight')
    const bh = goalReached(best, target, 'weight')
    if (ch !== bh) return ch
    return Math.abs(cur - target) < Math.abs(best - target)
  }
  return cur > best
}

// lineStats 汇总一条线:已认领的代数、历代最佳嗓音 / 体重百分位。没认领子代的代不计数。
export function lineStats(line) {
  const goal = (line && line.goal) || {}
  let gens = 0
  let bestVoice = null
  let bestWeight = null
  for (const g of (line && line.gens) || []) {
    const c = g.child
    if (!c) continue
    gens++
    if (bestVoice == null || closerVoice(c.voice, bestVoice, goal.voice)) bestVoice = c.voice
    if (c.weightPct != null && (bestWeight == null || closerWeight(c.weightPct, bestWeight, goal.weightPct))) {
      bestWeight = c.weightPct
    }
  }
  return { gens, bestVoice, bestWeight }
}

// goalBits 把目标拆成四枚徽标。未填的项显示「—」而不是消失 —— 卡片高度才不会一张高一张矮。
// 性格那一枚显示的是 goalNatureLabel:确切性格给名字,「正面加某维」给「加物攻」。
export function goalBits(goal, matrix) {
  const g = goal || {}
  const nat = goalNatureLabel(goal, matrix)
  return [
    { k: 'V', on: g.voice != null, v: g.voice != null ? String(g.voice) : '—', title: '目标嗓音(-100~100),留空表示不关心' },
    { k: 'W', on: g.weightPct != null, v: g.weightPct != null ? `${fmtPct(g.weightPct)}%` : '—', title: '目标体重百分位(0~100),留空表示不关心' },
    { k: '性', on: !!nat, v: nat || '—', title: '目标性格:一个确切性格名,或「性格正面加某维」的一组;留空表示不关心' },
    { k: '♂♀', on: hasGenderGoal(g), v: g.gender || '—', title: '目标性别(♂ / ♀),留空表示不关心 —— 破壳前不可预测,只用于判定达成' },
  ]
}

export const fmtPct = (v) => (v == null ? '—' : Number(v).toFixed(1))

// hasGoal 这条线填过任何一项目标吗(没填的话建议是无意义的:所有组合等价)。
export function hasGoal(goal) {
  const g = goal || {}
  return g.voice != null || g.weightPct != null || hasNatureGoal(g) || hasGenderGoal(g)
}

// hasGenderGoal 填了性别目标吗(镜像后端 BreedingGoal.hasGenderGoal)。
// 与后端同一口径(非空即填了):parseGoal 已把非法值挡在提交之前。
export function hasGenderGoal(goal) {
  const g = goal || {}
  return !!g.gender
}

// —— 谱系:子线链 ——
//
// 换种母会开一条**子线**(见 internal/pet.NewChildLine):线的身份是种母,换了就是另一条线,
// 但她是上一线孵出来的,故用 parentLineId 挂上去,代数也接着数(子线第一代 = 母线末代 + 1)。
// 于是「一条培育史」在库里其实是**一串线**,显示时必须沿链把它们拼回来 —— 否则玩家看到
// 的是几段互不相干的碎片,而那正是换种母最频繁发生的时候(孵出更好的子代拿它接着配)。
//
// 只做**展示**,不改数据:物理合并(把子线并进母线)是另一个带确认的独立操作。

// lineageOf 取这条线所在的整条谱系,按代次从早到晚排好(母线在前、子线在后)。
//
// 只沿 parentLineId 单向往上找祖先、再往下找子孙,两条都是**链**,不存在分叉(一次换种母
// 只产生一条子线);万一数据里出现环(parentLineId 指回来),seen 兜住不死循环。
export function lineageOf(lines, id) {
  const byId = new Map((lines || []).map((l) => [l.id, l]))
  const self = byId.get(id)
  if (!self) return []

  const ups = []
  const seen = new Set([id])
  for (let cur = self; cur && cur.parentLineId; cur = byId.get(cur.parentLineId)) {
    if (seen.has(cur.parentLineId)) break
    seen.add(cur.parentLineId)
    ups.unshift(byId.get(cur.parentLineId))
  }
  const kidsOf = new Map()
  for (const l of lines || []) {
    if (!l.parentLineId) continue
    if (!kidsOf.has(l.parentLineId)) kidsOf.set(l.parentLineId, [])
    kidsOf.get(l.parentLineId).push(l)
  }
  const downs = []
  const walk = (l) => {
    for (const k of kidsOf.get(l.id) || []) {
      if (seen.has(k.id)) continue
      seen.add(k.id)
      downs.push(k)
      walk(k)
    }
  }
  seen.add(id)
  walk(self)
  return [...ups.filter(Boolean), self, ...downs]
}

// chainStats 整条谱系的累计统计:代数相加、历代最佳跨链取最优。
//
// 口径与后端 pet.LineStats 一致(「最佳」按目标算:**达标优先,其次离目标最近**),故这里对
// 每段各算一次再比 —— 不自己重算,免得两处口径分叉。
export function chainStats(lines, id) {
  const chain = lineageOf(lines, id)
  if (!chain.length) return { gens: 0, bestVoice: null, bestWeight: null, extra: 0 }
  let gens = 0
  let bestVoice = null
  let bestWeight = null
  for (const l of chain) {
    const st = lineStats(l)
    gens += st.gens
    bestVoice = pickBest(bestVoice, st.bestVoice, l.goal && l.goal.voice, 'voice')
    bestWeight = pickBest(bestWeight, st.bestWeight, l.goal && l.goal.weightPct, 'weight')
  }
  return { gens, bestVoice, bestWeight, extra: chain.length - 1 }
}

// pickBest 跨段挑最优:填了目标就「达标优先,其次离目标最近」(玩家要的是达标),没填则取
// 绝对值最大的。null 表示这一整段还没有子代记录,不参与比较 —— 拿 0 去比会把「没记录」当成「最差」。
// kind 决定方向(见 goalReached);同一条线里两段用的是同一套目标,故 kind 一致。
function pickBest(cur, next, goal, kind) {
  if (next == null) return cur
  if (cur == null) return next
  if (goal != null) {
    const nh = goalReached(next, goal, kind)
    const ch = goalReached(cur, goal, kind)
    if (nh !== ch) return nh ? next : cur
    return Math.abs(next - goal) < Math.abs(cur - goal) ? next : cur
  }
  return Math.abs(next) > Math.abs(cur) ? next : cur
}

const clamp01 = (v) => (v < 0 ? 0 : v > 1 ? 1 : v)

// goalItem 某一维「离目标还差多少」的统一口径:够到就是「达标」,没够到才说差多少。
//
// 判据与后端 pet.goalHit 完全一致(方向化,见 goalReached;性格那一路见 natureHit / childHits):
// 页面上的「达标」正是后端把这条线置为 done 用的那一个判定,两处不一致会让人以为状态没刷新。
//
// **不区分「超出 / 不足」**:培育只问够没够到,而「超出 1.6pp」与「差 1.6pp」是同一个距离 ——
// 分开说既多一档状态,又会让「重了 1.6」被读成「还差 1.6」(这正是此前那句文案的误导来源)。
//
// 返回 null 表示这一维没填目标或该值未知,不参与展示(与后端「只对填了的项计分」同口径)。
export function goalItem(cur, goal, kind) {
  if (cur == null || goal == null) return null
  const hit = goalReached(cur, goal, kind)
  const diff = Math.abs(cur - goal)
  let text
  if (kind === 'weight') {
    text = hit ? `${fmtPct(cur)}% · 达标` : `${fmtPct(cur)}% · 差 ${fmtPct(diff)}pp`
  } else {
    text = hit ? `${cur} · 达标` : `${cur} · 差 ${diff}`
  }
  return { hit, diff, text }
}

// natureHit 是否已有子代命中目标性格(镜像后端 goalHit 的性格那一项)。
// 单独拎出来:性格是「达成」最常卡住的一项,而它此前在页面上完全没有进度表示 —— 玩家看着
// V/W 都差不多了,却不知道为什么状态还是「进行中」。
export function natureHit(line, goal) {
  const names = goalNatures(goal)
  if (!names.length) return false
  for (const g of (line && line.gens) || []) {
    if (g.child && names.includes(g.child.nature)) return true
  }
  return false
}

// genderHit 是否已有子代性别与目标相符(镜像后端 goalHit 的性别那一项)。
// 与性格同理:它也是「达成」可能卡住的一项,得在进度里有表示。快照缺性别(空串)不算命中。
export function genderHit(line, goal) {
  const g = goal || {}
  if (!g.gender) return false
  for (const gen of (line && line.gens) || []) {
    if (gen.child && gen.child.gender === g.gender) return true
  }
  return false
}

// goalProgress 卡片/详情页的「离目标还有多远」,分**两种展示语言**(没填的维度都不参与,
// 否则只想刷嗓音的线会被没填的体重项拖着走,与后端打分同口径):
//
//   - bars:嗓音 / 体重 —— 它们是有极值的**数值轴**,「离目标多远」说得清,故画进度条;
//     pct = 1 - 归一化差距,达标项直接满格;text 里写的才是「达标 / 差多少」。
//   - flags:性格 / 性别 —— 「命中 / 未命中」两态,没有「快满了」这回事,故**不给进度条**,
//     并排一行旗标(见 .br-flag)。性格显示**完整名字**而不是「加物攻」这种维度缩写:
//     玩家要认的是具体哪几个性格,不是它归在哪一维。
export function goalProgress(line) {
  const goal = (line && line.goal) || {}
  const st = lineStats(line)
  const bars = []
  const flags = []
  const v = goal.voice != null ? goalItem(st.bestVoice, goal.voice, 'voice') : null
  if (v) {
    bars.push({
      k: 'V', cls: 'v', pct: v.hit ? 100 : clamp01(1 - v.diff / VOICE_SPAN) * 100,
      text: v.text, hit: v.hit,
      title: `历代最佳嗓音 ${st.bestVoice},目标 ${goal.voice}`,
    })
  }
  const w = goal.weightPct != null ? goalItem(st.bestWeight, goal.weightPct, 'weight') : null
  if (w) {
    bars.push({
      k: 'W', cls: 'w', pct: w.hit ? 100 : clamp01(1 - w.diff / 100) * 100,
      text: w.text, hit: w.hit,
      title: `历代最佳体重百分位 ${fmtPct(st.bestWeight)}%,目标 ${fmtPct(goal.weightPct)}%`,
    })
  }
  if (hasNatureGoal(goal)) {
    const hit = natureHit(line, goal)
    flags.push({
      k: '性格', text: goalNatures(goal).join('/'), hit,
      title: hit ? '已有子代命中目标性格' : '还没有子代命中目标性格 —— 这是「未达成」最常见的原因',
    })
  }
  if (hasGenderGoal(goal)) {
    const hit = genderHit(line, goal)
    flags.push({
      k: '性别', text: goal.gender, hit,
      title: hit ? `已有子代是 ${goal.gender}` : `还没有子代是 ${goal.gender} —— 性别破壳前不可预测,只能多孵几只`,
    })
  }
  return { bars, flags }
}

// —— 同一只达标(与「各维最佳」是两个问题)——
//
// lineStats 回答的是「每一维分别推到多远了」,那两列的最优**可能来自不同代**(这一代嗓音好、
// 那一代体重好),它适合收集向玩法;而培育向要问的是「有没有一只同时满足我的全部目标」。
// 此前页面只把前者画在目标旁边,于是「各维都差不多了」被读成「同一只快达标了」。下面这几个
// 函数补上后一个问题,让页面能直接说出「还差哪一项」。

// childHits 某只子代对各项已填目标的逐项命中情况(镜像后端 pet.goalHit,只列填了的维度)。
export function childHits(child, goal) {
  const g = goal || {}
  const hits = {}
  if (g.voice != null) hits.voice = goalReached(child.voice, g.voice, 'voice')
  if (g.weightPct != null) {
    hits.weight = child.weightPct != null && goalReached(child.weightPct, g.weightPct, 'weight')
  }
  if (hasNatureGoal(g)) hits.nature = goalNatures(g).includes(child.nature)
  // 性别破壳后是**确定**的,故直接比;快照缺性别(空串)不算命中。
  if (hasGenderGoal(g)) hits.gender = child.gender === g.gender
  return hits
}

// bestChild 最接近同时达标的那一只子代:先比未命中项数(越少越好),再比归一化距离(越小越好)。
// 只排序、不改判定 —— 「是否已达成」始终以后端 ReachGoal 为准(它要求同一只全项命中)。
// 没有已认领子代、或目标为空时返回 null(没有可比的对象)。
export function bestChild(line) {
  const goal = (line && line.goal) || {}
  if (!hasGoal(goal)) return null
  let best = null
  for (const g of (line && line.gens) || []) {
    const c = g.child
    if (!c) continue
    const hits = childHits(c, goal)
    const miss = Object.keys(hits).filter((k) => !hits[k]).length
    const dist = goalDistance(c, goal)
    if (!best || miss < best.miss || (miss === best.miss && dist < best.dist)) {
      best = { gen: g.gen, child: c, hits, miss, dist }
    }
  }
  return best
}

// goalDistance 一只子代离全部已填目标的归一化距离(0 最好):接近后端 Score 的分子,只用于排序。
// 口径:嗓音 / VOICE_SPAN、体重 / 100、性格按命中与否记 0/1;**已达标的那一维记 0**(达标即满分,
// 与后端 Score 一致 —— 否则一个已达标的 100 会因为「离目标远」把更好的组合排到后面)。
// 值缺失按最差(1)算,免得「没测出来」反而排到前面。
function goalDistance(child, goal) {
  const g = goal || {}
  let sum = 0
  let n = 0
  if (g.voice != null) {
    n++
    const v = child.voice ?? 0
    if (!goalReached(v, g.voice, 'voice')) sum += Math.abs(v - g.voice) / VOICE_SPAN
  }
  if (g.weightPct != null) {
    n++
    if (child.weightPct == null) sum += 1
    else if (!goalReached(child.weightPct, g.weightPct, 'weight')) sum += Math.abs(child.weightPct - g.weightPct) / 100
  }
  if (hasNatureGoal(g)) {
    n++
    sum += goalNatures(g).includes(child.nature) ? 0 : 1
  }
  // 性别按命中与否记 0/1(与性格同款)。注意它只影响这里的**排名**,不影响后端建议打分 ——
  // 那边评的是「还没孵出来的预期」,而性别在破壳前不可预测(见后端 BreedingGoal.Gender)。
  if (hasGenderGoal(g)) {
    n++
    sum += child.gender === g.gender ? 0 : 1
  }
  return n === 0 ? 0 : sum / n
}

// lineAvatar 卡片头像:这条线里第一张能用的头像(母本优先,其次子代)。
// 线上的图都是快照(亲本被放生也不影响),故不会因为宠物没了而空白。
//
// 一代都还没有的线(刚手动建、还没孵过)没有快照可拿 —— 那种退回**这个品种的蛋**
// (chains 里按品种找到的那一项的 egg):卡片上写着品种名,配一颗蛋正合「这条线要孵的是
// 什么」,也不至于让人以为这条线没建全(此前是留一个空的占位框)。等第一代记下来就换回
// 母本头像 —— 那才是这条线真正在用的种母,比一颗蛋具体得多。
//
// 品种按 chainKey 找(有链用链 id):线的 species 是建线那一刻的形态名,选举时可能进化成
// 链上另一个阶段(先记成阿米亚特、后记成罗隐),拿名字比对不上,拿链 id 才对得上。
export function lineAvatar(line, chains) {
  // 后端下发的 mother 是这条线**当前**的种母(见 payload 的 Mother):线的身份就是她,
  // 同一个品种可以同时有几条线(几个窝、几只母本各孵各的),卡片全靠她区分 —— 故优先用
  // 她的头像,而不是 gens[0] 那位可能已经是好几代之前的母本。
  if (line && line.mother && line.mother.img) return line.mother.img
  for (const g of (line && line.gens) || []) {
    if (g.mother && g.mother.img) return g.mother.img
    if (g.child && g.child.img) return g.child.img
  }
  for (const g of (line && line.pending) || []) {
    if (g.mother && g.mother.img) return g.mother.img
  }
  const c = chainOf(chainKey(line), chains)
  // 蛋图查不到时退回链首头像(理由见 Breeding.jsx 的 chainItems):查不到蛋不等于孵不出来。
  return (c && (c.egg || c.img)) || ''
}

// —— 目标性格的选项 ——

// flattenNatures 把 /api/name-options 的 6×6 性格方阵拍平成一维性格名。
//
// 后端下发的是**方阵**(行=加什么、列=减什么,供宠物页画筛选矩阵),而目标性格是单选。
// 此前直接把方阵当字符串数组铺进 datalist,每个候选项其实是一个**数组**,渲染出来是
// 「沉默,平和,忧郁,…」这种拼接串 —— 玩家选中它就存进了 goal.nature,永远匹配不上任何
// 真实性格,后端性格维度的建议(Predict 里的 natureP)因此一直是失效的。
//
// 对角线是空格,顺带滤掉:矩阵里空值重复出现,不去重会让下拉里多出一堆空条目。
export function flattenNatures(matrix) {
  const out = []
  const seen = new Set()
  for (const row of matrix || []) {
    for (const n of row || []) {
      if (n && !seen.has(n)) { seen.add(n); out.push(n) }
    }
  }
  return out
}

// natureOptions 目标性格下拉的选项:空值表示「不关心」。
//
// 当前值不在候选里时(历史坏值,如上面那种拼接串)额外补一项并保留原值显示 —— 直接丢掉
// 会让玩家那条线的旧目标无声消失,而这里只要一保存就真的没了,该由玩家自己决定改不改。
export function natureOptions(value, natures) {
  const list = natures || []
  const opts = [{ value: '', label: '不关心' }, ...list.map((n) => ({ value: n, label: n }))]
  const cur = String(value == null ? '' : value).trim()
  if (cur && !list.includes(cur)) opts.push({ value: cur, label: `${cur}（旧值,建议重选）` })
  return opts
}

// NATURE_DIMS 六维顺序:必须与后端 nature_effect 的维度编号 1-6(1生命 2物攻 3魔攻 4物防 5魔防
// 6速度)以及宠物页 NatureMatrix 的 STAT_NAMES 一致 —— 下面按**行号**取「该维 +10% 的 5 个性格」,
// 顺序错了就会把「加物攻」存成一整行别的性格。三方同源,改一处务必改另两处。
export const NATURE_DIMS = ['生命', '物攻', '魔攻', '物防', '魔防', '速度']

// natureDimRows 把方阵的**行**转成「性格正面加某维」的候选:行 i = 维度 i+1 的 +10% 那 5 个名字。
// 空格(游戏内不存在的组合)滤掉;整行为空时这一维不可选(名字库缺这一维的数据)。
export function natureDimRows(matrix) {
  return NATURE_DIMS
    .map((label, i) => ({ dim: i + 1, label, names: ((matrix && matrix[i]) || []).filter(Boolean) }))
    .filter((r) => r.names.length > 0)
}

// goalNatures 目标性格**集合**(nature 与 natureIn 的并集,去重去空),与后端
// pet.BreedingGoal.natures 同口径 —— 两边对「填了什么」的判断必须完全一致,否则会出现页面显示
// 「没填性格」而事后端按填了算分(或反过来)。
export function goalNatures(goal) {
  const g = goal || {}
  const out = []
  const seen = new Set()
  const add = (n) => {
    const t = String(n == null ? '' : n).trim()
    if (t && !seen.has(t)) { seen.add(t); out.push(t) }
  }
  add(g.nature)
  for (const n of g.natureIn || []) add(n)
  return out
}

export function hasNatureGoal(goal) { return goalNatures(goal).length > 0 }

// goalNatureLabel 目标性格在页面上显示成什么:
//   精确一个 → 那个名字;一组恰好等于方阵的某一行 → 「加物攻」;否则 → 「N 种性格」。
//
// 为什么反查方阵而不是另存一个「哪一维」:后端存的就是名字集合(见 docs/data.md 的培育一节 ——
// 快照里只有性格**名**,判定要拿名字比)。方阵对不上时(游戏改了性格表)退化成列出数量,
// 而不是显示一个早已不对的维度名。
export function goalNatureLabel(goal, matrix) {
  const names = goalNatures(goal)
  if (!names.length) return ''
  if (names.length === 1 && (goal || {}).nature) return names[0]
  const row = natureDimRows(matrix).find(
    (r) => r.names.length === names.length && r.names.every((n) => names.includes(n)),
  )
  return row ? `加${row.label}` : `${names.length} 种性格`
}

// sameNames 两组名字是否等价(**顺序无关**:并集与方阵行来的顺序可能不同,而它们说的是同一件事)。
function sameNames(a, b) {
  if (a.length !== b.length) return false
  const s = new Set(b)
  return a.every((x) => s.has(x))
}

// —— 目标输入(输入框里是字符串,提交前才转成后端 BreedingGoal 的三项)—— 

// clampNum 把输入框里的字符串转成 [lo, hi] 区间内的数;空串或非数一律返回 undefined。
//
// 不用 0 兜底:0 在嗓音上是个**真实目标**(「一声不出」),把「没填」和「填了 0」混成一样,
// 玩家清空输入框时就会被悄悄改成去刷 0 音。
function clampNum(s, lo, hi) {
  const t = String(s == null ? '' : s).trim()
  if (t === '') return undefined
  const n = Number(t)
  if (!Number.isFinite(n)) return undefined
  return n < lo ? lo : n > hi ? hi : n
}

// parseGoal 输入框值 → 后端目标(键名对齐 pet.BreedingGoal 的 json tag)。
// 值为 undefined 的键会被 JSON.stringify 丢掉,与后端 *int32 / *float64 的 nil 同义。
export function parseGoal(v) {
  const g = v || {}
  const out = {}
  const voice = clampNum(g.voice, -100, 100)
  if (voice !== undefined) out.voice = Math.round(voice)
  const w = clampNum(g.weightPct, 0, 100)
  if (w !== undefined) out.weightPct = Math.round(w * 100) / 100
  const n = String(g.nature == null ? '' : g.nature).trim()
  if (n) out.nature = n
  // 「性格正面加某维」是一**组**名字(见 goalNatures)。顺序不重要,但要在这里定下来 ——
  // 同一次编辑里重复点同一维不该把它存成两条一样的名字。
  const list = []
  for (const x of g.natureIn || []) {
    const t = String(x == null ? '' : x).trim()
    if (t && !list.includes(t)) list.push(t)
  }
  if (list.length) out.natureIn = list
  // 性别只认 ♂ / ♀:脏值(手改的请求、旧版本)在这里就丢掉,而不是提交上去永远匹配不上任何子代。
  const gd = String(g.gender == null ? '' : g.gender).trim()
  if (gd === '♂' || gd === '♀') out.gender = gd
  return out
}

// goalToInput 后端目标 → 输入框值(受控输入框只认字符串;natureIn 保持数组)。
export function goalToInput(goal) {
  const g = goal || {}
  return {
    voice: g.voice == null ? '' : String(g.voice),
    weightPct: g.weightPct == null ? '' : String(g.weightPct),
    nature: g.nature || '',
    natureIn: Array.isArray(g.natureIn) ? g.natureIn.slice() : [],
    gender: g.gender || '',
  }
}

// sameGoal 两个目标是否等价。经 parseGoal 归一后再比 —— 直接比对象会被键顺序与
// 「空串 / undefined / 缺键」这几种同义写法骗到,把没改过的目标报成「有改动」。
//
// 性格比的是**集合**(与后端 BreedingGoal 的口径一致),而不是逐字段比:
// `{nature: 固执}` 与 `{natureIn: [固执]}` 说的是同一件事,按字段比会被判成「改了」,
// 于是「保存目标」按钮一直亮着、页面上写着「有改动未保存」,而按下去什么也不会变。
// 顺序也不比 —— 并集合并后的顺序与方阵行来的顺序未必相同。
export function sameGoal(a, b) {
  const x = parseGoal(goalToInput(a))
  const y = parseGoal(goalToInput(b))
  return x.voice === y.voice && x.weightPct === y.weightPct
    && x.gender === y.gender
    && sameNames(goalNatures(x), goalNatures(y))
}

// stepDelta 与上一代的**同一个维度**比,是否更接近目标(返回 'up' / 'down' / '')。
//
// 判据是「**达标优先,其次离目标更近**」(与 lineStats / Score 同一套口径):一个已达标的 97
// 相对一个未达标的 96 就是 up,哪怕两者离目标一样远。方向由目标落在轴的哪半边决定(见
// goalReached),故 kind 必须传对。没定目标时按「更极端」算。
export function stepDelta(cur, prev, target, kind) {
  if (prev == null || cur == null) return ''
  if (target != null) {
    const ch = goalReached(cur, target, kind)
    const ph = goalReached(prev, target, kind)
    if (ch !== ph) return ch ? 'up' : 'down'
    const dc = Math.abs(cur - target)
    const dp = Math.abs(prev - target)
    if (dc === dp) return ''
    return dc < dp ? 'up' : 'down'
  }
  const ac = Math.abs(cur)
  const ap = Math.abs(prev)
  if (ac === ap) return ''
  return ac > ap ? 'up' : 'down'
}

// —— 品种(进化链,与后端 pet.ChainRef 同口径)——

// chainKey 品种在界面上的取值键:有链用链 id,无链用形态名。
//
// 与后端 ChainRef 的两条匹配分支一一对应(见 internal/pet/breeding.go)。不用 evo 一号到底:
// 无链形态(首领 / 活动 / 内部占位形态,实测 1147 个形态里 526 个)的 evo 是 0,而「0」区分不了
// 几十只无链品种 —— 它们会撞在同一个取值上,下拉里选谁都是同一项。
export function chainKey(x) {
  if (!x) return ''
  return x.evo ? `e:${x.evo}` : `n:${x.species || ''}`
}

// chainOf 由取值键还原成品种标识({evo, species});候选里没有这一项时返回 null。
//
// 对不上的情形是真实存在的:线的品种来自旧数据(链口径之前建的老线只存了形态名)或别的设备,
// 而下拉的候选只列**库里现在有的** —— 由调用方兜底(通常保持原样不动),不要在这里猜一个。
export function chainOf(key, chains) {
  return (chains || []).find((c) => chainKey(c) === key) || null
}

// —— 一代的生命周期(与后端 pet.Generation 的 eggGid / childGid 同口径)——
//
// 收蛋即记之后,「待认领」这一格其实装着两种完全不同的处境,得分开说清楚:
//
//   incubating 待孵 —— 已收蛋、还没孵(有 eggGid、无 childGid)。收蛋即记的产物:
//              双亲那一刻最全,玩家刚从窝里拿完蛋,培育页立刻就能看到这一代 ——
//              这就是「收蛋时就有反馈」要的那条。它可能永远停在待孵(蛋送人、不孵了),
//              故要给「丢弃这一代」的出口。
//   claim     待认领 —— 破壳已确认(有 childGid),子代的快照还没补上(它还没进背包,
//              或那一包漏抓了)。gid 已经落库,跨会话、重启都丢不了。
//   legacy    老数据 —— 两个字段都没有(本次改动之前建的 pending),按待认领渲染:
//              那是升级前的唯一形态,玩家已经认熟了那种展示,不该因为升级就变样。
export const genState = (g) => (g?.childGid ? 'claim' : g?.eggGid ? 'incubating' : 'legacy')

// sameParents 这一代与上一条记录的双亲是否相同。
//
// 嗓音 = 双亲均值向零取整(见 docs/data.md 3.6)是**确定值**,所以双亲相同意味着
// 这一胎的嗓音与上一胎**必然一模一样** —— 再孵只是在掷体重与性格的随机,不是在推进。
// 刷嗓音时这恰恰是最该看见的一句话:否则玩家会以为多孵几胎总能更高。
//
// 返回 'both'(母 + 父都相同) / 'mother'(母相同、父本未定或缺失) / ''(不同)。
// 串窝时父本有多个候选、实际用了谁只有玩家自己知道,故不按「相同」处理 ——
// 说错比不说更糟(他可能照着「嗓音一样」的提示放弃了一个其实不同的组合)。
export function sameParents(g, prev) {
  if (!g || !prev) return ''
  const m = g.mother && g.mother.gid
  const pm = prev.mother && prev.mother.gid
  if (!m || m !== pm) return ''
  const f = (g.father && g.father.gid) || 0
  const pf = (prev.father && prev.father.gid) || 0
  if (f && f === pf) return 'both'
  // 父本都没定:串窝(多候选)说不准,只有候选不超过一个时才算「母本相同」
  if (!f && !pf && (g.fathers || []).length <= 1 && (prev.fathers || []).length <= 1) return 'mother'
  return ''
}

// pendCounts 把待处理的代按两态分开数。列表页与详情页顶部各用一次 ——
// 「3 代待认领」这种笼统说法会让人以为都在等它认领,而其中多半只是蛋还没孵。
export function pendCounts(list) {
  let incubating = 0
  let claim = 0
  for (const g of list || []) {
    if (genState(g) === 'incubating') incubating++
    else claim++
  }
  return { incubating, claim }
}
