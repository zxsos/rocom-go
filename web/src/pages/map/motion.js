// 地图视图与箭头平滑移动的纯计算部分(不含 React 状态)。

export const ZOOM_MIN = 1
// 放大上限按底图原始分辨率设定:卡洛西亚大陆/魔法学院 4096、家园/种植园 2048。
// mapPx = 视口短边 × zoom,桌面视口短边约 600-900px,zoom=32 时 mapPx≈2-2.9万px,
// 相对原始像素约放大 5-7 倍,已到贴图纹理的可用细节极限(再大只剩模糊)。
export const ZOOM_MAX = 32
// 各场景默认缩放(按细节人工调优):卡洛西亚大陆/魔法学院 5、家园室内 2、种植园 3;
// 未列出的场景回退 ZOOM_FALLBACK。键为 scene_res_cfg_id。
const ZOOM_DEFAULTS = { 10003: 5, 10018: 5, 30001: 2, 30002: 3 }
export const ZOOM_FALLBACK = 5
export const clamp = (v, lo, hi) => Math.max(lo, Math.min(hi, v))
// 默认缩放按场景(底图)决定;洞穴层只是叠加在底图上,进层不改缩放(与外层保持一致)。
export const defaultZoom = (p) => ZOOM_DEFAULTS[p && p.sceneResId] || ZOOM_FALLBACK

// —— 平滑移动(航位推算 + 真实轨迹回放)——
// 移动包是按操作事件上报的(地面与飞行同理):持续改方向/变速时约 0.1s 一包;推住摇杆不动、
// 直线巡航或坐骑自行盘旋时输入不变,就退化成约 2.5-3s 一次心跳。若收到才画,箭头会定住再硬跳。
// 故沿用客户端给其他玩家做平滑的同一套办法:每包除位置外还带速度向量(vu/vv,归一化底图坐标每秒,
// 后端投影,见 cmd/rocom-capture),两包之间逐帧外推 pos + v*Δt。实测预测下一包实际位置的误差中位
// 仅 3cm(地面)、2.5m(飞行巡航),都远小于"收到才画"的硬跳。
//
// 心跳空窗里如果玩家其实在转弯(推住摇杆盘旋),外推必然偏出去——但那几秒实际走的路会随下一包的
// path(后端投影自 move_seg_list)补报上来:箭头届时沿这条**真实曲线**滑回正轨(GLIDE 秒内追平),
// 而不是直线跳过去。转向本身最多晚一个心跳(~3s)才可见,那是游戏的上报节奏决定的,任何画法都提前
// 不了(实测:此时直线外推仍是各策略中最准的,阻尼/定住/圆弧都更差)。见 docs/protocol.md 6。
const MAX_EXTRAP = 3.5 // 外推上限(秒):超过心跳间隔仍无新包(抓包中断/掉线)就停住,免得一路飘走

// 沿真实轨迹追平的时长(秒)。
//
// 回放的**倍速** = (len/glide) / speed,而 len = speed × 心跳间隔,故倍速 = 心跳间隔 / glide
// —— 与移动速度无关!所以"高速时回放更快"是错觉:固定 0.45s 时无论快慢都是约 6.7 倍速
// (2.5~3s 的心跳 / 0.45s),只是高速下 6.7 倍速对应的绝对位移更大、看着更冲。
//
// 改为按「以真实速度走完这段路所需秒数」(即 len/speed = 心跳间隔)× 压缩系数:
// 倍速恒定 = 1/GLIDE_RATIO,不再随心跳间隔长短波动,且比原来的 6.7 倍温和。
const GLIDE_MIN = 0.3
const GLIDE_MAX = 1.2
const GLIDE_RATIO = 0.25 // 倍速 = 1/0.25 = 4 倍(原为约 6.7 倍);仍快于实时,免延迟感过重

