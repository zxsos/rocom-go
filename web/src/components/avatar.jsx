import React from 'react'
import { imgURL, useImgFallback } from './icons'
import { PetMark } from './badges'

// Avatar 渲染宠物小头像(列表/事件用);无图(未上线/缺源)或无 pet 回退 emoji;
// 有搭档标记时在左上角叠加徽章。
export function Avatar({ p, className = 'pet-avatar' }) {
  const src = p && p.image && p.image.head
  const [bad, onError] = useImgFallback(src)
  const inner = (src && !bad)
    ? <img className={className} src={imgURL(src)} alt={p.species} loading="lazy" onError={onError} />
    : <div className={className}>{p && p.shiny ? '✨' : '🐾'}</div>
  return <span className="avatar-wrap">{inner}<PetMark p={p} /></span>
}

// Portrait 渲染宠物全身图(详情用,优先 Pet256 全身缩略,退大头像);无图回退 emoji。
export function Portrait({ p }) {
  const src = (p.image && (p.image.portraitSmall || p.image.bigHead)) || ''
  const [bad, onError] = useImgFallback(src)
  return (
    <div className="detail-hero">
      <PetMark p={p} />
      {src && !bad
        ? <img src={imgURL(src)} alt={p.species} onError={onError} />
        : <span>{p.shiny ? '✨' : '🐾'}</span>}
    </div>
  )
}
