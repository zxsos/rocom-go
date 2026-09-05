import React, { useState, useEffect, useContext, useRef, useCallback } from 'react'
import { getEggs, subscribe, queryEggMatch } from '../../api'
import { AccountContext } from '../../context'
import { imgURL } from '../../components/icons'
import { PetDetailModal } from '../../components/PetDetailModal'
import { fmtTime, pad2, pctHot, voiceHot } from '../../utils/format'
import { useInterval } from '../../hooks/useAsyncData'
import { Marks } from '../../components/badges'
import { hatchProgress, remainRealSecs } from './hatch'
import { toast } from '../../components/toast'

// 精灵蛋页面:两段垂直 —— 孵蛋器(在孵且进度未满的蛋)、仓库(其余蛋)。不分标签页——在孵的蛋
// 本来就不出现在背包格子里(BagModuleData.IsRemoveEggItem)。
//
// 此前还有第三段「已孵化蛋」(标记在孵但进度满),那是给历史 bug 打的补丁:服务器取出孵蛋器
// 不清 start_hatch_time,背包全量/登录(0x1344)会把已取出的蛋重新标成在孵,而进度字段被清零
// 后前端外推瞬间顶满,于是按「在孵且进度满」把它们圈出来隔离。15648bf 改用登录/开孵蛋器时
// 的权威 egg_gid 判定在孵后,残留标记不再产生,该栏作废。
//
// 但「进度满就不显示」这个过滤**回收得不干净**:真孵好的蛋(等玩家点破壳)也会被它
// 一并滤掉,两栏都看不到 —— 玩家等着蛋孵好,蛋却不见了。故现在两栏都只按 hatching
// 标记分(与上游一致):进度只影响进度条怎么画,不决定蛋出不出现 —— 进度是本地估算
// 的(倍率靠两次采样反推,见 hatch.js),拿它当「该不该出现」的依据会把估算误差放大成
// 数据丢失。孵好的蛋照常留在孵蛋器,进度条显示「可破壳」。
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

// 预计完成时间:按孵化倍率(后端按加速日时间表给,见 hatch.js)外推剩余时间,换算成时间点。
//
// ⚠️ 剩的是**孵化秒**,不是真实秒 —— 必须过 remainRealSecs 除以倍率。
// 直接拿 maxSecs − p.secs 当秒数,平时(1 倍)恰好对得上,一旦撞上孵蛋加速
// (5 倍)就会把完成时刻报成 5 倍远:真实还剩 24 分钟的说成 2 小时后。
// 同一天只显时分(手机双列卡片宽度紧张),跨天补「月-日 时:分」;title 里给剩余时长。
//
// 倍率不是估的:加速日是每周五 04:00~周一 04:00 的固定活动,服务器照此推进(离线
// 也一样),后端按时刻直接算 —— 故这里可以明说「按 ×N 倍率」,不用像早先那样挂
// 「倍率尚未测出」(那是靠差分估倍率时的说法,已废弃)。
function etaText(egg, p, now) {
  const eta = new Date(now + remainRealSecs(egg, p) * 1000)
  const hm = `${pad2(eta.getHours())}:${pad2(eta.getMinutes())}`
  return new Date(now).toDateString() === eta.toDateString()
    ? hm
    : `${eta.getMonth() + 1}-${eta.getDate()} ${hm}`
}
function etaTitle(egg, p, moving) {
  const s = remainRealSecs(egg, p)
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  const dur = h > 0 ? `${h} 小时 ${m} 分` : `${m} 分钟`
  const base = `按 ×${rateText(p.rate)} 倍率估算,约剩 ${dur}`
  // 玩家此刻在移动:进度会比这个数更快,但快多少随速度与移动方式而变(实测静止 5.00
  // 对移动 16.9~25.8),没有可信的定值,故只作定性提示、不折算进上面的 d(虚报更糟)。
  return base + (moving ? ' · 移动中,实际更快' : '')
}

// rateText 把倍率格式化成给人看的样子:整数不带小数(5 倍就是「5」),否则留一位。
const rateText = (r) => (Number.isInteger(r) ? String(r) : r.toFixed(1))

