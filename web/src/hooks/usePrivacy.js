import { useCallback, useEffect, useState } from 'react'

// 截图防泄(全局开关):状态打在 <html data-privacy> 上,CSS 据此模糊 .privacy 元素。
//
// 默认开启——敏感文字(昵称/UID)常驻模糊,由 App 顶栏品牌名「妙妙屋」点击切换解除/恢复。
// 不依赖任何窗口焦点/鼠标/触屏事件:截图/录屏/投屏都不触发 DOM 事件,故「默认即保护」最可靠,
// 手机与电脑行为也一致。
//
// 另有一处**临时解除**:展开账号切换器(桌面浮层 / 手机 sheet)期间自动解除,
// 收起即恢复 —— 无论是切成功了还是取消了,都一样(见 AccountSelect 的
// onDropdownOpenChange 与 App.jsx 的接线)。
//
// 为什么挑账号时要解除:列表里每行都是昵称 + UID + 真人头像,糊着根本认不出
// 该点哪一个,而「把手指伸向自己的账号」这个动作本身就说明屏幕前是本人。
// 为什么收起必须恢复(而不是留在解除态):挑完就该回到保护态 —— 切换成功时
// 顶栏已显示新账号、取消时页面还是原来那样,两种情况都不再需要看清列表,
// 没有理由让保护一直关着。这是**临时**解除,不是给用户加了一个关闭保护的开关。
//
// 首屏默认账号没设 PIN 时不解除:那条路径由 useAccounts 自行选定,不经
// AccountSelect,故保持保护态。
export function usePrivacy() {
  const [on, setOn] = useState(true)
  useEffect(() => {
    document.documentElement.toggleAttribute('data-privacy', on)
  }, [on])
  const toggle = useCallback(() => setOn((v) => !v), [])
  // setOff / setOn 是**单向**的(幂等),供「展开账号切换器时临时解除、收起恢复」使用。
  // 必须用单向的而不是 toggle:后者在状态与预期不一时会把它翻反,而这里要的是
  // 「展开=解除、收起=恢复」这种确定的映射,不是取反。
  const setOff = useCallback(() => setOn(false), [])
  const setOnPrivacy = useCallback(() => setOn(true), [])
  return { on, toggle, setOff, setOn: setOnPrivacy }
}
