"""合并两份第三方技能资料,输出到 internal/gamedata/data/skills.json。

与其余 gen_*.py 不同,本脚本的数据源**不是游戏解包**,而是两个第三方资料站:

  1. skillIds.json(由 scripts/fetch_skill_ids.py 抓取,aismile.dev 技能图鉴)
     → **skill_id → 技能中文名**,604 条。这是协议里 base_skill_id(7020500 这类)
       的唯一可用映射:有了它,草系试炼里那些裸技能 id 才能显示中文名。
  2. skillGuideData.json(arkmeng.cn 的开放数据,含 492 个技能的威力/能耗/效果,
     以及每个技能与精灵的三类关系)
     → **形态 → 天生 / 技能石可学 / 血脉** 三张表。

两份互补,缺一不可:
  - 只有 1:试炼技能能显示名,但宠物详情拿不到「这只精灵能学什么」;
  - 只有 2:宠物详情有数据,但试炼的裸 id 反查不了 —— 试炼技能会**融合**,
    威力被改写(实测 7020500 融合1次:威力 25→30),按数值反推会命中错误技能
    (30/4 反查得「埋伏」,真身是「乱打」),只能按 id 精确查。

**这是过渡方案**:权威来源是游戏解包的 SKILL_CONF(id→中文名,与协议同源)。
等 scripts/unpack.sh 解出后应改走解包数据,并删掉本脚本与 fetch_skill_ids.py。

为什么不进 names.json:names.json 全部来自解包 Bin 配置,数据源与更新节奏都不同,
混在一起会让「这份名字是从哪来的」说不清。故独立成 skills.json。

三类技能的关系(实测 462 个形态):

| 类别 | 含义 | 条目 | 与其他类重叠 |
| --- | --- | --- | --- |
| `innate` 天生 | 升级自然学会,带学会等级 | 6518 | 与技能石 3 条、与血脉 0 条 |
| `stone` 技能石 | 需消耗技能石才能学 | 7376 | 与天生 3 条 |
| `blood` 血脉 | 通过血脉获得,无等级/条件 | 8316 | 与前两类 0 条 |

三类几乎完全互斥,故各自独立成表、不去重(重叠那 3 条是「既能升级学会、
也能用技能石学」,两边都列才符合玩家查图鉴的预期)。

输出结构 —— **技能定义单一来源**(每个技能只存一份,形态侧只存下标):
  skills:  [[名, 属性, 威力, 能耗, 效果下标, skill_id], ...]  全局技能表
  innate:  {petbase_id: [[技能下标, 学会等级], ...]}
  stone:   {petbase_id: [技能下标, ...]}
  blood:   {petbase_id: [技能下标, ...]}
  effects: [...]                        效果描述共享池
  names:   {skill_id: 技能名}            供按 id 查(试炼页)

为什么这么组织:技能定义(名/属性/威力/能耗/效果)在三类里**完全重复** ——
22210 条条目其实只涉及 492 个不同技能。把定义抽出来只存一份,形态侧存下标,
体积从「每条重复存」的 ~480 KiB 降到 ~185 KiB,且后来加第四类技能
(如活动技能)只需再加一张索引表。

运行:
  uv run python scripts/fetch_skill_ids.py   # 抓 skill_id 映射(1)
  uv run python scripts/gen_skills.py        # 合并生成

数据源(均可用环境变量覆盖):
  ROCOM_SKILL_IDS   skill_id→名,    默认 ~/Downloads/rocom/skillIds.json
  ROCOM_SKILL_GUIDE 技能详情+三类关系, 默认 ~/Downloads/rocom/skillGuideData.json
"""
import json
import os
import sys

SKILL_IDS = os.environ.get(
    "ROCOM_SKILL_IDS", os.path.expanduser("~/Downloads/rocom/skillIds.json")
)
SKILL_JSON = os.environ.get(
    "ROCOM_SKILL_GUIDE", os.path.expanduser("~/Downloads/rocom/skillGuideData.json")
)
NAMES = "internal/gamedata/data/names.json"
OUT_DIR = "internal/gamedata/data"
OUT = os.path.join(OUT_DIR, "skills.json")

