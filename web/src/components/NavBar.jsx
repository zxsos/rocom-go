import React, { useCallback, useRef, useState } from 'react'
import { NavLink, useLocation } from 'react-router-dom'
import { NAV } from '../data/nav'
import { useOutsideClick } from '../hooks/useDropdown'

// 双击当前激活的导航项:平滑滚动回页面顶部(非激活项照常跳转,不滚动)。
// 只是个工厂函数(不调任何 hook),故不用 use 前缀。
const navDoubleClick = (curPath) => (to) => () => {
  if (curPath === to) window.scrollTo({ top: 0, behavior: 'smooth' })
}

// TopNav 顶栏导航:普通项直接链接,分组项 hover 展开 2 级下拉菜单(纯 CSS hover,无状态)。
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
          <div key={n.label} className={'navgroup' + (active ? ' active' : '')}>
            <button type="button" className="navgroup-btn" title={n.label}>
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
