# rocom-capture 容器镜像:多阶段构建,运行镜像只留二进制与最小依赖。
#
# ⚠️ **cgo 无法关闭**:抓包用 gopacket/afpacket(mmap 的 AF_PACKET 原始套接字),
# 它必须经 cgo 编译,故 CGO_ENABLED=1 与 gcc 都是硬要求 —— 这也是构建阶段要装
# build-base 的原因。别照抄「Go 项目通用 Dockerfile」里的 CGO_ENABLED=0 + scratch,
# 那样会编出不能抓包的二进制(编译能过,运行时 afpacket.NewTPacket 才报错)。
#
# 构建(builder 与运行镜像都用 alpine → 同为 musl,二进制可直接跑):
#   docker build -t rocom-capture .
#
# 运行(抓包需要两项 capability,见 docker-compose.yml 的注释):
#   docker run --cap-add=NET_ADMIN --cap-add=NET_RAW \
#     -v rocom-data:/data -p 4939:4939 rocom-capture -iface eth0 -db /data/rocom.db
#
# 已在容器内实测通过:镜像构建、离线回放(743 只宠物解析)、Web API、数据落卷。
# 实时抓包**收包**未验证 —— 开发环境是嵌套容器且外层无 CAP_NET_RAW,
# capability 无法授予(排查方法见 docker-compose.yml)。二进制里 afpacket
# 是正常编译进去的,能启动到创建 socket 那一步。

# ---- 构建阶段 ----
FROM golang:1.26-alpine AS builder

# build-base    = gcc + musl-dev + binutils,cgo 编译所需
# linux-headers = **必需**:afpacket 要 #include <linux/if_packet.h>(AF_PACKET 的
#                 tpacket 结构体定义),而 build-base 只带 C 库与编译器、不带内核头。
#                 少了它编译会停在 "fatal error: linux/if_packet.h: No such file"。
# git           = go mod download 取依赖(改用 vendor 后可去掉)
RUN apk add --no-cache build-base linux-headers git

WORKDIR /src

# 先只拷依赖清单:改业务代码时不会让这一层失效,省去重复 download
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# -trimpath 去掉构建机路径(便于复现构建),-ldflags "-s -w" 去符号表与调试信息
RUN CGO_ENABLED=1 go build -trimpath -ldflags "-s -w" -o /out/rocom-capture ./cmd/rocom-capture

# ---- 运行阶段 ----
FROM alpine:3.22

# ca-certificates:查随机蛋物种要打第三方图鉴 HTTPS 接口,缺根证书会 x509 报错
# tzdata:     容器默认 UTC,装上才能用 TZ=Asia/Shanghai 让日志与「午后」这类
#             显示对得上(加速日窗口按 FixedZone(+8) 判定,不依赖系统时区,
#             故它只影响可读性,不影响孵化倍率的正确性)
RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /out/rocom-capture /usr/local/bin/rocom-capture
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh

# 数据库与自签 TLS 证书都落这里;挂卷后重建/更新容器不会丢宠物、事件、涂地等历史
VOLUME /data

# Web 服务默认端口。抓包场景常用 network_mode: host,那时本行不生效、
# 端口直接由 -addr 决定,保留它仅为 bridge 模式下的文档与 -p 映射。
EXPOSE 4939

ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]

# 默认只给与模式无关的项:缺 -iface / -pcap 时入口脚本会明确提示,
# 而不是让进程打印一行用法就退出(容器里那句很容易被忽略)。
CMD ["-addr", ":4939", "-db", "/data/rocom.db"]
