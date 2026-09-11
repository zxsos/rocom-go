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
  // 品牌扫光:每次点击遮罩开关都把 .brand-spark 重新挂载(key 变)→ CSS 动画必然重跑。
  // 为什么不用纯 CSS 触发(:hover 或属性选择器):鼠标停在品牌上时 :hover 持续命中,
  // 动画播完后仍是「同一条、已播完」—— 再点只是切遮罩,animation-name 没变过,
  // 浏览器不会重播,于是「一直点只有开关有反应、扫光没反应」(实测确认)。
  // ⚠️ 这两段必须放在 usePrivacy() **之后**:依赖数组里的 togglePrivacy/privacyOn 是 const,
  // 写在前面会命中 TDZ,整页白屏(ReferenceError: Cannot access before initialization)。
  const [sweep, setSweep] = useState(0)
  const onBrandClick = useCallback(() => {
    setSweep((n) => n + 1)
    togglePrivacy()
  }, [togglePrivacy])
  // 悬停也给一次(鼠标用户知道这块能点)。**只在名字清晰时**给:遮罩开着时名字是糊的,
  // 那时掠一下等于在糊块上闪一道,没有信息量。触摸端的 mouseenter 会跟着首次 tap 一起触发,
  // 与随后的 click 各挂载一次 —— 后一次会顶掉前一次(key 变),表现为一次干净的扫光。
  const onBrandHover = useCallback(() => {
    if (!privacyOn) setSweep((n) => n + 1)
  }, [privacyOn])
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
                onClick={onBrandClick} onMouseEnter={onBrandHover}
                title={privacyOn ? '点击解除遮罩' : '点击开启遮罩'}>
                <img className="brand-logo" src="/logo.svg" alt="" draggable={false} />
                {/* 铭牌式站名:「-go」切强调色、名字下面一道淡出的尾迹。
                    字体换掉是本轮的重点 —— 原先走 --font-display(=思源黑体,中文标题用),
                    拿它排拉丁小写笔画均匀、字面没有收放,8 个字符平得像打印体,这才是
                    「死板」的来源;改走 --font-num(Bricolage Grotesque 700,ASCII 子集)。 */}
                <span className="brand-name privacy">
                  <span className="brand-word">rocom<span className="brand-go">-go</span></span>
                  {/* 扫光:key 一变即重新挂载,动画必然重跑(见上面 sweep 的注释)。
                      方向跟着动作走:解除遮罩向右(揭开),开启遮罩向左(合上)。 */}
                  {sweep > 0 && (
                    <span key={sweep} className={'brand-spark' + (privacyOn ? ' back' : '')} aria-hidden="true" />
                  )}
                </span>
              </button>
              {/* 应用版本号:唯一真源是仓库根 VERSION,构建时注入(见 vite.config.js)。
                  既是给玩家看的「跑的是哪一版」,也是排障时第一句要问的信息。 */}
              <span className="topbar-ver" title="应用版本(赛季.大更新.小更新)">v{__APP_VERSION__}</span>
              <TopNav />
              {fullscreen.supported && (
                <button type="button" className={'topbar-fs' + (fullscreen.isFull ? ' on' : '')}
                  onClick={fullscreen.toggle}
                  title={fullscreen.isFull ? '退出网页全屏' : '网页全屏'}>
                  <span className="topbar-fs-icon">{fullscreen.isFull ? <IconCompress size={16} /> : <IconExpand size={16} />}</span>
                  <span className="topbar-fs-text">{fullscreen.isFull ? '退出全屏' : '全屏'}</span>
                </button>
              )}
              {/* onClick 直接接 cycleTheme:**事件对象本身就是扩散的圆心来源** ——
                  它读 e.currentTarget.getBoundingClientRect() 拿按钮位置,
                  故别改成 `() => cycleTheme()`(那会丢掉事件,退化成瞬时切换)。
                  详见 hooks/useTheme。 */}
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
