"""抓取第三方「洛克助手」的大地高清底图(4×4 张 2048² 瓦片,行主序),原样落到
internal/gamedata/data/img/bigmap/<res>_hd/<NN>.webp(编译期 embed),供前端**按视口**
只加载当前需要的那几张。

⚠️ 这份数据**不是自行解包的**,与仓库其余生成物来源不同,单独成脚本正是为了把这件事说清:

- 来源:第三方站点「洛克助手」(http://103.236.77.188:12580,路径 /BigMap/<NN>.webp)。
  它把大地图切成 4×4 共 16 张 2048² 瓦片供 OpenSeadragon 按需加载,行主序编号
  (piece = row*4 + col + 1),与游戏客户端 BigMapUtils 的切分方式一致。
- 与本仓库解包版(gen_bigmap.py 产出的 <res>.webp,4096²)的关系:**同源同投影** ——
  实测逐像素对应(投影系数恰为 2 倍:0.02009493 / 2 = 0.01003922),只是采了 2 倍边长。
- 三处必须知道的差异:
  1. **远海是透明的**:只覆盖大陆与近海(实测仅约 26% 像素不透明),四角为纯透明。
     故保留 alpha、不合成底色 —— 前端把它当「高清叠加层」压在原底图之上,透明处自然
     透出原图(见 web/src/pages/map/useMapEngine.jsx 的 hdTiles)。
  2. **色调比解包版深**:对方素材经过后期处理(对比度更高),切换时大陆会略变暗。
     不做色调对齐 —— 那属于篡改素材,且会在海岸线处引入新的断层。
  3. 只覆盖有底图的场景里实测对得上的那张(res=10003 卡洛西亚大陆);其余场景没有高清版,
     后端 MapImageHD 查不到就返回空、前端不显示开关。

**为什么存瓦片而不是拼成一张 8192² 整图**:整图解码后是 8192×8192×4B = **268MB** 位图,
一次性呈现会把主线程卡住数秒(实测无 GPU 的 headless 下单帧 2.8 秒)。瓦片化后前端只按
视口取需要的几张(默认档实测 2~4 张,放大后 1~2 张),内存与首挂开销都降一个量级。
本脚本因此**不重编码**(直接存对方给的那张瓦片字节):切分与源一一对应,省掉一次有损转换,
总体积也从自编码的 4.62MB 降到 4.1MB。

用法:
    uv run python scripts/fetch_bigmap_hd.py [--base-url URL] [--res 10003] [--force]

依赖网络可达该第三方站点;抓不到会报错退出,不会写出半成品。
"""

import io
import json
import os
import sys
import urllib.request

from PIL import Image

# 对方站点的瓦片根路径。换部署地址时用 --base-url 覆盖。
DEFAULT_BASE = "http://103.236.77.188:12580/BigMap"
SIDE = 4        # 4x4 瓦片
TILE = 2048     # 每张瓦片边长(= 解包版 4096 的两倍)
NAMES = "internal/gamedata/data/names.json"
OUT = "internal/gamedata/data/img/bigmap"


def arg_value(flag, default):
    """取 --flag value 形式的参数值;未给则返回 default。"""
    if flag in sys.argv[1:]:
        i = sys.argv[1:].index(flag)
        return sys.argv[1:][i + 1]
    return default


def fetch(url):
    with urllib.request.urlopen(url, timeout=30) as r:
        return r.read()


def main():
    force = "--force" in sys.argv[1:]
    base = arg_value("--base-url", DEFAULT_BASE).rstrip("/")
    res = arg_value("--res", "10003")

    with open(NAMES, encoding="utf-8") as f:
        m = json.load(f)["maps"].get(res)
    if m is None:
        sys.exit(f"names.json 无该底图: {res}")

    # 本脚本的上一版产出的是拼合整图,已被瓦片取代:留着会两头都 embed(多占约 4.6MB)。
    stale = os.path.join(OUT, f"{res}_hd.webp")
    if os.path.exists(stale):
        os.remove(stale)
        print(f"已删除旧版整图(本轮起改用瓦片):{stale}")

    outdir = os.path.join(OUT, f"{res}_hd")
    want = SIDE * SIDE
    if not force and os.path.isdir(outdir) and len(os.listdir(outdir)) == want:
        print(f"瓦片已齐({want} 张),跳过:{outdir}(--force 强制重抓)")
        return

    print(f"目标 {res}({m['n']}):抓 {SIDE}×{SIDE} 张 {TILE}² 瓦片(行主序)→ {outdir}")
    os.makedirs(outdir, exist_ok=True)
    total = 0
    opaque = 0
    for i in range(1, want + 1):
        name = f"{i:02d}"
        url = f"{base}/{name}.webp"
        try:
            raw = fetch(url)
        except Exception as e:
            sys.exit(f"抓取失败 {url}: {e}")
        with Image.open(io.BytesIO(raw)) as im:
            if im.size != (TILE, TILE):
                sys.exit(f"瓦片尺寸不符 {name}.webp: {im.size},期望 {TILE}²"
                         f"(对方可能换了切分方式,需同步改 SIDE/TILE)")
            opaque += im.convert("RGBA").split()[3].histogram()[255]
        with open(os.path.join(outdir, name + ".webp"), "wb") as f:
            f.write(raw)
        total += len(raw)
        print(f"  {name}.webp {len(raw) / 1024:.0f} KB")

    ratio = opaque / (TILE * TILE * want) * 100
    print(f"-> {outdir}  {want} 张  {total / 1024 / 1024:.2f} MB"
          f"(不透明像素 {ratio:.1f}%,其余透明由前端透出原底图)")
    if ratio < 5:
        print("警告:不透明像素过少,确认抓到的不是一张空白图")


if __name__ == "__main__":
    main()
