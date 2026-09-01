# AGENTS.md

## 项目概述

`rocom-capture`：在 Linux 网关上被动抓取手机游戏《洛克王国：世界》(进程 `com.tencent.nrc`)
的 TCP 8195 流量，解析 tsf4g/GCP 协议，对**宠物信息**做自定义统计，并提供响应式 Web 页面。
支持局域网多设备同时在线,按登录 `user_id` 隔离多账号(单库加 `account` 列,见 docs/architecture.md)。
不读内存、不注入进程，只解析网络流量。Go 后端 + React 前端，构建为单二进制(前端经 embed)。

面向使用者的说明见 [README.md](README.md)；设计细节见 `docs/`：
[协议](docs/protocol.md)、[数据来源与解析](docs/data.md)、[服务架构](docs/architecture.md)、
[参考资料](docs/reference.md)。

## 解包数据流程

原始解包数据**不进仓库**,仓库只提交精炼后的生成物(详见 docs/data.md):

1. 从游戏目录原样复制 pak 到 `~/Downloads/rocom/Paks/`(或直接用安卓 .apk)。
2. `scripts/unpack.sh` 用 CUE4Parse 全量解包到 `~/Downloads/rocom/parsed/`,按虚拟路径镜像
   (顶层 `NRC/Content/...`):uasset/umap → 属性 json(纹理另出 png),其余(.bytes/.non/.pb/
   .lua 等)原样字节。并行、增量,`--filter <前缀>` 选导、`--list` 预览;默认排除三维美术/
   视频/音频等纯客户端运行时资源(`--exclude` 追加、`--no-exclude` 全量,清单见 --help)。C# 实现在
   `scripts/unpack/`,依赖 dotnet-sdk 与 CUE4Parse 克隆(默认 `~/Git/gh/CUE4Parse`,
   `CUE4PARSE_DIR` 覆盖;内置 `GAME_RocoKingdomWorld` 支持)。
   导出后自动跑两个后置步骤(增量,`--no-post` 跳过):全树 RocoBinData `.bytes` → 紧邻 `.json`
   (`scripts/bin2json.py`,需 uv;既供查数据也是 gen_* 输入)、`.luac` → `.lua` 反编译
   (`scripts/decompile_luac.sh`,需 unluac,单文件超时兜住死循环、真失败打 `.lua.nodecomp` 标记免重试)。
3. 生成脚本直接读 `parsed/`(解包根统一用环境变量 `ROCOM_PARSED` 覆盖,默认
   `~/Downloads/rocom/parsed`),产出随仓库提交的生成物。

## 并行工作的边界（多 AI / 多人同时改动时必读）

本仓库可能同时有几个 AI / 人在改动，各自负责不同区域。**跨区改动前先确认归属**，
尤其 `docs/` 是三方共享的公共区域。

| 区域 | 归属 | 说明 |
| --- | --- | --- |
| `internal/**`、`cmd/**`、`scripts/**`、`docs/api/**` | 后端 | Go 代码、生成脚本、对外 API 契约 |
| `web/**`、`internal/server/web/**` | 前端 | React 源码与 vite 构建产物（后者前端独占,后端只读） |
| `docs/`（除 `api/`）、`_private-inbox-DO-NOT-TOUCH/**` | 数据分析 | 解包/抓包分析结论、pcap 归档 |

规则：

1. **每个区域只有一个写者**。越界修改会与对方的未提交改动冲突，且对方无从得知。
2. **`docs/` 是公共区域**：改前先 `git status` 看是否已有人改了同一文件；
   分析类结论写进各自的新文件（如 `docs/pcap-<日期>.md`），不要直接往 `docs/data.md`
   里堆 —— 需要沉淀到那里的共识，先在文件内登记再改。
3. **跨区约束要双写**：一条约束若同时约束代码与文档，两边都要写。
   例如「炫彩判据必须是 `mutation_type & 8`」既在 `docs/data.md` 说明，
   也在 `internal/pet/model.go` 的 `Colorful` 处加了警示注释 ——
   只写在文档里，改代码的人看不到。
4. 沟通文件（`AI_*.md`）在重构结束后删除；长期共识沉淀到 `docs/`。

## 约定

