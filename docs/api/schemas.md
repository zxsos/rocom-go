# 响应结构

## 关于可信度标记

- **✅ 已验证**：有 golden 快照，由 `internal/server/contract_test.go` 守护，
  与线上输出必然一致。示例即 `internal/server/testdata/contract/*.json`。
- **⚠️ 源码推导**：无 golden 快照，据 Go struct 的 `json` tag 与 handler 代码整理，
  可能滞后。**以代码为准**，欢迎补 golden。

> **阶段 3 已把这四个 payload 强类型化**，定义见 `internal/server/payload.go`，
> pipeline 与 server 共用同一份，字段名受编译期保护 —— 不再有「拼错 key 编译照样过」的风险。
> 此前它们是无 Go struct 的 `map[string]any`，字段只能靠 golden 反推。

---

## 宠物

### `GET /api/pets` ✅

```json
{ "total": 2, "pets": [ { ...宠物对象... } ] }
```

`pets` 为 `[]`（非 `null`）当无结果。

### `GET /api/pets/{gid}` ✅

单个宠物对象。不存在返回 `404`。

### 宠物对象（Pet）✅

取自 `pet-detail.json`，标注了类型的字段来自 `internal/pet/model.go`：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `gid` | int | 唯一实例 id |
| `confId` | int | 种类配置 id（指向进化线**一阶** base） |
| `baseConfId` | int | 当前形态 petbase id（进化后随之变化） |
| `species` | str | 种类名（当前形态） |
| `book` | int? | 图鉴编号 |
| `form` | str? | 地区/季节形态名（普通宠物为空） |
| `stage` | int? | 进化阶段 |
| `name` | str | 昵称 |
| `level` | int | 等级 |
| `natureId` / `nature` | int / str | 性格 |
| `gender` | str | `♂` / `♀` |
| `types` | str[] | 系别中文（可多系） |
| `typeIcons` | str[]? | 系别图标相对路径 |
| `bloodId` / `blood` / `bloodIcon` | int?/str?/str? | 血脉编号/中文短名/主图标 |
| `eggGroups` | obj[]? | 蛋组，`{name, desc}` |
| `heightM` / `weightKg` | float | 身高（米）/ 体重（千克） |
| `heightMin/Max` `weightMin/Max` | float? | 该形态取值区间（读取时注入，不入库） |
| `heightPct` / `weightPct` | float? | 形态内百分位 0-100（可为 `null`） |
| `voice` | int | 声音值 |
| `talentRank` | str | 天分评价 |
| `medal` / `medalDesc` / `medalIcon` | str / str / str? | 佩戴奖牌 |
| `wearMedalConfId` | int | 佩戴奖牌配置 id |
| `medalIds` | int[]? | 已拥有奖牌 id（佩戴+custom+free，去重） |
| `partnerMark` / `partnerMarkIcon` | str / str? | 搭档标记 |
| `speciality` / `specialityId` | str / int | 特长 |
| `catchTime` | int | 捕捉时间（Unix 秒） |
| `shiny` / `colorful` | bool | 异色 / 炫彩 |
| `glassType` / `glassValue` | int? | 炫彩类型与数值 |
| `image` | obj | `{head, bigHead, portrait, portraitSmall}`，相对 `/img/` 的路径 |
| `box` | obj? | `{boxId, slot, boxName?, mark?}`（在盒中） |
| `team` | obj? | `{teamIdx, pos}`（在队中；与 `box` **互斥**） |
| `hp` `attack` `defense` `spAttack` `spDefense` `speed` | obj | `{value, talentLv, nature}` |
| `skills` | obj[]? | **仅详情接口有**：当前形态天生会的技能，见下 |
| `learnable` | obj[]? | **仅详情接口有**：技能石可学的技能，见下 |
| `bloodline` | obj[]? | **仅详情接口有**：血脉可获得的技能，见下 |

> `image` 取图优先 `baseConfId` 回退 `confId` —— 只按 `confId` 取会拿到进化线一阶的图。

#### 三类技能 `skills` / `learnable` / `bloodline`（仅 `GET /api/pets/{gid}`）

```json
{ "skills":    [{ "name": "山火", "level": 48, "elem": "火", "skillId": 7040250,
                  "power": "15", "cost": "3", "effect": "造成物伤，…" }],
  "learnable": [{ "name": "乘风", "elem": "翼", "skillId": 7150120,
                  "power": "—", "cost": "2", "effect": "自己获得速度+120。" }],
  "bloodline": [{ "name": "冰爪", "elem": "冰", "skillId": 7090250,
                  "power": "80", "cost": "2", "effect": "对敌方精灵造成物理伤害。" }] }
```

按 `baseConfId`（当前形态）分别查 `gamedata.InnateSkills` / `LearnableSkills` /
`BloodlineSkills`。三类按**获取途径**划分，实测几乎互斥（天生 6518 条中与技能石
重叠 3 条、与血脉 0 条），故并列展示、不去重。七点约束：

1. **这些是「该形态可获得的技能」，不是某只宠物当前携带的**。技能是可换配置
   （见 `git 0762eb6` 移除 `Pet.SkillIDs` 的理由），后端不存个体技能。
2. **只有详情接口带**，`GET /api/pets` 列表不带 —— 避免给每只宠物都序列化一份。
3. **`level` 只有 `skills` 有**：技能石与血脉在资料站里没有等级、也没有解锁条件
   （只有精灵与进化链），不要想当然地补一个等级出来。
4. `skills` 按学会等级降序；`learnable` / `bloodline` 按技能名升序。
5. `power` / `cost` 是**字符串**：变化类技能无威力，值为 `"—"`（如「防御」），整数表达不了。
6. **该形态无资料时整键缺失**（`omitempty`），不是空数组 —— 见下「数据来源」。
7. **`skillId` 可能为 0**：该技能在资料站是重名的（借用/取念/复写各有 4 个变体，
   愿力冲击有 15 个精灵专属版），无从判断这个形态会的是哪一个。

> ⚠️ **数据来源是第三方资料站（过渡方案）**，不是游戏解包：
> 见 `scripts/fetch_skill_ids.py`、`scripts/gen_skills.py` 与 `internal/gamedata/skills.go`。
> 它覆盖 462 个形态；权威映射是解包的 `SKILL_CONF`
> （`base_skill_id` → 中文名，与协议同源），解出后应替换。
> 完整背景与局限见 `docs/data.md`「技能名」。

### `GET /api/stats` ✅
```json
{ "petCount": 2 }
```

### `GET /api/pet-page` ✅
```json
{ "page": 1, "found": true }
```

### `GET /api/filter-options` ✅

键为维度名，值为可选值数组。**`medal` 可能为 `null`**（本账号无奖牌时）。

```json
{ "nature": ["固执"], "talentRank": ["S"], "speciality": ["暴击"],
  "partnerMark": ["首领"],
  "chains": [{ "evo": 3003, "species": "火花", "base": 3003,
               "label": "火花（焰火/火神/烈火战神）", "img": "HeadIcon/3003.webp",
               "egg": "egg/egg_huohua.webp", "eggGroups": ["魔力", "巨灵"], "count": 1 }],
  "box": ["1-常用"], "medal": null }
```

维度来源：`nature`/`talentRank`/`speciality`/`partnerMark`/`form` 取自 `pets` 表去重，
`box` 取自 `pet_box`，`medal` 由后端把 id 转成中文名后追加，`chains` 由 `gamedata` 把本账号宠物的
**当前形态**收敛成「品种」。

