import { useState, useCallback, useContext, useEffect } from 'react'
import { MapEngineContext } from '../../context'
import { MapViz } from './useMapEngine.jsx'
import LayerPanel from './LayerPanel'

// 实时地图页:外壳 = 图层抽屉 + MapViz(地图本体,引擎在 useMapEngine)。
// 图层栏所有宽度统一为侧滑抽屉(对齐手机端):右上角 ☰ / 遮罩 / ✕ 控制开合。
//
// 引擎**不在这里创建**:它由 App 层的 MapEngineProvider 常驻并经 MapEngineContext
// 下发——画中画要在离开本页之后继续更新,引擎必须活在本页的生命周期之外。
// (若在这里再调一次 useMapEngine,就会有两份引擎、两套 SSE 订阅与两份图层数据。)
export default function MapPage() {
  const ctx = useContext(MapEngineContext)
  const engine = ctx ? ctx.engine : null
  const pip = ctx ? ctx.pip : null
  const [collapsed, setCollapsed] = useState(true) // 图层抽屉(开合),默认收起=地图全屏

  // 引擎常驻后,交互态(资料卡/野生宠悬浮卡)不会随本页卸载而清掉——
  // 若离开时正弹着卡片,下次进地图页会凭空出现上次的卡片,没有对应的点击动作。
  //
  // 依赖**必须取函数本身而非 engine**:engine 是 useMapEngine 每次渲染返回的新对象,
  // 直接依赖它会让 cleanup 在**每次重渲染**时都跑一遍 —— 而点击标记本身就是一次
  // setState 触发的重渲染,于是刚设好的 wildTip 立刻被 clearUiState 清掉,
  // 表现为「点一下资料卡闪一下就没了」。clearUiState 是 useCallback([]) 的稳定引用,
  // 依赖它才等价于「仅卸载时清理」。
  const clearUiState = engine?.clearUiState
  useEffect(() => () => clearUiState?.(), [clearUiState])

  const toggleLayers = () => setCollapsed((c) => !c)

  // 收集度面板点某分区:把地图移到该区点位中心,并收起抽屉露出地图。
  // 直接改 focusRef 而非走 state——视口中心是每帧消费的 ref(跟随模式下每帧跟着玩家变),
  // 进 state 会让每次平移都重渲染整页;pokeFrame 唤醒可能已停的 RAF 画这一帧
  // (玩家静止时 RAF 会停,不唤醒就看不到移动)。
  const focusZone = useCallback((z) => {
    if (!engine || z.u == null) return
    engine.view.setFollow(false)
    engine.focusRef.current = { u: z.u, v: z.v }
    engine.pokeFrame()
    setCollapsed(true)
  }, [engine])

  // 引擎未就绪(不在 Provider 下):渲染占位,等 Provider 挂上。
  if (!engine) return null

  return (
    <div className="map-page">
      <div className="map-layout">
        <LayerPanel pois={engine.pois} wilds={engine.wilds} paint={engine.paint} routes={engine.routes}
          collapsed={collapsed} onClose={() => setCollapsed(true)} onFocusZone={engine.hasMap ? focusZone : null} />

        <MapViz engine={engine} pip={pip}
          layersActive={!collapsed}
          onToggleLayers={toggleLayers} />
      </div>
    </div>
  )
}
