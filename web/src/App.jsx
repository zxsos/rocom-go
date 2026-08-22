import React, { useEffect, useState, useRef, useMemo } from 'react'
import { NavLink, Outlet, useLocation } from 'react-router-dom'
import { getAccounts, getCurrentAccount, setCurrentAccount, getIcons } from './api'
import { AccountContext, IconsContext } from './context'
import { useFullscreen } from './hooks/useFullscreen'
import { dropBoxFilter } from './pages/pet-list/filters'

const NAV = [
  { to: '/pets', label: '宠物列表', icon: '🐾' },
  { to: '/events', label: '捕获事件', icon: '🔔' },
  { to: '/eggs', label: '精灵蛋', icon: '🥚' },
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
  const fullscreen = useFullscreen() // 网页全屏:全局入口,各页面都能用(原先只在宠物列表)
  const location = useLocation()
  // 双击当前激活的导航项:平滑滚动回页面顶部(非激活项照常跳转,不滚动)
  const onNavDoubleClick = (to) => () => {
    if (location.pathname === to) window.scrollTo({ top: 0, behavior: 'smooth' })
  }

  // 截图防泄:窗口失焦(切到其他应用/截图工具)时给 <html> 打 data-blur,
  // CSS 据此模糊顶栏里的昵称、UID、品牌副标题;窗口重新聚焦自动恢复。
  useEffect(() => {
    const on = () => document.documentElement.setAttribute('data-blur', '')
    const off = () => document.documentElement.removeAttribute('data-blur')
    window.addEventListener('blur', on)
    window.addEventListener('focus', off)
    // 页面隐藏(切 tab/最小化)也算失焦
    const onVis = () => document.hidden ? on() : off()
    document.addEventListener('visibilitychange', onVis)
    return () => {
      window.removeEventListener('blur', on)
      window.removeEventListener('focus', off)
      document.removeEventListener('visibilitychange', onVis)
    }
  }, [])

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

  // 账号在线状态轮询:后端按「最近 30s 内有流量」判定(见 server.AccountOnline),
  // 下拉里用 ● 在线 / ○ 离线 标注。15s 刷一次足够(状态不会秒变),仅列表非空时轮询;
  // setAccounts 触发重渲染但 account 未变,<main key={account}> 不会重挂各页。
  useEffect(() => {
    if (!accounts.length) return
    const refresh = () => {
      getAccounts().then((list) => {
        if (list && list.length) setAccounts(list)
      }).catch(() => {})
    }
    const timer = setInterval(refresh, 15000)
    return () => clearInterval(timer)
  }, [accounts.length])

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
          <div className="brand"><img className="brand-logo" src="/logo.svg" alt="" draggable={false} />小洛克的妙妙工具 <span className="brand-sub">宠物统计</span></div>
          <nav className="topnav">{navLinks('navlink')}</nav>
          {fullscreen.supported && (
            <button type="button" className={'topbar-fs' + (fullscreen.isFull ? ' on' : '')}
              onClick={fullscreen.toggle}
              title={fullscreen.isFull ? '退出网页全屏' : '网页全屏'}>
              {fullscreen.isFull ? '退出全屏' : '全屏'}
            </button>
          )}
          {accounts.length > 0 && (() => {
            const cur = accounts.find((a) => a.account === account)
            return (
              <AccountSelect
                accounts={accounts}
                current={cur}
                onChange={switchAccount}
                uidOf={uidOf}
              />
            )
          })()}
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

// AccountSelect 自定义账号下拉:原生 <option> 不支持内嵌 <img>,无法显示用户上传的
// login.svg/logout.svg 状态图标——故用 div 模拟 dropdown。键鼠/触屏均可操作:
// 鼠标点击展开/选条;键盘 ↑↓ 切换、Enter 选择、Esc 关闭。点外部自动收起。
// 仍复用 .account-wrap/.account-state/.account-select 容器样式,只是下拉浮层是自绘。
function AccountSelect({ accounts, current, onChange, uidOf }) {
  const [open, setOpen] = useState(false)
  const [hi, setHi] = useState(0) // 高亮项索引(键盘 ↑↓ 移动)
  const rootRef = useRef(null)
  const listRef = useRef(null)

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
      const idx = current ? accounts.findIndex((a) => a.account === current.account) : 0
      setHi(idx < 0 ? 0 : idx)
    }
  }, [open]) // eslint-disable-line react-hooks/exhaustive-deps

  // 高亮项变化时滚动到可见(键盘 ↑↓ 时不至于飘出可见区)
  useEffect(() => {
    if (!open || !listRef.current) return
    const el = listRef.current.children[hi]
    if (el) el.scrollIntoView({ block: 'nearest' })
  }, [hi, open])

  const choose = (a) => {
    setOpen(false)
    if (a.account !== current?.account) onChange(a.account)
  }

  const onKey = (e) => {
    if (!open) {
      if (e.key === 'ArrowDown' || e.key === 'Enter' || e.key === ' ') {
        e.preventDefault()
        setOpen(true)
      }
      return
    }
    switch (e.key) {
      case 'ArrowDown':
        e.preventDefault()
        setHi((i) => (i + 1) % accounts.length)
        break
      case 'ArrowUp':
        e.preventDefault()
        setHi((i) => (i - 1 + accounts.length) % accounts.length)
        break
      case 'Enter':
      case ' ':
        e.preventDefault()
        if (accounts[hi]) choose(accounts[hi])
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
    <div className="account-wrap" ref={rootRef} onKeyDown={onKey}>
      <button
        type="button"
        className={'select account-select account-trigger' + (open ? ' open' : '')}
        onClick={() => setOpen((o) => !o)}
        title="切换账号(玩家)"
        aria-haspopup="listbox"
        aria-expanded={open}
      >
        {current && (
          <img className="account-state" src={current.online ? '/login.svg' : '/logout.svg'}
            alt="" draggable={false} title={current.online ? '在线' : '离线'} />
        )}
        <span className="account-trigger-name">
          {current ? `${current.name} (UID:${uidOf(current.account)})` : '选择账号…'}
        </span>
        <span className="account-caret">▾</span>
      </button>
      {open && (
        <ul className="account-dropdown" ref={listRef} role="listbox">
          {accounts.map((a, i) => (
            <li
              key={a.account}
              role="option"
              aria-selected={current && a.account === current.account}
              className={
                'account-item' +
                (current && a.account === current.account ? ' cur' : '') +
                (i === hi ? ' hi' : '')
              }
              onMouseDown={(e) => { e.preventDefault(); choose(a) }}
              onMouseEnter={() => setHi(i)}
            >
              <img className="account-state" src={a.online ? '/login.svg' : '/logout.svg'}
                alt="" draggable={false} title={a.online ? '在线' : '离线'} />
              <span className="account-item-name">{a.name}</span>
              <span className="muted account-item-uid">UID:{uidOf(a.account)}</span>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
