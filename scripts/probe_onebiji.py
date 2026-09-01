"""探测 onebiji(快爆工具箱)远行商人页面的档期语义。

验证目的:判断这个源能不能替代/补充我们现用的 xianyuw API。

现行方案(internal/server/merchant.go)的痛点是**第三方缓存滞后** ——
轮次整点(8/12/16/20)切换后,货单要滞后 6~56 分钟才从上一轮切成新轮
(实测见 AI_merchant_probe.md),故整点后得每 10 秒回源狂砸。

onebiji 这个源的结构不同(参考 kanzakine/Roco-API 的解析):
页面上每档商品都带 `data-time` = **该档期的结束时间(Unix 秒)**,
且一次给出全天四档。若它在营业时段就能给出「当前进行中的这一档」,
则按时间戳直接取值即可,滞后问题从根上消失。

## 两种模式

**--watch(推荐,可提前跑)**:连续采样,监视页面**何时切换到新的一天**。
不必等到 8 点开市 —— 关键要区分的是两种截然不同的行为:

  A 按天前瞻:8:00 之前页面就换成了「今天全天四档」
             → 开市即有数据,**滞后问题可绕开**
  B 回顾完整营业日:页面始终显示「最近一个已结束的营业日」
             → 8:00 时还显示昨天,要到今天结束才切,**无用**

两者的区别在**日切时刻**就会暴露,而现在(凌晨)正好能观察到:
页面此时显示 09-01,盯着它何时变成 09-02 即可判定。

**--latency(最有价值,要在开市前启动)**:把上面的 A/B 判定**量化成秒数** ——
8:00 开市后多久才能拿到 08:00-12:00 这一档。现用源的痛点是滞后 6~56 分钟,
若这个源也要滞后同样的量,换了等于没换。故这个数字直接决定要不要接。

  uv run python scripts/probe_onebiji.py --latency              # 测今天 8:00 那档
  uv run python scripts/probe_onebiji.py --latency --slot-start 12:00   # 测 12:00 那档

⚠️ 必须**在开市前**启动(默认测 8:00,则 8:00 之前启动)。越过窗口会拒绝并提示 ——
否则会把「测错对象」报成「这个源无价值」这类误导性结论。

**默认(单次)**:抓一次并按「当前时间是否落在某一档内」判定。
⚠️ 休市时段(0:00-8:00)抓到的恒为「昨天全天」,判定必然是 ❌ ——
那不是失败,是抓早了。此模式要在营业时段(8:00 后)跑才有意义。

用法:
  uv run python scripts/probe_onebiji.py --latency              # 量化滞后(8 点前启动!)
  uv run python scripts/probe_onebiji.py --watch                # 每 5 分钟采样,一直跑
  uv run python scripts/probe_onebiji.py --watch --max-runs 3   # 只采 3 次(试跑)
  uv run python scripts/probe_onebiji.py                        # 单次判定(8 点后跑)

日志同时写入 ~/probe_onebiji.log,便于事后回看。

结论出来后本脚本与日志一并删除,或转正为正式的抓取脚本。

📄 **完整背景与判定标准见 `docs/merchant-onebiji-probe.md`** ——
那份文档是给下一个接手的人/AI 的交接说明:为什么要做、现状如何、
怎么解读结果、结论出来后怎么接。改本脚本前请先读它。
"""
import argparse
import datetime
import os
import re
import sys
import time
import urllib.request

URL = "https://www.onebiji.com/hykb_tools/comm/lkwgmerchant/preview.php?id=1&immgj=0"
SLOT = datetime.timedelta(hours=4)      # 每档 4 小时
TZ = datetime.timezone(datetime.timedelta(hours=8))  # 游戏内档期按北京时间
UA = {"User-Agent": "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 Chrome/120"}
LOG = os.path.expanduser("~/probe_onebiji.log")
# 原始页面存档目录 —— 解析结果只是"我们的理解",原始 HTML 才是证据。
# 存档后可用 grep / 浏览器打开回看,验证解析对不对、站点有没有改版。
HTML_DIR = os.environ.get(
    "ROCOM_ONEBIJI_HTML", os.path.expanduser("~/probe_onebiji_html")
)

RE_BLOCK = re.compile(r'<[^>]*class="all_show[^"]*"[^>]*data-time="(\d+)"[^>]*>(.*?)</li>', re.S)
RE_NAME = re.compile(r'class="shop_name[^"]*"[^>]*>([^<]+)<')
RE_PRICE = re.compile(r'class="shop_price[^"]*"[^>]*>([^<]+)<')
RE_LIMIT = re.compile(r'<em>([^<]*)</em>')


