import { useState, useEffect } from 'react'
import { getHome, subscribe } from '../../api'

// —— 家园小窝图层 ——
// 只有在家园场景才有内容:后端从进场景快照里取家具布局(小窝也是家具)与住户/窝上的蛋,
// 走远/换场景即推空列表(见 internal/pipeline/home.go)。
// 空窝也在列表里——「哪个窝还空着」正是要看的信息之一,故不过滤。
// 不设开关:进了家园就该看得见,不在家园本来就是空列表,没什么可关的。

// useHomeNests 管理小窝图层:订阅后端推送即可。
export function useHomeNests(account) {
  const [nests, setNests] = useState([])

  useEffect(() => {
    let alive = true
    setNests([])
    getHome().then((d) => { if (alive && d) setNests(d.nests || []) }).catch(() => {})
    return () => { alive = false }
  }, [account])

  // 后端每次变化都推全量(进家园、收走一颗蛋、宠物进出窝),直接替换。
  useEffect(() => subscribe((m) => {
    if (m.type === 'home') setNests(m.data.nests || [])
  }), [account])

  return { marks: nests }
}

// nestTitle 组一个小窝标记的悬浮说明,单行「身份 · 个体」:
//   点点 ♀ Lv.1 · W 90% V -50 急躁
// W 是体重百分位、V 是嗓音原值,与野生宠物标记/事件页同一口径。
// 只说住户:窝上有没有蛋看标记右上角那个蛋图标即可,不进这行。
export function nestTitle(n) {
  if (!n.pet) return `${n.name || '精灵小窝'}(空)`
  const p = n.pet
  const who = [p.name || p.species, p.gender, p.level ? `Lv.${p.level}` : '']
  const stat = []
  if (p.weightPct != null) stat.push(`W ${Math.round(p.weightPct)}%`)
  stat.push(`V ${p.voice ?? 0}`, p.nature)
  return [who, stat].map((g) => g.filter(Boolean).join(' ')).filter(Boolean).join(' · ')
}
