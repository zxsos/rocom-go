import React from 'react'
import { uidOf } from '../data/nav'
import RankTitle from './RankTitle'
import AccountAvatar from './AccountAvatar'
import { IconCheck } from './svg'

// AccountItem 单行账号行:桌面浮层与手机 sheet **共用**的唯一行渲染源。
// 两者只有行高/圆角/字号不同(由各自容器的 CSS 决定),内容结构完全一致,
// 故不会出现「改了桌面忘了手机」的视觉漂移。
//
// 布局:头像 → 昵称(可截断) → 称号 → PIN 锁 → UID → 对勾(仅选中项)。
// 两个徽标(称号、锁)跟在昵称**后面**而不是前面:列表里各行昵称左对齐在同一列,
// 徽标放前面会把各行的名字推得参差不齐;放后面则名字始终齐平,徽标长短不影响扫视。
//
// touch 决定选中时机(这是桌面/手机唯一的**行为**差异):
//   桌面走 onMouseDown(与通用 Dropdown 一致,按下即选中,不等 mouseup,手感更快);
//   手机走 onClick —— sheet 支持下拉手势关闭,mousedown 会在起手那一瞬间就误选。
export default function AccountItem({ account, cur, hi, onChoose, onHover, touch }) {
  return (
    <li
      role="option"
      aria-selected={cur}
      className={'account-item' + (cur ? ' cur' : '') + (hi ? ' hi' : '')}
      onMouseDown={touch ? undefined : (e) => { e.preventDefault(); onChoose() }}
      onClick={touch ? onChoose : undefined}
      onMouseEnter={onHover}
    >
      <AccountAvatar account={account.account} name={account.name} online={account.online} avatar={account.avatar} pin={account.hasPin} />
      <span className="privacy account-item-name">{account.name}</span>
      <RankTitle title={account.title} />
      {/* PIN 锁已移到头像左下角(AccountAvatar 的 pin 参数),这里不再单独渲染 ——
          它挂在这一层时会被伸缩的昵称推来推去。 */}
      <span className="privacy account-item-uid">UID:{uidOf(account.account)}</span>
      {/* 对勾槽位**恒定占位**(未选中也占同样的宽):否则选中行的昵称会比其它行短一截,
          各行名字对不齐,扫视反而更慢。选中与否只改槽内的透明度与缩放。 */}
      <span className="account-item-check" aria-hidden={!cur}>
        <IconCheck size={14} />
      </span>
    </li>
  )
}