// glideFor 按轨迹跨度与当前速度算回放时长:len/speed 即「真实速度走完所需秒数」。
// 速度为 0(停下)或跨度退化(0)时无从计算,回退到最短时长。
const glideFor = (len, speed) =>
  (len > 0 && speed > 0) ? clamp((len / speed) * GLIDE_RATIO, GLIDE_MIN, GLIDE_MAX) : GLIDE_MIN

export const SMOOTH_TAU = 0.12 // 误差收敛时间常数(秒):新包与外推位置的落差按 e^(-Δt/τ) 抹平,而非硬跳
// —— 高速移动下真正的"冲刺"来源在这里 ——
// 指数衰减的**峰值修正速度** = 落差 / τ。τ 固定 0.12s 时:
//   低速外推准,落差几米 → 峰值几十 m/s,与实际速度同量级,无感;
//   高速大落差(心跳空窗里转弯,补报落差十几米)→ 峰值上百 m/s,可达实际速度的十几倍,
//   即箭头猛地一冲再归位。落差与速度成正比,τ 却不变,倍数失控就在所难免。
//
// 故 τ 改为按「落差换算成落后了几秒的路」(落差/速度)来定:让追赶耗时 = 落后秒数 / TAU_RATIO,
// 峰值修正速度恒定 = 实际速度 × TAU_RATIO,与速度快慢无关。
//   外推准(落后零点几秒)→ τ 小,收敛快,跟手;
//   补报大落差(落后约一个心跳)→ τ 约 1s,平滑归位而非冲刺。
const TAU_RATIO = 3 // 峰值修正速度 = 实际速度 × TAU_RATIO
const TAU_MAX = 1.2
// tauFor 按落差与速度算收敛时间常数。落差或速度为 0 时回退基准 τ。
const tauFor = (gap, speed) =>
  (gap > 0 && speed > 0) ? clamp((gap / speed) / TAU_RATIO, SMOOTH_TAU, TAU_MAX) : SMOOTH_TAU
// 衰减截止倍数:dt 超过 8τ 后 decay 直接归零。原因——e^(-dt/τ) 永不为 0,亚像素小数经 snap 的
// Math.round 在整数边界(如 0.4999↔0.5001)反复跳,玩家静止时箭头/地图每帧抖 1px,即典型抽搐。
// 8τ 时残差已 < 0.034%,肉眼与像素吸附都无感;此后置零,画面就锁死在收敛值上,稳定不抖。
// τ 现为逐锚点动态值(见 TAU_RATIO),故由调用方按 a.tau * TAU_CUTOFF 算,不再导出固定常量。
export const TAU_CUTOFF = 8
const SNAP_DIST = 0.005 // 落差超过底图边长的 0.5%(几十米)判为传送/换场景:直接跳过去,不做平滑
const angleDiff = (a, b) => (((a - b) % 360) + 540) % 360 - 180 // a-b 折算到 (-180,180]
const easeOut = (x) => 1 - (1 - x) * (1 - x)

// snap 把平移量对齐整设备像素。底图与洞穴层图是两个元素,浏览器绘制时各自把位置吸附到整像素;
// 若容器按小数像素逐帧平移,两者的吸附时机会错开,看起来就是层图与底图错位抖动(Firefox 实测
// 相对位移抖 1px;Chromium 把整个地图合成为一张纹理、平移不重绘,故几乎看不出——但不能指望)。
// 平移量落在设备像素网格上后,两者每帧的吸附结果恒定,相对位置就锁死了。代价是地图以 1 设备像素
// 为步进移动:跟随时地图本就只有几 px/s,肉眼无感。
// dpr 运行时读取(而非模块加载时固化):窗口跨到不同 dpr 的屏幕、或系统缩放变化时,吸附网格
// 仍与实际设备像素一致,否则箭头会相对地图抖半个像素。
export const snap = (n) => {
  const dpr = window.devicePixelRatio || 1
  return Math.round(n * dpr) / dpr
}

