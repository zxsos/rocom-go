"""用客户端官方配置生成草系徽章试炼静态数据,输出到 internal/gamedata/data/trial.json。

数据源是 scripts/unpack.sh 解包出的官方配置:
    BinDataCompressed/GRASS_TRIAL_CONF.json        3 档难度(10000/10001/10002)
    BinDataCompressed/GRASS_TRIAL_CHAPTER_CONF.json 难度 × 章的节点配置
    BinDataCompressed/GRASS_TRIAL_EVENT_CONF.json   每个事件的精灵/类型
    BinDataCompressed/GRASS_TRIAL_PERIOD_CONF.json  活动周期
    BinDataCompressed/GRASS_TRIAL_LOG_CONF.json     章节封面图与见闻录文案

与 scripts/gen_trial.py(数据源为玩家 wiki)的关系:
官方解包能**直接给出**层结构(node_struct)、各章普通战斗池(chapter_event→EVENT→精灵)、
22 名首领(第 4 层 node_event)、章节名/场景与活动周期,因此这些字段换成官方来源;
**第 7 层 NPC 的具体阵容客户端没有静态表**(战斗时由服务器下发,见
api_trial.go 关于 0x1316 的注释),仍沿用 gen_trial.py 从 wiki 收集的玩家实测阵容
透传。实测 wiki 的 opponent id(300005 研究员/310005 易西/400001 罗兰…)与官方
node7 的 node_event id 完全对齐,本脚本会做一致性校验,不一致会告警。

普通池口径说明:官方池按「本章普通战斗事件 → 事件精灵名 → 我方 petbase」解析,
每章 160/234/132 种;旧 wiki 池 188/295/177(玩家把 1/2/3/5 层与部分实测遭遇并表)。
两套口径都标注在 _note 里,前端「其他遭遇」分组会兜住官方池外的实战照面。

运行:
  uv run python scripts/gen_trial_official.py
依赖文件:
  ROCOM_PARSED   解包根,默认 ~/Downloads/rocom/parsed
"""
import json
import os
import re
import sys

PARSED = os.environ.get("ROCOM_PARSED", os.path.expanduser("~/Downloads/rocom/parsed"))
BIN_DIR = os.path.join(PARSED, "NRC", "Content", "ScriptC", "Data", "Bin", "BinDataCompressed")
NAMES = os.path.join(os.path.dirname(__file__), "..", "internal", "gamedata", "data", "names.json")
OLD_TRIAL = os.path.join(os.path.dirname(__file__), "..", "internal", "gamedata", "data", "trial.json")
OUT = os.path.join(os.path.dirname(__file__), "..", "internal", "gamedata", "data", "trial.json")

for p in (BIN_DIR,):
    if not os.path.isdir(p):
        sys.exit(f"缺解包目录: {p}\n请先跑 scripts/unpack.sh(或设 ROCOM_PARSED)。")
if not os.path.exists(NAMES):
    sys.exit(f"缺 {NAMES} —— 请先跑 scripts/gen_gamedata.py")
if not os.path.exists(OLD_TRIAL):
    sys.exit(f"缺 {OLD_TRIAL} —— 首次生成需先跑 gen_trial.py(透传 NPC 阵容)")

GEN_DATE = "2026-09-03"  # 生成日,手填以便对比;不重要时也可取文件 mtime


def rows(table: str) -> dict:
    with open(os.path.join(BIN_DIR, table + ".json"), encoding="utf-8-sig") as f:
        return json.load(f)["RocoDataRows"]


# 层类型(node_index 0~7;与 gen_trial.py 的 FLOORS 一致,这里再按官方 node_struct 校验)
FLOORS = ["start", "normal", "normal", "normal", "boss", "normal", "merchant", "npc"]
# 节点下标 -> 期望的层型(官方 node_struct 的 node_event 给出的判定)
NODE_MEAN = {
    0: "start",     # node0: 起点事件 5xxxxx
    1: "normal", 2: "normal", 3: "normal",  # node1~3: 普通层,无显式 node_event
    4: "boss",      # node4: 22 名首领 node_event 200001~200022
    5: "normal",    # node5: 普通层
    6: "merchant",  # node6: 商人/魔力之源 node_event 600003/700001
    7: "npc",       # node7: NPC 阵容 300xxx/310xxx/400xxx
}