> `chains` 不在宠物页的筛选面板里，是给**培育页选品种**用的，元素即 `gamedata.ChainOption`：
> `evo` = 进化链 id（0 = 无链形态，此时按 `species` 匹配）、`species` = 链首形态名、`base` = 链首的
> petbase id、`label` = 整条链的展示名（见下）、`img` = 链首头像（品种的「证件照」）、
> `egg` = 这个品种的**蛋图**、`eggGroups` = 链首的蛋组名（培育页按它把候选收窄到「与某只宠物
> 至少共一个蛋组」的品种 —— 配种要求母本与种公同蛋组）、`count` = 库里这个品种有几只。
>
> `eggGroups` 查不到时**缺字段**（超进化/分支形态的链首，实测 255 个品种里 35 个如此）：消费方
> 不能据此把这个品种滤掉，「没配蛋组」不等于「配不上」，宁多勿漏（同下面对不育品种的判据）。
>
> **生不出蛋的品种不进候选**：判据是**繁殖组（蛋组）**为「未发现」（`EGG_GROUP` id = 1，name 与
> desc 都叫这四个字）—— 迪莫、帕尔萨斯/圣羽翼王那一系、绒绒、犀角鸟、热团团等 53 个形态都在这一组，
> 它们进不了小窝配种。实测**没有**形态是「含 1 又带别的组」，判据无歧义；反过来，**没有蛋组**的
> 形态（超进化/分支形态，实测 64 个）只是没配，一律按能生算。
>
> 判据**不能**用「有没有蛋图」：蛋只挂在**初始形态**上（孵出来的就是它），而同一物种在 petbase 里
> 可能有多个条目、蛋只挂在其中一个（板板壳 3055 有蛋、3516 没有）—— 照蛋图过滤会误伤几十个正常
> 品种。故 `egg` 认不出时该键缺失，由前端退回 `img`。
>
> `label` 必须带出**形态名**（「地鼠（枯水期的样子）（遁鼠/遁地鼠）」）：同一只精灵的地区/季节形态
> 各占一条链，而它们的阶段名一模一样（地鼠两条链都是「地鼠/遁鼠/遁地鼠」、波波螺都是「波波螺/消波螺/
> 嗜波螺」、小狮鹫都是「小狮鹫/神圣狮鹫/皇家狮鹫」）—— 不带形态，下拉里就会并排出现两条**完全一样**
> 的选项，而品种选错就直接决定了这条线能配哪些母本。形态与链首相同的阶段不重复标注（链内形态实测多
> 数一致），链内形态确实不同的（化蝶：平常/幽冥眼/喵喵/奇丽花同链）才逐个带出来。
>
> 唯一的例外是**无链**品种（`evo` = 0）：它们按**名字**认，一个选项跨的就是同名的那一整组形态
> （海枝枝的 4 个样子：碧蓝珊瑚/杏黄百合/洋红沙丁/翠绿纶布、各物种的「首领形态」与「草系徽章-首领
> 形态」变体），故只写名字、**不带形态** —— 挑其中一个样子写上去，等于把「这条线只认这一个形状」说成
> 事实。它们与链的展示名也不撞（实测全库 206 条链 + 73 个无链品种名，零重名）。
>
> `egg` 同理按**形态**查、不按物种名查：波波螺两支各是 `egg_boboluo` / `egg_bobolouar`、地鼠是
> `egg_dishu` / `egg_dishu_2`——按名字查会让两个品种指着同一张图，而「这条线要孵哪种蛋」正是培育页
> 要回答的问题（反查路径：形态 → 它那个形态组的普通蛋配置 → 背包蛋物品 → 图标，见
> `gamedata.EggIconOfBase`）。依据是**有没有蛋 id**：自己没有的形态退回**本来样子**那颗（同名的
> 另一个形态）—— 多形态物种里绝大多数每个形态各有各的蛋（鸭吉吉 6 种形态 6 颗蛋、地鼠两支是
> `egg_dishu` / `egg_dishu_2`），只有板板壳、石肤蜥、海盔虫这三支的第二形态没有自己的蛋 id（官方
> 设定是**后天**变成的样子，孵出来还是本来样子那颗蛋）。蛋图只用于**展示**：连本来样子都查不到时
> 该键缺失，由前端退回 `img`；决定这个品种进不进候选的是繁殖组，不是它（见上）。
>
> 为什么给链而不是一列形态名：一条链上的各阶段是**同一个**品种（蛋的物种随母本，链上任一阶段的 ♀
> 都能当种母），而同一只精灵的不同形态各占一条链（嗜波螺「本来的样子」/「被污染的样子」）—— 按名字
> 列会把前者拆开、把后者并成一个，两边都错。去重键与 `/api/breeding/pool` 的匹配口径完全一致
> （有链按 `evo`、无链按 `species`），否则玩家选了也配不出来。只列本账号**确实拥有**的品种：蛋的
> 物种随母本，选了库里没有的品种就永远配不出任何组合。

> **值为空时该键直接缺失**（不是 `[]`）：上例因种子宠物无形态而**不含 `form`**，
> 无奖牌时 `medal` 为 `null`（该键总存在，值可能为空）。前端取值需容错。

### `GET /api/boxes` ✅
```json
[{ "id": 1, "name": "常用", "slots": [1002, 0, 0, ...], "heads": { "1002": "HeadIcon/3001.webp" } }]
```
`slots` 长 30，下标=格位（0 起），值=宠物 gid（`0` 空）。`heads` 是 gid 字符串→小头像路径。

### `GET /api/teams` ✅
```json
{ "slots": [0, 0, 1001, 0, ...], "heads": { "1001": "HeadIcon/3006.webp" } }
```
18 格（三队各 6）。

### `GET /api/evolution?base=3006` ✅
```json
[{ "petbase": 3001, "name": "小火猴", "stage": 1, "book": 1, "image": { ... } }]
```
按阶段升序。无链时返回 `[]`。

---

## 事件

### `GET /api/events` ✅
```json
[{ "id": 1, "time": 1700000000, "subKind": "捕捉", "gid": 1001, "pet": { ...宠物对象... } }]
```
按 id 倒序。参数：`limit` / `beforeId` / `offset`。无结果时为 `[]`。

### `GET /api/events/stats` ✅
```json
{ "total": 1, "bySubKind": { "捕捉": 1 }, "shiny": 0, "colorful": 0,
  "daily": [{ "day": "11-14", "n": 1 }], "topSpecies": [{ "s": "火神", "n": 1 }] }
```
`daily` 近 30 天升序（含 0）；`topSpecies` 至多 10。

---

## 全局固定数据

### `GET /api/icons` ✅
```json
{ "stat": { "hp": "...", "attack": "...", "spAttack": "...",
            "defense": "...", "spDefense": "...", "speed": "..." },
  "type": { "火": "...", "水": "..." },
  "shiny": "...", "colorful": "...", "shinyColorful": "...",
  "pollution": "...", "partnerFrame": "..." }
```
值为相对 `/img/` 的路径。不随宠物/账号变化，App 启动拉一次即可。

### `GET /api/medals` ✅
```json
[{ "id": 1, "name": "大块头", "desc": "...", "icon": "..." }]
```

### `GET /api/name-options` ✅
```json
{ "speciality": ["暴击", "..."] }
```

### `GET /api/accounts` ⚠️
```json
[{ "account": "UID:1", "name": "...", "petCount": 12, "title": "大富翁", "hasPin": true,
   "online": true, "avatar": "https://thirdwx.qlogo.cn/mmopen/vi_32/.../132" }]
```
`online` 由内存表判定（最近 30s 有流量），不落库；`title` 是当日排行榜称号。

`avatar` 是玩家**平台头像 URL**（登录回包 `plat_avatar_url`，可直接 `<img src>`）。
实测回包里出现过两类地址，**URL 形态完全不同**：

- `https://thirdwx.qlogo.cn/mmopen/.../132` —— 微信头像。末段是**尺寸位**，
  `0`/`46`/`64`/`96`/`132` 可取不同分辨率；前端统一取 `96`（最大的一处是手机端
  sheet 的 36px，2x 屏也只要 72px）。
- `https://photo-prod.nrc.qq.com/<uid>/card/<一串数字>` —— 游戏内名片照。**末段是图片
  ID 而非尺寸**，没有尺寸位可用，必须原样请求（实测原图 200 返回 ~400KB png，
  把它当尺寸位改写成 `/96` 会直接 404）。

> ⚠️ **别按「末段是不是纯数字」来猜尺寸位**：这两类地址都会命中该判据，后者会被改坏
> 成破图。前端的改写规则按**域名白名单**收紧（见 `AccountAvatar.jsx` 的 `sized`）。
> 将来再出现新的头像域名，先确认它有没有尺寸位，再决定要不要加进白名单。

- **`avatar` 可能整个键缺席**：游客号、未绑平台、或版本变更后解析失败时取不到，
  此时字段带 `omitempty` 不下发。前端**必须**回退到占位（如昵称首字徽章），
  不要直接渲染 `<img src="">`——那会打出破图并发一次无谓请求。
- **取到后不会被清空**：解析失败时后端保留旧值（`SetAccountAvatar` 忽略空串），
  故快速登录等不带头像的回包不会让头像凭空消失。

