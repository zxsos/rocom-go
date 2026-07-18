import React from 'react'
import { Avatar } from '../../components/avatar'
import { Types, Marks, Gender, Form, Blood, EggGroups } from '../../components/badges'
import { Six, StatRange } from '../../components/stats'
import { locTag, voiceHot, pctHot } from '../../utils/format'

// PetCards 移动端宠物卡片列表(桌面端由 CSS 隐藏,对应 PetTable)。
export default function PetCards({ pets, selected, itemProps }) {
  return (
    <div className="cards">
      {pets.map((p) => (
        <div className={'card' + (p.gid === selected ? ' selected' : '')} key={p.gid} {...itemProps(p)}>
          <div className="card-head">
            <Avatar p={p} />
            <div style={{ flex: 1 }}>
              <div className="pet-name">{p.name || p.species}<Gender g={p.gender} /><Marks p={p} /><Blood p={p} iconOnly /><Form form={p.form} /></div>
              <div className="pet-sub">
                {p.species} · Lv.{p.level} · <span className="loc">{locTag(p)}</span>
              </div>
            </div>
            <Types types={p.types} icons={p.typeIcons} plain />
          </div>
          <div className="card-grid">
            <div>性格：{p.nature || '-'}</div>
            <div>特长：{p.speciality || '无'}</div>
            <div>奖牌：{p.medal || '-'}</div>
            {p.eggGroups?.length > 0 && <div className="egg-cell">蛋组：<EggGroups groups={p.eggGroups} /></div>}
            <div>体重：<span className={pctHot(p.weightPct)}><StatRange value={p.weightKg} min={p.weightMin} max={p.weightMax} pct={p.weightPct} unit=" kg" /></span></div>
            <div>身高：<StatRange value={p.heightM} min={p.heightMin} max={p.heightMax} pct={p.heightPct} unit=" m" /></div>
            <div>声音：<span className={voiceHot(p.voice)}>{p.voice}</span></div>
          </div>
          <Six p={p} />
        </div>
      ))}
    </div>
  )
}
