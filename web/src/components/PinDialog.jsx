import { useState, useEffect, useRef } from 'react'
import { verifyAccountPin, setAccountPin, deleteAccount } from '../api'

// PinDialog: 账号 PIN 保护弹窗,三种模式:
//   mode='verify'  — 切账号时校验 PIN,输入正确后调 onVerified()
//   mode='manage'  — 管理 PIN(改/设),需输旧 PIN(已设时)+ 新 PIN 两次
//   mode='delete'  — 删账号,需输 PIN 确认,调 onDeleted()
// Props: { account, name, hasPin, mode, onClose, onVerified, onDeleted }
export function PinDialog({ account, name, hasPin, mode, onClose, onVerified, onDeleted }) {
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
        onClose?.()
      } else if (mode === 'delete') {
        await deleteAccount(account, pin)
        sessionStorage.removeItem('pin:' + account)
        onDeleted?.()
      }
    } catch (e) {
      setErr(e.message || '操作失败')
    } finally {
      setBusy(false)
    }
  }

  // 管理员删除 PIN(一键清除,免旧 PIN)
  const clearPin = async () => {
    setErr('')
    setBusy(true)
    try {
      await setAccountPin(account, '', '')
      sessionStorage.removeItem('pin:' + account)
      onClose?.()
    } catch (e) {
      setErr(e.message || '清除失败')
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
                <button className="btn pin-clear-btn" onClick={clearPin} disabled={busy}>清除 PIN</button>
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