> ⚠️ **隐私：这是真人社交账号头像，敏感度高于昵称与 UID。**
> 后端原样下发（能打开页面的人就能看到，与昵称/洛克贝同级暴露面，**不要公网部署**）；
> 前端渲染时**必须挂 `.privacy`** 纳入全局截图防泄（见 `web/src/styles/shell.css`）——
> 昵称首字都已被判定为需遮罩的敏感信息（见 `web/src/components/AccountAvatar.jsx`），
> 真人头像更没有豁免的道理。

### `GET /api/leaderboard` ⚠️
```json
{ "forbes": [{ "account", "name", "coins", "hasCoins", "baseline", "profit", "title" }],
  "profit": [...], "titles": [{ "date", "account", "name", "title" }],
  "me": { "account", "name", "join", "coins", "hasCoins", "baseline", "profit", "title" } }
```
`forbes` 按洛克贝降序（`hasCoins=false` 沉底，前端显示「待同步」）；`profit` = 当前洛克贝 − 首次快照。

---

## 实时快照

### `GET /api/position` ✅

> 结构见 `internal/server/payload.go` 的 `PositionPayload`（已强类型化）。
> 无记录时返回 `null`。

**未过期**（`tsMs` 在 `posFresh`=4s 内）保留全部字段：
```json
{ "account": "UID:1", "sceneResId": 10003, "sceneCfgId": 1001, "sceneName": "卡洛西亚大陆",
  "img": "bigmap/10003.webp", "imgHd": "bigmap/10003_hd.webp",
  "x": 510000, "y": 612000, "z": 1200,
  "u": 0.5, "v": 0.5, "vu": 0.0001, "vv": -0.0002,
  "heading": 123.5, "stop": false, "paintable": true, "inBattle": false,
  "ts": 0, "tsMs": 0, "path": [{ "u": 0.49, "v": 0.49 }, { "u": 0.5, "v": 0.5 }] }
```

**已过期**则**抹掉 `vu` / `vv` / `path`**（其余字段不变）：
```json
{ "account": "UID:1", "sceneResId": 10003, "sceneCfgId": 1001, "sceneName": "卡洛西亚大陆",
  "img": "bigmap/10003.webp", "x": 510000, "y": 612000, "z": 1200,
  "u": 0.5, "v": 0.5, "heading": 123.5, "stop": false, "paintable": true,
  "inBattle": false, "ts": 0, "tsMs": 0 }
```

| 字段 | 说明 |
| --- | --- |
| `u` / `v` | 底图归一化坐标（0-1）；**该场景无底图时不存在** |
| `vu` / `vv` | 速度向量（归一化坐标/秒），供前端两包之间外推；`stop=true` 时不下发 |
| `path` | 客户端沉默后补报的真实轨迹点数组（至少 2 点才下发） |
| `ts` / `tsMs` | Unix 秒 / **毫秒**（前端按 `tsMs` 判过期）；golden 中抹为 `0` |
| `heading` | 朝向角（度），0=世界+X（地图东/右），顺时针增 |
| `paintable` | 该场景能否涂地，前端据此显示图层开关 |
| `inBattle` | 玩家**此刻是否在对战中**；地图据此在头顶挂脉冲图标。进战 `0x1316` 置、结算 `0x132c` 清，另有超时兜底（见 `docs/data.md` 3.9） |
| `img` | 底图文件名（家园按等级 `<res>_<lv>`）；无底图为空串 |
| `imgHd` | 高清底图文件名（`<img名>_hd`）；**该场景没有高清素材时整个键缺席**，前端据此显示「高清」开关。它是**叠加层**：只覆盖大陆与近海、远海透明，须压在 `img` 之上而非替换（见 `docs/data.md` 3.1） |

> 过期抹除是**有意设计**：陈旧速度会让前端外推一路飘走，故先静态回显，
> 等下一个移动包自然接管。

### `GET /api/wildpets` ✅

> 结构见 `internal/server/payload.go` 的 `WildPayload`（已强类型化）。
> 从未收到 AOI 通知时返回 `null`。

```json
{ "account": "UID:1", "sceneResId": 10003,
  "pets": [{ "id": "1234567890123456789", "n": "珀尔鼬", "img": "HeadIcon/3006.webp",
             "kinds": ["shiny", "big"], "u": 0.4, "v": 0.6, "x": 100, "y": 200, "z": 30,
             "lv": 45, "voice": 96, "height": 120, "weight": 8800, "weightPct": 98.5,
             "glassType": 1, "glass": "暗夜拾光", "glassValue": 131073, "mutation": 1 }],
  "allPets": [{ "id": "2234567890123456789", "n": "鸭吉吉", "img": "HeadIcon/3001.webp",
                "u": 0.7, "v": 0.2 }] }
```

- `pets`：稀有标记（异色/炫彩/污染/奖牌四件套），带全量字段。
- `allPets`：普通野生宠（「全部野生」图层），只有名/头像/坐标。
- `id` 是 **字符串**（`actor_id` 为 uint64，超出 JS 安全整数）。
- `kinds` 取值：`colorful` / `shiny` / `pollution` / `big` / `small` / `high` / `low`。
- `stale`（示例未含）：已离开 AOI，位置是最后所见，前端置灰。
- 两组均按 `id` 升序，**顺序稳定**（前端以 id 作 key，免得每次推送重排 DOM）。

### `GET /api/home` ✅

> 结构见 `internal/server/payload.go` 的 `HomePayload`（已强类型化）。
> 不在家园时 `nests` 为 `[]`；从未进过家园返回 `null`。

除 `account` / `nests` 外，还有四个字段**只在玩家确实在家园时下发**
（`omitempty`，不在家园时键直接缺席）：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `sceneResId` | int | 家园场景资源 id |
| `level` | int | 家园等级（决定底图取 `<res>_<lv>`） |
| `roomLevel` | int | 房间等级 |
| `couplesStale` | bool | 配对信息是否已过期 |

> 这四个是阶段 3 补进 golden 的 —— 原先契约只覆盖了「空家园」一种形态。

```json
{ "account": "UID:1",
  "nests": [
    { "id": "998877665544332211", "u": 0.3, "v": 0.8, "x": 10, "y": 20, "name": "精灵小窝",
      "pet": { "gid": 1001, "name": "小火", "species": "火神", "level": 60, "voice": 12,
               "weightPct": 55.5, "mates": [{ "gid": 1002, "name": "小水" }] } },
    { "id": "998877665544332212", "u": 0.6, "v": 0.4, "x": 30, "y": 40, "name": "精灵小窝",
      "egg": { "itemId": 5001, "name": "友爱天天的蛋", "icon": "egg/5001.webp" } }
  ] }
```

- `pet` 与 `egg` **互斥**（`omitempty`）：`pet` 为空即空窝，`egg` 是有蛋待收。
- `pet` 是简要信息（悬浮显示）；点击看详情走 `GET /api/pets/{gid}`。
- `mates` 是配对的另一半；多于一个即串窝。
- `id` 是字符串（`furniture_guid` 为 uint64）。

### `GET /api/flowers` ✅

> 结构见 `internal/server/payload.go` 的 `FlowerPayload`（已强类型化）。
> 从未收到 0x0375 时返回 `null`。
> **后端不输出内部字段 `cur` / `worlds`**：输出类型是独立的 `flowerView`（只有
> `account`/`flowers`），由结构保证不外泄，不再是 handler 里运行时过滤键。

```json
{ "account": "UID:1",
  "flowers": [{ "id": 7001, "name": "火神", "img": "HeadIcon/3006.webp", "star": 7,
                "blood": 3, "bloodName": "火", "bloodIcon": "blood/3.webp",
                "npcLogicId": 70010, "challengeCount": 2, "endTs": 1700086400,
                "specSeedId": 0, "activityId": 7, "ownerUserId": 0, "detail": true,
                "lv": 60, "glassType": 1, "glass": "暗夜拾光", "glassValue": 131073,
                "bindName": "火神", "bindImg": "HeadIcon/3006.webp", "bindEvo": 2,
                "medalName": "大块头", "medalIcon": "medal/1.webp" }] }
```

- `star`：普通花灵 5，特殊花灵 7。
- `ownerUserId`：`0` = 自己世界，非 0 = 好友世界（该值即世界归属者）。
- `detail`：是否已收到 0x0338 详情（玩家点过地图花种）。**仅 `detail=true` 时**
  `lv` / `glass*` / `bind*` / `medal*` 才有效，否则为 0/空。
- 面板 0x0375 给基础字段，点击后 0x0338 详情合并进来。

### `GET /api/flowers/slots` ✅

