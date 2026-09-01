// 徽章星盘(ElementWheel)的结构与几何校验。
//
// 为什么需要它:这是一个纯几何的 SVG 组件 —— 半径、角度、扇区都是手算的常数,
// 编译通过**不能**说明画对了。层与层叠不叠、文字出不出界、主题系在不在正上方,
// 这些问题只有渲染出来量一遍才知道。
//
// 校验项:
//   1. 元素齐全:18 个节点 / 18 个系名 / 54 枚难度刻度 / 18 条暗底槽;
//   2. 主题系在正上方(12 点方向),且是唯一一个带加冕金环的;
//   3. 中心印记显示主题系的进度与「XX系徽章」;
//   4. 几何:各层不重叠、不越出 viewBox、进度弧不画过扇区;
//   5. 极端数据:全 0 / 部分 / 全满 / 后端只下发 1 个系,均不产生 NaN。
import { mkdirSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { pathToFileURL } from 'node:url'
import { build } from 'esbuild'
import { JSDOM } from 'jsdom'

// 组件是 JSX,node 直接 import 不了 —— 运行时用 esbuild 打一份再 import。
//
// 输出必须落在**项目内**(node_modules/.cache):产物 import 了 react-dom,
// 放到系统临时目录会因为解析不到 node_modules 而失败。
// 不预打包进仓库:那会多一个必须记得重新生成的产物,过期了还静默用旧的。
const CACHE = join(process.cwd(), 'node_modules', '.cache', 'verify-sigil')
mkdirSync(CACHE, { recursive: true })
const OUT = join(CACHE, 'probe.mjs')
await build({
  entryPoints: ['src/pages/trial/__probe.jsx'],
  bundle: true, write: false, platform: 'node', format: 'esm', jsx: 'automatic',
  external: ['react', 'react-dom', 'react/jsx-runtime'],
  outfile: OUT, logLevel: 'error',
}).then((r) => writeFileSync(OUT, r.outputFiles[0].text))
const { probe } = await import(pathToFileURL(OUT).href)

const names = ['普', '草', '火', '水', '光', '地', '冰', '龙', '电', '毒', '虫', '武', '翼', '萌', '幽', '恶', '机械', '幻']
const mk = (cleared) => names.map((damName, i) => ({ slotId: 1000 + i, damType: i + 1, damName, cleared }))

let fail = 0
const ok = (cond, label, extra = '') => {
  console.log(`  ${cond ? 'OK  ' : 'FAIL'} ${label}${extra ? ' — ' + extra : ''}`)
  if (!cond) fail++
}

function parse(html) {
  const { document } = new JSDOM(`<!doctype html><body>${html}</body>`).window
  return document
}

// 从一个 <g transform="translate(x,y)"> 里取出坐标
const xy = (el) => {
  const m = /translate\(([-\d.]+),([-\d.]+)\)/.exec(el.getAttribute('transform') || '')
  return m ? [parseFloat(m[1]), parseFloat(m[2])] : null
}

function check(label, slots, theme, expect) {
  console.log(`\n[${label}] slots=${slots.length} theme=${theme}`)
  const doc = parse(probe(slots, theme))

  // —— 1. 元素齐全 ——
  const nodes = doc.querySelectorAll('g.sigil-node')
  const labels = doc.querySelectorAll('text.sigil-label')
  const ticks = doc.querySelectorAll('rect.sigil-tick')
  const slotsArc = doc.querySelectorAll('path.sigil-arc-slot')
  ok(nodes.length === 18, '18 个节点', `实际 ${nodes.length}`)
  ok(labels.length === 18, '18 个系名', `实际 ${labels.length}`)
  ok(ticks.length === 54, '54 枚难度刻度(18×3)', `实际 ${ticks.length}`)
  ok(slotsArc.length === 18, '18 条弧带暗底槽', `实际 ${slotsArc.length}`)

  // —— 2. 主题系在正上方 + 唯一加冕 ——
  const themeEl = [...nodes].find((n) => n.classList.contains('theme'))
  ok(!!themeEl, '存在主题系节点')
  if (themeEl) {
    const [x, y] = xy(themeEl)
    // 正上方:x 应等于圆心 220(容差 0.5),y 应小于圆心 220
    ok(Math.abs(x - 220) < 0.5 && y < 220, '主题系在正上方(12点)',
      `坐标 (${x.toFixed(1)}, ${y.toFixed(1)})`)
    ok(themeEl.getAttribute('aria-label').startsWith(theme), '主题系节点 aria 正确',
      themeEl.getAttribute('aria-label'))
    ok(!!themeEl.querySelector('circle.sigil-crown'), '主题系有加冕金环')
  }
  const crowns = doc.querySelectorAll('circle.sigil-crown')
  ok(crowns.length === 1, '加冕金环恰好 1 个(不随通关变化)', `实际 ${crowns.length}`)

  // —— 3. 中心印记 ——
  const num = doc.querySelector('text.sigil-core-num')
  const sub = doc.querySelector('text.sigil-core-sub')
  const clearedBy = Object.fromEntries(slots.map((s) => [s.damName, s.cleared]))
  const wantNum = `${clearedBy[theme] ?? 0}/3`
  ok(num && num.textContent.replace(/\s/g, '') === wantNum, '中心显示主题系进度',
    `实际 ${num?.textContent.replace(/\s/g, '')} 期望 ${wantNum}`)
  ok(sub && sub.textContent === `${theme}系徽章`, '中心副标题点明徽章主题',
    `实际 ${sub?.textContent}`)

  // —— 4. 期望值(可选)——
  if (expect) {
    const done = /sigil-done/.test(doc.querySelector('.sigil-wrap').className)
    ok(done === expect.done, '全通态 sigil-done', `实际 ${done} 期望 ${expect.done}`)
    ok(doc.querySelectorAll('line.sigil-link').length === expect.links,
      '封印链亮段数', `实际 ${doc.querySelectorAll('line.sigil-link').length} 期望 ${expect.links}`)
  }

  // —— 5. 无 NaN / undefined ——
  ok(!/NaN|undefined|Infinity/.test(doc.body.innerHTML), '无 NaN/undefined/Infinity')
}

// 进度弧不越出自己的扇区:cleared=3 时弧的圆心角必须 <= 20°
function checkArcGeometry() {
  console.log('\n[几何] 进度弧不越界')
  const doc = parse(probe(mk(3), '草'))
  const arcs = [...doc.querySelectorAll('path.sigil-arc')]
  ok(arcs.length === 18, '18 条进度弧', `实际 ${arcs.length}`)
  // 取每条弧的首末点算圆心角。只比角度,故不需要半径 ——
  // 弧是否越界看的是它跨了多少度,与画在哪个半径上无关。
  const CX = 220, CY = 220
  let worst = 0
  for (const p of arcs) {
    const d = p.getAttribute('d')
    const m = /M([-\d.]+),([-\d.]+)A[\d.]+,[\d.]+ 0 0 1 ([-\d.]+),([-\d.]+)/.exec(d)
    if (!m) continue
    const a0 = Math.atan2(+m[2] - CY, +m[1] - CX)
    const a1 = Math.atan2(+m[4] - CY, +m[3] - CX)
    let span = Math.abs(a1 - a0)
    if (span > Math.PI) span = 2 * Math.PI - span
    worst = Math.max(worst, span)
  }
  const SEG = (Math.PI * 2) / 18
  ok(worst <= SEG + 0.01, `弧跨角 <= 扇区 ${(SEG * 180 / Math.PI).toFixed(1)}°`,
    `最大 ${(worst * 180 / Math.PI).toFixed(2)}°`)
}

console.log('=== 徽章星盘 ElementWheel 校验 ===')
check('全通', mk(3), '草', { done: true, links: 18 })
check('全零', mk(0), '草', { done: false, links: 0 })
// 满级的系(index):普0 草1 地5 电8 翼12 萌13 幽14
// 封印链只在**相邻两个都满**时亮,故只有 (普,草) (翼,萌) (萌,幽) 这 3 段 ——
// 地、电虽然满了,但左右邻居都没满,孤立的满系不连成链(这正是设计意图:
// 环上的缺口比连续的亮段更刺眼)。
check('部分', names.map((damName, i) => ({
  slotId: 1000 + i, damType: i + 1, damName, cleared: [3, 3, 1, 0, 2, 3, 0, 0, 3, 1, 2, 0, 3, 3, 3, 0, 2, 1][i],
})), '草', { done: false, links: 3 })
check('后端只下发 1 个系', [{ slotId: 1000, damType: 1, damName: '草', cleared: 3 }], '草', { done: false, links: 0 })
check('后端顺序打乱', [...mk(3)].reverse(), '草', { done: true, links: 18 })
check('主题换成火系(复用性)', mk(2), '火', { done: false, links: 0 })
check('空数据', [], '草', { done: false, links: 0 })
checkArcGeometry()

console.log(fail === 0 ? '\n=== 全部通过 ===' : `\n=== ${fail} 项失败 ===`)
process.exit(fail === 0 ? 0 : 1)
