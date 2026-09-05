import React, { useEffect, useState } from 'react'
import { adminWebAddrTrial, adminWebAddrConfirm, adminWebAddrRevert } from '../../api'
import { splitAddr, joinAddr, validatePort } from '../../utils/netaddr'

// WebAddrCard Web 服务监听地址(「高级设置」分页)。
//
// 它是面板上唯一一项**改错了会把自己锁在外面**的配置:改的是管理员正用来改它的
// 那条连接的另一端。新端口被占、容器没映射、IP 填成 127.0.0.1,任何一条都会让
// 面板当场失联 —— 而服务通常跑在网关上,人不在机器旁。
//
// 故后端不做「保存即生效」,而是试运行 → 确认两步(见 internal/server/api_web_addr.go):
//   1. 试运行  新地址开始监听,新旧**并存**,配置文件一个字节都不动
//   2. 确认    由「管理员在新地址上打开了面板」这一事实自动触发,才落盘 + 停旧监听
//   3. 超时    90 秒内没人确认就自动回滚 —— 改错的代价恒为零
//
// 本卡片负责把这三条如实呈现出来,并让「去新地址看看」这一步只需点一下。

export default function WebAddrCard({ config, error, onChanged, notice }) {
  const web = config?.web ?? {}
  const { host, port } = splitAddr(web.addr)
  const [form, setForm] = useState(null)   // null = 未编辑,显示后端值
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const [msg, setMsg] = useState('')
  // 交接码只在**试运行那一刻**由后端返回一次:它是一次性凭据,故不进
  // GET /api/admin/config 的 pending(那样每刷一次页面就能再取一遍)。
  // 代价是刷新后跳转链接没了 —— 但那也意味着管理员还没去过新地址,
  // 重新试运行一次即可(旧地址在此期间一直可用)。
  const [handoff, setHandoff] = useState('')
  // 「直接保留」成功后的落地提示。它不只是文案 —— 那一刻旧监听已经关了,
  // 本页**再也刷不动**(再拉配置必然失败,而那句「拉取配置失败」会盖掉刚成功的提示)。
  // 故切换结果只能由本地状态记着,并给一个去新地址的链接。
  const [done, setDone] = useState(null)   // {realAddr, url}

  const f = form ?? { host, port }
  const set = (patch) => { setForm({ ...f, ...patch }); setErr(''); setMsg('') }

  const pending = web.pending          // {addr, realAddr, port, deadline}
  // 试运行已结束(回滚/确认/超时)时顺手丢掉交接码,免得它多活一轮。
  useEffect(() => { if (!pending) setHandoff('') }, [pending])
  // 倒计时:每秒重算一次剩余秒数,归零时刷新配置(后端此刻已自动回滚)。
  const [left, setLeft] = useState(0)
  useEffect(() => {
    if (!pending) { setLeft(0); return undefined }
    const tick = () => {
      const s = Math.max(0, Math.round(pending.deadline - Date.now() / 1000))
      setLeft(s)
      if (s === 0) onChanged()
    }
    tick()
    const t = setInterval(tick, 1000)
    return () => clearInterval(t)
    // 依赖项刻意只取 deadline 而非整个 pending:后者每次拉配置都是新对象,
    // 会让倒计时每秒被重建一次(定时器永远走不到 0)。
  }, [pending?.deadline, onChanged]) // eslint-disable-line react-hooks/exhaustive-deps

  // 试运行:新地址立刻开始监听,但**不落盘**。之后由管理员去新地址确认。
  const trial = async () => {
    setDone(null)
    const h = String(f.host).trim()
    const p = String(f.port).trim()
    const bad = validatePort(p)
    if (bad) return setErr(bad)
    if (p === '') return setErr('端口不能留空(Web 服务必须有个监听端口)')
    setBusy(true); setErr(''); setMsg('')
    try {
      const res = await adminWebAddrTrial(joinAddr(h, p))
      setForm(null)
      setHandoff(res.handoff || '')
      setMsg(`新地址已在 ${res.realAddr} 试运行:请点下面的链接打开,确认能进去再保留。`)
      onChanged()
    } catch (e) {
      setErr(e.message || '试运行失败')
    } finally {
      setBusy(false)
    }
  }

  // 保留:管理员在旧地址上直接确认(没去新地址看过时的路径)。
  //
  // 注意**不要**用它做自动确认 —— 一旦它由「页面加载」触发,「新地址可达」这个
  // 证据就不成立了:点「试运行」的人始终连在旧地址上,那条请求走的是旧监听,
  // 于是新地址其实一次都没被访问过,却照样写了配置文件。
  // 刻意**不**调 onChanged():旧监听此刻已停止接入,再拉配置必然失败,
  // 而那句「拉取配置失败」会盖掉刚成功的提示 —— 报错的时机比报错本身更糟。
  const confirm = async () => {
    setBusy(true); setErr(''); setMsg('')
    try {
      const res = await adminWebAddrConfirm()
      setDone({ realAddr: res.realAddr, url: newURL })
      setMsg(`已切换到 ${res.realAddr} 并写入配置文件,旧地址已停止服务。`)
    } catch (e) {
      setErr(e.message || '确认失败')
    } finally {
      setBusy(false)
    }
  }

  const revert = async () => {
    setBusy(true); setErr(''); setMsg(''); setDone(null)
    try {
      await adminWebAddrRevert()
      setMsg('已回滚,继续在原地址提供服务(配置文件未被改动)。')
      onChanged()
    } catch (e) {
      setErr(e.message || '回滚失败')
    } finally {
      setBusy(false)
    }
  }

  // 自动确认**不在这里**:它由 Admin 在页面加载时凭 URL 上的一次性交接码完成
  // (见 Admin.jsx)。理由同上 —— 只有从新地址发来的那次请求才构成证据。

  if (error || !config) {
    return (
      <div className="admin-card admin-wide">
        <h3>Web 服务</h3>
        {error ? <p className="admin-error">{error}</p> : <p className="admin-hint">加载中…</p>}
      </div>
    )
  }
  if (!config.writable) {
    return (
      <div className="admin-card admin-wide">
        <h3>Web 服务</h3>
        <p className="admin-hint">
          配置文件不可写({config.path || '/etc/rocom.env'}),无法在此修改监听地址。
          请在服务器上编辑该文件后执行 <code>systemctl restart rocom</code>。
        </p>
        <p className="admin-hint">当前监听:<code>{web.realAddr || web.addr || '—'}</code></p>
      </div>
    )
  }

  // 新地址的跳转目标:主机名沿用当前页面(管理员是从这里连过去的),只换端口。
  //
  // 带上 handoff:换端口 = 换源,localStorage 里的管理员令牌**不跟过去**
  // (它是按协议+主机+端口隔离的),新源凭这个一次性码完成确认并接上会话 ——
  // 否则管理员兴冲冲打开新地址,看到的却是登录页。
  const newURL = pending?.port
    ? `${location.protocol}//${location.hostname}:${pending.port}${location.pathname}#/admin?tab=advanced`
    : ''
  const trialURL = newURL && handoff ? `${newURL}&handoff=${encodeURIComponent(handoff)}` : ''

  return (
    <div className="admin-card admin-wide">
      <h3>Web 服务</h3>
      <p className="admin-hint">
        本页的监听地址。当前:<code>{web.realAddr || web.addr || '—'}</code>
        {web.addr && web.realAddr && web.addr !== web.realAddr && <> (配置值 <code>{web.addr}</code>)</>}
      </p>
      <p className="admin-hint">
        ⚠ 改它会让你自己跟面板断开,故不直接生效:先在新端口试运行,确认能打开再保留;
        90 秒内不确认就自动回滚,配置文件在那之前一个字节都不会动。
      </p>

      <label className="admin-field">
        <span>监听 IP</span>
        <input
          type="text" value={f.host} placeholder="留空 = 监听所有网卡,如 127.0.0.1"
          onChange={(e) => set({ host: e.target.value })}
        />
      </label>
      <label className="admin-field">
        <span>端口</span>
        <input
          type="number" min="0" max="65535" value={f.port} placeholder="如 4939"
          onChange={(e) => set({ port: e.target.value })}
        />
      </label>

      {done ? (
        // 用实线框:配置**已经落盘**,不再是「随时会撤」的暂态 —— 虚线框是
        // 上面试运行块专用的语义(见 admin.css 的 .admin-trial)。
        <div className="admin-config-group">
          <p className="admin-hint">
            已切换到 <code>{done.realAddr}</code> 并写入配置文件。旧地址已停止服务,
            本页不会再刷新 —— 请到新地址继续。
          </p>
          <div className="admin-config-actions">
            <a className="btn primary" href={done.url}>打开新地址</a>
          </div>
        </div>
      ) : pending ? (
        <div className="admin-trial">
          <p className="admin-hint">
            试运行中:<code>{pending.realAddr}</code> 与现有地址同时在服务,配置文件还没改。
            <b> {left} 秒</b>后不确认将自动回滚。
          </p>
          <div className="admin-config-actions">
            {/* 没有交接码时不渲染这个链接:href="" 会重新加载**当前**页面,
                看着像点了却什么都没发生,而实际该做的是重新试运行。 */}
            {trialURL && <a className="btn primary" href={trialURL}>打开新地址并保留</a>}
            <button className="btn" type="button" disabled={busy} onClick={confirm}>直接保留</button>
            <button className="btn ghost danger" type="button" disabled={busy} onClick={revert}>回滚</button>
          </div>
          {trialURL ? (
            <p className="admin-hint">
              打不开?别点保留 —— 等它自己回滚,或点「回滚」立即结束。旧地址在此期间一直可用。
            </p>
          ) : (
            <p className="admin-hint">
              交接码只在发起试运行时发放一次,刷新页面后不再补发。想从新地址确认,请重新试运行。
            </p>
          )}
        </div>
      ) : (
        <div className="admin-config-actions">
          <button
            className={'btn' + (busy ? ' is-loading' : '')} type="button"
            disabled={busy} onClick={trial}
          >
            试运行新地址
          </button>
          {form && (
            <button className="btn" type="button" disabled={busy} onClick={() => { setForm(null); setErr('') }}>
              放弃修改
            </button>
          )}
        </div>
      )}

      {err && <p className="admin-error">{err}</p>}
      {msg && <p className="admin-ok">{msg}</p>}
      {notice && !msg && !err && <p className="admin-ok">{notice}</p>}
    </div>
  )
}
