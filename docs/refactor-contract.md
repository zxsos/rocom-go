# 前后端并行重构 · 交接契约

两个 AI 并行重构：**前端 A**（`web/`）与 **后端 B**（Go 侧）。二者不在同一会话、无法直接对话，
故以本文件为**单一事实来源**：任一方要动跨端契约，先改这里，再动代码。

契约基线快照时间：后端当前 `master` 工作区状态。

---

## 1. 目录所有权（硬边界，越界 = 冲突）

| 范围 | 归属 | 说明 |
| --- | --- | --- |
| `web/**` | 前端 A | React 源码、vite 配置 |
| `internal/server/web/**` | 前端 A（**独占**） | vite `build` 的输出目录 |
| `cmd/**`、`internal/**`（上者除外）、`scripts/**` | 后端 B | Go 与 Python 生成脚本 |

### 为什么 `internal/server/web/` 必须独占

- 后端 `internal/server/server.go:26` 是 `//go:embed all:web` —— 整目录 embed 进二进制。
- 前端每次 `npm run build` 会**整目录重刷**：带哈希的 `assets/index-*.js` 换新名、旧文件删除。
  git 状态里表现为「删除 `index-*.js` + 新增另一个 `index-*.js`」，是正常现象。
- 结论：**后端只读不写该目录**；后端需要改静态资源服务方式时，改 `server.go` 里的
  `handleStatic` / `cacheControl` / `http.FileServerFS`，不碰 embed 内容。

---

## 2. 契约面（前后端唯二的接触点）

1. **路由表**：`internal/server/server.go:126` 起的 `routes()`，约 60 条路由集中注册。
2. **前端调用封装**：`web/src/api.js`，逐个接口注释了响应形状。

后端保证：路径、HTTP 方法、响应 JSON 结构与 `api.js` 中描述的一致。

> **对外契约的权威描述现在在 [`docs/api/`](api/README.md)**（端点表 / 响应字段 /
> 机器可读 `fields.json`）。`api.js` 的注释是前端消费方视角，可能滞后；
> 而四个实时接口（position / wildpets / home / flowers）的**完备字段清单**在
> `internal/server/payload.go` 的 struct 定义里 —— 不在 `fields.json` 里
> （它是 golden 样本，`omitempty` 字段可能缺失）。详见 `docs/api/README.md`。

---

## 3. 冻结项（后端重构不得单方面变更）

以下 4 条是**踩过坑或有强约束**的设计，改动必须由双方确认：

1. **账号隔离 `?account=`**
   后端 `Server.acct()`（`server.go:203`，实现在 `state.go` 的 `acctResolver`）优先取 `?account=`，缺省回退「最近活跃账号」
   （先查内存 `lastSeen` 表，5s 缓存；再回退 `ListAccounts()` 查库）。
   前端 `buildQuery()` 给所有带账号的请求自动拼该参数。

2. **SSE 必须保持单连接 `/api/stream`**（可带 `?debug=1`）
   浏览器对同域 **HTTP/1.1 仅 6 条并发连接**，每处实时数据各开一条 EventSource 会把连接池占满，
   导致底图 webp 与后续 API 排不上队（实测：地图一片空白）。
   前端 `subscribe()` 全局复用一条连接、按 `msg.type` 分发给所有订阅者，**此设计不可退化为多连接**。
   附带约定：SSE 无历史缓存，`onopen`（含断线重连）时前端广播 `{type:'stream-open'}`，
   各订阅者自行补拉一次快照 —— 后端需确保所有快照型 REST 接口可重复拉取且幂等。

3. **鉴权两条**
   - 管理员：请求头 `X-Admin-Token`（内存令牌，服务重启失效），涉及全部 `/api/admin/**`。
   - 账号：PIN，涉及 `/api/account/verify`、`/api/account/pin`、`DELETE /api/account`。
   - 前端据此判定：**401 = 管理员会话失效**（踢回登录页）、账号侧 401 = PIN 错误、429 = 频繁。

4. **JSON 字段名现状（重构前先决定是否统一）**
   当前 **snake_case 与 camelCase 混用**，例如 `spAttack` / `hasCoins` 与 `base_conf_id` 并存。
   前端直接用后端返回的原始字段名，无映射层。若要在重构中统一命名，属**跨端破坏性变更**，
   必须前后端同批改，见第 4 节流程。

### 错误响应格式

后端**不统一**：既有 JSON `{error}`，也有 `http.Error` 的 `text/plain`。
前端 `errorBody()` 两种都兼容（先试 JSON 取 `.error`，失败按纯文本）。
后端若改为统一 JSON，前端可简化，但**不阻塞**后端 —— 属「可选项」。

