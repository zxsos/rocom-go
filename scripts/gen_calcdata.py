"""从 roco-calculator 的赛季快照抽取「伤害估算」所需的三张小表。

背景:本仓库的数据**全部来自自行解包**,但 PVP 伤害公式需要的两份东西解包里拿不全:

  1. **形态的六维种族值** —— 我方宠物的六维协议里就有(attribute_info),但
     **对手不下发**(见 docs/api/schemas.md 的 /api/shanyao 要点)。要估算对手
     得先有种族值,再按「种族值 + 默认个体 + 已知性格」推算。
  2. **技能的威力 / 系别 / 类别 / 能耗** —— 现有 skills.json 只覆盖 492 条第三方
     资料(且花精灵、聚能这类老 id 没有),协议里也只有部分技能带威力参数。

roco-calculator(https://github.com/Evenstar-tools/roco-calculator)的赛季快照
有这两份数据(619 形态 / 579 技能),且它的伤害公式与取整口径是本项目的对齐目标
(见 web/src/pages/shanyao/calc.js)。故离线抽取、桥接后落进仓库。

⚠️ 桥接靠**中文名**,因为 roco 用的是自有哈希 id(spirit_xxx / skill_xxx),
与游戏内的 petbase_id / skill_id 不是一套:
  - 形态:roco 的 fullName 实测无重名 → 与我们 names.json.petbase[].n 精确匹配;
  - 技能:roco 的 name 实测无重名 → 与我们 skills.json 的 names 匹配。
覆盖率实测:形态 611/1136(53.8%),未命中的多为未进化小形态;本次 pcap 里
实际登场的形态全部命中。技能侧命中约 674/1918。**未命中一律留空,不猜。**

⚠️ 许可:roco 代码是 MIT,但它的数据来自 BWIKI,适用 **CC BY-NC-SA 4.0**
(署名 + 非商业 + 相同方式共享)。故:
  - 8.3MB 快照**不入仓库**,只入抽取后的小表,并在生成物里带 _source
    (快照 meta.id / bwikiRevision / 源 URL / 许可 / 抓取时间);
  - 后续同步 = 重跑本脚本(换赛季只需换快照),meta 里记录了版本便于核对。

运行:
  uv run python scripts/gen_calcdata.py
  ROCOM_CALC_SNAPSHOT=/path/to/current.json uv run python scripts/gen_calcdata.py

数据源(可用环境变量覆盖):
  ROCOM_CALC_SNAPSHOT  快照路径或 URL,默认 raw.githubusercontent 的 current.json
"""
import json
import os
import pathlib
import sys
import urllib.request

from gamedata_sources import MissingGameData, load_conf, require_conf  # noqa: E402

ROOT = pathlib.Path(__file__).resolve().parent.parent
DATA = ROOT / "internal" / "gamedata" / "data"

SNAPSHOT = os.environ.get(
    "ROCOM_CALC_SNAPSHOT",
    "https://raw.githubusercontent.com/Evenstar-tools/roco-calculator/main/data/snapshots/current.json",
)
SNAPSHOT_PAGE = "https://github.com/Evenstar-tools/roco-calculator"
BWIKI = "https://wiki.biligame.com/rocom/"

OUT_RACE = DATA / "calc_race.json"
OUT_SKILLS = DATA / "calc_skills.json"
OUT_TYPES = DATA / "calc_types.json"

# 六维键与 roco 一致(physicalAttack / magicalAttack / …),顺序固定便于数组化存储。
STAT_KEYS = ["hp", "physicalAttack", "magicalAttack", "physicalDefense", "magicalDefense", "speed"]


def load_json(path):
    with open(path, encoding="utf-8") as f:
        return json.load(f)


def load_snapshot(src):
    if src.startswith("http://") or src.startswith("https://"):
        with urllib.request.urlopen(src, timeout=120) as r:  # noqa: S310 - 只在生成期离线拉取
            return json.load(r)
    return load_json(src)


