import { useCallback, useEffect } from 'react'
import { getHome, subscribe } from '../../api'
import { pctText } from '../../utils/format'
import { useAsyncData } from '../../hooks/useAsyncData'

// —— 家园小窝图层 ——
// 只有在家园场景才有内容:后端从进场景快照里取家具布局(小窝也是家具)与住户/窝上的蛋,
// 走远/换场景即推空列表(见 internal/pipeline/home.go)。
// 空窝也在列表里——「哪个窝还空着」正是要看的信息之一,故不过滤。
// 不设开关:进了家园就该看得见,不在家园本来就是空列表,没什么可关的。

// 空数据的兜底常量:引用稳定,免得每次渲染都造一个新数组、打穿下游 memo 子组件。
const NO_HOME = { nests: [] }

// useHomeNests 管理小窝图层:拉一次快照,之后订阅后端推送(每次变化都推全量,直接替换)。
// 断线重连期间的增量丢失了,故重连成功时补拉一次快照。
export function useHomeNests(account) {
  const { data, setData, refresh } = useAsyncData(
    useCallback(() => getHome(), []),
    { fallback: NO_HOME, reloadKey: account },
  )

  useEffect(
    () => subscribe('home', (d) => setData(d), { onOpen: refresh }),
    [refresh, setData],
  )

  return { marks: (data || NO_HOME).nests }
}

// nestTitle 组一个小窝标记的悬浮说明,单行「身份 · 个体」:
//   点点 ♀ Lv.1 · W 90.4321% V -50 急躁
// W 是体重百分位、V 是嗓音原值。百分位全站统一口径(pctText,最多 4 位去尾零;见
// utils/rules.js 的 round4):显示与判定必须同一精度,否则 97.9995 会被写成「98.000%」
// 却又不算大块头。只说住户:窝上有没有蛋看标记右上角那个蛋图标即可,不进这行。
export function nestTitle(n) {
  if (!n.pet) return `${n.name || '精灵小窝'}(空)`
  const p = n.pet
  const who = [p.name || p.species, p.gender, p.level ? `Lv.${p.level}` : '']
  const stat = []
  if (p.weightPct != null) stat.push(`W ${pctText(p.weightPct)}`)
  stat.push(`V ${p.voice ?? 0}`, p.nature)
  return [who, stat].map((g) => g.filter(Boolean).join(' ')).filter(Boolean).join(' · ')
}
