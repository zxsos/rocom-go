# 通用约定

## 1. 账号隔离 `?account=`

账号是玩家的 `user_id` 派生的 key，形如 `UID:10001`。

- **带账号的数据接口**：请求需带 `?account=<key>`。缺省时后端**回退到「最近活跃账号」**
  （先查内存 `lastSeen` 表，5 秒缓存；再回退查库，见 `internal/server/server.go:216`）。
- **全局固定数据接口**（图标、奖牌、特长名、进化链、账号列表、排行榜等）**不带** `account`。

前端 `web/src/api.js` 的 `buildQuery()` 给所有带账号的请求自动拼该参数，调用方无需手传。

> 缺省回退是**有意设计**：前端首次加载时 `currentAccount` 可能为空，靠它撑过首屏。
> 前端**不得**假设当前账号非空。

## 2. 鉴权

两条互不相关的链路：

### 管理员：`X-Admin-Token` 请求头

- 覆盖全部 `/api/admin/**`。
- 令牌是**内存**的（服务重启即失效，需重新登录），存前端 `localStorage` 的 `adminToken`。
- 密码经 `POST /api/admin/setup`（首次）或 `POST /api/admin/login` 换取。
- 未登录返回 **401**。前端据此踢回登录页。

### 账号 PIN

- 覆盖 `/api/account/verify`、`/api/account/pin`、`DELETE /api/account`。
- 账号侧 **401 = PIN 错误**（与管理端 401 语义不同，勿混用）。
- 未设 PIN 的账号只能由管理员删除，普通请求返回 **403**。
- **429** = 尝试过于频繁（限流）。

## 3. SSE：全站共用一条连接

`GET /api/stream`（可带 `?account=`、`?debug=1`）

**必须保持单连接。** 浏览器对同域 **HTTP/1.1 仅 6 条并发连接**，若每处实时数据各开一条
`EventSource`，连接池会被占满，底图 webp 与后续 API 排不上队（实测后果：地图一片空白）。
前端 `subscribe()` 全局复用一条连接、按 `msg.type` 分发给所有订阅者，此设计不可退化为多连接。

消息格式：

```json
{ "type": "position", "account": "UID:1", "data": { ... } }
```

- `type`：`pet` / `event` / `position` / `wildpets` / `stars` / `starzones` / `paint` /
  `home` / `flowers` / `eggs` / `debug` / `accounts`
- `account`：**带 `omitempty`** —— 值为 `""`（全局消息）时**该字段不出现在 JSON 里**，
  JS 侧读到 `undefined`（不是空串）。

### 全局消息必须放行（易踩坑）

后端 `internal/server/hub.go:12` 中 `Account` 带 `omitempty`，`""` 表示全局/调试消息。
两端的过滤必须放行它们：

```go
// 后端 internal/server/stream.go:34
if msg.account != "" && msg.account != account { continue }
```
```js
// 前端 web/src/api.js:297
if (currentAccount && msg.account && msg.account !== currentAccount) return
```

**任一端改成严格比对（`msg.account !== currentAccount`）都会静默拦掉全局消息，且无任何报错。**

### 断线补拉

SSE 无历史缓存，断线期间的增量就丢了。前端在 `es.onopen`（含原生重连）时广播
`{type:'stream-open'}`，各订阅者自行补拉一次快照。
**后端必须保证所有快照型 REST 接口可重复拉取且幂等。**

### `debug` 流

逐条 opcode 的高频调试流量**默认不推送**，仅调试页显式带 `?debug=1` 时才发，
避免其它页面白拉。

## 4. 错误响应格式

后端**不统一**，两种都可能出现：

| 形式 | 场景 |
| --- | --- |
| JSON `{ "error": "..." }` | 部分 handler |
| `text/plain` | `http.Error`（多数 4xx/5xx） |

前端 `errorBody()` 两种都兼容：先试 JSON 取 `.error`，失败按纯文本。

常用状态码：

| 码 | 含义 |
| --- | --- |
| 200 | 成功 |
| 204 | 成功无内容（`DELETE /api/events`） |
| 400 | 参数错误 |
| 401 | 管理员会话失效 / 账号 PIN 错误 |
| 403 | 账号未设 PIN，需管理员操作 |
| 404 | 资源不存在 |
| 429 | 请求过于频繁（PIN 限流） |
| 500 | 服务端错误（多为数据库） |
| 503 | 服务端配置无法落盘（`/etc/rocom.env` 不可写），或 Web 监听未托管 |

## 5. 字段命名

**snake_case 与 camelCase 混用**，例如 `spAttack` / `hasCoins` 与 `base_conf_id` 并存。
前端直接使用后端返回的原始字段名，**无映射层**。

这是历史现状，不是规范。若要统一为 camelCase，属跨端破坏性变更：
60 个接口的字段名散在 35 个组件里，需前后端同批改。
`docs/api/fields.json` 就是为此准备的机器可读清单，可据此做可校验的脚本替换。

## 6. 静态资源

| 路径 | 内容 | 缓存 |
| --- | --- | --- |
| `/` | SPA（`index.html` + hash 路由 fallback） | — |
| `/img/**` | embed 的 webp（宠物图、图标、大地图瓦片） | `max-age=86400` |

前端组装图片 URL：`/img/` + 接口返回的路径（如 `HeadIcon/3006.webp`）。

## 7. 时间与数值

- 时间戳多为 **Unix 秒**（`catchTime`、`ts`、`endTs`、`obtainedAt`）；
  位置包的 `tsMs` 是**毫秒**（前端据此判断缓存是否过期）。
- `u` / `v` 是**底图归一化坐标**（0-1），后端已投影，前端直接乘底图宽高。
- 可能超出 JS 安全整数的 id（`actor_id`、花种 `npcLogicId`、窝 `furniture_guid`）
  一律以**字符串**下发。
