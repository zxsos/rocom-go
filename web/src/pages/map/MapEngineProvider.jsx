import { useEffect, useContext } from 'react'
import { MapEngineContext } from '../../context'
import { useMapEngine } from './useMapEngine'
import { usePip } from './usePip'

// MapEngineProvider 持有常驻的地图引擎与画中画控制器。
//
// 为什么引擎要单独放在一个 Provider 组件里,而不是直接写在 App 里:
// useMapEngine 内部有一堆 state(野生宠列表、家园小窝、涂地、POI…),它们的更新
// 会**触发宿主组件重渲染**。若写在 App 里,每来一批野生宠数据就把整个应用
// (宠物列表、孵蛋页、顶栏…)全部重渲染一遍;放进这个独立组件后,只有它自己
// 重渲染,children 传进来的元素引用没变,React 会跳过整棵子树——只有真正
// 消费 MapEngineContext 的 MapPage 会重新渲染。
//
// 引擎常驻的意义:画中画要在离开地图页之后继续更新(切页、切出浏览器),
// 故不能挂在 MapPage 上随路由卸载。
export default function MapEngineProvider({ account, theme, icons, children }) {
  const engine = useMapEngine(account)
  // theme 传给 usePip:canvas 拿不到 CSS 变量,需按主题重读一次配色。
  const pip = usePip(engine, theme)

  // 异色/炫彩角标图标:App 拉到后注入绘制器(野生宠标记要把它们画在小窗里)。
  const setIcons = pip.setIcons
  useEffect(() => { setIcons(icons) }, [icons, setIcons])

  // value 每次渲染都是新对象:消费方(MapPage)需要因此拿到最新的引擎字段。
  // 不会波及其它页面——它们不是 context 消费者,且 children 引用未变会被跳过。
  return (
    <>
      <MapEngineContext.Provider value={{ engine, pip }}>
        {children}
      </MapEngineContext.Provider>
      {/* 画中画的 video 挂载点:不能塞进地图页——.map-vp 建了自己的层叠上下文,
          被它困住的 video 进 PiP 后会被裁;也不能 display:none,部分浏览器不解码
          隐藏元素,PiP 出来是黑屏。 */}
      <div className="pip-video-host" ref={pip.hostRef} />
    </>
  )
}

// useMapEngineCtx 取常驻引擎。未挂在 Provider 下返回 null(MapPage 会自行判空),
// 而不是抛错——调试页等孤立路由未必在 Provider 内。
export function useMapEngineCtx() {
  return useContext(MapEngineContext)
}
