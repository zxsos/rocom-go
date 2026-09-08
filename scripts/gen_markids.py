#!/usr/bin/env python3
"""生成 buff_id → 印记/状态名 的映射(calc_mark_ids.json)。

为什么需要它:协议里印记是**数字 buff_id**(如 20010090),而 roco-calculator 的
印记 id 是字符串 slug("momentum"/"tailwind")—— 两者对不上,那份站里没有。

两条路子,可信度不同,输出里逐条标出 source 与证据:

  1. `--from-pcap`(无需解包数据,**对拍推断**)
     技能描述里写「敌方获得5层冻结」,而该技能施放后目标身上出现了层数=5 的
     buff —— 层数能对上就是一条候选。可信度 medium,**需人工复核**:
     一次施放常伴随多个 buff(能量/奉献/天气…),层数巧合也会误配。
  2. `--from-parsed`(需解包数据,**权威**)
     游戏解包配置里的 BUFF 表,id 与名字直接对应。可信度 high。
     需在有解包数据的机器上跑(ROCOM_PARSED)。

两种模式**合并写入**,同 id 冲突时以权威来源为准。

用法:
  # 对拍推断(需要 cmd/markprobe 先产出 tsv)
  go run ./cmd/markprobe -pcap 你的.pcap > /tmp/marks.tsv
  uv run python scripts/gen_markids.py --from-pcap /tmp/marks.tsv

  # 权威(有解包数据的机器上)
  ROCOM_PARSED=/path/to/parsed uv run python scripts/gen_markids.py --from-parsed
"""
import argparse
import json
import os
import pathlib
import re
import sys
import urllib.request

ROOT = pathlib.Path(__file__).resolve().parent.parent
DATA = ROOT / "internal" / "gamedata" / "data"
PARSED = pathlib.Path(os.environ.get("ROCOM_PARSED", pathlib.Path.home() / "Downloads" / "rocom" / "parsed"))
OUT = DATA / "calc_mark_ids.json"
MARKS = DATA / "calc_marks.json"

from gamedata_sources import conf_path, require_conf  # noqa: E402  (需先拼好 ROOT/DATA)
WEATHER_FILE = conf_path("WEATHER_CONF.json")
S3 = "https://raw.githubusercontent.com/Evenstar-tools/roco-calculator/main/data/skill-query/s3-source.json"

# 「(自己|敌方|对方|己方队伍)获得N层X」—— 与 roco marks.js 的提取正则同思路,
# 但放宽到**状态**(冻结/灼烧)而不只是「印记」二字结尾的。
PAT = re.compile(r"(自己|敌方|对方|己方队伍)获得(\d+)层([^，。；：]+?)(?:印记)?(?=[，。；：]|$)")


def known_names():
    """已登记的印记名(用于判断「这个名字我们认不认」——不认的也照样登记,
    但会在 note 里说明规则尚未接入。"""
    try:
        return {m["name"] for m in json.load(open(MARKS, encoding="utf-8"))["marks"]}
    except Exception:
        return set()


def load_skill_desc():
    ours = json.load(open(DATA / "skills.json", encoding="utf-8"))["descs"]
    try:
        src = json.loads(urllib.request.urlopen(S3, timeout=120).read())
        roco = {s["gameId"]: s for s in src["skills"]}
    except Exception as exc:
        print(f"  (取 roco 技能表失败,只用本地 descs: {exc})")
        roco = {}
    names = json.load(open(DATA / "skills.json", encoding="utf-8"))["names"]

    def desc(sid):
        if str(sid) in ours:
            return ours[str(sid)]
        r = roco.get(str(sid))
        return r["description"] if r else None

    def name(sid):
        r = roco.get(str(sid))
        return r["name"] if r else names.get(str(sid), "?")

    return desc, name


def from_pcap(tsv_paths):
    desc_of, name_of = load_skill_desc()
    cand, ev = {}, {}
    for path in tsv_paths:
        for line in open(path, encoding="utf-8"):
            p = line.rstrip("\n").split("\t")
            if len(p) < 4 or p[0] in ("0", "skill_id"):
                continue
            sid, buff, stack = int(p[0]) // 100, p[2], int(p[3] or 0)
            d = (desc_of(sid) or "").replace(" ", "")
            for m in PAT.finditer(d):
                n, nm = int(m.group(2)), m.group(3)
                # 层数能对上(等于 N 或是 N 的整数倍)才算候选 —— 这是唯一的判别依据
                if stack and (stack == n or stack % n == 0):
                    cand.setdefault(buff, {}).setdefault(nm, 0)
                    cand[buff][nm] += 1
                    ev.setdefault(buff, set()).add(name_of(sid))
    out = {}
    for buff, c in cand.items():
        nm, n = max(c.items(), key=lambda kv: kv[1])
        out[buff] = {
            "name": nm,
            "source": "pcap-correlation",
            "confidence": "medium" if len(c) == 1 else "low",
            "evidence": sorted(ev.get(buff, [])),
            "note": f"技能描述「获得N层{nm}」与 buff 层数对上 ×{n}"
            + (f";同 id 另有候选 {[k for k in c if k != nm]}" if len(c) > 1 else ""),
        }
    return out