// pathAt 取折线上按弧长比例 r∈[0,1] 的点(cum 为累计弧长,末点即上报位置)。
const pathAt = (path, cum, r) => {
  const target = r * cum[cum.length - 1]
  let i = 1
  while (i < cum.length - 1 && cum[i] < target) i++
  const seg = cum[i] - cum[i - 1]
  const f = seg > 0 ? (target - cum[i - 1]) / seg : 1
  const a = path[i - 1], b = path[i]
  return { u: a.u + (b.u - a.u) * f, v: a.v + (b.v - a.v) * f }
}

// posAt 是锚点在其之后 dt 秒的应有位置(不含误差修正):先回放真实轨迹(有的话),再按速度外推。
// 回放时长取锚点自带的 glide(按跨度自适应,见 glideFor),不再用固定常量。
export const posAt = (a, dt) => {
  const g = a.glide || GLIDE_MIN
  if (a.cum && dt < g) return pathAt(a.path, a.cum, easeOut(dt / g))
  const t = dt - (a.cum ? g : 0) // 回放结束时正好停在上报位置,由此继续外推
  const ex = Math.min(t, MAX_EXTRAP)
  return { u: a.u + a.vu * ex, v: a.v + a.vv * ex }
}

// makeAnchor 由一个移动包构造逐帧外推的锚点:位置/速度/朝向 + 收到它时与画面位置的落差(cu/cv/dh)。
// 停下的包不带速度(vu/vv 缺省),外推量自然为零。
export function makeAnchor(p, disp, sceneChanged) {
  const a = {
    u: p.u, v: p.v, vu: p.vu || 0, vv: p.vv || 0, heading: p.heading || 0,
    t0: performance.now(), cu: 0, cv: 0, dh: 0,
  }
  const speed = Math.hypot(a.vu, a.vv)
  // 心跳空窗后补报的真实轨迹(那几秒实际走过的点,末点即本包位置):预先算好累计弧长供回放取点。
  if (p.path && p.path.length >= 2) {
    const cum = [0]
    for (let i = 1; i < p.path.length; i++) {
      cum.push(cum[i - 1] + Math.hypot(p.path[i].u - p.path[i - 1].u, p.path[i].v - p.path[i - 1].v))
    }
    if (cum[cum.length - 1] > 0) {
      a.path = p.path
      a.cum = cum
      a.glide = glideFor(cum[cum.length - 1], speed)
    }
  }
  // 无轨迹也给个值:posAt 与静止判定都要读,缺了会退化成 undefined 比较(恒假)。
  if (a.glide === undefined) a.glide = GLIDE_MIN
  // 与画面当前位置的落差:小落差(外推的正常误差)平滑抹平;换场景/传送这种大落差直接跳过去。
  // 有轨迹时起点是轨迹首点(箭头先并入真实路线),故落差按它算。
  // 注意:带轨迹的包必然是普通走路(传送落点是 onTeleport 合成的无轨迹 MoveReq),其轨迹首点
  // 是几秒前的位置,与外推画面的落差就是心跳间隔的路程(15-25m),通常远超 SNAP_DIST——若仍按
  // 传送判定硬跳,直线走路时箭头会先瞬移到轨迹首点、再在 GLIDE 内回放归位,即「瞬移一点又归位」。
  // 故有轨迹时无条件允许平滑,只有无轨迹的包才用 SNAP_DIST 判别传送/换场景。
  const start = posAt(a, 0)
  if (disp && !sceneChanged && (a.cum || Math.hypot(disp.u - start.u, disp.v - start.v) < SNAP_DIST)) {
    a.cu = disp.u - start.u
    a.cv = disp.v - start.v
    a.dh = angleDiff(disp.heading, a.heading) // 转向同样平滑,不硬掰
  }
  // τ 必须等落差算出来再定:它按「落差 = 落后了几秒的路」缩放,
  // 使峰值修正速度恒为实际速度的 TAU_RATIO 倍(见 tauFor 的注释)。
  a.tau = tauFor(Math.hypot(a.cu, a.cv), speed)
  return a
}
