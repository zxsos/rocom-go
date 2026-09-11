// 「长按 → 孵蛋配种」深链的验收(见 pages/breeding/pets.js 的 chainOfPet / narrowChainsByEggGroups
// 与 Breeding.jsx 里那两处 useEffect)。
//
//   node scripts/verify-breeding-newlink.mjs
//
// 深链做的是「进培育页 → 开新建线 → 按蛋组收窄品种候选 → ♀ 预选她的品种」,每一步错了都**不报错**:
//   - 收窄写反(留下蛋组不搭的品种)→ 玩家建完线才发现永远配不出候选;
//   - 「链首没配对蛋组」的品种被一起滤掉 → 少掉的正是合法可配的品种(宁多勿漏);
//   - 认品种时拿宠物**当前形态**去比候选的 base(那是**链首**)→ 已进化的宠(罗隐 vs 阿米亚特)
//     一律认不出来,预选静默失效(玩家只会觉得「怎么没帮我填」)。
//
// ⚠️ 已做变异测试(每条断言都验证过会红):
//    - 收窄时用 `every` 代替 `some`      → 「共一个即保留」断言红
//    - 收窄时对「候选项没带蛋组」返回 false → 「宁多勿漏」断言红
//    - chainOfPet 用 steps[0] 之外的一项   → 「按链首认品种」断言红
//    - chainOfPet 拿 pet.base 直接比      → 同上(进化过的宠认不出来)
import { createServer } from 'vite'

const server = await createServer({
  root: process.cwd(), logLevel: 'error',
  server: { middlewareMode: true, hmr: false }, optimizeDeps: { noDiscovery: true },
})
const { chainKey, chainOf, chainOfPet, narrowChainsByEggGroups } =
  await server.ssrLoadModule('/src/pages/breeding/pets.js')
await server.close()

const R = []
const eq = (name, got, want) => R.push({ name, got, want, ok: JSON.stringify(got) === JSON.stringify(want) })

// 候选样本:三种情形 —— 与目标共蛋组、蛋组完全不搭、链首没配蛋组(实测 255 个品种里有 35 个)。
const chains = [
  { evo: 3003, species: '火花', base: 3003, label: '火花（焰火/火神/烈火战神）', eggGroups: ['魔力', '巨灵'] },
  { evo: 4001, species: '水蓝蓝', base: 4001, label: '水蓝蓝（波波拉/水灵）', eggGroups: ['海洋'] },
  { evo: 5001, species: '迪莫', base: 5001, label: '迪莫', eggGroups: [] },
  { evo: 0, species: '首领猫', base: 6001, label: '首领猫' }, // eggGroups 字段缺失
]

// —— 1. 按蛋组收窄 ——
eq('没有蛋组条件 → 不筛', narrowChainsByEggGroups(chains, []).length, 4)
eq('undefined 条件 → 不筛', narrowChainsByEggGroups(chains, undefined).length, 4)
eq('共一个蛋组即保留',
  narrowChainsByEggGroups(chains, ['巨灵']).map((c) => c.species),
  ['火花', '迪莫', '首领猫'])
eq('一个都不共的品种被滤掉',
  narrowChainsByEggGroups(chains, ['巨灵']).some((c) => c.species === '水蓝蓝'), false)
// 两个蛋组都要认(「有一个相同就行」,不是「全部相同」——后者是列表筛选那一项的口径)
eq('目标两个蛋组各自都能对上',
  narrowChainsByEggGroups(chains, ['海洋', '巨灵']).map((c) => c.species),
  ['火花', '水蓝蓝', '迪莫', '首领猫'])
// 「链首没配蛋组」不等于「配不上」:滤掉它们会让合法的品种从下拉里静默消失
eq('候选项缺蛋组时保留(宁多勿漏)',
  narrowChainsByEggGroups(chains, ['巨灵']).filter((c) => !(c.eggGroups || []).length).map((c) => c.species),
  ['迪莫', '首领猫'])
eq('库里一个都不共时不返回 null 而是空数组', narrowChainsByEggGroups(chains, ['妖精']).map((c) => c.species), ['迪莫', '首领猫'])

// —— 2. 按进化链阶段表认这只宠物的品种 ——
// 已进化的宠(火神,当前形态 petbase 3006)拿到的阶段表里,链首是火花(3003)—— 候选里记的 base 也是 3003。
const stepsOfEvolved = [{ petbase: 3003, name: '火花', stage: 1 }, { petbase: 3032, name: '焰火', stage: 2 }, { petbase: 3006, name: '火神', stage: 3 }]
eq('按链首认品种(进化过也能认)', (chainOfPet(chains, stepsOfEvolved) || {}).species, '火花')
eq('链首的取值键在候选里能还原', chainKey(chainOfPet(chains, stepsOfEvolved)), 'e:3003')
eq('取值键能还原成候选项(下拉的源与取值同一套口径)',
  (chainOf(chainKey(chainOfPet(chains, stepsOfEvolved)), chains) || {}).base, 3003)
eq('单形态(阶段表只有自己)也认',
  (chainOfPet(chains, [{ petbase: 4001, name: '水蓝蓝', stage: 1 }]) || {}).species, '水蓝蓝')
eq('这条链不在候选里 → null', chainOfPet(chains, [{ petbase: 9999, name: '库里没有', stage: 1 }]), null)
eq('阶段表没拿到 → null(不猜)', chainOfPet(chains, []), null)
eq('阶段表 undefined → null', chainOfPet(chains, undefined), null)

// —— 3. 预选之后,取值键必须落在**收窄后的候选**里(否则下拉显示为空 = 静默没选上) ——
const pickChains = narrowChainsByEggGroups(chains, ['巨灵'])
const prefill = chainOfPet(pickChains, stepsOfEvolved)
eq('♀ 预选的品种确实在收窄后的候选里', !!chainOf(chainKey(prefill), pickChains), true)

let bad = 0
for (const r of R) {
  if (r.ok) { console.log(`  ✓ ${r.name}`) } else {
    bad++
    console.log(`  ✗ ${r.name}  得到 ${JSON.stringify(r.got)}  期望 ${JSON.stringify(r.want)}`)
  }
}
console.log(`\n${bad ? `✗ ${bad} 条不符` : `✓ 全部 ${R.length} 条通过`}`)
process.exit(bad ? 1 : 0)