with open(NAMES, encoding="utf-8") as f:
    names = json.load(f)
petbase = {int(k): v for k, v in names.get("petbase", {}).items()}

with open(OLD_TRIAL, encoding="utf-8") as f:
    old = json.load(f)

gtc = rows("GRASS_TRIAL_CHAPTER_CONF")
evt = rows("GRASS_TRIAL_EVENT_CONF")
conf = rows("GRASS_TRIAL_CONF")
period = rows("GRASS_TRIAL_PERIOD_CONF")
logs = rows("GRASS_TRIAL_LOG_CONF")
try:  # 效果表(词条/特调);老解包没有该文件时留空,effects 键照出
    eff = rows("GRASS_TRIAL_EFFECT_CONF")
except FileNotFoundError:
    eff = {}


def base_of_name(nm: str):
    """官方事件名 -> 我方 petbase id:优先无形态(常规 id,不含 _f)且 id<10000 的最小者。

    普通池事件的名字是基础名(如「喵喵」),与我方 petbase 多条记录同名时取
    <10000 那套(3001~3756 常规形态),与战斗/遇见记录同口径。"""
    hits = [pid for pid, v in petbase.items() if v["n"] == nm]
    if not hits:
        return None
    pref = [h for h in hits if h < 10000]
    return min(pref) if pref else min(hits)


def event_base(eid: int, nm: str):
    """官方事件(eid)的精灵名 -> 我方 petbase id(events 映射用)。

    - 普通遭遇(100k/110k 段):官方 name 是精灵**基础名**,与 base_of_name 同口径
      (取 id<10000 常规形态),与普通池/遇见图一致;
    - 首领(200k 段):官方 name 是形态基础名,我方战斗形态全名 = name + 「草系徽章-
      首领形态」(8101~8122,gen_trial_official.py 的 boss 解析同款),先拼后缀匹配,
      命中不了才退回基础名(至少给得出名字与头像)。
    """
    if 200000 <= eid < 300000:
        want = nm + "_草系徽章-首领形态"
        hits = [pid for pid, v in petbase.items()
                if v["n"] + (("_" + v["f"]) if v.get("f") else "") == want]
        if hits:
            return min(hits)
    return base_of_name(nm)


def chapter_of(mode_id: int, idx: int) -> int:
    """难度 id -> 第 idx 章(1 起)对应的官方 chapter 配置 id。"""
    return conf[str(mode_id)]["chapter"][idx - 1]


def combat_events(chapter_conf_id: int):
    """官方某章的普通战斗事件 id 列表(剔除起点/首领/NPC/商人/魔力等特殊段)。"""
    out = []
    for x in (int(x) for x in gtc[str(chapter_conf_id)]["chapter_event"]):
        r = evt.get(str(x))
        # 战斗事件在 EVENT 表里 type 缺省;显式 type 5/6/7 是起点/商人/魔力等特殊层。
        # 段号本身也能判(20 首领 / 30-40 NPC / 50 起点 / 60-70 商人),这里两者都看。
        if r and (r.get("type") is None or r.get("type") == 1) and not 50000 <= x <= 79999:
            out.append(x)
    return out


