import React from 'react'
import { imgURL } from '../../components/icons'

// BoxMap 位置示意图:盒子 6 列 × 5 排(30 格)、队伍 3 列 × 6 行(18 格);
// 有宠物格显示头像,灰=空,选中高亮。
// 标题右侧上一个/下一个按钮在容器间切换(大世界队伍排在所有盒子最前)。
//
// 格子边长由 CSS 变量 --boxmap-cell 自适应(见 list.css),这里只按容器类型给列数:
// 盒子 6 列是**最宽**的形态,窄侧栏下必须让格子缩小才不撑破侧栏。
export default function BoxMap({ container, selected, onCell, onPrev, onNext }) {
  const slots = (container && container.slots) || []
  const heads = (container && container.heads) || {}
  const cols = (container && container.cols) || 6
  const cellTitle = (i) => {
    if (!container) return ''
    // 队伍:列=队、行=位(cols=3);盒子:行=排、列=格(cols=6)
    if (container.type === 'team') return `第${(i % cols) + 1}队第${Math.floor(i / cols) + 1}位`
    return `第${Math.floor(i / cols) + 1}排第${(i % cols) + 1}格`
  }
  return (
    <div className="boxmap">
      <div className="boxmap-head">
        <span className="boxmap-name">{container ? container.name : '盒子位置'}</span>
        <span className="boxmap-nav">
          <button className="boxmap-btn" title="上一个" onClick={onPrev}>‹</button>
          <button className="boxmap-btn" title="下一个" onClick={onNext}>›</button>
        </span>
      </div>
      {/* 只给列数,格子边长交给 CSS 自适应(见 list.css 的 --boxmap-cell):
          盒子 6 列在窄侧栏下要把格子缩小才不撑破侧栏,写死 40px 就会溢出。 */}
      <div className="boxmap-grid" style={{ '--boxmap-cols': cols }}>
        {slots.map((gid, i) => (
          <div
            key={i}
            className={'boxmap-cell' + (gid ? ' filled' : '') + (gid && gid === selected ? ' on' : '')}
            title={gid ? cellTitle(i) : '空'}
            onClick={() => gid && onCell(gid, container)}
          >
            {gid && heads[gid] ? <img src={imgURL(heads[gid])} alt="" loading="lazy" /> : null}
          </div>
        ))}
      </div>
    </div>
  )
}
