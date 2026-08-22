import { useState, useEffect } from 'react'
import FloatingMap from './FloatingMap'
import { subscribeFloat } from '../pages/map/floatState'

// FloatingHost 全局浮窗挂载点。订阅 floatState:open 时渲染 FloatingMap。
// 不走路由,切页面不卸载,确保浮窗持续运行。
// 由 App.jsx 在 AccountContext/IconsContext 内部渲染,故能拿到上下文。
export default function FloatingHost() {
  const [open, setOpen] = useState(false)
  useEffect(() => subscribeFloat((s) => setOpen(s.open)), [])
  if (!open) return null
  return <FloatingMap />
}