def log(msg=""):
    """同时打印到终端与日志(日志用于事后回看,终端用于当场观察)。"""
    print(msg, flush=True)
    try:
        os.makedirs(os.path.dirname(LOG), exist_ok=True)
        with open(LOG, "a", encoding="utf-8") as f:
            f.write(msg + "\n")
    except OSError:
        pass


def fetch(keep=False):
    """抓取页面。keep=True 时连原始 HTML 一起返回:(html, text)。

    单独存一个开关是因为:存档必须保留**做出结论的那一个响应**,
    重新抓一次的话商品可能已经变了,证据就对不上刚打印的结论。
    """
    req = urllib.request.Request(URL, headers=UA)
    with urllib.request.urlopen(req, timeout=30) as r:
        raw = r.read()
    if keep:
        return raw.decode("utf-8", "replace"), raw
    return raw.decode("utf-8", "replace")


def save_html(raw):
    """把原始页面存到 HTML_DIR/YYYYmmdd-HHMMSS.html 并返回路径。

    只在**关键节点**存(单次模式、档期首次出现、档期集合变化),
    不在每次轮询都存 —— 页面 1.1MB,1 分钟一次跑 90 分钟就是 100MB,
    而那些重复页面里没有新信息。
    """
    if not raw:
        return
    try:
        os.makedirs(HTML_DIR, exist_ok=True)
        path = os.path.join(HTML_DIR, f"{datetime.datetime.now(TZ):%Y%m%d-%H%M%S}.html")
        with open(path, "wb") as f:
            f.write(raw)
        log(f"    (原始页面已存: {path})")
    except OSError as e:
        log(f"    (原始页面保存失败: {e})")


def parse(html):
    """返回 [(end_dt, name, price, limit)],按档期结束时间排序。"""
    out = []
    for ts, body in RE_BLOCK.findall(html):
        nm = RE_NAME.search(body)
        if not nm:
            continue
        pr = RE_PRICE.search(body)
        lm = RE_LIMIT.search(body)
        end = datetime.datetime.fromtimestamp(int(ts), TZ)
        out.append((
            end,
            nm.group(1).strip(),
            pr.group(1).replace("价格：", "").strip() if pr else "",
            lm.group(1).strip() if lm else "",
        ))
    return sorted(out)


def group(items):
    """按档期分组:{end_dt: [(name, price, limit), ...]}"""
    g = {}
    for end, nm, pr, lm in items:
        g.setdefault(end, []).append((nm, pr, lm))
    return g


def signature(items):
    """档期集合的可比较签名 —— 只看「有哪些档」,不看档里有什么货。

    用结束时间戳而非商品内容:要监视的是**页面换没换天**,
    商品微调(如价格改一两个)不该触发报警,否则日志全是噪声。
    """
    return tuple(sorted({end for end, _, _, _ in items}))


def fmt_slots(g):
    return ", ".join(f"{(e - SLOT):%m-%d %H:%M}~{e:%H:%M}" for e in sorted(g))


