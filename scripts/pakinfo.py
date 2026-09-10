#!/usr/bin/env python3
"""pakinfo.py — 诊断游戏 pak 能否用当前主密钥解开(标准库实现,零依赖)。

用途:游戏更新后先跑它判断「换没换密钥」,再决定要不要重跑 unpack.sh ——
全量解包要几十分钟,而挂载失败只会表现为「0 个文件」,看不出根因。

对每个 pak 打印:版本、索引是否加密、索引偏移,并用内置主密钥(可 --aes 覆盖)
按 RocoKingdomWorld 变体解密索引头,解出合法的 mount point 才算密钥有效。

  RocoKingdomWorld 变体(与 CUE4Parse 的 RocoKingdomWorldAes 一致):
    1. 密钥 byte_reverse:前 31 字节反转,第 32 字节不动
    2. 密文 bit_reverse:每字节位序反转
    3. AES-256-ECB 解密
  索引头解密后开头是 UE 的 FString mount point(int32 长度 + 路径 + \\0),
  据此判定密钥是否有效(比"解出一堆乱码"可靠)。

用法:
  uv run python scripts/pakinfo.py "<Paks 目录>"      # 扫描目录内所有 *.pak
  uv run python scripts/pakinfo.py game.apk           # 读 apk 内嵌的 main.obb.png
  uv run python scripts/pakinfo.py a.pak --aes 0x...  # 换密钥试

退出码:0=至少一个 pak 能用给定密钥解开;1=全部解不开(密钥失效或格式变了)。
"""

import argparse
import hashlib
import io
import os
import struct
import sys
import zipfile

DEFAULT_AES = "0x34254D23E47299B3B7F6C4CFDE9BD0688703446D9D8F37B2EBDDDE5B06ED5ADF"
PAK_MAGIC = 0x5A6F12E1

# ── AES-256(只需解密,单块即可校验密钥)──────────────────────────────
SBOX = bytes.fromhex(
    "637c777bf26b6fc53001672bfed7ab76ca82c97dfa5947f0add4a2af9ca472c0"
    "b7fd9326363ff7cc34a5e5f171d8311504c723c31896059a071280e2eb27b275"
    "09832c1a1b6e5aa0523bd6b329e32f8453d100ed20fcb15b6acbbe394a4c58cf"
    "d0efaafb434d338545f9027f503c9fa851a3408f929d38f5bcb6da2110fff3d2"
    "cd0c13ec5f974417c4a77e3d645d197360814fdc222a908846eeb814de5e0bdb"
    "e0323a0a4906245cc2d3ac629195e479e7c8376d8dd54ea96c56f4ea657aae08"
    "ba78252e1ca6b4c6e8dd741f4bbd8b8a703eb5664803f60e613557b986c11d9e"
    "e1f8981169d98e949b1e87e9ce5528df8ca1890dbfe6426841992d0fb054bb16"
)
INV_SBOX = bytes.fromhex(
    "52096ad53036a538bf40a39e81f3d7fb7ce339829b2fff87348e4344c4dee9cb"
    "547b9432a6c2233dee4c950b42fac34e082ea16628d924b2765ba2496d8bd125"
    "72f8f66486689816d4a45ccc5d65b6926c704850fdedb9da5e154657a78d9d84"
    "90d8ab008cbcd30af7e45805b8b34506d02c1e8fca3f0f02c1afbd0301138a6b"
    "3a9111414f67dcea97f2cfcef0b4e67396ac7422e7ad3585e2f937e81c75df6e"
    "47f11a711d29c5896fb7620eaa18be1bfc563e4bc6d279209adbc0fe78cd5af4"
    "1fdda8338807c731b11210592780ec5f60517fa919b54a0d2de57a9f93c99cef"
    "a0e03b4dae2af5b0c8ebbb3c83539961172b047eba77d626e169146355210c7d"
)
RCON = [0x01, 0x02, 0x04, 0x08, 0x10, 0x20, 0x40, 0x80, 0x1B, 0x36, 0x6C, 0xD8, 0xAB, 0x4D]


