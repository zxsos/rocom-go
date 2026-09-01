// 标注模式(众包图鉴)的前端支撑:
//   - AnnotationsProvider:App 层挂载,拉取全服已审核标注(kind -> code -> {name,desc}),
//     供各页把协议里查不到名字的技能/特性 id 显示成名字;
//   - AnnotationModal:对某个未知 id 搜索候选词典、选中并提交(进入待审,管理员审核后全服可见);
//   - UnknownChip:未知 id 的统一展示块(id + 「标注」按钮,点击打开 AnnotationModal)。
//
// 数据流:玩家提交 → 管理员在 #/admin 审核 → 审核通过后 GET /api/annotations 全服下发,
// 刷新页面即见。提交的标注在通过前只有管理员能看到(展示侧不显示 pending)。
//
// 三类标注对象(kind):
//   skill   技能 id(7xxxxx)→ 技能名,候选是 skills.json 全量目录;
//   feature 特性 id(288xxx)→ 特性名,候选是 wiki 特性词典;
//   event   草系试炼的 event_conf_id → 对应哪只精灵,候选是全部精灵形态。
// 前两类是「协议给了 id、缺名字」;第三类反过来 —— 协议连对象都没给(事件到精灵
// 的映射表在游戏配置里,未解包),只能照着游戏画面标。
//
// ⚠️ ANNOTATION_KINDS 是 kind 的**唯一登记处**,其余地方一律从这里派生。
// 这不是洁癖:新增 event 那一类时,前端有四处各写各的列表 ——
// 提交弹窗的候选类型、Provider 的缓存键、审核面板的切换按钮、
// 以及「全部」时实际去拉的那份数组。漏掉任何一处的**症状都是静默的**:
// 玩家提交成功、数据进了库,但面板永远显示「暂无待审核」,
// 或者标注了页面却读不到缓存 —— 没有任何报错指向真正的原因。
import React, { useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react'
import { getAnnotations, getAnnotationCandidates, submitAnnotation, subscribe } from '../api'
import { AnnotationsContext } from '../context'
import { toast } from './toast'

export const ANNOTATION_KINDS = [
  { key: 'skill', label: '技能', codeLabel: 'id' },
  { key: 'feature', label: '特性', codeLabel: 'id' },
  // event 的 code 是 event_conf_id(事件编号)而非精灵 id —— 写「id 130056」
  // 会把两件事说反,审核者会以为是哪个 id 对不上。
  { key: 'event', label: '精灵', codeLabel: '事件' },
]
const ANNOTATION_KIND_KEYS = ANNOTATION_KINDS.map((k) => k.key)
const KIND_LABEL = Object.fromEntries(ANNOTATION_KINDS.map((k) => [k.key, k.label]))
const CODE_LABEL = Object.fromEntries(ANNOTATION_KINDS.map((k) => [k.key, k.codeLabel]))

// AnnotationsProvider 拉取两类已审核标注并缓存。
//
// 刷新时机有两条,缺一条都会「标注了却没变化」:
//   1. 挂载时拉一次(基线);
//   2. 订阅 SSE 的 annotations 事件 —— 管理员审核通过后后端广播,所有在线页面
//      重拉。没有这条,审完必须手动刷浏览器才看得到,提交的人得不到反馈,
//      下一个遇到同一 id 的人又会再标一次。
export default function AnnotationsProvider({ children }) {
  // 缓存按 kind 分格,初始每格空对象 —— 从 ANNOTATION_KIND_KEYS 派生,
  // 免得新增 kind 时这里漏一格、查缓存恒为 undefined。
  const [byKind, setByKind] = useState(
    () => Object.fromEntries(ANNOTATION_KIND_KEYS.map((k) => [k, {}])),
  )
  // 自己提交、还没通过审核的标注。只在本会话内可见(别人看不到),用来让
  // 「我刚标了」立刻有回显 —— 否则玩家提交后页面纹丝不动,只会以为没生效。
  const [mine, setMine] = useState({})

  // 返回最新拉取到的 {kind: {code: {name,desc}}},供调用方据此清理本地回显
  // (不能读 byKind 状态:那是上一次渲染的值,refresh 之后还没更新)。
  const refresh = useCallback(async () => {
    try {
      const pairs = await Promise.all(ANNOTATION_KIND_KEYS.map(async (kind) => {
        const d = await getAnnotations(kind)
        const m = {}
        for (const it of (d.items || [])) m[it.code] = { name: it.name, desc: it.desc }
        return [kind, m]
      }))
      const fresh = Object.fromEntries(pairs)
      setByKind(fresh)
      return fresh
    } catch {
      return null // 拉取失败时维持旧缓存,页面照常展示 id
    }
  }, [])

  useEffect(() => { refresh() }, [refresh])

  // 审核通过/拒绝都会广播:
  //   - 通过的:重拉,别人刚标的名字已生效;
  //   - 同时清掉已进入正式列表的本地回显 —— 那条已经由后端下发,不需要再靠
  //     mine 顶着,否则它会一直带着「待审」标记(永远显示成未核实的状态)。
  //     被管理员**拒绝**的条目不在正式列表里,故不会被剔除:它仍以「待审」
  //     样式显示,这点是对的 —— 提交者应当知道自己标的被驳回了,而不是页面
  //     悄无声息地变回裸 id(那会让人以为没提交成功)。
  useEffect(() => subscribe('annotations', async () => {
    const fresh = await refresh()
    if (!fresh) return
    setMine((prev) => {
      const next = {}
      let dropped = 0
      for (const [key, v] of Object.entries(prev)) {
        const [kind, code] = key.split(':')
        if ((fresh[kind] || {})[code]) dropped++
        else next[key] = v
      }
      return dropped === 0 ? prev : next
    })
  }), [refresh])

  const value = useMemo(() => ({
    // lookup 查标注:优先已审核(全服共享),其次自己提交待审的(仅本会话可见)。
    // 命中返回 {name,desc,pending?};未命中返回 null。
    lookup: (kind, code) => {
      const approved = (byKind[kind] || {})[code]
      if (approved) return approved
      const own = mine[kind + ':' + code]
      return own ? { ...own, pending: true } : null
    },
    // addMine 记录自己刚提交的标注,让本会话立即回显(见上)。
    addMine: (kind, code, name, desc) => {
      setMine((prev) => ({ ...prev, [kind + ':' + code]: { name, desc } }))
    },
    refresh,
  }), [byKind, mine, refresh])

  return <AnnotationsContext.Provider value={value}>{children}</AnnotationsContext.Provider>
}

// useAnnotations 供页面消费标注缓存。
export function useAnnotations() {
  return useContext(AnnotationsContext)
}

// AnnotationModal 标注弹窗。
//   kind: 'skill' | 'feature' | 'event'   code: 协议里的未知 id
// 交互:输入框过滤候选词典 → 点击候选填名(与描述)→ 提交。提交后进入待审,
// 提示「已提交,等待管理员审核」并关闭。候选词典来自后端
// GET /api/annotation-candidates(skill=全量技能目录,feature=wiki 特性词典,
// event=全部精灵形态)。
export function AnnotationModal({ kind, code, onClose }) {
  const { refresh, addMine } = useAnnotations()
  const [candidates, setCandidates] = useState(null) // null = 加载中
  const [q, setQ] = useState('')
  const [name, setName] = useState('')
  const [desc, setDesc] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [err, setErr] = useState('')
  const inputRef = useRef(null)

  useEffect(() => {
    let alive = true
    getAnnotationCandidates(kind).then((d) => { if (alive) setCandidates(d.items || []) })
      .catch(() => { if (alive) setCandidates([]) })
    return () => { alive = false }
  }, [kind])
  useEffect(() => { if (inputRef.current) inputRef.current.focus() }, [])

  // 过滤候选:名字/描述含搜索词(忽略大小写);特性候选还按描述搜。
  //
  // **比较时忽略空格**:wiki 为排版在字间插了空格,玩家照着搜「魔 法 增 效」或反过来
  // 搜「魔法增效」,都该命中同一条候选。两边都去掉空白再比,就不必要求玩家猜中
  // 数据里到底有没有空格(后端提交时也会清洗,见 cleanAnnotationName)。
  const filtered = useMemo(() => {
    const strip = (s) => (s || '').replace(/\s+/g, '')
    const kw = strip(q).toLowerCase()
    if (!candidates) return []
    if (!kw) return candidates.slice(0, 50)
    return candidates.filter((c) =>
      strip(c.name).toLowerCase().includes(kw) ||
      strip(c.desc).toLowerCase().includes(kw),
    ).slice(0, 50)
  }, [candidates, q])

  const pick = (c) => {
    setName(c.name)
    setDesc((c.desc || '').slice(0, 500))
    setErr('')
  }

  const submit = async () => {
    if (!name.trim()) { setErr('请先从候选里选择一个名字'); return }
    setSubmitting(true)
    setErr('')
    try {
      await submitAnnotation(kind, code, name.trim(), desc.trim())
      // 立刻在本会话回显(带「待审」标记):提交后页面纹丝不动会让人以为没生效,
      // 而等管理员审核可能要几小时。其他人要等审核通过才看得到。
      addMine(kind, code, name.trim(), desc.trim())
      toast('已提交,等待管理员审核(你这边已先显示)')
      refresh()
      onClose()
    } catch (e) {
      setErr(e.message || '提交失败')
      setSubmitting(false)
    }
  }

  const kindLabel = KIND_LABEL[kind] || kind
  // event 那一类标的是「事件 → 精灵」,code 是 event_conf_id 而不是精灵 id,
  // 标题里写「精灵 id 130056」会把两件事说反 —— 分开措辞。
  const title = kind === 'event'
    ? `标注事件 ${code} 对应哪只精灵`
    : `标注${kindLabel} id ${code}`

  return (
    <div className="confirm-backdrop show" onClick={() => onClose()}>
      <div className="confirm-dialog anno-dialog" onClick={(e) => e.stopPropagation()}>
        <div className="anno-title">{title}</div>
        <div className="anno-hint">
          {kind === 'event'
            ? '照游戏画面里这个事件的精灵,在候选里搜名字选中并提交;提交后由管理员审核、全服共享。'
            : '在候选里搜索并选择一个名字,提交后由管理员审核、全服共享。'}
        </div>
        <input
          ref={inputRef}
          className="anno-input"
          placeholder={`搜索${kind === 'event' ? '精灵' : kindLabel}候选…`}
          value={q}
          onChange={(e) => setQ(e.target.value)}
        />
        <div className="anno-list">
          {!candidates
            ? <div className="anno-empty">候选加载中…</div>
            : filtered.length === 0
              ? <div className="anno-empty">没有匹配的候选,试试其它关键词</div>
              : filtered.map((c) => (
                // pets 是「wiki 上哪些精灵带这个特性」:玩家对着裸 id 没有线索,
                // 凭战斗表现猜答案时,这条归属往往是能直接印证的那一块拼图。
                <button
                  key={c.id || c.name}
                  type="button"
                  className="anno-item"
                  title={c.pets && c.pets.length > 0 ? `拥有该特性的精灵:${c.pets.join('、')}` : undefined}
                  onClick={() => pick(c)}
                >
                  <span className="anno-item-name">{c.name}</span>
                  {c.desc && <span className="anno-item-desc">{c.desc}</span>}
                  {c.id && <span className="anno-item-id">id {c.id}</span>}
                </button>
              ))}
        </div>
        <input
          className="anno-input"
          placeholder="名字(必填,一般与选中的候选一致)"
          value={name}
          onChange={(e) => setName(e.target.value)}
        />
        <textarea
          className="anno-input anno-desc"
          placeholder="描述(选填)"
          rows={2}
          value={desc}
          onChange={(e) => setDesc(e.target.value)}
        />
        {err && <div className="anno-err">{err}</div>}
        <div className="confirm-actions">
          <button type="button" className="btn" onClick={() => onClose()}>取消</button>
          <button type="button" className="btn primary" disabled={submitting} onClick={submit}>
            {submitting ? '提交中…' : '提交标注'}
          </button>
        </div>
      </div>
    </div>
  )
}

// UnknownChip 未知 id 的展示块:显示 id,提供「标注」入口。
// 游戏协议只给 id 时(试炼特性/技能),内置词典查不到就渲染这个;
// 玩家点「标注」打开 AnnotationModal。
export function UnknownChip({ kind, code }) {
  const [open, setOpen] = useState(false)
  return (
    <>
      <span className="anno-unknown" title={`未知${KIND_LABEL[kind] || kind} id ${code} — 点击标注`}>
        {code}
        <button type="button" className="anno-chip-btn" onClick={() => setOpen(true)}>标注</button>
      </span>
      {open && <AnnotationModal kind={kind} code={code} onClose={() => setOpen(false)} />}
    </>
  )
}
