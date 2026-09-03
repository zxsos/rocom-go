"""解析草系徽章试炼的静态配置,输出到 internal/gamedata/data/trial.json。

数据源是 wiki 的 Lua 数据模块(由 scripts/fetch_trial_data.py 抓取),提供协议里
**没有**的三样东西:各章精灵池、22 名首领、第 7 层 NPC 候选阵容。

与其余 gen_*.py 一样,数据源**不是游戏解包** —— 但这一次连第三方资料站都不是,
而是玩家维护的 wiki 攻略页(CC BY-NC-SA 4.0)。故独立成 trial.json 而非并入
names.json,并在生成物里标注来源与更新时间。

**2026-09-03 起已被 scripts/gen_trial_official.py 取代**(改读客户端官方配置
`GRASS_TRIAL_{CONF,CHAPTER,EVENT,PERIOD,LOG}_CONF`):官方能直接给出层结构
(见下方实测,与官方 `node_struct` 逐节点一致)、章节/场景/活动周期、22 名首领与
各章普通池;第 7 层 NPC 阵容客户端无静态表,仍由该脚本用 wiki 实测阵容透传并校验。
本脚本与 fetch_trial_data.py 保留作对照/回滚,下方实测结论经官方表核验仍有效。

## 层类型是怎么确定的(关键,别弄错)

wiki 说每章 **7 层**,而协议里每章是 **8 个节点**(node_index 0~7)。两者口径不同,
直接套用会错位。实测对齐(样本 PCAPdroid_31_8月_22_54_57):

    node_index 0        —— 章节起点,无战斗(wiki 没有对应层)
    node_index 1,2,3    —— wiki 1~3 层:普通池,随机 3 个对手选项
    node_index 4        —— wiki 4 层:首领池,22 名首领里随机 3 个
    node_index 5        —— wiki 5 层:普通池
    node_index 6        —— wiki 6 层:无精灵(远行商人或魔力之源)
    node_index 7        —— wiki 7 层:NPC 阵容

两条独立证据:
  1. **node_index 6 之后有 18 秒无战斗**空档(14:59:38 推进、下一次战斗 14:59:57),
     正对上 wiki 的「6 层无精灵」;
  2. **node_index 7 的战斗对手是 NPC「草系研究员」**,敌方阵容里的「蒲公英娃娃」
     正是 wiki 候选 [300005](兽花蕾/蒲公英娃娃/魔力猫/针叶巡林)的成员。

## 关于 NPC 阵容的一个限制

wiki 的 opponent id(300xxx/310xxx/400xxx)与协议里的 npc_id(实测 86023)
**不是同一套编号**,无从绑定。故只能按「难度 + 章」给出候选池,不能精确锁定
当前遭遇的是哪一个 —— 响应里也如实这么标注。

运行:
  uv run python scripts/fetch_trial_data.py   # 抓 Lua 模块
  uv run python scripts/gen_trial.py          # 解析生成

数据源(可用环境变量覆盖):
  ROCOM_TRIAL_DATA  Lua 模块,默认 ~/Downloads/rocom/grassTrialData.lua
"""
import json
import os
import re
import sys

TRIAL_DATA = os.environ.get(
    "ROCOM_TRIAL_DATA", os.path.expanduser("~/Downloads/rocom/grassTrialData.lua")
)
NAMES = "internal/gamedata/data/names.json"
OUT_DIR = "internal/gamedata/data"
OUT = os.path.join(OUT_DIR, "trial.json")

if not os.path.exists(TRIAL_DATA):
    sys.exit(f"缺 {TRIAL_DATA}\n先跑: uv run python scripts/fetch_trial_data.py")
if not os.path.exists(NAMES):
    sys.exit(f"缺 {NAMES} —— 请先跑 scripts/gen_gamedata.py")

# 层类型(按 node_index 索引,见文件头「层类型是怎么确定的」)。
# 协议每章 8 个节点,wiki 只有 7 层 —— node 0 是章节起点,wiki 无对应。
FLOORS = [
    "start",  # 0 章节起点,无战斗
    "normal",  # 1 普通池
    "normal",  # 2 普通池
    "normal",  # 3 普通池
    "boss",  # 4 首领池(22 名首领里随机 3 个)
    "normal",  # 5 普通池
    "merchant",  # 6 无精灵:远行商人或魔力之源
    "npc",  # 7 NPC 阵容
]

with open(TRIAL_DATA, encoding="utf-8") as f:
    wiki = f.read()
with open(NAMES, encoding="utf-8") as f:
    names = json.load(f)

petbase = {int(k): v for k, v in names.get("petbase", {}).items()}

