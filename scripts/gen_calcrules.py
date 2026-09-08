#!/usr/bin/env python3
"""生成伤害估算的**印记与特性规则表**(internal/gamedata/data/calc_marks.json / calc_traits.json)。

与 gen_calcdata.py 的分工:
  - gen_calcdata.py:形态种族值 / 技能数值 / 克制矩阵(快照里有现成数据,自动抽取)
  - 本脚本:印记与特性的**规则**(roco 只给「名字 + 一句文案」,系数得人工登记,
    然后从 roco 源码**校验名字没变** —— 改名时直接报错,避免登记项悄悄失效)

⚠️ 关键缺口:buff_id → 印记名 的映射**不在 roco 里**(它的印记 id 是 "momentum"
这类字符串 slug,协议给的是数字 buff_id)。那张表只能来自游戏解包配置(BUFF 相关
Bin 配置),见 calc_mark_ids.json 与 scripts/gen_markids.py —— 需在有解包数据的
机器上跑(本机没有 ~/Downloads/rocom/parsed)。

运行:  uv run python scripts/gen_calcrules.go.py
数据源(可覆盖):
  ROCO_MARKS    marks.js 的 URL 或本地路径
  ROCO_TRAITS   traits.js 的 URL 或本地路径
"""
import json
import os
import pathlib
import re
import sys
import urllib.request

from gamedata_sources import require_conf, OFFICIAL_NOTE  # noqa: E402

ROOT = pathlib.Path(__file__).resolve().parent.parent
DATA = ROOT / "internal" / "gamedata" / "data"
BASE = "https://raw.githubusercontent.com/Evenstar-tools/roco-calculator/main"
MARKS_SRC = os.environ.get("ROCO_MARKS", f"{BASE}/miniapp/src/shared/domain/marks.js")
TRAITS_SRC = os.environ.get("ROCO_TRAITS", f"{BASE}/miniapp/src/shared/domain/traits.js")

OUT_MARKS = DATA / "calc_marks.json"
OUT_TRAITS = DATA / "calc_traits.json"
OUT_IDS = DATA / "calc_mark_ids.json"

SOURCE = {
    "upstream": "Evenstar-tools/roco-calculator",
    "url": "https://github.com/Evenstar-tools/roco-calculator",
    "site": "https://rococalc.top",
    "files": "src/domain/marks.js（印记定义）、src/domain/traits.js（TRAIT_NAME_TO_RULE）",
    "origin": "原始资料: BWIKI 洛克王国：世界（CC BY-NC-SA 4.0）",
    "license": "CC BY-NC-SA 4.0（署名-非商业-相同方式共享）;roco 代码为 MIT,不覆盖数据",
    "note": "印记与特性的**名字**从 roco 源码自动抽取并校验;**系数**由本脚本人工登记,名字对不上即报错退出",
}

