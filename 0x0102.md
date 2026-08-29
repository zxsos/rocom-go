# opcode 0x0102 `ZONE_LOGIN_RSP` 分析

登录响应，玩家登录时服务器下发的**全量数据快照**。一次登录只出现一条（实测 pcap 中 s2c 单条
`78550` 字节），内容几乎覆盖账号全部状态，是本项目**多账号身份**、**盒子布局**、**队伍快照**、
**宠物奖牌墙**的权威来源。

- 方向：s2c（下行）
- 对应请求：`ZONE_LOGIN_REQ(0x0101)`
- 消息体：`Next.ZoneLoginRsp`（`ret_info` + `player_info`）

## 1. 总览

```
Next.ZoneLoginRsp
├── ret_info            #1 返回码(ret_code=0)
└── player_info         #2 玩家全量数据
    ├── brief_info          玩家基础信息(身份/等级/货币/家园/战斗简报)
    ├── common_info         通用信息(出生场景/当前选中宠物/家园访问设置)
    ├── pet_info            ★宠物信息(本消息主体,占 90%+)
    ├── story_flag_info     剧情标记(version + story_flags)
    ├── misc_info           杂项(当前投掷道具/新手引导/离线消耗/赛季奖励)
    ├── world_map_info      世界地图(空)
    ├── svr_data_info       服务器数据(外观/兑换)
    ├── red_point_info      红点数据(group_info)
    ├── black_info          黑名单(空)
    ├── pvp_his_cli         PVP 历史
    ├── music_info          已解锁音乐
    ├── star_light_info     星芒进度
    ├── emoji_bag_info      表情包
    ├── lottery_confirm     抽奖确认(空)
    ├── client_water_mark_info  客户端水印
    └── start_up_privilege_info 开服特权
```

## 2. 各 section 详解

### 2.1 brief_info — 玩家基础信息

| 字段 | 说明 | 实测值 |
| --- | --- | --- |
| `uin` | 腾讯 uin | 100000001 |
| `openid` | 平台 openid | 0000000000000000000 |
| `name` | 昵称 | "示例玩家" |
| `sex` | 性别 | 2 |
| `role_level` | 玩家等级 | 50 |
| `world_level` | 世界等级 | 3 |
| `register_time` | 注册时间(Unix) | 1777392609 |
| `login_time` / `logout_time` | 本次登录/上次登出时间 | 1787871793 / 1787871721 |
| `login_times` | 累计登录次数 | 231 |
| `daily_online_time` | 当日在线秒数 | 3660 |
| `total_online_time` | 累计在线秒数 | 404940 |
| `enter_cell_times` | 进场景次数 | 1207 |

嵌套子结构：

- `core_additional_brief_info`：`brief_sec_info`（积分/黑名单标记）、`card_brief_info`
  （名片头像/图鉴收集数 297）、`card_appearance_info`（穿戴时装 `fashion_wear_id[]`、
  `card_skin_selected`、沙龙 `salon_item_data`）、`business_card_info`、`mobile_bind_info`
  （手机绑定）、`setting_brief_info`、`battle_pass_brief_info`（战令）
- `vitem_info`：虚拟货币(洛克贝/钻石等)
- `plat_info`：平台信息
- `home_brief_info`：家园（`room_expansion_info` 房间扩展、`access_info` 访问权限与
  违规 `ban_info`/`violation_info`）
- `scene_info`：当前位置（`cell_id`、`pt` 坐标）
- `level_award_info`：等级奖励
- `battle_brief`：战斗简报（`battle_state`、`battle_conf_id=1007`）
- `additional_data`：附加（`world_level`、名片图鉴数等）

### 2.2 common_info — 通用信息

- `scene_info`：出生/常驻场景（`cell_id=4640679060421738496`、`pt` 坐标、`belong_camp=130173`、
  `time_of_day=2`、`weather_type=1`）
- `level_award_info`：等级奖励领取状态
- `select_pet_conf_id`：当前展示宠物 conf(3004002)
- `select_pet_conf_id_list`：展示宠物列表(2000672 / 3004002)
- `next_region_group_id`、`pet_select_region_id`：选宠区域
- `visit_permission_setting`、`home_last_visit_time`、`is_home_visiting`、`home_owner_uin`：家园访问

### 2.3 pet_info — 宠物信息（核心）

`pet_info` 占消息主体的绝大部分（实测约 2.9 万行 / 3.1 万行），包含：

```
PlayerPetInfo
├── pet_data[]                #1 当前拥有的宠物(实测 4 只)
├── catch_info[]              #2 捕捉统计(实测 106 条)
├── team_info                 #3 当前队伍
├── handbook                  #4 图鉴(record_collection 356 个)
├── backpack_info             #5 盒子布局(egg_gid + boxes[])
├── habit_info                #6 伙伴习惯
├── statistics_info           #7 宠物统计数据
├── team_infos[]              #8 大世界队伍(实测 13 组)
├── pet_medal_info            #9 宠物奖牌墙
├── pet_task_info             #10 宠物任务(空)
└── backtrack_round_info      #11 回溯轮次
```

