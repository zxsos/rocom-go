// 宠物列表的筛选/排序状态定义与换算(与 sessionStorage 持久化格式对应)。

export const SORTS = [
  { key: 'gid', label: '编号' },
  { key: 'boxpos', label: '盒子位置' },
  { key: 'level', label: '等级' },
  { key: 'weight', label: '体重(百分位)' },
  { key: 'height', label: '身高(百分位)' },
  { key: 'voice', label: '声音' },
  { key: 'catchTime', label: '捕捉时间' },
]

// 捕捉时间区间选项(键存入 filter,查询时按本地时间实时算出 catch_time 下限,避免持久化的时间戳过期)。
export const CATCH_RANGES = [
  ['', '全部'], ['h1', '最近一小时'], ['h6', '最近六小时'],
  ['today', '今日'], ['week', '本周'], ['month', '本月'],
]

// catchAfterTs 把区间键转为 unix 秒下限(0=不限);今日/本周/本月按本地日历边界(周一为一周起点)。
function catchAfterTs(range) {
  const nowSec = Math.floor(Date.now() / 1000)
  const startOfDay = () => { const d = new Date(); d.setHours(0, 0, 0, 0); return Math.floor(d.getTime() / 1000) }
  switch (range) {
    case 'h1': return nowSec - 3600
    case 'h6': return nowSec - 6 * 3600
    case 'today': return startOfDay()
    case 'week': { const d = new Date(); const back = (d.getDay() + 6) % 7; d.setDate(d.getDate() - back); d.setHours(0, 0, 0, 0); return Math.floor(d.getTime() / 1000) }
    case 'month': { const d = new Date(); const m = new Date(d.getFullYear(), d.getMonth(), 1); return Math.floor(m.getTime() / 1000) }
    default: return 0
  }
}

// withCatch 把 filter.catchRange 转成后端 catchAfter 时间戳(并从查询参数里去掉 catchRange)。
export function withCatch(f) {
  const { catchRange, ...rest } = f
  const ts = catchAfterTs(catchRange)
  return ts > 0 ? { ...rest, catchAfter: ts } : rest
}

// 列表视图(陈列 / 表格)。
//
// 默认**陈列**:找宠物时认的是形象 —— 宠物图是这个应用最讨喜的资产(全身像
// 随包下发、此前只用到 34px 头像),一屏扫十几只时异色/炫彩与体型是能"扫"出来的。
// 表格留给「筛完之后逐列比」(百分位/声音/六维)那一步,那时再切过去。
//
// 注意默认值只对**没有存储记录**的浏览器生效 —— 已选过视图的用户保持其选择。
export const DEFAULT_VIEW = 'gallery'

// sanitizeView 规整持久化的视图值:两个合法值都原样保留,其余(含空串/垃圾值)回落默认。
//
// ⚠️ 必须写成「两个值都保留」的**对称**形式,不能写成
// `v === 'table' ? 'table' : DEFAULT_VIEW` 这种「只放行一个、其余回落」——
// 那样一旦 DEFAULT_VIEW 改到另一边,**已选过另一个视图的用户会被静默翻过去**
// (本次默认值 table→gallery 就撞上了:旧写法下存了 table 的用户刷新后变陈列)。
// 这是**静默**的:页面照常显示,只是每个用户的视图被强制翻到另一个,日志查不出来。
//
// 单独成函数而非内联箭头,就是为了能给它写断言(见 verify-pet-views.mjs)。
export const sanitizeView = (v) => (v === 'table' || v === 'gallery' ? v : DEFAULT_VIEW)

// 列表状态(筛选/排序/分页)持久化到 sessionStorage 的这个键,从详情返回时还原。
export const FILTER_KEY = 'petListFilter'
export const DEFAULT_FILTER = { page: 1, pageSize: 20, sort: 'boxpos', order: 'asc' }
export const sanitizeFilter = (v, fallback) => (v && typeof v === 'object' ? v : fallback)

// dropBoxFilter 清掉持久化筛选里与账号绑定的盒子条件(切换账号时调,其它条件跨账号仍有意义)。
export function dropBoxFilter() {
  try {
    const f = JSON.parse(sessionStorage.getItem(FILTER_KEY))
    if (f && f.box) { delete f.box; sessionStorage.setItem(FILTER_KEY, JSON.stringify(f)) }
  } catch { /* ignore */ }
}
