import React, { useState, useMemo } from 'react'
import { StatIcon } from '../../components/icons'

// ---- 性格罗盘:6×6 方阵筛选 ----
//
// 数据是天然的方阵(见后端 gamedata.NatureMatrix):31 个性格里 30 个非中性,
// 铺满 6×6 去掉对角线。故 UI 直接照数据形状铺,不另造下拉/列表。
//
// 关键约定 —— **左轴是增益、顶轴是减益**:
//   行 i(左轴绿色)= 该维度 +10%,列 j(顶轴红色)= 该维度 -10%,
//   格子 [i][j] 即「+行维 −列维」的性格(如 [物攻][魔攻] = 固执)。
//   两个轴用**语义色**分色(绿=↑、红=↓),左上角再放一枚对角分割的徽标点破它,
//   玩家不必读说明就能猜出坐标轴含义。
//
// 三种选择粒度,共用一套选中态:
//   1. 点格子  → 精确一个性格
//   2. 点左轴  → 该维度↑ 的全部 5 个(整行)
//   3. 点顶轴  → 该维度↓ 的全部 5 个(整列)
// 行/列头是**三态**(未选 / 半选 / 全选),点击时「非全选 → 全选,全选 → 清空」,
// 与常见的全选控件一致。

// 六维顺序:与后端 iconMeta.Stat 及 nature_effect 的维度编号 1-6 一致
// (1生命 2物攻 3魔攻 4物防 5魔防 6速度),**不可随意调整** —— 后端下发的
// 方阵就是按这个顺序索引的,前端重排会让每格装成别的性格。
export const STAT_KEYS = ['hp', 'attack', 'spAttack', 'defense', 'spDefense', 'speed']
const STAT_NAMES = ['生命', '物攻', '魔攻', '物防', '魔防', '速度']

// 三态:0=未选 1=半选 2=全选
const NONE = 0, PART = 1, ALL = 2

