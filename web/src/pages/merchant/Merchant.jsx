import React, { useCallback, useContext, useEffect, useState } from 'react'
import { getMerchant, getMerchantSub, setMerchantSub, delMerchantSub, getAccounts, setAccountRank } from '../../api'
import { AccountContext } from '../../context'
import RankTitle from '../../components/RankTitle'
import { confirmDialog } from '../../components/confirm'
import { fmtTime } from '../../utils/format'

// 远行商人页:展示后端缓存的 4h 轮次数据(令牌在服务端,缓存 2 天,见
// internal/server/api_merchant.go)。业务节奏:每天 8 点开张、0 点收摊,8/12/16/20 四个整点
// 各上架一轮新货并售卖 4 小时;只有 00:00~08:00 是打烊休市(页面此时显示昨日四轮全天回顾)。
// 刷新按钮只是重拉后端缓存(不烧 token);强制刷新(绕缓存回源第三方)已移到管理面板。
// 图片字段兼容两种形式:http(s) 外链直接用;否则按本地 /img/ 相对路径解析。
const imgSrc = (it) => {
  const v = it && it.image
  if (!v) return ''
  return /^https?:\/\//i.test(v) ? v : '/img/' + v
}

// msTime 第三方的时间戳是毫秒,fmtTime 要秒,这里除 1000 再交给它。
const msTime = (ms) => (ms ? fmtTime(ms / 1000) : '')

// 推荐关键词:常见「值得买」商品词,点击即填入(自动补英文逗号,再点一次取消)。
const SUB_PRESETS = ['球', '棱镜', '国王', '项链', '粉尘', '零碎', '相框', '魔镜', '钥匙']
// 关键词规范化:中文逗号/顿号/分号/句号/空白等间隔符统一成英文逗号,去空项。
// 后端按英文逗号分词(大小写不敏感子串匹配),这里保证前端提交的都是规范形式。
const normKws = (s) =>
  String(s || '').replace(/[，、;；。\s]+/g, ',').split(',').map((x) => x.trim()).filter(Boolean).join(',')

// 营业状态 → 徽标文案与样式
const STATUS = {
  open: { cls: 'ok', text: '营业中' },
  idle: { cls: 'off', text: '已打烊' },
}

// 后端 merchant 槽存的是第三方原始响应(带 code/msg/data 壳,见 api_merchant.go
// PutMerchantSlot(string(body))),业务字段(merchant_name/items/round…)都在 data 里。
// unwrap 取出 data 层;老缓存可能直接存裸 data,typeof 判断兜底兼容。
const unwrap = (m) => (m && m.data && typeof m.data === 'object' ? m.data : m)

// count 统计某轮的第三方 item 数(优先 item_count,兜底数 items 数组)。
const count = (raw) => {
  const m = unwrap(raw)
  return m ? (m.item_count ?? ((m.items && m.items.length) || 0)) : 0
}

// 标准售卖时段(北京时间):8/12/16/20 四轮,各 4 小时。
const SLOTS = ['08:00-12:00', '12:00-16:00', '16:00-20:00', '20:00-24:00']

// parseSlots 解析商品售卖时段:优先用第三方 time_label("08:00-12:00 / …"),
// 解析失败时按 start_time/end_time(毫秒,北京时间)推断为单个时段串。
const parseSlots = (it) => {
  const raw = it && it.time_label
  if (raw) {
    const slots = String(raw).split('/').map((s) => s.trim()).filter((s) => /^\d{2}:\d{2}-\d{2}:\d{2}$/.test(s))
    if (slots.length) return slots
  }
  const st = it && it.start_time, et = it && it.end_time
  if (!st || !et) return []
  const bj = (ms) => {
    const d = new Date(ms + 8 * 3600 * 1000) // 加 8h 读 UTC 组件 = 北京时间
    return `${String(d.getUTCHours()).padStart(2, '0')}:${String(d.getUTCMinutes()).padStart(2, '0')}`
  }
  const s = bj(st)
  let e = bj(et)
  if (e === '00:00') e = '24:00' // 结束恰在北京午夜 → 按 24:00
  return s === e ? [] : [`${s}-${e}`]
}

