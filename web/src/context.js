import { createContext } from 'react'

// AccountContext 提供当前选中账号(玩家 user_id key),供各页对 SSE 按账号过滤。
export const AccountContext = createContext('')

// AccountNameContext 提供当前账号昵称(accounts 表的 name,登录包 ZoneLoginRsp 解析),
// 供展示类场景(如分享图标题)使用;未知时为空串。
export const AccountNameContext = createContext('')

// IconsContext 提供全局固定图标(六维属性小图 + 异色/炫彩/污染标记图);App 启动拉一次。
export const IconsContext = createContext({ stat: {} })

// AnnotationsContext 提供全服已审核的标注(kind -> code -> {name,desc}),供各页把
// 协议里查不到名字的技能/特性 id 显示成名字。由 components/annotations.jsx 的
// AnnotationsProvider 提供;App 启动拉一次,审核通过的新标注刷新页面后可见。
export const AnnotationsContext = createContext(null)

// MapEngineContext 提供**全局常驻**的地图引擎与画中画控制器:{ engine, pip }。
// 引擎不由 MapPage 自建:画中画要在离开地图页之后继续更新(切页、切出浏览器),
// 故引擎提升到 App 层(见 pages/map/MapEngineProvider.jsx),任何页面共享同一份
// 位置与图层数据。默认 null —— 未挂在 Provider 下时消费方需自行判空。
export const MapEngineContext = createContext(null)
