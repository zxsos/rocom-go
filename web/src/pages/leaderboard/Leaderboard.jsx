import React, { useEffect, useState, useContext, useRef } from 'react'
import { AccountContext } from '../../context'
import { getLeaderboard, setAccountRank } from '../../api'

// 称号展示配置:大富翁👑 / 赚钱王💹 / 败家子💸(每晚 00:05 结算,当天佩戴一天)
const TITLES = [
  { key: '大富翁', icon: '👑', cls: 'rich', desc: '昨日结束时金币最多' },
  { key: '赚钱王', icon: '💹', cls: 'earner', desc: '昨日净赚最多' },
  { key: '败家子', icon: '💸', cls: 'spender', desc: '昨日净亏最多' },
]

const fmt = (n) => (n == null ? '—' : n.toLocaleString('zh-CN'))
const isMe = (a, account) => a.account === account

// 排行榜行:前三名奖牌,当前账号高亮;称号徽标(若有)
function RankRow({ entry, rank, account, mode }) {
  const medal = rank <= 3 ? <span className="rank-medal">{[null, '🥇', '🥈', '🥉'][rank]}</span> : <span className="rank-no">{rank}</span>
  const mine = isMe(entry, account)
  return (
    <div className={`rank-row${mine ? ' is-me' : ''}${entry.hasCoins ? '' : ' is-unknown'}`}>
      <span className="rank-pos">{medal}</span>
      <span className="rank-name">
        <span className="rank-uname">{entry.name || entry.account}</span>
        {entry.title ? <span className={`rank-title t-${TITLES.find((t) => t.key === entry.title)?.cls || 'x'}`}>{entry.title}</span> : null}
        {mine ? <span className="rank-me">我</span> : null}
      </span>
      {mode === 'forbes' ? (
        <span className="rank-num">
          {entry.hasCoins ? <span className="rank-coins">🪙 {fmt(entry.coins)}</span> : <span className="rank-unknown">待同步</span>}
        </span>
      ) : (
        <span className="rank-num">
          {entry.hasCoins ? (
            <>
              <span className={`rank-profit ${entry.profit >= 0 ? 'pos' : 'neg'}`}>
                {entry.profit > 0 ? '+' : ''}{fmt(entry.profit)}
              </span>
              <span className="rank-sub">{fmt(entry.coins)} / 基线 {fmt(entry.baseline)}</span>
            </>
          ) : (
            <span className="rank-unknown">待同步</span>
          )}
        </span>
      )}
    </div>
  )
}

export default function Leaderboard() {
  const account = useContext(AccountContext)
  const [data, setData] = useState(null) // {forbes, profit, titles, me}
  const [err, setErr] = useState('')
  const [tab, setTab] = useState('forbes')
  const [busy, setBusy] = useState(false)
  const [busyJoin, setBusyJoin] = useState(false)
  const timer = useRef(0)

  const load = () => {
    getLeaderboard().then((d) => {
      setData(d)
      setErr('')
    }).catch((e) => setErr(String(e && e.message || e)))
  }
  useEffect(() => {
    load()
    timer.current = setInterval(load, 30000) // 每 30s 自动刷新(账号在线/金币变化/跨日结算)
    return () => clearInterval(timer.current)
  }, [])

  const join = async (want) => {
    if (!account) return
    setBusyJoin(true)
    try {
      await setAccountRank(account, want)
      load()
    } catch (e) {
      setErr(String(e && e.message || e))
    } finally {
      setBusyJoin(false)
    }
  }

  if (err && !data) {
    return <div className="panel rank-page"><div className="rank-err">加载失败:{err}</div></div>
  }
  const forbes = data?.forbes || []
  const profit = data?.profit || []
  const titles = data?.titles || []
  const me = data?.me
  const mineIn = forbes.some((a) => isMe(a, account))
  const list = tab === 'forbes' ? forbes : profit
  return (
    <div className="panel rank-page">
      <div className="panel-head rank-head">
        <h2>🏆 排行榜</h2>
        <div className="rank-head-right">
          <span className="rank-settle-note">每晚 00:05 结算 · 称号佩戴一天</span>
          <button className="btn small ghost" disabled={busy} onClick={() => { setBusy(true); load(); setTimeout(() => setBusy(false), 400) }}>刷新</button>
        </div>
      </div>

      {/* 今日称号(结算后展示,未结算显示待评选) */}
      <div className="rank-titles">
        {TITLES.map((t) => {
          const w = titles.find((x) => x.title === t.key)
          return (
            <div key={t.key} className={`rank-title-card ${t.cls}${w ? '' : ' pending'}`}>
              <span className="rank-title-icon">{t.icon}</span>
              <div className="rank-title-body">
                <div className="rank-title-key">{t.key}</div>
                <div className="rank-title-who">
                  {w ? <span className="rank-title-name">{w.name || w.account}</span> : <span className="rank-title-none">今日待评选</span>}
                </div>
                <div className="rank-title-desc">{t.desc}</div>
              </div>
            </div>
          )
        })}
      </div>

      {account && me && !me.join && (
        <div className="rank-join-tip">
          <span>你当前未参加排行榜({me.name || me.account}),退出后不再参与福布斯/盈亏排行与称号评选。</span>
          <button className="btn small" disabled={busyJoin} onClick={() => join(true)}>{busyJoin ? '处理中…' : '一键参加'}</button>
        </div>
      )}
      {account && !mineIn && me && me.join && (
        <div className="rank-join-tip">你在排行榜中,但金币尚未同步(登录游戏后自动记录)。</div>
      )}

      <div className="rank-tabs">
        <button className={tab === 'forbes' ? 'active' : ''} onClick={() => setTab('forbes')}>福布斯(总金币)</button>
        <button className={tab === 'profit' ? 'active' : ''} onClick={() => setTab('profit')}>盈利/亏损</button>
      </div>

      <div className="rank-list">
        {list.length === 0 && <div className="rank-empty">暂无参加者,在金币旁点击「参加」即可上榜。</div>}
        {list.map((e, i) => <RankRow key={e.account} entry={e} rank={i + 1} account={account} mode={tab} />)}
      </div>
      <div className="rank-foot">
        盈亏 = 当前金币 − 首次记录金币(每次登录自动记录)· 仅统计「已参加」账号 · 未同步金币的账号显示「待同步」
      </div>
    </div>
  )
}