for path, how in (
    (SKILL_IDS, "先跑: uv run python scripts/fetch_skill_ids.py"),
    (SKILL_JSON, "获取: curl -o ~/Downloads/rocom/skillGuideData.json \\\n"
                 "        https://arkmeng.cn/storage/files/skillGuideData.json"),
):
    if not os.path.exists(path):
        sys.exit(f"缺技能数据: {path}\n{how}\n(或设 ROCOM_SKILL_IDS / ROCOM_SKILL_GUIDE 指向已有文件)")
if not os.path.exists(NAMES):
    sys.exit(f"缺 {NAMES} —— 请先跑 scripts/gen_gamedata.py")

with open(SKILL_IDS, encoding="utf-8") as f:
    id_raw = json.load(f)
with open(SKILL_JSON, encoding="utf-8") as f:
    skills = json.load(f)
with open(NAMES, encoding="utf-8") as f:
    names = json.load(f)

# ---- 1. skill_id -> 技能名(主映射,来自 fetch_skill_ids.py)----
id2name = {}
for s in id_raw.get("skills", []):
    id2name[int(s["skillId"])] = s["name"]

# 试炼专属 id(788 段):资料站的技能表**整段没有**这些 id,但抓包实测它们就是
# 某个技能 —— 草系徽章试炼有一套自己的技能池,池里的技能走 788xxxx 编号。
# 融合**不会**改变 base_skill_id(只改 fused_power/fusion_count),所以这不是
# 融合产物;融合态技能也能用基础 id 查到名 —— 早先以为「融合生成新 id」是错的。
#
# 池的特征(样本 PCAPdroid_31_8月_22_54_57,250 场历史战绩):
#   - 至少 27 个 id,分布在 7880000~7880071,不与基础技能(702/704/712…段)连续;
#   - **多数是宠物本来就会的技能**。拿每场战绩的形态去比对账号里那只宠物的实际
#     技能列表,已确认的 6 个里有 69%~100% 的场次该形态自己就会这个技能
#     (力量增效 100%、霜降 94%、毒孢子 90%、魔能爆 87%、孢子 74%、引燃 69%)。
#     故「同一 id 跨形态出现」**不是**因为与形态无关,而是那些形态恰好都会它
#     —— 这点早先写错过一次(写成「试炼侧技能池、与形态无关」),已更正;
#   - 剩下 0~26% 的场次形态不会该技能,那些是试炼过程中现给的(节点奖励/技能石),
#     按形态推不出来,只能人工在游戏里核对;
#   - 状态类居多(变化类技能融合不改威力,故其能耗是稳定的识别特征)。
#
# 已确认的 13 个(数值**全部**与资料站吻合,攻击技连威力+能耗都精确对上,
# 例如 藤绞80/4、滚雪球55/3、暴风雪85/3):
EXTRA_SKILL_IDS = {
    7880000: "力量增效",
    7880007: "热身运动",
    7880008: "藤绞",
    7880011: "引燃",
    7880018: "霜降",
    7880026: "毒孢子",
    7880029: "虫群智慧",
    7880054: "滚雪球",
    7880056: "暴风雪",
    7880057: "孢子",
    7880058: "魔能爆",
    7880062: "光合作用",
    7880068: "疫病吐息",
}
#
# 7880058 另有三条独立实证(它是最早被确认的一个):
#   1. 开局 0x1951 里,玩家选的 initial_skill_ids = [7020500, 7020550, 7170210],
#      而服务器下发的技能槽是 [7020500, 7880058, 7170210] —— 只有 7020550
#      (魔能爆)被替换成 7880058,另两个原样不变。
#   2. 7880058 开局**零融合**时就存在;融合只改 fused_power(20 -> 150)与
#      fusion_count(0 -> 2),**不改 base_skill_id**。
#   3. 能耗 0 是魔能爆的独有指纹(其余技能能耗均为正):其效果是「使用时消耗
#      所有能量,消耗越高伤害越高」,试炼需按融合态动态算威力,故单给一个 id。
#
# 为什么硬编码:788 段在资料站查不到,只能靠抓包实证 + 人工在游戏里核对逐个
# 得出。新增时请补上「怎么确认的」(哪一场/什么方法),否则后来者无从判断
# 这条映射还可不可信 —— 这点吃过亏: 早先给 7880058 编了个「融合产物」的解释,
# 没验证就写进文档,结果它是魔能爆。

