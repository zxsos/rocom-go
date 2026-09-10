import React, { useEffect, useMemo, useRef, useState } from 'react'
import { useDropdown } from '../hooks/useDropdown'
import { imgURL } from './icons'
import { IconChevronDown } from './svg'

// ComboSelect 可输入的组合下拉:触发区**本身就是输入框**,边打字边弹候选。
//
// 与 Dropdown 的分工:选项只有几档(状态、排序、页大小)时用 Dropdown —— 点开、挑一个就完了;
// 选项多到要「找」时用这个 —— 宠物库里的品种有几百个,滚着找一个名字基本等于不能用,
// 而玩家心里多半已经有几个字(「火神」「独角兽」)。
//
// 与 PetPicker 的分工:那个的搜索框在**菜单里**,触发按钮仍然只显示「当前选中」;这里触发区
// 就是搜索框,少一次「先点开、再打字」。宠物那种候选(带头像、同名个体极多)仍该用 PetPicker。
//
// 只认候选里的值:text 与 value 分开存,收起时若输入的文字匹配不到任何候选就回填成当前值 ——
// 留在框里的自由文字会让人以为「填上了」,而这个值要跟别的数据对得上(品种名对不上破壳记录)。
//
// 选项可以带 img(可选,如培育页的品种给的是**蛋图**):名字分不出两条长得一样的候选时,
// 图是唯一能确认选中的是哪一条的东西。放在候选行左侧(复用 PetPicker 的 .dropdown-av);
// 触发区是输入框、放不下图,故触发区只显示文字,图只在菜单里 —— 这一点与 PetPicker 不同。
export default function ComboSelect({
  value, options, onChange, placeholder = '输入关键字搜索…', disabled, className = '', title, emptyText = '候选',
}) {
  const items = useMemo(
    () => (options || []).map((o) => ((typeof o === 'string' || typeof o === 'number') ? { value: o, label: String(o) } : o)),
    [options],
  )
  const cur = items.find((o) => String(o.value) === String(value == null ? '' : value))
  const [q, setQ] = useState('')
  const inputRef = useRef(null)

  // 关键字过滤放在本地:候选一次就给全了(几百条),O(n) 的字符串比较没有节流与竞态的麻烦。
  // 前缀命中的排前面(搜「火神」时,「火神」不该被「火神兽」挤到后面去),其余按原序。
  const kw = q.trim().toLowerCase()
  const shown = useMemo(() => {
    if (!kw) return items
    const starts = []
    const rest = []
    for (const o of items) {
      const s = String(o.label).toLowerCase()
      if (s.startsWith(kw)) starts.push(o)
      else if (s.includes(kw)) rest.push(o)
    }
    return [...starts, ...rest]
  }, [items, kw])

  const curIdx = shown.findIndex((o) => String(o.value) === String(value == null ? '' : value))

  const { open, setOpen, up, hi, setHi, rootRef, ulRef, onKeyDown, pickAt } = useDropdown({
    count: shown.length,
    selectedIndex: curIdx,
    disabled,
    searchRef: inputRef, // 焦点在这个输入框里:字符/退格交给它自己,只接管导航与选中
    onPick: (i) => {
      const o = shown[i]
      if (o) onChange(o.value)
    },
  })

  // 收起时把输入框回填成当前值的 label。**不入 state 存「编辑中」标记**:编辑态就是
  // 「菜单开着」,两者分开会出现「菜单关了但框里还留着我打了一半的字」。
  // 依赖 cur 而非 value:重拉回来的线把品种改成了别的品种(比如另一台设备改的)时也要跟上。
  useEffect(() => {
    if (!open) setQ(cur ? cur.label : '')
  }, [open, cur])

  return (
    <div
      className={'dropdown combo' + (open ? ' open' : '') + (up ? ' up' : '') + (className ? ' ' + className : '')}
      ref={rootRef} onKeyDown={onKeyDown} title={title}
    >
      <div className="combo-box">
        <input
          ref={inputRef}
          className="combo-input"
          type="text"
          role="combobox"
          autoComplete="off"
          value={q}
          placeholder={placeholder}
          disabled={disabled}
          aria-expanded={open}
          aria-haspopup="listbox"
          onFocus={() => { if (!disabled) setOpen(true) }}
          onClick={() => { if (!disabled) setOpen(true) }}
          onChange={(e) => {
            setQ(e.target.value)
            setHi(0) // 打完字高亮回到第一条:接着按回车选的就是眼前这一条
            if (!open) setOpen(true)
          }}
        />
        <span className="combo-caret"><IconChevronDown size={13} /></span>
      </div>
      {open && (
        <ul className="dropdown-menu" ref={ulRef} role="listbox">
          {shown.map((o, i) => (
            <li
              key={String(o.value)}
              role="option"
              aria-selected={curIdx === i}
              className={'dropdown-item' + (curIdx === i ? ' cur' : '') + (i === hi ? ' hi' : '')}
              // preventDefault:不让这次按下把焦点从输入框拿走 —— 拿走就触发不了 click 了
              // (与 Dropdown/PetPicker 同一处理)。
              onMouseDown={(e) => { e.preventDefault(); pickAt(i) }}
              onMouseEnter={() => setHi(i)}
            >
              {o.img ? <img className="dropdown-av" src={imgURL(o.img)} alt="" draggable={false} /> : null}
              <span className="dropdown-item-text">{o.label}</span>
              {o.sub ? <span className="dropdown-item-meta">{o.sub}</span> : null}
            </li>
          ))}
          {/* 空态说清「是搜不到」而不是「一个都没有」—— 前者玩家自己能改(换关键字) */}
          {shown.length === 0 ? (
            <li className="dropdown-empty muted">
              {kw ? `${emptyText}里没有名字含「${q.trim()}」的` : `没有可选的${emptyText}`}
            </li>
          ) : null}
        </ul>
      )}
    </div>
  )
}