# —— 印记效果的人工登记 ——
# key = roco 里的印记名;value = 效果描述。字段含义:
#   affects: power(威力) / flatPower(固定威力加值) / speed(速度) / extra(追加伤害) / none
#   per:     每层数值(percent 为百分比,flat 为绝对值)
#   unit:    percent | flat
#   needs:   触发该效果需要的前置条件(先手 / 迸发 / 非幻系攻击 …);空表示无条件
#   affectsThisHit: False = 不影响本次技能伤害(结算时机在回合末/入场),不参与计算
#
# ⚠️ 只登记「本次伤害」相关的;其余一律 affectsThisHit=False 并在 note 里写明原因
#    —— 这是 roco 的口径(见 marks.js 各 summary 的「本次伤害不变/不追加伤害」)。
MARK_RULES = {
    "蓄势": dict(affects="power", per=30, unit="percent", needs=[], affectsThisHit=True,
             note="每层攻击技能威力 +30%(roco: 每层攻击技能威力 +30%)"),
    "风起": dict(affects="power", per=20, unit="percent", needs=["先手"], affectsThisHit=True,
             note="先手时每层技能威力 +20%;不先手则不生效"),
    "蓄电": dict(affects="flatPower", per=10, unit="flat", needs=["迸发"], affectsThisHit=True,
             note="迸发时每层技能威力 +10(绝对值,不是百分比)"),
    "攻击": dict(affects="power", per=10, unit="percent", needs=[], affectsThisHit=True,
             note="每层技能威力 +10%"),
    "减速": dict(affects="speed", per=-10, unit="flat", needs=[], affectsThisHit=True,
             note="每层速度 −10;参与「先手」判定,不直接改伤害"),
    "星陨": dict(affects="extra", per=0, unit="flat", needs=["非幻系攻击"], affectsThisHit=True,
             note="非幻系攻击触发额外幻系伤害;数值由 roco 的 starfall 规则决定,本期按「追加伤害」手工/自动填"),
    # —— 不影响本次伤害(结算时机不在本次技能)——
    "湿润": dict(affects="none", per=0, unit="flat", needs=[], affectsThisHit=False, note="技能能耗降低;本次伤害不变"),
    "光合": dict(affects="none", per=0, unit="flat", needs=[], affectsThisHit=False, note="回合结束回复能量;本次伤害不变"),
    "萌芽": dict(affects="none", per=0, unit="flat", needs=[], affectsThisHit=False, note="当前伤害不变"),
    "降灵": dict(affects="none", per=0, unit="flat", needs=[], affectsThisHit=False, note="入场失去能量;本次伤害不变"),
    "中毒": dict(affects="none", per=0, unit="flat", needs=[], affectsThisHit=False, note="回合结束结算;本次技能不追加伤害"),
    "棘刺": dict(affects="none", per=0, unit="flat", needs=[], affectsThisHit=False, note="入场时结算;本次技能不追加伤害"),
    "龙噬": dict(affects="none", per=0, unit="flat", needs=[], affectsThisHit=False,
             note="使用 3 能耗技能后双攻 +40%;roco 也标注「当前由能力配置结算」"),
    "重组": dict(affects="none", per=0, unit="flat", needs=[], affectsThisHit=False,
             note="幻伤 100%/300%;属特殊伤害类型,本期不参与主公式"),
}

# —— 特性规则:roco traits.js 的 TRAIT_NAME_TO_RULE(共 7 条)——
# 我们按名字登记参数;roco 的实现在 trait-effects.js(700 行),此处只落地
# 「能明确表达为乘区/减伤」的部分,其余标 unimplemented。
TRAIT_RULES = {
    "破空": dict(rule="power_if_acted_before_enemy", side="attacker", effect="powerMultiplier",
              params={"multiplier": 1.3}, needs=["本回合已先于对手行动"],
              note="先于对手行动时威力 ×1.3(roco: power_if_acted_before_enemy)"),
    "顺风": dict(rule="power_if_faster", side="attacker", effect="powerMultiplier",
              params={"multiplier": 1.3}, needs=["速度更快"],
              note="速度更快时威力 ×1.3(roco: power_if_faster)"),
    "专注力": dict(rule="physical_power_first_turn", side="attacker", effect="powerMultiplier",
               params={"multiplier": 2}, needs=["首回合"],
               note="首回合物攻技能威力 ×2(快照里唯一带 ruleId 的特性,params.multiplier=2)"),
    "偏振": dict(rule="reduce_matching_skill_type", side="defender", effect="damageReduction",
              params={"multiplier": 0.75}, needs=[],
              note="受同系技能伤害减免(roco: reduce_matching_skill_type)"),
    "完全偏振": dict(rule="reduce_matching_skill_type_strong", side="defender", effect="damageReduction",
                params={"multiplier": 0.5}, needs=[],
                note="受同系技能伤害更强减免(roco: ..._strong)"),
    "绝对秩序": dict(rule="reduce_off_type", side="defender", effect="damageReduction",
                params={"multiplier": 0.75}, needs=[],
                note="受非本系技能伤害减免(roco: reduce_off_type)"),
    "冰钻": dict(rule="power_by_enemy_total_cost", side="attacker", effect="powerByEnemyCost",
              params={"perCost": 10}, needs=["对手技能总能耗"],
              note="威力随对手技能总能耗提升(roco: power_by_enemy_total_cost);能耗需可取得"),
}


def fetch(src):
    if src.startswith("http"):
        with urllib.request.urlopen(src, timeout=120) as r:
            return r.read().decode("utf-8")
    return pathlib.Path(src).read_text(encoding="utf-8")


