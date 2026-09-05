import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'

// bottomObstacleTop 探测**视口底部的粘性障碍**(移动端底栏 .bottomnav)的上沿,
// 返回可安全使用的视口底边(无障碍时即 innerHeight)。
//
// 运行时探测而非硬编码高度:底栏高度会随 env(safe-area-inset-bottom)、字号、
// 内容行数变化,写死一个数字在 iPhone 刘海屏或大字号下必然偏 —— 而它一旦偏小,
// 菜单就会正好压在被挡的那一档上,等于没修。
//
// 判定「粘性障碍」而非「任何元素」:视口底部也可能是普通内容(桌面无底栏时),
// 那些不该引发翻转 —— 内容本来就可以滚动,只有 fixed/sticky 才是真挡路的。
function bottomObstacleTop() {
  const vh = window.innerHeight
  const el = document.elementFromPoint(Math.min(window.innerWidth - 1, window.innerWidth / 2), vh - 1)
  if (!el) return vh
  for (let n = el; n && n !== document.body; n = n.parentElement) {
    if (getComputedStyle(n).position === 'fixed' || getComputedStyle(n).position === 'sticky') {
      const r = n.getBoundingClientRect()
      // 只认贴在视口底边的那一类(顶栏那类 sticky top 的 bottom 不接近 vh)
      if (r.bottom >= vh - 2) return r.top
    }
  }
  return vh
}

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
  const [up, setUp] = useState(false) // 菜单向上翻转(下方空间不足,见下面的 useLayoutEffect)
  const [hi, setHi] = useState(0)
  const rootRef = useRef(null)
  const ulRef = useRef(null)

  const close = useCallback(() => setOpen(false), [])
  useOutsideClick(rootRef, close, open, extraRef)

  // —— 下方空间不足时向上翻转 ——
  //
  // 移动端底栏(.bottomnav)是 sticky 的、层级(--z-shell 80)高于内容区浮层,
  // 故靠近视口底部的下拉一旦向下展开就会被它压住。实测分页档位下拉在
  // 360/390/414/760 四个视口下菜单底边与底栏重叠 24px,5 档里 4 档点不到
  // (「20 条/页」落到 bottomnav、后三档落在视口外)—— 见 verify-pager-dropdown.mjs。
  //
  // 用 useLayoutEffect 而非 useEffect:翻转必须在**首帧绘制前**定下来,
  // 否则用户会看到菜单先在下方闪一下再跳上去。
  //
  // 判据是「可用空间不够就翻」,而不是「检测到遮挡才翻」:
  // 后者要等布局完成后再探测、且上方也可能同样不够(那时来回翻),
  // 前者一次比较即可定下方向。
  useLayoutEffect(() => {
    if (!open) { setUp(false); return }
    const root = rootRef.current
    const ul = ulRef.current
    if (!root || !ul) return
    const trigger = root.getBoundingClientRect()
    // 菜单实际高度:此刻已渲染(布局阶段),可直接量;兜底 min() 是首帧前的估算
    const menuH = ul.getBoundingClientRect().height || 200
    const gap = 8 // 菜单与障碍之间留一点余量,别贴着边
    const below = bottomObstacleTop() - trigger.bottom - gap
    const above = trigger.top - gap
    // 下方放不下、且上方更宽裕时才翻 —— 两边都放不下时仍朝下(内容可滚动到达)
    setUp(menuH > below && above > below)
  }, [open])

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

  return { open, setOpen, up, hi, setHi, rootRef, ulRef, onKeyDown, pickAt }
}
