import React, { useState, useEffect, useCallback, useRef, useMemo, useContext } from 'react'
import { getPets, getFilterOptions, getBoxes, getTeams, getPetPage, subscribe } from '../../api'
import { AccountContext } from '../../context'
import { useStoredFlag, useStoredJSON } from '../../hooks/useStoredState'
import { useFullscreen } from '../../hooks/useFullscreen'
import { PetDetailModal } from '../../components/PetDetailModal'
import { SORTS, withCatch, FILTER_KEY, DEFAULT_FILTER, sanitizeFilter } from './filters'
import FilterPanel from './FilterPanel'
import BoxMap from './BoxMap'
import PetTable from './PetTable'
import PetCards from './PetCards'
import ContextMenu from './ContextMenu'

export default function PetList() {
  const account = useContext(AccountContext)
  const [filter, setFilter] = useStoredJSON(sessionStorage, FILTER_KEY, DEFAULT_FILTER, sanitizeFilter)
  const [collapsed, setCollapsed] = useStoredFlag(sessionStorage, 'petListCollapsed', true)
  const [sync, setSync] = useStoredFlag(localStorage, 'petSync', true) // 实时同步:游戏内操作自动跳转到对应宠物(默认开)
  const [detailGid, setDetailGid] = useState(null) // 详情弹窗的 gid(null=关闭)
  const [data, setData] = useState({ total: 0, pets: [] })
  const [options, setOptions] = useState({})
  const [selected, setSelected] = useState(null) // 单击选中的 gid
  const [menu, setMenu] = useState(null)          // 右键/长按菜单 {gid,pet,x,y}
  const [boxes, setBoxes] = useState([])          // 各盒子槽位布局
  const [teams, setTeams] = useState({ slots: [] }) // 大世界三队 18 格
  const [activeIdx, setActiveIdx] = useState(0)   // 示意图当前容器下标(0=队伍)
  const fullscreen = useFullscreen()
  const reloadRef = useRef(null)
  const filterRef = useRef(filter)      // 供 SSE 回调读取最新筛选(避免闭包旧值)
  const containersRef = useRef([])       // 供 SSE 回调按盒号查容器名(避免闭包旧值)
  const lpRef = useRef(null)        // 长按定时器
  const lpFiredRef = useRef(false)  // 本次触摸是否已触发长按
  const menuAtRef = useRef(0)       // 菜单打开时刻(用于忽略紧随的合成 click)
  const syncRef = useRef(sync)      // 供 SSE 回调读取最新同步开关(避免闭包旧值)

  const load = useCallback(() => { getPets(withCatch(filter)).then(setData).catch(() => {}) }, [filter])
  const loadBoxes = useCallback(() => {
    getBoxes().then(setBoxes).catch(() => {})
    getTeams().then(setTeams).catch(() => {})
  }, [])
  useEffect(() => { load() }, [load])
  useEffect(() => { getFilterOptions().then(setOptions).catch(() => {}) }, [])
  useEffect(() => { loadBoxes() }, [loadBoxes])

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
  const boxIdxById = (id) => containers.findIndex((c) => c.type === 'box' && c.id === id)
  // 宠物盒筛选变化时,示意图跟随展示该盒
  useEffect(() => {
    const id = parseInt((filter.box || '').split('-')[0], 10)
    if (id) { const i = boxIdxById(id); if (i >= 0) setActiveIdx(i) }
  }, [filter.box, containers])

  useEffect(() => { containersRef.current = containers }, [containers])
  useEffect(() => { filterRef.current = filter }, [filter])
  useEffect(() => { syncRef.current = sync }, [sync])

  // 实时：收到宠物更新时防抖重载当前页;若带 focusGid(客户端刚调整位置),
  // 自动切到该宠物所在页并选中,示意图跟随展示其盒子/队伍。
  useEffect(() => {
    return subscribe((m) => {
      if (m.type !== 'pet') return
      if (m.account && m.account !== account) return // 只认当前账号的更新
      // 同步关闭时不自动跳转,避免打断当前筛选(仍走下方防抖刷新,列表静默更新)
      const focus = m.data && m.data.focusGid
      if (focus && syncRef.current) {
        setSelected(focus)
        // 清掉其它筛选、改按该宠物移动后所在的盒子过滤:既保证被选中的宠物一定在列表中
        // (否则原有筛选可能把它排除),又通过 filter.box 联动让左上角示意图切到该盒。
        const f = filterRef.current
        const base = { pageSize: f.pageSize, sort: f.sort, order: f.order }
        const box = m.data.focusBox
        if (box) {
          const cont = containersRef.current.find((c) => c.type === 'box' && c.id === box)
          base.box = cont ? `${cont.id}-${cont.name}` : `${box}-`
        }
        getPetPage(focus, base)
          .then((r) => setFilter({ ...base, page: (r && r.page) || 1 }))
          .catch(() => setFilter({ ...base, page: 1 }))
        loadBoxes()
      }
      // 防抖重载用 filterRef 读取最新筛选(含 focus 切过去的新页),
      // 避免捕获旧 load 闭包,在 600ms 后把列表拉回切换前的页。
      clearTimeout(reloadRef.current)
      reloadRef.current = setTimeout(() => {
        if (reloadRef.current) { getPets(withCatch(filterRef.current)).then(setData).catch(() => {}); loadBoxes() }
      }, 600)
    })
  }, [load, loadBoxes, account])

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
        filter={filter} options={options} total={data.total}
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
          <select className="select sort-select" value={filter.sort} onChange={(e) => set({ sort: e.target.value })}>
            {SORTS.map((s) => <option key={s.key} value={s.key}>{s.label}</option>)}
          </select>
          <button className="btn" onClick={() => set({ order: filter.order === 'asc' ? 'desc' : 'asc' })}>{filter.order === 'asc' ? '升序' : '降序'}</button>
          <button className={'btn' + (sync ? ' primary' : '')} title="开启后,游戏内捕捉/移动宠物会自动跳转并选中该宠物;关闭可避免打断当前筛选" onClick={() => setSync((v) => !v)}>同步</button>
          {fullscreen.supported && (
            <button className={'btn' + (fullscreen.isFull ? ' primary' : '')} title={fullscreen.isFull ? '退出网页全屏' : '网页全屏'} onClick={fullscreen.toggle}>全屏</button>
          )}
          <div className="spacer" />
          <span className="muted">共 {data.total} 只</span>
        </div>

        <PetTable pets={data.pets} selected={selected} sort={filter.sort} order={filter.order} onSort={sortBy} itemProps={itemProps} />
        <PetCards pets={data.pets} selected={selected} itemProps={itemProps} />

        {data.pets.length === 0 && <div className="empty">没有匹配的宠物</div>}

        <div className="pager">
          <button className="btn" disabled={filter.page <= 1} onClick={() => set({ page: 1 })}>首页</button>
          <button className="btn" disabled={filter.page <= 1} onClick={() => set({ page: filter.page - 1 })}>上一页</button>
          <span className="muted">{filter.page} / {pages}</span>
          <button className="btn" disabled={filter.page >= pages} onClick={() => set({ page: filter.page + 1 })}>下一页</button>
          <button className="btn" disabled={filter.page >= pages} onClick={() => set({ page: pages })}>尾页</button>
          <select className="select pager-size" value={filter.pageSize} onChange={(e) => set({ pageSize: +e.target.value })}>
            {[10, 20, 30, 60, 100].map((n) => <option key={n} value={n}>{n} 条/页</option>)}
          </select>
        </div>
      </section>

      <ContextMenu menu={menu} onDetail={openDetail} onCopy={copyGid} onFilterSame={filterSame} />

      {detailGid != null && <PetDetailModal gid={detailGid} onClose={() => setDetailGid(null)} />}
    </div>
  )
}
