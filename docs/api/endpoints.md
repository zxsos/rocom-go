# 端点总表

路由集中注册在 `internal/server/server.go:135-195`。

**鉴权列**：`—` 无 / `admin` 需 `X-Admin-Token` / `pin|admin` 需账号 PIN 或管理员。
**账号列**：`✓` 表示按 `?account=` 隔离（缺省回退最近活跃账号），`—` 表示全局固定数据。

## 宠物与事件

| 方法 路径 | 鉴权 | 账号 | 说明 |
| --- | --- | --- | --- |
| `GET /api/pets` | — | ✓ | 宠物列表，分页/筛选/排序 |
| `GET /api/pets/{gid}` | — | ✓ | 单只宠物详情 |
| `GET /api/pet-page` | — | ✓ | 某宠物在筛选下所处页码（盒子示意图跳页） |
| `GET /api/events` | — | ✓ | 获得事件历史 |
| `GET /api/events/count` | — | ✓ | 事件总数（累计获得宠物数） |
| `GET /api/events/stats` | — | ✓ | 事件统计（总览/稀有/近30天/热门形态） |
| `DELETE /api/events` | — | ✓ | 清空事件历史，返回 204 |
| `GET /api/filter-options` | — | ✓ | 筛选下拉可选值 |
| `GET /api/stats` | — | ✓ | 宠物总数 |
| `GET /api/boxes` | — | ✓ | 盒子槽位布局 |
| `GET /api/teams` | — | ✓ | 大世界三队布局 |
| `GET /api/evolution` | — | — | 某 `base` 的进化链（按阶段升序） |

### `GET /api/pets` 查询参数

经 `parseFilter`（`internal/server/api_pets.go:16`）构造：

| 参数 | 说明 |
| --- | --- |
| `search` | 昵称/种类模糊 |
| `nature` / `natureExclude` | 性格（含/排除，逗号分隔） |
| `gender` `talentRank` `speciality` `form` `partnerMark` | 等值筛选 |
| `medal` | 奖牌**名**（后端解析为 id 列表，同名多枚全含） |
| `eggGroup` `types` | 蛋组 / 系别（`types` 逗号分隔） |
| `shiny` `colorful` | 异色 / 炫彩 |
| `medalBig` `medalSmall` `medalHigh` `medalLow` | 奖牌特征，`1` 启用（体重百分位 ≥98 / ≤2；嗓音 ≥96 / ≤-96） |
| `box` | 盒子 |
| `catchAfter` | 捕捉时间下界（Unix 秒） |
| `levelMin` `levelMax` | 等级区间 |
| `sort` `order` | 排序字段与方向 |
| `page` `pageSize` | 分页 |

## 全局固定数据

| 方法 路径 | 鉴权 | 账号 | 说明 |
| --- | --- | --- | --- |
| `GET /api/medals` | — | — | 全部奖牌（id/name/desc/icon） |
| `GET /api/name-options` | — | — | 全量特长名 |
| `GET /api/icons` | — | — | 六维属性小图 + 异色/炫彩/污染标记图 |
| `GET /api/accounts` | — | — | 已知账号列表（含在线态、今日称号、平台头像 URL） |
| `GET /api/leaderboard` | — | — | 排行榜（福布斯/盈亏/今日称号/我） |

## 实时地图

| 方法 路径 | 鉴权 | 账号 | 说明 |
| --- | --- | --- | --- |
| `GET /api/position` | — | ✓ | 最近一次位置；过期则抹掉 `vu`/`vv`/`path` |
| `GET /api/pois` | — | — | 某场景（`?res=`）大地图 POI 图层 |
| `GET /api/wildpets` | — | ✓ | 最近一次野生宠物标记 |
| `GET /api/paint` | — | ✓ | 涂地覆盖位图（`?res=&layer=`，cells 为 base64 位图） |
| `DELETE /api/paint` | — | ✓ | 清空涂地，并广播 `{reset:true}` |
| `GET /api/home` | — | ✓ | 最近一次家园小窝图层 |
| `GET /api/trial` | — | ✓ | 最近一次草系徽章试炼状态（进行中的一局 + 账号档案） |
| `GET /api/trial/encounters` | — | ✓ | 草系试炼**遇见记录**（三章各一张精灵图，**读库累积**，不经 SSE） |

## 花种

| 方法 路径 | 鉴权 | 账号 | 说明 |
| --- | --- | --- | --- |
| `GET /api/flowers` | — | ✓ | 最近一次花种分组（**剥掉**内部 `cur`/`worlds`，见 `flowerView`） |
| `GET /api/flowers/slots` | — | ✓ | 花种世界槽列表（把内部 `worlds` 转成 `slots[]` 暴露，**非透传**） |
| `DELETE /api/flowers/slots` | — | ✓ | 删好友世界槽（`?key=`；`self` 槽拒绝） |

## 精灵蛋

| 方法 路径 | 鉴权 | 账号 | 说明 |
| --- | --- | --- | --- |
| `GET /api/eggs` | — | ✓ | 背包精灵蛋（库里即背包现状） |
| `GET /api/eggs/query` | — | ✓ | 查随机蛋可能孵出的物种（数据源由服务端配置，管理面板切换） |
| `GET /api/handbook-glasses` | — | ✓ | 图鉴炫彩收集（按品种聚合，图鉴号升序） |

## 远行商人

