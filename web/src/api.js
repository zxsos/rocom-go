// REST 封装与 SSE 订阅。

// 当前选中账号(玩家 user_id 派生的 key,如 "UID:10001")。持久化到 localStorage,
// 带 account 的 REST 请求自动带上 ?account=;为空则由后端回退到最近活跃账号。
let currentAccount = localStorage.getItem('account') || ''
export function getCurrentAccount() { return currentAccount }
export function setCurrentAccount(a) {
  currentAccount = a || ''
  if (currentAccount) localStorage.setItem('account', currentAccount)
  else localStorage.removeItem('account')
}

// buildQuery 把参数对象拼成查询串并附加当前 account。
function buildQuery(params) {
  const q = new URLSearchParams()
  Object.entries(params || {}).forEach(([k, v]) => {
    if (v !== undefined && v !== null && v !== '' && !(Array.isArray(v) && v.length === 0)) {
      q.set(k, Array.isArray(v) ? v.join(',') : v)
    }
  })
  if (currentAccount) q.set('account', currentAccount)
  return q.toString()
}

// getJSON 请求并解析 JSON;传了 fallback 时,非 2xx 不再解析、直接返回 fallback。
async function getJSON(url, fallback) {
  const r = await fetch(url)
  if (fallback !== undefined && !r.ok) return fallback
  return r.json()
}

// errorBody 读失败响应的正文:后端既有 JSON 的 {error},也有 http.Error 的 text/plain;
// 都取不到就返回空串(调用方据此退回兜底文案)。
// 注意 error 只认非空字符串:{"error":""} 不该把整个 JSON 显示给用户,留空走 fallback。
async function errorBody(r) {
  try {
    const t = (await r.text()).trim()
    try {
      const j = JSON.parse(t)
      if (j && typeof j.error === 'string' && j.error) return j.error
    } catch { /* 非 JSON,按 text/plain 处理 */ }
    return t
  } catch { return '' }
}

// httpError 把失败响应转成 Error:正文优先;读不到正文时取 notes[status],再退回 fallback(附状态码)。
// notes 用于替换特定状态码的文案(如 401→「PIN 错误」),这些文案本身已够明确,不再附状态码。
// 错误对象带 status,供调用方按 401/429 等分支处理。
async function httpError(r, fallback = '请求失败', notes) {
  const body = await errorBody(r)
  const note = notes && notes[r.status]
  const err = new Error(body || note || `${fallback}(${r.status})`)
  err.status = r.status
  return err
}

// —— 按账号隔离的数据(自动带 ?account=)——

export const getPets = (params) => getJSON('/api/pets?' + buildQuery(params))

// getHandbookGlasses 返回本账号图鉴炫彩收集(按品种聚合,图鉴号升序):
//   {glasses:[{base,name,book,head,common:[glass_value…],hidden:[glass_value…]}]}
// 数据来自登录包 pet_handbook(每次登录时快照更新,非实时)。
export const getHandbookGlasses = () => getJSON('/api/handbook-glasses?' + buildQuery(), { glasses: [] })

export async function getPet(gid) {
  const r = await fetch('/api/pets/' + gid + '?' + buildQuery())
  if (!r.ok) throw new Error('not found')
  return r.json()
}

// getPetPage 查询某宠物在指定筛选/排序下所处页码。
export const getPetPage = (gid, params) => getJSON('/api/pet-page?' + buildQuery({ ...params, gid }))

export const getEvents = (params) => getJSON('/api/events?' + buildQuery(params))

export async function clearEvents() {
  await fetch('/api/events?' + buildQuery(), { method: 'DELETE' })
}

// getEventCount 返回事件总数({count}),即自上次清空以来获得的宠物数(失去事件不入库)。
export const getEventCount = (params) => getJSON('/api/events/count?' + buildQuery(params))

// getEventStats 返回事件统计(总览/稀有/近30天分布/热门形态)。
export const getEventStats = (params) => getJSON('/api/events/stats?' + buildQuery(params))

export const getFilterOptions = () => getJSON('/api/filter-options?' + buildQuery())

export const getBoxes = () => getJSON('/api/boxes?' + buildQuery())

export const getTeams = () => getJSON('/api/teams?' + buildQuery())