def snapshot_source(meta):
    """生成物里的来源说明:够别人一眼看出「数据从哪来、什么许可、哪个版本」。"""
    return {
        "upstream": "Evenstar-tools/roco-calculator 赛季快照",
        "url": SNAPSHOT_PAGE,
        "snapshotId": meta.get("id", ""),
        "seasonId": meta.get("seasonId", ""),
        "rulesVersion": meta.get("rulesVersion", ""),
        "bwikiRevision": meta.get("bwikiRevision", ""),
        "fetchedAt": meta.get("fetchedAt", ""),
        "origin": "BWIKI 洛克王国：世界（" + BWIKI + "）",
        "license": "CC BY-NC-SA 4.0（署名-非商业-相同方式共享）；roco 代码为 MIT，不覆盖数据",
        "bridgedBy": "中文名（形态 fullName ↔ names.json.petbase[].n；技能 name ↔ skills.json.names）",
        "note": "roco 的 spirit_/skill_ 是自有哈希 id，与游戏内 petbase_id/skill_id 不是一套，只能按名桥接",
    }


def patch_before(snapshot):
    """取出「S4 前瞻调整」的 before 值:rocoId -> {六维键: 调整前的值}。

    ⚠️ 快照的 raceStats 已经**应用**了 currentPatchChanges(S4 前瞻, 2026.09.10 生效),
    而我们抓的是现网当前版本 —— 用快照值会与协议里的种族值对不上(实测花衣蝶:
    协议 122/67/72/84/94, 快照 132/72/77/89/100, 差值正好是前瞻里的 before/after)。
    故默认**回退到 before**, 与线上一致; 前瞻正式生效后重跑并设 ROCOM_CALC_APPLY_PATCH=1。
    """
    if os.environ.get("ROCOM_CALC_APPLY_PATCH") == "1":
        return {}, None
    pc = snapshot.get("currentPatchChanges") or {}
    before = {}
    for s in pc.get("spirits") or []:
        stat = {}
        for item in s.get("items") or []:
            if item.get("kind") == "stat" and item.get("field") in STAT_KEYS:
                stat[item["field"]] = item.get("before")
        if stat:
            before[s.get("entityId")] = stat
    return before, pc.get("patch") or {}


# 官方 18 系在 TYPE_DICTIONARY 里的 id 顺序(与 calc_types.json 的 types 顺序一致;
# id 7「岩系」已废弃、字段全空,故跳过)。
OFFICIAL_TYPE_IDS = [2, 3, 4, 5, 6, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20]

# 官方字段名 → STAT_KEYS
OFFICIAL_RACE_FIELDS = (
    ("hp", "hp_max_race"),
    ("physicalAttack", "phy_attack_race"),
    ("magicalAttack", "spe_attack_race"),
    ("physicalDefense", "phy_defence_race"),
    ("magicalDefense", "spe_defence_race"),
    ("speed", "speed_race"),
)


def try_conf(name):
    """读官方数值表;缺解包数据时返回 None(此时回退 roco 快照,不阻断生成)。"""
    try:
        return load_conf(name)
    except MissingGameData:
        return None


def official_type_names():
    """TYPE_DICTIONARY → {系别 id: 系别名}。type_name 带「系」后缀(「草系」),
    去掉后与 calc_types.json 的 types 口径一致(「草」)。"""
    rows = try_conf("TYPE_DICTIONARY.json") or {}
    out = {}
    for i in OFFICIAL_TYPE_IDS:
        n = (rows.get(str(i)) or {}).get("type_name") or ""
        out[str(i)] = n[:-1] if n.endswith("系") else n
    return out


