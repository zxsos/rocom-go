import { wildTags } from './wildMatch'
import { chime, rareChime } from '../../utils/audio'

// —— 稀有宠出现提醒 ——
// 通知开关独立存 localStorage(与图层状态分开,不占图层版本号)。开启后,后端推来的实体
// 本就全是稀有类别(普通宠不会推,见 internal/pipeline/wildpets.go),但**只有能在地图上
// 带环显示(当前开着且命中的图层/奖牌筛选,见 wildShown)的新宠才提醒**——与 marks 过滤
// 同一口径:画得出环的才算稀有宠,画不出环的(开关关掉/奖牌拖严后不再命中)再出现不打扰。
// **例外:异色/炫彩属于最高优先级**,无论是否开关双牌模式、无论图层开关开关,只要有提醒
// 开关就一定响——它太稀有,不能被任何子筛选拦住(见提醒循环中的短路 continue)。
export const NOTIFY_KEY = 'map.wildNotify.v1'
// 「仅双牌时提醒」子开关:勾选后只有双牌(命中≥2张奖牌)的新出现稀有宠才响提醒,
// 单牌/污染等不响。异色/炫彩因最高优先级不受此限——勾选后仍照常提醒。独立持久化,默认关
// (=所有带环稀有宠都提醒,保持原行为)。
export const NOTIFY_DUAL_ONLY_KEY = 'map.wildNotifyDualOnly.v1'

// fireWildNotify 弹一条系统通知:标题 = 名字 + 类别标签(与资料卡同口径,见 wildTags),
// 正文 = 等级 / 体重百分位 / 坐标;tag 用实体 id,浏览器同 id 自动去重。点击通知聚焦页面。
//
// rangeRules 传进来是为了让标签里带上区间规则的名字:一只宠可能是因为「体重 40~60」
// 这种自定义区间被圈出来的,光看 kinds 标签(后端只有固定四种)说不清它为什么稀有。
export function fireWildNotify(p, rangeRules = []) {
  const tags = wildTags(p, rangeRules)
  const title = `${p.n || '野生宠物'}${tags.length ? ' · ' + tags.join(' ') : ''}`
  const parts = []
  if (p.lv) parts.push('Lv.' + p.lv)
  if (p.weightPct != null) parts.push(`体重 ${Math.round(p.weightPct * 10) / 10}%`)
  parts.push(`X${p.x} Y${p.y} Z${p.z}`)
  // 异色/炫彩是全场最稀有,响更尖更醒目的升级音;其余稀有类别(污染/奖牌四件套)响普通提示音。
  const ks = p.kinds || []
  ;(ks.includes('shiny') || ks.includes('colorful')) ? rareChime() : chime()
  if (!('Notification' in window) || Notification.permission !== 'granted') return
  try {
    const n = new Notification(title, { body: parts.join(' · '), tag: 'wild-' + p.id, renotify: true })
    n.onclick = () => { window.focus(); n.close() }
  } catch { /* 个别环境抛异常:音效已响,不再补 */ }
}
