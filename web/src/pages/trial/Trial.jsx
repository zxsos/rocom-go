import React, { useCallback, useContext, useEffect, useState } from 'react'
import { clearTrialEncounters, getTrial, getTrialEncounters, subscribe } from '../../api'
import { AccountContext } from '../../context'
import { useAsyncData } from '../../hooks/useAsyncData'
import { ImgAvatar } from '../../components/icons'
import { confirmDialog } from '../../components/confirm'
import { fmtTime } from '../../utils/format'

// 草系徽章试炼页:实时同步游戏内的一局。
//
// 数据来源与更新时机(全部被动,靠抓包,不碰客户端):
//   - 载入时 GET /api/trial 回显后端缓存的最近一份快照;
//   - 之后由 SSE 的 trial 消息整份覆盖(服务器每次下发都是全量,前端不做增量合并);
//   - 页面只做展示,不向游戏发任何指令。
//
// 技能/特性/碎片**只有 id**:游戏技能名表尚未接入本项目(names.json 里没有,
// 见 docs/data.md「待校准」),故一律按 id 原样展示 —— 编一个名字比留 id 更糟。

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
// 数据来自第三方 wiki(精灵池)+ 抓包记录(遇到情况),故顶部标注更新时间。
function EncountersView({ data, onReload }) {
  const [ch, setCh] = useState(1)
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')

  // 清空记录:精灵池是第三方 wiki 的静态配置,游戏换版本换池子后旧记录会对不上
  // (里头有当前版本根本遇不到的 petbase,进度永远停在某个不满的数)。
  // 不可恢复,故走 confirmDialog 二次确认,且**只清当前这一章** —— 全清的入口
  // 不放到界面上,真需要时用 curl,免得误触毁掉三章的累积。
  const clear = async () => {
    const name = cur ? cur.name || `第${cur.chapter}章` : ''
    if (
      !await confirmDialog({
        message: `确认清空${name}的遇见记录?该章已遇见的 ${cur ? cur.seen : 0} 只会全部变为未遇见,不可恢复。`,
        okText: '清空', danger: true,
      })
    ) return
    setBusy(true); setErr('')
    try {
      await clearTrialEncounters(cur.chapter)
      onReload()
    } catch (e) {
      setErr(e.message || '清空失败')
    } finally {
      setBusy(false)
    }
  }

  if (!data) return <div className="empty">加载中…</div>
  const books = data.chapters || []
  if (!books.length) {
    return (
      <div className="empty">
        没有试炼精灵池数据(需先跑 scripts/fetch_trial_data.py 与 gen_trial.py)
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
        <div className="muted trial-note">精灵池来自 wiki,{data.updated},可能与当前版本有出入</div>
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
        <button
          className="btn trial-enc-clear"
          onClick={clear}
          disabled={busy || cur.seen === 0}
          title={cur.seen === 0 ? '本章还没有遇见记录,无需清空' : '清空本章遇见记录(不可恢复)'}
        >
          {busy ? '清空中…' : '清空本章'}
        </button>
      </div>
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
            {run.effects.map((e) => <span key={e} className="trial-chip">{e}</span>)}
          </div>
        )}
      </section>

      {run.result && <ResultCard result={run.result} />}

      {pet && <PetCard pet={pet} />}

      {run.options && run.options.length > 0 && (
        <section className="trial-group">
          <h4 className="trial-group-t">当前节点({run.options.length} 个事件)</h4>
          <div className="trial-grid">
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
            <span className="trial-chip trial-reward">{rewardKind(run.reward.id)} {run.reward.id}</span>
            {run.reward.extra && run.reward.extra.map((x) => (
              <span key={x} className="trial-chip">额外 {rewardKind(x)} {x}</span>
            ))}
          </div>
        </section>
      )}

      {run.shop && run.shop.length > 0 && (
        <section className="trial-group">
          <h4 className="trial-group-t">商店</h4>
          <div className="trial-grid">
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
      return <span className="muted">{rewardKind(e.ids[0])} {e.ids[0] || '—'}</span>
    case 'shop':
      return <span className="muted">{e.ids[0]}{e.coin ? ` · 剩 ${e.coin}` : ''}</span>
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
  return (
    <section className="trial-group">
      <h4 className="trial-group-t">试炼宠物</h4>
      <div className="trial-pet">
        <ImgAvatar src={pet.img} alt={pet.name} className="trial-pet-img" />
        <div className="trial-pet-info">
          <div className="trial-pet-name">
            {pet.name || pet.species || '未知'}
            {pet.species && pet.species !== pet.name && <span className="muted"> · {pet.species}</span>}
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
                {/* 技能名按 id 查(融合不改 id,故融合态也有名);
                    资料站未收录的新技能查不到,回退显示 id */}
                <span className="trial-skill-id" title={`技能 id ${s.id}`}>
                  {s.name || s.id}
                </span>
              </div>
              <div className="trial-skill-meta">
                <span title="融合后威力">威力 {s.power}</span>
                <span title="融合后能耗">能耗 {s.cost}</span>
                {s.fusion > 0 && <span className="trial-skill-fusion" title="融合次数">融合 ×{s.fusion}</span>}
              </div>
              {s.merged && s.merged.length > 0 && (
                <div className="trial-skill-merged" title="被融合进来的技能">
                  + {s.merged.join(' , ')}
                </div>
              )}
            </div>
          ))}
        </div>
      )}

      {(pet.features && pet.features.length > 0) || (pet.shards && pet.shards.length > 0)
        ? (
          <div className="trial-meta">
            {/* 特性只有 id:游戏特性名表未接入(不像技能那样有 skills.json),
                故一律按 id 展示 —— 编一个名字比留 id 更糟。
                能拿到天生/获得的拆分时分开显示(局级 initial_feature_ids,
                整局不变;与已获特性之差就是试炼中拿到的,见 trial.InitialFeatures);
                拿不到就退回不区分的展示。 */}
            {pet.innateFeatures || pet.gainedFeatures
              ? (
                <>
                  {pet.innateFeatures && pet.innateFeatures.length > 0 && (
                    <>
                      <span className="muted" title="宠物自带的特性">特性·天生</span>
                      {pet.innateFeatures.map((f) => (
                        <span key={f} className="trial-chip trial-feat-innate">{f}</span>
                      ))}
                    </>
                  )}
                  {pet.gainedFeatures && pet.gainedFeatures.length > 0 && (
                    <>
                      <span className="muted" title="本局试炼中获得的特性">特性·获得</span>
                      {pet.gainedFeatures.map((f) => (
                        <span key={f} className="trial-chip trial-feat-gained">{f}</span>
                      ))}
                    </>
                  )}
                </>
              )
              : (
                <>
                  <span className="muted">特性</span>
                  {[...new Set(pet.features)].map((f) => <span key={f} className="trial-chip">{f}</span>)}
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

// OptionCard 是当前节点的一个候选事件。
function OptionCard({ o }) {
  return (
    <div className="trial-card">
      <div className="trial-card-t">槽 {o.slot} · 事件 {o.event}</div>
      <div className="trial-card-id">{rewardKind(o.reward)} {o.reward}</div>
      <div className="trial-card-meta">
        {o.level > 0 && <span>Lv {o.level}</span>}
        {o.eventCost > 0 && <span className="muted">换事件 {o.eventCost}</span>}
        {o.rewardCost > 0 && <span className="muted">换奖励 {o.rewardCost}</span>}
      </div>
      {o.extra && o.extra.length > 0 && (
        <div className="trial-card-meta">
          <span className="muted">额外 {o.extra.map((x) => rewardKind(x)).join(' / ')}</span>
        </div>
      )}
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
          <div className="trial-slots">
            {history.slots.map((s) => (
              <div key={s.slotId} className={'trial-slot' + (s.cleared >= 3 ? ' full' : '')}>
                <span className="trial-slot-n">{s.damName || s.damType}</span>
                <span className="trial-slot-c">{s.cleared}/3</span>
              </div>
            ))}
          </div>
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

// rewardKind 按 id 区间判断奖励类别:技能 7xxxxx / 特性 288xxx / 碎片 20xx-30xx。
// 判据来自 docs/pcap-20260831-grass-trial.md 2.1(逐条对照 updated_pet 的落点)。
function rewardKind(id) {
  if (!id) return ''
  if (id >= 7000000) return '技能'
  if (id >= 288000 && id < 289000) return '特性'
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
