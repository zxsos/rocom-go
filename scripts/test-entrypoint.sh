#!/bin/bash
# docker-entrypoint.sh 的回归测试。
#
# 为什么单独测它:这个脚本干的是「把配置文件组装成启动参数」,而 POSIX sh 里
# 没有数组、函数改不了调用方的位置参数,加上 set -e 与 && 列表的交互极易埋雷 ——
# 比如「只配了部分项」时 [ -n "$v" ] && ... 会返回非零,在 set -e 下直接退出,
# 表现是容器起不来而日志里什么都没有。这些只有真跑一遍才会暴露。
#
# 手法:不真跑 rocom-capture,而是放一个把收到的参数原样打印的 stub 到 PATH,
# 断言的就是「最终交给二进制的那条命令行」。
#
# 断言方式:**比较 flag 集合而非参数顺序**。参数顺序是实现细节(取决于脚本里
# if 的排列),Go flag 解析也不在乎顺序;要守的是「每个 flag 最终取到什么值」。
# 按顺序比对会让改动 if 顺序就红,那是脆弱测试。
#
# 用法: bash scripts/test-entrypoint.sh

set -uo pipefail

ENTRYPOINT="$(cd "$(dirname "$0")/.." && pwd)/docker-entrypoint.sh"
WORK=""
STUB_DIR=""

pass=0
fail=0

setup() {
    WORK=$(mktemp -d)
    STUB_DIR="$WORK/bin"
    mkdir -p "$STUB_DIR"
    # stub:每个参数单独一行,便于解析
    cat > "$STUB_DIR/rocom-capture" <<'EOF'
#!/bin/sh
for a in "$@"; do printf '%s\n' "$a"; done
EOF
    chmod +x "$STUB_DIR/rocom-capture"
    export PATH="$STUB_DIR:$PATH"
    export ROCOM_ENV_FILE="$WORK/rocom.env"
}

teardown() { rm -rf "$WORK"; }

# normalize:把 stub 输出(每行一个参数)归一化成 "flag=value" 列表并排序。
# -tls 是无值的开关,bool flag 特判为真,不去吃下一个参数。
normalize() {
    awk '
    /^--/    { sub(/^--/, "-") }
    /^-tls$/ { print "-tls=true"; next }
    /^-/     {
        if (index($0, "=") > 0) print $0
        else { flag = $0; if ((getline val) > 0) print flag "=" val; else print flag "=" }
    }
    ' | LC_ALL=C sort
}

# check <名> <期望的 flag 集合(每行一条 flag=value)> [命令行参数...]
# 期望值直接写归一化后的形式(flag=value),只排序不再解析 —— 它本来就是人写的常量。
check() {
    local name="$1"; shift
    local expect="$1"; shift
    local actual
    actual=$(sh "$ENTRYPOINT" "$@" 2>/dev/null | normalize)
    local want
    want=$(printf '%s\n' "$expect" | LC_ALL=C sort)
    if [ "$actual" = "$want" ]; then
        pass=$((pass + 1))
        printf '  ✓ %s\n' "$name"
    else
        fail=$((fail + 1))
        printf '  ✗ %s\n' "$name"
        printf '      期望:\n%s\n' "$(printf '%s\n' "$want" | sed 's/^/        /')"
        printf '      实得:\n%s\n' "$(printf '%s\n' "$actual" | sed 's/^/        /')"
    fi
}

# check_fail <名> [命令行参数...]:期望非零退出
check_fail() {
    local name="$1"; shift
    if sh "$ENTRYPOINT" "$@" >/dev/null 2>&1; then
        fail=$((fail + 1)); printf '  ✗ %s(应当失败但成功了)\n' "$name"
    else
        pass=$((pass + 1)); printf '  ✓ %s\n' "$name"
    fi
}

# 默认总会补上 db/cert/key 三项,单独提出来免得每个用例都写一遍
DEFAULTS="-db=/data/rocom.db
-cert=/data/rocom-cert.pem
-key=/data/rocom-key.pem"

echo "=== docker-entrypoint.sh 回归测试 ==="

# --- 1. 纯命令行:原样透传 + 补默认值 ---
setup
printf '# 空配置\n' > "$ROCOM_ENV_FILE"
check "纯命令行透传 + 默认值" "-iface=eth0
$DEFAULTS" -iface eth0
teardown

# --- 2. 命令行优先于配置文件 ---
setup
cat > "$ROCOM_ENV_FILE" <<'EOF'
ROCOM_IFACE=ens17
ROCOM_ADDR=:9999
EOF
# -iface 命令行给了 → 用命令行的 eth0;-addr 没给 → 用配置文件的 :9999
check "命令行压过配置文件" "-iface=eth0
-addr=:9999
$DEFAULTS" -iface eth0
teardown

# --- 3. 配置文件补充命令行没给的项 ---
setup
cat > "$ROCOM_ENV_FILE" <<'EOF'
ROCOM_IFACE=ens17
ROCOM_ADDR=:8080
ROCOM_TLS=1
EOF
check "配置文件补充缺失项" "-iface=ens17
-addr=:8080
-tls=true
$DEFAULTS" -addr :8080 -tls
teardown

# --- 4. 只配了部分项:set -e 下不得误退 ---
# 专门守 [ -n "$v" ] && ... 那个陷阱:只有一个键有值,其余显式留空。
setup
cat > "$ROCOM_ENV_FILE" <<'EOF'
ROCOM_IFACE=ens17
ROCOM_SOCKS5_ADDR=
ROCOM_SOCKS5_PASS=
EOF
check "只配部分项不误退" "-iface=ens17
$DEFAULTS"
teardown

