"""生成 internal/gamedata/data/features.json —— 特性名与「精灵 → 特性」索引。

特性(288xxx)与技能(7020500)一样是协议里的**全局编号**,但项目此前没有特性名表
(技能有 skills.json 过渡方案)。特性名+描述只能从外部资料站抓。

本脚本产出三块:

  features:     特性名 -> {描述, 拥有该特性的精灵}。这是「标注候选库」的词典:
                玩家在 web 端搜名字/描述,选中后把协议里的 288xxx id 与名字绑起来。
  pet_feature:  精灵页名 -> 特性名。**旧接口,按名字建键**,为兼容保留 ——
                新代码请一律用 petbase_feature(见下)。
  petbase_feature: petbase_id -> 特性名。**新接口,按 id 建键**,首选。

为什么要有两份「精灵 → 特性」:

  pet_feature 是 wiki(biligame)抓的,键是**精灵页名**("鸭吉吉（蓬松的样子）")。
  我方只能靠形态全名去反查,而形态全名要拼「名 + 全角括号的形态后缀」——
  口径差一个字符就静默查不到。实测覆盖率只有 74%,且有 8 处**抄串**
  (女王蜂与花魁蜂后的特性对调了,见下方 merge 处的说明)。

  petbase_feature 来自 roco.world,它的页面数据里带 **petbase_id**,
  与我方解包出的 id **完全一致**(实测 594/594 对得上,学院呱呱 3620 是同一个)。
  于是可以直接按 id 索引,不依赖任何名字匹配 —— 覆盖率 89%,且不会抄串。

故:查得到 petbase_feature 就用它,查不到才回退 pet_feature。

**注意**:两份资料都只给名字,不给协议 id。id→名 的正向映射要靠 标注
(众包+管理员审核)或 试炼抓包桥接 补,本文件不含 288xxx。
且 roco.world 的特性 id 是 200xxx/280xxx 段,与协议的 288xxx 不是同一套编号,
无法换算(详见 docs/data.md「特性名」)。

运行:
  uv run python scripts/fetch_features.py     # 抓 wiki 原始数据(增量、可续传)
  uv run python scripts/fetch_rocoworld.py    # 抓 roco.world 原始数据(增量、可续传)
  uv run python scripts/gen_features.py       # 生成 features.json

数据源(可用环境变量覆盖):
  ROCOM_FEATURES_RAW    wiki 原始 jsonl,默认 ~/Downloads/rocom/features_raw.jsonl
  ROCOM_ROCOWORLD_RAW   roco.world 原始 jsonl,默认 ~/Downloads/rocom/rocoworld_raw.jsonl

两份数据都是可选的:只有一个也能生成,只是对应那份索引为空。
"""
import collections
import json
import os
import sys

RAW_WIKI = os.environ.get(
    "ROCOM_FEATURES_RAW", os.path.expanduser("~/Downloads/rocom/features_raw.jsonl")
)
RAW_ROCO = os.environ.get(
    "ROCOM_ROCOWORLD_RAW", os.path.expanduser("~/Downloads/rocom/rocoworld_raw.jsonl")
)
OUT = "internal/gamedata/data/features.json"

have_wiki, have_roco = os.path.exists(RAW_WIKI), os.path.exists(RAW_ROCO)
if not (have_wiki or have_roco):
    sys.exit(
        f"缺原始数据: {RAW_WIKI} 与 {RAW_ROCO} 都不存在\n"
        f"先跑: uv run python scripts/fetch_features.py\n"
        f"  或: uv run python scripts/fetch_rocoworld.py"
    )

# ——— 1. wiki(biligame):按精灵页名 ———
wiki_rows = []
if have_wiki:
    with open(RAW_WIKI, encoding="utf-8") as f:
        for line in f:
            try:
                r = json.loads(line)
            except json.JSONDecodeError:
                continue
            if r.get("feature"):  # 过滤 wiki 里特性字段为空的精灵
                wiki_rows.append(r)
    if not wiki_rows:
        sys.exit(f"{RAW_WIKI} 里没有带特性名的记录 —— 抓取可能失败了")