唯一把内部槽表 `worlds` **转成数组**暴露的接口 —— 不是原样透传，
`worlds` 这个键本身不出现在响应里，每个槽变成 `slots[]` 里的一个对象
（由前端 `[A5] §6` 指出措辞不准，原先写「透传」会让人误以为字段名就叫 `worlds`）：

```json
{ "slots": [{ "key": "self", "name": "自己世界", "ts": 1700000000,
              "flowers": [ { ...同上 flower 对象... } ] },
            { "key": "owner:10001", "name": "好友 UID:10001", "ts": 1700000100,
              "flowers": [...] }] }
```

`self` 槽排在前，好友槽按 key 升序。`self` 槽不可删。

### `GET /api/trial` ✅

> 结构见 `internal/server/payload.go` 的 `TrialPayload`（已强类型化）。
> 从未收到试炼报文时返回 `null`。玩法与报文时序见 `docs/pcap-20260831-grass-trial.md`。

```json
{ "account": "UID:1", "ts": 1700000000, "active": true,
  "run": {
    "trialId": 10002, "slotId": 1000, "slotName": "普系",
    "chapterId": 3001, "chapterIdx": 2, "nodeIndex": 3, "coin": 12,
    "chapters": [3000, 3001, 3002], "effects": [1001, 1008], "boss": false,
    "pet": { "gid": 133, "name": "黑猫巫师", "species": "黑猫巫师", "img": "HeadIcon/3569.webp",
             "level": 60, "hp": 264, "maxHp": 389, "energy": 10, "growth": 2,
             "skills": [{ "id": 7020500, "power": 25, "cost": 4, "fusion": 1, "slot": 2, "merged": [7090100] }],
             "features": [288135, 288001], "innateFeatures": [288135], "gainedFeatures": [288001],
             "featureNames": { "288135": "预警" },
             "shards": [2016], "equipped": [1, 2] },
    "options": [{ "slot": 1, "event": 110061, "reward": 7110340, "level": 40,
                  "eventCost": 1, "rewardCost": 4, "extra": [2016],
                  "pool": [288135, 7110340, 7020430, 7020440, 7160140], "used": [7040220],
                  "names": { "7110340": "超导加速", "7020430": "见招拆招",
                             "7020440": "触底强击", "7160140": "超级糖果" },
                  "pet": { "base": 3031, "name": "奇丽花", "img": "HeadIcon/3031.webp" } }],
    "refreshCost": 2,
    "bless": { "event": 500001, "options": [100], "effect": 0, "candidates": [7120100] },
    "reward": { "event": 110005, "id": 288001, "extra": [2016], "coin": 10 },
    "shop": [{ "type": 2, "id": 288154, "price": 6, "index": 4, "bought": false }],
    "result": { "victory": true, "duration": 1439, "petBaseId": 3569, "petLevel": 60,
                "settleAt": 1699999999, "score": 100 },
    "log": [{ "ts": 1700000000, "kind": "node", "label": "推进节点", "ids": [3001, 3] }]
  },
  "history": {
    "challengeInc": 251, "total": 251, "wins": 23, "cleared": [10000, 10001, 10002],
    "recent": [{ "settleAt": 1699999999, "petBaseId": 3569, "petName": "黑猫巫师",
                 "petLevel": 60, "trialId": 10002, "victory": true, "duration": 1439, "slotId": 1000 }],
    "topPets": [{ "petBaseId": 3141, "name": "花衣蝶", "img": "HeadIcon/3141.webp", "count": 56 }],
    "slots": [{ "slotId": 1000, "damType": 2, "damName": "普", "cleared": 3 }],
    "logs": [{ "logConfId": 100, "discovered": 167, "total": 210, "unlocked": true }]
  } }
```

要点：

- `active` 恒在下发（是否正在打一局）；`run` 只在握有某一局时出现，`history` 只在收到过
  0x1975 账号档案后出现。
- `run.pet.skills[]` 的**技能有 `name`**（按 `id` 查第三方资料的 `skill_id → 中文名`，
  见 `docs/data.md`「技能名」）。**融合不改变 `base_skill_id`**（只改威力与融合次数），
  故融合态技能同样有 `name`。仅当资料站未收录该 id 时 `name` 缺失，前端回退显示 `id`。
  试炼给少数技能换了 id（如魔能爆 `7020550` → 试炼态 `7880058`），这些已单独登记。
- 其余（特性 `288xxx`、碎片 `20xx-30xx`、事件、奖励）协议里**只有 id**，类别按区间可判
  （7xxxxx=技能、288xxx=特性、20xx-30xx=碎片），前端已内置该映射。名字看类型：
  技能查第三方资料（`skills.json`）；特性本身没有表，但见下面 `featureNames` 的桥接。

#### `run.options[]` 一个节点事件 = 一只精灵 + 从它身上抽的一个奖励

```json
{ "slot": 1, "event": 110061, "reward": 7110340, "level": 40,
  "eventCost": 1, "rewardCost": 4, "extra": [2016],
  "pool": [288135, 7110340, 7020430, 7020440, 7160140], "used": [7040220],
  "names": { "7110340": "超导加速", "7020430": "见招拆招" },
  "pet": { "base": 3031, "name": "奇丽花", "img": "HeadIcon/3031.webp" } }
```

- `pool`（协议 `random_skills[]`）是本事件的抽取池：**该精灵 1 个自身特性 + 4 个技能**。
  两种重掷都花金币，换的东西不同 ——
  `rewardCost`=**换奖励**，只在这 5 个里重抽一个；`eventCost`=**换事件**，整只精灵换掉，
  `pool` 随之变成新精灵的那一套。只看 `reward` 无法预判重掷会出什么，故整池下发。
- `used`（协议 `used_reward_ids[]`）是本节点该槽位**已抽过**的，重掷时服务器排除它们 ——
  `pool` 减去 `used` 才是下一次重掷的真实候选。
- `names` 是本卡片里查得到中文名的 id → 名字，覆盖 `reward` / `pool` / `extra`。
  **查不到的 id 不出现在这里**，前端显示裸 id。
- ⚠️ `pet` **协议不下发**：事件到精灵的映射来自官方事件表
  （`GRASS_TRIAL_EVENT_CONF`，gen_trial_official.py 落表，`gamedata.TrialEventPetBase`）。
  **普通遭遇/首领事件在表内、直接带出** `base` / `img`；表外事件（NPC 阵容/祝福/商人等）
  该项缺失，前端显示「事件 {id}」占位 —— 别把它当成「服务器说是这只」。
- `pool` 里那条 `288xxx` 就是这只精灵自身的特性。**事件在官方表内（能映射出精灵）时，
  它自动带上名字**（走「精灵 → 特性」表，见 `docs/data.md`「特性名」）。
  仅在池里**恰好一个**特性 id 时才绑，多条说明理解有误，宁可都不给。
- `log` 是操作流水（最新在前，最多 40 条），`kind` 取值 `start/node/refresh/bless/reward/
  shop/boss/settle`；`ids` 随 kind 而异（如 `node` 是 [章节号, 节点号]）。
- `run.pet.innateFeatures` / `gainedFeatures` 是 `features` 的**天生 vs 试炼中获得的**拆分
  （局级 `initial_feature_ids` 减出来）；拿不到局级字段时两者都缺席、`features` 仍在 ——
  刻意不猜，标错比不标更糟。
- `run.pet.featureNames` 是天生特性的名字（id → 名）。**只在恰好一条天生特性时给**，
  名字由 wiki「精灵 → 特性」表按形态反查而来（见 `docs/data.md`「特性名」），
  未经 id 校验，前端用虚线边框弱化显示。

#### `run.pet` 的外观：异色 / 炫彩

试炼带的是**玩家自己的精灵**，异色与炫彩原样带进去。三个字段都取自内嵌 `PetData`
（`trial_pet_data.pet_data`，即 `ParsePet` 的字段 13）：

| 字段 | 来源 | 说明 |
| --- | --- | --- |
| `shiny` | `mutation_type` 字段 45，bit0 | 异色 |
| `colorful` | `mutation_type` 字段 45，bit3 | 炫彩 |
| `glassType` / `glassValue` | `glass_info` 字段 86 的 17/18 | 炫彩配色，前端按色卡还原 |

与宠物主流程（`internal/pet/model.go`）同一套判据。**位是并存的**，不是三选一：
异色炫彩精灵两者同时为真。

