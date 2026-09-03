import React, { useCallback, useContext, useEffect, useState } from 'react'
import { getTrial, getTrialEncounters, subscribe } from '../../api'
import { AccountContext } from '../../context'
import { useAsyncData } from '../../hooks/useAsyncData'
import { ImgAvatar, imgURL } from '../../components/icons'
import { confirmDialog } from '../../components/confirm'
import { fmtTime } from '../../utils/format'
import { Marks } from '../../components/badges'
import ElementWheel from './ElementWheel'

// 草系徽章试炼页:实时同步游戏内的一局。
//
// 数据来源与更新时机(全部被动,靠抓包,不碰客户端):
//   - 载入时 GET /api/trial 回显后端缓存的最近一份快照;
//   - 之后由 SSE 的 trial 消息整份覆盖(服务器每次下发都是全量,前端不做增量合并);
//   - 页面只做展示,不向游戏发任何指令。
//
// 命名来源:技能名与效果文案来自官方 SKILL_CONF(gen_skill_official.py 落表,
// 后端随包下发 names/descs,协议 id 是 base_skill_id 也能直接查);特性只有 288xxx
// id —— wiki「精灵→特性」表桥接(精灵天生的那条,pet.featureNames)能给名,
// 其余一律按 id 原样展示 —— 编一个名字比留 id 更糟。

// 草系徽章试炼的语义映射(来自 BWIKI「草系徽章试炼」攻略页,仅名称,无协议 id):
//   - 三章主题名:服务器只下发 chapter_id(3000/3001/3002),名字见 wiki。
//   - 三档难度:trial_conf_id 10000/10001/10002 对应 基础/进阶1/进阶2。
// 放前端而非 names.json:这些是 wiki 内容而非游戏解包数据,且只有 6 个条目,硬编码即可。
const CHAPTER_NAMES = {
  3000: '记忆中的索米亚草原',
  3001: '记忆中的巨石阵',
  3002: '记忆中的普拉塔草原',
}
const TRIAL_NAMES = {
  10000: '草系徽章试炼',
  10001: '进阶 1',
  10002: '进阶 2',
}

// 层结构已由后端下发(run.floor / run.floorLabel),见 gamedata/trial.go。
// 要点:wiki 说每章 7 层,协议是 8 个节点(node_index 0~7)—— node_index 0 是章节
// 起点(无战斗),1~7 依次对应 wiki 的 1~7 层。这个映射是抓包实测出来的
// (node_index 6 无战斗、node_index 7 对手是 NPC),不是照抄 wiki,别改成 7 段。

export default function Trial() {
  const account = useContext(AccountContext)
  const { data, setData } = useAsyncData(useCallback(() => getTrial(), []), { reloadKey: account })
  const [tab, setTab] = useState('run') // run | history | encounters

  // 遇见记录是**累积历史**(读库,不经 SSE),且只在切到该页时才需要 ——
  // 三章近 800 只精灵,随本局状态一起加载纯属浪费。故按需拉取,
  // 换账号时清掉重来。
  const [enc, setEnc] = useState(null)
  // 加一个自增序号当重拉信号:清空记录后要让页面重新取数,而 tab/account 都没变,
  // 光靠依赖数组触发不了 —— 用这个「世代」把刷新意图显式传下去。
  const [encGen, setEncGen] = useState(0)
  useEffect(() => {
    setEnc(null)
    if (tab !== 'encounters') return
    let alive = true
    getTrialEncounters().then((d) => { if (alive) setEnc(d) })
    return () => { alive = false }
  }, [tab, account, encGen])

  // 打完一场就重拉一次:管线记下新的遇会后广播 trial_enc(只发信号不带数据),
  // 这里收到便让上面那个 effect 再跑一遍。
  //
  // 合并 400ms 内的连续触发:一场战斗可能带多只精灵、紧接着又进下一场,
  // 每次都拉 786 条纯属浪费。合一下,视觉上也更稳(不会连闪)。
  useEffect(() => {
    let t = 0
    // 断线重连时补拉一次:SSE 断开期间的消息全丢了,不补就会一直显示旧数据。
    const un = subscribe('trial_enc', () => {
      clearTimeout(t)
      t = setTimeout(() => setEncGen((n) => n + 1), 400)
    }, { onOpen: () => setEncGen((n) => n + 1) })
    return () => { clearTimeout(t); un() }
  }, [])

  useEffect(() => subscribe('trial', (d) => setData(d)), [setData])

  const run = data && data.run
  const history = data && data.history

  // 从没见过试炼报文时**不整页拦掉**:「遇见记录」是累积历史、读库的,
  // 哪怕此刻没在打也该能翻。故空态下移到各 tab 内部判断。
  //
  // ⚠️ 代价:整页范围内**不能再无条件访问 data 的属性** —— 原本拦在这里的
  // `if (!data) return` 一删,`data` 为 null(接口返回 null)时任何 `data.x`
  // 都会让整页白屏。下面这行就栽过一次(data.active → TypeError,页面进不去)。
  // 判空一律用可选链;只有 run/history 为真的分支里才能安全地直取 data。
  return (
    <div className="trial-page">
      <div className="toolbar">
        <h3 style={{ margin: 0 }}>草系试炼</h3>
        <span className="muted toolbar-hint">
          {data?.active ? '正在同步游戏内的一局' : '当前没有进行中的一局'}
        </span>
        <div className="spacer" />
        <div className="trial-tabs">
          <button className={'trial-tab' + (tab === 'run' ? ' on' : '')} onClick={() => setTab('run')}>
            本局
          </button>
          <button
            className={'trial-tab' + (tab === 'history' ? ' on' : '')}
            onClick={() => setTab('history')}
          >
            档案{history ? ` (${history.wins}/${history.total})` : ''}
          </button>
          <button
            className={'trial-tab' + (tab === 'encounters' ? ' on' : '')}
            onClick={() => setTab('encounters')}
          >
            遇见记录
          </button>
        </div>
      </div>

      {tab === 'run'
        ? (run
          ? <RunView run={run} active={data.active} />
          : <div className="empty">还没有进行过一局(游戏内进入一次草系徽章试炼后自动同步)</div>)
        : tab === 'history'
          ? (history
            ? <HistoryView history={history} />
            : <div className="empty">尚未收到账号档案(游戏内打开一次试炼面板后自动同步)</div>)
          : <EncountersView data={enc} onReload={() => setEncGen((n) => n + 1)} />}
    </div>
  )
}

