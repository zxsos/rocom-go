#!/bin/sh
# 容器入口:把配置文件里的参数组装成启动参数,再 exec 真正的二进制。
#
# 为什么需要它 —— 两件事,缺一不可:
#
#   1. 缺 -iface / -pcap 时,rocom-capture 只打印一行用法就退出。容器里那行孤零零的
#      日志极易被当成「镜像坏了」。这里把「必须二选一」说清楚,并给可复制的命令。
#
#   2. **管理面板要靠配置文件才能改设置**。systemd 部署下那份文件是 /etc/rocom.env
#      (deploy.sh 生成、run.sh 把每项组装成 flag);容器里没有 systemd,若不做同样的事,
#      面板就会显示「配置文件不可写」而降级只读 —— 改了也存不下。
#      故这里把「参数组装」这一层搬进容器,让 Docker 与 systemd 两种部署行为一致:
#      **配置文件是参数的唯一来源,命令行只用来覆盖它**。
#
# 优先级:命令行显式给的 > 配置文件 > 内置默认值。
# 命令行必须能压过配置文件 —— 否则一旦往配置里写过值,命令行参数就再也覆盖不了。
#
# 支持的键与 systemd 版完全一致(见 scripts/deploy.sh 的注释头),故同一份配置文件
# 在两种部署方式下通用。

set -e

# ---- 配置文件位置 ----
# 默认 /data/rocom.env(挂卷,重建容器不丢),而非 systemd 版的 /etc/rocom.env ——
# 容器里 /etc 不持久化。
#
# ⚠️ 必须 export:Go 侧的 configEnvPath() 读同一个环境变量来决定面板读写哪个文件,
# 不 export 的话面板仍会去找 /etc/rocom.env,与这里读的不是同一份。
: "${ROCOM_ENV_FILE:=/data/rocom.env}"
export ROCOM_ENV_FILE

# 文件不存在时先建一个带注释的模板:面板据此判断「可写」。
# 不建的话首次启动就是只读,管理员得先上手建文件才知道原来能改。
if [ ! -f "$ROCOM_ENV_FILE" ]; then
    if mkdir -p "$(dirname "$ROCOM_ENV_FILE")" 2>/dev/null &&
        cat > "$ROCOM_ENV_FILE" <<'ENVTEMPLATE'
# rocom-capture 运行参数(Docker 部署)
# 改完执行: docker restart rocom
#
# 多数项可在管理面板(#/admin)里改并立即生效;
# 抓包网卡、游戏端口、HTTPS 属启动项,须重启容器。
#
# 完整键列表见 scripts/deploy.sh 头部注释(与 systemd 部署通用)。
ENVTEMPLATE
    then
        chmod 600 "$ROCOM_ENV_FILE" 2>/dev/null || true
    else
        echo "rocom-capture: 提示: 无法创建 $ROCOM_ENV_FILE,管理面板将为只读" >&2
    fi
fi

# env_get <KEY>:取配置文件里某个键的值;文件不存在或没有该键时为空。
#
# 先**剥掉注释行**再匹配:注释里常出现 KEY= 字样(本脚本生成的模板就是),
# 只靠 tail -1 取最后一个匹配是碰运气 —— 注释若写在配置**之后**就会被取中,
# 拿注释里的说明文字去当参数。故顺序必须是「去注释 → 匹配 → 取最后一个」。
#
# 行首锚定与去注释是两道不同的防线:前者防「# 前面还有内容」,后者防顺序问题。
env_get() {
    [ -f "$ROCOM_ENV_FILE" ] || return 0
    line=$(sed 's/^[[:space:]]*#.*//' "$ROCOM_ENV_FILE" 2>/dev/null |
        grep -E "^[[:space:]]*(export[[:space:]]+)?$1=" | tail -1)
    [ -n "$line" ] || return 0
    val=${line#*=}
    # 去首尾配对引号:EnvironmentFile 允许用引号包住含空格的值(如带空格的密码)
    case "$val" in
        \"*\") val=${val#\"}; val=${val%\"} ;;
        \'*\') val=${val#\'}; val=${val%\'} ;;
    esac
    printf '%s' "$val"
}

# ---- 记录命令行已显式给出的 flag ----
# 只取 = 之前的部分,故含空格的密码之类不会被误当成 flag。
# 归一化 --flag 为 -flag,支持两种前缀写法。
given=" "
for a in "$@"; do
    case "$a" in
        --*) a="-${a#--}" ;;
    esac
    case "$a" in
        -*) given="$given${a%%=*} " ;;
    esac
done

# has <flag>:命令行是否已显式给过该 flag
has() {
    case "$given" in
        *" $1 "*) return 0 ;;
    esac
    return 1
}

# ---- 补充参数:命令行没给的,才从配置文件取 ----
#
# 逐个内联写而非抽成函数:POSIX sh 的函数改不了调用方的位置参数,而值可能含空格
# (密码、白名单),不能走字符串拼接,只能靠 set -- 逐个追加。
#
# `[ -z "$v" ] ||` 而非 `[ -n "$v" ] &&`:后者在 v 为空时整个 AND 列表返回非零,
# 在 set -e 下会**直接退出** —— 那会让「只配了部分项」变成启动失败。
# OR 写法无论哪种情况都返回成功。

if ! has "-iface"; then
    v=$(env_get ROCOM_IFACE)
    [ -z "$v" ] || set -- "$@" -iface "$v"
