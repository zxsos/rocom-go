import React, { useCallback, useState } from 'react'
import { adminPlaySessions } from '../../api'
import Dropdown from '../../components/Dropdown'
import { uidOf } from '../../data/nav'
import { fmtShortTime } from '../../utils/format'
import { useAdminFetch } from './useAdminFetch'
import { fmtDur, PlayDailyChart } from './charts'

// PlaySessionsCard 游玩记录:玩家上/下线时间与时长。
// 自动记录玩家每次上线的起止时间与游玩时长(来源:连接登录/断开与心跳活跃,近14天每日聚合)。
// 挂后台或断线超过 90 秒无流量判定一次下线,再次活跃自动续记新会话。
// 数据(daily 结构见 api.adminPlaySessions:[{day,sessions,duration}])与账号筛选都是本卡片自持。
export default function PlaySessionsCard({ accounts, onUnauthed }) {
  const [playAccount, setPlayAccount] = useState('') // 明细账号筛选(空=全部)
  // 筛选变化即换 fetcher → useAsyncData 自动重取,故 Dropdown 的 onChange 不用手动触发刷新。
  const fetcher = useCallback(() => adminPlaySessions(playAccount), [playAccount])
  const { data: plays, error, refresh } = useAdminFetch(fetcher, onUnauthed)

  return (
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
            ...accounts.map((a) => ({ value: a.account, label: `${a.name || a.account} (UID:${uidOf(a.account)})` })),
          ]}
          onChange={setPlayAccount}
          title="按账号筛选游玩记录(空=全部)"
        />
        <button className="btn" onClick={refresh}>刷新</button>
      </div>
      {error && <p className="admin-error">{error.message}</p>}
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
                    <td>{s.name || s.account} <span className="muted">{uidOf(s.account)}</span></td>
                    <td>{fmtShortTime(s.loginTime)}</td>
                    <td>{s.online ? <span className="play-online">在线中</span> : fmtShortTime(s.logoutTime)}</td>
                    <td>{s.online ? '—' : fmtDur(s.duration)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
    </div>
  )
}
