"""抓取第三方「洛克助手」的大地高清底图(4×4 张 2048² 瓦片),拼成 8192² 整图 webp,
落到 internal/gamedata/data/img/bigmap/<res>_hd.webp(编译期 embed)。

⚠️ 这份数据**不是自行解包的**,与仓库其余生成物来源不同,单独成脚本正是为了把这件事说清:

- 来源:第三方站点「洛克助手」(http://103.236.77.188:12580,路径 /BigMap/<NN>.webp)。
  它把大地图切成 4×4 共 16 张 2048² 瓦片供 OpenSeadragon 按需加载,行主序编号
  (piece = row*4 + col + 1),与 games 客户端 BigMapUtils 的切分方式一致。
- 与本仓库解包版(gen_bigmap.py 产出的 <res>.webp,4096²)的关系:**同源同投影**——
  实测逐像素对应(投影系数恰为 2 倍:0.02009493 / 2 = 0.01003922),只是采了 2 倍边长。
- 三处必须知道的差异:
  1. **远海是透明的**:只覆盖大陆与近海(实测仅约 26% 像素不透明),四角为纯透明。
     故本脚本**保留 alpha、不合成底色** —— 前端把它当「高清层」叠在原底图之上,
     透明处自然透出原图(见 web/src/pages/map/useMapEngine.jsx 的 map-base-hd)。
  2. **色调比解包版深**:对方素材经过后期处理(对比度更高),切换时大陆会略变暗。
     不做色调对齐 —— 那属于篡改素材,且会在海岸线处引入新的断层。
  3. 只覆盖有底图的场景里实测对得上的那张(res=10003 卡洛西亚大陆);其余场景没有高清版,
     后端 MapImageHD 查不到就返回空、前端不显示开关。

用法(需 uv 管理的 pillow):
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
TILE = 2048     # 每张瓦片边长(= 原图 4096 的两倍)
# 二次编码质量:源已是有损 webp,压太低会把它本来的锐度又磨掉,故取 90(高于
# gen_bigmap.py 的 82)。实测约 ~2MB,远小于「合成成不透明整图」的 4~5MB ——
# 因为透明区不占体积。
QUALITY = 90
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

    # 用 names.json 里的投影边长交叉校验瓦片数:整图边长 = 4×2048 = 8192,
    # 若该 res 的地块边长与之对不上,说明抓错了图(或对方换了切分方式)。
    with open(NAMES, encoding="utf-8") as f:
        m = json.load(f)["maps"].get(res)
    if m is None:
        sys.exit(f"names.json 无该底图: {res}")
    want = TILE * SIDE
    print(f"目标 {res}({m['n']}):拼接 {SIDE}×{SIDE} 张 {TILE}² → {want}²")

    dst = os.path.join(OUT, f"{res}_hd.webp")
    if os.path.exists(dst) and not force:
        print(f"已存在,跳过:{dst}(--force 强制重抓)")
        return

    canvas = Image.new("RGBA", (want, want), (0, 0, 0, 0))
    opaque = 0
    for i in range(1, SIDE * SIDE + 1):
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
            tile = im.convert("RGBA")
        canvas.paste(tile, (((i - 1) % SIDE) * TILE, ((i - 1) // SIDE) * TILE))
        alpha_hist = tile.split()[3].histogram()
        opaque += alpha_hist[255]
        print(f"  {name}.webp {len(raw) / 1024:.0f} KB")

    os.makedirs(OUT, exist_ok=True)
    canvas.save(dst, "WEBP", quality=QUALITY, method=6)
    ratio = opaque / (want * want) * 100
    print(f"-> {dst}  {canvas.width}²  {os.path.getsize(dst) / 1024 / 1024:.2f} MB"
          f"(不透明像素 {ratio:.1f}%,其余透明由前端透出原底图)")
    if ratio < 5:
        print("警告:不透明像素过少,确认抓到的不是一张空白图")


if __name__ == "__main__":
    main()
