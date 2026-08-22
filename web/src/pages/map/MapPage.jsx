import { useState, useEffect, useContext, useCallback } from 'react'
import { AccountContext } from '../../context'
import { useMapEngine, MapViz } from './useMapEngine.jsx'
import LayerPanel from './LayerPanel'
import { setFloatState, subscribeFloat, getFloatState } from './floatState'

// 实时地图页:外壳 = 图层侧栏 + MapViz(地图本体,引擎在 useMapEngine)。
// 浮窗独占:订阅 floatState,浮窗开着时主页面渲染占位提示(省一份 RAF),浮窗关后自动恢复。
// 右上角 ☰ 控制图层侧栏(桌面可折叠成地图全宽);左下角 ☰ 是移动端图层抽屉入口(在 MapViz 内)。
export default function MapPage() {
  const account = useContext(AccountContext)
  const engine = useMapEngine(account)
  const [collapsed, setCollapsed] = useState(true)        // 移动端图层抽屉(开合)
  const [sidebarOpen, setSidebarOpen] = useState(true)   // 桌面图层侧栏(可折叠,折叠后地图全宽)
  const [floatOpen, setFloatOpen] = useState(() => getFloatState().open)

  // 订阅浮窗状态:开浮窗 → 占位;关浮窗 → 恢复地图。
  useEffect(() => subscribeFloat((s) => setFloatOpen(s.open)), [])

  const openFloat = useCallback(() => {
    setFloatState({ open: true, mode: null }) // mode 由 FloatingMap 运行时决定
  }, [])

  // 右上角 ☰:窄屏开/关图层抽屉,桌面折叠/展开图层侧栏(折叠后地图全宽)。
  const toggleLayers = () => {
    if (window.matchMedia('(max-width: 760px)').matches) setCollapsed((c) => !c)
    else setSidebarOpen((o) => !o)
  }

  // 浮窗独占占位:不渲染 MapViz(省 RAF/SSE 订阅),只留提示与关闭浮窗按钮。
  if (floatOpen) {
    return (
      <div className="map-page map-page-float-host">
        <div className="map-float-host-card">
          <div className="map-float-host-ic">🗺️</div>
          <div className="map-float-host-text">地图已开浮窗</div>
          <div className="muted">浮窗已悬浮在屏幕上(桌面可拖出浏览器,手机可进系统画中画)</div>
          <button className="btn primary" onClick={() => setFloatState({ open: false, mode: null })}>
            关闭浮窗
          </button>
        </div>
      </div>
    )
  }

  return (
    <div className="map-page">
      <div className={'map-layout' + (sidebarOpen ? '' : ' closed')}>
        <LayerPanel pois={engine.pois} wilds={engine.wilds} paint={engine.paint} collapsed={collapsed}
          onClose={() => setCollapsed(true)} onCollapseSidebar={() => setSidebarOpen(false)} />

        <MapViz engine={engine}
          sidebarOpen={sidebarOpen}
          onToggleLayers={toggleLayers}
          onOpenFloat={openFloat}
          floatMode={false} />
      </div>
    </div>
  )
}