| 方法 路径 | 鉴权 | 账号 | 说明 |
| --- | --- | --- | --- |
| `GET /api/merchant` | — | — | 营业状态与 4h 槽缓存；`?force=1` 强制回源第三方 |
| `GET /api/merchant/sub` | — | ✓ | 当前账号订阅状态 |
| `POST /api/merchant/sub` | — | ✓ | 订阅/更新（body: `email`, `keywords`） |
| `DELETE /api/merchant/sub` | — | ✓ | 退订 |

`GET /api/merchant` 顶层多一个 `source` 字段：当前生效的数据源标识（`xianyu` / `haoyou`）。
**前端不展示它** —— 两个源出自不同第三方，但玩家无从判断该信哪个，标注只会平添困惑；
该字段是为排查留的（收到「货单不对」的反馈时，`curl` 一下就知道当时是哪个源在供数）。
两源返回的**货单结构一致**（好游快爆源在服务端归一化了，见 `docs/data.md` 第 6 节）。
**连商品图都是同一个图床**：两源的 `image` 都是 `patchwiki.biligame.com` 的 https 直链
（2026-09-03 用咸鱼源真实响应核对过），不存在本地相对路径。前端 `imgSrc` 与订阅邮件
`merchantMailItemImg` 都只认 http(s) 外链，**其余取值一律不显示**（不拼本站路径、也不内嵌）。

> 邮件里的图片是外链，客户端可能默认拦截远程图片（QQ 邮箱 / Outlook 要手动点「显示图片」）。
> 若哪天第三方改回相对路径，表现是**商品图静默消失**（不报错、发信照成功），
> 排查时先看 `items[].image` 的实际取值。

> 第三方令牌与 SMTP 配置在服务端。未配置令牌时：`source` 为 `xianyu` 的接口返回 **503**
> （前端提示未配置，或建议切到无需令牌的好游快爆源）；`source` 为 `haoyou` 时照常返回 ——
> 该源抓的是公开页面，不需要令牌。

## 账号安全与排行榜

| 方法 路径 | 鉴权 | 账号 | 说明 |
| --- | --- | --- | --- |
| `POST /api/account/verify` | — | — | 校验 PIN（body: `account`, `pin`） |
| `POST /api/account/pin` | `pin|admin` | — | 设置/修改/清除 PIN（body: `account`, `oldPin`, `newPin`） |
| `DELETE /api/account` | `pin|admin` | — | 删除账号及全部数据（body: `account`, `pin`） |
| `POST /api/account/rank` | — | — | 排行榜参与开关（body: `account`, `join`） |

## 管理员

面板是隐式的（前端 `#/admin`，导航不显示）。除 `setup` / `login` / `status` 外均需
`X-Admin-Token`；未登录返回 **401**。

| 方法 路径 | 鉴权 | 说明 |
| --- | --- | --- |
| `GET /api/admin/status` | — | 密码是否已配置、当前是否已登录 |
| `POST /api/admin/setup` | — | 首次设置密码，成功即登录 |
| `POST /api/admin/login` | — | 密码登录，返回 token |
| `POST /api/admin/logout` | admin | 注销 |
| `GET /api/admin/rules` | admin | 黑白名单列表 |
| `POST /api/admin/rules` | admin | 新增/更新规则（body: `account`, `mode`, `note`） |
| `DELETE /api/admin/rules` | admin | 删除规则（`?account=`） |
| `GET /api/admin/stats` | admin | 全部成员抓捕统计 |
| `GET /api/admin/play-sessions` | admin | 游玩记录明细分页 + 汇总（`?account=&limit=&offset=`，返回 `total` 为同筛选下总条数） |
| `GET /api/admin/egg-stats` | admin | 查蛋 API 使用统计 |
| `GET /api/admin/wild-pets` | admin | 可投放的野生宠物形态 |
| `GET /api/admin/injects` | admin | 当前注入中的精灵列表 |
| `POST /api/admin/inject-wild` | admin | 投放稀有野生精灵 |
| `DELETE /api/admin/inject-wild` | admin | 撤销注入（`?account=&id=`） |
| `POST /api/admin/inject-flower` | admin | 投放假炫彩花种 |
| `GET /api/admin/merchant-subs` | admin | 商人邮件推送名单 |
| `DELETE /api/admin/merchant-subs` | admin | 删除某邮箱订阅（`?email=`） |
| `POST /api/admin/merchant-test-mail` | admin | 发测试邮件验证 SMTP |
| `GET /api/admin/merchant-source` | admin | 远行商人数据源：`{source, keySet, sources:[{id, name, needKey}]}` |
| `POST /api/admin/merchant-source` | admin | 切换数据源（body: `source`）；会清空已缓存货单并按新源重抓当前轮 |
| `GET /api/admin/egg-source` | admin | 查蛋数据源：`{source, keySet, sources:[{id, name, needKey}]}` |
| `POST /api/admin/egg-source` | admin | 切换查蛋数据源（body: `source`，`local`\|`xianyu`）；立即生效，不清缓存 |

> `GET /api/admin/placeholder` 曾为占位接口，前端已删除唯一调用者，
> 后端连带删除（见 `docs/refactor-contract.md`）。

## 其它

| 方法 路径 | 鉴权 | 说明 |
| --- | --- | --- |
| `GET /api/stream` | — | SSE 实时推送，见 [约定 §3](conventions.md#3-sse全站共用一条连接) |
| `POST /api/debug/parse` | — | 把某条消息的原始数据解析成可读树（调试页） |
| `GET /img/**` | — | embed 的 webp 静态资源，`max-age=86400` |
| `GET /` | — | SPA 静态资源与 hash 路由 fallback |
