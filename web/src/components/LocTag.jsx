import React from 'react'
import { IconPackage, IconGlobe, IconHourglass } from './svg'
import { locKind, locTag } from '../utils/format'

// LocTag 渲染「位置图标 + 位置文案」,是位置信息在 UI 上的唯一权威显示形式。
//
// 图标原先由 locTag() 直接拼进字符串(📦 / 🌍 / ⏳)。emoji 有两个硬伤:
//   1. 不响应 currentColor —— 主题切换时周围都变了色,只有它纹丝不动;
//   2. 各家系统字形不同,同一处的视觉重量在 iOS / 安卓 / Windows 上不一致。
// 故把「文案」与「图标」拆开(见 utils/format.js 的 locKind / locTag),
// 由本组件把两者合起来。纯文本场合(title、剪贴板)继续用 locTag()。
//
// size 默认 12:位置标签都挤在卡片底部/表格副行里,比正文小两号。
const ICONS = { box: IconPackage, world: IconGlobe, pending: IconHourglass }

export default function LocTag({ pet, size = 12, className = 'pt-loc' }) {
  const Icon = ICONS[locKind(pet)] || IconHourglass
  return (
    <span className={className} title={locTag(pet)}>
      <Icon size={size} className="loc-ic" />
      {locTag(pet)}
    </span>
  )
}
