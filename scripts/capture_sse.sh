#!/usr/bin/env bash
# 抓一次 pcap 回放期间的 SSE 推送,用于核对推送侧的载荷形状。
#
# 存在理由:前端 [A9] §4 把「SSE 实时刷新」标为未验,而阶段 3 恰好把
# position/wildpets/home/flowers 四个载荷从 map[string]any 改成了 struct。
# REST 侧有 golden 兜底,但**推送侧没有** —— 广播出去的 JSON 形状无人比对。
# 本脚本在回放期间挂上 SSE,把真实推送落盘供核对。
#
# 用法: bash scripts/capture_sse.sh <pcap路径> [输出文件] [端口]
# 注意:回放是一次性的(约 40ms 放完),故必须先挂 SSE 再启动服务。
#
# 端口默认 4940 而非服务默认的 4939:4939 通常是前端 dev server 代理的目标
# (vite 已配好 /api → 4939),占用它会打断前端验收。

set -u
PCAP="${1:?用法: bash scripts/capture_sse.sh <pcap路径> [输出文件] [端口]}"
OUT="${2:-/tmp/sse-capture.txt}"
PORT="${3:-4940}"
DB="$(mktemp /tmp/sse-verify-XXXXXX.db)"

cleanup() { [ -n "${SRV_PID:-}" ] && kill "$SRV_PID" 2>/dev/null; rm -f "$DB" "$DB-wal" "$DB-shm"; }
trap cleanup EXIT

# 1) 先起 SSE 抓取:服务还没监听,故循环重试直到连上
: > "$OUT"
( for _ in $(seq 1 500); do
    curl -sN --max-time 60 "http://127.0.0.1:${PORT}/api/stream" >> "$OUT" 2>/dev/null && break
    sleep 0.05
  done ) &
CURL_PID=$!

# 2) 等抓取端就位,再启动回放(太快会错过开头的推送)
sleep 0.5
"${BIN:-/tmp/rocom-stage3}" -pcap "$PCAP" -db "$DB" -addr ":${PORT}" >/tmp/sse-server.log 2>&1 &
SRV_PID=$!

# 3) 等回放跑完(3.3MB pcap 约 40ms,给足余量)
sleep 8
kill $CURL_PID 2>/dev/null
wait $CURL_PID 2>/dev/null

echo "=== 抓取完成 ==="
echo "字节数: $(wc -c < "$OUT")"
echo "事件数: $(grep -c '^event:' "$OUT")"
echo
echo "=== 各 type 的出现次数 ==="
grep '^event:' "$OUT" | sort | uniq -c | sort -rn
echo
echo "=== data 行的 type 字段分布(推送载荷的真实类型) ==="
grep '^data:' "$OUT" | python3 -c '
import sys, json, collections
c = collections.Counter()
bad = 0
for line in sys.stdin:
    s = line[5:].strip()
    if not s:
        continue
    try:
        c[json.loads(s).get("type", "<无type>")] += 1
    except Exception:
        bad += 1
for k, v in c.most_common():
    print(f"  {v:5d}  {k}")
if bad:
    print(f"  {bad:5d}  <解析失败>")
'
echo
echo "原始输出: $OUT"
