import React, { useState } from 'react'
import {
  RANGE_DIMS, DIM_BY_K, RULE_PALETTE, RULE_PRESETS, RULE_SCHEMES,
  DEFAULT_RANGE_RULES, newRuleId, rangeRuleLabel, clampRange, sliderTop,
} from '../utils/rules'

// RangeRules 体重/声音区间规则的编辑器(事件页与大地图共用同一份规则,见 hooks/useRangeRules)。
//
// 每条规则两行:
//   1. 色点(换色)+ 名称(可直接改)+ 区间值 + 命中数 + 启停 + 删除
//   2. 维度(点切)+ **可拖的双滑块**
//
// 滑块是这一版的关键:上一版区间条只能看不能拖,改区间得去数字框里填 —— 有可视化
// 却不让人操作,是最别扭的一处。现在直接拖两端即可,数字只作为读数。
//
// 「+ 添加」改成从常用档位里点选(见 RULE_PRESETS),配上顶部整套方案(RULE_SCHEMES):
// 自由度高不等于每次都从零配,多数时候选一个现成的再微调就够了。

// RangeSlider 双滑块:两个原生 range 叠在一起,只用它们的 thumb 接收指针事件。
//
// 原生 range 只能给单值,故叠两个(下限/上限),容器设 pointer-events:none、
// 只给 thumb 设 auto —— 不这么处理的话上层 input 的轨道会整片盖住下层,
// 另一个端点就永远拖不动了。
function RangeSlider({ dim, min, max, color, onChange, label }) {
  const span = dim.max - dim.min
  const pct = (v) => ((v - dim.min) / span) * 100
  const top = sliderTop(min, max, dim)
  return (
    <div className="rslider">
      <div className="rslider-track" />
      <div
        className="rslider-fill"
        style={{ left: `${pct(min)}%`, width: `${pct(max) - pct(min)}%`, background: color }}
      />
      {(['min', 'max']).map((edge) => (
        <input
          key={edge}
          type="range"
          className={'rslider-input' + (top === edge ? ' top' : '')}
          min={dim.min} max={dim.max} step={dim.step}
          value={edge === 'min' ? min : max}
          onChange={(e) => {
            const v = Number(e.target.value)
            const [lo, hi] = clampRange(edge, v, edge === 'min' ? max : min, dim)
            onChange(lo, hi)
          }}
          aria-label={`${label}${edge === 'min' ? '下限' : '上限'}`}
        />
      ))}
    </div>
  )
}

export default function RangeRules({ rules = [], setRules, counts }) {
  const [picking, setPicking] = useState(false) // 预设选择区是否展开
  const patch = (id, next) => setRules((rs) => rs.map((r) => (r.id === id ? { ...r, ...next } : r)))
  const remove = (id) => setRules((rs) => rs.filter((r) => r.id !== id))

  const addPreset = (p) => {
    setRules((rs) => [...rs, {
      id: newRuleId(), dim: p.dim, min: p.min, max: p.max,
      label: p.label, color: p.color, on: true,
    }])
    setPicking(false)
  }

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
      {/* 整套方案:点一下替换全部规则。面向「不想逐条配」的场景。 */}
      <div className="rrule-schemes">
        {RULE_SCHEMES.map((s) => (
          <button
            key={s.k} className="chip"
            onClick={() => setRules(s.k === 'none' ? [] : schemeOf(s))}
            title={s.ids.length ? `只留:${s.ids.join('、')}` : '清空全部规则'}
          >{s.n}</button>
        ))}
      </div>

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
              {/* 区间读数:拖动时跟着变。有自定义名时名称框已占用,这里补上数值,
                  免得改了名就看不见区间了。 */}
              <span className="rrule-val muted">
                {rule.min}~{rule.max}{dim.unit}
              </span>
              {n != null && <span className="rrule-n muted" title="当前命中数">{n}</span>}
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

            <div className="rrule-slide">
              <button
                className="rrule-dim" onClick={() => switchDim(rule, rule.dim === 'voice' ? 'weightPct' : 'voice')}
                title="切换维度(体重百分位 / 嗓音原值),区间按比例映射过去"
              >{dim.n}</button>
              <RangeSlider
                dim={dim} min={rule.min} max={rule.max} color={rule.color}
                label={rule.label || dim.n}
                onChange={(lo, hi) => patch(rule.id, { min: lo, max: hi })}
              />
            </div>
          </div>
        )
      })}

      {/* 「+ 添加」展开常用档位:从零拖滑块太慢,点一个现成的再微调更省事。 */}
      <div className="rrule-actions">
        <button className="btn ghost small" onClick={() => setPicking((v) => !v)}
          aria-expanded={picking}>+ 添加规则</button>
        <button
          className="btn ghost small"
          onClick={() => setRules(DEFAULT_RANGE_RULES.map((r) => ({ ...r })))}
          title="恢复成游戏奖牌四件套的边界(大块头/小不点/婉转声/粗嗓门)"
        >恢复默认</button>
      </div>
      {picking && (
        <div className="rrule-presets">
          {RANGE_DIMS.map((d) => (
            <div className="rrule-preset-group" key={d.k}>
              <span className="muted">{d.n}</span>
              <div className="chips">
                {RULE_PRESETS.filter((p) => p.dim === d.k).map((p) => (
                  <span key={p.label} className="chip" onClick={() => addPreset(p)}
                    title={`${p.label}:${p.min}~${p.max}${d.unit}`}>
                    <i className="rrule-preset-dot" style={{ background: p.color }} />
                    {p.label}
                  </span>
                ))}
              </div>
            </div>
          ))}
        </div>
      )}

      {rules.length === 0 && (
        <div className="rrule-empty muted">
          还没有规则。点上面「+ 添加规则」选个常用档位,或直接选一套方案。
        </div>
      )}
    </div>
  )
}

// schemeOf 取方案的规则数组。抽出来是为了让上面的 JSX 短一点;
// ids 为空时(清空)返回空数组,由调用方保证不传 none 进来。
function schemeOf(s) {
  const presets = RULE_PRESETS.filter((p) => s.ids.includes(p.label))
  return presets.map((p) => ({
    id: newRuleId(), dim: p.dim, min: p.min, max: p.max,
    label: p.label, color: p.color, on: true,
  }))
}
