# pcap 分析：草系徽章试炼（2026-08-31）

样本 `PCAPdroid_31_8月_22_54_57.pcap`（4.27 MB，3749 条消息，140 种 opcode）。
整份抓包就是**一局完整的「草系徽章-野外研究院」试炼**（opcode 族 `ZONE_GRASS_TRIAL_*`），
仓库此前对这族 opcode **零支持**（只在生成物 `opmsg.json`/`names.json` 里有名字），本文把它摸清。

> **时间口径**：pcapdump 打印的是 **UTC**（`t=14:55:06`），文件名 `22_54_57` 是本地 UTC+8。
> 登录包 `login_time=1788188106` = `2026-08-31 14:55:06 UTC` = 北京 22:55:06，与文件名吻合。

## 1. 会话概况

| 项 | 值 |
| --- | --- |
| 账号 | uin `100000002`，昵称「示例玩家」，玩家等级 68，世界等级 5 |
| 时段 | 14:55:06 → 15:20:25 UTC（约 25 分钟，抓包在试炼刚结束时截断） |
| 宠物总数 | **683 只**（`0x1346` 14 页 ×50，末页 33 只；1..14 页连续到达，对账逻辑可正常触发） |
| 场景 | 卡洛西亚大陆(103/10003) → 传送至 **草系徽章-野外研究院(601/60008)**；试炼结束回到大陆 |
| 战斗 | 17 场，**全胜**：15 × `WIN_DEFEAT(18)` + 2 × `WIN_HP(66)`（后者是 8200000xx 那类特殊判定） |
| 试炼宠物 | gid 133 **黑猫巫师**（conf 3568003，Lv60，普系，`slot_id=1000`） |

时间线（UTC）：

```
14:55:06  0x0102 登录(165KB) → 0x0152 进场景(大陆)
14:55:09  0x1345/0x1346 宠物分页 1..14
14:55:10  0x1958/0x1959 试炼信息(55KB, 全局进度)
14:55:26  0x1a42 传送至首个副本 → 0x015c 落地 601/60008
14:55:51  0x197c/0x197d 查询可挑战章节  → 3000/3001/3002, effect 1001/1008/1006/1010/1003, 初始金币 10
14:55:53  0x1950/0x1951 开始挑战(pet 133, 带 3 个初始技能) → 0x195c 下发 challenge_data
14:56:02  0x1952/0x1953 进入试炼场景
14:56:10  0x1961/0x1962 推进节点 → 节点是「祝福」:三选一
14:56:14  0x196f/0x1970 选祝福 → 候选技能3选1
14:56:19  0x1971/0x1972 确认祝福(effect 0, 选 7120100)
14:56:29  0x1965/0x1966 刷新节点选项(花金币)
14:56:30  0x1963/0x1964 选事件 → 进战斗(0x1316) …
14:56:46  0x132c 战斗胜利 → 0x1967 节点奖励通知 → 0x1968/0x1969 处理奖励
   …（3 章 × 8 节点 = 24 个节点，穿插战斗/祝福/商店）
15:07:22  0x1978/0x1979 商店购买(6 次)
15:15:54  0x196d/0x196e 进入 BOSS 战(0x1316 #17, 10982B 最大一场)
15:19:52  0x132c #17 胜利(3638B) → 0x15e0 星芒 / 0x1595 任务面板
15:19:56  0x0243 奖励通知：把 gid 133 的完整 PetData 吐回来(试炼结算)
15:20:05  0x01df 世界地图增量 → 回到大陆
```

## 2. 玩法还原

一局试炼 = **选一只自己的宠物 → 连打 3 章（每章 8 个节点）→ 每节点是事件/战斗 → 战利品
（技能·特性·碎片·金币）当场装配 → 章末商店 → 章末 BOSS**。试炼里的宠物是一份**独立副本**
（`trial_pet_data`：血量/能量上限/技能槽/融合威力/已获特性），真实 `PetData` 只是作为底本内嵌。

关键 opcode（全部能被 `pcapdump` 精确解码，描述符对得上）：