def from_parsed():
    """权威来源:官方客户端解出的 BUFF_CONF(3000+ 条 id → 名字 + 描述)。

    以前只能靠 pcap 对拍推断(medium),且**推断错过**:把 20070020 判成冻结,官方是**灼烧**;
    冻结其实是 20580010。故官方表一来,medium 一律被 high 覆盖(见 main 的合并规则)。
    """
    names = known_names()
    buffs = require_conf("BUFF_CONF.json")
    found = {}
    for bid, r in buffs.items():
        if not isinstance(r, dict):
            continue
        nm = str(r.get("name") or "")
        if not nm:
            continue
        try:
            i = int(bid)
        except (TypeError, ValueError):
            continue
        info = {
            "name": nm,
            "source": "official-client",
            "confidence": "high",
            "evidence": [],
            "buffType": r.get("type"),
        }
        desc = str(r.get("desc") or "")
        if desc:
            info["desc"] = desc
        # 名字正好是已登记印记 → 顺带标出来(规则表里有效果系数);否则只登记名字(前端能显示,规则未接入)
        info["ruleRegistered"] = nm in names
        found[str(i)] = info
    return found


def weather_section():
    """天气:WEATHER_CONF 给 `weather_type → 名字 + 该天气挂的 buff`。

    有了它就能从场上的 buff 反推当前天气(暴风雪 = buff 20170910/20171230),
    天气的伤害修正才有输入。
    """
    rows = require_conf("WEATHER_CONF.json")
    out = {}
    for k, r in rows.items():
        if not isinstance(r, dict):
            continue
        try:
            i = int(k)
        except (TypeError, ValueError):
            continue
        wt = r.get("weather_type", i)
        out[str(wt)] = {
            "name": r.get("name") or "",
            "buffs": [int(x) for x in (r.get("weather_buff") or []) if isinstance(x, int)],
        }
    return out


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--from-pcap", nargs="*", default=[], help="cmd/markprobe 产出的 tsv")
    ap.add_argument("--from-parsed", action="store_true", help="从解包配置读(权威)")
    args = ap.parse_args()

    merged = {}
    if OUT.exists():
        try:
            merged = json.load(open(OUT, encoding="utf-8")).get("buffs", {})
        except Exception:
            merged = {}
    added = 0
    for mode, got in (("parsed", from_parsed() if args.from_parsed else {}),
                      ("pcap", from_pcap(args.from_pcap) if args.from_pcap else {})):
        for bid, info in got.items():
            old = merged.get(bid)
            # 权威覆盖推断;同可信度时保留证据更多的那条
            if old and old.get("confidence") == "high" and info.get("confidence") != "high":
                continue
            if old != info:
                merged[bid] = info
                added += 1
    if not args.from_parsed and not args.from_pcap:
        print("(未指定来源,保持原表不变。用法见文件头)")
        return

    # 天气单独一段:它不是 buff,而是「哪个 buff 代表哪种天气」的反查表。
    weather = weather_section() if (args.from_parsed or WEATHER_FILE.exists()) else {}
    payload = {
        "_source": {
            "upstream": "官方客户端解包配置(权威) + pcap 对拍推断(候选)",
            "note": ("roco 的印记 id 是字符串 slug,与协议 buff_id 无关。"
                     "官方 BUFF_CONF 直给 id → 名字;pcap 推断仅作候选(层数对拍),需复核"),
        },
        "_note": (f"buff_id → 印记/状态名,共 {len(merged)} 条;天气 {len(weather)} 条。"
                  "未收录的 buff 前端显示原始 id,不猜名字。"),
        "buffs": merged,
        "weather": weather,
    }
    OUT.write_text(json.dumps(payload, ensure_ascii=False, indent=1) + "\n", encoding="utf-8")
    print(f"已写入 {OUT.relative_to(ROOT)}: {len(merged)} 条(本次新增/更新 {added})")
    for bid, info in sorted(merged.items()):
        print(f"  {bid} → {info['name']} [{info['confidence']}] 证据: {info.get('evidence') or info.get('source')}")
    missing = sorted(known_names() - {i["name"] for i in merged.values()})
    if missing:
        print(f"  仍未覆盖的印记: {missing}")


if __name__ == "__main__":
    main()
