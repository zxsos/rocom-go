import React, { useCallback, useContext, useEffect, useMemo, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import {
  claimBreedingChild, deleteBreeding, getBreeding, getEvolution, getFilterOptions, getNameOptions,
  getPet, mergeBreeding, saveBreeding, setNest, subscribe,
} from '../../api'
import { AccountContext } from '../../context'
import { breedableEggGroups } from '../../constants'
import { useAsyncData } from '../../hooks/useAsyncData'
import { PetDetailModal } from '../../components/PetDetailModal'
import PetLocateModal from '../../components/PetLocateModal'
import ComboSelect from '../../components/ComboSelect'
import { toast } from '../../components/toast'
import { IconBreeding, IconRefresh } from '../../components/svg'
import LineCard from './LineCard'
import LineDetail from './LineDetail'
import { GoalFields } from './GoalEditor'
import {
  chainKey, chainOf, chainOfPet, fatherCandidates, lineStats, narrowChainsByEggGroups, parseGoal,
  pendCounts, petPickerOption,
} from './pets'
import ParentSlot, { NestHint, ParentRow, parentCard } from './ParentSlot'
import useBreedPool from './useBreedPool'

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
  // 长按「孵蛋配种」带来的宠物(见 PetList.breedFrom):URL 里只有 gid,性别/蛋组/品种一律
  // 以库为准 —— 分享出去的链接在宠物进化/送人后仍会如实反映,而不是按过期快照配种。
  const breedGid = Number(params.get('new')) || 0

  const [q, setQ] = useState('')
  const [status, setStatus] = useState('')
  const [sort, setSort] = useState('updated')
  const [detailGid, setDetailGid] = useState(0)
  const [locGid, setLocGid] = useState(0) // 仓库定位弹窗的目标 gid(0=关闭)
  const [busy, setBusy] = useState(false)
  const [creating, setCreating] = useState(false)
  const [newChain, setNewChain] = useState('') // 选中品种的取值键(见 pets.chainKey)
  const [newGoal, setNewGoal] = useState(EMPTY_GOAL)
  // 长按「孵蛋配种」带过来的宠物本身与它那条进化链(后者用于认品种,见 pets.chainOfPet)
  const [breedPet, setBreedPet] = useState(null)
  const [breedSteps, setBreedSteps] = useState(null)
  // 建线时可选的一对亲本(gid 的字符串形式,与 PetPicker 的取值一致);空串 = 不选。
  const [newMotherGid, setNewMotherGid] = useState('')
  const [newFatherGid, setNewFatherGid] = useState('')
  // 建线时想给小窝的计划 (0 = 没设):此时线还没建,勾选**只改本地**,随建线请求一起提交成
  // line.nestPlanGid。刻意不在这里调 setNest —— 那会把整个账号的小窝真值改掉,而玩家此刻
  // 只是「这条线打算这样试算一下」,把它当成既成事实写下去是在替游戏改事实(且与后端家园
  // 管线维护的真值打架)。旁注:线建好之后,详情页的勾选才走 onSave(写计划值)。
  const [newNestPlanGid, setNewNestPlanGid] = useState(0)
  // 亲本被自动取消时的说明(见下面那个 effect):不说明的话玩家只会发现「我选的公没了」。
  const [parentNote, setParentNote] = useState('')
  // 长按预置只做一次:玩家把预置的那侧清掉后,重渲染不该又塞回来。
  const [prefilled, setPrefilled] = useState(false)

  // 「孵蛋配种」的落地:拉这只宠物的权威数据 → 打开新建表单,并按它的蛋组收窄品种候选。
  // 宠物已经不在库里(放生/送人/换了账号)时提示一句并退回普通列表 —— 不能让玩家对着一张
  // 永远配不出候选的表单去填目标。
  useEffect(() => {
    if (!breedGid) { setBreedPet(null); setBreedSteps(null); return }
    let alive = true
    getPet(breedGid).then(
      (p) => {
        if (!alive) return
        setBreedPet(p)
        setCreating(true)
        // 认品种要的是**链首**形态(候选里的 base),已进化的宠物只带当前形态,故再查一次它那条
        // 进化链(见 pets.chainOfPet)。查不到就不预选 —— 预选只是省一步,不预选也是正常流程。
        getEvolution(p.baseConfId).then((steps) => { if (alive) setBreedSteps(steps || []) }, () => {})
      },
      () => { if (alive) { toast('这只宠物已经不在库里了'); setParams({}) } },
    )
    return () => { alive = false }
    // 只在 breedGid 变化时重跑(含点「不限蛋组」把参数清空那一次)。
  }, [breedGid]) // eslint-disable-line react-hooks/exhaustive-deps

  const { data, loading, error, refresh } = useAsyncData(
    useCallback(() => getBreeding(), []), { fallback: { lines: [] }, reloadKey: account },
  )
  const lines = useMemo(() => (data && data.lines) || [], [data])
  // 学院小窝(全库唯一一只,随培育响应一起下发):{gid,name,nature},gid=0 表示空着。
  // 亲本卡上的勾选与建议卡片上的「性格 100%」都由它解释,故它必须与线**同一份响应**读出来 ——
  // 分开拉一次接口的话,刷新时机不同就会出现「卡上勾着、建议却按常规概率算」的中间态。
  const nest = useMemo(() => (data && data.nest) || { gid: 0 }, [data])
  // 建线表单里那两张卡的**生效值**:设了本地计划就按计划算,没设就按游戏真值 —— 与后端算建议
  // 时用的口径(NestPlanGid || nest.gid)一致,勾选状态才不会和建完线看到的百分比指两只宠物。
  const formNestEff = newNestPlanGid || (nest.gid || 0)
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
  // 长按「孵蛋配种」带来的蛋组条件:候选只列与它至少共一个蛋组的品种(见 pets.narrowChainsByEggGroups),
  // 并按 ♀ 的品种预选好(蛋随母本;♂ 不预选 —— 他是种公,得由玩家决定要哪条链的蛋)。
  // 限制只是**筛选候选**,不写进线里:建完线的品种仍是玩家在下拉里确认过的那一个。
  const breedEggGroups = useMemo(() => breedableEggGroups(breedPet && breedPet.eggGroups), [breedPet])
  const pickChains = useMemo(() => narrowChainsByEggGroups(chains, breedEggGroups), [chains, breedEggGroups])

  useEffect(() => {
    if (!breedPet || breedPet.gender !== '♀' || !breedSteps) return
    const c = chainOfPet(pickChains, breedSteps)
    if (c) setNewChain(chainKey(c))
  }, [breedPet, breedSteps, pickChains])

  // 长按的那只直接占住一侧:**蛋随母本**,故 ♀ 占「种母」位(她这条链由上面那步选成品种);
  // ♂ 占「种公」位 —— 要哪条链的蛋由玩家自己挑,链上的 ♀ 才是种母。
  // 只做一次(用标记而不是靠依赖):玩家把这一侧清掉之后重渲染,不该又被他塞回来。
  useEffect(() => {
    if (!breedPet || prefilled) return
    const gid = String(breedPet.gid)
    if (breedPet.gender === '♀') setNewMotherGid(gid)
    else setNewFatherGid(gid)
    setPrefilled(true)
  }, [breedPet, prefilled])

  const chainItems = useMemo(
    () => pickChains.map((c) => ({ value: chainKey(c), label: c.label, sub: `${c.count} 只`, img: c.egg || c.img })),
    [pickChains],
  )

  // —— 亲本(种母 / 种公)—— 都可空,任意组合(见 pet.BreedingLine 的 MotherGid / FatherGid)。
  //
  // 候选来自这个品种的候选池(见 useBreedPool):种母 = 同品种雌性;种公 = 与该品种雌性
  // 共蛋组的雄性,选定种母后再按**她**的蛋组收一次(见 fatherCandidates)—— 配种要求同蛋组,
  // 列一个配不上的公等于让玩家白建一条线。
  const pickedChain = useMemo(() => chainOf(newChain, pickChains), [newChain, pickChains])
  const pool = useBreedPool(pickedChain)
  const motherCand = useMemo(
    () => pool.mothers.find((p) => String(p.gid) === newMotherGid) || null,
    [pool.mothers, newMotherGid],
  )
  const fatherPool = useMemo(() => fatherCandidates(pool.fathers, motherCand), [pool.fathers, motherCand])
  // 长按带过来的那只:它可能还没进候选池(还没选品种、或它的蛋组与母本对不上),但**必须显示
  // 得出来** —— 玩家长按它才进来的,卡上却空着,他只会以为没选上。
  const presetCard = useMemo(() => {
    if (!breedPet) return null
    const gid = String(breedPet.gid)
    if (gid !== newMotherGid && gid !== newFatherGid) return null
    return parentCard(breedPet, breedableEggGroups(breedPet.eggGroups))
  }, [breedPet, newMotherGid, newFatherGid])
  const cardOf = useCallback(
    (gid, cand) => parentCard(cand) || (presetCard && String(presetCard.gid) === gid ? presetCard : null),
    [presetCard],
  )
  const fatherCand = useMemo(
    () => pool.fathers.find((p) => String(p.gid) === newFatherGid) || null,
    [pool.fathers, newFatherGid],
  )
  const motherCard = cardOf(newMotherGid, motherCand)
  const fatherCard = cardOf(newFatherGid, fatherCand)
  // 「你长按的那只」只标在长按带过来的那一侧:从列表长按进来时,得让人知道系统已经替他放好了哪一边。
  const longPressBadge = (gid) => (breedPet && gid && String(breedPet.gid) === gid ? '你长按的那只' : null)
  const motherOpts = useMemo(() => pool.mothers.map(petPickerOption), [pool.mothers])
  // 池子拿到之前,把长按预置的那位补进选项里(否则下拉显示不出他);池子拿到之后不补 ——
  // 那时「不在候选里」就是真的配不上,该由下面那个 effect 清掉并说明。
  // loading 也要看:换品种时 data 还留着**上一个品种**的池子,拿它判「配不上」会误清。
  const poolReady = pool.ready && !pool.loading
  const fatherOpts = useMemo(() => {
    const opts = fatherPool.map(petPickerOption)
    const p = !poolReady ? presetCard : null
    if (p && String(p.gid) === newFatherGid && !opts.some((o) => o.value === String(p.gid))) {
      opts.unshift({ value: String(p.gid), img: p.img, name: p.name, sub: p.species })
    }
    return opts
  }, [fatherPool, presetCard, newFatherGid, poolReady])
  // 选种母 → **顺手把品种(蛋)选好**:蛋的物种随母本,她那一条链就是这条线要孵的蛋。
  // 她在当前候选里对不上时(多半是长按带来的蛋组收窄把她的链挡在外面)先撤掉那个收窄 ——
  // 否则下拉里会显示一个候选列表里根本没有的品种,看起来像没选上。
  const pickMother = useCallback((gid) => {
    setNewMotherGid(gid)
    setParentNote('')
    if (!gid) return
    const c = pool.mothers.find((p) => String(p.gid) === gid)
    if (!c) return
    // 候选带上所属进化链(见 petCandidate 里的 Evo),故这里能直接把「这只母」翻成「哪条链」。
    const key = chainKey(c)
    if (!chainOf(key, chains)) {
      // 她所属的品种不在可配种清单里(生不出蛋的那批被后端滤掉了):留着她但说清为什么没填品种。
      setParentNote('这只种母所属的品种不在可配种清单里（生不出蛋的品种不参与配种）')
      return
    }
    if (!chainOf(key, pickChains)) setParams({})
    setNewChain(key)
  }, [pool.mothers, chains, pickChains, setParams])

  // 换品种 / 换种母之后,已选的那两位可能不再配得上:种母不属于这个品种(蛋的物种随母本,
  // 她必须与品种同种)、或种公与母本蛋组对不上。留着就会提交一对配不出候选的亲本,而界面上
  // 看不出来 —— 故清掉并说明一句。
  //
  // 只在池子**确实拿到过**之后才判(poolReady):换品种时 data 还留着上一个品种的池子,
  // 拿它判会误清;而没定品种时后端给的是全库雌雄,那两位本来就都在里面,不会误伤。
  useEffect(() => {
    if (!poolReady) return
    if (newMotherGid && !pool.mothers.some((p) => String(p.gid) === newMotherGid)) {
      setNewMotherGid('')
      setParentNote('换品种后原来那只种母不属于这个品种，已取消 —— 蛋随母本,也可以先选种母再来定品种')
      return
    }
    if (newFatherGid && !fatherOpts.some((o) => o.value === newFatherGid)) {
      setNewFatherGid('')
      setParentNote('这只种公与母本蛋组对不上，已取消')
    }
  }, [poolReady, newMotherGid, newFatherGid, pool.mothers, fatherOpts])

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

  // setNestTruth 把**游戏真值**手工兜底成某只(gid=0 = 空出来)。
  //
  // 正常情况真值由家园管线自动维护(见 docs/data.md 3.6),玩家不需要碰它;只在「还没抓到小窝
  // 数据、但确实已经放好了」时,用它把本线的计划一次性扶正成事实(详情页状态行那个
  // 「记为游戏真值」按钮)。走 run 的「提交 → 重拉 → 提示」,与别的写操作同一套路:这一改动的
  // 是**建议里的性格命中率**,而那是后端算的 —— 本地只改勾选状态的话卡片上的百分比会对不上。
  const setNestTruth = useCallback((gid) => run(() => setNest(gid), gid ? '已记为游戏真值' : '学院小窝已空出'), [run])

  // toggleNewNestPlan 建线表单里的计划勾选:只改本地,建线时随 nestPlanGid 一起提交(见上面那段注释)。
  // 详情页的勾选不走这里 —— 那时线已经存在,改计划就是整条覆盖写,由 LineDetail 自己 onSave。
  const toggleNewNestPlan = useCallback((gid) => setNewNestPlanGid(gid ? Number(gid) : 0), [])

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
    // 亲本两位都只提交**gid**(快照由服务端按当前库生成):线上存的是「指的是哪只」,
    // 之后他改名/换形态不会让线里带一份过期的快照 —— 与历史记录存快照的分工正相反(见 schemas.md)。
    saveBreeding({
      id, evo: picked.evo, species: picked.species, goal: parseGoal(newGoal), status: 'active', gens: [],
      // 只提交**候选里确实解析出来**的那两位:卡上显示为空(值不在候选里 —— 例如换品种后那位
      // 还没被清掉)时,把 state 里的 gid 原样提交会建出一条「母本与品种不符」的线,而界面上
      // 看着是空的。所见即所交。
      ...(motherCand ? { motherGid: motherCand.gid } : {}),
      ...(fatherCand ? { fatherGid: fatherCand.gid } : {}),
      // 小窝计划同样「为 0 就不放进 body」(与其他可选字段一致):后端把缺失与 0 都当「没设计划」,
      // 但少塞一个字段能让请求更像「玩家确实没打算改它」,也免得日后再加默认值时空 0 被误读。
      ...(newNestPlanGid ? { nestPlanGid: newNestPlanGid } : {}),
    })
      .then(() => refresh())
      .then(
        () => {
          setCreating(false); setNewChain(''); setNewGoal(EMPTY_GOAL)
          setNewMotherGid(''); setNewFatherGid(''); setParentNote(''); setNewNestPlanGid(0)
          openLine(id)
          // 同品种已经有进行中的线时多说一句。不拦 —— 同一个品种同时开几条线是正常用法
          // (几个窝、几只母本各孵各的,它们**不会**互相干扰:蛋按种母归线)。
          // 但要讲清这条新线此刻还没有种母,要等第一次收蛋才固定到某只母本身上 ——
          // 只在玩家**没选**种母时说:选了就是已经固定了,再说不就自相矛盾了。
          const same = lines.filter((l) => (l.status || 'active') === 'active'
            && chainKey(l) === newChain).length
          if (same > 0) {
            toast(`培育线已建立 —— 这个品种还有 ${same} 条进行中的线,它们各跟各的种母,互不干扰。`
              + (newMotherGid ? '' : '这条线还没固定种母,第一次收蛋时会按当时的母本固定下来'))
          } else {
            toast('培育线已建立 —— 填上目标后建议才有意义')
          }
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
            nest={nest} onNestTruth={setNestTruth}
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
          {/* 长按「孵蛋配种」进来的蛋组条件:候选被收窄了,这里必须**明说**是哪只宠物、哪几个
              蛋组、剩多少个品种,并给一键解除 —— 否则玩家只会觉得「品种列表怎么少了一大半」
              (下拉里既看不出被筛过,也没有痕迹说明是按什么筛的)。 */}
          {breedEggGroups.length ? (
            <p className="br-hint muted">
              已按蛋组收窄：只列与 <b>{breedPet.name || breedPet.species}</b> 共蛋组（{breedEggGroups.join(' / ')}）的品种，
              共 {pickChains.length} 个{breedPet.gender === '♀' ? '；已把她这条进化链填进下拉' : ''}。
              <button className="btn small" onClick={() => setParams({})}>不限蛋组</button>
            </p>
          ) : null}
          <p className="br-hint muted">
            一个选项就是一条进化链(括号里是它的各阶段),左边的图是这条线要孵的蛋 ——
            链上任一阶段的 ♀ 都能当这条线的种母,而同一只精灵的两种样子各有各的链(也各有各的蛋)。
            生不出蛋的特殊精灵(迪莫、翼王那一系)不在这里 —— 它们进不了小窝配种。
          </p>

          {/* 亲本(种母 × 种公):都可空、任意组合 —— 都不选就沿用「先建线,第一次收蛋时按当时的
              母本固定种母」那套老用法(见 pet.BreedingLine.MotherGid/FatherGid)。
              **顺序不设限**:品种还没定也能先挑亲本(那时后端给的是全库的雌雄,见 BreedCandidates
              的空引用那一支);定了品种之后再按品种收窄。 */}
          <div className="br-field">
            <span>亲本（可不填）</span>
            <ParentRow
              mother={(
                <ParentSlot
                  role="mother" card={motherCard} options={motherOpts} value={newMotherGid}
                  badge={longPressBadge(newMotherGid)}
                  nestGid={formNestEff} nestTruth={nest.gid || 0} nestPlanGid={newNestPlanGid} onNest={toggleNewNestPlan}
                  title="种母决定这条线的身份与品种:她孵的蛋会记到这条线上。选了她,上面的品种(蛋)会自动跟着填好"
                  onChange={pickMother}
                />
              )}
              father={(
                <ParentSlot
                  role="father" card={fatherCard} options={fatherOpts} value={newFatherGid}
                  badge={longPressBadge(newFatherGid)}
                  nestGid={formNestEff} nestTruth={nest.gid || 0} nestPlanGid={newNestPlanGid} onNest={toggleNewNestPlan}
                  title="种公是计划值:抓包抓到的真实父本仍按当时的记录写进各代,这里选的只是这条线打算用谁"
                  onChange={(gid) => { setNewFatherGid(gid); setParentNote('') }}
                />
              )}
            />
            {/* 建线这一步还没有线,故这里给的是「真值 + 本地计划」,计划写不写下去由建线决定。
                状态行也据此显示:目标只是让玩家看到勾了这一下会怎么算。 */}
            <NestHint truth={nest} planGid={newNestPlanGid} planReleased={false}
              onClearPlan={() => setNewNestPlanGid(0)} />
            <p className="br-hint muted">
              {pickedChain
                ? `种母 ${pool.mothers.length} 只可选 · 与母本共蛋组的种公 ${fatherOpts.length} 只可选`
                : `也可以先选种母 —— 品种(蛋)会随她自动填好 · 全库 ${pool.mothers.length} 只雌性可选`}
            </p>
            {/* 勾选在新线上只记下「打算」,百分比得等线建出来、建议面板算完才有 —— 不先说明,
                玩家勾完会以为立刻该看到 100%。 */}
            <p className="br-hint muted">
              勾选只记下这条线的计划（试算用）；百分比在建线后的建议面板里看。
              它不会改动游戏里的学院小窝 —— 要真放进去得在游戏里放/抱走。
            </p>
            {parentNote ? <p className="br-hint muted">{parentNote}</p> : null}
          </div>

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
