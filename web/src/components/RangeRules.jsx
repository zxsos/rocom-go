import React, { useState } from 'react'
import {
  RANGE_DIMS, DIM_BY_K, RULE_PALETTE, DEFAULT_RANGE_RULES,
  newRuleId, rangeRuleLabel,
} from '../utils/rules'

// RangeRules 体重/声音区间规则的编辑器(事件页与大地图共用同一份规则,见 hooks/useRangeRules)。
//
// 每条规则一张小卡,三段:
//   1. 头部:色点(点击换色)+ 名称(可直接改)+ 启停 + 删除
//   2. 区间条:整条是该维度的取值域,高亮段即当前区间 —— 这是这个编辑器里最值得留的
//      一笔:数字要读一遍再心算,色条扫一眼就知道「卡在尾巴上还是占了一大半」。
//   3. 底部:维度 + 下限/上限输入 + 命中数
//
// 删除**不做二次确认**:规则是用户自己配的一条区间,重建只要两秒;而确认弹窗会打断
// 「连着调几条」的手感。真正删不起的是数据(事件历史/涂地/路线进度),那些才确认。

// NumInput 数字输入:输入期间保留原始文本(draft),失焦后才回到规范化值。
//
// 为什么要 draft:直接在 onChange 里钳值的话,想输「-96」时先敲的「-」是 NaN,
// 会被立刻改写成边界值,光标和已输入内容一起乱掉 —— 负数区间根本没法输。
// 故输入时不钳(只把有限数提交上去),失焦丢弃草稿,显示真正生效的值。
function NumInput({ value, min, max, step, onChange, aria }) {
  const [draft, setDraft] = useState(null)
  const clamp = (n) => Math.min(Math.max(n, min), max)
  return (
    <input
      className="input rrule-num" type="number"
      min={min} max={max} step={step} value={draft ?? value} aria-label={aria}
      onChange={(e) => {
        setDraft(e.target.value)
        const n = Number(e.target.value)
        if (e.target.value !== '' && Number.isFinite(n)) onChange(clamp(n))
      }}
      onBlur={() => setDraft(null)}
    />
  )
}

// RangeBar 区间可视化:整条是维度取值域,高亮段是 [min,max]。
export function RangeBar({ rule, dim }) {
  const span = dim.max - dim.min
  const left = ((Math.min(rule.min, rule.max) - dim.min) / span) * 100
  const width = ((Math.abs(rule.max - rule.min)) / span) * 100
  return (
    <div className="rrule-bar" title={dim.hint}>
      <span
        className="rrule-bar-fill"
        style={{ left: `${left}%`, width: `${width}%`, background: rule.color }}
      />
    </div>
  )
}

export default function RangeRules({ rules = [], setRules, counts }) {
  const patch = (id, next) => setRules((rs) => rs.map((r) => (r.id === id ? { ...r, ...next } : r)))
  const remove = (id) => setRules((rs) => rs.filter((r) => r.id !== id))

  const add = () => setRules((rs) => {
    // 新规则默认铺满整个维度(全命中)而不是缩在边界上:用户接着就是收窄两端,
    // 从一个「太宽」的起点往里收,比从一个「太窄」的起点往外扩更符合直觉。
    const dim = RANGE_DIMS[0]
    return [...rs, {
      id: newRuleId(), dim: dim.k, min: dim.min, max: dim.max,
      label: '', color: RULE_PALETTE[rs.length % RULE_PALETTE.length], on: true,
    }]
  })

  // 维度切换:旧维度的区间按比例映射到新维度。直接保留数值会让体重 [98,100]
  // 切到声音后变成 [98,100](仍在声音域内,但语义完全变了);按比例映射则
  // 「尾巴上那一小段」切过去还是尾巴上的一小段,意图得以延续。
  const switchDim = (rule, k) => {
    const from = DIM_BY_K[rule.dim]
    const to = DIM_BY_K[k]
    const at = (v) => to.min + ((v - from.min) / (from.max - from.min)) * (to.max - to.min)
    patch(rule.id, {
      dim: k,
      min: Math.round(at(rule.min)),
      max: Math.round(at(rule.max)),
    })
  }

  const cycleColor = (rule) => {
    const i = RULE_PALETTE.indexOf(rule.color)
    patch(rule.id, { color: RULE_PALETTE[(i + 1) % RULE_PALETTE.length] })
  }

  return (
    <div className="rrule-list">
      {rules.map((rule) => {
        const dim = DIM_BY_K[rule.dim]
        const n = counts && counts[rule.id]
        return (
          <div className={'rrule' + (rule.on ? '' : ' off')} key={rule.id}>
            <div className="rrule-head">
              <button
                className="rrule-swatch" style={{ background: rule.color }}
                onClick={() => cycleColor(rule)}
                title="点击换色(这条规则的高亮/描边颜色)" aria-label="换色"
              />
              <input
                className="input rrule-label"
                value={rule.label}
                placeholder={rangeRuleLabel({ ...rule, label: '' })}
                onChange={(e) => patch(rule.id, { label: e.target.value })}
                aria-label="规则名称"
              />
              <button
                className={'rrule-mini' + (rule.on ? ' on' : '')}
                onClick={() => patch(rule.id, { on: !rule.on })}
                title={rule.on ? '停用这条规则(保留配置)' : '启用这条规则'}
                aria-label={rule.on ? '停用' : '启用'} aria-pressed={rule.on}
              >{rule.on ? '✓' : ''}</button>
              <button
                className="rrule-mini danger" onClick={() => remove(rule.id)}
                title="删除这条规则" aria-label="删除"
              >×</button>
            </div>

            <RangeBar rule={rule} dim={dim} />

            <div className="rrule-foot">
              <select
                className="select rrule-dim" value={rule.dim}
                onChange={(e) => switchDim(rule, e.target.value)}
                aria-label="判定维度"
              >
                {RANGE_DIMS.map((d) => <option key={d.k} value={d.k}>{d.n}</option>)}
              </select>
              <NumInput
                value={rule.min} min={dim.min} max={dim.max} step={dim.step}
                onChange={(v) => patch(rule.id, { min: v })} aria="下限"
              />
              <span className="rrule-tilde muted">~</span>
              <NumInput
                value={rule.max} min={dim.min} max={dim.max} step={dim.step}
                onChange={(v) => patch(rule.id, { max: v })} aria="上限"
              />
              {n != null && <span className="rrule-n muted" title="当前命中数">{n}</span>}
            </div>
          </div>
        )
      })}

      <div className="rrule-actions">
        <button className="btn ghost small" onClick={add}>+ 添加规则</button>
        <button
          className="btn ghost small"
          onClick={() => setRules(DEFAULT_RANGE_RULES.map((r) => ({ ...r })))}
          title="恢复成游戏奖牌四件套的边界(大块头/小不点/婉转声/粗嗓门)"
        >恢复默认</button>
      </div>

      {rules.length === 0 && (
        <div className="rrule-empty muted">还没有规则。命中区间由你定 —— 想只看「体重 40%~60% 的中等体型」也可以。</div>
      )}
    </div>
  )
}
