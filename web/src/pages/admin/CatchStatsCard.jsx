import React, { useCallback } from 'react'
import { adminStats } from '../../api'
import AdminCharts from './AdminCharts'
import { useAdminFetch } from './useAdminFetch'

// CatchStatsCard 成员抓捕图表:所有成员累计抓捕精灵情况(来源:获得宠物事件,近30天时间轴)。
// 图表本体在 AdminCharts.jsx。
export default function CatchStatsCard({ onUnauthed }) {
  const { data: stats, error } = useAdminFetch(useCallback(() => adminStats(), []), onUnauthed)

  return (
    <div className="admin-card admin-rules admin-wide">
      <h3>成员抓捕图表</h3>
      <p className="admin-hint">所有成员累计抓捕精灵情况(来源:获得宠物事件,近30天时间轴)。</p>
      {error && <p className="admin-error">{error.message}</p>}
      <AdminCharts data={stats} />
    </div>
  )
}
