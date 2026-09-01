"""抓取 wiki 每只精灵图鉴页的「特性名 + 特性描述」,存到 ~/Downloads/rocom/features_raw.jsonl。

为什么抓这个:草系试炼等只给特性 id(288xxx),项目此前没有特性名表。
wiki 没有集中的「特性图鉴」页 —— 特性数据是**逐只精灵**写在图鉴页 wikitext 里的
(`{{精灵信息/兼容|特性=助燃|特性描述=...}}`)。所以要全量特性词典,就得先枚举
全部精灵页,再逐页取这两个字段。

两个数据来源(都是页面 HTML 或 MediaWiki API,无需登录):

  1. 精灵页列表:站内全文搜索 `insource:"精灵信息/兼容"`(能搜到该模板的页面
     全是精灵图鉴页),结果页 HTML 每 50 条一页,offset 分页。
     走 searchwiki.biligame.com 的**搜索页 HTML** 而不是 api.php —— 该子域不
     受限流,而 api.php 连续调用会返回 567。
  2. 页面内容:MediaWiki `action=query&prop=revisions&rvprop=content&rvslots=main`,
     **一次最多取 50 个页面**的 wikitext(726 只精灵约 15 次请求),正则提取
     `|特性=` 与 `|特性描述=`。

数据来源: https://wiki.biligame.com/rocom/草系徽章试炼 → 精灵图鉴系列页
授权: CC BY-NC-SA 4.0(站点声明)。生成物里会标注来源与更新时间。

运行:  uv run python scripts/fetch_features.py
输出:  ROCOM_FEATURES_RAW,默认 ~/Downloads/rocom/features_raw.jsonl
       (随后跑 scripts/gen_features.py 生成 internal/gamedata/data/features.json)

注意:特性只有名字,没有 id(协议里的 288xxx 是抓包侧的编号)。
     id→名字 的桥接靠「试炼抓包里宠物与其天生特性同时出现」+「本词典里
     该宠物的特性名」间接建立,见 docs/data.md 与 gen_features.py。
"""
import json
import os
import re
import sys
import time
import urllib.error
import urllib.parse
import urllib.request

SEARCH_URL = "https://searchwiki.biligame.com/rocom/index.php"
API_URL = "https://wiki.biligame.com/rocom/api.php"
SEARCH_TERM = 'insource:"精灵信息/兼容"'
OUT_PATH = os.environ.get(
    "ROCOM_FEATURES_RAW", os.path.expanduser("~/Downloads/rocom/features_raw.jsonl")
)
PAGE_SIZE = 50        # 搜索结果每页条数
BATCH = 50            # 一次 revisions 请求取的页面数
SLEEP = 1.0           # 请求间隔,别把 wiki 打挂
UA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36"

# 搜索结果页:只取 <ul class='mw-search-results'> 容器里的条目,避免命中导航栏链接。
RE_RESULTS = re.compile(r"<ul class=['\"]mw-search-results['\"]>(.*?)</ul>", re.S)
RE_TITLE = re.compile(r'title="([^"]+)" data-serp-pos="\d+"')
RE_FEATURE = re.compile(r"^\|特性=([^\n|]*)", re.M)
RE_DESC = re.compile(r"^\|特性描述=(.*?)(?=^\||^}})", re.M | re.S)


def get(url):
    req = urllib.request.Request(url, headers={"User-Agent": UA})
    with urllib.request.urlopen(req, timeout=40) as r:
        return r.read().decode("utf-8", "replace")


def fetch_pet_pages():
    """抓全部精灵页名(去重保序)。返回 [] 说明站点改版。"""
    pages, offset = [], 0
    while True:
        url = (
            f"{SEARCH_URL}?search={urllib.parse.quote(SEARCH_TERM)}"
            f"&fulltext=1&limit={PAGE_SIZE}&offset={offset}"
        )
        html = get(url)
        m = RE_RESULTS.search(html)
        got = RE_TITLE.findall(m.group(1)) if m else []
        if not got:
            break
        for t in got:
            if t not in pages:
                pages.append(t)
        print(f"  列表第 {offset // PAGE_SIZE + 1} 页: {len(got)} 个页面,累计 {len(pages)}")
        if len(got) < PAGE_SIZE:
            break
        offset += PAGE_SIZE
        time.sleep(SLEEP)
        if offset > 5000:  # 兜底
            break
    return pages


