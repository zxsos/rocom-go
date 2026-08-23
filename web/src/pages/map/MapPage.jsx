import { useState, useContext } from 'react'
import { AccountContext } from '../../context'
import { useMapEngine, MapViz } from './useMapEngine.jsx'
import LayerPanel from './LayerPanel'

// 实时地图页:外壳 = 图层侧栏 + MapViz(地图本体,引擎在 useMapEngine)。
// 右上角控制组里的 ☰ 控制图层侧栏:桌面可折叠成地图全宽;移动端开/关图层抽屉。
export default function MapPage() {
  const account = useContext(AccountContext)
  const engine = useMapEngine(account)
  const [collapsed, setCollapsed] = useState(true)        // 移动端图层抽屉(开合)
  const [sidebarOpen, setSidebarOpen] = useState(true)   // 桌面图层侧栏(可折叠,折叠后地图全宽)

  // 点击时实时判移动端断点(响应窗口尺寸变化):窄屏开/关图层抽屉,桌面折叠/展开侧栏。
  const toggleLayers = () => {
    if (typeof window !== 'undefined' && window.matchMedia('(max-width: 760px)').matches)
      setCollapsed((c) => !c)
    else
      setSidebarOpen((o) => !o)
  }

  // ☰ 按钮的 on 态:窄屏看抽屉是否展开(!collapsed),桌面看侧栏是否展开(sidebarOpen)。
  const mobile = typeof window !== 'undefined' && window.matchMedia('(max-width: 760px)').matches
  const layersActive = mobile ? !collapsed : sidebarOpen

  return (
    <div className="map-page">
      <div className={'map-layout' + (sidebarOpen ? '' : ' closed')}>
        <LayerPanel pois={engine.pois} wilds={engine.wilds} paint={engine.paint} collapsed={collapsed}
          onClose={() => setCollapsed(true)} onCollapseSidebar={() => setSidebarOpen(false)} />

        <MapViz engine={engine}
          layersActive={layersActive}
          onToggleLayers={toggleLayers} />
      </div>
    </div>
  )
}