#### pet_data — 单只宠物（核心）

```
PetData
├── 身份
│   ├── gid                服务器唯一 id(1/6/115/4054)
│   ├── conf_id / info_id  配置 id(2000672 火神 / 3004002 迪莫 / 3484003 帕帕斯卡 / 1007 魔力猫)
│   ├── name               宠物名(可改名,本地展示用)
│   ├── name_src           名字来源
│   └── skill_dam_type[]   属性(火/光/机械+翼/草)
├── 养成
│   ├── level              等级(55/50/55/60)
│   ├── exp                经验
│   ├── nature             性格(2/24/2/23)
│   ├── gender             性别(1/2)
│   ├── base_conf_id       基础形态 conf(3003/3006...)
│   ├── evolution_stage    进化阶段
│   ├── pet_status_flags   状态位
│   ├── height / weight    身高/体重(体型判定)
│   ├── classis            品阶
│   ├── last_breakthrough_lv  突破等级
│   ├── talent_rank        天赋等级
│   ├── grow_times         培养次数
│   ├── speciality_id / real_speciality_ids  特性
│   └── bitflag / voice / nature_desc_id / patch_version
├── 捕捉信息
│   ├── ball_id            捕捉球(100002/100255/100286)
│   ├── add_time           获得时间
│   ├── energy             精力
│   ├── synchron_num       同步数
│   ├── is_first_catch     首次捕捉
│   ├── catch_status / catch_lv / catch_base_id
│   ├── catch_way          捕捉途径(cheer_point_info 内)
│   └── mutation_type      突变位标志(bit0=异色, bit3=炫彩)
├── 技能
│   └── skill.skill_data[]
│       ├── id             技能 id(7040250...)
│       ├── type           技能类型
│       ├── is_learned / is_equipped  已学/已装
│       ├── pos            装备位
│       ├── unlock_need_lv 解锁等级
│       ├── conf_idx       配置序号
│       ├── skill_src      技能来源
│       └── use_times      使用次数
├── 属性(种族值/天赋/努力值)
│   └── attribute_info
│       ├── hp/attack/special_attack/defense/special_defense/speed
│       │   ├── total_race     种族值
│       │   ├── talent        天赋(0-40)
│       │   ├── base_value    基础值
│       │   ├── effort_exp    努力值经验
│       │   ├── effort_lv / effort_add  努力等级/加成
│       │   └── talent_add_value 天赋加成
│       └── break_enhance_enum[]  突破增强(1/2/6)
│   └── attribute_new_info.addi_attr_data[]  附加属性(addi_attr + type)
├── 携带/装备
│   └── possession
│       ├── slot_size       格位数
│       ├── item[]          携带物
│       └── auto_supply     自动补充
├── 亲密度/伙伴
│   ├── closeness_info     亲密度(closeness_exp / closeness_lv)
│   ├── evolute_info[]     进化记录(evolute_time + before/after_base_conf_id)
│   ├── evlution_need_info 进化需求
│   ├── cheer_point_info   喝彩点(catch_way + cheer_point)
│   ├── partner_mark       伙伴标记(PPMT_NONE)
│   ├── scene_info         场景互动(npc_id / interact_quantity / 阈值 / can_trig_bond)
│   └── llm_nature_tag     LLM 性格标签(空)
└── 其他
    ├── blood_id           血统
    ├── habit_group_id / habit_level  伙伴习惯
    └── changed_nature_pos_attr_type / changed_nature_neg_attr_type  性格修正属性
```

#### catch_info — 捕捉统计（106 条）

按 `pet_base_id` 统计：`success_count`(成功数)、`fail_count`(失败数)、
`catch_probability`(捕捉概率,万分比,如 9500/6608)。

#### team_info / team_infos — 队伍快照

`team_infos[]` 中 `team_type==PTT_BIG_WORLD(1)` 的 `PetTeamInfo` 是**大世界队伍**：
`teams[]` 每队 `pet_infos[]`(6 位)携带 `pet_gid`，`main_team_idx` 指主队。本项目的
`ParseTeams` 取宠物数最多的候选展开为 gid→(队,位)，实测 3 队 18 只全命中。

#### handbook — 图鉴（356 个 record_collection）

每个 `record_collection`：

```
record_collection
├── handbook_id            图鉴条目 id(356 个)
├── record[]               已捕获宠物记录(410 条)
│   ├── pet_base_id        基础宠物 id
│   ├── is_boss            是否首领
│   ├── height_min/max / weight_min/max  体型范围
│   ├── add_time           捕获时间
│   ├── status             状态(2/3)
│   ├── caught_camp        捕获营地
│   ├── other_boss_base_ids 其他首领 id
│   ├── form_group         形态组
│   └── catch_mutation     捕获时突变
├── topic_list[]           收集主题(1889 条)
│   ├── finish_cnt         完成计数
│   ├── topic_type / topic_id
│   └── get_award          是否已领奖
├── complete_node_num      完成节点数
└── status                 图鉴状态
```

#### backpack_info — 盒子布局（本项目核心）