def official_types():
    """TYPE_DICTIONARY → (18 系名, 18×18 克制矩阵)。

    type_restraint<N>:对第 N 系的关系,`1` = ×2、`-1` = ×0.5、缺省 = ×1。
    """
    rows = try_conf("TYPE_DICTIONARY.json")
    if not rows:
        return None, None
    names, matrix = [], []
    for a in OFFICIAL_TYPE_IDS:
        row = rows.get(str(a)) or {}
        n = row.get("type_name") or ""
        names.append(n[:-1] if n.endswith("系") else n)
        line = []
        for d in OFFICIAL_TYPE_IDS:
            v = row.get(f"type_restraint{d}", 0)
            line.append(2 if v == 1 else (0.5 if v == -1 else 1))
        matrix.append(line)
    return names, matrix


def official_damage_floor():
    """ATTR_GLOBAL_CONFIG 的伤害下限(phy_dam_floor / spe_dam_floor)。

    两者实测同为 2;dam_floor 是 None(未启用)故不采用。两者不一致时返回 None 并告警 ——
    宁可不下发,也不猜一个口径。
    """
    rows = try_conf("ATTR_GLOBAL_CONFIG.json")
    if not rows:
        return None
    got = {}
    for r in rows.values():
        if isinstance(r, dict) and r.get("key") in ("phy_dam_floor", "spe_dam_floor"):
            if isinstance(r.get("num"), int):
                got[r["key"]] = r["num"]
    if len(got) < 2:
        return None
    if got["phy_dam_floor"] != got["spe_dam_floor"]:
        print("  ⚠️ 官方伤害下限 phy/spe 不一致,已跳过:", got)
        return None
    return got["phy_dam_floor"]


def build_official_race():
    """官方客户端 `PETBASE_CONF` → 形态种族值。**按 id 直取**,不靠中文名桥接,
    覆盖率远高于 roco 快照那条路(1128 行 vs 611)。

    ⚠️ `SUM_race` **不是**六维之和(花衣蝶六维和 539,SUM_race 却是 553),语义未确认,
    故不参与计算与校验 —— 校验一律走 ANCHORS(协议实测)。
    """
    rows = try_conf("PETBASE_CONF.json") or {}
    dam_names = official_type_names()
    out = {}
    for sid, r in rows.items():
        if not isinstance(r, dict):
            continue
        race = {k: r.get(f) for k, f in OFFICIAL_RACE_FIELDS}
        if not all(isinstance(v, int) for v in race.values()):
            continue
        types = [dam_names.get(str(t), "") for t in (r.get("unit_type") or [])]
        pbid = r.get("pictorial_book_id")
        out[str(sid)] = {
            "name": r.get("name") or "",
            "dexNo": str(pbid) if isinstance(pbid, int) else "",
            "types": [t for t in types if t],
            "race": race,
            "total": sum(race.values()),
            "source": "official-client",
        }
    return out


