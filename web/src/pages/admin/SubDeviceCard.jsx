import React, { useCallback, useEffect, useState } from 'react'
import { adminSubDeviceCreate, adminSubDeviceRevoke, adminSubDevices } from '../../api'
import { copyText } from '../../utils/clipboard'
import QrCode from '../../components/QrCode'

// SubDeviceCard 订阅设备管理:一人(一台设备)一枚令牌 + 一条短链。
//
// 为什么不再只有那一条「当前密码派生」的订阅地址:派生地址把「踢掉某个人」和
// 「换密码」焊死了 —— 想让某人的订阅失效就得换密码,而换密码会让**所有人**的订阅
// 一起失效,包括你自己的手机。拆成每设备一枚之后,吊销就只是这一行的事。
//
// 近况几列(末次拉取 / UA / 实发格式 / 命中数)是排障用的:朋友说「连上了但没数据」时,
// 第一眼要能分开「他根本没来拉过(lastSeenAt=0)」与「拉到了但规则没生效」。
// 这两种故障在下游的表现一模一样,而修法完全不同。
export default function SubDeviceCard() {
  const [devices, setDevices] = useState([])
  const [label, setLabel] = useState('')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [msg, setMsg] = useState('')
  const [qrOf, setQrOf] = useState('') // 展开二维码的设备 token

  const load = useCallback(async () => {
    try {
      const r = await adminSubDevices()
      setDevices(r.devices || [])
    } catch (e) {
      setErr(e.message || '拉取订阅设备失败')
    }
  }, [])

  useEffect(() => { load() }, [load])

  async function create(e) {
    e.preventDefault()
    setBusy(true); setErr(''); setMsg('')
    try {
      const r = await adminSubDeviceCreate(label.trim())
      setLabel('')
      setQrOf(r.device.token)
      setMsg('已生成,把下面的链接或二维码发给朋友即可。')
      await load()
    } catch (ex) {
      setErr(ex.message || '创建失败')
    } finally {
      setBusy(false)
    }
  }

  async function revoke(d) {
    // 把三档后果说清再让人确认:吊销本身只挡「下次更新」,这是最容易被误解的一点 ——
    // 管理员点完会以为对方已经断线,而实际上他还在线上玩着。
    const ok = window.confirm(
      `吊销「${d.label || d.token.slice(0, 8)}」?\n\n` +
      '吊销后:对方下次刷新订阅会被拒绝(403),已装在他手机上的配置暂时还能用。\n' +
      '要让他立刻连不上:去上面的代理设置里换一次 hy2 密码(热更,不影响别人在线)。'
    )
    if (!ok) return
    setBusy(true); setErr(''); setMsg('')
    try {
      await adminSubDeviceRevoke(d.token)
      setMsg('已吊销。')
      await load()
    } catch (ex) {
      setErr(ex.message || '吊销失败')
    } finally {
      setBusy(false)
    }
  }

  async function copy(url) {
    const ok = await copyText(url)
    setMsg(ok ? '已复制链接' : '复制失败,请手动选中复制')
    setErr('')
  }

  return (
    <div className="admin-config-group">
      <h4>订阅设备(一人一条链接)</h4>
      <p className="admin-hint">
        给每个人单独生成一条:他点开链接跟着引导走就行,不用手填密码和规则。
        谁不玩了就单独吊销,不影响别人。
      </p>

      <form className="sub-device-form" onSubmit={create}>
        <input
          type="text" value={label} placeholder="备注名,例如「老王」「我自己的手机」"
          onChange={(e) => setLabel(e.target.value)}
        />
        <button className="btn" type="submit" disabled={busy}>生成链接</button>
      </form>

      {err && <p className="admin-error">{err}</p>}
      {msg && <p className="admin-hint">{msg}</p>}

      {devices.length === 0 ? (
        <p className="admin-hint">还没有生成过。上面填个名字点「生成链接」。</p>
      ) : (
        <ul className="sub-device-list">
          {devices.map((d) => (
            <li key={d.token} className={d.usable ? '' : 'off'}>
              <div className="sub-device-head">
                <b>{d.label || '(未命名)'}</b>
                {d.revokedAt > 0 && <span className="sub-device-tag bad">已吊销</span>}
                {d.revokedAt === 0 && d.expiresAt > 0 && <span className="sub-device-tag">有到期</span>}
                <span className="sub-device-meta">
                  {d.lastSeenAt > 0
                    ? `最近拉取 ${new Date(d.lastSeenAt * 1000).toLocaleString()} · ${d.hits} 次`
                    : '从未拉取'}
                  {d.lastUa ? ` · ${d.lastUa}` : ''}
                  {d.lastFormat ? ` · ${d.lastFormat}` : ''}
                </span>
              </div>

              <input
                className="admin-link" type="text" readOnly value={d.subUrl}
                onFocus={(e) => e.target.select()}
              />

              <div className="admin-config-actions">
                <button className="btn ghost" type="button" onClick={() => copy(d.subUrl)}>复制链接</button>
                <button
                  className="btn ghost" type="button"
                  onClick={() => setQrOf(qrOf === d.token ? '' : d.token)}
                >
                  {qrOf === d.token ? '收起二维码' : '二维码'}
                </button>
                {d.usable && (
                  <button className="btn ghost danger" type="button" onClick={() => revoke(d)}>吊销</button>
                )}
              </div>

              {qrOf === d.token && (
                <div className="sub-device-qr">
                  <QrCode value={d.subUrl} size={200} />
                  <p className="admin-hint">
                    让朋友用手机相机或客户端里的「扫码」直接扫这条即可。
                  </p>
                </div>
              )}
            </li>
          ))}
        </ul>
      )}

      <p className="admin-hint">
        ⚠ 链接等于钥匙:拿到它的人能拿到节点密码。别发群里,一对一发。
      </p>
    </div>
  )
}
