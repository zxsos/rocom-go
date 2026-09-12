// CSS 变量作用域校验:按主题分别检查 var() 引用是否都有定义。
//
// 为什么需要这个脚本:
// CSS 里引用未定义的 var() 会**静默失效**(回退到继承值/初始值),不报任何错。
// 而 base.css 的变量分两类块:
//   :root                       → 默认主题生效(也是所有覆写块的兜底)
//   :root[data-theme="…"]       → 仅该主题生效(现有 light / handbook 两块)
// 若把「与主题无关」的变量(如 --z-*、--c-* 语义色)误写进覆写块,
// 默认主题下它们全部未定义 → 所有引用静默失效。
//
//   真实事故:z-index 标度被写进 :root[data-theme="light"] 块,
//   导致暗色主题下全部 z-index 失效、浮层层级完全靠 DOM 顺序,
//   表现为「白天模式浮层正常、夜晚模式错乱」。
//   而「只看变量是否在某文件里被定义过」的朴素检查抓不到 —— 它不区分选择器作用域。
//
// 用法: node scripts/check-css-vars.mjs   (退出码非 0 即有问题)

import { readFileSync, readdirSync } from 'node:fs'
import { join, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

const DIR = join(dirname(fileURLToPath(import.meta.url)), '..', 'src', 'styles')
const files = readdirSync(DIR).filter((f) => f.endsWith('.css'))

// 提取某选择器块内定义的变量(块 = 从 { 到配平的 })
function blockAt(lines, startIdx) {
  let depth = 0
  const body = []
  for (let i = startIdx; i < lines.length; i++) {
    for (const ch of lines[i]) {
      if (ch === '{') depth++
      else if (ch === '}') depth--
    }
    body.push(lines[i])
    if (depth === 0 && i > startIdx) break
  }
  return body
}

function varsIn(body) {
  const out = new Set()
  for (const l of body) {
    const m = /^\s*(--[\w-]+)\s*:/.exec(l)
    if (m) out.add(m[1])
  }
  return out
}

// 主题块:一个默认块(:root)+ 若干覆写块(:root[data-theme="…"],每套主题一个)。
// 这里刻意**扫描**而不是写死块名 —— 早先写死了 :root 与 :root[data-theme="light"] 两块,
// 于是加第三套主题(handbook)时,新块里的漏变量不会有任何人来查,而「漏一个变量」
// 恰恰是本脚本要防的那类静默失效(该变量在本主题下回落到默认主题的值)。
const THEME_BLOCK_RE = /^:root\[data-theme="([\w-]+)"\]\s*\{/
const baseLines = readFileSync(join(DIR, 'base.css'), 'utf8').split('\n')
let rootVars = new Set()
const themes = new Map() // 主题名 -> 该块覆写的变量集
for (let i = 0; i < baseLines.length; i++) {
  const l = baseLines[i]
  if (/^:root\s*\{/.test(l)) rootVars = varsIn(blockAt(baseLines, i))
  const m = THEME_BLOCK_RE.exec(l)
  if (m) themes.set(m[1], varsIn(blockAt(baseLines, i)))
}

// 局部变量:非 :root 块内定义的(组件自己的,不在全局作用域)
const localVars = new Map() // var -> Set(file)
for (const f of files) {
  const lines = readFileSync(join(DIR, f), 'utf8').split('\n')
  if (f === 'base.css') {
    // base.css 里除两个 :root 块外的定义也算局部
    let inRoot = false
    for (const l of lines) {
      if (/^:root/.test(l)) inRoot = true
      else if (inRoot && /^\}/.test(l)) inRoot = false
      else if (!inRoot) {
        const m = /^\s*(--[\w-]+)\s*:/.exec(l)
        if (m) localVars.set(m[1], (localVars.get(m[1]) || new Set()).add(f))
      }
    }
    continue
  }
  for (const l of lines) {
    const m = /^\s*(--[\w-]+)\s*:/.exec(l)
    if (m) localVars.set(m[1], (localVars.get(m[1]) || new Set()).add(f))
  }
}

// 收集所有 var() 引用
const used = new Map() // var -> Set(file)
for (const f of files) {
  const txt = readFileSync(join(DIR, f), 'utf8')
  for (const m of txt.matchAll(/var\(\s*(--[\w-]+)/g)) {
    used.set(m[1], (used.get(m[1]) || new Set()).add(f))
  }
}

// —— JSX 内联注入的 CSS 变量 ——
//
// React 的 style={{ '--c': color }} 会把变量挂到元素的 style 上,其作用域是
// **那个元素及其后代**。CSS 里 var(--c) 完全合法,但脚本若只扫 .css 就会把它
// 报成「未定义」。
//
// 自动扫 src/ 下的 .jsx 而非手工维护白名单:白名单会漂移 —— 新增一个注入点而
// 忘了登记,脚本就退化成噪音(报假错),人一旦开始忽略红灯,真错也就漏了。
// 自动扫描则永远与代码同步。
//
// 真实案例:trial.css 的 .sigil-* 用 var(--c) / var(--d),由
// pages/trial/ElementWheel.jsx 的 style={{ '--c': d.color, '--d': ... }} 注入。
const injected = new Map() // var -> Set(file)
const SRC = join(DIR, '..')
const walk = (dir) => {
  for (const e of readdirSync(dir, { withFileTypes: true })) {
    const p = join(dir, e.name)
    if (e.isDirectory()) { if (e.name !== 'styles' && e.name !== 'node_modules') walk(p) } else if (e.name.endsWith('.jsx') || e.name.endsWith('.js')) {
      const txt = readFileSync(p, 'utf8')
      // 匹配 '--x': 与 '--x': 两种字面量键写法(单引号/双引号/无引号不适用)
      for (const m of txt.matchAll(/['"](--[\w-]+)['"]\s*:/g)) {
        injected.set(m[1], (injected.get(m[1]) || new Set()).add(p.slice(SRC.length + 1)))
      }
    }
  }
}
walk(SRC)
if (injected.size) {
  console.log('ℹ️  JSX 内联注入的变量(视为已定义):',
    [...injected.keys()].sort().map((v) => `${v}(${[...injected.get(v)].join(',')})`).join(' '))
}

let bad = 0

// 某主题下「可用」的变量 = :root 定义的 ∪ 组件局部 ∪ JSX 内联注入 ∪ 该主题块自行覆写的
const knownFor = (themeVars) => (v) =>
  rootVars.has(v) || localVars.has(v) || injected.has(v) || themeVars.has(v)

// 1) 默认主题(:root):没有任何覆写块兜底,缺了就是全站失效
const darkMissing = [...used.keys()].filter((v) => !knownFor(new Set())(v))
if (darkMissing.length) {
  bad++
  console.log('❌ 默认主题(:root)下未定义的变量 —— 会静默失效:')
  for (const v of darkMissing.sort()) {
    const where = [...(used.get(v) || [])].join(', ')
    console.log(`     ${v}   被引用: ${where}`)
    const holder = [...themes].find(([, vs]) => vs.has(v))
    if (holder) {
      console.log(`       ↑ 它只定义在 :root[data-theme="${holder[0]}"] 块内 —— 很可能应移到 :root`)
    }
  }
} else {
  console.log(`✅ 默认主题(:root): ${used.size} 个 var() 引用全部有定义`)
}

// 2) 每套覆写主题:引用都得有定义。理论上不会缺(覆写块本身就依赖 :root),列出是为了显式确认。
for (const [name, vs] of themes) {
  const miss = [...used.keys()].filter((v) => !knownFor(vs)(v))
  if (miss.length) {
    bad++
    console.log(`❌ 主题 ${name} 下有未定义的变量:`, miss.sort().join(', '))
  } else {
    console.log(`✅ 主题 ${name}: ${used.size} 个 var() 引用全部有定义`)
  }
}

// 3) 与主题无关却被写进覆写块:这是本项目踩过的事故形态(见文件头),逐块预警。
//    判定:覆写块定义了、但 :root 没定义 → 默认主题下必然失效(除非是局部变量)。
const themeLeak = [...themes].flatMap(([name, vs]) =>
  [...vs]
    .filter((v) => !rootVars.has(v) && !localVars.has(v) && !injected.has(v))
    .map((v) => `${name}:${v}`))
if (themeLeak.length) {
  bad++
  console.log('❌ 覆写块定义但 :root 未定义(默认主题会失效):', themeLeak.sort().join(', '))
} else {
  const total = [...themes.values()].reduce((n, s) => n + s.size, 0)
  console.log(`✅ ${themes.size} 个覆写块共 ${total} 个变量均在 :root 有基础定义(纯覆写,无泄漏)`)
}

// 4) 定义了但从未使用
const allDefined = new Set([...rootVars, ...[...themes.values()].flatMap((s) => [...s])])
const unused = [...allDefined].filter((v) => !used.has(v))
if (unused.length) console.log('⚠️  定义但未被 var() 引用:', unused.sort().join(', '))
else console.log('✅ 无未使用的变量定义')

console.log(
  `\n汇总: :root ${rootVars.size} 个 / ${[...themes].map(([n, s]) => `${n} ${s.size} 个`).join(' / ')} / 引用 ${used.size} 个`,
)
process.exit(bad ? 1 : 0)
