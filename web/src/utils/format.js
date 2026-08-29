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

// pad2 补零到两位(时间字段的通用补位,各页自用一份的都收到这里)。
export const pad2 = (n) => String(n).padStart(2, '0')

// fmtTime 把 unix 秒格式化为本地时间(年月日 时分秒)。
export function fmtTime(ts) {
  if (!ts) return '-'
  const d = new Date(ts * 1000)
  return `${d.getFullYear()}-${pad2(d.getMonth() + 1)}-${pad2(d.getDate())} ${pad2(d.getHours())}:${pad2(d.getMinutes())}:${pad2(d.getSeconds())}`
}

// fmtShortTime 省略年份的「月-日 时:分」:管理面板的表格列窄、且都是近期数据,年份冗余。
// 与 fmtTime 只差年份与秒;两者刻意分开命名,免得同名不同格式的坑(曾有两个 fmtTime 并存)。
export function fmtShortTime(ts) {
  if (ts == null) return '-'
  const d = new Date(ts * 1000)
  return `${pad2(d.getMonth() + 1)}-${pad2(d.getDate())} ${pad2(d.getHours())}:${pad2(d.getMinutes())}`
}

// fmtClock 只格式化时:分:秒(实时流事件全在「当下」发生,日期冗余,省横向宽度)。
export function fmtClock(ts) {
  if (!ts) return '-'
  const d = new Date(ts * 1000)
  return `${pad2(d.getHours())}:${pad2(d.getMinutes())}:${pad2(d.getSeconds())}`
}

// maskUid 把 UID 半隐藏显示:保留前 3 后 3,中间以 ＊＊＊ 代替(1234567890 → 123＊＊＊890)。
// 用于花种页「当前世界/自己世界/好友槽名」等展示位,防止完整 UID 被旁观者直接看到;
// 位数不足 7 位时整体隐藏意义不大,原样返回。
export function maskUid(uid) {
  const s = String(uid || '')
  if (s.length <= 6) return s
  return s.slice(0, 3) + '＊＊＊' + s.slice(-3)
}

// maskEmail 邮箱脱敏:local 保留前 2 与末 1,中间打星;local 过短(≤2)只留首位,域名完整保留。
// 用于远行商人订阅成功后折叠展示,防旁窥(与 maskUid 同为「脱敏展示」族)。
export function maskEmail(email) {
  const s = String(email || '')
  const i = s.indexOf('@')
  if (i < 2) return s // 无 @ 或 local 过短,原样返回
  const local = s.slice(0, i)
  if (local.length <= 2) return local[0] + '*'.repeat(local.length - 1) + s.slice(i)
  return local.slice(0, 2) + '*'.repeat(local.length - 3) + local.slice(-1) + s.slice(i)
}
