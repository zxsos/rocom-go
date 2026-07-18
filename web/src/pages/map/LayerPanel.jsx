import React from 'react'
import { imgURL } from '../../components/icons'

// LayerPanel 图层侧栏:POI 图层开关 + 眠枭之星收集模式。
// 复用宠物列表那套 .filters:桌面常驻左列,移动端为侧滑抽屉(collapsed 控制开合)。
export default function LayerPanel({ pois, collapsed, onClose }) {
  const { kinds, poiOn, togglePoi, hasStars, starMode, toggleStarMode, starStat } = pois
  return (
    <>
      <div className={'filters-backdrop' + (collapsed ? '' : ' show')} onClick={onClose} />
      <aside className={'filters map-filters' + (collapsed ? ' collapsed' : '')}>
        <div className="filters-bar">
          <span className="filters-title">图层</span>
          <button className="icon-btn" onClick={onClose} aria-label="关闭图层">✕</button>
        </div>
        <div className="filter-group">
          <label>地图图标</label>
          {kinds.length === 0 && <span className="muted" style={{ fontSize: 13 }}>该场景暂无可显示的图标</span>}
          {kinds.map((k) => (
            <button key={k.k} className={'map-layer-btn' + (poiOn.has(k.k) ? ' on' : '')}
              onClick={() => togglePoi(k.k)}>
              <img src={imgURL(k.icon)} alt="" draggable={false} />
              <span className="map-layer-name">{k.n}</span>
              <span className="muted">{k.num}</span>
            </button>
          ))}
        </div>
        {/* 眠枭之星收集模式:隐藏已收集的,只留还没拿的。判定来源见 usePois.js。 */}
        {hasStars && (
          <div className="filter-group">
            <label>眠枭之星</label>
            <button className={'map-layer-btn' + (starMode ? ' on' : '')} onClick={toggleStarMode}>
              <span className="map-layer-name">收集模式(隐藏已收集)</span>
              <span className="muted">{starMode ? '开' : '关'}</span>
            </button>
            {starMode && (
              <span className="muted map-star-stat">
                已隐藏 {starStat.hidden} / {starStat.total};其中 {starStat.sure} 处已确认还在。
                未走到过、且所在区域没收满的点一律仍显示。
              </span>
            )}
          </div>
        )}
        <div className="filters-foot">
          <button className="btn primary" onClick={onClose}>查看地图</button>
        </div>
      </aside>
    </>
  )
}
