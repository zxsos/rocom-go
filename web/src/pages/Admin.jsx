import React, { useEffect, useState } from 'react'
import {
  getAdminStatus, adminSetup, adminLogin, adminLogout,
  getAdminToken, setAdminToken, adminRules, adminSetRule, adminDeleteRule,
  adminStats, adminPlaySessions, adminEggStats, adminWildPetOptions, adminInjectWild, adminInjectFlower, adminListInjects, adminRevokeInject,
  adminMerchantSubs, adminMerchantSubDelete, adminTestMail, getMerchant,
  getAccounts, setAccountPin, deleteAccount,
} from '../api'
import { GLASS_PARTICLES, GLASS_COLORS, GLASS_HIDDEN } from '../data/glassConf'
import { imgURL } from '../components/icons'
import Dropdown from '../components/Dropdown'
import AdminCharts from './AdminCharts'

// glassOf 把色卡选择器的选择换算成后端接口的 glassType/glassValue。
// random -> 0/0(后端随机合法色卡);common -> 1 + (粒子id<<20)|配色id;hidden -> 2 + 赛季id(1/2/3、1000)。
const glassOf = (g) => {
  if (g.type === 'common') return { glassType: 1, glassValue: (g.particle << 20) | g.color }
  if (g.type === 'hidden') return { glassType: 2, glassValue: g.hidden }
  return { glassType: 0, glassValue: 0 }
}

// fmtGlassName 从素材文件名里抠出展示名("img_dazzling_Bg3_png-四角星.png" -> "四角星")。
const fmtGlassName = (name) => (name.split('-')[1] || name).replace(/\.png$/, '')

// GlassPicker 炫彩色卡选择器:随机 / 普通炫彩(粒子+配色) / 隐藏炫彩(赛季整图)。
// value={type, particle, color, hidden};普通/隐藏模式附带小预览(双色渐变块 / 整图缩略)。
function GlassPicker({ value, onChange }) {
  const set = (patch) => onChange({ ...value, ...patch })
  return (
    <div className="admin-glass-picker">
      <Dropdown
        value={value.type}
        options={[
          { value: 'random', label: '随机色卡' },
          { value: 'common', label: '普通炫彩' },
          { value: 'hidden', label: '隐藏炫彩' },
        ]}
        onChange={(v) => set({ type: v })}
        title="炫彩色卡类型"
      />
      {value.type === 'common' && (
        <>
          <Dropdown
            value={value.particle}
            options={Object.entries(GLASS_PARTICLES).map(([id, name]) => ({ value: Number(id), label: fmtGlassName(name) }))}
            onChange={(v) => set({ particle: Number(v) })}
            title="粒子形状"
          />
          <Dropdown
            value={value.color}
            options={Object.keys(GLASS_COLORS).map((id) => ({ value: Number(id), label: `配色${id}` }))}
            onChange={(v) => set({ color: Number(v) })}
            title="配色(共 39 组)"
          />
          <span
            className="admin-glass-preview"
            style={{ background: `linear-gradient(90deg, ${GLASS_COLORS[value.color][0]}, ${GLASS_COLORS[value.color][1]})` }}
            title={`配色${value.color} 粒子${value.particle}`}
          />
        </>
      )}
      {value.type === 'hidden' && (
        <>
          <Dropdown
            value={value.hidden}
            options={Object.entries(GLASS_HIDDEN).map(([id, name]) => ({ value: Number(id), label: fmtGlassName(name) }))}
            onChange={(v) => set({ hidden: Number(v) })}
            title="隐藏炫彩(赛季整图)"
          />
          <img className="admin-glass-preview" src={imgURL('dazzling/' + GLASS_HIDDEN[value.hidden])} alt="隐藏炫彩" title="隐藏炫彩整图" />
        </>
      )}
    </div>
  )
}

// fmtDur 把秒格式化为「X小时Y分 / Y分Z秒 / Z秒」。
const fmtDur = (s) => {
  if (s == null || s < 0) return '-'
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  const sec = s % 60
  if (h > 0) return `${h}小时${m}分`
  if (m > 0) return `${m}分${sec}秒`
  return `${sec}秒`
}

