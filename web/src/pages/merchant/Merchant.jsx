import React, { useCallback, useEffect, useState } from 'react'
import { getMerchant, getMerchantSub, setMerchantSub, delMerchantSub } from '../../api'
import { fmtTime } from '../../utils/format'

// 远行商人页:展示后端缓存的 4h 轮次数据(令牌在服务端,缓存 2 天,见
// internal/server/api_merchant.go)。业务节奏:每天 8 点开张、0 点收摊,8/12/16/20 四个整点
// 各上架一轮新货并售卖 4 小时;只有 00:00~08:00 是打烊休市(页面此时显示昨日四轮全天回顾)。
// 刷新按钮只是重拉后端缓存(不烧 token);强制刷新才让后端回源第三方。
// 图片字段兼容两种形式:http(s) 外链直接用;否则按本地 /img/ 相对路径解析。
const imgSrc = (it) => {
  const v = it && it.image
  if (!v) return ''
  return /^https?:\/\//i.test(v) ? v : '/img/' + v
}

// msTime 第三方的时间戳是毫秒,fmtTime 要秒,这里除 1000 再交给它。
const msTime = (ms) => (ms ? fmtTime(ms / 1000) : '')

// 营业状态 → 徽标文案与样式
const STATUS = {
  open: { cls: 'ok', text: '营业中' },
  idle: { cls: 'off', text: '已打烊' },
}

// count 统计某轮的第三方 item 数(优先 item_count,兜底数 items 数组)。
const count = (m) => (m ? (m.item_count ?? ((m.items && m.items.length) || 0)) : 0)

