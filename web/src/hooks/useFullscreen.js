import { useEffect, useState } from 'react'

const fullEl = () => document.fullscreenElement || document.webkitFullscreenElement

// useFullscreen 网页全屏(Fullscreen API,带 webkit 前缀兼容):对整页文档切换。
// isFull 初值取当前实际全屏状态,并监听状态变化(含浏览器 UI/ESC 退出)保持同步;
// supported=false 的环境(如 iOS Safari 部分场景)应隐藏入口。
export function useFullscreen() {
  const [isFull, setIsFull] = useState(() => !!fullEl())
  useEffect(() => {
    const onFs = () => setIsFull(!!fullEl())
    document.addEventListener('fullscreenchange', onFs)
    document.addEventListener('webkitfullscreenchange', onFs)
    return () => {
      document.removeEventListener('fullscreenchange', onFs)
      document.removeEventListener('webkitfullscreenchange', onFs)
    }
  }, [])
  const toggle = () => {
    const el = document.documentElement
    if (fullEl()) {
      const exit = document.exitFullscreen || document.webkitExitFullscreen
      exit.call(document)?.catch(() => {})
    } else {
      const req = el.requestFullscreen || el.webkitRequestFullscreen
      req.call(el)?.catch(() => {})
    }
  }
  const supported = !!(document.documentElement.requestFullscreen || document.documentElement.webkitRequestFullscreen)
  return { isFull, toggle, supported }
}
