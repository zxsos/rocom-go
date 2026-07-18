// 地图视图与箭头平滑移动的纯计算部分(不含 React 状态)。

export const ZOOM_MIN = 1
export const ZOOM_MAX = 10
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
const GLIDE = 0.45 // 沿真实轨迹追平的时长(秒):这段轨迹本是过去几秒走的,快放一遍即可
export const SMOOTH_TAU = 0.12 // 误差收敛时间常数(秒):新包与外推位置的落差按 e^(-Δt/τ) 抹平,而非硬跳
const SNAP_DIST = 0.005 // 落差超过底图边长的 0.5%(几十米)判为传送/换场景:直接跳过去,不做平滑
const angleDiff = (a, b) => (((a - b) % 360) + 540) % 360 - 180 // a-b 折算到 (-180,180]
const easeOut = (x) => 1 - (1 - x) * (1 - x)

const dpr = window.devicePixelRatio || 1
// snap 把平移量对齐整设备像素。底图与洞穴层图是两个元素,浏览器绘制时各自把位置吸附到整像素;
// 若容器按小数像素逐帧平移,两者的吸附时机会错开,看起来就是层图与底图错位抖动(Firefox 实测
// 相对位移抖 1px;Chromium 把整个地图合成为一张纹理、平移不重绘,故几乎看不出——但不能指望)。
// 平移量落在设备像素网格上后,两者每帧的吸附结果恒定,相对位置就锁死了。代价是地图以 1 设备像素
// 为步进移动:跟随时地图本就只有几 px/s,肉眼无感。
export const snap = (n) => Math.round(n * dpr) / dpr

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
export const posAt = (a, dt) => {
  if (a.cum && dt < GLIDE) return pathAt(a.path, a.cum, easeOut(dt / GLIDE))
  const t = dt - (a.cum ? GLIDE : 0) // 回放结束时正好停在上报位置,由此继续外推
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
  // 心跳空窗后补报的真实轨迹(那几秒实际走过的点,末点即本包位置):预先算好累计弧长供回放取点。
  if (p.path && p.path.length >= 2) {
    const cum = [0]
    for (let i = 1; i < p.path.length; i++) {
      cum.push(cum[i - 1] + Math.hypot(p.path[i].u - p.path[i - 1].u, p.path[i].v - p.path[i - 1].v))
    }
    if (cum[cum.length - 1] > 0) { a.path = p.path; a.cum = cum }
  }
  // 与画面当前位置的落差:小落差(外推的正常误差)平滑抹平;换场景/传送这种大落差直接跳过去。
  // 有轨迹时起点是轨迹首点(箭头先并入真实路线),故落差按它算。
  const start = posAt(a, 0)
  if (disp && !sceneChanged && Math.hypot(disp.u - start.u, disp.v - start.v) < SNAP_DIST) {
    a.cu = disp.u - start.u
    a.cv = disp.v - start.v
    a.dh = angleDiff(disp.heading, a.heading) // 转向同样平滑,不硬掰
  }
  return a
}
