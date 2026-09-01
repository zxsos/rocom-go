"""一次性探测:onebiji(快爆工具箱)远行商人页面的档期语义。

验证目的:判断这个源能不能替代/补充我们现用的 xianyuw API。

现行方案(internal/server/merchant.go)的痛点是**第三方缓存滞后** ——
轮次整点(8/12/16/20)切换后,货单要滞后 6~56 分钟才从上一轮切成新轮
(实测见 AI_merchant_probe.md),故整点后得每 10 秒回源狂砸。

onebiji 这个源的结构不同(参考 kanzakine/Roco-API 的解析):
页面上每档商品都带 `data-time` = **该档期的结束时间(Unix 秒)**,
且一次给出全天四档。若它在营业时段就能给出「当前进行中的这一档」,
则按时间戳直接取值即可,滞后问题从根上消失。

**待验证的前提**:营业时段抓取时,页面是否已包含当前档(而非只给已结束的)。
凌晨(0:00-8:00 休市)抓到的恒为「刚结束营业日全天」,验证不了这一点,
故必须在营业时段(如 08:05)抓一次。

用法:
  uv run python scripts/probe_onebiji.py            # 抓一次并打印
  uv run python scripts/probe_onebiji.py >> log     # 追加到日志(自动化用)

结论出来后本脚本与日志一并删除,或转正为正式的抓取脚本。
"""
import datetime
import re
import sys
import urllib.request

URL = "https://www.onebiji.com/hykb_tools/comm/lkwgmerchant/preview.php?id=1&immgj=0"
SLOT = datetime.timedelta(hours=4)      # 每档 4 小时
TZ = datetime.timezone(datetime.timedelta(hours=8))  # 游戏内档期按北京时间
UA = {"User-Agent": "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 Chrome/120"}

RE_BLOCK = re.compile(r'<[^>]*class="all_show[^"]*"[^>]*data-time="(\d+)"[^>]*>(.*?)</li>', re.S)
RE_NAME = re.compile(r'class="shop_name[^"]*"[^>]*>([^<]+)<')
RE_PRICE = re.compile(r'class="shop_price[^"]*"[^>]*>([^<]+)<')
RE_LIMIT = re.compile(r'<em>([^<]*)</em>')


def fetch():
    req = urllib.request.Request(URL, headers=UA)
    with urllib.request.urlopen(req, timeout=30) as r:
        return r.read().decode("utf-8", "replace")


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


def main():
    now = datetime.datetime.now(TZ)
    print(f"\n===== 抓取时刻 {now:%Y-%m-%d %H:%M:%S} (UTC+8) =====")
    try:
        html = fetch()
    except Exception as e:
        print(f"抓取失败: {e}")
        return 1
    items = parse(html)
    if not items:
        print("!! 没有解析到任何商品 —— 站点结构变了(选择器失效)或该时段无数据")
        return 1

    # 按档期分组
    slots = {}
    for end, nm, pr, lm in items:
        slots.setdefault(end, []).append((nm, pr, lm))

    cur_slot = None
    for end in sorted(slots):
        start = end - SLOT
        if start <= now < end:
            state, cur_slot = ">>> 进行中 <<<", end
        elif end <= now:
            state = "已结束"
        else:
            state = "即将开始"
        print(f"\n[{start:%H:%M} - {end:%H:%M}]  {state}  {len(slots[end])} 件")
        for nm, pr, lm in slots[end]:
            print(f"    {nm:<14} {pr:<12} {lm}")

    # —— 结论:这就是要验证的那个前提 ——
    print("\n----- 判定 -----")
    if cur_slot:
        print(f"✅ 页面**包含当前进行中的档期** ({cur_slot - SLOT:%H:%M}-{cur_slot:%H:%M}),"
              f"共 {len(slots[cur_slot])} 件")
        print("   → 可按 data-time 直接取值,不必等第三方缓存切换;滞后问题可绕开。")
    else:
        print("❌ 页面**不含当前进行中的档期** —— 只能拿到已结束(或尚未开始)的档。")
        print("   → 这个源无法解决滞后问题,价值仅限「当天回顾」。")
    return 0


if __name__ == "__main__":
    sys.exit(main())
