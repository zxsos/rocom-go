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


def build_race(snapshot, names, source):
    """形态 → 种族值 + 系别。"""
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
        }
        for sid in ids:  # 同名形态(极少)共享一份数据
            out[str(sid)] = entry
    used = {s.get("id") for s in (snapshot.get("spirits") or []) if s.get("fullName") in name_to_ids}
    src = dict(source)
    if patch:
        src["patchReverted"] = f"{patch.get('id', '')}（{patch.get('label', '')}，{patch.get('date', '')}）已回退到 before"
    return {
        "_source": src,
        "_note": (
            f"形态种族值: {len(out)} / {len(petbase)} 个 petbase 命中 roco 快照"
            f"(共 {len(snapshot.get('spirits') or [])} 形态, 用到 {len(used)} 个)。"
            "未命中多为未进化小形态, 一律留空不猜。race 六维键与 roco 一致。"
            + (
                f" 快照已应用 {patch.get('label', '前瞻调整')}({patch.get('date', '')}), "
                f"本表已回退 {reverted} 个形态到 before, 与现网一致。"
                if patch else " ROCOM_CALC_APPLY_PATCH=1 时保留前瞻值。"
            )
            + (f" 我方同名形态(共享同一份数据): {len(ambiguous)} 组。" if ambiguous else "")
        ),
        "stats": STAT_KEYS,
        "forms": out,
    }, len(out), len(petbase), ambiguous


def build_skills(snapshot, skills, source):
    """技能 → 威力 / 系别 / 类别 / 能耗。"""
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
    for n, ids in name_to_ids.items():
        sk = by_name.get(n)
        if not sk:
            continue
        # roco 用 0 表示「无威力」(变化/防御类),与我们 skills.json 的 '—' 同义。
        # 归一成 null:0 与「没收录」在 JSON 里看不出区别,而「这技能没有威力」
        # 与「我们不知道威力」对计算是两回事。
        power = sk.get("basePower")
        if not power:
            power = None
        entry = {
            "name": n,
            "type": sk.get("type") or "",
            "category": sk.get("category") or "",
            "cost": sk.get("cost"),
            "basePower": power,
            "ruleId": sk.get("ruleId"),
            "rocoId": sk.get("id", ""),
        }
        for sid in ids:
            out[str(sid)] = entry
    used = {s.get("id") for s in (snapshot.get("skills") or []) if s.get("name") in name_to_ids}
    return {
        "_source": source,
        "_note": (
            f"技能数值: {len(out)} / {len(names)} 个已知 skill_id 命中 roco 快照"
            f"(共 {len(snapshot.get('skills') or [])} 技能, 用到 {len(used)} 个)。"
            "basePower 为 null 表示无威力技能(roco 的 0 已归一为 null, 与「没收录」区分开);"
            "category: physical 物攻 / magical 魔攻 / status 变化 / defense 防御。"
            "未命中留空, 由协议内的实时威力参数(skill_round_data / 0x1324 damage_param_result)兜底。"
        ),
        "skills": out,
    }, len(out), len(names)


def build_types(snapshot, source):
    """18 系克制关系 + 钳制规则。"""
    tc = snapshot.get("typeChart") or {}
    types, matrix = tc.get("types"), tc.get("matrix")
    fallback = False
    if not types or not matrix:
        fallback = True
        types, matrix = BUILTIN_TYPES, BUILTIN_MATRIX
    return {
        "_source": source,
        "_note": (
            "属性克制: 单系 ×2 / ×0.5, 双系相乘后钳制(raw>=4 记 3, raw<=0.25 记 0.25), "
            "与 roco-calculator 的 type-chart.js 一致。"
            + ("快照未提供 typeChart, 已回退内置关系表。" if fallback else "")
        ),
        "types": types,
        "matrix": matrix,
        "clamp": {"max": 3, "min": 0.25},
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

    race, race_hit, race_total, ambiguous = build_race(snapshot, names, source)
    check_anchors(race["forms"])
    sk, sk_hit, sk_total = build_skills(snapshot, skills, source)
    types = build_types(snapshot, source)

    for path, payload in ((OUT_RACE, race), (OUT_SKILLS, sk), (OUT_TYPES, types)):
        changed = write_if_changed(path, payload)
        print(f"  {path.relative_to(ROOT)}: {'已更新' if changed else '无变化'} ({path.stat().st_size // 1024} KiB)")

    print(
        f"快照 {meta.get('id', '?')} (BWIKI {meta.get('bwikiRevision', '?')})\n"
        f"  形态种族值: {race_hit}/{race_total} 命中"
        + (f", 我方同名 {len(ambiguous)} 组" if ambiguous else "")
        + f"\n  技能数值:   {sk_hit}/{sk_total} 命中\n"
        "  许可: 数据源自 BWIKI, CC BY-NC-SA 4.0(署名/非商业/相同方式共享)"
    )


if __name__ == "__main__":
    main()