// EncountersView 是「遇见记录」:三章各一张精灵图,遇到过的置灰。
//
// 两个设计点:
//   - **每章独立**:同一只精灵在第 1 章遇到过,第 2 章的图里仍算未遇见 ——
//     与 wiki 口径一致(页面注明「3 章首领按章节独立计算」),三张图的进度才各自真实。
//   - **置灰而非高亮**:这是「待办清单」语义 —— 还差哪些没刷到要一眼看得见,
//     故未遇见的保持原色、已遇见的压暗。wiki 那边反着来(高亮已遇见)是因为它
//     主要用于「我刷到了什么」,这里更关心「还差什么」。
//
// 数据来自客户端官方配置(精灵池,gen_trial_official.py 生成)+ 抓包记录(遇到情况)。
// 顶部标注数据源与更新时间。
function EncountersView({ data, onReload }) {
  const [ch, setCh] = useState(1)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')

  if (!data) return <div className="empty">加载中…</div>
  const books = data.chapters || []
  if (!books.length) {
    return (
      <div className="empty">
        没有试炼精灵池数据(需先跑 scripts/gen_trial_official.py 生成 internal/gamedata/data/trial.json)
      </div>
    )
  }
  const cur = books.find((b) => b.chapter === ch) || books[0]
  // 后端已保底给空数组,这里再兜一层:任一侧将来改动都不会让整页白屏。
  // 普通池为空时分组整个不渲染 —— 空标题 + 空网格没有意义。
  const normal = cur.normal || []
  const boss = cur.boss || []
  // 池外的实战遭遇(NPC 战 / 最终 BOSS):静态配置没有这两层的精灵池,
  // 这些遭遇无处安放,单列一组展示 —— 丢掉的话用户明明打过照面却显示未遇见。
  // 不计入进度(分母是池子大小,塞进来源不明的条目会让百分比失去意义)。
  const extra = cur.extra || []
  const pct = cur.total > 0 ? Math.round((cur.seen / cur.total) * 100) : 0
  return (
    <div>
      {data.updated && (
        <div className="muted trial-note" title={data.source}>
          章节/普通池/首领来自客户端官方配置 · {data.updated}
          {data.activity ? <span> · 当前活动:{data.activity}</span> : null}
        </div>
      )}
      <div className="trial-tabs">
        {books.map((b) => (
          <button
            key={b.chapter}
            className={'trial-tab' + (b.chapter === cur.chapter ? ' on' : '')}
            onClick={() => setCh(b.chapter)}
          >
            第{b.chapter}章 ({b.seen}/{b.total})
          </button>
        ))}
      </div>
      {cur.image && (
        <img className="trial-banner" src={imgURL(cur.image)} alt={cur.name} loading="lazy" />
      )}
      <div className="trial-enc-head">
        <button
          className="btn trial-enc-refresh"
          onClick={onReload}
          disabled={busy}
          title="重新读取遇见记录"
        >
          {busy ? '…' : '刷新'}
        </button>
        <b>{cur.name || `第${cur.chapter}章`}</b>
        <span className="muted">已遇见 {cur.seen}/{cur.total}</span>
        <div className="trial-enc-bar">
          <i style={{ width: pct + '%' }} />
        </div>
        <span className="muted">{pct}%</span>
      </div>
      {cur.intro && <p className="trial-intro">{cur.intro}</p>}
      {err && <div className="trial-enc-err">{err}</div>}
      {normal.length > 0 && (
        <section className="trial-group">
          <h4 className="trial-group-t">普通池({normal.length})——第 1/2/3/5 层</h4>
          <PetGrid pets={normal} />
        </section>
      )}
      {boss.length > 0 && (
        <section className="trial-group">
          <h4 className="trial-group-t">首领({boss.length})——第 4 层,三章共用</h4>
          <PetGrid pets={boss} />
        </section>
      )}
      {extra.length > 0 && (
        <section className="trial-group">
          <h4 className="trial-group-t">其他遭遇({extra.length})</h4>
          <div className="muted trial-note">
            遇到过、但不在上面两组池子里(NPC 战 / 最终 BOSS 等)。静态配置只有普通池与
            22 名首领,没有第 7 层的精灵池,故单列;不计入上方进度。
          </div>
          <PetGrid pets={extra} />
        </section>
      )}
    </div>
  )
}

