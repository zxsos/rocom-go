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

## 三、纠正了之前的错误推断 ⚠️

pcap 对拍推断(medium)曾把 `20070020` 判成**冻结**;官方表:`20070020` = **灼烧**;冻结是 `20580010`。其余 7 条推断(减速/中毒/光合/湿润/棘刺/风起)与官方一致。故官方表一来,medium 一律被 high 覆盖。

## 四、仍然没拿到的

- **连击段数**:`SKILL_CONF.hit_para` 恒为 `10000`(即 ×1),描述里的「3连击」对应段数不在这张表里。段数字段仍未定位(协议侧 `cast_cnt` 也多为 1)。
- 技能**效果系数**(如「威力+30%」的触发时机)散在 `BUFFBASE_CONF`/效果表里,尚未解析。

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
