## 架构

```
afpacket/pcap → TCP 重组 → GCP 分帧 → 0x1002 取密钥 → 0x4013 AES-CBC 解密
  → opcode 路由 → PetData(protobuf) 解析 → 名称本地化 → SQLite → REST/SSE → React 前端
```

| 目录 | 说明 |
| --- | --- |
| `internal/gcp` | GCP 分帧、密钥提取、AES 解密 |
| `internal/capture` | afpacket 实时抓包 / pcap 离线回放 + TCP 重组 |
| `internal/pb` | 由游戏描述符 all.pb 生成的宠物消息结构(`scripts/gen_proto.py`) |
| `internal/pet` | PetData 解析与业务模型 |
| `internal/scene` | 移动/场景/实体消息解析(实时位置、分层、野生宠物、捕捉结果;详见 docs/data.md 3.1/3.2/3.5) |
| `internal/gamedata` | id→中文名 查找表 + 场景/大地图投影(`scripts/gen_gamedata.py` 生成，embed) |
| `internal/store` | SQLite 存储与筛选查询 |
| `internal/server` | REST API + SSE 推送 + embed 前端 |
| `web` | React + Vite 前端 |
| `scripts/capture.sh` | tcpdump 全量抓包脚本 |

## 文档

- [协议说明](docs/protocol.md) — tsf4g/GCP 字节布局、分帧、密钥与解密、opcode
- [数据来源与解析](docs/data.md) — 解包数据源(all.pb + Bin 配置)、proto 与名称表生成、宠物字段映射
- [服务架构](docs/architecture.md) — 数据流、模块、HTTP 接口、前端、部署
- [参考资料](docs/reference.md) — 相关工具与开源项目

## 环境准备(首次搭建)

全新机器上一次装齐:Go(含 cgo 依赖)、Node、Chromium。**已装过可跳过,直接看「构建」。**
以下命令在**仓库根目录**执行,Go 版本自动与 `go.mod` 对齐。

> 另需 `uv` 的只有「更新游戏数据」那几条生成脚本(见「构建」第 1 步);
> 不更新游戏数据时用不到,装法见 https://docs.astral.sh/uv。

### Debian / Ubuntu

```bash
# 1. Go —— 版本与 go.mod 对齐,别用发行版仓库里的旧版
GOVER=$(grep -m1 '^go ' go.mod | awk '{print $2}')
GOARCH=$(uname -m | sed -e s/x86_64/amd64/ -e s/aarch64/arm64/)
curl -fsSLO "https://dl.google.com/go/go${GOVER}.linux-${GOARCH}.tar.gz"
sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf "go${GOVER}.linux-${GOARCH}.tar.gz"

# 写入 PATH(用 zsh 就把 ~/.bashrc 换成 ~/.zshrc)
echo 'export PATH=$PATH:/usr/local/go/bin:$HOME/go/bin' >> ~/.bashrc
export PATH=$PATH:/usr/local/go/bin

# 2. cgo 依赖 —— 抓包必经,漏了会编出不能抓包的二进制(见下方"cgo 是硬要求")
sudo apt-get install -y build-essential   # gcc + linux/if_packet.h(经 libc6-dev → linux-libc-dev)
go env -w CGO_ENABLED=1                   # 固化到 ~/.config/go/env,换机器要重设

# 3. Node —— 前端构建用,建议 20+(已有可跳过)
node -v && npm -v

# 4. 前端依赖 + Chromium(仅 `npm run verify:browser` 需要,不跑浏览器验收可省)
cd web
npm install
npx playwright install chromium             # 浏览器二进制
sudo npx playwright install-deps chromium    # 补系统库(libnss3 等);脚本已内置 --no-sandbox,root 下可直接跑
cd ..
```

### 其它系统对照

| 步骤 | Arch | macOS |
| --- | --- | --- |
| Go | `sudo pacman -S go` | `brew install go`(或同上用官方包) |
| cgo 依赖 | `sudo pacman -S base-devel linux-headers` | Xcode CLT:`xcode-select --install` |
| Node | `sudo pacman -S nodejs npm` | `brew install node` |
| Chromium 系统库 | 通常已齐 | 不需要(Homebrew 版自带依赖) |

### 自检

