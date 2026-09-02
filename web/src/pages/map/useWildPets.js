import { useState, useEffect, useMemo, useRef, useCallback } from 'react'
import { getWildPets, clearWildPets, subscribe } from '../../api'
import { useAsyncData } from '../../hooks/useAsyncData'
import { useRangeRules } from '../../hooks/useRangeRules'
import { matchRangeRule } from '../../utils/rules'
import {
  WILD_LAYERS, SWITCH_KEYS, DEFAULT_SWITCHES, LS_KEY, LEGACY_LS_KEY, migrateLegacyMedals,
} from './wildConfig'
import { wildShown, isDualMedal, wildRing } from './wildMatch'
import { fireWildNotify, NOTIFY_KEY, NOTIFY_DUAL_ONLY_KEY } from './wildNotify'

// —— 野生宠物图层(异色/炫彩 · 污染 · 体重/声音区间规则)——
// 与 POI 图层不同,这几类**不是固定点位**:野生宠会刷新、被别人抓走,只有走进 AOI 才知道它在。
// 后端从周边实体快照与 AOI 通知里挑出这几类推过来(见 internal/pipeline/wildpets.go),
// 前端只管开关与摆放。判定依据(捕捉前后一致的属性)见 docs/data.md 3.5。
//
// 本文件只放 hook(订阅 + 筛选 + 对外操作)。三块内容已拆出:
//   - wildConfig.js:开关图层的元数据、默认值、localStorage 读写与迁移
//   - wildMatch.js :判定口径(标签、规则命中、是否显示、是否双牌、描边样式)
//   - wildNotify.js:稀有宠出现的系统通知
// 体重/声音的规则**不在本地** —— 用 utils/rules.js 里那套,与事件页共用
// (见 hooks/useRangeRules),配一次两边生效。

// —— 存储:v7 起为 { on: 开关键数组, open: 区间规则是否展开, dual: 双牌开关 } ——
// 体重/声音的阈值与开关已移出,改为共享的区间规则(见 utils/rules.js)。
// 旧 v6 的 medals / medalOn / dual.medals 读出来后在下面的迁移 effect 里搬到规则上。
const loadState = () => {
  const base = { on: new Set(DEFAULT_SWITCHES), open: false, dual: false, legacy: null }
  try {
    const v = JSON.parse(localStorage.getItem(LS_KEY))
    // 旧键:读出来做迁移,同时把开关/展开/双牌这三个仍有效的字段带过来
    if ((!v || typeof v !== 'object' || Array.isArray(v)) && LEGACY_LS_KEY) {
      const old = JSON.parse(localStorage.getItem(LEGACY_LS_KEY) || 'null')
      if (old && typeof old === 'object' && !Array.isArray(old)) {
        return {
          on: new Set(Array.isArray(old.on) ? old.on.filter((k) => SWITCH_KEYS.has(k)) : DEFAULT_SWITCHES),
          open: !!old.open,
          dual: !!(old.dual && old.dual.on),
          legacy: old, // 交给迁移 effect 处理(阈值/开关搬到区间规则上)
        }
      }
      return base
    }
    if (!v || typeof v !== 'object' || Array.isArray(v)) return base
    return {
      on: new Set(Array.isArray(v.on) ? v.on.filter((k) => SWITCH_KEYS.has(k)) : DEFAULT_SWITCHES),
      open: !!v.open,
      dual: !!v.dual,
      legacy: null,
    }
  } catch { return base }
}
const persist = (st) => {
  try {
    localStorage.setItem(LS_KEY, JSON.stringify({ on: [...st.on], open: st.open, dual: st.dual }))
  } catch { /* 隐私模式下 localStorage 不可写,忽略即可 */ }
}