| opcode | 消息名 | 方向 | 关键字段 |
| --- | --- | --- | --- |
| 0x1958/9 | `GrassTrialGetInfo{Req,Rsp}` | c2s/s2c | `trial_data{challenge_data, progress_data}` + `banned_skill_ids[]`(18 条禁用技能) |
| 0x197c/d | `GrassTrialQueryChallenge{Req,Rsp}` | c2s/s2c | Req `trial_conf_id/pet_gid/slot_id`；Rsp `effective_chapter_ids[]`、`trial_effect_ids[]`、`initial_coin` |
| 0x1950/1 | `GrassTrialStartChallenge{Req,Rsp}` | c2s/s2c | Req `trial_conf_id/pet_gid/initial_skill_ids[3]/first_dungeon_id/slot_id`；Rsp `ret_info.goods_reward` 内嵌完整 PetData |
| 0x1952/3 | `GrassTrialEnterScene{Req,Rsp}` | c2s/s2c | 空消息 |
| 0x195c | `GrassTrialChallengeDataSyncNotify` | s2c | 全量 `challenge_data`（见下） |
| 0x1961/2 | `GrassTrialNextNode{Req,Rsp}` | c2s/s2c | Req `chapter_id/node_index/npc_obj_id`；Rsp `node_selection` / `bless_selection{event_conf_id, options[]{option_conf_id,is_infeasible}}` |
| 0x1963/4 | `GrassTrialSelectEvent{Req,Rsp}` | c2s/s2c | Req `event_index`；Rsp `bless_selection` |
| 0x1965/6 | `GrassTrialNodeRefresh{Req,Rsp}` | c2s/s2c | Req `node_index/slot_index/refresh_type`；Rsp `new_selection.node_events[]`（全量重掷）+ `total_refresh_cost` + `remaining_coin` |
| 0x1967 | `GrassTrialHandleRewardNotify` | s2c | `event_conf_id/reward_id/cur_coin` + 可选 `extra_reward_ids[]`(碎片) |
| 0x1968/9 | `GrassTrialHandleReward{Req,Rsp}` | c2s/s2c | Req `action/reward_id/target_slot_pos`；Rsp `updated_pet` + `remaining_coin` |
| 0x196f/70 | `GrassTrialBlessSelect{Req,Rsp}` | c2s/s2c | Req `option_conf_id`；Rsp `pending_step{effect, candidate_skills[]}` + `updated_pet` |
| 0x1971/2 | `GrassTrialBlessConfirm{Req,Rsp}` | c2s/s2c | Req `effect/chosen_skill_id/action` 或 `effect/target_slot_pos/second_target_slot_pos`；Rsp `pending_step/updated_pet/finished` |
| 0x1975 | `GrassTrialProgressDataSyncNotify` | s2c | 全量 `progress_data`（55 KB，见 §4） |
| 0x1978/9 | `GrassTrialShopBuy{Req,Rsp}` | c2s/s2c | Req `item_index`；Rsp `updated_item{item_type,item_id,price,is_purchased,index}` + `updated_pet` + `remaining_coin` |
| 0x196d/e | `GrassTrialBossBattleEnter{Req,Rsp}` | c2s/s2c | 空消息 |
| 0x1a42/3 | `GrassTrialTeleportFirstDungeon{Req,Rsp}` | c2s/s2c | 空消息（传送到试炼营地） |

`challenge_data`（0x195c，共 4 次：3 章开头各一次 + BOSS 前一次）：

```
state=2  trial_conf_id=10002  current_chapter_id=3000/3001/3002
slot_id=1000  challenge_id=3891795859771228411  first_dungeon_id=600001
initial_skill_ids[3]  initial_feature_ids=288135  active_trial_effect_ids[5]
remaining_coin  fusion_type=3  skill_fuse_max_time=2  accumulated_play_seconds=0
trial_pet_data{ pet_gid, base_conf_id, current_hp, max_hp, level, energy_ceiling, growth,
                skills[]{base_skill_id, fused_power, fused_energy_cost, skill_type, slot_pos,
                         merged_skill_ids[], fusion_count},
                acquired_feature_ids[], acquired_shard_effect_ids[], equipped_skill_slots[],
                pet_data{ …完整 PetData… } }
```

