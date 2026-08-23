#!/usr/bin/env bash
#
# deploy.sh — 在 Linux 网关上部署/更新 rocom-capture,数据与程序分离,更新不丢历史。
#
# 数据(rocom.db / TLS 证书)放在独立的 /var/lib/rocom/ 下,程序放在 /opt/rocom/ 下。
# 更新时只替换二进制并重启服务,数据库与证书不受影响——会话密钥/归属/场景全部预热恢复,
# 宠物/事件/涂地等历史统计原样保留。
#
# 用法:
#   sudo ./deploy.sh --build        # 服务器上 git pull + go build + 部署(日常更新用这个)
#   sudo ./deploy.sh                # 首次安装或更新已有二进制(自动找 dist/ 或当前目录)
#   sudo ./deploy.sh --binary /tmp/rocom-capture  # 指定二进制路径
#   sudo ./deploy.sh --archive x.tar  # 从 tar 包安装(内含 rocom-capture 单文件)
#   sudo ./deploy.sh --stop           # 仅停止服务(不删除数据)
#   sudo ./deploy.sh --backup         # 备份数据库到 /var/lib/rocom/backup/
#   sudo ./deploy.sh --migrate /root/roco  # 从旧目录迁移数据并安装(首次从手动部署切到 systemd)
#   sudo ./deploy.sh --uninstall      # 卸载程序(保留数据;加 --purge 同时删数据)
#
# 环境变量(在调用前 export,或写入 /etc/rocom.env):
#   ROCOM_IFACE       抓包网卡(默认 eth0,如 eth0 / br-lan)
#   ROCOM_PORT        游戏端口(默认 8195)
#   ROCOM_ADDR        Web 监听地址(默认 :4939)
#   ROCOM_TLS         启用 HTTPS(设 1 启用)
#   ROCOM_SOCKS5_ADDR SOCKS5 监听地址(如 :1080;空=不启用)
#   ROCOM_SOCKS5_ALLOW SOCKS5 客户端白名单(逗号分隔 IP/CIDR)
#   ROCOM_SOCKS5_USER / ROCOM_SOCKS5_PASS  SOCKS5 认证
#   ROCOM_SKIP_SELF_IP  socks5 模式下设 false(默认 true)
#   ROCOM_EXTRA       其他要透传的参数(如 -ignore-ip)
#
set -euo pipefail

# ---- 目录约定 ----
INSTALL_DIR="/opt/rocom"
RUN_SCRIPT="$INSTALL_DIR/run.sh"
DATA_DIR="/var/lib/rocom"
BACKUP_DIR="$DATA_DIR/backup"
SERVICE_NAME="rocom"
SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}.service"
ENV_FILE="/etc/rocom.env"

BIN_NAME="rocom-capture"
ARCH="$(uname -m)"
case "$ARCH" in
    x86_64)  ARCH="amd64" ;;
    aarch64) ARCH="arm64" ;;
    *) echo "不支持的架构: $ARCH" >&2; exit 1 ;;
esac

# ---- 参数 ----
ACTION="install"
ARCHIVE=""
BINARY=""
MIGRATE_SRC=""
PURGE=0
while [[ $# -gt 0 ]]; do
    case "$1" in
        --build)    ACTION="build"; shift ;;
        --archive)  ARCHIVE="$2"; ACTION="install"; shift 2 ;;
        --binary)   BINARY="$2"; ACTION="install"; shift 2 ;;
        --stop)     ACTION="stop"; shift ;;
        --backup)   ACTION="backup"; shift ;;
        --migrate)  ACTION="migrate"; MIGRATE_SRC="$2"; shift 2 ;;
        --uninstall) ACTION="uninstall"; shift ;;
        --purge)    PURGE=1; shift ;;
        -h|--help)  sed -n '2,24p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
        *) echo "未知参数: $1" >&2; exit 1 ;;
    esac
done

# ---- 必须 root ----
if [[ "$(id -u)" -ne 0 ]]; then
    echo "错误: 需要 root 权限,请用 sudo 运行。" >&2
    exit 1
fi

