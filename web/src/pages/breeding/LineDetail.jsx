import React, { useCallback, useContext, useMemo } from 'react'
import { getBreedingPool } from '../../api'
import { AccountContext } from '../../context'
import { useAsyncData } from '../../hooks/useAsyncData'
import { confirmDialog } from '../../components/confirm'
import ComboSelect from '../../components/ComboSelect'
import { fmtShortTime } from '../../utils/format'
import GoalEditor from './GoalEditor'
import GenerationRow from './GenerationRow'
import RecordPanel from './RecordPanel'
import SuggestPanel from './SuggestPanel'
import { chainKey, chainOf, fmtPct, goalProgress, lineStats, pendCounts } from './pets'

const STATUS = [
  { k: 'active', label: '进行中', title: '继续推进这条线(破壳的自动记录只会记到「进行中」的线上)' },
  { k: 'done', label: '已达成',
    title: '填了的目标项被某一代的同一只子代全部满足时,记录写入会自动打上这个状态;也可以手动改成这个' },
  { k: 'archived', label: '归档', title: '不打算继续,但想留着这段历史' },
]

// LineDetail 一条培育线的详情:目标编辑 + 代数时间线 + 选配建议 + 手动补录。
//
// 桌面两栏(时间线在左、决策用的建议在右);窄屏上下堆叠 —— 时间线是纵向长列表,
// 与建议挤在一行只会两边都读不清。
export default function LineDetail({
  line, chains, chainItems, natureMatrix, loading, onSave, onClaim, onRemove, onPet, onLocate, busy,
}) {
  const pets = useLinePets(line)
  const st = lineStats(line)
  const bars = goalProgress(line)
  const pend = pendCounts(line.pending)

  const rows = useMemo(() => {
    const all = [
      ...((line.gens || []).map((g) => ({ g, pending: false }))),
      ...((line.pending || []).map((g) => ({ g, pending: true }))),
    ]
    return all.sort((a, b) => a.g.gen - b.g.gen)
  }, [line.gens, line.pending])

  // 某一代「比上一代更好吗」要和**同一条时间线上的前一条**比(已认领的才算),
  // 待认领的那一代没有子代,不参与。
  const saveGen = useCallback((gen) => {
    onSave({ ...line, gens: (line.gens || []).map((x) => (x.gen === gen.gen ? gen : x)) })
  }, [line, onSave])

  const delGen = useCallback(async (g) => {
    const ok = await confirmDialog({
      message: `删掉第 ${g.gen} 代?这只影响培育线上的记录,不会动宠物库里的任何宠物。`,
      okText: '删掉', danger: true,
    })
    if (!ok) return
    onSave({
      ...line,
      gens: (line.gens || []).filter((x) => x.gen !== g.gen),
      pending: (line.pending || []).filter((x) => x.gen !== g.gen),
    })
  }, [line, onSave])

  // 线认的是**品种**(进化链):chainName 把链上各阶段都列出来 —— 只显示 species 会让人以为
  // 这条线只认那一个形态名(见 api.js 的 getBreeding 注释)。
  const kind = line.chainName || line.species || '未定品种'

  const removeLine = async () => {
    const ok = await confirmDialog({
      message: `删掉「${kind}」这条培育线?${(line.gens || []).length} 代的记录会一起消失,宠物不受影响。`,
      okText: '删掉这条线', danger: true,
    })
    if (ok) onRemove(line.id)
  }

  // changeKind 换这条线认的品种(进化链)。
  //
  // 为什么线要能改品种:链口径之前建的老线只存了形态名,而按名字推不出链的情形是真实存在的
  // (同名多形态、名字库里没有),推不出来时它只能一直按名字认 —— 玩家在这里重选一次就归到链上。
  // 已记的代数不动,但选种与补录的候选池会按新品种重算,故有记录时先确认一次。
  const changeKind = async (next) => {
    const c = chainOf(next, chains)
    if (!c || chainKey(line) === next) return
    const gens = (line.gens || []).length + (line.pending || []).length
    if (gens > 0) {
      const ok = await confirmDialog({
        message: `把这条线的品种改成「${c.label}」?已记的 ${gens} 代不动,但选种与补录的候选池会按新品种重算。`,
        okText: '改品种',
      })
      if (!ok) return
    }
    onSave({ ...line, evo: c.evo, species: c.species })
  }

  return (
    <div className="br-detail">
      <div className="toolbar br-detail-head">
        {/* 标题本身就是品种选择器:链口径下「这条线认的是哪个品种」是随时该能改的,
            而它也正是这个页面最重要的一行信息(见 changeKind)。 */}
        <ComboSelect className="br-detail-kind" value={chainKey(line)} options={chainItems}
          onChange={changeKind} disabled={busy || !chainItems.length}
          placeholder="选品种(进化链)…" emptyText="品种"
          title={`这条线认的品种:${kind} —— 链上任一阶段的 ♀ 都能当它的种母`} />
        <div className="br-status-seg">
          {STATUS.map((s) => (
            <button key={s.k} type="button" title={s.title}
              className={'chip' + ((line.status || 'active') === s.k ? ' on' : '')}
              disabled={busy}
              onClick={() => onSave({ ...line, status: s.k })}>{s.label}</button>
          ))}
        </div>
        <span className="muted br-detail-meta">
          {st.gens} 代已记录
          {/* 待孵与待认领分开写:前者是在等一颗蛋,后者是在等子代进背包 ——
              混成一句「N 代待认领」会让人以为都在等他操作。 */}
          {pend.incubating > 0 ? ` · ${pend.incubating} 代待孵` : ''}
          {pend.claim > 0 ? ` · ${pend.claim} 代待认领` : ''}
          {line.updatedAt ? ` · 最近 ${fmtShortTime(line.updatedAt)}` : ''}
        </span>
        <div className="spacer" />
        <button className="btn ghost danger small" disabled={busy} onClick={removeLine}>删除这条线</button>
      </div>

      <div className="br-cols">
        <div className="br-main">
          <section className="br-panel">
            <div className="br-sec-head">
              <h3>代数时间线</h3>
              <span className="muted">母 × 父 → 子代 · 数值带 V/W 前缀,点击宠物看详情</span>
            </div>
            {rows.length === 0 ? (
              <p className="br-hint muted">
                还没有任何记录。在游戏里孵蛋破壳时会自动记一代(前提是蛋上留有小窝 Interaction 时的双亲快照);
                也可以直接用下面的「手动补录一代」。
              </p>
            ) : (
              <ol className="br-timeline">
                {rows.map(({ g, pending }, i) => (
                  <GenerationRow key={pending ? 'p' + g.gen : 'g' + g.gen}
                    g={g} pending={pending} line={line}
                    prevChild={prevChildOf(rows, i)}
                    fathers={pets.fathers} kids={pets.kids} matrix={natureMatrix}
                    onEdit={saveGen} onClaim={onClaim} onDelete={delGen} onPet={onPet} busy={busy} />
                ))}
              </ol>
            )}
          </section>

          <RecordPanel line={line} mothers={pets.mothers} fathers={pets.fathers} kids={pets.kids}
            onAdd={(gen) => onSave({ ...line, gens: [...(line.gens || []), gen] })} busy={busy} />
        </div>

        <aside className="br-side">
          <GoalEditor line={line} natureMatrix={natureMatrix} busy={busy}
            onSave={(goal) => onSave({ ...line, goal })} />

          <section className="br-panel br-progress">
            <div className="br-sec-head">
              <h3>进度概览</h3>
              <span className="muted">历代最佳 vs 目标</span>
            </div>
            {st.gens === 0 ? (
              <p className="br-hint muted">还没有已认领的子代,先把某一代的子代认领了这里才有数。</p>
            ) : (
              <>
                <div className="br-stat-row">
                  <span className="br-stat-k">历代最佳嗓音</span>
                  <b className="br-stat-v">{st.bestVoice == null ? '—' : st.bestVoice}</b>
                  <span className="br-stat-k">最佳体重百分位</span>
                  <b className="br-stat-v">{st.bestWeight == null ? '—' : fmtPct(st.bestWeight) + '%'}</b>
                </div>
                {bars.length === 0 ? (
                  <p className="br-hint muted">目标还是空的 —— 填上以后这里会显示每一代离目标还有多远。</p>
                ) : bars.map((b) => (
                  <div key={b.k} className="br-bar" title={b.title}>
                    <span className="br-bar-k">{b.k}</span>
                    <span className="br-bar-t"><i className={'br-bar-f ' + b.cls} style={{ width: b.pct.toFixed(1) + '%' }} /></span>
                    <span className="br-bar-v">{b.text}</span>
                  </div>
                ))}
              </>
            )}
          </section>

          <SuggestPanel line={line} matrix={natureMatrix} suggesting={loading} onPet={onPet} onLocate={onLocate} />
        </aside>
      </div>
    </div>
  )
}

