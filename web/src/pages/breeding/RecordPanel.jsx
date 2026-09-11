import React, { useMemo, useState } from 'react'
import PetPicker from '../../components/PetPicker'
import { fatherCandidates, nextGen, petPickerOption, toParent } from './pets'

// RecordPanel 手动补录一代。
//
// 为什么需要它:自动记录只在「破壳回包 + 子代入库」两条消息都抓到、且蛋上留有双亲快照时才成立
// (见 pipeline/breeding.go)。漏掉其一(掉线、非家园蛋、很久以前孵的)就永远缺那一代 ——
// 而培育史的价值恰恰在连续性,故必须能手填与修正。
//
// 提交的是**快照**而不是 gid 引用(与后端 ParentSnapshot 同构):亲本之后被放生/送人
// 不该影响这条历史 —— 与 store 侧「双亲/子代都存快照」的取舍是同一件事。
export default function RecordPanel({ line, mothers, fathers, kids, onAdd, busy }) {
  const [open, setOpen] = useState(false)
  // 默认就是这条线定的那对亲本(见 pet.BreedingLine 的 MotherGid / FatherGid):补录最常见的情形
  // 正是「这对生下的下一代」,预置好之后玩家只需要填子代。清掉或换掉都随他。
  // gid 优先取快照里的,没有快照(那位已不在库)时退回线上存的 gid —— 不在候选里时 PetPicker
  // 自然显示为空,不必为它特判。
  const defMother = String((line.mother && line.mother.gid) || line.motherGid || '')
  const defFather = String((line.father && line.father.gid) || line.fatherGid || '')
  const [motherGid, setMotherGid] = useState(defMother)
  const [fatherGid, setFatherGid] = useState(defFather)
  const [childGid, setChildGid] = useState('')
  const [backcross, setBackcross] = useState(false)
  const [note, setNote] = useState('')
  const [err, setErr] = useState('')

  const mother = useMemo(
    () => mothers.find((p) => String(p.gid) === motherGid) || null,
    [mothers, motherGid],
  )
  // 父本候选按母本蛋组过滤:母本没选时不筛(筛了就没有可选项,玩家根本填不下去)。
  const fCands = useMemo(() => fatherCandidates(fathers, mother), [fathers, mother])
  // 三个队列各转一次成选择器的选项(名字给过滤、数值压成副文本);候选池按品种缓存,
  // 这三份转换在展开补录面板期间不会因为别的状态变化而重算。
  const motherOpts = useMemo(() => mothers.map(petPickerOption), [mothers])
  const fatherOpts = useMemo(() => fCands.map(petPickerOption), [fCands])
  const kidOpts = useMemo(() => kids.map(petPickerOption), [kids])

  // reset 回到**这对默认亲本**而不是清空:清空的话「默认选中」只管到第一次提交,补录第二、三代时
  // 又得重新挑一次母本 —— 而这是同一条线上的同一对。
  const reset = () => {
    setMotherGid(defMother); setFatherGid(defFather); setChildGid('')
    setBackcross(false); setNote(''); setErr('')
  }

  const submit = async () => {
    if (!mother) {
      setErr('先选一只种母 —— 蛋的物种随母本,没有她就定不了这条线的品种')
      return
    }
    const child = kids.find((p) => String(p.gid) === childGid)
    if (!child) {
      setErr('子代必填:这一代的价值就在子代记下的嗓音 / 体重百分位 / 性格')
      return
    }
    const father = fCands.find((p) => String(p.gid) === fatherGid)
    setErr('')
    // 只有提交成功才清空:失败(如服务端拒绝)时把玩家刚填的一整代留着,改一改就能重试。
    const ok = await onAdd({
      gen: nextGen(line),
      mother: toParent(mother),
      ...(father ? { father: toParent(father) } : {}),
      child: toParent(child),
      ...(backcross ? { backcross: true } : {}),
      source: 'manual',
      at: Math.floor(Date.now() / 1000),
      ...(note.trim() ? { note: note.trim() } : {}),
    })
    if (ok !== false) reset()
  }

  if (!open) {
    return (
      <section className="br-panel br-record">
        <button className="btn small" onClick={() => setOpen(true)}>手动补录一代</button>
        <span className="muted">
          自动记录漏掉的那一代(掉线 / 非家园蛋 / 很久以前孵的)在这里补上
        </span>
      </section>
    )
  }

  return (
    <section className="br-panel br-record">
      <div className="br-sec-head">
        <h3>手动补录第 {nextGen(line)} 代</h3>
        <div className="spacer" />
        <button className="btn ghost small" onClick={() => { reset(); setOpen(false) }}>收起</button>
      </div>

      <div className="br-rec-form">
        <label className="br-field">
          <span>种母</span>
          <PetPicker value={motherGid} options={motherOpts} placeholder="选择一只种母…"
            onChange={(gid) => { setMotherGid(gid); setFatherGid('') }} />
        </label>
        <label className="br-field">
          <span>种公</span>
          <PetPicker value={fatherGid} options={fatherOpts} placeholder="未知 / 不填"
            clearLabel="未知 / 不填" onChange={setFatherGid} />
        </label>
        <label className="br-field">
          <span>子代</span>
          <PetPicker value={childGid} options={kidOpts} placeholder="选择一只…"
            onChange={setChildGid} />
        </label>
        <label className="br-field">
          <span>备注</span>
          <input className="input" maxLength={80} placeholder="选填,如:凭记忆补的" value={note}
            onChange={(e) => setNote(e.target.value)} />
        </label>
      </div>

      <label className="br-check">
        <input type="checkbox" checked={backcross} onChange={(e) => setBackcross(e.target.checked)} />
        <span>这一代是回交(子代 × 亲本)</span>
      </label>

      {err ? <p className="br-error">{err}</p> : null}
      <p className="br-hint muted">
        候选来自整个宠物库:{mothers.length} 只同品种种母 · {fCands.length} 只可配种公 · {kids.length} 只子代。
        每个框都可以输入名字搜索。
      </p>

      <div className="br-gen-act">
        <button className="btn primary small" disabled={busy} onClick={submit}>补录这一代</button>
      </div>
    </section>
  )
}
