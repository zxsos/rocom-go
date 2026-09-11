import React, { useMemo, useState } from 'react'
import { fmtShortTime, pctHot, voiceHot } from '../../utils/format'
import PetPicker from '../../components/PetPicker'
import Dropdown from '../../components/Dropdown'
import { imgURL } from '../../components/icons'
import PetInline from './PetInline'
import {
  flattenNatures, fmtPct, genState, natureOptions, petPickerOption, sameParents, stepDelta, toParent,
} from './pets'

// 手填属性可以落到这一代的哪个角色上。亲本与子代都可能缺(收蛋那一刻它们还没进库),
// 而补齐哪一只由玩家看着办 —— 工具不替他猜该改谁。
const FILL_ROLES = [
  { k: 'mother', label: '母本' },
  { k: 'father', label: '父本' },
  { k: 'child', label: '子代' },
]

// GenerationRow 时间线上的一代。一条记录有两段生命周期,由「有没有子代」区分:
//
//   - 待认领(无子代):破壳时记下的双亲已确定,子代还不知道是谁 —— 只能「认领」一只库里的宠;
//   - 已认领(有子代):这一代已经完整,可以改父本 / 换子代 / 标回交 / 写备注 —— 用来修正记错的那一代。
//
// 两者在时间线上是同一个位置、同一套排布,故共用一个组件(拆开只会让两边的排布慢慢长歪)。
//
// 认领必须走后端(见 api.claimBreedingChild):子代快照要按当前 gamedata 重新取,
// 前端手里没有权威的那份;而「从待认领挪进正式代数」只该有一处实现。
export default function GenerationRow({
  g, pending, prev, prevChild, line, fathers, kids, matrix,
  onEdit, onClaim, onDelete, onPet, busy,
}) {
  // 待处理的那一代究竟在等什么(见 pets.genState):等孵蛋,还是等子代进背包。
  const st = pending ? genState(g) : ''
  // 这颗蛋的当前信息(后端读取时回查蛋表给的投影,见 api_breeding.eggSnapshots)。
  // 查不到 = 蛋已不在背包;有它就能在破壳前看出这颗蛋值不值得孵。
  const egg = pending ? (line.eggs || {})[g.eggGid] : null
  // 孵出的那只已经不在宠物库(放生/送人,或断网期间孵的从没抓到过)—— 后端判的,不猜。
  const lost = pending && (line.lostChildGens || []).includes(g.gen)
  // 记录里的那只也要在选项里(见 withCurrent):它是编辑表单的**当前值**,缺了它下拉会显示成
  // 占位文案,玩家会以为这一代没记着父本 / 子代,顺手一存就把原记录覆盖成空。
  const kidOpts = useMemo(() => withCurrent(kids.map(petPickerOption), g.child), [kids, g.child])
  const fatherOpts = useMemo(() => withCurrent(fathers.map(petPickerOption), g.father), [fathers, g.father])
  const [editing, setEditing] = useState(false)
  const [childGid, setChildGid] = useState('')
  const [fatherGid, setFatherGid] = useState('')
  const [backcross, setBackcross] = useState(!!g.backcross)
  const [note, setNote] = useState(g.note || '')
  // 手填属性:改哪只 + 改它的哪几项。空串 = 不动那一项(与「清空」区分开 ——
  // 见 pets.clampNum 的注释:0 在嗓音上是合法值,拿 0 当「没填」会误改)。
  const [fillWho, setFillWho] = useState('child')
  const [fillNature, setFillNature] = useState('')
  const [fillVoice, setFillVoice] = useState('')
  const [fillWeight, setFillWeight] = useState('')

  // 编辑表单每次打开都从当前记录重新起步:上次改到一半又关掉的内容不该留在下一次编辑里。
  const startEdit = () => {
    setChildGid(g.child ? String(g.child.gid) : '')
    setFatherGid(g.father ? String(g.father.gid) : '')
    setBackcross(!!g.backcross)
    setNote(g.note || '')
    setEditing(true)
  }

  const fatherList = g.father ? [g.father] : (g.fathers || [])
  // 与**上一条**记录双亲相同 → 这一胎的嗓音必然与上一胎一样(见 pets.sameParents)。
  // 待孵的代同样适用:它还没孵,但嗓音此刻已经定了。
  const same = sameParents(g, prev)
  // 串窝:实际采用了其中一只时父本列只画那一只,其余候选退到下面一行小字里 ——
  // 既要看清「到底用了谁」,也不能把「当时还有谁能配」这个信息丢掉(这正是串窝要留档的原因)。
  const otherCandidates = g.father ? (g.fathers || []).filter((f) => f.gid !== g.father.gid) : []
  const deltas = childDeltas(g.child, prevChild, (line && line.goal) || {})

  const save = () => {
    const child = kids.find((p) => String(p.gid) === childGid)
    const father = fathers.find((p) => String(p.gid) === fatherGid)
    const next = { ...g, backcross, note: note.trim() }
    if (child) next.child = toParent(child)
    if (father) next.father = toParent(father)
    // 手填:只改**填了的**那几项,其余保持快照原样 —— 玩家多半只想补一个性格,
    // 不该顺手把嗓音/体重也归零(那两项空着是"没测出来",不是 0)。
    const patch = {}
    if (fillNature) patch.nature = fillNature
    if (fillVoice !== '') patch.voice = Number(fillVoice)
    if (fillWeight !== '') patch.weightPct = Number(fillWeight)
    if (Object.keys(patch).length > 0 && next[fillWho]) {
      next[fillWho] = { ...next[fillWho], ...patch }
    }
    onEdit(next)
    setEditing(false)
  }
  const natures = useMemo(() => flattenNatures(matrix), [matrix])

  return (
    <li className={'br-gen' + (pending ? ' pending' : '') + (g.backcross ? ' backcross' : '')}>
      <div className="br-gen-head">
        <span className="br-gen-no">第 {g.gen} 代</span>
        {g.backcross ? <span className="br-tag bc" title="子代 × 亲本:用来把已经变好的那段基因固定下来">回交</span> : null}
        <span className="br-tag" title={g.source === 'manual' ? '手动补录的一代' : '破壳时自动记下的一代'}>
          {g.source === 'manual' ? '手动' : '自动'}
        </span>
        {(g.fathers || []).length > 1
          ? <span className="br-tag amb" title="串窝:同一时刻有多个父本候选,实际是哪只无从确定">父本 {g.fathers.length} 选 1</span>
          : null}
        {same ? (
          <span className="br-tag same" title={same === 'both'
            ? '与上一代同一对双亲 —— 嗓音是双亲均值的向下取整,故这一胎与上一胎必然一样,再孵只是在掷体重与性格'
            : '与上一代同一只母本(父本未定)—— 嗓音至少不会因母本而变,想推进得换父本'}>
            {same === 'both' ? '同双亲' : '同母本'}
          </span>
        ) : null}
        {/* 待处理的那一代**等的是哪一步**要写在脸上:等孵蛋与等子代进背包是两回事,
            前者可能永远等不到(蛋送人、不孵了),后者只要那只宠一进背包就自动补上。 */}
        {pending ? (
          <span className={'br-tag ' + (st === 'incubating' ? 'wait' : 'claim')}
            title={st === 'incubating'
              ? '已收蛋、还没孵 —— 破壳后这里会自动变成「待认领」'
              : '破壳已确认,子代还没进背包(或那一包漏抓了)—— 可以手动认领一只'}>
            {st === 'incubating' ? '待孵' : '待认领'}
          </span>
        ) : null}
        <span className="br-gen-at muted">{g.at ? fmtShortTime(g.at) : ''}</span>
        <div className="spacer" />
        {pending ? (
          <button className="btn small" disabled={busy} onClick={() => setEditing((v) => !v)}>
            {editing ? '收起' : '认领子代'}
          </button>
        ) : (
          <button className="btn small" disabled={busy} onClick={() => (editing ? setEditing(false) : startEdit())}>
            {editing ? '收起' : '编辑'}
          </button>
        )}
        <button className="btn ghost danger small" disabled={busy}
          title={pending ? '删掉这条待认领记录' : `删掉第 ${g.gen} 代`}
          onClick={() => onDelete(g)}>删除</button>
      </div>

      <div className="br-gen-body">
        <PetInline role="♀" p={g.mother} onPet={onPet} ph="母本缺失" />
        <span className="br-gen-x" aria-hidden="true">×</span>
        <span className="br-gen-fathers">
          {fatherList.length === 0 ? (
            <PetInline role="♂" p={null} ph="父本未知" />
          ) : fatherList.map((f, i) => (
            <PetInline key={f.gid || i} role="♂" p={f} onPet={onPet}
              tag={g.fathers && g.fathers.length > 1 && i === 0
                ? <span className="br-tag amb" title="串窝:这几只都在同一时刻配过,实际是哪只无从确定">多候选</span>
                : null} />
          ))}
        </span>
        <span className="br-gen-arrow" aria-hidden="true">→</span>
        <span className="br-gen-child">
          <PetInline role="子" p={g.child} onPet={onPet} ph="待认领" />
          {deltas.map((d) => (
            <span key={d.k} className={'br-pi-delta ' + d.dir} title={d.title}>{d.text}</span>
          ))}
        </span>
      </div>

      {otherCandidates.length > 0 && (
        <div className="br-gen-cands">
          <span className="muted">当时可配的其它父本候选:</span>
          {otherCandidates.map((f) => (
            <button key={f.gid} type="button" className="br-sug-p m" title="点击查看这只宠的详情"
              disabled={!onPet} onClick={() => onPet && onPet(f.gid)}>
              <em>♂</em>
              <span className="br-sug-n">{f.name}</span>
              <span>V{f.voice}</span>
            </button>
          ))}
        </div>
      )}

      {/* 待孵/待认领各给一句「接下来会发生什么」;更要把**等不到**的情形说穿 ——
          让玩家对着一条永远停着的记录空等,比告诉他真相糟得多。 */}
      {pending && st === 'incubating' && egg ? (
        <div className="br-gen-egg">
          <span className="br-gen-egg-k">这颗蛋</span>
          {/* 体重百分位与嗓音破壳后原样落到子代身上,故现在就能看 —— 不用等孵出来
              才知道这颗蛋值不值得孵。 */}
          {egg.weightPct != null
            ? <span className={pctHot(egg.weightPct) || ''}>W{fmtPct(egg.weightPct)}%</span>
            : null}
          {egg.voice != null
            ? <span className={voiceHot(egg.voice) || ''}>V{egg.voice}</span>
            : null}
          {(egg.medals || []).map((m) => (
            <span key={m.dim} className="br-medal" title={`${m.name} —— 这颗蛋确定能拿到`}>
              {m.icon ? <img className="br-medal-img" src={imgURL(m.icon)} alt="" /> : null}
              {m.name}
            </span>
          ))}
          <span className="muted">破壳后自动补上孵出的那只</span>
        </div>
      ) : null}
      {pending && st === 'incubating' && !egg ? (
        <div className="br-gen-note warn">
          这颗蛋已经不在背包里了(孵掉 / 送人 / 被清理)—— 这一代不会再有子代,
          可以点右边「删除」丢掉它。
        </div>
      ) : null}
      {pending && st === 'claim' ? (
        lost
          ? (
            <div className="br-gen-note warn">
              孵出的 #{g.childGid} 已经不在宠物库里了(放生 / 送人,或是断网期间孵的、
              从没抓到过)—— 认领不了。记得它是谁就手动补录一只,否则丢掉这一代。
            </div>
          )
          : (
            <div className="br-gen-note muted">
              已孵出 #{g.childGid},等它进背包就自动认领;等不及也可以手动指定一只。
            </div>
          )
      ) : null}
      {g.note ? <div className="br-gen-note muted" title={g.note}>{g.note}</div> : null}

      {pending && editing && (
        <div className="br-gen-edit">
          <label className="br-field">
            <span>子代</span>
            <PetPicker value={childGid} options={kidOpts} placeholder="从宠物库里选一只…"
              onChange={setChildGid} />
          </label>
          <p className="br-hint muted">
            认领时后端会按当前数据重新取这只宠的快照(名字 / 嗓音 / 体重百分位 / 性格),
            不采用页面上的旧值。
          </p>
          <div className="br-gen-act">
            <button className="btn small" onClick={() => setEditing(false)}>取消</button>
            <button className="btn primary small" disabled={!childGid || busy}
              onClick={() => { onClaim(g.gen, Number(childGid)); setEditing(false) }}>
              认领为第 {g.gen} 代子代
            </button>
          </div>
        </div>
      )}

      {!pending && editing && (
        <div className="br-gen-edit">
          <label className="br-field">
            <span>子代</span>
            <PetPicker value={childGid} options={kidOpts} placeholder="保留当前子代"
              clearLabel="保留当前子代" onChange={setChildGid} />
          </label>
          <label className="br-field">
            <span>种公(实际采用)</span>
            <PetPicker value={fatherGid} options={fatherOpts}
              placeholder="不改动" onChange={setFatherGid}
              title={(g.fathers || []).length > 1 ? '串窝,实际父本未知' : ''} />
          </label>
          <label className="br-check">
            <input type="checkbox" checked={backcross} onChange={(e) => setBackcross(e.target.checked)} />
            <span>这一代是回交(子代 × 亲本)</span>
          </label>
          <label className="br-field">
            <span>备注</span>
            <input className="input" maxLength={80} placeholder="如:用的是第 2 代那只公的"
              value={note} onChange={(e) => setNote(e.target.value)} />
          </label>
          {/* 手填属性:亲本/子代在收蛋那一刻还没进宠物库时,快照里就没有性格/嗓音/体重。
              能在库里找到它时后端会按 gid 自动补(读取时投影),补不上的(那只宠已经不在
              库里了)只有玩家自己知道,故留一个手填口子 —— 而不是整只换掉。 */}
          <div className="br-fill">
            <span className="br-fill-k">手填属性</span>
            <div className="br-fill-roles">
              {FILL_ROLES.map((r) => (
                <button key={r.k} type="button"
                  className={'chip' + (fillWho === r.k ? ' on' : '')}
                  title={`把下面填的属性改到这一代的${r.label}上`}
                  onClick={() => setFillWho(r.k)}>{r.label}</button>
              ))}
            </div>
            <div className="br-fill-inputs">
              <Dropdown value={fillNature} options={natureOptions(fillNature, natures)}
                placeholder="性格(不改)" onChange={setFillNature} />
              <input className="input br-num" type="number" step="1" min="-100" max="100"
                placeholder="嗓音(不改)" value={fillVoice}
                onChange={(e) => setFillVoice(e.target.value)} />
              <input className="input br-num" type="number" step="0.1" min="0" max="100"
                placeholder="体重%(不改)" value={fillWeight}
                onChange={(e) => setFillWeight(e.target.value)} />
            </div>
            <p className="br-hint muted">
              只改填了的项,留空的不动 —— 嗓音 0 是合法值,故「不改」靠留空表示而不是靠 0。
            </p>
          </div>
          {fathers.length === 0 ? (
            <p className="br-hint muted">
              库里没有可选的种公(要跟母本同蛋组的雄性)—— 先不选也行,这一代会保留原记录里的父本。
            </p>
          ) : null}
          <div className="br-gen-act">
            <button className="btn small" onClick={() => setEditing(false)}>取消</button>
            <button className="btn primary small" disabled={busy} onClick={save}>保存这一代</button>
          </div>
        </div>
      )}
    </li>
  )
}

