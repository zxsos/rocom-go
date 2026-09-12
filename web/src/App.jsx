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
import { IconSun, IconMoon, IconMonitor, IconBook, IconBookNight, IconExpand, IconCompress } from './components/svg'
import MapEngineProvider from './pages/map/MapEngineProvider'
import ThemeMenu from './components/ThemeMenu'
import Splash from './components/Splash'

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

  const { accounts, account, current, accountName, loaded: accountsLoaded, requestAccount, selectAccount, refreshAccounts } =
    useAccounts(onPinRequired)
  const { theme, solid, choose, toggle } = useTheme()
  // 主题菜单只由**版本号**打开(顶栏那个图标按钮是纯切换,不弹菜单,见下面两处 JSX)。
  // 于是开合就是一个布尔,没有「从哪个入口打开的」要记。
  const [themeMenuOpen, setThemeMenuOpen] = useState(false)
  const closeThemeMenu = useCallback(() => setThemeMenuOpen(false), [])
  // 选主题:choose 需要拿到 click 事件当**扩散圆心**(见 hooks/useTheme),选完顺手收起菜单。
  const pickTheme = useCallback((next, e) => { choose(next, e); closeThemeMenu() }, [choose, closeThemeMenu])
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
  // 图标请求是否了结 —— 开屏动画的进场判据之一(见 components/Splash.jsx)。
  const [iconsLoaded, setIconsLoaded] = useState(false)

  // 全局固定图标只随游戏版本变,拉一次即可。
  // 用 finally 而非 then:请求失败也要放行开屏,不然后端不通时开屏会一直挂在「加载中」。
  useEffect(() => {
    let alive = true
    getIcons()
      .then((d) => { if (alive) setIcons(d || { stat: {} }) })
      .catch(() => {})
      .finally(() => { if (alive) setIconsLoaded(true) })
    return () => { alive = false }
  }, [])

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

  // 图标按**实际生效**的主题(solid)取,所以「跟随系统」也显示当下真实的明暗;
  // 标题则描述用户选的组合(风格 · 明暗),两者各自回答一个不同的问题。
  const themeIcon = SOLID_ICON[solid] || <IconMonitor size={17} />
  const solidDark = solid === 'dark' || solid.endsWith('-dark')
  const themeTitle = `主题:${FAM_LABEL[theme.fam]}·${MODE_LABEL[theme.mode]}(点击切${solidDark ? '浅色' : '深色'})`

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
                  既是给玩家看的「跑的是哪一版」,也是排障时第一句要问的信息。
                  它**同时是主题入口**(点击弹菜单)—— 但刻意不做出按钮的样子:仍然只是一行小字,
                  尺寸被窄屏顶栏的布局算着(shell.css 的 .topbar-ver 有说明,别给它加内边距)。 */}
              <div className="ver-wrap">
                <button
                  type="button"
                  className="topbar-ver"
                  onClick={() => setThemeMenuOpen((o) => !o)}
                  aria-haspopup="dialog"
                  aria-expanded={themeMenuOpen}
                  title="应用版本(赛季.大更新.小更新)· 点击选择主题"
                >
                  v{__APP_VERSION__}
                </button>
                {themeMenuOpen && (
                  <ThemeMenu theme={theme} onPick={pickTheme} onClose={closeThemeMenu} />
                )}
              </div>
              <TopNav />
              {fullscreen.supported && (
                <button type="button" className={'topbar-fs' + (fullscreen.isFull ? ' on' : '')}
                  onClick={fullscreen.toggle}
                  title={fullscreen.isFull ? '退出网页全屏' : '网页全屏'}>
                  <span className="topbar-fs-icon">{fullscreen.isFull ? <IconCompress size={16} /> : <IconExpand size={16} />}</span>
                  <span className="topbar-fs-text">{fullscreen.isFull ? '退出全屏' : '全屏'}</span>
                </button>
              )}
              {/* 主题图标:它**不是菜单入口**,只做一键切浅/深(选风格与「跟随系统」都在
                  版本号那个菜单里)。两个按钮职责分开,顶栏就只有一个入口要找。
                  切的时候按**实际生效**的明暗取反 —— 选着「跟随系统」时也符合眼里看到的。
                  onClick 直接接 toggle:**事件对象本身就是扩散的圆心来源**,
                  写成 `() => toggle()` 会丢掉事件、退化成瞬时切换。详见 hooks/useTheme。 */}
              <button type="button" className="topbar-fs"
                onClick={toggle}
                title={themeTitle}>
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
          {/* 开屏动画:盖在最上层(--z-loading 压过 PIN 弹窗),1.7s 后自己卸载。
              放在这里而非 main.jsx 的 Suspense 外层,是因为它的进场判据要读 icons 与
              accounts 两个首屏请求的状态,而这两个请求都发生在 App 内。 */}
          <Splash ready={iconsLoaded && accountsLoaded} />
        </IconsContext.Provider>
      </AccountNameContext.Provider>
    </AccountContext.Provider>
  )
}

// 主题按钮的图标:按**实际生效**的主题名取(solid,由 useTheme 从「风格×明暗」解析出来)。
// 取「生效值」而不是「用户选的那档」是刻意的:选着「跟随系统」时,图标要显示当下真实的明暗
// (书是摊开还是合上、太阳还是月亮),否则一个中性的显示器图标回答不了「现在是浅还是深」。
// 表里没有的值回落显示器图标(理论上不可能命中,兜底而已)。
// 风格 / 明暗的文案给 title 用:标题说的是「你选了什么」,图标说的是「现在长什么样」。
const SOLID_ICON = {
  handbook: <IconBook size={17} />,
  'handbook-dark': <IconBookNight size={17} />,
  light: <IconSun size={17} />,
  dark: <IconMoon size={17} />,
}
const FAM_LABEL = { roco: '洛克', classic: '经典' }
const MODE_LABEL = { auto: '跟随系统', light: '浅色', dark: '深色' }

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