# ---- 1. chapters/pools:官方普通池(基础难度,三难度同章池相同,已核验) ----
warnings: list[str] = []
chapter_names, pools = {}, {}
for idx in (1, 2, 3):
    cid = chapter_of(10000, idx)
    row = gtc[str(cid)]
    bases: list[int] = []
    seen = set()
    for x in combat_events(cid):
        r = evt.get(str(x))
        if not r:
            continue
        b = base_of_name(r.get("name", ""))
        if b and b not in seen:
            seen.add(b)
            bases.append(b)
    bases.sort()
    pools[str(idx)] = bases
    img = logs.get(str(99 + idx), {}).get("image", "")
    m = re.search(r"\.(img_maoxianrizhi_Photo\d+)\b", img)
    intro = outro = ""
    ls = logs.get(str(99 + idx), {}).get("log_struct", [])
    if len(ls) > 2 and isinstance(ls[2], dict):
        intro = ls[2].get("log", "")
    if len(ls) > 7 and isinstance(ls[7], dict):
        outro = ls[7].get("log", "")
    chapter_names[str(idx)] = {
        "label": f"第{'一二三'[idx - 1]}章",
        "name": row.get("name", ""),
        "n": len(bases),
        "scene_id": row.get("scene_id"),
        "image": f"badge/{m.group(1)}.webp" if m else "",
        "intro": intro,
        "outro": outro,
    }

    # 差异报告(旧 wiki 池 vs 官方池)
    oldp = set(old.get("pools", {}).get(str(idx), []))
    lost = oldp - set(bases)
    if lost:
        print(f"  [报告] 第{idx}章: 官方池 {len(bases)} < 旧 wiki 池 {len(oldp)},"
              f" wiki 有官方无 {len(lost)} 只: {sorted(lost)[:12]}…")
    gained = set(bases) - oldp
    if gained:
        print(f"  [报告] 第{idx}章: 官方池新增 {sorted(gained)}")

# ---- 2. floors:按官方 node_struct 校验 ----
for mode in ("10000", "10001", "10002"):
    for cid in conf[mode]["chapter"]:
        ns = gtc[str(cid)].get("node_struct")
        if not isinstance(ns, list) or len(ns) != len(FLOORS):
            warnings.append(f"难度{mode} 章节{cid} node_struct 不是 8 节点,与 FLOORS 不符")
            continue
        for i, want in NODE_MEAN.items():
            nd = ns[i]
            ne = [int(x) for x in (nd.get("node_event") or [])]
            if want == "boss":
                if not (200000 < ne[0] < 200100 if ne else False):
                    warnings.append(f"章节{cid} node{i}: 首领节点 node_event={ne} 异常")
            elif want == "npc":
                if not ne or not (300000 <= ne[0] <= 400099):
                    warnings.append(f"章节{cid} node{i}: NPC 节点 node_event={ne} 异常")
            elif want == "merchant":
                if not ne or not any(600000 <= v <= 799999 for v in ne):
                    warnings.append(f"章节{cid} node{i}: 商人节点 node_event={ne} 异常")
            elif want == "normal":
                if ne:
                    warnings.append(f"章节{cid} node{i}: 普通层不应有显式 node_event={ne}")
print(f"  层结构校验: {'OK(3 难度 × 3 章 × 8 节点)' if not warnings else str(len(warnings)) + ' 处告警(见文末)'}")

# ---- 3. bosses:官方 node4 的 22 名首领,按名字映射「草系徽章-首领形态」 ----
boss_ids: list[int] = []
miss_boss: list[str] = []
for cid in conf["10000"]["chapter"]:
    for x in (int(v) for v in gtc[str(cid)]["node_struct"][4].get("node_event", [])):
        r = evt.get(str(x))
        if not r or r.get("type") is None or r.get("type") != 1:
            continue
        nm = r.get("name", "")
        full_hits = [
            pid for pid, v in petbase.items()
            if v["n"] + (("_" + v["f"]) if v.get("f") else "") == nm + "_草系徽章-首领形态"
        ]
        if full_hits:
            boss_ids.extend(full_hits)
        else:
            miss_boss.append(f"{x} {nm}")
boss_ids = sorted(set(boss_ids))
if miss_boss:
    warnings.append(f"{len(miss_boss)} 个首领未命中草系徽章形态: {miss_boss}")
