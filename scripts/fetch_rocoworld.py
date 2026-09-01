"""抓取 roco.world 精灵图鉴的结构化数据,存到 ~/Downloads/rocom/rocoworld_raw.jsonl。

为什么需要它:wiki(biligame)的「精灵 → 特性」表是**按精灵名字**建的键,
我方只能靠形态全名(名 + 全角括号的形态后缀)去反查 —— 口径差一个字符就
静默查不到,实测 640 个候选里有 168 个(26%)查不到特性名。

roco.world 的图鉴页内嵌一份 SSR 的 application/json,里面有:

    page.data.petbase_id        —— **与我方 petbase id 完全一致**(实测学院呱呱 3620)
    page.data.handbook_id       —— 图鉴号,同样与我方 b 字段一致
    page.data.passive_skills[]  —— 特性 [{id, name, description}]

于是可以直接**按 petbase_id 建索引**,不再依赖名字匹配 —— 覆盖率和可靠性
都比 wiki 那份高。附带还能拿到种族值、身高体重、系别、进化阶段、蛋组、技能表。

注意:特性 id 在这里是 200xxx 段(如「留学生」=200252),**与协议里的 288xxx
不是同一套编号**,两者无法直接换算。本脚本取的是「精灵 → 特性名」这层关系 ——
那正是桥接需要的,288xxx 的绑定仍走标注或试炼抓包(见 docs/data.md「特性名」)。

数据来源: https://roco.world/zh/jini/<图鉴号> (非官方玩家站,页面声明与
腾讯无关联)。URL 清单从 sitemap-zh-hans-jini.xml 取,不硬编码 442 这个数 ——
站点加新精灵时 sitemap 会自动带上。

运行:  uv run python scripts/fetch_rocoworld.py
输出:  ROCOM_ROCOWORLD_RAW,默认 ~/Downloads/rocom/rocoworld_raw.jsonl
       (随后跑 scripts/gen_features.py 把它并进 features.json)

站点改版时:本脚本靠 <script type="application/json"> 提取,改版后拿不到
(每页解析失败会报错退出),此时按新的结构更新 RE_JSON / 字段路径即可。
"""
import concurrent.futures
import json
import os
import re
import sys
import time
import urllib.request

SITEMAP = "https://roco.world/sitemap-zh-hans-jini.xml"
BASE = "https://roco.world"
OUT_PATH = os.environ.get(
    "ROCOM_ROCOWORLD_RAW", os.path.expanduser("~/Downloads/rocom/rocoworld_raw.jsonl")
)
CONCURRENCY = 4     # 礼貌抓取:实测 4 并发下每页约 1 秒,594 页约 3 分钟
RETRIES = 3

RE_LOC = re.compile(r"<loc>([^<]+)</loc>")
RE_JSON = re.compile(
    r'<script[^>]*type="application/json"[^>]*>(.*?)</script>', re.S
)
UA = {"User-Agent": "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 Chrome/120"}


def http_get(url, timeout=30):
    for i in range(RETRIES):
        try:
            req = urllib.request.Request(url, headers=UA)
            with urllib.request.urlopen(req, timeout=timeout) as r:
                return r.read().decode("utf-8", "replace")
        except Exception as e:
            if i == RETRIES - 1:
                raise
            time.sleep(1.5 * (i + 1))
    return None  # unreachable


def fetch_urls():
    """从 sitemap 取全部精灵页 URL(含形态页 /form/N)。"""
    xml = http_get(SITEMAP)
    urls = [u for u in RE_LOC.findall(xml) if "/zh/jini/" in u]
    if not urls:
        sys.exit("sitemap 里没有 /zh/jini/ 的 URL —— 站点结构变了,"
                 f"请手工看一下 {SITEMAP}")
    return urls


def parse_page(url, html):
    """提取一只形态的字段。返回 None 表示该页不是精灵页(结构变了)。"""
    m = RE_JSON.search(html)
    if not m:
        return None
    try:
        data = json.loads(m.group(1))
    except json.JSONDecodeError:
        return None
    pd = (data.get("page") or {}).get("data") or {}
    if not pd.get("petbase_id"):
        return None
    return {
        "url": url,
        "petbase_id": pd.get("petbase_id"),
        "handbook_id": pd.get("handbook_id"),
        "name": pd.get("name"),
        "form": pd.get("form"),
        "evolution_stage": pd.get("evolution_stage"),
        "types": pd.get("unit_types") or [],
        "stats": pd.get("stats"),
        "height_m": (pd.get("physical_profile") or {}).get("height_m"),
        "weight_kg": (pd.get("physical_profile") or {}).get("weight_kg"),
        "egg_groups": pd.get("egg_groups"),
        "passive_skills": [
            {"id": p.get("id"), "name": p.get("name"), "desc": p.get("description")}
            for p in (pd.get("passive_skills") or [])
            if p.get("name")
        ],
        # 技能表(升级/技能石/血脉),含 7xxxxx 的技能 id —— 与 skills.json 同格式
        "skills": [
            {
                "id": s.get("id"), "name": s.get("name"),
                "source": s.get("source_type"), "level": s.get("unlock_level"),
            }
            for s in (pd.get("skills") or [])
            if s.get("id")
        ],
    }


def main():
    os.makedirs(os.path.dirname(OUT_PATH), exist_ok=True)

    # 增量:已有的 URL 跳过(按 petbase_id + form 记,形态页也算独立一条)
    done = set()
    if os.path.exists(OUT_PATH):
        with open(OUT_PATH, encoding="utf-8") as fh:
            for line in fh:
                try:
                    done.add(json.loads(line)["url"])
                except Exception:
                    pass

    urls = fetch_urls()
    todo = [u for u in urls if u not in done]
    print(f"sitemap {len(urls)} 个 URL,已完成 {len(done)} 个,待抓 {len(todo)} 个")
    if not todo:
        print("没有新增,跳过。删掉 jsonl 可强制重抓。")
        return

    t0 = time.time()
    ok, failed = 0, []

    def work(url):
        try:
            return url, parse_page(url, http_get(url))
        except Exception as e:
            return url, ("ERR", str(e))

    with open(OUT_PATH, "a", encoding="utf-8") as out, \
            concurrent.futures.ThreadPoolExecutor(CONCURRENCY) as ex:
        for i, (url, rec) in enumerate(ex.map(work, todo), 1):
            if isinstance(rec, tuple):  # 出错
                failed.append((url, rec[1]))
                continue
            if rec is None:
                failed.append((url, "页面结构不符(拿不到 page.data)"))
                continue
            out.write(json.dumps(rec, ensure_ascii=False) + "\n")
            ok += 1
            if i % 50 == 0:
                el = time.time() - t0
                print(f"  {i}/{len(todo)}  用时 {el:.0f}s  预计剩 "
                      f"{(len(todo) - i) * el / i:.0f}s", flush=True)

    print(f"\n完成 {ok} 个,失败 {len(failed)} 个,用时 {time.time() - t0:.0f}s")
    for url, why in failed[:10]:
        print(f"  失败 {url}: {why}")
    # 失败过多说明站点改版了,别静默产出一份残缺的数据
    if ok == 0 or len(failed) > len(todo) * 0.1:
        sys.exit(f"\n失败率过高({len(failed)}/{len(todo)}) —— 站点结构多半变了,"
                 f"请手工核对 {BASE}/zh/jini/375 后更新本脚本的解析逻辑。")


if __name__ == "__main__":
    main()
