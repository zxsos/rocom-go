import React, { useEffect, useMemo, useState } from 'react'
import Dropdown from '../../components/Dropdown'
import {
  flattenNatures, goalNatures, goalToInput, hasGoal, natureDimRows, natureOptions, parseGoal, sameGoal,
} from './pets'

// 目标模板:一键把某一维度推到极限,**其余维度保持不动**。
//
// 语义是「开关」而不是「整体替换」:点「极限嗓音 +100」只改嗓音这一项,已经填着的体重 / 性格
// 不会被抹掉。玩家的实际目标常常是组合的(「培育出一只大块头、婉转声的就停」),而整体替换会
// 逼他点完模板再回头把另一项补上 —— 更糟的是他多半以为模板已经带上了那一项,照着跑好几代才发现
// 分数一直是按单维度算的。
//
// key/val 而不是一份 goal 对象:开关要按「这一项现在是不是正好是那个值」来判断(见 tplActive),
// 得知道该改哪一项、改成什么。
export const GOAL_TEMPLATES = [
  { k: 'v-hi', label: '极限嗓音 +100', key: 'voice', val: '100' },
  { k: 'v-lo', label: '极限嗓音 -100', key: 'voice', val: '-100' },
  { k: 'w-hi', label: '极限体重 100%', key: 'weightPct', val: '100' },
  { k: 'w-lo', label: '极限体重 0%', key: 'weightPct', val: '0' },
]

const EMPTY = { voice: '', weightPct: '', nature: '', natureIn: [], gender: '' }

// 目标性别选项:空 = 不限(与后端 BreedingGoal.Gender 的空值同义)。
// 它**不参与建议排序**(子代性别破壳前不可预测、双亲怎么配都一样),只用于判定「达成」。
const GENDERS = [
  { v: '', label: '不限', title: '不挑性别 —— 子代是 ♂ 是 ♀ 都算达标' },
  { v: '♂', label: '♂', title: '雄性' },
  { v: '♀', label: '♀', title: '雌性' },
]

// tplActive 模板是否处于激活态。由输入框里的**当前值**纯派生,而不是另存一份「点过哪个模板」:
// 值可以直接被手输改掉(把 100 改成 95),高亮必须跟着掉 —— 存一份状态就会出现「高亮着,
// 但值并不是它标的那个」,而玩家会按高亮去理解当前目标。
export function tplActive(t, value) {
  const cur = value ? value[t.key] : ''
  return String(cur == null ? '' : cur).trim() === t.val
}

// applyTemplate 点模板 = 只改它对应的那一项:已经是目标值就取消(置空),否则设为该值。
// 其余项原样展开保留(见 GOAL_TEMPLATES 的注释)。
export function applyTemplate(t, value) {
  return { ...value, [t.key]: tplActive(t, value) ? '' : t.val }
}

// GoalFields 三项输入 + 性格维度 + 模板按钮。**受控于调用方的字符串 state**:
//
// 值保持字符串而不是 number —— 数字 input 清空时值会变成 NaN/0,受控于 number 会让
// 「删光再输」跳到 0(见 pets.parseGoal 的注释)。空串在这里精确地表示「这一项不关心」。
//
// 新建线的表单与已有线的「改目标」共用它:两者要填的东西完全一样,拆开写只会让
// 校验与模板在其中一处慢慢缺失。
//
// 性格有两种填法,**互斥**:
//   1. 确切一个性格名(下拉)—— 「就要固执」;
//   2. 「性格正面加某维」(一排 chip)—— 「只要加物攻,是这 5 个里的哪个都行」。
// 选后者时存的是该维那 5 个名字(natureIn),不存维度编号(见 pets.goalNatures 的注释)。
export function GoalFields({ value, onChange, matrix }) {
  const set = (k, v) => onChange({ ...value, [k]: v })
  const natures = useMemo(() => flattenNatures(matrix), [matrix])
  const dims = useMemo(() => natureDimRows(matrix), [matrix])
  const cur = useMemo(() => goalNatures(value), [value])

  // 当前选的是哪一维:只有「一组名字」才可能是维度目标 —— 单个名字走上面的精确下拉
  // (它更明确,不该同时点亮某个维度)。方阵对不上时(旧值/游戏改了表)返回 0,不误点亮。
  const activeDim = useMemo(() => {
    if (cur.length < 2) return 0
    const hit = dims.find((r) => r.names.length === cur.length && r.names.every((n) => cur.includes(n)))
    return hit ? hit.dim : 0
  }, [cur, dims])

  // 再点一次当前维度 = 取消(与上面的模板 chip 同一种开关手感)。
  const setDim = (row) => onChange(activeDim === row.dim
    ? { ...value, natureIn: [] }
    : { ...value, nature: '', natureIn: row.names.slice() })

  return (
    <>
      <div className="br-goal-inputs">
        <label className="br-field">
          <span>目标嗓音</span>
          <input className="input br-num" type="number" step="1" min="-100" max="100"
            placeholder="不关心" value={value.voice}
            onChange={(e) => set('voice', e.target.value)} />
        </label>
        <label className="br-field">
          <span>目标体重百分位</span>
          <input className="input br-num" type="number" step="0.1" min="0" max="100"
            placeholder="不关心" value={value.weightPct}
            onChange={(e) => set('weightPct', e.target.value)} />
        </label>
        <label className="br-field">
          <span>目标性格</span>
          {/* 单选必须是**确切的性格名**(后端靠它匹配双亲),故用自绘下拉而不是自由输入。
              此前这里把 6×6 方阵当字符串数组铺进 datalist,选中后存进去的是拼接串。
              选了具体名字会清掉维度那一组(两者互斥,见上面的说明)。 */}
          <Dropdown value={value.nature} options={natureOptions(value.nature, natures)}
            placeholder={activeDim ? '已按维度选' : '不关心'}
            onChange={(v) => onChange({ ...value, nature: v, natureIn: [] })} />
        </label>
      </div>
      {/* 「正面加某维」与精灵列表的性格筛选是同一套口径(那边点行头也是把该维的 5 个名字铺进
          筛选),但那里是 6×6 全矩阵(要精确到「+物攻 −魔攻」的那一个,还要能多选),
          这里只关心正面那一维,故只留一排开关。 */}
      {dims.length ? (
        <div className="br-natdims">
          <span className="br-natdims-k">性格正面加</span>
          {dims.map((r) => (
            <button key={r.dim} type="button"
              className={'chip br-natdim' + (activeDim === r.dim ? ' on' : '')}
              title={`不挑性格名:${r.label} +10% 的这 ${r.names.length} 个都算达标 —— ${r.names.join(' / ')}`}
              onClick={() => setDim(r)}>+{r.label}</button>
          ))}
        </div>
      ) : null}
      {/* 目标性别:它与 V/W/性格不同 —— 破壳前完全不可预测,故只是一个「达成」条件,
          不会影响建议怎么排(见 pets.hasGenderGoal 与后端 BreedingGoal.Gender)。 */}
      <div className="br-gender">
        <span className="br-gender-k">目标性别</span>
        {GENDERS.map((gd) => (
          <button key={gd.label} type="button"
            className={'chip' + (String(value.gender || '') === gd.v ? ' on' : '')}
            title={gd.title}
            onClick={() => set('gender', gd.v)}>{gd.label}</button>
        ))}
      </div>
      <div className="br-goal-tpl">
        {GOAL_TEMPLATES.map((t) => (
          <button key={t.k} type="button"
            className={'chip' + (tplActive(t, value) ? ' on' : '')}
            title="点一下把这一项推到极限,再点一下取消;其它项保持不动"
            onClick={() => onChange(applyTemplate(t, value))}>{t.label}</button>
        ))}
        <button type="button" className="chip" onClick={() => onChange(EMPTY)}>清空目标</button>
      </div>
    </>
  )
}

