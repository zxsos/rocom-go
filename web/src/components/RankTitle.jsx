import React from 'react'

// 排行榜称号配色映射(每晚 00:05 结算,当天佩戴一天;样式见 styles/leaderboard.css)
const TITLE_CLS = { '大富翁': 't-rich', '赚钱王': 't-earner', '败家子': 't-spender' }

// RankTitle 账号旁佩戴的称号徽标(title 为空时渲染 null)。
export default function RankTitle({ title }) {
  if (!title) return null
  return <span className={'rank-title ' + (TITLE_CLS[title] || 't-x')}>{title}</span>
}
