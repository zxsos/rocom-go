import React, { useEffect, useRef } from 'react'
import { useDropdown } from '../hooks/useDropdown'
import { useMediaQuery } from '../hooks/useMediaQuery'
import { uidOf } from '../data/nav'
import RankTitle from './RankTitle'
import AccountAvatar from './AccountAvatar'
import AccountItem from './AccountItem'
import AccountSheet from './AccountSheet'

// 断点必须与 CSS 同步:styles/shell.css 的 @media (max-width: 760px) 与
// styles/base.css 的移动端触控基线都用它。改一处要同步另一处。
// 它决定**渲染哪套 DOM**(锚定浮层 / 底部 sheet),CSS 只决定各自的样式。
const MOBILE_Q = '(max-width: 760px)'

// AccountSelect 账号切换器:桌面端锚定浮层,手机端底部 sheet。
//
// 为什么不是一套 DOM 靠 CSS 变形态 —— sheet 必须 createPortal 到 document.body
// (.topbar 的 backdrop-filter 会成为 fixed 后代的包含块,详见 AccountSheet.jsx),
// 而 portal 与否是 DOM 结构问题,CSS 改不了。
//
// 但**行为只有一份**(useDropdown)、**行渲染只有一份**(AccountItem):
// 两套 DOM 只是同一套内容的两种呈现,不会出现改了桌面忘了手机。
//
// 键鼠/触屏均可操作:点击展开/选条;键盘 ↑↓ 切换、Enter 选择、Esc 关闭、Tab 收起,
// 点外部自动收起(见 hooks/useDropdown.js,与通用 Dropdown 共用)。
// 手机端 sheet 走 extraRef 纳入「点内部」判断,否则点它自己也会被当成点外部。
export default function AccountSelect({ accounts, current, onChange, onManagePin, onDeleteAccount, onDropdownOpenChange }) {
  const selIdx = current ? accounts.findIndex((a) => a.account === current.account) : -1
  // sheet 根节点的 ref:传给 useDropdown 让它不被判成「点外部」。桌面端从不挂载,
  // .current 恒为 null,useOutsideClick 里的 extraRef 判据自然短路。
  const sheetRef = useRef(null)
  const { open, setOpen, hi, setHi, rootRef, ulRef, onKeyDown, pickAt } = useDropdown({
    count: accounts.length,
    selectedIndex: selIdx,
    extraRef: sheetRef,
    onPick: (i) => { if (accounts[i].account !== current?.account) onChange(accounts[i].account) },
  })
  const mobile = useMediaQuery(MOBILE_Q)

  // 开合状态上报给父级,供「临时解除截图遮罩」用(见 hooks/usePrivacy)。
  //
  // 挂在这里而不是各退出路径上散着调:收起有五条路径(选中、Esc、点外部、点遮罩、
  // 点关闭按钮),漏掉任何一条遮罩就留下了。而它们最终都收敛到 open 变 false,
  // 监听 open 是唯一不会漏的写法。
  useEffect(() => { onDropdownOpenChange?.(open) }, [open, onDropdownOpenChange])

  const rows = accounts.map((a, i) => (
    <AccountItem
      key={a.account}
      account={a}
      cur={current && a.account === current.account}
      hi={i === hi}
      touch={mobile}
      onChoose={() => pickAt(i)}
      onHover={() => setHi(i)}
    />
  ))

  // 当前账号的 PIN 管理 + 删除入口。浮层与 sheet 共用(只有外层 CSS 不同)。
  const actions = current && (
    <li className="account-actions" onMouseEnter={() => setHi(-1)}>
      <button className="btn small account-action-btn" onClick={() => {
        setOpen(false)
        onManagePin?.(current)
      }}>管理 PIN</button>
      <button className="btn small account-action-btn account-del-btn" onClick={() => {
        setOpen(false)
        onDeleteAccount?.(current)
      }}>删除账号</button>
    </li>
  )

  return (
    <div className="account-wrap" ref={rootRef} onKeyDown={onKeyDown}>
      <button
        type="button"
        className={'select account-select account-trigger' + (open ? ' open' : '')}
        onClick={() => setOpen((o) => !o)}
        // 手机端触发条只画头像,昵称得靠 title / aria-label 补上:
        // 长按与读屏用户仍然能知道当前是哪个账号。
        title={current ? `切换账号(当前:${current.name})` : '切换账号(玩家)'}
        aria-label={current ? `切换账号,当前 ${current.name}` : '切换账号'}
        aria-haspopup="listbox"
        aria-expanded={open}
      >
        {/* 顺序:头像 → 称号 → 昵称 → UID。
            徽章在最左:它是唯一「一眼可辨」的元素,靠颜色和首字就能区分账号,
            不用逐条读名字;也把整条的视觉重心钉在左边,昵称再长也不影响定位。
            称号跟在头像后、昵称**之前**:它是不随昵称长短变化的固定标识,
            挤在昵称和 UID 中间会被伸缩的文本推来推去。
            昵称与 UID 都带 .privacy,与全局截图防泄一致(见 shell.css 的 html[data-privacy])。

            PIN 锁**不在这一层**:它挂在头像的左下角(见 AccountAvatar 的 pin 参数),
            因为它是头像的属性而不是账号名的一部分 —— 挪到头像上之后,
            昵称再长再短都不会把它推来推去。

            手机端**不显示昵称**(mobile 时不渲染,见下面 !mobile 的条件):
            顶栏横向就那么点地方,昵称一长就挤压 brand 与右侧按钮;而认账号
            已经有三重保障了 —— 带色相的头像、称号、以及这里保留的 UID。
            昵称本身没丢:它进了 button 的 title 与 aria-label,长按与读屏照样拿得到,
            点开 sheet 里每行也写得清清楚楚。 */}
        {current && <AccountAvatar account={current.account} name={current.name} online={current.online} avatar={current.avatar} pin={current.hasPin} />}
        {current && <RankTitle title={current.title} />}
        {!mobile && (
          <span className="privacy account-trigger-name">
            {current ? <span className="acct-name">{current.name}</span> : '选择账号…'}
          </span>
        )}
        {current && <span className="privacy acct-uid">UID:{uidOf(current.account)}</span>}
        {/* 手机端无账号时的兜底:上面几项在 current 为空时都不渲染,
            不补这一句整条就是个空按钮(只有一个箭头),看不出点它做什么。 */}
        {mobile && !current && <span className="account-trigger-name">选择账号…</span>}
        <span className="account-caret">▾</span>
      </button>

      {mobile ? (
        <AccountSheet open={open} onClose={() => setOpen(false)} sheetRef={sheetRef} title="切换账号">
          <ul className="account-list" ref={ulRef} role="listbox">
            {rows}
            {actions}
          </ul>
        </AccountSheet>
      ) : open && (
        <ul className="account-list account-dropdown" ref={ulRef} role="listbox">
          {rows}
          {actions}
        </ul>
      )}
    </div>
  )
}
