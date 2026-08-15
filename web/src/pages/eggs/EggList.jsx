import React, { useState, useEffect, useContext } from 'react'
import { getEggs, subscribe } from '../../api'
import { AccountContext } from '../../context'
import { imgURL } from '../../components/icons'
import { PetDetailModal } from '../../components/PetDetailModal'
import { fmtTime, pctHot, voiceHot } from '../../utils/format'
import { hatchProgress, HATCH_RATE_NOTE } from './hatch'

// 精灵蛋页面:背包里的蛋 + 家园收的蛋记下的双亲。
// 数据来自后端 eggs 表(见 internal/store/egg.go);孵化进度按「当前值 + 倍率 × 已过秒数」
// 本地外推(倍率由后端两次采样得出,加速活动期间会变,见 docs/data.md 3.6)。
const TABS = [
  { k: 'inBag', label: '背包中', state: '0' },
  { k: 'hatching', label: '孵化中', state: '0', hatching: '1' },
  { k: 'hatched', label: '已破壳', state: '1' },
]

const SORTS = [
  { k: 'obtained', label: '获得时间' },
  { k: 'weight', label: '体重百分位' },
  { k: 'height', label: '身高百分位' },
  { k: 'name', label: '名称' },
]

export default function EggList() {
  const account = useContext(AccountContext)
  const [tab, setTab] = useState('inBag')
  const [sort, setSort] = useState('obtained')
  const [order, setOrder] = useState('desc')
  const [search, setSearch] = useState('')
  const [data, setData] = useState({ eggs: [], counts: {} })
  const [detailGid, setDetailGid] = useState(null)
  const [now, setNow] = useState(() => Date.now())

  const t = TABS.find((x) => x.k === tab) || TABS[0]
  const load = () => getEggs({ state: t.state, hatching: t.hatching, search, sort, order })
    .then((d) => setData(d || { eggs: [], counts: {} })).catch(() => {})

  useEffect(() => { load() }, [account, tab, sort, order, search]) // eslint-disable-line react-hooks/exhaustive-deps
  // 后端在蛋有变动(收蛋/入孵/进度/破壳)时推 eggs,收到就重拉当前视图。
  useEffect(() => subscribe((m) => { if (m.type === 'eggs') load() }), [account, tab, sort, order, search]) // eslint-disable-line react-hooks/exhaustive-deps
  // 孵化进度随时间涨:秒级刷新即可(只在「孵化中」有意义)。
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(id)
  }, [])

  const counts = data.counts || {}
  return (
    <div className="eggs-page">
      <div className="eggs-bar">
        <div className="eggs-tabs">
          {TABS.map((x) => (
            <button key={x.k} className={'chip' + (tab === x.k ? ' on' : '')} onClick={() => setTab(x.k)}>
              {x.label} <span className="muted">{counts[x.k] ?? 0}</span>
            </button>
          ))}
        </div>
        <input className="input eggs-search" placeholder="搜索蛋名/物种" value={search}
          onChange={(e) => setSearch(e.target.value)} />
        <select className="select" value={sort} onChange={(e) => setSort(e.target.value)}>
          {SORTS.map((s) => <option key={s.k} value={s.k}>按{s.label}</option>)}
        </select>
        <button className="btn" onClick={() => setOrder((o) => (o === 'desc' ? 'asc' : 'desc'))}
          title="切换升序/降序">{order === 'desc' ? '↓ 降序' : '↑ 升序'}</button>
      </div>

      {data.eggs.length === 0 && (
        <div className="empty">暂无精灵蛋(需后端抓到背包全量:游戏内打开一次背包即可)</div>
      )}

      <div className="egg-grid">
        {data.eggs.map((e) => (
          <EggCard key={e.gid} egg={e} now={now} onPet={setDetailGid} />
        ))}
      </div>

      {tab === 'hatching' && data.eggs.length > 0 && (
        <p className="muted eggs-note">{HATCH_RATE_NOTE}</p>
      )}
      {detailGid != null && <PetDetailModal gid={detailGid} onClose={() => setDetailGid(null)} />}
    </div>
  )
}

