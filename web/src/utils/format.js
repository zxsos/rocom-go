// 展示层的纯格式化函数(列表/事件/详情统一使用,保证同一信息只有一种写法)。

// 极值高亮:声音接近 ±100、体重百分位接近上下限时按边界方向着色。
// 返回 val-hot-hi(接近上边界)/ val-hot-lo(接近下边界)/ undefined。
export const voiceHot = (v) => v >= 96 ? 'val-hot-hi' : v <= -96 ? 'val-hot-lo' : undefined
export const pctHot = (pct) => pct == null ? undefined : pct >= 98 ? 'val-hot-hi' : pct <= 2 ? 'val-hot-lo' : undefined

// boxLabel 把盒子位置渲染为 "13-性格1 5-2"(排-格,每盒 5 排 × 6 格,slot 从 0 起)。
export function boxLabel(box) {
  if (!box) return '-'
  const name = box.boxName || `盒${box.boxId}`
  const row = Math.floor(box.slot / 6) + 1
  const col = (box.slot % 6) + 1
  return `${box.boxId}-${name} ${row}-${col}`
}

// teamLabel 把队伍位置渲染为 "3-2"(队-位,teamIdx/pos 从 0 起)。
export function teamLabel(team) {
  if (!team) return '-'
  return `${team.teamIdx + 1}-${team.pos + 1}`
}

// locTag 返回宠物位置的【简化文案】(单一权威格式):
// 盒子 📦盒号-盒名 排-格 / 大世界 🌍大世界 队-位 / 尚未落位 ⏳位置待同步。
// 盒位/队位均缺失多为刚捕捉、登录快照之后新增的宠物:游戏「打开盒子」不重传布局,
// 位置要等下次登录 / 挪格 / 整理才会经流量落库,故标「位置待同步」而非留空。
export function locTag(pet) {
  if (pet?.box) return `📦${boxLabel(pet.box)}`
  if (pet?.team) return `🌍大世界 ${teamLabel(pet.team)}`
  return '⏳位置待同步'
}

const pad2 = (n) => String(n).padStart(2, '0')

// fmtTime 把 unix 秒格式化为本地时间(年月日 时分秒)。
export function fmtTime(ts) {
  if (!ts) return '-'
  const d = new Date(ts * 1000)
  return `${d.getFullYear()}-${pad2(d.getMonth() + 1)}-${pad2(d.getDate())} ${pad2(d.getHours())}:${pad2(d.getMinutes())}:${pad2(d.getSeconds())}`
}

// fmtClock 只格式化时:分:秒(实时流事件全在「当下」发生,日期冗余,省横向宽度)。
export function fmtClock(ts) {
  if (!ts) return '-'
  const d = new Date(ts * 1000)
  return `${pad2(d.getHours())}:${pad2(d.getMinutes())}:${pad2(d.getSeconds())}`
}
