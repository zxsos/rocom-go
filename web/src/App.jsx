import React, { useCallback, useEffect, useState } from 'react'
import { Outlet } from 'react-router-dom'
import { getIcons } from './api'
import { AccountContext, AccountNameContext, IconsContext } from './context'
import { useFullscreen } from './hooks/useFullscreen'
import { useTheme } from './hooks/useTheme'
import { usePrivacy } from './hooks/usePrivacy'
import { useAccounts } from './hooks/useAccounts'
import { TopNav, BottomNav } from './components/NavBar'
import AccountSelect from './components/AccountSelect'
import { PinDialog } from './components/PinDialog'
import { IconSun, IconMoon, IconMonitor, IconExpand, IconCompress } from './components/svg'

// App 全局壳:顶栏导航 + 账号切换 + 底部 tab(移动),并分发账号/图标两个全局 Context。
// 各块细节分别见 hooks/useTheme、hooks/usePrivacy、hooks/useAccounts、components/NavBar、
// components/AccountSelect;本文件只负责组装与 PIN 弹窗编排(三者共用一个弹窗)。
export default function App() {
  // PIN 弹窗编排:三种模式共用一个弹窗——切账号校验(verify,由 useAccounts 触发)、
  // 管理 PIN(manage)、删除账号(delete),后两者来自账号下拉菜单。
  const [pinDialog, setPinDialog] = useState(null) // null | { mode, account, name, hasPin }
  const [pendingAccount, setPendingAccount] = useState(null) // 校验通过后要切过去的账号
  // 首屏拦截(默认账号设了 PIN):也要记 pendingAccount,使校验通过后走 selectAccount
  // 落地——切过去的动作本身是幂等的,但它会顺带清掉账号绑定的盒子筛选。
  const onPinRequired = useCallback((acc) => {
    setPendingAccount(acc.account)
    setPinDialog({ mode: 'verify', account: acc.account, name: acc.name, hasPin: true })
  }, [])

  const { accounts, account, current, accountName, requestAccount, selectAccount, refreshAccounts } =
    useAccounts(onPinRequired)
  const { theme, cycle: cycleTheme } = useTheme()
  const { on: privacyOn, toggle: togglePrivacy } = usePrivacy()
  const fullscreen = useFullscreen() // 网页全屏:全局入口,各页面都能用(原先只在宠物列表)
  const [icons, setIcons] = useState({ stat: {} })

  // 全局固定图标只随游戏版本变,拉一次即可。
  useEffect(() => { getIcons().then((d) => setIcons(d || { stat: {} })).catch(() => {}) }, [])

  // 切账号:目标账号设了 PIN 且本会话未解锁时,useAccounts 返回账号对象 → 弹窗;否则已直接切好。
  const switchAccount = (acc) => {
    const needPin = requestAccount(acc)
    if (needPin) onPinRequired(needPin)
  }
  const closePin = () => { setPinDialog(null); setPendingAccount(null) }

  const themeLabel = theme === 'auto' ? '跟随系统' : theme === 'light' ? '白天' : '夜间'
  const themeIcon = theme === 'auto' ? <IconMonitor size={17} />
    : theme === 'light' ? <IconSun size={17} /> : <IconMoon size={17} />

  return (
    <AccountContext.Provider value={account}>
      <AccountNameContext.Provider value={accountName}>
        <IconsContext.Provider value={icons}>
          <div className="app">
            <header className="topbar">
              {/* 品牌名即截图遮罩开关:开启时变暗,提示当前处于保护态 */}
              <button type="button" className={'brand' + (privacyOn ? ' privacy-on' : '')}
                onClick={togglePrivacy} title={privacyOn ? '点击解除遮罩' : '点击开启遮罩'}>
                <img className="brand-logo" src="/logo.svg" alt="" draggable={false} /><span className="privacy">妙妙屋</span>
              </button>
              <TopNav />
              {fullscreen.supported && (
                <button type="button" className={'topbar-fs' + (fullscreen.isFull ? ' on' : '')}
                  onClick={fullscreen.toggle}
                  title={fullscreen.isFull ? '退出网页全屏' : '网页全屏'}>
                  <span className="topbar-fs-icon">{fullscreen.isFull ? <IconCompress size={16} /> : <IconExpand size={16} />}</span>
                  <span className="topbar-fs-text">{fullscreen.isFull ? '退出全屏' : '全屏'}</span>
                </button>
              )}
              <button type="button" className="topbar-fs"
                onClick={cycleTheme}
                title={'主题:' + themeLabel + '(点击切换)'}>
                <span className="topbar-theme-icon">{themeIcon}</span>
              </button>
              {accounts.length > 0 && (
                <AccountSelect
                  accounts={accounts}
                  current={current}
                  onChange={switchAccount}
                  onManagePin={(acc) => setPinDialog({ mode: 'manage', account: acc.account, name: acc.name, hasPin: acc.hasPin })}
                  onDeleteAccount={(acc) => setPinDialog({ mode: 'delete', account: acc.account, name: acc.name, hasPin: acc.hasPin })}
                />
              )}
            </header>

            {/* 不用 key={account} 强制重挂:各页按 account 依赖重取(见 hooks/useAsyncData 的
                reloadKey),切账号只刷新数据、不动组件树,故筛选/页码/详情弹窗等 UI 态得以保留。 */}
            <main className="content">
              <Outlet />
            </main>

            <BottomNav />
          </div>
          {pinDialog && (
            <PinDialog
              mode={pinDialog.mode}
              account={pinDialog.account}
              name={pinDialog.name}
              hasPin={pinDialog.hasPin}
              onClose={closePin}
              onVerified={() => {
                // PIN 校验通过,执行待切换(closePin 已清 pendingAccount,此处读的仍是本次渲染的闭包值)
                closePin()
                if (pendingAccount) selectAccount(pendingAccount)
                refreshAccounts()
              }}
              onDeleted={() => {
                // 账号已删除:若删的是当前账号,切到列表第一个;刷新列表
                const removed = pinDialog.account
                closePin()
                if (removed === account) {
                  const remaining = accounts.filter((a) => a.account !== removed)
                  // 删完了就清空选中(selectAccount('') 会一并清掉当前账号与盒子筛选)
                  selectAccount(remaining.length ? remaining[0].account : '')
                }
                refreshAccounts()
              }}
            />
          )}
        </IconsContext.Provider>
      </AccountNameContext.Provider>
    </AccountContext.Provider>
  )
}
