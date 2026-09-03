import { useState, useEffect, useRef, useMemo, useCallback } from 'react'
import { getPois, subscribe } from '../../api'
import { useAsyncData } from '../../hooks/useAsyncData'

// 空数据的兜底常量:引用稳定,免得每次渲染都造新对象、打穿下游的 useMemo。
const NO_POIS = { kinds: [], pois: [], zones: [] }

// GATHER_KIND 是采集物图层的键,与后端 poi_kinds 的 k、POI.K 同源。
// 导出给 useGathers 复用:那边判断「这是不是采集物点位」要用同一个键,
// 两处各写一份字符串,改一处漏一处就会静默错(表现为筛选对某个图层失效)。
export const GATHER_KIND = 'gather'

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
//      p.zone 列表里的区域全部 got>=tot 才算)→ 隐藏,不必逐个走到;仅眠枭之星——不咕钟零件
//      没有服务器分区进度,点不带 zone,只走第 2 条;
//   2) 逐点确认:玩家走到某点 50m 内而服务器没下发该点的实体 ⇒ 已收集(已收集的不再刷)
//      → 隐藏(石像走挂件状态,见后端)。
// 两条都没命中的点一律**照常显示**——宁可多显示,不能藏掉没拿的。
const ST_UNCOLLECTED = 1 // 收到过实体 ⇒ 还在,未收集
const ST_COLLECTED = 2   // 走近了却没实体 ⇒ 已收集

