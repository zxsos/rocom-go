import { useCallback, useEffect } from 'react'
import { flushSync } from 'react-dom'
import { useStoredJSON } from './useStoredState'

// 主题 = **风格(family)× 明暗(mode)** 两个轴,而不是一条扁平的清单。
//
//   风格:roco(洛克 = 图鉴 / 图鉴·夜)   classic(经典 = 白天 / 夜间)
//   明暗:auto(跟随系统) / light / dark
//
// 组合出四套具体主题 —— handbook / handbook-dark / light / dark,它们的令牌块都在 base.css 里,
// 本文件只负责「(风格, 明暗) → 该挂哪个 data-theme」这一件事。
//
// 为什么是两轴而不是把四套并列成一个清单:菜单要按「先风格、后明暗」两行呈现,
// 而**「跟随系统」必须能分别记住属于哪条风格** —— 经典的跟随系统(灰调)与洛克的跟随系统
// (图鉴)是两回事。扁平清单里 auto 只能挂一条风格,另一条风格就没有「跟随系统」可选了。
const FAMILIES = ['roco', 'classic']
// 「跟随系统」这一档。**保留成命名常量**而不是把 'auto' 散在四处字面量里:
// 它有三个语义相关的用处(清单成员、resolveTheme 的分支、监听是否挂载),散开之后
// 改一处漏一处不会报错,只会表现为「跟随系统突然不跟了」。
const AUTO = 'auto'
const MODES = [AUTO, 'light', 'dark']
// 默认:洛克风格 + 跟随系统明暗。于是新访客看到的是 —— 亮系统给图鉴、黑系统给图鉴·夜。
const DEFAULT = { fam: 'roco', mode: AUTO }
const DARK_MQ = '(prefers-color-scheme: dark)'
const REDUCED_MQ = '(prefers-reduced-motion: reduce)'

// 存储:localStorage('theme') 里存一个短串 "风格:明暗"(如 "roco:auto")。
// 单键而不是「风格一个键、明暗一个键」:两个键会出现「风格换了、明暗还没换」的中间态
// 被别处读到(刷新时机不巧就闪一下旧风格);单键是一次原子写入。
const encode = ({ fam, mode }) => `${fam}:${mode}`
const decode = (key) => {
  const [fam, mode] = String(key || '').split(':')
  return FAMILIES.includes(fam) && MODES.includes(mode) ? { fam, mode } : DEFAULT
}

// 上一版存的是五档扁平值,这里做一次性迁移(迁移后立刻按新格式写回,故只会命中一次)。
// auto 归到 roco —— 上一版的 auto 正是解析到图鉴家族的;四套主题名按所属风格拆开。
const LEGACY = {
  [AUTO]: { fam: 'roco', mode: AUTO },
  handbook: { fam: 'roco', mode: 'light' },
  'handbook-dark': { fam: 'roco', mode: 'dark' },
  light: { fam: 'classic', mode: 'light' },
  dark: { fam: 'classic', mode: 'dark' },
}
// sanitize 的返回值必须是**存进 localStorage 的那个字符串**(统一出口,免得状态里
// 一会儿是对象一会儿是字符串)。无法辨识的值回到默认 —— 与「没存过」同一条出口。
const sanitize = (v) => {
  if (typeof v === 'string') return encode(LEGACY[v] || decode(v))
  if (v && typeof v === 'object') return encode(decode(encode(v)))
  return encode(DEFAULT)
}

// (风格, 明暗) → 具体要挂的 data-theme 名。**本文件唯一的真相映射**,
// 其余地方(图标、文案、断言)都从它派生,避免「改了这里忘了那里」。
// auto 就地解析:洛克的 auto 给图鉴的日/夜两面,经典的 auto 给普通白天/夜间。
const resolveTheme = ({ fam, mode }) => {
  const dark = mode === AUTO ? window.matchMedia(DARK_MQ).matches : mode === 'dark'
  return fam === 'roco' ? (dark ? 'handbook-dark' : 'handbook') : (dark ? 'dark' : 'light')
}

// 把结果落到 <html data-theme> 上。
// 单独抽成函数是因为切主题那条路径要**脱离 React 同步**改这个属性:
// 见下面 choose() 里 startViewTransition 的注释。
const applyTheme = (t) => document.documentElement.setAttribute('data-theme', resolveTheme(t))

// 扩散切换:<html> 挂着这个 class 期间,base.css 让 view-transition 的新快照
// 以按钮为圆心做圆形 clip-path 展开(见 base.css「主题切换」段)。
const SPREAD = 'theme-spread'

// 能否播扩散动画。任一条不满足就退回原来的瞬时切换(功能不受影响,只是没动画):
//   - 浏览器没有 View Transitions API(Chrome 111+ / Safari 18+ 才有);
//   - 用户开了「减少动态效果」——整屏范围的颜色蔓延对光敏感人群是强刺激。
const canSpread = () =>
  typeof document.startViewTransition === 'function' &&
  !window.matchMedia(REDUCED_MQ).matches

