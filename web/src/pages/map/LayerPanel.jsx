import React from 'react'
import { imgURL } from '../../components/icons'
import { confirmDialog } from '../../components/confirm'
import { WILD_LAYERS } from './wildConfig'
import RangeRules from '../../components/RangeRules'
import ZonePanel from './ZonePanel'

// LayerPanel 图层侧栏:POI 图层开关;可收集图层(眠枭之星/不咕钟零件)行右侧另有收集模式小开关
// (开 = 隐藏该图层已收集的点,判定来源见 usePois.js)。另有「野生宠物」一组:不是固定点位,
// 而是附近实时刷出的稀有个体(见 useWildPets.js)。
// 家园小窝不在此列:那层始终开着,不给开关也不占图例(见 useHomeNests.js)。
// 跑图路线组(useRoutes.js):B站泽口博士的收集路线,仅卡洛西亚大陆(10003)有数据;
// 点开才见路线列表,每条可单独开关叠加,选择存 localStorage。
// 复用宠物列表那套 .filters:所有宽度统一为侧滑抽屉(collapsed 控制开合,桌面也对齐手机端)。
export default function LayerPanel({ pois, wilds, paint, routes, collapsed, onClose, onFocusZone }) {
  const { kinds, poiOn, togglePoi, collectOn, toggleCollect, zoneStats } = pois
  const dualNum = wilds.num.dual || 0
  const dualGone = wilds.numStale.dual || 0
  return (
    <>
      <div className={'map-filters-backdrop' + (collapsed ? '' : ' show')} onClick={onClose} />
      <aside className={'filters map-filters' + (collapsed ? ' collapsed' : '')}>
        <div className="filters-bar">
          <span className="filters-title">
            <span className="filters-title-ic">⚙️</span>设置
          </span>
          {/* ✕ 关抽屉;右上角 ☰ 再打开 */}
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
        {/* 区域收集度:数据同源于上面的图层(服务器分区进度),但视角不同——
            图层是「图上显示什么」,这里是「还差多少、差在哪」,点一行把地图移过去。
            放在图标图层下面,同属大地图静态收集物一类。 */}
        <ZonePanel stats={zoneStats} onFocus={onFocusZone} disabled={!onFocusZone} />
        <div className="filter-group">
          {/* 清空按钮放进 label 内:复用 .filter-group > label 的既有 flex 布局
              (左侧装饰竖条靠 ::before),不必另加包裹层与配套样式。
              这是唯一会真正删除野生宠标记的入口 —— 换场景/传送都只置灰
              (见 pipeline.resetWilds),系统不自动抹掉,清不清由用户决定。 */}
          <label>
            野生宠物
            <button className="map-collect-btn map-wild-clear" onClick={() => {
              confirmDialog({
                message: '清空全部野生宠物标记?(含已置灰的「最后所见」)',
                okText: '清空', danger: true,
              }).then((ok) => ok && wilds.clear())
            }} title="清空地图上的野生宠物标记(含灰点)" aria-label="清空野生宠标记">🗑</button>
          </label>
          <div className="map-layer-row">
            <button className={'map-layer-btn map-notify-btn' + (wilds.notify ? ' on' : '')}
              onClick={wilds.toggleNotify}
              title="只有能在地图上带环显示(当前开着且命中的图层/奖牌筛选)的稀有宠新出现才提醒;画不出环的不提醒(需允许通知权限;仅地图页打开时有效)">
              <span className="map-notify-ic">🔔</span>
              <span className="map-layer-name">稀有宠出现提醒</span>
              <span className="muted">{wilds.notify ? '开' : '关'}</span>
            </button>
          </div>
          {/* 仅双牌:勾选后只有双牌(同时命中≥2张奖牌)的新出现稀有宠才响提醒,
              单牌/污染等不响;异色/炫彩属最高优先级,勾选后仍照常提醒。只在提醒开着时可用。 */}
          <div className="map-layer-row map-notify-dual-row">
            <button className={'map-layer-btn map-notify-dual-btn' + (wilds.notifyDualOnly ? ' on' : '')}
              onClick={wilds.toggleNotifyDualOnly} disabled={!wilds.notify}
              title="勾选后只有双牌(同时命中≥2张奖牌)的新出现稀有宠才响提醒,其余不响;异色/炫彩为最高优先级,不受此限">
              <span className="map-collect-ic">{wilds.notifyDualOnly ? '✓' : ''}</span>
              <span className="map-layer-name">仅双牌</span>
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
          {/* 体重/声音:区间规则,**与事件页共用同一套**(见 utils/rules.js)。
              规则编辑器就是 RangeRules 组件,两个页面长得一样、改哪边都一样。
              整体收在折叠按钮下:侧栏地方有限,而这几条多数时候配好就不动了。 */}
          <button className="map-medal-toggle" onClick={wilds.toggleOpen} aria-expanded={wilds.open}
            title="体重百分位 / 嗓音原值的命中区间,自己定范围;与事件页共用同一套规则">
            <span>体重 / 声音</span>
            <span className="muted">▾</span>
          </button>
          {wilds.open && (
            <RangeRules
              rules={wilds.rangeRules}
              setRules={wilds.setRangeRules}
              counts={wilds.ruleNum}
            />
          )}
          {/* 双牌:规则组的「元筛选」—— 命中 ≥2 条规则才显示。
              规则是用户自己配的,故「双牌」即命中其中任意两条,不再限定体重族+嗓音族各一
              (那正是旧版表达不了的:现在可以配 5 条体重区间,任意两条组合都算双牌)。
              开关图层(异色/炫彩、污染)不受影响。 */}
          <div className="map-medal-row map-medal-dual"
            title={dualGone ? `视野内 ${dualNum - dualGone} · 已离开视野 ${dualGone}` : undefined}>
            <button className={'map-collect-btn map-medal-switch' + (wilds.dual ? ' on' : '')}
              onClick={wilds.toggleDual}
              title="只显示同时命中 2 条规则的宠(如 大块头+婉转声);需至少启用 2 条规则"
              aria-label="双牌筛选开关" aria-pressed={wilds.dual}>✓</button>
            <span className="map-medal-dual-ic">✧</span>
            <span className="map-layer-name">双牌</span>
            <span className="muted">{dualNum}</span>
          </div>
        </div>
        {routes.kinds.length > 0 && (
          <div className="filter-group">
            <label>跑图路线</label>
            {/* 收起时也能看出开了几条;点击展开/收起路线列表。 */}
            <div className="map-routes-head">
              <button className="map-medal-toggle" onClick={routes.toggleOpen} aria-expanded={routes.open}
                title="B站泽口博士的收集路线(1~20 号收集片区/精灵球/冲刺),可叠加多条">
                <span>收集路线</span>
                <span className="map-route-count">{routes.marks.length}/{routes.kinds.length}<i className="muted">▾</i></span>
              </button>
              <span className="map-routes-all">
                <button onClick={() => routes.setAll(true)} title="一键开启所有路线" aria-label="全开">全开</button>
                <button onClick={() => routes.setAll(false)} title="一键关闭所有路线" aria-label="全关">全关</button>
              </span>
            </div>
            {routes.open && (
              <div className="map-route-follow">
                <button className={'map-collect-btn' + (routes.follow ? ' on' : '')}
                  onClick={routes.toggleFollow} aria-pressed={routes.follow} aria-label="跟走模式开关"
                  title="开启后走到点位附近,该点之前的线自动隐藏,只留剩余路线和下一目标">✓</button>
                <span className="map-layer-name">跟走模式</span>
                <span className="muted">{routes.follow ? '到点即隐藏' : '显示全部'}</span>
                <button className="map-collect-btn" onClick={() => {
                  confirmDialog({ message: '重置所有路线的跟走进度?', okText: '重置', danger: true })
                    .then((ok) => ok && routes.resetProgress())
                }} title="重置跟走进度" aria-label="重置进度">↺</button>
              </div>
            )}
            {routes.open && routes.follow && (
              <div className="map-route-range">
                <span className="map-layer-name">判定范围</span>
                <input type="range" min={10} max={50} step={5} value={routes.nearM}
                  onChange={(e) => routes.setNearM(Number(e.target.value))}
                  title="走到目标点该距离内即判定到达,隐藏已走线路" aria-label="到达判定半径" />
                <span className="muted">{routes.nearM}m</span>
              </div>
            )}
            {routes.open && routes.kinds.map((r) => (
              <div className="map-route-row" key={r.name}
                title={r.short}>
                <button className={'map-collect-btn' + (r.on ? ' on' : '')}
                  onClick={() => routes.toggle(r.name)}
                  aria-label={`${r.short}开关`} aria-pressed={r.on}>✓</button>
                <button className="map-route-swatch" style={{ background: r.color }}
                  onClick={() => routes.cycleColor(r.name)}
                  title="点击换色:按这条路线实际经过的地形,挑一个与背景/其它路线对比明显的颜色" aria-label={`${r.short}换色`} />
                <span className="map-layer-name">{r.short}</span>
                <span className="muted">{routes.follow && r.progress >= 0 ? `${r.progress + 1}/${r.count}` : r.count}</span>
              </div>
            ))}
          </div>
        )}
        <div className="filter-group">
          <label>涂色模式</label>
          <div className="map-layer-row">
            <button className={'map-layer-btn' + (paint.on ? ' on' : '')}
              onClick={paint.toggle} disabled={!paint.available}
              title={paint.available ? '把「刷新过野生精灵」的区域涂色' : '该场景没有底图,无法涂色'}>
              <span className="map-paint-swatch" />
              <span className="map-layer-name">刷新过精灵的区域</span>
            </button>
            {/* 重置只清当前场景/当前层。误点代价不小(要重走一遍),故要确认一次。
                用 confirmDialog 而非 window.confirm:样式跟主题、移动端可用(原生弹窗在
                全屏/PWA 下会打断沉浸模式)。 */}
            <button className="map-collect-btn on" onClick={async () => {
              if (await confirmDialog({
                message: '清空本场景已涂的区域?重来一遍要重新走。',
                okText: '清空', danger: true,
              })) paint.reset()
            }} disabled={!paint.available} title="重置本场景的涂色" aria-label="重置涂色">↺</button>
          </div>
        </div>
      </aside>
    </>
  )
}