血量随章节推进（389 → 264/389 → 414/459 → 529/529），`growth` 恒为 2。

### 2.1 实测取值语义

- **奖励 id 分三类**（按数值区间，与 `updated_pet` 落点一一对应）：
  `7xxxxxx` = 技能（进 `skills[]`）、`288xxx` = 特性（进 `acquired_feature_ids`）、
  `20xx/30xx` = 碎片（进 `acquired_shard_effect_ids`）。
- **0x1968 的 `action`**（26 条请求逐条对照 `updated_pet` 推出）：
  | action | 含义 | 证据 |
  | --- | --- | --- |
  | 0 | 融合到 `target_slot_pos` 指定槽位的技能上 | 7020590→槽2：`7880058` 的 `merged_skill_ids` 加上 7020590 |
  | 1 | 作为**新技能**学习，占一个新槽位 | 7130130→槽5、7120100→槽6 |
  | 2 | 直接收下（特性/碎片直接入账，金币不变） | 288001/288022/2016/3005… |
  | 3 | 不要，折算成金币（金币 +1~+2，技能/特性不入账） | 7050340、7030230、288221 |
  | 4 | 无 `reward_id`，纯刷新试炼宠物状态（买完东西/关面板时发） | 2 次 |
- **祝福 `effect`**：`0` = 从 `candidate_skills[3]` 里选一个学会（`chosen_skill_id`）；
  `9` = 合并两个技能槽（`target_slot_pos` + `second_target_slot_pos`）。
- **节点刷新**：针对「节点内某个 `slot_index`」重掷，服务器回**整节新选项**
  （`node_events[]` 全量，含 `event_conf_id/reward_id/random_skills[]/used_reward_ids[]/level`
  与两种报价 `event_refresh_cost`/`reward_refresh_cost`）。
  **同一节点内刷新次数越多越贵**（实测单次 1 → 2 → 3，`total_refresh_cost` 是累计值 1 → 3 → 6）。
  `refresh_type` 有 1/2 两种，差别未实测确认（两者都会重掷槽位内容）。
- **商店** `item_type`：`3` = 碎片（item_id 2016，价 4）、`2` = 特性（item_id 288154，价 6）。
- **金币**：`initial_coin=10`，来源=战斗胜利/节点奖励，去向=刷新节点与商店购买，全程在 0~24 波动，
  进 BOSS 前被花到 0。
- **`slot_id` 就是属性系图鉴槽位**：`1000 + (dam_type - 2)`，即 1000=普、1001=草、1002=火、1003=水、
  1004=光、1005=地…1017=幻（18 个，与 `progress_data.handbook_slots` 的 18 个 `dam_type` 一一对应）。

## 3. 一局到底的报文模板

```
0x1a42 传送进营地 → 0x197c 查章节 → 0x1950 开局（带 3 个初始技能）
  → 0x195c challenge_data
  ┌ 每个节点 ────────────────────────────────────────────────
  │ 0x1961 推进节点 → (0x1965 刷选项) → 0x1963 选事件
  │    ├ 战斗节点：0x1316 进战 → 0x132c 胜利 → 0x1967 奖励通知 → 0x1968 处理
  │    └ 祝福节点：0x196f 选祝福 → 0x1971 确认
  └────────────────────────────────────────────────────────
  → 章末 0x1978 商店购买 → 0x1975 全量进度同步(55KB)
  → 0x196d 进 BOSS → 0x132c 胜利 → 0x0243 归还宠物(GT_PET) → 回到大世界
```

## 4. 全局进度 `progress_data`（0x1975，55 KB，4 次全量下发：15:04:45 / 15:11:51 / 15:12:32 / 15:14:49）

这是**账号级**的试炼档案，与当前是否在进行无关，是本样本里最有统计价值的一块：