5. **全局消息必须放行（SSE）** —— 双方确认于 2026-08-29（`[A1]` / `[B3]`）。

   - 后端 `hub.go:12`：`Account` 带 `json:"account,omitempty"`，`""` 表示全局/调试消息。
     带 `omitempty` ⇒ 全局消息**在 JSON 里没有 `account` 字段**，JS 侧是 `undefined`（非 `""`）。
   - 后端过滤 `stream.go:34`：`if msg.account != "" && msg.account != account { continue }`
   - 前端过滤 `api.js:298`：`if (currentAccount && msg.account && msg.account !== currentAccount) return`

   两端语义等价，均放行全局消息。**任一端改成严格比对（`msg.account !== currentAccount`）
   都会静默拦掉全局消息且无任何报错** —— 改动前必须在信箱提案。

### 已确认的「维持现状」项（本次重构不做）

以下由双方确认不动，记录在此避免重复讨论：

- **字段命名维持 snake_case / camelCase 混用**（不统一）。前端无映射层，全量改名是纯机械
  劳动、零用户价值，风险是漏改一处就静默 `undefined`。阶段 0 的契约测试会产出机器可读的
  字段清单，供将来真要统一时做可校验的脚本替换。
- **`web/src/api.js` 保持为唯一后端调用封装**：不合并聚合接口、不加字段映射层、不上 TS 类型生成。
- **SSE 保持单连接**：前端仅重构分发层（`subscribe(type, onData, {onOpen, debug})`），
  URL 拼装与连接数不变。
- **前端不换路由、不换状态管理、不上 TypeScript。**
- **孤儿路由 `GET /api/admin/placeholder` 删除**：前端已删除唯一调用者 `adminPlaceholder()`
  （该函数本就零调用）。后端已连带删除路由注册与 `handleAdminPlaceholder`。
  前端无需配合。

---

## 4. 跨端变更流程

1. 发起方登记到下方「待定变更」（或直接提案给对方）：变更内容、影响的前端/后端位置、
   是否破坏性。
2. 另一方回复 `确认` 或反对意见。
3. 双方确认后**同批落地**；破坏性变更必须一次改完两端，不留中间态。
4. 达成的新共识同步补进本文件（本文件是长期契约）。
5. 三方（前端 / 后端 / 数据分析）同时改动时，改动同一文件前先 `git status`
   确认没有别人在改它 —— 详见 `AGENTS.md`「并行工作的边界」。

> 重构期间曾用根目录两个**单向信箱**沟通（`AI_BACK_TO_FRONT.md` / `AI_FRONT_TO_BACK.md`，
> 每个文件只有一个写者故不会互相覆盖）。**本次重构结束，两个文件已删除。**
> 若将来再次并行改动需要异步沟通，可照此模式重建，并在结束后删除。

---

## 5. 待定变更（待登记）

<!-- 格式：
### [编号] 标题
- 发起方：前端 A / 后端 B
- 破坏性：是 / 否
- 内容：...
- 对方回复：...
-->

_（暂无）_

---

## 6. 协作沉淀（双方共同认可的实践）

这轮重构里两端各自踩坑、互相纠偏得出的结论。它们不是代码约定，是**做事方式**，
但每一条都对应一个真实事故，故记录下来。

### 6.1 验证纪律：改动后必须与基线对比

**改动后重新生成 golden，diff 只能告诉你「变了什么」，不能告诉你「该不该变」。**

- 后端实例：阶段 3 把 `home` 的 `map[string]any` 改成 struct 时给 `couplesStale` 加了
  `omitempty`，`false` 被静默省掉、字段从响应里消失。重新生成的 diff 显示
  「新增 3 个字段」，看着完全合理 —— **因为没有基线可比对**。
  最终靠「用 pcap 跑真实服务、抓 SSE、与 HEAD 基线逐字段对比」才发现。
  工具：`scripts/capture_sse.sh`。
- 前端实例：3 个白屏 bug（Events TDZ、NavBar 两处 `location is not defined`）
  都是 lint 与 build 全绿、靠真实渲染才抓到。

**结论：静态检查全绿 ≠ 真的没坏。** 涉及对外输出或渲染链路的改动，
必须跑一次「与改动前对比」或「真实渲染」。

### 6.1.1 起一个能持续产生 SSE 推送的验收环境

验收「实时刷新」类功能需要**仍在推送**的后端，而不是一次性快照。

