import React, { useEffect, useRef, useState } from 'react'
import { NavLink, Outlet, useLocation } from 'react-router-dom'
import { getAccounts, getCurrentAccount, setCurrentAccount, getIcons } from './api'
import { AccountContext, IconsContext } from './context'
import { useFullscreen } from './hooks/useFullscreen'
import { PinDialog } from './components/PinDialog'
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
  // PIN 保护:切到有 PIN 的账号需先校验;pendingAccount=待切换账号,pinDialog=当前弹窗模式
  const [pendingAccount, setPendingAccount] = useState(null)
  const [pinDialog, setPinDialog] = useState(null) // null | { mode, account, name, hasPin }
  // 双击当前激活的导航项:平滑滚动回页面顶部(非激活项照常跳转,不滚动)
  const onNavDoubleClick = (to) => () => {
    if (location.pathname === to) window.scrollTo({ top: 0, behavior: 'smooth' })
  }

  // 截图防泄(全局开关):点击顶栏品牌名「洛克妙妙屋」切换。默认开启——敏感文字
  // (昵称/UID)常驻模糊,点击品牌名解除全部,再点恢复。不依赖任何窗口焦点/鼠标/触屏事件,
  // 手机电脑行为一致,截图/录屏/投屏都不触发 DOM 事件故默认即保护最可靠。
  // 状态打在 <html data-privacy> 上,CSS 据此模糊 .privacy 元素;品牌名本身遮罩开启时变暗。
  const [privacyOn, setPrivacyOn] = useState(true)
  useEffect(() => {
    document.documentElement.toggleAttribute('data-privacy', privacyOn)
  }, [privacyOn])
  const togglePrivacy = () => setPrivacyOn((v) => !v)

  // 全局固定图标只随游戏版本变,拉一次即可。
  useEffect(() => { getIcons().then((d) => setIcons(d || { stat: {} })).catch(() => {}) }, [])

  // 拉账号列表;当前无选中(或选中的已不存在)时默认选最近活跃的第一个。
  // 默认账号若设了 PIN 且未解锁,弹 PIN 框(首屏即拦截)。
  useEffect(() => {
    getAccounts().then((list) => {
      list = list || []
      setAccounts(list)
      const cur = getCurrentAccount()
      const target = (cur && list.some((a) => a.account === cur)) ? cur
        : (list.length ? list[0].account : '')
      if (!cur || !list.some((a) => a.account === cur)) {
        if (target) { setCurrentAccount(target); setAccount(target) }
      }
      // 首屏 PIN 拦截:默认账号有 PIN 且本会话未解锁
      if (target) {
        const acc = list.find((a) => a.account === target)
        if (acc?.hasPin && sessionStorage.getItem('pin:' + target) !== '1') {
          setPendingAccount(target)
          setPinDialog({ mode: 'verify', account: target, name: acc.name, hasPin: true })
        }
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

  // 刷新账号列表(PIN 变更/账号删除后调用)
  const refreshAccounts = () => {
    getAccounts().then((list) => { if (list) setAccounts(list) }).catch(() => {})
  }

  // 切换账号:若目标账号设了 PIN 且本会话未解锁,弹 PIN 框;否则直接切。
  // (下方 <main key={account}> 据此重挂各页,让其以新账号重新拉数据)。
  const switchAccount = (a) => {
    if (!a || a === account) return
    const target = accounts.find((x) => x.account === a)
    const hasPin = target?.hasPin
    const unlocked = sessionStorage.getItem('pin:' + a) === '1'
    if (hasPin && !unlocked) {
      setPendingAccount(a)
      setPinDialog({ mode: 'verify', account: a, name: target?.name, hasPin: true })
      return
    }
    doSwitch(a)
  }
  const doSwitch = (a) => {
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
          <button type="button" className={'brand' + (privacyOn ? ' privacy-on' : '')}
            onClick={togglePrivacy} title={privacyOn ? '点击解除遮罩' : '点击开启遮罩'}>
            <img className="brand-logo" src="/logo.svg" alt="" draggable={false} /><span className="privacy">洛克妙妙屋</span>
          </button>
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
                onManagePin={(acc) => setPinDialog({ mode: 'manage', account: acc.account, name: acc.name, hasPin: acc.hasPin })}
                onDeleteAccount={(acc) => setPinDialog({ mode: 'delete', account: acc.account, name: acc.name, hasPin: acc.hasPin })}
              />
            )
          })()}
        </header>

        <main className="content" key={account}>
          <Outlet />
        </main>

        <nav className="bottomnav">{navLinks('tab')}</nav>
      </div>
      {pinDialog && (
        <PinDialog
          mode={pinDialog.mode}
          account={pinDialog.account}
          name={pinDialog.name}
          hasPin={pinDialog.hasPin}
          onClose={() => { setPinDialog(null); setPendingAccount(null) }}
          onVerified={() => {
            // PIN 校验通过,执行待切换
            setPinDialog(null)
            if (pendingAccount) { doSwitch(pendingAccount); setPendingAccount(null) }
            refreshAccounts()
          }}
          onDeleted={() => {
            // 账号已删除:若删的是当前账号,切到列表第一个;刷新列表
            setPinDialog(null)
            setPendingAccount(null)
            if (pinDialog.account === account) {
              const remaining = accounts.filter((a) => a.account !== pinDialog.account)
              if (remaining.length) { doSwitch(remaining[0].account) }
              else { setCurrentAccount(''); setAccount('') }
            }
            refreshAccounts()
          }}
        />
      )}
      </IconsContext.Provider>
    </AccountContext.Provider>
  )
}

