import { createContext } from 'react'

// AccountContext 提供当前选中账号(玩家 user_id key),供各页对 SSE 按账号过滤。
export const AccountContext = createContext('')

// AccountNameContext 提供当前账号昵称(accounts 表的 name,登录包 ZoneLoginRsp 解析),
// 供展示类场景(如分享图标题)使用;未知时为空串。
export const AccountNameContext = createContext('')

// IconsContext 提供全局固定图标(六维属性小图 + 异色/炫彩/污染标记图);App 启动拉一次。
export const IconsContext = createContext({ stat: {} })
