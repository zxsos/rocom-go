// 页面入场动效的静态校验(web/src/styles/motion.css)。
//
// 为什么需要它:这类编排有三个**静默失效**的坑,编译与构建都发现不了:
//
//   1. 选择器挂错了元素 —— 曾把花种的入场动画挂到 .flower-chip 上,
//      而那是卡片**内部**的小标签(Lv / 绑定宠物 / 挑战次数),一张卡有好几个。
//      结果不是卡片入场,而是每张卡连续闪好几下。选择器在源码里"存在",
//      所以任何"这个 class 用到了吗"的检查都会通过 —— 只有明确知道
//      **它必须是列表项**才抓得住。
//   2. 新增动画忘了加进 prefers-reduced-motion 块 —— 无障碍缺陷,
//      且不会有任何报错。
//   3. nth-child 的基准不是列表容器 —— 延迟整体错位,
//      表现为"动画慢半拍",极难归因。
//
// 本脚本覆盖 1(人工核对清单)与 2(自动断言);3 靠文件注释里记录的
// 容器名(见 TABLE),改动容器时请一并更新那里。
import { readFileSync } from 'node:fs'

const CSS = 'src/styles/motion.css'
const css = readFileSync(CSS, 'utf8')

let fail = 0
const ok = (cond, label, extra = '') => {
  console.log(`  ${cond ? 'OK  ' : 'FAIL'} ${label}${extra ? ' — ' + extra : ''}`)
  if (!cond) fail++
}

// 每条入场动效:[CSS 选择器, 父容器, 用于 grep 的 class, 说明]
// ⚠️ 改容器结构时同步更新这里 —— 它是「nth-child 基准」的唯一记录。
//
// 第三列单独给出是因为选择器未必以 class 结尾:
// `.boxmap-grid > *` 的末段是 `*`,`.pets tbody tr` 末段是元素选择器 `tr`,
// 拿末段去拼正则会得到 `\t...`(`\t` 被当成 tab)这类错误。
const TABLE = [
  ['.flower-card', '.flower-grid', 'flower-card', '花种卡片(非 .flower-chip!那是卡内标签)'],
  ['.egg-card', '(列表容器)', 'egg-card', '精灵蛋卡片'],
  ['.merchant-item', '(列表容器)', 'merchant-item', '远行商人商品'],
  ['.hb-card', '.hb-list', 'hb-card', '图鉴炫彩卡片'],
  ['.admin-bar-row .admin-bar-fill', '.admin-bars', 'admin-bar-fill', '管理员柱状图(柱体)'],
  ['.rank-row', '.rank-list', 'rank-row', '排行榜行'],
  ['.boxmap-grid > *', '.boxmap-grid', 'boxmap-grid', '盒子地图格子'],
  ['.pets tbody tr', 'table.pets', 'pets', '宠物列表表格行'],
]
const esc = (s) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')

console.log('=== 入场动效校验 motion.css ===\n')

// —— 1. 每个选择器都在 CSS 里定义了动画 ——
console.log('[1] 每条编排都有动画定义')
for (const [sel, , , desc] of TABLE) {
  // 用完整选择器匹配(末段可能是 `*` 或元素选择器,不能只取最后一段)
  const re = new RegExp(`${esc(sel)}\\s*\\{[^}]*animation:`)
  ok(re.test(css), sel, re.test(css) ? desc : '未定义 animation')
}

// —— 2. reduced-motion 必须覆盖全部动画选择器 ——
// 这是本脚本最有价值的一条:新增动画忘记降级是最常见的无障碍疏漏。
console.log('\n[2] prefers-reduced-motion 覆盖全部选择器')
const rmBlock = /@media \(prefers-reduced-motion: reduce\)\s*\{([\s\S]*?)\n\}/.exec(css)
ok(!!rmBlock, '存在 reduced-motion 块')
if (rmBlock) {
  // 块里是逗号分隔的选择器列表,末项带 `{`,需去掉
  const listed = rmBlock[1]
    .replace(/\{[\s\S]*$/, '') // 去掉末项之后的声明体
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean)
  for (const [sel] of TABLE) {
    ok(listed.includes(sel), `${sel} 已降级`,
      listed.includes(sel) ? '' : '降级块里没有它 —— 无障碍遗漏')
  }
}

// —— 3. 选择器在 JSX 源码中确实存在 ——
console.log('\n[3] 选择器在页面源码中存在')
const { execSync } = await import('node:child_process')
for (const [sel, , cls] of TABLE) {
  let found = ''
  try {
    found = execSync(
      `grep -rl --include=*.jsx "${cls}" src/pages src/components 2>/dev/null | head -1`,
      { encoding: 'utf8' },
    ).trim()
  } catch {
    /* grep 无匹配时返回非零退出码,视为未找到 */
  }
  ok(!!found, `${sel} → 源码含 "${cls}"`, found || '源码里找不到')
}

// —— 4. 时长与缓动都用项目变量,不硬编码 ——
console.log('\n[4] 时长/缓动复用项目变量(不另造体系)')
const animLines = [...css.matchAll(/animation:\s*([^;]+);/g)].map((m) => m[1])
// 排除降级块里的 animation: none —— 它没有时长,不该参与变量检查
const real = animLines.filter((l) => !l.includes('none'))
ok(real.every((l) => l.includes('var(--dur-')),
  `${real.length} 条 animation 都用 var(--dur-*)`,
  real.filter((l) => !l.includes('var(--dur-')).join(' | ') || '')

// —— 5. stagger 都有上限(不能无限递增)——
console.log('\n[5] stagger 有封顶(nth-child(n+N) 统一延迟)')
const groups = css.split(/\/\* ——— \d\./).slice(1)
for (const g of groups) {
  const name = /\.([a-z-]+)/.exec(g)?.[1] || '?'
  const capped = /nth-child\(n \+ \d+\)/.test(g)
  const hasStagger = /nth-child\(\d+\)/.test(g)
  // 有 stagger 就必须有封顶,否则列表长时末尾会等太久
  ok(!hasStagger || capped, `.${name} stagger 已封顶`,
    hasStagger && !capped ? '有递增但没封顶 —— 长列表末尾会明显延迟' : '')
}

console.log(fail === 0 ? '\n=== 全部通过 ===' : `\n=== ${fail} 项失败 ===`)
process.exit(fail === 0 ? 0 : 1)