export default function NatureMatrix({ matrix, nature, natureIn, onChange }) {
  const [open, setOpen] = useState(false)

  // 选中集合:优先用多选(natureIn),单格选择时前端只给 nature
  const picked = useMemo(() => {
    const s = new Set()
    if (natureIn) natureIn.split(',').filter(Boolean).forEach((n) => s.add(n))
    else if (nature) s.add(nature)
    return s
  }, [nature, natureIn])

  const empty = picked.size === 0
  // 展开态默认打开:若已有选择(多半是从别处点「筛选相同性格」过来的),
  // 折叠起来会看不到选了什么,像是丢了条件。
  const expanded = open || !empty

  // 行/列各自的选中计数(对角线为空,故有效格最多 5 个)
  const rowPicked = useMemo(() => count(matrix, (i) => i), [matrix])
  const colPicked = useMemo(() => count(matrix, (_i, j) => j), [matrix])
  const pickedRows = useMemo(() => countPicked(matrix, picked, (i) => i), [matrix, picked])
  const pickedCols = useMemo(() => countPicked(matrix, picked, (_i, j) => j), [matrix, picked])

  const toggleCell = (name) => {
    const next = new Set(picked)
    next.has(name) ? next.delete(name) : next.add(name)
    emit(next)
  }
  // 行/列:非全选则选满,全选则清空(保留其它行/列的选择)
  const toggleAxis = (axis, idx) => {
    const isRow = axis === 'row'
    const names = []
    for (let k = 0; k < 6; k++) {
      const n = isRow ? matrix[idx][k] : matrix[k][idx]
      if (n) names.push(n)
    }
    const allIn = names.every((n) => picked.has(n))
    const next = new Set(picked)
    names.forEach((n) => (allIn ? next.delete(n) : next.add(n)))
    emit(next)
  }
  const emit = (set) => {
    const arr = [...set]
    // 一个也不给时两个字段都清空;多于一个走 natureIn,单个走 nature
    // (后者兼容「筛选相同性格」这类只给一个的入口,行为与改版前一致)。
    onChange(arr.length === 1
      ? { nature: arr[0], natureIn: '', natureExclude: '' }
      : { nature: '', natureIn: arr.join(','), natureExclude: '' })
  }
  const clear = () => onChange({ nature: '', natureIn: '', natureExclude: '' })

  const axisState = (idx, isRow) => {
    const total = isRow ? rowPicked[idx] : colPicked[idx]
    const got = isRow ? pickedRows[idx] : pickedCols[idx]
    if (got === 0 || total === 0) return NONE
    return got >= total ? ALL : PART
  }

  return (
    <div className="filter-group natmat">
      <label>
        性格
        {!empty && (
          <button type="button" className="natmat-clear" onClick={clear}>清除 {picked.size}</button>
        )}
      </label>

      {/* 快捷行:只要某维度↑。折叠时也能用,是最高频的路径(玩家通常只关心「速度↑」)。
          与展开后的行头等价,但折叠状态下行头不可见,故这一行始终保留。 */}
      <div className="natmat-quick">
        {STAT_KEYS.map((k, i) => {
          const st = axisState(i, true)
          return (
            <button
              key={k}
              type="button"
              className={'natmat-q' + (st === ALL ? ' on' : '') + (st === PART ? ' part' : '')}
              onClick={() => toggleAxis('row', i)}
              title={`${STAT_NAMES[i]} +10%(共 ${rowPicked[i]} 个性格)`}
            >
              <StatIcon statKey={k} className="natmat-q-ic" />
              <span className="natmat-q-up">↑</span>
            </button>
          )
        })}
      </div>

      {/* 折叠开关:默认收起,避免筛选面板被 264px 高的方阵撑到无法扫视 */}
      <button type="button" className="natmat-toggle" onClick={() => setOpen((v) => !v)}>
        {expanded ? '收起方阵' : '展开方阵'}
        <span className={'natmat-caret' + (expanded ? ' up' : '')}>▾</span>
      </button>

      {expanded && (
        <div className="natmat-grid" role="grid">
          {/* (0,0) 角落:对角分割的增益/减益徽标 —— 两轴的图例。
              放在这里而不是画在图外,是因为它就是坐标系的原点,
              视线从表头扫到正文必然经过这里。 */}
          <div className="natmat-corner" title="左列=增益 +10%,顶行=减益 -10%">
            <i className="natmat-corner-up" />
            <i className="natmat-corner-dn" />
          </div>
          {STAT_KEYS.map((k, j) => {
            const st = axisState(j, false)
            return (
              <button
                key={'c' + k}
                type="button"
                className={'natmat-h natmat-h-col' + (st === ALL ? ' on' : '') + (st === PART ? ' part' : '')}
                onClick={() => toggleAxis('col', j)}
                title={`${STAT_NAMES[j]} -10%(共 ${colPicked[j]} 个性格)`}
              >
                <StatIcon statKey={k} className="natmat-h-ic" />
              </button>
            )
          })}
          {matrix.map((row, i) => (
            <React.Fragment key={'r' + i}>
              <button
                type="button"
                className={'natmat-h natmat-h-row' + (axisState(i, true) === ALL ? ' on' : '') + (axisState(i, true) === PART ? ' part' : '')}
                onClick={() => toggleAxis('row', i)}
                title={`${STAT_NAMES[i]} +10%(共 ${rowPicked[i]} 个性格)`}
              >
                <StatIcon statKey={STAT_KEYS[i]} className="natmat-h-ic" />
              </button>
              {row.map((name, j) => {
                // 对角线:该组合游戏内不存在,画一道斜线而非留白 ——
                // 留白会被读成「有个没加载出来的性格」。
                if (i === j) return <span key={j} className="natmat-cell natmat-diag" aria-hidden="true" />
                return (
                  <button
                    key={j}
                    type="button"
                    className={'natmat-cell' + (picked.has(name) ? ' on' : '')}
                    onClick={() => toggleCell(name)}
                    title={`${STAT_NAMES[i]}↑ ${STAT_NAMES[j]}↓`}
                  >
                    {name}
                  </button>
                )
              })}
            </React.Fragment>
          ))}
        </div>
      )}
    </div>
  )
}

// pick 取「这一格归哪一行/哪一列」:传 (i) => i 算按行,(_i, j) => j 算按列。
// 两个统计函数只差在「格子有没有被选中」,合并成一个遍历、两种用途反而绕,故分开写。
const each = (matrix, fn) => {
  for (let i = 0; i < 6; i++) {
    for (let j = 0; j < 6; j++) {
      if (matrix[i][j]) fn(i, j, matrix[i][j])
    }
  }
}

// count 统计每行/每列的**有效格**数(对角线为空,故每行最多 5)。
function count(matrix, pick) {
  const out = new Array(6).fill(0)
  each(matrix, (i, j) => { out[pick(i, j)]++ })
  return out
}

// countPicked 统计每行/每列**已被选中**的格数。
function countPicked(matrix, picked, pick) {
  const out = new Array(6).fill(0)
  each(matrix, (i, j, n) => { if (picked.has(n)) out[pick(i, j)]++ })
  return out
}