// prevChildOf 第 i 条记录之前**最近的一个已认领子代**(用来算「比上一代更好吗」)。
function prevChildOf(rows, i) {
  for (let k = i - 1; k >= 0; k--) {
    if (rows[k].g.child) return rows[k].g.child
  }
  return null
}

// useLinePets 一次取齐这条线要用的三类候选(种母 / 种公 / 子代),来自后端按品种裁剪好的候选池。
//
// 一个查询,而不是两次 200 条分页:分页的天花板(store.clampPageSize)正好卡在候选池的要害上 ——
// 漏掉一页就等于「库里还有更合适的个体,玩家却无从知道」,而补录是照着记忆 / 截图去找某一只有
// 没有在库里。三个队列的范围规则(同品种 ♀、与母本同蛋组的 ♂、同品种)都只跟品种有关,后端
// 一并算完,前端只做关键词过滤(见 internal/pet.BreedCandidates)。
//
// 按**品种**(evo + species)缓存:同一条线内多次展开 / 收起不重拉;改目标也**不重拉**
// —— 候选池与目标无关(见 api.js 的 getBreedingPool)。两个字段都要进依赖:无链形态的 evo 是 0,
// 仅凭它区分不出品种。
function useLinePets(line) {
  const account = useContext(AccountContext)
  const evo = line.evo || 0
  const species = line.species || ''

  const { data } = useAsyncData(
    useCallback(
      () => (evo || species ? getBreedingPool(evo, species) : Promise.resolve(null)),
      [evo, species],
    ),
    { fallback: null, reloadKey: account },
  )

  return useMemo(() => ({
    mothers: ((data && data.mothers) || []).slice().sort(byVoiceDesc),
    fathers: ((data && data.fathers) || []).slice().sort(byVoiceDesc),
    kids: ((data && data.kids) || []).slice().sort(byVoiceDesc),
  }), [data])
}

// byVoiceDesc 候选按嗓音绝对值降序:极端个体排在前面,先看到的多半是玩家真正想要的。
const byVoiceDesc = (a, b) => Math.abs(b.voice || 0) - Math.abs(a.voice || 0)