// groupBySlot 把商品按时段分栏:覆盖全部四段的商品归「全天」栏(不重复进各时段栏),
// 其余按各自时段归栏;时段串不在标准四段内的归「其他」栏。
function groupBySlot(items) {
  const groups = SLOTS.map((s) => ({ slot: s, items: [] }))
  const allDay = []
  const other = []
  for (const it of items) {
    const slots = parseSlots(it)
    if (slots.length && SLOTS.every((s) => slots.includes(s))) {
      allDay.push(it)
      continue
    }
    let hit = false
    for (const s of slots) {
      const g = groups.find((x) => x.slot === s)
      if (g) { g.items.push(it); hit = true }
    }
    if (!hit) other.push(it)
  }
  return {
    allDay,
    groups: groups.filter((g) => g.items.length),
    other,
  }
}

export default function Merchant() {
  const account = useContext(AccountContext) // 当前登录账号 key(账号下拉切换后自动跟随)
  const [d, setD] = useState(null) // {now,day,status,today,prev}
  const [coins, setCoins] = useState(null) // 当前账号金币:null=未同步,数字=已同步(含 0)
  const [curAcc, setCurAcc] = useState('') // 金币/参加按钮实际归属的账号
  const [join, setJoin] = useState(true) // 是否参加排行榜(默认参加,可在金币旁一键退出)
  const [title, setTitle] = useState('') // 当前账号今日佩戴的称号
  const [busyRank, setBusyRank] = useState(false)
  const [rankErr, setRankErr] = useState('')
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

  // 金币按当前登录账号(AccountContext)取,每个人看到自己的金币:
  // 先精确匹配当前账号,找不到(未登录/游客)再回退到在线/最近活跃账号。
  // hasCoins=false 表示该账号从未解析到金币(没重登游戏),显示「待同步」而非隐藏徽标。
  useEffect(() => {
    let done = false
    getAccounts().then((list) => {
      if (done || !list) return
      const mine = list.find((a) => a.account === account)
      const cur = mine || list.find((a) => a.online) || list[0]
      setCurAcc(cur ? cur.account : '')
      setCoins(cur && cur.hasCoins ? cur.coins : null)
      setJoin(cur ? !!cur.join : true)
      setTitle(cur ? (cur.title || '') : '')
    }).catch(() => {})
    return () => { done = true }
  }, [account])

  // 金币旁的参加/退出按钮(默认参加;退出后不再参与福布斯/盈亏排行与称号评选)
  const toggleRank = async () => {
    if (!curAcc) return
    if (join && !(await confirmDialog({
      message: '退出排行榜?退出后不再参与福布斯/盈亏排行,称号评选也会排除。',
      okText: '退出', danger: true,
    }))) return
    setBusyRank(true)
    setRankErr('')
    try {
      await setAccountRank(curAcc, !join)
      setJoin(!join)
    } catch (e) {
      setRankErr(e.message || '设置失败')
    } finally {
      setBusyRank(false)
    }
  }

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
  const m = head && unwrap(head.merchant)
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
            {coins !== null ? (
              <span className="merchant-coins" title={`${account || '当前'} 金币(每次登录游戏时同步)`}>🪙 {coins.toLocaleString()}</span>
            ) : (
              <span className="merchant-coins merchant-coins-unk" title={`${account || '当前'} 金币尚未同步,请重新登录游戏后刷新`}>🪙 待同步</span>
            )}
            <RankTitle title={title} />
            {curAcc && (
              <button type="button" className={`merchant-rank-btn${join ? ' joined' : ''}`}
                onClick={toggleRank} disabled={busyRank}
                title={join ? '已参加排行榜,点击退出' : '未参加排行榜,点击参加(默认参加)'}>
                {join ? '🏆 已参加' : '🏆 参加'}
              </button>
            )}
            {rankErr && <span className="merchant-rank-err">{rankErr}</span>}
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

// MerchantTurn 单个上架轮:轮次头 + 按售卖时段分栏的商品网格(全天商品单独一栏)。
function MerchantTurn({ r, cur }) {
  const m = unwrap(r.merchant)
  const items = (m && m.items) || []
  const time = r.label.split('~')[0]
  const { allDay, groups, other } = groupBySlot(items)
  const has = allDay.length > 0 || groups.length > 0 || other.length > 0
  return (
    <div className={`merchant-turn ${cur ? 'is-cur' : ''}`}>
      <div className="merchant-turn-head">
        <span className="merchant-turn-time">{time} 上架</span>
        {cur && <span className="merchant-turn-cur">当前售卖中</span>}
        <span className="merchant-turn-range muted">{r.label}</span>
        <span className="merchant-turn-count">{count(m)} 件</span>
      </div>
      {has ? (
        <div className="merchant-slots">
          {allDay.length > 0 && (
            <div className="merchant-slot">
              <div className="merchant-slot-head">
                <span className="merchant-slot-time merchant-slot-all">全天 08:00-24:00</span>
                <span className="merchant-slot-count">{allDay.length} 件</span>
              </div>
              <div className="merchant-grid">
                {allDay.map((it, i) => <MerchantItem key={it.name + i} it={it} />)}
              </div>
            </div>
          )}
          {groups.map((g) => (
            <div className="merchant-slot" key={g.slot}>
              <div className="merchant-slot-head">
                <span className="merchant-slot-time">{g.slot}</span>
                <span className="merchant-slot-count">{g.items.length} 件</span>
              </div>
              <div className="merchant-grid">
                {g.items.map((it, i) => <MerchantItem key={it.name + i} it={it} />)}
              </div>
            </div>
          ))}
          {other.length > 0 && (
            <div className="merchant-slot">
              <div className="merchant-slot-head">
                <span className="merchant-slot-time">其他时段</span>
                <span className="merchant-slot-count">{other.length} 件</span>
              </div>
              <div className="merchant-grid">
                {other.map((it, i) => <MerchantItem key={it.name + i} it={it} />)}
              </div>
            </div>
          )}
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

// SubCard 新货邮件提醒订阅:填任意邮箱(可选关键词,空=全部),每轮新货上架后后端发邮件。
// 订阅按当前登录账号绑定(AccountContext):一个账号一个邮箱,换设备登录同一账号也能识别已订阅。
// 需两次输入同一邮箱确认,订阅成功后自动发送验证邮件并默认折叠。
// 服务端未配置发信邮箱(configured=false)时禁用并提示。
function SubCard() {
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
          <span className="merchant-sub-fold-mail">📮 {email}</span>
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
              disabled={busy || (cfg && !cfg.configured)} required />
            <input className="merchant-sub-input" type="email" placeholder="再次输入同一邮箱确认"
              value={confirm} onChange={(e) => setConfirm(e.target.value)}
              disabled={busy || (cfg && !cfg.configured)} required />
            <input className="merchant-sub-input" placeholder="关键词(逗号分隔,留空=全部)"
              value={kws} onChange={(e) => setKws(normKws(e.target.value))}
              disabled={busy || (cfg && !cfg.configured)} />
            <div className="merchant-sub-presets">
              <span className="merchant-sub-presets-label">推荐:</span>
              {SUB_PRESETS.map((kw) => (
                <button key={kw} type="button"
                  className={'merchant-sub-chip' + (kwList.includes(kw) ? ' on' : '')}
                  onClick={() => toggleKw(kw)}
                  disabled={busy || (cfg && !cfg.configured)}>{kw}</button>
              ))}
            </div>
            <button type="submit" className="btn" disabled={busy || (cfg && !cfg.configured)}>
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
