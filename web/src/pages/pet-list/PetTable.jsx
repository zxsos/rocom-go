import React from 'react'
import { Avatar } from '../../components/avatar'
import { Types, Marks, Gender, Form, Blood, EggGroups } from '../../components/badges'
import { Six, StatRange } from '../../components/stats'
import { locTag, fmtTime, voiceHot, pctHot } from '../../utils/format'

// PetTable 桌面端宠物表格(移动端由 CSS 隐藏,改用 PetCards)。
// itemProps(p) 由父级注入行交互(单击选中/双击详情/右键长按菜单)。
export default function PetTable({ pets, selected, sort, order, onSort, itemProps }) {
  const arrow = (k) => (sort === k ? (order === 'asc' ? ' ▲' : ' ▼') : '')
  return (
    <div className="table-wrap">
      <table className="pets">
        <thead>
          <tr>
            <th onClick={() => onSort('gid')}>宠物{arrow('gid')}</th>
            <th>系别</th><th>性格</th><th>特长</th><th>佩戴奖牌</th>
            <th onClick={() => onSort('voice')}>声音{arrow('voice')}</th>
            <th title="按百分位排序" onClick={() => onSort('weight')}>体重{arrow('weight')}</th>
            <th title="按百分位排序" onClick={() => onSort('height')}>身高{arrow('height')}</th>
            <th>六维</th>
            <th onClick={() => onSort('catchTime')}>捕捉时间{arrow('catchTime')}</th>
          </tr>
        </thead>
        <tbody>
          {pets.map((p) => (
            <tr key={p.gid} className={p.gid === selected ? 'selected' : ''} {...itemProps(p)}>
              <td>
                <div className="pet-cell">
                  <Avatar p={p} />
                  <div>
                    <div className="pet-name">{p.name || p.species}<Gender g={p.gender} /><Marks p={p} /><Blood p={p} iconOnly /><Form form={p.form} /><EggGroups groups={p.eggGroups} /></div>
                    <div className="pet-sub">{p.species} · Lv.{p.level}{p.book ? ` · #${p.book}` : ''} · {locTag(p)}</div>
                  </div>
                </div>
              </td>
              <td><Types types={p.types} icons={p.typeIcons} plain /></td>
              <td>{p.nature || '-'}</td>
              <td>{p.speciality || '无'}</td>
              <td>{p.medal || '-'}</td>
              <td className={voiceHot(p.voice)}>{p.voice}</td>
              <td className={pctHot(p.weightPct)}><StatRange value={p.weightKg} min={p.weightMin} max={p.weightMax} pct={p.weightPct} unit=" kg" /></td>
              <td><StatRange value={p.heightM} min={p.heightMin} max={p.heightMax} pct={p.heightPct} unit=" m" /></td>
              <td><Six p={p} /></td>
              <td className="muted">{fmtTime(p.catchTime)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