// PetGrid 一图精灵:遇到过的置灰。
function PetGrid({ pets }) {
  return (
    <div className="trial-grid">
      {pets.map((p) => (
        <div
          key={p.base}
          className={'trial-grid-item' + (p.seen ? ' is-seen' : '')}
          title={p.name + (p.seen && p.time ? ` · ${fmtTime(p.time)}` : '')}
        >
          <ImgAvatar src={p.img} alt={p.name} className="trial-grid-img" />
          <span className="trial-grid-name">{p.name || p.base}</span>
        </div>
      ))}
    </div>
  )
}

// RunView 展示一局:进度条 + 试炼宠物 + 当前节点/祝福/奖励/商店 + 操作流水。
function RunView({ run, active }) {
  const pet = run.pet
  const chapters = run.chapters || []
  // 章节进度:chapters 是服务器给的可选章节(如 3000/3001/3002),chapterIdx 为第几章
  const perChapter = 8 // 每章 8 个节点(实测 3 章 × 8 节点)
  const doneNodes = chapters.length
    ? (run.chapterIdx - 1) * perChapter + run.nodeIndex + (active ? 0 : 1)
    : 0
  const totalNodes = chapters.length ? chapters.length * perChapter : perChapter

  return (
    <>
      <section className="trial-progress">
        <div className="trial-progress-head">
          <span className={'trial-dot' + (active ? ' on' : '')} />
          <strong>{active ? '进行中' : '已结束'}</strong>
          {run.slotName && <span className="trial-chip">{run.slotName}</span>}
          <span className="trial-chip" title={'难度 id ' + run.trialId}>
            {TRIAL_NAMES[run.trialId] || ('难度 ' + run.trialId)}
          </span>
          {/* 章节名优先用后端的(来自静态配置),硬编码表只作兜底 */}
          <span className="trial-chip" title={'章节 id ' + run.chapterId}>
            第 {run.chapterIdx || 1} 章 ·{' '}
            {run.chapterName || CHAPTER_NAMES[run.chapterId] || '未知章节'}
          </span>
          <span className="trial-chip">节点 {run.nodeIndex + 1}</span>
          {/* 层类型来自静态配置(wiki):这一层是普通/首领/商人还是 NPC。
              协议只给 node_index,「这一层是什么」查表才知道。缺数据时整块不显示。 */}
          {run.floorLabel && (
            <span
              className={'trial-chip trial-floor trial-floor-' + run.floor}
              title={`第 ${run.nodeIndex + 1} 个节点 · ${run.floor}`}
            >
              {run.floorLabel}
            </span>
          )}
          <span className="trial-chip trial-coin" title="试炼金币">🪙 {run.coin}</span>
          {run.boss && <span className="trial-chip trial-boss">BOSS</span>}
        </div>
        <div className="trial-bar" title={`${doneNodes}/${totalNodes}`}>
          <div className="trial-bar-fill" style={{ width: `${Math.min(100, (doneNodes / totalNodes) * 100)}%` }} />
        </div>
        {run.effects && run.effects.length > 0 && (
          <div className="trial-meta">
            <span className="muted">本周词条</span>
            {/* 词条只给 effect_id 时是「1001/1008/…」,后端把官方名字一并下发
                (effectNames),如 1001=融合次数 —— 只看编号看不出词条在改什么规则 */}
            {run.effects.map((e) => (
              <span key={e} className="trial-chip" title={`词条 effect_id ${e}`}>
                {(run.effectNames && run.effectNames[e]) || e}
              </span>
            ))}
          </div>
        )}
      </section>

      {run.result && <ResultCard result={run.result} />}

      {pet && <PetCard pet={pet} />}

      {run.options && run.options.length > 0 && (
        <section className="trial-group">
          <h4 className="trial-group-t">当前节点({run.options.length} 个事件)</h4>
          <div className="trial-opts">
            {run.options.map((o) => <OptionCard key={o.slot} o={o} />)}
          </div>
          {run.refreshCost > 0 && (
            <div className="muted trial-note">本节点刷新已花费 {run.refreshCost} 金币</div>
          )}
        </section>
      )}

      {run.opponents && run.opponents.length > 0 && (
        <section className="trial-group">
          <h4 className="trial-group-t">
            第 7 层 NPC 候选({run.opponents.length} 套阵容)
          </h4>
          {/* wiki 的 opponent id 与协议 npc_id 不是同一套编号,无从锁定当前遭遇到底
              是哪一个 —— 这里是候选池,措辞上不能说成「对面就是这几只」。 */}
          <div className="muted trial-note">
            进入战斗前只能列出候选;实际遭遇以游戏内为准
          </div>
          <div className="trial-opps">
            {run.opponents.map((o) => <OpponentCard key={o.id} o={o} />)}
          </div>
        </section>
      )}

      {run.bless && (
        <section className="trial-group">
          <h4 className="trial-group-t">祝福(事件 {run.bless.event})</h4>
          <div className="trial-meta">
            <span className="muted">选项</span>
            {run.bless.options.map((o) => <span key={o} className="trial-chip">{o}</span>)}
            {run.bless.effect !== undefined && (
              <span className="trial-chip trial-bless">
                {run.bless.effect === 9 ? '合并技能' : run.bless.effect === 0 ? '选择技能' : `效果 ${run.bless.effect}`}
              </span>
            )}
          </div>
          {run.bless.candidates && run.bless.candidates.length > 0 && (
            <div className="trial-meta">
              <span className="muted">候选技能</span>
              {run.bless.candidates.map((c) => <span key={c} className="trial-chip">{c}</span>)}
            </div>
          )}
        </section>
      )}

      {run.reward && (
        <section className="trial-group">
          <h4 className="trial-group-t">待处理奖励</h4>
          <div className="trial-meta">
            <span className="muted">事件 {run.reward.event}</span>
            {/* 奖励的名由后端给(name:技能/碎片效果查得到时),查不到回退 kind+id;
                技能类主奖励带 desc(官方效果文案),悬停即看它是干嘛的 */}
            <span
              className="trial-chip trial-reward"
              title={run.reward.desc ? `效果:${run.reward.desc} · reward_id ${run.reward.id}` : `reward_id ${run.reward.id}`}
            >
              {run.reward.name || `${rewardKind(run.reward.id)} ${run.reward.id}`}
            </span>
            {run.reward.extra && run.reward.extra.map((x) => {
              const en = (run.reward.names && run.reward.names[x]) || ''
              return (
                <span key={x} className="trial-chip" title={en ? `额外 reward_id ${x}` : ''}>
                  额外 {en || `${rewardKind(x)} ${x}`}
                </span>
              )
            })}
          </div>
        </section>
      )}

      {run.shop && run.shop.length > 0 && (
        <section className="trial-group">
          <h4 className="trial-group-t">商店</h4>
          <div className="trial-shop">
            {run.shop.map((s) => (
              <div key={s.index} className={'trial-card' + (s.bought ? ' trial-card-bought' : '')}>
                <div className="trial-card-t">{shopKind(s.type)}</div>
                <div className="trial-card-id">{s.id}</div>
                <div className="trial-card-meta">
                  <span>🪙 {s.price}</span>
                  {s.bought && <span className="muted">已购</span>}
                </div>
              </div>
            ))}
          </div>
        </section>
      )}

      {run.log && run.log.length > 0 && (
        <section className="trial-group">
          <h4 className="trial-group-t">操作流水</h4>
          <ul className="trial-log">
            {run.log.map((e, i) => (
              <li key={i} className="trial-log-item">
                <span className="trial-log-ts">{fmtTime(e.ts)}</span>
                <span className={'trial-log-kind trial-log-' + e.kind}>{e.label}</span>
                <LogDetail e={e} />
              </li>
            ))}
          </ul>
        </section>
      )}
    </>
  )
}

