import React from 'react'
import { Avatar } from '../../components/avatar'
import { Types, Marks, Gender, Form, Blood, EggGroups } from '../../components/badges'
import { StatRange } from '../../components/stats'
import { SixBars } from './metrics'
import { locTag, fmtTime, voiceHot, pctHot } from '../../utils/format'

// PetTable 紧凑表格(陈列视图之外的第二视图,由 PetList 的视图开关切换)。
// itemProps(p) 由父级注入行交互(单击选中/双击详情/右键长按菜单)。
//
// 保留表格的理由很具体:**逐列对比**只有表格做得到。陈列视图适合"找那一只",
// 表格适合"把这一页的体重百分位/声音竖着比一遍"—— 后者是筛完之后的那一步。
//
// 这次改了三处:
//   - 首列吸左:表格 ~1100px 宽,横滚时没有它就无法知道在比哪只宠物;
//   - 六维换微柱:见 metrics.jsx 的 SixBars,省下的宽度让整表少一截横滚;
//   - 表头排序指示:原先是往文字后拼 ' ▲' 字符,切页时列宽会跟着抖一个字符,
//     且屏幕阅读器读不到排序态。改成 CSS 画的角标 + aria-sort。

// Th 单个表头:可排序列挂 sortable/sorted 与 aria-sort,角标由 CSS 画。
function Th({ k, sort, order, onSort, title, children }) {
  if (!onSort) return <th>{children}</th>
  const on = sort === k
  return (
    <th
      className={'sortable' + (on ? ' sorted' : '')}
      aria-sort={on ? (order === 'asc' ? 'ascending' : 'descending') : 'none'}
      title={title}
      onClick={() => onSort(k)}
    >
      {children}
    </th>
  )
}

export default function PetTable({ pets, selected, sort, order, onSort, itemProps }) {
  return (
    <div className="table-wrap">
      <table className="pets">
        <thead>
          <tr>
            <Th k="gid" sort={sort} order={order} onSort={onSort}>宠物</Th>
            <th>系别</th>
            <th>性格</th>
            <th>特长</th>
            <th>佩戴奖牌</th>
            <Th k="voice" sort={sort} order={order} onSort={onSort}>声音</Th>
            <Th k="weight" sort={sort} order={order} onSort={onSort} title="按百分位排序">体重</Th>
            <Th k="height" sort={sort} order={order} onSort={onSort} title="按百分位排序">身高</Th>
            <th>六维</th>
            <Th k="catchTime" sort={sort} order={order} onSort={onSort}>捕捉时间</Th>
          </tr>
        </thead>
        <tbody>
          {pets.map((p) => (
            <tr key={p.gid} className={p.gid === selected ? 'selected' : ''} {...itemProps(p)}>
              {/* 首列用 td 而非 th:table.pets th 带着"表头"的一整套样式
                  (吸顶/灰字/指针),行首单元格继承了就会变成灰字且被吸到顶部。
                  吸左由 .col-pet 自己声明(见 list.css),不与表头争。 */}
              <td className="col-pet">
                <div className="pet-cell">
                  <Avatar p={p} />
                  <div>
                    <div className="pet-name">
                      {p.name || p.species}<Gender g={p.gender} /><Marks p={p} /><Blood p={p} iconOnly /><Form form={p.form} /><EggGroups groups={p.eggGroups} />
                    </div>
                    <div className="pet-sub">{p.species} · Lv.{p.level}{p.book ? ` · #${p.book}` : ''} · {locTag(p)}</div>
                  </div>
                </div>
              </td>
              <td><Types types={p.types} icons={p.typeIcons} plain /></td>
              <td>{p.nature || '-'}</td>
              <td>{p.speciality || '无'}</td>
              <td>{p.medal || '-'}</td>
              <td className={voiceHot(p.voice)}>{p.voice}</td>
              <td className={pctHot(p.weightPct)}><StatRange value={p.weightKg} min={p.weightMin} max={p.weightMax} pct={p.weightPct} unit=" kg" stacked /></td>
              <td><StatRange value={p.heightM} min={p.heightMin} max={p.heightMax} pct={p.heightPct} unit=" m" stacked /></td>
              <td><SixBars p={p} /></td>
              <td className="muted">{fmtTime(p.catchTime)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