```bash
go version      # 期望与 go.mod 一致,如 go1.26.4

# 关键:afpacket 必须真的编进去。CGO 关掉时它被静默忽略,而 go build 报的却是
# 看似无关的 "undefined: pageSize",极易误判成依赖版本问题。
go list -f '{{.IgnoredGoFiles}}' github.com/google/gopacket/afpacket \
  | grep -q afpacket.go && echo "❌ cgo 未启用" || echo "✅ afpacket 已编入"

go build ./...  # 无输出即通过
```

> **cgo 是硬要求,不能关。** 实时抓包用 `gopacket/afpacket`(mmap 的 AF_PACKET 原始套接字),
> 它靠 `import "C"` 实现。环境无 gcc 时 Go 会把 `CGO_ENABLED` 自动降为 0,
> `afpacket.go` / `header.go` 被静默移入 `IgnoredGoFiles` —— 于是 `pageSize` 未定义。
> 别照抄「Go 项目通用 Dockerfile」里的 `CGO_ENABLED=0`:那样编出的二进制能启动、
> 能开 Web,但一抓包就失败。容器构建见 `Dockerfile` 头部注释。

## 构建

```bash
# 1. (可选)重新生成 proto / 名称表 / 图片,见「更新游戏数据」与 docs/data.md
#    生成物(internal/pb、names.json、img webp)已随仓库提交,不更新游戏数据可跳过;
#    重新生成需先按「更新游戏数据」解包到 ~/Downloads/rocom/parsed;脚本依赖经 uv 管理
uv sync
uv run python scripts/gen_proto.py     # all.pb → internal/pb
uv run python scripts/gen_gamedata.py  # Bin 配置 + all.pb → names.json(含图标索引)
uv run python scripts/gen_images.py    # 宠物头像/全身图 → img/{HeadIcon,BigHeadIcon256,Pet256} webp
uv run python scripts/gen_icons.py     # 属性/血脉/奖牌/POI 等 UI 图标 → img/{filter,blood,static,worldmap,medal} webp
uv run python scripts/gen_bigmap.py    # 大地图/分层切片 → img/bigmap{,/layer} webp(实时地图页)
uv run python scripts/fetch_bigmap_hd.py  # (可选,需联网)第三方高清底图瓦片 → img/bigmap/<res>_hd/
                                          #  供地图页「高清」开关按视口分块加载;非解包数据,见 docs/data.md 3.1

# 2. 构建前端到 embed 目录
cd web && npm install && npm run build && cd ..

# 3. 构建单二进制
go build -o rocom-go ./cmd/rocom-go
```

### 前端开发

前端在 `web/`,`npm run dev` 起 Vite 开发服务器(默认 5173),已配好 `/api`、`/img` 代理到后端
4939,故改前端不用每次 build。

```bash
cd web
npm run dev      # 开发服务器(需后端另起在 4939)
npm run build    # 构建到 internal/server/web/(embed 产物,vite emptyOutDir 会换掉 assets 文件名)
npm run lint     # ESLint(含 react-hooks 规则,查依赖缺失等)
npm run verify   # jsdom 真实渲染验收:10 条路由 + SSE 分发 + 切账号(需后端在 4939)
```

`npm run verify` 覆盖:路由渲染(含内容校验,防「渲染了空壳」)、SSE 分发层语义
(类型过滤/账号过滤/断线补拉)、切账号(账号隔离 + 组件树未重建 + 请求数)。
脚本在 `web/scripts/`,改完前端建议跑一次。

要验「后端真推 → 前端真消费」的完整链路,用:

```bash
cd web
npm run verify:live   # 需先按提示备好 pcap;会抓真实 SSE 事件喂给前端组件
```

它借 `scripts/capture_sse.sh`(先挂 SSE 再启动 pcap 回放)落盘真实推送,再喂给前端。
注意 pcap 回放是一次性的(约 40ms 放完),直接连上去只能收到心跳,必须先挂后放。

### 发布构建(amd64 + arm64)

