import React from 'react'
import { imgURL } from '../../components/icons'
import { WILD_LAYERS } from './useWildPets'

// LayerPanel 图层侧栏:POI 图层开关;可收集图层(眠枭之星/不咕钟零件)行右侧另有收集模式小开关
// (开 = 隐藏该图层已收集的点,判定来源见 usePois.js)。另有「野生宠物」一组:不是固定点位,
// 而是附近实时刷出的稀有个体(见 useWildPets.js)。
// 复用宠物列表那套 .filters:桌面常驻左列,移动端为侧滑抽屉(collapsed 控制开合)。
export default function LayerPanel({ pois, wilds, home, collapsed, onClose }) {
  const { kinds, poiOn, togglePoi, collectOn, toggleCollect } = pois
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
            <div className="map-layer-row" key={k.k}>
              <button className={'map-layer-btn' + (poiOn.has(k.k) ? ' on' : '')}
                onClick={() => togglePoi(k.k)}>
                <img src={imgURL(k.icon)} alt="" draggable={false} />
                <span className="map-layer-name">{k.n}</span>
                <span className="muted">{k.num}</span>
              </button>
              {k.collect && (
                <button className={'map-collect-btn' + (collectOn.has(k.k) ? ' on' : '')}
                  onClick={() => toggleCollect(k.k)} disabled={!poiOn.has(k.k)}
                  title="收集模式:隐藏已收集的点(需先开启图层)" aria-label={`${k.n}收集模式`}
                  aria-pressed={collectOn.has(k.k)}>✓</button>
              )}
            </div>
          ))}
        </div>
        <div className="filter-group">
          <label>野生宠物</label>
          {WILD_LAYERS.map(({ k, n, color }) => {
            // 计数含灰点(与图上标记一致),悬浮再拆开说明其中多少已离开视野。
            const num = wilds.num[k] || 0
            const gone = wilds.numStale[k] || 0
            return (
              <div className="map-layer-row" key={k}>
                <button className={'map-layer-btn map-wild-btn' + (wilds.on.has(k) ? ' on' : '')}
                  onClick={() => wilds.toggle(k)}
                  title={gone ? `视野内 ${num - gone} · 已离开视野 ${gone}` : undefined}>
                  <span className="map-wild-swatch" style={{ borderColor: color }} />
                  <span className="map-layer-name">{n}</span>
                  <span className="muted">{num}</span>
                </button>
              </div>
            )
          })}
          <span className="muted" style={{ fontSize: 12, lineHeight: 1.5 }}>
            只标出周围已下发的野生宠(走近才知道);位置≈刷新点。灰色 = 已离开视野的最后所见,
            同样计入数量(悬浮看拆分)。
          </span>
        </div>
        {home.total > 0 && (
          <div className="filter-group">
            <label>家园</label>
            <div className="map-layer-row">
              <button className={'map-layer-btn' + (home.on ? ' on' : '')} onClick={home.toggle}>
                <span className="map-nest-swatch" />
                <span className="map-layer-name">精灵小窝</span>
                <span className="muted">{home.used}/{home.total}</span>
              </button>
            </div>
            <span className="muted" style={{ fontSize: 12, lineHeight: 1.5 }}>
              空窝画虚线圈;窝上有蛋则挂蛋图标({home.eggs} 颗待收)。悬浮看住户简要信息,点头像看详情。
              {home.stale && ' 本次进家园后有宠物进出窝,配对信息可能不全——重进一次家园即刷新。'}
            </span>
          </div>
        )}
        <div className="filters-foot">
          <button className="btn primary" onClick={onClose}>查看地图</button>
        </div>
      </aside>
    </>
  )
}
