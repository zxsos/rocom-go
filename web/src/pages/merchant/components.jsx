import React, { useState } from 'react'
import { count, groupBySlot, imgSrc, kindText, msTime, unwrap } from './format'

// 远行商人的纯展示组件:轮次步骤条、单个轮次、商品卡片。都不持有业务状态,
// 只依赖传入的数据(商品图加载失败是唯一的局部状态)。

// RoundSteps 四个上架轮次步骤条:8:00 / 12:00 / 16:00 / 20:00。
// 营业中:已过轮打勾、当前轮高亮、未来轮置灰;打烊回顾:四轮全部完成。
export function RoundSteps({ turns, curIdx }) {
  return (
    <div className="merchant-steps">
      {turns.map((r, i) => {
        const state = i < curIdx ? 'done' : i === curIdx ? 'cur' : 'todo'
        return (
          <div key={r.start} className={`merchant-step ${state}`}>
            <div className="merchant-step-node">
              <span className="merchant-step-dot">{state === 'done' ? '✓' : ''}</span>
            </div>
            <div className="merchant-step-time">{r.label.split('~')[0]}</div>
            <div className="merchant-step-cap">{i < curIdx ? '已上架' : i === curIdx ? '售卖中' : '待上架'}</div>
          </div>
        )
      })}
    </div>
  )
}

// MerchantTurn 单个上架轮:轮次头 + 按售卖时段分栏的商品网格(全天商品单独一栏)。
export function MerchantTurn({ r, cur }) {
  const m = unwrap(r.merchant)
  const items = (m && m.items) || []
  const time = r.label.split('~')[0]
  const { allDay, groups, other } = groupBySlot(items)
  const has = allDay.length > 0 || groups.length > 0 || other.length > 0
  return (
    <div className={`merchant-turn ${cur ? 'is-cur' : ''}`}>
      <div className="merchant-turn-head">
        <span className="merchant-turn-time">{time} 上架</span>
        {cur && <span className="merchant-turn-cur">当前售卖中</span>}
        <span className="merchant-turn-range muted">{r.label}</span>
        <span className="merchant-turn-count">{count(m)} 件</span>
      </div>
      {has ? (
        <div className="merchant-slots">
          {allDay.length > 0 && (
            <div className="merchant-slot">
              <div className="merchant-slot-head">
                <span className="merchant-slot-time merchant-slot-all">全天 08:00-24:00</span>
                <span className="merchant-slot-count">{allDay.length} 件</span>
              </div>
              <div className="merchant-grid">
                {allDay.map((it, i) => <MerchantItem key={it.name + i} it={it} />)}
              </div>
            </div>
          )}
          {groups.map((g) => (
            <div className="merchant-slot" key={g.slot}>
              <div className="merchant-slot-head">
                <span className="merchant-slot-time">{g.slot}</span>
                <span className="merchant-slot-count">{g.items.length} 件</span>
              </div>
              <div className="merchant-grid">
                {g.items.map((it, i) => <MerchantItem key={it.name + i} it={it} />)}
              </div>
            </div>
          ))}
          {other.length > 0 && (
            <div className="merchant-slot">
              <div className="merchant-slot-head">
                <span className="merchant-slot-time">其他时段</span>
                <span className="merchant-slot-count">{other.length} 件</span>
              </div>
              <div className="merchant-grid">
                {other.map((it, i) => <MerchantItem key={it.name + i} it={it} />)}
              </div>
            </div>
          )}
        </div>
      ) : (
        <div className="empty">该轮暂无商品</div>
      )}
    </div>
  )
}

// MerchantItem 单个商品卡片:大图 + 名称/类别 + 价格/限购/售卖时间。
export function MerchantItem({ it }) {
  const [imgBad, setImgBad] = useState(false)
  const src = imgSrc(it)
  const range = it.start_time && it.end_time ? `${msTime(it.start_time)} ~ ${msTime(it.end_time)}` : ''
  const tLabel = it.time_label || range
  return (
    <div className="merchant-item">
      <div className="merchant-item-img-wrap">
        {src && !imgBad ? (
          <img className="merchant-item-img" src={src} alt="" loading="lazy" draggable={false}
            onError={() => setImgBad(true)} />
        ) : (
          <span className="merchant-item-fb">🧳</span>
        )}
      </div>
      <div className="merchant-item-body">
        <div className="merchant-item-name" title={it.name}>{it.name}</div>
        {it.kind && <span className="merchant-item-kind">{kindText(it.kind)}</span>}
        <div className="merchant-item-meta">
          <span className="merchant-price">{it.price} <span className="merchant-unit">洛克贝</span></span>
          {it.limit > 0 && <span className="merchant-limit">限购 {it.limit}</span>}
        </div>
        {tLabel && (
          <div className="merchant-item-time" title={range || tLabel}>{tLabel}</div>
        )}
      </div>
    </div>
  )
}
