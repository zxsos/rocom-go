import React from 'react'

// 管理面板的两个小图表,纯 SVG/CSS 实现(不引入图表库),各自只依赖传入的 daily 数组。

// fmtDur 把秒格式化为「X小时Y分 / Y分Z秒 / Z秒」。
export const fmtDur = (s) => {
  if (s == null || s < 0) return '-'
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  const sec = s % 60
  if (h > 0) return `${h}小时${m}分`
  if (m > 0) return `${m}分${sec}秒`
  return `${sec}秒`
}

// PlayDailyChart 近14天每日游玩时长 SVG 柱状图:坐标轴 + 网格线 + 渐变圆角柱 + hover 高亮。
// daily 结构见 api.adminPlaySessions:[{day,sessions,duration}]。
export const PlayDailyChart = ({ daily }) => {
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
export const EggDailyChart = ({ daily }) => {
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
