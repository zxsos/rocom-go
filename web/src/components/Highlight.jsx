import React, { memo } from 'react'

// Highlight 把纯文本中所有与 query 匹配的部分(大小写不敏感)包进 <mark>,
// 用于搜索框输入时的协议名 / 解析树内容高亮。query 为空或 text 为空时原样返回。
export const Highlight = memo(function Highlight({ text = '', query = '' }) {
  const q = String(query).trim()
  const raw = String(text)
  if (!q || !raw) return raw
  const lower = q.toLowerCase()
  const out = []
  let i = 0
  while (i < raw.length) {
    const idx = raw.toLowerCase().indexOf(lower, i)
    if (idx < 0) {
      out.push(raw.slice(i))
      break
    }
    if (idx > i) out.push(raw.slice(i, idx))
    out.push(<mark key={idx}>{raw.slice(idx, idx + q.length)}</mark>)
    i = idx + q.length
  }
  return out
})
