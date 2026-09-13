// 「图鉴」家族主题(浅色 handbook + 暗色 handbook-dark…)的静态校验。
//
//   node scripts/verify-theme-handbook.mjs
//
// 为什么需要它:加一套主题有一批**全是静默**的失败形态,构建、lint、其余 verify 脚本
// 一个都抓不到:
//
//   1. 覆写块漏了一个变量 —— 该变量在本主题下回落到默认主题的值,页面上某块突然变了色,
//      而**不报任何错**。这是下面 [2] 逐名比对存在的理由。
//   2. 覆写块收了不该收的变量(--c-* 语义色 / --z-* 标度 / 动效与字体令牌)——
//      语义色一变,玩家靠颜色认的奖牌血脉就会误认;标度一变,浮层层级整体错乱。
//   3. 对比度不够 —— 暗色主题最常出的两个坑:亮金强调色配白字(1.86:1)、
//      次要文字在深底上糊掉。这**只能算**,看不出来也测不出来,[8] 就是干这个的。
//   4. 主题名解析退回二值(「是不是深色」的布尔)—— handbook 会被折成 light 或 dark,
//      表现是「点了图鉴出来的是亮色」,不报错。
//   5. 贴图用了相对路径 —— vite 会按**源文件位置**解析 url(),搬进 /assets/ 后就是 404,
//      而构建期只给一句极容易被忽略的警告。
//
// ⚠️ 本脚本覆盖不到的部分:主题**长什么样**(配色好不好看、贴图有没有贴歪、层次够不够)
// 只能靠真浏览器截图 —— 见 npm run verify:browser 与各候选的对比帧。
import { readFileSync, readdirSync, existsSync } from 'node:fs'
import { join, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..')
const css = readFileSync(join(ROOT, 'src/styles/base.css'), 'utf8')
// ⚠️ 主题形态层是 theme-handbook.css,**不是** handbook.css —— 后者是炫彩图鉴页
// (pages/handbook/HandbookGlasses)的样式。两者曾被我写重过一次(拿主题内容覆盖了页面样式,
// 该页在整轮验收里都是无样式的,而没有任何脚本发现),下面的 [1] 就是为此加的护栏。
const chrome = readFileSync(join(ROOT, 'src/styles/theme-handbook.css'), 'utf8')
const pageCss = readFileSync(join(ROOT, 'src/styles/handbook.css'), 'utf8')
const shell = readFileSync(join(ROOT, 'src/styles/shell.css'), 'utf8')
const js = readFileSync(join(ROOT, 'src/hooks/useTheme.js'), 'utf8')
const app = readFileSync(join(ROOT, 'src/App.jsx'), 'utf8')
const menu = readFileSync(join(ROOT, 'src/components/ThemeMenu.jsx'), 'utf8')
const svg = readFileSync(join(ROOT, 'src/components/svg.jsx'), 'utf8')
const fetchScript = readFileSync(join(ROOT, 'scripts/fetch-theme-textures.mjs'), 'utf8')

let fail = 0
const ok = (name, cond, extra = '') => {
  if (cond) { console.log(`  ✓ ${name}`); return }
  fail++
  console.log(`  ✗ ${name}${extra ? ' —— ' + extra : ''}`)
}

// 取某选择器的声明体(从它开始的配平 { ... }),注释里的同名文本靠「必须带 {」排除
const body = (src, sel) => {
  const i = src.indexOf(sel)
  if (i < 0) return ''
  let depth = 0
  for (let j = src.indexOf('{', i); j < src.length; j++) {
    if (src[j] === '{') depth++
    else if (src[j] === '}' && --depth === 0) return src.slice(i, j + 1)
  }
  return ''
}
// 声明体里定义的变量名
const varsIn = (block) => [...block.matchAll(/^\s*(--[\w-]+)\s*:/gm)].map((m) => m[1])
// 某变量在该块里的值(取第一处)
const varOf = (block, name) =>
  new RegExp(`^\\s*${name}\\s*:\\s*([^;]+);`, 'm').exec(block)?.[1]?.trim() ?? null

// —— 色值工具:WCAG 相对亮度与对比度 ——
// 只接受 #rgb / #rrggbb / rgb(r g b)。**半透明色一律返回 null 并让断言显式失败**,
// 而不是悄悄跳过 —— 悄悄跳过正是「测试永远绿灯」的配方。
const parseColor = (s) => {
  const t = (s || '').trim()
  let m = /^#([0-9a-f]{3})$/i.exec(t)
  if (m) return [...m[1]].map((c) => parseInt(c + c, 16))
  m = /^#([0-9a-f]{6})$/i.exec(t)
  if (m) return [0, 2, 4].map((i) => parseInt(m[1].slice(i, i + 2), 16))
  m = /^rgb\(\s*(\d+)[\s,]+(\d+)[\s,]+(\d+)\s*\)$/.exec(t)
  if (m) return [+m[1], +m[2], +m[3]]
  return null
}
const lum = ([r, g, b]) => {
  const f = (c) => { const v = c / 255; return v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4 }
  return 0.2126 * f(r) + 0.7152 * f(g) + 0.0722 * f(b)
}
const ratio = (a, b) => {
  const [hi, lo] = [lum(a), lum(b)].sort((x, y) => y - x)
  return (hi + 0.05) / (lo + 0.05)
}

// 所有 :root[data-theme="…"] 覆写块 —— **扫描**而不是逐个写名字:
// 加/删候选时本脚本无需改动,也就不会出现「加了新主题但校验脚本没跟上」的缺口。
const blocks = new Map()
for (const m of css.matchAll(/^:root\[data-theme="([\w-]+)"\]\s*\{/gm)) {
  const text = body(css, `:root[data-theme="${m[1]}"] {`)
  blocks.set(m[1], { text, vars: new Set(varsIn(text)) })
}
const lightBlock = blocks.get('light')?.text || ''

console.log('=== 图鉴家族主题校验(handbook / handbook-dark…) ===')

// —— 1. 块结构与 :root 唯一性 ——
console.log('\n[1] 令牌块结构')
ok('base.css 里有 handbook 覆写块', blocks.has('handbook'))
ok('亮色块仍在(下面要拿它做镜像基准)', !!lightBlock)
ok(`覆写块共 ${blocks.size} 个:`, blocks.size >= 2, [...blocks.keys()].join(' / '))
// :root 只能有一个块:check-css-vars 用 `rootVars = ...` 逐块赋值,第二个会把第一个整块顶掉,
// 全站变量都会被判未定义。这条是 base.css 注释里记着的坑,值得机器守。
const rootBlocks = (css.match(/^:root\s*\{/gm) || []).length
ok(':root 块全文件只有一个(多一个会顶掉前一个)', rootBlocks === 1, `实际 ${rootBlocks} 个`)
// 护栏:主题形态层必须与**炫彩图鉴页**的样式分开在两个文件里。
// 起因:本主题第一版把内容写进了 handbook.css —— 而那个名字已属炫彩图鉴页
// (pages/handbook/HandbookGlasses 的 .hb-* 一族),于是整页样式被覆盖,
// 而当时 jsdom 断言、check-css-vars、lint、npm run verify **全部照绿**:
// 页面照常渲染,只是没样式。这类「文件被顶掉」的损失只有文件层面能守。
ok('炫彩图鉴页的样式仍在 handbook.css 里(.hb-page 有定义)', /\.hb-page\s*\{/.test(pageCss),
  '被主题文件覆盖了?该页会整页失样式,而渲染类断言发现不了')
ok('主题形态层与页面样式是两个不同的文件', chrome !== pageCss && chrome.length > 0)

// —— 2. 逐名镜像亮色块(最有价值的一条)——
console.log('\n[2] 覆写完整性:亮色块的变量一个都不能漏')
const lightVars = varsIn(lightBlock)
const radii = ['--radius-sm', '--radius-md', '--radius', '--radius-lg']
for (const [name, b] of blocks) {
  if (name === 'light') continue
  const missing = lightVars.filter((v) => !b.vars.has(v))
  ok(`${name} 覆盖了亮色块的全部 ${lightVars.length} 个变量`, missing.length === 0,
    missing.length ? `漏掉: ${missing.join(', ')} —— 在本主题下会回落到默认主题的值` : '')
  ok(`${name} 覆写了 4 个圆角(图鉴家族的胶囊感)`, radii.every((v) => b.vars.has(v)),
    radii.filter((v) => !b.vars.has(v)).join(', '))
}

// —— 3. 不该进主题块的变量 ——
console.log('\n[3] 硬约束:语义色与标度不得随主题变')
// 前缀即判据:--c-* 语义色、--z-* 标度、--dur-*/--ease-* 动效、--font-*/--text-* 排版、
// --theme-*(JS 注入的扩散圆心)、--type-*、--acct-*
const FORBIDDEN = /^--(c-|z-|dur-|ease-|font-|text-|theme-|type-|acct-)/
for (const [name, b] of blocks) {
  const leaked = [...b.vars].filter((v) => FORBIDDEN.test(v))
  ok(`${name} 未收录语义色 / z-index / 动效 / 字体 / 排版令牌`, leaked.length === 0, leaked.join(', '))
}
// --on-gold 是「固定素材上的字」,必须只在 :root 定义、任何主题块都不得覆写(见 base.css 注释)
const onGoldBlocks = [...blocks].filter(([, b]) => b.vars.has('--on-gold')).map(([n]) => n)
ok('--on-gold 未被任何主题块覆写(它绑的是固定素材,不是主题)',
  onGoldBlocks.length === 0, onGoldBlocks.join(', '))

// —— 3b. 「--accent 底 + 白字」不得回流 ——
// 这条守的是这次刚抽出来的 --on-accent:全站曾有 8 处把强调色填充块上的字写死成 #fff,
// 在浅色主题下侥幸成立(强调色是深蓝/深琥珀),但暗色图鉴的强调色是亮金,白字只有 1.86:1。
// 改完没人拦着的话,下一个新组件很容易又写一次 #fff —— 那时只有肉眼能发现,
// 而「金底白字」在深色屏幕上非常刺眼却**不会报任何错**。
// 例外是允许的但必须显式:底色写 var(--red) 的块不在扫描范围内(红底白字是通用惯例)。
const STYLE_DIR = join(ROOT, 'src/styles')
const accentWhite = []
for (const f of readdirSync(STYLE_DIR).filter((n) => n.endsWith('.css'))) {
  const txt = readFileSync(join(STYLE_DIR, f), 'utf8')
  for (const m of txt.matchAll(/[^{}]+\{[^}]*\}/g)) {
    const rule = m[0]
    if (!/background[^;]*var\(--accent\)/.test(rule)) continue
    const w = /color:\s*(#fff\b|#ffffff\b|white\b)/i.exec(rule)
    if (w) accentWhite.push(`${f}: ${rule.split('{')[0].trim().slice(0, 36)} → ${w[0]}`)
  }
}
ok('没有「--accent 底 + 白字」的组合(该用 var(--on-accent))',
  accentWhite.length === 0, accentWhite.join(' | '))

// —— 4. 主题模型:风格 × 明暗(JS)——
console.log('\n[4] useTheme.js 的「风格 × 明暗」模型')
// 唯一允许没有覆写块的是**默认主题** —— base.css 的 :root 就是它(--bg/--fg 等全定义在
// 那个块里),故不需要也不该再有 :root[data-theme="dark"]。其余任何主题名没有块,
// 都意味着选中它时页面只有默认主题的颜色(「无色可用」),而界面上看起来一切正常。
const DEFAULT_THEME = 'dark'

// 清单里的元素可能是字符串字面量,也可能是**常量标识符**(如 MODES = [AUTO, 'light', 'dark'])。
// 后者要回到常量定义去取值 —— 只认字面量的话会把 auto 漏掉,而报出来的却是
// 「明暗三档都在」失败:看的人会以为「auto 被删了」,其实只是断言没跟上写法。
const identVal = (name) => new RegExp(`const ${name} = '([^']+)'`).exec(js)?.[1] ?? null
const listOf = (name) =>
  [...(new RegExp(`const ${name} = \\[([^\\]]*)\\]`).exec(js)?.[1] || '')
    .matchAll(/(?:([A-Z_]\w*)|'([^']+)')/g)]
    .map((m) => (m[1] ? identVal(m[1]) : m[2]))
    .filter(Boolean)
const families = listOf('FAMILIES')
const modes = listOf('MODES')
ok('两条风格都在(洛克 roco / 经典 classic)',
  families.includes('roco') && families.includes('classic'), families.join(' / '))
ok('明暗三档都在(auto / light / dark)',
  ['auto', 'light', 'dark'].every((m) => modes.includes(m)), modes.join(' / '))

// resolveTheme 是**唯一的真相映射**:(风格, 明暗) → 具体要挂的 data-theme 名。
// 这里把那几组组合展开,再逐个断言它们落在「有令牌块的主题」上 ——
// 组合出一个没有块的名字,页面就只剩默认主题的颜色,而界面上看不出任何异常。
// ⚠️ 不能借 body() 取这个函数体:helper 找的是选择器之后的第一个 `{`,而箭头函数的
// **参数解构** `({ fam, mode })` 里就有一对 —— 它会把参数当成函数体、只截到那里为止。
// 故直接切到第一个行首的 `}`。
const resolveBody = /const resolveTheme = \(\{[\s\S]*?\n\}/.exec(js)?.[0] || ''
// 只取 `return` 之后的部分,并把**轴的取值**排除掉 —— 函数里除了产出主题名,还要
// 比较 `fam === 'roco'` / `mode === 'auto'`,那些字符串不是主题名,当成主题名会误报。
const emitPart = resolveBody.slice(resolveBody.indexOf('return'))
const emitted = [...new Set([...emitPart.matchAll(/'([\w-]+)'/g)].map((m) => m[1]))]
// 映射的产出 = base.css 里的四套主题。这份契约**写死在断言里**而不是从表达式推导:
// 它是设计意图(洛克的浅/深 = 图鉴两套;经典的浅/深 = 灰调两套),不是从代码里能猜出来的东西。
// (想靠「排除轴的取值」来反推产出会误伤 —— `light`/`dark` 既是明暗档位、又是经典风格的主题名。)
const CONTRACT = ['handbook', 'handbook-dark', 'light', 'dark']
const notEmitted = CONTRACT.filter((t) => !emitted.includes(t))
ok('四套具体主题都被映射到了(handbook / handbook-dark / light / dark)', notEmitted.length === 0,
  notEmitted.length ? `${notEmitted.join(', ')} 没出现在产出那一句里(实际 ${emitted.join(', ') || '取不到函数体'})` : '')
const noBlock = CONTRACT.filter((t) => !blocks.has(t) && t !== DEFAULT_THEME)
ok(`映射只会产出有令牌块的主题(含定义在 :root 的默认主题 ${DEFAULT_THEME})`, noBlock.length === 0,
  `${noBlock.join(', ')} 没有令牌块 —— 选中它就是一个无色可用的页面`)
// auto 就地解析:风格决定用哪一对(图鉴的日/夜 还是 灰调的浅/深),系统只决定这一对里的哪一面。
ok('auto 的两侧分属图鉴与经典两族(风格由用户选,系统只决定明暗)',
  /fam === 'roco' \? \(dark \? 'handbook-dark' : 'handbook'\) : \(dark \? 'dark' : 'light'\)/.test(js))
ok('「跟随系统」只在 mode 是 auto 时监听系统明暗(选了固定明暗就不该被系统改)',
  /theme\.mode !== AUTO/.test(js))

// —— 默认 + 存储格式 + 旧值迁移 ——
ok('默认是洛克风格 + 跟随系统明暗',
  /const DEFAULT = \{ fam: 'roco', mode: AUTO \}/.test(js))
// auto 要有名字:散成四处字面量时,改一处漏一处不报错,只表现为「跟随系统突然不跟了」。
ok('「跟随系统」是命名常量,不是散落的字面量',
  /const AUTO = 'auto'/.test(js) && /const MODES = \[AUTO, /.test(js))
ok('encode / decode 成对,且 decode 会校验两轴(不校验就会把坏值当主题用)',
  /const encode = /.test(js) && /const decode = /.test(js) &&
  /FAMILIES\.includes\(fam\) && MODES\.includes\(mode\)/.test(js))
// 存储是**单键** "风格:明暗"(而不是风格、明暗各一个键):两个键会留下
// 「风格换了、明暗还没换」的中间态被别处读到(刷新时机不巧就闪一下旧风格)。
ok('存储是单键(原子写入),sanitize 的出口统一是那个字符串',
  /localStorage, 'theme'/.test(js) && /const sanitize = \(v\) => \{/.test(js) && /return encode\(/.test(js))
// 上一版的五档扁平值必须都被识别 —— 漏一个,那个人就卡在旧值上:decode 会把非法值
// **静默**回落默认,看起来"碰巧对了",等哪天默认值一变就露馅。故这里数的是清单本身。
const legacySrc = /const LEGACY_VALUES = new Set\(\[([^\]]*)\]\)/.exec(js)?.[1] || ''
// 元素可能是字面量也可能是常量标识符(AUTO)—— 只认字面量会把 auto 漏掉,
// 报出来却像是「迁移清单少了一项」(这类假警报在第 4 组踩过一次)。
const legacyVals = [...legacySrc.matchAll(/(?:([A-Z_]\w*)|'([^']+)')/g)]
  .map((m) => (m[1] ? identVal(m[1]) : m[2]))
  .filter(Boolean)
ok('上一版五个扁平值都在迁移清单里(auto / handbook / handbook-dark / light / dark)',
  ['auto', 'handbook', 'handbook-dark', 'light', 'dark'].every((k) => legacyVals.includes(k)),
  `实际 ${legacyVals.join(', ')}`)
// 2026-09-12 的决定:五个老值**一律**回默认,而不是各自保留外观(理由见 useTheme.js 那段注释)。
// else 分支必须是 decode —— 那是「两轴格式的值不受迁移影响」的唯一证据,别把它简化成 return DEFAULT。
ok('老值一律回到默认,其余值走 decode 原样解析(新界面下选过的不被翻)',
  /LEGACY_VALUES\.has\(v\) \? DEFAULT : decode\(v\)/.test(js))

ok('applyTheme 把解析后的真名写进 data-theme',
  /setAttribute\('data-theme', resolveTheme\(t\)\)/.test(js))
ok('扩散判据比较主题名(而非「明暗是否相同」)',
  /resolveTheme\(next\) === shown/.test(js),
  '亮系统里「洛克·跟随系统」→「洛克·浅色」明暗也「没变」,但那两件事本来就不该播动画')

// —— 4b. 首次打开的默认 ——
// 「新访客第一眼就是洛克风,同时跟随系统明暗」的落点在 useStoredJSON 的 fallback 参数上:
// 那个参数只在**没有存储记录**(或解析失败)时生效 —— 故选过主题的人不受影响。
ok('首次打开的 fallback 用的是 DEFAULT(而不是写死的某套主题)',
  /useStoredJSON\(localStorage, 'theme', encode\(DEFAULT\), sanitize\)/.test(js),
  '传写死的主题名会让新访客固定看到某一套,与「默认洛克 + 跟随系统」不符')

// —— 5. 版本号=菜单入口,主题图标=纯切换 ——
console.log('\n[5] 版本号(菜单入口)/ 主题图标(纯切换)')
ok('版本号包了锚点容器(菜单挂在它下面)', /className="ver-wrap"/.test(app))
// 顶栏只留一个入口:菜单只渲染一处、aria-haspopup 只出现一次(都在版本号上)。
// 若第二个按钮也弹菜单,就又要多维护一套状态同步 —— 「只留一个入口」是刻意的简化。
ok('菜单只渲染一处、只有版本号带 aria-haspopup',
  (app.match(/<ThemeMenu /g) || []).length === 1 &&
  (app.match(/aria-haspopup/g) || []).length === 1,
  `ThemeMenu ${(app.match(/<ThemeMenu /g) || []).length} 处 / aria-haspopup ${(app.match(/aria-haspopup/g) || []).length} 处`)
ok('图标接的是纯切换 toggle(而不是打开菜单)',
  /className="topbar-fs"[\s\S]{0,120}onClick=\{toggle\}/.test(app))
// 一键切浅/深**只动明暗那一轴**,风格保持不变 —— 这是「职责分开」的核心。
ok('toggle 只改明暗、保留风格', /fam: theme\.fam, mode: dark \? 'light' : 'dark'/.test(js))
// 往哪边切按**实际生效**的明暗取反:选着「跟随系统」时,用户眼里看到的是浅的,
// 点一下就该变深;若按 mode==='auto' 去算,这一下会先跳到一个跟他看到的不一样的档。
ok('切换方向按实际生效的明暗取反(不是按 mode 字段)',
  /const dark = solid === 'dark' \|\| solid\.endsWith\('-dark'\)/.test(js))
ok('切完写明确的 light/dark(用户指定过明暗,就不该再被系统改)',
  /mode: dark \? 'light' : 'dark'/.test(js) && /toggle/.test(js))
// 图标按**实际生效**的主题取:选着「跟随系统」时也要能看出当下是浅还是深,
// 否则一个中性的图标回答不了「现在长什么样」。
ok('图标取自实际生效的主题 solid,而不是用户选的那一档', /SOLID_ICON\[solid\]/.test(app))
const iconKeys = [...body(app, 'const SOLID_ICON = {').matchAll(/^\s+'?([\w-]+)'?:\s*</gm)].map((m) => m[1])
ok('图标表覆盖映射能产出的全部主题(漏一个就回落到中性图标)',
  ['handbook', 'handbook-dark', 'light', 'dark'].every((t) => iconKeys.includes(t)), iconKeys.join(', '))
ok('svg.jsx 导出了 IconBook 与 IconBookNight',
  /export const IconBook\b/.test(svg) && /export const IconBookNight\b/.test(svg))
// 版本号原本是 <span>,改成 <button> 后必须把按钮的 UA 外观抹平:
// 顶栏在 360~389px 那档是**算着它 34px 宽**排的(见 shell.css 的 .topbar-ver 注释,
// 以及 verify-account-width.mjs 的手机分支),多出一点内边距就会把那档顶栏挤高。
ok('版本号是按钮,但按钮外观被抹平(尺寸与改前一致)',
  /className="topbar-ver"/.test(app) && /padding: 0; border: none; background: none/.test(shell))
ok('菜单用真 button + aria-pressed(键盘与读屏靠它知道当前选的是哪个)',
  /aria-pressed=\{cur === it\.key\}/.test(menu) && /role="group"/.test(menu))
// 选主题时把 click 事件原样交给 choose —— 它要用 currentTarget 当**扩散圆心**。
// 写成 `onClick={() => pick(key)}` 就会丢掉事件,退化成瞬时切换(只有肉眼能看出来)。
ok('选项的 click 事件被原样传给 choose(扩散圆心来自被点的那个选项)',
  /onPick\(build\(it\.key\), e\)/.test(menu) && /choose\(next, e\)/.test(app))

// —— 6. 贴图引用 ——
console.log('\n[6] 贴图引用与混合方式')
// CSS 里 url() 的路径若是相对的,vite 会按**源文件所在目录**解析,成品 CSS 搬进 /assets/
// 之后就成了 /assets/theme/... → 404,而构建期只给一句警告。故一律要求以 / 开头。
const cssUrls = [...chrome.matchAll(/url\(\s*["']?([^"')]+)["']?\s*\)/g)].map((m) => m[1])
const rel = cssUrls.filter((u) => !u.startsWith('/'))
ok('theme-handbook.css 里的 url() 全部是绝对路径', cssUrls.length > 0 && rel.length === 0,
  rel.length ? `相对路径: ${rel.join(', ')} —— vite 会把它解析到 /assets/ 下,404` : '')
const themed = cssUrls.filter((u) => u.includes('/theme/handbook/'))
ok('贴图引用带 /theme/handbook/ 前缀', themed.length === cssUrls.length,
  cssUrls.filter((u) => !u.includes('/theme/handbook/')).join(', '))
// 底纹的混合方式必须按明暗分家:深底用 soft-light 会**把整页提亮**,等于绕过令牌改了 --bg,
// 于是 [8] 算出来的对比度就不再是屏幕上的真实数字(守护会失真)。
const blendOf = (sel) => varOf(body(chrome, sel), 'background-blend-mode')
ok('浅色图鉴的底纹用 soft-light', blendOf(':root[data-theme^="handbook"] body {') === 'soft-light')
ok('暗色图鉴的底纹用 multiply(变暗,才能让 [8] 的对比度成为下界)',
  blendOf(':root[data-theme^="handbook-dark"] body {') === 'multiply',
  '用时 soft-light 会把深底提亮,令牌算出的对比度就失真了')

// —— 7. 引用的贴图与磁盘/下载清单三方一致 ——
console.log('\n[7] 贴图三方对账(CSS 引用 ↔ 磁盘 ↔ 下载清单)')
const DIR = join(ROOT, 'public/theme/handbook')
const disk = new Set()
const walk = (d, pre = '') => {
  if (!existsSync(d)) return
  for (const e of readdirSync(d, { withFileTypes: true })) {
    if (e.isDirectory()) walk(join(d, e.name), pre + e.name + '/')
    else disk.add(pre + e.name)
  }
}
walk(DIR)
const quoted = new Set(themed.map((u) => u.replace('/theme/handbook/', '')))
// 下载清单:scripts/fetch-theme-textures.mjs 的 FILES 表第二列
const listed = new Set([...fetchScript.matchAll(/\['[^']+',\s*'([^']+)',/g)].map((m) => m[1]))
ok(`磁盘上有 ${disk.size} 张贴图,且全部被 CSS 引用`, [...disk].every((f) => quoted.has(f)),
  `未被引用(白进构建产物): ${[...disk].filter((f) => !quoted.has(f)).join(', ')}`)
ok('CSS 引用的贴图都在磁盘上(无图裂)',
  [...quoted].every((f) => disk.has(f)),
  `缺文件: ${[...quoted].filter((f) => !disk.has(f)).join(', ')}`)
ok('下载清单与磁盘一致(脚本可幂等重跑)',
  listed.size === disk.size && [...listed].every((f) => disk.has(f)),
  `清单 ${listed.size} 张 / 磁盘 ${disk.size} 张`)

// —— 8. 逐主题 WCAG 对比度(暗色主题最容易翻车的一处,只能算)——
console.log('\n[8] 每套主题的对比度(WCAG AA)')
// 门槛:正文与次要文字 4.5(本站信息密度高,次要文字也不能放水);
// 强调色对底 3.0(多用于描边/图标这类非文字);填充块上的字 4.5。
const PAIRS = [
  ['--fg', '--bg', 4.5, '正文/页面底'],
  ['--fg', '--bg-1', 4.5, '正文/卡片底'],
  ['--fg-dim', '--bg', 4.5, '次要文字/页面底'],
  ['--fg-dim', '--bg-1', 4.5, '次要文字/卡片底'],
  ['--accent', '--bg', 3.0, '强调色/页面底'],
  ['--on-accent', '--accent', 4.5, '强调色填充块上的字'],
  ['--on-accent', '--accent-hi', 4.5, 'hover 后填充块上的字'],
]
for (const [name, b] of blocks) {
  const bad = []
  for (const [fgV, bgV, min, why] of PAIRS) {
    const fg = parseColor(varOf(b.text, fgV))
    const bg = parseColor(varOf(b.text, bgV))
    if (!fg || !bg) { bad.push(`${why}:${fgV}/${bgV} 取不到纯色(解析失败)`); continue }
    const r = ratio(fg, bg)
    if (r < min) bad.push(`${why} ${r.toFixed(2)}<${min}`)
  }
  // 红底白字是通用惯例,前提是 --red 够深 —— 所以这里不是「不检查」,而是把 --red 也纳入门槛
  const red = parseColor(varOf(b.text, '--red'))
  if (red && ratio([255, 255, 255], red) < 4.5) {
    bad.push(`白字/--red ${ratio([255, 255, 255], red).toFixed(2)}<4.5`)
  }
  ok(`${name}: ${PAIRS.length + 1} 对颜色全部达标`, bad.length === 0, bad.join('; '))
}
// 金素材上的字:主体色是 canvas 取样实测的(见 base.css 的 --on-gold 注释),素材换色要重取。
const RIBBON = [0xf8, 0xb0, 0x40] // select.png 主色
const STAMP = [0xb0, 0xa8, 0x90]  // finish.png 主色
const onGold = parseColor(varOf(body(css, ':root {'), '--on-gold'))
ok('--on-gold 在金锦带上可读', !!onGold && ratio(onGold, RIBBON) >= 4.5,
  onGold ? `${ratio(onGold, RIBBON).toFixed(2)}:1` : '取不到 --on-gold')
ok('--on-gold 在已达成印章上可读', !!onGold && ratio(onGold, STAMP) >= 4.5,
  onGold ? `${ratio(onGold, STAMP).toFixed(2)}:1` : '取不到 --on-gold')

console.log(fail === 0 ? '\n✓ 全部通过' : `\n✗ ${fail} 项未通过`)
process.exit(fail === 0 ? 0 : 1)
