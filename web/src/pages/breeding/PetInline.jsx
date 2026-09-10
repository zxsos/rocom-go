import React from 'react'
import { imgURL } from '../../components/icons'
import { pctHot, voiceHot } from '../../utils/format'
import { fmtPct } from './pets'

// PetInline 是培育页里「一只宠一行」的紧凑展示:角色 + 头像 + 名 + 嗓音 + 体重百分位 + 性格。
//
// 亲本与子代共用它 —— 两者在记录里都是 EggParent 快照、字段完全一样,只有角色不同;
// 拆成两个组件只会把那套排布各写一遍,然后慢慢长歪(一处加了性格、另一处没加)。
//
// 数值一律带 V/W 前缀而不是靠位置辨认:窄屏两列会折成一行,那时若只把「哪个数是嗓音」
// 藏在 title 里,折行后没人分得清。
//
// 缺失值留「?」「—」而不省略整列:时间线是逐行对齐扫的,少一列会让同一列的数字错位。
export default function PetInline({ role, p, onPet, tag, ph }) {
  if (!p) {
    return (
      <span className="br-pi br-pi-ph" title={ph || ''}>
        <span className="br-pi-role">{role}</span>
        <span className="br-pi-name">{ph || '未知'}</span>
      </span>
    )
  }
  const clickable = !!(onPet && p.gid)
  const Element = clickable ? 'button' : 'span'
  const title = `${roleText(role)} ${p.name} · 嗓音 ${p.voice}`
    + (p.weightPct != null ? ` · 体重百分位 ${fmtPct(p.weightPct)}%` : '')
    + (p.talentRank ? ` · 天分 ${p.talentRank}` : '')
    + (clickable ? ' · 点击查看详情' : ' · 这只宠已不在库里,这里只是收蛋/认领那一刻的快照')
  return (
    <Element
      className="br-pi"
      {...(clickable ? { type: 'button', onClick: () => onPet(p.gid) } : {})}
      title={title}
    >
      <span className="br-pi-role">{role}</span>
      {p.img
        ? <img className="br-pi-img" src={imgURL(p.img)} alt="" draggable={false} />
        : <span className="br-pi-img ph">?</span>}
      <span className="br-pi-name">{p.name || '无名'}</span>
      <span className={'br-pi-v ' + (voiceHot(p.voice) || '')}>{`V${p.voice}`}</span>
      <span className={'br-pi-w ' + (pctHot(p.weightPct) || '')}>
        {p.weightPct == null ? 'W—' : `W${fmtPct(p.weightPct)}%`}
      </span>
      {/* 缺性格时**留占位**而不是整列消失:时间线是逐行对齐扫的,少一列会让同一列的数字错位
          (见文件头)。灰色的「性—」还兼作提示:这只宠收蛋那一刻还没进库,快照里没有性格
          —— 后端会在读取时按 gid 自动补,补不上的就在这里手填(编辑那一代)。 */}
      {p.nature
        ? <span className="br-pi-nature">{p.nature}</span>
        : <span className="br-pi-nature na" title="这只宠收蛋/认领那一刻还没进库,快照里没有性格 —— 能在库里找到它时后端会自动补上,补不上可以在编辑这一代时手填">性—</span>}
      {tag}
    </Element>
  )
}

const roleText = (role) => (role === '♀' ? '种母' : role === '♂' ? '种公' : '子代')
