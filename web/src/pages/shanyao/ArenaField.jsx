import React from 'react'
import PetCard from './PetCard'

// 对立站场:左下我方、右上他方,沿对角线对峙。
//
// 布局用 grid 而非绝对定位:两块各占一行(我方在下、对手在上),行内再用
// justify-content 把两边推向对角 —— 这样窄屏只需把头像缩小、技能槽折行,
// 不必重写一套坐标(绝对定位在 6 只 → 9 只不等的队伍长度下必然溢出)。
export default function ArenaField({ self, foe, round, selection, onSelect }) {
  const selfPets = (self && self.pets) || []
  const foePets = (foe && foe.pets) || []
  return (
    <div className="sy-arena">
      <div className="sy-arena-bg" aria-hidden="true" />
      <div className="sy-side foe">
        <div className="sy-side-head">
          <span className="sy-side-name">{foe?.name || '对手'}</span>
          {foe?.level ? <span className="muted">Lv{foe.level}</span> : null}
          <span className="sy-stamina" title="对手剩余体力">
            {typeof foe?.hp === 'number' ? `体力 ${foe.hp}${foe.hpMax ? '/' + foe.hpMax : ''}` : '体力 —'}
          </span>
        </div>
        <div className="sy-row">
          {foePets.map((p) => (
            <PetCard
              key={p.gid || 'p' + p.petId}
              pet={p}
              side="foe"
              selected={selection.defKey === keyOf('foe', p)}
              onSelect={() => onSelect('def', 'foe', p)}
              dim={selection.atkKey && selection.atkKey !== keyOf('foe', p)}
            />
          ))}
        </div>
      </div>

      <div className="sy-vs" aria-hidden="true">
        <span className="sy-vs-t">VS</span>
        {round ? <span className="sy-vs-r">第 {round} 回合</span> : null}
      </div>

      <div className="sy-side self">
        <div className="sy-row">
          {selfPets.map((p) => (
            <PetCard
              key={p.gid || 'p' + p.petId}
              pet={p}
              side="self"
              selected={selection.atkKey === keyOf('self', p)}
              onSelect={() => onSelect('atk', 'self', p)}
              dim={selection.defKey && selection.defKey !== keyOf('self', p)}
            />
          ))}
        </div>
        <div className="sy-side-head">
          <span className="sy-side-name">{self?.name || '我方'}</span>
          {self?.level ? <span className="muted">Lv{self.level}</span> : null}
          <span className="sy-stamina" title="我方剩余体力">
            {typeof self?.hp === 'number' ? `体力 ${self.hp}${self.hpMax ? '/' + self.hpMax : ''}` : '体力 —'}
          </span>
        </div>
      </div>
    </div>
  )
}

// keyOf 是选中态的键:gid 优先(服务端唯一 id),没有则退回战斗编号 + 阵营。
export function keyOf(side, pet) {
  return side + ':' + (pet.gid || 'p' + pet.petId)
}
