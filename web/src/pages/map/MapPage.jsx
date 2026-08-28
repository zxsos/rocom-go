import { useState, useContext } from 'react'
import { AccountContext } from '../../context'
import { useMapEngine, MapViz } from './useMapEngine.jsx'
import LayerPanel from './LayerPanel'

// 实时地图页:外壳 = 图层抽屉 + MapViz(地图本体,引擎在 useMapEngine)。
// 图层栏所有宽度统一为侧滑抽屉(对齐手机端):右上角 ☰ / 遮罩 / ✕ 控制开合。
export default function MapPage() {
  const account = useContext(AccountContext)
  const engine = useMapEngine(account)
  const [collapsed, setCollapsed] = useState(true) // 图层抽屉(开合),默认收起=地图全屏

  const toggleLayers = () => setCollapsed((c) => !c)

  return (
    <div className="map-page">
      <div className="map-layout">
        <LayerPanel pois={engine.pois} wilds={engine.wilds} paint={engine.paint} collapsed={collapsed}
          onClose={() => setCollapsed(true)} />

        <MapViz engine={engine}
          layersActive={!collapsed}
          onToggleLayers={toggleLayers} />
      </div>
    </div>
  )
}
