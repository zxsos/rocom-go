import React, { useContext } from 'react'
import { IconsContext } from '../../context'
import { ALL_TYPES, ALL_EGG_GROUPS, HOT_NATURES, HOT_NATURE_NAMES } from '../../constants'
import { InlineIcon } from '../../components/icons'
import { Gender } from '../../components/badges'
import { CATCH_RANGES } from './filters'

// FilterPanel 筛选侧栏:桌面常驻左列,移动端为侧滑抽屉(collapsed 控制开合)。
// children 为顶部的位置示意图(BoxMap)插槽;筛选状态由父级持有,经 set 增量更新。
export default function FilterPanel({ filter, options, total, collapsed, onClose, set, toggleType, reset, children }) {
  const icons = useContext(IconsContext)
  return (
    <>
      {/* 移动端筛选抽屉的背景遮罩:点击关闭 */}
      <div className={'filters-backdrop' + (collapsed ? '' : ' show')} onClick={onClose} />
      <aside className={'filters' + (collapsed ? ' collapsed' : '')}>
        {/* 抽屉标题栏(仅移动端显示):关闭入口与打开处的「筛选」按钮同侧 */}
        <div className="filters-bar">
          <span className="filters-title">筛选</span>
          <button className="icon-btn" onClick={onClose} aria-label="关闭筛选">✕</button>
        </div>
        {children}
        <div className="filter-group filter-reset">
          <button className="btn" onClick={reset}>重置筛选</button>
        </div>
        <div className="filter-group">
          <label>系别</label>
          <div className="chips">
            {ALL_TYPES.map((t) => (
              <span key={t} className={'chip' + ((filter.types || []).includes(t) ? ' on' : '')} onClick={() => toggleType(t)}>
                <InlineIcon src={icons.type && icons.type[t]} className="chip-ic" alt="" />{t}
              </span>
            ))}
          </div>
        </div>
        <div className="filter-group">
          <label>等级</label>
          <div className="range">
            <input className="input" type="number" placeholder="最小" value={filter.levelMin || ''} onChange={(e) => set({ levelMin: e.target.value })} />
            <span className="muted">~</span>
            <input className="input" type="number" placeholder="最大" value={filter.levelMax || ''} onChange={(e) => set({ levelMax: e.target.value })} />
          </div>
        </div>
        <NatureSelect filter={filter} set={set} />
        <Select label="天分" opts={options.talentRank} value={filter.talentRank} onChange={(v) => set({ talentRank: v })} />
        <Select label="特长" opts={options.speciality} value={filter.speciality} onChange={(v) => set({ speciality: v })} />
        <Select label="蛋组" opts={ALL_EGG_GROUPS} value={filter.eggGroup} onChange={(v) => set({ eggGroup: v })} />
        <Select label="奖牌" opts={options.medal} value={filter.medal} onChange={(v) => set({ medal: v })} />
        <Select label="宠物盒" opts={options.box} value={filter.box} onChange={(v) => set({ box: v })} />
        <div className="filter-group">
          <label>捕捉时间</label>
          <select className="select" value={filter.catchRange || ''} onChange={(e) => set({ catchRange: e.target.value })}>
            {CATCH_RANGES.map(([v, lbl]) => <option key={v || 'all'} value={v}>{lbl}</option>)}
          </select>
        </div>
        <div className="filter-group">
          <label>性别</label>
          <div className="radios">
            {['', '♂', '♀'].map((v) => (
              <label key={v || 'all'} className="radio">
                <input type="radio" name="gender" checked={(filter.gender || '') === v} onChange={() => set({ gender: v })} />
                {v ? <Gender g={v} /> : '全部'}
              </label>
            ))}
          </div>
        </div>
        <div className="filter-group">
          <label>变异</label>
          <div className="checks">
            <label className="check">
              <input type="checkbox" checked={filter.shiny === '1'} onChange={(e) => set({ shiny: e.target.checked ? '1' : '' })} />异色
            </label>
            <label className="check">
              <input type="checkbox" checked={filter.colorful === '1'} onChange={(e) => set({ colorful: e.target.checked ? '1' : '' })} />炫彩
            </label>
          </div>
        </div>
        {/* 抽屉底部操作条(仅移动端显示):重置 + 查看结果并关闭 */}
        <div className="filters-foot">
          <button className="btn" onClick={reset}>重置</button>
          <button className="btn primary" onClick={onClose}>查看 {total} 只</button>
        </div>
      </aside>
    </>
  )
}

// 性格下拉:热门性格逐项列出(带六维影响),「其他」= 排除全部热门项(natureExclude)。
function NatureSelect({ filter, set }) {
  return (
    <div className="filter-group">
      <label>性格</label>
      <select
        className="select"
        value={filter.natureExclude ? '__other__' : filter.nature || ''}
        onChange={(e) => {
          const v = e.target.value
          if (v === '__other__') set({ nature: '', natureExclude: HOT_NATURE_NAMES.join(',') })
          else set({ nature: v, natureExclude: '' })
        }}
      >
        <option value="">全部</option>
        {HOT_NATURES.map(([n, eff]) => (
          <option key={n} value={n}>{n}（{eff}）</option>
        ))}
        <option value="__other__">其他</option>
      </select>
    </div>
  )
}

function Select({ label, opts, value, onChange }) {
  return (
    <div className="filter-group">
      <label>{label}</label>
      <select className="select" value={value || ''} onChange={(e) => onChange(e.target.value)}>
        <option value="">全部</option>
        {(opts || []).map((o) => <option key={o} value={o}>{o}</option>)}
      </select>
    </div>
  )
}
