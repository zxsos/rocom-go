# 构建与部署

本文件是从 [README](../README.md) 拆出的运维细节。README 只留入门，这里是完整手册。

## 环境准备（首次搭建）

全新机器一次装齐 Go（含 cgo 依赖）、Node、Chromium。已装过可跳过。
以下命令在**仓库根目录**执行，Go 版本自动与 `go.mod` 对齐。

> 只有「更新游戏数据」那几条生成脚本需要 `uv`（见下文）；不重新生成时不用装。

### Debian / Ubuntu

```bash
# 1. Go —— 版本与 go.mod 对齐，别用发行版仓库里的旧版
GOVER=$(grep -m1 '^go ' go.mod | awk '{print $2}')
GOARCH=$(uname -m | sed -e s/x86_64/amd64/ -e s/aarch64/arm64/)
curl -fsSLO "https://dl.google.com/go/go${GOVER}.linux-${GOARCH}.tar.gz"
sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf "go${GOVER}.linux-${GOARCH}.tar.gz"

# 写入 PATH（用 zsh 就把 ~/.bashrc 换成 ~/.zshrc）
echo 'export PATH=$PATH:/usr/local/go/bin:$HOME/go/bin' >> ~/.bashrc
export PATH=$PATH:/usr/local/go/bin

# 2. cgo 依赖 —— 抓包必经，漏了会编出「不能抓包」的二进制（见下方 cgo 硬要求）
sudo apt-get install -y build-essential   # gcc + linux/if_packet.h(经 libc6-dev → linux-libc-dev)
go env -w CGO_ENABLED=1                   # 固化到 ~/.config/go/env，换机器要重设

# 3. Node —— 前端构建用，建议 20+（已有可跳过）
node -v && npm -v

# 4. 前端依赖 + Chromium（仅 `npm run verify:browser` 需要，不跑浏览器验收可省）
cd web
npm install
npx playwright install chromium              # 浏览器二进制
sudo npx playwright install-deps chromium    # 补系统库(libnss3 等)；脚本已内置 --no-sandbox，root 下可直接跑
cd ..
```

### 其它系统对照

| 步骤 | Arch | macOS |
| --- | --- | --- |
| Go | `sudo pacman -S go` | `brew install go`（或同上官方包） |
| cgo 依赖 | `sudo pacman -S base-devel linux-headers` | Xcode CLT：`xcode-select --install` |
| Node | `sudo pacman -S nodejs npm` | `brew install node` |
| Chromium 系统库 | 通常已齐 | 不需要（Homebrew 版自带依赖） |

### 自检

```bash
go version      # 期望与 go.mod 一致，如 go1.26.4

# 关键：afpacket 必须真的编进去。CGO 关掉时它被静默忽略，而 go build 报的却是
# 看似无关的 "undefined: pageSize"，极易误判成依赖版本问题。
go list -f '{{.IgnoredGoFiles}}' github.com/google/gopacket/afpacket \
  | grep -q afpacket.go && echo "❌ cgo 未启用" || echo "✅ afpacket 已编入"

go build ./...  # 无输出即通过
```

> **cgo 是硬要求，不能关。** 实时抓包用 `gopacket/afpacket`（mmap 的 AF_PACKET 原始套接字），
> 它靠 `import "C"` 实现。环境无 gcc 时 Go 会把 `CGO_ENABLED` **自动降为 0**，
> `afpacket.go` / `header.go` 静默移入 `IgnoredGoFiles` —— 于是 `pageSize` 未定义。
> **别照抄「Go 项目通用 Dockerfile」里的 `CGO_ENABLED=0`**：那样编出的二进制能启动、
> 能开 Web，但一抓包就失败。容器构建见 `Dockerfile` 头部注释。

## 构建