// 空数据的兜底常量:引用稳定,免得每次渲染都造一个新数组、连带打穿下游的 useMemo。
const NO_PETS = { pets: [], allPets: [] }
// useWildPets 管理野生宠物图层:订阅后端推送、按「开关 + 区间规则」筛出可绘制的标记。
export function useWildPets(account) {
  // 后端一次给两份列表:稀有标记 pets 与普通野生宠 allPets(「全部野生」图层)。
  // 二者同源(同一接口、同一推送),故存成一份状态,推送直接整体替换。
  const { data, setData, refresh } = useAsyncData(
    useCallback(() => getWildPets(), []),
    { fallback: NO_PETS, reloadKey: account },
  )
  const { pets, allPets } = data || NO_PETS
  // 体重/声音的区间规则:**与事件页共用同一份**。这里只消费,不自带副本。
  const [rangeRules, setRangeRules] = useRangeRules()
  const [st] = useState(loadState) // 初始快照只取一次(含 localStorage 的旧选择)
  const [on, setOn] = useState(st.on)
  const [open, setOpen] = useState(st.open)
  const [dual, setDual] = useState(st.dual)

  // —— 迁移:把旧版「奖牌四件套」的阈值与开关搬到共享区间规则上 ——
  //
  // 不做的话,用户精心拖过的边界(如大块头从 98 拖到 99.5)会在升级后悄悄回到默认值,
  // 而界面上完全看不出发生过什么。规则 id 与旧的奖牌 key 同名,对号入座即可。
  //
  // 只在旧配置**确实被改过**时才迁移:全默认的用户直接沿用规则默认值(两者本就等价),
  // 免得把他在事件页刚配好的规则覆盖掉。
  const migratedRef = useRef(false)
  useEffect(() => {
    if (migratedRef.current || !st.legacy) return
    migratedRef.current = true
    setRangeRules((rs) => migrateLegacyMedals(rs, st.legacy))
  }, [st, setRangeRules])

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
  // 「仅双牌时提醒」子开关:勾选后只有双牌(命中≥2条规则)的新出现稀有宠才响提醒。
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
  //   - 其余类别(污染/区间规则):用 wildShown 判定,与 marks 过滤同一口径——只有地图上
  //     画得出环的才算稀有宠,画不出环的再出现不打扰;勾选「仅双牌」后只响双牌。
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
        fireWildNotify(p, rangeRules)
        continue
      }
      // 其余类别:先过 wildShown(地图上画得出环的才算稀有宠),再过「仅双牌」。
      if (!wildShown(p, on, rangeRules, dual)) continue
      if (notifyDualOnly && !isDualMedal(p, rangeRules)) continue
      fireWildNotify(p, rangeRules)
    }
  }, [pets, notify, notifyDualOnly, on, rangeRules, dual])

  // 后端每次成员/状态变化都推全量列表(实体进出 AOI 是低频事件),直接替换即可。
  // pets = 稀有标记(异色/炫彩/污染等),allPets = 普通野生宠(「全部野生」图层)。
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
    persist({ on, open, dual })
  }, [on, open, dual])

  const toggle = (k) => {
    setOn((prev) => {
      const next = new Set(prev)
      next.has(k) ? next.delete(k) : next.add(k)
      return next
    })
  }

  const toggleDual = () => setDual((prev) => !prev)
  // 双牌开关变化时联动「仅双牌时提醒」:开 → 自动勾选(一步到位「只看双牌 + 只提醒双牌」);
  // 关时不自动取消(保留用户选择;关后 isDualMedal 仍按 ≥2 条判,仍能工作)。
  // 只改 state,落盘交给上面那个 effect(避免 updater 里的副作用)。
  useEffect(() => {
    if (dual) setNotifyDualOnly(true)
  }, [dual])

  const toggleOpen = () => setOpen((prev) => !prev)

  // marks 与计数只依赖 pets/allPets/on/rangeRules/dual(位置推送不碰这几个 state),用 useMemo
  // 缓存:玩家移动时 position 高频推送 → MapPage 重渲染,但这里跳过全部过滤/计数重算,marks
  // 引用不变 → 标记层子组件(MapPage 的 WildLayer,React.memo)整层不重渲染。
  // 过滤时**顺带把描边样式算好**挂到副本上(style):渲染层直接展开,不再每标记每渲染调
  // wildRing(同样只在开关/规则/宠物数据变化时算一次)。
  // 「全部野生」图层(all 开关):数据源是 allPets(普通野生宠,后端 wildAllMark),不走
  // kinds 命中也不走区间规则,style 给空对象(无描边,渲染层靠 .map-wild-all 降级样式)。
  const marks = useMemo(() => {
    const rare = pets
      .filter((p) => wildShown(p, on, rangeRules, dual))
      .map((p) => ({ ...p, style: wildRing(p, on, rangeRules) }))
    // 普通野生宠:all 开关打开时才加入,标记上挂 all:true 让渲染层用降级样式。
    const all = on.has('all')
      ? allPets.map((p) => ({ ...p, all: true, style: {} }))
      : []
    // 稀有在后(数组末尾),DOM 顺序靠后 → 默认压在普通宠之上(z-index 相同时后者居上)。
    return [...all, ...rare]
  }, [pets, allPets, on, rangeRules, dual])

  // 计数与地图上画出的标记一一对应:灰点(已离开视野的最后所见)也画在图上,
  // 故也计入——否则侧栏显示 0 而图上还挂着几个,只会让人以为标记出错了。
  // 另单算其中的灰点数,供侧栏悬浮说明拆开「视野内 / 已离开」(见 LayerPanel)。
  // all 行的计数取 allPets 长度(普通宠不参与稀有类别的判定)。
  // ruleNum 是**逐条规则**的命中数,供规则编辑器显示(与事件页同一个 counts 接口)。
  const [num, numStale, ruleNum] = useMemo(() => {
    const layerHit = (l, p) => (p.kinds || []).some((k) => l.kinds.includes(k))
    const countLayer = (l, pick) => pets.filter((p) => pick(p) && layerHit(l, p)).length
    const num = Object.fromEntries(
      WILD_LAYERS.map((l) => [l.k, l.k === 'all' ? allPets.length : countLayer(l, () => true)]))
    const numStale = Object.fromEntries(
      WILD_LAYERS.map((l) => [l.k, l.k === 'all' ? allPets.filter((p) => p.stale).length : countLayer(l, (p) => p.stale)]))
    const ruleNum = {}
    for (const r of rangeRules) {
      if (!r.on) continue
      ruleNum[r.id] = pets.filter((p) => matchRangeRule(p, r)).length
    }
    // 双牌计数:命中 ≥2 条(与 wildShown 双牌段的口径一致)。
    num.dual = pets.filter((p) => isDualMedal(p, rangeRules)).length
    numStale.dual = pets.filter((p) => p.stale && isDualMedal(p, rangeRules)).length
    return [num, numStale, ruleNum]
  }, [pets, allPets, rangeRules])

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

  return {
    marks, num, numStale, ruleNum,
    on, toggle, clear,
    open, toggleOpen,
    rangeRules, setRangeRules,
    dual, toggleDual,
    notify, toggleNotify, notifyDualOnly, toggleNotifyDualOnly,
  }
}
