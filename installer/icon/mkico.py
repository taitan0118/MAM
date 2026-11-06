#!/usr/bin/env python3
"""Tạo file .ico và .syso (tài nguyên biểu tượng) cho Go trên Windows.
Chạy: python3 mkico.py <ảnh 1024.png> <tiền tố>  (vd: mam-server-1024.png server -> server.ico, server_windows_amd64.syso)"""
import struct, io, sys
from PIL import Image
SRC, PRE = (sys.argv[1], sys.argv[2]) if len(sys.argv) > 2 else ('icon-1024.png', 'MAM')
src = Image.open(SRC).convert('RGBA')
SIZES = [16, 24, 32, 48, 64, 256]
imgs = {n: src.resize((n, n), Image.LANCZOS) for n in SIZES}

def png(im):
    b = io.BytesIO(); im.save(b, 'PNG'); return b.getvalue()

def dib(im):  # BMP không nén 32-bit + mặt nạ AND, dùng cho cỡ nhỏ
    n = im.size[0]
    hdr = struct.pack('<IiiHHIIiiII', 40, n, n * 2, 1, 32, 0, 0, 0, 0, 0, 0)
    px = im.load(); rows = []
    for y in range(n - 1, -1, -1):
        rows.append(b''.join(struct.pack('<BBBB', px[x, y][2], px[x, y][1], px[x, y][0], px[x, y][3]) for x in range(n)))
    stride = ((n + 31) // 32) * 4
    mask = b'\x00' * (stride * n)
    return hdr + b''.join(rows) + mask

data = {n: (png(imgs[n]) if n >= 256 else dib(imgs[n])) for n in SIZES}

# ---- MAM.ico
ico = struct.pack('<HHH', 0, 1, len(SIZES)); off = 6 + 16 * len(SIZES); body = b''
for n in SIZES:
    d = data[n]
    ico += struct.pack('<BBBBHHII', n % 256, n % 256, 0, 0, 1, 32, len(d), off + len(body)); body += d
open(PRE + '.ico', 'wb').write(ico + body)

# ---- .syso: COFF với một section .rsrc (RT_ICON x N + RT_GROUP_ICON)
def syso(machine, reloc_type):
    entries = []  # (type, id, bytes)
    for i, n in enumerate(SIZES): entries.append((3, i + 1, data[n]))
    grp = struct.pack('<HHH', 0, 1, len(SIZES))
    for i, n in enumerate(SIZES):
        grp += struct.pack('<BBBBHHIH', n % 256, n % 256, 0, 0, 1, 32, len(data[n]), i + 1)
    entries.append((14, 1, grp))
    types = sorted({t for t, _, _ in entries})
    # bố cục: [dir loại][dir tên mỗi loại][dir ngôn ngữ mỗi tên][data entry][dữ liệu]
    def dir_(n):  return struct.pack('<IIHHHH', 0, 0, 0, 0, 0, n)
    def ent(id_, off, sub): return struct.pack('<II', id_, off | (0x80000000 if sub else 0))
    size_type = 16 + 8 * len(types)
    names_by_type = {t: sorted(i for tt, i, _ in entries if tt == t) for t in types}
    pos = size_type; name_off = {}
    for t in types:
        name_off[t] = pos; pos += 16 + 8 * len(names_by_type[t])
    lang_off = {}
    for t in types:
        for i in names_by_type[t]:
            lang_off[(t, i)] = pos; pos += 16 + 8
    de_off = {}
    for (t, i, _) in entries:
        de_off[(t, i)] = pos; pos += 16
    data_off = {}
    for (t, i, d) in entries:
        data_off[(t, i)] = pos; pos += (len(d) + 7) // 8 * 8
    raw = bytearray(pos)
    def put(o, b): raw[o:o + len(b)] = b
    put(0, dir_(len(types)) + b''.join(ent(t, name_off[t], True) for t in types))
    for t in types:
        put(name_off[t], dir_(len(names_by_type[t])) + b''.join(ent(i, lang_off[(t, i)], True) for i in names_by_type[t]))
    for (t, i) in lang_off:
        put(lang_off[(t, i)], dir_(1) + ent(0x409, de_off[(t, i)], False))
    relocs = []
    for (t, i, d) in entries:
        put(de_off[(t, i)], struct.pack('<IIII', data_off[(t, i)], len(d), 0, 0))
        relocs.append(de_off[(t, i)])
        put(data_off[(t, i)], d)
    raw = bytes(raw)
    reloc_tbl = b''.join(struct.pack('<IIH', o, 0, reloc_type) for o in relocs)  # symbol 0 = section .rsrc
    ptr_raw = 20 + 40
    ptr_rel = ptr_raw + len(raw)
    ptr_sym = ptr_rel + len(reloc_tbl)
    hdr = struct.pack('<HHIIIHH', machine, 1, 0, ptr_sym, 1, 0, 0)
    sec = struct.pack('<8sIIIIIIHHI', b'.rsrc', 0, 0, len(raw), ptr_raw, ptr_rel, 0, len(relocs), 0, 0xC0000040)
    sym = struct.pack('<8sIhHBB', b'.rsrc', 0, 1, 0, 3, 0)
    return hdr + sec + raw + reloc_tbl + sym + struct.pack('<I', 4)

open(PRE + '_windows_amd64.syso', 'wb').write(syso(0x8664, 3))   # IMAGE_REL_AMD64_ADDR32NB
open(PRE + '_windows_386.syso', 'wb').write(syso(0x14c, 7))      # IMAGE_REL_I386_DIR32NB
print('ok', PRE)
