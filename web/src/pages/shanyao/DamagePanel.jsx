import React, { useMemo, useState } from 'react'
import { estimate, petLabel, STEP_LABELS, STAT_KEYS, STAT_LABELS, statsFromPayload, typeMultiplier } from './calc'

// 伤害预估面板(单向:我方 → 对手)。
//
// 三块:选人/选技能 → 结果区(伤害 / 占生命% / 剩余生命)+ 可折叠四行算式卡 → 输入区
// (能力等级 ±档、减伤、最终倍率、追加伤害)。最后是上期就有的「数据校对」区。
//
// 用原生 <select> / <input> 而非自绘控件:键盘操作、移动端弹层与无障碍语义都是白拿的。
export default function DamagePanel({ self, foe, selection, onPick, rules }) {
  const [cond, setCond] = useState({ attackStage: 0, defenseStage: 0, reduction: 0, finalMultiplier: 100, extraDamage: 0 })
  const [openFormula, setOpenFormula] = useState(false)

  const all = useMemo(() => {
    const list = []
    for (const p of (self?.pets || [])) list.push({ key: 'self:' + (p.gid || 'p' + p.petId), pet: p, side: 'self' })
    for (const p of (foe?.pets || [])) list.push({ key: 'foe:' + (p.gid || 'p' + p.petId), pet: p, side: 'foe' })
    return list
  }, [self, foe])

  const atk = all.find((x) => x.key === selection.atkKey)
  const def = all.find((x) => x.key === selection.defKey)
  const skills = (atk?.pet.skills || []).slice(0, 4)
  const skill = skills.find((s) => String(s.id) === selection.skillId)

  const res = useMemo(
    () => estimate({
      attacker: atk?.pet,
      defender: def?.pet,
      skill,
      rules,
      conditions: {
        level: 60,
        attackStage: Number(cond.attackStage) || 0,
        defenseStage: Number(cond.defenseStage) || 0,
        // 面板按百分比输入(如「减伤 80」),换算成倍率 0.2;最终倍率同理(100 → 1.0)。
        reduction: 1 - Math.min(100, Math.max(0, Number(cond.reduction) || 0)) / 100,
        finalMultiplier: Math.max(0, Number(cond.finalMultiplier) || 0) / 100,
        extraDamage: Math.max(0, Number(cond.extraDamage) || 0),
      },
    }),
    [atk, def, skill, rules, cond],
  )

  const set = (k) => (e) => setCond((c) => ({ ...c, [k]: e.target.value }))

  return (
    <section className="sy-panel">
      <div className="sy-panel-head">
        <h3 className="sy-panel-t">伤害预估</h3>
        <span className="muted">我方 → 对手</span>
      </div>

      <div className="sy-pick">
        <label className="sy-field">
          <span className="sy-field-t">攻方</span>
          <select value={selection.atkKey || ''} onChange={(e) => onPick({ atkKey: e.target.value })}>
            <option value="">未选择</option>
            {all.map((x) => (
              <option key={x.key} value={x.key}>
                {x.side === 'self' ? '我方 ' : '对手 '}
                {petLabel(x.pet)}
              </option>
            ))}
          </select>
        </label>

        <label className="sy-field">
          <span className="sy-field-t">守方</span>
          <select value={selection.defKey || ''} onChange={(e) => onPick({ defKey: e.target.value })}>
            <option value="">未选择</option>
            {all.map((x) => (
              <option key={x.key} value={x.key}>
                {x.side === 'self' ? '我方 ' : '对手 '}
                {petLabel(x.pet)}
              </option>
            ))}
          </select>
        </label>

        <label className="sy-field">
          <span className="sy-field-t">技能</span>
          <select
            value={selection.skillId || ''}
            onChange={(e) => onPick({ skillId: e.target.value })}
            disabled={!skills.length}
          >
            <option value="">{skills.length ? '未选择' : '攻方技能未下发'}</option>
            {skills.map((s) => (
              <option key={s.id} value={String(s.id)}>
                {s.name || '技能 ' + s.id}
              </option>
            ))}
          </select>
        </label>
      </div>

      <Result res={res} />

      <button
        type="button"
        className={'sy-formula-toggle' + (openFormula ? ' on' : '')}
        onClick={() => setOpenFormula((v) => !v)}
        aria-expanded={openFormula}
      >
        四行算式{openFormula ? '（收起）' : '（展开核对）'}
      </button>
      {openFormula && <Formula res={res} />}

      <div className="sy-inputs">
        <StageInput label="攻方能力等级" value={cond.attackStage} onChange={set('attackStage')} />
        <StageInput label="守方能力等级" value={cond.defenseStage} onChange={set('defenseStage')} />
        <NumInput label="减伤" suffix="%" value={cond.reduction} onChange={set('reduction')} step={5} />
        <NumInput label="最终倍率" suffix="%" value={cond.finalMultiplier} onChange={set('finalMultiplier')} step={5} />
        <NumInput label="追加伤害" value={cond.extraDamage} onChange={set('extraDamage')} step={5} />
      </div>
      <p className="sy-note">
        减伤按「减伤 80%」填 80;最终倍率 100 = 不加成。能力等级每档 ±10%,攻/守档位合成一个系数。
        公式口径对齐
        {' '}
        <a href="https://github.com/Evenstar-tools/roco-calculator" target="_blank" rel="noreferrer">roco-calculator</a>
        ;特性、印记、天气与动态威力之外的规则本期不参与计算。
      </p>

      <Audit attacker={atk?.pet} defender={def?.pet} skill={skill} rules={rules} res={res} />
    </section>
  )
}

