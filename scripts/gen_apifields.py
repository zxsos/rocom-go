#!/usr/bin/env python3
"""从契约测试的 golden 快照生成机器可读的字段清单 docs/api/fields.json。

golden 是跑真实 handler 落盘的响应(见 internal/server/contract_test.go),
故这份清单与线上输出必然一致 —— 将来若要统一字段命名(现 snake_case 与 camelCase 混用),
前端侧可据此做可校验的脚本替换,而不是靠 grep 猜。

用法:  uv run python scripts/gen_apifields.py
"""
import json
import pathlib
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent
GOLDEN = ROOT / "internal/server/testdata/contract"
OUT = ROOT / "docs/api/fields.json"

# 端点 -> (HTTP 方法与路径, 一句话说明)。golden 文件名与之对应。
# 只列有 golden 快照的端点;其余见 docs/api/endpoints.md。
ENDPOINTS = {
    "pets": ("GET /api/pets", "宠物列表(分页/筛选/排序)"),
    "pet-detail": ("GET /api/pets/{gid}", "单只宠物详情"),
    "pet-page": ("GET /api/pet-page", "某宠物在筛选下所处页码"),
    "stats": ("GET /api/stats", "本账号宠物总数"),
    "filter-options": ("GET /api/filter-options", "筛选下拉可选值"),
    "boxes": ("GET /api/boxes", "盒子槽位布局"),
    "teams": ("GET /api/teams", "大世界三队布局"),
    "events": ("GET /api/events", "获得事件历史"),
    "events-stats": ("GET /api/events/stats", "事件统计"),
    "icons": ("GET /api/icons", "全局固定图标(不随账号)"),
    "name-options": ("GET /api/name-options", "全量特长名"),
    "medals": ("GET /api/medals", "全部奖牌(不随账号)"),
    "evolution": ("GET /api/evolution", "某 petbase 的进化链"),
    "eggs": ("GET /api/eggs", "背包精灵蛋"),
    "handbook-glasses": ("GET /api/handbook-glasses", "图鉴炫彩收集"),
    "position-fresh": ("GET /api/position", "最近位置(未过期,含速度与轨迹)"),
    "position-stale": ("GET /api/position", "最近位置(已过期,抹掉 vu/vv/path)"),
    "wildpets": ("GET /api/wildpets", "最近一次野生宠物标记"),
    "home": ("GET /api/home", "最近一次家园小窝图层"),
    "flowers": ("GET /api/flowers", "最近一次花种分组(已剥 cur/worlds)"),
    "flowers-slots": ("GET /api/flowers/slots", "花种世界存档槽位列表"),
    "trial": ("GET /api/trial", "最近一次草系徽章试炼状态"),
    "trial-encounters": ("GET /api/trial/encounters", "草系试炼遇见记录(三章精灵图,读库累积)"),
}


def type_of(v):
    if v is None:
        return "null"
    if isinstance(v, bool):
        return "bool"
    if isinstance(v, int):
        return "int"
    if isinstance(v, float):
        return "float"
    if isinstance(v, str):
        return "str"
    if isinstance(v, list):
        return "array"
    if isinstance(v, dict):
        return "object"
    return type(v).__name__


def shape(v):
    """递归描述结构:字段名 -> 类型;对象/数组展开一层子元素。"""
    if isinstance(v, dict):
        return {k: shape(x) for k, x in v.items()}
    if isinstance(v, list):
        if not v:
            return "array<empty>"
        # 数组内元素若结构不一,取并集(如 nests 里一窝有 pet、一窝有 egg)。
        merged = {}
        for item in v:
            s = shape(item)
            if isinstance(s, dict):
                for k, sub in s.items():
                    merged.setdefault(k, sub)
        return [merged]
    return type_of(v)


def main():
    if not GOLDEN.is_dir():
        sys.exit(f"找不到 golden 目录 {GOLDEN}。先跑:\n"
                 "  UPDATE_CONTRACT=1 go test ./internal/server/ -run TestContract")
    out = {}
    for path in sorted(GOLDEN.glob("*.json")):
        name = path.stem
        data = json.loads(path.read_text(encoding="utf-8"))
        method_path, desc = ENDPOINTS.get(name, ("", ""))
        out[name] = {
            "endpoint": method_path,
            "desc": desc,
            "sample": path.relative_to(ROOT).as_posix(),
            "shape": shape(data),
        }
    OUT.parent.mkdir(parents=True, exist_ok=True)
    OUT.write_text(
        json.dumps(out, ensure_ascii=False, indent=2, sort_keys=True) + "\n",
        encoding="utf-8",
    )
    print(f"已生成 {OUT.relative_to(ROOT)} ({len(out)} 个端点)")


if __name__ == "__main__":
    main()