# ---- 1. pets: gt_内部id -> {name, base_id} ----
# wiki 的 base_id == 我方 petbase id(实测 390/390 全部命中),故只留 base_id;
# 名字用我方的(带形态后缀,如「刺轮砣_下弦的样子」),比 wiki 的基础名更准。
pets = {}
for m in re.finditer(
    r'gt_([0-9a-f]{16}) = \{\s*\n\s*name = "([^"]*)",\s*\n\s*marker_name = "([^"]*)",'
    r"\s*\n\s*base_id = (\d+),",
    wiki,
):
    gid, name, marker, base = m.groups()
    pets["gt_" + gid] = {"name": name, "marker": marker, "base": int(base)}
if not pets:
    sys.exit("解析出 0 只精灵 —— Lua 结构变了,请检查 pets 段的正则")

unknown = sorted({p["base"] for p in pets.values()} - set(petbase))
if unknown:
    print(f"  注意: {len(unknown)} 个 base_id 不在我方 petbase 里: {unknown[:10]}")


def to_bases(gt_ids):
    """gt_内部id 列表 -> 我方 petbase 列表(去重保序,丢掉未知的与首领形态)。

    **剔除「xxx_首领形态」**(如 圣水守护_首领形态,4005~4090 那 20 只):
    它们只作为首领出现在第 4 层,普通层(第 1/2/3/5 层)遇不到,却混在各章普通池里,
    于是「遇见记录」三张图各列出 20 个永远碰不上的形态 —— 进度分母被虚增,
    用户看着「还有这么多没遇到」其实是假象。实测抓包里这 20 只出现 0 次。

    ⚠️ 判名要**排除「xxx_草系徽章-首领形态」**(8101~8122):那是 bosses 组
    (第 4 层的 22 名首领),是真实存在且要保留的,别误删。
    """
    out, seen = [], set()
    dropped = []
    for g in gt_ids:
        p = pets.get(g)
        if not p or p["base"] in seen or p["base"] not in petbase:
            continue
        seen.add(p["base"])
        form = petbase[p["base"]]
        full = form["n"] + ("_" + form["f"] if form.get("f") else "")
        if "首领形态" in full and "草系徽章" not in full:
            dropped.append(full)
            continue
        out.append(p["base"])
    if dropped:
        print(f"  剔除普通池里的首领形态 {len(dropped)} 个:", "、".join(dropped))
    return out


# ---- 2. chapters: 各章普通池 ----
def balanced(s, i):
    """从 s[i]=='{' 起,返回配平的花括号块(含两端)。"""
    depth = 0
    for k in range(i, len(s)):
        if s[k] == "{":
            depth += 1
        elif s[k] == "}":
            depth -= 1
            if depth == 0:
                return s[i : k + 1]
    return None


ci = wiki.find("\n  chapters = {")
chapters_raw = balanced(wiki, wiki.find("{", ci)) if ci >= 0 else None
if not chapters_raw:
    sys.exit("找不到 chapters 段 —— Lua 结构变了")

# 按缩进层级解析(比正则可靠:章节块里嵌套了 pets 数组)。
# 与下面 modes 同一套写法:块结束 → 块开始 → 字段,**结束判断排最前**。
#
# ⚠️ **键用章节序号(1/2/3),不用 wiki 的 chapter id**:
#   - 协议里 chapter_id 恒为 3000/3001/3002(三章固定);
#   - wiki 的 chapters 段用 1000/1001/1002;
#   - wiki 的 modes 段**按难度分段**:mode 10000 下是 1000/1001/1002,
#     10001 下是 2000/2001/2002,10002 下是 3000/3001/3002。
# 三套编号互不相干,拿 id 当键必错。故统一按「第几章」(从 label「第一章」提取
# 序号,取不到就按出现顺序)作键,这也是前端唯一需要的维度。
def chapter_no(label, fallback):
    m = re.search(r"第([一二三四五六七八九十\d]+)章", label or "")
    if not m:
        return fallback
    cn = "一二三四五六七八九十"
    s = m.group(1)
    return int(s) if s.isdigit() else cn.index(s) + 1