// Result 是结果区:伤害 / 占守方生命% / 剩余生命。数字用 tabular-nums 防跳动。
function Result({ res }) {
  if (!res.ok) {
    return (
      <div className="sy-result empty">
        {res.missing?.length
          ? `缺少：${res.missing.join('、')} —— 不能精确计算时不给估算值`
          : '选择攻方、守方与技能后显示预估伤害'}
      </div>
    )
  }
  const pct = res.hpPct == null ? null : res.hpPct.toFixed(1)
  return (
    <div className="sy-result">
      <div className="sy-result-item">
        <span className="sy-result-k">预估伤害</span>
        <span className="sy-result-v gold">{res.damage}</span>
      </div>
      <div className="sy-result-item">
        <span className="sy-result-k">占守方生命</span>
        <span className="sy-result-v">{pct == null ? '—' : pct + '%'}</span>
      </div>
      <div className="sy-result-item">
        <span className="sy-result-k">剩余生命</span>
        <span className="sy-result-v">{res.remainHp == null ? '—' : res.remainHp}</span>
      </div>
    </div>
  )
}

// Formula 是四行算式卡:与结果区**同一个 res**,保证两者数值必然一致。
function Formula({ res }) {
  if (!res.ok) {
    return <div className="sy-formula muted">算式需要完整输入后才可生成。</div>
  }
  return (
    <div className="sy-formula">
      {res.steps.map((st) => (
        <div key={st.label} className="sy-formula-row">
          <span className="sy-formula-l">{st.label}</span>
          <span className="sy-formula-e">{st.expr ?? '—'}</span>
          <span className="sy-formula-v">{st.value ?? '—'}</span>
        </div>
      ))}
      <div className="sy-formula-tip">
        {STEP_LABELS.join(' → ')};取整只写「四舍五入 / 向下取整」,`×1` 的乘区已省略。
      </div>
    </div>
  )
}

// StageInput 能力等级:− / 数字 / ＋,档位夹在 ±99。
function StageInput({ label, value, onChange }) {
  const n = Number(value) || 0
  const bump = (d) => onChange({ target: { value: Math.min(99, Math.max(-99, n + d)) } })
  return (
    <div className="sy-field">
      <span className="sy-field-t">{label}</span>
      <div className="sy-stage">
        <button type="button" onClick={() => bump(-1)} aria-label={`${label}降低一级`}>−</button>
        <input type="number" value={value} onChange={onChange} min={-99} max={99} step={1} />
        <button type="button" onClick={() => bump(1)} aria-label={`${label}提高一级`}>＋</button>
      </div>
    </div>
  )
}

function NumInput({ label, suffix = '', value, onChange, step = 1, min = 0 }) {
  return (
    <label className="sy-field">
      <span className="sy-field-t">{label}{suffix ? `（${suffix}）` : ''}</span>
      <div className="sy-num">
        <input type="number" value={value} onChange={onChange} step={step} min={min} />
        {suffix ? <span className="sy-num-u">{suffix}</span> : null}
      </div>
    </label>
  )
}