```bash
# 1.（可选）重新生成 proto / 名称表 / 图片。生成物已随仓库提交，
#    不更新游戏数据可跳过；要重新生成需先按「更新游戏数据」解包到 ~/Downloads/rocom/parsed。
uv sync
uv run python scripts/gen_proto.py        # all.pb → internal/pb
uv run python scripts/gen_gamedata.py     # Bin 配置 + all.pb → names.json（含图标索引）
uv run python scripts/gen_pbdesc.py       # all.pb + ProtoCMD.lua → internal/pbdesc（opcode→消息名）
uv run python scripts/gen_images.py       # 宠物头像/全身图 → img/{HeadIcon,BigHeadIcon256,Pet256}
uv run python scripts/gen_icons.py        # 属性/血脉/奖牌/POI 等 UI 图标 → img/{filter,blood,static,worldmap,medal,egg}
uv run python scripts/gen_skills.py       # 技能 id→名、形态→天生技能 → data/skills.json
uv run python scripts/gen_features.py     # 特性词典 + 宠物→特性索引 → data/features.json
uv run python scripts/gen_trial.py        # 试炼层结构与精灵池 → data/trial.json
uv run python scripts/gen_glass.py        # 炫彩色卡 → web/src/data/glassConf.js
uv run python scripts/gen_bigmap.py       # 大地图/分层切片 → img/bigmap{,/layer}
uv run python scripts/fetch_bigmap_hd.py  # （可选，联网）第三方高清瓦片；非解包数据，见 data.md 3.1

# 2. 构建前端到 embed 目录
cd web && npm install && npm run build && cd ..

# 3. 构建单二进制
go build -o rocom-go ./cmd/rocom-go
```

⚠️ **顺序不能反**：`//go:embed all:web` 对残缺或过期的资源目录**不报错**（2026-09-04
就因此炸过一次——二进制正常启动但页面白屏）。先 `npm run build`，再 `go build`，
并确认 `internal/server/web/index.html` 里的版本串是新的。

生成物（`internal/pb/*.pb.go`、`names.json`、`internal/pbdesc/data/*`）**改不动就别手改**，
要改改生成脚本。

### 前端开发

```bash
cd web
npm run dev      # Vite 开发服务器（5173），已配 /api、/img 代理到后端 4939
npm run build    # 构建到 internal/server/web/（vite emptyOutDir 会换掉 assets 文件名）
npm run lint     # ESLint（含 react-hooks 规则）
npm run check:css # 设计令牌引用校验（未使用变量 / 悬空 var()）
npm run verify   # jsdom 真实渲染：10 条路由 + SSE 分发 + 切账号（需后端在 4939）
npm run verify:live  # 需先备好 pcap：抓真实 SSE 推送喂给前端组件，验完整链路
```

`npm run verify` 覆盖路由渲染（含内容校验，防「渲染了个空壳」）、SSE 分发层语义
（类型过滤 / 账号过滤 / 断线补拉）、切账号（隔离 + 组件树未重建 + 请求数）。
 pcap 回放是一次性的（约 40ms 放完），直接连上去只能收到心跳，
所以 `verify:live` 借 `scripts/capture_sse.sh` **先挂 SSE 再启动回放**。

另有 16 个用 Chromium 的实测脚本（几何/层叠类断言，jsdom 测不了），
都在 `web/scripts/`，需先 `npx playwright install chromium`。

### 发布构建（amd64 + arm64）

