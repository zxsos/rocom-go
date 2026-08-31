import React, { useCallback, useRef, useState } from 'react'
import { NavLink, useLocation } from 'react-router-dom'
import { NAV } from '../data/nav'
import { useOutsideClick } from '../hooks/useDropdown'

// 双击当前激活的导航项:平滑滚动回页面顶部(非激活项照常跳转,不滚动)。
// 只是个工厂函数(不调任何 hook),故不用 use 前缀。
const navDoubleClick = (curPath) => (to) => () => {
  if (curPath === to) window.scrollTo({ top: 0, behavior: 'smooth' })
}

// 二级菜单的键盘操作。
//
// 菜单的显隐**没有进 React state** —— 它纯靠 CSS 的 :hover / :focus-within
// (见 shell.css 的 .navgroup-pop)。所以这里也就不去「打开/关闭」菜单,只做
// **搬动焦点**:焦点进了分组,菜单自然显形;焦点离开,菜单自然收起。
// 这是唯一自洽的做法 —— 若引入 open state,键盘搬焦点就会与 CSS 的 hover 打架
// (鼠标悬停时按 Esc 关不掉,或者状态互斥导致菜单闪一下)。
//
// 前提是 .navgroup-pop 隐藏时用 visibility:hidden 而非 display:none:
// 两者都使元素不可聚焦,故「收起时 Tab 进不去、聚焦按钮后 Tab 得进去」都成立。
const btnOf = (root) => root.querySelector('.navgroup-btn')

function groupKeyDown(e) {
  // Escape:焦点移出整个分组 → :focus-within 落空 → 菜单收起
  if (e.key === 'Escape') {
    btnOf(e.currentTarget)?.blur()
    return
  }
  if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp') return
  e.preventDefault() // 方向键默认会滚动页面
  const items = [...e.currentTarget.querySelectorAll('.navgroup-item')]
  if (!items.length) return
  const cur = items.indexOf(document.activeElement)
  // 焦点还在按钮上(不在任何子项):↓ 进第一项、↑ 进最后一项
  if (cur < 0) {
    items[e.key === 'ArrowDown' ? 0 : items.length - 1].focus()
    return
  }
  const next = cur + (e.key === 'ArrowDown' ? 1 : -1)
  // 越界:焦点退回分组按钮(菜单随之收起),而不是掉到 body —— 后者会让
  // 用户从顶栏「掉」回页面,再 Tab 一次也回不到导航。
  if (next < 0 || next >= items.length) btnOf(e.currentTarget)?.focus()
  else items[next].focus()
}

// TopNav 顶栏导航:普通项直接链接,分组项 hover 展开 2 级下拉菜单。
// 菜单开合由 CSS 承担(无 state),键盘操作见 groupKeyDown。
export function TopNav() {
  const { pathname } = useLocation()
  const onDbl = navDoubleClick(pathname)
  return (
    <nav className="topnav">
      {NAV.map((n) => {
        if (!n.children) {
          return (
            <NavLink key={n.to} to={n.to} onDoubleClick={onDbl(n.to)}
              className={({ isActive }) => 'navlink' + (isActive ? ' active' : '')}>
              <span className="nav-icon"><n.icon size={18} /></span>
              <span className="nav-label">{n.label}</span>
            </NavLink>
          )
        }
        const active = n.children.some((c) => pathname === c.to)
        return (
          <div key={n.label} className={'navgroup' + (active ? ' active' : '')}
            onKeyDown={groupKeyDown}>
            <button type="button" className="navgroup-btn" title={n.label} aria-haspopup="true">
              <span className="nav-icon"><n.icon size={18} /></span>
              <span className="nav-label">{n.label}</span>
              <span className="navgroup-arrow">▾</span>
            </button>
            <div className="navgroup-pop">
              {n.children.map((c) => (
                <NavLink key={c.to} to={c.to} onDoubleClick={onDbl(c.to)}
                  className={({ isActive }) => 'navgroup-item' + (isActive ? ' active' : '')}>
                  <span className="navgroup-item-icon"><c.icon size={15} /></span>
                  <span className="navgroup-item-label">{c.label}</span>
                </NavLink>
              ))}
            </div>
          </div>
        )
      })}
    </nav>
  )
}

// BottomNav 底部 tab(移动端):普通项直接链接,分组项点击弹出子菜单面板。
export function BottomNav() {
  const onDbl = navDoubleClick(useLocation().pathname)
  return (
    <nav className="bottomnav">
      {NAV.map((n) => (n.children
        ? <TabGroup key={n.label} item={n} onNavDoubleClick={onDbl} />
        : (
          <NavLink key={n.to} to={n.to} onDoubleClick={onDbl(n.to)}
            className={({ isActive }) => 'tab' + (isActive ? ' active' : '')}>
            <span className="tab-icon"><n.icon size={20} /></span>
            <span className="tab-label">{n.label}</span>
          </NavLink>
        )))}
    </nav>
  )
}

// TabGroup 分组 tab:点击弹出子菜单面板(向上展开),点外部/选中后关闭。
function TabGroup({ item, onNavDoubleClick }) {
  const [open, setOpen] = useState(false)
  const rootRef = useRef(null)
  const close = useCallback(() => setOpen(false), [])
  useOutsideClick(rootRef, close, open)

  const { pathname } = useLocation()
  const active = item.children.some((c) => pathname === c.to)
  return (
    <div className="tabgroup" ref={rootRef}>
      <button type="button" className={'tab' + (active ? ' active' : '')} onClick={() => setOpen((o) => !o)}>
        <span className="tab-icon"><item.icon size={20} /></span>
        <span className="tab-label">{item.label}</span>
      </button>
      {open && (
        <div className="tabgroup-pop">
          {item.children.map((c) => (
            <NavLink key={c.to} to={c.to} onDoubleClick={onNavDoubleClick(c.to)}
              className={({ isActive }) => 'tabgroup-item' + (isActive ? ' active' : '')}
              onClick={() => setOpen(false)}>
              <span className="tabgroup-item-icon"><c.icon size={17} /></span>
              <span className="tabgroup-item-label">{c.label}</span>
            </NavLink>
          ))}
        </div>
      )}
    </div>
  )
}