def build_race(snapshot, names, source):
    """形态 → 种族值 + 系别。官方表优先,roco 快照兜底,协议锚点最终裁决。"""
    petbase = names.get("petbase") or {}
    name_to_ids = {}
    ambiguous = []
    for sid, info in petbase.items():
        n = (info or {}).get("n")
        if not n:
            continue
        name_to_ids.setdefault(n, []).append(sid)
    for n, ids in name_to_ids.items():
        if len(ids) > 1:
            ambiguous.append(n)

    by_name = {s.get("fullName"): s for s in snapshot.get("spirits") or []}
    before, patch = patch_before(snapshot)
    reverted = 0
    out = {}
    for n, ids in name_to_ids.items():
        sp = by_name.get(n)
        if not sp:
            continue
        race = dict(sp.get("raceStats") or {})
        if sp.get("id") in before:
            reverted += 1
            race.update(before[sp["id"]])
        entry = {
            "name": n,
            "dexNo": sp.get("dexNo") or "",
            "types": list(sp.get("types") or []),
            "race": {k: race.get(k) for k in STAT_KEYS},
            "total": race.get("total"),
            "rocoId": sp.get("id", ""),
            "source": "roco-snapshot",
        }
        for sid in ids:  # 同名形态(极少)共享一份数据
            out[str(sid)] = entry

    # 官方客户端的种族值按 id 直取,覆盖率远高于按名桥接的 roco 快照 —— 有就用官方的。
    official = build_official_race()
    off_hit = 0
    for sid, entry in official.items():
        if sid in petbase:
            out[sid] = entry
            off_hit += 1

    # 协议实测锚点是「事实」,优先级最高:官方表/快照与它冲突时以锚点为准,并如实记录 ——
    # 冲突通常意味着某侧口径变了(如客户端已内置未生效的前瞻值),静默覆盖会埋雷。
    conflicts = []
    for sid, want in ANCHORS.items():
        cur = out.get(sid)
        if not cur:
            continue
        if cur.get("race") != want:
            conflicts.append(
                f"{sid}({cur.get('name', '?')}): {cur.get('source', '?')}={cur.get('race')} ≠ 协议锚点={want}"
            )
        cur = dict(cur)
        cur["race"] = dict(want)
        cur["source"] = "protocol-anchor"
        out[sid] = cur

    used = {s.get("id") for s in (snapshot.get("spirits") or []) if s.get("fullName") in name_to_ids}
    src = dict(source)
    if patch:
        src["patchReverted"] = f"{patch.get('id', '')}（{patch.get('label', '')}，{patch.get('date', '')}）已回退到 before"
    return {
        "_source": src,
        "_note": (
            f"形态种族值: {len(out)} / {len(petbase)} 个 petbase 有数据"
            f"(官方客户端 {off_hit} 条按 id 直取, 其余 roco 快照按中文名桥接; "
            f"共 {len(snapshot.get('spirits') or [])} 形态, 用到 {len(used)} 个)。"
            "未命中一律留空不猜。race 六维键与 roco 一致;每个形态带 source 标出来历。"
            + (
                f" 快照已应用 {patch.get('label', '前瞻调整')}({patch.get('date', '')}), "
                f"本表已回退 {reverted} 个形态到 before, 与现网一致。"
                if patch else " ROCOM_CALC_APPLY_PATCH=1 时保留前瞻值。"
            )
            + (f" 我方同名形态(共享同一份数据): {len(ambiguous)} 组。" if ambiguous else "")
            + f" 协议实测锚点(最高优先级)覆盖 {len(ANCHORS)} 个形态"
            + (f", 其中 {len(conflicts)} 个与官方表/快照冲突(已按锚点取值,见生成时输出)。" if conflicts else ", 全部一致。")
        ),
        "stats": STAT_KEYS,
        "forms": out,
    }, len(out), len(petbase), ambiguous, conflicts


def build_official_skills():
    """官方客户端 `SKILL_CONF` → 技能数值。**按 id 直取**,不靠中文名桥接,故覆盖率远高于 roco 快照那条路。

    字段口径(实测核对过):
      dam_para[0] = 威力(天洪 150 / 突袭 70 / 虫击 90,与 roco 的 basePower 一致);多个取值的(如魔能爆
      [1, 450000, 700000…])是随能量/条件变化的动态威力,**不能**取第一个当威力,一律留 None 并标 dynamic=True
      energy_cost[0] = 能耗;skill_dam_type = 系别(1..21,与 names.json 的 skill_dam_type 同一套编号)
      type: 1=主动 2=被动;damage_type: 1=无伤害 2=物攻 3=魔攻

    ⚠️ 段数(连击)不在这张表里:SKILL_CONF 的 hit_para 恒为 10000(即 ×1),描述里的「3连击」对应的段数在别处,
    官方表里没找到 —— 这条仍是缺口,别拿 hit_para 当段数用。
    """
    rows = require_conf("SKILL_CONF.json")
    # 系别名以官方 `TYPE_DICTIONARY` 为准(「普通」),**不用** names.json 的简称(「普」):
    # 克制矩阵 calc_types.json 的 types 用的是全称,两边不一致时前端 indexOf 会返回 -1,
    # 静默退化成「无克制」—— 这类错在页面上看不出来,只能靠口径统一来防。
    # 缺解包数据时才回退简称(那本来就是旧行为)。
    dam_names = official_type_names() or (load_json(DATA / "names.json").get("skill_dam_type") or {})

    out = {}
    for sid, r in rows.items():
        if not isinstance(r, dict):
            continue
        try:
            i = int(sid)
        except (TypeError, ValueError):
            continue
        dam = r.get("dam_para") or []
        power = None
        dynamic = False
        if len(dam) == 1 and isinstance(dam[0], int):
            power = dam[0] or None
        elif len(dam) > 1:
            dynamic = True  # 多段取值 = 条件/能量决定威力
        cost = (r.get("energy_cost") or [None])[0] if isinstance(r.get("energy_cost"), list) else r.get("energy_cost")
        dt = r.get("skill_dam_type")
        out[str(i)] = {
            "name": r.get("name") or "",
            "type": {1: "主动", 2: "被动"}.get(r.get("type"), ""),
            "category": {1: "无", 2: "物攻", 3: "魔攻"}.get(r.get("damage_type"), ""),
            "damType": dam_names.get(str(dt), ""),
            "cost": cost if isinstance(cost, int) else None,
            "basePower": power,
            "dynamicPower": dynamic,
            "source": "official-client",
        }
    return out


