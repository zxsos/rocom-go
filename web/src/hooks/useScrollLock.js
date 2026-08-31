import { useEffect } from 'react'

// useScrollLock 浮层打开期间锁住页面滚动,关闭后还原。
//
// 锁打在 documentElement(<html>)而不是 body:styles/base.css 里
// html, body, #root 都是 height:100%,滚动条实际挂在视口(html)上 ——
// 只锁 body 拦不住滚动。
//
// 必须**保存并还原原值**而不是写回空串:清空会覆盖别处设置的 overflow
// (devtools 调试、其它逻辑),且再也恢复不了。还原写在 effect cleanup 里,
// 故组件卸载时同样会还原,不会留下一个滚不动的页面。
export function useScrollLock(active) {
  useEffect(() => {
    if (!active) return
    const el = document.documentElement
    const prev = el.style.overflow
    el.style.overflow = 'hidden'
    return () => { el.style.overflow = prev }
  }, [active])
}
