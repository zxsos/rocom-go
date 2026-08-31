import React, { useCallback, useContext, useEffect, useState } from 'react'
import { getTrial, subscribe } from '../../api'
import { AccountContext } from '../../context'
import { useAsyncData } from '../../hooks/useAsyncData'
import { ImgAvatar } from '../../components/icons'
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

// 每层结构(来自 wiki):1-3 层普通池、4 层首领池、5 层普通池、6 层无精灵(商人/魔力之源)、
// 7 层 NPC 阵容。试炼实际是「3 章 × 8 节点」,与这里的层数是两套口径,此处仅作参考注释,
// 不参与进度计算(进度仍按实测的每章 8 节点推)。

export default function Trial() {
  const account = useContext(AccountContext)
  const { data, setData } = useAsyncData(useCallback(() => getTrial(), []), { reloadKey: account })
  const [tab, setTab] = useState('run') // run | history

  useEffect(() => subscribe('trial', (d) => setData(d)), [setData])

  const run = data && data.run
  const history = data && data.history
  // 从没见过试炼报文:给一句明确的引导,而不是空页面
  if (!data) {
    return (
      <div className="trial-page">
        <div className="empty">尚未收到试炼数据:游戏内进入一次草系徽章试炼后自动显示…</div>
      </div>
    )
  }

  return (
    <div className="trial-page">
      <div className="toolbar">
        <h3 style={{ margin: 0 }}>草系试炼</h3>
        <span className="muted toolbar-hint">
          {data.active ? '正在同步游戏内的一局' : '当前没有进行中的一局'}
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
        </div>
      </div>

      {tab === 'run'
        ? (run
          ? <RunView run={run} active={data.active} />
          : <div className="empty">还没有进行过一局</div>)
        : (history
          ? <HistoryView history={history} />
          : <div className="empty">尚未收到账号档案(游戏内打开一次试炼面板后自动同步)</div>)}
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
          <span className="trial-chip" title={'章节 id ' + run.chapterId}>
            第 {run.chapterIdx || 1} 章 · {CHAPTER_NAMES[run.chapterId] || '未知章节'}
          </span>
          <span className="trial-chip">节点 {run.nodeIndex + 1}</span>
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
            {pet.features && pet.features.length > 0 && (
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