# ---- 确定二进制来源 ----
find_binary() {
    # 1) --binary 直接指定路径
    if [[ -n "$BINARY" ]]; then
        if [[ ! -f "$BINARY" ]]; then
            echo "错误: --binary 指定的文件不存在: $BINARY" >&2
            exit 1
        fi
        echo "$BINARY"
        return
    fi
    # 2) --archive 指定的 tar 包
    if [[ -n "$ARCHIVE" ]]; then
        local tmp; tmp="$(mktemp -d)"
        tar -xf "$ARCHIVE" -C "$tmp"
        local found
        found="$(find "$tmp" -name "${BIN_NAME}-linux-${ARCH}" -o -name "$BIN_NAME" | head -1 || true)"
        if [[ -z "$found" ]]; then
            echo "错误: tar 包内未找到 $BIN_NAME (linux-$ARCH)" >&2
            rm -rf "$tmp"
            exit 1
        fi
        echo "$found"
        return
    fi
    # 2) dist/ 构建产物
    local dist="./dist/${BIN_NAME}-linux-${ARCH}"
    if [[ -f "$dist" ]]; then
        echo "$dist"
        return
    fi
    # 3) 当前目录
    if [[ -f "./$BIN_NAME" ]]; then
        echo "./$BIN_NAME"
        return
    fi
    echo "错误: 未找到二进制。请先 make release,或用 --archive 指定 tar 包。" >&2
    exit 1
}

# ---- 生成 systemd service 文件 ----
write_service() {
    # 启动脚本:systemd 的 ExecStart 不支持 ${VAR:+...} 条件展开,
    # 参数组装放在 bash 脚本里做(通过 systemctl edit 或改 /etc/rocom.env 调整参数)。
    cat > "$RUN_SCRIPT" <<'EOF'
#!/usr/bin/env bash
# 由 deploy.sh 生成,勿手改;参数调整请编辑 /etc/rocom.env
BIN=/opt/rocom/rocom-capture
args=(
  -db /var/lib/rocom/rocom.db
  -cert /var/lib/rocom/rocom-cert.pem
  -key /var/lib/rocom/rocom-key.pem
)
[[ -n "${ROCOM_IFACE:-}" ]] && args+=(-iface "$ROCOM_IFACE")
[[ -n "${ROCOM_PORT:-}" ]] && args+=(-port "$ROCOM_PORT")
[[ -n "${ROCOM_ADDR:-}" ]] && args+=(-addr "$ROCOM_ADDR")
[[ -n "${ROCOM_TLS:-}" ]] && args+=(-tls)
if [[ -n "${ROCOM_SOCKS5_ADDR:-}" ]]; then
  args+=(-socks5-addr "$ROCOM_SOCKS5_ADDR")
  # Go flag.Bool 不接受空格分开的 true/false,必须用 =false 形式
  args+=(-skip-self-ip="${ROCOM_SKIP_SELF_IP:-false}")
fi
[[ -n "${ROCOM_SOCKS5_ALLOW:-}" ]] && args+=(-socks5-allow "$ROCOM_SOCKS5_ALLOW")
if [[ -n "${ROCOM_SOCKS5_USER:-}" ]]; then
  args+=(-socks5-user "$ROCOM_SOCKS5_USER")
  [[ -n "${ROCOM_SOCKS5_PASS:-}" ]] && args+=(-socks5-pass "$ROCOM_SOCKS5_PASS")
fi
# ROCOM_EXTRA 按空格拆分透传(可含多个 flag)
read -r -a extra <<< "${ROCOM_EXTRA:-}"
args+=("${extra[@]}")
exec "$BIN" "${args[@]}"
EOF
    chmod +x "$RUN_SCRIPT"
    cat > "$SERVICE_FILE" <<EOF
[Unit]
Description=rocom-capture (游戏流量抓包与统计)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
# 环境变量从 /etc/rocom.env 读取(IFACE / SOCKS5 等,见 deploy.sh 注释)
EnvironmentFile=-/etc/rocom.env
# 数据库与证书放在 /var/lib/rocom 下,更新二进制不动数据
ExecStart=$RUN_SCRIPT
# 抓包需要 root(afpacket);如用 pcap 模式可改为专用用户
User=root
# 崩溃自动重启
Restart=on-failure
RestartSec=2
# 数据目录由 systemd 创建
StateDirectory=rocom
# 日志走 journald
StandardOutput=journal
StandardError=journal
SyslogIdentifier=rocom

[Install]
WantedBy=multi-user.target
EOF
    echo "已写入 $SERVICE_FILE 与 $RUN_SCRIPT"
}

