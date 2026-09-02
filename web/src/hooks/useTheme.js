import { useCallback, useEffect } from 'react'
import { flushSync } from 'react-dom'
import { useStoredJSON } from './useStoredState'

// 主题模式:auto(跟随浏览器 prefers-color-scheme,默认)/ light / dark。
// 持久化到 localStorage('theme'),三态循环切换:auto → light → dark → auto。
// <html data-theme="light|dark"> 上挂实际生效的主题:auto 时由 matchMedia 决定并监听变化。
const AUTO = 'auto'
const sanitize = (v) => (v === 'light' || v === 'dark' || v === AUTO ? v : AUTO)
const DARK_MQ = '(prefers-color-scheme: dark)'
const REDUCED_MQ = '(prefers-reduced-motion: reduce)'

// 某模式**实际会渲染成**深色吗。auto 时问浏览器,否则按用户手选。
const darkOf = (mode) => (mode === AUTO ? window.matchMedia(DARK_MQ).matches : mode === 'dark')

// 把「模式」落到 <html data-theme> 上。
// 单独抽成函数是因为切主题那条路径要**脱离 React 同步**改这个属性:
// 见下面 cycle() 里 startViewTransition 的注释。
const applyTheme = (mode) =>
  document.documentElement.setAttribute('data-theme', darkOf(mode) ? 'dark' : 'light')

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
  const [theme, setTheme] = useStoredJSON(localStorage, 'theme', AUTO, sanitize)

  useEffect(() => {
    applyTheme(theme)
    // auto 模式下监听浏览器主题变化,实时跟随(切到固定 light/dark 后不再监听)
    if (theme !== AUTO) return
    const mq = window.matchMedia(DARK_MQ)
    const onChange = () => applyTheme(AUTO)
    mq.addEventListener('change', onChange)
    return () => mq.removeEventListener('change', onChange)
  }, [theme])

  // cycle 收 click 事件:拿按钮的屏幕位置当扩散圆心。
  // 直接 `onClick={cycle}` 即可(React 事件对象作为首个实参传入),键盘回车/空格
  // 触发的也是同一个 click 事件,故键盘切换同样从按钮处扩散;程序调用时不传事件,
  // 退化为瞬时切换(没有「用户点了哪儿」这回事)。
  const cycle = useCallback(
    (e) => {
      const next = theme === AUTO ? 'light' : theme === 'light' ? 'dark' : AUTO
      const el = e?.currentTarget
      // 「屏幕上现在是什么颜色」以 data-theme 属性为准(而非 theme 状态):
      // 上一次过渡可能还在飞,那时属性已经是新的了。
      const shownDark = document.documentElement.getAttribute('data-theme') !== 'light'
      // 换了模式但**颜色不变**时(如浅色浏览器里 auto → light),不值得为它冻屏
      // 620ms —— 全程看不到任何变化,观感就是「点了没反应」(过渡期间页面是快照,
      // 连按钮图标都得等过渡结束才变)。直接瞬时切,图标立刻跟着换。
      if (!el || !canSpread() || darkOf(next) === shownDark) {
        setTheme(next)
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
        // 而 React 只承诺把 setTheme 排进微任务/宏任务,**不承诺在下一帧之前提交**
        // —— 一旦这次渲染被拉长(并发渲染让出、主线程忙),新快照就是旧主题,
        // 表现为「圆张开了,里面铺开的还是原来那个颜色」。
        // (实测当前 Chromium 上就算不写也赶得及,但那是撞运气;写死成本为零。)
        flushSync(() => setTheme(next))
        // 双保险:上面那条走的是 React → effect 的链路,而 effect 的时机由 React 调度;
        // 这里按新模式直接再落一次属性(幂等,值相同则无副作用)。
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
    [theme, setTheme],
  )

  return { theme, cycle }
}