// 需要提醒的来源:红色 = 缺/未识别/未支持,金色 = 推算或假定(能用但不是实测值)。
const BAD_SRC = new Set(['缺数据', '未识别', '未支持', '未实现'])
const EST_SRC = new Set(['推算（静态种族值）', '假定未触发', '部分未知'])

// Audit 是「数据校对」区:把每一类输入数据的**来源**摊开。
function Audit({ attacker, defender, skill, rules, res }) {
  const rows = []
  for (const [who, pet] of [['攻方', attacker], ['守方', defender]]) {
    if (!pet) continue
    const st = statsFromPayload(pet)
    const src = st ? (st.estimated ? '推算（静态种族值）' : '协议精确值') : '缺数据'
    rows.push({ k: `${who}六维`, v: st ? STAT_KEYS.map((kind) => `${STAT_LABELS[kind]} ${Math.round(st.values[kind])}`).join(' / ') : '—', src })
  }
  if (skill) {
    rows.push({
      k: '技能威力',
      v: (skill.name || skill.id) + (skill.power != null ? ` · 威力 ${skill.power}` : ' · 无威力'),
      src: skill.power == null ? '未收录' : skill.source === 'battle' ? '对局内实时' : skill.source === 'static' ? '静态表' : '未收录',
    })
    const mult = typeMultiplier(skill.type, defender?.damNames, rules)
    rows.push({ k: '克制倍率', v: mult == null ? '—' : String(mult), src: rules ? '规则表' : '缺数据' })
    if (skill.ruleId) {
      rows.push({ k: '动态威力', v: skill.ruleId, src: res?.ok ? '已按规则重算' : (res?.missing || []).join('、') || '规则' })
    }
    if (attacker?.energy != null) rows.push({ k: '当前能量', v: String(attacker.energy), src: '协议' })
  }

  // 印记:层数来自协议 buff,名字由后端按 calc_mark_ids 翻译;认不出的显示原始 id。
  for (const [who, pet] of [['攻方', attacker], ['守方', defender]]) {
    for (const b of pet?.buffs || []) {
      rows.push({
        k: `${who}印记`,
        v: `${b.name ? `${b.name} ×${b.stacks}` : `buff #${b.id} ×${b.stacks}`}`,
        src: b.name ? '协议' : '未识别',
      })
    }
  }
  // 特性:按名字匹配规则表,未实现的/条件不满足的都要说清。
  for (const [who, pet] of [['攻方', attacker], ['守方', defender]]) {
    if (!pet?.trait) continue
    const known = (rules?.traits || []).find((t) => t.name === pet.trait)
    rows.push({
      k: `${who}特性`,
      v: pet.trait,
      src: !known ? '未支持' : known.implemented ? '已实现' : '未实现',
    })
  }
  // 触发条件:自动推断的结果摆出来,「假定未触发」的必须让用户看见。
  const trig = res?.ctx?.triggerContext
  if (trig) {
    rows.push({
      k: '触发条件',
      v: `首回合 ${trig.firstTurn === undefined ? '未知' : trig.firstTurn ? '是' : '否'} / 先手 ${trig.faster === undefined ? '未知' : trig.faster ? '是' : '否'}`,
      src: trig.firstTurn === undefined || trig.faster === undefined ? '部分未知' : '已推断',
    })
  }
  for (const t of [res?.ctx?.traits?.attacker, res?.ctx?.traits?.defender]) {
    if (t?.note) rows.push({ k: '特性说明', v: t.note, src: '假定未触发' })
  }
  if (!rows.length) return null
  return (
    <div className="sy-audit">
      <div className="sy-audit-t">数据校对</div>
      <table className="sy-audit-tb">
        <tbody>
          {rows.map((r) => (
            <tr key={r.k}>
              <th>{r.k}</th>
              <td className="sy-audit-v">{r.v}</td>
              <td className={'sy-audit-src' + (BAD_SRC.has(r.src) ? ' bad' : EST_SRC.has(r.src) ? ' est' : '')}>{r.src}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
