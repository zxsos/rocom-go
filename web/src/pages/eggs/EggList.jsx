import React, { useState, useEffect, useContext, useRef, useCallback } from 'react'
import { getEggs, subscribe, queryEggMatch } from '../../api'
import { AccountContext } from '../../context'
import { imgURL } from '../../components/icons'
import { PetDetailModal } from '../../components/PetDetailModal'
import { fmtTime, pad2, pctHot, voiceHot } from '../../utils/format'
import { useInterval } from '../../hooks/useAsyncData'
import { Marks } from '../../components/badges'
import { hatchProgress } from './hatch'
import { toast } from '../../components/toast'

// 精灵蛋页面:两段垂直 —— 孵蛋器(在孵且进度未满的蛋)、仓库(其余蛋)。不分标签页——在孵的蛋
// 本来就不出现在背包格子里(BagModuleData.IsRemoveEggItem)。
//
// 此前还有第三段「已孵化蛋」(标记在孵但进度满),那是给历史 bug 打的补丁:服务器取出孵蛋器
// 不清 start_hatch_time,背包全量/登录(0x1344)会把已取出的蛋重新标成在孵,而进度字段被清零
// 后前端外推瞬间顶满,于是按「在孵且进度满」把它们圈出来隔离。15648bf 改用登录/开孵蛋器时
// 的权威 egg_gid 判定在孵后,残留标记不再产生,该栏随之作废,进度满的蛋也一并不再显示
// (孵化倍率是本地估的,见 hatch.js,拿它当「可破壳」的依据并不可靠;且孵蛋器上限 3 格,
// 孵满了玩家会立刻点掉,不会在页面上看它挂着)。
// 排序复刻游戏内背包的两种(见 docs/data.md 3.6 与 internal/pet.SortEggs)。
const SORTS = [
  { k: 'quality', label: '品质' },
  { k: 'obtained', label: '获取时间' },
]

// 孵蛋器格子数:游戏内固定 3 格(游戏内上限,不随等级/道具变)。
const HATCH_SLOTS = 3

// 部分异色形态的蛋配置名自带「的蛋」,后端模板({0}的蛋)再拼一层就成了「XX的蛋的蛋」,
// 这里只规整结尾的重复(中间的「的蛋」是名字本身,不动)。
const tidyEggName = (name) => (name || '').replace(/的蛋的蛋$/, '的蛋')

// 预计完成时间:按当前估算孵化倍率(见 hatch.js)外推剩余秒数,换算成时间点。
// 同一天只显时分(手机双列卡片宽度紧张),跨天补「月-日 时:分」;title 里给剩余时长,
// 并注明是估算(倍率本身是估的)。
function etaText(egg, p, now) {
  const remainSecs = Math.max(0, egg.maxSecs - p.secs)
  const eta = new Date(now + remainSecs * 1000)
  const hm = `${pad2(eta.getHours())}:${pad2(eta.getMinutes())}`
  return new Date(now).toDateString() === eta.toDateString()
    ? hm
    : `${eta.getMonth() + 1}-${eta.getDate()} ${hm}`
}
function etaTitle(egg, p) {
  const s = Math.max(0, egg.maxSecs - p.secs)
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  return `按当前孵化倍率估算,约剩 ${h > 0 ? `${h} 小时 ${m} 分` : `${m} 分钟`}`
}

// 随机蛋「猜猜孵出谁」查询缓存:同一组身高/体重结果相同,按 `height|weight` 为 key 存
// localStorage,刷新页面不丢、重复查询直接复用,少烧第三方 token(限流 10 次/分钟)。
const EGG_GUESS_CACHE_KEY = 'eggGuessCache.v1'
const EGG_GUESS_CACHE_MAX = 30 // 最多缓存 30 组,超出按最旧淘汰(LRU)

function readEggGuessCache(key) {
  try {
    const c = JSON.parse(localStorage.getItem(EGG_GUESS_CACHE_KEY) || '{}')
    return c[key] && c[key].data ? c[key].data : null
  } catch { return null }
}

