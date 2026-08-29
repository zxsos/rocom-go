import React, { useState } from 'react'
import { setAccountPin, deleteAccount } from '../../api'
import { confirmDialog } from '../../components/confirm'
import { uidOf } from '../../data/nav'

// PinCard 账号 PIN 管理 + 账号删除(管理员可代设,免旧 PIN)。
// 为每个账号设置/清除 PIN(隐私保护:切到该账号需输 PIN)。
// 用户自行修改需旧 PIN;无 PIN 的账号可直接删除,有 PIN 的需 PIN 或管理员权限。
// PIN 输入态是本卡片自持,账号列表由 Admin 持有并在变更后回调 onAccountsChanged 刷新。
export default function PinCard({ accounts, onAccountsChanged }) {
  const [pinErr, setPinErr] = useState('')
  const [pinMsg, setPinMsg] = useState('')
  const [pinEditing, setPinEditing] = useState(null) // 正在设 PIN 的账号 key
  const [pinValue, setPinValue] = useState('')

  // 管理员设 PIN(免旧 PIN):设/改
  const adminSetPin = async (account) => {
    setPinErr(''); setPinMsg('')
    if (!pinValue || pinValue.length < 4) { setPinErr('PIN 至少 4 位'); return }
    try {
      await setAccountPin(account, '', pinValue)
      setPinMsg(account + ' PIN 已设置')
      setPinEditing(null); setPinValue('')
      onAccountsChanged()
    } catch (err) { setPinErr(err.message || '设置失败') }
  }

  // 管理员清 PIN(免旧 PIN)
  const adminClearPin = async (account) => {
    setPinErr(''); setPinMsg('')
    try {
      await setAccountPin(account, '', '')
      setPinMsg(account + ' PIN 已清除')
      onAccountsChanged()
    } catch (err) { setPinErr(err.message || '清除失败') }
  }

  // 管理员删账号(免 PIN)
  const adminDelete = async (account) => {
    setPinErr(''); setPinMsg('')
    if (!await confirmDialog({
      message: '确认删除账号 ' + account + ' 的全部数据?此操作不可恢复。',
      okText: '删除', danger: true,
    })) return
    try {
      await deleteAccount(account)
      setPinMsg(account + ' 已删除')
      onAccountsChanged()
    } catch (err) { setPinErr(err.message || '删除失败') }
  }

  return (
    <div className="admin-card admin-rules">
      <h3>账号 PIN 管理</h3>
      <p className="admin-hint">
        为每个账号设置/清除 PIN(隐私保护:切到该账号需输 PIN)。
        管理员可代设,用户自行修改需旧 PIN。无 PIN 的账号可直接删除,有 PIN 的需 PIN 或管理员权限。
      </p>
      {pinErr && <p className="admin-error">{pinErr}</p>}
      {pinMsg && <p className="admin-hint" style={{ color: 'var(--green, #4caf50)' }}>{pinMsg}</p>}
      <ul className="admin-rule-list">
        {accounts.map((a) => (
          <li key={a.account}>
            <span className={'rule-badge ' + (a.hasPin ? 'black' : 'white')} title={a.hasPin ? '已设 PIN' : '未设 PIN'}>
              {a.hasPin ? '锁' : '开'}
            </span>
            <code className="rule-account">{a.name || a.account}</code>
            <span className="rule-note">UID:{uidOf(a.account)}</span>
            {pinEditing === a.account ? (
              <>
                <input className="input admin-pin-input" type="password" inputMode="numeric"
                  placeholder="4-6 位" value={pinValue} maxLength={6}
                  onChange={(e) => setPinValue(e.target.value.replace(/\D/g, ''))}
                  onKeyDown={(e) => { if (e.key === 'Enter') adminSetPin(a.account) }} />
                <button className="btn small" onClick={() => adminSetPin(a.account)}>确定</button>
                <button className="btn ghost" onClick={() => { setPinEditing(null); setPinValue('') }}>取消</button>
              </>
            ) : (
              <>
                <button className="btn small" onClick={() => { setPinEditing(a.account); setPinValue(''); setPinErr(''); setPinMsg('') }}>
                  {a.hasPin ? '改 PIN' : '设 PIN'}
                </button>
                {a.hasPin && <button className="btn ghost" onClick={() => adminClearPin(a.account)}>清 PIN</button>}
                <button className="btn ghost" onClick={() => adminDelete(a.account)}>删除账号</button>
              </>
            )}
          </li>
        ))}
      </ul>
    </div>
  )
}
