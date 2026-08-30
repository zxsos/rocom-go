// 实时地图「箭头平滑移动」的回归度量(多 fixture)。
//
//   node scripts/verify-motion.mjs
//
// 存在理由:motion.js 里那几个常数(EXTRAP_TAU / GLIDE_RATIO / TAU_RATIO …)此前全靠
// 「跑一把看看手感」来调,改坏了没人知道 —— 单帧跳 20px 也不会有任何测试报警。
// 本脚本用**真实抓包**驱动真实的 motion.js,把「平不平」变成可比较的数字。
//
// 做法:
//  1. fixture 是真实移动包经 pipeline 的 ParseMoveReq 解析 + gamedata 同款投影后的形状
//     (即 buildPos 推给前端的东西),由 `go run ./cmd/movean -out f.json x.pcap` 生成,
//     见 fixtures/README.md;
//  2. 真值 = 包位置 + move_seg_list 补报的轨迹点(带服务器时间戳),按时间线性插值;
//  3. 以 60fps 跑 useMapEngine.applyFrame 的同款公式,逐帧比对真值。
//
// 三个指标各有分工,缺一个就会被「拆东墙补西墙」蒙混过去:
//   - 偏差(米):箭头离玩家真实位置多远 —— 总体准不准;
//   - 每帧位移最大值(米):单帧跳多远 —— 抽搐/瞬移;
//   - 抽搐帧占比:超过「真实最大速度对应步长」的帧 —— 抖动频率。
//
// 每份 fixture 独立设门槛(移动特征不同,绝对值不可比),**任一超标即失败**。
// 门槛略放宽于当前实测(约 15%),避免换机器/浮点次序造成假红。
// 若某次改动有意让数字变差(换取别的收益),请连同理由一起更新门槛,不要删断言。
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const here = path.dirname(fileURLToPath(import.meta.url))
const M = await import(path.join(here, '../src/pages/map/motion.js'))

const FPS = 60

// 各 fixture 的门槛。实测值随 motion.js 改动而变,改完请用 `node scripts/verify-motion.mjs -v`
// 打印实测值再更新这里,并**在提交信息里写清为什么变了**。
const FIXTURES = [
  {
    file: 'tap-move.json',
    name: '点触式走走停停',
    // 137 包/80.6s,33% 是 stop 包,骑乘峰值 29m/s。最能暴露启停问题。
    // errMax 比此前(23.5)放宽是**有意的**:外推从「全时程衰减」改为「全速保持 0.6s 再衰减」
    // 后,均值 -12%、抽搐率 -24%,但长静默段的尾部变大(29.3)。取舍理由见 motion.js 的
    // EXTRAP_HOLD 注释 —— 实测「停下后箭头并不冲过头」(过冲中位 -1.9m,即落后而非超前),
    // 真正的病灶是滞后,故选择多外推,接受尾部变大。
    limit: { errMean: 7.7, errP95: 18.3, errMax: 34, jumpP99: 1.5, jumpMax: 3.05, jitter: 0.019 },
  },
  {
    file: 'continuous.json',
    name: '连续移动',
    // 1088 包/268.5s,密度高(间隔中位 0.12s、p95 0.97s),stop 仅 9%。考验跟手与稳态抖动。
    limit: { errMean: 3.1, errP95: 13.2, errMax: 24.1, jumpP99: 1.0, jumpMax: 1.9, jitter: 0.006 },
  },
]

// motion.js 读 performance.now() 当锚点时刻,这里接管成可控时钟。
let NOW = 0
Object.defineProperty(globalThis, 'performance', { value: { now: () => NOW }, writable: true })

