import React from 'react'

// 成员抓捕图表:纯 CSS 条形图/柱状图,不引入图表库。
// data 结构见 api.adminStats:{members:[{account,name,total,shiny,colorful,daily}], days, daily}。

// AdminCharts 全部成员抓捕情况:成员总数条形图 + 每日合计柱状图 + 成员明细。
export default function AdminCharts({ data }) {
  if (!data) return null
  const { members = [], days = [], daily = [] } = data

  // 成员条形图:按抓捕总数降序,条长按最大值归一化。
  const sorted = [...members].sort((a, b) => b.total - a.total)
  const maxMember = Math.max(...sorted.map((m) => m.total), 1)
  const totalAll = members.reduce((s, m) => s + m.total, 0)

  // 每日柱状图:展示近 7 天(移动端友好),标签只标首尾。
  const span = 7
  const tail = days.slice(-span)
  const tailDaily = daily.slice(-span)
  const maxDay = Math.max(...tailDaily, 1)

  return (
    <div className="admin-stats">
      {members.length === 0 ? (
        <p className="admin-hint">暂无抓捕数据(需要先有流量入库)。</p>
      ) : (
        <>
          <div className="admin-stats-sum">
            <div className="admin-stat-item">
              <b>{members.length}</b><span>成员数</span>
            </div>
            <div className="admin-stat-item">
              <b>{totalAll}</b><span>累计抓捕</span>
            </div>
            <div className="admin-stat-item">
              <b>{members.reduce((s, m) => s + m.shiny, 0)}</b><span>异色</span>
            </div>
            <div className="admin-stat-item">
              <b>{members.reduce((s, m) => s + m.colorful, 0)}</b><span>炫彩</span>
            </div>
          </div>

          <h4>各成员抓捕总数</h4>
          <div className="admin-bars">
            {sorted.map((m) => (
              <div className="admin-bar-row" key={m.account}>
                <span className="admin-bar-name" title={m.account}>{m.name || m.account}</span>
                <div className="admin-bar-track">
                  <div
                    className="admin-bar-fill"
                    style={{ width: (m.total / maxMember) * 100 + '%' }}
                    title={m.total + ' 只'}
                  />
                </div>
                <span className="admin-bar-n">{m.total}</span>
              </div>
            ))}
          </div>

          <h4>近 {span} 天抓捕趋势(全成员)</h4>
          <div className="admin-daily">
            {tailDaily.map((n, i) => (
              <div className="admin-daily-col" key={i}>
                <div className="admin-daily-track">
                  <div
                    className="admin-daily-fill"
                    style={{ height: (n / maxDay) * 100 + '%' }}
                    title={tail[i] + ': ' + n + ' 只'}
                  />
                </div>
                <span className="admin-daily-day">{tail[i]}</span>
              </div>
            ))}
          </div>

          <h4>成员明细</h4>
          <table className="admin-table">
            <thead>
              <tr><th>成员</th><th>账号</th><th>抓捕</th><th>异色</th><th>炫彩</th></tr>
            </thead>
            <tbody>
              {sorted.map((m) => (
                <tr key={m.account}>
                  <td>{m.name || '—'}</td>
                  <td className="mono">{m.account}</td>
                  <td>{m.total}</td>
                  <td>{m.shiny > 0 ? <span className="stat-hot">{m.shiny}</span> : 0}</td>
                  <td>{m.colorful > 0 ? <span className="stat-hot">{m.colorful}</span> : 0}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </>
      )}
    </div>
  )
}
