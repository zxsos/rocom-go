# API 文档

rocom-capture 的 HTTP 接口契约。**前端与后端以此为准**，任一端要改契约先改这里的对应条目。

| 文档 | 内容 |
| --- | --- |
| [README.md](README.md) | 通用约定：鉴权、账号隔离、SSE、错误格式、命名 |
| [endpoints.md](endpoints.md) | 全部路由表（方法/路径/鉴权/账号/说明） |
| [schemas.md](schemas.md) | 各接口响应字段（含示例） |
| [fields.json](fields.json) | **机器可读**的字段清单，供脚本消费（自动生成） |

> ⚠️ **`fields.json` 是 golden 样本，不是完备字段清单**：`omitempty` 字段在构造数据
> 取零值时不会出现在 golden 里。做全量改名这类操作须另以 Go 源码为准。
> 好消息是 position / wildpets / gathers / home / flowers / trial 六个实时接口的字段已由
> `internal/server/payload.go` 的 struct 定义，**那才是完备清单**（含 `omitempty` 字段）。

## 事实来源优先级

争议时按此顺序判定，前者覆盖后者：

1. **golden 快照** `internal/server/testdata/contract/*.json` —— 跑真实 handler 落盘的响应，
   由 `internal/server/contract_test.go` 守护。**最高权威。**
2. **载荷类型定义** `internal/server/payload.go` —— position / wildpets / gathers /
   home / flowers / trial 六个实时接口的字段由 Go struct 定义，pipeline 与 server
   共用同一份。
3. 本文档
4. Go struct 的 `json` tag
5. `web/src/api.js` 的注释（前端消费方视角，可能滞后）

> ⚠️ **可选字段用指针而非 `omitempty` 值类型**：`u` / `v` 是归一化坐标（0-1），
> 停在左上角就是合法的 `0`。用值类型 + `omitempty` 会把 `0` 误删，前端读不到该键。
> 见 `PositionPayload` 的注释。

> ⚠️ **无 struct 定义的接口（`map[string]any` 内联拼出的）字段只能以 golden 快照为准**，
> 读代码推字段不可靠 —— 这类接口的字段散落在 handler 里，改一处漏一处。
>
> 2026-09 核对：上面那六个实时接口**都已**有 struct 定义（阶段 3「载荷强类型化」
> 的成果），本条对它们不再适用；仍以内联 `map` 拼响应的是 `debug` 广播与少数
> 管理接口（见 `internal/server/api_debug.go`、`api_admin.go`）。
> 新增实时接口时请照 `GatherPayload` 的样子在 `payload.go` 定义 struct ——
> 好处不只是「有完备字段清单」，还有字段名写错时**编译期就报错**，
> 而不是等前端读到 `undefined` 才发现。

## 重新生成

改了后端响应结构后：

```bash
# 1. 重新生成 golden 快照（务必 review diff，那正是「对外契约变了」的清单）
UPDATE_CONTRACT=1 go test ./internal/server/ -run TestContract

# 2. 重新生成机器可读字段清单
uv run python scripts/gen_apifields.py

# 3. 更新本目录的 endpoints.md / schemas.md
```

CI 会在不带 `UPDATE_CONTRACT` 时比对 golden，JSON 变了就红 —— 这是防止契约被无意改动的护栏。

> ⚠️ **重新生成 golden 时，diff 只能告诉你「变了什么」，不能告诉你「该不该变」。**
>
> 阶段 3 实测踩到过：把 `map[string]any` 改成 struct 时给 `couplesStale` 加了
> `omitempty`，于是 `false` 被省掉、字段从响应里消失。重新生成的 diff 只显示
> 「新增了几个字段」，看上去完全合理 —— **因为根本没有基线可比对**。
>
> 改动实时推送侧（position / wildpets / gathers / home / flowers）后，请用
> `bash scripts/capture_sse.sh <pcap>` 抓真实推送，与改动前的构建逐字段对比。
> 该脚本会先挂上 SSE 再启动回放（回放是一次性的，约 40ms，反序会错过），
> 默认端口 4940（4939 是前端 dev server 的代理目标，别占用）。