// LogDetail 渲染流水条目的数字部分(各类字段含义不同,逐类给中文标签)。
function LogDetail({ e }) {
  if (!e.ids || e.ids.length === 0) {
    return e.coin ? <span className="muted">剩 {e.coin}</span> : null
  }
  switch (e.kind) {
    case 'node':
      return <span className="muted">第 {chapterNo(e.ids[0])} 章 节点 {e.ids[1] + 1}</span>
    case 'refresh':
      return <span className="muted">类型 {e.ids[0]} · 剩 {e.coin}</span>
    case 'reward':
      // 后端把这条奖励里查得到名字的 id 补在 e.name 里;查不到(特性等)才回退 kind+id
      return <span className="muted">{e.name || `${rewardKind(e.ids[0])} ${e.ids[0] || '—'}`}</span>
    case 'shop':
      return <span className="muted">{e.name || e.ids[0]}{e.coin ? ` · 剩 ${e.coin}` : ''}</span>
    case 'bless':
      // bless 的 ids 语义随 effect 变:选技能时是 [effect, 技能],合并时只有 effect。
      // label 已把 effect 说清(选择技能/合并技能),这里只展示那个技能
      // (名字由后端查好,查不到仍是原始 id),effect 编号不再重复打一遍。
      if (e.ids.length < 2) return null
      return <span className="muted">{e.name || e.ids[1]}</span>
    case 'settle':
      return <span className="muted">用时 {fmtDuration(e.ids[1])}</span>
    default:
      return <span className="muted">{e.ids.join(' / ')}</span>
  }
}

