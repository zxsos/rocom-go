import React, { useState, useEffect, useContext, useRef, useCallback, useMemo } from 'react'
import { getEvents, getEventCount, getEventStats, clearEvents, subscribe } from '../../api'
import { AccountContext } from '../../context'
import { useStoredState, useStoredFlag, useStoredJSON } from '../../hooks/useStoredState'
import { useWakeLock, wakeLockSupported } from '../../hooks/useWakeLock'
import { confirmDialog } from '../../components/confirm'
import { useAsyncData } from '../../hooks/useAsyncData'
import { Avatar } from '../../components/avatar'
import { Marks, Blood, Gender } from '../../components/badges'
import { PetDetailModal } from '../../components/PetDetailModal'
import { TweenNumber } from '../../components/TweenNumber'
import { locTag, fmtTime, voiceHot, pctHot } from '../../utils/format'
import { chime, rareChime } from '../../utils/audio'
import { sanitizeRules, isHighlight, matchedRules, NOTABLE_BLOODS, SUB_KINDS } from './highlight'
import RulePanel from './RulePanel'
import Dropdown from '../../components/Dropdown'
import { useRangeRules } from '../../hooks/useRangeRules'
import { matchRangeRule } from '../../utils/rules'

// 空列表的兜底常量:引用稳定,免得每次渲染造新数组打穿下游 memo。
const NO_EVENTS = []