⚠️ **异色头像不是每只精灵都有素材**。没有时 `gamedata.imageOf` 静默回退普通图，
此时 `shiny` 仍为 `true` 而 `img` 是普通图的路径 ——
所以**前端不可拿 `img` 反推 `shiny`**（如判断路径里有没有 `_1`），
那样没有异色素材的精灵会被误判成普通的。要判断异色，只看 `shiny` 字段。

- 一局结束有两条路径收敛：0x196a 结算通知，或档案 0x1975 里最新战绩的补判
  （服务器未必发结算通知，见 `docs/pcap-20260831-grass-trial.md` 第 5 节）。

#### `run.floor` / `run.chapterName` / `run.opponents`（静态配置）

协议只下发编号（`chapterId` / `nodeIndex`），「这一层是什么、对面可能是谁」协议里没有，
来自**静态配置**（`scripts/gen_trial.py` 从 wiki 生成，见 `gamedata/trial.go`）：

```json
{ "floor": "npc", "floorLabel": "NPC", "chapterName": "记忆中的巨石阵",
  "opponents": [{ "id": 310005, "name": "易西",
                  "pets": [{ "base": 3031, "name": "奇丽果", "img": "HeadIcon/3031.webp" }] }] }
```

- **`floor` 按 `nodeIndex`（0~7）查表**，两者不是简单的一一对应：协议每章 8 个节点，
  wiki 只有 7 层。`nodeIndex 0` = `start`（章节起点，无战斗），1~7 依次对应 wiki 的
  1~7 层（`normal`×3 → `boss` → `normal` → `merchant` → `npc`）。这个映射是抓包实测
  得出的，见 `scripts/gen_trial.py` 文件头的两条证据，**不要照抄 wiki 改成 7 段**。
- `opponents` **只在 `floor == "npc"` 时出现**，其余层为 `null`。
- ⚠️ **`opponents` 是候选池，不是「当前遭遇的对手」**：wiki 的 opponent id（300xxx 等）
  与协议里的 `npc_id`（实测 86023）**不是同一套编号**，无从绑定。前端措辞应照此表述。
- `pets[].img` 可能缺失 —— 并非每个形态都有头像。
- 静态配置缺失时（数据没生成）`floor` / `chapterName` 为空、`opponents` 为 `null`，
  不是错误，前端应能容忍。

### `GET /api/trial/encounters` ✅

草系试炼的**遇见记录**：三章各一张精灵图，列出本章可能遇到的精灵，遇到过的置灰。
与 `GET /api/trial`（实时状态、走 SSE）不同，这份是**累积的历史、直接读库**，
故不随 SSE 推送 —— 打完一局重新进页或刷新即可。

```json
{ "account": "UID:1", "ts": 1700000000,
  "updated": "S3 铅字幻梦 2026/08/18（页面标注的更新时间）",
  "chapters": [{ "chapter": 1, "name": "记忆中的索米亚草原",
                 "total": 230, "seen": 2,
                 "normal": [{ "base": 3001, "name": "喵喵", "img": "HeadIcon/3001.webp",
                              "seen": true, "kind": 0, "time": 1700000000 }],
                 "boss":   [{ "base": 8101, "name": "圣水守护_草系徽章-首领形态",
                              "img": "HeadIcon/4005.webp", "seen": true, "kind": 1,
                              "time": 1700000000 }] }] }
```

- **每章独立计算**：同一只精灵在第 1 章遇到过，第 2/3 章的图里仍算未遇见。
  与 wiki 口径一致（页面注明「3 章首领按章节独立计算」）。例：3005 同时在第 2、3 章池里，
  只在第 2 章打过照面 → 第 3 章那张图仍显示未遇见。
- `normal` 是普通池（第 1/2/3/5 层，208/315/197 只），`boss` 是第 4 层的 22 名首领、
  **三章共用**。二者来源独立（`gamedata.TrialPool` / `TrialBosses`），故分开列出。
- ⚠️ `kind` / `time` 是**可选指针**：未遇见时**键不出现**（不是 `0` 或 `null`）。
  `kind` 取值见 `TrialEncounterPet`：`0` 普通 / `1` 首领 / `2` NPC / `3` 最终 BOSS。
  ⚠️ 正因为普通战 `kind` 就是 `0`，这两个字段**必须用指针、不能加 `omitempty` 值类型**，
  否则 JSON 会抹掉取值 0 的键，前端便分不清「普通战遇到过」与「压根没遇到」——
  与 `docs/api/README.md` 里 `u`/`v` 那条是同一个坑。
- ⚠️ 精灵池来自**静态配置**（wiki），与数据库无关，故**没有遇见记录时 `chapters` 照样存在**
  （三章齐全、每章 `seen: 0`）。`chapters` 上的 `omitempty` 因此是个死标签 —— 判空请用
  `chapters.length`（仅在静态配置缺失时才为空），不要指望那个 `omitempty`。
- `extra` 是**见过但不在上面两组池子里**的精灵 —— 主要是 NPC 战(`kind: 2`)与最终
  BOSS(`kind: 3`)。静态配置只有普通池与 22 名首领,**没有第 7 层的精灵池**,这些遭遇
  无处安放。回放实测就撞上了:3027(NPC 战)与 5061(最终 BOSS,即敌方式斗酷猫)
  都真实打过照面却不在 pools/bosses 里,按旧逻辑会**静默丢失** —— 用户明明遇到过,
  图上却永远显示未遇见。故单列一组,不丢弃。
  ⚠️ **`extra` 不计入 `total` / `seen`**:那两个字段的口径是「池子里还剩多少」,
  把来源不明的条目塞进分母会让进度百分比失去意义。展示时也应与上面两组分开。

- `updated` 是静态配置的更新时间，用于提示「精灵池可能与当前版本有出入」；取不到时键不出现。
- 数据来源：池子来自 `gamedata`（wiki），遇见情况来自 `trial_encounter` 表
  （管线解析 0x1316 写入，见 `internal/trial/battle.go`）。只记试炼战斗 —— 以消息带
  `grass_trial_battle_info` 为准，野外/PVP 不会进库。

---

## 其它有快照的接口

### `GET /api/eggs` ✅
```json
{ "eggs": [{ "gid": 9001, "itemId": 5001, "name": "友爱天天的蛋", "species": "火神",
             "icon": "egg/5001.webp", "heightM": 0.3, "weightKg": 1.2,
             "obtainedAt": 1700000000, "src": 1, "srcName": "牧场",
             "hatching": true, "hatchedSecs": 600, "maxSecs": 3600,
             "hatchUpdate": 1700000000, "random": false, "typeOrder": 0 }],
  "hatchRate": 5 }
```
`hatchUpdate` 是上面三个数的计算时刻，前端据此外推孵化进度。

`hatchRate` 是孵化倍率（每过 1 真实秒推进多少孵化秒），整个响应一份而非逐蛋——
倍率是全局的，不逐蛋（实测三颗不同 `maxSecs` 的蛋同秒各 +10s，统一 5.00）。

它取自两个来源，**实测优先**：

| 来源 | 取值 | 何时变 |
| --- | --- | --- |
| 玩家实测（`hatchSpeed.rate`） | 两次 0x0312 进度下发的差分 | 玩家在孵蛋页测过速之后 |
| 活动倍率（`pet.HatchActivityRate`） | 加速日时间表，按当前时刻算 | 只在窗口起止 |

即 `hatchRate = hatchSpeed.state=="done" ? hatchSpeed.rate : 活动倍率`，
只有 **1x / 5x 两档**——**不含**移动增益：在线加成没有可信定值（实测移动档
16.90~27.32），按它外推会在半数样本上虚报，还会随玩家跑跑停停每几秒跳一次
（详见 `docs/data.md` 3.6「为什么不再按移动状态推算」）。

加速日窗口是每周**北京时间周五 04:00 ~ 周一 04:00**，服务器在窗口内照此推进、
**玩家离线期间也一样**，故离线外推天然准确，不需要攒样本、不存在冷启动。

`activityRate` 单独返回活动倍率那一份，供前端区分「这个数是实测的还是按时间表算的」
（徽章要标出来）。

`hatchSpeed` 是测速状态机，字段：

| 字段 | 说明 |
| --- | --- |
| `state` | `idle` 未测速 / `armed` 已点开始等第一次 / `first` 已采一次等第二次 / `done` 已完成 |
| `rate` | 测出的倍率（仅 `done` 时有效；`omitempty`，`idle` 时缺失） |
| `t1` | 第一次采样时刻（unix 秒，抓包主机时钟），前端据此显示已等多久 |
| `measuredAt` | 完成时刻（unix 秒） |