# ---- 生成环境变量文件 ----
write_env() {
    if [[ -f "$ENV_FILE" ]]; then
        echo "$ENV_FILE 已存在,保留现有配置(如需更新请手动编辑)"
        return
    fi
    cat > "$ENV_FILE" <<EOF
# rocom-capture 运行参数(改后执行: systemctl restart rocom)
# 抓包网卡(默认 eth0)
ROCOM_IFACE=eth0
# 游戏端口(默认 8195)
ROCOM_PORT=8195
# Web 监听地址(默认 :4939)
ROCOM_ADDR=:4939
# 启用 HTTPS(设 1 启用,首次自动生成自签证书)
ROCOM_TLS=
# SOCKS5 代理(云端部署时用;留空=不启用)
ROCOM_SOCKS5_ADDR=
ROCOM_SOCKS5_ALLOW=
ROCOM_SOCKS5_USER=
ROCOM_SOCKS5_PASS=
ROCOM_SKIP_SELF_IP=false
# 其他透传参数(如 -ignore-ip 10.0.0.1)
ROCOM_EXTRA=
EOF
    chmod 600 "$ENV_FILE"
    echo "已写入 $ENV_FILE —— 请编辑填入 ROCOM_IFACE 等参数后执行: systemctl start rocom"
}

# ---- 主流程 ----
case "$ACTION" in
    build)
        # 在服务器上 git pull + go build + 部署一条龙。
        # 前端产物(internal/server/web)已提交在仓库里,go build 时 embed 进二进制。
        # 若 web/ 源码比产物新(改了前端但忘 build),会自动调 npm run build 刷新产物
        # (需服务器装 node/npm;未装则报错提示本机 build)。
        # 依赖:go(已装)、git(拉代码)。不需要 zig(那是交叉编译用的)。
        REPO_DIR="$(cd "$(dirname "$0")/.." && pwd)"
        echo "==> 拉取最新代码 ($REPO_DIR)"
        cd "$REPO_DIR"
        git pull --ff-only

        # 确认 go 可用:sudo 的 secure_path 可能不含 go 的安装路径,
        # 从常见位置(/usr/local/go/bin、$HOME/go/bin、原用户 PATH)自动补找。
        if ! command -v go >/dev/null 2>&1; then
            for d in /usr/local/go/bin /usr/lib/go/bin "$HOME/go/bin" "${SUDO_USER:+$(getent passwd "$SUDO_USER" | cut -d: -f6)/go/bin}"; do
                if [[ -x "$d/go" ]]; then
                    export PATH="$PATH:$d"
                    break
                fi
            done
        fi
        if ! command -v go >/dev/null 2>&1; then
            echo "错误: 未找到 go,请先安装 Go。" >&2
            exit 1
        fi

        # 前端构建:对比 web/ 与 internal/server/web/ 两个目录的最近改动提交,
        # 若 web/ 的提交晚于产物目录,说明前端源码改了但产物没同步,自动 npm run build。
        # (用 git log 比对提交而非 mtime——git pull 后所有文件 mtime 都被刷新,mtime 不可靠)
        FRONTEND_OUT="$REPO_DIR/internal/server/web"
        NEED_FRONTEND=0
        if [[ -d "$REPO_DIR/web" && -f "$FRONTEND_OUT/index.html" ]]; then
            WEB_COMMIT="$(git -C "$REPO_DIR" log -1 --format=%H -- web/ 2>/dev/null || true)"
            OUT_COMMIT="$(git -C "$REPO_DIR" log -1 --format=%H -- internal/server/web/ 2>/dev/null || true)"
            if [[ -n "$WEB_COMMIT" && -n "$OUT_COMMIT" && "$WEB_COMMIT" != "$OUT_COMMIT" ]]; then
                # web/ 提交是否晚于产物提交(是 web/ 的祖先吗?是则产物已包含此次前端改动)
                if ! git -C "$REPO_DIR" merge-base --is-ancestor "$WEB_COMMIT" "$OUT_COMMIT" 2>/dev/null; then
                    NEED_FRONTEND=1
                fi
            fi
        fi
        if [[ "$NEED_FRONTEND" -eq 1 ]]; then
            echo "==> 检测到前端源码比产物新,构建前端..."
            # 找 npm/node(sudo 同样可能不在 secure_path 里)
            if ! command -v npm >/dev/null 2>&1; then
                NPM_DIRS=(/usr/local/bin /usr/bin "$HOME/.local/bin")
                # 官方二进制包常装在 /usr/local/node-v*/bin(版本号目录)
                for d in /usr/local/node-*/bin; do
                    [[ -d "$d" ]] && NPM_DIRS+=("$d")
                done
                if [[ -n "${SUDO_USER:-}" ]]; then
                    SUDO_HOME="$(getent passwd "$SUDO_USER" | cut -d: -f6)"
                    NPM_DIRS+=("$SUDO_HOME/.local/bin")
                    for d in "$SUDO_HOME"/.local/node-*/bin "$SUDO_HOME"/node-*/bin; do
                        [[ -d "$d" ]] && NPM_DIRS+=("$d")
                    done
                    # nvm: ~/.nvm/versions/node/<ver>/bin
                    if [[ -d "$SUDO_HOME/.nvm/versions/node" ]]; then
                        NVM_NODE="$(ls "$SUDO_HOME/.nvm/versions/node" 2>/dev/null | tail -1)"
                        [[ -n "$NVM_NODE" ]] && NPM_DIRS+=("$SUDO_HOME/.nvm/versions/node/$NVM_NODE/bin")
                    fi
                fi
                for d in "${NPM_DIRS[@]}"; do
                    if [[ -x "$d/npm" ]]; then export PATH="$PATH:$d"; break; fi
                done
            fi
            if ! command -v npm >/dev/null 2>&1; then
                echo "错误: 检测到前端源码有更新但服务器未装 node/npm。" >&2
                echo "       请在本机执行 npm run build 提交产物,或服务器装 node 后重试。" >&2
                exit 1
            fi
            (cd "$REPO_DIR/web" && npm install && npm run build)
        else
            echo "==> 前端产物已最新(源码无更新),跳过构建"
        fi

        echo "==> 编译 (go build,前端 embed)"
        CGO_ENABLED=1 go build -trimpath -o "$BIN_NAME" ./cmd/rocom-capture
        echo "    产物: $REPO_DIR/$BIN_NAME ($(du -h "$BIN_NAME" | cut -f1))"

        # 编译完,设置 BINARY 让 install 分支用这个二进制
        BINARY="$REPO_DIR/$BIN_NAME"
        ACTION="install"
        ;&  # fall through 到 install

    install)
        SRC_BIN="$(find_binary)"
        mkdir -p "$INSTALL_DIR" "$DATA_DIR" "$BACKUP_DIR"

        # 停旧服务(若在运行)
        if systemctl is-active --quiet "$SERVICE_NAME" 2>/dev/null; then
            echo "停止旧服务..."
            systemctl stop "$SERVICE_NAME"
        fi

        # 替换二进制
        echo "安装二进制: $SRC_BIN → $INSTALL_DIR/$BIN_NAME"
        cp -f "$SRC_BIN" "$INSTALL_DIR/$BIN_NAME"
        chmod +x "$INSTALL_DIR/$BIN_NAME"

        # 写 service 与 env
        write_service
        write_env

        systemctl daemon-reload
        systemctl enable "$SERVICE_NAME" 2>/dev/null || true

        # 如果 env 已配置了 IFACE,直接启动;否则提示用户编辑
        if grep -qP '^\s*ROCOM_IFACE=\S' "$ENV_FILE"; then
            echo "启动服务..."
            systemctl restart "$SERVICE_NAME"
            sleep 1
            systemctl status "$SERVICE_NAME" --no-pager || true
            echo "==> 部署完成。日志: journalctl -u rocom -f"
        else
            echo "==> 二进制已就位。请编辑 $ENV_FILE 填入 ROCOM_IFACE 后执行:"
            echo "      systemctl start rocom"
        fi
        ;;

    stop)
        systemctl stop "$SERVICE_NAME" 2>/dev/null || true
        echo "服务已停止(数据保留在 $DATA_DIR)"
        ;;

    migrate)
        # 从手动部署(rocom-capture 直接跑在工作目录、库在工作目录下)迁移到 systemd 管理。
        # 会:停旧进程 → 搬库与证书 → 识别旧启动参数生成 env → 安装二进制 → 启动服务。
        SRC_DIR="$MIGRATE_SRC"
        if [[ -z "$SRC_DIR" || ! -d "$SRC_DIR" ]]; then
            echo "用法: sudo ./deploy.sh --migrate <旧程序目录>" >&2
            echo "示例: sudo ./deploy.sh --migrate /root/roco" >&2
            exit 1
        fi
        OLD_DB="$SRC_DIR/rocom.db"
        if [[ ! -f "$OLD_DB" ]]; then
            echo "错误: 未找到旧库 $OLD_DB" >&2
            exit 1
        fi

        # 如果旧进程在跑,从 ps 提取启动参数自动生成 env
        ENV_AUTO=0
        OLD_PID="$(pgrep -f "$SRC_DIR/$BIN_NAME" | head -1 || true)"
        if [[ -n "$OLD_PID" ]]; then
            OLD_ARGS="$(tr '\0' ' ' < /proc/$OLD_PID/cmdline 2>/dev/null || true)"
            echo "检测到旧进程 (PID $OLD_PID),提取启动参数..."
            echo "  $OLD_ARGS"
        else
            OLD_ARGS=""
            echo "未检测到运行中的旧进程,使用默认 env 模板"
        fi

        # 停旧进程(不管有没有 systemd 都停)
        if systemctl is-active --quiet "$SERVICE_NAME" 2>/dev/null; then
            systemctl stop "$SERVICE_NAME"
        fi
        pkill -f "$SRC_DIR/$BIN_NAME" 2>/dev/null || true
        sleep 1

        # 搬数据
        mkdir -p "$DATA_DIR" "$BACKUP_DIR"
        echo "迁移数据库: $OLD_DB → $DATA_DIR/rocom.db"
        cp -f "$OLD_DB" "$DATA_DIR/rocom.db"
        # WAL/shm 一起搬(存在则搬)
        for ext in -wal -shm; do
            if [[ -f "$OLD_DB$ext" ]]; then
                cp -f "$OLD_DB$ext" "$DATA_DIR/rocom.db$ext"
            fi
        done
        # 证书(存在则搬)
        for f in rocom-cert.pem rocom-key.pem; do
            if [[ -f "$SRC_DIR/$f" ]]; then
                cp -f "$SRC_DIR/$f" "$DATA_DIR/$f"
                echo "迁移证书: $SRC_DIR/$f → $DATA_DIR/$f"
            fi
        done

        # 从旧启动参数解析出 env(仅当旧进程在跑时)
        if [[ -n "$OLD_ARGS" ]]; then
            # 临时清空 env,逐个解析填充
            ROCOM_IFACE="" ROCOM_PORT="" ROCOM_ADDR="" ROCOM_TLS=""
            ROCOM_SOCKS5_ADDR="" ROCOM_SOCKS5_ALLOW="" ROCOM_SOCKS5_USER="" ROCOM_SOCKS5_PASS=""
            ROCOM_SKIP_SELF_IP="" ROCOM_EXTRA=""

            # 用空格切,遍历 -key value 对
            set -- $OLD_ARGS
            while [[ $# -gt 0 ]]; do
                case "$1" in
                    -iface)         ROCOM_IFACE="$2"; shift 2 ;;
                    -port)          ROCOM_PORT="$2"; shift 2 ;;
                    -addr)          ROCOM_ADDR="$2"; shift 2 ;;
                    -tls)           ROCOM_TLS=1; shift ;;
                    -socks5-addr)   ROCOM_SOCKS5_ADDR="$2"; shift 2 ;;
                    -socks5-allow)  ROCOM_SOCKS5_ALLOW="$2"; shift 2 ;;
                    -socks5-user)   ROCOM_SOCKS5_USER="$2"; shift 2 ;;
                    -socks5-pass)   ROCOM_SOCKS5_PASS="$2"; shift 2 ;;
                    -skip-self-ip)  ROCOM_SKIP_SELF_IP="$2"; shift 2 ;;
                    -db|-cert|-key|rocom-capture|sudo) shift ;;
                    -socks5-max-conns) shift 2 ;;  # systemd service 用默认值
                    *) shift ;;
                esac
            done

            # 写 env(覆盖,因为是从旧进程提取的)
            cat > "$ENV_FILE" <<EOF