def _xtime(a: int) -> int:
    a <<= 1
    return (a ^ 0x11B) & 0xFF if a & 0x100 else a


def _gmul(a: int, b: int) -> int:
    r = 0
    while b:
        if b & 1:
            r ^= a
        a, b = _xtime(a), b >> 1
    return r


def _expand_key(key: bytes) -> list[list[int]]:
    """AES-256 密钥扩展:60 个 4 字节字 → 15 个 16 字节轮密钥。"""
    words = [list(key[4 * i:4 * i + 4]) for i in range(8)]
    for i in range(8, 60):
        t = list(words[i - 1])
        if i % 8 == 0:
            t = t[1:] + t[:1]
            t = [SBOX[b] for b in t]
            t[0] ^= RCON[i // 8 - 1]
        elif i % 8 == 4:
            t = [SBOX[b] for b in t]
        words.append([words[i - 8][j] ^ t[j] for j in range(4)])
    return [[b for w in words[4 * r:4 * r + 4] for b in w] for r in range(15)]


def aes256_decrypt_block(key: bytes, block: bytes) -> bytes:
    rk = _expand_key(key)
    s = list(block)  # 列优先: s[4*c + r]
    s = [s[i] ^ rk[14][i] for i in range(16)]
    for r in range(13, 0, -1):
        # InvShiftRows:行 r 右移 r;state 按列优先存放(s[4*c + r])
        s = [s[4 * ((c - r) % 4) + r] for c in range(4) for r in range(4)]
        s = [INV_SBOX[b] for b in s]
        s = [s[i] ^ rk[r][i] for i in range(16)]
        ns = []
        for c in range(4):
            a0, a1, a2, a3 = s[4 * c:4 * c + 4]
            ns += [
                _gmul(a0, 14) ^ _gmul(a1, 11) ^ _gmul(a2, 13) ^ _gmul(a3, 9),
                _gmul(a0, 9) ^ _gmul(a1, 14) ^ _gmul(a2, 11) ^ _gmul(a3, 13),
                _gmul(a0, 13) ^ _gmul(a1, 9) ^ _gmul(a2, 14) ^ _gmul(a3, 11),
                _gmul(a0, 11) ^ _gmul(a1, 13) ^ _gmul(a2, 9) ^ _gmul(a3, 14),
            ]
        s = ns
    s = [s[4 * ((c - r) % 4) + r] for c in range(4) for r in range(4)]
    s = [INV_SBOX[b] for b in s]
    return bytes(s[i] ^ rk[0][i] for i in range(16))


# ── pak 变体 ────────────────────────────────────────────────────────
def key_byte_reverse(key: bytes) -> bytes:
    """前 31 字节反转,第 32 字节不动(对应 CUE4Parse 的 MutateGameKey)。"""
    return bytes(key[30 - i] for i in range(31)) + key[31:32]


def block_bit_reverse(block: bytes) -> bytes:
    out = bytearray(block)
    for i, x in enumerate(out):
        x = ((x >> 1) & 0x55) | ((x << 1) & 0xAA)
        x = ((x >> 2) & 0x33) | ((x << 2) & 0xCC)
        out[i] = (x >> 4) | ((x << 4) & 0xF0)
    return bytes(out)


# ── 2026-09 起的「可配置加密」(FConfigurableCrypto)─────────────────────
# pak trailer 末尾多 1 字节 strategy id:0 = 主密钥 + AES;其余每算法 5 个
# ((KeyId 1..4) + 文件名派生),算法顺序见 ALGOS。只有 AES 能在本脚本验证,
# 其余(SM4/MLE/RC5/...)请用带实现的 CUE4Parse(LukeFZ fork)解。
ALGOS = ["AES", "SM4", "MLE", "RC5", "XTEA", "Speck", "Salsa20", "Simon", "ChaCha20"]
CRYPT_SEED = b"NRC_CRYPTO_OBFUSCATION_SEED2026\x07"
# _keyMaterial[1..4](源码里第 5 组未参与任何策略)
KEY_MATERIAL = [
    bytes.fromhex("df1b0673b2f41a735bc4ce0684dc6f15ad4fc684f688b47454321934eab37aff"),
    bytes.fromhex("7ce3619cc904e1fee2ee0377bbdec0e5bcb761e8fccf8e81620321554a9641f7"),
    bytes.fromhex("e08e516eb7a3888ead938cd6aa9c828a2585877eab46c9694a625d1ba2ff059f"),
    bytes.fromhex("60d2f9578a0322ed6231edc8d75d032e8054339d37132f6f52baaba6feea1155"),
]


def derive_key(key_material: bytes) -> bytes:
    """按 SHA1(keyMaterial || seed || round) 循环填充到 32 字节。"""
    out = bytearray(len(key_material))
    processed = rnd = 0
    while processed != len(out):
        h = hashlib.sha1(key_material + CRYPT_SEED + bytes([rnd])).digest()
        n = min(len(out) - processed, len(h))
        out[processed:processed + n] = h[:n]
        rnd += 1
        processed += n
    return bytes(out)


def derive_file_key(pak_name: str) -> bytes:
    """按 pak 文件名(无扩展名,小写,UTF-16LE)派生:SHA1 → MD5 → 与 seed 异或。"""
    name = os.path.splitext(os.path.basename(pak_name))[0].lower()
    digest = hashlib.sha1(name.encode("utf-16-le") + CRYPT_SEED[:16]).digest()
    xor_key = hashlib.md5(digest + CRYPT_SEED[16:]).digest()
    return (bytes(xor_key[i] ^ CRYPT_SEED[i] for i in range(16))
            + bytes(xor_key[i] ^ CRYPT_SEED[len(CRYPT_SEED) - 1 - i] for i in range(16)))


def parse_mount_point(plain: bytes) -> str | None:
    """解出的前 32 字节是否像 UE FString mount point;是则返回路径。"""
    (n,) = struct.unpack_from("<i", plain, 0)
    if 1 <= n <= 28:
        body = plain[4:4 + n - 1]
        if plain[4 + n - 1] == 0 and all(0x20 <= b <= 0x7E for b in body):
            return body.decode("ascii")
    if -14 <= n <= -2:  # UTF-16
        chars = -n
        if all(plain[4 + i * 2 + 1] == 0 and 0x20 <= plain[4 + i * 2] <= 0x7E for i in range(chars - 1)):
            return plain[4:4 + (chars - 1) * 2:2].decode("ascii")
    return None


def read_pak_info(fh, size: int) -> dict | None:
    """读 pak 尾部 FPakInfo。magic 前 1 字节是 bEncryptedIndex。"""
    fh.seek(max(0, size - 1024))
    tail = fh.read()
    i = tail.rfind(struct.pack("<I", PAK_MAGIC))
    if i < 0:
        return None
    version, = struct.unpack_from("<i", tail, i + 4)
    off, isize = struct.unpack_from("<qq", tail, i + 8)
    comp = tail[i + 44:i + 76].split(b"\x00")[0].decode("ascii", "replace")
    fh.seek(size - 1)
    strategy = fh.read(1)[0]  # 2026-09 起末尾多 1 字节(旧版这里是压缩名尾部,仅作参考)
    return {
        "version": version,
        "strategy": strategy,
        "encrypted": tail[i - 1] == 1,
        "index_offset": off,
        "index_size": isize,
        "compression": comp or "?",
    }


def check_pak(name: str, open_fn, size: int, aes: bytes) -> bool:
    """打印单个 pak 的诊断;返回密钥是否可用。"""
    with open_fn() as fh:
        info = read_pak_info(fh, size)
        if info is None:
            print(f"  {name}: 找不到 magic,不是 pak 或已损坏")
            return False
        strat = info["strategy"]
        alg = ""
        if strat:
            idx = strat - 1
            algo, slot = idx // 5, idx % 5
            name_algo = ALGOS[algo] if algo < len(ALGOS) else "?"
            key_src = "文件名派生" if slot == 4 else f"keyMaterial[{slot + 1}]"
            alg = f" strategy={strat}({name_algo}+{key_src})"
        print(f"  {name}: version={info['version']} 加密索引={info['encrypted']} "
              f"index=({info['index_offset']}, {info['index_size']}) 压缩={info['compression']}{alg}")
        if info["index_size"] < 32:
            print("    索引过小(空包),跳过")
            return False
        fh.seek(info["index_offset"])
        head = fh.read(32)
        if not info["encrypted"]:
            # 未加密包说明不了密钥对不对(安卓包里的基础 pak 常是 340 字节占位壳),不计入判定
            mp = parse_mount_point(head)
            print(f"    索引未加密 → mount={mp!r}(占位包,不计入判定)"
                  if mp else "    索引未加密,但开头不像 mount point")
            return False
        cands = [("主密钥", aes)]
        if strat:
            algo, slot = (strat - 1) // 5, (strat - 1) % 5
            if algo != 0:
                print(f"    策略用 {ALGOS[algo] if algo < len(ALGOS) else '?'} 算法,本脚本未实现 —— "
                      f"用 LukeFZ fork 的 CUE4Parse 解(见 docs/unpack-2026-09.md)")
                return False
            key = derive_file_key(name) if slot == 4 else derive_key(KEY_MATERIAL[slot])
            cands.insert(0, (f"策略{strat}(AES+{key_src})", key))
        for label, key in cands:
            ct = block_bit_reverse(head)
            plain = (aes256_decrypt_block(key_byte_reverse(key), ct[:16])
                     + aes256_decrypt_block(key_byte_reverse(key), ct[16:32]))
            mp = parse_mount_point(plain)
            if mp:
                print(f"    {label}解密成功 → mount={mp!r}")
                return True
            print(f"    {label}解密: {plain.hex()}  ✗")
        return False


def iter_paks(path: str):
    """产出 (显示名, 打开函数, 大小);支持目录 / 单个 pak / apk(内嵌 main.obb.png)。"""
    if path.lower().endswith(".apk"):
        with zipfile.ZipFile(path) as apk:
            obb = next((n for n in apk.namelist() if n.endswith("main.obb.png")), None)
            if obb is None:
                print(f"{path}: apk 内没有 main.obb.png")
                return
            data = apk.read(obb)
        with zipfile.ZipFile(io.BytesIO(data)) as z:
            for n in z.namelist():
                if n.endswith(".pak"):
                    blob = z.read(n)
                    yield n.split("/")[-1], (lambda b=blob: io.BytesIO(b)), len(blob)
        return
    if os.path.isdir(path):
        for root, _, files in os.walk(path):
            for fn in sorted(files):
                if fn.lower().endswith(".pak"):
                    p = os.path.join(root, fn)
                    yield os.path.relpath(p, path), (lambda p=p: open(p, "rb")), os.path.getsize(p)
        return
    yield os.path.basename(path), (lambda: open(path, "rb")), os.path.getsize(path)


def main() -> int:
    ap = argparse.ArgumentParser(description="诊断 pak 能否用当前主密钥解开")
    ap.add_argument("path", help="Paks 目录 / 单个 .pak / 安卓 .apk")
    ap.add_argument("--aes", default=DEFAULT_AES, help="AES 主密钥(64 位十六进制,可带 0x)")
    args = ap.parse_args()

    aes_hex = args.aes[2:] if args.aes.startswith("0x") else args.aes
    if len(aes_hex) != 64:
        sys.exit(f"错误: AES 密钥须为 64 位十六进制,拿到 {len(aes_hex)} 位")
    aes = bytes.fromhex(aes_hex)

    print(f"路径: {args.path}\n密钥: 0x{aes_hex}")
    ok = False
    for name, open_fn, size in iter_paks(args.path):
        ok |= check_pak(name, open_fn, size, aes)
    print("\n结论: " + ("密钥有效,可以重跑 unpack.sh" if ok else "密钥失效或 pak 格式变了 —— 先别跑全量解包"))
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())
