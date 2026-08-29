import React, { useMemo, useState } from 'react'

// ZonePanel 区域收集度:列出本场景各分区的「已收集/总数」,按缺口从大到小排序,
// 点一行把地图移到该区中心。数据全部来自 GET /api/pois 的 zones[](服务器口径,
// 见 docs/data.md 3.4)——后端没动,只是这些数据原先只用于「收满即隐藏」的判定,
// 从没展示给用户。
//
// 与收集模式的分工:收集模式是「图上少显示几个点」;这里是「还差多少、差在哪」
// 的清单。两者互补:前者省事,后者指路。
const LS_KEY = 'map.zonePanelOpen'
const loadOpen = () => {
  try { return localStorage.getItem(LS_KEY) !== '0' } catch { return true }
}

export default function ZonePanel({ stats, onFocus, disabled }) {
  // 分组默认展开:这块信息是进地图页最想看的之一(还差哪),藏起来等于没有。
  // 但用户收起过就尊重选择(和图层开关一个套路)。
  const [open, setOpen] = useState(loadOpen)
  const toggle = () => setOpen((o) => {
    try { localStorage.setItem(LS_KEY, o ? '0' : '1') } catch { /* 隐私模式忽略 */ }
    return !o
  })

  const rows = stats?.rows || []
  const pct = stats && stats.tot > 0 ? (stats.got * 100) / stats.tot : 0

  // 「只看没收满」:集满的区域排在最后且通常很长(43 个区里可能 40 个都差几个),
  // 勾上后列表只剩真正要去的地方。
  const [onlyLeft, setOnlyLeft] = useState(false)
  const shown = useMemo(
    () => (onlyLeft ? rows.filter((z) => z.miss > 0) : rows),
    [rows, onlyLeft])

  if (rows.length === 0) {
    return (
      <div className="filter-group">
        <label>收集进度</label>
        <span className="muted" style={{ fontSize: 13 }}>该场景暂无收集进度</span>
      </div>
    )
  }

  return (
    <div className="filter-group">
      <label>收集进度</label>

      {/* 总览行:也是展开/收起的开关。收起时仍能一眼看到总进度。 */}
      <button className="map-medal-toggle map-zone-head" onClick={toggle} aria-expanded={open}
        title="各分区的眠枭之星收集进度(服务器口径);点开看明细,点某区可把地图移过去">
        <span>眠枭之星</span>
        <span className="map-zone-total">
          <b>{stats.got}</b><i>/{stats.tot}</i>
          <em className="muted">{pct.toFixed(pct < 10 ? 1 : 0)}%</em>
        </span>
        <span className="muted">▾</span>
      </button>

      {open && (
        <>
          {/* 总进度条:与孵蛋页同一套观感(--accent 填充),不新造视觉语言。 */}
          <div className="map-zone-bar" title={`已收集 ${stats.got} / 共 ${stats.tot}`}>
            <div className="map-zone-bar-fill" style={{ width: `${pct}%` }} />
          </div>

          <div className="map-zone-filter">
            <button className={'map-collect-btn' + (onlyLeft ? ' on' : '')}
              onClick={() => setOnlyLeft((v) => !v)} aria-pressed={onlyLeft}
              title="只看还没集满的分区" aria-label="只看未集满">✓</button>
            <span className="map-layer-name">只看未集满</span>
            <span className="muted">
              {rows.filter((z) => z.miss === 0).length} / {rows.length} 区已集满
            </span>
          </div>

          {/* 明细:缺口大的在前。点行定位;不能定位时(无底图/该区无点位)整行禁用,
              但不影响看数——进度本身就是有用的信息。 */}
          {shown.map((z) => {
            const p = z.tot > 0 ? (z.got * 100) / z.tot : 0
            const done = z.miss === 0
            const canFocus = !disabled && z.n > 0 && z.u != null
            return (
              <div className={'map-zone-row' + (done ? ' done' : '')} key={z.camp}>
                <button className="map-zone-btn" disabled={!canFocus}
                  onClick={() => canFocus && onFocus(z)}
                  title={canFocus
                    ? `把地图移到${z.name}(${z.n} 个点位的中心)`
                    : z.n > 0 ? '该场景没有底图,无法定位' : `${z.name}:本场景无点位`}>
                  <span className="map-zone-name">{z.name}</span>
                  <span className="map-zone-num">
                    <b>{z.got}</b><i>/{z.tot}</i>
                  </span>
                  <span className="map-zone-miss muted">{done ? '已集满' : `缺 ${z.miss}`}</span>
                  <span className="map-zone-bar map-zone-bar-sm">
                    <span className="map-zone-bar-fill" style={{ width: `${p}%` }} />
                  </span>
                </button>
              </div>
            )
          })}
          {shown.length === 0 && (
            <span className="muted" style={{ fontSize: 13, padding: '4px 2px' }}>
              全部分区都已集满 🎉
            </span>
          )}
        </>
      )}
    </div>
  )
}
