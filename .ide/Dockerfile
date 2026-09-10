 # CNB 云原生开发(沙盒)环境镜像。
#
# 为什么需要它:官方默认镜像 cnbcool/default-dev-env(= debian:trixie + code-server
# + Node 22 + uv)**不含 Go,也不含浏览器**。本项目 go.mod 要求 go 1.26.4,
# 前端 web/scripts/verify-*.mjs 里的浏览器校验需要 Playwright Chromium,
# 缺任一项都要在每次新建环境时手工重装约 1GB —— 而沙盒闲置 18 小时即被回收销毁,
# 装进 /root 的东西默认不漫游(见 docs 说明),重建就没了。故把工具链声明进镜像,
# 与代码同源、一次构建长期复用。
#
# 构建入口:.cnb.yml 的 vscode 事件(build: .ide/Dockerfile),构建上下文为仓库根目录。

FROM cnbcool/default-dev-env:latest

# ---- Go(版本与 go.mod 的 go 指令保持一致,升级 Go 时改这里)----
ENV GO_VERSION=1.26.4
ENV GOROOT=/usr/local/go
ENV GOPATH=/root/go
ENV PATH=/usr/local/go/bin:/root/go/bin:$PATH
# Playwright 默认就查这个路径,显式声明是为了与 .cnb.yml 的 stage 保持一致
ENV PLAYWRIGHT_BROWSERS_PATH=/root/.cache/ms-playwright

RUN set -eux; \
    arch="$(dpkg --print-architecture)"; \
    case "$arch" in \
      amd64) GO_ARCH=amd64 ;; \
      arm64) GO_ARCH=arm64 ;; \
      *) echo "unsupported arch: $arch" >&2; exit 1 ;; \
    esac; \
    curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-${GO_ARCH}.tar.gz" \
      | tar -C /usr/local -xz; \
    go version

# ---- cgo 依赖 ----
# 抓包用 gopacket/afpacket,必须 CGO_ENABLED=1 编译,且要 #include <linux/if_packet.h>
# (AF_PACKET 的 tpacket 结构体):
#   build-essential    = gcc 等编译器
#   linux-headers-amd64 = 内核头,只装 build-essential 会停在
#                        "fatal error: linux/if_packet.h: No such file"
# 注意:装了这些也**不能**在沙盒里实时抓包 —— 容器没有 CAP_NET_RAW/NET_ADMIN
# (实测 -iface 报 operation not permitted),实时抓包请在网关机上跑;
# 沙盒里用 -pcap <文件> 离线回放。
RUN apt-get update && apt-get install -y --no-install-recommends \
        build-essential \
        linux-headers-amd64 \
    && apt-get clean && rm -rf /var/lib/apt/lists/*

# ---- Go 模块预下载:只拷依赖清单,改业务代码不会让这一层失效 ----
COPY go.mod go.sum ./
RUN go mod download

# ---- Playwright Chromium ----
# 版本需与 web/package.json 的 playwright 对齐:浏览器 revision 由 playwright 版本锁定,
# 版本不一致会报 "Executable doesn't exist"。升级前端 playwright 时同步改这里,
# 忘记改也没关系 —— .cnb.yml 的 stage 会在环境启动时按 web/package.json 补装。
RUN npm install -g playwright@1.62.1 \
    && playwright install --with-deps chromium

# 上面的 ENV PATH 只对非登录 shell 生效:登录 shell(bash -l / ssh / zsh)会先跑
# /etc/profile,把 PATH 重置成系统默认值,go 就找不到了 —— 实测 bash -lc 'go version'
# 报 "未找到命令"。故再写一份 profile.d(本镜像的 ~/.zshrc 开头会 source /etc/profile)。
# 放在文件末尾:这一层最轻,不会让上面几个大层的构建缓存失效。
RUN printf '%s\n' 'export GOROOT=/usr/local/go' 'export GOPATH=/root/go' \
      'export PATH=/usr/local/go/bin:/root/go/bin:$PATH' \
      'export PLAYWRIGHT_BROWSERS_PATH=/root/.cache/ms-playwright' \
      > /etc/profile.d/rocom-toolchain.sh \
    && chmod +x /etc/profile.d/rocom-toolchain.sh
