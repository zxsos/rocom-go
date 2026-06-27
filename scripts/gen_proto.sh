#!/usr/bin/env bash
# 从 world-data 导出的 .proto 生成宠物相关 Go 结构体。
# 游戏 .proto 无 go_package，用 M 映射把闭包内全部文件指向同一 Go 包。
set -euo pipefail

SRC="${ROCO_PROTO_SRC:-$HOME/Git/gh/Roco-Kingdom-World-Data-2026-05-21/pakchunk4-WindowsNoEditor/PB/proto_out}"
PKG="github.com/whoisnian/rocom-capture/internal/pb"
OUT="internal/pb"
export PATH="$(go env GOPATH)/bin:$PATH"

FILES=(com_pet.proto com_base_types.proto com_monster.proto com_pet_skill.proto rpc_options.proto xls_enum.proto)

# 修复 proto(补 syntax、enum 补零)到项目 proto/ 目录
echo "修复 proto 源 -> proto/"
python3 scripts/fix_proto.py "$SRC" proto

MAPPINGS=()
for f in "${FILES[@]}"; do MAPPINGS+=("--go_opt=M${f}=${PKG}"); done
MAPPINGS+=("--go_opt=Mgoogle/protobuf/descriptor.proto=google.golang.org/protobuf/types/descriptorpb")

mkdir -p "$OUT"
protoc -I=proto -I=/usr/include \
  --go_out="$OUT" --go_opt=paths=source_relative \
  "${MAPPINGS[@]}" \
  "${FILES[@]}"

echo "生成完成:"
ls -la "$OUT"/*.pb.go
