# rocom-go

《洛克王国：世界》的**游戏数据分析工具**：在 Linux 网关上被动抓取手机游戏的 TCP 流量，
解析其私有协议，把宠物、精灵蛋、远行商人、涂地、试炼等数据做成一个响应式 Web 面板。

**不读内存、不注入进程、不改客户端** —— 它只看网络包，本质上是一个懂这套协议的分析器。

> ⚠️ 公开分发前请先读[免责声明](#免责声明)：本项目涉及第三方游戏的私有协议与客户端素材。

## 它能做什么

- **宠物与仓库** —— 解析 `PetData`，按个体值 / 性格 / 特性 / 血脉 / 异色炫彩统计与筛选，历史变更可回溯
- **精灵蛋与孵化** —— 孵蛋记录与孵化速率；**「猜猜孵出谁」**用本地解包配置，按蛋的尺寸 + 孵化时长反推候选物种
- **远行商人** —— 8/12/16/20 四轮货单，按 4h 槽缓存；可按关键词**邮件订阅提醒**
- **图鉴与炫彩** —— 39 组配色 + 粒子素材合成的炫彩渲染，与游戏内表现对齐
- **实时地图** —— 位置、分层、野生宠物分布、涂地覆盖位图
- **草系试炼** —— 层结构、各章精灵池、首领阵容
- **多账号隔离** —— 局域网多台设备同时在线，数据按登录 `user_id` 隔离，切账号需 PIN
- **实时推送** —— SSE 增量更新，断线自动补拉；手机页支持「屏幕常亮」（需 HTTPS）
- **管理面板** —— 隐式入口 `#/admin`：黑白名单规则、订阅名单、游玩记录、配置热更
- **离线回放** —— `-pcap` 直接分析已抓好的包，设备不必在线

## 快速开始

需要 root（afpacket 原始套接字）。完整的环境准备、cgo 注意事项、systemd 与 Docker 部署
见 [docs/deploy.md](docs/deploy.md)。

```bash
go build -o rocom-go ./cmd/rocom-go     # 前端产物已随仓库提交，可直接编译
sudo ./rocom-go -iface auto             # auto = 自动选默认路由所在的网卡
```

打开 <http://localhost:4939>。**先启动本工具**（要抓到密钥协商包），再进游戏
**打开一次宠物仓库**触发宠物列表下发。会话密钥会随连接落库，服务重启后对仍存活的
连接自动恢复密钥继续解析（有效期 24h），无需重登游戏重新协商。

两种典型部署：

| 方式 | 适用 | 关键参数 |
| --- | --- | --- |
| **局域网网关** | 软路由 / 旁路由 / 开热点的机器，手机流量必经它 | `-iface <网卡>` |
| **云端代理** | 有公网 IP 的 VPS，手机用 Clash 把游戏流量代理过来 | 再加 `-skip-self-ip=false -tls` |

```bash
# 云端模式的完整形态(代理是内嵌的 hysteria2,走 UDP)
sudo ./rocom-go -iface auto -hy2-addr :11443 -skip-self-ip=false \
  -hy2-allow <手机公网IP> -hy2-pass <强密码> -tls
```

> 云端模式**必须**设 `-hy2-allow` 白名单：密码挡得住「连进来」，挡不住扫描器把 UDP 端口打满
> 或拿它当跳板，耗尽 fd / goroutine 会连同进程的 Web 服务一起拖垮。
>
> 三点容易踩的：**①** 防火墙与云安全组放行的是 **UDP** 11443，不是 TCP；
> **②** 手机端须用支持 hysteria2 的客户端（Clash Meta / 小火箭 / sing-box），
> `deploy/rocom-clash.yaml` 有一份可直接导入的配置；**③** 证书复用 Web 那份自签证书，
> 客户端记得 `skip-cert-verify: true`。
>
> 换 hysteria2 而不是 SOCKS5 的理由见 [internal/hy2](internal/hy2/server.go) 的包注释：
> 公网上的 SOCKS5 明文认证与开放代理特征无法靠配置弥补。抓包侧则完全不变 ——
> 流量到本机后仍以本机 IP 出站，`-skip-self-ip=false` 照旧。

## 架构

```
afpacket/pcap → TCP 重组 → GCP 分帧 → 0x1002 取密钥 → 0x4013 AES-CBC 解密
  → opcode 路由 → PetData(protobuf) → 名称本地化 → SQLite → REST/SSE → React 前端
```

Go 后端 + React 前端，构建为**单个二进制**（前端经 `embed` 内嵌）；SQLite 单文件存全部数据。

| 目录 | 说明 |
| --- | --- |
| `internal/capture` | afpacket 实时抓包 / pcap 离线回放 + TCP 重组 |
| `internal/gcp` | GCP 分帧、密钥提取、AES 解密 |
| `internal/pb` `internal/pbdesc` | 由游戏描述符生成的消息结构、opcode → 消息名映射 |
| `internal/pet` `internal/scene` `internal/trial` | 领域解析：宠物 / 场景与实体 / 试炼 |
| `internal/gamedata` | id → 中文名查找表、图标与地图投影（生成物，内嵌） |
| `internal/pipeline` | 消息分发与落库 |
| `internal/store` | SQLite 存储与筛选查询 |
| `internal/server` | REST + SSE + 内嵌前端 |
| `internal/hy2` | 内嵌 hysteria2 代理（云端模式，UDP），支持运行期改配置而不打断抓包 |
| `internal/dialout` | 代理出站侧的共用能力：白名单、DNS 缓存、happy-eyeballs 拨号 |
| `web` | React + Vite 前端 |
| `scripts` | 解包与代码生成、抓包、契约校验脚本 |

## 开发

```bash
cd web && npm install
npm run dev         # Vite 开发服务器，已配 /api 与 /img 代理到后端 4939
npm run build       # 构建到 internal/server/web/（内嵌产物）
npm run lint && npm run check:css && npm run verify    # jsdom 真实渲染验收
cd .. && go vet ./... && go test ./...
```

两条最容易踩的：

1. **`npm run build` 必须在 `go build` 之前。** `//go:embed` 对残缺或过期的资源目录**不报错**，
   顺序反了会编出一个能正常启动、但打开白屏的二进制。
2. **`CGO_ENABLED=0` 编不出能抓包的二进制。** `gopacket/afpacket` 靠 cgo 实现，缺 gcc 时 Go 会
   静默降级，报错却是一个看似无关的 `undefined: pageSize`，极易误判成依赖版本问题。
   自检：`go list -f '{{.IgnoredGoFiles}}' github.com/google/gopacket/afpacket`。

生成物（`internal/pb/*.pb.go`、`names.json`、`internal/pbdesc/data/*`）请改生成脚本而非手改。
重构后跑 `python3 scripts/check_refactor.py` —— 它回答的是「有没有**未登记**的改动」，
比 `go build` 多查注释归属与净内容守恒。

## 文档

| | |
| --- | --- |
| [docs/deploy.md](docs/deploy.md) | 环境准备、构建、运行、`-skip-self-ip` 判据表、systemd / Docker |
| [docs/protocol.md](docs/protocol.md) | tsf4g / GCP 字节布局、分帧、密钥与解密、opcode |
| [docs/protocol-0x0102-login.md](docs/protocol-0x0102-login.md) | 登录报文逐字段拆解 |
| [docs/data.md](docs/data.md) | 解包数据源、生成链、各业务字段的实测依据 |
| [docs/architecture.md](docs/architecture.md) | 数据流、模块划分、HTTP 接口、前端 |
| [docs/api/](docs/api/README.md) | 对外 HTTP 契约（含机器可读的 `fields.json`） |
| [docs/unpack-2026-09.md](docs/unpack-2026-09.md) | 新加密方案下的解包步骤与诊断 |
| [docs/reference.md](docs/reference.md) | 相关工具与开源项目 |

版本号唯一真源是 [`VERSION`](VERSION)，格式 `赛季.大更新.小更新`。

## 免责声明

使用与再分发前，请确认你能接受以下全部几点：

1. **这可能违反游戏的服务条款。** 逆向私有协议、抓取游戏数据属于多数网游 ToS 中的禁止行为。
   由此导致的**账号封禁或其他后果由使用者自行承担**，作者不提供任何豁免或支持。
2. **本仓库内嵌了第三方素材与数据。** `internal/gamedata/data/` 下的图片与名称表由游戏客户端
   资源包解包生成，**版权归游戏方所有**；随仓库分发这些文件未必构成合法的再分发。
3. **手机流量会经过部署它的机器。** 云端 hysteria2 代理模式下确实如此 —— 请把这台风控视为你需要
   负责的网络出口，并务必配置白名单与认证。
4. 本项目按「现状」提供，**不含任何明示或暗示的保证**。

## 上游与致谢

- **早期版本来自 [@whoisnian](https://github.com/whoisnian)**：本仓库最早的 168 条提交
  （2026-06-27 ~ 2026-08-19）来自其早期版本，本项目在其之上继续开发；
  这些提交的作者信息按原样保留，未作改写。

## 许可

**代码与文档采用 [GNU GPL v3.0](LICENSE)。** 再分发（含衍生作品）须同样以 GPL-3.0 开源并提供源码。

**明确排除**：`internal/gamedata/data/` 下的图片、名称表与大地图瓦片由游戏客户端资源包解包生成，
**版权归游戏方所有**，不属本许可证的授权范围 —— 本仓库无权授权他人再分发它们，
随仓库分发这些文件本身也未必构成合法的再分发（另见下方免责声明第 2 点）。
