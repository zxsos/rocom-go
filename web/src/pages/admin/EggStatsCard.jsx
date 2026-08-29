import React, { useCallback } from 'react'
import { adminEggStats } from '../../api'
import { uidOf } from '../../data/nav'
import { fmtShortTime } from '../../utils/format'
import { useAdminFetch } from './useAdminFetch'
import { EggDailyChart } from './charts'

// EggStatsCard 查蛋 API 使用统计:孵蛋页「查蛋」代理第三方图鉴,每次查询消耗一次对方 token。
// daily 结构见 api.adminEggStats:[{day,total,ok}];keySet=false 表示服务端未配 -egg-api-key(统计恒为 0)。
export default function EggStatsCard({ onUnauthed }) {
  const { data: eggStats, error, refresh } = useAdminFetch(useCallback(() => adminEggStats(), []), onUnauthed)

  return (
    <div className="admin-card admin-rules admin-wide">
      <h3>查蛋 API 统计</h3>
      <p className="admin-hint">
        孵蛋页「查蛋」代理第三方图鉴,每次查询消耗一次对方 token,统计已发起第三方请求的调用。
        {eggStats && eggStats.keySet === false && '当前服务端未配置 -egg-api-key,统计恒为 0。'}
      </p>
      {error && <p className="admin-error">{error.message}</p>}
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
                        <td>{a.name || a.account} <span className="muted">{uidOf(a.account)}</span></td>
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
                        <td>{fmtShortTime(rec.time)}</td>
                        <td>{rec.name || rec.account} <span className="muted">{uidOf(rec.account)}</span></td>
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
              <button className="btn" onClick={refresh}>刷新</button>
            </div>
          </>
        )}
    </div>
  )
}
