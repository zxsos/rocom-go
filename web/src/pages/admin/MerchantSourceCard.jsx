import React, { useCallback, useState } from 'react'
import { adminMerchantSource, adminMerchantSourceSet } from '../../api'
import { confirmDialog } from '../../components/confirm'
import { useAdminFetch } from './useAdminFetch'

// MerchantSourceCard 远行商人数据源切换。
//
// 两个源是互相独立的第三方,不是同一站的两条路:
//   - 咸鱼源:第三方 JSON 接口,字段最全(商人名/副标题/商品图/类别/本轮倒计时),需令牌。
//   - 好游快爆源:抓公开页面,无需令牌,只有商品名/价格/限购。
// 差异说明只服务于这个界面,故留在前端;源标识与是否需要令牌由后端下发
// (合法标识只有后端能校验,前端自己列一份迟早与校验逻辑漂移)。
const SOURCE_HINT = {
  xianyu: {
    label: '第三方 JSON 接口(默认)',
    detail: '字段最全:商人名、副标题、商品图、商品类别、本轮倒计时。需要配置第三方令牌。',
  },
  haoyou: {
    label: '好游快爆页面抓取',
    detail: '无需令牌,公开页面直接抓。商品图与类别都有(图是外链),只是没有商人名等少数几项。',
  },
}

export default function MerchantSourceCard({ onUnauthed }) {
  const { data, error, refresh } = useAdminFetch(useCallback(() => adminMerchantSource(), []), onUnauthed)
  const [srcErr, setSrcErr] = useState('')
  const [srcMsg, setSrcMsg] = useState('')
  const [switchBusy, setSwitchBusy] = useState(false)

  const current = data?.source
  const sources = data?.sources ?? []

  // 切换数据源:换的是**所有人**看到的数据源,且会清空当日已缓存货单 —— 全局影响,
  // 没有撤销,故走二次确认;确认文案里把代价(昨日回顾当天可能为空)写明白,
  // 而不是让人切完了才发现少了东西。
  const switchSource = async (id) => {
    setSrcErr(''); setSrcMsg('')
    if (!await confirmDialog({
      message: '确认切换到「' + (SOURCE_HINT[id]?.label ?? id) + '」?\n\n'
        + '切换会清空当日已缓存的货单并立即按新源重新获取。切源当天「昨日回顾」可能为空,'
        + '直到下一个营业日的档被缓存。',
      okText: '确认切换',
    })) return
    setSwitchBusy(true)
    try {
      await adminMerchantSourceSet(id)
      setSrcMsg('已切换,正在按新源重新获取货单。')
      refresh()
    } catch (err) {
      setSrcErr(err.message || '切换失败')
    } finally {
      setSwitchBusy(false)
    }
  }

  return (
    <div className="admin-card admin-rules admin-wide">
      <h3>远行商人数据源</h3>
      <p className="admin-hint">
        远行商人货单的获取来源,切换后对所有人生效。当前生效:
        <b>{data ? (SOURCE_HINT[current]?.label ?? current) : '加载中…'}</b>
        {/* 当前源需要令牌却没配时要明说:此时页面取不到任何数据,而管理员最需要的
            提示恰恰是「还有个不用令牌的源可以切」。 */}
        {data && data.keySet === false && sources.find((s) => s.id === current)?.needKey && (
          <span style={{ color: 'var(--danger, #e5534b)' }}>
            {' '}⚠ 未配置第三方令牌,该源当前取不到数据。
          </span>
        )}
      </p>

      {sources.map((s) => (
        <div className="admin-source-row" key={s.id}>
          <div className="admin-source-head">
            <b>{s.name}</b>
            <span className="muted">{s.needKey ? '需要第三方令牌' : '无需令牌'}</span>
            {s.id === current
              ? <span className="admin-source-tag">生效中</span>
              : (
                <button
                  className={'btn' + (switchBusy ? ' is-loading' : '')} type="button"
                  disabled={switchBusy} onClick={() => switchSource(s.id)}
                >
                  切换到此源
                </button>
              )}
          </div>
          <span className="admin-hint">{SOURCE_HINT[s.id]?.detail ?? ''}</span>
        </div>
      ))}

      <p className="admin-hint">
        切换会清空当日已缓存货单并立即按新源重新获取。切源当天「昨日回顾」可能为空 ——
        直到下一个营业日的档被缓存。两个源的滞后实测在同一量级(整点后约 1 分钟内),
        换源的主要收益是「不需要第三方令牌」,而不是更快。
      </p>

      {error && <p className="admin-error">{error.message}</p>}
      {srcErr && <p className="admin-error">{srcErr}</p>}
      {srcMsg && <p className="admin-hint" style={{ color: 'var(--green, #4caf50)' }}>{srcMsg}</p>}
    </div>
  )
}