测法是玩家在页面点「开始测速」后去游戏里**打开两次孵蛋器**（进度没有被动推送，
必须他自己触发两次）。两次间隔需 ≥10 秒——服务器约按 5 秒步长结算，短区间是假值；
间隔不足时第二次采样会**重置起点**而非算出错误结果。

测速结果**落库**，页面刷新与服务重启都保留；**玩家下线时清除**（与游玩会话的下线
判定同一处），因为倍率依赖他此刻的状态，下次上线不作数。

> ⚠️ `hatchRate`、`activityRate`、`hatchSpeed.rate` 都随当前时刻/玩家状态而变，
> 写契约 golden 时必须抹掉（见 `internal/server/contract_test.go` 的 `scrubHatch`）。
> 抹漏一个的后果是 **golden 只在生成它的那一档星期里是绿的，换一档就红** —— 实测漏抹
> `activityRate`（周五 04:00~周一 04:00 是 5、其余 1）时，周末生成的 golden 到
> 周一~周四全红，而周五当天跑却是绿的 —— 这类问题最容易在工作日早上集中爆出来。
> 防它的是 `TestScrubHatchIsWeekdayProof`：不依赖系统时间，直接断言工作日与周末
> 两个取值被抹成同一份。
>
> 进度采样由 0x0312 驱动，状态变化时会广播 SSE 事件 **`eggs`**（仅带 `{account}`，
> 前端自行重拉）。详见 `docs/data.md` 3.6 与 `internal/store/hatch_speed.go`。

### `POST /api/hatch/speed` ✅

开始或取消孵蛋倍率实测。请求 `{"on": true|false}`，返回新的 `hatchSpeed` 状态。

- `on: true` —— 重置并进入 `armed`（若已有结果会**清掉**，等于重测）；
- `on: false` —— 取消，回到 `idle`。

同账号可能开着多个页面，故后端会广播 `eggs` 让它们一起变。

### `GET /api/handbook-glasses` ✅
```json
{ "glasses": [{ "base": 3006, "name": "火神", "book": 1, "head": "HeadIcon/3006.webp",
                "stage": 2, "evo": 100, "common": [131073], "hidden": [2] }] }
```
按图鉴号升序。`common` / `hidden` 为 `glass_value` 列表（已去重保序）。
数据来自登录包 pet_handbook 快照，非实时。

### `GET /api/breeding` ✅

本账号全部培育线。**建议与线一起下发**，不另开 `/suggest` 接口：建议依赖这条线的目标，
目标一改建议就变；分开请求时前端每次改目标都得记得再拉一次，还会出现「线已更新、
建议是旧的」的中间态。

```json
{ "lines": [{
    "id": "contract-line", "species": "火神", "confId": 3006,
    "evo": 3003, "chainName": "火花（焰火/火神/烈火战神）",
    "goal": { "voice": 96, "weightPct": 98, "nature": "固执",
              "natureIn": ["逞强", "固执", "大胆", "调皮", "勇敢"] },
    "status": "active",
    "gens": [{ "gen": 1, "mother": { ...亲本快照... }, "father": { ... }, "child": { ... },
               "source": "manual", "at": 1700000000, "note": "..." }],
    "pending": [{ "gen": 2, "mother": { ... },
                  "fathers": [{ ... }, { ... }], "source": "auto", "at": 1700000100 }],
    "suggest": [{ "mother": { ... }, "father": { ... }, "score": 0.492, "ambiguous": true,
                  "exp": { "voice": 64, "weightPct": 0, "weightHi": 2,
                           "natureP": 0.6645, "natureFrom": "parent" } }],
    "backcross": { "advised": false, "reason": "换种更接近目标：…",
                   "withParent": { ... }, "exp": { ... },
                   "alt": { ... }, "altExp": { ... } },
    "reach": { "target": 96, "best": 64, "hit": false },
    "nestPlanGid": 2005, "nestPlan": { "gid": 2005, "name": "小水母", "nature": "胆小", ... },
    "createdAt": 1700000000, "updatedAt": 1700000100 }],
  "nest": { "gid": 2001, "name": "小母", "nature": "固执" } }
```

| 字段 | 说明 |
| --- | --- |
| `evo` / `chainName` | 线的品种：`evo` = 进化链 id（0 = 无链，此时按 `species` 匹配），`chainName` = 品种展示名，口径与 `filter-options.chains[].label` **完全一致**（有链时带各阶段与形态名，无链时只写名字 —— 见上文 `chains`）。`species`/`confId` 是**建线那一刻的快照**，宠物一进化就过期，故品种归并一律看 `evo` |
| `motherGid` / `mother` / `motherReleased` | **线的身份 = 种母**：`motherGid` = 这条线固定在哪个个体上（0 = 还没固定，此时 `mother` 由历代末代派生，老线不做迁移 —— 见 `internal/pet/breeding.go` 的 `MotherGidOf`）；`mother` 是她的快照（库里查不到时缺失）；`motherReleased` 表示「固定过、但她已不在宠物库」（放生 / 送人）—— 与「从没固定过」是两回事，前端要分开显示 |
| `fatherGid` / `father` / `fatherReleased` | 这条线**指定**的种公（与种母成对的另一半，建线时可选、都可空）：`fatherGid` 0 = 没指定，**不做派生**（每代父本各不相同，末代父本代表不了「指定的种公」）；`father` 是他的快照；`fatherReleased` 口径同 `motherReleased`。⚠️ 它是**计划值**：`gens[].father` 才是每一代**实际采用**的父本，抓包自动记录从不读 `fatherGid` |
| `goal` | 培育目标，各项都可留空（留空即不关心）：`voice`（-100~100）、`weightPct`（0~100）、`nature`（确切性格名）、`natureIn`（**可接受的性格集合**，「性格正面加某维」= 该维 +10% 的 5 个名字；与 `nature` 同时填按并集算） |
| `status` | `active` 进行中 / `done` 已达成 / `archived` 归档 |
| `gens[]` | 已认领的代数，按 `gen` 升序；`gen` 从 1 起 |
| `gens[].source` | `auto` 破壳时管线自动记下 / `manual` 页面补录 |
| `gens[].fathers[]` | 全部父本候选；串窝时不止一只，此时 `father` 可能缺失（实际是哪只无从确定） |
| `gens[].backcross` | 这一代是「子代 × 亲本」回交 |
| `pending[]` | **待认领**的一代：破壳时只知双亲（`mother` + `fathers`），`child` 缺失 |
| `suggest[]` | 按目标排序的 top 5「母 × 父」组合；`score` 越小越接近目标（只对填了的目标项计分） |
| `suggest[].ambiguous` | 该母本的父本候选不唯一（串窝） |
| `suggest[].backcross` | 这条组合的父本就是本线自己的子代 |
| `suggest[].exp` | **预期值**（按双亲均值推算），不是实测 |
| `exp.natureFrom` | `parent` 双亲之一带在目标性格集合里（双亲都在就是 60% 那一档） / `roll` 双亲都不在，只能等重掷（40% 里掷中） / `nest` **学院小窝**里那只在这一组里（100% 随它，见下面的 `nest`） / `none` 没设目标 |
| `nest` | **学院小窝的真值**：游戏里现在是哪一只 `{gid, name, nature}`（`gid: 0` = 空着，此时只剩这一个键）。**全库唯一**，与性别无关；它参与孵蛋时子代性格 **100% 随它**。**由游戏流自动维护**（自己家园里那件 `config_id 1001072` 家具的住户，见 `docs/data.md` 的培育一节），页面不必也不该去写它；`name`/`nature` 按 gid **现查**宠物库补充、不落库 —— 那只被放生后这两个键缺失，由前端写「已不在库」。手工兜底见 `GET/POST /api/nest` |
| `nestPlanGid` / `nestPlan` / `nestPlanReleased` | **学院小窝的计划值**（与 `fatherGid` 同属「计划值」）：这条线**打算**把小窝给谁 —— `nestPlanGid` `0` = 没打算，非 0 时 `nestPlan` 是那只的快照、`nestPlanReleased` 表示那只已不在宠物库。它唯一的用途是让玩家在**还没真把宠物放进游戏小窝**时也能按计划看到性格 100% 的预测（改它走 `POST /api/breeding` 整条覆盖写）。⚠️ 与 `fatherGid` 一样**不做写入校验**、**不参与达标判定**（`ReachGoal` 只看子代快照）、也不写进任何一代 |
| `backcross` | 回交 vs 换种的两组预期对比；**还没有子代时整个字段缺失**（不是空壳） |
| `reach` | 现有候选的下一代嗓音极限 `{target, best, hit}`；**只在目标填了嗓音时下发**（其余维度没有这种硬约束） |

