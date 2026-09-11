import React, { useCallback, useContext, useEffect, useMemo, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import {
  claimBreedingChild, deleteBreeding, getBreeding, getFilterOptions, getNameOptions, mergeBreeding,
  saveBreeding, subscribe,
} from '../../api'
import { AccountContext } from '../../context'
import { useAsyncData } from '../../hooks/useAsyncData'
import { PetDetailModal } from '../../components/PetDetailModal'
import PetLocateModal from '../../components/PetLocateModal'
import ComboSelect from '../../components/ComboSelect'
import { toast } from '../../components/toast'
import { IconBreeding, IconRefresh } from '../../components/svg'
import LineCard from './LineCard'
import LineDetail from './LineDetail'
import { GoalFields } from './GoalEditor'
import { chainKey, chainOf, lineStats, parseGoal, pendCounts } from './pets'

const EMPTY_GOAL = { voice: '', weightPct: '', nature: '', natureIn: [] }

const STATUS_FILTERS = [
  { k: '', label: '全部' },
  { k: 'active', label: '进行中' },
  { k: 'done', label: '已达成' },
  { k: 'archived', label: '归档' },
]

const SORTS = [
  { k: 'updated', label: '最近更新' },
  { k: 'gens', label: '代数' },
  { k: 'voice', label: '历代最佳嗓音' },
]

// 培育页:左侧是培育线列表,右侧(或整屏)是选中那条线的详情。
//
// 列表与详情同路由、用 ?line=<id> 区分:刷新与分享链接都停在同一屏,浏览器前进/后退可用
// (与 HomeQuery / Admin 的查询参数用法一致)。详情不开弹窗 —— 时间线 + 建议面板是**长时间
// 对着看**的内容,压在弹窗里既挤不出两栏,也没法滚到别处对照。
//
// 数据流:写操作全部交给后端(整条线覆盖写),成功后重拉 —— 培育线的价值在于「谁配谁生出了谁」,
// 这种记录不该由前端做乐观拼接(一旦拼错,看起来和真记录一模一样)。服务端另有
// Broadcast("breeding") 信号,别的页面 / 设备改动时会自动重拉(见 api_breeding.go)。
export default function Breeding() {
  const account = useContext(AccountContext)
  const [params, setParams] = useSearchParams()
  const lineId = params.get('line') || ''

  const [q, setQ] = useState('')
  const [status, setStatus] = useState('')
  const [sort, setSort] = useState('updated')
  const [detailGid, setDetailGid] = useState(0)
  const [locGid, setLocGid] = useState(0) // 仓库定位弹窗的目标 gid(0=关闭)
  const [busy, setBusy] = useState(false)
  const [creating, setCreating] = useState(false)
  const [newChain, setNewChain] = useState('') // 选中品种的取值键(见 pets.chainKey)
  const [newGoal, setNewGoal] = useState(EMPTY_GOAL)

  const { data, loading, error, refresh } = useAsyncData(
    useCallback(() => getBreeding(), []), { fallback: { lines: [] }, reloadKey: account },
  )
  const lines = useMemo(() => (data && data.lines) || [], [data])
  const { data: nameOpts } = useAsyncData(
    useCallback(() => getNameOptions(), []), { fallback: { nature: [] } },
  )
  // 性格方阵:后端给的是宠物页筛选用的 6×6 矩阵。**原样留着、不拍平** —— 「目标性格 = 正面加某维」
  // 在前端按**行**展开成那 5 个名字(natureIn),拍平成一维名单就丢掉了「哪一行是哪一维」。
  const natureMatrix = useMemo(() => (nameOpts && nameOpts.nature) || [], [nameOpts])
  // 品种选项:按**进化链**归并、只列**宠物库里确实有的**(filter-options 的 chains)。
  //
  // 为什么不是按形态名列:一条链上的各阶段是同一个品种(蛋的物种随母本,链上任一阶段的 ♀ 都能
  // 当这条线的种母),而同一只精灵的不同形态各占一条链(嗜波螺两种样子)—— 按形态名列会把
  // 前者拆开、把后者并成一个,两边都错(见 internal/gamedata.ChainOptions)。
  const { data: filterOpts } = useAsyncData(
    useCallback(() => getFilterOptions(), []), { fallback: {} },
  )
  const chains = useMemo(() => (filterOpts && filterOpts.chains) || [], [filterOpts])
  // 下拉项:label 是整条链(「阿米亚特（阿米樱/罗隐/深渊罗隐）」),sub 写库里这个品种有几只 ——
  // 长名字靠文字认不出是哪个品种,数量则是「建这条线有没有得配」的第一手信息。
  //
  // 图优先用这个品种的**蛋**(c.egg):这条线最终要孵的就是它 —— 选品种的这一刻就该看见
  // 「孵出来的是哪颗蛋」。而且同名多形态的品种各有各的蛋(地鼠两种样子分别是
  // egg_dishu / egg_dishu_2、波波螺是 egg_boboluo / egg_bobolouar),光看名字完全一样。
  // 查不到蛋图时退回链首头像 c.img:蛋只挂在初始形态上,而同一物种在库里可能有多个条目
  // (板板壳 3055 有蛋、3516 没有),查不到不等于孵不出来。
  //
  // 真正生不出蛋的品种**不在候选里** —— 后端按**繁殖组(蛋组)**滤掉了(见 gamedata.IsInfertile):
  // 蛋组「未发现」的那一批(迪莫、帕尔萨斯/圣羽翼王那一系等)进不了小窝配种。判据不能用
  // 「有没有蛋图」,那样会误伤几十个正常品种。
  const chainItems = useMemo(
    () => chains.map((c) => ({ value: chainKey(c), label: c.label, sub: `${c.count} 只`, img: c.egg || c.img })),
    [chains],
  )

  // 服务端推送「培育数据变了」→ 重拉整份。onOpen 也补拉一次:断线期间的消息不会重放。
  useEffect(() => subscribe('breeding', refresh, { onOpen: refresh }), [refresh])

  const line = useMemo(() => lines.find((l) => l.id === lineId) || null, [lines, lineId])

  // run 统一「提交 → 重拉 → 提示」:写操作全是整条覆盖写,前端不做乐观更新,
  // 故每处都只关心「发哪个请求、成功说什么」,失败一律 toast 原文(后端的校验文案比前端猜的更准)。
  // 返回 true / false 而不是抛错:调用方(如手动补录表单)要按成败决定「清不清空输入框」——
  // 失败时清空等于把玩家刚填的一整代丢掉,那比不提交还糟。
  const run = useCallback((task, okMsg) => {
    setBusy(true)
    return Promise.resolve()
      .then(task)
      .then(() => refresh())
      .then(
        () => { if (okMsg) toast(okMsg); return true },
        (e) => { toast((e && e.message) || '操作失败'); return false },
      )
      .then((ok) => { setBusy(false); return ok })
  }, [refresh])

  const openLine = (id) => setParams({ line: id })
  const closeLine = () => setParams({})

  const createLine = () => {
    // 下拉框可以打字,但只有**选中候选**才会回到这里(ComboSelect 收起时会把没选中的文字回填成
    // 当前值)。仍要再查一次:选项是拉回来的快照,期间那只宠物可能已被放生/进化,库里就此没有
    // 这个品种了 —— 建一条配不出候选的线,玩家只会以为这个页面坏了。
    const picked = chainOf(newChain, chains)
    if (!picked) {
      toast('先选品种 —— 蛋的物种随母本,线只能挂在库里真有的品种上')
      return
    }
    // id 自己造:后端要求非空且**同 id 覆盖写**,故不能用「品种名」这种固定值
    // (同品种可以先归档再开一条新的,固定 id 会把上一次的历史覆盖掉)。带时刻 + 随机后缀。
    const id = `manual-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`
    setBusy(true)
    // evo + species 是这条线认的品种(见 pets.chainKey):链上任一阶段的个体从此都算它的种母。
    saveBreeding({
      id, evo: picked.evo, species: picked.species, goal: parseGoal(newGoal), status: 'active', gens: [],
    })
      .then(() => refresh())
      .then(
        () => {
          setCreating(false); setNewChain(''); setNewGoal(EMPTY_GOAL)
          openLine(id)
          // 同品种已经有进行中的线时多说一句。不拦 —— 同一个品种同时开几条线是正常用法
          // (几个窝、几只母本各孵各的,它们**不会**互相干扰:蛋按种母归线)。
          // 但要讲清这条新线此刻还没有种母,要等第一次收蛋才固定到某只母本身上。
          const same = lines.filter((l) => (l.status || 'active') === 'active'
            && chainKey(l) === newChain).length
          toast(same > 0
            ? `培育线已建立 —— 这个品种还有 ${same} 条进行中的线,它们各跟各的种母,互不干扰。`
              + '这条线还没固定种母,第一次收蛋时会按当时的母本固定下来'
            : '培育线已建立 —— 填上目标后建议才有意义')
        },
        (e) => toast((e && e.message) || '建线失败'),
      )
      .then(() => setBusy(false))
  }

  const shown = useMemo(() => {
    const kw = q.trim()
    let out = lines
    // 搜的是**这条线认的品种**:chainName 把链上各阶段都列了出来(「阿米亚特（阿米樱/罗隐/深渊罗隐）」),
    // 故搜「阿米亚特」也能找到一条记成罗隐的线 —— 而 species 只有建线时那一个形态名。
    if (kw) out = out.filter((l) => (l.chainName || '').includes(kw) || (l.species || '').includes(kw))
    if (status) out = out.filter((l) => (l.status || 'active') === status)
    const sorted = [...out]
    if (sort === 'gens') sorted.sort((a, b) => lineStats(b).gens - lineStats(a).gens)
    else if (sort === 'voice') {
      sorted.sort((a, b) => Math.abs(lineStats(b).bestVoice ?? 0) - Math.abs(lineStats(a).bestVoice ?? 0))
    }
    return sorted
  }, [lines, q, status, sort])

  // 待孵与待认领分开计:前者在等蛋、后者在等子代进背包,混成一句会让人以为都要他操作。
  const pend = lines.reduce((acc, l) => {
    const c = pendCounts(l.pending)
    return { incubating: acc.incubating + c.incubating, claim: acc.claim + c.claim }
  }, { incubating: 0, claim: 0 })

  if (lineId) {
    return (
      <div className="br-page">
        <div className="toolbar">
          <button className="btn small" onClick={closeLine}>← 全部培育线</button>
          {line ? null : <span className="muted">{loading ? '加载这条培育线…' : '这条培育线不在了(可能刚被删除)'}</span>}
        </div>
        {line ? (
          <LineDetail
            line={line} lines={lines} chains={chains} chainItems={chainItems}
            natureMatrix={natureMatrix} loading={loading} busy={busy}
            onOpenLine={openLine}
            onMerge={(id) => run(() => mergeBreeding(id)).then((ok) => {
              // 合并后这条线就没了:停在这一屏会显示「这条培育线不在了」。跳到母线更自然 ——
              // 代数都在它那儿。
              if (ok && line.parentLineId) openLine(line.parentLineId)
            })}
            onSave={(next) => run(() => saveBreeding(next))}
            onClaim={(gen, gid) => run(() => claimBreedingChild(line.id, gen, gid), `已认领第 ${gen} 代`)}
            onRemove={(id) => { closeLine(); run(() => deleteBreeding(id), '培育线已删除') }}
            onPet={setDetailGid} onLocate={setLocGid}
          />
        ) : (
          <div className="empty">{loading ? '加载中…' : '它可能已被删除,或换到了别的账号。'}</div>
        )}
        {/* 定位弹窗与详情弹窗都挂在详情页这一层(而不是嵌套):嵌套时点击详情遮罩会
            冒泡到定位遮罩,一次点击把两层一起关掉。 */}
        {locGid ? (
          <PetLocateModal gid={locGid} onClose={() => setLocGid(0)} onDetail={setDetailGid} />
        ) : null}
        {detailGid ? <PetDetailModal gid={detailGid} onClose={() => setDetailGid(0)} /> : null}
      </div>
    )
  }

  return (
    <div className="br-page">
      <div className="toolbar">
        <h3><IconBreeding size={18} /> 培育</h3>
        <span className="muted toolbar-hint">
          逐代记录孵蛋结果(母 × 父 → 子代),按你的目标给出选配与回交建议
        </span>
        <div className="spacer" />
        <span className="muted">
          {lines.length} 条线
          {pend.incubating > 0 ? ` · ${pend.incubating} 代待孵` : ''}
          {pend.claim > 0 ? ` · ${pend.claim} 代待认领` : ''}
        </span>
        <button className="btn small" onClick={() => refresh()} title="重新拉取(服务端推送也会自动刷新)">
          <IconRefresh size={14} /> 刷新
        </button>
        <button className="btn primary small" onClick={() => setCreating((v) => !v)}>
          {creating ? '收起' : '新建培育线'}
        </button>
      </div>

      {creating ? (
        <section className="br-panel br-new">
          <div className="br-sec-head">
            <h3>新建培育线</h3>
            <span className="muted">只列宠物库里有的品种 —— 破壳的自动记录按品种(进化链)找线</span>
          </div>
          <label className="br-field br-new-species">
            <span>品种</span>
            {/* 品种有几百个,纯下拉只能滚着找 —— 这里可以直接打字,边打边弹匹配项(ComboSelect)。 */}
            <ComboSelect value={newChain} options={chainItems} onChange={setNewChain}
              placeholder="输入品种名搜索…" emptyText="品种" disabled={!chainItems.length} />
          </label>
          <p className="br-hint muted">
            一个选项就是一条进化链(括号里是它的各阶段),左边的图是这条线要孵的蛋 ——
            链上任一阶段的 ♀ 都能当这条线的种母,而同一只精灵的两种样子各有各的链(也各有各的蛋)。
            生不出蛋的特殊精灵(迪莫、翼王那一系)不在这里 —— 它们进不了小窝配种。
          </p>
          <p className="br-hint muted">
            目标可以先不填(先攒几代再定也行)。填了以后下面的选配建议就会按它排序。
          </p>
          {chainItems.length ? null : (
            <p className="br-hint muted">
              宠物库里还没有能配种的品种 —— 先在游戏里抓到一只(迪莫、翼王那一系生不出蛋,不算)。
            </p>
          )}
          <GoalFields value={newGoal} onChange={setNewGoal} matrix={natureMatrix} />
          <div className="br-gen-act">
            <button className="btn primary small" disabled={busy} onClick={createLine}>建线</button>
          </div>
        </section>
      ) : null}

      <div className="toolbar br-filters">
        <input className="input" placeholder="按品种搜索(链上各阶段名都能搜到)" value={q}
          onChange={(e) => setQ(e.target.value)} />
        <div className="br-status-seg">
          {STATUS_FILTERS.map((s) => (
            <button key={s.k} className={'chip' + (status === s.k ? ' on' : '')}
              onClick={() => setStatus(s.k)}>{s.label}</button>
          ))}
        </div>
        <div className="br-status-seg">
          {SORTS.map((s) => (
            <button key={s.k} className={'chip' + (sort === s.k ? ' on' : '')}
              onClick={() => setSort(s.k)}>{s.label}</button>
          ))}
        </div>
      </div>

      {error ? <p className="br-error">拉取培育线失败:{error.message}</p> : null}

      {shown.length === 0 ? (
        <div className="empty">
          {lines.length === 0
            ? '还没有培育线。在游戏里孵一次家园蛋(破壳时自动开一条线并记一代),或用上面的「新建培育线」'
            : '没有符合筛选条件的培育线'}
        </div>
      ) : (
        <div className="br-grid">
          {shown.map((l) => (
            <LineCard key={l.id} line={l} chains={chains} matrix={natureMatrix} onOpen={openLine} />
          ))}
        </div>
      )}
    </div>
  )
}
