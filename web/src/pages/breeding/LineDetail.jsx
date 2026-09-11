import React, { useCallback, useMemo } from 'react'
import { confirmDialog } from '../../components/confirm'
import ComboSelect from '../../components/ComboSelect'
import { fmtShortTime } from '../../utils/format'
import GoalEditor from './GoalEditor'
import { chainStats, lineageOf } from './pets'
import GenerationRow from './GenerationRow'
import RecordPanel from './RecordPanel'
import SuggestPanel from './SuggestPanel'
import {
  bestChild, chainKey, chainOf, fatherCandidates, fmtPct, goalProgress, lineStats, pendCounts,
  petPickerOption,
} from './pets'
import ParentSlot, { NestHint, ParentRow, parentCard, releasedCard } from './ParentSlot'
import useBreedPool from './useBreedPool'

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
  nest, onNestTruth,
  onSave, onClaim, onRemove, onPet, onLocate, onOpenLine, onMerge, busy,
}) {
  const pets = useBreedPool(line)
  const st = lineStats(line)
  // 谱系:换种母会开子线(见 internal/pet.NewChildLine),故一条培育史在库里是一串线。
  // 只有一段时(没有换过种母)不显示这一条 —— 凭空冒出「谱系」两个字只会让人困惑。
  const chain = useMemo(() => lineageOf(lines, line.id), [lines, line.id])
  const cs = useMemo(() => chainStats(lines, line.id), [lines, line.id])
  const { bars, flags } = goalProgress(line)
  // 最接近同时达标的那一只子代(见 pets.bestChild):它与上面的「各维最佳」是两个问题 ——
  // 后者可能来自不同代,而「为什么还没达成」只有前者能回答。
  const bc = bestChild(line)
  // 全部目标项都达标、状态却还停在进行中:判定放宽后老数据会露出这种状态 ——
  // AutoDoneOnReach 只在「未达成→达成」的转变时动手,而老线在放宽前的那次写入时 before 就已经
  // 是 true,之后任何写入都不会再翻。不静默改写玩家的状态,给一个显式入口。
  const allHit = !!bc && bc.miss === 0
  const pend = pendCounts(line.pending)

  // —— 亲本(种母 × 种公)——
  //
  // 种母是这条线的**身份**(她孵的蛋自动记到这里,见 pet.BreedingLine.MotherGid),故这里只读;
  // 种公是**计划值**(每代实际用的父本另记在各代里),可以随时换 —— 换个公就是换个公,不必另开线。
  //
  // 蛋组不在快照里(pet.EggParent 没有这个字段),故按 gid 从候选池那份里取出来补上:
  // 同一只宠物在两处的形状不同,摆平它比让卡片去猜简单(见 ParentSlot.parentCard)。
  const motherCand = useMemo(
    () => pets.mothers.find((p) => p.gid === (line.mother && line.mother.gid)) || null,
    [pets.mothers, line.mother],
  )
  const fatherCand = useMemo(
    () => pets.fathers.find((p) => p.gid === (line.father && line.father.gid)) || null,
    [pets.fathers, line.father],
  )
  // 指定过、但那位已不在库:没有快照可用,造一张只有名字的卡给种母位(她的位是只读的,
  // 必须显示「已不在库」而不是「＋ 选种母」);种公位可换,故改成一句提示 + 让他重新挑。
  const motherCard = parentCard(line.mother, motherCand && motherCand.eggGroups)
    || (line.motherReleased ? releasedCard() : null)
  const fatherCard = parentCard(line.father, fatherCand && fatherCand.eggGroups)
  const fatherReleasedHint = line.fatherReleased ? '原来指定的种公已不在库（放生 / 送人），换一位即可' : null
  // 学院小窝的三个取值分给两张卡(见 ParentSlot 里那段注释):
  //   nestTruth  = 游戏真值(响应顶层 nest,工具改不了);
  //   nestPlanGid= 本线的计划值(随线保存);
  //   nestEff    = 后端算概率时用的**生效值** = 计划优先、没计划才回落真值 —— 勾选看它,
  //               才能与卡上那些百分比(后端按同一个 gid 算的)指同一只。
  // onNest 在详情里就是写本线的计划值(经 onSave 整条覆盖写),不碰 /api/nest:那才是
  // 「本线打算把小窝给谁」的落点;要改游戏里的事实得去游戏里放/抱。
  const nestTruth = (nest && nest.gid) || 0
  const nestPlanGid = line.nestPlanGid || 0
  const nestEff = nestPlanGid || nestTruth
  // 两张卡上的勾选都写**本线的计划值**(gid=0 = 清掉计划、回落真值):整条覆盖写,与别的写操作
  // 同一套路(经 onSave → run)。种母位是只读的(不能换这只母本),但**照样能勾计划** ——
  // 只读说的是身份,不是说她不能当小窝里那只。
  const setNestPlan = useCallback((gid) => {
    onSave({ ...line, nestPlanGid: gid ? Number(gid) : 0 })
  }, [line, onSave])
  // 取值优先用**快照里的 gid**,没有快照时退回线上存的 gid:老线只存了 MotherGid 那条链路、
  // 快照由读取时派生,两种情形都可能只拿到一半。
  const motherValue = String((line.mother && line.mother.gid) || line.motherGid || '')
  const fatherValue = String((line.father && line.father.gid) || line.fatherGid || '')
  // 换种公的候选与建线时**同一套口径**(见 Breeding 里的注释):先按品种取池子,再按母本蛋组收。
  const fatherOpts = useMemo(
    () => fatherCandidates(pets.fathers, motherCand).map(petPickerOption),
    [pets.fathers, motherCand],
  )
  const changeFather = useCallback((gid) => {
    onSave({ ...line, fatherGid: gid ? Number(gid) : 0 })
  }, [line, onSave])

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
        {/* 种母与种公移到头部下面那一行(见下面的亲本区):它们是「一对」,塞在工具栏里
            只能一个一个地摆,「母 × 公」这件事就散了。这里只留代数的元信息。 */}
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

      {/* 亲本:这条线押的那一对(与建线表单是同一张卡,见 ParentSlot)。
          种母只读 —— 她孵的蛋自动记到这里,换种母就是另一条线;种公可换 —— 它只是计划值,
          各代实际用的父本仍记在各代里(那里写着抓包抓到的真实父本)。
          「学院小窝」勾选照给:两位都可能正是小窝里那只(母本尤其常见),而这份加成与性别无关。
          种母位**只读**也不妨碍勾它 —— 只读说的是「不能换这只母本」,不是「不能把她放进小窝」。 */}
      <div className="br-parents-block">
        <ParentRow
          mother={(
            <ParentSlot role="mother" card={motherCard} value={motherValue} disabled
              nestGid={nestEff} nestTruth={nestTruth} nestPlanGid={nestPlanGid} onNest={setNestPlan}
              disabledHint="还没固定种母:第一次收蛋时按当时的母本固定"
              title="种母是这条线的身份:她孵的蛋自动记到这里。换种母请在游戏里用她收蛋,后端会自动另开一条线" />
          )}
          father={(
            <ParentSlot role="father" card={fatherCard} options={fatherOpts} value={fatherValue}
              disabled={busy} disabledHint="加载中…" onChange={changeFather} releasedHint={fatherReleasedHint}
              nestGid={nestEff} nestTruth={nestTruth} nestPlanGid={nestPlanGid} onNest={setNestPlan}
              title="种公是计划值:改它只影响这条线的展示与补录默认值,不会改动任何一代已记录的真实父本" />
          )}
        />
        <NestHint truth={nest} planGid={nestPlanGid} plan={line.nestPlan} planReleased={line.nestPlanReleased}
          onClearPlan={() => onSave({ ...line, nestPlanGid: 0 })}
          onCommitTruth={onNestTruth} />
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
                title={`${l.mother ? `种母 ${l.mother.name}` : l.motherReleased ? '种母已放生' : '未定种母'} · ${lineStats(l).gens} 代`}>
                {l.mother ? l.mother.name : (l.motherReleased ? '已放生' : (l.species || '未定'))}
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
                    {bc.hits.gender !== undefined
                      ? <span className={'br-hit' + (bc.hits.gender ? ' on' : '')} title={bc.hits.gender ? '性别已达标' : '性别未达标'}>{bc.child.gender || '性别未知'}</span>
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
                {bars.length === 0 && flags.length === 0 ? (
                  <p className="br-hint muted">目标还是空的 —— 填上以后这里会显示每一代离目标还有多远。</p>
                ) : (
                  <>
                    {bars.map((b) => (
                      <div key={b.k} className="br-bar" title={b.title}>
                        <span className="br-bar-k">{b.k}</span>
                        <span className="br-bar-t"><i className={'br-bar-f ' + b.cls + (b.hit ? ' hit' : '')} style={{ width: b.pct.toFixed(1) + '%' }} /></span>
                        <span className={'br-bar-v' + (b.hit ? ' hit' : '')}>{b.text}</span>
                      </div>
                    ))}
                    {/* 性格 / 性别不是「有极值的轴」,只有命中与未命中 —— 并排一行旗标,不占进度条的位置。 */}
                    {flags.length > 0 ? (
                      <div className="br-flags">
                        {flags.map((f) => (
                          <span key={f.k} className={'br-flag' + (f.hit ? ' hit' : '')} title={f.title}>
                            <em>{f.k}</em><b>{f.text}</b><i>{f.hit ? '达标' : '未命中'}</i>
                          </span>
                        ))}
                      </div>
                    ) : null}
                  </>
                )}
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
