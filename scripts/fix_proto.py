"""把 world-data 导出的 .proto 修成可被 protoc(proto3) 编译的形式。

游戏 .proto 缺 `syntax`，且大量 enum 非 0 起始。本脚本：
1. 复制闭包内文件到项目 proto/；
2. 行首补 `syntax = "proto3";`；
3. 给每个 enum 插入 0 占位值；仅当原 enum 含 0 值或有重复值时才加 allow_alias。

只用于让 protoc 生成 Go 结构体做字段解码，不改变任何已有字段/值的语义。
"""
import os
import re
import sys

SRC = sys.argv[1]
OUT = sys.argv[2]
FILES = ["com_pet", "com_base_types", "com_monster",
         "com_pet_skill", "rpc_options", "xls_enum"]

os.makedirs(OUT, exist_ok=True)
enum_open = re.compile(r'^(\s*)enum\s+(\w+)\s*\{\s*$')
val_re = re.compile(r'^\s*\w+\s*=\s*(-?\d+)\s*;')


def fix_enum(header, indent, ename, body_lines):
    """body_lines: enum 内部到 '}' 之前的行。返回插入了占位/alias 的完整块行列表。"""
    values = [int(m.group(1)) for ln in body_lines if (m := val_re.match(ln))]
    need_alias = (0 in values) or (len(values) != len(set(values)))
    out = [header]
    if need_alias:
        out.append(f"{indent}  option allow_alias = true;")
    out.append(f"{indent}  {ename}__PB3_ZERO = 0;")
    out.extend(body_lines)
    return out


for name in FILES:
    src_lines = open(os.path.join(SRC, name + ".proto"),
                     encoding="utf-8", errors="ignore").read().splitlines()
    out_lines = ['syntax = "proto3";', '']
    i = 0
    while i < len(src_lines):
        line = src_lines[i]
        m = enum_open.match(line)
        if m:
            indent, ename = m.group(1), m.group(2)
            body = []
            j = i + 1
            while j < len(src_lines) and src_lines[j].strip() != '}':
                body.append(src_lines[j])
                j += 1
            out_lines.extend(fix_enum(line, indent, ename, body))
            out_lines.append(src_lines[j] if j < len(src_lines) else f"{indent}}}")
            i = j + 1
            continue
        out_lines.append(line)
        i += 1
    with open(os.path.join(OUT, name + ".proto"), "w", encoding="utf-8") as f:
        f.write("\n".join(out_lines) + "\n")
    print(f"  fixed {name}.proto")
