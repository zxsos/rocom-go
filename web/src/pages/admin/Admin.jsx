import React, { useCallback, useEffect, useState } from 'react'
import {
  getAdminStatus, adminSetup, adminLogin, adminLogout,
  getAdminToken, setAdminToken, adminWildPetOptions, adminListInjects,
  getAccounts,
} from '../../api'
import AdminLoginCard from './AdminLoginCard'
import CatchStatsCard from './CatchStatsCard'
import PlaySessionsCard from './PlaySessionsCard'
import EggStatsCard from './EggStatsCard'
import RulesCard from './RulesCard'
import { InjectWildCard, InjectFlowerCard } from './InjectCards'
import MerchantSubsCard from './MerchantSubsCard'
import MerchantSourceCard from './MerchantSourceCard'
import EggSourceCard from './EggSourceCard'
import PinCard from './PinCard'

// 管理员面板(隐式入口:导航不显示,需手动输入 #/admin 进入)。
// 首次进入引导设置密码,之后凭密码登录。
//
// 本文件只保留「会话」与「多个面板共用的数据」:
//   - 会话:未配置则引导设密码,已配置则登录;401 视为会话失效踢回登录页。
//   - 共用数据:账号列表、可投放形态、当前注入列表——它们被两个以上面板用到,
//     故统一在此拉取并下发,避免重复请求与两份不同步的副本。
// 各面板自己的数据与表单状态都在各自文件里(见 pages/admin/),新增功能请加卡片而非堆这里。
export default function Admin() {
  const [loading, setLoading] = useState(true)   // 状态拉取中
  const [configured, setConfigured] = useState(false)
  const [authed, setAuthed] = useState(getAdminToken() !== '')
  const [password, setPassword] = useState('')
  const [confirmPw, setConfirmPw] = useState('')
  const [error, setError] = useState('')

  useEffect(() => {
    getAdminStatus().then((s) => {
      setConfigured(!!s.configured)
      setAuthed(!!s.authed || getAdminToken() !== '')
    }).catch(() => {})
      .finally(() => setLoading(false))
  }, [])

  // 仅 401(令牌失效/未登录)才视为会话失效踢回登录页;其余错误(404/500/网络)只展示,不打断面板。
  const kickIfUnauthed = useCallback((err) => { if (err.status === 401) setAuthed(false) }, [])

  // 账号列表:投放目标、PIN 管理、游玩记录筛选三处共用。
  const [accounts, setAccounts] = useState([])
  const loadAccounts = useCallback(() => {
    getAccounts().then((list) => { if (list) setAccounts(list) }).catch(() => {})
  }, [])

  // 可投放的精灵形态(异色/炫彩候选),两个投放卡片共用。
  const [wildOptions, setWildOptions] = useState(null)
  const loadWildOptions = useCallback(() => {
    adminWildPetOptions().then((d) => setWildOptions(d.options || [])).catch(() => {})
  }, [])

  // 当前注入中的精灵列表:注入是后端内存态(不落盘),玩家换场景或靠近 10 米 10 秒后自动消失。
  // 由投放卡片渲染,投放/撤销后刷新。
  const [injects, setInjects] = useState(null)
  const loadInjects = useCallback(() => {
    adminListInjects().then((d) => setInjects(d.injects || [])).catch(() => {})
  }, [])

  // 登录后拉共用数据,验证令牌可用(失败视为会话失效)。
  useEffect(() => {
    if (!authed) return
    loadAccounts()
    loadWildOptions()
    loadInjects()
  }, [authed, loadAccounts, loadWildOptions, loadInjects])

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

  // 登出:只需清会话与令牌。各面板的数据在 authed 变 false 时随整棵子树卸载而自然重置,
  // 无需逐个清空(原来的显式清空是冗余的,且新增面板时容易漏)。
  const logout = async () => {
    await adminLogout()
    sessionStorage.removeItem('admin-unlocked')
    setAuthed(false)
  }

  if (loading) return <div className="admin-page"><p className="admin-hint">加载中…</p></div>

  if (!authed) {
    return (
      <AdminLoginCard
        configured={configured}
        password={password}
        confirmPw={confirmPw}
        error={error}
        onPasswordChange={setPassword}
        onConfirmChange={setConfirmPw}
        onSubmit={submit}
      />
    )
  }

  return (
    <div className="admin-page">
      <div className="admin-head">
        <h2>管理面板</h2>
        <button className="btn" onClick={logout}>退出登录</button>
      </div>

      <div className="admin-section-title">数据统计</div>
      <CatchStatsCard onUnauthed={kickIfUnauthed} />
      <PlaySessionsCard accounts={accounts} onUnauthed={kickIfUnauthed} />
      <EggStatsCard onUnauthed={kickIfUnauthed} />

      <div className="admin-section-title">投放管理</div>
      <InjectWildCard
        accounts={accounts} wildOptions={wildOptions} injects={injects}
        onInjectsChanged={loadInjects}
      />
      <InjectFlowerCard
        accounts={accounts} wildOptions={wildOptions}
        onInjectsChanged={loadInjects}
      />

      <div className="admin-section-title">账号与规则</div>
      <RulesCard onUnauthed={kickIfUnauthed} />
      <MerchantSubsCard onUnauthed={kickIfUnauthed} />
      <MerchantSourceCard onUnauthed={kickIfUnauthed} />
      <EggSourceCard onUnauthed={kickIfUnauthed} />
      <PinCard accounts={accounts} onAccountsChanged={loadAccounts} />
    </div>
  )
}
