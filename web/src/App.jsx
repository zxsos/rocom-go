import React, { useCallback, useEffect, useState } from 'react'
import { Outlet, useLocation } from 'react-router-dom'
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
import MapEngineProvider from './pages/map/MapEngineProvider'
import AnnotationsProvider from './components/annotations'

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
  const { on: privacyOn, toggle: togglePrivacy, setOff: setPrivacyOff, setOn: setPrivacyOn } = usePrivacy()
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

  // 账号切换器展开期间临时解除遮罩,收起即恢复 —— 无论是切成功还是取消。
  // 详见 hooks/usePrivacy 的注释。用 useCallback 包住:它作为 prop 传给 AccountSelect
  // 并进了那里的 useEffect 依赖,引用不稳会导致每次渲染都重跑。
  const onDropdownOpenChange = useCallback((open) => {
    if (open) setPrivacyOff()
    else setPrivacyOn()
  }, [setPrivacyOff, setPrivacyOn])

  const themeLabel = theme === 'auto' ? '跟随系统' : theme === 'light' ? '白天' : '夜间'
  const themeIcon = theme === 'auto' ? <IconMonitor size={17} />
    : theme === 'light' ? <IconSun size={17} /> : <IconMoon size={17} />

  return (
    <AccountContext.Provider value={account}>
      <AccountNameContext.Provider value={accountName}>
        <AnnotationsProvider>
        <IconsContext.Provider value={icons}>
          <div className="app">
            <header className="topbar">
              {/* 品牌名**即**截图遮罩开关,且**刻意不做成按钮的样子** ——
                  无边框/底色/图标,看着只是顶栏角落一个 logo + 站名;站名还带 .privacy,
                  与昵称/UID 一起被糊掉(截图里不该留下「这是哪个工具」的线索)。
                  曾有一版把它拆成右侧独立的盾牌图标按钮 + 顶栏金色状态条,已按原设计退回:
                  这块的价值就在于「看起来不像开关」,做显著了就失去意义。
                  开启时整块变暗(见 shell.css 的 html[data-privacy] .brand),是它唯一的提示。 */}
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
                  onDropdownOpenChange={onDropdownOpenChange}
                  onManagePin={(acc) => setPinDialog({ mode: 'manage', account: acc.account, name: acc.name, hasPin: acc.hasPin })}
                  onDeleteAccount={(acc) => setPinDialog({ mode: 'delete', account: acc.account, name: acc.name, hasPin: acc.hasPin })}
                />
              )}
            </header>

            {/* 地图引擎常驻于此(而非 MapPage):画中画要在离开地图页之后继续更新,
                故引擎必须活在任何单个页面之外。放进独立 Provider 组件而非直接写在这里,
                是为了把「图层数据推送」引发的重渲染隔离在地图页内——详见 MapEngineProvider。 */}
            <MapEngineProvider account={account} theme={theme} icons={icons}>
              {/* 不用 key={account} 强制重挂:各页按 account 依赖重取(见 hooks/useAsyncData 的
                  reloadKey),切账号只刷新数据、不动组件树,故筛选/页码/详情弹窗等 UI 态得以保留。 */}
              <main className="content">
                <RouteEnter><Outlet /></RouteEnter>
              </main>
            </MapEngineProvider>

            <BottomNav />
          </div>
          {/* onSaved:改/清 PIN 后重拉账号列表 —— hasPin 由 accounts 持有,
              不刷新的话下拉里的锁图标与「修改 PIN / 设置 PIN」文案都还停在旧状态。
              verify 与 delete 两条分支各自已在 onVerified / onDeleted 里刷过,不缺。 */}
          {pinDialog && (
            <PinDialog
              mode={pinDialog.mode}
              account={pinDialog.account}
              name={pinDialog.name}
              hasPin={pinDialog.hasPin}
              onClose={closePin}
              onSaved={refreshAccounts}
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
        </AnnotationsProvider>
      </AccountNameContext.Provider>
    </AccountContext.Provider>
  )
}

// RouteEnter 页面切换过渡(P5.4.1):路由出口包一层,pathname 变化时换 key 触发
// CSS animation —— 内容淡入 + 8px 上移(--dur-base / --ease-out,见 base.css 注释)。
//
// 两个刻意的设计:
//  1. **只包 Outlet,不包整页**:顶栏/底导航是固定外壳,跟着内容一起动会显得整页在晃。
//  2. **key 用 pathname 而非 location.key**:后者每次导航(含同路径的 replace)都变,
//     会让「点同一个菜单项」也重播动画;pathname 语义正好是「换了一屏」。
//     路由组件本来就会随导航卸载重挂(见 useAsyncData 的 reloadKey 注释),
//     故这里换 key 不会额外损失 UI 态。
//
// 地图页安全性:.map-vp 的尺寸用 clientWidth/clientHeight 测量(usePanZoom),
// 布局尺寸不受 transform 影响,故 8px 位移不会让地图算错视口;位移也只在动画
// 期间存在(animation 无 fill-mode,结束后回到常态 transform: none)。
function RouteEnter({ children }) {
  const { pathname } = useLocation()
  return <div className="route-enter" key={pathname}>{children}</div>
}