# ——— 2. roco.world:按 petbase_id(首选)———
# 同一 petbase_id 可能出现多次(多个 URL 指向同一形态),取第一条即可。
roco_by_id = {}
if have_roco:
    with open(RAW_ROCO, encoding="utf-8") as f:
        for line in f:
            try:
                r = json.loads(line)
            except json.JSONDecodeError:
                continue
            pid, sk = r.get("petbase_id"), r.get("passive_skills") or []
            if pid is None or not sk or not sk[0].get("name"):
                continue
            roco_by_id.setdefault(pid, {"name": sk[0]["name"], "desc": sk[0].get("desc") or ""})
    if not roco_by_id:
        sys.exit(f"{RAW_ROCO} 里没有带特性名的记录 —— 抓取可能失败了")

# ——— 3. 合并成「特性名 -> 描述 + 拥有精灵」词典 ———
# 两只资料站的精灵名口径不同,故这里的 pets 列表**只用 wiki 的页名**
# (roco.world 那份的精灵名与我方形态名格式未必一致,混进来只会让词典变脏)。
by_name = collections.OrderedDict()
for r in wiki_rows:
    e = by_name.setdefault(r["feature"], {"desc": "", "pets": []})
    if not e["desc"] and r.get("feature_desc"):
        e["desc"] = r["feature_desc"]
    if r["page"] not in e["pets"]:
        e["pets"].append(r["page"])

# roco.world 的特性描述补进来(它带的那份更全),并登记名字本身。
# ⚠️ 只补描述与"这个名字存在",**不往 pets 里塞精灵** —— 见上方说明。
n_desc_from_roco = 0
for pid, e in roco_by_id.items():
    ent = by_name.setdefault(e["name"], {"desc": "", "pets": []})
    if not ent["desc"] and e["desc"]:
        ent["desc"] = e["desc"]
        n_desc_from_roco += 1

# 排序保证生成物 diff 稳定(按 Unicode 码位)
features = [
    {"name": n, "desc": e["desc"], "pets": e["pets"]}
    for n, e in sorted(by_name.items())
]
pet_feature = {r["page"]: r["feature"] for r in wiki_rows}
petbase_feature = {str(pid): e["name"] for pid, e in sorted(roco_by_id.items())}

src = []
if have_wiki:
    src.append("wiki.biligame.com/rocom 精灵图鉴页(逐只精灵的特性字段), CC BY-NC-SA 4.0")
if have_roco:
    src.append("roco.world/zh/jini 图鉴页内嵌 JSON(带 petbase_id,与我方 id 一致)")

out = {
    "_source": " + ".join(src)
    + "。两份都只含特性名与描述,不含协议里的 288xxx id —— id 由标注/抓包桥接补充",
    "_note": "features: 特性名 -> 描述+拥有精灵(标注候选词典);"
             "pet_feature: 精灵页名 -> 特性名(wiki,按名字建键,兼容旧代码);"
             "petbase_feature: petbase_id -> 特性名(roco.world,按 id 建键,**新代码首选**)。"
             "没有协议 id 段(288xxx),id→名 由标注/抓包补。",
    "features": features,
    "pet_feature": pet_feature,
    "petbase_feature": petbase_feature,
}

os.makedirs(os.path.dirname(OUT), exist_ok=True)
with open(OUT, "w", encoding="utf-8") as f:
    json.dump(out, f, ensure_ascii=False, separators=(",", ":"))

n_empty_desc = sum(1 for e in by_name.values() if not e["desc"])
print(f"已生成 {OUT}")
print(f"  特性名 {len(features)} 个(其中 {n_empty_desc} 个无描述)")
if have_wiki:
    print(f"  pet_feature      {len(pet_feature)} 条  (wiki,按精灵页名)")
if have_roco:
    print(f"  petbase_feature  {len(petbase_feature)} 条  (roco.world,按 petbase_id)")
    print(f"  其中 {n_desc_from_roco} 个特性的描述来自 roco.world")
