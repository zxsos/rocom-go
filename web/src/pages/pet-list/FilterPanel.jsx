import React, { useContext } from 'react'
import { IconsContext } from '../../context'
import { ALL_TYPES, ALL_EGG_GROUPS } from '../../constants'
import { InlineIcon } from '../../components/icons'
import { Gender } from '../../components/badges'
import { CATCH_RANGES } from './filters'
import NatureMatrix from './NatureMatrix'
import Dropdown from '../../components/Dropdown'

// FilterPanel 筛选侧栏:桌面常驻左列,移动端为侧滑抽屉(collapsed 控制开合)。
// children 为顶部的位置示意图(BoxMap)插槽;筛选状态由父级持有,经 set 增量更新。
//
// 分组按「玩家找宠物时先在心里问什么」划分四块,而不是按控件类型堆在一起:
//   外观 → 资质 → 体型声音 → 来源
// 原先 13 组平铺,扫视时每一组都要读标题才知道是不是自己要找的那项;
// 分块后可以先定位到块、再在块内找,且块与块之间有明确的语义边界。
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

        <fieldset className="filter-sec">
          <legend>外观</legend>
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
            <label>变异</label>
            <div className="toggles">
              <Toggle checked={filter.shiny === '1'} onChange={(v) => set({ shiny: v ? '1' : '' })}>异色</Toggle>
              <Toggle checked={filter.colorful === '1'} onChange={(v) => set({ colorful: v ? '1' : '' })}>炫彩</Toggle>
            </div>
          </div>
          <div className="filter-group">
            <label>性别</label>
            <div className="toggles">
              {['', '♂', '♀'].map((v) => (
                <Radio
                  key={v || 'all'} name="gender"
                  checked={(filter.gender || '') === v}
                  onChange={() => set({ gender: v })}
                >
                  {v ? <Gender g={v} /> : '全部'}
                </Radio>
              ))}
            </div>
          </div>
        </fieldset>

        <fieldset className="filter-sec">
          <legend>资质</legend>
          {/* 性格改走 6×6 方阵:原先的「热门 8 项 + 其他」丢掉了 22 个性格的区分度,
              且「其他」是排除式条件,与其它筛选叠加时语义很绕。方阵一次给全 30 个,
              并支持「只要某维↑ / 只要某维↓」这类按维度的选法。 */}
          <NatureMatrix
            matrix={options.natureMatrix}
            nature={filter.nature}
            natureIn={filter.natureIn}
            onChange={set}
          />
          <Select label="天分" opts={options.talentRank} value={filter.talentRank} onChange={(v) => set({ talentRank: v })} />
          <Select label="特长" opts={options.speciality} value={filter.speciality} onChange={(v) => set({ speciality: v })} />
        </fieldset>

        <fieldset className="filter-sec">
          <legend>体型 · 声音</legend>
          <Select label="奖牌" opts={options.medal} value={filter.medal} onChange={(v) => set({ medal: v })} />
          <div className="filter-group">
            <label>奖牌特征</label>
            <div className="toggles">
              <Toggle checked={filter.medalBig === '1'} onChange={(v) => set({ medalBig: v ? '1' : '' })}>大块头</Toggle>
              <Toggle checked={filter.medalSmall === '1'} onChange={(v) => set({ medalSmall: v ? '1' : '' })}>小不点</Toggle>
              <Toggle checked={filter.medalHigh === '1'} onChange={(v) => set({ medalHigh: v ? '1' : '' })}>婉转声</Toggle>
              <Toggle checked={filter.medalLow === '1'} onChange={(v) => set({ medalLow: v ? '1' : '' })}>粗嗓门</Toggle>
            </div>
            <div className="muted small">按体重百分位/嗓音判定，与地图奖牌筛选同口径；可多选，多选=同时满足（如大块头+婉转声）</div>
          </div>
        </fieldset>

        <fieldset className="filter-sec">
          <legend>来源</legend>
          <Select label="宠物盒" opts={options.box} value={filter.box} onChange={(v) => set({ box: v })} />
          <div className="filter-group">
            <label>捕捉时间</label>
            <Dropdown
              value={filter.catchRange || ''}
              options={CATCH_RANGES.map(([v, lbl]) => ({ value: v, label: lbl }))}
              onChange={(v) => set({ catchRange: v })}
            />
          </div>
          <Select label="蛋组" opts={ALL_EGG_GROUPS} value={filter.eggGroup} onChange={(v) => set({ eggGroup: v })} />
        </fieldset>

        {/* 抽屉底部操作条(仅移动端显示):重置 + 查看结果并关闭 */}
        <div className="filters-foot">
          <button className="btn" onClick={reset}>重置</button>
          <button className="btn primary" onClick={onClose}>查看 {total} 只</button>
        </div>
      </aside>
    </>
  )
}

function Select({ label, opts, value, onChange }) {
  return (
    <div className="filter-group">
      <label>{label}</label>
      <Dropdown value={value || ''} options={opts || []} onChange={onChange} />
    </div>
  )
}

// —— 按键式筛选开关(替代原生 checkbox / radio)——
//
// 原先直接用 <input type="checkbox"> / <input type="radio">,与同一面板里系别的
// .chip 按钮视觉割裂(一个是原生方框、一个是圆角按键),且原生控件在移动端只有
// ~13px 的命中区,远低于项目的 36px 触控基线。
//
// 做法:**保留原生 input**(语义、键盘可达、读屏播报都靠它),只用 CSS 把它
// 铺满整块并设为透明,视觉全部交给 label。于是:
//   - 整块按键都是命中区(≥32px,移动端 36px);
//   - 键盘 Tab / 空格切换与读屏的「已选中」播报都还是原生的,没有自制控件
//     常见的可访问性缺失;
//   - 选中态由 React 显式加 .on,不依赖 :has() —— 老浏览器上最多少个焦点环,
//     不会把「选中了没」整个显示错(那是信息错误,比样式降级严重得多)。
function Toggle({ checked, onChange, children, title }) {
  return (
    <label className={'toggle' + (checked ? ' on' : '')} title={title}>
      <input type="checkbox" checked={checked} onChange={(e) => onChange(e.target.checked)} />
      <span>{children}</span>
    </label>
  )
}

// Radio 单选(与 Toggle 同视觉,name 相同的为一组)。
function Radio({ name, checked, onChange, children, title }) {
  return (
    <label className={'toggle' + (checked ? ' on' : '')} title={title}>
      <input type="radio" name={name} checked={checked} onChange={onChange} />
      <span>{children}</span>
    </label>
  )
}
