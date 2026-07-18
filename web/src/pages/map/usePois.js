import { useState, useEffect, useRef } from 'react'
import { getPois, subscribe } from '../../api'

// —— POI 图层(炼金釜/魔力之源/守护地/眠枭庇护所/眠枭之星/不咕钟零件)——
// 点位与图标由后端按场景下发(GET /api/pois,u/v 已按底图投影,同玩家位置那套),前端只管开关与摆放。
// 默认开哪些由后端 kinds[].on 给(魔力之源 + 炼金釜);用户改过之后记住选择(眠枭之星有几百个点,
// 不该每次进页面都被强行打开)。
const POI_LS_KEY = 'map.poiLayers'
const COLLECT_LS_KEY = 'map.collectLayers' // 开了收集模式的图层键列表(默认全关)
const loadKeys = (key) => {
  try {
    const v = JSON.parse(localStorage.getItem(key))
    return Array.isArray(v) ? v : null // null = 用户没选过,用后端默认
  } catch { return null }
}

// —— 收集模式(可收集图层:眠枭之星与不咕钟零件,后端 kinds[].collect 标记;按图层各自开关)——
// 开启后隐藏该图层已收集的点,只留还没拿的。判定全部来自实测流量(见 docs/data.md 3.4),不做猜测:
//   1) 候选区域全部收满(服务器按区域给「已收集/总数」;点在管辖区重叠带上会有多个候选,
//      p.z 列表里的区域全部 got>=tot 才算)→ 隐藏,不必逐个走到;仅眠枭之星——不咕钟零件
//      没有服务器分区进度,点不带 z,只走第 2 条;
//   2) 逐点确认:玩家走到某点 50m 内而服务器没下发该点的实体 ⇒ 已收集(已收集的不再刷)
//      → 隐藏(石像走挂件状态,见后端)。
// 两条都没命中的点一律**照常显示**——宁可多显示,不能藏掉没拿的。
const ST_UNCOLLECTED = 1 // 收到过实体 ⇒ 还在,未收集
const ST_COLLECTED = 2   // 走近了却没实体 ⇒ 已收集

// usePois 管理某场景的 POI 图层:点位/图层开关/可收集点(星星/零件)收集状态,返回筛好的可绘制标记。
export function usePois(account, res) {
  // poi 是当前场景的图层清单与点位;poiOn 是已开启的图层键集合。
  const [poi, setPoi] = useState({ kinds: [], pois: [], zones: [] })
  const [poiOn, setPoiOn] = useState(() => new Set(loadKeys(POI_LS_KEY) || []))
  const poiPicked = useRef(loadKeys(POI_LS_KEY) !== null) // 用户是否手动选过(未选过则跟随后端默认)
  const [collectOn, setCollectOn] = useState(() => new Set(loadKeys(COLLECT_LS_KEY) || []))
  const [starSt, setStarSt] = useState({}) // 刷新点 id -> 1未收集/2已收集(随玩家移动由后端推增量)
  const [poiVer, setPoiVer] = useState(0)  // 区域进度变化时递增,触发重取点位

  // POI 随场景走(每个场景的点位/图层不同):换 scene_res 就重取。首次(用户没手动选过图层)
  // 按后端 kinds[].on 初始化开关。
  useEffect(() => {
    if (!res) return
    let alive = true
    getPois(res).then((d) => {
      if (!alive) return
      setPoi(d)
      // 逐点状态随点位一起来(库里已确认的);之后由 SSE 推增量。
      setStarSt(Object.fromEntries(d.pois.filter((p) => p.r).map((p) => [p.r, p.st || 0])))
      if (!poiPicked.current) setPoiOn(new Set(d.kinds.filter((k) => k.on).map((k) => k.k)))
    }).catch(() => {})
    return () => { alive = false }
  }, [res, poiVer])

  // 收集状态增量:玩家一边走,后端一边判定(走近却没实体 ⇒ 已收集),即时推过来。
  // 区域进度只在进场景时更新(区域隐藏用),那时重取一次点位即可。
  useEffect(() => subscribe((m) => {
    if (m.type === 'stars') setStarSt((prev) => ({ ...prev, ...m.data }))
    if (m.type === 'starzones') setPoiVer((v) => v + 1)
  }), [account])

  const togglePoi = (k) => {
    setPoiOn((prev) => {
      const next = new Set(prev)
      next.has(k) ? next.delete(k) : next.add(k)
      poiPicked.current = true
      localStorage.setItem(POI_LS_KEY, JSON.stringify([...next]))
      return next
    })
  }
  // 某图层的收集模式开关(仅可收集图层有,LayerPanel 摆在该图层开关右侧)。
  const toggleCollect = (k) => {
    setCollectOn((prev) => {
      const next = new Set(prev)
      next.has(k) ? next.delete(k) : next.add(k)
      localStorage.setItem(COLLECT_LS_KEY, JSON.stringify([...next]))
      return next
    })
  }

  // 本场景有点位的图层才给开关(如魔法学院只有魔力之源);标记只画开启的图层。
  const kinds = poi.kinds.filter((k) => k.num > 0)
  const iconOf = Object.fromEntries(poi.kinds.map((k) => [k.k, k.icon]))
  // 已收满的区域(服务器口径 got>=tot)。
  const doneZones = new Set((poi.zones || []).filter((z) => z.tot > 0 && z.got >= z.tot).map((z) => z.camp))
  // 收集模式下隐藏「已收集」的点:逐点确认过的,或候选区域(p.z 列表)全部收满的。其余一律显示。
  const collected = (p) => starSt[p.r] === ST_COLLECTED || (p.z?.length > 0 && p.z.every((c) => doneZones.has(c)))
  const marks = poi.pois.filter((p) => {
    if (!poiOn.has(p.k)) return false
    if (!p.r || !collectOn.has(p.k)) return true
    return !collected(p)
  })
  // 开着收集模式的图层里「已确认还在」的点(高亮一圈;其余是尚未走到过的候选点)。
  const isSure = (p) => collectOn.has(p.k) && starSt[p.r] === ST_UNCOLLECTED

  return { kinds, iconOf, marks, isSure, poiOn, togglePoi, collectOn, toggleCollect }
}
