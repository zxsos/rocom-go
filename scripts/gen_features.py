"""生成 internal/gamedata/data/features.json —— 特性名与「宠物 → 特性」索引。

⚠️ 数据源已改为**官方客户端解包**,不再依赖资料站爬取。

官方口径:特性就是 `SKILL_CONF` 里的**被动技能**(type=2),宠物侧由
`PETBASE_CONF.pet_feature` 指向它 —— **按 id 直取**,不需要任何名字匹配,也就不会抄串。

两份爬取来源的下场:
  - roco.world(按 petbase_id):594 条,被官方 954 条**完全覆盖**(实测独有 0 条),
    且「宝藏沙狐」那两条与官方冲突(官方有技能描述「在场时识破敌方的伪装」佐证),
    故**整份停用**。
  - wiki(按精灵页名):覆盖率更低(74%)且有 8 处抄串;仅 `pet_feature` 旧接口还用它,
    且只在原始数据存在时才生成 —— `petbase_feature` **不再回退**到它。

本脚本产出三块:
  features:        特性名 -> {描述, 拥有该特性的形态}。标注候选库的词典。
  pet_feature:     精灵页名 -> 特性名。**旧接口,按名字建键**;缺 wiki 原始数据时
                   保留生成物里已有的值(不清空)。
  petbase_feature: petbase_id -> 特性名。**新接口,按 id 建键,首选**。

**注意**:特性的 200xxx 与协议里试炼特性的 288xxx **不是同一套编号**,无法换算
(见 docs/data.md「特性名」);协议 id → 名的映射仍要靠标注/抓包桥接补。

运行(需先解包官方客户端,见 docs/apk-unpack-notes.md):
  uv run python scripts/gen_features.py
  uv run python scripts/fetch_features.py   # 可选:补 pet_feature 旧接口(wiki)
"""
import json
import os
import sys

from gamedata_sources import MissingGameData, load_conf  # noqa: E402

RAW_WIKI = os.environ.get(
    "ROCOM_FEATURES_RAW", os.path.expanduser("~/Downloads/rocom/features_raw.jsonl")
)
OUT = "internal/gamedata/data/features.json"


def load_official():
    """官方 `PETBASE_CONF` + `SKILL_CONF` → (petbase_id→特性名, 特性名→{描述, 拥有形态})。

    特性 = 被动技能(type=2);只收**真的被某个形态引用**的那些(288 条),
    `GM被动` 这类测试条目即使躺在 SKILL_CONF 里也不会进来。
    """
    pb = load_conf("PETBASE_CONF.json")
    sk = load_conf("SKILL_CONF.json")
    by_id, owners = {}, {}
    for pid, r in pb.items():
        if not isinstance(r, dict):
            continue
        s = sk.get(str(r.get("pet_feature"))) if r.get("pet_feature") else None
        name = (s or {}).get("name") if isinstance(s, dict) else None
        if not name:
            continue
        by_id[str(pid)] = name
        e = owners.setdefault(name, {"desc": (s or {}).get("desc") or "", "pets": []})
        pet_name = r.get("name") or ""
        if pet_name and pet_name not in e["pets"]:
            e["pets"].append(pet_name)
    return by_id, owners


try:
    petbase_feature, owners = load_official()
except MissingGameData as exc:
    sys.exit(
        f"缺官方数值表: {exc}\n"
        "  特性表现在**只**用官方客户端解包数据(爬取来源已停用),请先解包:\n"
        "    见 docs/apk-unpack-notes.md,或设 ROCOM_PARSED 指向解包根"
    )

# ——— wiki(可选):只补 pet_feature 旧接口 ———
# 缺原始数据时不报错:官方的 petbase_feature 已经覆盖了它的用途(且更全、不抄串)。
pet_feature = {}
if os.path.exists(RAW_WIKI):
    wiki_rows = []
    with open(RAW_WIKI, encoding="utf-8") as f:
        for line in f:
            try:
                r = json.loads(line)
            except json.JSONDecodeError:
                continue
            if r.get("feature"):
                wiki_rows.append(r)
    pet_feature = {r["page"]: r["feature"] for r in wiki_rows}
else:
    # 原始数据不在(抓取要联网 6 分钟),不能把已经生成好的那份清空 —— 保留旧值。
    if os.path.exists(OUT):
        try:
            with open(OUT, encoding="utf-8") as f:
                pet_feature = json.load(f).get("pet_feature") or {}
        except (OSError, json.JSONDecodeError):
            pet_feature = {}

# 排序保证生成物 diff 稳定(按 Unicode 码位)
features = [
    {"name": n, "desc": e["desc"], "pets": sorted(e["pets"])}
    for n, e in sorted(owners.items())
]
petbase_feature = {k: petbase_feature[k] for k in sorted(petbase_feature)}

out = {
    "_source": "官方安卓客户端(com.tencent.nrc)解包配置:PETBASE_CONF.pet_feature → "
    "SKILL_CONF(被动技能 = 特性),按 id 直取。仅供本非商业玩家工具本地统计使用",
    "_note": "features: 特性名 -> 描述+拥有该特性的形态(标注候选词典);"
    "pet_feature: 精灵页名 -> 特性名(wiki 旧接口,按名字建键,新代码别用);"
    "petbase_feature: petbase_id -> 特性名(官方解包,按 id 建键,**新代码首选**)。"
    "特性与协议里试炼特性的 288xxx 不是同一套编号,id→名 由标注/抓包补。",
    "features": features,
    "pet_feature": pet_feature,
    "petbase_feature": petbase_feature,
}

os.makedirs(os.path.dirname(OUT), exist_ok=True)
with open(OUT, "w", encoding="utf-8") as f:
    json.dump(out, f, ensure_ascii=False, separators=(",", ":"))

n_empty_desc = sum(1 for e in features if not e["desc"])
print(f"已生成 {OUT}")
print(f"  特性名 {len(features)} 个(其中 {n_empty_desc} 个无描述)")
print(f"  petbase_feature  {len(petbase_feature)} 条(官方解包,按 petbase_id)")
print(f"  pet_feature      {len(pet_feature)} 条(wiki 旧接口,按精灵页名)")
