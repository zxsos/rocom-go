"""抓取「技能 id → 技能中文名」映射,存到 ~/Downloads/rocom/skillIds.json。

为什么单独一个抓取脚本:技能 id(7020500 这类 base_skill_id)是**协议里的编号**,
只能从资料站的技能图鉴页逐页刮出来 —— 站点没有公开的 JSON 接口(试过
lib/roco/data/skills.json 等路径均 404),只有服务端渲染的分页列表,每页 24 条。

这一份映射是**关键**:有了它,草系试炼里那些裸技能 id 才能显示中文名。
第三方资料站(arkmeng.cn)的技能数据虽然字段更全(威力/能耗/效果/天生精灵),
但**没有技能 id**,只能按「形态+学会等级」间接组织,无法反查试炼技能
(且试炼技能会融合、威力被改写,按数值反推会命中错误技能)。

数据来源: https://aismile.dev/zh-hans/roco-tools/skills (玩家整理的第三方资料站,
页面明确声明与游戏官方无从属关系,仅供参考)。

运行:  uv run python scripts/fetch_skill_ids.py
输出:  ROCOM_SKILL_IDS 环境变量,默认 ~/Downloads/rocom/skillIds.json
       (随后跑 scripts/gen_skills.py 把它并进 internal/gamedata/data/skills.json)

站点改版时:本脚本靠 HTML 的 class 定位字段,改版后会抓不到(id 数为 0 会报错退出),
此时按新的 HTML 结构更新下面三个正则即可。
"""
import json
import os
import re
import sys
import time
import urllib.error
import urllib.request

BASE_URL = "https://aismile.dev/zh-hans/roco-tools/skills"
OUT_PATH = os.environ.get(
    "ROCOM_SKILL_IDS", os.path.expanduser("~/Downloads/rocom/skillIds.json")
)
PAGE_SIZE = 24          # 站点每页固定 24 条(?pageSize= 无效,实测仍返回 24)
SLEEP = 1.2             # 礼貌抓取:26 页约 30 秒

# 三个字段的定位(class 名来自站点当前的 Tailwind 写法)
RE_ID = re.compile(r'tracking-\[0\.24em\][^>]*>(\d{7})</div>')
RE_NAME = re.compile(r'<h2 class="truncate text-2xl font-black[^>]*>([^<]+)</h2>')
RE_CAT = re.compile(r'rounded-full bg-\[var\(--roco-gold\)\]/12[^>]*>([^<]+)</span>')


def fetch_page(page):
    url = f"{BASE_URL}?page={page}"
    req = urllib.request.Request(url, headers={"User-Agent": "Mozilla/5.0"})
    with urllib.request.urlopen(req, timeout=25) as r:
        return r.read().decode("utf-8", "replace")


def parse_page(html):
    """按「第 i 个 id 对应第 i 个名字/分类」配对。

    三个字段在 HTML 里是各自独立成串的(没有包在同一个父节点下),故只能靠
    **顺序**配对 —— 页面渲染顺序恒为 id → 名字 → 分类,实测 26 页都对得上。
    这是脆弱点,故下面用「三者数量一致」做校验,不一致就报错而非静默错位。
    """
    ids = RE_ID.findall(html)
    names = RE_NAME.findall(html)
    cats = RE_CAT.findall(html)
    if not (len(ids) == len(names) == len(cats)):
        raise ValueError(
            f"字段数量对不上: id={len(ids)} name={len(names)} cat={len(cats)} "
            "—— 站点可能改版,请更新正则"
        )
    return [
        {"skillId": int(i), "name": n, "category": c}
        for i, n, c in zip(ids, names, cats)
    ]


def main():
    skills, page = {}, 1
    while True:
        try:
            got = parse_page(fetch_page(page))
        except urllib.error.URLError as e:
            print(f"第 {page} 页请求失败: {e}", file=sys.stderr)
            break
        if not got:
            break
        for s in got:
            skills[s["skillId"]] = s
        print(f"  第 {page} 页: {len(got)} 条,累计 {len(skills)}")
        if len(got) < PAGE_SIZE:  # 末页
            break
        page += 1
        time.sleep(SLEEP)
        if page > 100:  # 兜底,防死循环
            break

    if not skills:
        sys.exit(
            "一个技能都没抓到 —— 站点很可能改版了。\n"
            "请打开 https://aismile.dev/zh-hans/roco-tools/skills 看一下,"
            "并更新本脚本里的三个正则(RE_ID / RE_NAME / RE_CAT)。"
        )

    os.makedirs(os.path.dirname(OUT_PATH), exist_ok=True)
    with open(OUT_PATH, "w", encoding="utf-8") as f:
        json.dump(
            {
                "_source": f"{BASE_URL} (第三方资料站,玩家整理,仅供参考)",
                "_note": "技能 id(base_skill_id) -> 技能中文名;用于试炼等只给 id 的场景",
                "skills": [skills[k] for k in sorted(skills)],
            },
            f,
            ensure_ascii=False,
            indent=1,
        )
    print(f"已写入 {OUT_PATH}: {len(skills)} 个技能")


if __name__ == "__main__":
    main()
