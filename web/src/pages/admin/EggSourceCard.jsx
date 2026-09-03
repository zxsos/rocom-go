import React, { useCallback, useState } from 'react'
import { adminEggSource, adminEggSourceSet } from '../../api'
import { confirmDialog } from '../../components/confirm'
import { useAdminFetch } from './useAdminFetch'

// EggSourceCard 随机蛋「猜猜孵出谁」的数据源切换。
//
// 两个源的性质差别很大,不是同一站的两条路:
//   - 本地源(默认):用解包的 PET_EGG_CONF 反推,**会用蛋的孵化时长硬筛**,
//     零外部依赖、无限流、离线可用。没有系别(系别只在协议里,配置表没有)。
//   - 咸鱼源:代理第三方图鉴,多给系别;但**不做时长筛选**,候选里会混入时长
//     根本对不上的物种,且需令牌、限流 10 次/分钟。
// 差异说明只服务于这个界面,故留在前端;源标识与是否需要令牌由后端下发
// (合法标识只有后端能校验,前端自己列一份迟早与校验逻辑漂移)。
const SOURCE_HINT = {
  local: {
    label: '本地解包数据(默认)',
    detail: '按 PET_EGG_CONF 反推,并用蛋的孵化时长硬筛 —— 这是唯一有实测支撑的一维。'
      + '零外部依赖、无限流、离线可用;没有系别。实测同一颗蛋:本地 4 条、咸鱼源 12 条,'
      + '其中 8 条的孵化时长与那颗蛋不符。',
  },
  xianyu: {
    label: '咸鱼源(第三方图鉴)',
    detail: '比本地多给系别,但只按身高体重匹配、不做时长筛选。需要配置第三方令牌,'
      + '限流 10 次/分钟。仅在本地数据落后于游戏版本时临时切过去对照。',
  },
}

export default function EggSourceCard({ onUnauthed }) {
  const { data, error, refresh } = useAdminFetch(useCallback(() => adminEggSource(), []), onUnauthed)
  const [srcErr, setSrcErr] = useState('')
  const [srcMsg, setSrcMsg] = useState('')
  const [switchBusy, setSwitchBusy] = useState(false)

  const current = data?.source
  const sources = data?.sources ?? []

  // 切换数据源:换的是**所有人**看到的结果,全局影响、没有撤销,故走二次确认。
  // 与远行商人不同,这里切源没有任何代价(无缓存需清),确认文案因此只说影响范围。
  const switchSource = async (id) => {
    setSrcErr(''); setSrcMsg('')
    if (!await confirmDialog({
      message: '确认切换到「' + (SOURCE_HINT[id]?.label ?? id) + '」?\n\n'
        + '切换后所有玩家的「猜猜孵出谁」都会改用这个源,且立即生效。',
      okText: '确认切换',
    })) return
    setSwitchBusy(true)
    try {
      await adminEggSourceSet(id)
      setSrcMsg('已切换,立即生效。')
      refresh()
    } catch (err) {
      setSrcErr(err.message || '切换失败')
    } finally {
      setSwitchBusy(false)
    }
  }

  return (
    <div className="admin-card admin-rules admin-wide">
      <h3>查蛋数据源</h3>
      <p className="admin-hint">
        随机蛋「猜猜孵出谁」的候选来源,切换后对所有人生效、立即生效。当前生效:
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
        切源不影响任何缓存:两个源都是每次请求实时算,故切换立即生效、也没有
        「切源后数据为空」这类代价。默认本地源不是「退而求其次」—— 第三方读的是
        同一张表,却没用上时长这一维,候选反而更宽。
      </p>

      {error && <p className="admin-error">{error.message}</p>}
      {srcErr && <p className="admin-error">{srcErr}</p>}
      {srcMsg && <p className="admin-hint" style={{ color: 'var(--green, #4caf50)' }}>{srcMsg}</p>}
    </div>
  )
}
