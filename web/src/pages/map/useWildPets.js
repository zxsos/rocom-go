import { useState, useEffect, useMemo, useRef, useCallback } from 'react'
import { getWildPets, clearWildPets, subscribe } from '../../api'
import { useAsyncData } from '../../hooks/useAsyncData'
import { WILD_LAYERS, MEDAL_FILTERS, SWITCH_KEYS, MEDAL_KEYS,
  DEFAULT_SWITCHES, DEFAULT_MEDALS, DEFAULT_MEDAL_ON,
  DEFAULT_DUAL_MEDALS, clampDual, LS_KEY } from './wildConfig'
import { wildShown, isDualMedal, wildRing, medalMatch } from './wildMatch'
import { fireWildNotify, NOTIFY_KEY, NOTIFY_DUAL_ONLY_KEY } from './wildNotify'

// —— 野生宠物图层(异色/炫彩 · 污染 · 奖牌四件套:大块头/小不点/婉转声/粗嗓门)——
// 与 POI 图层不同,这几类**不是固定点位**:野生宠会刷新、被别人抓走,只有走进 AOI 才知道它在。
// 后端从周边实体快照与 AOI 通知里挑出这几类推过来(见 internal/pipeline/wildpets.go),
// 前端只管开关与摆放。判定依据(捕捉前后一致的属性)见 docs/data.md 3.5。
//
// 本文件只放 hook(订阅 + 筛选 + 对外操作)。三块内容已拆出:
//   - wildConfig.js:图层/奖牌的元数据、默认值、localStorage 读写与迁移
//   - wildMatch.js :判定口径(标签、阈值匹配、是否显示、是否双牌、描边样式)
//   - wildNotify.js:稀有宠出现的系统通知

// —— 存储:v5 起为一个对象 { on: 开关键数组, medals: {big:…}, open: 奖牌筛选是否展开,
//   medalOn: 奖牌开关数组, dual: 双牌筛选 { on, medals }(默认关) } ——
// null = 用户从没手动选过(或旧格式),按默认值;数组只可能是旧键,同样回默认。
// medalOn 是后加的字段,旧数据缺失时按默认(全开)处理,与无开关时代的旧行为一致。
// dual 旧版是 boolean(true=纯开关双牌),现版是 { on, medals };迁移时旧 true→{on:true},
// 旧 false/缺→{on:false},medals 缺失按 DEFAULT_DUAL_MEDALS 补(=单牌默认=擦边双牌)。
const loadState = () => {
  const base = { on: new Set(DEFAULT_SWITCHES), medals: { ...DEFAULT_MEDALS }, open: false, medalOn: new Set(DEFAULT_MEDAL_ON), dual: { on: false, medals: { ...DEFAULT_DUAL_MEDALS } } }
  try {
    const v = JSON.parse(localStorage.getItem(LS_KEY))
    if (!v || typeof v !== 'object' || Array.isArray(v)) return base
    const on = new Set(Array.isArray(v.on) ? v.on.filter((k) => SWITCH_KEYS.has(k)) : DEFAULT_SWITCHES)
    const medals = { ...DEFAULT_MEDALS }
    if (v.medals && typeof v.medals === 'object') {
      for (const m of MEDAL_FILTERS) {
        const t = v.medals[m.k]
        if (typeof t === 'number' && t >= m.lo && t <= m.hi) medals[m.k] = t
      }
    }
    const medalOn = new Set(Array.isArray(v.medalOn) ? v.medalOn.filter((k) => MEDAL_KEYS.has(k)) : DEFAULT_MEDAL_ON)
    // dual 迁移:旧 boolean 或缺失 → { on: !!v, medals: 默认 };新 { on, medals } 校验后采用,
    // 阈值超出 [lo,hi] 的按默认补(单牌值变化后的联动在 setThreshold 里做,这里只做范围校验)。
    let dual
    if (v.dual && typeof v.dual === 'object' && !Array.isArray(v.dual)) {
      const dm = { ...DEFAULT_DUAL_MEDALS }
      if (v.dual.medals && typeof v.dual.medals === 'object') {
        for (const m of MEDAL_FILTERS) {
          const t = v.dual.medals[m.k]
          if (typeof t === 'number' && t >= m.lo && t <= m.hi) dm[m.k] = t
        }
      }
      dual = { on: !!v.dual.on, medals: dm }
    } else {
      dual = { on: !!v.dual, medals: { ...DEFAULT_DUAL_MEDALS } }
    }
    return { on, medals, open: !!v.open, medalOn, dual }
  } catch { return base }
}
const persist = (st) => {
  try {
    localStorage.setItem(LS_KEY, JSON.stringify({
      on: [...st.on], medals: st.medals, open: st.open, medalOn: [...st.medalOn], dual: st.dual,
    }))
  } catch { /* 隐私模式下 localStorage 不可写,忽略即可 */ }
}