// getPosition 返回当前账号最近一次实时位置(实时地图页加载即时回显);无记录返回 null。
// 形如 {sceneResId,sceneName,x,y,z,u,v,stop,ts};u,v 仅当该场景有底图时存在。
export const getPosition = () => getJSON('/api/position?' + buildQuery(), null)

// —— 全局固定数据(不随账号变化)——

export const getMedals = () => getJSON('/api/medals')

// getNameOptions 返回全量性格/特长名({nature, speciality}),供事件页高亮规则点选。
export const getNameOptions = () => getJSON('/api/name-options', { nature: [], speciality: [] })

// getIcons 返回全局固定图标(六维属性小图 stat.{hp,attack,...} + 异色/炫彩/污染标记图);
// 不随宠物/账号变化,App 启动时拉一次经 IconsContext 分发。
export const getIcons = () => getJSON('/api/icons')

// getAccounts 返回已知账号列表 [{account,name,petCount,title}](账号切换下拉用;
// title 是该账号今天佩戴的排行榜称号,见 api_rank.go 的 handleAccounts 合并)。
export const getAccounts = () => getJSON('/api/accounts')

// getLeaderboard 拉取排行榜:
//   {forbes:[{account,name,coins,hasCoins,baseline,profit,title}], profit:[...],
//    titles:[{date,account,name,title}], me:{account,name,join,coins,hasCoins,baseline,profit,title}}
// forbes 按洛克贝降序、profit 按盈亏降序(盈亏=当前洛克贝-首次快照);
// titles 是今天(佩戴日)每晚 00:05 结算评出的称号;me 是当前账号的参与状态。
export const getLeaderboard = () => getJSON('/api/leaderboard', { forbes: [], profit: [], titles: [], me: null })

// setAccountRank 设置账号是否参加排行榜(join=true 参加,false 退出)。默认参加。
export async function setAccountRank(account, join) {
  const r = await fetch('/api/account/rank', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ account, join }),
  })
  if (!r.ok) throw await httpError(r, '设置失败')
  return r.json()
}

// getPois 返回某场景(scene_res_cfg_id)的大地图 POI 图层:
//   {kinds:[{k,n,icon,on,num}], pois:[{k,u,v,n}]}——u,v 是底图归一化坐标(后端已投影,同玩家位置)。
// 场景无底图时两者皆空。
export const getPois = (res) => getJSON('/api/pois?res=' + res, { kinds: [], pois: [] })

// getWildPets 返回当前账号最近一次野生宠物标记(异色/炫彩、污染、声音):
//   {sceneResId, pets:[{id,n,img,kinds,u,v,lv,voice,height,weight,weightPct,mutation,glass,stale}]}
// 之后由 SSE wildpets 增量覆盖;从未收到过任何 AOI 通知时返回 null。
export const getWildPets = () => getJSON('/api/wildpets?' + buildQuery(), null)

// getGathers 返回当前账号**此刻视野内**的采集物(花/草/菌/矿/果树):
//   {sceneResId, gathers:[{id,r,n,icon,u,v,x,y,z}]}
//
// 与 getPois 的「采集物」图层互补而非重复:那边是全部**候选刷新点**(3552 个,回答
// 「哪儿会有」),这里只给服务器当下真下发的实体(回答「这会儿有」)。实测刷出率
// 只有三到四成,故这层的价值正是替玩家滤掉那七成空的候选点。
//
// 之后由 SSE gathers 整份覆盖(实体进出是突发的,后端按 150ms 窗口合并后推全量);
// 从未收到过任何实体时返回 null。
export const getGathers = () => getJSON('/api/gathers?' + buildQuery(), null)

// getPaint 返回某场景某层的涂地覆盖位图(玩家走过的地方,见 docs/data.md 3.8):
//   {res, layer, w, h, cell, corridor, safe, cells}——cells 是 w*h 位的位图 base64(每字节 8 格、低位在前);
// 无底图的场景 w=0。之后的新格子由 SSE paint 增量推来。
export const getPaint = (res, layer) => getJSON('/api/paint?' + buildQuery({ res, layer }), { w: 0, h: 0 })

// resetPaint 清空某场景某层的涂地(后端删库并广播 {reset:true},同账号其它页面一起清屏)。
export async function resetPaint(res, layer) {
  await fetch('/api/paint?' + buildQuery({ res, layer }), { method: 'DELETE' })
}