export default function Events() {
  const account = useContext(AccountContext)
  const [rules, setRules] = useStoredJSON(localStorage, 'hlRules', [], sanitizeRules)
  // 体重/声音的区间规则:**与大地图共用同一份**(见 hooks/useRangeRules)。
  // 这里只消费、不自带副本 —— 在两个页面各配一遍既麻烦,也会让两边判定口径漂移。
  const [rangeRules, setRangeRules] = useRangeRules()
  // 只看指定来源(捕捉/孵蛋/赠送获得/获得);空=不筛选。
  const [srcs, setSrcs] = useStoredState(localStorage, 'ev.srcs',
    (s) => (s ? s.split(',').filter((k) => SUB_KINDS.includes(k)) : []),
    (v) => (v || []).join(','))
  // 多规则联合逻辑:'and'=需全部命中(默认)、'or'=任一命中
  const [mode, setMode] = useStoredState(localStorage, 'hlMode', (s) => (s === 'or' ? 'or' : 'and'), (v) => v)
  // 规则抽屉开合(仅移动端生效,桌面侧栏常驻)
  const [collapsed, setCollapsed] = useStoredFlag(sessionStorage, 'hlCollapsed', true)
  // 仅展示命中高亮规则的事件
  const [onlyHl, setOnlyHl] = useStoredFlag(localStorage, 'onlyHl', false)
  // 统计图表折叠(手机竖屏图表占比大,默认收起;桌面空间充足默认展开;用户手动切换后按选择持久化)
  const [statsOpen, setStatsOpen] = useStoredState(
    localStorage, 'ev.statsOpen',
    (s) => (s === null ? !window.matchMedia('(max-width: 760px)').matches : s === '1'),
    (v) => (v ? '1' : '0'),
  )
  // 屏幕常亮开关(Screen Wake Lock)
  const [keepAwake, setKeepAwake] = useStoredFlag(localStorage, 'keepAwake', false)
  // 规则命中提示音开关:新捕获事件命中高亮规则时响铃(异色/炫彩响升级音)。默认关,不打扰。
  const [soundOn, setSoundOn] = useStoredFlag(localStorage, 'ev.hlSound.v1', false)
  const [detailGid, setDetailGid] = useState(null) // 详情弹窗的 gid(null=关闭)
  // 分页:页码分页(offset 游标),列表底部「第 X / Y 页 + 每页条数」;实时推送仍进顶部(仅第 1 页)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useStoredState(localStorage, 'ev.pageSize',
    (s) => [20, 50, 100, 200].includes(parseInt(s, 10)) ? parseInt(s, 10) : 100, (v) => String(v))
  useWakeLock(keepAwake)

  // 三路数据拉取。必须放在 page/pageSize 声明**之后**:fetcher 闭包引用了它们,
  // 提前到前面会因 TDZ(Cannot access 'page' before initialization)直接白屏。
  // 列表:页码/每页条数变化时按 offset 拉取,替换当前页(不追加)。
  const { data: events, setData: setEvents, loading } = useAsyncData(
    useCallback(
      () => getEvents({ limit: pageSize, offset: (page - 1) * pageSize }).then((e) => e || []),
      [page, pageSize],
    ),
    { fallback: NO_EVENTS, reloadKey: account },
  )
  // total=自上次清空以来累计获得的宠物数(即列表最新一条的序号);列表可能因上限被截断,
  // 故序号以后端总数为准:列表第 i 条(0=最新)序号 = total - i。
  const { data: total, setData: setTotal } = useAsyncData(
    useCallback(() => getEventCount().then((r) => (r && r.count) || 0), []),
    { fallback: 0, reloadKey: account },
  )
  // 事件统计(总览/稀有/近30天分布/热门形态):随账号与每个新事件刷新。
  const { data: stats, setData: setStats, refresh: refreshStats } = useAsyncData(
    useCallback(() => getEventStats(), []),
    { reloadKey: account },
  )

  // subscribe 的 effect 只依赖 account,回调里读 rules/mode/soundOn/page/pageSize 需走 ref 拿最新值
  // 区间规则也要进 ref:提示音按最新规则判,否则改完规则后旧事件仍按旧口径响铃。
  const soundRef = useRef({ rules, rangeRules, mode, soundOn })
  const pageRef = useRef({ page, pageSize })
  useEffect(() => {
    soundRef.current = { rules, rangeRules, mode, soundOn }
    pageRef.current = { page, pageSize }
  })

  // 后端只记录获得宠物事件(放生/赠送出等减少事件不入库),故无需再按类型过滤。
  useEffect(() => subscribe('event', (ev) => {
      // 规则命中提示音:命中高亮规则的新捕获响铃,异色/炫彩(最高优先级)响升级音
      const { rules: rs, rangeRules: rr, mode: md, soundOn: so } = soundRef.current
      const pet = ev && ev.pet
      if (so && isHighlight(pet, rs, rr, md)) {
        pet.shiny || pet.colorful ? rareChime() : chime()
      }
      // 仅第 1 页实时推进(头部插入并裁掉超出本页的旧条);其他页保持不动,翻回第 1 页时重拉可见
      const { page: curPage, pageSize: curSize } = pageRef.current
      if (curPage === 1) setEvents((prev) => [ev, ...prev].slice(0, curSize))
      setTotal((n) => n + 1)
      refreshStats() // 新事件入库后刷新统计
    }), [refreshStats, setEvents, setTotal])

  // 点选条目:已选则移除、未选则添加(即时生效,无需「添加」按钮);addRule 只添加(去重)。
  const hasRule = (field, value) => rules.some((r) => r.field === field && r.value === value)
  const addRule = (field, value) => { if (!hasRule(field, value)) setRules((r) => [...r, { field, value }]) }
  const toggleRule = (field, value) => setRules((r) => hasRule(field, value)
    ? r.filter((x) => !(x.field === field && x.value === value))
    : [...r, { field, value }])
  // 来源筛选:点已选中的取消、未选中的加入;空数组=不筛选(看全部)。
  const toggleSrc = (k) => setSrcs((prev) =>
    prev.includes(k) ? prev.filter((x) => x !== k) : [...prev, k])

  // 各区间规则在当前页的命中数,供规则编辑器逐条显示。
  // 只统计**已加载的这一页**:总数要遍历全部事件才准,而这里只是给用户调区间时
  // 一个即时反馈(拖宽区间数字就涨),不必为它再拉一次全量。
  const rangeCounts = useMemo(() => {
    const c = {}
    for (const r of rangeRules) {
      if (!r.on) continue
      c[r.id] = events.filter((ev) => ev.pet && matchRangeRule(ev.pet, r)).length
    }
    return c
  }, [events, rangeRules])

  // 先算高亮与命中规则,再依次过「仅看命中」「来源」两道筛选,最后统一渲染。
  //
  // 顺序有讲究:序号 # 取自**未过滤**的原始下标 i,若先 filter 再 map,被筛掉的行
  // 会让后面所有行的序号错位(列表里 # 是「第几只」,必须稳定)。
  // 命中规则(hits)也在这里一次算好,避免渲染时每行再算一遍。
  const filtered = useMemo(() => events
    .map((ev, i) => ({
      ev, i,
      hl: isHighlight(ev.pet, rules, rangeRules, mode),
      hits: matchedRules(ev.pet, rules, rangeRules),
    }))
    .filter(({ hl }) => !onlyHl || hl)
    .filter(({ ev }) => srcs.length === 0 || srcs.includes(ev.subKind)),
  [events, rules, rangeRules, mode, onlyHl, srcs])

  // 清空事件历史(后端删除 + 前端清列表并将计数归零,下次获得从 1 重新计)
  const clearAll = async () => {
    if (!await confirmDialog({
      message: '确定清空所有事件历史?计数将从头开始。',
      okText: '清空', danger: true,
    })) return
    clearEvents().then(() => { setEvents([]); setTotal(0); setStats(null); setPage(1) }).catch(() => {})
  }

  // 总页数(事件总数 = total,实时推送会递增)
  const totalPages = Math.max(1, Math.ceil(total / pageSize))

  return (
    <div className="list-layout events-page">
      <RulePanel
        rules={rules} mode={mode} setMode={setMode}
        addRule={addRule} toggleRule={toggleRule}
        rangeRules={rangeRules} setRangeRules={setRangeRules} rangeCounts={rangeCounts}
        collapsed={collapsed} onClose={() => setCollapsed(true)}
      />

      <section className="events-main">
        <div className="toolbar list-toolbar event-head">
          <button className="btn filter-toggle" onClick={() => setCollapsed(false)}>规则{rules.length ? ` (${rules.length})` : ''}</button>
          <strong className="event-title">捕获事件</strong>
          <span className="muted">共 {total} 只</span>
          <div className="spacer" />
          {/* 三个操作统一为单图标,含义见各自 title */}
          <button className={'btn btn-icon' + (statsOpen ? ' primary' : '')} onClick={() => setStatsOpen((v) => !v)}
            title={statsOpen ? '收起统计图表' : '展开统计图表'}>{statsOpen ? '▴' : '▾'}</button>
          <button className={'btn btn-icon' + (onlyHl ? ' primary' : '')} onClick={() => setOnlyHl((v) => !v)}
            title="仅展示命中高亮规则的事件">{onlyHl ? '★' : '☆'}</button>
          {wakeLockSupported
            ? <button className={'btn btn-icon' + (keepAwake ? ' primary' : '')} onClick={() => setKeepAwake((v) => !v)}
                title="阻止屏幕熄灭,方便盯着高亮提醒">☀</button>
            : <button className="btn btn-icon" disabled title="当前非 HTTPS/localhost 环境,浏览器不提供屏幕常亮">☀</button>}
          <button className={'btn btn-icon' + (soundOn ? ' primary' : '')} onClick={() => setSoundOn((v) => !v)}
            title="规则命中提示音,新捕获命中高亮规则时响铃(异色/炫彩响升级音)">{soundOn ? '🔊' : '🔈'}</button>
          <button className="btn btn-icon" disabled={events.length === 0} onClick={clearAll} title="清空事件历史">🗑</button>
        </div>
        {/* 来源筛选:获得方式由 catch_way 推断(捕捉/孵蛋/赠送获得/获得)。
            此前列表里完全看不出来源,只能点开详情 —— 混在一起时想单看「这波孵了几只」
            做不到。空选=看全部,故「全部」只在已筛选时出现。 */}
        <div className="event-srcs">
          <span className="muted small">来源</span>
          {SUB_KINDS.map((k) => (
            <button key={k} className={'chip' + (srcs.includes(k) ? ' on' : '')} onClick={() => toggleSrc(k)}
              title={srcs.includes(k) ? `取消「${k}」` : `只看${k}`} aria-pressed={srcs.includes(k)}>{k}</button>
          ))}
          {srcs.length > 0 && (
            <button className="chip" onClick={() => setSrcs([])} title="清除来源筛选,看全部">全部</button>
          )}
        </div>
        {statsOpen && stats && (
          <div className="event-stats">
            <div className="stat-cards">
              {/* 统计数字用 TweenNumber:新事件推入时这些数会 +1,
                  滚动一下让「刚抓住一只」这件事被看见(组件注释见 components/TweenNumber.jsx)。 */}
              <div className="stat-card">
                <div className="stat-num"><TweenNumber value={stats.total} /></div>
                <div className="stat-label">累计获得</div>
              </div>
              {['捕捉', '孵蛋', '赠送获得', '获得'].map((k) => (stats.bySubKind[k] || 0) > 0 && (
                <div className="stat-card" key={k}>
                  <div className="stat-num"><TweenNumber value={stats.bySubKind[k]} /></div>
                  <div className="stat-label">{k}</div>
                </div>
              ))}
              <div className="stat-card">
                <div className="stat-num"><TweenNumber value={stats.shiny} /></div>
                <div className="stat-label">异色</div>
              </div>
              <div className="stat-card">
                <div className="stat-num"><TweenNumber value={stats.colorful} /></div>
                <div className="stat-label">炫彩</div>
              </div>
            </div>
            <div className="stat-chart" title="近30天每天获得数">
              {(() => {
                const max = Math.max(1, ...stats.daily.map((d) => d.n))
                return stats.daily.map((d) => (
                  <div className="bar" key={d.day} title={`${d.day} · ${d.n} 只`}>
                    <div className="bar-fill" style={{ height: `${Math.round((d.n / max) * 100)}%` }} />
                    <span className="bar-day">{d.day.slice(3)}</span>
                  </div>
                ))
              })()}
            </div>
            {stats.topSpecies.length > 0 && (
              <div className="stat-top">
                {stats.topSpecies.map((t) => (
                  <div className="top-item" key={t.s}>
                    <span className="top-name">{t.s}</span>
                    <div className="top-track">
                      <div className="top-fill" style={{ width: `${Math.round((t.n / stats.topSpecies[0].n) * 100)}%` }} />
                    </div>
                    <span className="top-n">{t.n}</span>
                  </div>
                ))}
              </div>
            )}
          </div>
        )}
        <div className="event-list">
          {/* 先按原始下标算序号(#total-offset-i)与高亮,再按"仅看高亮"过滤,保证序号不因过滤错位 */}
          {filtered.map(({ ev, i, hl, hits }) => (
            <EventItem key={ev.id || ev.gid + '-' + ev.time} ev={ev} seq={total - (page - 1) * pageSize - i} hl={hl}
              hits={hits} onOpen={() => ev.gid && setDetailGid(ev.gid)} />
          ))}
          {loading && <div className="empty muted">加载中…</div>}
          {!loading && events.length === 0 && <div className="empty">暂无事件。游戏中捕捉/孵蛋新宠物后将实时出现在这里。</div>}
          {(onlyHl || srcs.length > 0) && events.length > 0 && filtered.length === 0 && (
            <div className="empty">
              当前筛选下没有事件。
              {onlyHl && rules.length === 0 && rangeRules.every((r) => !r.on) ? '请先添加高亮规则。' : ''}
            </div>
          )}
          {events.length > 0 && (
            <div className="event-pager">
              <label className="muted pager-size">
                每页
                <Dropdown
                  small
                  value={pageSize}
                  options={[20, 50, 100, 200].map((n) => ({ value: n, label: String(n) }))}
                  onChange={(v) => { setPageSize(Number(v)); setPage(1) }}
                />
                条
              </label>
              <button className="btn" disabled={page <= 1 || loading} onClick={() => setPage((p) => p - 1)}>‹ 上一页</button>
              <span className="muted">第 {page} / {totalPages} 页</span>
              <button className="btn" disabled={page >= totalPages || loading} onClick={() => setPage((p) => p + 1)}>下一页 ›</button>
            </div>
          )}
        </div>
      </section>

      {detailGid != null && <PetDetailModal gid={detailGid} onClose={() => setDetailGid(null)} />}
    </div>
  )
}