def build_skills(snapshot, skills, source):
    """技能 → 威力 / 系别 / 类别 / 能耗。官方表优先,roco 快照按名桥接兜底。"""
    official = build_official_skills()
    names = skills.get("names") or {}
    name_to_ids = {}
    for sid, n in names.items():
        if not n:
            continue
        name_to_ids.setdefault(n, []).append(sid)

    by_name = {}
    for s in snapshot.get("skills") or []:
        n = s.get("name")
        if n:
            by_name[n] = s

    out = {}
    for sid in names:
        # 1) 官方表有就是权威,直接采用
        if sid in official:
            out[sid] = official[sid]
            continue
        # 2) 官方没有,退回 roco 按名桥接
        n = names[sid]
        sk = by_name.get(n)
        if not sk:
            continue
        power = sk.get("basePower")
        if not power:
            power = None
        # ⚠️ roco 快照的 `type` 字段是**系别**,而官方条目的 `type` 是「主动/被动」、
        # 系别在 `damType` —— 同名不同义,取错就全盘错。这里两个字段都填成系别。
        out[sid] = {
            "name": n,
            "type": sk.get("type") or "",
            "category": sk.get("category") or "",
            "damType": sk.get("type") or "",
            "cost": sk.get("cost"),
            "basePower": power,
            "ruleId": sk.get("ruleId"),
            "rocoId": sk.get("id", ""),
            "source": "roco-snapshot",
        }

    return {
        "_source": source,
        "_note": (
            f"技能数值: 共 {len(out)} 条(官方客户端 {sum(1 for v in out.values() if v.get('source') == 'official-client')} 条按 id 直取,"
            "其余为 roco 快照按中文名桥接兜底)。"
            "basePower 为 null = 无威力(变化/防御类);dynamicPower=true = 威力随条件变化(如魔能爆按能量),不能取单一威力。"
            "⚠️ 连击段数官方表里没找到(SKILL_CONF 的 hit_para 恒为 10000=×1),仍是缺口,靠协议内的 cast_cnt 兜底。"
        ),
        "skills": out,
    }, len(out), len(names)


