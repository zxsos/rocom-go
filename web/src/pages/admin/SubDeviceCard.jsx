import React, { useCallback, useEffect, useState } from 'react'
import {
  adminSubDeviceCreate, adminSubDeviceRestore, adminSubDeviceRevoke, adminSubDevices,
} from '../../api'
import { copyText } from '../../utils/clipboard'
import { confirmDialog } from '../../components/confirm'
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

  // 确认框一律走 confirmDialog(见 components/confirm.jsx):样式跟主题,
  // 且原生弹窗在全屏/PWA 下会打断沉浸模式。文案只说**不可逆的那部分**,
  // 「三层吊销」那套解释放在卡片底部常驻 —— 塞进弹窗里没人会读完。
  async function revoke(d) {
    const name = d.label || d.token.slice(0, 8)
    const ok = await confirmDialog({
      message: `吊销「${name}」?对方下次刷新订阅会被拒绝,但已装在他手机上的配置暂时还能用。`,
      okText: '吊销', danger: true,
    })
    if (!ok) return
    setBusy(true); setErr(''); setMsg('')
    try {
      await adminSubDeviceRevoke(d.token)
      setMsg('已吊销。对方还能用手机上的旧配置,要立刻断掉就去换一次 hy2 密码。')
      await load()
    } catch (ex) {
      setErr(ex.message || '吊销失败')
    } finally {
      setBusy(false)
    }
  }

  // 恢复:误点一行就吊销了,没有它只能删掉重建 —— 而重建会换新短码,
  // 已发出去的二维码和链接全部作废。
  async function restore(d) {
    const name = d.label || d.token.slice(0, 8)
    const ok = await confirmDialog({
      message: `恢复「${name}」?这条链接可以重新拉到配置了。`,
      okText: '恢复',
    })
    if (!ok) return
    setBusy(true); setErr(''); setMsg('')
    try {
      await adminSubDeviceRestore(d.token)
      setMsg('已恢复。')
      await load()
    } catch (ex) {
      setErr(ex.message || '恢复失败')
    } finally {
      setBusy(false)
    }
  }

  // 删除:只给已吊销的行(按钮不出现),后端也只接受已吊销的 ——
  // 在用设备的拉取记录是排障时唯一能回答「他到底有没有来拉过」的东西。
  async function purge(d) {
    const name = d.label || d.token.slice(0, 8)
    const ok = await confirmDialog({
      message: `彻底删除「${name}」?这条链接的记录会一起消失,不可恢复。`,
      okText: '删除', danger: true,
    })
    if (!ok) return
    setBusy(true); setErr(''); setMsg('')
    try {
      await adminSubDeviceRevoke(d.token, true)
      setMsg('已删除。')
      if (qrOf === d.token) setQrOf('')
      await load()
    } catch (ex) {
      setErr(ex.message || '删除失败')
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
                {d.usable ? (
                  <button className="btn ghost danger" type="button" onClick={() => revoke(d)}>吊销</button>
                ) : (
                  // 吊销后的行给「恢复」和「删除」两条出路:没有它们,一个误点就只能
                  // 删掉重建 —— 而重建会换新短码,已发出的二维码和链接全部作废。
                  <>
                    <button className="btn ghost" type="button" onClick={() => restore(d)}>恢复</button>
                    <button className="btn ghost danger" type="button" onClick={() => purge(d)}>删除</button>
                  </>
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

      {/* 三层吊销的常驻说明。放在这里而不是确认框里:弹窗里塞三行没人会读完,
          而这句话恰恰是点完吊销之后最需要再确认一次的东西。 */}
      <p className="admin-hint">
        ⚠ 吊销有三层,别误会第一层:吊销只挡「下次刷新订阅」;要让他立刻连不上,
        去上面的代理设置换一次 hy2 密码(热更,不影响别人在线);换端口才会把所有人都踢掉。
      </p>
      <p className="admin-hint">
        ⚠ 链接等于钥匙:拿到它的人能拿到节点密码。别发群里,一对一发。
      </p>
    </div>
  )
}