# rocom-capture 运行参数(由 deploy.sh --migrate 从旧进程自动生成)
# 抓包网卡
ROCOM_IFACE=${ROCOM_IFACE:-eth0}
# 游戏端口
ROCOM_PORT=${ROCOM_PORT:-8195}
# Web 监听地址
ROCOM_ADDR=${ROCOM_ADDR:-:4939}
# 启用 HTTPS(1=启用)
ROCOM_TLS=${ROCOM_TLS:-}
# SOCKS5 代理(留空=不启用)
ROCOM_SOCKS5_ADDR=${ROCOM_SOCKS5_ADDR:-}
ROCOM_SOCKS5_ALLOW=${ROCOM_SOCKS5_ALLOW:-}
ROCOM_SOCKS5_USER=${ROCOM_SOCKS5_USER:-}
ROCOM_SOCKS5_PASS=${ROCOM_SOCKS5_PASS:-}
ROCOM_SKIP_SELF_IP=${ROCOM_SKIP_SELF_IP:-false}
# 其他透传参数
ROCOM_EXTRA=
EOF
            chmod 600 "$ENV_FILE"
            ENV_AUTO=1
            echo "已从旧进程参数生成 $ENV_FILE"
        fi

        # 安装二进制
        SRC_BIN="$(find_binary)"
        mkdir -p "$INSTALL_DIR"
        echo "安装二进制: $SRC_BIN → $INSTALL_DIR/$BIN_NAME"
        cp -f "$SRC_BIN" "$INSTALL_DIR/$BIN_NAME"
        chmod +x "$INSTALL_DIR/$BIN_NAME"

        write_service
        if [[ "$ENV_AUTO" -eq 0 ]]; then
            write_env  # 没从旧进程提取到参数,用默认模板
        fi

        systemctl daemon-reload
        systemctl enable "$SERVICE_NAME" 2>/dev/null || true

        echo "启动服务..."
        systemctl restart "$SERVICE_NAME"
        sleep 1
        systemctl status "$SERVICE_NAME" --no-pager || true
        echo ""
        echo "==> 迁移完成。"
        echo "    旧目录 $SRC_DIR 可在确认无误后手动删除:"
        echo "      rm -rf $SRC_DIR"
        echo "    数据已迁移到 $DATA_DIR,日志: journalctl -u rocom -f"
        ;;

    backup)
        if [[ ! -f "$DATA_DIR/rocom.db" ]]; then
            echo "数据库不存在: $DATA_DIR/rocom.db" >&2
            exit 1
        fi
        TS="$(date +%Y%m%d-%H%M%S)"
        OUT="$BACKUP_DIR/rocom.db.$TS"
        # 用 sqlite3 的 backup API 做热备份(不锁库);无 sqlite3 命令则直接 cp
        if command -v sqlite3 >/dev/null 2>&1; then
            sqlite3 "$DATA_DIR/rocom.db" ".backup '$OUT'"
        else
            cp "$DATA_DIR/rocom.db" "$OUT"
        fi
        echo "已备份: $OUT ($(du -h "$OUT" | cut -f1))"
        # 保留最近 10 份
        ls -t "$BACKUP_DIR"/rocom.db.* 2>/dev/null | tail -n +11 | xargs -r rm --
        ;;

    uninstall)
        systemctl stop "$SERVICE_NAME" 2>/dev/null || true
        systemctl disable "$SERVICE_NAME" 2>/dev/null || true
        rm -f "$SERVICE_FILE" "$ENV_FILE"
        rm -rf "$INSTALL_DIR"
        systemctl daemon-reload
        if [[ "$PURGE" -eq 1 ]]; then
            rm -rf "$DATA_DIR"
            echo "已卸载程序并清除数据($DATA_DIR)"
        else
            echo "已卸载程序,数据保留在 $DATA_DIR(加 --purge 可同时删除)"
        fi
        ;;
esac
