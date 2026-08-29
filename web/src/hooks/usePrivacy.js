import { useCallback, useEffect, useState } from 'react'

// 截图防泄(全局开关):状态打在 <html data-privacy> 上,CSS 据此模糊 .privacy 元素。
//
// 默认开启——敏感文字(昵称/UID)常驻模糊,由 App 顶栏品牌名「妙妙屋」点击切换解除/恢复。
// 不依赖任何窗口焦点/鼠标/触屏事件:截图/录屏/投屏都不触发 DOM 事件,故「默认即保护」最可靠,
// 手机与电脑行为也一致。
export function usePrivacy() {
  const [on, setOn] = useState(true)
  useEffect(() => {
    document.documentElement.toggleAttribute('data-privacy', on)
  }, [on])
  const toggle = useCallback(() => setOn((v) => !v), [])
  return { on, toggle }
}