// ResultCard 展示上一局结算。
function ResultCard({ result }) {
  return (
    <section className={'trial-result' + (result.victory ? ' win' : ' lose')}>
      <strong>{result.victory ? '通关' : '未通关'}</strong>
      <span className="muted">用时 {fmtDuration(result.duration)}</span>
      {result.petLevel > 0 && <span className="muted">Lv {result.petLevel}</span>}
      {result.score > 0 && <span className="muted">得分 {result.score}</span>}
      <span className="muted">{fmtTime(result.settleAt)}</span>
    </section>
  )
}

// PetCard 展示试炼宠物副本。
// OpponentCard 是第 7 层的一个候选 NPC 阵容:标题是 NPC 名,下面是它带的精灵。
// 每只精灵带名字与头像 —— 都由后端补齐(头像并非每个形态都有,缺图时 img 为空,
// ImgAvatar 会自己占位,这里不用管)。
function OpponentCard({ o }) {
  return (
    <div className="trial-opp">
      <div className="trial-opp-name">
        {o.name}
        <span className="muted trial-opp-id"> #{o.id}</span>
      </div>
      <div className="trial-opp-pets">
        {(o.pets || []).map((p) => (
          <div key={p.base} className="trial-opp-pet" title={p.name || String(p.base)}>
            <ImgAvatar src={p.img} alt={p.name} className="trial-opp-img" />
            <span>{p.name || p.base}</span>
          </div>
        ))}
      </div>
    </div>
  )
}