抓包依赖 cgo，无法用 `CGO_ENABLED=0` 直接交叉编译。用 [zig](https://ziglang.org)
作交叉 C 编译器即可一键出两版**静态**二进制到 `dist/` —— zig 自带各架构 musl libc
与 Linux 头，**只需装 zig，无需 arm64 库 / sysroot**：

```bash
sudo pacman -S zig     # 以 Arch 为例
make release           # → dist/rocom-go-linux-{amd64,arm64}，均静态、已 strip
make clean
```

## 更新游戏数据

游戏更新后三步（详见 [data.md](data.md)）：

```bash
# 1. 从游戏目录原样复制 pak（Windows 客户端 <安装目录>\Win64\NRC\Content\Paks）
# 2. 解包到 ~/Downloads/rocom/parsed/（增量；需 dotnet SDK 与 CUE4Parse 克隆）
bash scripts/unpack.sh
# 3. 重跑上面「构建」第 1 步的生成脚本
```

⚠️ **2026-09 起**（安卓 1.111.0.112 及同期 PC）pak 加密换成「可配置加密」，必须用
LukeFZ/CUE4Parse fork，上游 FabianFG 解出来会「挂载成功但 0 个文件」。
先跑 `uv run python scripts/pakinfo.py <Paks>` 再决定要不要全量解包。
完整步骤与诊断见 [unpack-2026-09.md](unpack-2026-09.md)。

解包按虚拟路径**镜像导出**（顶层 `NRC/Content/...`）：`.uasset`/`.umap` → 属性 `.json`
（纹理另出 `.png`），其余（`.bytes`/`.non`/`.pb`/`.lua` 等）原样字节。生成脚本直接读
`parsed/`，解包根可用环境变量 **`ROCOM_PARSED`** 覆盖（默认 `~/Downloads/rocom/parsed`）；
**仓库只提交精炼后的生成物**（`internal/pb`、`names.json`、webp 图片），原始解包数据不进仓库。

## 运行

```bash
# 实时抓包（需 root；网卡必须是手机流量的必经之路）
sudo ./rocom-go -iface eth0

# 不确定网卡名就填 auto：读路由表自动选默认路由那张（容器里也算得出来）
sudo ./rocom-go -iface auto

# 离线回放已抓的 pcap
./rocom-go -pcap capture.pcap

# 启用 HTTPS（自签证书；手机经局域网访问时为满足「屏幕常亮」所需 secure context）
sudo ./rocom-go -iface auto -tls
```

浏览器打开 `http://localhost:4939`。

**云端 hysteria2 代理模式**（本机同时当代理与抓包机，适合有公网 IP 的 VPS）：

```bash
sudo ./rocom-go -iface eth0 -hy2-addr :11443 -skip-self-ip=false \
  -hy2-allow 1.2.3.4 -hy2-pass 换成强密码 -tls
# 把 1.2.3.4 换成手机当前公网出口 IP；手机 IP 变了要更新参数重启。
```

> ⚠️ **公网部署必须设 `-hy2-allow` 白名单。** hy2 的密码在 QUIC 隧道**内部**传输（不像
> RFC 1929 那样明文），但那只挡得住「连进来」，挡不住扫描器把 UDP 端口打满 —— 白名单才是
> 把攻击面缩到手机出口 IP 的那道防线。
>
> **防火墙 / 云安全组放行的是 UDP 11443，不是 TCP。** 手机端须用支持 hysteria2 的客户端
> （Clash Meta / 小火箭 / sing-box），可直接导入 `deploy/rocom-clash.yaml`；证书复用 Web 那份
> 自签证书，客户端记得 `skip-cert-verify: true`。
>
> **不用手拼：管理面板「高级设置 → 导入配置」会给一条订阅地址。** 配置里三样东西容易填错，
> 而它们分别散落在配置文件（密码）、运行中的监听地址（端口）与证书 SAN（要不要跳过校验）里，
> 所以一律由服务端算好：
>
> - 端口取**实际监听值**，在面板改完端口保存后地址不变、内容自动跟着变，不必重新导入；
> - 主机默认取你打开面板时地址栏那个（从公网域名打开就是域名），也可以就地改；
> - 主机能被证书验上（如经 `https://域名` 打开面板）就不跳过校验，按 IP 连时才跳；
> - 云端有 NAT 端口映射（对外端口 ≠ 监听端口）时，填「对外地址」为 `:对外端口` 或
>   `host:port`，它压过监听值。该值存 `ROCOM_HY2_ADVERTISE`，只用于生成配置，不参与监听。
>
> 地址含明文密码，服务端回 `no-store` —— 别截图、别贴群里。
>
> **为什么给的是「整份配置」而不是一条 `hysteria2://` 节点链接。** 抓包成立的前提是
> **游戏那一条 TCP 流量被送进代理**，而那取决于客户端的**分流规则**（`deploy/rocom-clash.yaml`
> 里那条 `DST-PORT,8195,抓包通道`）—— 规则塞不进一条 node 链接里。只导入 node 的典型结果是：
> 手机连上了、游戏也能正常玩、面板上却一条数据都没有。原因是游戏服务器是**国内 IP**，
> 客户端默认的分流（`GEOIP,CN,DIRECT` / `MATCH,DIRECT`）会把它放回直连，流量根本不经过
> 这台机器。这个故障的表象与「配置完全没生效」几乎一样，极难自查。故面板只提供订阅这一条 ——
> 被选错的那条所坑的正是这一点。（单节点链接的能力还在 `GET /api/admin/hy2/link` 的 `link`
> 字段里，排障时想单独验证「连得上吗」可以用它，但面板不展示，免得被当成主路径。）
>
> #### 订阅地址：两种令牌，两条入口
>
> | | 旧的地址令牌 | 设备令牌（推荐） |
> |---|---|---|
> | 形态 | `/sub/<由密码派生的一串>` | 短链 `/i/<8 位短码>` |
> | 吊销粒度 | 换密码 = **所有人**一起失效 | 单独吊销一枚，互不影响 |
> | 入口 | 只有一串长地址 | 短链 + 二维码 + 一键深链 + 引导页 |
> | 生成位置 | 面板「导入配置」卡片 | 面板「订阅设备」卡片 |
>
> 旧的地址留着是刻意的：早期已经导入的人还能拉，且「换密码即失效」在排障时
> 仍然好用。**新发的一律给设备令牌。**
>
> 同一个短码会按下面的优先级给出两种格式，用户只需传一条链接：
>
> | 客户端 | 格式 | 分流规则怎么生效 |
> |---|---|---|
> | Clash Meta / FlClash / Mihomo / Verge / Stash | Clash YAML | 采用配置里的 `rules` / `proxy-groups` |
> | 小火箭 Shadowrocket | `.conf` | 采用配置里的 `[Rule]` |
>
> 优先级自左向右、命中即停：**`?format=`** > **路径后缀 `/sub/{token}/{格式}`** >
> **`User-Agent`** > **`Accept`** > 默认 Clash YAML。`?format=` 与路径后缀都收客户端名
> 当别名（`mihomo` / `verge` / `flclash` / `stash` / `hiddify` → clash，
> `shadowrocket` → `sr`）。显式优先是刻意的：UA 既不可靠（Clash 系的 UA 常常是
> `okhttp` / `Dart` 之类没信息量的串）又会被改，而手动指定必须能压过一切猜测，
> 否则排障时复现不出「某某客户端到底拿到了什么」。
>
> #### 三个端点只需要两个端口
>
> 订阅、短链、引导页**全走 Web 那个端口**，不再额外占端口：
>
> ```
> 手机 /i/<短码>  ──TCP <Web 端口>──▶ rocom-go（下发配置 / 302 到引导页）
> 手机,游戏流量   ──UDP <hy2 端口>──▶ rocom-go（代理 + 整网卡抓包）
> ```
>
> 若 443 也可用，加一层 nginx 终结 TLS 是收益更高的事（见上文反代那段）；
> 不能用也完全够用 —— 客户端不要求订阅必须是 https，代价只有两条：订阅是**明文 http**，
> 以及按 IP 访问时配置里会带 `skip-cert-verify: true`（证书 SAN 里没有 IP）。
> 想去掉后者就填「对外地址」为一个解析到本机的域名（`ROCOM_HY2_ADVERTISE`）。
>
> #### 给朋友：二维码与一键深链
>
> 二维码里只装**短链**，不装整份配置：配置里带明文密码，二维码会被截图、转发；
> 何况整份配置的体积会让码密到扫不出来。
>
> 一键深链的写法都取自各家公开说明（`clash://install-config?url=<编码>` 见 Clash Verge Rev
> 官方文档；`hiddify://import/<订阅链接>` 见 Hiddify Wiki，且它明确支持订阅链接是 clash 格式；
> `shadowrocket://config/add/<url>` 见社区手册）。**小火箭那条尚未实测**，故引导页始终同时
> 保留「复制链接」这条人肉路径 —— scheme 在没装客户端时只会弹一个「无法打开网页」，
> 朋友看不懂，会以为链接坏了。
>
> #### 吊销的三层语义（别误会第一层）
>
> | 层 | 动作 | 效果 | 代价 |
> |---|---|---|---|
> | L1 | 吊销设备令牌 | 挡住**下次拉订阅**（HTTP 403） | 无 |
> | L2 | 换 hy2 密码 | 对方一断线就再也连不上 | **零中断**：`Manager.Start` 在监听地址不变时走 `SetParams` 换参数（`internal/hy2/manager.go`），已建立的连接不受影响 |
> | L3 | 重启 hy2 实例 | 立即踢掉**所有人** | 正在抓包的连接全断 |
>
> L1 挡不住「人家本地已经存着的那份配置里的密码」——这是最容易误解的一点。面板的吊销确认框
> 里写明了这一点，别点完就以为对方下线了。要真断，走 L2。

> **曾有第三路（sing-box 原生 JSON，给 Hiddify），2026-09-19 删除。** 值得记下原因：实测它对
> Hiddify 无效，而该客户端先后拿到过 Clash YAML 与 sing-box JSON，**两次都没抓到游戏流量**。
> 两种格式都失败，说明瓶颈在**客户端侧**（新导入的配置有没有被切换启用、它自带的路由有没有把
> 国内 IP 放回直连），而不是服务端发什么格式。继续维护一路没人验证过的格式，只会让人误以为
> 「换个格式能解决」，把排查方向带偏。真遇到非 sing-box 不可的客户端，先把这一条查清楚再加。
>
> 日志里能直接看出客户端**有没有来拉**、拿到的是哪种格式（见 endpoints.md 的 `/sub` 一行）——
> 「拉到了但规则没生效」与「根本没来拉」是两种问题，下游表现却一模一样。
>
> 两种格式都只把游戏端口送进代理，其余直连：这台机器是抓包机，不是日常出口。
>
> 复用归复用，**Web 走不走 HTTPS 仍然只看 `-tls`**：hy2 协议本身必须有证书（QUIC 上跑
> TLS 1.3，没有明文模式），所以启用它就得准备一份；但那只喂给 hy2，不会再连带把 Web
> 翻成 HTTPS（4.3.1 起；此前 `-hy2-addr` 一设，Web 就静默转 HTTPS，经 nginx 反代的部署
> 会被这手牵连得上游协议对不上）。经 nginx 终结 TLS 的话，后端这一跳保持明文反而省事。
>
> `-skip-self-ip=false` 在启用 hy2 时必须：代理进程以本机 IP 出站的游戏流量，
> 若按默认的去重逻辑会被**两个方向全部丢弃**——表现是「手机能正常玩、包数在涨，
> 却一条数据都解析不出来」，极难自查。启用 `-hy2-addr` 且你没显式指定时，
> 程序会自动改成 `false` 并在日志里说明。

**使用顺序**：先启动本工具，确保抓到 `0x1002 ACK` 里的会话密钥；再进游戏**打开宠物仓库**
触发宠物列表下发。密钥随连接落库缓存，抓包服务异常重启后会对仍在线的连接自动恢复密钥
继续解析（有效期 24h），无需重登游戏重新协商。

**屏幕常亮 / HTTPS**：捕获事件页有「屏幕常亮」开关（防手机熄屏，方便盯高亮提醒），但浏览器
只在 secure context（HTTPS 或 localhost）下提供该能力。手机经 `http://内网IP` 访问时开关会
禁用，需加 `-tls`：证书首次自动生成（`-cert`/`-key` 指定路径），SAN 覆盖 localhost 与本机
所有 IP，且会持久化——信任一次后重启仍复用。**网关 IP 变动后删掉证书文件让它重新生成**。

## 部署（数据持久化 / 更新不丢历史）

直接命令行运行时数据库默认落工作目录（`rocom.db`），和二进制放一起——删程序目录就连带
删掉积累的宠物 / 事件 / 涂地历史。生产部署应**把数据放到程序目录之外**并用 systemd 托管。

`scripts/deploy.sh` 自动完成「程序装 `/opt/rocom/`、数据放 `/var/lib/rocom/`、systemd 托管」：

```bash
# 服务器上已装 go 时的标准流程（首次和更新都用这一条）：git pull + go build + 部署，数据不动
sudo ./scripts/deploy.sh --build

sudo vim /etc/rocom.env            # 首次跑完后填 ROCOM_HY2_ADDR / PASS / ALLOW 等
sudo systemctl restart rocom-go && sudo systemctl status rocom-go
journalctl -u rocom-go -f          # 看日志

sudo ./scripts/deploy.sh --migrate /root/roco   # 从手动部署迁移：停旧进程、搬库与证书、由旧启动参数生成 env
sudo ./scripts/deploy.sh --backup               # 热备数据库，不锁库
sudo ./scripts/deploy.sh --archive rocom-go.tar # 从 tar 包装（而非本地 dist/）
sudo ./scripts/deploy.sh --uninstall            # 卸载（默认保留数据，加 --purge 才连数据删）
```

> **从旧名 `rocom` 升级过来？** 单元名已随项目改名变成 `rocom-go.service`。跑一次
> `deploy.sh` 会**自动停用并删除旧单元**（否则两个单元抢同一个 Web 端口），而
> `/etc/rocom.env`、`/opt/rocom`、`/var/lib/rocom/rocom.db` **一律不动**。

`/etc/rocom.env` 里多数项可在管理面板（`#/admin`，隐式入口）直接改，立即生效并写回该文件；
只有**抓包网卡、游戏端口、HTTPS** 属启动项，改它们仍需 `systemctl restart rocom-go`。
**Web 监听地址**也能在线改，但它正是你用来改它的那条连接的另一端，故走「试运行 → 从新地址
确认」，90 秒内不确认自动回滚（见 [api/](api/README.md)）。

更新只替换 `/opt/rocom/rocom-go` 并重启，数据库不受影响。重启后自动从 `sessions` 表预热
会话密钥、连接归属、场景定位（24h 内），对仍存活的游戏连接**从中段继续解密**，历史统计原样保留。
仅当 schema 变化（新版加了字段/表）才需删库重建——`CREATE TABLE IF NOT EXISTS` 是幂等的。

> ⚠️ `deploy.sh` 的 `write_env` **不覆盖已存在的 `/etc/rocom.env`**。所以删掉某个启动
> flag 后（历史上 `-egg-api-key` 就踩过），旧 env 里那行会继续把它喂给新版本，而 Go 的
> flag 包遇到未定义参数**直接退出**，配合 `Restart=on-failure` 就是崩溃循环。
> 删 flag 必须同步手工清理 env 文件。

### Docker 部署

不想在宿主机装 Go / 配 systemd 时用容器。`Dockerfile` 多阶段：builder 装 gcc 与内核头编出
二进制，运行镜像只留 alpine + 证书（**约 106 MB**）。

```bash
docker pull docker.cnb.cool/bangbang222/roco:latest   # 预构建，amd64，免登录
docker build -t rocom-go .                             # 其它架构（ARM 等）从源码构建
```

先选一种方式：**A. 局域网网关**（家里软路由/旁路由，手机流量必经它）用 `-iface <网卡>`；
**B. 云端 hy2 代理**（有公网 IP 的 VPS，手机用 Clash 把游戏流量代理过去）在 A 之上多加
`-skip-self-ip=false`（通常还要 `-tls`）。

```bash
# A 局域网网关
docker run -d --name rocom-go --restart unless-stopped \
  --cap-add=NET_ADMIN --cap-add=NET_RAW --network host \
  -e TZ=Asia/Shanghai -v rocom-data:/data \
  docker.cnb.cool/bangbang222/roco:latest -iface auto

# B 云端 hy2 代理（放行 UDP 11443；bridge 模式需 -p 11443:11443/udp,host 模式不必）
docker run -d --name rocom-go --restart unless-stopped \
  --cap-add=NET_ADMIN --cap-add=NET_RAW --network host \
  -e TZ=Asia/Shanghai -v rocom-data:/data \
  docker.cnb.cool/bangbang222/roco:latest -iface auto -skip-self-ip=false -tls
```

`-iface auto` 读默认路由选网卡，`--network host` 下容器与宿主机共享路由表所以算得出来。
仍想手填：VPS 网卡很少叫 `eth0`（常见 `ens17` / `ens33` / `enp1s0`），用
`ip route get 8.8.8.8` 看 `dev` 后面那个最准。填错时日志会**列出候选网卡与默认路由**，
不再只报一句 `no such device`。其余参数（`-db`/`-cert`/`-addr`/`-hy2-addr` 等）都有默认值，
或启动后在管理面板改。

更新镜像（数据在卷里，不丢历史）：

```bash
docker pull <镜像> && docker rm -f rocom-go && 用同样的 run 命令重新起
docker run --rm -v rocom-data:/data <镜像> sh -c 'sqlite3 /data/rocom.db ".backup /data/rocom-backup.db"'
```

> 抓包报 `operation not permitted` 时，把 `--cap-add` 换成 `--privileged`。仍报错多半是
> **嵌套容器**（在容器里再跑 Docker），宿主的内核能力传递受限，这种场景建议直接走上面的
> systemd 部署。判断方法：`grep CapBnd /proc/self/status` —— capability 不能超过 bounding set。

也可以用 `docker-compose.yml`（已写好 capability、host 网络与数据卷），先把里面 command 的
网卡名改对：`docker compose up -d`。日常运维：`docker logs -f rocom-go` / `docker restart rocom-go`。

### `-skip-self-ip` 到底该设什么

判据是：**游戏流量经过网卡时，包的源或目的 IP 会不会是本机 IP？**
（实现上 `src` 或 `dst` 任一命中即整包丢弃，见 `internal/capture/capture.go`）

| 部署方式 | 该设 | 理由 |
| --- | --- | --- |
| 单臂网关（一张网卡做 SNAT 转发） | `true`（默认） | 去 SNAT 重复副本：同一条流会在同一网卡上出现两次（NAT 前 + 源改成本机 IP 的副本），不去重会被解析两次 |
| 旁路镜像 / SPAN 端口 | `true` | 只收镜像流量，本机不参与转发 |
| 透明网桥 | `true` | 转发但不改源 IP |
| **云端 hy2 代理** | **`false`** | 代理进程以本机 IP 为源出站，回包目的也是本机 → 设 `true` 会**两个方向全丢**，一个包都抓不到 |
| 本机跑安卓模拟器 | `false` | 模拟器流量源 IP 就是本机 |

**多数情况不用自己算**：启用 `-hy2-addr` 且没有显式传 `-skip-self-ip` 时，程序会自动改用
`false` 并打日志说明（Docker 下改这项要重启容器，自动判定最省事）。

设错的后果都不好排查：该 `false` 却用 `true` 时，手机代理连得上、游戏能玩，但**一条包都解析
不出来**，日志也不报错，只是「抓包统计」的包数在涨却始终没有宠物数据。现在这段有自动兜底：
启动横幅会打出实际生效值，运行期还有一道自检会抓出来（见下）。

⚠️ 它是 `RunLive(iface, skipSelf)` 的参数，**引擎启动时一次性生效**，面板改不了、热更也不生效，
必须 `docker restart rocom-go` / `systemctl restart rocom-go`。

### 验证：启动横幅与两条自动检查

```bash
docker ps --filter name=rocom --format '{{.Status}}'   # 期望 Up，不是 Restarting
docker logs rocom-go 2>&1 | tail -12                  # 期望看到下面这段「抓包配置」
```

```
==================== 抓包配置 ====================
网卡      ens17 (172.16.0.29)  自动选中(默认路由)
模式      hysteria2 代理(监听 UDP :11443)   -skip-self-ip=false
端口      游戏 8195    Web https://<本机IP>:4939
数据库    /data/rocom.db
=================================================
```

另有两条**自动检查**，不必等出问题才发现：

- **网桥检测**：若自动选中的网卡落在 Docker 默认网桥网段（172.17.x），直接提示漏了
  `--network host` —— 否则表现是「容器起来了、包数却是 0」。
- **`-skip-self-ip` 自检**：每 30 秒采样一次，若「包在进来、却全被本机 IP 丢掉、且一条游戏
  消息都没解析出来」就警告 —— 这正是设错该项的指纹。

然后**先在浏览器打开面板设管理员密码**（首次进入引导设置，≥4 位）：
`https://<服务器IP>:4939/#/admin`，自签证书点「高级 → 继续前往」。

> ⚠️ **公网部署记得放通端口。** 若网卡上是内网 IP（如 `172.16.x.x`）而公网 IP 是云厂商的
> 弹性 IP / NAT 映射，那么**系统内防火墙只是第二道**，主要关卡在**云控制台安全组** —— 需要在
> 那里放行 Web(TCP 4939)与 hy2(**UDP** 11443)两个端口。

### 配置文件与热更范围

容器启动时从配置文件读参数，**与 systemd 部署用同一套键**（完整列表见 `scripts/deploy.sh`
头部注释），故两种部署方式的配置可互换。默认路径 `/data/rocom.env`（挂卷，重建容器不丢；
systemd 版是 `/etc/rocom.env`），可用 `-e ROCOM_ENV_FILE=/某路径` 改。

优先级：**命令行显式给的 > 配置文件 > 内置默认值**。命令行必须能压过配置，否则一旦往配置里
写过值，命令行参数就再也覆盖不了。

首次启动会自动创建该文件（带注释模板），管理面板随即**可写**——不建文件的话面板会显示
「配置文件不可写」而降级只读，改了也存不下。

改完的生效方式：

| 项 | 生效 |
| --- | --- |
| hy2 地址 / 白名单 / 屏蔽域名 / 密码 / 连接数上限 | ✅ **立即**（改端口才热重启，抓包不中断；**带宽是启动项**，改它要 restart） |
| SMTP 邮箱 | ✅ 立即 |
| Web 监听地址 | ✅ 面板内「试运行 → 确认」，不用 restart |
| **抓包网卡 / 游戏端口 / HTTPS / `-skip-self-ip`** | ❌ **必须重启进程** |

### 注意事项

- **网络用 host**：桥接模式下容器看不见宿主机物理网卡，`-iface` 会报网卡不存在。host 模式的
  代价是端口直接占宿主机，`-p` 不生效（端口由 `-addr` 定）。
- **数据持久化**：数据库在卷的 `/data/rocom.db`，重建容器 / 更新镜像都不丢历史；自签证书也生成
  在这里，信任一次后复用。**公网 IP 变了要删证书重生成**：
  `docker exec rocom-go rm -f /data/rocom-cert.pem /data/rocom-key.pem && docker restart rocom-go`
- **cgo 不能关**：builder 阶段除 gcc 外还要 `linux-headers`（提供 `linux/if_packet.h`），
  少装会编译失败。
- **离线回放**不需要 capability 与 host 网络，挂 pcap 即可：
  ```bash
  docker run -d --name rocom-go-replay -p 4939:4939 \
    -v /path/to/pcap:/pcap:ro -v rocom-data:/data \
    docker.cnb.cool/bangbang222/roco:latest -pcap /pcap/xxx.pcap
  ```

### 实测状态（诚实版）

已实测：构建、离线回放（743 只宠物解析）、Web API、数据落卷、管理面板配置闭环
（面板改配置 → 落盘 → 重启容器后生效）均正常。

**实时抓包收包未在 CI 环境验证**（该环境为嵌套容器且无 `CAP_NET_RAW`），二进制中 afpacket
正常编译、可启动到创建 socket 那一步。