// Boost 孵化加速徽章。倍率是**全局**的(实测三颗不同 maxSecs 的蛋同秒各 +10s,
// 见 docs/data.md 3.6),故整个孵蛋器标一处即可,不必逐蛋重复。
//
// 倍率是加速日时间表算出来的定值(离线也准),故 rate 必然是 1 或 5 —— 不存在
// 「估不出来」的中间态,<=1 时(非加速日)直接不画。
function Boost({ rate, moving }) {
  if (!(rate > 1)) return null
  return (
    <span className="incu-boost" title={
      `孵蛋加速日:每过 1 真实秒推进 ${rateText(rate)} 个孵化秒` +
      '(北京时间周五 04:00 ~ 周一 04:00,服务器离线期间同样按此推进)' +
      (moving ? ' · 你正在移动,进度还会更快' : '')}>
      加速中 ×{rateText(rate)}
    </span>
  )
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
  // onOpen 断线重连时也要补拉:SSE 断开期间的推送全丢了,不补就一直显示旧数据
  // (登录那次广播尤其可能正好落在断线窗口里 —— 见 pipeline/eggs.go 登录分支)。
  useEffect(() => subscribe('eggs', load, { onOpen: load }), [load])
  // 孵化进度随时间涨:秒级刷新即可。
  useInterval(() => setNow(Date.now()), 1000)

  // 两段分类:**只看 hatching 标记**,不看进度。
  //
  // 这个过滤条件曾经是 `hatching && pct < 100`,那是给一个**已修复的**历史 bug 打的
  // 补丁:服务器取出孵蛋器不清 start_hatch_time,背包全量会把已取出的蛋重新标成在孵,
  // 而进度被清零后前端外推瞬间顶满,于是把「在孵且进度满」的圈出来隔离。
  // 15648bf 改用登录/开孵蛋器时的权威 egg_gid 判定在孵后,残留标记不再产生,补丁该
  // 回收了 —— 留着它,**真孵好的蛋(进度确实满了、等玩家点破壳)会两栏都不显示**,
  // 玩家眼看着蛋不见了。上游(wiki 同源项目)也是只按 hatching 分栏。
  //
  // 更根本的:进度是**本地估算**的(倍率靠两次采样反推,见 hatch.js),拿它决定
  // 「这颗蛋该不该出现」就是把估算误差放大成数据丢失。它只该影响进度条怎么画。
  const incubating = data.eggs.filter((e) => e.hatching)
  const bag = data.eggs.filter((e) => !e.hatching)

  // 孵化倍率直接取后端那份:它按**加速日时间表**(北京时间周五 04:00 ~ 周一 04:00)
  // 算出,离线也准,不存在「还没校准」的冷启动。
  //
  // 早先这里做的是「对相邻两次进度下发做差分、取跨蛋中位数」,那套已废弃 —— 它把
  // 活动倍率(固定时间表)与在线加成(移动/风场)混成一个数去测,于是移动时段的
  // 加速会污染样本、离线那段又完全算不了。详见 hatch.js 文件头与 docs/data.md 3.6。
  const rate = data.hatchRate || 1
  // moving 是后端按移动包(0x0133)判的「玩家此刻在移动」。它**不进**倍率与 ETA ——
  // 移动加成随速度与移动方式而变(静止 5.00 vs 移动 16.9~25.8),没有可信定值,
  // 折算进去会虚报「可破壳」。只作定性提示。
  const moving = !!data.hatchMoving
  const slots = HATCH_SLOTS

  return (
    <div className="eggs-page">
      <div className="eggs-cols">
        <IncuTitle n={incubating.length} slots={slots} rate={rate} moving={moving} />
        {/* 空格子只在宽屏画出来,手机上由 CSS 收起(见 eggs.css) */}
        <aside className="eggs-incu">
          {Array.from({ length: slots }, (_, i) => incubating[i]).map((e, i) => (
            e ? <EggCard key={e.gid} egg={e} now={now} rate={rate} moving={moving} onPet={setDetailGid} />
              : <div key={'s' + i} className="egg-slot-empty">空格子</div>
          ))}
          {/* 全空时给一句解释:光秃秃 3 个空格子,用户分不清是「真没在孵」还是
              「数据还没到」。蛋的完整数据只随背包全量(0x1344)下发 —— 那是玩家
              在游戏内**打开一次背包**才发的,登录包只带「哪几颗在孵」的 id 清单
              (见 internal/pipeline/eggs.go),光有 id 画不出卡片。
              故首次使用(库里还没有蛋)时,提示去游戏内开一次背包。 */}
          {incubating.length === 0 && bag.length === 0 && (
            <div className="empty eggs-incu-hint">
              孵蛋器是空的。若游戏里正在孵蛋,需在游戏内打开一次背包 ——
              蛋的数据只随背包下发给服务端。
            </div>
          )}
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
                {bag.map((e) => <EggCard key={e.gid} egg={e} now={now} rate={rate} moving={moving} onPet={setDetailGid} />)}
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
function IncuTitle({ n, slots, rate, moving }) {
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
      <Boost rate={rate} moving={moving} />
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
function EggCard({ egg, now, rate, moving, onPet }) {
  const p = hatchProgress(egg, now, rate)
  const src = egg.srcName ? `来源:${egg.srcName}` : ''
  const name = tidyEggName(egg.name)
  // 随机蛋(神奇的蛋)的「猜猜孵出谁」。
  //
  // **用哪个数据源由服务端配置决定**(管理面板切换,见 EggSourceCard):前端传不了,
  // 也不该能传 —— 数据源是对全服生效的运维选项,若能让请求参数覆盖,任何玩家都能
  // 夹带 src=xianyu 去烧第三方额度(10 次/分钟)。
  //
  // 查询结果按身高体重+时长缓存(localStorage),刷新页面时立即恢复、不闪空白。
  // 只缓存**本地源**的结果:咸鱼源要烧额度,不进缓存,故每次点都会真去问第三方。
  const guessKey = [egg.heightM, egg.weightKg, egg.maxSecs].map((v) => v ?? '').join('|')
  const [match, setMatch] = useState(() => {
    if (!egg.random) return null
    const hit = readEggGuessCache(guessKey)
    return hit ? { loading: false, error: '', data: hit } : null
  }) // {loading,error,data}
  const query = useCallback(() => {
    setMatch({ loading: true, error: '', data: null })
    queryEggMatch(egg.heightM, egg.weightKg, egg.maxSecs)
      .then((d) => {
        // 只缓存本地源:咸鱼源每次都要烧额度,缓存它等于偷偷替管理员决定「这份结果
        // 可以复用多久」—— 那是他的令牌,不该由前端做主。
        if (d.source === 'local') writeEggGuessCache(guessKey, d)
        setMatch({ loading: false, error: '', data: d })
      })
      .catch((e) => {
        const msg = e.message || '查询失败'
        if (/429|请求过于频繁/.test(msg)) {
          // 限流只可能发生在咸鱼源:不占卡片位置,弹自制 toast 提醒(不阻塞页面)
          setMatch(null)
          toast('喂喂喂,当我Token不要钱吗,等会再查啊魂淡')
        } else {
          setMatch({ loading: false, error: msg, data: null })
        }
      })
  }, [egg.heightM, egg.weightKg, egg.maxSecs, guessKey])

  // 恢复出来的缓存可能是**另一个源**给的(管理员在面板切过源之后,localStorage 还在)。
  // 故挂载后静默重查一次校正:本地源是内存查表,这次请求几乎不要钱;咸鱼源不写缓存,
  // 走不到这条路(它的缓存值只可能来自上一次本地源查询,正是要被校正掉的那种)。
  // 依赖写 [query] 而非 []:query 的依赖全是原始值,引用稳定,效果等同挂载一次,
  // 如此写是为了让 lint 能验证闭包没捕获到过期变量。
  const restoredRef = useRef(match?.data != null)
  useEffect(() => {
    if (!restoredRef.current) return
    restoredRef.current = false
    query()
  }, [query])

  const openQuery = () => {
    // 咸鱼源的结果不进缓存,故命中缓存的一定是本地源的 —— 先立即渲染它(不闪空白),
    // 再由上面那次静默刷新校正。
    const hit = readEggGuessCache(guessKey)
    if (hit) setMatch({ loading: false, error: '', data: hit })
    query()
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
          {p.pct < 100 && <span className="egg-eta" title={etaTitle(egg, p, moving)}>预计 {etaText(egg, p, now)}</span>}
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
                ) : match.data.total > 0 ? (
                  <>
                    <div className="muted egg-guess-line">
                      匹配 {match.data.total} 条
                      {match.data.source === 'xianyu' ? ',来源 示例玩家' : ',来源 本地解包数据'}
                    </div>
                    <div className="egg-guess-list">
                      {/* 后端已按匹配度排好序,这里不再重排。 */}
                      {(match.data.matches || []).map((m) => (
                        <div key={m.confId || m.name} className="egg-guess-item">
                          {/* img 是后端给的完整可用值(本地站内路径 / 第三方外链),不再拼 /img/。 */}
                          {m.img
                            ? <img className="egg-guess-img" src={m.img} alt="" loading="lazy" draggable={false} /> : null}
                          <div className="egg-guess-txt">
                            <div className="egg-guess-name">{m.name}</div>
                            {/* 本地源额外给出蛋落在候选区间内的百分位:两条都齐时才显示,
                                第三方不提供这两维,故只显示 note(孵化时长文案)。 */}
                            <div className="muted">
                              {/* score 是后端算的 float,原样渲染会出「81.90337105999757」
                                  这种长尾 —— 它只用于排序,精度到小数一位足够。 */}
                              匹配度 {m.score?.toFixed(1) ?? '—'}
                              {m.heightPct != null && m.weightPct != null
                                ? ` · 百分位 身高${m.heightPct.toFixed(1)}% 体重${m.weightPct.toFixed(1)}%`
                                : m.note ? ` · ${m.note}` : ''}
                            </div>
                          </div>
                        </div>
                      ))}
                    </div>
                    {/* 用哪个数据源由管理面板配置,这里不提供切换入口:
                        数据源是对全服生效的运维选项,不该让单个玩家按一下就改。 */}
                    <div className="egg-guess-acts">
                      <button className="btn" onClick={() => setMatch(null)}>关闭</button>
                    </div>
                  </>
                ) : (
                  <div className="egg-guess-line err">
                    <span>找不到这颗臭蛋</span>
                    <button className="btn" onClick={() => setMatch(null)}>关闭</button>
                  </div>
                )}
            </div>
          ) : (
            <button
              className="btn egg-guess-btn"
              onClick={openQuery}
              title="按蛋的身高/体重/孵化时长反推可能孵出谁(数据源由管理面板配置)"
            >猜猜孵出谁</button>
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