function writeEggGuessCache(key, data) {
  try {
    const c = JSON.parse(localStorage.getItem(EGG_GUESS_CACHE_KEY) || '{}')
    c[key] = { data, ts: Date.now() }
    const keys = Object.keys(c)
    if (keys.length > EGG_GUESS_CACHE_MAX) {
      // 删最旧的,直到回到上限
      keys.sort((a, b) => (c[a].ts || 0) - (c[b].ts || 0))
      keys.slice(0, keys.length - EGG_GUESS_CACHE_MAX).forEach((k) => delete c[k])
    }
    localStorage.setItem(EGG_GUESS_CACHE_KEY, JSON.stringify(c))
  } catch { /* 存满/隐私模式等忽略 */ }
}

// 孵蛋器状态只由后端 0x0312 对账维护(见 docs/data.md 3.6),故把在孵的蛋缓存到
// localStorage(按账号隔离):打开页面先用缓存顶住孵蛋器栏,再等后端推送刷新——
// 后端 hatching 列没变时,缓存就是最后一次 0x0312 的权威结果。
const hatchKey = (account) => `hatch:${account}`
const readHatchCache = (key) => {
  try {
    const v = JSON.parse(localStorage.getItem(key))
    return Array.isArray(v) && v.length ? v : null
  } catch { return null }
}
const writeHatchCache = (key, eggs) => {
  try { localStorage.setItem(key, JSON.stringify(eggs)) } catch { /* 配额等异常忽略 */ }
}

export default function EggList() {
  const account = useContext(AccountContext)
  const [sort, setSort] = useState('quality')
  const [order, setOrder] = useState('desc')
  const [search, setSearch] = useState('')
  // 初始先用缓存的孵蛋器蛋(可能为空),load 拉回全量后替换;搜索会过滤在孵蛋,
  // 故只有无搜索时才写缓存,免得把过滤后的不完整列表存进去。
  const [data, setData] = useState(() => {
    const cached = readHatchCache(hatchKey(account))
    return cached ? { eggs: cached } : { eggs: [] }
  })
  const [detailGid, setDetailGid] = useState(null)
  const [now, setNow] = useState(() => Date.now())

  const load = useCallback(() => getEggs({ search, sort, order })
    .then((d) => {
      d = d || { eggs: [] }
      if (!search) writeHatchCache(hatchKey(account), d.eggs.filter((e) => e.hatching))
      setData(d)
    }).catch(() => {}), [account, search, sort, order])

  useEffect(() => { load() }, [load])
  // 后端在蛋有变动(收蛋/入孵/进度/破壳)时推 eggs,收到就重拉。
  useEffect(() => subscribe('eggs', load), [load])
  // 孵化进度随时间涨:秒级刷新即可。
  useInterval(() => setNow(Date.now()), 1000)

  // 两段分类:孵蛋器=标记在孵且进度未满的蛋(保持后端槽位序);仓库=其余(后端已排好序,原样保留)。
  // 进度满的蛋两处都不显示(见文件头说明)。
  const withP = data.eggs.map((e) => ({ e, p: hatchProgress(e, now) }))
  const incubating = withP.filter((x) => x.e.hatching && (x.p == null || x.p.pct < 100)).map((x) => x.e)
  const bag = withP.filter((x) => !x.e.hatching).map((x) => x.e)
  const slots = HATCH_SLOTS

  return (
    <div className="eggs-page">
      <div className="eggs-cols">
        <IncuTitle n={incubating.length} slots={slots} />
        {/* 空格子只在宽屏画出来,手机上由 CSS 收起(见 eggs.css) */}
        <aside className="eggs-incu">
          {Array.from({ length: slots }, (_, i) => incubating[i]).map((e, i) => (
            e ? <EggCard key={e.gid} egg={e} now={now} onPet={setDetailGid} />
              : <div key={'s' + i} className="egg-slot-empty">空格子</div>
          ))}
        </aside>

        <div className="eggs-bar">
          <div className="eggs-col-t">仓库 <span className="muted">{bag.length} 颗</span></div>
          <div className="eggs-sorts">
            {SORTS.map((s) => (
              <button key={s.k} className={'chip' + (sort === s.k ? ' on' : '')}
                onClick={() => setSort(s.k)}>{s.label}</button>
            ))}
            <button className="btn" onClick={() => setOrder((o) => (o === 'desc' ? 'asc' : 'desc'))}
              title="反向排序(同游戏内那个箭头)">{order === 'desc' ? '↓' : '↑'}</button>
          </div>
          <input className="input eggs-search" placeholder="搜索蛋名/物种" value={search}
            onChange={(e) => setSearch(e.target.value)} />
        </div>

        <section className="eggs-bag">
          {bag.length === 0
            ? <div className="empty">仓库里没有精灵蛋(需后端抓到背包全量:游戏内打开一次背包即可)</div>
            : (
              <div className="egg-grid">
                {bag.map((e) => <EggCard key={e.gid} egg={e} now={now} onPet={setDetailGid} />)}
              </div>
            )}
        </section>
      </div>
      {detailGid != null && <PetDetailModal gid={detailGid} onClose={() => setDetailGid(null)} />}
    </div>
  )
}