- Go：`go build ./...`。代码生成:`uv run python scripts/gen_proto.py`(all.pb → internal/pb)、
  `uv run python scripts/gen_gamedata.py`(Bin 配置 + all.pb → names.json)、
  `uv run python scripts/gen_pbdesc.py`(all.pb + ProtoCMD.lua → internal/pbdesc/data:
  裁剪后的描述符集 + opcode→消息名,供 pcapdump 精确解码)、
  `uv run python scripts/gen_images.py`(解包 PNG → internal/gamedata/data/img 的宠物图 webp)、
  `uv run python scripts/gen_icons.py`(UI 图标 → img/{filter,blood,static,worldmap,medal,egg}:属性/
  六维/搭档标记、血脉主图标、手挑杂项、手挑大地图 POI、奖牌小图、精灵蛋图与蛋品类角标;图集精灵从解包属性 JSON + 图集 PNG 裁切,
  奖牌与精灵蛋等整张贴图直接转码;webp 保持原始解包文件名,语义键→原名索引写入 names.json;
  详见 docs/data.md)、  `uv run python scripts/gen_glass.py`(炫彩色卡 → 前端渲染配置 web/src/data/glassConf.js:
  直接读仓库内 COLOR_RANDOM_CONF.ts / PARTICLE_RANDOM_CONF.ts(不需解包数据),
  给出底图 Bg / 中层 Bg2 / 4 张粒子大图 / 39 组配色 [ui_color_1, ui_color_2] / 4 张隐藏整图
  (1/2/3 赛季、1000 黑白);普通炫彩 value=(粒子id<<20)|配色id 由前端按素材 alpha 蒙版
  CSS mask 三层填色合成(Bg 填 ui_color_2 → Bg2 填 ui_color_1 顶部对齐 → 粒子染白最上层),
  隐藏炫彩直接引用整图;渲染组件 web/src/components/badges.jsx 的 GlassChip)、`uv run python scripts/gen_bigmap.py`(大地图瓦片 → img/bigmap 整图 webp,4x4
  行主序拼合;另转分层地图切片 LayerMap → img/bigmap/layer;坐标单位/投影见 docs/data.md 3.1/3.2);
  `uv run python scripts/fetch_skill_ids.py`(刮 aismile.dev 技能图鉴 26 页 →
  `~/Downloads/rocom/skillIds.json`:**skill_id → 技能中文名** 604 条;站点无 JSON 接口,
  靠 HTML class 正则提取,改版后抓不到会报错退出)、`uv run python scripts/gen_skills.py`
  (skillIds.json + arkmeng.cn 的 skillGuideData.json → internal/gamedata/data/skills.json:
  技能 id→名 供试炼页、形态→天生技能 供宠物详情;**两份互补缺一不可,见 docs/data.md「技能名」**);
  `uv run python scripts/fetch_trial_data.py`(草系试炼静态配置:wiki 的
  Module:GrassTrialData → ~/Downloads/rocom/grassTrialData.lua;页面由 Lua 渲染,
  数据在模块里不在正文)、`uv run python scripts/gen_trial.py`(→ data/trial.json:
  层结构/各章精灵池/22 名首领/NPC 阵容;**层与 node_index 的对应是抓包实测的,
  别照抄 wiki 的 7 层**,详见 docs/data.md)、
  抓包脚本 `scripts/capture.sh`(bash)。`.bytes` 配置解码用 `uv run python scripts/bin2json.py`
  (unpack.sh 已自动调):全树 RocoBinData `.bytes` → 紧邻 `.json`(增量,秒级),既供 grep/jq
  查数据、也是 gen_gamedata/gen_icons 的输入(它们直接读这些 JSON,不再自行解 .bytes)。
