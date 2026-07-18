import { createContext } from 'react'

// AccountContext 提供当前选中账号(玩家 user_id key),供各页对 SSE 按账号过滤。
export const AccountContext = createContext('')

// IconsContext 提供全局固定图标(六维属性小图 + 异色/炫彩/污染标记图);App 启动拉一次。
export const IconsContext = createContext({ stat: {} })
