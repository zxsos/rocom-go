import { useStoredJSON } from './useStoredState'
import { DEFAULT_RANGE_RULES, sanitizeRangeRules } from '../utils/rules'

// —— 体重 / 声音区间规则的共享状态 ——
//
// 事件页与大地图都从这里取同一份规则:配一次两边生效,口径只有这一个来源
// (见 utils/rules.js 里关于「三处各写一份」的说明)。
//
// 存储键是**共享**的,不含页面前缀 —— 这正是目的所在。
// 两个页面分属不同路由(#/events 与 #/map),同一时刻只有一个挂载,
// 故不存在同页面内两份 state 不同步的问题;切页面时各自重新读一次即可。
export const RANGE_RULES_KEY = 'rangeRules.v1'

// useRangeRules 返回 [rules, setRules]。
//
// fallback 用默认四条(奖牌边界):用户从未配置过时才落这套;一旦存过(哪怕是
// 空数组)就以存储为准 —— 否则用户删光规则后会被重新塞回默认值,「删除」就失效了。
export function useRangeRules() {
  return useStoredJSON(localStorage, RANGE_RULES_KEY, DEFAULT_RANGE_RULES, sanitizeRangeRules)
}