抓包依赖 `gopacket/afpacket`(cgo),无法用 `CGO_ENABLED=0` 直接交叉编译。用 [zig](https://ziglang.org)
作交叉 C 编译器即可一键出两版**静态**二进制到 `dist/`——zig 自带各架构 musl libc 与 Linux 头,
**只需装 zig,无需 arm64 库/sysroot**:

```bash
# 装 zig (以本机 Arch Linux 为例)
sudo pacman -S zig

make release   # → dist/rocom-go-linux-amd64、dist/rocom-go-linux-arm64(均静态、已 strip)
make clean     # 清理 dist/
```

## 更新游戏数据

游戏更新后三步(详见 [docs/data.md](docs/data.md)):

```bash
# 1. 从游戏目录原样复制 pak(Windows 客户端 <安装目录>\Win64\NRC\Content\Paks)
cp -r <游戏Paks目录>/* ~/Downloads/rocom/Paks/

# 2. 解包到 ~/Downloads/rocom/parsed/(增量,产物不比来源 pak 旧才跳过;需 dotnet SDK 与 CUE4Parse 克隆;
#    默认排除三维美术/视频/音频等与数据链无关的大目录,--no-exclude 可真·全量;
#    导出后自动 .bytes→JSON、luac→lua 反编译(需 unluac,--no-post 跳过))
./scripts/unpack.sh

# 3. 重跑「构建」步骤 1 的生成脚本
```

解包按虚拟路径镜像导出:`.uasset`/`.umap` → 属性 `.json`(纹理另出 `.png`),其余
(`.bytes`/`.non`/`.pb`/`.lua` 等)原样字节。生成脚本直接读 `parsed/`(解包根可用环境变量
`ROCOM_PARSED` 覆盖),仓库只提交精炼后的生成物(`internal/pb`、`names.json`、webp 图片)。

## 运行

```bash
# 实时抓包(需 root；网卡需为手机流量的必经之路)
sudo ./rocom-go -iface <网卡> -port 8195 -addr :4939

# 离线回放已抓的 pcap
./rocom-go -pcap ./pcap/xxx.pcap -addr :4939

# 启用 HTTPS(自签证书;手机经局域网访问时用)
sudo ./rocom-go -iface <网卡> -tls

# 云端 socks5 抓包:本机同时当 socks5 网关 + 抓包机(带公网 IP 的服务器)
#   -socks5-addr :1080   内置 SOCKS5 代理(仅 TCP CONNECT、无认证),手机 clash mate 连「公网IP:1080」
#   -skip-self-ip=false  不忽略本机 IP——否则代理进程以本机 IP 出站的游戏流量会被单臂去重逻辑丢弃
#   -socks5-allow <IP>   客户端 IP 白名单(逗号分隔,支持 CIDR)。公网部署必填:全网扫描器几
#                        分钟内就会找上无认证代理并滥用(日志里出现一堆陌生 IP 即是被扫),不设
#                        白名单会耗尽 fd/goroutine,把同进程的 Web 服务也拖垮。
#   -socks5-max-conns 64 同时处理的最大连接数,超限直接拒绝(默认 64),防连接风暴。
#   -socks5-user/-socks5-pass  RFC 1929 用户名/密码认证。Clash 里在 socks5 代理上填同款
#                              username/password 即可。注意该认证密码是明文传输的(无加密
#                              通道),公网直连建议白名单+认证双保险,或走 tailscale 加密隧道。
sudo ./rocom-go -iface eth0 -socks5-addr :1080 -skip-self-ip=false \
  -socks5-allow 1.2.3.4 -socks5-user rocom -socks5-pass 换成强密码 -tls
# ↑ 把 1.2.3.4 换成手机当前公网出口 IP;手机 IP 变了就更新参数重启。

浏览器打开 `http://localhost:4939`。

> **屏幕常亮 / HTTPS**:捕获事件页有「屏幕常亮」开关(阻止手机熄屏,方便盯着高亮提醒),
> 但浏览器仅在 secure context(HTTPS 或 localhost)下提供该能力。手机经 `http://内网IP`
> 访问时开关会禁用,需加 `-tls`:首次不存在证书时自动生成自签证书(`-cert`/`-key` 指定路径,
> 默认 `rocom-cert.pem`/`rocom-key.pem`),SAN 覆盖 localhost 与本机所有 IP。手机打开
> `https://<内网IP>:4939` 点过安全警告后即为 secure context,开关可用。证书会持久化,
> 信任一次后重启服务仍复用;**网关 IP 变动后删除证书文件让其重新生成**即可。

> 进入游戏前先启动本工具，确保抓到 `0x1002 ACK` 中的会话密钥；
> 然后在游戏中打开宠物仓库以触发宠物列表下发。
> 密钥会随连接落库缓存,抓包服务异常重启后可对仍在线的连接自动恢复密钥继续解析(有效期 24h),
> 无需重登游戏重新协商。

## 部署(数据持久化 / 更新不丢历史)

直接命令行运行时,数据库默认落在工作目录(`rocom.db`),和二进制放一起——删程序目录就会连带删掉
积累的宠物/事件/涂地等历史数据。生产部署应**把数据放到程序目录之外**,用 systemd 管理进程:

`scripts/deploy.sh` 自动完成「程序装 `/opt/rocom/`、数据放 `/var/lib/rocom/`、systemd 托管」:

```bash
# 服务器上已装 go 时的标准流程(首次和更新都用这一条):
#    git pull + go build + 部署,数据不动
sudo ./scripts/deploy.sh --build

# 首次跑完后编辑配置填入 socks5 等参数,然后启动
sudo vim /etc/rocom.env          # 填 ROCOM_SOCKS5_ADDR / USER / PASS 等
sudo systemctl restart rocom-go
sudo systemctl status rocom-go
journalctl -u rocom-go -f           # 看日志

# 从手动部署迁移(已有旧库在跑,想切到 systemd 管理)
#    自动停旧进程、搬库与证书到 /var/lib/rocom、从旧启动参数生成 env
sudo ./scripts/deploy.sh --migrate /root/roco

# 备份数据库(热备,不锁库)
sudo ./scripts/deploy.sh --backup

# 从 tar 包安装(而非本地 dist/)
sudo ./scripts/deploy.sh --archive rocom-go.tar

# 卸载(默认保留数据,加 --purge 才连数据一起删)
sudo ./scripts/deploy.sh --uninstall
```

> **从旧名 `rocom` 升级过来?** 项目已改名为 `rocom-go`,systemd 单元名随之由 `rocom.service`
> 变成 `rocom-go.service`。跑一次 `scripts/deploy.sh` 会**自动停用并删除旧单元**(否则两个单元会
> 抢同一个 Web 端口),而配置 `/etc/rocom.env`、程序目录 `/opt/rocom`、数据库
> `/var/lib/rocom/rocom.db` **一律不动** —— 不需要搬数据,只是之后 `systemctl` 认的名字变了。

`/etc/rocom.env` 里的多数项可在管理面板(`#/admin`,隐式入口)直接改,改动立即生效并写回该文件;
只有抓包网卡、游戏端口、HTTPS 属启动项,改它们仍需 `systemctl restart rocom-go`。
**Web 监听地址**也可以在线改,但它正是你用来改它的那条连接的另一端,故不直接生效:先在新端口
试运行,从新地址打开过面板后才落盘;90 秒内不确认会自动回滚(详见 [docs/api/](docs/api/README.md))。

更新流程只替换 `/opt/rocom/rocom-go` 并 `systemctl restart`,数据库 `/var/lib/rocom/rocom.db`
不受影响。重启后自动从 `sessions` 表预热会话密钥、连接归属、场景定位(有效期 24h),对仍存活的
游戏连接从中段继续解密,历史统计原样保留。仅当库 schema 变化(新版加了字段/表)时才需删库重建——
`CREATE TABLE IF NOT EXISTS` 是幂等的,schema 没变就直接打开旧库即可。

### Docker 部署

不想在宿主机装 Go / 配 systemd 时,用容器跑。`Dockerfile` 是多阶段构建:builder
装 gcc 与内核头编出二进制,运行镜像只留 alpine + 证书(**约 106 MB**)。

预构建镜像(**amd64/x86_64**,免登录):

```bash
docker pull docker.cnb.cool/test00123/roco:latest
```

其它架构(ARM 等)从源码本地构建:`docker build -t rocom-go .`

#### 先选一种方式

| 方式 | 适用 | 关键参数 |
| --- | --- | --- |
| **A. 局域网网关** | 家里软路由 / 旁路由 / 开热点的机器,手机流量必经它 | `-iface <网卡>` |
| **B. 云端 socks5** | 有公网 IP 的 VPS,手机装 Clash 把游戏流量代理过去 | 多加 `-skip-self-ip=false` |

#### 第 1 步:网卡名(现在可以不查了)

填 `-iface auto` 即可 —— 程序读路由表自动选**默认路由**所在的那张网卡,
`--network host` 下容器与宿主机共享路由表,故这一步在容器里同样算得出来。

仍想手填的话:VPS 的网卡很少叫 `eth0`(常见 `ens17` / `ens33` / `enp1s0`),用默认路由反查最准:

```bash
ip route get 8.8.8.8
# 8.8.8.8 via 10.0.0.1 dev ens17 src 172.16.0.29
#                      ^^^^^ 这个就是该抓的网卡
```

填错时会把**候选网卡与默认路由列在日志里**(`docker logs` 可见),不会再只报一句 `no such device`。

#### 第 2 步:启动

**场景 A —— 局域网网关:**

```bash
docker run -d --name rocom-go --restart unless-stopped \
  --cap-add=NET_ADMIN --cap-add=NET_RAW \
  --network host \
  -e TZ=Asia/Shanghai \
  -v rocom-data:/data \
  docker.cnb.cool/test00123/roco:latest \
  -iface ens17
```

**场景 B —— 云端 socks5**(比 A 多 `-skip-self-ip=false` 与 `-tls`):

```bash
docker run -d --name rocom-go --restart unless-stopped \
  --cap-add=NET_ADMIN --cap-add=NET_RAW \
  --network host \
  -e TZ=Asia/Shanghai \
  -v rocom-data:/data \
  docker.cnb.cool/test00123/roco:latest \
  -iface ens17 -skip-self-ip=false -tls
```

其余参数(`-db` / `-cert` / `-addr` / socks5)都有默认值,**或启动后在管理面板改**,
故命令行可以这么短。想要 HTTPS 就加 `-tls`(手机「屏幕常亮」需要 secure context)。

也可以用 `docker-compose.yml`(已写好 capability、host 网络与数据卷),
先把里面 command 的网卡名改对:`docker compose up -d`。

#### 第 3 步:验证

```bash
docker ps --filter name=rocom --format '{{.Status}}'   # 期望 Up,不是 Restarting
docker logs rocom-go 2>&1 | tail -12                      # 期望看到下面这段「抓包配置」
```

开始抓包前会打一段固定格式的摘要,`docker logs` 里一眼核对:

```
==================== 抓包配置 ====================
网卡      ens17 (172.16.0.29)  自动选中(默认路由)
模式      socks5 代理(监听 :1080)   -skip-self-ip=false
端口      游戏 8195    Web https://<本机IP>:4939
数据库    /data/rocom.db
=================================================
```

另有两条**自动检查**,不必等出问题才发现:

- **网桥检测**:若自动选中的网卡落在 Docker 默认网桥网段(172.17.x),直接提示漏了
  `--network host` —— 否则表现是「容器起来了、包数却是 0」。
- **`-skip-self-ip` 自检**:每 30 秒采样一次,若「包在进来、却全被本机 IP 丢掉、且一条游戏
  消息都没解析出来」就警告 —— 这正是设错 `-skip-self-ip` 的指纹。

**然后先在浏览器打开面板设管理员密码**(首次进入引导设置,≥4 位):

```
https://<服务器IP>:4939/#/admin
```

自签证书 → 点「高级 → 继续前往」。socks5 的账号、白名单、图鉴令牌等都在面板里
改(见下「配置文件与管理面板」)。

> **公网部署记得放通端口。** 若网卡上是内网 IP(如 `172.16.x.x`)而公网 IP 是云厂商
> 的弹性 IP / NAT 映射,则**系统内防火墙只是第二道**,主要关卡在**云控制台安全组** ——
> 需要在那里放行 4939(Web)与 10801(socks5)。

#### `-skip-self-ip` 什么时候用 false

> **多数情况不用自己算了**:启用 `-socks5-addr` 且**没有显式传** `-skip-self-ip` 时,
> 程序会自动改用 `false` 并打日志说明 —— Docker 下改这项要重启容器,自动判定最省事。
> 另有一道运行期自检兜底(见「第 3 步:验证」里的自动检查)。
> 下表留给「显式传了 true 想确认对不对」以及其它混合场景。

判据是:**游戏流量经过网卡时,包的源或目的 IP 会不会是本机 IP?**
(实现上 `src` 或 `dst` 任一命中即整包丢弃,见 `internal/capture/capture.go`)

| 部署方式 | 该设 | 理由 |
| --- | --- | --- |
| 单臂网关(一张网卡做 SNAT 转发) | `true`(默认) | 去 SNAT 重复副本:同一条流会在同一网卡上出现两次(NAT 前 + 源改成本机 IP 的副本),不去重会被解析两次 |
| 旁路镜像 / SPAN 端口 | `true` | 只收镜像流量,本机不参与转发 |
| 透明网桥 | `true` | 转发但不改源 IP |
| **云端 socks5 代理** | **`false`** | 代理进程以本机 IP 为源出站,回包目的也是本机 → 设 true 会**两个方向全丢**,一个包都抓不到 |
| 本机跑安卓模拟器 | `false` | 模拟器流量源 IP 就是本机 |

设错的后果都不好排查:该 false 却用 true 时,手机代理连得上、游戏能玩,但**一条包都
解析不出来**,日志还不报错,只是「抓包统计」的包数在涨却始终没有宠物数据。
**现在这段有自动兜底**:启动横幅会打出实际生效的 `-skip-self-ip`,运行期还有一道自检会把它抓出来。

⚠️ 它是 `RunLive(iface, skipSelf)` 的参数,**引擎启动时一次性生效**,面板改不了、
热更也不生效,必须 `docker restart rocom-go`。

#### 配置文件与管理面板

容器启动时从配置文件读参数,**与 systemd 部署用同一套键**(完整列表见
`scripts/deploy.sh` 头部注释),故两种部署方式的配置可互换。默认路径
`/data/rocom.env`(挂卷,重建容器不丢;不是 systemd 版的 `/etc/rocom.env`),
可用 `-e ROCOM_ENV_FILE=/某路径` 改。

优先级:**命令行显式给的 > 配置文件 > 内置默认值**。命令行始终能压过配置,
否则一旦往配置里写过值,命令行参数就再也覆盖不了。

首次启动会自动创建该文件(带注释模板),管理面板随即**可写** —— 不建文件的话面板
会显示「配置文件不可写」而降级只读,改了也存不下。

改完的生效方式:

| 项 | 生效 |
| --- | --- |
| socks5 地址 / 白名单 / 账号密码 / 连接数上限 | ✅ **立即**(代理热重启,抓包不中断) |
| 图鉴令牌、SMTP 邮箱 | ✅ 立即 |
| Web 监听地址 | ✅ 面板内「试运行 → 确认」,不用 restart |
| **抓包网卡 / 游戏端口 / HTTPS / `-skip-self-ip`** | ❌ **必须 `docker restart rocom-go`** |

#### 日常运维

```bash
docker logs -f rocom-go                      # 看日志
docker restart rocom-go                      # 重启

# 更新镜像(数据库在卷里,不丢历史)
docker pull docker.cnb.cool/test00123/roco:latest
docker rm -f rocom-go && docker run ...      # 用同样的 docker run 命令重建

# 备份(SQLite 热备)
docker run --rm -v rocom-data:/data -v $(pwd):/backup alpine \
  tar czf /backup/rocom-$(date +%F).tar.gz /data
```

#### 注意事项

- **抓包权限**:`--cap-add=NET_ADMIN --cap-add=NET_RAW` 是最小集,别用
  `--privileged`。若加了仍报 `operation not permitted`,多半是**嵌套容器**
  (容器内跑 docker):capability 不能超过 bounding set,此时只能 privileged
  或直接在宿主机部署。判断:`grep CapBnd /proc/self/status`。
- **网络用 host**:桥接模式下容器看不见宿主机物理网卡,`-iface` 会报网卡不存在。
  host 模式的代价是端口直接占宿主机,`-p` 不生效(端口由 `-addr` 定)。
- **数据持久化**:数据库在卷的 `/data/rocom.db`,重建容器 / 更新镜像都不丢历史;
  自签证书也生成在这里,信任一次后复用。**公网 IP 变了要删证书重生成**:
  `docker exec rocom-go rm -f /data/rocom-cert.pem /data/rocom-key.pem && docker restart rocom-go`
- **cgo 不能关**:抓包用 `gopacket/afpacket`,必须 `CGO_ENABLED=1`;builder 阶段
  除 gcc 外还要 `linux-headers`(提供 `linux/if_packet.h`),少装会编译失败。
- **公网 socks5 务必设白名单**:`-socks5-allow <手机公网IP>`。全网扫描器几分钟内
  就会找上无白名单的代理,耗尽 fd/goroutine 会把同进程的 Web 服务一起拖垮。
  密码认证(RFC 1929)是明文传输的,只能当第二道防线。
- **离线回放**:不需要 capability 与 host 网络,挂 pcap 即可:
  ```bash
  docker run -d --name rocom-go-replay -p 4939:4939 \
    -v /path/to/pcap:/pcap:ro -v rocom-data:/data \
    docker.cnb.cool/test00123/roco:latest -pcap /pcap/xxx.pcap
  ```

> 已实测:构建、离线回放(743 只宠物解析)、Web API、数据落卷、管理面板配置闭环
> (面板改配置 → 落盘 → 重启容器后生效)均正常。
> 实时抓包**收包**未在 CI 环境验证(该环境为嵌套容器且无 `CAP_NET_RAW`),
> 二进制中 afpacket 正常编译、可启动到创建 socket 那一步。
