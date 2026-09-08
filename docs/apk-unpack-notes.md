# 安卓客户端解包实测记录

目标:用官方客户端(`com.tencent.nrc`)的配置表补齐伤害估算的数据缺口。
**结论:成功了** —— 技能、印记/状态、天气、特性四张表都拿到了官方依据。

## 一、包体与解包路径

| 项 | 值 |
| --- | --- |
| 官网 | `https://rocom.qq.com/` → 下载入口 `https://rocom.qq.com/zlkdatasys/mct/d/play.shtml?device=android` |
| APK | `https://dlied4.myapp.com/myapp/1110613799/cos.release-75793/10040714_com.tencent.nrc_a4382074_1.110.0.109_4mMrGY.apk`(2.12 GB,v1.110.0.109;同版本有 4 个渠道包,任意一个都行
| OBB | APK 内 `assets/main.obb.png`(953 MB),**其实就是 zip**,里面 `NRC/Content/Paks/*.pak` 共 10 个
| 解包工具 | 仓库自带 `scripts/unpack.sh` + `scripts/unpack/`(C#/CUE4Parse,内置 `GAME_RocoKingdomWorld`)

### 环境(本机原本没有)
```bash
# dotnet SDK 10+ —— 装完还缺 libicu
curl -sSL https://dot.net/v1/dotnet-install.sh | bash -s -- --channel 10.0 --install-dir "$HOME/.dotnet"
apt-get install -y libicu-dev          # 否则报 "Couldn't find a valid ICU package"
export PATH="$HOME/.dotnet:$PATH"
git clone --depth 1 https://github.com/FabianFG/CUE4Parse ~/Git/gh/CUE4Parse
```

### 实测步骤
```bash
# 1) 下载 + 取出 pak(约 5 分钟 + 1 分钟)
mkdir -p /tmp/rocom-apk && cd /tmp/rocom-apk
curl -L -C - -o nrc.apk '<上面的 APK URL>'
unzip -o nrc.apk 'assets/main.obb.png' -d ext
python3 -c "import zipfile,shutil,os; z=zipfile.ZipFile('ext/assets/main.obb.png'); [shutil.copyfileobj(z.open(n), open('/tmp/rocom-apk/paks_obb/'+os.path.basename(n),'wb')) for n in z.namelist() if n.endswith(('.pak','.utoc','.ucas'))]"

# 2) 先预览(别一上来全量 2 GiB)
bash scripts/unpack.sh --paks /tmp/rocom-apk/paks_obb --list BinDataCompressed

# 3) 精准导出数值表(808 张,78 MB,约 0.1s)
bash scripts/unpack.sh --paks /tmp/rocom-apk/paks_obb \
     --filter 'NRC/Content/ScriptC/Data/Bin' --no-post
uv run python scripts/bin2json.py ~/Downloads/rocom/parsed   # .bytes → .json
```

- 挂载 10 个 pak、34020 个文件正常;AES 用 `unpack.sh` 内置默认值即可(1.110 未换密钥)。
- 默认排除策略(`ArtRes`/`Movies`)不影响数值表。

## 二、拿到哪些表(都在 `NRC/Content/ScriptC/Data/Bin/BinDataCompressed/`)

| 表 | 条数 | 关键字段 | 补到哪 |
| --- | --- | --- | --- |
| `SKILL_CONF` | 1888 | `dam_para[0]`=威力、`energy_cost[0]`=能耗、`skill_dam_type`=系别、`type`(1主动/2被动)、`damage_type`(1无/2物攻/3魔攻)、`desc` | `calc_skills.json`(**按 id 直取**,覆盖率 674 → **1906**)
| `BUFF_CONF` | 3002 | `name`、`type`(1能力等级/2负面状态/3特性/4印记)、`desc`、`type_id` | `calc_mark_ids.json` 权威 `buff_id → 名字`(978 条 high)、特性 buff 索引
| `WEATHER_CONF` | 15 | `weather_type`、`name`、`weather_buff`(该天气挂的 buff)、`temperature` | `calc_mark_ids.json` 的 `weather` 段
| `PET_TALENT_CONF` | 98 | `name`、`desc`、`effect_group`、`condition_group` | `calc_traits.json` 的 `talents`
| `ABNORMAL_STATUS_CONF` | 15 | `class_name`(`AbnormalStatus_Hot/Cold/Toxicity/…`)、`status_type`、`max_duration` | 负面状态清单(灼烧/冻结/中毒/催眠…)
| `PETBASE_CONF` | 1128 | `hp_max_race`、`phy_attack_race`、`spe_attack_race`、`phy_defence_race`、`spe_defence_race`、`speed_race`、`SUM_race` | **形态种族值**(按 id 直取,见下)
| `TYPE_DICTIONARY` | 22 | `type_name`、`type_restraint<N>`:对第 N 系 `1`=×2、`-1`=×0.5、缺省=×1 | **18 系克制矩阵**(见下)
| `ATTR_GLOBAL_CONFIG` | 65 | `phy/spe_dam_param`=41、`*_dam_level_mag`=45、`*_dam_level_add`=10、`*_dam_floor`=2、面板六维常数 | **伤害公式参数**(见下)

#### `PETBASE_CONF` —— 形态种族值

六维字段与 `calc_race.json` 的键一一对应:`hp_max_race→hp`、`phy_attack_race→physicalAttack`、
`spe_attack_race→magicalAttack`、`phy_defence_race→physicalDefense`、`spe_defence_race→magicalDefense`、
`speed_race→speed`。

⚠️ `SUM_race` **不是**六维之和(花衣蝶六维和 539,`SUM_race` 却是 553),语义未确认 ——
**不要**拿它当校验依据。校验一律走协议实测锚点(见下)。

实测「花衣蝶」:`hp_max_race`=122、`phy_attack_race`=67、`spe_attack_race`=72、
`phy_defence_race`=84、`spe_defence_race`=94、`speed_race`=100、`SUM_race`=553 ——
与 `docs/calc-data.md` 记录的**抓包实测锚点**(HP 122、物攻 67)**一致**,口径可直接采用。

1128 行 ⇒ 接入后覆盖率 **53.8%(611/1136)→ 95.2%(1081/1136)**,且**按 id 直取**,
不再依赖中文名桥接(缺口是不在 `names.json` 里的形态)。

⚠️ 已知冲突:`3179 恶魔红钻` 官方物防/魔防 = 93/74,协议实测锚点是 108/89(差 15)。
roco 快照里它**没有** S4 前瞻条目,所以不是「前瞻未回退」造成的 —— 成因待查。
处理:生成脚本以**协议锚点为准**取值,并把冲突打印出来(不静默覆盖)。

2026-09-09 复核:**108/89 才是官方图鉴数据**(协议侧与图鉴一致),故以锚点为准是对的;
`PETBASE_CONF` 的 93/74 来源不明(疑为另一形态或未启用的旧值),**不要**拿它覆盖锚点。

#### `TYPE_DICTIONARY` —— 18 系克制矩阵

22 行,id 2–20 为 18 个有效系;id 7「岩系」字段全空且自带「废弃!」标注,21/90 为占位。
系别顺序与现有 `calc_types.json` 的 `types` **完全一致**:
普通 2 / 草 3 / 火 4 / 水 5 / 光 6 / 地 8 / 冰 9 / 龙 10 / 电 11 / 毒 12 /
虫 13 / 武 14 / 翼 15 / 萌 16 / 幽 17 / 恶 18 / 机械 19 / 幻 20。

按 `1→×2`、`-1→×0.5`、缺省→`×1` 展开为 18×18 矩阵,与现有快照矩阵 **324 格零差异**
(说明第三方快照内容正确;换官方来源是为了权威与随版本可更新,不是修错)。

已接入 `gen_calcdata.py`:官方表优先、快照兜底,且**每次生成都会与快照逐格对拍并打印差异数**
—— 哪天官方改了克制关系,生成时就会看见。

#### `ATTR_GLOBAL_CONFIG` —— 伤害公式参数(`key` / `num` 结构)

- 等级系数:`phy/spe_dam_level_mag`=45、`*_dam_level_add`=10、`*_dam_param`=41
  ⇒ `(level × 45 / 100 + 10) / 41`,**佐证** roco 口径(此前无官方依据)
- 伤害下限:`phy_dam_floor`=`spe_dam_floor`=2 —— 现有口径清单**未涉及**,接入时需核对
- 面板六维常数:`*_race_constant`(HP 200 / 其余 100)、`*_talent_constant`(HP 100 / 其余 50)、
  `*_base_point_constant`=10000、`*_race_add_level`(HP 25 / 其余 50)、`*_talent_add_level` 同
- 上限:`at_*_maximum`(HP 500 / 其余 300)、`race_*_maximum`(HP 255 / 其余 200)、`talent_*_maximum`=50

接入情况:`floor` 已随 `/api/calc-rules` 下发,前端按**每段伤害**兜底,且只在官方确实下发了值时
生效(未下发就老老实实是 0,不编默认值);45/10/41 目前仅作口径佐证,未改动现有公式。

## 三、纠正了之前的错误推断 ⚠️

pcap 对拍推断(medium)曾把 `20070020` 判成**冻结**;官方表:`20070020` = **灼烧**;冻结是 `20580010`。其余 7 条推断(减速/中毒/光合/湿润/棘刺/风起)与官方一致。故官方表一来,medium 一律被 high 覆盖。

## 四、仍然没拿到的

- **连击段数**:`SKILL_CONF.hit_para` 恒为 `10000`(即 ×1),描述里的「3连击」对应段数不在这张表里。
  本次复查:技能效果走 `skill_result[].effect_id`(「乘风连击」= `20350300`),但 `EFFECT_CONF` 的 id
  是 7 位(`1001001`…)、`BUFFBASE_CONF`(2590 行)里也查不到 —— **effect_id 指向的表仍未定位**。
  协议侧 `cast_cnt` 也多为 1,故段数仍只能靠抓包实测。
- 技能**效果系数**(如「威力+30%」的触发时机):`BUFFBASE_CONF`(2590 行,`buffbase_param`)与
  `EFFECT_CONF`(1168 行,`effect_param`)两张表都在,但尚未解析。

## 五、生成脚本与产物

```bash
# 技能(官方优先、roco 兜底)、种族值、克制表
uv run python scripts/gen_calcdata.py
# 印记/特性规则(从 roco 源码校验名字 + 官方表补 buffIds/talents)
uv run python scripts/gen_calcrules.py
# buff_id → 名字(权威) + 天气
uv run python scripts/gen_markids.py --from-parsed
```

- 解包根由 `ROCOM_PARSED` 指定(默认 `~/Downloads/rocom/parsed`);统一入口见 `scripts/gamedata_sources.py`。
- **只提交 json 生成物**;APK / pak / 纹理一律留在 `/tmp`,不入库。

## 六、合规

数据出自官方客户端,仅供本非商业玩家工具本地统计;不重新分发游戏素材(纹理只在本机核对)。
生成物的 `_source` 均标注来源与许可/边界。