# 查名用的表含试炼专属 id;但**回填技能表的 skill_id 时只用原始抓取的 id**
# (见下),否则「魔能爆」会因同时有 7020550 与 7880058 被当成重名、填 0
# —— 试炼态 id 只该出现在试炼里,宠物详情要的是基础技能 id。
lookup_names = dict(id2name)
id2name.update(EXTRA_SKILL_IDS)

# 守一道名字校验:EXTRA_SKILL_IDS 是手抄的,写错一个字(如「热身」抄成「热身动
# 作」)不会报错,只会静默产出一个资料站里根本不存在的技能名。故要求每个名字
# 都能在 arkmeng 的技能表里找到 —— 找不到直接报错退出,逼人当场核对。
all_names = {s["name"] for s in skills}
bogus = {sid: nm for sid, nm in EXTRA_SKILL_IDS.items() if nm not in all_names}
if bogus:
    raise SystemExit(f"EXTRA_SKILL_IDS 里的名字在技能表中不存在(抄错了?): {bogus}")

# 反查 名 -> [skill_id],用于把 id 回填进技能表。
# 有**重名**的技能(同名多个 id)填 0 表示「不确定是哪个」:
#   - 借用/取念/复写:各 4 个连续 id(7020840~7020843),是同技能的不同变体;
#   - 愿力冲击:15 个 id,是各精灵的专属版本。
# 这类按名字无从判断是哪个,填错比不填更糟。
name2ids = {}
for sid, nm in lookup_names.items():  # 注意:用 lookup_names,排除试炼专属 id
    name2ids.setdefault(nm, []).append(sid)
ambiguous = {n for n, ids in name2ids.items() if len(ids) > 1}

# ---- 2. 形态全名 -> petbase_id ----
name2pets = {}
for pid, v in names.get("petbase", {}).items():
    full = v["n"] + (("_" + v["f"]) if v.get("f") else "")
    name2pets.setdefault(full, []).append(int(pid))


def main_id(ids):
    """取主形态 petbase_id:优先常规区间(<1e7)里最小的那个。

    与 gen_gamedata.py 的 _real 判据同口径 —— 同名还有一批 petbase_id 落在
    1.3e7~1.9e7 的复制形态(剧情/测试/NPC 用,如「迪莫」16000004),它们不是真实
    图鉴形态,技能表不该挂在它们身上。
    """
    real = [i for i in ids if i < 10_000_000]
    return min(real or ids)


def form_key(p):
    """资料站的精灵条目 -> 我方形态全名。

    petName + form 拼接,与 names.json 的 petbase 全名口径一致
    (如「鸭吉吉」+「蓬松的样子」-> 「鸭吉吉_蓬松的样子」)。
    """
    return p["petName"] + (("_" + p["form"]) if p.get("form") else "")