if boss_ids != list(range(8101, 8123)):
    warnings.append(f"首领解析结果 {boss_ids[0] if boss_ids else '-'}~{boss_ids[-1] if boss_ids else '-'},"
                    f"不是预期的 8101~8122 连续 22 名")

# ---- 4. npc:沿用 gen_trial.py(玩家实测),并校验 id 与官方 node7 对齐 ----
npc = old.get("npc", {})
for mode, md in npc.items():
    for chkey, ops in md.get("chapters", {}).items():
        official = [int(v) for v in
                    gtc[str(chapter_of(int(mode), int(chkey)))]["node_struct"][7].get("node_event", [])]
        mine = [o["id"] for o in ops]
        if sorted(official) != sorted(mine):
            warnings.append(
                f"难度{mode} 第{chkey}章: 官方 node7 {official} ≠ 透传 NPC 阵容 {mine}"
            )
print(f"  NPC 阵容: 透传 {len(npc)} 个难度;id 与官方 node7 对齐校验见文末告警")

# ---- 5. activity:活动周期与难度说明 ----
def period_text(p: dict) -> str:
    t = str(p.get("start_time", ""))
    pd = str(p.get("period", ""))
    m = re.match(r"(\d+)\s+(\d+):\d+:\d+", pd)
    tail = f",持续 {int(m.group(1))} 天 {int(m.group(2))} 小时" if m else ""
    return f"{t} 开启{tail}"

activity = {}
for pk in ("10000", "10001"):
    p = period.get(pk, {})
    if p:
        activity[pk] = period_text(p)

# ---- 6. events:官方事件 -> 精灵(实时视图把事件号翻译成头像,免人工标注) ----
# 协议只下放 event_conf_id;普通遭遇(100k/110k)与首领(200k)在 EVENT_CONF 里有
# 精灵名,实时视图据此直接显示对手。NPC 阵容(300xxx+,整队)与祝福/商人(5~7x 段)
# 不是单只精灵,不入表 —— 前端遇不到它们当"对手精灵"的场景。
events: dict[str, int] = {}
miss_events: list[str] = []
for eid, r in evt.items():
    nid = int(eid)
    if not 100000 <= nid < 300000:
        continue
    nm = (r.get("name") or "").strip()
    if not nm:
        continue
    b = event_base(nid, nm)
    if b:
        events[eid] = b
    else:
        miss_events.append(f"{eid} {nm}")
print(f"  events: {len(events)} 个事件映射到精灵" +
      (f";{len(miss_events)} 个名字对不上我方 petbase: {miss_events[:5]}…" if miss_events else ""))
if miss_events:
    warnings.append(f"events: {len(miss_events)} 个事件名未命中我方 petbase")

# ---- 6.5 event_names:特殊事件名(实时视图槽位行「事件 {id}」的可读名) ----
# events 只收能映射出**单只精灵**的战斗遭遇(100k/110k 普通 + 200k 首领,这以外
# 还有 12x/13x 段遇见精灵)。剩下的段没有单只精灵可映射,但官方 EVENT_CONF 给得出
# 名字 —— 祝福(500xxx)/商人(600xxx)/魔力之源(700xxx,如 700000=魔力之源)/NPC
# (300xxx+,整队),一并收进 event_names,前端槽位行/「换事件」那条不再裸 id。
# NPC 段官方名带前缀尾缀(草系徽章-研究员-1),清理成可读名(研究员/易西/罗兰)。
event_names: dict[str, str] = {}
for eid, r in evt.items():
    nid = int(eid)
    if 100000 <= nid < 300000:  # 已进 events 的战斗段,不需要事件名
        continue
    nm = (r.get("name") or "").strip()
    if not nm:
        continue
    if 300000 <= nid < 500000:  # NPC 段:去「草系徽章-」前缀与「-难度」尾缀
        base = nm
        if base.startswith("草系徽章-"):
            base = base[len("草系徽章-"):]
        base = re.sub(r"-\d+$", "", base)
        nm = base
    event_names[eid] = nm