// usePois 管理某场景的 POI 图层:点位/图层开关/可收集点(星星/零件)收集状态,返回筛好的可绘制标记。
export function usePois(account, res) {
  // poi 是当前场景的图层清单与点位;poiOn 是已开启的图层键集合。
  const [poiOn, setPoiOn] = useState(() => new Set(loadKeys(POI_LS_KEY) || []))
  const poiPicked = useRef(loadKeys(POI_LS_KEY) !== null) // 用户是否手动选过(未选过则跟随后端默认)
  const [collectOn, setCollectOn] = useState(() => new Set(loadKeys(COLLECT_LS_KEY) || []))
  const [starSt, setStarSt] = useState({}) // 刷新点 id -> 1未收集/2已收集(随玩家移动由后端推增量)
  const [poiVer, setPoiVer] = useState(0)  // 区域进度变化时递增,触发重取点位

  // POI 随场景走(每个场景的点位/图层不同):换 scene_res 就重取。
  // poiVer 是额外的重取键:区域进度(stars 的候选区)只在进场景时更新,那时重取一次点位。
  const fetchPois = useCallback(() => (res ? getPois(res) : Promise.resolve(NO_POIS)), [res])
  const { data: poi } = useAsyncData(fetchPois, { fallback: NO_POIS, reloadKey: `${account}|${res}|${poiVer}` })

  // 逐点状态随点位一起来(库里已确认的);之后由 SSE 推增量。
  // 首次(用户没手动选过图层)按后端 kinds[].on 初始化开关。
  useEffect(() => {
    setStarSt(Object.fromEntries(poi.pois.filter((p) => p.r).map((p) => [p.r, p.st || 0])))
    if (!poiPicked.current && poi.kinds.length) setPoiOn(new Set(poi.kinds.filter((k) => k.on).map((k) => k.k)))
  }, [poi])

  // 收集状态增量:玩家一边走,后端一边判定(走近却没实体 ⇒ 已收集),即时推过来。
  useEffect(() => subscribe(['stars', 'starzones'], (d, type) => {
    if (type === 'stars') setStarSt((prev) => ({ ...prev, ...d }))
    else setPoiVer((v) => v + 1)
  }), [account])

  const togglePoi = (k) => {
    setPoiOn((prev) => {
      const next = new Set(prev)
      next.has(k) ? next.delete(k) : next.add(k)
      return next
    })
    poiPicked.current = true
  }
  // 某图层的收集模式开关(仅可收集图层有,LayerPanel 摆在该图层开关右侧)。
  const toggleCollect = (k) => {
    setCollectOn((prev) => {
      const next = new Set(prev)
      next.has(k) ? next.delete(k) : next.add(k)
      return next
    })
  }

  // 持久化:开关状态存 localStorage(下次进游戏沿用)。
  // 放 effect 里而非 setValue 的 updater 内——StrictMode 会把 updater 调用两次,
  // 副作用写在那里会重复执行(写盘两次虽幂等,但申请通知权限这类不是)。
  useEffect(() => {
    try { localStorage.setItem(POI_LS_KEY, JSON.stringify([...poiOn])) } catch { /* 隐私模式下忽略 */ }
  }, [poiOn])
  useEffect(() => {
    try { localStorage.setItem(COLLECT_LS_KEY, JSON.stringify([...collectOn])) } catch { /* 同上 */ }
  }, [collectOn])

  // 本场景有点位的图层才给开关(如魔法学院只有魔力之源);标记只画开启的图层。
  // 这些都只随 poi 数据走,useMemo 缓存,位置推送不触发重算。
  //
  // 排除 gather:3552 个候选点全画出来糊成一片,没有实用价值 ——
  // 想知道「此刻能采什么」有实时采集物图层(见 useGathers.js),它只画服务器
  // 当下真下发的那几个(实测刷出率三到四成)。候选点**仍会加载**并用于品种清单
  // (allPois),只是不再提供「全部画出来」这个开关。
  const kinds = useMemo(() => poi.kinds.filter((k) => k.num > 0 && k.k !== GATHER_KIND), [poi])
  const iconOf = useMemo(() => Object.fromEntries(poi.kinds.map((k) => [k.k, k.icon])), [poi])
  const doneZones = useMemo(
    () => new Set((poi.zones || []).filter((z) => z.tot > 0 && z.got >= z.tot).map((z) => z.camp)),
    [poi])

  // 区域收集度(给 ZonePanel 展示用):服务器按分区给「已收集/总数」,这里再补两件事——
  //   1) 汇总成总进度;
  //   2) 反推每个分区的中心(u/v),让面板能把地图移过去。
  // 中心取自该分区在本场景的点位坐标均值:一个点的 zone 可能有多个候选区(管辖区重叠带),
  // 故同一点会计入它所属的每个区。点在跨区边界时中心会略微偏向邻区,对「定位过去」
  // 这个用途无所谓(真要精确得靠 AREA_FUNC_CONF 的多边形,那边没有现成数据)。
  // 只统计带 zone 的点(即眠枭之星一类有服务器分区进度的收集物),tot=0 的区不列。
  const zoneStats = useMemo(() => {
    const acc = new Map() // camp -> {su, sv, n}
    for (const p of poi.pois) {
      if (!p.zone?.length) continue
      for (const c of p.zone) {
        const e = acc.get(c) || { su: 0, sv: 0, n: 0 }
        e.su += p.u; e.sv += p.v; e.n++
        acc.set(c, e)
      }
    }
    let got = 0, tot = 0
    const rows = (poi.zones || [])
      .filter((z) => z.tot > 0)
      .map((z) => {
        got += z.got; tot += z.tot
        const e = acc.get(z.camp)
        return {
          camp: z.camp, name: z.name, got: z.got, tot: z.tot, miss: z.tot - z.got,
          n: e ? e.n : 0,
          u: e ? e.su / e.n : null,
          v: e ? e.sv / e.n : null,
        }
      })
      .sort((a, b) => b.miss - a.miss || a.camp - b.camp) // 缺口大的在前
    return { got, tot, rows }
  }, [poi])
  // marks 顺带把「已确认还在」(收集模式高亮)的布尔算好挂到副本上(sure):渲染层直接读,
  // 替代每标记每渲染调 isSure。依赖只有 poi/开关/收集状态,位置推送不碰这些。
  const marks = useMemo(() => {
    // 收集模式下隐藏「已收集」的点:逐点确认过的,或候选区域(p.zone 列表)全部收满的。其余一律显示。
    const collected = (p) => starSt[p.r] === ST_COLLECTED || (p.zone?.length > 0 && p.zone.every((c) => doneZones.has(c)))
    return poi.pois
      .filter((p) => {
        // 候选点一律不画:开关已从面板移除(见 kinds 的注释),这里再挡一道,
        // 免得用户早先手动开过、localStorage 里留着 'gather' 又画满一屏。
        if (p.k === GATHER_KIND) return false
        if (!poiOn.has(p.k)) return false
        if (!p.r || !collectOn.has(p.k)) return true
        return !collected(p)
      })
      // 采集物每个点自带品种图标(p.i,后端已拼好路径);其余图层共用图层图标。
      .map((p) => ({ ...p, icon: p.i || iconOf[p.k], sure: collectOn.has(p.k) && starSt[p.r] === ST_UNCOLLECTED }))
  }, [poi, poiOn, collectOn, starSt, doneZones, iconOf])

  return { kinds, iconOf, marks, zoneStats, poiOn, togglePoi, collectOn, toggleCollect }
}
