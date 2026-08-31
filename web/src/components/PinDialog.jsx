import { useState, useEffect, useRef } from 'react'
import { verifyAccountPin, setAccountPin, deleteAccount } from '../api'

// PinDialog: 账号 PIN 保护弹窗,三种模式:
//   mode='verify'  — 切账号时校验 PIN,输入正确后调 onVerified()
//   mode='manage'  — 管理 PIN(设/改/清除),需输旧 PIN(已设时)+ 新 PIN 两次
//   mode='delete'  — 删账号,需输 PIN 确认,调 onDeleted()
// Props: { account, name, hasPin, mode, onClose, onVerified, onDeleted, onSaved }
//
// 这是**用户自助**路径。管理员代管走 pages/admin/PinCard.jsx(带 admin token,
// 后端免旧 PIN),不经过本组件。
export function PinDialog({ account, name, hasPin, mode, onClose, onVerified, onDeleted, onSaved }) {
  const [oldPin, setOldPin] = useState('')
  const [newPin, setNewPin] = useState('')
  const [confirmPin, setConfirmPin] = useState('')
  const [pin, setPin] = useState('')
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const inputRef = useRef(null)

  useEffect(() => { inputRef.current?.focus() }, [])

  const title = mode === 'verify' ? '输入 PIN'
    : mode === 'manage' ? (hasPin ? '修改 PIN' : '设置 PIN')
    : mode === 'delete' ? '删除账号'
    : ''

  const submit = async () => {
    setErr('')
    setBusy(true)
    try {
      if (mode === 'verify') {
        await verifyAccountPin(account, pin)
        sessionStorage.setItem('pin:' + account, '1')
        onVerified?.()
      } else if (mode === 'manage') {
        if (!newPin) { setErr('请输入新 PIN'); return }
        if (newPin !== confirmPin) { setErr('两次输入不一致'); return }
        await setAccountPin(account, hasPin ? oldPin : '', newPin)
        sessionStorage.setItem('pin:' + account, '1')
        // 账号的 hasPin 由父级的 accounts 列表持有,改完必须让外部重拉,
        // 否则下拉里仍是「修改 PIN」且锁图标不消失(旧的 hasPin 一直生效)。
        onSaved?.()
        onClose?.()
      } else if (mode === 'delete') {
        await deleteAccount(account, pin)
        sessionStorage.removeItem('pin:' + account)
        onDeleted?.()
      }
    } catch (e) {
      // 后端对旧 PIN 校验失败返回的是裸英文 "wrong old pin"(见 internal/server/api_account.go
      // 的 handleAccountPin),直接透给用户很难看,这里翻成中文。
      setErr(e.status === 401 ? '旧 PIN 不正确' : (e.message || '操作失败'))
    } finally {
      setBusy(false)
    }
  }

  // 清除 PIN:**必须带旧 PIN**。
  //
  // 原先这里传空旧 PIN 做「一键清除」,但后端 handleAccountPin 对非管理员一律要求
  // verifyPin(oldPin)(internal/server/api_account.go),空串恒失败 → 401;
  // 而「修改」路径又强制新 PIN 非空(上面 submit),两条路都表达不了「清除」 ——
  // 结果就是:用户一旦设了 PIN 就再也清不掉,只能求管理员。
  // 管理员代清走 admin/PinCard 的「清 PIN」(带 admin token,后端免旧 PIN),
  // 故这里要求旧 PIN 不会让管理员失去任何能力。
  const clearPin = async () => {
    setErr('')
    if (oldPin.length < 4) { setErr('清除 PIN 需先输入旧 PIN'); return }
    setBusy(true)
    try {
      await setAccountPin(account, oldPin, '')
      // 清掉本会话的解锁标记:它只对「这个账号当前有 PIN」成立,
      // PIN 没了再留着会让下次切回来时走一条已不存在的校验路径。
      sessionStorage.removeItem('pin:' + account)
      onSaved?.()
      onClose?.()
    } catch (e) {
      setErr(e.status === 401 ? '旧 PIN 不正确' : (e.message || '清除失败'))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="pin-backdrop" onClick={(e) => { if (e.target === e.currentTarget) onClose?.() }}>
      <div className="pin-dialog">
        <div className="pin-title">{title}</div>
        {name && <div className="pin-acct">{name}</div>}
        {err && <div className="pin-err">{err}</div>}
        {mode === 'verify' && (
          <>
            <input ref={inputRef} className="input pin-input" type="password" inputMode="numeric"
              placeholder="4-6 位数字" value={pin} maxLength={6}
              onChange={(e) => setPin(e.target.value.replace(/\D/g, ''))}
              onKeyDown={(e) => { if (e.key === 'Enter') submit() }} />
            <button className="btn primary pin-submit" onClick={submit} disabled={busy || pin.length < 4}>确认</button>
          </>
        )}
        {mode === 'manage' && (
          <>
            {hasPin && (
              <input ref={inputRef} className="input pin-input" type="password" inputMode="numeric"
                placeholder="旧 PIN" value={oldPin} maxLength={6}
                onChange={(e) => setOldPin(e.target.value.replace(/\D/g, ''))} />
            )}
            <input className="input pin-input" type="password" inputMode="numeric"
              ref={!hasPin ? inputRef : undefined}
              placeholder="新 PIN(4-6 位数字)" value={newPin} maxLength={6}
              onChange={(e) => setNewPin(e.target.value.replace(/\D/g, ''))} />
            <input className="input pin-input" type="password" inputMode="numeric"
              placeholder="再次输入新 PIN" value={confirmPin} maxLength={6}
              onChange={(e) => setConfirmPin(e.target.value.replace(/\D/g, ''))}
              onKeyDown={(e) => { if (e.key === 'Enter') submit() }} />
            <div className="pin-actions">
              {hasPin && (
                <button className="btn pin-clear-btn" onClick={clearPin} disabled={busy || oldPin.length < 4}
                  title={oldPin.length < 4 ? '需先输入旧 PIN 才能清除' : '清除后切到该账号不再需要 PIN'}>清除 PIN</button>
              )}
              <button className="btn primary pin-submit" onClick={submit} disabled={busy || newPin.length < 4 || newPin !== confirmPin}>保存</button>
            </div>
          </>
        )}
        {mode === 'delete' && (
          <>
            <div className="pin-warn">将永久删除该账号的全部宠物、事件、精灵蛋等数据,不可恢复。</div>
            <input ref={inputRef} className="input pin-input" type="password" inputMode="numeric"
              placeholder="输入 PIN 确认删除" value={pin} maxLength={6}
              onChange={(e) => setPin(e.target.value.replace(/\D/g, ''))}
              onKeyDown={(e) => { if (e.key === 'Enter') submit() }} />
            <div className="pin-actions">
              <button className="btn" onClick={onClose} disabled={busy}>取消</button>
              <button className="btn pin-danger-btn" onClick={submit} disabled={busy || pin.length < 4}>确认删除</button>
            </div>
          </>
        )}
      </div>
    </div>
  )
}