// getHome 返回当前账号最近一次家园小窝图层(不在家园时 nests 为空):
//   {sceneResId, level, roomLevel, nests:[{id,u,v,x,y,name,pet:{…},egg:{…}}]}
// 之后由 SSE home 覆盖;从未进过家园时返回 null。
export const getHome = () => getJSON('/api/home?' + buildQuery(), null)

// getTrial 返回当前账号最近一次草系徽章试炼状态(试炼页加载即时回显):
//   {ts, active, run:{trialId,slotId,slotName,chapterId,chapterIdx,nodeIndex,coin,chapters,
//    effects,boss,pet:{gid,name,species,img,level,hp,maxHp,energy,growth,skills,features,shards,
//    equipped},options[],refreshCost,bless,reward,shop[],result,log[]}, history:{…}}
// 之后由 SSE trial 覆盖;从未见过试炼报文时返回 null。
export const getTrial = () => getJSON('/api/trial?' + buildQuery(), null)

// getTrialEncounters 返回草系试炼的「遇见记录」:三章各一张精灵图,遇到过的置灰。
// 与 getTrial(实时状态)不同,这是**累积的历史**,直接读库,故不随 SSE 更新 ——
// 打完一局重新切到该页即可(或刷新)。
//   {ts, updated, chapters:[{chapter,name,total,seen,normal:[{base,name,img,seen,kind,time}],
//    boss:[…]}]}
export const getTrialEncounters = () => getJSON('/api/trial/encounters?' + buildQuery(), null)

// getFlowers 返回当前账号最近一次花种(花灵)BOSS 分组(花种页加载即时回显):
//   {flowers:[{id,name,img,star,blood,endTs,specSeedId,activityId,ownerUserId}]}
// 之后由 SSE flowers 覆盖;从未收到过 0x0375(游戏内未打开过花种面板)时返回 null。
export const getFlowers = () => getJSON('/api/flowers?' + buildQuery(), null)

// getHomeQuery 按 uid 查询**任意玩家**的家园快照(回源第三方,与抓包无关):
//   {uid,homeName,homeLevel,roomLevel,comfort,pets:[{base,form,name,species,level,
//    gender,mutation,status,head,book}],plants:[{seedName,harvest,ripeAt,canSteal,stolen}],
//    cached,fetchedAt}
// 注意与 /api/home 的区别:那个是**自己**家园的小窝图层(抓包数据),这个查的是别人。
// force=true 跳过服务端 15 分钟缓存强制回源;回源失败时后端会降级返回旧缓存。
export const getHomeQuery = async (uid, force = false) => {
  const r = await fetch('/api/home/query?' + buildQuery({ uid, force: force ? 1 : '' }))
  if (!r.ok) throw await httpError(r, '查询家园失败')
  return r.json()
}

// getFlowerSlots 返回当前账号的花种世界存档槽位列表(槽位管理用):
//   {slots:[{key,name,ts,flowers:[…]}]}——key 为 "self"(自己世界)或 "owner:<uid>"(好友世界,
// uid 即世界归属者)。每槽 flowers 是该世界最近一次完整花种列表(含 0x0338 详情)。
export const getFlowerSlots = () => getJSON('/api/flowers/slots?' + buildQuery(), { slots: [] })

// deleteFlowerSlot 删除一个好友世界存档槽(?key=);self 槽后端拒绝,删除后回访该世界会重新建档。
export async function deleteFlowerSlot(key) {
  const r = await fetch('/api/flowers/slots?' + buildQuery({ key }), { method: 'DELETE' })
  if (!r.ok) {
    let msg = '删除失败(' + r.status + ')'
    try { const t = (await r.text()).trim(); if (t) msg = t } catch { /* ignore */ }
    throw new Error(msg)
  }
  return r.json()
}

// getEggs 返回背包里的精灵蛋(库里存的就是背包现状,破壳/送人的行已删):
//   {eggs:[{gid,name,species,icon,typeName,medals,heightM,weightKg,heightPct,weightPct,
//           obtainedAt,hatching,parents,…}]}
export const getEggs = (params) => getJSON('/api/eggs?' + buildQuery(params), { eggs: [] })