// withCurrent 把记录里的那一只补进候选列表。它可能已经不在候选池里:被放生或送人(库里没有了),
// 或者蛋组与当前母本对不上。补一个带说明的条目,而不是让它显示成「没选」—— 编辑表单里的空值
// 会被当成「玩家清空了」,一保存就把那段历史冲掉了。
function withCurrent(opts, snap) {
  if (!snap || opts.some((o) => o.value === String(snap.gid))) return opts
  const o = petPickerOption(snap)
  return [{ ...o, sub: [o.sub, '已不在候选里'].filter(Boolean).join(' · ') }, ...opts]
}

// childDeltas 子代相对**上一代子代**的变化:只标「朝目标更近」的那一项。
//
// 方向判据用「离目标更近」而不是「数值更大」:嗓音是双向的(目标 -100 时变小才是改善),
// 只有按距离比才在两种目标下都成立(见 pets.stepDelta)。没定该项目标时退化为「更极端」——
// 那正是没目标时的目标。
function childDeltas(child, prev, goal) {
  if (!child || !prev) return []
  const out = []
  const dv = (child.voice || 0) - (prev.voice || 0)
  const dirV = stepDelta(child.voice, prev.voice, goal.voice, 'voice')
  if (dirV) {
    out.push({
      k: 'v', dir: dirV, text: `V${dv > 0 ? '+' : ''}${dv}`,
      title: `${dirV === 'up' ? '更接近' : '偏离'}目标:嗓音较上一代 ${dv > 0 ? '+' : ''}${dv}`,
    })
  }
  if (child.weightPct != null && prev.weightPct != null) {
    const dw = child.weightPct - prev.weightPct
    const dirW = stepDelta(child.weightPct, prev.weightPct, goal.weightPct, 'weight')
    if (dirW && Math.abs(dw) >= 0.05) {
      out.push({
        k: 'w', dir: dirW, text: `W${dw > 0 ? '+' : ''}${dw.toFixed(1)}pp`,
        title: `${dirW === 'up' ? '更接近' : '偏离'}目标:体重百分位较上一代 ${dw > 0 ? '+' : ''}${dw.toFixed(1)}pp`,
      })
    }
  }
  return out
}