lines = wiki.split("\n")
pools, chapter_names, cur, seq = {}, {}, None, 0
si = next((i for i, l in enumerate(lines) if l.strip().startswith("chapters = {")), None)
for l in lines[si + 1 :]:
    st, ind = l.strip(), len(l) - len(l.lstrip())

    if st == "}," and ind == 2:
        break  # chapters 段结束
    if st == "}," and ind == 4:  # 一章结束
        if cur is not None and "id" in cur:
            seq += 1
            key = str(chapter_no(cur.get("label"), seq))
            bases = to_bases(cur["pets"])
            pools[key] = bases
            chapter_names[key] = {
                "label": cur.get("label", ""),
                "name": cur.get("name", ""),
                "n": len(bases),
            }
        cur = None
        continue
    if st == "{" and ind == 4:
        cur = {"pets": []}
        continue

    if cur is None:
        continue
    if ind == 6 and st.startswith("id ="):
        cur["id"] = int(re.search(r"\d+", st).group())
    elif ind == 6 and st.startswith("label ="):
        cur["label"] = re.search(r'"([^"]*)"', st).group(1)
    elif ind == 6 and st.startswith("name ="):
        cur["name"] = re.search(r'"([^"]*)"', st).group(1)
    elif ind == 8 and st.startswith('"gt_'):
        cur["pets"].append(st.strip('",'))
if not pools:
    sys.exit("解析出 0 章 —— Lua 结构变了")

# ---- 3. modes: 各难度各章的 NPC 候选阵容 ----
# 实测:进阶难度会累积低难度的阵容(基础 2/2/1 → 进阶1 4/4/1 → 进阶2 6/6/1)。
mi = next((i for i, l in enumerate(lines) if l.strip().startswith("modes = {")), None)
# 缩进层级(实测 Module:GrassTrialData):
#   modes(2) > mode 块(4) > 字段(6) > chapter 块(8) > 字段(10)
#           > opponent 块(12) > 字段(14) > pets 项(16)
#
# **结束判断必须排在最前**:块结束时 cur_* 已被清成 None,若把结束分支放在
# `if cur_x is None: continue` 之后,收尾逻辑会被那句 continue 吃掉 ——
# 表现是「解析出 0 个难度」却看不出哪里错。这里按「结束 → 开始 → 字段」排。
modes, cur_mode, cur_ch, cur_op = {}, None, None, None
mode_seq = 0
for l in lines[mi + 1 :]:
    st, ind = l.strip(), len(l) - len(l.lstrip())

    # --- 块结束 ---
    if st == "}," and ind == 2:
        break  # modes 段结束
    if st == "}," and ind == 8:  # 一章结束,收进当前难度
        if cur_mode is not None and cur_ch is not None and "id" in cur_ch:
            mode_seq += 1
            # 键用章节序号:wiki 的 chapter id 按难度分段(1000/2000/3000 段),
            # 与 chapters 段的 1000/1001/1002 不是一套,见上面 chapter_no 的说明。
            cur_mode["chapters"][str(chapter_no(cur_ch.get("label"), mode_seq))] = [
                {"id": o["id"], "name": o["name"], "pets": to_bases(o["pets"])}
                for o in cur_ch["ops"]
                if o.get("name")
            ]
        cur_ch, cur_op = None, None
        continue
    if st == "}," and ind == 4:  # 一个难度结束
        if cur_mode is not None and "id" in cur_mode:
            modes[str(cur_mode["id"])] = {
                "name": cur_mode.get("name", ""),
                "chapters": cur_mode["chapters"],
            }
        cur_mode, cur_ch, cur_op = None, None, None
        mode_seq = 0
        continue

    # --- 块开始 ---
    if st == "{" and ind == 4:
        cur_mode, cur_ch, cur_op = {"chapters": {}}, None, None
        continue
    if st == "{" and ind == 8 and cur_mode is not None:
        cur_ch, cur_op = {"ops": []}, None
        continue
    if st == "{" and ind == 12 and cur_ch is not None:
        cur_op = {"pets": []}
        cur_ch["ops"].append(cur_op)
        continue

    # --- 字段 ---
    if cur_mode is None:
        continue
    if ind == 6 and st.startswith("id ="):
        cur_mode["id"] = int(re.search(r"\d+", st).group())
    elif ind == 6 and st.startswith("name ="):
        cur_mode["name"] = re.search(r'"([^"]*)"', st).group(1)
    elif cur_ch is not None and ind == 10 and st.startswith("id ="):
        cur_ch["id"] = int(re.search(r"\d+", st).group())
    elif cur_ch is not None and ind == 10 and st.startswith("label ="):
        cur_ch["label"] = re.search(r'"([^"]*)"', st).group(1)
    elif cur_op is not None and ind == 14 and st.startswith("id ="):
        cur_op["id"] = int(re.search(r"\d+", st).group())
    elif cur_op is not None and ind == 14 and st.startswith("name ="):
        cur_op["name"] = re.search(r'"([^"]*)"', st).group(1)
    elif cur_op is not None and ind == 16 and st.startswith('"gt_'):
        cur_op["pets"].append(st.strip('",'))
if not modes:
    sys.exit("解析出 0 个难度 —— Lua 结构变了")