// IncuTitle 孵蛋器标题:「孵蛋器 n/3」+ 提示图标。图标比数字小;
// 点击弹出半透明气泡,说明在孵口径(后端权威快照)与进度是本地外推的估算。点气泡外关闭。
function IncuTitle({ n, slots }) {
  const ref = useRef(null)
  const [tip, setTip] = useState(false)
  useEffect(() => {
    if (!tip) return
    const onDoc = (e) => { if (ref.current && !ref.current.contains(e.target)) setTip(false) }
    document.addEventListener('click', onDoc)
    return () => document.removeEventListener('click', onDoc)
  }, [tip])
  return (
    <div className="eggs-col-t">
      孵蛋器 <span className="muted">{n}/{slots}</span>
      <span ref={ref} className="incu-tip">
        <img className="incu-tip-ic" src="/ps.svg" alt="?" title="在孵按权威快照判定,进度是本地外推的估算"
          onClick={() => setTip((t) => !t)} draggable={false} />
        {tip && <span className="incu-tip-bubble">在孵按后端登录 / 开孵蛋器时的权威快照判定;进度条是本地外推的估算</span>}
      </span>
    </div>
  )
}

// EggCard 一颗蛋。布局固定(缺什么都留位置,免得同一行的卡片高低不齐):
//   [蛋图] 名称 / 奖牌标签        [品类角标][孵出物种头像]
//   重量 / 声音 / 高度 / 时间
//   (在孵才有的进度条)
//   双亲两行(非家园蛋留占位)
function EggCard({ egg, now, onPet }) {
  const p = hatchProgress(egg, now)
  const src = egg.srcName ? `来源:${egg.srcName}` : ''
  const name = tidyEggName(egg.name)
  // 随机蛋(神奇的蛋)的「猜猜孵出谁」:后端代理第三方图鉴 API(令牌在服务端,不进浏览器)。
  // 查询结果按身高体重缓存(localStorage):已查过又刷新页面时,初始化直接恢复缓存结果,
  // 不用再点「猜猜孵出谁」重新请求第三方。
  const guessKey = [egg.heightM, egg.weightKg].map((v) => v ?? '').join('|')
  const [match, setMatch] = useState(() => {
    if (!egg.random) return null
    const hit = readEggGuessCache(guessKey)
    return hit ? { loading: false, error: '', data: hit } : null
  }) // {loading,error,data}
  const query = () => {
    const hit = readEggGuessCache(guessKey)
    if (hit) { // 同身高体重查过,直接复用缓存,不再请求第三方
      setMatch({ loading: false, error: '', data: hit })
      return
    }
    setMatch({ loading: true, error: '', data: null })
    queryEggMatch(egg.heightM, egg.weightKg)
      .then((d) => { writeEggGuessCache(guessKey, d); setMatch({ loading: false, error: '', data: d }) })
      .catch((e) => {
        const msg = e.message || '查询失败'
        if (/429|请求过于频繁/.test(msg)) {
          // 限流:不占卡片位置,弹自制 toast 提醒(不阻塞页面)
          setMatch(null)
          toast('喂喂喂,当我Token不要钱吗,等会再查啊魂淡')
        } else {
          setMatch({ loading: false, error: msg, data: null })
        }
      })
  }
  return (
    <div className="egg-card">
      <div className="egg-head">
        <img className="egg-icon" src={imgURL(egg.icon)} alt="" draggable={false} />
        <div className="egg-title">
          <div className="egg-name" title={[name, egg.species && `孵出 ${egg.species}`, src]
            .filter(Boolean).join(' · ')}>{name}</div>
          <div className="egg-tags">
            {(egg.medals || []).map((m) => (
              <span key={m.dim} className="egg-chip" title={`${DIM_NAME[m.dim] || ''}奖牌`}>{m.name}</span>
            ))}
          </div>
        </div>
        <div className="egg-marks">
          {/* 异色/炫彩蛋用全站统一的那两个标记(同宠物列表),其余品类(珍贵/唯一/噩梦…)
              用游戏自己的蛋品类角标;普通蛋什么都不画(卡片高度由右边的头像撑着,不会塌)。 */}
          {egg.shiny || egg.colorful
            ? <span className="egg-type" title={egg.typeName}><Marks p={egg} chip={false} /></span>
            : egg.typeIcon
              ? <img className="egg-type" src={imgURL(egg.typeIcon)} alt="" title={egg.typeName} draggable={false} />
              : null}
          {egg.petImg
            ? <img className="egg-pet" src={imgURL(egg.petImg)} alt="" title={egg.species} draggable={false} />
            : <span className="egg-pet ph" title="孵出前无从得知是谁">?</span>}
        </div>
      </div>

      <div className="egg-rows">
        <Row k="重量" v={egg.weightKg ? `${egg.weightKg} kg` : ''} pct={egg.weightPct}
          title={egg.adultWeightKg ? `孵出后约 ${egg.adultWeightKg} kg(百分位破壳后原样保留)` : ''} />
        <Row k="声音" v={voiceText(egg)} hot={egg.voice != null && egg.voiceMax == null ? voiceHot(egg.voice) : ''}
          title={egg.voice != null
            ? '按双亲嗓音均值向下取整推出(蛋上没有嗓音字段)' + (egg.voiceMax != null ? ',串窝父本不唯一故给区间' : '')
            : '蛋上没有嗓音字段,双亲也没记下,破壳才知道'} />
        <Row k="高度" v={egg.heightM ? `${egg.heightM} m` : ''} pct={egg.heightPct}
          title={egg.adultHeightM ? `孵出后约 ${egg.adultHeightM} m(百分位破壳后原样保留)` : ''} />
        <Row k="时间" v={timeNode(egg.obtainedAt)} title={`获得时间 ${fmtTime(egg.obtainedAt)}`} />
      </div>

      {p && (
        <div className="egg-hatch">
          <div className="egg-bar"><div className="egg-bar-fill" style={{ width: p.pct + '%' }} /></div>
          <span className={p.pct >= 100 ? 'val-hot-hi' : undefined}>{p.pct >= 100 ? '可破壳' : p.pct + '%'}</span>
          {p.pct < 100 && <span className="egg-eta" title={etaTitle(egg, p)}>预计 {etaText(egg, p, now)}</span>}
        </div>
      )}

      {egg.random && (
        <div className="egg-guess">
          {match ? (
            <div className="egg-guess-res">
              {match.loading ? <div className="muted egg-guess-line">查询中…</div>
                : match.error ? (
                  <div className="egg-guess-line err">
                    <span>{match.error}</span>
                    <button className="btn" onClick={() => setMatch(null)}>关闭</button>
                  </div>
                ) : match.data.data.total > 0 ? (
                  <>
                    <div className="muted egg-guess-line">
                      匹配 {match.data.data.total} 条,来源 示例玩家
                    </div>
                    <div className="egg-guess-list">
                      {[...(match.data.data.matches || [])]
                        .sort((a, b) => (b.score || 0) - (a.score || 0))
                        .map((m) => (
                        <div key={m.pet_id} className="egg-guess-item">
                          {/^https?:\/\//.test(m.img_name || '')
                            ? <img className="egg-guess-img" src={m.img_name} alt="" loading="lazy" draggable={false} /> : null}
                          <div className="egg-guess-txt">
                            <div className="egg-guess-name">{m.pet_name}
                              <span className="muted"> {[m.main_type, m.sub_type].filter(Boolean).join('/')}</span>
                            </div>
                            <div className="muted">匹配度 {m.score} · {m.hatch_label}</div>
                          </div>
                        </div>
                      ))}
                    </div>
                    <button className="btn" onClick={() => setMatch(null)}>关闭</button>
                  </>
                ) : (
                  <div className="egg-guess-line err">
                    <span>找不到这颗臭蛋</span>
                    <button className="btn" onClick={() => setMatch(null)}>关闭</button>
                  </div>
                )}
            </div>
          ) : (
            <button className="btn egg-guess-btn" onClick={query}
              title="按蛋的身高/体重查第三方图鉴,猜可能孵出谁">猜猜孵出谁</button>
          )}
        </div>
      )}

      <Parents p={egg.parents} onPet={onPet} />
    </div>
  )
}