| 结构 | 内容 | 实测 |
| --- | --- | --- |
| `review_records[250]` | 战绩回顾（滚动保留 250 条）：`settle_timestamp / petbase_conf_id / pet_level / pet_growth / trial_conf_id / is_victory / challenge_duration / slot_id / mutation_type / glass_info / is_first_victory` + `node_records[]`(1586) + `review_skills[]`(1117) + `review_feature_ids[]`(632) + `review_shard_ids[]`(601) | 胜率 **22/250 = 8.8%**；用时 min 6s / p50 246s / p90 1961s / max 7077s（含 6 秒即弃的趟数）；`trial_conf_id` 10002×233 / 10001×10 / 10000×7；等级几乎全是 60；`mutation_type` 炫彩 8×85、异色 1×79、普通 0×66、异色+炫彩 9×20 |
| 常用形态 top5 | 花衣蝶(3141)×56、酷拉(3189)×18、黑猫巫师(3569)×18、龙息帕尔(3182)×17、影狸(3066)×13 | |
| `handbook_slots[18]` | 按属性系的图鉴槽位：`slot_id/slot_type/dam_type` + `rewards[3]`（每个 `trial_conf_id` × `is_cleared`） | 18 系 × 3 难度 = 54 项**全部 `is_cleared=true`**，即该账号已用全部 18 个系通关过 3 个难度 |
| `log_records[3]` | 见闻录三册（`log_conf_id` 100/101/102，各 3 个 `chapter_id`）：`discovered_petbase_ids[]` / `total_petbase_count` / `is_unlocked` / `stage_rewards[12]`（`seen_pet_num` + `stage_reward`） | 167/210、128/317、98/199 |
| `cleared_trial_ids[3]` | 已通关的试炼 conf：10000/10001/10002 | |
| `challenge_inc_id=251` | 累计挑战次数自增 id | |
| `progress_final_reward` | 最终奖励 `{id=86001000, state=REWARD_STATE_TYPE_GOT(2)}` | |

## 5. 与本项目现状的关系（踩坑提示）

1. **现有管线对这族 opcode 完全没处理**，不会误动数据。唯一会碰到的是末尾那条 `0x0243`
   （`GT_PET`/`OT_SET`/id=133，内嵌完整 PetData）——`pipeline.applyNewPet` 走 `UpsertPet` 的
   `isNew`（按 `account+gid` 查存在性，`internal/store/pet.go:53`），gid 133 早已在库 → **不会误记
   「获得宠物」事件**，只是把这条旧宠覆盖写一遍。
2. **试炼里的一切都不写回真实宠物**。对照试炼前（`0x1346` 里的 gid 133）与结算时（15:19:56
   `0x0243`）的 PetData：技能 19 条逐条相同、等级/形态/血脉（`blood_id=14`）未变；
   融合出来的 `merged_skill_ids`、特性、碎片只活在 `trial_pet_data` 里。
   开局 `initial_skill_ids`（7020500/7020550/7170210）**本来就是宠物已会的技能**（试炼前技能表里就有），
   不是试炼新给的。
3. **`0x0220 ZONE_HANDBOOK_CHANGE_NOTIFY` 在本样本里出现了 117 次**，成对紧跟在每场战斗进场之后
   ——试炼中遇到的宠物会登记进图鉴（`record_coll{handbook_id, record[]{pet_base_id, status, ...}}`）。
   注意这批 `record.add_time` 是**假时间**（实测 4254869860 / 3000807466 / 1970578711，都不是合法
   Unix 秒），**不能拿来当捕获时间**。本项目目前只从登录包取图鉴炫彩（`pet_handbook`），不受影响。
4. **试炼场景 601/60008 没有大地图底图**（`names.json` 的 `maps` 只有 10003/10018/30001/30002），
   实时地图页在试炼里没有底图可叠——要显示就只能显示场景名。

## 6. 复现

```bash
go run ./cmd/pcapdump -pcap PCAPdroid_31_8月_22_54_57.pcap                      # 概览
go run ./cmd/pcapdump -pcap … -op GRASS_TRIAL -limit 2 -maxbody 100000          # 试炼全族(注意 55KB 要放开 maxbody)
go run ./cmd/pcapdump -pcap … -op 0x1967,0x1968 -limit 30 -maxbody 200          # 奖励与 action 对照
go run ./cmd/pcapdump -pcap … -gid 133                                          # 试炼宠物出现在哪些 opcode
```