# ---- 4. 首领池:22 名,三章共用 ----
# 页面「第 4 层 · 首领列表」在三章下是同一份 22 个名字。数据模块里**没有**单独的
# 首领表,只能按名字反查。
#
# 为什么不靠 marker_name 含「首领」判:wiki 标注不全(霜翼领主、深渊罗隐没标),
# 按 marker 只会得到 21 个。故以页面标注为准,硬编码下面这份名单。
#
# ⚠️ **必须用「草系徽章-首领形态」那套 id(8101~8122),不能用 wiki 的 base_id**:
#   wiki 的 pets 表里首领记的是「首领形态」(如女王蜂 4021),而**战斗里实际出现的
#   是「草系徽章-首领形态」**(8107)—— 实测 0x1316 的 enemy_team 就是这个。
#   两套 id 差了 4000 多,用错的话「遇到的首领」与首领池对不上。
#   我方解包数据里 22 名**都有**草系徽章-首领形态(8101~8122,连续),故统一用它。
#   校验:数量必须等于 22,且每个都要落到 8101~8122 这个区间。
BOSS_NAMES = [
    "圣水守护", "烈火战神", "叶冕魔力猫", "恶魔狼王", "蹦蹦果", "千棘海针",
    "女王蜂", "奇丽果", "幻影荆棘", "迷嶂布莱克", "风暴战犬", "钻石蜗",
    "黑猫密探", "祭礼巨像", "雪影冰灵", "棋契陛下", "奇梦咪", "波普鹿",
    "圣剑骑士", "伊兰龙", "霜翼领主", "深渊罗隐",
]
# 我方 petbase 全名 -> id(用于 wiki pets 表里查不到的首领,见下)
petbase_full = {}
for pid, v in petbase.items():
    petbase_full[v["n"] + (("_" + v["f"]) if v.get("f") else "")] = pid

name2base = {}
for p in pets.values():
    name2base.setdefault(p["name"], p["base"])

bosses, missing_boss = set(), []
for n in BOSS_NAMES:
    # 只认「<名字>_草系徽章-首领形态」—— 见上面的说明,这是战斗里的实际形态。
    # wiki 的 base_id(name2base)对首领无效,故这里不用它。
    hit = [
        pid
        for full, pid in petbase_full.items()
        if full == n + "_草系徽章-首领形态"
    ]
    if hit:
        bosses.add(min(hit))
    else:
        missing_boss.append(n)
if missing_boss:
    sys.exit(f"首领名单里查不到「草系徽章-首领形态」: {missing_boss} —— 我方名称库或 BOSS_NAMES 变了")
bosses = sorted(bosses)
if len(bosses) != 22:
    sys.exit(f"解析出 {len(bosses)} 名首领(应为 22) —— 请核对 BOSS_NAMES")
# id 必须落在 8101~8122(草系徽章首领的连续区间);若哪天游戏换了区间,这里会拦住
if bosses[0] < 8100 or bosses[-1] > 8130:
    sys.exit(f"首领 id {bosses[0]}~{bosses[-1]} 不在预期的 8101~8122 区间 —— 游戏可能改版了")

out = {
    "_source": "wiki.biligame.com/rocom/草系徽章试炼 → Module:GrassTrialData"
    " (玩家维护的 wiki, CC BY-NC-SA 4.0; 协议里没有这些数据)",
    "_updated": "S3 铅字幻梦 2026/08/18(页面标注的更新时间)",
    "_note": "pools: 各章普通池的 petbase id(188/295/177);bosses: 22 名首领;"
    "npc: 难度 -> 章 -> 候选阵容(每个阵容是 petbase id 列表);"
    "floors: 按 node_index 索引的层类型,见 scripts/gen_trial.py 文件头。"
    "精灵一律用我方 petbase id —— wiki 的 base_id 与之 100% 一致(390/390)。",
    "floors": FLOORS,
    "chapters": chapter_names,
    "pools": pools,
    "bosses": bosses,
    "npc": modes,
}
os.makedirs(OUT_DIR, exist_ok=True)
with open(OUT, "w", encoding="utf-8") as f:
    json.dump(out, f, ensure_ascii=False, separators=(",", ":"))

n_ops = sum(len(v) for md in modes.values() for v in md["chapters"].values())
pool_desc = "  ".join(f"{v['label']}({v['n']})" for v in chapter_names.values())
print(
    f"已生成 {OUT}\n"
    f"  精灵池: {pool_desc}\n"
    f"  首领: {len(bosses)} 名\n"
    f"  NPC 阵容: {len(modes)} 个难度 / 共 {n_ops} 套"
)