def build_types(snapshot, source):
    """18 系克制关系 + 钳制规则 + 伤害下限。官方 TYPE_DICTIONARY 优先,快照兜底。"""
    types, matrix = official_types()
    origin = "官方客户端 TYPE_DICTIONARY(按 id 直取)"
    if not types or not matrix:
        tc0 = snapshot.get("typeChart") or {}
        types, matrix = tc0.get("types"), tc0.get("matrix")
        origin = "roco 快照 typeChart"
    if not types or not matrix:
        types, matrix = BUILTIN_TYPES, BUILTIN_MATRIX
        origin = "内置关系表(官方表与快照都缺)"

    # 官方与快照对拍:内容本应一致,不一致说明某侧口径变了 —— 要让人看见,而不是静默取一个。
    diff_note = ""
    tc = snapshot.get("typeChart") or {}
    st, sm = tc.get("types"), tc.get("matrix")
    if st and sm and st == types and len(sm) == len(matrix):
        bad = sum(
            1 for i, row in enumerate(sm) for j, v in enumerate(row) if abs(v - matrix[i][j]) > 1e-9
        )
        diff_note = f" 与 roco 快照逐格对拍: {'完全一致' if bad == 0 else f'{bad} 格不一致(以官方为准)'}。"

    floor = official_damage_floor()
    return {
        "_source": source,
        "_note": (
            "属性克制: 单系 ×2 / ×0.5, 双系相乘后钳制(raw>=4 记 3, raw<=0.25 记 0.25), "
            f"与 roco-calculator 的 type-chart.js 一致。来源: {origin}。" + diff_note
            + (f" 伤害下限 floor={floor}(官方 ATTR_GLOBAL_CONFIG 的 phy/spe_dam_floor)。" if floor else "")
        ),
        "types": types,
        "matrix": matrix,
        "clamp": {"max": 3, "min": 0.25},
        "floor": floor,
    }


# 快照缺 typeChart 时的兜底(与 roco type-chart.js 的 BUILTIN_RELATIONS 同构,
# 由 strongAgainst/resistedBy 展开成矩阵)。
BUILTIN_RELATIONS = {
    "普通": {"strong": [], "resist": ["地", "幽", "机械"]},
    "草": {"strong": ["水", "光", "地"], "resist": ["火", "龙", "毒", "虫", "翼", "机械"]},
    "火": {"strong": ["草", "冰", "虫", "机械"], "resist": ["水", "地", "龙"]},
    "水": {"strong": ["火", "地", "机械"], "resist": ["草", "冰", "龙"]},
    "光": {"strong": ["幽", "恶"], "resist": ["草", "冰"]},
    "地": {"strong": ["火", "冰", "电", "毒"], "resist": ["草", "武"]},
    "冰": {"strong": ["草", "地", "龙", "翼"], "resist": ["火", "冰", "机械"]},
    "龙": {"strong": ["龙"], "resist": ["机械"]},
    "电": {"strong": ["水", "翼"], "resist": ["草", "地", "龙", "电"]},
    "毒": {"strong": ["草", "萌"], "resist": ["地", "毒", "幽", "机械"]},
    "虫": {"strong": ["草", "恶", "幻"], "resist": ["火", "毒", "武", "翼", "萌", "幽", "机械"]},
    "武": {"strong": ["普通", "地", "冰", "恶", "机械"], "resist": ["毒", "虫", "翼", "萌", "幽", "幻"]},
    "翼": {"strong": ["草", "虫", "武"], "resist": ["地", "龙", "电", "机械"]},
    "萌": {"strong": ["龙", "武", "恶"], "resist": ["火", "毒", "机械"]},
    "幽": {"strong": ["光", "幽", "幻"], "resist": ["普通", "恶"]},
    "恶": {"strong": ["毒", "萌", "幽"], "resist": ["光", "武", "恶"]},
    "机械": {"strong": ["地", "冰", "萌"], "resist": ["火", "水", "电", "机械"]},
    "幻": {"strong": ["毒", "武"], "resist": ["光", "机械", "幻"]},
}
BUILTIN_TYPES = list(BUILTIN_RELATIONS)
BUILTIN_MATRIX = [
    [
        2 if d in BUILTIN_RELATIONS[a]["strong"] else (0.5 if d in BUILTIN_RELATIONS[a]["resist"] else 1)
        for d in BUILTIN_TYPES
    ]
    for a in BUILTIN_TYPES
]