## 7. 与社区工具 `rocokingdom_recognizer` 的对照

[iozxc/rocokingdom_recognizer](https://github.com/iozxc/rocokingdom_recognizer)（MIT，2 star，347 commit）
做的是**同一个玩法**「洛克王国草系徽章助手」，但走**图像识别**路线：窗口截图 → YOLOv8 版面检测 →
PP-OCRv4 识字 + ResNet50 特征检索 → 结果融合，桌面端 pywebview + React，ONNX Runtime 推理，
SQLite 存图鉴库。不读内存、不改游戏，与本项目「不碰客户端」的底线一致。

差异只有一句话：**它在屏幕外猜，我们在协议里读。**

| | 它（图像识别） | 本项目（抓包解析） |
| --- | --- | --- |
| 输入 | Windows 客户端窗口截图 | Linux 网关旁路 TCP 8195 |
| 遇到哪只精灵 | 截图 → 模型预测 | `review_records[].petbase_conf_id`（权威） |
| 是否异色/炫彩 | 看不出（要另外训） | `mutation_type` / `glass_info`（见 data.md 3.5） |
| 地图/章节 | 图像分类 | `chapter_id` 3000/3001/3002 |
| 数据源 | **B站 wiki 爬取**（`wiki.biligame.com/rocom`，2026-08-26 生成） | 自行解包（AGENTS.md 约定，不依赖外部仓库） |

**可交叉对账，但不能当数据源。** 实测（把它的 `datasets/*.json` 拉下来与 `names.json` 对跑）：

1. **它的精灵 `id` 就是我们的图鉴编号**：`roco_all_pets_info.json` 的 `id` 与 `petbase[].b`
   （= `PETBASE_CONF.pictorial_book_id`，见 `scripts/gen_gamedata.py:232`）462 例相等。
   故社区名单经 `b` 可**无损翻译**成本项目 petbase id —— 三份地图名单 182/238/136 个图鉴号
   → 318/422/306 个 petbase 形态，**零丢失**。
2. **它的名字口径与游戏不同，不能反向修正我们**：568 条里 436 条 `form_name` 直接命中我们的
   petbase 形态全名，未命中的 132 条几乎全是它自己的**形态别名**（`鸭吉吉_蓬松/紧实`、
   `板板壳_本来/蜕皮`、`雪绒鸟_冬天/春天/夏天/秋天`…），来源就是它的
   `pet_renames.json`（人工改过名，如「鸭吉吉_睡帽」→「鸭吉吉_起来鸭」）与
   `ocr_corrections.json`（OCR 纠错表）。这些是**社区叫法**，拿它们改我们的表只会引入污染。
3. **静态名单会过期**：本次 pcap 高频形态里，花衣蝶(3141/b=101)、机幕方舟(3735/b=350)
   **不在它的任何一张地图名单里**（名单生成于 8-26，本 pcap 是 8-31，且含新区形态）。
   它靠名单把识别候选从 2 万压到几百 —— 名单一旧就漏；抓包拿到的是**当下真实**刷的那批。

结论：它的地图名单可以当作「试炼三章候选池」的参考（经 `b` 翻译），真值仍以协议下发为准。
两者不冲突、也不该合并：一个在客户端屏幕外、一个在网关网络侧。

## 8. 待验证（需要新样本）

- `refresh_type` 1 与 2 的确切差别（是否一个只换奖励、一个换整个事件）。
- `event_conf_id` 的编号规则（实测 110005/100155/130126/200006/310005…，看不出区间含义，需要解包
  侧的表；本机没有解包目录，未能查中文名）。
- `trial_effect_ids`（1001/1003/1006/1008/1010 等「试炼词条」）的具体效果文案。
- `0x132c` 里 `TRUE_BATTLE_RESULT_WIN_HP(66)` 与 `WIN_DEFEAT(18)` 的判定差别（本样本 2 : 15）。
- 试炼**失败**或中途退出的报文路径（本样本 17 场全胜、跑到通关为止）。
- `banned_skill_ids` 18 条是版本级禁用清单还是按试炼 id 变化。