`exp` 与 `backcross` 都是**预测**：嗓音取 `floor((母+父)/2)`，体重取双亲百分位均值并在
`weightHi` 给出乐观上界，性格按固定分档算命中率（`natureP`）：**母 30% / 父 30% / 其余 40% 从全表
重掷**（两个 30% 是互斥槽位，故双亲性格相同时那个性格是 60%），再加重掷槽本身掷中的那一份
`0.4 × |目标性格集合| / 30`。故 `natureP` 随集合大小走：目标只填一个确切性格时是 `0.61` / `0.31` /
`0.01`（双亲都在 / 一方在 / 都不在），「正面加某维」的 5 个名字则是 `0.66` / `0.36` / `0.06`。

**学院小窝是这套概率的唯一例外**：**生效的那一只**参与这一组时，子代性格 **100% 随它** ——
`natureFrom` 变 `nest`，`natureP` 只有 `1`（它那个性格在目标集合里）或 `0`（不在，这一项配不出来）。
与性别无关（它当种母、当种公都一样），但**逐组合**判：建议里把它换掉的那几组照旧按上面的分档算
—— 那时它根本没进这对亲本。

**「生效的那一只」= 计划值优先、没计划才用真值**（`nestPlanGid || nest.gid`）：设了计划说明玩家
就是按这份计划在试算（哪怕游戏里还没放进去），页面会把真值与计划并排写出来，不一致时给
「改用游戏真值」。口径与样本来源见 `docs/data.md` 的培育一节。

`reach.best` 是**现有候选里离目标最近的那组能达到的预期嗓音**，`hit` 表示其中是否真有恰好命中
目标的组合（空池时 `best = target`、`hit = false`）。它回答的是「照现在的库接着配，到底能不能到」：
嗓音是**向下取整的均值**，目标落在上限时只有 `100 × 100` 孵得出，差一点就永远差一点（`99 × 100 → 99`）；
负向同规则但**不是镜像** —— `floor` 朝负无穷取整，`-100 × -99 → -100` 反而够得着。

> 写操作见 `POST /api/breeding`（**整条覆盖写**：改目标 / 补录一代 / 删掉某一代共用一个入口）与
> `POST /api/breeding/claim`（认领子代）。两者落库后都广播 SSE **`breeding`**，只带 `{account}`，
> 前端自行重拉 —— 整条线含全部代数，塞进推送太重（与 `eggs` / `trial` 同一路数）。

### `GET /api/breeding/pool` ✅

手动补录面板的候选池：给一个**品种**，返回该品种的**全库**候选，不受 `/api/pets` 的 200 条分页限制
（漏掉一页就等于「库里还有更合适的个体而玩家无从知道」）。品种用 `?evo=` 标识（进化链 id），
无链形态（`evo=0`）时用 `?species=` 形态名；两个参数**原样回显**（`species` 为空时该键缺失），
前端据此缓存 —— 候选只与品种有关、与这条线无关。

```json
{ "evo": 3003, "species": "火神",
  "mothers": [{ "gid": 2001, "name": "小母", "species": "火神", "confId": 2000672,
                "img": "HeadIcon/3006.webp", "gender": "♀", "heightM": 1.5, "weightKg": 88,
                "heightPct": 62.4, "weightPct": 88.1, "voice": 40, "nature": "固执",
                "talentRank": "A", "eggGroups": ["龙"] }],
  "fathers": [], "kids": [] }
```

匹配口径与 `filter-options` 的 `chains` 选项一致：有链按 `evo` 归并（链上**任一**阶段的个体都算这个
品种），无链按 `species`。故一次就能拿到链上所有阶段的个体 —— 库里只养到「火花」时按名字筛「火神」
会什么都捞不到，而它们其实正是同一个品种。

三个队列形状相同（`pet.PetCandidate` = `pet.EggParent` 平铺 + `eggGroups` 字符串数组）：
种母 = 同品种 ♀、子代 = 同品种全部个体、种公 = ♂（该品种雌性的蛋组都能判定时才按蛋组粗筛，
任一雌性蛋组未知则整个不筛 —— 粗筛必须宁多勿漏）。每个队列按名字升序、同名按 gid 降序。
前端按关键词过滤后选择，选中结果仍以**快照**形式提交回 `POST /api/breeding`。

**不给品种**（`evo` 与 `species` 都为空/0）是合法调用，表示「还没定品种」：这时返回**全库**的
♀（种母队列）与 ♂（种公队列），`kids` 为空 —— 建线表单允许先挑种母，再由她把品种带出来
（蛋随母本），那一刻前端还没有品种可传。认领子代必须有品种来限定范围，故那时必然已经定了品种。

独立于 `/api/breeding` 的原因：候选池只与品种有关、与目标无关（改目标不必重拉），
而按线塞进去等于每条线重复一份几百只的清单。

### `GET|POST /api/nest` ✅ —— 学院小窝真值的手工兜底

**学院小窝**：游戏里只有一只，故这里也只有**一个** gid —— 全库唯一，不按账号隔离
（`academy_nest` 单行表，见 `store/academy.go`；切账号后旧 gid 落不到这个账号的库里，等于空着，
而那也正是真相：那只不归这个账号）。它参与孵蛋时子代性格 **100% 随它**，口径见上面
`GET /api/breeding` 的 `nest` 与 `exp.natureFrom`。

⚠️ **正常不需要写它**：真值由家园管线自动维护 —— 玩家在自己家园里把宠物放进/抱出学院小窝，
工具就跟着更新（见 `docs/data.md` 的培育一节），这条接口是**抓不到时**的兜底（没进过家园、
包里没抓到、或想手工纠正），以及页面上「把计划记为真值」那一键的落点。玩家自己「打算怎么放」
是另一件事，存在**培育线**里（`nestPlanGid`，走 `POST /api/breeding`）。

```json
{ "gid": 2001, "name": "小母", "nature": "固执" }
```

`GET` 读它，`POST` 改它（请求体 `{"gid": 2001}`，`{"gid": 0}` = 把小窝空出来 —— 此时响应只剩
`gid` 一个键）。库里没有的 gid 一律 400：小窝里那只参与孵蛋才有加成，一个库里没有的 gid 只会
**静默失效**（页面上勾上了、百分比却一动不动）。写成功后广播 SSE `breeding`，与培育的其它写操作
同一套路。`name`/`nature` 是读取时按 gid 现查宠物库补的，不落库。

前端把它做成**亲本卡上的一个勾选**（见 `ParentSlot.jsx`）：换一只就是勾上另一张卡，而「小窝只有
一只」由后端这一个 gid 保证 —— 前端不另存一份「只能勾一个」的状态。`/api/breeding` 的响应里带着
同一份 `nest`，故页面不必为它多拉一次。

---

## 无快照的接口（源码推导 ⚠️）