print(f"  event_names: {len(event_names)} 个特殊事件名" +
      (f"(例: {list(event_names.items())[:3]})" if event_names else ""))

# ---- 6.7 effects:试炼效果名(GRASS_TRIAL_EFFECT_CONF)----
# 协议到处只给 effect_id:顶部「本周词条」(1000 段,融合次数/技能冷却/回复限制…)、
# 奖励/额外奖励里的碎片(20xx,其中 2015~2020 就是生命/速度/双攻双防特调)、
# 30xx 事件效果。前端把这些 id 直接贴出来玩家看不懂,这里整表落一份 id -> 名。
# 名字是官方文案(无 HTML/空名,58 条全有);重复名(如 1004/1005 都叫初出茅庐)
# 是不同数值的同名词条,保留原名不去重。
effects: dict[str, str] = {}
for eid, r in eff.items():
    nm = (r.get("name") or "").strip()
    if nm:
        effects[eid] = nm
print(f"  effects: {len(effects)} 个试炼效果名" +
      (f"(例: {list(effects.items())[:5]})" if effects else ""))
diff_text = "  ".join(f"第{i}章官方 {len(v)}" for i, v in pools.items())
out = {
    "_source": "客户端官方配置:BinDataCompressed/GRASS_TRIAL_{CONF,CHAPTER,EVENT,EFFECT,PERIOD,LOG}_CONF"
    "(CUE4Parse 解包,普通池/首领/章节/周期/词条均来自官方);第 7 层 NPC 阵容为玩家实测(wiki)透传",
    "_updated": f"GRASS_TRIAL_* 官方配置,{GEN_DATE} 生成;旧 wiki 池见 gen_trial.py",
    "_note": "pools: 各章普通池的 petbase id —— 官方口径(按 chapter_event 战斗事件解析,"
    + diff_text + "),旧 wiki 口径 188/295/177;bosses: 22 名首领;npc: 难度 -> 章 -> 候选阵容"
    "(玩家实测,id 与官方 node7 event 对齐);floors: 按 node_index 索引的层类型。"
    "chapters.image/intro/outro 来自 GRASS_TRIAL_LOG_CONF(封面图与见闻录文案)。"
    "events: 官方 event_conf_id -> 精灵(普通遭遇 100k/110k + 首领 200k 段,实时视图"
    "把事件号直接翻译成头像,免人工标注;NPC 阵容/祝福/商人非单只精灵不入表)。"
    "event_names: 特殊事件 -> 事件名(祝福 500xxx/商人 600xxx/魔力之源 700xxx/NPC"
    " 300xxx 等无单只精灵的事件,槽位行/换事件需要可读名;NPC 段名去「草系徽章-」"
    "前缀与「-难度」尾缀。精灵一律用我方 petbase id,事件名用官方 name)。"
    "effects: 试炼效果 GRASS_TRIAL_EFFECT_CONF 的 id -> 名(1000 段词条 / 20xx 精灵"
    "天赋与属性特调 / 30xx 事件效果;协议只给 id,实时视图据名显示)。",
    "floors": FLOORS,
    "chapters": chapter_names,
    "pools": pools,
    "bosses": boss_ids,
    "npc": npc,
    "activity": activity,
    "events": events,
    "event_names": event_names,
    "effects": effects,
}
os.makedirs(os.path.dirname(OUT), exist_ok=True)
with open(OUT, "w", encoding="utf-8") as f:
    json.dump(out, f, ensure_ascii=False, separators=(",", ":"))

print(f"已生成 {OUT}")
print(f"  章节: " + "  ".join(f"{v['label']} {v['name']} 池{v['n']} 图={v['image']}" for v in chapter_names.values()))
print(f"  首领: {len(boss_ids)} 名")
if warnings:
    print("告警:")
    for w in warnings:
        print("  ! " + w)
else:
    print("全部校验通过")