// 空数据的兜底常量:引用稳定,免得每次渲染都造一个新数组、连带打穿下游的 useMemo。
const NO_PETS = { pets: [], allPets: [] }
// useWildPets 管理野生宠物图层:订阅后端推送、按「开关 + 奖牌阈值」筛出可绘制的标记。
export function useWildPets(account) {
  // 后端一次给两份列表:稀有标记 pets 与普通野生宠 allPets(「全部野生」图层)。
  // 二者同源(同一接口、同一推送),故存成一份状态,推送直接整体替换。
  const { data, setData, refresh } = useAsyncData(
    useCallback(() => getWildPets(), []),
    { fallback: NO_PETS, reloadKey: account },
  )
  const { pets, allPets } = data || NO_PETS
  const [st] = useState(loadState) // 初始快照只取一次(含 localStorage 的旧选择)
  const [on, setOn] = useState(st.on)
  const [medals, setMedals] = useState(st.medals)
  const [open, setOpen] = useState(st.open)
  const [medalOn, setMedalOn] = useState(st.medalOn)
  const [dual, setDual] = useState(st.dual)
  // 稀有宠出现提醒开关:独立键持久化(与图层状态分开)。默认关,不打扰。
  const [notify, setNotify] = useState(() => {
    try { return localStorage.getItem(NOTIFY_KEY) === '1' } catch { return false }
  })
  const toggleNotify = () => setNotify((prev) => !prev)
  // 持久化与权限申请都放 effect:StrictMode 会把 setState 的 updater 调用两次,
  // 副作用写在里面会重复执行。写盘两次虽然幂等,但**申请通知权限不是**——
  // 重复弹窗会打断用户,且部分浏览器在短时间内重复请求会直接拒绝。
  useEffect(() => {
    try { localStorage.setItem(NOTIFY_KEY, notify ? '1' : '0') } catch { /* 隐私模式下忽略 */ }
    // 开启时若还没要过权限就主动要一次;拒绝/忽略都不影响——没权限时只响音效不弹系统通知。
    if (notify && typeof Notification !== 'undefined' && Notification.permission === 'default') {
      Notification.requestPermission().catch(() => {})
    }
  }, [notify])
  // 「仅双牌时提醒」子开关:勾选后只有双牌(命中≥2张奖牌)的新出现稀有宠才响提醒。
  const [notifyDualOnly, setNotifyDualOnly] = useState(() => {
    try { return localStorage.getItem(NOTIFY_DUAL_ONLY_KEY) === '1' } catch { return false }
  })
  const toggleNotifyDualOnly = () => setNotifyDualOnly((prev) => !prev)
  useEffect(() => {
    try { localStorage.setItem(NOTIFY_DUAL_ONLY_KEY, notifyDualOnly ? '1' : '0') } catch { /* 同上 */ }
  }, [notifyDualOnly])
  // 新出现提醒:对比相邻两批推送,挑出「刚进视野、非灰点」的实体通知(已通知过的 id 不重复
  // 弹;它变灰点=离开视野后从集合移除,重新出现可再提醒)。判定口径:
  //   - 异色/炫彩:**最高优先级**,短路提醒——不经过 wildShown(图层开关关掉也响)、不被
  //     「仅双牌」拦截,只要有提醒开关就一定响。
  //   - 其余类别(污染/奖牌四件套):用 wildShown 判定,与 marks 过滤同一口径——只有地图上
  //     画得出环的才算稀有宠(开关图层 kinds 命中且开关开,或奖牌开关开且滑块阈值命中),
  //     画不出环的再出现不打扰;勾选「仅双牌」后只响双牌(命中≥2张奖牌)。
  // 未命中的宠也照常记入「已见」集合——中途把某类开关打开时,已在地图上的旧宠不会被当新
  // 出现补弹一遍,只有之后新出现的才提醒。notify 关闭时不做对比,但照常同步「已见」集合,
  // 同理防补弹。首轮(挂载后第一批数据)只建基线不通知,否则刚进页面就把当前在场的全弹一遍。
  const seenIdsRef = useRef(new Set())
  const initedRef = useRef(false)
  useEffect(() => {
    const ids = new Set(pets.map((p) => p.id))
    for (const id of [...seenIdsRef.current]) if (!ids.has(id)) seenIdsRef.current.delete(id)
    if (!notify || !initedRef.current) {
      for (const p of pets) seenIdsRef.current.add(p.id)
      initedRef.current = true
      return
    }
    for (const p of pets) if (p.stale) seenIdsRef.current.delete(p.id)
    for (const p of pets) {
      if (p.stale || seenIdsRef.current.has(p.id)) continue
      seenIdsRef.current.add(p.id)
      // 异色/炫彩是全场最高优先级:无论是否开关双牌模式(甚至图层开关关掉),
      // 只要有提醒开关就一定响——它太稀有,不能被任何子筛选拦住。
      const ks = p.kinds || []
      if (ks.includes('shiny') || ks.includes('colorful')) {
        fireWildNotify(p)
        continue
      }
      // 其余类别:先过 wildShown(地图上画得出环的才算稀有宠),再过「仅双牌」(勾选时只响双牌)。
      if (!wildShown(p, on, medals, medalOn, dual)) continue
      if (notifyDualOnly && !isDualMedal(p, medals, medalOn, dual)) continue
      fireWildNotify(p)
    }
  }, [pets, notify, notifyDualOnly, on, medals, medalOn, dual])

  // 后端每次成员/状态变化都推全量列表(实体进出 AOI 是低频事件),直接替换即可。
  // pets = 稀有标记(异色/炫彩/污染/奖牌四件套),allPets = 普通野生宠(「全部野生」图层)。
  // 断线重连期间的增量丢失了,故重连成功时补拉一次全量快照。
  useEffect(() => subscribe('wildpets', (d) => {
    // injectRevoke:后端撤销某只注入精灵时只推一个 id,从当前列表剔除该标记,
    // 避免整表替换抖动(尤其是管理员主动撤销后立即清掉那只)。
    if (d.injectRevoke) {
      setData((prev) => ({
        pets: (prev?.pets || []).filter((p) => p.id !== d.injectRevoke),
        allPets: prev?.allPets || [],
      }))
      return
    }
    setData(d)
  }, { onOpen: refresh }), [refresh, setData])

  // 图层状态持久化:统一在这一个 effect 里落盘,不散在各个 setValue 的 updater 内——
  // StrictMode(见 main.jsx)会把 updater 调用两次,副作用写在里面会重复执行。
  // 写盘本身幂等,但同类的「申请通知权限」不是(见下方 toggleNotify 的说明),
  // 故统一收口到此,避免将来再踩同一个坑。
  useEffect(() => {
    persist({ on, medals, open, medalOn, dual })
  }, [on, medals, open, medalOn, dual])

  const toggle = (k) => {
    setOn((prev) => {
      const next = new Set(prev)
      next.has(k) ? next.delete(k) : next.add(k)
      return next
    })
  }

  const setThreshold = (k, v) => {
    setMedals((prev) => {
      // range 的 0.1 步进值是 0.1 的浮点倍数(如 99.60000000000001),存前取整到十分位,
      // 保证侧栏显示与判定(medalMatch 内也 round1)拿到的都是干净的一位小数。
      const next = { ...prev, [k]: Math.round(v * 10) / 10 }
      // 联动双牌:单牌拖严后,对应双牌阈值若被越过(双牌比单牌宽了)要自动跟上,维持
      // T_dual ≥ T_single(只严不宽)。其余 3 条双牌阈值不动。dual 是对象,需新建引用触发更新。
      const m = MEDAL_FILTERS.find((mm) => mm.k === k)
      const newDual = m
        ? { ...dual, medals: { ...dual.medals, [k]: clampDual(m, dual.medals[k], next[k]) } }
        : dual
      setDual(newDual)
      return next
    })
  }

  const toggleMedal = (k) => {
    setMedalOn((prev) => {
      const next = new Set(prev)
      next.has(k) ? next.delete(k) : next.add(k)
      return next
    })
  }

  const toggleDual = () => {
    setDual((prev) => {
      const next = { ...prev, on: !prev.on }
      return next
    })
  }
  // 双牌开关变化时联动「仅双牌时提醒」:开 → 自动勾选(一步到位「只看双牌 + 只提醒双牌」);
  // 关时不自动取消(保留用户选择;关后 isDualMedal 退回单牌阈值判 ≥2,仍能工作)。
  // 只改 state,落盘交给上面那个 effect(避免 updater 里的副作用)。
  useEffect(() => {
    if (dual.on) setNotifyDualOnly(true)
  }, [dual.on])

  // setDualThreshold 拖双牌子滑块:只改对应奖牌的双牌阈值,钳到 [单牌当前值, 极端值]内
  // (保证不比单牌宽)。单牌值是下限/上限(取决于 dir),由 clampDual 处理方向。
  const setDualThreshold = (k, v) => {
    setDual((prev) => {
      const m = MEDAL_FILTERS.find((mm) => mm.k === k)
      if (!m) return prev
      const clamped = clampDual(m, Math.round(v * 10) / 10, medals[k])
      return { ...prev, medals: { ...prev.medals, [k]: clamped } }
    })
  }

  const toggleOpen = () => setOpen((prev) => !prev)

  // marks 与计数只依赖 pets/allPets/on/medals/medalOn(位置推送不碰这几个 state),用 useMemo 缓存:
  // 玩家移动时 position 高频推送 → MapPage 重渲染,但这里跳过全部过滤/计数重算,marks 引用
  // 不变 → 标记层子组件(MapPage 的 WildLayer,React.memo)整层不重渲染。
  // 过滤时**顺带把描边样式算好**挂到副本上(style):渲染层直接展开,不再每标记每渲染调
  // wildRing(同样只在开关/阈值/宠物数据变化时算一次)。
  // 「全部野生」图层(all 开关):数据源是 allPets(普通野生宠,后端 wildAllMark),不走
  // kinds 命中也不走奖牌阈值,style 给空对象(无描边,渲染层靠 .map-wild-all 降级样式)。
  const marks = useMemo(() => {
    const rare = pets
      .filter((p) => wildShown(p, on, medals, medalOn, dual))
      .map((p) => ({ ...p, style: wildRing(p, on, medals, medalOn, dual) }))
    // 普通野生宠:all 开关打开时才加入,标记上挂 all:true 让渲染层用降级样式。
    const all = on.has('all')
      ? allPets.map((p) => ({ ...p, all: true, style: {} }))
      : []
    // 稀有在后(数组末尾),DOM 顺序靠后 → 默认压在普通宠之上(z-index 相同时后者居上)。
    return [...all, ...rare]
  }, [pets, allPets, on, medals, medalOn, dual])

  // 图层行上的计数与地图上画出的标记一一对应:灰点(已离开视野的最后所见)也画在图上,
  // 故也计入——否则侧栏显示 0 而图上还挂着几个,只会让人以为标记出错了。
  // 另单算其中的灰点数,供侧栏悬浮说明拆开「视野内 / 已离开」(见 LayerPanel)。
  // all 行的计数取 allPets 长度(普通宠不参与稀有类别的 kinds/奖牌命中判定)。
  // 双牌行计数:双牌开时用双牌阈值判 ≥2 张,关时用单牌阈值判 ≥2(供侧栏参考,关时该
  // 计数仍显示但不影响图上标记——关时图上按 ≥1 张显示)。
  const [num, numStale] = useMemo(() => {
    const hit = (l, p) => l.kinds
      ? (p.kinds || []).some((k) => l.kinds.includes(k))
      : medalOn.has(l.k) && medalMatch(l, p, medals)
    const count = (l, pick) => pets.filter((p) => pick(p) && hit(l, p)).length
    const num = Object.fromEntries([...WILD_LAYERS, ...MEDAL_FILTERS].map((l) => [l.k, l.k === 'all' ? allPets.length : count(l, () => true)]))
    const numStale = Object.fromEntries([...WILD_LAYERS, ...MEDAL_FILTERS].map((l) => [l.k, l.k === 'all' ? allPets.filter((p) => p.stale).length : count(l, (p) => p.stale)]))
    // 双牌计数口径与 wildShown 的奖牌段一致:双牌开用双牌阈值,关用单牌阈值,判 ≥2 张。
    const dualTh = dual && dual.on ? dual.medals : medals
    const dualHit = (p) => MEDAL_FILTERS.filter((m) => medalOn.has(m.k) && medalMatch(m, p, dualTh)).length >= 2
    num.dual = pets.filter(dualHit).length
    numStale.dual = pets.filter((p) => p.stale && dualHit(p)).length
    return [num, numStale]
  }, [pets, allPets, medals, medalOn, dual])

  // clear 主动清空野生宠标记(侧栏「清空」按钮),灰点也一并清掉。
  //
  // 与换场景/传送不同:那两者只把标记置灰(见 pipeline.resetWilds),标记会一直
  // 留在图上、久了可能攒一堆灰点,是否抹平由用户决定 —— 这里就是那个决定。
  // 清空后不会被服务器补回(标记只随 AOI 实体下发重建),等走近了才重新出现。
  //
  // 本地先清(响应快),后端随后广播一条空列表兜住同账号的其它页面。
  const clear = useCallback(() => {
    setData({ pets: [], allPets: [] })
    return clearWildPets().catch(() => {}) // 失败无所谓:下次实体下发会重建
  }, [setData])

  return { marks, num, numStale, on, toggle, clear, medals, setThreshold, open, toggleOpen, medalOn, toggleMedal, dual, toggleDual, setDualThreshold, notify, toggleNotify, notifyDualOnly, toggleNotifyDualOnly }
}
