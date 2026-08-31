import { useEffect, useState } from 'react'

// useMediaQuery 订阅一条 CSS 媒体查询的匹配结果,供 JS 决定渲染哪套 DOM。
//
// 断点必须与 CSS 同步:本项目响应式断点是 760px(见 styles/shell.css 的
// @media (max-width: 760px) 与 styles/base.css 的移动端触控基线)。CSS 决定样式、
// JS 决定渲染哪套 DOM,两边不一致会「两套都渲染」或「两套都不渲染」。
// 改一处必须同步另一处(AccountSelect 的 MOBILE_Q 是本 hook 的唯一调用点)。
//
// 用 matchMedia 的 change 事件而不是 resize 监听:手机端地址栏收放会连续触发
// resize,而 change 只在跨越断点那一刻触发一次,省掉大量无谓重排。
export function useMediaQuery(query) {
  const [matches, setMatches] = useState(() => window.matchMedia(query).matches)

  useEffect(() => {
    const mql = window.matchMedia(query)
    // 再取一次:首次渲染与订阅生效之间断点可能已经变过
    setMatches(mql.matches)
    const onChange = (e) => setMatches(e.matches)
    mql.addEventListener('change', onChange)
    return () => mql.removeEventListener('change', onChange)
  }, [query])

  return matches
}