// fmtTime 把 Unix 秒格式化为「MM-DD HH:mm」。
const fmtTime = (ts) => {
  if (ts == null) return '-'
  const d = new Date(ts * 1000)
  const p = (n) => String(n).padStart(2, '0')
  return `${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

// PlayDailyChart 近14天每日游玩时长 SVG 柱状图:坐标轴 + 网格线 + 渐变圆角柱 + hover 高亮。
// daily 结构见 api.adminPlaySessions:[{day,sessions,duration}]。纯 SVG 实现,不引入图表库。
const PlayDailyChart = ({ daily }) => {
  const W = 560, H = 200
  const PL = 44, PR = 8, PT = 14, PB = 28 // 内边距:左(Y轴标签)/右/上/下(日期标签)
  const iw = W - PL - PR, ih = H - PT - PB
  const max = Math.max(...daily.map((d) => d.duration), 1)
  const slot = iw / daily.length
  const barW = Math.min(26, slot * 0.62)
  // 坐标轴刻度的短时长格式:≥1h 显示 Xh,≥1m 显示 Xm,否则 Xs。
  const fmtAxis = (s) => {
    if (s <= 0) return '0'
    const h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60)
    if (h > 0) return h + 'h'
    if (m > 0) return m + 'm'
    return s + 's'
  }
  // 网格线位置:100% / 50% / 0 三档。
  const ticks = [1, 0.5, 0].map((f) => ({ y: PT + ih * (1 - f), f }))
  return (
    <div className="admin-play-chart">
      <svg viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="xMidYMid meet" role="img" aria-label="近14天每日游玩时长柱状图">
        <defs>
          <linearGradient id="playBarGrad" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor="#5aa9e6" />
            <stop offset="100%" stopColor="#3d7ab8" />
          </linearGradient>
        </defs>
        {ticks.map((t) => (
          <g key={t.f}>
            <line className="admin-play-svg-grid" x1={PL} y1={t.y} x2={W - PR} y2={t.y} />
            <text className="admin-play-svg-label" x={PL - 6} y={t.y + 3} textAnchor="end">{fmtAxis(max * t.f)}</text>
          </g>
        ))}
        <line className="admin-play-svg-axis" x1={PL} y1={PT + ih} x2={W - PR} y2={PT + ih} />
        {daily.map((d, i) => {
          const h = d.duration > 0 ? Math.max((d.duration / max) * ih, 2) : 2
          const x = PL + i * slot + (slot - barW) / 2
          const y = PT + ih - h
          const isToday = i === daily.length - 1
          return (
            <g key={d.day}>
              <title>{`${d.day}: ${d.sessions} 次会话,共 ${fmtDur(d.duration)}`}</title>
              {d.duration > 0 ? (
                <rect
                  className="admin-play-svg-bar" x={x} y={y} width={barW} height={h} rx={3}
                  fill="url(#playBarGrad)"
                  stroke={isToday ? '#6ab8ff' : 'none'} strokeWidth={isToday ? 1.5 : 0}
                />
              ) : (
                <rect className="admin-play-svg-bar-zero" x={x} y={PT + ih - 2} width={barW} height={2} rx={1} />
              )}
              <text className="admin-play-svg-label" x={x + barW / 2} y={PT + ih + 16} textAnchor="middle">{d.day.slice(5)}</text>
            </g>
          )
        })}
      </svg>
    </div>
  )
}

// EggDailyChart 查蛋 API 近14天每日查询次数 CSS 柱状图:绿色=成功,红色=失败,叠加显示。
// daily 结构见 api.adminEggStats:[{day,total,ok}]。
const EggDailyChart = ({ daily }) => {
  const max = Math.max(...daily.map((d) => d.total), 1)
  return (
    <div className="egg-daily">
      {daily.map((d) => {
        const okH = (d.ok / max) * 100
        const failH = ((d.total - d.ok) / max) * 100
        return (
          <div key={d.day} className="egg-daily-col" title={`${d.day}:共 ${d.total} 次,成功 ${d.ok},失败 ${d.total - d.ok}`}>
            <div className="egg-daily-bar">
              {d.total > 0 && (
                <>
                  <div className="egg-daily-fill ok" style={{ height: `${okH}%` }} />
                  {failH > 0 && <div className="egg-daily-fill fail" style={{ height: `${failH}%` }} />}
                </>
              )}
            </div>
            <span className="egg-daily-day">{d.day.slice(5)}</span>
          </div>
        )
      })}
    </div>
  )
}

// 管理员面板(隐式入口:导航不显示,需手动输入 #/admin 进入)。
// 首次进入引导设置密码,之后凭密码登录;面板内其余功能留空待实现。
export default function Admin() {
  const [loading, setLoading] = useState(true)   // 状态拉取中
  const [configured, setConfigured] = useState(false)
  const [authed, setAuthed] = useState(getAdminToken() !== '')
  const [password, setPassword] = useState('')
  const [confirmPw, setConfirmPw] = useState('')
  const [error, setError] = useState('')
  const [rules, setRules] = useState(null)      // 黑白名单规则列表
  const [ruleErr, setRuleErr] = useState('')
  const [rAccount, setRAccount] = useState('')
  const [rMode, setRMode] = useState('black')
  const [rNote, setRNote] = useState('')
  const [stats, setStats] = useState(null)      // 成员抓捕图表数据
  const [statsErr, setStatsErr] = useState('')
  // 游玩记录:玩家上/下线时间与游玩时长
  const [plays, setPlays] = useState(null)      // {sessions:[], summary:{}}
  const [playErr, setPlayErr] = useState('')
  const [playAccount, setPlayAccount] = useState('') // 明细账号筛选(空=全部)
  // 查蛋 API 使用统计(第三方图鉴,一次查蛋烧一次对方 token)
  const [eggStats, setEggStats] = useState(null)   // {total,todayTotal,todayOK,todayFail,successRate,daily,byAccount,recent,keySet}
  const [eggErr, setEggErr] = useState('')
  // 投放稀有野生精灵
  const [wildOptions, setWildOptions] = useState(null)
  const [accounts, setAccounts] = useState([])
  const [injAccount, setInjAccount] = useState('')
  const [injBase, setInjBase] = useState('')
  const [injKind, setInjKind] = useState('shiny')
  const [injOffset, setInjOffset] = useState(30)
  const [injLevel, setInjLevel] = useState(0) // 0=随机 30-60;指定 1-100 固定等级
  const [injErr, setInjErr] = useState('')
  const [injMsg, setInjMsg] = useState('')
  const [injGlass, setInjGlass] = useState({ type: 'random', particle: 1, color: 1, hidden: 1 }) // 炫彩投放色卡
  const [injects, setInjects] = useState(null)  // 当前注入中的精灵列表(管理面板撤销)
  // 投放假炫彩花种
  const [flAccount, setFlAccount] = useState('')
  const [flBase, setFlBase] = useState('')
  const [flStar, setFlStar] = useState(7) // 花种星级 1-7
  const [flGlass, setFlGlass] = useState({ type: 'random', particle: 1, color: 1, hidden: 1 })
  const [flErr, setFlErr] = useState('')
  const [flMsg, setFlMsg] = useState('')
  // 账号 PIN 管理 + 账号删除
  const [pinErr, setPinErr] = useState('')
  const [pinMsg, setPinMsg] = useState('')
  const [pinEditing, setPinEditing] = useState(null) // 正在设 PIN 的账号 key
  const [pinValue, setPinValue] = useState('')
  // 邮箱推送名单(远行商人订阅)+ 测试邮件
  const [merchantSubs, setMerchantSubs] = useState(null) // {configured, subs:[{email,account,keywords,created_at}]}
  const [subErr, setSubErr] = useState('')
  const [subMsg, setSubMsg] = useState('')
  const [testEmail, setTestEmail] = useState('')
  const [testSubject, setTestSubject] = useState('')
  const [testBody, setTestBody] = useState('')
  const [testBusy, setTestBusy] = useState(false)
  const [forceBusy, setForceBusy] = useState(false) // 强制刷新商人数据(回源第三方)

  useEffect(() => {
    getAdminStatus().then((s) => {
      setConfigured(!!s.configured)
      setAuthed(!!s.authed || getAdminToken() !== '')
    }).catch(() => {})
      .finally(() => setLoading(false))
  }, [])

  // 登录后拉规则列表与抓捕图表,验证令牌可用(失败视为会话失效)。
  useEffect(() => {
    if (!authed) return
    loadRules()
    loadStats()
    loadPlaySessions()
    loadEggStats()
    loadWildOptions()
    loadAccounts()
    loadInjects()
    loadMerchantSubs()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [authed])

  // 仅 401(令牌失效/未登录)才视为会话失效踢回登录页;其余错误(404/500/网络)只展示,不打断面板。
  const kickIfUnauthed = (err) => { if (err.status === 401) setAuthed(false) }

  const loadRules = () => {
    setRuleErr('')
    adminRules().then((d) => setRules(d.rules || []))
      .catch((err) => { setRuleErr(err.message); kickIfUnauthed(err) })
  }

  const loadStats = () => {
    setStatsErr('')
    adminStats().then(setStats)
      .catch((err) => { setStatsErr(err.message); kickIfUnauthed(err) })
  }

  const loadPlaySessions = () => {
    setPlayErr('')
    adminPlaySessions(playAccount).then(setPlays)
      .catch((err) => { setPlayErr(err.message); kickIfUnauthed(err) })
  }

  const loadEggStats = () => {
    setEggErr('')
    adminEggStats().then(setEggStats)
      .catch((err) => { setEggErr(err.message); kickIfUnauthed(err) })
  }

  const loadWildOptions = () => {
    adminWildPetOptions().then((d) => setWildOptions(d.options || []))
      .catch(() => {})
  }

  const loadMerchantSubs = () => {
    setSubErr('')
    adminMerchantSubs().then(setMerchantSubs)
      .catch((err) => { setSubErr(err.message); kickIfUnauthed(err) })
  }

  // 强制刷新商人数据:绕过后端缓存,强制后端重新向第三方抓取当前轮(烧 token,仅维护用)。
  const forceMerchant = async () => {
    setSubErr(''); setSubMsg('')
    setForceBusy(true)
    try {
      const d = await getMerchant(true)
      setSubMsg('已强制刷新商人数据(' + (d.status === 'open' ? '当前营业中' : '当前打烊') + '),商人页下次打开即为最新。')
    } catch (err) {
      setSubErr(err.message || '强制刷新失败')
    } finally {
      setForceBusy(false)
    }
  }

  // 发送测试邮件:验证 SMTP 配置(发件邮箱/授权码)是否可用;主题/正文可自填,留空用默认。
  const sendTestMail = async (e) => {
    e.preventDefault()
    setSubErr(''); setSubMsg('')
    const email = testEmail.trim()
    if (!email) return
    setTestBusy(true)
    try {
      await adminTestMail(email, testSubject.trim(), testBody.trim())
      setSubMsg('测试邮件已发送到 ' + email + ',请检查收件箱(含垃圾箱)。')
    } catch (err) {
      setSubErr(err.message || '发送失败')
    } finally {
      setTestBusy(false)
    }
  }

  // 从推送名单删除订阅(不再向其发提醒)。
  const removeSub = async (email) => {
    setSubErr(''); setSubMsg('')
    if (!window.confirm('确认删除 ' + email + ' 的订阅?删除后不再向其发送新货提醒。')) return
    try {
      await adminMerchantSubDelete(email)
      setSubMsg('已删除订阅:' + email)
      loadMerchantSubs()
    } catch (err) {
      setSubErr(err.message || '删除失败')
    }
  }

  const loadAccounts = () => {
    getAccounts().then((list) => {
      const l = list || []
      setAccounts(l)
      // 默认选中在线玩家(有实时位置才可投放);无在线则退回最近活跃的第一个。
      setInjAccount((prev) => (prev && l.some((a) => a.account === prev)
        ? prev : (l.find((a) => a.online) || l[0] || {}).account || ''))
    }).catch(() => {})
  }

  const injectWild = async (e) => {
    e.preventDefault()
    const account = injAccount.trim()
    if (!account || !injBase) return
    setInjErr(''); setInjMsg('')
    try {
      const { glassType, glassValue } = glassOf(injGlass)
      const res = await adminInjectWild(
        account, Number(injBase), injKind, Number(injOffset) || 30, Number(injLevel) || 0, glassType, glassValue,
      )
      setInjMsg('已投放:' + (res.id || '') + '(u=' + (res.u != null ? res.u.toFixed(3) : '') + ')')
      loadInjects()
    } catch (err) {
      setInjErr(err.message || '投放失败')
    }
  }

  // 投放假炫彩花种:向目标成员的花种页注入一只特殊花种(默认 7 星,星级可自定义),不要求其在线。
  const injectFlower = async (e) => {
    e.preventDefault()
    const account = flAccount.trim()
    if (!account || !flBase) return
    setFlErr(''); setFlMsg('')
    try {
      const { glassType, glassValue } = glassOf(flGlass)
      const res = await adminInjectFlower(account, Number(flBase), Number(flStar), glassType, glassValue)
      setFlMsg('已投放花种:' + (res.id || '') + '(npcLogicId=' + res.npcLogicId + ')')
      loadInjects()
    } catch (err) {
      setFlErr(err.message || '投放失败')
    }
  }

  // 注入精灵是后端内存态(不落盘):玩家换场景或靠近 10 米 10 秒后自动消失。
  const loadInjects = () => {
    adminListInjects().then((d) => setInjects(d.injects || []))
      .catch(() => {})
  }

  const revokeInject = async (account, id) => {
    setInjErr('')
    try {
      await adminRevokeInject(account, id)
      setInjMsg('已撤销注入:' + id)
      loadInjects()
    } catch (err) {
      setInjErr(err.message || '撤销失败')
    }
  }

  // 管理员设 PIN(免旧 PIN):设/改
  const adminSetPin = async (account) => {
    setPinErr(''); setPinMsg('')
    if (!pinValue || pinValue.length < 4) { setPinErr('PIN 至少 4 位'); return }
    try {
      await setAccountPin(account, '', pinValue)
      setPinMsg(account + ' PIN 已设置')
      setPinEditing(null); setPinValue('')
      loadAccounts()
    } catch (err) { setPinErr(err.message || '设置失败') }
  }

  // 管理员清 PIN(免旧 PIN)
  const adminClearPin = async (account) => {
    setPinErr(''); setPinMsg('')
    try {
      await setAccountPin(account, '', '')
      setPinMsg(account + ' PIN 已清除')
      loadAccounts()
    } catch (err) { setPinErr(err.message || '清除失败') }
  }

  // 管理员删账号(免 PIN)
  const adminDelete = async (account) => {
    setPinErr(''); setPinMsg('')
    if (!window.confirm('确认删除账号 ' + account + ' 的全部数据?此操作不可恢复。')) return
    try {
      await deleteAccount(account)
      setPinMsg(account + ' 已删除')
      loadAccounts()
    } catch (err) { setPinErr(err.message || '删除失败') }
  }

  const addRule = async (e) => {
    e.preventDefault()
    const account = rAccount.trim()
    if (!account) return
    setRuleErr('')
    try {
      await adminSetRule(account, rMode, rNote.trim())
      setRAccount('')
      setRNote('')
      loadRules()
    } catch (err) {
      setRuleErr(err.message || '添加失败')
    }
  }

  const removeRule = async (account) => {
    setRuleErr('')
    try {
      await adminDeleteRule(account)
      loadRules()
    } catch (err) {
      setRuleErr(err.message || '删除失败')
    }
  }

  const submit = async (e) => {
    e.preventDefault()
    setError('')
    if (!configured && password !== confirmPw) {
      setError('两次输入的密码不一致')
      return
    }
    try {
      const res = configured ? await adminLogin(password) : await adminSetup(password)
      setAdminToken(res.token)
      setPassword('')
      setConfirmPw('')
      setAuthed(true)
      // 管理员免密标记:本会话已通过管理员认证,之后切到有 PIN 的账号直接放行(见 App.jsx)
      sessionStorage.setItem('admin-unlocked', '1')
    } catch (err) {
      setError(err.message || '操作失败')
    }
  }

  const logout = async () => {
    await adminLogout()
    sessionStorage.removeItem('admin-unlocked')
    setAuthed(false)
    setRules(null)
    setStats(null)
    setPlays(null)
    setPlayAccount('')
    setWildOptions(null)
    setAccounts([])
    setInjAccount('')
    setInjects(null)
    setMerchantSubs(null)
    setSubErr(''); setSubMsg('')
    setTestEmail(''); setTestSubject(''); setTestBody(''); setTestBusy(false); setForceBusy(false)
  }

  if (loading) return <div className="admin-page"><p className="admin-hint">加载中…</p></div>

  if (!authed) {
    return (
      <div className="admin-page admin-login">
        <div className="admin-card">
          <h2>{configured ? '管理员登录' : '设置管理员密码'}</h2>
          <p className="admin-hint">
            {configured
              ? '输入管理员密码进入管理面板'
              : '首次进入,请设置管理员密码(至少 4 位)'}
          </p>
          <form onSubmit={submit}>
            <input
              className="input" type="password" placeholder="密码" value={password}
              onChange={(e) => setPassword(e.target.value)} autoFocus
            />
            {!configured && (
              <input
                className="input" type="password" placeholder="确认密码" value={confirmPw}
                onChange={(e) => setConfirmPw(e.target.value)}
              />
            )}
            {error && <p className="admin-error">{error}</p>}
            <button className="btn primary" type="submit" disabled={!password}>
              {configured ? '登录' : '设置并进入'}
            </button>
          </form>
        </div>
      </div>
    )
  }

  // 已登录:管理面板主体。
  return (
    <div className="admin-page">
      <div className="admin-head">
        <h2>管理面板</h2>
        <button className="btn" onClick={logout}>退出登录</button>
      </div>

      <div className="admin-section-title">数据统计</div>
      <div className="admin-card admin-rules admin-wide">
        <h3>成员抓捕图表</h3>
        <p className="admin-hint">所有成员累计抓捕精灵情况(来源:获得宠物事件,近30天时间轴)。</p>
        {statsErr && <p className="admin-error">{statsErr}</p>}
        <AdminCharts data={stats} />
      </div>

      <div className="admin-card admin-rules admin-wide">
        <h3>游玩记录</h3>
        <p className="admin-hint">
          自动记录玩家每次上线的起止时间与游玩时长(来源:连接登录/断开与心跳活跃,近14天每日聚合)。
          挂后台或断线超过 90 秒无流量判定一次下线,再次活跃自动续记新会话。
        </p>
        <div className="admin-play-summary">
          <div className="admin-play-stat">
            <b>{plays && plays.summary ? plays.summary.online : '-'}</b>
            <span>当前在线</span>
          </div>
          <div className="admin-play-stat">
            <b>{plays && plays.summary ? plays.summary.todaySessions : '-'}</b>
            <span>今日会话</span>
          </div>
          <div className="admin-play-stat">
            <b>{plays && plays.summary ? fmtDur(plays.summary.todayDuration) : '-'}</b>
            <span>今日游玩时长</span>
          </div>
        </div>
        {plays && plays.summary && plays.summary.daily && plays.summary.daily.length > 0 && (
          <PlayDailyChart daily={plays.summary.daily} />
        )}
        <div className="admin-play-toolbar">
          <Dropdown
            value={playAccount}
            options={[
              { value: '', label: '全部账号' },
              ...accounts.map((a) => ({ value: a.account, label: `${a.name || a.account} (UID:${(a.account || '').replace(/^UID:/, '')})` })),
            ]}
            onChange={(v) => { setPlayAccount(v); loadPlaySessions() }}
            title="按账号筛选游玩记录(空=全部)"
          />
          <button className="btn" onClick={loadPlaySessions}>刷新</button>
        </div>
        {playErr && <p className="admin-error">{playErr}</p>}
        {plays === null
          ? <p className="admin-hint">加载中…</p>
          : plays.sessions.length === 0
            ? <p className="admin-hint">暂无游玩记录(登录游戏并产生流量后自动生成)。</p>
            : (
              <table className="admin-play-table">
                <thead>
                  <tr>
                    <th>玩家</th>
                    <th>上线时间</th>
                    <th>下线时间</th>
                    <th>游玩时长</th>
                  </tr>
                </thead>
                <tbody>
                  {plays.sessions.map((s) => (
                    <tr key={s.account + ':' + s.loginTime}>
                      <td>{s.name || s.account} <span className="muted">{(s.account || '').replace(/^UID:/, '')}</span></td>
                      <td>{fmtTime(s.loginTime)}</td>
                      <td>{s.online ? <span className="play-online">在线中</span> : fmtTime(s.logoutTime)}</td>
                      <td>{s.online ? '—' : fmtDur(s.duration)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
      </div>

      <div className="admin-card admin-rules admin-wide">
        <h3>查蛋 API 统计</h3>
        <p className="admin-hint">
          孵蛋页「查蛋」代理第三方图鉴,每次查询消耗一次对方 token,统计已发起第三方请求的调用。
          {eggStats && eggStats.keySet === false && '当前服务端未配置 -egg-api-key,统计恒为 0。'}
        </p>
        {eggErr && <p className="admin-error">{eggErr}</p>}
        {eggStats === null
          ? <p className="admin-hint">加载中…</p>
          : (
            <>
              <div className="admin-play-summary">
                <div className="admin-play-stat">
                  <b>{eggStats.total}</b>
                  <span>累计查询</span>
                </div>
                <div className="admin-play-stat">
                  <b>{eggStats.todayTotal}</b>
                  <span>今日查询</span>
                </div>
                <div className="admin-play-stat">
                  <b>{eggStats.todayOK}</b>
                  <span>今日成功</span>
                </div>
                <div className="admin-play-stat">
                  <b>{eggStats.todayFail}</b>
                  <span>今日失败</span>
                </div>
                <div className="admin-play-stat">
                  <b>{eggStats.successRate}%</b>
                  <span>成功率</span>
                </div>
              </div>
              {eggStats.daily && eggStats.daily.length > 0 && (
                <EggDailyChart daily={eggStats.daily} />
              )}
              {eggStats.total === 0 && <p className="admin-hint">还没有查蛋记录(玩家在孵蛋页点「查蛋」后出现)。</p>}
              {eggStats.byAccount && eggStats.byAccount.length > 0 && (
                <>
                  <h4>按账号排行</h4>
                  <table className="admin-play-table">
                    <thead>
                      <tr><th>玩家</th><th>累计查询</th><th>今日</th></tr>
                    </thead>
                    <tbody>
                      {eggStats.byAccount.map((a) => (
                        <tr key={a.account}>
                          <td>{a.name || a.account} <span className="muted">{(a.account || '').replace(/^UID:/, '')}</span></td>
                          <td>{a.total}</td>
                          <td>{a.today}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </>
              )}
              {eggStats.recent && eggStats.recent.length > 0 && (
                <>
                  <h4>最近查询</h4>
                  <table className="admin-play-table">
                    <thead>
                      <tr><th>时间</th><th>玩家</th><th>身高/体重</th><th>匹配</th><th>耗时</th><th>结果</th></tr>
                    </thead>
                    <tbody>
                      {eggStats.recent.map((rec, i) => (
                        <tr key={i}>
                          <td>{fmtTime(rec.time)}</td>
                          <td>{rec.name || rec.account} <span className="muted">{(rec.account || '').replace(/^UID:/, '')}</span></td>
                          <td>{rec.height || '-'} / {rec.weight || '-'}</td>
                          <td>{rec.matches}</td>
                          <td>{rec.costMs}ms</td>
                          <td>{rec.ok ? <span className="play-online">成功</span> : <span className="egg-fail">失败</span>}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </>
              )}
              <div className="admin-play-toolbar">
                <button className="btn" onClick={loadEggStats}>刷新</button>
              </div>
            </>
          )}
      </div>

      <div className="admin-section-title">投放管理</div>
      <div className="admin-card admin-rules">
        <h3>投放稀有野生精灵</h3>
        <p className="admin-hint">
          向下拉选定的成员实时地图注入一只稀有野生精灵(异色 / 炫彩)。
          位置取该成员最近一次缓存位置,按当前场景投影到附近 30 米处;仅影响前端地图显示,
          不修改真实流量。成员需在「有底图的场景」中且有缓存位置才能投放成功;异色只列有异色形态的精灵。
        </p>
        <form onSubmit={injectWild} className="admin-inject-form">
          <Dropdown
            value={injAccount}
            options={[
              { value: '', label: '选择目标玩家' },
              ...accounts.map((a) => ({ value: a.account, label: `${a.online ? '🟢 ' : '🟥 '}${a.name} (UID:${(a.account || '').replace(/^UID:/, '')})` })),
            ]}
            onChange={(v) => setInjAccount(v)}
            title="选择投放目标玩家(需在线且有缓存位置)"
          />
          <Dropdown
            value={injBase}
            options={[
              { value: '', label: '选择精灵形态' },
              ...(wildOptions || [])
                .filter((o) => injKind !== 'shiny' || o.shiny) // 异色只列有异色形态的精灵
                .map((o) => ({ value: o.base, label: `${o.name}(#${o.book})` })),
            ]}
            onChange={(v) => setInjBase(v)}
          />
          <Dropdown
            value={injKind}
            options={[
              { value: 'shiny', label: '异色' },
              { value: 'colorful', label: '炫彩' },
            ]}
            onChange={(v) => { setInjKind(v); setInjBase('') }}
          />
          {/* 炫彩色卡设置:仅炫彩投放时展示;选择后投出的精灵在角标/悬浮面板/详情里显示对应色卡 */}
          {injKind === 'colorful' && <GlassPicker value={injGlass} onChange={setInjGlass} />}
          <input
            className="input" type="number" min="1" max="200" value={injOffset}
            onChange={(e) => setInjOffset(e.target.value)} title="距玩家位置米数"
          />
          <input
            className="input" type="number" min="0" max="100" value={injLevel}
            onChange={(e) => setInjLevel(e.target.value)}
            title="等级(0=随机 30-60,指定 1-100 固定该等级)" placeholder="Lv(0=随机)"
          />
          <button className="btn primary" type="submit" disabled={!injAccount.trim() || !injBase}>
            投放
          </button>
        </form>
        {injErr && <p className="admin-error">{injErr}</p>}
        {injMsg && <p className="admin-hint" style={{ color: 'var(--green, #4caf50)' }}>{injMsg}</p>}
        {injects && injects.length > 0 && (
          <div className="admin-inject-list">
            <h4>当前注入中({injects.length})</h4>
            <ul>
              {injects.map((it) => (
                <li key={it.account + ':' + it.id}>
                  <span className="admin-inject-name">{it.name}</span>
                  <span className="admin-inject-kind">{(it.kinds || []).join('/') || '普通'}</span>
                  {it.kind === 'flower' && <span className="admin-inject-flower">花种</span>}
                  {it.kind !== 'flower' && <span className="muted admin-inject-scene">{it.sceneRes || '无底图'}</span>}
                  <button className="btn ghost" onClick={() => revokeInject(it.account, it.id)}>撤销</button>
                </li>
              ))}
            </ul>
          </div>
        )}
      </div>

      <div className="admin-card admin-rules">
        <h3>投放假炫彩花种</h3>
        <p className="admin-hint">
          向选定成员的花种页注入一只假花种(默认 7 星特殊花灵,星级可自定义),携带指定或
          随机炫彩色卡,卡片与真实花种无异,点开可查看完整色卡。不要求成员在线,不修改
          真实流量;生命周期由管理员在此手动撤销(见上方「当前注入中」列表)。
        </p>
        <form onSubmit={injectFlower} className="admin-inject-form">
          <Dropdown
            value={flAccount}
            options={[
              { value: '', label: '选择目标玩家' },
              ...accounts.map((a) => ({ value: a.account, label: `${a.online ? '🟢 ' : '🟥 '}${a.name} (UID:${(a.account || '').replace(/^UID:/, '')})` })),
            ]}
            onChange={(v) => setFlAccount(v)}
            title="选择投放目标玩家(无需在线)"
          />
          <Dropdown
            value={flBase}
            options={[
              { value: '', label: '选择守护宠物' },
              ...(wildOptions || []).map((o) => ({ value: o.base, label: `${o.name}(#${o.book})` })),
            ]}
            onChange={(v) => setFlBase(v)}
          />
          <Dropdown
            value={flStar}
            options={[1, 2, 3, 4, 5, 6, 7].map((n) => ({ value: n, label: `${n} 星${n === 7 ? '(花灵 BOSS)' : ''}` }))}
            onChange={(v) => setFlStar(Number(v))}
            title="花种星级(1-7)"
          />
          <GlassPicker value={flGlass} onChange={setFlGlass} />
          <button className="btn primary" type="submit" disabled={!flAccount.trim() || !flBase}>
            投放
          </button>
        </form>
        {flErr && <p className="admin-error">{flErr}</p>}
        {flMsg && <p className="admin-hint" style={{ color: 'var(--green, #4caf50)' }}>{flMsg}</p>}
      </div>

      <div className="admin-section-title">账号与规则</div>
      <div className="admin-card admin-rules">
        <h3>黑白名单</h3>
        <p className="admin-hint">
          黑名单账号的流量被完全丢弃(不统计、不显示在线);白名单非空时只处理白名单内账号,
          黑名单优先。账号格式与线上一致,如 <code>UID:10001</code>。
        </p>
        <form onSubmit={addRule} className="admin-rule-form">
          <input
            className="input" type="text" placeholder="账号(如 UID:10001)" value={rAccount}
            onChange={(e) => setRAccount(e.target.value)}
          />
          <Dropdown
            value={rMode}
            options={[
              { value: 'black', label: '黑名单' },
              { value: 'white', label: '白名单' },
            ]}
            onChange={(v) => setRMode(v)}
          />
          <input
            className="input" type="text" placeholder="备注(可选)" value={rNote}
            onChange={(e) => setRNote(e.target.value)}
          />
          <button className="btn primary" type="submit" disabled={!rAccount.trim()}>添加</button>
        </form>
        {ruleErr && <p className="admin-error">{ruleErr}</p>}
        {rules === null
          ? <p className="admin-hint">加载中…</p>
          : rules.length === 0
            ? <p className="admin-hint">暂无规则(当前所有账号均被处理)。</p>
            : (
              <ul className="admin-rule-list">
                {rules.map((r) => (
                  <li key={r.account}>
                    <span className={'rule-badge ' + r.mode}>{r.mode === 'black' ? '黑' : '白'}</span>
                    <code className="rule-account">{r.account}</code>
                    {r.note && <span className="rule-note">{r.note}</span>}
                    <button className="btn ghost" onClick={() => removeRule(r.account)}>删除</button>
                  </li>
                ))}
              </ul>
            )}
      </div>

      <div className="admin-card admin-rules admin-wide">
        <h3>邮箱推送名单(远行商人订阅)</h3>
        <p className="admin-hint">
          玩家在「远行商人」页登记新货邮件提醒后,名单出现在这里。每轮(8/12/16/20 点)新增商品
          上架时,后端自动向名单内邮箱发提醒;0~8 点打烊不提醒。发件邮箱与授权码由
          <code> -merchant-smtp-user / -merchant-smtp-pass </code>配置。
          {merchantSubs && !merchantSubs.configured && (
            <span style={{ color: 'var(--danger, #e5534b)' }}> ⚠ 服务端未配置 SMTP,发送会失败。</span>
          )}
        </p>
        {/* 测试邮件:验证发件配置,主题/正文可自填 */}
        <form onSubmit={sendTestMail} className="admin-test-form">
          <input
            className="input" type="email" placeholder="测试收件邮箱(如 123@qq.com)" value={testEmail}
            onChange={(e) => setTestEmail(e.target.value)}
          />
          <input
            className="input" placeholder="主题(留空用默认)" value={testSubject}
            onChange={(e) => setTestSubject(e.target.value)}
          />
          <textarea
            className="input" rows={3} placeholder="邮件内容(留空用默认)" value={testBody}
            onChange={(e) => setTestBody(e.target.value)}
          />
          <button className="btn primary" type="submit" disabled={!testEmail.trim() || testBusy}>
            {testBusy ? '发送中…' : '发送测试邮件'}
          </button>
        </form>
        {/* 强制刷新:回源第三方重抓商人数据(烧对方额度,仅维护用) */}
        <div className="admin-play-toolbar">
          <button className="btn" type="button" onClick={forceMerchant} disabled={forceBusy}
            title="绕过后端缓存,强制后端重新向第三方抓取当前轮商人数据(烧对方额度,非必要别点)">
            {forceBusy ? '强制刷新中…' : '强制刷新商人数据'}
          </button>
          <span className="admin-hint">绕过后端缓存,强制后端重新向第三方抓取当前轮商人数据(烧对方额度,非必要别点)。</span>
        </div>
        {subErr && <p className="admin-error">{subErr}</p>}
        {subMsg && <p className="admin-hint" style={{ color: 'var(--green, #4caf50)' }}>{subMsg}</p>}
        {merchantSubs === null
          ? <p className="admin-hint">加载中…</p>
          : merchantSubs.subs.length === 0
            ? <p className="admin-hint">暂无订阅(玩家在远行商人页登记后显示)。</p>
            : (
              <table className="admin-play-table">
                <thead>
                  <tr>
                    <th>账号</th>
                    <th>邮箱</th>
                    <th>关键词</th>
                    <th>订阅时间</th>
                    <th>操作</th>
                  </tr>
                </thead>
                <tbody>
                  {merchantSubs.subs.map((s) => (
                    <tr key={s.account}>
                      <td>{s.account}</td>
                      <td>{s.email}</td>
                      <td>{s.keywords || <span className="muted">全部</span>}</td>
                      <td>{fmtTime(s.created_at)}</td>
                      <td>
                        <button className="btn ghost" onClick={() => removeSub(s.email)}>删除</button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
      </div>

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
              <span className="rule-note">UID:{(a.account || '').replace(/^UID:/, '')}</span>
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
    </div>
  )
}
