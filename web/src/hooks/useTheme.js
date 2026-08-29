import { useCallback, useEffect } from 'react'
import { useStoredJSON } from './useStoredState'

// 主题模式:auto(跟随浏览器 prefers-color-scheme,默认)/ light / dark。
// 持久化到 localStorage('theme'),三态循环切换:auto → light → dark → auto。
// <html data-theme="light|dark"> 上挂实际生效的主题:auto 时由 matchMedia 决定并监听变化。
const AUTO = 'auto'
const sanitize = (v) => (v === 'light' || v === 'dark' || v === AUTO ? v : AUTO)

export function useTheme() {
  const [theme, setTheme] = useStoredJSON(localStorage, 'theme', AUTO, sanitize)

  useEffect(() => {
    const mq = window.matchMedia('(prefers-color-scheme: dark)')
    const apply = (dark) => document.documentElement.setAttribute('data-theme', dark ? 'dark' : 'light')
    // 算出实际生效主题:auto 时看浏览器,否则用户手选
    if (theme !== AUTO) {
      apply(theme === 'dark')
      return
    }
    apply(mq.matches)
    // auto 模式下监听浏览器主题变化,实时跟随(切到固定 light/dark 后不再监听)
    const onChange = (e) => apply(e.matches)
    mq.addEventListener('change', onChange)
    return () => mq.removeEventListener('change', onChange)
  }, [theme])

  const cycle = useCallback(
    () => setTheme((t) => (t === AUTO ? 'light' : t === 'light' ? 'dark' : AUTO)),
    [setTheme],
  )

  return { theme, cycle }
}
