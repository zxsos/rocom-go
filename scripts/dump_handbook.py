"""临时工具:从 internal/pbdesc/data/proto.desc.gz 导出 handbook 相关消息字段号(不提交)。"""
import gzip
import sys

from google.protobuf import descriptor_pb2 as dpb

with gzip.open("internal/pbdesc/data/proto.desc.gz", "rb") as f:
    fds = dpb.FileDescriptorSet.FromString(f.read())

index = {}  # 全名 -> DescriptorProto


def walk(f, prefix, m):
    full = prefix + "." + m.name
    index[full] = m
    for n in m.nested_type:
        walk(f, full, n)


for f in fds.file:
    prefix = "." + f.package if f.package else ""
    for m in f.message_type:
        walk(f, prefix, m)

targets = [
    ".Next.PetHandbook",
    ".Next.HandbookRecordCollection",
    ".Next.HandbookRecord",
    ".Next.GlassInfo",
]

KIND = {
    dpb.FieldDescriptorProto.TYPE_MESSAGE: "msg",
    dpb.FieldDescriptorProto.TYPE_ENUM: "enum",
    dpb.FieldDescriptorProto.TYPE_INT32: "i32",
    dpb.FieldDescriptorProto.TYPE_INT64: "i64",
    dpb.FieldDescriptorProto.TYPE_UINT32: "u32",
    dpb.FieldDescriptorProto.TYPE_UINT64: "u64",
    dpb.FieldDescriptorProto.TYPE_BOOL: "bool",
    dpb.FieldDescriptorProto.TYPE_STRING: "str",
    dpb.FieldDescriptorProto.TYPE_BYTES: "bytes",
    dpb.FieldDescriptorProto.TYPE_FIXED32: "fx32",
    dpb.FieldDescriptorProto.TYPE_FIXED64: "fx64",
}

for t in targets:
    m = index.get(t)
    if m is None:
        print(f"== {t}: <未找到>")
        continue
    print(f"== {t}")
    for fd in m.field:
        lab = "rep" if fd.label == dpb.FieldDescriptorProto.LABEL_REPEATED else "opt"
        kind = KIND.get(fd.type, str(fd.type))
        print(f"  #{fd.number} {fd.name} {lab} {kind} -> {fd.type_name}")

    # 打印嵌套枚举
    for e in m.enum_type:
        vals = ", ".join(f"{v.name}={v.number}" for v in e.value)
        print(f"  enum {e.name}: {vals}")
