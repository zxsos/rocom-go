import React from 'react'
import { ImgAvatar } from '../../components/icons'

// 站场上的一张精灵卡:头像 + 名字/等级/性别 + 炫彩角标 + HP 条 + 4 技能槽。
//
// 三处刻意的设计:
//  1. **血条只改宽度、不重挂载卡片**:key 用 gid(服务端给的宠物 id),头像 img 的
//     src 不变,React 不会重建 <img> —— 否则每回合血条一动,头像就闪一下。
//  2. HP 未知(服务端没下发)时显示「—」而不是 0:0 血是「倒下」,两者不能混。
//  3. 技能只显示前 4 个(协议常给 7 个,含被动/聚能一类),悬停出全名。

const GENDER = { 1: { t: '♂', cls: 'male' }, 2: { t: '♀', cls: 'female' } }

function HpBar({ hp, max, side }) {
  const known = typeof hp === 'number' && typeof max === 'number' && max > 0
  const pct = known ? Math.max(0, Math.min(100, (hp / max) * 100)) : 0
  const low = known && pct <= 25
  return (
    <div className={'sy-hp' + (known ? '' : ' unknown')} title={known ? `${hp} / ${max}` : '服务端未下发'}>
      <div
        className={'sy-hp-fill ' + side + (low ? ' low' : '')}
        style={{ width: known ? pct + '%' : '100%' }}
      />
      <span className="sy-hp-text">{known ? `${hp} / ${max}` : '—'}</span>
    </div>
  )
}

export default function PetCard({ pet, side, selected, onSelect, dim }) {
  const gender = GENDER[pet.gender]
  const skills = (pet.skills || []).slice(0, 4)
  return (
    <button
      type="button"
      className={
        'sy-card' +
        (side === 'foe' ? ' foe' : '') +
        (pet.onField ? ' on-field' : '') +
        (pet.dead ? ' dead' : '') +
        (selected ? ' selected' : '') +
        (dim ? ' dim' : '')
      }
      onClick={() => onSelect && onSelect(pet)}
      title={[pet.name, pet.species, pet.natureName, (pet.damNames || []).join('/')].filter(Boolean).join(' · ')}
    >
      {pet.shiny && <span className={'sy-badge' + (pet.glassType === 2 ? ' hidden-glass' : '')}>炫彩</span>}
      <div className="sy-avatar-wrap">
        <ImgAvatar src={pet.img} alt={pet.name || ''} className="sy-avatar" />
      </div>
      <div className="sy-name">
        <span className="sy-name-t">{pet.name || pet.species || `#${pet.baseConfId}`}</span>
        {gender && <span className={'sy-gender ' + gender.cls}>{gender.t}</span>}
      </div>
      <div className="sy-meta">
        <span>{pet.level ? 'Lv' + pet.level : 'Lv—'}</span>
        {pet.natureName && <span className="sy-nature">{pet.natureName}</span>}
      </div>
      <HpBar hp={pet.hp} max={pet.hpMax} side={side} />
      <div className="sy-skills">
        {skills.length
          ? skills.map((s) => (
              <span key={s.id} className="sy-skill" title={s.name || '技能 ' + s.id}>
                {s.name || s.id}
              </span>
            ))
          : <span className="sy-skill empty" title="服务端未下发对手技能">未下发</span>}
      </div>
    </button>
  )
}