# ---- 3. 全局技能表(每个技能只存一份)----
# 按名字排序,使下标稳定 —— 生成物随仓库提交,顺序不稳定会让 diff 没法看。
effects, eff_idx = [], {}
skill_table, skill_idx = [], {}
no_id = 0
for s in sorted(skills, key=lambda x: x["name"]):
    nm = s["name"]
    if nm in skill_idx:  # 同名技能(资料站里不应有,防一手)
        continue
    skill_idx[nm] = len(skill_table)
    eff = s.get("effect") or ""
    if eff not in eff_idx:
        eff_idx[eff] = len(effects)
        effects.append(eff)
    # 重名技能填 0(见上 ambiguous 的说明)
    sid = 0 if nm in ambiguous else (name2ids.get(nm) or [0])[0]
    if sid == 0 and nm not in ambiguous:
        no_id += 1
    skill_table.append([
        nm,
        s.get("element") or "",
        s.get("power") or "",
        s.get("cost") or "",
        eff_idx[eff],
        sid,
    ])

# ---- 4. 三类关系表:形态 -> 技能下标 ----
# innate 额外带学会等级(其余两类无等级/条件)。
innate, stone, blood = {}, {}, {}
skipped = set()
for s in skills:
    idx = skill_idx[s["name"]]
    for p in s.get("innatePets", []):
        ids = name2pets.get(form_key(p))
        if not ids:
            skipped.add(form_key(p))
            continue
        innate.setdefault(str(main_id(ids)), []).append([idx, p.get("level") or 0])
    # 技能石/血脉只记下标,按下标排序(即按技能名)保证输出稳定
    for key, acc in (("stonePets", stone), ("bloodlinePets", blood)):
        for p in s.get(key, []):
            ids = name2pets.get(form_key(p))
            if not ids:
                skipped.add(form_key(p))
                continue
            acc.setdefault(str(main_id(ids)), []).append(idx)

for lst in innate.values():
    lst.sort(key=lambda x: (-x[1], x[0]))  # 等级降序,同级按名
for acc in (stone, blood):
    for lst in acc.values():
        lst.sort()

out = {
    "_source": "skillIds.json(aismile.dev)+skillGuideData.json(arkmeng.cn),"
               "均为第三方资料站的过渡方案;解出 SKILL_CONF 后应替换",
    "_note": "names: 技能 id -> 名(试炼等只给 id 的场景直接查)。"
             "skills: 全局技能表,条目是数组 [名, 属性, 威力, 能耗, 效果下标, 技能id];"
             "技能id=0 表示该技能在资料站是重名的(如借用有 4 个变体),无从判断是哪一个;"
             "威力/能耗是字符串,无威力者为 —。"
             "innate/stone/blood: 按 petbase 形态 id 索引的 天生/技能石可学/血脉 技能,"
             "存的是 skills 表下标(innate 额外带学会等级)。"
             "三者几乎完全互斥(天生∩技能石 3 条、与血脉 0 条),故独立成表不去重。",
    "effects": effects,
    "names": {str(k): v for k, v in sorted(id2name.items())},
    "skills": skill_table,
    "innate": innate,
    "stone": stone,
    "blood": blood,
}
os.makedirs(OUT_DIR, exist_ok=True)
with open(OUT, "w", encoding="utf-8") as f:
    json.dump(out, f, ensure_ascii=False, separators=(",", ":"))

n_inn = sum(len(v) for v in innate.values())
n_sto = sum(len(v) for v in stone.values())
n_bld = sum(len(v) for v in blood.values())
print(
    f"已生成 {OUT}\n"
    f"  技能 id 映射 {len(id2name)} 条(其中重名 {len(ambiguous)} 个: "
    f"{'、'.join(sorted(ambiguous)[:5])})\n"
    f"  全局技能表 {len(skill_table)} 个技能 / 效果池 {len(effects)} 条"
    f"({len(skills)} 个技能源, {no_id} 个无 skill_id)\n"
    f"  天生 {len(innate)} 形态 / {n_inn} 条\n"
    f"  技能石 {len(stone)} 形态 / {n_sto} 条\n"
    f"  血脉 {len(blood)} 形态 / {n_bld} 条"
)
if skipped:
    print("  未匹配(资料站有、我方 petbase 无同名):", "、".join(sorted(skipped)[:12]))