- **后端**：`go build ./cmd/rocom-capture` 后
  `./rocom-capture -pcap <样本> -db /tmp/x.db -addr :4939`。
  回放是一次性的（3.3MB pcap 约 40ms 放完），故**先挂 SSE 再起服务**
  才收得到推送 —— `scripts/capture_sse.sh` 封装了这个顺序。
- **前端**：dev server（5173）已配 `/api` 代理到 4939，连自己的后端即可。
- **多账号**：样本通常只有 1 个账号，切账号类功能需手动往 `accounts` 表插第 2 个。
- **工作区编译不过时**：不要去改对方代码。
  `git clone /workspace /tmp/xxx` 拿干净副本另建 —— 并行改动时这是标准规避手段。

### 6.2 `omitempty` 陷阱：零值但也必须存在的字段

值为零值、但语义上**必须出现**的字段，用值类型 + `omitempty` 会被静默省掉：

| 字段 | 零值 | 后果 |
| --- | --- | --- |
| `couplesStale: false` | `false` | 字段消失（已用内嵌 `*HomeMeta` 修复） |
| `u` / `v`: 归一化坐标 | `0` | 玩家在地图左上角时坐标消失 |

**应该用指针**（`*float64`）或**内嵌结构体指针**（保证一组字段同进同退）。
反例见 `internal/server/payload.go` 的 `HomePayload` 与 `PositionPayload` 注释。

### 6.3 SSE 消费的两种形态

验收时只查「有没有发请求」会漏掉**直接更新型**页面：

- **补拉型**：收到事件后再发请求取数据（如 `/map` 触发 `/api/pois`、`/eggs` 触发 `/api/eggs`）
- **直接更新型**：广播本身带全量数据，直接 `setState` 即可（如 `/flowers`）

前端验收脚本 `web/scripts/verify-live-sse.mjs` 的判据已改为
「补拉 **或** DOM 出现事件数据」，两种都算消费成功。

### 6.4 全局消息放行（SSE）—— 前端一律读 envelope 的 `msg.account`

不消费 payload 内的 `account` 字段。各 type 的情况：

| type | `account` 的取值 |
| --- | --- |
| `pet` / `event` | `0`（全局） |
| `accounts` | 空 |
| `eggs` | 按连接账号 |

判定一律以 **envelope 顶层的 `msg.account`** 为准。
背景与两端实现见第 3 节冻结项第 5 条。

---

## 7. 后端侧已知的关注点（供前端参考，非承诺）

- 前端首屏会**并行发 5-8 个 API**，每个都触发账号推导。后端已用 5s 缓存
  （`state.go` 的 `acctResolver`）+ 内存在线表（`onlineTracker`）消除重复查库开销；
  若前端改为「一次聚合接口」，后端需新增对应聚合路由。
- 静态资源缓存：`/img/` 下 embed 的 webp 为 `max-age=86400`（版本变更时内容随之变），
  其余经 `handleStatic` 的 SPA fallback 处理。

---

## 8. 本次重构的收尾状态

四阶段（契约测试护栏 → 拆上帝文件 → 拆上帝对象 → 载荷强类型化）已完成，
并补齐了 `internal/pipeline` 的测试（原 0% 覆盖）。

期间修复的真实缺陷：

| 缺陷 | 性质 |
| --- | --- |
| `gopacket` 构建失败 | 误判为依赖不兼容，实为环境缺 C 开发包致 cgo 被禁用 |
| `handleMerchantSub` 注释与函数分家 | 拆分时注释留在 A 文件、函数体去了 B 文件 |
| `sweepInjects` 死代码 | `type snap` / `var todo` 声明后未用，靠 `_ = todo` 压制报错 |
| `events-stats` golden 跨天失效 | `daily` 是 30 天滑动窗口，同日测试查不出来 |
| `RemoveFlowerItem` 原地改共享 map | 可触发 Go map 并发读写 **fatal error**（进程崩溃，不可 recover） |
| `home` 的 `couplesStale` 丢失 | 改用 struct 时误加 `omitempty`，`false` 被静默省掉 |

最后一条尤其值得记：它是靠「与 HEAD 基线逐字段对比」才发现的，
golden 与 `-race` 都没抓到 —— 因为 golden 是在改动后重新生成的，没有基线可比对（见 6.1）。

重构期间的两个沟通文件（`AI_BACK_TO_FRONT.md` / `AI_FRONT_TO_BACK.md`）已按约定删除；
需长期留存的共识都在本文件与 `docs/api/`、`AGENTS.md` 里。
