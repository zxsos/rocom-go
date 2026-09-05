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

# 2. 构建前端到 embed 目录
cd web && npm install && npm run build && cd ..

# 3. 构建单二进制
go build -o rocom-capture ./cmd/rocom-capture
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

make release   # → dist/rocom-capture-linux-amd64、dist/rocom-capture-linux-arm64(均静态、已 strip)
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
sudo ./rocom-capture -iface <网卡> -port 8195 -addr :4939

# 离线回放已抓的 pcap
./rocom-capture -pcap ./pcap/xxx.pcap -addr :4939

# 启用 HTTPS(自签证书;手机经局域网访问时用)
sudo ./rocom-capture -iface <网卡> -tls

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
sudo ./rocom-capture -iface eth0 -socks5-addr :1080 -skip-self-ip=false \
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
sudo systemctl restart rocom
sudo systemctl status rocom
journalctl -u rocom -f           # 看日志

# 从手动部署迁移(已有旧库在跑,想切到 systemd 管理)
#    自动停旧进程、搬库与证书到 /var/lib/rocom、从旧启动参数生成 env
sudo ./scripts/deploy.sh --migrate /root/roco

# 备份数据库(热备,不锁库)
sudo ./scripts/deploy.sh --backup

# 从 tar 包安装(而非本地 dist/)
sudo ./scripts/deploy.sh --archive rocom-capture.tar

# 卸载(默认保留数据,加 --purge 才连数据一起删)
sudo ./scripts/deploy.sh --uninstall
```

`/etc/rocom.env` 里的多数项可在管理面板(`#/admin`,隐式入口)直接改,改动立即生效并写回该文件;
只有抓包网卡、游戏端口、HTTPS 属启动项,改它们仍需 `systemctl restart rocom`。
**Web 监听地址**也可以在线改,但它正是你用来改它的那条连接的另一端,故不直接生效:先在新端口
试运行,从新地址打开过面板后才落盘;90 秒内不确认会自动回滚(详见 [docs/api/](docs/api/README.md))。

更新流程只替换 `/opt/rocom/rocom-capture` 并 `systemctl restart`,数据库 `/var/lib/rocom/rocom.db`
不受影响。重启后自动从 `sessions` 表预热会话密钥、连接归属、场景定位(有效期 24h),对仍存活的
游戏连接从中段继续解密,历史统计原样保留。仅当库 schema 变化(新版加了字段/表)时才需删库重建——
`CREATE TABLE IF NOT EXISTS` 是幂等的,schema 没变就直接打开旧库即可。
