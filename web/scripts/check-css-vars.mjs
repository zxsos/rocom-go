// CSS 变量作用域校验:按主题分别检查 var() 引用是否都有定义。
//
// 为什么需要这个脚本:
// CSS 里引用未定义的 var() 会**静默失效**(回退到继承值/初始值),不报任何错。
// 而 base.css 的变量分两块:
//   :root                      → 默认(暗色)主题生效
//   :root[data-theme="light"]  → 仅亮色主题生效
// 若把「与主题无关」的变量(如 --z-*、--c-* 语义色)误写进 light 块,
// 暗色主题下它们全部未定义 → 所有引用静默失效。
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

const baseLines = readFileSync(join(DIR, 'base.css'), 'utf8').split('\n')
let rootVars = new Set()
let lightVars = new Set()
for (let i = 0; i < baseLines.length; i++) {
  const l = baseLines[i]
  if (/^:root\s*\{/.test(l)) rootVars = varsIn(blockAt(baseLines, i))
  if (/^:root\[data-theme="light"\]\s*\{/.test(l)) lightVars = varsIn(blockAt(baseLines, i))
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

let bad = 0

// 1) 暗色(默认)主题:可用 = :root 定义的 ∪ 局部变量
const darkMissing = [...used.keys()].filter(
  (v) => !rootVars.has(v) && !localVars.has(v),
)
if (darkMissing.length) {
  bad++
  console.log('❌ 暗色主题(默认 :root)下未定义的变量 —— 会静默失效:')
  for (const v of darkMissing.sort()) {
    const where = [...(used.get(v) || [])].join(', ')
    console.log(`     ${v}   被引用: ${where}`)
    if (lightVars.has(v)) {
      console.log(`       ↑ 它定义在 :root[data-theme="light"] 块内 —— 很可能应移到 :root`)
    }
  }
} else {
  console.log(`✅ 暗色主题: ${used.size} 个 var() 引用全部有定义`)
}

// 2) 亮色主题:可用 = :root ∪ light ∪ 局部。这项理论上不会缺,列出是为了显式确认。
const lightMissing = [...used.keys()].filter(
  (v) => !rootVars.has(v) && !lightVars.has(v) && !localVars.has(v),
)
if (lightMissing.length) {
  bad++
  console.log('❌ 亮色主题下也未定义:', lightMissing.sort().join(', '))
} else {
  console.log(`✅ 亮色主题: ${used.size} 个 var() 引用全部有定义`)
}

// 3) 与主题无关却被写进 light 块:这是本次事故的形态,单独预警。
//    判定:light 块定义了、但 :root 没定义 → 暗色主题下必然失效(除非是局部变量)
const themeLeak = [...lightVars].filter((v) => !rootVars.has(v) && !localVars.has(v))
if (themeLeak.length) {
  bad++
  console.log('❌ light 块定义但 :root 未定义(暗色主题会失效):', themeLeak.sort().join(', '))
} else {
  console.log(`✅ light 块 ${lightVars.size} 个变量均在 :root 有基础定义(纯覆写,无泄漏)`)
}

// 4) 定义了但从未使用
const allDefined = new Set([...rootVars, ...lightVars])
const unused = [...allDefined].filter((v) => !used.has(v))
if (unused.length) console.log('⚠️  定义但未被 var() 引用:', unused.sort().join(', '))
else console.log('✅ 无未使用的变量定义')

console.log(
  `\n汇总: :root ${rootVars.size} 个 / light ${lightVars.size} 个 / 引用 ${used.size} 个`,
)
process.exit(bad ? 1 : 0)
