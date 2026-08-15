// 孵化进度的本地外推。
//
// 服务器只在下发那一刻给出 hatchedSecs(以及它的计算时刻 hatchUpdate),之后不再推送
// (进度只在开孵蛋器/开背包时才下发,没有被动推送)。要让页面上的进度条动起来,只能本地按
// 「当前值 + 倍率 × 已过秒数」外推。
//
// **倍率不是常数**:平时 1 倍,「孵蛋加速日」活动期间是 2 或 5 倍(2026-08 那期文案写的是
// 「速度提升至500%」),另外玩家跑动、用孵化宝典都会再加。配置里没有可读的倍率字段,
// 所以这里按后端两次采样之间的实际增速反推:pipeline 每次收到新的 hatchedSecs 都会更新,
// 页面据最近两次的差算出倍率;只有一次采样时保守按 1 倍(宁可显示得慢些,也不要虚报可破壳)。
// 详见 docs/data.md 3.6。

export const HATCH_RATE_NOTE =
  '进度按最近两次下发之间的实测增速外推(平时 1 倍速,孵蛋加速日会是 2/5 倍,跑动与孵化宝典还会再加)。' +
  '游戏内打开一次孵蛋器即可让这里对齐。'

// 每颗蛋记住上一次见到的 (hatchUpdate, hatchedSecs),据此估倍率。
// 只在本模块内存里留一份:刷新页面即回到保守的 1 倍,不会把错误估计持久化。
const seen = new Map()

// hatchRate 收下一次采样并返回估出的倍率(秒/秒)。
function hatchRate(egg) {
  const prev = seen.get(egg.gid)
  const cur = { t: egg.hatchUpdate, v: egg.hatchedSecs }
  if (!prev || prev.t !== cur.t) {
    if (prev && cur.t > prev.t && cur.v >= prev.v) {
      const r = (cur.v - prev.v) / (cur.t - prev.t)
      cur.rate = r > 0 ? r : prev.rate
    } else if (prev) {
      cur.rate = prev.rate
    }
    seen.set(egg.gid, cur)
    return cur.rate || 1
  }
  return prev.rate || 1
}

// hatchProgress 返回 {pct, secs} —— 外推到 now(毫秒)的孵化进度;不在孵蛋器里返回 null。
export function hatchProgress(egg, now) {
  if (!egg || !egg.hatching || !egg.maxSecs) return null
  const rate = hatchRate(egg)
  const elapsed = Math.max(0, Math.floor(now / 1000) - (egg.hatchUpdate || 0))
  const secs = Math.min(egg.maxSecs, (egg.hatchedSecs || 0) + elapsed * rate)
  const pct = Math.floor(Math.min(100, (secs / egg.maxSecs) * 100))
  return { pct, secs }
}