// EventItem 单条捕获事件:头像 + 名称徽标行 + 关键数值摘要;点击打开详情弹窗。
//
// hits 是该条命中了哪些规则(含区间规则自带的颜色):高亮本身只把整行染成金色,
// 看不出是体重还是声音命中的 —— 这些小标签就是那层解释,颜色与地图描边同源,
// 同一条规则在两个页面认得出是同一条。
function EventItem({ ev, seq, hl, hits = [], onOpen }) {
  const p = ev.pet
  return (
    <div className={'event' + (hl ? ' hl' : '')} onClick={onOpen}>
      <Avatar p={p} />
      <div className="event-body">
        <div className="event-row">
          <span className="event-seq muted">#{seq}</span>
          <span className="pet-name">
            {p?.name || p?.species}
            <Gender g={p?.gender} />
            <Marks p={p} />
            {NOTABLE_BLOODS.includes(p?.blood) && <Blood p={p} iconOnly />}
          </span>
          {/* 获得方式:由 catch_way 推断(见服务端 catchWayName)。放在名称后,
              与异色/炫彩这类「本身稀有」的标记分开 —— 来源是背景信息,不是稀有度。 */}
          {ev.subKind && <span className="event-src">{ev.subKind}</span>}
          {hits.length > 0 && (
            <span className="rule-tags">
              {hits.map((h) => (
                <span key={h.id} className="rule-tag">
                  {/* 色点用行内 background 而非 CSS 变量:颜色取自规则本身,
                      走变量的话 check-css-vars 认不出「它在行内定义」会报未定义。 */}
                  <i className="rule-tag-dot" style={{ background: h.color }} />
                  {h.label}
                </span>
              ))}
            </span>
          )}
          <span className="event-time muted">{fmtTime(ev.time)}</span>
        </div>
        <div className="pet-sub">
          {p?.nature}
          {p?.speciality && p.speciality !== '无' ? ` · ${p.speciality}` : ''}
          {' · W '}<span className={pctHot(p?.weightPct)}>{p?.weightPct != null ? `${Math.round(p.weightPct)}%` : '-'}</span>
          {' · V '}<span className={voiceHot(p?.voice)}>{p?.voice ?? '-'}</span>
          {' · '}{locTag(p)}
        </div>
      </div>
    </div>
  )
}