以下接口尚无 golden，结构据代码整理。需要精确契约时按
[README 的重新生成步骤](README.md#重新生成)补一个即可。

| 接口 | 结构 |
| --- | --- |
| `GET /api/pois?res=` | `{kinds:[{k,n,icon,on,num}], pois:[{k,u,v,n,i}]}`；`i` 仅采集物有(逐点品种图标，同层不同品种各不相同)，其余图层用 `kinds[].icon`；场景无底图时两者皆空 |
| `GET /api/paint` | `{res, layer, w, h, cell, corridor, safe, cells}`；`cells` 是 w×h 位的 base64 位图（每字节 8 格、低位在前）；无底图时 `w=0` |
| `GET /api/merchant` | `{now, day, status:"open\|closed\|idle", today:[{start,end,label,empty,merchant}], prev:[...]}`，`merchant` 是第三方原始 JSON |
| `GET /api/merchant/sub` | `{configured, subscribed, email, keywords}` |
| `GET /api/eggs/query` | `{source, total, matches:[{name,img,hatchSecs,score,heightPct,weightPct,confId,note}]}`；**两个数据源共用此结构**，详见下节 |
| `GET /api/home/query` | `{uid,homeName,homeLevel,roomLevel,comfort,pets:[{base,form,name,species,level,gender,mutation,status,head,book}],plants:[{seedName,harvest,ripeAt,canSteal,stolen}],cached,fetchedAt}`，详见下节 |
| `POST /api/account/verify` | `{ok, hasPin}` |
| 各 admin 接口 | 见 `web/src/api.js` 的注释（前端侧已逐个注明响应形状） |

### `GET /api/eggs/query` —— 两个数据源，一份契约

随机蛋（神奇的蛋）`conf_id = 0`，猜它孵出谁。用哪个源由服务端配置决定
（管理面板切换，见下节），默认**本地源**。两个源的响应结构一致，前端不分支：

```json
{ "source": "local", "total": 3,
  "matches": [{ "name": "权杖-Ⅱ", "img": "/img/HeadIcon/3410.webp", "hatchSecs": 57600,
                "score": 87.5, "heightPct": 25.0, "weightPct": 34.1,
                "confId": 3410001, "note": "孵化 16 小时" }] }
```

| 字段 | 说明 |
| --- | --- |
| `source` | `"local"`（本地源，默认）或 `"xianyu"`（咸鱼源） |
| `total` | 候选条数，恒等于 `len(matches)`（**不是**第三方响应里的 `total`） |
| `matches[].img` | **可直接赋给 `<img src>` 的完整值**：本地给 `/img/` 开头的站内路径、咸鱼源给外链。**与其它接口的相对路径语义不同，不要再套 `imgURL()`** |
| `matches[].score` | 匹配度 0-100，**仅用于排序，不是概率**（合成测试下真值进前 3 只有约一半，且那是未去重口径） |
| `matches[].heightPct` / `weightPct` | 蛋落在候选区间内的百分位，仅本地源提供；咸鱼源无此两维 |
| `matches[].confId` | 物种 conf_id。咸鱼源的 `pet_id` 口径未必相同，**勿跨源比较** |
| `matches[].note` | 本地给「孵化 N 小时」文案，咸鱼源给对方的 `hatch_label` |

请求参数：`height`（米）、`weight`（千克）、`maxSecs`（孵满秒数），都取自前端
`EggView`；**`maxSecs` 是最强的一维约束**（见 docs/data.md「随机蛋的区间藏在哪」），
能传一定要传 —— 缺了就退化成纯尺寸匹配，候选会宽得多。

**用哪个源由服务端配置决定，请求参数覆盖不了**（接口不收 `src`）：数据源是对全服
生效的运维选项，若能让请求参数覆盖，任何玩家都能夹带 `src=xianyu` 去烧第三方额度
（10 次/分钟）。切换走管理面板，见下节。

咸鱼源未配令牌时返回 **503**，且**不落统计**（统计是给「烧了多少额度」看的，
没发出去的请求不该计入）。本地源永不因缺令牌失败。

### `GET /api/home/query` —— 按 uid 查任意玩家的家园

⚠️ 与 `GET /api/home` **不是一回事**，别混：

| 接口 | 查谁 | 数据从哪来 |
| --- | --- | --- |
| `GET /api/home` | **自己** | 抓包（小窝图层、下蛋配对，见 `scene/home.go`） |
| `GET /api/home/query` | **任意 uid** | 回源第三方 rocodex.org，与抓包无关 |

参数：`uid`（必填，**非空纯数字**，**不限位数**——实测有 6 位与 9 位，位数由上游判定）、`force=1`（可选，跳过服务端 15 分钟缓存）。

> **登录后会自动预热一份**（当前登录账号自己）：抓包管线在**首次见到某账号登录**时异步回源一次，
> 填进同一份 15 分钟缓存。故用户点开家园查询页时通常直接命中（`cached=true`），不必等那两步回源
> ——那是登录时就提前省掉的。预热失败静默（只记日志），不牵连登录与页面；缓存还新鲜时不重复回源，
> 同一账号 15 分钟内重复登录（或多台设备同时在玩）都只算一次。实现见 `server.PrefetchHomeQuery`。

```json
{ "uid": "5678116", "homeName": "牢大", "homeLevel": 25, "roomLevel": 5, "comfort": 75780, "exp": 2116674,
  "pets": [{ "base": 3123, "form": 37147, "name": "秋天", "species": "雪影娃娃",
             "level": 60, "gender": "雌性", "mutation": "异色", "status": "已驻守",
             "head": "HeadIcon/3123.webp", "book": 123 }],
  "plants": [{ "seedName": "恶魔雪茄", "harvest": 18, "ripeAt": 1786498395,
               "canSteal": 6, "stolen": 0 }],
  "cached": false, "fetchedAt": 1788780000 }
```

| 字段 | 说明 |
| --- | --- |
| `pets[].base` | 品种 **petbase_id**，与本地 `gamedata` 同一套 id，故能直接查名/查图。上游把形态 id 放在 `gid` 字段，**不是**同一个东西 |
| `pets[].form` | 形态 id（上游 `gid`）。同品种不同形态各有一个，炫彩/异色绑在这一层（实测 3121 同时有 53264、54509 两个形态） |
| `pets[].name` | 玩家起的昵称（如「秋天」） |
| `pets[].species` | 品种中文名，取自本地名称表；查不到时回落上游 `defaultName` |
| `pets[].head` | 本地小头像，**相对路径**，需套 `imgURL()` 成 `/img/…`（与 `/api/eggs/query` 的 `img` 不同，那个已是完整值）。**异色个体给异色图**（`HeadIcon/<id>_1.webp`）；全库仅 19% 品种有专属异色图，没有的自动回退普通图 |
| `pets[].mutation` | `异色` / `异色炫彩` / 空。**没有色号** —— 上游只给这个文本，`glass_type`/`glass_value` 拿不到，别指望用它补图鉴炫彩 |
| `pets[].status` | `未喂食` / `已喂食` / `可收取灵感`，对应上游 status `1700` / `1701` / `1702`（由玩家在游戏内比对确认）。**未收录的状态码返回空串**，不用上游的 `statusText`（它把认不出的码拼成「状态 1700」这种内部枚举，给人看没有意义） |
| `pets[].feed` | 喂食轮次（上游 `feedRound`）。**不是时间**，语义待确认 |
| `plants[].ripeAt` | 成熟时刻（Unix 秒）；0=上游没给或格式不认 |
| `cached` | 是否命中服务端缓存（`force=1` 时恒为 `false`） |
| `fetchedAt` | 本份数据**回源**时刻；命中缓存时是上次回源的时间，不是本次请求时间 |

两点使用注意：

- **没有「是否已成熟」这个字段**，前端自己比 `ripeAt` 与当前时刻。上游的 `state` 已观察到 `1`=生长中（harvestNum 0、ripeAt 在未来）、`2`=可收获（harvestNum>0、ripeAt 已过），与 ripeAt 自洽，但样本仅两种，不足以下发布尔字段。
- **拿不到喂食时间**。上游 pet 的全部字段是 `id/gid/name/defaultName/level/gender/genderText/energy/mutationName/haveEgg/feedRound/status/statusText/icon`，**没有任何时间戳**。`feedRound` 是累计轮次、`energy` 恒为 10，都推不出时间。
- **回源失败会降级返回旧缓存**（同一 uid 此前成功过的话），此时 `cached` 为 `true`、HTTP 仍是 200。只有从没成功过才 502。上游是免费站，偶发失败是预期内的。

上游按 clientId 限额（每天 3 次），服务端每次请求先 `GET /api/home-query/quota` 领一个新 clientId 再查询，故额度不受限 —— **这个顺序不能省**，省了会撞限额（有测试守着，见 `api_home_query_test.go`）。

### `GET|POST /api/admin/egg-source` —— 切换查蛋数据源

管理员端点，范式同 `merchant-source`。

```
GET  → {source, keySet, sources:[{id, name, needKey}]}
POST {source} → {ok:true}
```

| 源 | 说明 |
| --- | --- |
| `local`（默认） | 本地解包数据反推，**用 `maxSecs` 硬筛**。零外部依赖、无限流、离线可用；无系别 |
| `xianyu` | 第三方图鉴。多给系别，但**不做时长筛选**，候选里会混入时长不符的物种；需 `-egg-api-key`，限流 10 次/分钟 |

与切远行商人数据源不同，这里**不清任何缓存**：两个源都是每次请求实时算，
没有跨源复用的缓存，故切换立即生效、也没有「切源后数据为空」这类代价。

`GET` 的 `keySet` 表示服务端是否已配 `-egg-api-key`；若当前源 `needKey` 而
`keySet=false`，管理面板会给出警示（该源当前取不到数据）。源清单由后端下发，
前端不硬编码 —— 合法标识只有后端能校验。