```
backpack_info
├── egg_gid[]              宠物蛋 gid(551/540/549)
└── boxes[]                ★宠物盒
    ├── box_id             盒号(=展示位置,1 起)
    ├── mark_type          标记(WarehouseMarkType:0默认/1首领/2污染/4奇异/8炫彩/16闪光)
    ├── box_name           玩家命名
    ├── lock               锁定
    ├── pet_gid[]          有序数组,每盒 30 格,空格=0
    └── vacancy_num        空位数
```

实测解出 ~525 只（27 盒）。`ParseBackpack` 取非零 gid 数最多的候选（排除误解析），
展开为 gid→位置存入 `pet_box`，同时把全量盒子元数据（含空盒）存入 `pet_boxes`。

#### statistics_info — 宠物统计数据

`pet_statistics_data[]` 按 `pet_base_id`：`battle_count`(战斗次数)、`collect_count`(收集数)、
`follow_duration`(跟随时长)、`collected_gender_bit`(性别位)、`collected_nature_bit`(性格位)、
`collected_blood_bit`(血统位)、`perfect_talent_count`(满天赋数)、
`collected_naturebuff_bit`(性格增益位)、`mutation_count[]`(突变计数: mutation + cnt)。

#### pet_medal_info — 宠物奖牌墙

```
PlayerPetMedalInfo.medal_infos[]
├── medal_conf_id      奖牌配置 id(1002/1030...)
├── medal_type         奖牌类型(2)
└── buckets[]          槽位(hash_id)
    └── detail_list[]  获奖宠物
        ├── owner_id   获得者
        ├── add_time   获得时间
        ├── is_wear    是否佩戴
        ├── obtain_pet_gid  获得宠物 gid
        └── wear_pet_gid    佩戴宠物 gid
```

> 该消息线上 wire 格式与 all.pb 的 `PetMedalOwnerInfo` 定义不一致（版本偏移），
> `pet.ParsePetMedals` 纯按 wire 经验解码，不走 pb。

### 2.4 其余 section 摘要

| section | 内容 | 实测 |
| --- | --- | --- |
| `story_flag_info` | 剧情标记 `version=172` + `story_flags[]`(repeated 标量 flag id,97 条) | — |
| `misc_info` | `cur_selected_throw_item`(当前投掷道具)、`guide_info[]`(引导,22 条)、`home_level_reward_info`、`offline_operation_consume_state`(离线消耗)、`season_catch_reward_info`(赛季捕捉奖励) | — |
| `svr_data_info` | `appearance_info`(外观/时装)、`exchange_info`(兑换 `exchange_data[]`:次数/刷新时间/组) | 16 组 |
| `red_point_info` | `group_info[]` 红点分组(33 条) | — |
| `pvp_his_cli` | PVP 历史(空) | — |
| `music_info` | 已解锁音乐 `music_id_list[]` | 15 首 |
| `star_light_info` | 星芒：`current_progress=6000`、`current_efficiency=1`、`today_star_light_num=48` | — |
| `emoji_bag_info` | 表情包 `emoji_list[]`(emoji_id + is_unlock) | 20 个 |
| `lottery_confirm` | 抽奖确认(空) | — |
| `client_water_mark_info` | 客户端水印：`close_watermark=true`、`end_time=4070880000` | — |
| `start_up_privilege_info` | 开服特权：`cli_startup_day=1787846400` | — |

## 3. 与项目的关系

0x0102 是抓包启动后**必须最先命中**的消息，身份/布局/队伍/奖牌都以它为权威快照：

1. **多账号身份**（`ParseLoginAccount`）：wire 三层下钻
   `body → #2(LoginData) → #1(base) → {#1=user_id(varint), #3=nickname(bytes)}`，
   取 `user_id` 作账号键 `"UID:"+id`。pipeline 遇 `LOGIN_RSP` 先解析 user_id 写 connID→account
   映射，再据 connID 归属同包携带的背包/队伍/奖牌快照。
2. **盒子布局**（`ParseBackpack`，`CarriesBackpack` 判定）：`backpack_info.boxes[]` 展开
   gid→位置落 `pet_box`，盒子元数据落 `pet_boxes`。
3. **队伍快照**（`ParseTeams`，`CarriesTeam` 判定）：`team_infos` 中大世界队伍展开落 `pet_team`。
4. **宠物奖牌墙**（`ParsePetMedals`）：`pet_medal_info` 解 gid↔medal 落 `pet_medal`。
   注意：奖牌数据仅完整登录携带（普通/快速登录可能不含）。

> 详细数据流与落库语义见 [data.md](data.md) 与 [architecture.md](architecture.md)。

## 4. 复现方法

```bash
go run ./cmd/pcapdump -pcap <文件> -op 0x0102   # 转储完整消息(精确解码)
go run ./cmd/pcapdump -pcap <文件>              # opcode 概览(确认只出现一次)
```

实测样本：`PCAPdroid_28_8月_07_03_02.pcap` 中单条 `ZONE_LOGIN_RSP` 78550 字节，
4 只宠物（火神 gid=1 / 迪莫 gid=6 / 帕帕斯卡 gid=115 / 魔力猫 gid=4054）。
