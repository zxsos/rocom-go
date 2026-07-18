import React, { useState, useEffect } from 'react'
import { getNameOptions } from '../../api'
import { HOT_NATURE_NAMES } from '../../constants'
import { FIELDS, WEIGHT_OPTS, VOICE_OPTS } from './highlight'

// RulePanel 高亮规则侧栏:桌面常驻左栏,移动端为抽屉(collapsed 控制开合)。
// 规则与 AND/OR 模式由父级持有;「种类」的自由输入草稿是面板内部状态。
export default function RulePanel({ rules, mode, setMode, addRule, toggleRule, collapsed, onClose }) {
  const [nameOpts, setNameOpts] = useState({ speciality: [] }) // 全量特长点选条目
  const [speciesDraft, setSpeciesDraft] = useState('') // 「种类」自由输入框内容
  useEffect(() => { getNameOptions().then((o) => setNameOpts(o || { speciality: [] })).catch(() => {}) }, [])

  // 某维度下可点选的条目:性格取宠物列表常用项、特长取全表、体重/声音取两极标签。
  const paletteFor = (field) => {
    if (field === 'nature') return HOT_NATURE_NAMES
    if (field === 'speciality') return (nameOpts.speciality || []).filter((v) => v !== '无') // 「无特长」不作高亮项
    if (field === 'weight') return WEIGHT_OPTS
    if (field === 'voice') return VOICE_OPTS
    return []
  }
  const hasRule = (field, value) => rules.some((r) => r.field === field && r.value === value)

  // 种类为自由输入,回车/点「添加」落规则(去重)。
  const addSpecies = () => {
    const value = speciesDraft.trim()
    if (value) addRule('species', value)
    setSpeciesDraft('')
  }
  const speciesRules = rules.filter((r) => r.field === 'species')

  return (
    <>
      {/* 移动端规则抽屉的背景遮罩:点击关闭 */}
      <div className={'filters-backdrop' + (collapsed ? '' : ' show')} onClick={onClose} />
      <aside className={'filters' + (collapsed ? ' collapsed' : '')}>
        {/* 标题行:标题在左,AND/OR 切换靠右;✕ 关闭仅移动端抽屉显示 */}
        <div className="rules-header">
          <h3 className="rules-title">高亮规则</h3>
          <div className="rule-logic-toggle">
            <button className={'btn small' + (mode === 'and' ? ' primary' : '')} onClick={() => setMode('and')}>AND</button>
            <button className={'btn small' + (mode === 'or' ? ' primary' : '')} onClick={() => setMode('or')}>OR</button>
          </div>
          <button className="icon-btn rules-close" onClick={onClose} aria-label="关闭规则">✕</button>
        </div>
        <div className="rule-logic">
          <span className="muted small" title="AND:各维度都要命中(同维度内任一条目即可)。OR:任一条目命中即可。体重/声音按百分位判定。异色/炫彩始终高亮。">
            {mode === 'and' ? '同时满足所选条件' : '任一条件命中'}即高亮，异色/炫彩始终高亮
          </span>
        </div>
        <div className="rule-groups">
          {FIELDS.map((f) => (
            <div className="filter-group" key={f.k}>
              <label>{f.label}</label>
              {f.k === 'species' ? (
                <>
                  <div className="rule-species-add">
                    <input className="input" placeholder="输入种类名，如 鸭吉吉"
                      value={speciesDraft} onChange={(e) => setSpeciesDraft(e.target.value)}
                      onKeyDown={(e) => e.key === 'Enter' && addSpecies()} />
                    <button className="btn primary" onClick={addSpecies}>添加</button>
                  </div>
                  {speciesRules.length > 0 && (
                    <div className="chips">
                      {speciesRules.map((r) => (
                        <span key={r.value} className="chip on" onClick={() => toggleRule('species', r.value)}>{r.value} ✕</span>
                      ))}
                    </div>
                  )}
                </>
              ) : (
                <div className="chips">
                  {paletteFor(f.k).map((v) => (
                    <span key={v} className={'chip' + (hasRule(f.k, v) ? ' on' : '')} onClick={() => toggleRule(f.k, v)}>{v}</span>
                  ))}
                  {paletteFor(f.k).length === 0 && <span className="muted">暂无可选条目</span>}
                </div>
              )}
            </div>
          ))}
        </div>
      </aside>
    </>
  )
}