export default function Merchant() {
  const [d, setD] = useState(null) // {now,day,status,today,prev}
  const [err, setErr] = useState('')
  const [loading, setLoading] = useState(false)

  const load = useCallback(async (force) => {
    setLoading(true)
    setErr('')
    try {
      setD(await getMerchant(force))
    } catch (e) {
      setErr(e.message || '拉取失败')
      setD(null)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { load(false) }, [load])

  if (err) {
    return <div className="merchant-page"><div className="merchant-err">{err}</div></div>
  }
  if (!d) {
    return <div className="merchant-page"><div className="empty">{loading ? '加载中…' : ''}</div></div>
  }

  const open = d.status === 'open'
  // 上架轮(排除 off 打烊槽):营业中看今天,打烊看昨天回顾。
  const turns = ((open ? d.today : d.prev) || []).filter((r) => !r.off)
  // 当前轮 = 开始时刻 ≤ now 的最后一轮(都是后端 Unix 秒,直接比较数值,无时区问题)。
  const curIdx = turns.reduce((last, r, i) => (r.start <= d.now ? i : last), -1)
  const cur = curIdx >= 0 ? turns[curIdx] : null
  const active = turns.filter((r) => !r.empty && r.merchant) // 查过且有货的轮
  // 营业中最新轮在前(玩家最关心当前货),回顾按时间正序。
  const list = open ? [...active].reverse() : active
  const head = list[0]
  const m = head && head.merchant
  const total = active.reduce((n, r) => n + count(r.merchant), 0)
  const st = STATUS[d.status] || STATUS.idle
  const dayText = open ? d.day : `昨日 ${d.day}`

  return (
    <div className="merchant-page">
      {/* 顶部横幅:商人名 + 营业状态 + 轮次进度 */}
      <div className={`merchant-hero ${open ? 'is-open' : ''}`}>
        <div className="merchant-hero-top">
          <div className="merchant-hero-info">
            <div className="merchant-name">{m ? m.merchant_name : '远行商人'}</div>
            {m && m.subtitle && <div className="merchant-sub">{m.subtitle}</div>}
          </div>
          <div className="merchant-hero-side">
            <span className={`merchant-status merchant-status-${st.cls}`}>{st.text}</span>
            <span className="merchant-day">{dayText}</span>
            {m && m.round && m.round.countdown && (
              <span className="merchant-countdown" title="距本轮结束">⏳ {m.round.countdown}</span>
            )}
          </div>
        </div>
        {open ? (
          <RoundSteps turns={turns} curIdx={curIdx} />
        ) : (
          <div className="merchant-closed-bar">
            打烊休市 00:00 ~ 08:00,下一轮 08:00 开张 · 以下为 {d.day} 全天回顾
          </div>
        )}
      </div>

      {/* 订阅卡片:新货上架邮件提醒 */}
      <SubCard />

      {/* 操作条 */}
      <div className="merchant-bar">
        <span className="merchant-count">
          {open && cur
            ? `第 ${curIdx + 1}/4 轮 · 在售 ${total} 件`
            : `昨日全天回顾 · 共 ${total} 件`}
        </span>
        <span className="merchant-btns">
          <button type="button" className="btn" onClick={() => load(false)} disabled={loading}>
            {loading ? '加载中…' : '刷新'}
          </button>
          <button type="button" className="btn" onClick={() => load(true)} disabled={loading}
            title="强制后端重新向第三方抓取(烧对方额度,非必要别点)">
            强制刷新
          </button>
        </span>
      </div>

      {list.length ? (
        <div className="merchant-rounds">
          {list.map((r) => (
            <MerchantTurn key={r.start} r={r} cur={open && r === cur} />
          ))}
        </div>
      ) : (
        <div className="empty">该时段暂无数据,后端会按 4h 自动补查,稍后再试</div>
      )}
    </div>
  )
}

// RoundSteps 四个上架轮次步骤条:8:00 / 12:00 / 16:00 / 20:00。
// 营业中:已过轮打勾、当前轮高亮、未来轮置灰;打烊回顾:四轮全部完成。
function RoundSteps({ turns, curIdx }) {
  return (
    <div className="merchant-steps">
      {turns.map((r, i) => {
        const state = i < curIdx ? 'done' : i === curIdx ? 'cur' : 'todo'
        return (
          <div key={r.start} className={`merchant-step ${state}`}>
            <div className="merchant-step-node">
              <span className="merchant-step-dot">{state === 'done' ? '✓' : ''}</span>
            </div>
            <div className="merchant-step-time">{r.label.split('~')[0]}</div>
            <div className="merchant-step-cap">{i < curIdx ? '已上架' : i === curIdx ? '售卖中' : '待上架'}</div>
          </div>
        )
      })}
    </div>
  )
}

// MerchantTurn 单个上架轮:轮次头 + 该轮商品网格。
function MerchantTurn({ r, cur }) {
  const m = r.merchant
  const items = (m && m.items) || []
  const time = r.label.split('~')[0]
  return (
    <div className={`merchant-turn ${cur ? 'is-cur' : ''}`}>
      <div className="merchant-turn-head">
        <span className="merchant-turn-time">{time} 上架</span>
        {cur && <span className="merchant-turn-cur">当前售卖中</span>}
        <span className="merchant-turn-range muted">{r.label}</span>
        <span className="merchant-turn-count">{count(m)} 件</span>
      </div>
      {items.length ? (
        <div className="merchant-grid">
          {items.map((it, i) => <MerchantItem key={it.name + i} it={it} />)}
        </div>
      ) : (
        <div className="empty">该轮暂无商品</div>
      )}
    </div>
  )
}

// MerchantItem 单个商品卡片:大图 + 名称/类别 + 价格/限购/售卖时间。
function MerchantItem({ it }) {
  const [imgBad, setImgBad] = useState(false)
  const src = imgSrc(it)
  const range = it.start_time && it.end_time ? `${msTime(it.start_time)} ~ ${msTime(it.end_time)}` : ''
  const tLabel = it.time_label || range
  return (
    <div className="merchant-item">
      <div className="merchant-item-img-wrap">
        {src && !imgBad ? (
          <img className="merchant-item-img" src={src} alt="" loading="lazy" draggable={false}
            onError={() => setImgBad(true)} />
        ) : (
          <span className="merchant-item-fb">🧳</span>
        )}
      </div>
      <div className="merchant-item-body">
        <div className="merchant-item-name" title={it.name}>{it.name}</div>
        {it.kind && <span className="merchant-item-kind">{it.kind}</span>}
        <div className="merchant-item-meta">
          <span className="merchant-price">{it.price} <span className="merchant-unit">金币</span></span>
          {it.limit > 0 && <span className="merchant-limit">限购 {it.limit}</span>}
        </div>
        {tLabel && (
          <div className="merchant-item-time" title={range || tLabel}>{tLabel}</div>
        )}
      </div>
    </div>
  )
}

// SubCard 新货邮件提醒订阅:填 QQ 邮箱(可选关键词,空=全部),每轮新货上架后后端发邮件。
// 服务端未配置发信邮箱(configured=false)时禁用并提示。
function SubCard() {
  const [cfg, setCfg] = useState(null) // {configured, subscribed, email, keywords}
  const [email, setEmail] = useState('')
  const [kws, setKws] = useState('')
  const [msg, setMsg] = useState('')
  const [err, setErr] = useState('')
  const [busy, setBusy] = useState(false)

  const refresh = useCallback(async (e) => {
    try {
      const d = await getMerchantSub(e)
      setCfg(d)
      if (d.subscribed) setKws(d.keywords)
    } catch (e2) { setErr(e2.message) }
  }, [])

  useEffect(() => {
    const saved = localStorage.getItem('merchant-sub-email')
    if (saved) {
      setEmail(saved)
      refresh(saved)
    } else {
      setCfg({ configured: true, subscribed: false, email: '', keywords: '' })
    }
  }, [refresh])

  const save = async (ev) => {
    ev.preventDefault()
    const e = email.trim()
    if (!e.includes('@') || e.length < 5) { setErr('请填写正确的 QQ 邮箱'); return }
    setBusy(true); setErr(''); setMsg('')
    try {
      await setMerchantSub(e, kws.trim())
      localStorage.setItem('merchant-sub-email', e)
      setMsg('订阅成功,新货上架后会发邮件到 ' + e)
      refresh(e)
    } catch (e2) { setErr(e2.message) } finally { setBusy(false) }
  }

  const unsub = async () => {
    const e = email.trim()
    setBusy(true); setErr(''); setMsg('')
    try {
      await delMerchantSub(e)
      localStorage.removeItem('merchant-sub-email')
      setKws('')
      setMsg('已退订')
      setCfg((c) => ({ ...c, subscribed: false }))
    } catch (e2) { setErr(e2.message) } finally { setBusy(false) }
  }

  return (
    <div className="merchant-sub">
      <div className="merchant-sub-head">
        <span className="merchant-sub-title">🔔 新货邮件提醒</span>
        {cfg && cfg.configured && cfg.subscribed && <span className="merchant-sub-badge">已订阅</span>}
        {cfg && !cfg.configured && <span className="merchant-sub-warn">服务端未配置发信邮箱,不可用</span>}
      </div>
      <p className="merchant-sub-desc">
        每轮(8/12/16/20 点)有<b>新增商品</b>上架时,自动发邮件到你的 QQ 邮箱;0~8 点打烊不提醒。
      </p>
      <form className="merchant-sub-form" onSubmit={save}>
        <input className="merchant-sub-input" type="email" placeholder="QQ 邮箱(如 123456@qq.com)"
          value={email} onChange={(e) => setEmail(e.target.value)}
          disabled={busy || (cfg && !cfg.configured)} required />
        <input className="merchant-sub-input" placeholder="关键词(逗号分隔,可留空=全部)"
          value={kws} onChange={(e) => setKws(e.target.value)}
          disabled={busy || (cfg && !cfg.configured)} />
        <button type="submit" className="btn" disabled={busy || (cfg && !cfg.configured)}>
          {cfg && cfg.subscribed ? '更新订阅' : '订阅'}
        </button>
        {cfg && cfg.subscribed && (
          <button type="button" className="btn btn-ghost" onClick={unsub} disabled={busy}>退订</button>
        )}
      </form>
      {msg && <div className="merchant-sub-msg ok">{msg}</div>}
      {err && <div className="merchant-sub-msg err">{err}</div>}
    </div>
  )
}
