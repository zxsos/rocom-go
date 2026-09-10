"""解包数据源的公共入口:定位并读取官方客户端解出的数值表。

各 gen_*.py 都需要「读 ~/Downloads/rocom/parsed 下 BinDataCompressed/*.json"。
以前每个脚本各写一遍路径拼接与容错,现在收在一处:
  - 解包根:环境变量 ROCOM_PARSED,默认 ~/Downloads/rocom/parsed
  - 表名:BinDataCompressed/<NAME>_CONF.json(RocoBinData 经 scripts/bin2json.py 解码后的形状:{RocoDataRows:{...}})

为什么单独成文件:路径与「缺数据时怎么报错」的文案在一处,换游戏版本时只改这里。
"""
import json
import os
import pathlib
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent
DEFAULT_PARSED = pathlib.Path.home() / "Downloads" / "rocom" / "parsed"

# 数值表目录(相对解包根)
BIN_SUBDIR = pathlib.Path("NRC/Content/ScriptC/Data/Bin/BinDataCompressed")

OFFICIAL_NOTE = (
    "数据来自官方安卓客户端(com.tencent.nrc)解包出的配置表;"
    "仅供本非商业玩家工具本地统计使用,不重新分发游戏素材。"
)


def parsed_root():
    return pathlib.Path(os.environ.get("ROCOM_PARSED", DEFAULT_PARSED)).expanduser()


def conf_path(name):
    """返回某张数值表的路径(不论是否存在)。"""
    return parsed_root() / BIN_SUBDIR / name


def load_conf(name):
    """读取数值表,返回行字典 {id: row};缺文件或格式不对时抛 MissingGameData。"""
    p = conf_path(name)
    if not p.exists():
        raise MissingGameData(name, p)
    with open(p, encoding="utf-8") as f:
        d = json.load(f)
    rows = d.get("RocoDataRows") if isinstance(d, dict) else None
    if not isinstance(rows, dict):
        raise MissingGameData(name, p, "格式不是 {RocoDataRows:{...}}")
    return rows


def require_conf(name):
    """读取数值表;缺数据时打印可操作的提示并退出(2)。"""
    try:
        return load_conf(name)
    except MissingGameData as e:
        print(f"错误: {e}", file=sys.stderr)
        print(
            "  需要先解包官方客户端(见 docs/apk-unpack-notes.md);或设 ROCOM_PARSED 指向解包根。",
            file=sys.stderr,
        )
        sys.exit(2)


class MissingGameData(Exception):
    def __init__(self, name, path, why="文件不存在"):
        super().__init__(f"缺官方数值表 {name}({path}):{why}")

    pass