def write_if_changed(path, payload):
    """内容不变就不写:生成物是提交进仓库的,无谓的 mtime 变动只会制造噪音。"""
    text = json.dumps(payload, ensure_ascii=False, indent=1, sort_keys=False) + "\n"
    if path.exists():
        try:
            if path.read_text(encoding="utf-8") == text:
                return False
        except OSError:
            pass
    path.write_text(text, encoding="utf-8")
    return True


# 协议实测锚点:形态种族值必须与**游戏协议**下发的 total_race 一致,否则桥接就错了。
#
# 来源:本机解析 PCAPdroid_07_9月_23_17_59.pcap(2026-09-07)PVP 战局里我方宠物的
# battle_common_pet_info.attribute_info.*.total_race。抓包日期早于 S4 前瞻调整
# (2026.09.10),故这四组同时是「必须回退前瞻」的守门员 —— 少了它,某天快照改了
# 默认行为就会静默用上前瞻值,而协议里我方宠物的种族值却还是旧口径。
ANCHORS = {
    "3141": {"hp": 122, "physicalAttack": 67, "magicalAttack": 72, "physicalDefense": 84, "magicalDefense": 94, "speed": 100},
    "3179": {"hp": 100, "physicalAttack": 147, "magicalAttack": 137, "physicalDefense": 108, "magicalDefense": 89, "speed": 115},
    "3069": {"hp": 128, "physicalAttack": 77, "magicalAttack": 75, "physicalDefense": 101, "magicalDefense": 81, "speed": 105},
    "3400": {"hp": 81, "physicalAttack": 78, "magicalAttack": 78, "physicalDefense": 76, "magicalDefense": 96, "speed": 100},
}


def check_anchors(forms):
    bad = []
    for sid, want in ANCHORS.items():
        got = (forms.get(sid) or {}).get("race") or {}
        for k, v in want.items():
            if got.get(k) != v:
                bad.append(f"{sid}({ (forms.get(sid) or {}).get('name', '?') }).{k}: 期望 {v}, 实得 {got.get(k)}")
    if bad:
        sys.exit("种族值锚点校验失败(桥接或快照口径变了):\n  " + "\n  ".join(bad))


def main():
    names = load_json(DATA / "names.json")
    skills = load_json(DATA / "skills.json")
    try:
        snapshot = load_snapshot(SNAPSHOT)
    except Exception as exc:  # 生成期失败要给出可操作的提示
        sys.exit(f"读取快照失败: {SNAPSHOT}\n  {exc}\n(可设 ROCOM_CALC_SNAPSHOT 指向本地文件)")

    meta = snapshot.get("meta") or {}
    source = snapshot_source(meta)

    race, race_hit, race_total, ambiguous, conflicts = build_race(snapshot, names, source)
    check_anchors(race["forms"])
    sk, sk_hit, sk_total = build_skills(snapshot, skills, source)
    types = build_types(snapshot, source)

    for path, payload in ((OUT_RACE, race), (OUT_SKILLS, sk), (OUT_TYPES, types)):
        changed = write_if_changed(path, payload)
        print(f"  {path.relative_to(ROOT)}: {'已更新' if changed else '无变化'} ({path.stat().st_size // 1024} KiB)")

    srcs = {}
    for v in race["forms"].values():
        k = v.get("source") or "?"
        srcs[k] = srcs.get(k, 0) + 1
    print(
        f"快照 {meta.get('id', '?')} (BWIKI {meta.get('bwikiRevision', '?')})\n"
        f"  形态种族值: {race_hit}/{race_total} 有数据"
        + "".join(f", {k} {n} 条" for k, n in sorted(srcs.items()))
        + (f", 我方同名 {len(ambiguous)} 组" if ambiguous else "")
        + f"\n  技能数值:   {sk_hit}/{sk_total} 命中\n"
        "  许可: 数据源自 BWIKI, CC BY-NC-SA 4.0(署名/非商业/相同方式共享)"
    )
    if conflicts:
        print("  ⚠️ 种族值与协议实测锚点冲突(已按锚点取值,请复核):")
        for c in conflicts:
            print("     -", c)


if __name__ == "__main__":
    main()
