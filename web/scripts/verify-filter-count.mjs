// 筛选侧栏「已选条件计数」的验收(见 filters.js 的 countPicked)。
//
//   node scripts/verify-filter-count.mjs
//
// 锁住的是**计数口径**,不是渲染:侧栏顶部状态条与四个分组标题上的角标都靠它,
// 数错了就是「显示 3 项但实际生效 4 条」这类静默错误 —— 页面照常显示,
// 用户却按错误的数字判断自己设了什么。
//
// 三条不变量:
//   1. 空筛选 = 0 —— 没设条件时不该冒出角标;
//   2. 排序/分页**不计数** —— 它们不改变「有哪些」,只改变「怎么排/一页几只」;
//      若把它们算进去,用户只是翻到第 2 页就会看到「已选 2 项」,纯属误导;
//   3. 各分组之和 == 总数,且性格的单选/多选两种存法只算一条。
//
// ⚠️ 已做变异测试(每条断言都验证过会红):
//    - 把 sort/page 也计入            → 断言 2 红
//    - 性格 nature 与 natureIn 各算一条 → 断言 3 红
//    - 去掉 types 的 length(只算 0/1)  → 断言 3 红
import { createServer } from 'vite'

const server = await createServer({
  root: process.cwd(), logLevel: 'error',
  server: { middlewareMode: true, hmr: false }, optimizeDeps: { noDiscovery: true },
})
const { countPicked, DEFAULT_FILTER, sanitizeFilter } = await server.ssrLoadModule('/src/pages/pet-list/filters.js')
await server.close()

const eq = (name, got, want) => ({ name, got, want, ok: JSON.stringify(got) === JSON.stringify(want) })
const R = []

// 1. 默认筛选(只有 page/pageSize/sort/order)必须全 0
const d = countPicked(DEFAULT_FILTER)
R.push(eq('默认筛选全 0', d, { look: 0, gift: 0, body: 0, from: 0, total: 0 }))

// 2. 排序/分页/搜索等「不改变有哪些」的项不计数。
//    这是最容易误加的一类:它们都在 filter 里,一眼看去都像「筛选条件」。
const paging = {
  ...DEFAULT_FILTER, page: 7, pageSize: 100, sort: 'level', order: 'desc',
}
R.push(eq('排序/分页不计数', countPicked(paging), { look: 0, gift: 0, body: 0, from: 0, total: 0 }))

// 3. 分组归属与求和:每组各放一条,总数必须是 4 且分组各 1
const oneEach = {
  types: ['火'],           // 外观
  talentRank: '了不起的天分', // 资质
  medalBig: '1',           // 体型·声音
  box: '13-性格1',          // 来源
}
R.push(eq('四组各一条', countPicked(oneEach), { look: 1, gift: 1, body: 1, from: 1, total: 4 }))

// 4. 多选系别按个数算(选 3 个系 = 3 条,不是 1 条)
R.push(eq('系别多选按个数', countPicked({ types: ['火', '龙', '萌'] }).look, 3))

// 4b. 蛋组多选同口径(改单值 eggGroup → 数组 eggGroups 后,若仍按 0/1 算,
//     选 3 组只会显示「已选 +1」,用户看不到自己设了几条)
R.push(eq('蛋组多选按个数', countPicked({ eggGroups: ['巨灵', '天空'] }).from, 2))
R.push(eq('蛋组单选', countPicked({ eggGroups: ['巨灵'] }).from, 1))

// 5. 性格:单选 nature 与多选 natureIn 是同一条件的两种存法,只算一条
R.push(eq('性格单选', countPicked({ nature: '固执' }).gift, 1))
R.push(eq('性格多选', countPicked({ natureIn: '固执,顽皮,大胆' }).gift, 1))
// 两者同时存在(前端会以 natureIn 为准)仍只算一条,不能加成 2
R.push(eq('性格单选+多选共存仍算 1', countPicked({ nature: '固执', natureIn: '固执,顽皮' }).gift, 1))

// 6. 奖牌特征 4 项全开 = 4 条(可多选,多选=同时满足,故各项各算一条)
R.push(eq('奖牌特征全开', countPicked({
  medalBig: '1', medalSmall: '1', medalHigh: '1', medalLow: '1',
}).body, 4))

// 7. 空值/伪值不算:'' 与 0 与 undefined 都表示「未设」
R.push(eq('空串不计数', countPicked({ gender: '', box: '', catchRange: '' }).total, 0))
R.push(eq('undefined 不计数', countPicked({ gender: undefined, types: undefined }).total, 0))

// 8. 旧版单值 eggGroup 必须迁到 eggGroups —— 不迁的后果是**静默丢条件**:
//    面板上没有一个蛋组显示为选中,而用户以为自己还筛着,列表却悄悄多出一堆。
R.push(eq('旧 eggGroup 迁为数组', sanitizeFilter({ ...DEFAULT_FILTER, eggGroup: '巨灵' }, DEFAULT_FILTER).eggGroups, ['巨灵']))
R.push(eq('旧 eggGroup 为空则不留键', 'eggGroup' in sanitizeFilter({ ...DEFAULT_FILTER, eggGroup: '' }, DEFAULT_FILTER), false))
R.push(eq('已是数组则原样', sanitizeFilter({ ...DEFAULT_FILTER, eggGroups: ['巨灵'] }, DEFAULT_FILTER).eggGroups, ['巨灵']))

let bad = 0
for (const r of R) {
  if (r.ok) { console.log(`  ✓ ${r.name}`) } else {
    bad++
    console.log(`  ✗ ${r.name}  得到 ${JSON.stringify(r.got)}  期望 ${JSON.stringify(r.want)}`)
  }
}
console.log(`\n${bad ? `✗ ${bad} 条不符` : `✓ 全部 ${R.length} 条通过`}`)
process.exit(bad ? 1 : 0)
