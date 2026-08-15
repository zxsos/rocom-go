import { useState, useEffect } from 'react'
import { getHome, subscribe } from '../../api'

// —— 家园小窝图层 ——
// 只有在家园场景才有内容:后端从进场景快照里取家具布局(小窝也是家具)与住户/窝上的蛋,
// 走远/换场景即推空列表(见 internal/pipeline/home.go)。
// 空窝也在列表里——「哪个窝还空着」正是要看的信息之一,故不过滤。
const LS_KEY = 'map.homeNests'

// useHomeNests 管理小窝图层:订阅后端推送 + 开关(默认开,进了家园就该看得见)。
export function useHomeNests(account) {
  const [nests, setNests] = useState([])
  // 配对信息只在进家园那一刻下发一次:期间有宠物进/出窝后它就可能不全(见后端 couplesStale)。
  const [stale, setStale] = useState(false)
  const [on, setOn] = useState(() => localStorage.getItem(LS_KEY) !== '0')

  useEffect(() => {
    let alive = true
    setNests([]); setStale(false)
    getHome().then((d) => {
      if (alive && d) { setNests(d.nests || []); setStale(!!d.couplesStale) }
    }).catch(() => {})
    return () => { alive = false }
  }, [account])

  // 后端每次变化都推全量(进家园、收走一颗蛋、宠物进出窝),直接替换。
  useEffect(() => subscribe((m) => {
    if (m.type === 'home') { setNests(m.data.nests || []); setStale(!!m.data.couplesStale) }
  }), [account])

  const toggle = () => setOn((v) => {
    localStorage.setItem(LS_KEY, v ? '0' : '1')
    return !v
  })

  const marks = on ? nests : []
  const used = nests.filter((n) => n.pet).length
  const eggs = nests.filter((n) => n.egg).length
  return { marks, on, toggle, total: nests.length, used, eggs, stale }
}

// nestTitle 组一个小窝标记的悬浮说明:
//   小独角兽 ♀ Lv.1 · 1.39m 95% / 54.6kg 93% · 声音 100 · 胆小 · 喂食 4 轮
//   配对:小独角兽(37620)
//   窝上有:小独角兽的蛋
export function nestTitle(n, stale) {
  if (!n.pet) return `${n.name || '精灵小窝'}(空)`
  const p = n.pet
  const pct = (v) => (v == null ? '' : ` ${Math.round(v)}%`)
  const head = [p.name || p.species, p.gender, p.level ? `Lv.${p.level}` : ''].filter(Boolean).join(' ')
  const size = [
    p.heightM ? `${p.heightM}m${pct(p.heightPct)}` : '',
    p.weightKg ? `${p.weightKg}kg${pct(p.weightPct)}` : '',
  ].filter(Boolean).join(' / ')
  const line1 = [head, size, `声音 ${p.voice ?? 0}`, p.nature, p.feedRound ? `喂食 ${p.feedRound} 轮` : '']
    .filter(Boolean).join(' · ')
  const lines = [line1]
  if (p.mates && p.mates.length) {
    lines.push('配对:' + p.mates.map((m) => `${m.name}(${m.gid})`).join('、') +
      (p.mates.length > 1 ? '(串窝,父本不唯一)' : ''))
  } else if (stale) {
    lines.push('配对:未知(本次进家园后有宠物进出窝,重进一次即可刷新)')
  }
  if (n.egg) lines.push('窝上有:' + (n.egg.name || '精灵蛋'))
  return lines.join('\n')
}
