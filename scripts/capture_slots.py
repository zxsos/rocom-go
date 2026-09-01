"""远行商人:定点密集抓取,记录每档商品**首次出现的时间**。

用途:判断 onebiji(快爆工具箱)这个源能否替代/补充现用的 xianyuw API。
背景与结论解读见 `docs/merchant-onebiji-probe.md`。

做法(**不做任何判断,只抓**):
  - 07:58 起每 30 秒抓一次,到 08:07 停,共 19 次;
  - 每次抓完立刻打印:时间 + 该页面里有哪些档期、各档有什么货;
  - 原始页面全存,便于事后 grep 核对。

要回答的问题:08:00 开市后,**08:00-12:00 这一档什么时候第一次出现**?
现用源滞后 6~56 分钟(见 AI_merchant_probe.md),这个源若也是这个量级,
换了等于没换。故需要分钟级甚至秒级的密集采样。

用法:
  uv run python scripts/capture_slots.py                      # 用默认时间窗
  uv run python scripts/capture_slots.py --from 11:58 --to 12:07   # 测 12:00 那档
  uv run python scripts/capture_slots.py --every 10           # 10 秒一次(更密)

输出同时打印到终端与 `~/slots_capture.log`;原始页面存 `~/slots_capture_html/`。
"""
import argparse
import datetime
import os
import re
import sys
import time
import urllib.request

URL = "https://www.onebiji.com/hykb_tools/comm/lkwgmerchant/preview.php?id=1&immgj=0"
SLOT = datetime.timedelta(hours=4)
TZ = datetime.timezone(datetime.timedelta(hours=8))
UA = {"User-Agent": "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 Chrome/120"}
LOG = os.path.expanduser("~/slots_capture.log")
HTML_DIR = os.environ.get(
    "ROCOM_SLOTS_HTML", os.path.expanduser("~/slots_capture_html"))

RE_BLOCK = re.compile(r'<[^>]*class="all_show[^"]*"[^>]*data-time="(\d+)"[^>]*>(.*?)</li>', re.S)
RE_NAME = re.compile(r'class="shop_name[^"]*"[^>]*>([^<]+)<')
RE_PRICE = re.compile(r'class="shop_price[^"]*"[^>]*>([^<]+)<')
RE_LIMIT = re.compile(r'<em>([^<]*)</em>')


def log(msg=""):
    print(msg, flush=True)
    try:
        os.makedirs(os.path.dirname(LOG), exist_ok=True)
        with open(LOG, "a", encoding="utf-8") as f:
            f.write(msg + "\n")
    except OSError:
        pass


def grab(now):
    """抓一次,打印该页面里的全部档期与商品,存原始页面。返回本次抓到的商品数。"""
    try:
        req = urllib.request.Request(URL, headers=UA)
        with urllib.request.urlopen(req, timeout=30) as r:
            raw = r.read()
    except Exception as e:
        log(f"{now:%H:%M:%S}  抓取失败: {e}")
        return None

    try:
        os.makedirs(HTML_DIR, exist_ok=True)
        path = os.path.join(HTML_DIR, f"{now:%H%M%S}.html")
        with open(path, "wb") as f:
            f.write(raw)
    except OSError as e:
        path = f"(保存失败: {e})"

    html = raw.decode("utf-8", "replace")
    slots = {}
    for ts, body in RE_BLOCK.findall(html):
        nm = RE_NAME.search(body)
        if not nm:
            continue
        pr = RE_PRICE.search(body)
        lm = RE_LIMIT.search(body)
        end = datetime.datetime.fromtimestamp(int(ts), TZ)
        slots.setdefault(end, []).append((
            nm.group(1).strip(),
            pr.group(1).replace("价格：", "").strip() if pr else "",
            lm.group(1).strip() if lm else "",
        ))

    log(f"\n----- {now:%Y-%m-%d %H:%M:%S} (UTC+8)  [{path}] -----")
    if not slots:
        log("  (没有解析到商品 —— 站点结构变了,或该时段无数据)")
        return 0
    n = 0
    for end in sorted(slots):
        start = end - SLOT
        log(f"  {start:%m-%d %H:%M} ~ {end:%H:%M}   {len(slots[end])} 件")
        for nm, pr, lm in slots[end]:
            log(f"      {nm:<14} {pr:<12} {lm}")
            n += 1
    return n


def main():
    ap = argparse.ArgumentParser(description="定点密集抓取远行商人档期")
    ap.add_argument("--from", dest="start", default="07:58",
                    help="开始时刻 HH:MM(默认 07:58,即 8 点开市前 2 分钟)")
    ap.add_argument("--to", dest="end", default="08:07",
                    help="结束时刻 HH:MM(默认 08:07)")
    ap.add_argument("--every", type=float, default=30, help="间隔秒数,默认 30")
    a = ap.parse_args()

    today = datetime.datetime.now(TZ).date()
    hh, mm = map(int, a.start.split(":"))
    t0 = datetime.datetime.combine(today, datetime.time(hh, mm), tzinfo=TZ)
    hh, mm = map(int, a.end.split(":"))
    t1 = datetime.datetime.combine(today, datetime.time(hh, mm), tzinfo=TZ)

    log(f"\n########## 定点抓取 {t0:%H:%M:%S} → {t1:%H:%M:%S},"
        f"每 {a.every:g} 秒一次 ##########")
    now = datetime.datetime.now(TZ)
    if now > t1:
        # 静默退出最糟:用户以为跑过了,实则一次都没抓。
        log(f"窗口已过(现在 {now:%H:%M:%S})—— 一次都不会抓。")
        log(f"换时间窗: --from/--to,或只跑单次看当前页面: scripts/probe_onebiji.py")
        return 1
    if now < t0:
        wait = (t0 - now).total_seconds()
        log(f"等待 {wait / 60:.1f} 分钟到 {t0:%H:%M:%S} …(Ctrl+C 可中断)")
        time.sleep(wait)

    n = 0
    while True:
        now = datetime.datetime.now(TZ)
        if now > t1:
            break
        r = grab(now)
        if r is not None:
            n += 1
        nxt = now + datetime.timedelta(seconds=a.every)
        if nxt > t1:
            break
        time.sleep((nxt - datetime.datetime.now(TZ)).total_seconds())

    log(f"\n########## 结束,共抓 {n} 次。日志 {LOG} ##########")
    return 0


if __name__ == "__main__":
    sys.exit(main())
