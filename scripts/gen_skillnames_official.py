"""用官方解包 SKILL_CONF 刷新 skills.json 的 names(skill_id -> 技能名)。

背景:
  skills.json 的 names 原本来自第三方资料站 skillIds.json(aismile.dev)+
  手抄的试炼 788 段 EXTRA_SKILL_IDS(见 gen_skills.py) —— 只覆盖 604+13 条,
  试炼池里 7880012(炙热波动)这类新技能查不到名。
  官方解包(scripts/unpack-roco.ps1)之后 BinDataCompressed/SKILL_CONF.json 是
  与协议同源的权威表:id 与协议 base_skill_id 同一编号体系,1918 个技能全有中文名,
  **试炼专属 788 段(74 个)整段在内**,这脚本把 names 段整体切成官方数据。

  不替换其余段:skills 定义/innate/stone/blood 仍来自第三方(官方表没有形态关系),
  names 是独立键,只刷它,宠物详情与试炼实时视图(按 id 查名)即刻受益。

运行:
  uv run python scripts/gen_skillnames_official.py
  数据源由环境变量 ROCOM_SKILL_CONF 指向解包后的数值表 json,
  默认 D:/rocom/parsed/NRC/Content/ScriptC/Data/Bin/BinDataCompressed/SKILL_CONF.json

输出:
  原地改写 internal/gamedata/data/skills.json(只动 names 与元信息),
  并打印新增/改名的统计,便于 diff 审查。
"""
import json
import os
import re
import sys

SKILL_CONF = os.environ.get(
    "ROCOM_SKILL_CONF",
    r"D:/rocom/parsed/NRC/Content/ScriptC/Data/Bin/BinDataCompressed/SKILL_CONF.json",
)
DATA = "internal/gamedata/data/skills.json"

if not os.path.exists(SKILL_CONF):
    sys.exit(f"缺官方技能表: {SKILL_CONF}\n(解包 BinDataCompressed/SKILL_CONF.json,"
             f"或设 ROCOM_SKILL_CONF 指向它)")

with open(SKILL_CONF, encoding="utf-8") as f:
    rows = json.load(f)["RocoDataRows"]

official = {}
descs = {}
for k, v in rows.items():
    nm = (v.get("name") or "").strip()
    if not nm:
        continue
    official[int(k)] = nm
    d = (v.get("desc") or "").strip()
    # desc 里用 <desc_id=1002>灼烧</> 引用词条(标签中间就是可显示的词条名),
    # 直接剥掉标签、保留词条名做纯文本 —— tooltip / 效果行按纯文本渲染。
    d = re.sub(r"<desc_id=\d+>", "", d)
    d = d.replace("</>", "")
    if d:
        descs[int(k)] = d

with open(DATA, encoding="utf-8") as f:
    out = json.load(f)

old = {int(k): v for k, v in out.get("names", {}).items()}
added = {k: v for k, v in official.items() if k not in old}
renamed = {k: (old[k], v) for k, v in official.items() if k in old and old[k] != v}
dropped = {k: v for k, v in old.items() if k not in official}
empty_desc = sum(1 for k in official if k not in descs)

# 试炼专属 788 段的覆盖情况(重点:早先只能手抄 13 条,官方整段都在)
trial_off = {k: v for k, v in official.items() if 7880000 <= k < 7890000}

out["names"] = {str(k): official[k] for k in sorted(official)}
out["descs"] = {str(k): descs[k] for k in sorted(descs)}
out["_source"] = (
    "names/descs: 官方解包 BinDataCompressed/SKILL_CONF.json(与协议 base_skill_id 同源,"
    "含试炼专属 788 段整段;desc 为官方效果文案,已剥 <desc_id=…></> 标签成纯文本);"
    "skills/innate/stone/blood: skillIds.json(aismile.dev)"
    "+skillGuideData.json(arkmeng.cn),第三方资料站过渡方案"
)
out["_note"] = (
    "names: 技能 id -> 名,供只给 id 的场景直接查(试炼页/宠物详情);"
    "descs: 技能 id -> 官方效果文案(每个技能都有,1918/1918 非空),"
    "试炼实时视图技能槽/技能池显示效果用。均由 scripts/gen_skillnames_official.py"
    "刷新,非手抄 —— 7880012=炙热波动等新技能不必再走众包标注。"
    "官方表含 200xxx 老编号与 7xxxxx,id 与协议同源。"
    "skills/innate/stone/blood 的说明见 gen_skills.py。"
)

with open(DATA, "w", encoding="utf-8") as f:
    json.dump(out, f, ensure_ascii=False, separators=(",", ":"))

print(
    f"已刷新 {DATA}\n"
    f"  names: {len(old)} -> {len(official)}(官方 SKILL_CONF 全量)\n"
    f"  新增 {len(added)} 个 id,改名 {len(renamed)} 个,移除 {len(dropped)} 个\n"
    f"  试炼 788 段官方覆盖: {len(trial_off)} 条\n"
    f"  descs: {len(descs)} 条官方效果文案(全量非空;{empty_desc} 条仅名无文案)"
)
if renamed:
    print("  改名示例:")
    for k, (a, b) in sorted(renamed.items())[:10]:
        print(f"    {k}: {a} -> {b}")
if dropped:
    print("  移除(官方无此 id):")
    for k, v in sorted(dropped.items())[:10]:
        print(f"    {k}: {v}")