// setHatchSpeed 开始/取消孵化倍率实测。on=true 重置并等第一次采样,false 取消。
// 返回测速状态 {state:"idle"|"armed"|"first"|"done", rate, t1, measuredAt}。
//
// 测法:玩家点开始 → 去游戏内打开**两次**孵蛋器(每次触发一次进度下发) → 后端取
// 差分。进度没有被动推送,只能靠玩家自己触发,故必须他先点一下才知道要配对哪两次。
export async function setHatchSpeed(on) {
  const r = await fetch('/api/hatch/speed', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ on: !!on }),
  })
  if (!r.ok) {
    let msg = '操作失败(' + r.status + ')'
    try { const t = (await r.text()).trim(); if (t) msg = t } catch { /* ignore */ }
    throw new Error(msg)
  }
  return r.json()
}

// queryEggMatch 查随机蛋(神奇的蛋)可能孵出的物种。
//
// **用哪个数据源由服务端配置决定,前端传不了** —— 数据源是对全服生效的运维选项,
// 若能让请求参数覆盖,任何玩家都能夹带 src=xianyu 去烧第三方额度。想换源只能走
// 管理面板(见 adminEggSourceSet)。
//
// 两个源的响应结构一致,前端不分支:
//   {source:"local"|"xianyu", total, matches:[{name,img,hatchSecs,score,heightPct,weightPct,confId,note}]}
//
// img 是**可直接赋给 <img src> 的完整值**:本地给 /img/ 开头的站内路径、第三方给外链,
// 故这里不要再用 imgURL() 去拼(其它接口给的是相对路径,唯独这条不是)。
//
// maxSecs 是这颗蛋孵满所需的秒数,本地源拿它当最强的一维约束(见 docs/data.md
// 「随机蛋的区间藏在哪」),能传一定要传。
export const queryEggMatch = async (height, weight, maxSecs) => {
  const r = await fetch('/api/eggs/query?' + buildQuery({ height, weight, maxSecs }))
  if (!r.ok) throw await httpError(r, '查询失败')
  return r.json()
}

// getMerchant 拉取远行商人数据:后端按当前时间返回营业状态与 4h 槽缓存(本地缓存,令牌在服务端,
// 见 internal/server/api_merchant.go),只在槽缺失时才回源第三方,避免玩家反复打开页面烧 token。
// force=true 强制后端回源第三方(烧对方额度,仅管理面板「强制刷新商人数据」按钮用)。
// 响应结构:{now,day,status:"open|closed|idle",today:[{start,end,label,empty,merchant}],prev:[...]},
// 其中 merchant 是第三方原始 JSON:{merchant_name,subtitle,fetched_at,round:{...},item_count,
// items:[{name,kind,image,start_time,end_time,time_label,price,limit}]}。
// 服务端未配置令牌时抛错(503)。force 回源第三方可能较慢,30s 超时兜底,避免按钮无限转圈。
export const getMerchant = async (force = false) => {
  const ctl = new AbortController()
  const timer = setTimeout(() => ctl.abort(), 30000)
  let r
  try {
    r = await fetch('/api/merchant' + (force ? '?force=1' : ''), { signal: ctl.signal })
  } catch (e) {
    if (e && e.name === 'AbortError') throw new Error('请求超时(回源第三方较慢),请稍后重试')
    throw e
  } finally {
    clearTimeout(timer)
  }
  if (!r.ok) throw await httpError(r, '拉取失败')
  return r.json()
}

// getMerchantSub 查询当前账号订阅状态:{configured, subscribed, email, keywords}。
// 订阅按登录账号绑定(buildQuery 自动带 ?account=):换设备登录同一账号也能查到同一订阅。
export const getMerchantSub = async () => {
  const r = await fetch('/api/merchant/sub?' + buildQuery())
  if (!r.ok) throw await httpError(r, '查询订阅失败')
  return r.json()
}

// setMerchantSub 订阅/更新当前账号:keywords 逗号分隔的商品名关键词,空=全部新上架都提醒。
export const setMerchantSub = async (email, keywords) => {
  const r = await fetch('/api/merchant/sub?' + buildQuery(), {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email, keywords }),
  })
  if (!r.ok) throw await httpError(r, '订阅失败')
  return r.json()
}

// delMerchantSub 退订当前账号。
export const delMerchantSub = async () => {
  const r = await fetch('/api/merchant/sub?' + buildQuery(), { method: 'DELETE' })
  if (!r.ok) throw await httpError(r, '退订失败')
  return r.json()
}

// getEvolution 返回某 petbase(base_conf_id)所属进化链(按阶段升序)。
export const getEvolution = (base) => getJSON('/api/evolution?base=' + base)

