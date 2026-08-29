import React, { useEffect, useRef, useState } from 'react'

// Dropdown 通用自绘下拉,替代原生 <select>:原生浮层是系统样式,与站点深色主题割裂,
// 故沿用顶栏账号下拉(见 App.jsx AccountSelect)的 button + ul 浮层方案。
// 键鼠/触屏均可操作:点击展开/选条;键盘 ↑↓ 切换、Enter/空格选择、Esc 关闭、Tab 收起,
// 点外部自动收起。options 支持字符串/数字数组或 [{value, label, count}] 对象数组;
// count 存在时在触发按钮与列表项 label 后渲染弱化副文本(如花种槽位下拉的数量)。
export default function Dropdown({ value, options, onChange, placeholder = '全部', className = '', small, disabled, title }) {
  const [open, setOpen] = useState(false)
  const [hi, setHi] = useState(0) // 高亮项索引(键盘 ↑↓ 移动)
  const rootRef = useRef(null)
  const listRef = useRef(null)

  const items = (options || []).map((o) =>
    (typeof o === 'string' || typeof o === 'number') ? { value: o, label: String(o) } : o
  )

  // 点外部关闭
  useEffect(() => {
    if (!open) return
    const onDoc = (e) => { if (rootRef.current && !rootRef.current.contains(e.target)) setOpen(false) }
    document.addEventListener('mousedown', onDoc)
    return () => document.removeEventListener('mousedown', onDoc)
  }, [open])

  // 打开时把高亮重置为当前选中项,方便直接 ↑↓ 移动
  useEffect(() => {
    if (open) {
      const idx = items.findIndex((o) => String(o.value) === String(value))
      setHi(idx < 0 ? 0 : idx)
    }
  }, [open]) // eslint-disable-line react-hooks/exhaustive-deps

  // 高亮项变化时滚动到可见
  useEffect(() => {
    if (!open || !listRef.current) return
    const el = listRef.current.children[hi]
    if (el) el.scrollIntoView({ block: 'nearest' })
  }, [hi, open])

  const cur = items.find((o) => String(o.value) === String(value))
  const choose = (o) => {
    setOpen(false)
    if (String(o.value) !== String(value)) onChange(o.value)
  }

  const onKey = (e) => {
    if (disabled) return
    if (!open) {
      if (e.key === 'ArrowDown' || e.key === 'Enter' || e.key === ' ') {
        e.preventDefault()
        setOpen(true)
      }
      return
    }
    if (!items.length) return
    switch (e.key) {
      case 'ArrowDown':
        e.preventDefault()
        setHi((i) => (i + 1) % items.length)
        break
      case 'ArrowUp':
        e.preventDefault()
        setHi((i) => (i - 1 + items.length) % items.length)
        break
      case 'Enter':
      case ' ':
        e.preventDefault()
        if (items[hi]) choose(items[hi])
        break
      case 'Escape':
        e.preventDefault()
        setOpen(false)
        break
      case 'Tab':
        setOpen(false)
        break
    }
  }

  return (
    <div
      className={'dropdown' + (open ? ' open' : '') + (small ? ' small' : '') + (className ? ' ' + className : '')}
      ref={rootRef} onKeyDown={onKey} title={title}
    >
      <button
        type="button"
        className="dropdown-trigger"
        onClick={() => !disabled && setOpen((o) => !o)}
        disabled={disabled}
        aria-haspopup="listbox"
        aria-expanded={open}
      >
        <span className="dropdown-value">{cur ? cur.label : placeholder}</span>
        {cur && cur.count !== undefined && <span className="dropdown-count">{cur.count}</span>}
        <span className="dropdown-caret">▾</span>
      </button>
      {open && (
        <ul className="dropdown-menu" ref={listRef} role="listbox">
          {items.map((o, i) => (
            <li
              key={String(o.value)}
              role="option"
              aria-selected={cur && String(cur.value) === String(o.value)}
              className={'dropdown-item' + (cur && String(cur.value) === String(o.value) ? ' cur' : '') + (i === hi ? ' hi' : '')}
              onMouseDown={(e) => { e.preventDefault(); choose(o) }}
              onMouseEnter={() => setHi(i)}
            >
              <span className="dropdown-item-text">{o.label}</span>
              {o.count !== undefined && <span className="dropdown-item-count">{o.count}</span>}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
