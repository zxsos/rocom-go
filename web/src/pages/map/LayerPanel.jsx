import React from 'react'
import { imgURL } from '../../components/icons'
import { WILD_LAYERS, MEDAL_FILTERS } from './useWildPets'

// LayerPanel 图层侧栏:POI 图层开关;可收集图层(眠枭之星/不咕钟零件)行右侧另有收集模式小开关
// (开 = 隐藏该图层已收集的点,判定来源见 usePois.js)。另有「野生宠物」一组:不是固定点位,
// 而是附近实时刷出的稀有个体(见 useWildPets.js)。
// 家园小窝不在此列:那层始终开着,不给开关也不占图例(见 useHomeNests.js)。
// 复用宠物列表那套 .filters:桌面常驻左列,移动端为侧滑抽屉(collapsed 控制开合)。
export default function LayerPanel({ pois, wilds, paint, collapsed, onClose, onCollapseSidebar }) {
  const { kinds, poiOn, togglePoi, collectOn, toggleCollect } = pois
  const dualNum = wilds.num.dual || 0
  const dualGone = wilds.numStale.dual || 0
  return (
    <>
      <div className={'filters-backdrop' + (collapsed ? '' : ' show')} onClick={onClose} />
      <aside className={'filters map-filters' + (collapsed ? ' collapsed' : '')}>
        <div className="filters-bar">
          <span className="filters-title">
            <span className="filters-title-ic">🗺️</span>图层
          </span>
          {/* 桌面:收起按钮把侧栏折进地图(地图全宽),右上角 ☰ 可再展开;移动端:✕ 关抽屉 */}
          <button className="icon-btn map-sidebar-collapse" onClick={onCollapseSidebar}
            title="收起图层栏" aria-label="收起图层栏">◀</button>
          <button className="icon-btn map-sidebar-close" onClick={onClose} aria-label="关闭图层">✕</button>
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
          <div className="map-layer-row">
            <button className={'map-layer-btn map-notify-btn' + (wilds.notify ? ' on' : '')}
              onClick={wilds.toggleNotify}
              title="只有能在地图上带环显示(当前开着且命中的图层/奖牌筛选)的稀有宠新出现才提醒;画不出环的不提醒(需允许通知权限;仅地图页打开时有效)">
              <span className="map-notify-ic">🔔</span>
              <span className="map-layer-name">稀有宠出现提醒</span>
              <span className="muted">{wilds.notify ? '开' : '关'}</span>
            </button>
          </div>
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
          {/* 奖牌四件套:整体收在「奖牌筛选」按钮下,点开才见 4 条。滑块是单值阈值,
              默认=奖牌边界,只能往更极端拖(范围见 MEDAL_FILTERS),计数随阈值实时变化。 */}
          <button className="map-medal-toggle" onClick={wilds.toggleOpen} aria-expanded={wilds.open}
            title="大块头/小不点/婉转声/粗嗓门的判定阈值,默认即奖牌边界,可调更严">
            <span>奖牌筛选</span>
            <span className="muted">▾</span>
          </button>
          {wilds.open && MEDAL_FILTERS.map(({ k, n, color, dim, dir, lo, hi, step }) => {
            const num = wilds.num[k] || 0
            const gone = wilds.numStale[k] || 0
            const th = wilds.medals[k]
            const on = wilds.medalOn.has(k)
            return (
              <div className="map-medal-row" key={k}
                title={gone ? `视野内 ${num - gone} · 已离开视野 ${gone}` : undefined}>
                {/* 行首开关:关掉该项后图上不再标这类奖牌,阈值保留但滑块置灰 */}
                <button className={'map-collect-btn map-medal-switch' + (on ? ' on' : '')}
                  onClick={() => wilds.toggleMedal(k)}
                  title={on ? `关闭${n}筛选` : `开启${n}筛选`} aria-label={`${n}筛选开关`}
                  aria-pressed={on}>✓</button>
                <span className="map-wild-swatch" style={{ borderColor: color }} />
                <span className="map-layer-name">{n}</span>
                <span className="map-medal-val">{dir}{th}</span>
                <input type="range" className="map-medal-range" min={lo} max={hi} step={step ?? 0.5}
                  value={th} onChange={(e) => wilds.setThreshold(k, Number(e.target.value))}
                  disabled={!on} aria-label={`${n}判定阈值`} />
                <span className="muted">{num}</span>
              </div>
            )
          })}
          {/* 双牌:奖牌组的「元筛选」。同一只宠同时命中 ≥2 张奖牌判定(体重族+嗓音族各一,
              命中数上限就是 2,见 useWildPets 的 wildShown)才显示;开关图层(异色/炫彩、污染)
              不受影响。双牌开后奖牌段改用双牌阈值判 ≥2 张,单牌阈值不再参与该段。
              双牌有独立的 4 条阈值(默认=单牌当前值=擦边双牌),拖严后双牌比单牌更严但两者
              解耦——子滑块范围 [单牌当前值, 极端值],只严不宽,由 clampDual 保证不比单牌宽。
              开关关时子滑块折叠,计数仍显示(用单牌阈值判 ≥2 张,供参考)。 */}
          <div className="map-medal-row map-medal-dual"
            title={dualGone ? `视野内 ${dualNum - dualGone} · 已离开视野 ${dualGone}` : undefined}>
            <button className={'map-collect-btn map-medal-switch' + (wilds.dual.on ? ' on' : '')}
              onClick={wilds.toggleDual}
              title="只显示同时命中 2 张奖牌判定的宠(如 大块头+婉转声);需至少开启 2 条奖牌筛选"
              aria-label="双牌筛选开关" aria-pressed={wilds.dual.on}>✓</button>
            <span className="map-medal-dual-ic">✧</span>
            <span className="map-layer-name">双牌</span>
            <span className="muted">{dualNum}</span>
          </div>
          {wilds.dual.on && MEDAL_FILTERS.map(({ k, n, color, dim, dir, lo, hi, step }) => {
            // 双牌子滑块:范围 [单牌当前值, 极端值],只严不宽。dir>=': min=单牌值,max=hi;
            // dir<=': min=lo,max=单牌值。单牌值变化时 useWildPets 的 setThreshold 会联动
            // clampDual 把双牌阈值钳到合法范围,这里取 wilds.dual.medals[k] 即可。
            const single = wilds.medals[k]
            const dTh = wilds.dual.medals[k]
            const dMin = dir === '>=' ? single : lo
            const dMax = dir === '>=' ? hi : single
            const on = wilds.medalOn.has(k)
            return (
              <div className="map-medal-row map-medal-dual-sub" key={k}
                title={on ? undefined : `${n}奖牌筛选已关闭,双牌不含此项`}>
                <span className="map-wild-swatch" style={{ borderColor: color }} />
                <span className="map-layer-name">{n}</span>
                <span className="map-medal-val">{dir}{dTh}</span>
                <input type="range" className="map-medal-range map-medal-dual-range" min={dMin} max={dMax}
                  step={step ?? 0.5} value={dTh}
                  onChange={(e) => wilds.setDualThreshold(k, Number(e.target.value))}
                  disabled={!on} aria-label={`双牌${n}判定阈值`} />
              </div>
            )
          })}
        </div>
        <div className="filter-group">
          <label>涂色模式</label>
          <div className="map-layer-row">
            <button className={'map-layer-btn' + (paint.on ? ' on' : '')}
              onClick={paint.toggle} disabled={!paint.available}
              title={paint.available ? '把「刷新过野生精灵」的区域涂色' : '该场景没有底图,无法涂色'}>
              <span className="map-paint-swatch" />
              <span className="map-layer-name">刷新过精灵的区域</span>
            </button>
            {/* 重置只清当前场景/当前层。误点代价不小(要重走一遍),故要确认一次。 */}
            <button className="map-collect-btn on" onClick={() => {
              if (window.confirm('清空本场景已涂的区域?重来一遍要重新走。')) paint.reset()
            }} disabled={!paint.available} title="重置本场景的涂色" aria-label="重置涂色">↺</button>
          </div>
        </div>
        <div className="filters-foot">
          <button className="btn primary" onClick={onClose}>查看地图</button>
        </div>
      </aside>
    </>
  )
}
