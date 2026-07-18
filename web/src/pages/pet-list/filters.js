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
