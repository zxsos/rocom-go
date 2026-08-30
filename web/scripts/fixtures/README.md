# 运动平滑回归 fixture 的来源与重制方法

`tap-move.json` / `continuous.json` 是**真实抓包**的派生数据，不是手搓的。它存在的唯一目的：
让 `verify-motion.mjs` 能在没有 pcap、没有后端的环境下复现「箭头平不平」的度量。

## 两份 fixture 为什么都要有

只有一份时极易**过拟合**：曾只有 `tap-move.json`（点触式走走停停，间隔 p95 2.6s），
据此把外推调成「全时程速度衰减」，单看这份数据一切变好；补上 `continuous.json`
（连续移动，间隔 p95 0.97s）后才发现它在这个更常见的场景上把偏差从 2.7m 劣化到 4.2m。

两份数据的移动特征差异很大，任何只在一边变好的改动都不该被采纳：

| | tap-move | continuous |
| --- | --- | --- |
| 包数 / 时长 | 137 / 80.6s | 1088 / 268.5s |
| 上报间隔 中位 / p95 | 0.14s / 2.61s | 0.12s / 0.97s |
| stop 包占比 | 33% | 9% |
| 峰值速度 | 2917 cm/s | 2980 cm/s |
| 侧重点 | 启停、长静默 | 跟手、稳态抖动 |

## 内容

137 个 `ZONE_SCENE_MOVE_REQ`(0x0133) 移动包，按 **pipeline 推给前端的形状**导出：

| 字段 | 含义 |
| --- | --- |
| `side` | 场景边长（厘米），卡洛西亚大陆 `scene_res_cfg_id=10003` 为 408000 |
| `pkts[].t` | 包时刻（秒，相对首包） |
| `u` / `v` | `to_pos` 经 `gamedata.Project` 投影的归一化底图坐标 |
| `vu` / `vv` | `speed`（UE 厘米/秒）按同一投影换算的「归一化坐标/秒」；stop 包为 0 |
| `stop` | `stop_move`；**仅在为 true 时出现**（与 `PositionPayload` 的 omitempty 语义一致） |
| `path` | `move_seg_list` 补报的真实轨迹，仅当 `SegSpan() >= 0.6` 时才带（同 `buildPos` 的 `minSegSpan` 判据） |

`path[].t` 是各轨迹点的**服务器时间戳**（秒）——后端目前不下发它（前端按弧长回放），
但它是求"真值"的关键，故由导出脚本补上。若哪天后端开始下发，度量会更准。

## 重制方法

    go run ./cmd/movean -out web/scripts/fixtures/tap-move.json   a.pcap
    go run ./cmd/movean -out web/scripts/fixtures/continuous.json b.pcap

`cmd/movean` 复用 `internal/scene` 的解析与 `internal/gamedata` 的投影（与 `pipeline.buildPos`
同款判据，含 `SegSpan >= 0.6` 才带 path、末点补 `to_pos`）。它还会打一份数据画像
（包数 / 时长 / 间隔分位 / stop 占比 / 峰值速度 / 移动模式分布 / stop 后沉默时长），
用来判断新抓的包覆盖了哪些移动特征。

**不要用别的方式生成**：自己另写一份解析迟早会和后端跑偏，度量的就成了假数据。

## 更换 fixture 的注意事项

- **门槛要跟着重算**。数字依赖具体抓包的移动特征（本份是"点触式走走停停"：
  45/137 是 stop 包，骑乘峰值 29m/s）。换一份巡航为主的包，绝对值会变。
- 别为了让测试变绿而下调门槛而不写理由 —— 见 `verify-motion.mjs` 文件头。