def fetch_wikitext_batch(titles):
    """一次取最多 BATCH 个页面的 wikitext,返回 {标题: 文本}。

    用 POST 而不是 GET:api.php 的 GET 会被 CDN 限流(HTTP 567),POST 实测稳定。
    """
    data = urllib.parse.urlencode(
        {
            "action": "query",
            "prop": "revisions",
            "rvprop": "content",
            "rvslots": "main",
            "format": "json",
            "formatversion": "2",
            "titles": "|".join(titles),
        }
    ).encode()
    req = urllib.request.Request(
        API_URL,
        data=data,
        headers={
            "User-Agent": UA,
            "Content-Type": "application/x-www-form-urlencoded",
        },
    )
    with urllib.request.urlopen(req, timeout=60) as r:
        raw = json.load(r)
    out = {}
    for p in raw.get("query", {}).get("pages", []):
        revs = p.get("revisions") or [{}]
        content = (revs[0].get("slots") or {}).get("main", {}).get("content", "")
        out[p.get("title", "")] = content
    return out


def clean_text(s):
    """去掉 wikitext 里的模板调用、链接、注释,尽量留纯文字。"""
    s = re.sub(r"<!--.*?-->", "", s, flags=re.S)
    s = re.sub(r"\{\{[^{}]*\}\}", "", s)
    s = re.sub(r"\[\[([^\]|]*\|)?([^\]]*)\]\]", r"\2", s)
    s = re.sub(r"'''", "", s)
    return " ".join(s.split()).strip()


def parse_pet(wt):
    """从精灵页 wikitext 提取特性名与描述。无特性字段返回 None。"""
    fm = RE_FEATURE.search(wt)
    if not fm:
        return None
    name = clean_text(fm.group(1))
    dm = RE_DESC.search(wt)
    desc = clean_text(dm.group(1)) if dm else ""
    return {"feature": name, "feature_desc": desc}


def main():
    print("第 1 步:枚举精灵图鉴页面 …")
    try:
        pages = fetch_pet_pages()
    except urllib.error.URLError as e:
        sys.exit(f"列表请求失败: {e}")
    if not pages:
        sys.exit("一个精灵页都没列出来 —— 搜索页结构可能改版了,请检查 RE_RESULTS/RE_TITLE 正则。")
    print(f"共 {len(pages)} 个精灵图鉴页面")

    # 断点续传:已处理过的页面跳过
    done = set()
    if os.path.exists(OUT_PATH):
        with open(OUT_PATH, encoding="utf-8") as f:
            for line in f:
                try:
                    done.add(json.loads(line)["page"])
                except (json.JSONDecodeError, KeyError):
                    pass
    todo = [p for p in pages if p not in done]
    print(f"已抓 {len(done)} 页,剩余 {len(todo)} 页")

    os.makedirs(os.path.dirname(OUT_PATH), exist_ok=True)
    fail = 0
    with open(OUT_PATH, "a", encoding="utf-8") as f:
        for i in range(0, len(todo), BATCH):
            batch = todo[i : i + BATCH]
            try:
                wts = fetch_wikitext_batch(batch)
            except urllib.error.URLError as e:
                print(f"  批 {i // BATCH + 1} 请求失败: {e}(可重跑续传)", file=sys.stderr)
                time.sleep(5)
                fail += 1
                continue
            n_ok = 0
            for t, wt in wts.items():
                got = parse_pet(wt)
                if got is None:
                    continue
                f.write(json.dumps({"page": t, **got}, ensure_ascii=False) + "\n")
                n_ok += 1
            f.flush()
            print(f"  批 {i // BATCH + 1}: {len(batch)} 页,其中 {n_ok} 页有特性字段,累计 {len(done) + i + len(batch)}/{len(pages)}")
            time.sleep(SLEEP)

    if fail and fail >= (len(todo) + BATCH - 1) // BATCH:
        sys.exit(f"全部 {fail} 批请求都失败 —— wiki API 可能还在限流,过几分钟重跑续传即可。")

    # 汇总统计
    names, pairs = set(), {}
    with open(OUT_PATH, encoding="utf-8") as f:
        for line in f:
            try:
                r = json.loads(line)
            except json.JSONDecodeError:
                continue
            if r.get("feature"):
                names.add(r["feature"])
                pairs.setdefault(r["feature"], set()).add(r["page"])
    if not names:
        sys.exit("抓到了页面但没有特性字段 —— 精灵页模板可能改版,请检查 RE_FEATURE/RE_DESC 正则。")
    print(f"完成: {len(pairs)} 个不同特性名(来自 {len(done) + len(todo)} 个页面),已写 {OUT_PATH}")
    print("下一步: uv run python scripts/gen_features.py")


if __name__ == "__main__":
    main()