- **shader 逆向与 3D 渲染的工具链已迁到 [rocom-pets](https://github.com/whoisnian/rocom-pets)**
  (`scripts/shaderdump.py` / `dxbcdis.c` / `dxbcsig.py` / `matshader.py` / `uniexpr.py` /
  `matparams.py` / `glsldump.py`,文档 `docs/shader.md` 与 `docs/android-glsl.md`)。
  它们只服务于宠物材质还原,与本仓库的抓包/统计/生成流程零耦合;`unpack.sh` 仍在本仓库
  (三维之外的 Bin 配置、图标、大地图都靠它)。
- pcap 调试:`go run ./cmd/pcapdump -pcap <文件>` 把回放消息输出为「适合 AI 分析」的结构化文本,
  免去为调试新协议临时写一次性程序。三种模式:无参=opcode 概览(次数/方向/名称);
  `-op 0x1888,FREE`=转储匹配 opcode 的消息(opcode 支持 hex/十进制/名称子串,`-hex` 附原始字节);
  `-gid 20508,15895`=扫描某宠物编号出现在哪些 opcode。
  转储默认**精确解码**:按 opcode 查 `internal/pbdesc` 内嵌的游戏描述符,用真实消息类型解出带
  字段名/枚举名的树(消息边界靠「无未知字段 + 消费最多 + 回序列化长度一致」在 c2s 子头/tsf4g 尾之间试出);
  opcode 未映射或该版本对不上时自动退回通用 wire 级解码(只有字段编号)。`-msg .Next.XxxRsp` 可
  强制消息类型,`-wire` 强制只用 wire 级。
- 数据来源**均为自行解包提取**,不依赖外部数据仓库:中文名称表来自解包目录的 Bin 配置
  (由 `scripts/bin2json.py` 按 CUE4Parse 的 FRocoBinData 算法解为 JSON);`internal/pb` 结构、opcode、枚举同出
  游戏描述符 all.pb(前者经 protoc `--descriptor_set_in` 生成 Go,后者经 `scripts/pbdesc.py`
  读描述符);宠物图片索引(conf_id→头像/全身图)取自 `PETBASE_CONF`/`MODEL_CONF`,
  图片本体(webp)经解包 PNG 转码后 embed。更新游戏版本:重新复制 pak、重跑 unpack.sh、
  重跑生成脚本(详见 docs/data.md)。
- 前端：`web/` 下 `npm run build`，产物输出到 `internal/server/web/`(已提交，便于 `go build` 开箱即用)。
- **对外 HTTP 契约见 [docs/api/](docs/api/README.md)**：端点表、响应字段、以及机器可读的
  `fields.json`。契约由 golden 快照守护 —— 改了响应结构后跑
  `UPDATE_CONTRACT=1 go test ./internal/server/ -run TestContract` 重生成并 review diff，
  再 `uv run python scripts/gen_apifields.py` 更新 `fields.json`。
  三处易错点（详见 `docs/api/README.md`）：
  - `fields.json` 是 golden **样本**，非完备清单 —— `omitempty` 字段可能缺失。
    position/wildpets/home/flowers 的完备字段清单在 `internal/server/payload.go` 的 struct 定义。
  - **重新生成 golden 时 diff 只能说明「变了什么」，不能说明「该不该变」**。改动实时推送侧后，
    用 `bash scripts/capture_sse.sh <pcap>` 抓真实推送与改动前构建逐字段对比
    （脚本先挂 SSE 再启动回放，回放是一次性的；默认端口 4940，4939 是前端 dev server 的代理目标）。
  - 可选字段用**指针**而非 `omitempty` 值类型：`u`/`v` 为 0 是合法值，用值类型+omitempty 会被误删。
- Python 脚本依赖用 uv 管理(项目内 `.venv`)，勿用系统级 pip。
- **重构务必用 `uv run python scripts/check_refactor.py` 自证**：它回答「有没有**未登记**的改动」。
  比 `go build` / `go vet` / golden 测试多查三样:注释归属(函数注释与函数体被拆到不同文件,
  三者都查不出来)、注释完整性、净内容守恒。忽略 gofmt 的合法重排(单行 struct 展开、
  注释列表后插入裸 `//` 行)。有意删除/改写的东西登记在脚本内的白名单
  (`ALLOWED_REMOVED_FUNCS` / `INTENTIONALLY_CHANGED` / `RENAMES`),**新增改动务必连带理由登记**,
  否则脚本会退化成噪音。退出码非零即有问题。
- **新增测试后要做变异测试**：故意注入缺陷(改错一个常量、去掉一行赋值、反转一个比较符),
  确认测试真的会失败。**不验证过的测试等于没有** —— 一个永远绿灯的测试只给人虚假的安全感。
  覆盖率数字本身说明不了这一点。
- `internal/pb/*.pb.go`、`internal/gamedata/data/names.json`、`internal/pbdesc/data/*` 为生成物，
  改动应改生成脚本而非手改。
- 相关工具与开源项目清单见 [docs/reference.md](docs/reference.md)。
