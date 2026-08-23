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

// —— 按账号隔离的数据(自动带 ?account=)——

export const getPets = (params) => getJSON('/api/pets?' + buildQuery(params))

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

// getAccounts 返回已知账号列表 [{account,name,petCount}](账号切换下拉用)。
export const getAccounts = () => getJSON('/api/accounts')

// getPois 返回某场景(scene_res_cfg_id)的大地图 POI 图层:
//   {kinds:[{k,n,icon,on,num}], pois:[{k,u,v,n}]}——u,v 是底图归一化坐标(后端已投影,同玩家位置)。
// 场景无底图时两者皆空。
export const getPois = (res) => getJSON('/api/pois?res=' + res, { kinds: [], pois: [] })

// getWildPets 返回当前账号最近一次野生宠物标记(异色/炫彩、污染、声音):
//   {sceneResId, pets:[{id,n,img,kinds,u,v,lv,voice,height,weight,weightPct,mutation,glass,stale}]}
// 之后由 SSE wildpets 增量覆盖;从未收到过任何 AOI 通知时返回 null。
export const getWildPets = () => getJSON('/api/wildpets?' + buildQuery(), null)

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

// getEggs 返回背包里的精灵蛋(库里存的就是背包现状,破壳/送人的行已删):
//   {eggs:[{gid,name,species,icon,typeName,medals,heightM,weightKg,heightPct,weightPct,
//           obtainedAt,hatching,parents,…}]}
export const getEggs = (params) => getJSON('/api/eggs?' + buildQuery(params), { eggs: [] })

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
}

// subscribe 订阅 SSE，onMsg 收到 {type, account, data}。返回取消函数(幂等)。
// 服务端按当前 account 过滤(buildQuery 自动带上 ?account=):切账号时各页的 effect 会重订阅,
// 届时 URL 变化触发重连。
export function subscribe(onMsg, opts = {}) {
  subs.add(onMsg)
  if (opts.debug) debugSubs++
  syncStream()
  let done = false
  return () => {
    if (done) return // React StrictMode 会把 effect 的清理跑两遍,别把计数减穿
    done = true
    subs.delete(onMsg)
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

// adminError 把失败响应转成带 status 的错误(401 表示会话失效,调用方据此决定是否踢回登录页)。
async function adminError(r, fallback) {
  let msg = r.status === 401 ? '密码错误或会话已失效' : (fallback || '请求失败(' + r.status + ')')
  try {
    const e = await r.json()
    if (e && e.error) msg = e.error
  } catch {
    // 后端 http.Error 返回 text/plain(非 JSON),兜底读文本显示具体原因
    try {
      const t = (await r.text()).trim()
      if (t) msg = t
    } catch { /* ignore */ }
  }
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

// adminPlaceholder 管理员面板占位接口(其余功能待实现)。
export const adminPlaceholder = () => adminFetch('/api/admin/placeholder').then((r) => r.json())

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
      if (!r.ok) throw new Error('删除失败(' + r.status + ')')
      return r.json()
    })

// adminStats 全部成员抓捕情况:{members:[{account,name,total,shiny,colorful,daily}], days, daily}。
export const adminStats = () => adminFetch('/api/admin/stats').then(async (r) => {
  if (!r.ok) throw await adminError(r, '拉取统计失败')
  return r.json()
})

// adminPlaySessions 游玩记录:{sessions:[{account,name,loginTime,logoutTime,duration,online}],
// summary:{online,todaySessions,todayDuration,daily:[{day,sessions,duration}]}}。
// account=账号过滤(空=全部);limit=明细条数(默认200)。
export const adminPlaySessions = (account = '', limit = 200) =>
  adminFetch('/api/admin/play-sessions?account=' + encodeURIComponent(account) + '&limit=' + limit)
    .then(async (r) => {
      if (!r.ok) throw await adminError(r, '拉取游玩记录失败')
      return r.json()
    })

// adminWildPetOptions 可投放的野生宠物形态:{options:[{base,name,book}]}。
export const adminWildPetOptions = () => adminFetch('/api/admin/wild-pets').then(async (r) => {
  if (!r.ok) throw new Error('拉取形态列表失败(' + r.status + ')')
  return r.json()
})

// adminInjectWild 向指定成员投放稀有野生精灵。
// {account, base: petbase id, kind: 'shiny'|'colorful', offsetMeters}
export const adminInjectWild = (account, base, kind, offsetMeters = 30) =>
  postJSON('/api/admin/inject-wild', { account, base, kind, offsetMeters })

// adminRevokeInject 撤销某账号的一只注入精灵(?account=&id=)。
export function adminRevokeInject(account, id) {
  return adminFetch(
    '/api/admin/inject-wild?account=' + encodeURIComponent(account) +
    '&id=' + encodeURIComponent(id),
    { method: 'DELETE' },
  ).then(async (r) => {
    if (!r.ok) throw new Error('撤销失败(' + r.status + ')')
    return r.json()
  })
}

// adminListInjects 列出当前全部注入中的精灵(管理面板撤销用)。
// 返回 {injects:[{account,id,name,kinds,sceneRes,created}]};玩家换场景或靠近 10 米 10 秒后自动消失,列表随之减少。
export const adminListInjects = () => adminFetch('/api/admin/injects').then(async (r) => {
  if (!r.ok) throw new Error('拉取注入列表失败(' + r.status + ')')
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
  if (!r.ok) {
    let msg = r.status === 401 ? 'PIN 错误' : r.status === 429 ? '尝试过于频繁,请稍后再试' : '校验失败(' + r.status + ')'
    try { const t = (await r.text()).trim(); if (t) msg = t } catch { /* ignore */ }
    throw new Error(msg)
  }
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
    if (!r.ok) {
      let msg = r.status === 401 ? 'PIN 错误' : r.status === 403 ? '该账号未设 PIN,需管理员删除' : r.status === 429 ? '尝试过于频繁,请稍后再试' : '删除失败(' + r.status + ')'
      try { const t = (await r.text()).trim(); if (t) msg = t } catch { /* ignore */ }
      throw new Error(msg)
    }
    return r.json()
  })
}
