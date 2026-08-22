import { useState, useContext } from 'react'
import { AccountContext } from '../../context'
import { useMapEngine, MapViz } from './useMapEngine.jsx'
import LayerPanel from './LayerPanel'

// 实时地图页:外壳 = 图层侧栏 + MapViz(地图本体,引擎在 useMapEngine)。
// 右上角 ☰ 控制图层侧栏(桌面可折叠成地图全宽);左下角 ☰ 是移动端图层抽屉入口(在 MapViz 内)。
export default function MapPage() {
  const account = useContext(AccountContext)
  const engine = useMapEngine(account)
  const [collapsed, setCollapsed] = useState(true)        // 移动端图层抽屉(开合)
  const [sidebarOpen, setSidebarOpen] = useState(true)   // 桌面图层侧栏(可折叠,折叠后地图全宽)

  // 右上角 ☰:窄屏开/关图层抽屉,桌面折叠/展开图层侧栏(折叠后地图全宽)。
  const toggleLayers = () => {
    if (window.matchMedia('(max-width: 760px)').matches) setCollapsed((c) => !c)
    else setSidebarOpen((o) => !o)
  }

  return (
    <div className="map-page">
      <div className={'map-layout' + (sidebarOpen ? '' : ' closed')}>
        <LayerPanel pois={engine.pois} wilds={engine.wilds} paint={engine.paint} collapsed={collapsed}
          onClose={() => setCollapsed(true)} onCollapseSidebar={() => setSidebarOpen(false)} />

        <MapViz engine={engine}
          sidebarOpen={sidebarOpen}
          onToggleLayers={toggleLayers} />
      </div>
    </div>
  )
}
