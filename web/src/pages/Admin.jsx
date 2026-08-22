import React, { useEffect, useState } from 'react'
import {
  getAdminStatus, adminSetup, adminLogin, adminLogout,
  getAdminToken, setAdminToken, adminRules, adminSetRule, adminDeleteRule,
  adminStats, adminWildPetOptions, adminInjectWild, adminListInjects, adminRevokeInject,
  getAccounts, setAccountPin, deleteAccount,
} from '../api'
import AdminCharts from './AdminCharts'

// 管理员面板(隐式入口:导航不显示,需手动输入 #/admin 进入)。
// 首次进入引导设置密码,之后凭密码登录;面板内其余功能留空待实现。
export default function Admin() {
  const [loading, setLoading] = useState(true)   // 状态拉取中
  const [configured, setConfigured] = useState(false)
  const [authed, setAuthed] = useState(getAdminToken() !== '')
  const [password, setPassword] = useState('')
  const [confirm, setConfirm] = useState('')
  const [error, setError] = useState('')
  const [rules, setRules] = useState(null)      // 黑白名单规则列表
  const [ruleErr, setRuleErr] = useState('')
  const [rAccount, setRAccount] = useState('')
  const [rMode, setRMode] = useState('black')
  const [rNote, setRNote] = useState('')
  const [stats, setStats] = useState(null)      // 成员抓捕图表数据
  const [statsErr, setStatsErr] = useState('')
  // 投放稀有野生精灵
  const [wildOptions, setWildOptions] = useState(null)
  const [accounts, setAccounts] = useState([])
  const [injAccount, setInjAccount] = useState('')
  const [injBase, setInjBase] = useState('')
  const [injKind, setInjKind] = useState('shiny')
  const [injOffset, setInjOffset] = useState(30)
  const [injErr, setInjErr] = useState('')
  const [injMsg, setInjMsg] = useState('')
  const [injects, setInjects] = useState(null)  // 当前注入中的精灵列表(管理面板撤销)
  // 账号 PIN 管理 + 账号删除
  const [pinErr, setPinErr] = useState('')
  const [pinMsg, setPinMsg] = useState('')
  const [pinEditing, setPinEditing] = useState(null) // 正在设 PIN 的账号 key
  const [pinValue, setPinValue] = useState('')

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
    loadWildOptions()
    loadAccounts()
    loadInjects()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [authed])

  const loadRules = () => {
    setRuleErr('')
    adminRules().then((d) => setRules(d.rules || []))
      .catch((err) => { setRuleErr(err.message); setAuthed(false) })
  }

  const loadStats = () => {
    setStatsErr('')
    adminStats().then(setStats)
      .catch((err) => { setStatsErr(err.message); setAuthed(false) })
  }

  const loadWildOptions = () => {
    adminWildPetOptions().then((d) => setWildOptions(d.options || []))
      .catch(() => {})
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
      const res = await adminInjectWild(account, Number(injBase), injKind, Number(injOffset) || 30)
      setInjMsg('已投放:' + (res.id || '') + '(u=' + (res.u != null ? res.u.toFixed(3) : '') + ')')
      loadInjects()
    } catch (err) {
      setInjErr(err.message || '投放失败')
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
    if (!confirm('确认删除账号 ' + account + ' 的全部数据?此操作不可恢复。')) return
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
    if (!configured && password !== confirm) {
      setError('两次输入的密码不一致')
      return
    }
    try {
      const res = configured ? await adminLogin(password) : await adminSetup(password)
      setAdminToken(res.token)
      setPassword('')
      setConfirm('')
      setAuthed(true)
    } catch (err) {
      setError(err.message || '操作失败')
    }
  }

  const logout = async () => {
    await adminLogout()
    setAuthed(false)
    setRules(null)
    setStats(null)
    setWildOptions(null)
    setAccounts([])
    setInjAccount('')
    setInjects(null)
  }

  if (loading) return <div className="admin-page"><p className="admin-hint">加载中…</p></div>

  if (!authed) {
    return (
      <div className="admin-page">
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
                className="input" type="password" placeholder="确认密码" value={confirm}
                onChange={(e) => setConfirm(e.target.value)}
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

      <div className="admin-card admin-rules">
        <h3>成员抓捕图表</h3>
        <p className="admin-hint">所有成员累计抓捕精灵情况(来源:获得宠物事件,近30天时间轴)。</p>
        {statsErr && <p className="admin-error">{statsErr}</p>}
        <AdminCharts data={stats} />
      </div>

      <div className="admin-card admin-rules">
        <h3>投放稀有野生精灵</h3>
        <p className="admin-hint">
          向下拉选定的成员实时地图注入一只稀有野生精灵(异色 / 炫彩)。
          位置取该成员最近一次缓存位置,按当前场景投影到附近 30 米处;仅影响前端地图显示,
          不修改真实流量。成员需在「有底图的场景」中且有缓存位置才能投放成功;异色只列有异色形态的精灵。
        </p>
        <form onSubmit={injectWild} className="admin-inject-form">
          <select
            className="select" value={injAccount} onChange={(e) => setInjAccount(e.target.value)}
            title="选择投放目标玩家(需在线且有缓存位置)"
          >
            <option value="">选择目标玩家</option>
            {accounts.map((a) => (
              <option key={a.account} value={a.account}>
                {a.online ? '🟢 ' : '🟥 '}{a.name} (UID:{(a.account || '').replace(/^UID:/, '')})
              </option>
            ))}
          </select>
          <select
            className="select" value={injBase} onChange={(e) => setInjBase(e.target.value)}
          >
            <option value="">选择精灵形态</option>
            {wildOptions && wildOptions
              .filter((o) => injKind !== 'shiny' || o.shiny) // 异色只列有异色形态的精灵
              .map((o) => (
                <option key={o.base} value={o.base}>
                  {o.name}(#{o.book})
                </option>
              ))}
          </select>
          <select
            className="select" value={injKind}
            onChange={(e) => { setInjKind(e.target.value); setInjBase('') }}
          >
            <option value="shiny">异色</option>
            <option value="colorful">炫彩</option>
          </select>
          <input
            className="input" type="number" min="1" max="200" value={injOffset}
            onChange={(e) => setInjOffset(e.target.value)} title="距玩家位置米数"
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
                  <span className="muted admin-inject-scene">{it.sceneRes || '无底图'}</span>
                  <button className="btn ghost" onClick={() => revokeInject(it.account, it.id)}>撤销</button>
                </li>
              ))}
            </ul>
          </div>
        )}
      </div>

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
          <select className="select" value={rMode} onChange={(e) => setRMode(e.target.value)}>
            <option value="black">黑名单</option>
            <option value="white">白名单</option>
          </select>
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