def parse_mark_names(js):
    """从 marks.js 抽出 POSITIVE_MARKS / NEGATIVE_MARKS 的 (id, name, summary)。"""
    out = {}
    for var, polarity in (("POSITIVE_MARKS", "positive"), ("NEGATIVE_MARKS", "negative")):
        m = re.search(rf"const {var} = \[(.*?)\n\];", js, re.S)
        if not m:
            sys.exit(f"marks.js 里找不到 {var}(上游改结构了?)")
        for item in re.finditer(r'id:\s*"([^"]+)",\s*name:\s*"([^"]+)",\s*summary:\s*"([^"]*)"', m.group(1)):
            out[item.group(2)] = dict(id=item.group(1), name=item.group(2),
                                      summary=item.group(3), polarity=polarity)
    return out


def parse_trait_rules(js):
    """从 traits.js 抽出 TRAIT_NAME_TO_RULE(特性名 → 规则 id)。"""
    m = re.search(r"const TRAIT_NAME_TO_RULE = Object\.freeze\(\{(.*?)\}\);", js, re.S)
    if not m:
        sys.exit("traits.js 里找不到 TRAIT_NAME_TO_RULE(上游改结构了?)")
    out = {}
    for line in m.group(1).splitlines():
        mm = re.search(r"([^\s:]+):\s*\"([a-z_]+)\"", line)
        if mm:
            out[mm.group(1)] = mm.group(2)
    return out


def write_if_changed(path, payload):
    text = json.dumps(payload, ensure_ascii=False, indent=1) + "\n"
    if path.exists():
        try:
            if path.read_text(encoding="utf-8") == text:
                return False
        except OSError:
            pass
    path.write_text(text, encoding="utf-8")
    return True


def trait_buff_index():
    """官方「特性名 → buff_id 列表」。

    实测:战斗里的特性就是挂在宠物身上的 buff(BUFF_CONF 中 type=3 那一类,带 desc,如
    「专注力」=20010014「物攻+10%」。故这里按名字建索引,用于从宠物 buff 自动识别它带哪个特性 ——
    不再只靠「形态 → 特性名」那份(覆盖率 89%,且同名特性可能有多个 buff 变体(不同数值档),故返回列表。
    """
    try:
        buffs = require_conf("BUFF_CONF.json")
    except Exception:
        return {}
    idx = {}
    for bid, r in buffs.items():
        if not isinstance(r, dict) or r.get("type") != 3:
            continue
        nm = r.get("name") or ""
        if not nm:
            continue
        try:
            i = int(bid)
        except (TypeError, ValueError):
            continue
        idx.setdefault(nm, []).append(i)
    for v in idx.values():
        v.sort()
    return idx


def official_traits():
    """官方 `PET_TALENT_CONF` → 特性清单(98 条,含 desc 与 effect_group)。"""
    rows = require_conf("PET_TALENT_CONF.json")
    idx = trait_buff_index()
    out = {}
    for tid, r in rows.items():
        if not isinstance(r, dict):
            continue
        nm = r.get("name") or ""
        if not nm or nm == "无":
            continue
        out[str(tid)] = {
            "name": nm,
            "desc": r.get("desc") or "",
            "effectGroup": r.get("effect_group") or [],
            "buffIds": idx.get(nm) or [],
            "source": "official-client",
        }
    return out