# --- 5. 含空格的值(密码)必须保持为一个参数 ---
setup
cat > "$ROCOM_ENV_FILE" <<'EOF'
ROCOM_IFACE=ens17
ROCOM_SOCKS5_ADDR=:1080
ROCOM_SOCKS5_USER=rocom
ROCOM_SOCKS5_PASS=hello world 123
EOF
check "含空格的密码不分家" "-iface=ens17
-socks5-addr=:1080
-socks5-user=rocom
-socks5-pass=hello world 123
-skip-self-ip=false
$DEFAULTS"
teardown

# --- 6. socks5 启用时 -skip-self-ip 默认 false ---
# 这是最容易踩的坑:不设 false,代理进程以本机 IP 出站的流量被单臂去重丢掉,
# 表现是「代理连上了但一个包都抓不到」。
setup
cat > "$ROCOM_ENV_FILE" <<'EOF'
ROCOM_IFACE=ens17
ROCOM_SOCKS5_ADDR=:1080
EOF
check "socks5 模式默认 -skip-self-ip=false" "-iface=ens17
-socks5-addr=:1080
-skip-self-ip=false
$DEFAULTS"
teardown

# --- 7. 显式配置优先于 socks5 的 false 默认值 ---
setup
cat > "$ROCOM_ENV_FILE" <<'EOF'
ROCOM_IFACE=ens17
ROCOM_SOCKS5_ADDR=:1080
ROCOM_SKIP_SELF_IP=true
EOF
check "显式 SKIP_SELF_IP 优先" "-iface=ens17
-socks5-addr=:1080
-skip-self-ip=true
$DEFAULTS"
teardown

# --- 8. 非 socks5 场景不加 -skip-self-ip(沿用二进制默认 true) ---
setup
cat > "$ROCOM_ENV_FILE" <<'EOF'
ROCOM_IFACE=ens17
EOF
check "非 socks5 不加 skip-self-ip" "-iface=ens17
$DEFAULTS"
teardown

# --- 9. 注释行不得被当成配置 ---
setup
cat > "$ROCOM_ENV_FILE" <<'EOF'
# ROCOM_IFACE=注释里的假值
ROCOM_IFACE=ens17
EOF
check "跳过注释行(注释在前)" "-iface=ens17
$DEFAULTS"
teardown

# --- 9b. 注释在配置**之后**同样要忽略 ---
# 这一条专门守 env_get 里「先去注释再 tail -1」的顺序:只靠取最后一个匹配的话,
# 注释写在配置后面就会被取中,拿说明文字去当参数(变异测试曾在此处漏网)。
setup
cat > "$ROCOM_ENV_FILE" <<'EOF'
ROCOM_IFACE=ens17
# ROCOM_IFACE=注释里的假值
EOF
check "跳过注释行(注释在后)" "-iface=ens17
$DEFAULTS"
teardown

# --- 10. --flag 双横线写法同样识别 ---
setup
cat > "$ROCOM_ENV_FILE" <<'EOF'
ROCOM_IFACE=ens17
EOF
check "--iface 归一化(配置不重复追加)" "-iface=eth0
$DEFAULTS" --iface eth0
teardown

# --- 11. 带引号的值 ---
setup
cat > "$ROCOM_ENV_FILE" <<'EOF'
ROCOM_IFACE=ens17
ROCOM_SOCKS5_PASS="quoted pass"
EOF
check "去首尾配对引号" "-iface=ens17
-socks5-pass=quoted pass
$DEFAULTS"
teardown

# --- 12. -pcap 模式也算 mode ---
setup
cat > "$ROCOM_ENV_FILE" <<'EOF'
ROCOM_PCAP=/pcap/x.pcap
EOF
check "pcap 模式来自配置文件" "-pcap=/pcap/x.pcap
$DEFAULTS"
teardown

# --- 13. 缺 mode 必须报错退出 ---
setup
: > "$ROCOM_ENV_FILE"
check_fail "缺 mode 时报错"
check_fail "缺 mode(有其它参数)时报错" -addr :4939
teardown

# --- 14. 配置文件不存在时自动创建(否则面板只读) ---
setup
rm -f "$ROCOM_ENV_FILE"
sh "$ENTRYPOINT" -iface eth0 >/dev/null 2>&1
if [ -f "$ROCOM_ENV_FILE" ]; then
    pass=$((pass + 1)); printf '  ✓ 自动创建配置文件\n'
else
    fail=$((fail + 1)); printf '  ✗ 自动创建配置文件(文件不存在)\n'
fi
teardown

# --- 15. ROCOM_DB / CERT / KEY 可覆盖默认值 ---
setup
cat > "$ROCOM_ENV_FILE" <<'EOF'
ROCOM_IFACE=ens17
ROCOM_DB=/data/custom.db
ROCOM_CERT=/data/c.pem
ROCOM_KEY=/data/k.pem
EOF
check "db/cert/key 可覆盖" "-iface=ens17
-db=/data/custom.db
-cert=/data/c.pem
-key=/data/k.pem"
teardown

# --- 16. 配置文件里的 mode 也能通过校验(mode 可来自文件) ---
setup
cat > "$ROCOM_ENV_FILE" <<'EOF'
ROCOM_IFACE=ens17
EOF
if sh "$ENTRYPOINT" >/dev/null 2>&1; then
    pass=$((pass + 1)); printf '  ✓ mode 来自配置文件时通过校验\n'
else
    fail=$((fail + 1)); printf '  ✗ mode 来自配置文件时通过校验(被误判为缺 mode)\n'
fi
teardown

echo ""
echo "=== 结果: $pass 通过, $fail 失败 ==="
[ "$fail" -eq 0 ]
