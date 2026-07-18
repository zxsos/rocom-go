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

// getEvolution 返回某 petbase(base_conf_id)所属进化链(按阶段升序)。
export const getEvolution = (base) => getJSON('/api/evolution?base=' + base)

// subscribe 订阅 SSE，onMsg 收到 {type, account, data}。返回取消函数。
// 服务端按当前 account 过滤(buildQuery 自动带上 ?account=);高频 debug 流仅在 opts.debug 时请求,
// 其它页面不拉调试数据。返回的取消函数会关闭连接,服务端随之停止推送(真正的暂停/停止)。
export function subscribe(onMsg, opts = {}) {
  const q = buildQuery({ debug: opts.debug ? 1 : undefined })
  const es = new EventSource('/api/stream' + (q ? '?' + q : ''))
  es.onmessage = (e) => {
    try {
      onMsg(JSON.parse(e.data))
    } catch {
      /* ignore */
    }
  }
  return () => es.close()
}