function load(file) {
  const raw = JSON.parse(fs.readFileSync(path.join(here, 'fixtures', file), 'utf8'))
  const SIDE = raw.side
  const PK = raw.pkts
  // —— 真值时序:补报轨迹点(服务器时间戳)+ 包位置,按时间插值 ——
  // to_pos 比末个 seg 略新(实测差 0.2-0.6 个采样步长),故强制不早于末 seg。
  const pts = []
  for (const p of PK) {
    if (p.path && p.path.length >= 2) {
      let lastT = -Infinity
      for (const q of p.path) {
        const t = Math.max(q.t, lastT + 1e-3)
        pts.push({ t, u: q.u, v: q.v })
        lastT = t
      }
      pts.push({ t: Math.max(p.t, lastT + 0.05), u: p.u, v: p.v })
    } else {
      pts.push({ t: p.t, u: p.u, v: p.v })
    }
  }
  pts.sort((a, b) => a.t - b.t)
  for (let i = 1; i < pts.length; i++) if (pts[i].t <= pts[i - 1].t) pts[i].t = pts[i - 1].t + 1e-3
  let ti = 0
  const truthAt = (t) => {
    if (t <= pts[0].t) return { u: pts[0].u, v: pts[0].v }
    if (t >= pts[pts.length - 1].t) { const l = pts[pts.length - 1]; return { u: l.u, v: l.v } }
    while (ti < pts.length - 2 && pts[ti + 1].t < t) ti++
    while (ti > 0 && pts[ti].t > t) ti--
    const a = pts[ti], b = pts[ti + 1], f = (t - a.t) / (b.t - a.t)
    return { u: a.u + (b.u - a.u) * f, v: a.v + (b.v - a.v) * f }
  }
  return { SIDE, PK, truthAt }
}

// 与 useMapEngine 的 applyPos + applyFrame 同构:建锚点 → 逐帧外推 + 指数回拉。
function simulate({ SIDE, PK, truthAt }) {
  const toM = (d) => (d * SIDE) / 100 // 归一化坐标差 → 米
  const t0 = PK[0].t, tEnd = PK[PK.length - 1].t
  let pi = 0, a = null, disp = null, prev = null
  const errs = [], jumps = []
  for (let t = t0; t <= tEnd; t += 1 / FPS) {
    while (pi < PK.length && PK[pi].t <= t) {
      NOW = t * 1000
      a = M.makeAnchor(PK[pi], disp, false)
      pi++
    }
    if (!a) continue
    NOW = t * 1000
    const dt = (NOW - a.t0) / 1000
    const tau = a.tau || M.SMOOTH_TAU
    const decay = dt >= tau * M.TAU_CUTOFF ? 0 : Math.exp(-dt / tau)
    const q = M.posAt(a, dt)
    disp = { u: q.u + a.cu * decay, v: q.v + a.cv * decay }
    const T = truthAt(t)
    errs.push(toM(Math.hypot(disp.u - T.u, disp.v - T.v)))
    if (prev) jumps.push(toM(Math.hypot(disp.u - prev.u, disp.v - prev.v)))
    prev = disp
  }
  return { errs, jumps }
}

const q = (xs, p) => { const s = [...xs].sort((x, y) => x - y); return s[Math.min(s.length - 1, Math.floor(p * s.length))] }
const mean = (xs) => xs.reduce((a, b) => a + b, 0) / xs.length

let failed = 0
for (const fx of FIXTURES) {
  const data = load(fx.file)
  const { errs, jumps } = simulate(data)
  const got = {
    errMean: mean(errs),
    errP95: q(errs, .95),
    errMax: Math.max(...errs),
    jumpP99: q(jumps, .99),
    jumpMax: Math.max(...jumps),
    // >1m/帧 = 瞬时 60m/s。两份抓包的真实峰值都约 30m/s(合 0.5m/帧),故 1m 已属异常。
    jitter: jumps.filter((x) => x > 1).length / jumps.length,
  }
  console.log(`\n## ${fx.name}  (${fx.file}, ${data.PK.length} 包 / ${(data.PK[data.PK.length - 1].t - data.PK[0].t).toFixed(1)}s)`)
  const bad = []
  for (const k of Object.keys(fx.limit)) {
    const ok = got[k] <= fx.limit[k]
    if (!ok) bad.push(k)
    console.log(`  ${ok ? 'ok  ' : 'FAIL'} ${k.padEnd(9)} ${got[k].toFixed(3).padStart(8)}  (门槛 ${fx.limit[k]})`)
  }
  console.log(`  偏差 均值 ${got.errMean.toFixed(1)}m / p95 ${got.errP95.toFixed(1)}m / 最大 ${got.errMax.toFixed(1)}m` +
    ` · 每帧位移 p99 ${got.jumpP99.toFixed(2)}m / 最大 ${got.jumpMax.toFixed(2)}m · 抽搐帧 ${(got.jitter * 100).toFixed(1)}%`)
  if (bad.length) {
    failed++
    console.error(`  ✗ ${bad.join(', ')} 超标`)
  }
}

if (failed) {
  console.error('\n✗ 平滑指标劣化')
  process.exit(1)
}
console.log('\n✓ 全部 fixture 在门槛内')