def main():
    marks_js, traits_js = fetch(MARKS_SRC), fetch(TRAITS_SRC)
    roco_marks = parse_mark_names(marks_js)
    roco_traits = parse_trait_rules(traits_js)

    # 印记:以 roco 的名单为准,套上人工登记的效果;登记项里有多余的(上游改名)要报错。
    missing = [k for k in MARK_RULES if k not in roco_marks]
    if missing:
        sys.exit(f"登记了 roco 里没有的印记(上游改名?):{missing}\nroco 现有:{sorted(roco_marks)}")
    marks = []
    for name, meta in roco_marks.items():
        rule = MARK_RULES.get(name)
        entry = dict(id=meta["id"], name=name, polarity=meta["polarity"], summary=meta["summary"])
        if rule:
            entry.update(rule)
        else:
            entry.update(dict(affects="unknown", per=0, unit="flat", needs=[], affectsThisHit=False,
                              note="roco 有此印记但本地未登记效果 → 一律不参与计算,不猜"))
        marks.append(entry)
    payload_marks = {
        "_source": SOURCE,
        "_note": (f"印记定义: {len(marks)} 条(roco marks.js)。其中参与本次伤害计算的 "
                  f"{sum(1 for m in marks if m.get('affectsThisHit'))} 条;其余结算时机不在本次技能。"
                  "⚠️ buff_id → 印记的映射不在本表,见 calc_mark_ids.json。"),
        "marks": marks,
    }

    # 特性:roco 的 7 条命名规则 + 本地登记参数;两者取交集,差集要报错或标注。
    unknown = [k for k in roco_traits if k not in TRAIT_RULES]
    traits = []
    for name, rule_id in roco_traits.items():
        entry = dict(name=name, rule=rule_id)
        if name in TRAIT_RULES:
            entry.update(TRAIT_RULES[name])
            entry["implemented"] = True
        else:
            entry.update(dict(side="?", effect="?", params={}, needs=[],
                              note="roco 有规则但本地未实现 → 不参与计算", implemented=False))
        traits.append(entry)
    official_t = official_traits()
    tbidx = trait_buff_index()
    for tid, info in official_t.items():
        name = info["name"]
        if any(t["name"] == name for t in traits):
            continue
        traits.append(dict(name=name, rule="", side="?", effect="?", params={}, needs=[],
                           note="官方表已登记,规则未接入 → 不参与计算", implemented=False,
                           source="official-client", talentId=tid, buffIds=info.get("buffIds") or tbidx.get(name) or [],
                           desc=info.get("desc", "")))
    # 已有规则的特性也补上官方 buffId(用于从 buff 自动识别)
    for t in traits:
        t.setdefault("buffIds", [])
        if not t["buffIds"]:
            t["buffIds"] = tbidx.get(t["name"]) or []
        for tid, info in official_t.items():
            if info["name"] == t["name"]:
                t["buffIds"] = info.get("buffIds") or tbidx.get(t["name"]) or []
                t["talentId"] = tid
                break

    payload_traits = {
        "_source": SOURCE,
        "_note": (f"特性规则: roco traits.js 的 TRAIT_NAME_TO_RULE 共 {len(roco_traits)} 条, "
                  f"本地实现 {len(roco_traits) - len(unknown)} 条;其余特性(快照 241 条里只有 1 条带 ruleId) "
                  "一律「未支持」不参与计算。触发条件推断不出时按未触发处理并在 UI 标注。"),
        "traits": traits,
        "talents": official_t,
    }

    # buff_id → 印记:这张表只能来自游戏解包配置,这里只保证文件存在(空也写)。
    ids_path = OUT_IDS
    if not ids_path.exists():
        payload_ids = {
            "_source": {
                "upstream": "游戏解包配置(BUFF 相关 Bin 配置)",
                "note": "roco 的印记 id 是字符串 slug,协议给的是数字 buff_id,两者的映射只能来自游戏配置",
            },
            "_note": ("buff_id → 印记名。需在有解包数据的机器上跑 scripts/gen_markids.py 生成; "
                      "未收录的 buff 前端显示原始 id,不猜名字。"),
            "buffs": {},
        }
        write_if_changed(ids_path, payload_ids)
        print(f"  {ids_path.relative_to(ROOT)}: 已建空表(待 gen_markids.py 填充)")

    for path, payload in ((OUT_MARKS, payload_marks), (OUT_TRAITS, payload_traits)):
        changed = write_if_changed(path, payload)
        print(f"  {path.relative_to(ROOT)}: {'已更新' if changed else '无变化'} ({path.stat().st_size // 1024} KiB)")

    print(f"印记 {len(marks)} 条(参与计算 {sum(1 for m in marks if m.get('affectsThisHit'))}) "
          f"/ 特性 {len(traits)} 条(已实现 {sum(1 for t in traits if t.get('implemented'))})")
    if unknown:
        print(f"  roco 有规则但本地未实现: {unknown}")


if __name__ == "__main__":
    main()
