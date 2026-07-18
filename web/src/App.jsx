import React, { useEffect, useState } from 'react'
import { NavLink, Outlet, useLocation } from 'react-router-dom'
import { getAccounts, getCurrentAccount, setCurrentAccount, getIcons } from './api'
import { AccountContext, IconsContext } from './context'
import { dropBoxFilter } from './pages/pet-list/filters'

const NAV = [
  { to: '/pets', label: '宠物列表', icon: '🐾' },
  { to: '/events', label: '捕获事件', icon: '🔔' },
  { to: '/map', label: '实时地图', icon: '🗺️' },
  { to: '/debug', label: '调试', icon: '🐞' },
]

// uidOf 从账号键 "UID:<user_id>" 取出 user_id(用于展示 nickname(user_id))。
const uidOf = (acc) => (acc || '').replace(/^UID:/, '')

// App 全局壳:顶栏导航 + 账号切换 + 底部 tab(移动),并分发账号/图标两个全局 Context。
export default function App() {
  const [accounts, setAccounts] = useState([])
  const [account, setAccount] = useState(getCurrentAccount())
  const [icons, setIcons] = useState({ stat: {} })
  const location = useLocation()
  // 双击当前激活的导航项:平滑滚动回页面顶部(非激活项照常跳转,不滚动)
  const onNavDoubleClick = (to) => () => {
    if (location.pathname === to) window.scrollTo({ top: 0, behavior: 'smooth' })
  }

  // 全局固定图标只随游戏版本变,拉一次即可。
  useEffect(() => { getIcons().then((d) => setIcons(d || { stat: {} })).catch(() => {}) }, [])

  // 拉账号列表;当前无选中(或选中的已不存在)时默认选最近活跃的第一个。
  useEffect(() => {
    getAccounts().then((list) => {
      list = list || []
      setAccounts(list)
      const cur = getCurrentAccount()
      if ((!cur || !list.some((a) => a.account === cur)) && list.length) {
        setCurrentAccount(list[0].account)
        setAccount(list[0].account)
      }
    }).catch(() => {})
  }, [])

  // 切换账号:更新 api.js 当前账号、清掉与旧账号绑定的盒子筛选,再切 state
  // (下方 <main key={account}> 据此重挂各页,让其以新账号重新拉数据)。
  const switchAccount = (a) => {
    if (!a || a === account) return
    setCurrentAccount(a)
    dropBoxFilter()
    setAccount(a)
  }

  const navLinks = (base) => NAV.map((n) => (
    <NavLink key={n.to} to={n.to} onDoubleClick={onNavDoubleClick(n.to)}
      className={({ isActive }) => base + (isActive ? ' active' : '')}>
      <span className={base === 'tab' ? 'tab-icon' : 'nav-icon'}>{n.icon}</span>
      <span className={base === 'tab' ? 'tab-label' : 'nav-label'}>{n.label}</span>
    </NavLink>
  ))

  return (
    <AccountContext.Provider value={account}>
      <IconsContext.Provider value={icons}>
      <div className="app">
        <header className="topbar">
          <div className="brand">洛克助手 <span className="brand-sub">宠物统计</span></div>
          <nav className="topnav">{navLinks('navlink')}</nav>
          {accounts.length > 0 && (
            <select
              className="select account-select"
              value={account} onChange={(e) => switchAccount(e.target.value)}
              title="切换账号(玩家)"
            >
              {accounts.map((a) => (
                <option key={a.account} value={a.account}>{a.name} (UID:{uidOf(a.account)})</option>
              ))}
            </select>
          )}
        </header>

        <main className="content" key={account}>
          <Outlet />
        </main>

        <nav className="bottomnav">{navLinks('tab')}</nav>
      </div>
      </IconsContext.Provider>
    </AccountContext.Provider>
  )
}