function PetCard({ pet }) {
  const pct = pet.maxHp > 0 ? Math.round((pet.hp / pet.maxHp) * 100) : 0
  // 特性在协议里只有 id —— 没有内置名表(不像技能有 skills.json)。
  // 名字来源:wiki「精灵 → 特性」表桥接(pet.featureNames,这只精灵天生的那条),
  // 开局就能带上;其余 288xxx 只能按 id 展示 —— 编一个名字比留 id 更糟。
  const featureChip = (f) => {
    const wiki = (pet.featureNames || {})[String(f)]
    if (wiki) {
      return (
        <span
          key={f}
          className="trial-chip trial-feat-wiki"
          title={`${wiki}(wiki 精灵→特性表:按「${pet.name || pet.species}」反查,未经 id 校验)`}
        >
          {wiki} ⁓
        </span>
      )
    }
    return <span key={f} className="trial-chip" title={`未知特性 id ${f}`}>{f}</span>
  }
  return (
    <section className="trial-group">
      <h4 className="trial-group-t">试炼宠物</h4>
      <div className="trial-pet">
        <ImgAvatar src={pet.img} alt={pet.name} className="trial-pet-img" />
        <div className="trial-pet-info">
          <div className="trial-pet-name">
            {pet.name || pet.species || '未知'}
            {pet.species && pet.species !== pet.name && <span className="muted"> · {pet.species}</span>}
            {/* 异色/炫彩:试炼带的是玩家**自己的**精灵,外观原样带进去。
                标记只显示不解释来源 —— 它来自协议 mutation_type,不是猜的。
                ⚠️ 别拿 pet.img 反推异色:异色头像不是每只精灵都有素材,
                没素材时后端静默回退普通图(此时 shiny 仍为 true)。
                以 img 判断会把这些精灵全当成普通的。 */}
            <Marks p={pet} />
          </div>
          <div className="trial-meta">
            <span className="trial-chip">Lv {pet.level}</span>
            <span className="trial-chip">成长 {pet.growth}</span>
            <span className="trial-chip">能量 {pet.energy}</span>
          </div>
          <div className="trial-hp" title={`${pet.hp} / ${pet.maxHp}`}>
            <div className="trial-hp-fill" style={{ width: `${pct}%` }} />
            <span className="trial-hp-t">{pet.hp} / {pet.maxHp}</span>
          </div>
        </div>
      </div>

      {pet.skills && pet.skills.length > 0 && (
        <div className="trial-skills">
          {pet.skills.map((s) => (
            <div key={s.slot} className={'trial-skill' + (pet.equipped && pet.equipped.includes(s.slot) ? ' on' : '')}>
              <div className="trial-skill-head">
                <span className="trial-skill-slot">槽 {s.slot}</span>
                {/* 技能名由后端随包下发(s.name,查官方 SKILL_CONF,融合不改 id 也有名);
                    官方表外的才按 id 展示。 */}
                {(() => {
                  if (s.name) {
                    // 悬停给官方效果文案(下挂 desc 行与之互补:行内一眼看得到)
                    return <span className="trial-skill-id" title={s.desc ? `效果:${s.desc} · 技能 id ${s.id}` : `技能 id ${s.id}`}>{s.name}</span>
                  }
                  return <span className="trial-skill-id" title={`技能 id ${s.id}`}>{s.id}</span>
                })()}
              </div>
              {/* 官方效果文案常驻一行(小灰字),不悬停也能看懂这技能是干嘛的;
                  desc 与 name 同源同键,官方表内技能必有,查不到就没有这行 */}
              {s.desc && <div className="trial-skill-desc">{s.desc}</div>}
              <div className="trial-skill-meta">
                <span title="融合后威力">威力 {s.power}</span>
                <span title="融合后能耗">能耗 {s.cost}</span>
                {s.fusion > 0 && <span className="trial-skill-fusion" title="融合次数">融合 ×{s.fusion}</span>}
              </div>
              {s.merged && s.merged.length > 0 && (
                <div className="trial-skill-merged" title="被融合进来的技能(融合次数在上方)">
                  + {s.merged.map((m) => (s.mergedNames || {})[String(m)] || m).join(' , ')}
                </div>
              )}
            </div>
          ))}
        </div>
      )}

      {(pet.features && pet.features.length > 0) || (pet.shards && pet.shards.length > 0)
        ? (
          <div className="trial-meta">
            {/* 特性只有 id:游戏特性名表未接入(不像技能那样有 skills.json)。
                能拿到天生/获得的拆分时分开显示(局级 initial_feature_ids,
                整局不变;与已获特性之差就是试炼中拿到的,见 trial.InitialFeatures);
                拿不到就退回不区分的展示。 */}
            {pet.innateFeatures || pet.gainedFeatures
              ? (
                <>
                  {pet.innateFeatures && pet.innateFeatures.length > 0 && (
                    <>
                      <span className="muted" title="宠物自带的特性">特性·天生</span>
                      {pet.innateFeatures.map(featureChip)}
                    </>
                  )}
                  {pet.gainedFeatures && pet.gainedFeatures.length > 0 && (
                    <>
                      <span className="muted" title="本局试炼中获得的特性">特性·获得</span>
                      {pet.gainedFeatures.map(featureChip)}
                    </>
                  )}
                </>
              )
              : (
                <>
                  <span className="muted">特性</span>
                  {[...new Set(pet.features)].map(featureChip)}
                </>
              )}
            {pet.shards && pet.shards.length > 0 && (
              <>
                <span className="muted">碎片</span>
                {[...new Set(pet.shards)].map((s) => <span key={s} className="trial-chip">{s}</span>)}
              </>
            )}
          </div>
        )
        : null}
    </section>
  )
}

// IdChip 渲染一个协议 id:查得到名字就显示名字,查不到就原样展示 id。
//
// 名字来源按 id 分段:
//   技能(7xxxxx)—— 后端 names 来自官方 SKILL_CONF(与协议同源,含试炼 788 段);
//   特性(288xxx)—— 没有内置名表,名字只来自后端桥接出的 o.names
//                   (有精灵可查时按「精灵→特性」表带上精灵天生的那条);
//   碎片/效果(20xx/30xx)—— 官方 GRASS_TRIAL_EFFECT_CONF 有名字(如 2016=魔攻特调),
//                   后端带在 names 里。
//
// used 标出**本节点已经抽过**的:重掷时服务器排除它们,压暗显示能让人一眼看出
// 「换奖励还剩下哪些可能」。desc 是后端随包下发的官方技能效果文案(仅技能 id),
// 悬停即看「这技能是干嘛的」。
function IdChip({ id, name, used, current, desc }) {
  const isFeat = isFeatureID(id)
  const isSkill = isSkillID(id)
  const text = name
  return (
    <span
      className={'trial-chip trial-id' + (used ? ' trial-id-used' : '') +
        (isFeat ? ' trial-id-feat' : '') + (current ? ' trial-id-cur' : '')}
      title={[
        text ? text : `未知${isFeat ? '特性' : isSkill ? '技能' : 'id'} ${id}`,
        desc ? `效果:${desc}` : '',
        current ? '当前抽到的(与上方「技能」一致)' : '',
        used ? '本节点已抽过,重掷不会再出' : '',
      ].filter(Boolean).join(' · ')}
    >
      {text || id}
    </span>
  )
}

