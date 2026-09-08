# 伤害估算的数据来源（闪耀大赛 · 隐藏模块 `#/shanyao`）

本文件说明「伤害估算」用到的**外部数据**从哪来、怎么桥接、什么许可、怎么更新。
玩法与协议结论另见 `internal/shanyao/opcodes.go` 的头注释。

## 为什么需要外部数据

本仓库的数据原则上**全部来自自行解包**，但伤害公式要的两样东西解包里拿不全：

| 需要 | 解包/协议里的情况 | 外部来源 |
| --- | --- | --- |
| 形态的六维**种族值** | 我方宠物的六维协议里有（`attribute_info`），**对手不下发** | roco 赛季快照 |
| 技能的**威力 / 系别 / 类别 / 能耗** | `skills.json` 只有 492 条第三方资料，花精灵、聚能这类老 id 没有 | roco 赛季快照 |
| 18 系**克制关系** | 只有系别名（`skill_dam_type`），没有倍率表 | roco 赛季快照的 `typeChart` |

对局内的技能威力（`skill_round_data` 与 `0x1324` 的 `damage_param_result`）由抓包实时解析，
**优先于**静态表 —— 它带融合与被动修正，是当场的真实值。

## 来源与许可

- 上游项目：[Evenstar-tools/roco-calculator](https://github.com/Evenstar-tools/roco-calculator)
  （非官方玩家伤害计算器，代码 MIT，`Copyright (c) 2026 zhangzeyu99-web`）
- 上游数据文件：`data/snapshots/current.json`（约 8.3 MB，619 形态 / 579 技能 / 241 特性）
- **数据原始出处**：[BWIKI 洛克王国：世界](https://wiki.biligame.com/rocom/)，
  页面标注 **CC BY-NC-SA 4.0**（署名 · 非商业 · 相同方式共享）
- 上游的权利边界见其 `docs/legal/THIRD_PARTY_NOTICES.md`：MIT **只覆盖代码**，不覆盖游戏素材与第三方资料

因此本仓库的做法：

1. **快照不入仓库**（8.3 MB，且是别人的整理产物），只入抽取后的三张小表：
   `internal/gamedata/data/calc_race.json`（184 KiB）、`calc_skills.json`（118 KiB）、`calc_types.json`（3 KiB）
2. 生成物带 `_source`：快照 id、赛季、BWIKI revision、抓取时间、许可、桥接方式
3. 本仓库为非商业玩家工具；若将来要商用，须自行取得 BWIKI 与权利人的授权

## 桥接：靠中文名

roco 用的是**自有哈希 id**（`spirit_xxx` / `skill_xxx`），与游戏内的 `petbase_id`、`skill_id`
不是一套，只能按名字桥接：

- 形态：roco 的 `fullName` ↔ 我们 `names.json.petbase[].n`（roco 侧实测无重名）
- 技能：roco 的 `name` ↔ 我们 `skills.json` 的 `names`（roco 侧实测无重名）

覆盖率（快照 `s4-preview-2026-09-02`）：

- 形态 **611 / 1136**（53.8%）—— 未命中的多为未进化小形态（赤毛鸡仔、蹦蹦种子…）；
  本次 PVP 抓包里登场的 12 个形态**全部命中**
- 技能 **674 / 1918** —— 未命中留空，由对局内威力参数兜底

未命中一律「缺数据」，**不用近似值兜底**。

## ⚠️ S4 前瞻调整：必须回退

快照的 `raceStats` **已经应用**了 `currentPatchChanges`（S4 前瞻，2026.09.10 生效）。
抓的是现网当前版本，直接用快照值会与协议对不上。实测：

| 形态 | 协议（抓包 2026-09-07） | 快照 raceStats | 快照里的 before |
| --- | --- | --- | --- |
| 花衣蝶 HP | 122 | 132 | 122 ✅ |
| 花衣蝶 物攻 | 67 | 72 | 67 ✅ |

故生成脚本默认**回退到 before**（`ROCOM_CALC_APPLY_PATCH=1` 可保留前瞻值），
并用四个形态的协议实测值做锚点校验 —— 少了它，某天上游改了默认行为就会静默用错口径。

## 更新方式

```bash
# 默认拉 raw.githubusercontent 的 current.json
uv run python scripts/gen_calcdata.py

# 或指向本地快照
ROCOM_CALC_SNAPSHOT=/path/to/current.json uv run python scripts/gen_calcdata.py

# S4 前瞻正式生效后（去掉回退）
ROCOM_CALC_APPLY_PATCH=1 uv run python scripts/gen_calcdata.py
```

脚本幂等（内容不变不改写文件），并打印覆盖率与命中/未命中统计。
跑完记得：`go test ./internal/gamedata/`（锚点与桥接校验）、
`node scripts/verify-calc-pipeline.mjs` 在 `web/` 下跑（口径对拍）。

## 口径清单（与 roco 对齐，改前先看）

- 等级系数：`(level × 45 / 100 + 10) / 41`（60 级 = 0.902439）
- 面板六维：`round((HP?1.7:1.1) × (种族 + 3 × 个体/6)) + (HP?70:10)` 得 `baseValue`；
  面板值 = `round(baseValue × 性格) + (HP?100:50)`（协议里的 `effort_add`）
- 性格系数：**+1.2 / −0.9**
- 取整：`effectiveSkillPower` floor、`displayedPower` 四舍五入、`damageNumerator` 四舍五入、
  `oneHitDamage`/`finalOneHitDamage` floor、`hitCount` 先取整再乘
- 克制：单系 ×2 / ×0.5，双系相乘后钳制（≥4 记 3，≤0.25 记 0.25）

这些数字的对拍见 `web/scripts/verify-calc-pipeline.mjs`（`npm run verify:calc`）。

## 公式主体（第二期已实现，单向：我方 → 对手）

```
coefficient = (level × 45 / 100 + 10) / 41
numerator   = round(攻方能力值 × 显示威力 × coefficient)     // 四舍五入
oneHit      = floor(numerator ÷ 守方能力值 × 减伤)            // 向下取整
finalOneHit = floor(oneHit × 最终倍率)                        // 向下取整
total       = finalOneHit × 段数 + 追加伤害
显示威力     = round(技能威力 × 本系1.25 × 克制倍率)
攻方能力值   = 面板攻/魔攻 × abilityLevelMultiplier(攻档, 守档)
```

- **能力等级**：每档 ±10%，档位夹在 ±99；攻/守两档合成一个系数
  （正向加成进分子、负向加成进分母），**不是各自乘一次**。
- **减伤 / 最终倍率 / 追加伤害**：均为面板手工输入（减伤按百分比填，如「减伤 80%」填 80）。
- **威力取值**：只用自动取值 —— 对局内 `damage_param_result`（含融合/被动修正）优先，
  静态表兜底；**不接受手工覆盖**（roco 的 power override 我们刻意不做，少一个能改错的地方）。

### 动态威力规则（快照里只有 3 个技能带 ruleId）

| ruleId | 技能 | 威力算法 | 需要的输入 |
| --- | --- | --- | --- |
| `mana_burst` | 魔能爆 | 按当前能量查表 `[45,70,90,110,135,155,165,180,190,200,210]`（能量夹在 0..10） | 当前能量 |
| `speed_difference` | 闪击 | 「攻方速度 − 守方速度」查差值表 | 双方速度 |
| `physical_defense_difference` | 鸣沙陷阱 | 「攻方物防 − 守方物防」查同一张差值表 | 双方物防 |

差值表：`≤0→60，1–30→80，31–60→100，61–90→120，91–120→140，121–150→150，
151–180→160，181–210→170，211–240→180，241–270→190，≥271→200`。

**算不出就明说**：缺能量、缺双方能力值、缺威力时，页面显示「缺少：×××」，
不给估算值 —— 这条来自 roco 的展示规范（「无法精确计算时显示缺少条件，不伪造算式」）。

## 还缺哪些数据、该怎么录

「只能靠抓包拿到」的数据、当前覆盖状态、以及录制建议见
**[docs/pcap-capture-checklist.md](pcap-capture-checklist.md)**。

## 官方客户端解包（权威来源）

形态种族值 / 技能数值 / 克制矩阵目前来自 roco-calculator 的赛季快照（按中文名桥接）；
**技能威力、印记与状态、天气、特性** 现在有官方客户端解包出的配置表可用，按 **id 直取**，
不需要桥接，也不再依赖推断：

- 技能 `SKILL_CONF`：1888 条，`dam_para[0]` = 威力（覆盖率 674 → **1906**）
- 印记与状态 `BUFF_CONF`：3002 条，`buff_id → 名字`（978 条权威映射）
- 天气 `WEATHER_CONF`：15 种，含各自挂的 buff，可从场上 buff 反推当前天气
- 特性 `PET_TALENT_CONF` + `BUFF_CONF` 里 type=3 的条目：特性→buff 关联，
  可从宠物 buff 自动识别它带哪个特性

包体结构、解包步骤、字段口径、已知缺口见 **[docs/apk-unpack-notes.md](apk-unpack-notes.md)**。

⚠️ pcap 对拍推断的 `buff_id` 曾出错（`20070020` 判成冻结，官方是**灼烧**，冻结是 `20580010`）——
官方表一来就被覆盖。这也说明：**推断只能当候选，不能当事实**。