// EggCard 一颗蛋:图标 + 名称 + 尺寸(值 + 百分位两行,与宠物列表同一口径)+ 获得时间,
// 在孵的另有进度条,家园收的另附双亲。
function EggCard({ egg, now, onPet }) {
  const p = hatchProgress(egg, now)
  return (
    <div className="egg-card">
      <div className="egg-head">
        <img className="egg-icon" src={imgURL(egg.icon)} alt="" draggable={false} />
        <div className="egg-title">
          <div className="egg-name">
            {egg.name}
            {egg.random && <span className="pill egg-pill">未知物种</span>}
            {egg.hatched && <span className="pill egg-pill">已破壳</span>}
          </div>
          <div className="muted egg-sub">
            {egg.species ? `孵出 ${egg.species}` : '孵出前无从得知是谁'}
            {egg.srcName ? ` · ${egg.srcName}` : ''}
          </div>
        </div>
        {egg.petImg && <img className="egg-pet" src={imgURL(egg.petImg)} alt="" draggable={false} />}
      </div>

      <div className="egg-grid2">
        <Field label="身高" value={`${egg.heightM} m`} pct={egg.heightPct} />
        <Field label="体重" value={`${egg.weightKg} kg`} pct={egg.weightPct} />
      </div>
      {(egg.adultHeightM > 0 || egg.adultWeightKg > 0) && (
        <div className="muted egg-adult" title="蛋的百分位在破壳时原样保留,故可提前算出成体尺寸(随机蛋不适用)">
          孵出后约 {egg.adultHeightM} m / {egg.adultWeightKg} kg
        </div>
      )}

      <div className="egg-rows">
        <div><span className="muted">获得</span> {fmtTime(egg.obtainedAt)}</div>
        {egg.hatched && egg.petGid > 0 && (
          <div>
            <span className="muted">破壳</span> {fmtTime(egg.hatchedAt)} ·{' '}
            <button className="linkish" onClick={() => onPet(egg.petGid)}>看孵出的宠物</button>
          </div>
        )}
      </div>

      {p && (
        <div className="egg-hatch">
          <div className="egg-bar"><div className="egg-bar-fill" style={{ width: p.pct + '%' }} /></div>
          <span className={p.pct >= 100 ? 'val-hot-hi' : undefined}>{p.pct >= 100 ? '可破壳' : p.pct + '%'}</span>
        </div>
      )}

      {egg.parents && <Parents p={egg.parents} onPet={onPet} />}
    </div>
  )
}

function Field({ label, value, pct }) {
  return (
    <div className="egg-field">
      <span className="muted">{label}</span>
      <b className={pctHot(pct)}>{value}</b>
      {pct != null && <span className={'egg-pct ' + (pctHot(pct) || '')}>{pct.toFixed(2)}%</span>}
    </div>
  )
}

// Parents 双亲:母本确定(蛋趴在她的窝上),父本取服务器下发的配对候选;
// 多个候选即「串窝」,实际父本无从确定(见 docs/data.md 3.6)。
function Parents({ p, onPet }) {
  const one = (x, role) => (
    <button key={role + x.gid} className="egg-parent" onClick={() => onPet(x.gid)}
      title="点击查看该亲本的宠物详情(若已放生则查无此宠,双亲快照仍留在这里)">
      {x.img ? <img src={imgURL(x.img)} alt="" draggable={false} /> : <span className="egg-parent-ph">🐾</span>}
      <span className="egg-parent-info">
        <span className="egg-parent-name">{role} {x.name}{x.gender ? ` ${x.gender}` : ''}</span>
        <span className="muted egg-parent-sub">
          {x.weightPct != null && <span className={pctHot(x.weightPct)}>体重 {x.weightPct.toFixed(1)}%</span>}
          {x.voice != null && <span className={voiceHot(x.voice)}> · 声音 {x.voice}</span>}
          {x.nature ? ` · ${x.nature}` : ''}
        </span>
      </span>
    </button>
  )
  return (
    <div className="egg-parents">
      <div className="muted egg-parents-t">
        双亲(收蛋时记下的快照)
        {p.ambiguous && <span className="pill egg-pill" title="几个小窝挨得太近会串窝,协议里没有实际父本的记录">串窝</span>}
      </div>
      {p.mother && one(p.mother, '母')}
      {(p.fathers || []).map((f) => one(f, '父'))}
    </div>
  )
}