def latency(start_at, interval_min, timeout_min):
    """测「目标档期最早能提前/滞后多久拿到」—— 把 A/B 判定量化成秒数。

    为什么需要它:光知道「页面会不会切到今天」不够。真正决定方案价值的是
    8:00 开市后**多久**能拿到 08:00-12:00 这一档 —— 现用源的痛点是滞后
    6~56 分钟(见 AI_merchant_probe.md),若这个源也要滞后同样的量,
    换了等于没换。

    三个阶段用一个统一的「滞后秒数」表达,正负号即结论:

        负数  开市前就拿到了(页面按天前瞻)→ 不用等,可直接取值
        0~N   开市后 N 秒才拿到         → 与现用源的 6~56 分钟比
        超时  一直没出现               → 这个源给不了当前档

    采样节奏:距开市 >10 分钟时每 5 分钟一次(等待阶段,顺带看会不会提前出现);
    临近开市(<10 分钟)或已过开市则切到 interval(默认 1 分钟),密集捕捉首次出现。
    """
    end_at = start_at + SLOT
    log(f"\n### 滞后测量:目标档期 {start_at:%m-%d %H:%M} ~ {end_at:%H:%M}")
    log(f"### 开市前每 5 分钟采样,临近/过后每 {interval_min:g} 分钟;"
        + f"开市后最多等 {timeout_min:g} 分钟\n")

    deadline = start_at + datetime.timedelta(minutes=timeout_min)
    seen_at = None
    first_poll = True
    while True:
        now = datetime.datetime.now(TZ)
        try:
            # keep=True 先留着原始 HTML:若这就是"首次出现"的那一次,
            # 存档必须是**这一个响应** —— 再抓一次的话,商品可能已经变了,
            # 存下来的证据就对不上刚打印的结论。
            html, items = fetch(keep=True), None
            items = parse(html)
        except Exception as e:
            log(f"[{now:%H:%M:%S}] 抓取失败: {e}")
            html, items = None, []
        if items:
            present = end_at in {e for e, _, _, _ in items}
            if present and seen_at is None:
                seen_at = now
                delta = (now - start_at).total_seconds()
                log(f"[{now:%H:%M:%S}]  *** 目标档期首次出现 ***")
                save_html(html)  # 存**这一个**响应,作为结论的原始证据
                for nm, pr, lm in group(items).get(end_at, []):
                    log(f"          {nm:<14} {pr:<12} {lm}")
                log("")
                if delta < 0:
                    log(f"  >>> 结论:开市前 {-delta / 60:.1f} 分钟就已拿到目标档")
                    # 「至少提前 X 分钟」与「恰好提前 X 分钟」是两回事 ——
                    # 若不注明,会把观测起点当成页面换天的时刻,低估提前量。
                    if first_poll:
                        log("      ⚠ 观测启动时该档已在页面上 —— 真实提前量**可能更大**。")
                        log("        要测准得在页面换天之前就开始(开市前数小时启动,或先跑 --watch)。")
                    log("      → 页面按天前瞻。开市瞬间即可取值,**滞后为 0**。")
                elif delta == 0:
                    log("  >>> 结论:恰好在开市时刻拿到 —— 无滞后。")
                else:
                    mins = delta / 60
                    log(f"  >>> 结论:开市后 {mins:.1f} 分钟才拿到目标档")
                    log(f"      → 与现用源的滞后(6~56 分钟)对比:"
                        + ("更快,值得换/可绕开" if mins < 6
                           else "相当或更慢,**换源无收益**"))
                return 0
            log(f"[{now:%H:%M:%S}] 目标档未出现  "
                + (f"(距开市 {(start_at - now).total_seconds() / 60:.1f} 分钟)"
                   if now < start_at else f"(已开市 {(now - start_at).total_seconds() / 60:.1f} 分钟)"))
        if now > deadline:
            log(f"\n  >>> 结论:开市后 {timeout_min:g} 分钟内**始终没有**出现目标档期")
            log(f"      → 页面没有切换到 {start_at:%m-%d} 的档期。"
                + "若它一直显示昨天,则这个源给不了当前档,**无替换价值**。")
            return 1
        # 临近开市前 10 分钟起转高频;此前低频即可
        first_poll = False
        gap = (start_at - now).total_seconds() / 60
        time.sleep(interval_min * 60 if gap <= 10 or gap < 0 else 5 * 60)


def report(now, items):
    """打印一份完整快照 + 当前档判定。"""
    g = group(items)
    log(f"\n===== 抓取时刻 {now:%Y-%m-%d %H:%M:%S} (UTC+8) =====")
    cur = None
    for end in sorted(g):
        start = end - SLOT
        if start <= now < end:
            state, cur = ">>> 进行中 <<<", end
        elif end <= now:
            state = "已结束"
        else:
            state = "即将开始"
        log(f"\n[{start:%H:%M} - {end:%H:%M}]  {state}  {len(g[end])} 件")
        for nm, pr, lm in g[end]:
            log(f"    {nm:<14} {pr:<12} {lm}")

    log("\n----- 判定 -----")
    if cur:
        log(f"OK  页面**包含当前进行中的档期** ({cur - SLOT:%H:%M}-{cur:%H:%M}),"
            f"共 {len(g[cur])} 件")
        log("    → 可按 data-time 直接取值,不必等第三方缓存切换;滞后问题可绕开。")
    else:
        log("NO  页面**不含当前进行中的档期**")
        log("    → 单次抓无法定论:休市时段(0:00-8:00)本就恒为「昨天全天」。")
        log("      结论要看 --watch 监视到的**换天时刻**。")
    return cur