// GoalEditor 详情页顶部的目标编辑区:改完点保存才提交(与后端「整条线覆盖写」一致)。
//
// 为什么不让输入即保存:目标是评分口径,改一个字符就会让建议整份重排。实时保存的话,
// 玩家删掉一位数字的瞬间(如把 100 改成 95 的中间态「1」)就会把建议打乱一次 ——
// 而且那一次是发到服务端的真改动。故这里只在明确点保存时提交。
export default function GoalEditor({ line, natureMatrix, onSave, busy }) {
  const [val, setVal] = useState(() => goalToInput(line.goal))
  // 只在**这条线的服务端版本**变了才回灌输入框。依赖 line.updatedAt 而不是 line.goal:
  // 每次推送重拉都会产生一个全新的 goal 对象(内容可能一字未改),拿它当依赖会在玩家
  // 打字打到一半把输入框清回旧值 —— 改目标最恼人的形态。
  useEffect(() => { setVal(goalToInput(line.goal)) }, [line.id, line.updatedAt])

  const parsed = parseGoal(val)
  const dirty = !sameGoal(parsed, line.goal)

  return (
    <section className="br-panel br-goal">
      <div className="br-sec-head">
        <h3>培育目标</h3>
        <span className="muted">
          {hasGoal(parsed) ? '只对填了的项计分,留空即不关心' : '三项都空 —— 建议会退化成「随便挑两只」'}
        </span>
      </div>
      <GoalFields value={val} onChange={setVal} matrix={natureMatrix} />
      {/* 自动标记的口径写在**填目标的地方**:「已达成」是后端在写入记录时自己打上的,
          玩家看到状态跳变得知道是谁改的、以及怎么改回去(见 pet.ReachGoal / AutoDoneOnReach)。 */}
      <p className="br-hint muted">
        各项可以任意组合,只想培育一项就只填那一项。<b>达标按方向算</b>:目标偏高
        (嗓音 &gt; 0、体重 &gt; 50%)时「达到或超过」就算,目标偏低时「达到或低于」就算,
        正中间按精确相等。性格可以只要求<b>正面加某一维</b>(那 5 个性格都算达标),也可以点名
        一个确切性格;<b>性别</b>破壳前不可预测,只用于判定达成(不参与建议排序)。
        填了的项被<b>某一代的同一只子代</b>全部满足时,这条线会自动标成「已达成」
        (可以手动改回「进行中」接着刷)。
      </p>
      <div className="br-goal-act">
        <button className="btn primary small" disabled={!dirty || busy} onClick={() => onSave(parsed)}>
          {busy ? '保存中…' : '保存目标'}
        </button>
        {dirty
          ? <span className="muted">有改动未保存 · 保存后选配建议会按新目标重算</span>
          : <span className="muted">改完点右侧按钮 —— 目标决定建议怎么排序</span>}
      </div>
    </section>
  )
}