// —— SSE:全站共用一条连接 ——
// 一个页面往往有好几处要实时数据(地图页就有位置、POI、野生宠、家园小窝、涂地五处,再开个
// 宠物详情弹窗就是六处)。每处各开一条 EventSource 会撞上浏览器「同域 6 条 HTTP/1.1 连接」
// 的上限——连接全被 SSE 占住,底图 webp 与后续 API 都排不上队(实测过:地图一片空白)。
// 故全局只开一条,收到的消息分发给所有订阅者;各订阅者本来就按 msg.type 过滤,分发多余的无妨。
let es = null
let esKey = '' // 当前连接的 URL("" = 没连);账号/调试位一变就重连
const subs = new Set()
let debugSubs = 0 // 高频 debug 流只在调试页订阅时才请求

// syncStream 让实际连接与「当前该连什么」保持一致:没人订阅就断开,URL 变了就重连。
function syncStream() {
  const q = buildQuery({ debug: debugSubs > 0 ? 1 : undefined })
  const want = subs.size > 0 ? '/api/stream' + (q ? '?' + q : '') : ''
  if (want === esKey) return
  if (es) { es.close(); es = null } // 断开后服务端随之停止推送
  esKey = want
  if (!want) return
  es = new EventSource(want)
  es.onmessage = (e) => {
    let msg
    try { msg = JSON.parse(e.data) } catch { return }
    for (const fn of [...subs]) {
      try { fn(msg) } catch { /* 单个订阅者出错不牵连其它 */ }
    }
  }
  // 连接建立成功(首次或断线重连)时广播 {type:'stream-open'}:SSE 没有历史缓存,断线期间的
  // 位置/野生宠/涂地/小窝增量都丢了,等下一个移动包(静止时心跳 2.5-3s 一次)才恢复太慢;
  // 各订阅者收到后自行补拉一次快照立即恢复。注意:重连成功 = 已漏数据,onopen 必然触发,
  // 且事件源为浏览器原生重连,不会重复开连接,无需去重。
  es.onopen = () => {
    for (const fn of [...subs]) {
      try { fn({ type: 'stream-open' }) } catch { /* 单个订阅者出错不牵连其它 */ }
    }
  }
}

// subscribe 订阅一类 SSE 消息,返回取消函数(幂等)。
//   type  : 关心的消息类型;数组表示多种;'*' 表示全部(onData 的第二个参数给出实际类型)
//   onData: (data, type) => void,仅在类型匹配时调用
//   opts.onOpen : 连接建立(首次或断线重连)时调用,用于补拉快照(理由见 syncStream 的 stream-open)
//   opts.debug  : true 时请求后端打开高频 debug 流(仅调试页用)
// 类型过滤、账号过滤都在这里统一做,调用方不必各写一遍:
//   - 服务端已按 ?account= 过滤(见 server.handleStream),这里是切账号瞬间在途消息的兜底;
//     比对用的是模块内 currentAccount,比调用方闭包里的 account 更新。未选账号时不拦
//     (此时服务端回退到最近活跃账号,消息合法)。
//   - stream-open 只进 onOpen、不进 onData:它表示「断线期间的数据已丢失」,不是业务消息。
export function subscribe(type, onData, opts = {}) {
  const types = type === '*' ? null : new Set(Array.isArray(type) ? type : [type])
  const fn = (msg) => {
    if (msg.type === 'stream-open') {
      if (opts.onOpen) opts.onOpen()
      return
    }
    if (types && !types.has(msg.type)) return
    if (currentAccount && msg.account && msg.account !== currentAccount) return
    onData(msg.data, msg.type)
  }
  subs.add(fn)
  if (opts.debug) debugSubs++
  syncStream()
  let done = false
  return () => {
    if (done) return // React StrictMode 会把 effect 的清理跑两遍,别把计数减穿
    done = true
    subs.delete(fn)
    if (opts.debug) debugSubs--
    syncStream()
  }
}

// —— 管理员(隐式面板 #/admin,导航不显示)——
// 会话令牌存 localStorage,服务重启后失效需重新登录;所有管理请求带 X-Admin-Token 头。

let adminToken = localStorage.getItem('adminToken') || ''
export function getAdminToken() { return adminToken }

async function adminFetch(url, options = {}) {
  const headers = { ...(options.headers || {}) }
  if (adminToken) headers['X-Admin-Token'] = adminToken
  return fetch(url, { ...options, headers })
}

