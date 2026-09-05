import React, { useCallback, useEffect, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import {
  getAdminStatus, adminSetup, adminLogin, adminLogout,
  getAdminToken, setAdminToken, adminWildPetOptions, adminListInjects,
  getAccounts, adminConfig, adminConfigSave, adminWebAddrConfirm,
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
import { MailConfigCard, AdvConfigCard } from './ConfigCards'
import WebAddrCard from './WebAddrCard'

// 三个分页按**动作代价**递增排,不是按功能类别分 —— 管理员最容易犯的错是
// 「以为自己只是看看,结果改了全局」,顺序本身就是提示:
//   信息查询  只读,翻遍了也不会动到任何东西
//   普通设置  改展示层:数据源、黑白名单、邮件提醒,改错了随时能改回来
//   高级设置  改服务端自身:代理监听、第三方令牌、向玩家投放假数据
const TABS = [
  { id: 'query', label: '信息查询' },
  { id: 'basic', label: '普通设置' },
  { id: 'advanced', label: '高级设置' },
]

// 管理员面板(隐式入口:导航不显示,需手动输入 #/admin 进入)。
// 首次进入引导设置密码,之后凭密码登录。
//
// 登录后是三个分页(见 TABS):一屏只放一类动作,免得在长页里滚着滚着
// 把「投个假精灵」和「改个数据源」挨在一起点了。
//
// 本文件只保留「会话」与「多个面板共用的数据」:
//   - 会话:未配置则引导设密码,已配置则登录;401 视为会话失效踢回登录页。
//   - 共用数据:账号列表、可投放形态、当前注入列表、运行配置——它们被两个以上面板用到,
//     故统一在此拉取并下发,避免重复请求与两份不同步的副本。
// 各面板自己的数据与表单状态都在各自文件里(见 pages/admin/),新增功能请加卡片而非堆这里。
export default function Admin() {
  // 分页落在 URL 上(#/admin?tab=advanced):刷新与分享链接都停在同一页。
  // 切页用 replace —— 后退应当是「离开面板」,而不是在三个分页之间倒着走一遍。
  // (钩子必须在早退分支之前调用,故这一行留在顶部。)
  const [params, setParams] = useSearchParams()
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

  // 运行配置:邮件(普通设置)与令牌/代理(高级设置)分处两个分页,但改的是同一个
  // POST 接口 —— 两份独立副本会互相覆盖,故统一在此拉取并下发。
  const [config, setConfig] = useState(null)
  const [configError, setConfigError] = useState(null)
  const loadConfig = useCallback(() => {
    adminConfig()
      .then((d) => { setConfig(d); setConfigError(null) })
      .catch((err) => { setConfigError(err.message || '拉取配置失败'); kickIfUnauthed(err) })
  }, [kickIfUnauthed])

  // saveConfig patch 只带本分页要改的字段(敏感项留空即不修改),其余交给后端沿用。
  //
  // **必须回带 smtpUser**:后端对 POST 的每一项是「给了就写」,而 smtpUser 是普通文本、
  // 没有「留空 = 不修改」的语义(它是唯一能被用户主动清空的那一项)。若代理那块只提交
  // socks5,后端会把 smtpUser 写空 —— 邮件配置被无声抹掉,而保存回报还是「已生效」。
  // 故默认带上当前值,除非本次就是要改它。
  const saveConfig = useCallback(async (patch) => {
    try {
      const res = await adminConfigSave({ smtpUser: config?.smtpUser ?? '', ...patch })
      loadConfig()   // 重新拉回脱敏后的当前值(卡片草稿随之作废)
      return res
    } catch (err) {
      kickIfUnauthed(err)
      throw err
    }
  }, [config, loadConfig, kickIfUnauthed])

  // 登录后拉共用数据,验证令牌可用(失败视为会话失效)。
  useEffect(() => {
    if (!authed) return
    loadAccounts()
    loadWildOptions()
    loadInjects()
    loadConfig()
  }, [authed, loadAccounts, loadWildOptions, loadInjects, loadConfig])

  // —— 换端口后的交接 ——
  //
  // 管理员从旧地址点「打开新地址」过来时,URL 上带一个一次性交接码(handoff)。
  // 它同时是两件事:
  //   1. 证据 —— 这个页面是从**新地址**加载的,故新地址确实可达,可以落盘了
  //   2. 凭据 —— 新源上没有管理员会话(令牌按协议+主机+端口隔离,换端口即换源),
  //      凭它领回一个令牌,否则管理员刚确认完就被踢回登录页
  //
  // 必须放在 Admin 而非 WebAddrCard:未登录时渲染的是登录卡片,WebAddrCard 根本
  // 不会挂载 —— 而交接恰恰要发生在拿到令牌之前。
  const [webNotice, setWebNotice] = useState('')
  const handoff = params.get('handoff')
  // 同一个码只提交一次。开发模式下 StrictMode 会把 effect 跑两遍,而交接码是
  // 一次性的:第二遍必然 409,于是「交接失败」盖掉刚成功的提示 —— 那句假警报
  // 比没提示更糟,它会让人以为要回旧地址重来(其实配置已经写好了)。
  const handoffTried = useRef('')
  useEffect(() => {
    if (!handoff || handoffTried.current === handoff) return
    handoffTried.current = handoff
    // 先把码从 URL 摘掉:它是一次性的,不该留在地址栏里被截图或刷新重用
    const next = new URLSearchParams(params)
    next.delete('handoff')
    setParams(next, { replace: true })
    adminWebAddrConfirm(handoff).then((res) => {
      if (res?.token) setAdminToken(res.token)
      setAuthed(true)
      setWebNotice(`已切换到 ${res.realAddr} 并写入配置文件,旧地址已停止服务。`)
      loadConfig()
    }).catch((e) => {
      // 码失效(试运行已过期/已用过)不算错误:管理员回到旧地址重来一遍即可
      setWebNotice(e?.message ? `交接失败: ${e.message}` : '交接失败,请回原地址重试。')
    })
  }, [handoff]) // eslint-disable-line react-hooks/exhaustive-deps

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

  const tab = TABS.find((t) => t.id === params.get('tab'))?.id ?? TABS[0].id

  return (
    <div className="admin-page">
      <div className="admin-head">
        <h2>管理面板</h2>
        <button className="btn" onClick={logout}>退出登录</button>
      </div>

      <div className="admin-tabs" role="tablist">
        {TABS.map((t) => (
          <button
            key={t.id} type="button" role="tab" aria-selected={tab === t.id}
            className={'admin-tab' + (tab === t.id ? ' on' : '')}
            onClick={() => setParams({ tab: t.id }, { replace: true })}
          >
            {t.label}
          </button>
        ))}
      </div>

      {/* 只挂载当前分页:卡片自持的数据与草稿随之卸载,切回来重新拉。
          面板上的东西大多是「点开才需要看的」,留一堆可能过期的副本不如每次取新的。 */}
      {tab === 'query' && (
        <>
          <div className="admin-section-title">数据统计</div>
          <CatchStatsCard onUnauthed={kickIfUnauthed} />
          <PlaySessionsCard accounts={accounts} onUnauthed={kickIfUnauthed} />
          <EggStatsCard onUnauthed={kickIfUnauthed} />
        </>
      )}

      {tab === 'basic' && (
        <>
          <div className="admin-section-title">邮件提醒</div>
          <MailConfigCard config={config} error={configError} onSave={saveConfig} />
          <MerchantSubsCard onUnauthed={kickIfUnauthed} />

          <div className="admin-section-title">数据源</div>
          <MerchantSourceCard onUnauthed={kickIfUnauthed} />
          <EggSourceCard onUnauthed={kickIfUnauthed} />

          <div className="admin-section-title">账号与规则</div>
          <RulesCard onUnauthed={kickIfUnauthed} />
          <PinCard accounts={accounts} onAccountsChanged={loadAccounts} />
        </>
      )}

      {tab === 'advanced' && (
        <>
          <div className="admin-section-title">投放管理</div>
          <InjectWildCard
            accounts={accounts} wildOptions={wildOptions} injects={injects}
            onInjectsChanged={loadInjects}
          />
          <InjectFlowerCard
            accounts={accounts} wildOptions={wildOptions}
            onInjectsChanged={loadInjects}
          />

          <div className="admin-section-title">Web 服务</div>
          <WebAddrCard
            config={config} error={configError}
            onChanged={loadConfig} notice={webNotice}
          />

          <div className="admin-section-title">运行配置</div>
          <AdvConfigCard config={config} error={configError} onSave={saveConfig} />
        </>
      )}
    </div>
  )
}
