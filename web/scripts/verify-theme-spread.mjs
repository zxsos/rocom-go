// 主题切换「从按钮处扩散」的静态校验(web/src/hooks/useTheme.js + styles/base.css)。
//
// 为什么需要它:这条链路有三处**静默失效**,编译、构建、lint 全都发现不了 ——
//
//   1. 忘了关 UA 默认的交叉淡入。::view-transition-old/new(root) 自带淡入淡出,
//      不写 animation: none 的话,圆还没张到的地方也跟着整体变淡,
//      观感是「先整屏闪一层灰再扩散」—— 动画**在跑**,只是不对,
//      所以任何「动画存在吗」的检查都会通过。
//   2. 裁错了快照。裁 ::view-transition-**old**(root) 会变成「旧色被挖掉」,
//      方向完全相反,而不裁任何一个则整屏瞬变(等于没做)。
//   3. 半径只算到视口中心或某个角。圆张满时对角会留一条月牙形旧色残边,
//      只有在长宽比很极端的窗口下才看得见,极易漏过人工检查。
//
// 另外两处由 JS 承担、同样静默的失败也一并守住:
//   - 不用 flushSync:React 只承诺把更新排进微任务,不承诺在浏览器截新快照之前提交,
//     渲染一被拉长就会截到**旧**主题 —— 表现为「圆张开了,里面铺开的还是原来那个颜色」
//     (动画在跑,内容错)。当前 Chromium 上实测不写也赶得及,但那是撞运气。
//   - 没有特性检测 / 不开 reduced-motion 分支:老浏览器直接抛异常导致主题切不了。
//
// 用法: node scripts/verify-theme-spread.mjs   (退出码非 0 即有问题)
import { readFileSync } from 'node:fs'
import { join, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..')
const css = readFileSync(join(ROOT, 'src/styles/base.css'), 'utf8')
const js = readFileSync(join(ROOT, 'src/hooks/useTheme.js'), 'utf8')

let fail = 0
const ok = (cond, label, extra = '') => {
  console.log(`  ${cond ? 'OK  ' : 'FAIL'} ${label}${extra ? ' — ' + extra : ''}`)
  if (!cond) fail++
}
// 取出某选择器的声明体(到配平的 } 为止),便于对整块做断言
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

console.log('=== 主题扩散切换校验 useTheme.js + base.css ===\n')

// —— 1. CSS 侧:关掉默认交叉淡入,且裁的是 new(root) ——
console.log('[1] view-transition 快照:关默认动画 + 裁新快照')
const pair = body(css, '::view-transition-old(root),\n::view-transition-new(root)')
ok(!!pair, 'old/new 成对声明')
ok(/animation:\s*none/.test(pair), '关掉默认淡入淡出',
  /animation:\s*none/.test(pair) ? '' : '不关会「整屏闪一层灰再扩散」')
ok(/mix-blend-mode:\s*normal/.test(pair), 'mix-blend-mode: normal',
  /mix-blend-mode:\s*normal/.test(pair) ? '' : '默认 plus-lighter 会让两张快照越叠越亮')

const newRule = body(css, 'html.theme-spread::view-transition-new(root)')
ok(!!newRule, '存在 html.theme-spread::view-transition-new(root) 规则')
ok(/animation:\s*theme-spread/.test(newRule), '裁的是 **new**(root)(新色长出来)',
  /animation:\s*theme-spread/.test(newRule) ? '' : '裁 old 会变成旧色被挖掉,方向相反')

// —— 2. keyframes:圆心/半径取注入的变量,时长/缓动走项目令牌 ——
console.log('\n[2] @keyframes theme-spread 的取值来源')
// 带上 ` {`:上面 :root 的注释里也提到了 "@keyframes theme-spread",
// 只按名字找会先命中注释,拿到一段注释当声明体。
const kf = body(css, '@keyframes theme-spread {')
ok(!!kf, '定义了 @keyframes theme-spread')
ok(/circle\(\s*0(px)?\s+at\s+var\(--theme-x\)\s+var\(--theme-y\)\s*\)/.test(kf),
  'from: 半径 0,圆心用 var(--theme-x/y)')
ok(/circle\(\s*var\(--theme-r\)\s+at\s+var\(--theme-x\)\s+var\(--theme-y\)\s*\)/.test(kf),
  'to: 半径用 var(--theme-r)')
ok(/animation:\s*theme-spread\s+var\(--dur-theme\)\s+var\(--ease-out\)/.test(newRule),
  '时长/缓动复用项目令牌',
  /animation:\s*theme-spread\s+var\(--dur-theme\)/.test(newRule) ? '' : '不要硬编码时长')

// —— 3. 三个几何变量在 :root 有静态兜底(check-css-vars 只看 CSS 里的定义)——
console.log('\n[3] --theme-x/y/r 在 :root 有兜底定义')
const rootBlock = body(css, ':root {') || css.slice(css.indexOf(':root'), css.indexOf(':root[data-theme="light"]'))
for (const v of ['--theme-x', '--theme-y', '--theme-r']) {
  ok(new RegExp(`^\\s*${v}:`, 'm').test(rootBlock), `${v} 已在 :root 定义`,
    new RegExp(`^\\s*${v}:`, 'm').test(rootBlock) ? '' : '缺它会让 npm run check:css 判未定义')
}

// —— 4. reduced-motion 兜底 ——
console.log('\n[4] prefers-reduced-motion 覆盖')
// base.css 里有**多个** reduced-motion 块(本文件的、别处的),必须全取再判断 ——
// 只取第一个的话,加在后面的降级规则永远校验不到(而它恰恰是最容易漏写的那个)。
const rm = [...css.matchAll(/@media \(prefers-reduced-motion: reduce\)\s*\{([\s\S]*?)\n\}/g)]
  .map((m) => m[1])
  .join('\n')
ok(rm.includes('theme-spread::view-transition-new(root)'), '降级块里关掉了扩散动画',
  rm.includes('theme-spread') ? '' : 'class 残留时会留下裁掉半屏的快照')

// —— 5. JS 侧:圆心来自按钮,半径覆盖到最远角 ——
console.log('\n[5] 圆心与半径的取法')
ok(/e\?\.currentTarget/.test(js) && /getBoundingClientRect/.test(js),
  '圆心取自事件目标(按钮)的 getBoundingClientRect')
ok(/Math\.max\(x,\s*window\.innerWidth - x\)/.test(js) &&
   /Math\.max\(y,\s*window\.innerHeight - y\)/.test(js),
  '半径取到视口四角的最远距离',
  '只算到中心/单角会在对角留下月牙形旧色残边')
ok(/Math\.hypot/.test(js), '用 Math.hypot 合成半径')

// —— 6. JS 侧:同步落盘 / 特性检测 / reduced-motion / 清理 ——
console.log('\n[6] 切换路径的四个必要动作')
ok(/flushSync\(\(\)\s*=>\s*setTheme\(next\)\)/.test(js), '用 flushSync 保证截快照前已切好主题',
  /flushSync/.test(js) ? '' : 'React 18 批处理会让快照截到旧主题')
ok(/typeof document\.startViewTransition === 'function'/.test(js), '有 View Transitions 特性检测',
  '老浏览器不支持时会直接抛异常,主题切不了')
ok(/prefers-reduced-motion: reduce/.test(js), '读 prefers-reduced-motion 并跳过动画')
ok((js.match(/removeProperty\('--theme-/g) || []).length === 3 &&
   /classList\.remove\(SPREAD\)/.test(js), '结束后清掉 class 与三个变量')
ok(/vt\.ready\.catch\(cleanup\)/.test(js) && /vt\.finished\.then\(cleanup, cleanup\)/.test(js),
  'ready/finished 都挂了清理(连续快速点击不留残留)')

// —— 7. 调用点没有把事件丢掉 ——
console.log('\n[7] 调用点(App.jsx)传的是事件本身')
const app = readFileSync(join(ROOT, 'src/App.jsx'), 'utf8')
ok(/onClick=\{cycleTheme\}/.test(app), 'App.jsx 用 onClick={cycleTheme}',
  /onClick=\{cycleTheme\}/.test(app) ? '' : '写成 () => cycleTheme() 会丢事件、退化成瞬时切换')
ok(!/onClick=\{\(\)\s*=>\s*cycleTheme\(\)\}/.test(app), '没有丢事件的包一层箭头写法')

console.log(fail === 0 ? '\n=== 全部通过 ===' : `\n=== ${fail} 项失败 ===`)
process.exit(fail === 0 ? 0 : 1)