// 奖牌只在**确定**拿得到时才列(后端已判好,判不了的不下发),纯文字,没有就空着——
// 那一行仍占着高度(见 .egg-tags 的 min-height),卡片才不会一行高一行矮。
const DIM_NAME = { 2: '体型', 3: '嗓音' }

// voiceText 渲染推测嗓音:串窝(父本不唯一)时给区间,推不出来时留空(由 Row 显示破折号)。
function voiceText(egg) {
  if (egg.voice == null) return ''
  return egg.voiceMax != null ? `${egg.voice}~${egg.voiceMax}` : String(egg.voice)
}

// timeNode 渲染获得时间。手机上背包是双列,整串「2026-08-16 03:46:45」放不下会被省略号
// 咬掉分秒(而分秒正是「获取时间」排序看的东西),故把年份单独包一层,窄屏 CSS 藏掉即可
// (见 eggs.css;鼠标悬停的 title 里仍是完整时间)。
function timeNode(ts) {
  const s = fmtTime(ts)
  return <><span className="egg-row-y">{s.slice(0, 5)}</span>{s.slice(5)}</>
}

// Row 一行「标签 值 百分位」。值缺失时留破折号占位。
function Row({ k, v, pct, title, hot }) {
  const cls = hot || pctHot(pct) || ''
  return (
    <div className="egg-row" title={title || ''}>
      <span className="muted egg-row-k">{k}</span>
      <span className={'egg-row-v ' + cls}>{v || '—'}</span>
      <span className={'egg-row-p ' + cls}>{pct != null ? pct.toFixed(2) + '%' : ''}</span>
    </div>
  )
}

