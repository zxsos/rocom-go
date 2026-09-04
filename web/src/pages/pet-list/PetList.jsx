import React, { useState, useEffect, useCallback, useRef, useMemo, useContext } from 'react'
import { getPets, getFilterOptions, getNameOptions, getBoxes, getTeams, getPetPage, subscribe } from '../../api'
import { AccountContext } from '../../context'
import { useStoredFlag, useStoredJSON } from '../../hooks/useStoredState'
import { useAsyncData } from '../../hooks/useAsyncData'
import { PetDetailModal } from '../../components/PetDetailModal'
import { SkeletonRows } from '../../components/Skeleton'
import { SORTS, withCatch, FILTER_KEY, DEFAULT_FILTER, sanitizeFilter, DEFAULT_VIEW, sanitizeView } from './filters'
import FilterPanel from './FilterPanel'
import BoxMap from './BoxMap'
import PetTable from './PetTable'
import PetGallery from './PetGallery'
import ContextMenu from './ContextMenu'
import Dropdown from '../../components/Dropdown'

// 空数据的兜底常量:引用稳定,免得每次渲染造新对象打穿下游 memo。
const NO_PETS = { total: 0, pets: [] }
const NO_OPTIONS = {}
// 6×6 性格方阵的空兜底:数据未到时组件收到 undefined,铺出来是一张全空的格子,
// 会被误读成「这一版游戏没性格」。给一份结构完整的空阵,未到位时只是每格无名。
const NO_NAME_OPTIONS = { nature: Array.from({ length: 6 }, () => new Array(6).fill('')) }
const NO_BOXES = []
const NO_TEAMS = { slots: [] }

