import React, { useCallback, useContext, useEffect, useState } from 'react'
import { getMerchantSub, setMerchantSub, delMerchantSub } from '../../api'
import { AccountContext } from '../../context'
import { maskEmail } from '../../utils/format'
import { SUB_PRESETS, normKws } from './format'

// SubCard 新货邮件提醒订阅:填任意邮箱(可选关键词,空=全部),每轮新货上架后后端发邮件。
// 订阅按当前登录账号绑定(AccountContext):一个账号一个邮箱,换设备登录同一账号也能识别已订阅。
// 需两次输入同一邮箱确认,订阅成功后自动发送验证邮件并默认折叠。
// 服务端未配置发信邮箱(configured=false)时禁用并提示。
export default function SubCard() {
  const account = useContext(AccountContext)
  const [cfg, setCfg] = useState(null) // {configured, subscribed, email, keywords}
  const [email, setEmail] = useState('')
  const [confirm, setConfirm] = useState('')
  const [kws, setKws] = useState('')
  const [msg, setMsg] = useState('')
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)
  const [collapsed, setCollapsed] = useState(false)

  const refresh = useCallback(async () => {
    try {
      const d = await getMerchantSub() // buildQuery 自动带当前账号
      setCfg(d)
      if (d.subscribed) { setEmail(d.email); setConfirm(d.email); setKws(d.keywords) }
      setCollapsed(d.subscribed)
    } catch (e2) { setErr(e2.message) }
  }, [])

  useEffect(() => {
    setMsg(''); setErr('')
    if (account) refresh()
  }, [account, refresh])

  const save = async (ev) => {
    ev.preventDefault()
    const e = email.trim()
    if (!e.includes('@') || e.length < 5) { setErr('请填写正确的邮箱地址'); return }
    if (e !== confirm.trim()) { setErr('两次输入的邮箱不一致,请确认后重试'); return }
    setBusy(true); setErr(''); setMsg('')
    try {
      const res = await setMerchantSub(e, normKws(kws))
      if (res.mail_sent) {
        setMsg('订阅成功,验证邮件已发送到 ' + e + ',请查收(含垃圾箱)。')
      } else if (res.mail_error) {
        setMsg('订阅成功,但验证邮件发送失败:' + res.mail_error)
      } else {
        setMsg('订阅成功(服务端未配置发信邮箱,暂无法发送验证邮件)。')
      }
      setCollapsed(true)
      refresh()
    } catch (e2) { setErr(e2.message) } finally { setBusy(false) }
  }

  // 推荐词点击:已在列表则移除,否则追加(逗号自动处理);与手动输入共用 kws。
  const kwList = normKws(kws).split(',').filter(Boolean)
  const toggleKw = (kw) => {
    setKws((prev) => {
      const list = normKws(prev).split(',').filter(Boolean)
      const i = list.indexOf(kw)
      if (i >= 0) list.splice(i, 1)
      else list.push(kw)
      return list.join(',')
    })
  }

  const unsub = async () => {
    setBusy(true); setErr(''); setMsg('')
    try {
      await delMerchantSub()
      setKws('')
      setMsg('已退订')
      setCollapsed(false)
      setCfg((c) => ({ ...c, subscribed: false }))
    } catch (e2) { setErr(e2.message) } finally { setBusy(false) }
  }

  const disabled = busy || (cfg && !cfg.configured)

  return (
    <div className="merchant-sub">
      <div className="merchant-sub-head">
        <span className="merchant-sub-title">🔔 新货邮件提醒</span>
        {cfg && cfg.configured && cfg.subscribed && <span className="merchant-sub-badge">已订阅</span>}
        {cfg && !cfg.configured && <span className="merchant-sub-warn">服务端未配置发信邮箱,不可用</span>}
        {cfg && cfg.configured && cfg.subscribed && (
          <button type="button" className="merchant-sub-toggle" onClick={() => setCollapsed((c) => !c)}>
            {collapsed ? '展开设置' : '收起'}
          </button>
        )}
      </div>
      {collapsed && cfg && cfg.subscribed ? (
        <div className="merchant-sub-fold">
          <span className="merchant-sub-fold-mail" title={email}>📮 {maskEmail(email)}</span>
          <span className="merchant-sub-fold-kw">{kws.trim() ? '关键词:' + kws.trim() : '提醒全部新货'}</span>
        </div>
      ) : (
        <>
          <p className="merchant-sub-desc">
            每轮(8/12/16/20 点)有<b>新增商品</b>上架时,自动发邮件到你的邮箱(支持任意邮箱);0~8 点打烊不提醒。
            订阅时需两次输入同一邮箱确认,成功后自动发送验证邮件。
            <br />关键词:新增商品<b>名称包含任一关键词</b>即提醒;多个用逗号隔开(中文逗号/顿号/空格会自动转成英文逗号),留空=提醒全部。点击下方推荐词可快速添加,再点一次取消。
          </p>
          <form className="merchant-sub-form" onSubmit={save}>
            <input className="merchant-sub-input" type="email" placeholder="收件邮箱(如 name@example.com)"
              value={email} onChange={(e) => setEmail(e.target.value)}
              disabled={disabled} required />
            <input className="merchant-sub-input" type="email" placeholder="再次输入同一邮箱确认"
              value={confirm} onChange={(e) => setConfirm(e.target.value)}
              disabled={disabled} required />
            <input className="merchant-sub-input" placeholder="关键词(逗号分隔,留空=全部)"
              value={kws} onChange={(e) => setKws(normKws(e.target.value))}
              disabled={disabled} />
            <div className="merchant-sub-presets">
              <span className="merchant-sub-presets-label">推荐:</span>
              {SUB_PRESETS.map((kw) => (
                <button key={kw} type="button"
                  className={'merchant-sub-chip' + (kwList.includes(kw) ? ' on' : '')}
                  onClick={() => toggleKw(kw)}
                  disabled={disabled}>{kw}</button>
              ))}
            </div>
            <button type="submit" className="btn" disabled={disabled}>
              {cfg && cfg.subscribed ? '更新订阅' : '订阅'}
            </button>
            {cfg && cfg.subscribed && (
              <button type="button" className="btn btn-ghost" onClick={unsub} disabled={busy}>退订</button>
            )}
          </form>
        </>
      )}
      {msg && <div className="merchant-sub-msg ok">{msg}</div>}
      {err && <div className="merchant-sub-msg err">{err}</div>}
    </div>
  )
}
