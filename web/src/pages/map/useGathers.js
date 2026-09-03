import { useState, useEffect, useMemo, useCallback } from 'react'
import { getGathers, subscribe } from '../../api'
import { useAsyncData } from '../../hooks/useAsyncData'

// —— 实时采集物图层(花/草/菌/矿/果树)——
//
// 只有**一个开关**,没有任何筛选:
//   - 实时层显示玩家周围**所有品种**此刻刷着的实体,不可按品种过滤 ——
//     「这会儿身边有什么」是客观事实,筛选只会让人误以为没有。
//
// 后端见 internal/pipeline/gathers.go。要点:
//   - 采完就消失:实体离开(被采走或走出 AOI)后端当场撤标记,**不留灰点**
//     (与野生宠相反 —— 采集物采完会按刷新规则再刷,留灰点会让人白跑一趟)。
//   - 换场景/传送整份作废:旧实体必然已不在视野,留着就是一屏假标记。
//   - 每次推送都是**全量**(实体进出按 150ms 合并),直接替换即可。

// 空数据的兜底常量:引用稳定,免得每次渲染都造新对象、打穿下游的 useMemo。
const NO_GATHERS = { gathers: [] }
const GATHER_LS_KEY = 'map.gatherLayer'

// 默认**开启**:这层只画此刻真有的那几个(个位数到二十来个),不像候选点那样糊屏;
// 而「现在能采什么」正是跑图时最想知道的。用 null 区分「用户没选过」。
const loadOn = () => {
  try {
    const v = localStorage.getItem(GATHER_LS_KEY)
    return v === null ? true : v === '1'
  } catch { return true }
}

// useGathers 管理实时采集物图层:订阅后端推送,按主开关决定画不画。
export function useGathers(account) {
  const { data, setData, refresh } = useAsyncData(
    useCallback(() => getGathers(), []),
    { fallback: NO_GATHERS, reloadKey: account },
  )
  // 收紧成 useMemo:`(data || {}).gathers || []` 每次渲染都造一个新数组,
  // 会让下面的 useMemo 依赖**每次都变**,等于缓存全废 —— 实时层每秒推好几次。
  const gathers = useMemo(() => (data || NO_GATHERS).gathers || [], [data])
  const [on, setOn] = useState(loadOn)

  useEffect(() => {
    try { localStorage.setItem(GATHER_LS_KEY, on ? '1' : '0') } catch { /* 隐私模式下忽略 */ }
  }, [on])

  // 后端每次都推全量列表(实体进出已按窗口合并),整体替换即可;
  // 断线重连期间的增量丢失了,故重连成功时补拉一次全量快照。
  useEffect(() => subscribe('gathers', (d) => setData(d), { onOpen: refresh }), [refresh, setData])

  const toggle = () => setOn((v) => !v)

  // marks 直接透传:没有筛选,开关是唯一的决定项。
  // 位置推送不碰它,引用在 gathers 不变时保持稳定(GatherLayer 级别的 memo 才吃得满)。
  const marks = useMemo(() => (on ? gathers : []), [gathers, on])

  return { marks, on, toggle, total: gathers.length }
}