export function useTheme() {
  // 状态就是那个短串;theme 是解出来的对象。这样依赖数组里可以放 key(稳定字符串),
  // 而不是每次渲染都新建的 theme 对象 —— 后者会让下面那个 effect 每渲染一次就解绑重挂一次。
  const [key, setKey] = useStoredJSON(localStorage, 'theme', encode(DEFAULT), sanitize)
  const theme = decode(key)

  useEffect(() => {
    applyTheme(theme)
    // 跟随系统时监听浏览器明暗变化,实时跟随(选了固定浅/深之后不再监听)。
    // 只跟「明暗」这一轴:风格是用户选的,系统不会替你换风格。
    if (theme.mode !== AUTO) return
    const mq = window.matchMedia(DARK_MQ)
    const onChange = () => applyTheme(theme)
    mq.addEventListener('change', onChange)
    return () => mq.removeEventListener('change', onChange)
  }, [key]) // eslint-disable-line react-hooks/exhaustive-deps

  // choose 直接「设成所点的那个组合」,没有循环 —— 菜单里点哪项就是哪项。
  // 收 click 事件是为了拿**扩散圆心**:事件对象的 currentTarget 就是被点的那个选项,
  // 于是圆从它那里张出来(和早先「点按钮切换」时从按钮张出来是同一个道理)。
  // 程序调用时不传事件,退化为瞬时切换(没有「用户点了哪儿」这回事)。
  const choose = useCallback(
    (next, e) => {
      const el = e?.currentTarget
      // 「屏幕上现在是哪套主题」以 data-theme 属性为准(而非 theme 状态):
      // 上一次过渡可能还在飞,那时属性已经是新的了。
      const shown = document.documentElement.getAttribute('data-theme')
      // 换了组合但**生效主题不变**时(亮系统里「洛克·跟随系统」→「洛克·浅色」),
      // 不值得为它冻屏 620ms —— 全程看不到任何变化,观感就是「点了没反应」
      // (过渡期间页面是快照,连菜单都得等过渡结束才更新)。直接瞬时切。
      // 判据是**主题名相等**,不是「明暗相同」:深色系统里 auto → 洛克·浅色 明暗也「变了」,
      // 但那两件事本来就必须播动画(颜色真的换了整套)。
      if (!el || !canSpread() || resolveTheme(next) === shown) {
        setKey(encode(next))
        return
      }

      const root = document.documentElement
      const r = el.getBoundingClientRect()
      const x = r.left + r.width / 2
      const y = r.top + r.height / 2
      // 半径取圆心到视口**四角**的最远距离:圆张满时才盖得住整屏。
      // 只算到中心/到某个角的话,对角的旧主题会留一条月牙形残边直到过渡结束。
      // 向上取整:半径宁可大一点也不留残边(多出来的那点像素在视口外)。
      const rad = Math.ceil(Math.hypot(
        Math.max(x, window.innerWidth - x),
        Math.max(y, window.innerHeight - y),
      ))
      root.style.setProperty('--theme-x', `${x}px`)
      root.style.setProperty('--theme-y', `${y}px`)
      root.style.setProperty('--theme-r', `${rad}px`)
      root.classList.add(SPREAD)

      const vt = document.startViewTransition(() => {
        // flushSync 把「切换已提交」这件事钉死:回调返回后浏览器就在下一帧截新快照,
        // 而 React 只承诺把 setKey 排进微任务/宏任务,**不承诺在下一帧之前提交**
        // —— 一旦这次渲染被拉长(并发渲染让出、主线程忙),新快照就是旧主题,
        // 表现为「圆张开了,里面铺开的还是原来那个颜色」。
        // (实测当前 Chromium 上就算不写也赶得及,但那是撞运气;写死成本为零。)
        flushSync(() => setKey(encode(next)))
        // 双保险:上面那条走的是 React → effect 的链路,而 effect 的时机由 React 调度;
        // 这里按新组合直接再落一次属性(幂等,值相同则无副作用)。
        applyTheme(next)
      })
      const cleanup = () => {
        root.classList.remove(SPREAD)
        root.style.removeProperty('--theme-x')
        root.style.removeProperty('--theme-y')
        root.style.removeProperty('--theme-r')
      }
      // ready 在过渡被抢占/跳过时 reject,finished 在正常结束与异常时都 settle。
      // 两条都挂上:漏了前者会让连续快速点击留下一个摘不掉的 class。
      vt.ready.catch(cleanup)
      vt.finished.then(cleanup, cleanup)
    },
    [setKey],
  )

  // solid:当前**实际生效**的主题名(把「跟随系统」解析掉之后的真名)。图标、文案、
  // 以及下面 toggle 的取反方向都按它算。
  const solid = resolveTheme(theme)

  // toggle:**一键切浅/深**,只动「明暗」那一轴,风格不变(顶栏那个图标按钮专干这个)。
  //
  // 两个刻意的点:
  //   · 往哪边切,按**当下实际生效**的明暗取反(solid),而不是按 mode 字段 ——
  //     选着「跟随系统」时,用户眼里看到的是浅的,点一下就该变深;若按 mode==='auto' 去算,
  //     这一下会先跳到某个跟他看到的不一样的档,观感是「点了没按我看到的来」。
  //   · 切完写的是明确的 light / dark(不再是 auto)—— 用户手动指定了明暗,
  //     之后系统换主题就不该再改他这一步的选择(与「跟随系统」语义一致)。
  const toggle = useCallback((e) => {
    const dark = solid === 'dark' || solid.endsWith('-dark')
    choose({ fam: theme.fam, mode: dark ? 'light' : 'dark' }, e)
  }, [solid, theme.fam, choose])

  // themeKey 与 theme 一起返回:theme 是每次渲染新建的对象,调用方若要放进依赖数组,
  // 用 themeKey(稳定字符串)或 theme.fam / theme.mode。solid 是当前**实际生效**的主题名,
  // 图标与文案按它取(所以「跟随系统」也能显示出当下真实的样子,而不是一个中性的显示器图标)。
  // (solid 必须是个局部常量 —— 上面的 toggle 依赖它;写成 return 里现算,
  //  那个变量在函数体里根本不存在,而静态断言只看模式、看不出来,lint 才拦得住。)
  return { theme, themeKey: key, solid, choose, toggle }
}
