"""把抓取的精灵特性原始数据(scripts/fetch_features.py 的 features_raw.jsonl)
整理成 internal/gamedata/data/features.json。

特性(288xxx)与技能(7020500)一样是协议里的**全局编号**,但项目此前没有特性名表
(技能有 skills.json 过渡方案)。wiki 没有集中的特性图鉴页,特性名+描述只能逐只
精灵从图鉴页抓(见 fetch_features.py 的说明)。

本脚本产出两块:
  features:   特性名 -> {描述, 出现该特性的精灵}。这是「标注候选库」的词典:
              玩家在 web 端搜名字/描述,选中后把协议里的 288xxx id 与名字绑起来。
  pet_feature: 精灵页名 -> 特性名。试炼等「宠物与其特性同时出现」的场合,
              可用它把 id 和名字对上(桥接细节见 docs/data.md)。

**注意**:wiki 只有名字,没有 id。id→名 的正向映射要靠 标注(众包+管理员审核)
或 试炼抓包桥接 补,本文件不含 id。

运行:
  uv run python scripts/fetch_features.py   # 抓原始数据(增量、可续传)
  uv run python scripts/gen_features.py     # 生成 features.json

数据源(可用环境变量覆盖):
  ROCOM_FEATURES_RAW 原始 jsonl,默认 ~/Downloads/rocom/features_raw.jsonl
"""
import collections
import json
import os
import sys

RAW = os.environ.get(
    "ROCOM_FEATURES_RAW", os.path.expanduser("~/Downloads/rocom/features_raw.jsonl")
)
OUT = "internal/gamedata/data/features.json"
SRC_NOTE = (
    "wiki.biligame.com/rocom 精灵图鉴页(逐只精灵的特性字段), CC BY-NC-SA 4.0;"
    "只含特性名与描述,不含协议里的 288xxx id —— id 由标注/抓包桥接补充"
)

if not os.path.exists(RAW):
    sys.exit(f"缺原始数据: {RAW}\n先跑: uv run python scripts/fetch_features.py")

rows = []
with open(RAW, encoding="utf-8") as f:
    for line in f:
        try:
            r = json.loads(line)
        except json.JSONDecodeError:
            continue
        if r.get("feature"):  # 过滤 wiki 里特性字段为空的精灵
            rows.append(r)

if not rows:
    sys.exit(f"{RAW} 里没有带特性名的记录 —— 抓取可能失败了")

# 特性名 -> {描述(首个非空), 精灵列表}
by_name = collections.OrderedDict()
for r in rows:
    e = by_name.setdefault(r["feature"], {"desc": "", "pets": []})
    if not e["desc"] and r.get("feature_desc"):
        e["desc"] = r["feature_desc"]
    if r["page"] not in e["pets"]:
        e["pets"].append(r["page"])

# 排序保证生成物 diff 稳定(按 Unicode 码位)
features = [
    {"name": n, "desc": e["desc"], "pets": e["pets"]}
    for n, e in sorted(by_name.items())
]
pet_feature = {r["page"]: r["feature"] for r in rows}

out = {
    "_source": SRC_NOTE,
    "_note": "features: 特性名 -> 描述+拥有精灵,用作标注候选词典;"
             "pet_feature: 精灵页名 -> 特性名,供宠物特性与抓包 id 桥接。"
             "没有协议 id 段(288xxx),id→名 由标注/抓包补。",
    "features": features,
    "pet_feature": pet_feature,
}

os.makedirs(os.path.dirname(OUT), exist_ok=True)
with open(OUT, "w", encoding="utf-8") as f:
    json.dump(out, f, ensure_ascii=False, separators=(",", ":"))

n_empty_desc = sum(1 for e in by_name.values() if not e["desc"])
print(
    f"已生成 {OUT}\n"
    f"  特性名 {len(features)} 个(其中 {n_empty_desc} 个无描述),来自 {len(rows)} 只精灵"
)