// AccountSelect 自定义账号下拉:原生 <option> 不支持内嵌 <img>,无法显示用户上传的
// login.svg/logout.svg 状态图标——故用 div 模拟 dropdown。键鼠/触屏均可操作:
// 鼠标点击展开/选条;键盘 ↑↓ 切换、Enter 选择、Esc 关闭。点外部自动收起。
// 仍复用 .account-wrap/.account-state/.account-select 容器样式,只是下拉浮层是自绘。
function AccountSelect({ accounts, current, onChange, uidOf, onManagePin, onDeleteAccount }) {
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
        <span className="privacy account-trigger-name">
          {current ? `${current.name} (UID:${uidOf(current.account)})` : '选择账号…'}
        </span>
        {current?.hasPin && <span className="account-pin-mark" title="已设 PIN 保护">🔒</span>}
        <span className="account-caret">▾</span>
      </button>
      {open && (
        <ul className="account-dropdown" ref={listRef} role="listbox">
          {accounts.map((a, i) => (
            <AccountItem
              key={a.account}
              account={a}
              cur={current && a.account === current.account}
              hi={i === hi}
              uidOf={uidOf}
              onChoose={() => choose(a)}
              onHover={() => setHi(i)}
            />
          ))}
          {/* 当前账号的 PIN 管理 + 删除入口 */}
          {current && (
            <li className="account-item account-actions" onMouseEnter={() => setHi(-1)}>
              <button className="btn small account-action-btn" onClick={(e) => {
                e.stopPropagation(); setOpen(false)
                onManagePin?.(current)
              }}>管理 PIN</button>
              <button className="btn small account-action-btn account-del-btn" onClick={(e) => {
                e.stopPropagation(); setOpen(false)
                onDeleteAccount?.(current)
              }}>删除账号</button>
            </li>
          )}
        </ul>
      )}
    </div>
  )
}

// AccountItem 下拉里的单条账号。
function AccountItem({ account, cur, hi, uidOf, onChoose, onHover }) {
  return (
    <li
      role="option"
      aria-selected={cur}
      className={'account-item' + (cur ? ' cur' : '') + (hi ? ' hi' : '')}
      onMouseDown={(e) => { e.preventDefault(); onChoose() }}
      onMouseEnter={onHover}
    >
      <img className="account-state" src={account.online ? '/login.svg' : '/logout.svg'}
        alt="" draggable={false} title={account.online ? '在线' : '离线'} />
      <span className="privacy account-item-name">{account.name}</span>
      {account.hasPin && <span className="account-item-pin" title="已设 PIN">🔒</span>}
      <span className="muted privacy account-item-uid">UID:{uidOf(account.account)}</span>
    </li>
  )
}
