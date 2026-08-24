import gzip
from google.protobuf import descriptor_pb2

fds = descriptor_pb2.FileDescriptorSet()
fds.ParseFromString(gzip.open('internal/pbdesc/data/proto.desc.gz', 'rb').read())

def find(name):
    for f in fds.file:
        stack = [m for m in f.message_type]
        while stack:
            m = stack.pop()
            if m.name == name:
                return m
            stack.extend(m.nested_type)
    return None

def dump(m, indent='', seen=None):
    seen = seen or set()
    print(f'{indent}{m.name} {{')
    for fd in m.field:
        t = fd.type_name.split('.')[-1] if fd.type_name else str(fd.type)
        extra = ''
        if fd.type_name and fd.type in (11, 18):  # message/group
            sub = find(t)
            if sub and t not in seen:
                seen.add(t)
                extra = '  # nested below'
        print(f'{indent}  {fd.number:<4} {fd.type:<12} {fd.name} = {t}{extra}')
    print(f'{indent}}}')
    for fd in m.field:
        if fd.type_name and fd.type in (11, 18):
            t = fd.type_name.split('.')[-1]
            sub = find(t)
            if sub and t not in seen:
                seen.add(t)
                dump(sub, indent + '  ', seen)

m = find('TeamBattleInfo')
if m:
    dump(m)
