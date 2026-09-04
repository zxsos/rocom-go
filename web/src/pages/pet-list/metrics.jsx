import React from 'react'
import { StatIcon } from '../../components/icons'

// 宠物列表的数值可视化元件:六维(卡片小格 / 表格微柱)、百分位标尺、量测行。
//
// 为什么要把数字画成条:这些值的**绝对值没有可比性**(不同形态、不同等级),
// 玩家真正读的是「这只在我找的那一档里吗」—— 而那是一个位置/比例问题。
// 文本要逐个读再在脑内排序,条形与标尺把这一步交给眼睛。

// STAT_MAX 六维定标上限:与详情页雷达图 RADAR_MAX(components/stats.jsx)同值。
// 两处必须是同一把尺子 —— 列表里「条快满了」的宠物进详情看雷达也该是贴着外环的,
// 否则同一个人对同一只宠物会得到两个相反的印象。改一处务必改另一处。
const STAT_MAX = 500

// 六维顺序:生命 / 物攻 / 物防 / 魔攻 / 魔防 / 速度(游戏面板序)。
// 与详情页雷达的顺时针序(生命→魔攻→魔防→速度→物防→物攻)不同是**刻意的**:
// 雷达沿圆周排、要首尾相接,这里是按行读的 —— 行读必须把物攻物防、魔攻魔防挨着放,
// 否则「攻防」这一对要跨列比。
export const SIX_KEYS = [
  ['hp', '生命'],
  ['attack', '物攻'],
  ['defense', '物防'],
  ['spAttack', '魔攻'],
  ['spDefense', '魔防'],
  ['speed', '速度'],
]

// fill 把值换算成百分比字符串。夹到 [2%,100%]:
// 下限 2% 是让**值为 0 的维度仍看得见一条底**——没有下限的话,0 与「没有这一维」
// 长得一样,而 Never 蛋孵出的宠物确实有 0 值维度,会被误读成数据缺失。
const fill = (v) => `${Math.min(100, Math.max(2, ((v || 0) / STAT_MAX) * 100)).toFixed(1)}%`

// statTitle 单个维度的悬停全文:标签 + 面板值 + 性格增减 + 天分。
// 条形/微柱只画了长短,精确构成靠这里兜住(不悬停也能进详情看雷达图)。
const statTitle = (label, s) =>
  `${label} ${s.value ?? 0}`
  + (s.nature === 1 ? ' · 性格 +10%' : s.nature === -1 ? ' · 性格 -10%' : '')
  + (s.talentLv > 0 ? ` · 天分 +${s.talentLv}` : '')

// SixGrid 陈列卡里的六维:3 列 × 2 行,每格「属性图标 + 面板值 + 天分/性格角标 + 定标条」。
// 条长按 STAT_MAX 定标,跨卡片可比 —— 一排卡片扫过去,条最长那几只一目了然。
export function SixGrid({ p }) {
  return (
    <div className="sixgrid">
      {SIX_KEYS.map(([key, label]) => {
        const s = p[key] || {}
        return (
          <div className="sixgrid-cell" key={key} title={statTitle(label, s)}>
            <span className="sixgrid-top">
              <StatIcon statKey={key} className="sixgrid-ic" />
              <b className={s.talentLv > 0 ? 'has-talent' : undefined}>{s.value ?? 0}</b>
              {s.talentLv > 0 && <i className="sixgrid-talent">+{s.talentLv}</i>}
              {s.nature === 1 && <i className="sixgrid-nat up">▲</i>}
              {s.nature === -1 && <i className="sixgrid-nat dn">▼</i>}
            </span>
            <span className="sixgrid-track"><i style={{ width: fill(s.value) }} /></span>
          </div>
        )
      })}
    </div>
  )
}

// SixBars 表格里的六维微柱:6 根竖条,高度按同一把尺子定标。
//
// 用竖条而非「六个数横排」是两个取舍的结果:
//   - 表格里六维是**定性**看的(这只偏攻还是偏防、有没有明显短板),精确值在
//     详情弹窗的雷达图与数值表里;竖条一条 30px 宽就画完,六个数要 ~200px;
//   - 竖条在列内**上下对齐**,跨行扫视时能直接比「这一列(速度)哪几只突出」——
//     横排数字反而做不到,因为眼睛要逐个读。
// 精确值没丢:每根条都带 title(标签 + 值 + 性格 + 天分)。
export function SixBars({ p }) {
  return (
    <span className="sixbars">
      {SIX_KEYS.map(([key, label]) => {
        const s = p[key] || {}
        return (
          <i
            key={key}
            className={'sixbars-b' + (s.nature === 1 ? ' up' : s.nature === -1 ? ' dn' : '')}
            style={{ height: fill(s.value) }}
            title={statTitle(label, s)}
          />
        )
      })}
    </span>
  )
}

// PctBar 百分位标尺:一条轨道 + 一个按百分位定位的游标。
//
// 为什么画成标尺而不是直接写「99.2%」:找「大块头/小不点」时玩家要的是**位置**
// (靠上界还是靠下界、离极值还差多少),数字得先读再换算。游标把这一步省掉,
// 且一排卡片扫过去,靠右那几只就是大块头 —— 不用逐个读小数。
export function PctBar({ pct }) {
  if (pct == null) return null
  return (
    <span className="pctbar" aria-hidden="true">
      <i className="pctbar-dot" style={{ left: `${Math.min(100, Math.max(0, pct)).toFixed(1)}%` }} />
    </span>
  )
}

// Measure 一条量测行(体重/身高):标签 + 值 + 百分位标尺 + 百分位数字。
// 悬停全文带取值范围(下限-上限)——标尺只给了相对位置,绝对区间仍要可查。
export function Measure({ label, value, unit, min, max, pct }) {
  const hasRange = max > min
  return (
    <div className="measure" title={hasRange ? `${label} ${min}~${max}${unit}` : undefined}>
      <span className="measure-lb">{label}</span>
      <b className="measure-v">{value == null ? '-' : Number(value).toFixed(2)}<i>{unit}</i></b>
      <PctBar pct={pct} />
      <span className="measure-pct">{pct == null ? '-' : pct.toFixed(1) + '%'}</span>
    </div>
  )
}