def watch(interval_min, max_runs):
    """连续采样,监视页面何时切换到新的一天。"""
    log(f"\n### 进入监视模式:每 {interval_min} 分钟采样一次"
        + (f",共 {max_runs} 次" if max_runs else ",Ctrl+C 退出"))
    log("### 只看「档期集合」变化(换天);商品微调不报警。\n")

    prev_sig, prev_g = None, {}
    runs = 0
    try:
        while max_runs is None or runs < max_runs:
            now = datetime.datetime.now(TZ)
            try:
                html, raw = fetch(keep=True)
                items = parse(html)
            except Exception as e:
                log(f"[{now:%H:%M:%S}] 抓取失败: {e}")
                raw = None
                prev_sig = None  # 失败后下次强制打印快照,免得漏掉期间的变化
            else:
                if not items:
                    log(f"[{now:%H:%M:%S}] 解析到 0 件商品 —— 站点结构变了或该时段无数据")
                    prev_sig = None
                else:
                    g, sig = group(items), signature(items)
                    if sig != prev_sig:
                        if prev_sig is None:
                            log(f"[{now:%H:%M:%S}] 初始快照  档期: {fmt_slots(g)}")
                        else:
                            # —— 这就是要等的那一刻 ——
                            log(f"[{now:%H:%M:%S}]  *** 档期集合变化 ***")
                            save_html(raw)  # 存这个响应:换天是本次探测的核心证据
                            log(f"      旧: {fmt_slots(prev_g)}")
                            log(f"      新: {fmt_slots(g)}")
                            newest = max(g)
                            log(f"      最新档期结束于 {newest:%m-%d %H:%M},"
                                f"距现在 {(newest - now).total_seconds() / 3600:+.1f} 小时")
                            log(f"      → {'页面已切到新的一天(A 按天前瞻,可绕开滞后)'
                                       if newest > max(prev_g) else '档期回退了,请人工看一眼'}")
                        for end in sorted(g):
                            names = ", ".join(n for n, _, _ in g[end])
                            log(f"          {fmt_slots({end: 1})}  {names}")
                        prev_sig, prev_g = sig, g
                    else:
                        log(f"[{now:%H:%M:%S}] 无变化  ({fmt_slots(g)})")
            runs += 1
            if max_runs is None or runs < max_runs:
                time.sleep(interval_min * 60)
    except KeyboardInterrupt:
        log("\n### 已停止(用户中断)")
    log(f"### 共采样 {runs} 次")


def main():
    ap = argparse.ArgumentParser(
        description="探测 onebiji 远行商人页面的档期语义(详见 docs/merchant-onebiji-probe.md)")
    ap.add_argument("--watch", action="store_true", help="连续采样监视换天(可提前跑)")
    ap.add_argument("--latency", action="store_true",
                    help="测目标档期最早何时能拿到(量化滞后,默认测今天 8:00 那档)")
    ap.add_argument("--slot-start", metavar="HH:MM", default=None,
                    help="目标档期的开始时刻,默认 08:00(今天)")
    # 两种模式的合理采样间隔不同,各自给默认值(不共用 --interval 的 default):
    # --latency 要捕捉"首次出现"的分钟级差异,默认 1 分钟;
    # --watch 只需观察换天(小时级事件),5 分钟足够、也更省请求。
    ap.add_argument("--interval", type=float, default=None,
                    help="采样间隔(分钟);--latency 默认 1,--watch 默认 5")
    ap.add_argument("--timeout", type=float, default=90,
                    help="--latency 时开市后最多等几分钟,默认 90")
    ap.add_argument("--max-runs", type=int, default=None, help="最多采样几次(不填=一直跑)")
    a = ap.parse_args()

    now = datetime.datetime.now(TZ)
    if a.latency:
        hh, mm = (a.slot_start.split(":") if a.slot_start else ("08", "00"))
        start_at = now.replace(hour=int(hh), minute=int(mm), second=0, microsecond=0)
        # ⚠️ 越过观测窗口就拒绝测量,而不是硬跑出一个结论。
        # 否则会**误导**:目标是 00:00 那档、而现在 01:43 时,页面(显示昨天全天)
        # 当然没有今天 00:00 的档 —— 这会被报成「这个源给不了当前档,无替换价值」,
        # 而真实原因是**测错了对象**。首次出现的时刻早已不可观测,测出来的滞后无意义。
        deadline = start_at + datetime.timedelta(minutes=a.timeout)
        if now > deadline:
            log(f"目标档期 {start_at:%H:%M} 的观测窗口已过"
                f"(现在 {now:%H:%M},窗口到 {deadline:%H:%M})——")
            log("此时测不到「首次出现」的时刻,结论会误导。请:")
            log(f"  · 测今天的 08:00 档 → 在 08:00 **之前**启动(默认值即 08:00);")
            log(f"  · 测后续轮次 → --slot-start 12:00 / 16:00 / 20:00,同样要在开市前启动;")
            log(f"  · 只想看页面当前有什么 → 直接跑不带参数的单次模式。")
            return 1
        return latency(start_at, a.interval or 1, a.timeout)
    if a.watch:
        watch(a.interval or 5, a.max_runs)
        return 0
    try:
        html, raw = fetch(keep=True)
        items = parse(html)
    except Exception as e:
        log(f"抓取失败: {e}")
        return 1
    if not items:
        log("!! 没有解析到任何商品 —— 站点结构变了(选择器失效)或该时段无数据")
        return 1
    save_html(raw)  # 单次模式是人工核对的主要入口,必存
    report(now, items)
    return 0


if __name__ == "__main__":
    sys.exit(main())
