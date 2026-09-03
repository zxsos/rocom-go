import React, { useMemo, useState } from 'react'
import {
  RANGE_DIMS, DIM_BY_K, RULE_PALETTE, RULE_PRESETS, RULE_SCHEMES,
  DEFAULT_RANGE_RULES, newRuleId, rangeRuleLabel, clampRange, rangeScale, sliderTop,
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

// 轨道分辨率:输入在**位置空间**(0~SLIDER_STEPS)上走,再由 rangeScale 换算成取值。
//
// 有了变焦刻度之后,input 的 min/max 就不能再用取值域了 —— 浏览器只认均匀分布,
// 那样重点区的放大等于白做。故输入一律是「轨道上的第几格」,取值由尺子换算出来。
const SLIDER_STEPS = 1000

// RangeSlider 双滑块:两个原生 range 叠在一起,只用它们的 thumb 接收指针事件。
//
// 原生 range 只能给单值,故叠两个(下限/上限),容器设 pointer-events:none、
// 只给 thumb 设 auto —— 不这么处理的话上层 input 的轨道会整片盖住下层,
// 另一个端点就永远拖不动了。
//
// 轨道是**非线性**的(见 utils/rules.js 的 rangeScale):两头的奖牌区被放大,
// 中间段压扁。故这里额外把重点区与奖牌刻度画出来 —— 刻度不均匀这件事必须让用户
// 看见,否则只会以为拖动不准。
function RangeSlider({ dim, min, max, color, onChange, label }) {
  const scale = useMemo(() => rangeScale(dim), [dim])
  const top = sliderTop(min, max, dim)
  const lo = scale.toPos(min)
  const hi = scale.toPos(max)

  const at = (edge, pos) => {
    const v = scale.toValue(pos / SLIDER_STEPS)
    const [a, b] = clampRange(edge, v, edge === 'min' ? max : min, dim)
    onChange(a, b)
  }

  return (
    <div className="rslider">
      <div className="rslider-track" />
      {/* 重点区:刻度被放大的那两段。画得比轨道略高略亮,像两段被架起来的放大镜 */}
      {scale.segs.filter((s) => s.focus).map((s, i) => (
        <div
          key={i}
          className="rslider-zone"
          style={{ left: `${s.start * 100}%`, width: `${(s.end - s.start) * 100}%` }}
        />
      ))}
      <div
        className="rslider-fill"
        style={{ left: `${lo * 100}%`, width: `${(hi - lo) * 100}%`, background: color }}
      />
      {/* 奖牌边界:既是刻度线,也是磁吸点(拖到附近会吸附过去) */}
      {scale.marks.map((m) => (
        <i key={m.v} className="rslider-tick" style={{ left: `${m.pos * 100}%` }} />
      ))}
      {(['min', 'max']).map((edge) => (
        <input
          key={edge}
          type="range"
          className={'rslider-input' + (top === edge ? ' top' : '')}
          min={0} max={SLIDER_STEPS} step={1}
          value={Math.round((edge === 'min' ? lo : hi) * SLIDER_STEPS)}
          onChange={(e) => at(edge, Number(e.target.value))}
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
                title={
                  '切换维度(体重百分位 / 嗓音原值),区间按比例映射过去 —— 两头的奖牌窗口' +
                  '在两侧是等比放置的,故「体重 98~100」切过去正好是「声音 96~100」。\n' +
                  '轨道刻度不均匀:奖牌区被放大(底色更亮的两段,上面的刻度线是奖牌边界,' +
                  '拖到附近会吸附),中间段压扁 —— 中间拖得快,两头拖得细。'
                }
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
