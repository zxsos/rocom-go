import { useCallback, useEffect, useRef, useState } from 'react'

// useOutsideClick 浮层「点外部即收起」:ref 指向根容器,按下落在它之外时调用 onClose。
// 通用 Dropdown、账号下拉、移动端 tab 分组三处都要这个行为,故抽出来;onClose 需引用稳定。
//
// extraRef 是**可选的第二根容器**:账号的手机端 sheet 用 createPortal 挂到了
// document.body(见 AccountSheet.jsx),它不是 rootRef 的 DOM 后代 —— 若不把它一并纳入
// 包含性判断,点 sheet 里任何地方都会被判成「点外部」而立刻收起。
// 两个 ref 都允许为 null(未挂载时 .current 是 null),按下时现读即可。
export function useOutsideClick(ref, onClose, active = true, extraRef = null) {
  useEffect(() => {
    if (!active) return
    const inside = (node) =>
      (ref.current && ref.current.contains(node)) ||
      (extraRef?.current && extraRef.current.contains(node))
    const onDoc = (e) => { if (!inside(e.target)) onClose() }
    document.addEventListener('mousedown', onDoc)
    return () => document.removeEventListener('mousedown', onDoc)
  }, [ref, extraRef, onClose, active])
}

// useDropdown 自绘下拉的公共行为:开合、键鼠/触屏导航、点外部收起、高亮项滚动到可见。
// 站点不用原生 <select>——它的浮层是系统样式,与深色主题割裂,且 <option> 内不能放 <img>
// (账号下拉要显示头像与在线图标)。此前 Dropdown 与 AccountSelect 各抄了一份这套逻辑,现归一。
//
// 本 hook 只管「行为」,不碰渲染:count 决定键盘循环的范围,选中项/列表项长什么样由调用方决定。
// 返回:
//   open/setOpen        开合状态(触发按钮自行控制 onClick)
//   hi/setHi            高亮项索引;传 -1 表示高亮不在任何可选项上(如悬停在底部操作区)
//   rootRef/ulRef       挂到根容器与 <ul> 上(前者判「点外部」,后者按索引取子元素滚动)
//   onKeyDown           挂到根容器的 onKeyDown
//   pickAt(i)           选中第 i 项并收起
//
// extraRef 可选:透传给 useOutsideClick,供 portal 到别处的浮层(账号的手机端 sheet)
// 一并纳入「点内部」判断。Dropdown / NavBar 不传,行为不变。
export function useDropdown({ count, selectedIndex = -1, disabled = false, onPick, extraRef = null }) {
  const [open, setOpen] = useState(false)
  const [hi, setHi] = useState(0)
  const rootRef = useRef(null)
  const ulRef = useRef(null)

  const close = useCallback(() => setOpen(false), [])
  useOutsideClick(rootRef, close, open, extraRef)

  // 打开时把高亮重置为当前选中项,方便直接 ↑↓ 移动。
  // selectedIndex 走 ref:只作为「打开那一刻」的初值,不该让它在变化时重置用户的高亮。
  const selRef = useRef(selectedIndex)
  selRef.current = selectedIndex
  useEffect(() => {
    if (open) setHi(selRef.current < 0 ? 0 : selRef.current)
  }, [open])

  // 高亮项变化时滚动到可见(键盘 ↑↓ 时不至于飘出可见区)
  useEffect(() => {
    if (!open || !ulRef.current) return
    const el = ulRef.current.children[hi]
    if (el) el.scrollIntoView({ block: 'nearest' })
  }, [hi, open])

  const pickAt = useCallback((i) => {
    setOpen(false)
    onPick(i)
  }, [onPick])

  const onKeyDown = (e) => {
    if (disabled) return
    if (!open) {
      if (e.key === 'ArrowDown' || e.key === 'Enter' || e.key === ' ') {
        e.preventDefault()
        setOpen(true)
      }
      return
    }
    if (!count) return
    switch (e.key) {
      case 'ArrowDown':
        e.preventDefault()
        setHi((i) => (i + 1) % count)
        break
      case 'ArrowUp':
        e.preventDefault()
        setHi((i) => (i - 1 + count) % count)
        break
      case 'Enter':
      case ' ':
        e.preventDefault()
        if (hi >= 0 && hi < count) pickAt(hi)
        break
      case 'Escape':
        e.preventDefault()
        setOpen(false)
        break
      case 'Tab':
        setOpen(false) // 键盘用户 Tab 到下个控件时收起,免得浮层挡住后面的元素
        break
    }
  }

  return { open, setOpen, hi, setHi, rootRef, ulRef, onKeyDown, pickAt }
}
