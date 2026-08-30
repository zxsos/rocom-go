import React from 'react'
import { useDropdown } from '../hooks/useDropdown'
import { uidOf } from '../data/nav'
import RankTitle from './RankTitle'
import { IconLock } from './svg'

// AccountSelect 自定义账号下拉:原生 <option> 不支持内嵌 <img>,无法显示用户上传的
// login.svg/logout.svg 状态图标——故用 div 模拟 dropdown。键鼠/触屏均可操作:
// 鼠标点击展开/选条;键盘 ↑↓ 切换、Enter 选择、Esc 关闭、Tab 收起。点外部自动收起
// (交互行为见 hooks/useDropdown.js,与通用 Dropdown 共用)。
// 仍复用 .account-wrap/.account-state/.account-select 容器样式,只是下拉浮层是自绘。
export default function AccountSelect({ accounts, current, onChange, onManagePin, onDeleteAccount }) {
  const selIdx = current ? accounts.findIndex((a) => a.account === current.account) : -1
  const { open, setOpen, hi, setHi, rootRef, ulRef, onKeyDown, pickAt } = useDropdown({
    count: accounts.length,
    selectedIndex: selIdx,
    onPick: (i) => { if (accounts[i].account !== current?.account) onChange(accounts[i].account) },
  })

  return (
    <div className="account-wrap" ref={rootRef} onKeyDown={onKeyDown}>
      <button
        type="button"
        className={'select account-select account-trigger' + (open ? ' open' : '')}
        onClick={() => setOpen((o) => !o)}
        title="切换账号(玩家)"
        aria-haspopup="listbox"
        aria-expanded={open}
      >
        {current && (
          <img className="account-state" src={current.online ? '/login.svg' : '/logout.svg'}
            alt="" draggable={false} title={current.online ? '在线' : '离线'} />
        )}
        {/* 昵称与 UID 分成两个 span:窄屏靠 .acct-uid 把 UID 那截藏掉(见 shell.css)——
            顶栏就那么宽,一整条塞进去只能省略号收场,而昵称已足够认出是谁。
            两截都带 .privacy,与全局截图防泄一致(见 shell.css 的 html[data-privacy])。 */}
        <span className="privacy account-trigger-name">
          {current ? <><span className="acct-name">{current.name}</span><span className="acct-uid"> (UID:{uidOf(current.account)})</span></> : '选择账号…'}
        </span>
        {current && <RankTitle title={current.title} />}
        {current?.hasPin && <span className="account-pin-mark" title="已设 PIN 保护"><IconLock size={12} /></span>}
        <span className="account-caret">▾</span>
      </button>
      {open && (
        <ul className="account-dropdown" ref={ulRef} role="listbox">
          {accounts.map((a, i) => (
            <AccountItem
              key={a.account}
              account={a}
              cur={current && a.account === current.account}
              hi={i === hi}
              onChoose={() => pickAt(i)}
              onHover={() => setHi(i)}
            />
          ))}
          {/* 当前账号的 PIN 管理 + 删除入口 */}
          {current && (
            <li className="account-item account-actions" onMouseEnter={() => setHi(-1)}>
              <button className="btn small account-action-btn" onClick={(e) => {
                e.stopPropagation(); setOpen(false)
                onManagePin?.(current)
              }}>管理 PIN</button>
              <button className="btn small account-action-btn account-del-btn" onClick={(e) => {
                e.stopPropagation(); setOpen(false)
                onDeleteAccount?.(current)
              }}>删除账号</button>
            </li>
          )}
        </ul>
      )}
    </div>
  )
}

// AccountItem 下拉里的单条账号。
function AccountItem({ account, cur, hi, onChoose, onHover }) {
  return (
    <li
      role="option"
      aria-selected={cur}
      className={'account-item' + (cur ? ' cur' : '') + (hi ? ' hi' : '')}
      onMouseDown={(e) => { e.preventDefault(); onChoose() }}
      onMouseEnter={onHover}
    >
      <img className="account-state" src={account.online ? '/login.svg' : '/logout.svg'}
        alt="" draggable={false} title={account.online ? '在线' : '离线'} />
      <span className="privacy account-item-name">{account.name}</span>
      <RankTitle title={account.title} />
      {account.hasPin && <span className="account-item-pin" title="已设 PIN"><IconLock size={11} /></span>}
      <span className="muted privacy account-item-uid">UID:{uidOf(account.account)}</span>
    </li>
  )
}