// OptionCard 是当前节点的一个候选事件。
//
// 版面照游戏内的样子排:
//
//	┌────────────────────────────────┐
//	│ ┌────────────────────────────┐ │
//	│ │          (头像)        🧩  │ │  整体方框:头像居中,右上角挂额外碎片
//	│ │          奇丽花            │ │
//	│ │     技能 触底强击           │ │
//	│ │        Lv 40               │ │
//	│ │  抽取池 5 个 id             │ │
//	│ │        换奖励 🪙 2          │ │
//	│ └────────────────────────────┘ │
//	│          换事件 🪙 1            │  最下方:换的是整只精灵
//	└────────────────────────────────┘
//
// 两种重掷都花金币但换的东西不同,故分开摆:换奖励只在这只精灵的 5 选 1 里重抽,
// 换事件连精灵一起换掉(抽取池随之变成新精灵的那一套)—— 它不属于"这只精灵"
// 的内容,放在方框外,免得被读成"这只精灵的一项"。
function OptionCard({ o }) {
  // 精灵:后端按官方 GRASS_TRIAL_EVENT_CONF 回填(带头像);官方表外的事件
  // (NPC 阵容/祝福/商人等)没有单只精灵可映射 —— 其中商人/魔力之源这类有官方
  // 事件名(o.eventName),头像位就显示它;真的查不到名字才退回「事件 {id}」占位。
  const petName = (o.pet && o.pet.name) || ''
  const petImg = (o.pet && o.pet.img) || ''
  const pool = o.pool || []
  const used = new Set(o.used || [])
  const name = (id) => (o.names || {})[String(id)]
  // 效果文案:后端只给技能类 id 带(见 TrialOption.Descs),特性/碎片查不到。
  const desc = (id) => (o.descs || {})[String(id)]
  return (
    <div className="trial-opt">
      <div className="trial-opt-box">
        <div className="trial-opt-pet">
          {/* 头像右上角挂额外奖励(多是碎片):它是挂在**这个事件**上的,
              与抽取池里的奖励不是一回事,故单独成角标而非混进池子。 */}
          <div className="trial-opt-avatar">
            <ImgAvatar src={petImg} alt={petName} className="trial-opt-img" />
            {o.extra && o.extra.length > 0 && (
              <span
                className="trial-opt-extra"
                title={`额外奖励:${o.extra.map((x) =>
                  `${name(x) || (rewardKind(x) + ' ' + x)}${desc(x) ? `:${desc(x)}` : ''}`
                ).join('、')}`}
              >
                +{o.extra.length}
              </span>
            )}
          </div>
          <div className="trial-opt-petname">
            {petName || o.eventName || <span className="trial-chip" title={`未知事件 id ${o.event}`}>事件 {o.event}</span>}
          </div>
        </div>

        {/* reward=0(魔力之源这类无奖事件)时整行隐藏 —— 没有可抽的东西,露个
            「0」只会让人以为抽到了什么 */}
        {o.reward > 0 && (
          <div className="trial-opt-reward">
            <span className="muted trial-opt-kind">{rewardKind(o.reward)}</span>
            {name(o.reward)
              ? <b className="trial-opt-name" title={desc(o.reward) ? `效果:${desc(o.reward)}` : ''}>{name(o.reward)}</b>
              : <IdChip id={o.reward} desc={desc(o.reward)} />}
          </div>
        )}

        <div className="trial-opt-meta">
          {o.level > 0 && <span>Lv {o.level}</span>}
          <span className="trial-opt-cost" title="在这只精灵的抽取池里重抽一个">
            换奖励 🪙 {o.rewardCost || 0}
          </span>
        </div>

        {pool.length > 0
          ? (
            <div className="trial-opt-pool">
              <span className="muted trial-opt-kind">抽取池 {pool.length}</span>
              <div className="trial-opt-ids">
                {pool.map((id) => (
                  // current:池里这一条就是当前抽到的(与上方「技能」是同一个 id),
                  // 圈出来免得看着像「两个不同的奖励」。
                  <IdChip key={id} id={id} name={name(id)} desc={desc(id)} used={used.has(id)} current={id === o.reward} />
                ))}
              </div>
            </div>
          )
          : <div className="muted trial-note">协议未下发抽取池(random_skills 为空)</div>}
      </div>

      <div className="trial-opt-swap" title="换掉整只精灵,抽取池随之换成新精灵的一套">
        换事件 🪙 {o.eventCost || 0}
        {/* 槽位行:优先给可读名 —— 事件映射到精灵就显示精灵名(豆丁鱼),特殊事件
            (商人/魔力之源等)显示后端下发的 eventName;两者都没有才退回裸事件 id。
            原始 event_conf_id 保留在 title 里,方便对照抓包。 */}
        <span className="muted trial-opt-slot" title={`event_conf_id ${o.event}`}>
          槽 {o.slot} · {o.eventName || (o.pet && o.pet.name) || ('事件 ' + o.event)}
        </span>
      </div>
    </div>
  )
}