// Parents 双亲快照:母本确定(蛋趴在她的窝上),父本取服务器下发的配对候选,
// 几个窝挨太近「串窝」时有多个候选、实际父本无从确定(见 docs/data.md 3.6)。
// 非家园蛋没有双亲可言,留同样高度的占位,保证卡片等高。
function Parents({ p, onPet }) {
  const rows = []
  if (p?.mother) rows.push(['♀', p.mother])
  for (const f of p?.fathers || []) rows.push(['♂', f])
  return (
    <div className="egg-parents">
      {rows.length === 0 && <div className="egg-parent-ph2">无双亲记录</div>}
      {rows.slice(0, 2).map(([role, x], i) => (
        <button key={i} className="egg-parent" onClick={() => onPet(x.gid)}
          title={`点击查看${role === '♀' ? '母本' : '父本'}详情(已放生也不影响这里的快照)` +
            (p.ambiguous && role === '♂' ? ' · 串窝:父本不唯一' : '')}>
          {x.img ? <img src={imgURL(x.img)} alt="" draggable={false} /> : <span className="egg-parent-noimg">🐾</span>}
          <span className="egg-parent-txt">
            {role} {x.name}
            {x.weightPct != null && <span className={pctHot(x.weightPct)}> W {Math.round(x.weightPct)}%</span>}
            {x.voice != null && ` V ${x.voice}`}
            {x.nature ? ` ${x.nature}` : ''}
            {p.ambiguous && role === '♂' && <span className="egg-amb">?</span>}
          </span>
        </button>
      ))}
      {rows.length === 1 && <div className="egg-parent-ph2">父本未知</div>}
    </div>
  )
}
