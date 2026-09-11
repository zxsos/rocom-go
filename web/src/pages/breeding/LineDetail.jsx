import React, { useCallback, useContext, useMemo } from 'react'
import { getBreedingPool } from '../../api'
import { AccountContext } from '../../context'
import { useAsyncData } from '../../hooks/useAsyncData'
import { confirmDialog } from '../../components/confirm'
import ComboSelect from '../../components/ComboSelect'
import { fmtShortTime } from '../../utils/format'
import GoalEditor from './GoalEditor'
import { chainStats, lineageOf } from './pets'
import GenerationRow from './GenerationRow'
import RecordPanel from './RecordPanel'
import SuggestPanel from './SuggestPanel'
import { bestChild, chainKey, chainOf, fmtPct, goalProgress, lineStats, pendCounts } from './pets'

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
  line, lines, chains, chainItems, natureMatrix, loading,
  onSave, onClaim, onRemove, onPet, onLocate, onOpenLine, onMerge, busy,
}) {
  const pets = useLinePets(line)
  const st = lineStats(line)
  // 谱系:换种母会开子线(见 internal/pet.NewChildLine),故一条培育史在库里是一串线。
  // 只有一段时(没有换过种母)不显示这一条 —— 凭空冒出「谱系」两个字只会让人困惑。
  const chain = useMemo(() => lineageOf(lines, line.id), [lines, line.id])
  const cs = useMemo(() => chainStats(lines, line.id), [lines, line.id])
  const bars = goalProgress(line, natureMatrix)
  // 最接近同时达标的那一只子代(见 pets.bestChild):它与上面的「各维最佳」是两个问题 ——
  // 后者可能来自不同代,而「为什么还没达成」只有前者能回答。
  const bc = bestChild(line)
  // 全部目标项都达标、状态却还停在进行中:判定放宽后老数据会露出这种状态 ——
  // AutoDoneOnReach 只在「未达成→达成」的转变时动手,而老线在放宽前的那次写入时 before 就已经
  // 是 true,之后任何写入都不会再翻。不静默改写玩家的状态,给一个显式入口。
  const allHit = !!bc && bc.miss === 0
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

  // mergeIntoParent 把**当前这条子线**并入它的母线。
  //
  // 为什么默认不合并、要玩家点:合并会删掉这条线(代数合进母线后就不需要它了),而培育史
  // 删了回不来。谱系视图已经把它们**展示**成一条连续的史了,合并只是把它在库里也合成一条 ——
  // 是整理,不是必需。故放在这里当成一个显式的、带确认的动作。
  const mergeIntoParent = async () => {
    if (!line.parentLineId) return
    const ok = await confirmDialog({
      message: `把这条线并入它的母线?${(line.gens || []).length} 代会合进去,这条线随后删掉 —— 不可逆。`,
      okText: '并入母线', danger: true,
    })
    if (!ok) return
    await onMerge(line.id)
  }

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
        {/* 种母:这条线的身份就是她(见 internal/pet.BreedingLine.MotherGid)。放在标题旁边
            而不是埋在元信息里 —— 同一个品种可以同时有几条线,只有品种名分不出是哪一条。
            换种母不在这里改:换了种母就是另一条线,收蛋时后端会自动开子线或独立新线。 */}
        <span className="br-detail-mother" title={line.mother
          ? `这条线固定在种母「${line.mother.name}」身上:她孵的蛋自动记到这里。换种母时后端会另开一条线`
          : '这条线还没固定种母:第一次收蛋时会按当时的母本固定下来'}>
          {line.mother ? `种母 ${line.mother.name}` : '未定种母'}
        </span>
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

      {/* 谱系:换种母会开子线(子代接班当种母是选育的典型操作),故一条培育史在库里是
          一串线。不拼回来的话,玩家看到的是几段互不相干的碎片 —— 而那正好发生在他最想
          看「一路是怎么过来的」的时候。点任一段跳过去。 */}
      {chain.length > 1 ? (
        <div className="br-lineage">
          <span className="br-lineage-k muted">谱系</span>
          {chain.map((l, i) => (
            <React.Fragment key={l.id}>
              {i > 0 ? <span className="br-lineage-arrow muted">→</span> : null}
              <button type="button"
                className={'br-lineage-seg' + (l.id === line.id ? ' on' : '')}
                disabled={l.id === line.id || busy}
                onClick={() => onOpenLine && onOpenLine(l.id)}
                title={`${l.mother ? `种母 ${l.mother.name}` : '未定种母'} · ${lineStats(l).gens} 代`}>
                {l.mother ? l.mother.name : (l.species || '未定')}
                <em>{lineStats(l).gens} 代</em>
              </button>
            </React.Fragment>
          ))}
          {/* 累计:代数跨段相加(子线的代数是接着母线数的,故直接相加就是整条史的代数) */}
          <span className="br-lineage-sum muted">
            共 {cs.gens} 代
            {cs.bestVoice != null ? ` · 最佳 V${cs.bestVoice}` : ''}
            {cs.bestWeight != null ? ` · 最佳 W${fmtPct(cs.bestWeight)}%` : ''}
          </span>
          {/* 只有**当前这条是子线**时才给「并入母线」:合并的方向是子线并入母线,
              站在母线上时没有可并的(它的子线要各自进去)。 */}
          {line.parentLineId ? (
            <button className="btn ghost small" disabled={busy} onClick={mergeIntoParent}
              title="把这条线的代数合进母线,然后删掉这条线 —— 谱系视图本来就把它们连着看,合并只是把库里也合成一条(不可逆)">
              并入母线
            </button>
          ) : null}
        </div>
      ) : null}

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
                    prev={i > 0 ? rows[i - 1].g : null}
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
              <span className="muted">历代各维最佳 vs 目标</span>
            </div>
            {st.gens === 0 ? (
              <p className="br-hint muted">还没有已认领的子代,先把某一代的子代认领了这里才有数。</p>
            ) : (
              <>
                <div className="br-stat-row">
                  <span className="br-stat-k">历代嗓音最佳</span>
                  <b className="br-stat-v">{st.bestVoice == null ? '—' : st.bestVoice}</b>
                  <span className="br-stat-k">历代体重最佳</span>
                  <b className="br-stat-v">{st.bestWeight == null ? '—' : fmtPct(st.bestWeight) + '%'}</b>
                </div>
                {/* 「各维最佳」可能来自不同代,上面那两行因此不能读成「同一只快达标了」。
                    下面这一行给同一只子代的逐项命中 —— 它就是「为什么还停在进行中」的答案。 */}
                {bc ? (
                  <div className="br-bestgen">
                    <span className="br-bestgen-k">最接近达标的一代</span>
                    <button type="button" className="br-bestgen-gen"
                      disabled={!onPet || !bc.child.gid}
                      onClick={() => onPet && bc.child.gid && onPet(bc.child.gid)}
                      title={bc.miss === 0 ? '这一代已同时满足全部目标' : `这一代还差 ${bc.miss} 项未达标`}>
                      第 {bc.gen} 代
                    </button>
                    {bc.hits.voice !== undefined
                      ? <span className={'br-hit' + (bc.hits.voice ? ' on' : '')} title={bc.hits.voice ? '嗓音已达标' : '嗓音未达标'}>V{bc.child.voice}</span>
                      : null}
                    {bc.hits.weight !== undefined
                      ? <span className={'br-hit' + (bc.hits.weight ? ' on' : '')} title={bc.hits.weight ? '体重已达标' : '体重未达标'}>W{fmtPct(bc.child.weightPct)}%</span>
                      : null}
                    {bc.hits.nature !== undefined
                      ? <span className={'br-hit' + (bc.hits.nature ? ' on' : '')} title={bc.hits.nature ? '性格已达标' : '性格未达标'}>{bc.child.nature || '性格未知'}</span>
                      : null}
                    <span className="muted">{bc.miss === 0 ? '全部达标' : `还差 ${bc.miss} 项`}</span>
                  </div>
                ) : null}
                {allHit && line.status !== 'done' ? (
                  <div className="br-stale">
                    <span>
                      这一代的子代已满足<b>全部</b>填了的目标,但这条线还是「进行中」——
                      放宽判定后自动标记不会再触发(它只在「由未达成变达成」时动手)。
                    </span>
                    <button type="button" className="btn ghost small" disabled={busy}
                      onClick={() => onSave({ ...line, status: 'done' })}>标为已达成</button>
                  </div>
                ) : null}
                {bars.length === 0 ? (
                  <p className="br-hint muted">目标还是空的 —— 填上以后这里会显示每一代离目标还有多远。</p>
                ) : bars.map((b) => (
                  <div key={b.k} className="br-bar" title={b.title}>
                    <span className="br-bar-k">{b.k}</span>
                    <span className="br-bar-t"><i className={'br-bar-f ' + b.cls + (b.hit ? ' hit' : '')} style={{ width: b.pct.toFixed(1) + '%' }} /></span>
                    <span className={'br-bar-v' + (b.hit ? ' hit' : '')}>{b.text}</span>
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