// HistoryView 展示账号档案:胜率、常用形态、各系进度、见闻录、最近战绩。
function HistoryView({ history }) {
  const rate = history.total > 0 ? Math.round((history.wins / history.total) * 100) : 0
  return (
    <>
      <section className="trial-group">
        <h4 className="trial-group-t">战绩</h4>
        <div className="trial-meta">
          <span className="trial-chip">累计 {history.challengeInc} 次</span>
          <span className="trial-chip">通关 {history.wins} / {history.total}</span>
          <span className="trial-chip">胜率 {rate}%</span>
          {history.cleared && history.cleared.length > 0 && (
            <span className="trial-chip">已通难度 {history.cleared.join(' / ')}</span>
          )}
        </div>
      </section>

      {history.topPets && history.topPets.length > 0 && (
        <section className="trial-group">
          <h4 className="trial-group-t">常用形态</h4>
          <div className="trial-pets">
            {history.topPets.map((t) => (
              <div key={t.petBaseId} className="trial-pet-mini" title={`${t.name} · ${t.count} 次`}>
                <ImgAvatar src={t.img} alt={t.name} className="trial-pet-mini-img" />
                <span className="trial-pet-mini-n">{t.name || t.petBaseId}</span>
                <span className="muted">{t.count}</span>
              </div>
            ))}
          </div>
        </section>
      )}

      {history.slots && history.slots.length > 0 && (
        <section className="trial-group">
          <h4 className="trial-group-t">各系通关(每个系 3 个难度)</h4>
          {/* theme 是本页徽章所属的属性系:星盘会把草系转到正上方并加冕,
              中心印记也显示草系的进度。将来做火系/水系徽章页时改这一个值即可。 */}
          <ElementWheel slots={history.slots} theme="草" />
        </section>
      )}

      {history.logs && history.logs.length > 0 && (
        <section className="trial-group">
          <h4 className="trial-group-t">见闻录</h4>
          <div className="trial-meta">
            {history.logs.map((l) => (
              <span key={l.logConfId} className="trial-chip">
                第 {l.logConfId - 99} 册 {l.discovered}/{l.total}
              </span>
            ))}
          </div>
        </section>
      )}

      {history.recent && history.recent.length > 0 && (
        <section className="trial-group">
          <h4 className="trial-group-t">最近战绩</h4>
          <ul className="trial-log">
            {history.recent.map((r, i) => (
              <li key={i} className="trial-log-item">
                <span className="trial-log-ts">{fmtTime(r.settleAt)}</span>
                <span className={'trial-log-kind ' + (r.victory ? 'trial-log-win' : 'trial-log-lose')}>
                  {r.victory ? '通关' : '失败'}
                </span>
                <span className="muted">
                  {r.petName || r.petBaseId} · Lv {r.petLevel} · {TRIAL_NAMES[r.trialId] || ('难度 ' + r.trialId)} · 用时 {fmtDuration(r.duration)}
                </span>
              </li>
            ))}
          </ul>
        </section>
      )}
    </>
  )
}

// 三类 id 的**区间判据**:技能 7xxxxx / 特性 288xxx / 碎片 20xx-30xx。
// 协议不带类型字段,只能按区间分,判据来自 docs/pcap-20260831-grass-trial.md 2.1
// (逐条对照 updated_pet 的落点),与后端 trial.IsFeatureID 是同一套 —— 改一处要改两处。
const isSkillID = (id) => id >= 7000000
const isFeatureID = (id) => id >= 288000 && id < 289000

// rewardKind 按 id 区间判断奖励类别。
function rewardKind(id) {
  if (!id) return ''
  if (isSkillID(id)) return '技能'
  if (isFeatureID(id)) return '特性'
  if (id >= 2000 && id <= 3999) return '碎片'
  return '奖励'
}

// shopKind 商店商品类型:2=特性 3=碎片(实测仅见这两种)。
const shopKind = (t) => (t === 2 ? '特性' : t === 3 ? '碎片' : `类型 ${t}`)

// chapterNo 把服务器下发的 chapter_id(3000/3001/3002)换成第几章。
const chapterNo = (id) => (id >= 3000 ? id - 3000 + 1 : id)

// fmtDuration 把秒数渲染成「12 分 34 秒」。
function fmtDuration(sec) {
  if (!sec && sec !== 0) return '—'
  const m = Math.floor(sec / 60)
  const s = sec % 60
  return m > 0 ? `${m} 分 ${s} 秒` : `${s} 秒`
}
