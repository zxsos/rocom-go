"""抓取草系徽章试炼的静态配置(Lua 数据模块),存到 ~/Downloads/rocom/grassTrialData.lua。

为什么不刮「草系徽章试炼」那个 HTML 页面:页面正文由 `{{#invoke:GrassTrial|render}}`
渲染,真正的结构化数据在 `Module:GrassTrialData`(130 KB,带 base_id)里。
MediaWiki 的 `action=parse&prop=wikitext` 能直接取到模块源码,比解析 HTML 稳得多。

这份数据提供协议里**没有**的三样东西:

  1. 各章的精灵池(188/295/177 只,带 base_id,能直接对上我方 petbase 显示头像);
  2. 22 名首领;
  3. 第 7 层 NPC 的候选阵容(3 难度 × 3 章),以及每层是什么类型。

数据来源: https://wiki.biligame.com/rocom/草系徽章试炼
          → Module:GrassTrialData(通过 MediaWiki API 取 wikitext)
授权: CC BY-NC-SA 4.0(站点声明)。生成物里会标注来源与更新时间。

运行:  uv run python scripts/fetch_trial_data.py
输出:  ROCOM_TRIAL_DATA,默认 ~/Downloads/rocom/grassTrialData.lua
       (随后跑 scripts/gen_trial.py 生成 internal/gamedata/data/trial.json)

模块改版时:本脚本只做「取 wikitext 存盘」,不做解析 —— 解析在 gen_trial.py。
若模块结构变化,gen_trial.py 会报错(解析出 0 章/0 只),按新的 Lua 结构改它即可。
"""
import os
import sys
import urllib.error
import urllib.request

API = "https://wiki.biligame.com/rocom/api.php"
PAGE = "Module:GrassTrialData"
OUT_PATH = os.environ.get(
    "ROCOM_TRIAL_DATA", os.path.expanduser("~/Downloads/rocom/grassTrialData.lua")
)


def fetch_wikitext(page):
    import json as _json
    import urllib.parse

    url = (
        f"{API}?action=parse&page={urllib.parse.quote(page)}"
        "&prop=wikitext&format=json&formatversion=2"
    )
    req = urllib.request.Request(url, headers={"User-Agent": "Mozilla/5.0"})
    with urllib.request.urlopen(req, timeout=40) as r:
        raw = _json.load(r)
    if "error" in raw:
        raise RuntimeError(f"API 报错: {raw['error'].get('info')}")
    wt = raw.get("parse", {}).get("wikitext")
    if not wt:
        raise RuntimeError(f"模块 {page} 没有返回 wikitext(页面被删或改名?)")
    return wt


def main():
    print(f"抓取 {PAGE} …")
    try:
        text = fetch_wikitext(PAGE)
    except urllib.error.URLError as e:
        sys.exit(f"请求失败: {e}")
    except RuntimeError as e:
        sys.exit(str(e))

    # 最低限度的完整性检查:文件头 + 三个顶层字段。真正的解析在 gen_trial.py,
    # 这里只挡住「抓到个错误页/空页」的情况,避免把垃圾写盘。
    if "return {" not in text:
        sys.exit("不像 Lua 数据模块(没有 'return {')—— 页面结构可能变了")
    missing = [k for k in ("pets = {", "chapters = {", "modes = {") if k not in text]
    if missing:
        sys.exit(f"缺顶层字段 {missing} —— 模块可能改版,请检查 {PAGE}")

    os.makedirs(os.path.dirname(OUT_PATH), exist_ok=True)
    with open(OUT_PATH, "w", encoding="utf-8") as f:
        f.write(text)
    print(f"已写入 {OUT_PATH}: {len(text)} 字节")
    print("下一步: uv run python scripts/gen_trial.py")


if __name__ == "__main__":
    main()
