import React, { useCallback, useContext, useEffect, useState } from 'react'
import { getMerchant, getAccounts, setAccountRank } from '../../api'
import { AccountContext } from '../../context'
import RankTitle from '../../components/RankTitle'
import { confirmDialog } from '../../components/confirm'
import { useAsyncData } from '../../hooks/useAsyncData'
import { STATUS, count, unwrap } from './format'
import { RoundSteps, MerchantTurn } from './components'
import SubCard from './SubCard'

// 远行商人页:展示后端缓存的 4h 轮次数据(令牌在服务端,缓存 2 天,见
// internal/server/api_merchant.go)。
// 刷新按钮只是重拉后端缓存(不烧 token);强制刷新(绕缓存回源第三方)已移到管理面板。
// 纯展示逻辑见 format.js,展示组件见 components.jsx,订阅卡片见 SubCard.jsx。
export default function Merchant() {
  const account = useContext(AccountContext) // 当前登录账号 key(账号下拉切换后自动跟随)
  const [coins, setCoins] = useState(null) // 当前账号洛克贝:null=未同步,数字=已同步(含 0)
  const [curAcc, setCurAcc] = useState('') // 洛克贝/参加按钮实际归属的账号
  const [join, setJoin] = useState(true) // 是否参加排行榜(默认参加,可在洛克贝旁一键退出)
  const [title, setTitle] = useState('') // 当前账号今日佩戴的称号
  const [busyRank, setBusyRank] = useState(false)
  const [rankErr, setRankErr] = useState('')

  // 远行商人数据:第三方令牌在服务端且回源较慢,故只在挂载时重取,失败不自动重试。
  const { data: raw, loading, error, refresh } = useAsyncData(useCallback(() => getMerchant(false), []))
  // 出错时不展示上一次的陈旧数据:营业状态/货单会误导(显示「营业中」其实早打烊了)。
  const d = error ? null : raw

  // 洛克贝按当前登录账号(AccountContext)取,每个人看到自己的洛克贝:
  // 先精确匹配当前账号,找不到(未登录/游客)再回退到在线/最近活跃账号。
  // hasCoins=false 表示该账号从未解析到洛克贝(没重登游戏),显示「待同步」而非隐藏徽标。
  useEffect(() => {
    let done = false
    getAccounts().then((list) => {
      if (done || !list) return
      const mine = list.find((a) => a.account === account)
      const cur = mine || list.find((a) => a.online) || list[0]
      setCurAcc(cur ? cur.account : '')
      setCoins(cur && cur.hasCoins ? cur.coins : null)
      setJoin(cur ? !!cur.join : true)
      setTitle(cur ? (cur.title || '') : '')
    }).catch(() => {})
    return () => { done = true }
  }, [account])

  // 洛克贝旁的参加/退出按钮(默认参加;退出后不再参与福布斯/盈亏排行与称号评选)
  const toggleRank = async () => {
    if (!curAcc) return
    if (join && !(await confirmDialog({
      message: '退出排行榜?退出后不再参与福布斯/盈亏排行,称号评选也会排除。',
      okText: '退出', danger: true,
    }))) return
    setBusyRank(true)
    setRankErr('')
    try {
      await setAccountRank(curAcc, !join)
      setJoin(!join)
    } catch (e) {
      setRankErr(e.message || '设置失败')
    } finally {
      setBusyRank(false)
    }
  }

  if (error) {
    return <div className="merchant-page"><div className="merchant-err">{error.message}</div></div>
  }
  if (!d) {
    return <div className="merchant-page"><div className="empty">{loading ? '加载中…' : ''}</div></div>
  }

  const open = d.status === 'open'
  // 上架轮(排除 off 打烊槽):营业中看今天,打烊看昨天回顾。
  const turns = ((open ? d.today : d.prev) || []).filter((r) => !r.off)
  // 当前轮 = 开始时刻 ≤ now 的最后一轮(都是后端 Unix 秒,直接比较数值,无时区问题)。
  const curIdx = turns.reduce((last, r, i) => (r.start <= d.now ? i : last), -1)
  const cur = curIdx >= 0 ? turns[curIdx] : null
  const active = turns.filter((r) => !r.empty && r.merchant) // 查过且有货的轮
  // 营业中最新轮在前(玩家最关心当前货),回顾按时间正序。
  const list = open ? [...active].reverse() : active
  const head = list[0]
  const m = head && unwrap(head.merchant)
  const total = active.reduce((n, r) => n + count(r.merchant), 0)
  const st = STATUS[d.status] || STATUS.idle
  const dayText = open ? d.day : `昨日 ${d.day}`

  return (
    <div className="merchant-page">
      {/* 顶部横幅:商人名 + 营业状态 + 轮次进度 */}
      <div className={`merchant-hero ${open ? 'is-open' : ''}`}>
        <div className="merchant-hero-top">
          <div className="merchant-hero-info">
            <div className="merchant-name">{m ? m.merchant_name : '远行商人'}</div>
            {m && m.subtitle && <div className="merchant-sub">{m.subtitle}</div>}
          </div>
          <div className="merchant-hero-side">
            {coins !== null ? (
              <span className="merchant-coins" title={`${account || '当前'} 洛克贝(每次登录游戏时同步)`}>🪙 {coins.toLocaleString()}</span>
            ) : (
              <span className="merchant-coins merchant-coins-unk" title={`${account || '当前'} 洛克贝尚未同步,请重新登录游戏后刷新`}>🪙 待同步</span>
            )}
            <RankTitle title={title} />
            {curAcc && (
              <button type="button" className={`merchant-rank-btn${join ? ' joined' : ''}`}
                onClick={toggleRank} disabled={busyRank}
                title={join ? '已参加排行榜,点击退出' : '未参加排行榜,点击参加(默认参加)'}>
                {join ? '🏆 已参加' : '🏆 参加'}
              </button>
            )}
            {rankErr && <span className="merchant-rank-err">{rankErr}</span>}
            <span className={`merchant-status merchant-status-${st.cls}`}>{st.text}</span>
            <span className="merchant-day">{dayText}</span>
            {m && m.round && m.round.countdown && (
              <span className="merchant-countdown" title="距本轮结束">⏳ {m.round.countdown}</span>
            )}
          </div>
        </div>
        {open ? (
          <RoundSteps turns={turns} curIdx={curIdx} />
        ) : (
          <div className="merchant-closed-bar">
            打烊休市 00:00 ~ 08:00,下一轮 08:00 开张 · 以下为 {d.day} 全天回顾
          </div>
        )}
      </div>

      {/* 订阅卡片:新货上架邮件提醒 */}
      <SubCard />

      {/* 操作条 */}
      <div className="merchant-bar">
        <span className="merchant-count">
          {open && cur
            ? `第 ${curIdx + 1}/4 轮 · 在售 ${total} 件`
            : `昨日全天回顾 · 共 ${total} 件`}
        </span>
        <span className="merchant-btns">
          <button type="button" className="btn" onClick={refresh} disabled={loading}>
            {loading ? '加载中…' : '刷新'}
          </button>
        </span>
      </div>

      {list.length ? (
        <div className="merchant-rounds">
          {list.map((r) => (
            <MerchantTurn key={r.start} r={r} cur={open && r === cur} />
          ))}
        </div>
      ) : (
        <div className="empty">该时段暂无数据,后端会按 4h 自动补查,稍后再试</div>
      )}
    </div>
  )
}