// adminError 同 httpError,仅 401 且后端未给原因时换成面向登录页的话术
// (401 表示会话失效,调用方据此决定是否踢回登录页)。
async function adminError(r, fallback = '请求失败') {
  const body = await errorBody(r)
  const msg = body || (r.status === 401 ? '密码错误或会话已失效' : `${fallback}(${r.status})`)
  const err = new Error(msg)
  err.status = r.status
  return err
}

export async function postJSON(url, body) {
  const r = await adminFetch(url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (!r.ok) throw await adminError(r)
  return r.json()
}

// getAdminStatus 返回 {configured: 是否已设密码, authed: 当前是否已登录}。
export const getAdminStatus = () => adminFetch('/api/admin/status').then((r) => r.json())

// adminSetup 首次设置管理员密码,成功即登录并返回 token。
export const adminSetup = (password) => postJSON('/api/admin/setup', { password })

// adminLogin 密码登录,返回 token。
export const adminLogin = (password) => postJSON('/api/admin/login', { password })

// setAdminToken 保存/清除登录令牌。
export function setAdminToken(t) {
  adminToken = t || ''
  if (adminToken) localStorage.setItem('adminToken', adminToken)
  else localStorage.removeItem('adminToken')
}

// adminLogout 注销并清除本地令牌。
export async function adminLogout() {
  try { await adminFetch('/api/admin/logout', { method: 'POST' }) } catch { /* ignore */ }
  setAdminToken('')
}

// adminRules 黑白名单:列表 {rules:[{account,mode,note}]}。
export const adminRules = () => adminFetch('/api/admin/rules').then(async (r) => {
  if (!r.ok) throw await adminError(r, '拉取规则失败')
  return r.json()
})

// adminSetRule 新增/更新规则(account, mode: black|white, note)。
export const adminSetRule = (account, mode, note) =>
  postJSON('/api/admin/rules', { account, mode, note })

// adminDeleteRule 删除规则。
export const adminDeleteRule = (account) =>
  adminFetch('/api/admin/rules?account=' + encodeURIComponent(account), { method: 'DELETE' })
    .then(async (r) => {
      if (!r.ok) throw await adminError(r, '删除失败')
      return r.json()
    })

// adminStats 全部成员抓捕情况:{members:[{account,name,total,shiny,colorful,daily}], days, daily}。
export const adminStats = () => adminFetch('/api/admin/stats').then(async (r) => {
  if (!r.ok) throw await adminError(r, '拉取统计失败')
  return r.json()
})

// adminPlaySessions 游玩记录(分页):{sessions:[{account,name,loginTime,logoutTime,duration,online}],
// summary:{online,todaySessions,todayDuration,daily:[{day,sessions,duration}]}, total:总条数}。
// account=账号过滤(空=全部);limit=每页条数(默认50,上限200);offset=跳过条数(默认0)。
// total 与 sessions 同一筛选口径,供前端算总页数;summary 始终按全量计算,不随分页变化。
export const adminPlaySessions = (account = '', limit = 50, offset = 0) =>
  adminFetch('/api/admin/play-sessions?account=' + encodeURIComponent(account) + '&limit=' + limit + '&offset=' + offset)
    .then(async (r) => {
      if (!r.ok) throw await adminError(r, '拉取游玩记录失败')
      return r.json()
    })

// adminEggStats 查蛋 API(第三方图鉴)使用统计:
// {keySet,total,todayTotal,todayOK,todayFail,successRate,
//  daily:[{day,total,ok}], byAccount:[{account,name,total,today}], recent:[{account,name,time,ok,costMs,matches,height,weight}]}。
export const adminEggStats = () => adminFetch('/api/admin/egg-stats').then(async (r) => {
  if (!r.ok) throw await adminError(r, '拉取查蛋统计失败')
  return r.json()
})

// adminMerchantSubs 远行商人邮箱推送名单:{configured: SMTP 是否已配置, subs:[{email,keywords,created_at}]}。
export const adminMerchantSubs = () => adminFetch('/api/admin/merchant-subs').then(async (r) => {
  if (!r.ok) throw await adminError(r, '拉取订阅名单失败')
  return r.json()
})

// adminMerchantSubDelete 从推送名单删除某邮箱的订阅。
export function adminMerchantSubDelete(email) {
  return adminFetch('/api/admin/merchant-subs?email=' + encodeURIComponent(email), { method: 'DELETE' })
    .then(async (r) => {
      if (!r.ok) throw await adminError(r, '删除订阅失败')
      return r.json()
    })
}

// adminMerchantSource 远行商人数据源:{source, keySet, sources:[{id, name, needKey}]}。
// sources 由后端下发(合法标识只有后端能校验),前端只负责展示文案。
export const adminMerchantSource = () => adminFetch('/api/admin/merchant-source').then(async (r) => {
  if (!r.ok) throw await adminError(r, '拉取数据源失败')
  return r.json()
})

// adminMerchantSourceSet 切换远行商人数据源。
// 后端切换时会清空当日已缓存货单并按新源重新获取,故这个调用比一般的保存慢一点,
// 前端要给出「保存中」的状态(见 MerchantSourceCard)。
export const adminMerchantSourceSet = (source) => postJSON('/api/admin/merchant-source', { source })

// adminEggSource 查蛋数据源:{source, keySet, sources:[{id, name, needKey}]}。
// sources 由后端下发(合法标识只有后端能校验),前端只负责展示文案。
export const adminEggSource = () => adminFetch('/api/admin/egg-source').then(async (r) => {
  if (!r.ok) throw await adminError(r, '拉取数据源失败')
  return r.json()
})

// adminEggSourceSet 切换查蛋数据源(本地源 / 咸鱼源)。
// 与远行商人不同,这里切源**没有代价**:两个源都是每次请求实时算、没有跨源缓存,
// 故切换立即生效,也不用清任何东西。
export const adminEggSourceSet = (source) => postJSON('/api/admin/egg-source', { source })

// adminConfig 运行期配置:{writable, path, smtpUser, smtpPassSet, eggKeySet, socks5:{...}}。
//
// **敏感项只给「是否已设置」**(smtpPassSet / eggKeySet / socks5.passSet),令牌与授权码
// 原文从不下发 —— 它们能用来发信、能刷第三方图鉴配额,一旦经 API 下来就会出现在
// 浏览器响应、前端内存与可能的截图里。保存时留空即「不修改」,前端据此渲染占位文案。
export const adminConfig = () => adminFetch('/api/admin/config').then(async (r) => {
  if (!r.ok) throw await adminError(r, '拉取配置失败')
  return r.json()
})

// adminConfigSave 保存配置。入参里所有敏感字段留空表示「不修改」。
//
// 写入的两条语义(后端保证,前端据此提示):
//   - 先落盘 /etc/rocom.env 再改内存 —— 故「重启后配置还在」是天然成立的
//   - 代理(socks5)是热重启的,不影响抓包;邮箱与令牌纯热更
export const adminConfigSave = (payload) => postJSON('/api/admin/config', payload)

// —— Web 监听地址(改它要试运行 + 确认,见 internal/server/api_web_addr.go)——
//
// 它不能像其它配置那样「保存即生效」:改的是管理员正用来改它的那条连接的另一端,
// 一旦新地址连不上(端口被占、没映射、IP 填成 127.0.0.1)面板当场失联。
// 故后端要求先试运行、由管理员在新地址上打开过面板后再确认:
//
//   adminWebAddrTrial    → {addr, realAddr, port, deadline, handoff}
//   adminWebAddrConfirm  → {ok, addr, realAddr[, token]}:先落盘再停旧监听
//   adminWebAddrRevert   → {ok}:关掉新监听,配置不动
//
// deadline 是自动回滚时刻(Unix 秒):到点无人确认,新监听自己收掉。
// port 是**实际**端口号,前端据此拼跳转 URL(端口填 0 时由内核分配,与填的不一样)。
//
// handoff 是一次性交接码。换端口即换源,localStorage 里的令牌不跟过去,故新地址
// 凭这个码完成确认(它同时是「新地址可达」的证据),并在响应里领回一个令牌接上会话。
// adminWebAddrConfirm 的入参为空表示「仍在旧地址上确认」(须已登录);
// 传入 handoff 则表示「从新地址确认」(无需令牌),此时响应带 token。
export const adminWebAddrTrial = (addr) => postJSON('/api/admin/web-addr', { addr })
export const adminWebAddrConfirm = (handoff) => postJSON('/api/admin/web-addr/confirm', { handoff: handoff || '' })
export const adminWebAddrRevert = () => postJSON('/api/admin/web-addr/revert', {})

// adminTestMail 发送测试邮件验证 SMTP 配置(错误信息透传后端 SMTP 具体报错)。
// subject/body 可自定义,为空则后端用默认标题/内容。
export const adminTestMail = (email, subject, body) =>
  postJSON('/api/admin/merchant-test-mail', { email, subject, body })

// adminWildPetOptions 可投放的野生宠物形态:{options:[{base,name,book}]}。
export const adminWildPetOptions = () => adminFetch('/api/admin/wild-pets').then(async (r) => {
  if (!r.ok) throw await adminError(r, '拉取形态列表失败')
  return r.json()
})

// adminInjectWild 向指定成员投放稀有野生精灵。
// {account, base: petbase id, kind: 'shiny'|'colorful', offsetMeters, level, glassType, glassValue}
// level: 0 或缺省=后端随机 30-60;指定 1-100 则固定该等级。
// glassType/glassValue: kind=colorful 时的炫彩色卡,0/0 或缺省=后端随机合法色卡;
//   1=普通炫彩(value=(粒子id<<20)|配色id);2=隐藏炫彩(value=1/2/3 赛季、1000 黑白)。
export const adminInjectWild = (account, base, kind, offsetMeters = 30, level = 0, glassType = 0, glassValue = 0) =>
  postJSON('/api/admin/inject-wild', { account, base, kind, offsetMeters, level, glassType, glassValue })

// adminInjectFlower 向指定成员投放假炫彩花种(花灵 BOSS,默认 7 星特殊花种,星级可自定义)。
// {account, base: 守护宠物 petbase id, star: 1-7(0=默认 7), glassType, glassValue}
// glassType/glassValue 语义同 adminInjectWild(0/0=后端随机合法色卡)。
export const adminInjectFlower = (account, base, star = 0, glassType = 0, glassValue = 0) =>
  postJSON('/api/admin/inject-flower', { account, base, star, glassType, glassValue })

// adminRevokeInject 撤销某账号的一只注入精灵(?account=&id=)。
export function adminRevokeInject(account, id) {
  return adminFetch(
    '/api/admin/inject-wild?account=' + encodeURIComponent(account) +
    '&id=' + encodeURIComponent(id),
    { method: 'DELETE' },
  ).then(async (r) => {
    if (!r.ok) throw await adminError(r, '撤销失败')
    return r.json()
  })
}

// adminListInjects 列出当前全部注入中的精灵(管理面板撤销用)。
// 返回 {injects:[{account,id,name,kinds,sceneRes,created}]};玩家换场景或靠近 10 米 10 秒后自动消失,列表随之减少。
export const adminListInjects = () => adminFetch('/api/admin/injects').then(async (r) => {
  if (!r.ok) throw await adminError(r, '拉取注入列表失败')
  return r.json()
})

// —— 账号 PIN 保护 + 账号删除 ——

// verifyAccountPin 校验账号 PIN;无 PIN 时返回 {ok,hasPin:false}。
export async function verifyAccountPin(account, pin) {
  const r = await fetch('/api/account/verify', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ account, pin }),
  })
  if (!r.ok) throw await httpError(r, '校验失败', { 401: 'PIN 错误', 429: '尝试过于频繁,请稍后再试' })
  return r.json()
}

// setAccountPin 设置/修改/清除账号 PIN。
// newPin 为空串=清除;非管理员需提供 oldPin(已设 PIN 时)。
// 管理员操作自动带 X-Admin-Token(经 postJSON)。
export function setAccountPin(account, oldPin, newPin) {
  return postJSON('/api/account/pin', { account, oldPin, newPin })
}

// deleteAccount 删除账号及其全部数据;非管理员需提供 PIN。
export function deleteAccount(account, pin) {
  // 非管理员请求不走 adminFetch,单独 fetch
  return fetch('/api/account', {
    method: 'DELETE',
    headers: { 'Content-Type': 'application/json', ...(getAdminToken() ? { 'X-Admin-Token': getAdminToken() } : {}) },
    body: JSON.stringify({ account, pin }),
  }).then(async (r) => {
    if (!r.ok) throw await httpError(r, '删除失败', {
      401: 'PIN 错误',
      403: '该账号未设 PIN,需管理员删除',
      429: '尝试过于频繁,请稍后再试',
    })
    return r.json()
  })
}
