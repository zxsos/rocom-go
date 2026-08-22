// 浮窗全局状态:开/关 + 当前模式(pip|web)+ 订阅者通知。
// 不入 React state(避免 App 重渲染),用简单发布订阅;主页面 MapPage 订阅以切占位态。
// 浮窗本身由 FloatingMapPortal 在 main.jsx 末尾挂载,不走路由,切路由不丢。

const listeners = new Set()
let state = { open: false, mode: null } // mode: 'pip' | 'web' | null

export function getFloatState() { return { ...state } }

export function setFloatState(next) {
  state = { ...state, ...next }
  for (const fn of [...listeners]) {
    try { fn(state) } catch { /* 单个出错不牵连 */ }
  }
}

// 订阅浮窗状态变化,返回取消函数。
export function subscribeFloat(fn) {
  listeners.add(fn)
  return () => listeners.delete(fn)
}

// 浏览器原生 documentPictureInPicture 支持检测(桌面 Chrome/Edge 有,Safari/Firefox 无)。
export const pipSupported = typeof window !== 'undefined' && 'documentPictureInPicture' in window
