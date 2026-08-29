import React from 'react'
import { useDropdown } from '../hooks/useDropdown'

// Dropdown 通用自绘下拉,替代原生 <select>:原生浮层是系统样式,与站点深色主题割裂。
// 键鼠/触屏均可操作:点击展开/选条;键盘 ↑↓ 切换、Enter/空格选择、Esc 关闭、Tab 收起,
// 点外部自动收起(交互行为见 hooks/useDropdown.js)。
// options 支持字符串/数字数组或 [{value, label, count}] 对象数组;
// count 存在时在触发按钮与列表项 label 后渲染弱化副文本(如花种槽位下拉的数量)。
export default function Dropdown({ value, options, onChange, placeholder = '全部', className = '', small, disabled, title }) {
  const items = (options || []).map((o) =>
    (typeof o === 'string' || typeof o === 'number') ? { value: o, label: String(o) } : o
  )
  const curIdx = items.findIndex((o) => String(o.value) === String(value))
  const cur = items[curIdx]

  const { open, setOpen, hi, setHi, rootRef, ulRef, onKeyDown, pickAt } = useDropdown({
    count: items.length,
    selectedIndex: curIdx,
    disabled,
    onPick: (i) => { if (String(items[i].value) !== String(value)) onChange(items[i].value) },
  })

  return (
    <div
      className={'dropdown' + (open ? ' open' : '') + (small ? ' small' : '') + (className ? ' ' + className : '')}
      ref={rootRef} onKeyDown={onKeyDown} title={title}
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
        <ul className="dropdown-menu" ref={ulRef} role="listbox">
          {items.map((o, i) => (
            <li
              key={String(o.value)}
              role="option"
              aria-selected={cur && String(cur.value) === String(o.value)}
              className={'dropdown-item' + (cur && String(cur.value) === String(o.value) ? ' cur' : '') + (i === hi ? ' hi' : '')}
              onMouseDown={(e) => { e.preventDefault(); pickAt(i) }}
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
