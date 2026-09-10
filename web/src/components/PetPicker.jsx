import React, { useMemo, useRef, useState } from 'react'
import { useDropdown } from '../hooks/useDropdown'
import { imgURL } from './icons'
import { IconChevronDown } from './svg'

// PetPicker 可搜索的宠物单选下拉,替换培育页里那三个原生 <select className="select">。
//
// 为什么非换不可:候选来自**整个宠物库**(同品种几百上千只),而原生 <option> 既放不了头像、
// 也没法「输入名字即时过滤」—— 玩家面对一长串「小火 V12 W88%」同名个体,只能滚着找,基本
// 等于不能用。视觉与交互都复用站内下拉(.dropdown + useDropdown,见 Dropdown.jsx),
// 只在菜单顶部多一个搜索框。
//
// 选项由调用方组装成 {value,name,img,sub}(见 pets.petPickerOption):name 单独给出来是给过滤
// 用的,数值压成一行副文本。**不接整个宠物对象**:候选池下发的形状与 /api/pets 不同(见 api.js
// 的 getBreedingPool),让 picker 依赖其中任何一种,都会把「换数据源」变成改组件。
//
// clearLabel 给出时,列表首行是一条「不填」项(种公可以未知、认领时可以保留原值),
// 触发按钮上仍显示 placeholder —— 它表达的是「还没选」,而不是「选了一只叫『不填』的宠」。
export default function PetPicker({
  value, options, onChange, placeholder = '选择一只…', clearLabel, disabled, className = '', title,
}) {
  // 包一层 useMemo:否则 options 省略时每次渲染都会造一个新数组,下面的过滤 useMemo 就白算了。
  const items = useMemo(() => options || [], [options])
  const [q, setQ] = useState('')
  const searchRef = useRef(null)

  // 桌面(有精细指针且能悬浮)展开即聚焦搜索框 —— 点开就能打字。
  // 触摸设备不自动聚焦:那会立刻弹出软键盘、把候选列表顶掉大半屏,而玩家这时多半是想先扫一眼。
  const autoFocus = useMemo(
    () => typeof window !== 'undefined'
      && window.matchMedia('(hover: hover) and (pointer: fine)').matches,
    [],
  )

  // 关键词过滤放在本地:候选池一次就给全了,O(n) 的字符串比较(几百条)没有节流与竞态的麻烦,
  // 换来的是输入即出结果、也不需要为搜索再打一次请求。
  const shown = useMemo(() => {
    const k = q.trim().toLowerCase()
    if (!k) return items
    return items.filter((o) => String(o.name || '').toLowerCase().includes(k))
  }, [items, q])

  // 行 = 可选的「不填」行 + 过滤后的候选。键盘导航与回车选中的都是**眼前这一份** ——
  // 高亮索引若落在原始候选上,搜完之后按回车会选中一只已经看不见的宠。
  const rows = useMemo(() => (clearLabel ? [{ clear: true, value: '' }, ...shown] : shown), [clearLabel, shown])
  const curIdx = useMemo(
    () => rows.findIndex((o) => o.value === String(value == null ? '' : value)),
    [rows, value],
  )
  const sel = curIdx >= 0 && !rows[curIdx].clear ? rows[curIdx] : null

  const { open, setOpen, up, hi, setHi, rootRef, ulRef, onKeyDown, pickAt } = useDropdown({
    count: rows.length,
    selectedIndex: curIdx,
    disabled,
    searchRef,
    onPick: (i) => {
      const o = rows[i]
      if (o && String(o.value) !== String(value == null ? '' : value)) onChange(o.value)
    },
  })

  return (
    <div
      className={'dropdown pet-picker' + (open ? ' open' : '') + (up ? ' up' : '')
        + (className ? ' ' + className : '')}
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
        {sel ? (
          <>
            {sel.img ? <img className="dropdown-av" src={imgURL(sel.img)} alt="" /> : null}
            <span className="dropdown-value">{sel.name}</span>
            {sel.sub ? <span className="dropdown-meta">{sel.sub}</span> : null}
          </>
        ) : <span className="dropdown-value">{placeholder}</span>}
        <span className="dropdown-caret"><IconChevronDown size={13} /></span>
      </button>
      {open && (
        <div className="dropdown-menu">
          {/* 搜索框留在滚动区外:几百只候选滚到一半想改关键词时,它不该跟着滚走。 */}
          <div className="dropdown-search">
            <input
              ref={searchRef} className="input" value={q} autoFocus={autoFocus}
              placeholder="输入名字搜索…" aria-label="按名字搜索"
              onChange={(e) => { setQ(e.target.value); setHi(0) }}
            />
          </div>
          <ul className="dropdown-list" ref={ulRef} role="listbox">
            {rows.map((o, i) => (
              <li
                key={o.clear ? '__clear' : o.value}
                role="option"
                aria-selected={curIdx === i}
                className={'dropdown-item' + (curIdx === i ? ' cur' : '') + (i === hi ? ' hi' : '')
                  + (o.clear ? ' clear' : '')}
                onMouseDown={(e) => { e.preventDefault(); pickAt(i) }}
                onMouseEnter={() => setHi(i)}
              >
                {o.img ? <img className="dropdown-av" src={imgURL(o.img)} alt="" /> : null}
                <span className="dropdown-item-text">{o.clear ? clearLabel : o.name}</span>
                {o.sub ? <span className="dropdown-item-meta">{o.sub}</span> : null}
              </li>
            ))}
            {/* 空态要说清「是搜不到」而不是「库里没有」—— 前者是玩家自己能改的(关键词),
                后者要等宠物进库;两者混为一谈会让人以为这只宠真的没了。 */}
            {shown.length === 0 ? (
              <li className="dropdown-empty muted">
                {q.trim() ? `没有名字含「${q.trim()}」的宠物` : '这个品种在库里还没有候选'}
              </li>
            ) : null}
          </ul>
        </div>
      )}
    </div>
  )
}
