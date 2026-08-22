import React, { useEffect, useState, useRef, useMemo } from 'react'
import { NavLink, Outlet, useLocation } from 'react-router-dom'
import { getAccounts, getCurrentAccount, setCurrentAccount, getIcons } from './api'
import { AccountContext, IconsContext } from './context'
import { useFullscreen } from './hooks/useFullscreen'
import { PinDialog } from './components/PinDialog'
import FloatingHost from './components/FloatingHost'
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

  // 截图防泄:窗口失焦/鼠标移出窗口/页面隐藏时给 <html> 打 data-blur,
  // CSS 据此模糊顶栏里的昵称、UID、品牌副标题;窗口重新聚焦/鼠标进入时恢复。
  // 另提供手动「隐私」开关:点击锁定模糊态(截图时主动开启),再点解锁;锁定期间不受
  // focus/mouseenter 影响。window.blur 在很多截图工具下不触发(截图工具多以 overlay 覆盖
  // 而非抢焦点),故同时用 mouseleave 兜底,并保留手动开关作为最可靠的兜底。
  const [blurLocked, setBlurLocked] = useState(false) // 手动锁定的模糊态
  useEffect(() => {
    const root = document.documentElement
    const apply = () => root.setAttribute('data-blur', '')
    const clear = () => { if (!blurLocked) root.removeAttribute('data-blur') }
    const on = () => apply()
    const off = () => clear()
    // 鼠标离开窗口区域:截图框选前鼠标常先移出,比 window.blur 更可靠
    const onLeave = (e) => { if (e.relatedTarget === null) apply() }
    const onEnter = () => clear()
    window.addEventListener('blur', on)
    window.addEventListener('focus', off)
    document.addEventListener('mouseleave', onLeave)
    document.addEventListener('mouseenter', onEnter)
    // 页面隐藏(切 tab/最小化)也算失焦
    const onVis = () => document.hidden ? on() : off()
    document.addEventListener('visibilitychange', onVis)
    // 手动锁定时强制打上;解锁时立即移除(否则解锁后若窗口一直处于焦点,
    // 没有新的 focus/鼠标进入事件触发移除,表现为"只能打开不能关闭")
    if (blurLocked) apply()
    else root.removeAttribute('data-blur')
    return () => {
      window.removeEventListener('blur', on)
      window.removeEventListener('focus', off)
      document.removeEventListener('mouseleave', onLeave)
      document.removeEventListener('mouseenter', onEnter)
      document.removeEventListener('visibilitychange', onVis)
    }
  }, [blurLocked])

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
          <div className="brand"><img className="brand-logo" src="/logo.svg" alt="" draggable={false} />洛克妙妙屋</div>
          <nav className="topnav">{navLinks('navlink')}</nav>
          {fullscreen.supported && (
            <button type="button" className={'topbar-fs' + (fullscreen.isFull ? ' on' : '')}
              onClick={fullscreen.toggle}
              title={fullscreen.isFull ? '退出网页全屏' : '网页全屏'}>
              {fullscreen.isFull ? '退出全屏' : '全屏'}
            </button>
          )}
          <button type="button" className={'topbar-fs' + (blurLocked ? ' on' : '')}
            onClick={() => setBlurLocked((v) => !v)}
            title={blurLocked ? '取消隐私遮罩(恢复显示昵称/UID)' : '隐私遮罩(模糊昵称/UID,截图前开启)'}>
            {blurLocked ? '遮罩中' : '隐私'}
          </button>
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

        {/* 全局浮窗挂载点:不走路由,切页面不卸载;位于两个 Provider 内部拿到 account/icons */}
        <FloatingHost />
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
        <span className="account-trigger-name">
          {current ? `${current.name} (UID:${uidOf(current.account)})` : '选择账号…'}
        </span>
        {current?.hasPin && <span className="account-pin-mark" title="已设 PIN 保护">🔒</span>}
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
              {a.hasPin && <span className="account-item-pin" title="已设 PIN">🔒</span>}
              <span className="muted account-item-uid">UID:{uidOf(a.account)}</span>
            </li>
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
