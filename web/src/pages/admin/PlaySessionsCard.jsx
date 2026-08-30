import React, { useCallback, useEffect, useState } from 'react'
import { adminPlaySessions } from '../../api'
import Dropdown from '../../components/Dropdown'
import { uidOf } from '../../data/nav'
import { fmtShortTime } from '../../utils/format'
import { useStoredState } from '../../hooks/useStoredState'
import { useAdminFetch } from './useAdminFetch'
import { fmtDur, PlayDailyChart } from './charts'

// PlaySessionsCard 游玩记录:玩家上/下线时间与时长。
// 自动记录玩家每次上线的起止时间与游玩时长(来源:连接登录/断开与心跳活跃,近14天每日聚合)。
// 挂后台或断线超过 90 秒无流量判定一次下线,再次活跃自动续记新会话。
// 数据(daily 结构见 api.adminPlaySessions:[{day,sessions,duration}])与账号筛选都是本卡片自持。
//
// 明细按页码分页(offset 游标):记录随玩家上线次数累积,一次全拉会拖慢管理面板,
// 故每页只取 pageSize 条,total 由后端一并返回(与筛选同口径)用于算总页数。
// 汇总(在线数/今日/每日图)始终是全量,不随翻页变化。
const PAGE_SIZES = [20, 50, 100]

export default function PlaySessionsCard({ accounts, onUnauthed }) {
  const [playAccount, setPlayAccount] = useState('') // 明细账号筛选(空=全部)
  const [page, setPage] = useState(1)
  // 每页条数记进 localStorage:管理员大多固定在某个量级看,翻页不必反复调。
  const [pageSize, setPageSize] = useStoredState(localStorage, 'admin.playPageSize',
    (s) => (PAGE_SIZES.includes(parseInt(s, 10)) ? parseInt(s, 10) : 50), (v) => String(v))

  // 筛选变化即换 fetcher → useAsyncData 自动重取,故 Dropdown 的 onChange 只改状态即可;
  // 但**必须同时把页码重置回 1** —— 否则从「全部」的第 8 页切到只有几条记录的账号,
  // 会停在越界的空白页上,看上去像「筛完就没数据了」。
  const changeAccount = useCallback((v) => { setPlayAccount(v); setPage(1) }, [])
  const changePageSize = useCallback((v) => { setPageSize(Number(v)); setPage(1) }, [setPageSize])

  const fetcher = useCallback(
    () => adminPlaySessions(playAccount, pageSize, (page - 1) * pageSize),
    [playAccount, pageSize, page])
  const { data: plays, error, loading, refresh } = useAdminFetch(fetcher, onUnauthed)

  const total = (plays && plays.total) || 0
  const totalPages = Math.max(1, Math.ceil(total / pageSize))
  // 记录被清理导致当前页越界时退回最后一页:否则停在空白页且「下一页」是灰的,翻不回来。
  useEffect(() => {
    if (total > 0 && page > totalPages) setPage(totalPages)
  }, [total, totalPages, page])

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
          onChange={changeAccount}
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
                {/* key 必须用后端主键 id:同一账号同一秒内可有多条会话,
                    用 account+loginTime 会撞车,翻页时旧行不被回收而混入新页。 */}
                {plays.sessions.map((s) => (
                  <tr key={s.id}>
                    <td>{s.name || s.account} <span className="muted">{uidOf(s.account)}</span></td>
                    <td>{fmtShortTime(s.loginTime)}</td>
                    <td>{s.online ? <span className="play-online">在线中</span> : fmtShortTime(s.logoutTime)}</td>
                    <td>{s.online ? '—' : fmtDur(s.duration)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
      {total > 0 && (
        <div className="admin-pager">
          <label className="muted pager-size">
            每页
            <Dropdown
              small
              value={pageSize}
              options={PAGE_SIZES.map((n) => ({ value: n, label: String(n) }))}
              onChange={changePageSize}
              title="每页显示条数"
            />
            条
          </label>
          <button className="btn" disabled={page <= 1 || loading} onClick={() => setPage((p) => p - 1)}>‹ 上一页</button>
          <span className="muted">第 {page} / {totalPages} 页(共 {total} 条)</span>
          <button className="btn" disabled={page >= totalPages || loading} onClick={() => setPage((p) => p + 1)}>下一页 ›</button>
        </div>
      )}
    </div>
  )
}
