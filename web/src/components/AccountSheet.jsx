import { useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { useScrollLock } from '../hooks/useScrollLock'
import { IconClose } from './svg'

// 退出动画时长,与 styles/shell.css 里 .account-sheet.closing 的 animation 时长一致。
// 改一处必须同步另一处:短了动画被截断、长了关闭后要空等才消失。
const EXIT_MS = 180

// AccountSheet 手机端账号底部抽屉。
//
// **必须用 createPortal 挂到 document.body**,不是「更干净」而是「不这么做就是错的」:
// .topbar 有 backdrop-filter: blur(10px)(见 styles/shell.css),它既创建层叠上下文、
// 又成为 position: fixed 后代的**包含块** —— sheet 若留在顶栏的 DOM 子树里,
// 会相对顶栏那一条定位而不是相对视口,直接错乱。
//
// 键盘无需额外处理:createPortal 只挪 DOM,React 事件仍沿 React 树冒泡到
// .account-wrap 的 onKeyDown,故 useDropdown 的 ↑↓/Enter/Esc/Tab 天然覆盖本 sheet。
//
// open 由父级(useDropdown)控制;本组件额外维护 mounted/closing 两个状态,
// 只为「关闭时先播完退出动画再卸载」—— {open && ...} 会在 open 变假的同帧卸载,
// 没有留给动画的时间窗口。
export default function AccountSheet({ open, onClose, sheetRef, title, children }) {
  const [mounted, setMounted] = useState(open)
  const [closing, setClosing] = useState(false)

  useEffect(() => {
    if (open) {
      setMounted(true)
      setClosing(false)
      return
    }
    if (!mounted) return
    setClosing(true)
    // reduced-motion 下 CSS 把动画压到 .01ms(见 shell.css 同名媒体查询),
    // 卸载窗口也跟着归零 —— 否则关闭后要白等 180ms 才消失,像是「点了没反应」。
    const ms = window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 0 : EXIT_MS
    const t = setTimeout(() => { setMounted(false); setClosing(false) }, ms)
    // cleanup 清掉定时器:快速开合时残留的定时器会把刚开的新 sheet 再关一次
    return () => clearTimeout(t)
  }, [open, mounted])

  const panelRef = useRef(null)
  useScrollLock(mounted)

  if (!mounted) return null

  return createPortal(
    <div className="account-sheet-root" ref={sheetRef}>
      <div className={'account-scrim' + (closing ? ' closing' : '')} onClick={onClose} />
      <div
        className={'account-sheet' + (closing ? ' closing' : '')}
        ref={panelRef}
        role="dialog"
        aria-modal="true"
        aria-label={title}
      >
        {/* 抓手条:纯装饰,不可点 —— 只提示「这是个可关的浮层」 */}
        <div className="account-sheet-grip" aria-hidden="true" />
        <div className="account-sheet-head">
          <span className="account-sheet-title">{title}</span>
          <button type="button" className="icon-btn account-sheet-close" onClick={onClose} title="关闭" aria-label="关闭">
            <IconClose size={16} />
          </button>
        </div>
        {children}
      </div>
    </div>,
    document.body,
  )
}