fi
if ! has "-pcap"; then
    v=$(env_get ROCOM_PCAP)
    [ -z "$v" ] || set -- "$@" -pcap "$v"
fi
if ! has "-port"; then
    v=$(env_get ROCOM_PORT)
    [ -z "$v" ] || set -- "$@" -port "$v"
fi
if ! has "-addr"; then
    v=$(env_get ROCOM_ADDR)
    [ -z "$v" ] || set -- "$@" -addr "$v"
fi
# 数据库与证书默认落 /data:不挂卷的话容器一重建,历史与证书(要重新信任)就都没了。
if ! has "-db"; then
    v=$(env_get ROCOM_DB)
    set -- "$@" -db "${v:-/data/rocom.db}"
fi
if ! has "-cert"; then
    v=$(env_get ROCOM_CERT)
    set -- "$@" -cert "${v:-/data/rocom-cert.pem}"
fi
if ! has "-key"; then
    v=$(env_get ROCOM_KEY)
    set -- "$@" -key "${v:-/data/rocom-key.pem}"
fi
# -tls 是开关:变量非空即启用(与 systemd 版 run.sh 一致,不解析 true/false)
if ! has "-tls"; then
    v=$(env_get ROCOM_TLS)
    [ -z "$v" ] || set -- "$@" -tls
fi

# socks5 相关。顺序与 run.sh 保持一致。
if ! has "-socks5-addr"; then
    v=$(env_get ROCOM_SOCKS5_ADDR)
    [ -z "$v" ] || set -- "$@" -socks5-addr "$v"
fi
# -skip-self-ip:启用 socks5 时**必须** false,否则代理进程以本机 IP 出站的流量
# 会被单臂去重逻辑丢弃(表现是「代理连上了但抓不到任何包」)。这是最容易踩的坑,
# 故 run.sh 在启用 socks5 时默认 false;这里照办,显式配置优先。
if ! has "-skip-self-ip"; then
    v=$(env_get ROCOM_SKIP_SELF_IP)
    if [ -z "$v" ] && { has "-socks5-addr" || [ -n "$(env_get ROCOM_SOCKS5_ADDR)" ]; }; then
        v=false
    fi
    # Go 的 bool flag 不接受空格分开的 true/false,必须用 = 形式
    [ -z "$v" ] || set -- "$@" "-skip-self-ip=$v"
fi
if ! has "-socks5-allow"; then
    v=$(env_get ROCOM_SOCKS5_ALLOW)
    [ -z "$v" ] || set -- "$@" -socks5-allow "$v"
fi
if ! has "-socks5-block"; then
    v=$(env_get ROCOM_SOCKS5_BLOCK)
    [ -z "$v" ] || set -- "$@" -socks5-block "$v"
fi
if ! has "-socks5-max-conns"; then
    v=$(env_get ROCOM_SOCKS5_MAX_CONNS)
    [ -z "$v" ] || set -- "$@" -socks5-max-conns "$v"
fi
if ! has "-socks5-user"; then
    v=$(env_get ROCOM_SOCKS5_USER)
    [ -z "$v" ] || set -- "$@" -socks5-user "$v"
fi
if ! has "-socks5-pass"; then
    v=$(env_get ROCOM_SOCKS5_PASS)
    [ -z "$v" ] || set -- "$@" -socks5-pass "$v"
fi
# 邮箱与图鉴令牌:面板随时可改(热更),这里的初值只是「容器重建后不至于丢」。
if ! has "-merchant-smtp-user"; then
    v=$(env_get ROCOM_SMTP_USER)
    [ -z "$v" ] || set -- "$@" -merchant-smtp-user "$v"
fi
if ! has "-merchant-smtp-pass"; then
    v=$(env_get ROCOM_SMTP_PASS)
    [ -z "$v" ] || set -- "$@" -merchant-smtp-pass "$v"
fi
if ! has "-egg-api-key"; then
    v=$(env_get ROCOM_EGG_API_KEY)
    [ -z "$v" ] || set -- "$@" -egg-api-key "$v"
fi

# ---- 校验:必须二选一 ----
# 放在补充之后 —— mode 可能来自配置文件,先补再验才不会误报。
have_mode=0
for a in "$@"; do
    case "$a" in
        --*) a="-${a#--}" ;;
    esac
    case "$a" in
        -iface | -iface=* | -pcap | -pcap=*)
            have_mode=1
            ;;
    esac
done

if [ "$have_mode" -eq 0 ]; then
    echo "rocom-capture: 必须指定 -iface 或 -pcap 之一" >&2
    echo "" >&2
    echo "  实时抓包: -iface <网卡名>       (需 --cap-add=NET_ADMIN --cap-add=NET_RAW)" >&2
    echo "  离线回放: -pcap <pcap文件>" >&2
    echo "" >&2
    echo "  也可写入配置文件 $ROCOM_ENV_FILE:" >&2
    echo "    ROCOM_IFACE=eth0      或      ROCOM_PCAP=/pcap/xxx.pcap" >&2
    echo "" >&2
    echo "例:" >&2
    echo "  docker run --cap-add=NET_ADMIN --cap-add=NET_RAW \\" >&2
    echo "    --network host -v rocom-data:/data \\" >&2
    echo "    rocom-capture -iface eth0" >&2
    exit 1
fi

exec rocom-capture "$@"
