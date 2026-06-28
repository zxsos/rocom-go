"""把 FModel 导出的宠物图标 PNG 转成 webp,落到 internal/gamedata/data/img/(编译期 embed)。

只转**索引(names.json 的 images)实际引用到**且**源里存在**的文件:
- 已上线宠物才有美术资源;未上线条目(如占位的圣草帝魔)源里没有 PNG,自动跳过。
- 仅收录 embed 选定的尺寸:HeadIcon(小头像)/ BigHeadIcon256(大头像)/ Pet256(全身缩略);
  35MB 的 Pet1024 全身大图暂不 embed(详见 docs/data.md),需要时把它加进 DIRS 即可。

前置:在 FModel 里把这些 Icon 子目录以 **PNG** 导出(纹理保存格式设为 PNG),
默认源目录见下方 SRC(可用环境变量 IMG_SRC 覆盖)。运行(需 uv 管理的 pillow):
    uv run python scripts/gen_images.py [PNG源根目录]
"""
import json
import os
import sys

from PIL import Image

# 默认取 FModel 导出的 Common/Icon 根;PNG 与 uasset 同名同目录,只是扩展名不同。
SRC = sys.argv[1] if len(sys.argv) > 1 else os.environ.get(
    "IMG_SRC",
    os.path.expanduser("~/Downloads/NRC/Content/NewRoco/Modules/System/Common/Icon"),
)
NAMES = "internal/gamedata/data/names.json"
OUT = "internal/gamedata/data/img"
QUALITY = 90  # webp 有损质量;UI 图标够用且体积远小于 PNG

# embed 选定的三个尺寸:索引字段 -> (源/目标子目录, 文件名前缀)。
DIRS = {
    "h": ("HeadIcon", ""),
    "b": ("BigHeadIcon256", ""),
    "ps": ("Pet256", "JL_"),
}


def main():
    with open(NAMES, encoding="utf-8") as f:
        images = json.load(f)["images"]

    # 索引引用到的唯一文件:{(子目录, 文件名)}
    need = set()
    for e in images.values():
        for field, (sub, prefix) in DIRS.items():
            if field in e:
                need.add((sub, prefix + e[field]))

    done = {sub: 0 for sub, _ in DIRS.values()}
    skip = {sub: 0 for sub, _ in DIRS.values()}  # 源缺失(多为未上线)
    for sub, name in sorted(need):
        src = os.path.join(SRC, sub, name + ".png")
        if not os.path.exists(src):
            skip[sub] += 1
            continue
        dst_dir = os.path.join(OUT, sub)
        os.makedirs(dst_dir, exist_ok=True)
        with Image.open(src) as im:
            im.save(os.path.join(dst_dir, name + ".webp"), "WEBP", quality=QUALITY, method=6)
        done[sub] += 1

    if not os.path.isdir(SRC):
        sys.exit(f"源目录不存在: {SRC}\n请先在 FModel 里把 Icon 子目录以 PNG 导出,或用 IMG_SRC 指定。")
    total = sum(done.values())
    for sub in done:
        print(f"  {sub}: 转换 {done[sub]}  跳过(源缺失/未上线) {skip[sub]}")
    print(f"-> {OUT}  共 {total} 张 webp")
    if total == 0:
        sys.exit("未转换任何文件:确认源目录是 PNG 导出(不是 .uasset)。")


if __name__ == "__main__":
    main()