export default function PetList() {
  const account = useContext(AccountContext)
  const [filter, setFilter] = useStoredJSON(sessionStorage, FILTER_KEY, DEFAULT_FILTER, sanitizeFilter)

  // 盒子筛选是账号绑定的(盒子 id 归属特定账号),切账号必须清掉——否则拿 A 的盒 id 去查 B 的宠物。
  // 原先靠 <main key={account}> 重挂载时从 sessionStorage 重读(box 已被 App 的 dropBoxFilter 删掉);
  // 改为依赖驱动重取后不再重挂载,内存里的 filter 会残留旧 box,故在此显式清理。
  // 用「渲染期派生」(React 官方的 adjusting-state-on-prop-change 写法)而非 useEffect:
  // effect 里的 setFilter 要等下一轮渲染才生效,会让下方 useAsyncData 先用带旧 box 的 filter
  // 白跑一次请求;渲染期 setState 则会被 React 合并进本次渲染,effects 只在最终结果上跑一次。
  const [filterAccount, setFilterAccount] = useState(account)
  if (filterAccount !== account) {
    setFilterAccount(account)
    setFilter((f) => (f.box ? { ...f, box: '', page: 1 } : f))
  }
  const [collapsed, setCollapsed] = useStoredFlag(sessionStorage, 'petListCollapsed', true)
  // 视图开关(表格 / 陈列)。存 localStorage 而非 sessionStorage:这是"我习惯怎么看"
  // 而不是"这次会话的临时状态",与筛选条件(关掉页面就该忘)性质相反。
  //
  // 为什么让用户选而不是按视口宽度自动切:原先是 760px 断点一刀切 —— 平板竖屏
  // (宽 800px)被迫用横向滚动的表格,而窄窗口的桌面用户被迫用卡片。两者都是
  // 猜错了意图:表格适合"逐列比一页",陈列适合"找那一只",这与屏幕宽度无关。
  //
  // 默认 DEFAULT_VIEW(表格)。注意默认值只对**没有存储记录**的浏览器生效 ——
  // 已选过视图的用户保持其选择,新装/清过缓存的从默认值开始。
  const [view, setView] = useStoredJSON(localStorage, 'petView', DEFAULT_VIEW, sanitizeView)
  const [sync, setSync] = useStoredFlag(localStorage, 'petSync', true) // 实时同步:游戏内操作自动跳转到对应宠物(默认开)
  const [detailGid, setDetailGid] = useState(null) // 详情弹窗的 gid(null=关闭)
  const [selected, setSelected] = useState(null) // 单击选中的 gid
  const [menu, setMenu] = useState(null)          // 右键/长按菜单 {gid,pet,x,y}
  const [activeIdx, setActiveIdx] = useState(0)   // 示意图当前容器下标(0=队伍)
  const reloadRef = useRef(null)
  const filterRef = useRef(filter)      // 供 SSE 回调读取最新筛选(避免闭包旧值)
  const containersRef = useRef([])       // 供 SSE 回调按盒号查容器名(避免闭包旧值)
  const lpRef = useRef(null)        // 长按定时器
  const lpFiredRef = useRef(false)  // 本次触摸是否已触发长按
  const menuAtRef = useRef(0)       // 菜单打开时刻(用于忽略紧随的合成 click)
  const syncRef = useRef(sync)      // 供 SSE 回调读取最新同步开关(避免闭包旧值)

  // 列表随筛选条件重取;SSE 防抖重载复用同一个 refresh(内部读最新 fetcher,拿到的就是最新筛选)。
  const { data, loading, refresh: load } = useAsyncData(
    useCallback(() => getPets(withCatch(filter)), [filter]),
    { fallback: NO_PETS, reloadKey: account },
  )
  // 筛选下拉项:五维合并成一条 SELECT 再在 Go 侧去重排序(见后端 handleFilterOptions)。
  // 性格矩阵另走 /api/name-options —— 它是**全局固定数据**(不随账号变化),
  // 与按账号聚合的 filter-options 混在一起会让那份响应带上无谓的账号语义。
  const { data: options } = useAsyncData(useCallback(() => getFilterOptions(), []),
    { fallback: NO_OPTIONS, reloadKey: account })
  const { data: nameOpts } = useAsyncData(useCallback(() => getNameOptions(), []),
    { fallback: NO_NAME_OPTIONS })
  // 盒子与队伍是两路独立拉取,但总是一起重取(SSE 收到宠物变动时都要刷新),故合成一个 loadBoxes。
  const { data: boxes, refresh: loadBoxesData } = useAsyncData(useCallback(() => getBoxes(), []),
    { fallback: NO_BOXES, reloadKey: account })
  const { data: teams, refresh: loadTeams } = useAsyncData(useCallback(() => getTeams(), []),
    { fallback: NO_TEAMS, reloadKey: account })
  const loadBoxes = useCallback(() => { loadBoxesData(); loadTeams() }, [loadBoxesData, loadTeams])



  // 示意图容器:大世界队伍(6 排 × 3 队,竖向)排在所有盒子前,其后各盒子(5 排 × 6 格)
  const containers = useMemo(() => {
    // 原始 18 格为队序(team*6+pos);转置为「行=位置、列=队伍」的显示序(pos*3+team)
    const raw = teams.slots && teams.slots.length ? teams.slots : new Array(18).fill(0)
    const teamDisplay = []
    for (let pos = 0; pos < 6; pos++) for (let t = 0; t < 3; t++) teamDisplay.push(raw[t * 6 + pos])
    const list = [{ type: 'team', name: '大世界队伍', cols: 3, slots: teamDisplay, heads: teams.heads || {} }]
    for (const b of boxes) list.push({ type: 'box', id: b.id, name: b.name || ('盒' + b.id), cols: 6, slots: b.slots, heads: b.heads || {} })
    return list
  }, [teams, boxes])
  const boxIdxById = useCallback((id) => containers.findIndex((c) => c.type === 'box' && c.id === id), [containers])
  // 宠物盒筛选变化时,示意图跟随展示该盒
  useEffect(() => {
    const id = parseInt((filter.box || '').split('-')[0], 10)
    if (id) { const i = boxIdxById(id); if (i >= 0) setActiveIdx(i) }
  }, [filter.box, boxIdxById])

  useEffect(() => { containersRef.current = containers }, [containers])
  useEffect(() => { filterRef.current = filter }, [filter])
  useEffect(() => { syncRef.current = sync }, [sync])

  // 实时：收到宠物更新时防抖重载当前页;若带 focusGid(客户端刚调整位置),
  // 自动切到该宠物所在页并选中,示意图跟随展示其盒子/队伍。
  useEffect(() => {
    return subscribe('pet', (d) => {
      // 同步关闭时不自动跳转,避免打断当前筛选(仍走下方防抖刷新,列表静默更新)
      const focus = d && d.focusGid
      if (focus && syncRef.current) {
        setSelected(focus)
        // 清掉其它筛选、改按该宠物移动后所在的盒子过滤:既保证被选中的宠物一定在列表中
        // (否则原有筛选可能把它排除),又通过 filter.box 联动让左上角示意图切到该盒。
        const f = filterRef.current
        const base = { pageSize: f.pageSize, sort: f.sort, order: f.order }
        const box = d.focusBox
        if (box) {
          const cont = containersRef.current.find((c) => c.type === 'box' && c.id === box)
          base.box = cont ? `${cont.id}-${cont.name}` : `${box}-`
        }
        getPetPage(focus, base)
          .then((r) => setFilter({ ...base, page: (r && r.page) || 1 }))
          .catch(() => setFilter({ ...base, page: 1 }))
        loadBoxes()
      }
      // 防抖重载:连串的宠物更新(如分页同步一次推几十条)只在静默 600ms 后拉一次,
      // 避免抖动期间反复重取整页。load 内部读的是最新 fetcher,拿到的是最新筛选
      // (含 focus 刚切过去的那一页),不会被旧闭包拉回切换前的页。
      clearTimeout(reloadRef.current)
      reloadRef.current = setTimeout(() => {
        load()
        loadBoxes()
      }, 600)
    })
  }, [load, loadBoxes, setFilter])

  // 卸载时清掉防抖定时器:否则组件销毁后仍会触发一次拉取与 setState。
  useEffect(() => () => clearTimeout(reloadRef.current), [])

  const set = (patch) => setFilter((f) => ({ ...f, ...patch, page: patch.page || 1 }))
  const toggleType = (t) =>
    setFilter((f) => {
      const s = new Set(f.types || [])
      s.has(t) ? s.delete(t) : s.add(t)
      return { ...f, types: [...s], page: 1 }
    })
  const sortBy = (key) =>
    setFilter((f) => ({ ...f, sort: key, order: f.sort === key && f.order === 'asc' ? 'desc' : 'asc', page: 1 }))
  // 打开详情弹窗(不离开列表,保留当前操作状态);复制编号到剪贴板
  const openDetail = (gid) => { setSelected(gid); setDetailGid(gid); setMenu(null) }
  const copyGid = (gid) => {
    try { navigator.clipboard && navigator.clipboard.writeText(String(gid)) } catch { /* ignore */ }
    setMenu(null)
  }
  // 重置:清空所有过滤条件,保留排序与每页档位
  const reset = () => setFilter((f) => ({ page: 1, pageSize: f.pageSize, sort: f.sort, order: f.order }))

  // 右键/长按菜单:选中并在 (x,y) 弹出(限制不溢出视口),菜单内带上宠物用于"筛选相同…"
  const openMenu = (p, x, y) => {
    setSelected(p.gid)
    menuAtRef.current = Date.now()
    setMenu({ gid: p.gid, pet: p, x: Math.min(x, window.innerWidth - 140), y: Math.min(y, window.innerHeight - 180) })
  }
  // 应用一项筛选并关闭菜单(set 会把页码重置为 1)
  const filterSame = (patch) => { set(patch); setMenu(null) }
  // 菜单打开后:点击空白/滚动/Esc 关闭(忽略打开瞬间紧随的合成 click)
  useEffect(() => {
    if (!menu) return
    const close = (e) => {
      if (e && e.target && e.target.closest && e.target.closest('.ctx-menu')) return
      if (Date.now() - menuAtRef.current < 350) return
      setMenu(null)
    }
    const onKey = (e) => { if (e.key === 'Escape') setMenu(null) }
    window.addEventListener('click', close)
    window.addEventListener('scroll', close, true)
    window.addEventListener('keydown', onKey)
    return () => {
      window.removeEventListener('click', close)
      window.removeEventListener('scroll', close, true)
      window.removeEventListener('keydown', onKey)
    }
  }, [menu])

  // 选中宠物:高亮 + 示意图跟随展示其盒子/队伍
  const selectPet = (p) => {
    setSelected(p.gid)
    if (p.team) setActiveIdx(0)
    else if (p.box) { const i = boxIdxById(p.box.boxId); if (i >= 0) setActiveIdx(i) }
  }
  // 点击示意图格子:选中该宠物,并跳到列表里它所在页(超过一页时切页)。
  // 优先在「当前筛选」下定位:命中则仅切页、保留用户已设的筛选条件;
  // 仅当该宠物被当前筛选排除(或查询失败)时,才回退清空其它条件(仅保留排序/每页档位),
  // 确保目标宠物一定落在列表中。盒子格→筛到该盒;队伍格→不限盒。
  const onCell = (gid, container) => {
    setSelected(gid)
    const fallback = () => {
      const cleared = { pageSize: filter.pageSize, sort: filter.sort, order: filter.order }
      const base = container.type === 'box'
        ? { ...cleared, box: `${container.id}-${container.name}` }
        : { ...cleared }
      getPetPage(gid, base)
        .then((r) => setFilter({ ...base, page: (r && r.page) || 1 }))
        .catch(() => setFilter({ ...base, page: 1 }))
    }
    getPetPage(gid, withCatch(filter))
      .then((r) => { if (r && r.found) setFilter((f) => ({ ...f, page: r.page || 1 })); else fallback() })
      .catch(fallback)
  }

  // 列表项交互:单击选中、双击详情、右键(桌面)/长按(移动)弹菜单
  const itemProps = (p) => ({
    onClick: () => { if (lpFiredRef.current) { lpFiredRef.current = false; return } selectPet(p) },
    onDoubleClick: () => openDetail(p.gid),
    onContextMenu: (e) => { e.preventDefault(); openMenu(p, e.clientX, e.clientY) },
    onTouchStart: (e) => {
      lpFiredRef.current = false
      const t = e.touches[0]
      lpRef.current = setTimeout(() => { lpFiredRef.current = true; openMenu(p, t.clientX, t.clientY) }, 450)
    },
    onTouchMove: () => clearTimeout(lpRef.current),
    onTouchEnd: () => clearTimeout(lpRef.current),
  })

  const active = containers[Math.min(activeIdx, containers.length - 1)]
  const pages = Math.max(1, Math.ceil(data.total / filter.pageSize))

  return (
    <div className="list-layout">
      <FilterPanel
        filter={filter} options={{ ...options, natureMatrix: nameOpts.nature }} total={data.total}
        collapsed={collapsed} onClose={() => setCollapsed(true)}
        set={set} toggleType={toggleType} reset={reset}
      >
        <BoxMap
          container={active} selected={selected} onCell={onCell}
          onPrev={() => setActiveIdx((i) => (i - 1 + containers.length) % containers.length)}
          onNext={() => setActiveIdx((i) => (i + 1) % containers.length)}
        />
      </FilterPanel>

      <section>
        <div className="toolbar list-toolbar">
          <button className="btn filter-toggle" onClick={() => setCollapsed((c) => !c)}>筛选</button>
          <input className="input" placeholder="搜索昵称 / 种类" value={filter.search || ''} onChange={(e) => set({ search: e.target.value })} />
          <Dropdown
            className="sort-select"
            value={filter.sort}
            options={SORTS.map((s) => ({ value: s.key, label: s.label }))}
            onChange={(v) => set({ sort: v })}
            placeholder="排序"
          />
          <button className="btn" onClick={() => set({ order: filter.order === 'asc' ? 'desc' : 'asc' })}>{filter.order === 'asc' ? '升序' : '降序'}</button>
          <button className={'btn' + (sync ? ' primary' : '')} title="开启后,游戏内捕捉/移动宠物会自动跳转并选中该宠物;关闭可避免打断当前筛选" onClick={() => setSync((v) => !v)}>同步</button>
          <div className="spacer" />
          {/* 视图开关与「共 N 只」打包成一个不换行整体:窄屏下 flex-wrap 会让它们
              各掉一行,而「共 N 只」读起来本该紧跟视图开关(它们都属于"当前列表的
              状态")。打包后这个整体只作为一项参与换行,内部永不分家。 */}
          <div className="list-tail">
            {/* 默认视图排在最左:分段控件的首位会被读成"主推的那个",
                与默认项错开时会让人以为当前视图是次要的那个。
                ⚠️ 改 DEFAULT_VIEW 时必须同步调换这里的顺序(见 filters.js)。 */}
            <div className="viewseg" role="group" aria-label="列表视图">
              <button
                type="button" className={'viewseg-b' + (view === 'gallery' ? ' on' : '')}
                aria-pressed={view === 'gallery'}
                title="陈列:宠物图为主,适合一屏扫多只、找变异与体型"
                onClick={() => setView('gallery')}
              >陈列</button>
              <button
                type="button" className={'viewseg-b' + (view === 'table' ? ' on' : '')}
                aria-pressed={view === 'table'}
                title="表格:逐列对齐,适合把这一页的百分位/声音竖着比"
                onClick={() => setView('table')}
              >表格</button>
            </div>
            <span className="muted">共 {data.total} 只</span>
          </div>
        </div>

        {view === 'table'
          ? <PetTable pets={data.pets} selected={selected} sort={filter.sort} order={filter.order} onSort={sortBy} itemProps={itemProps} />
          : <PetGallery pets={data.pets} selected={selected} itemProps={itemProps} />}

        {/* 首次加载(还没有任何宠物数据)时铺骨架而不是「没有匹配的宠物」——
            后者是**结果**,加载中报结果会把空列表误读成「筛了个寂寞」。
            换筛选条件时 useAsyncData 保留旧数据,故只有真正从零开始那一次会见到骨架。 */}
        {data.pets.length === 0 && (loading
          // 骨架形状跟着视图走:表格行是 44px 的扁条,陈列卡是 ~336px 的方块。
          // 用同一份扁条骨架铺陈列视图会「先矮后高」地跳一下,而那不是加载变快了,
          // 只是骨架没铺对形状 —— 观感上等同于假进度。
          ? <SkeletonRows rows={6} h={view === 'table' ? 44 : 336} gap={8} />
          : <div className="empty">没有匹配的宠物</div>)}

        <div className="pager">
          <button className="btn" disabled={filter.page <= 1} onClick={() => set({ page: 1 })}>首页</button>
          <button className="btn" disabled={filter.page <= 1} onClick={() => set({ page: filter.page - 1 })}>上一页</button>
          <span className="muted">{filter.page} / {pages}</span>
          <button className="btn" disabled={filter.page >= pages} onClick={() => set({ page: filter.page + 1 })}>下一页</button>
          <button className="btn" disabled={filter.page >= pages} onClick={() => set({ page: pages })}>尾页</button>
          <Dropdown
            className="pager-size"
            small
            value={filter.pageSize}
            options={[10, 20, 30, 60, 100].map((n) => ({ value: n, label: `${n} 条/页` }))}
            onChange={(v) => set({ pageSize: +v })}
            placeholder="条/页"
          />
        </div>
      </section>

      <ContextMenu menu={menu} onDetail={openDetail} onCopy={copyGid} onFilterSame={filterSame} />

      {detailGid != null && <PetDetailModal gid={detailGid} onClose={() => setDetailGid(null)} />}
    </div>
  )
}
