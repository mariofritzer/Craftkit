#!/usr/bin/env python3
"""Draws the CraftKit icon and writes Windows resources (icon, version info, manifest)
as a COFF object (rsrc_windows_amd64.syso) that the Go linker embeds automatically.

Only the Python standard library is needed.

    python3 tools/winres.py --version 1.2.3            # writes rsrc_windows_amd64.syso
    python3 tools/winres.py --png web/icon.png 64      # also writes a PNG of the icon
"""
import argparse
import random
import struct
import zlib

# ---------------------------------------------------------------- icon drawing

BOX_TOP = [(214, 168, 106), (206, 160, 98), (220, 176, 114)]
BOX_L = [(186, 138, 82), (180, 132, 78), (192, 144, 88)]
BOX_R = [(152, 108, 62), (146, 102, 58), (158, 114, 68)]
TAPE_TOP = [(112, 200, 66), (104, 192, 60)]
TAPE_L = [(92, 172, 52), (86, 164, 48)]
TAPE_R = [(70, 138, 40), (66, 130, 36)]
OUTLINE = (40, 30, 22)


def inside(poly, x, y):
    # even-odd rule
    c = False
    n = len(poly)
    for i in range(n):
        x1, y1 = poly[i]
        x2, y2 = poly[(i + 1) % n]
        if (y1 > y) != (y2 > y):
            if x < (x2 - x1) * (y - y1) / (y2 - y1) + x1:
                c = not c
    return c


def draw_cube(n):
    """Returns an n*n list of RGBA tuples: a cardboard box with a green tape stripe."""
    rnd = random.Random(7)
    m = max(1, round(n * 0.06))           # side margin
    h = (n - 2 * m) / 4.0                 # half height of the top rhombus
    cx = n / 2.0
    top_y = m
    mid_y = top_y + h
    low_y = top_y + 2 * h
    bottom = n - m
    top = [(cx, top_y), (n - m, mid_y), (cx, low_y), (m, mid_y)]
    left = [(m, mid_y), (cx, low_y), (cx, bottom), (m, bottom - h)]
    right = [(cx, low_y), (n - m, mid_y), (n - m, bottom - h), (cx, bottom)]
    tape = max(1.0, n * 0.09)             # half width of the tape
    img = [[(0, 0, 0, 0)] * n for _ in range(n)]
    for y in range(n):
        for x in range(n):
            px, py = x + 0.5, y + 0.5
            on_tape = abs(px - cx) < tape
            if inside(top, px, py):
                c = rnd.choice(TAPE_TOP if on_tape else BOX_TOP)
            elif inside(left, px, py):
                c = rnd.choice(TAPE_L if on_tape else BOX_L)
            elif inside(right, px, py):
                c = rnd.choice(TAPE_R if on_tape else BOX_R)
            else:
                continue
            img[y][x] = c + (255,)
    # dark outline around the shape and along the box edges
    out = [row[:] for row in img]
    for y in range(n):
        for x in range(n):
            if img[y][x][3]:
                continue
            for dx, dy in ((1, 0), (-1, 0), (0, 1), (0, -1)):
                xx, yy = x + dx, y + dy
                if 0 <= xx < n and 0 <= yy < n and img[yy][xx][3]:
                    out[y][x] = OUTLINE + (255,)
                    break
    if n >= 24:
        # subtle darker edge where the top meets the sides
        for x in range(n):
            px = x + 0.5
            if m <= px <= n - m:
                ey = mid_y + (px - m) * (low_y - mid_y) / (cx - m) if px <= cx else low_y + (px - cx) * (mid_y - low_y) / (n - m - cx)
                y = int(ey)
                if 0 <= y < n and out[y][x][3]:
                    r, g, b, a = out[y][x]
                    out[y][x] = (int(r * 0.8), int(g * 0.8), int(b * 0.8), a)
    return out


def scale(img, f):
    return [[px for px in row for _ in range(f)] for row in img for _ in range(f)]


def png_bytes(img):
    n = len(img)
    raw = b"".join(b"\x00" + b"".join(struct.pack("BBBB", *px) for px in row) for row in img)

    def chunk(t, d):
        return struct.pack(">I", len(d)) + t + d + struct.pack(">I", zlib.crc32(t + d) & 0xFFFFFFFF)

    return (b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", n, n, 8, 6, 0, 0, 0))
            + chunk(b"IDAT", zlib.compress(raw, 9)) + chunk(b"IEND", b""))


def dib_bytes(img):
    """32-bit BGRA DIB with AND mask as stored inside .ico resources."""
    n = len(img)
    hdr = struct.pack("<IiiHHIIiiII", 40, n, n * 2, 1, 32, 0, 0, 0, 0, 0, 0)
    xor = b"".join(b"".join(struct.pack("BBBB", b, g, r, a) for (r, g, b, a) in row) for row in reversed(img))
    row_bytes = ((n + 31) // 32) * 4
    mask = b""
    for row in reversed(img):
        bits = 0
        out = bytearray(row_bytes)
        for x, px in enumerate(row):
            if px[3] == 0:
                out[x // 8] |= 0x80 >> (x % 8)
        mask += bytes(out)
    return hdr + xor + mask


def icon_images():
    imgs = []
    for size in (16, 24, 32, 48, 64):
        imgs.append((size, dib_bytes(draw_cube(size))))
    imgs.append((256, png_bytes(scale(draw_cube(32), 8))))
    return imgs


# ---------------------------------------------------------------- version info

def utf16z(s):
    return (s + "\0").encode("utf-16-le")


def pad4(b):
    return b + b"\0" * ((4 - len(b) % 4) % 4)


def block(key, value=b"", wtype=1, children=(), value_len=None):
    body = struct.pack("<HHH", 0, 0, 0) + utf16z(key)
    body = pad4(body)
    if value:
        body += value
    kids = b""
    for c in children:
        kids = pad4(kids) + c
    if kids:
        body = pad4(body) + kids
    if value_len is None:
        value_len = len(value) // 2 if wtype == 1 else len(value)
    return struct.pack("<HHH", len(body), value_len, wtype) + body[6:]


def version_info(version, product="CraftKit"):
    nums = [int(x) if x.isdigit() else 0 for x in (version.split("-")[0].split(".") + ["0"] * 4)[:4]]
    ms, ls = (nums[0] << 16) | nums[1], (nums[2] << 16) | nums[3]
    fixed = struct.pack("<13I", 0xFEEF04BD, 0x00010000, ms, ls, ms, ls, 0x3F, 0, 0x40004, 1, 0, 0, 0)
    strings = {
        "CompanyName": "Mario Fritzer",
        "FileDescription": "CraftKit – Minecraft-Paketmanager",
        "FileVersion": version,
        "InternalName": "CraftKit",
        "LegalCopyright": "© 2026 Mario Fritzer · MIT License",
        "OriginalFilename": "CraftKit.exe",
        "ProductName": product,
        "ProductVersion": version,
    }
    str_blocks = [block(k, utf16z(v), 1) for k, v in strings.items()]
    table = block("040904b0", b"", 1, str_blocks)
    sfi = block("StringFileInfo", b"", 1, [table])
    var = block("Translation", struct.pack("<HH", 0x0409, 0x04B0), 0)
    vfi = block("VarFileInfo", b"", 1, [var])
    return block("VS_VERSION_INFO", fixed, 0, [sfi, vfi])


MANIFEST = b"""<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0">
  <assemblyIdentity type="win32" name="CraftKit" version="1.0.0.0"/>
  <trustInfo xmlns="urn:schemas-microsoft-com:asm.v3">
    <security><requestedPrivileges><requestedExecutionLevel level="asInvoker" uiAccess="false"/></requestedPrivileges></security>
  </trustInfo>
  <application xmlns="urn:schemas-microsoft-com:asm.v3">
    <windowsSettings>
      <dpiAware xmlns="http://schemas.microsoft.com/SMI/2005/WindowsSettings">true</dpiAware>
      <longPathAware xmlns="http://schemas.microsoft.com/SMI/2016/WindowsSettings">true</longPathAware>
    </windowsSettings>
  </application>
</assembly>
"""

# ---------------------------------------------------------------- COFF / .rsrc writer

RT_ICON, RT_GROUP_ICON, RT_VERSION, RT_MANIFEST = 3, 14, 16, 24
LANG = 0x0409


def build_rsrc(resources):
    """resources: {type_id: {name_id: bytes}} -> (section bytes, reloc offsets)"""
    types = sorted(resources)
    # layout: all directory tables first, then data entries, then data
    dirs = []  # (offset placeholder) built in order

    def dir_size(count):
        return 16 + 8 * count

    offset = dir_size(len(types))
    type_dirs = []
    for t in types:
        type_dirs.append(offset)
        offset += dir_size(len(resources[t]))
    name_dirs = {}
    for t in types:
        for nid in sorted(resources[t]):
            name_dirs[(t, nid)] = offset
            offset += dir_size(1)
    data_entries = {}
    for t in types:
        for nid in sorted(resources[t]):
            data_entries[(t, nid)] = offset
            offset += 16
    data_off = {}
    for t in types:
        for nid in sorted(resources[t]):
            offset = (offset + 7) & ~7
            data_off[(t, nid)] = offset
            offset += len(resources[t][nid])
    out = bytearray(offset)
    relocs = []

    def put_dir(at, entries):
        struct.pack_into("<IIHHHH", out, at, 0, 0, 0, 0, 0, len(entries))
        for i, (ident, target, is_dir) in enumerate(entries):
            struct.pack_into("<II", out, at + 16 + 8 * i, ident, target | (0x80000000 if is_dir else 0))

    put_dir(0, [(t, type_dirs[i], True) for i, t in enumerate(types)])
    for i, t in enumerate(types):
        put_dir(type_dirs[i], [(nid, name_dirs[(t, nid)], True) for nid in sorted(resources[t])])
        for nid in sorted(resources[t]):
            put_dir(name_dirs[(t, nid)], [(LANG, data_entries[(t, nid)], False)])
            de = data_entries[(t, nid)]
            data = resources[t][nid]
            struct.pack_into("<IIII", out, de, data_off[(t, nid)], len(data), 0, 0)
            relocs.append(de)  # OffsetToData is an RVA -> needs a relocation
            out[data_off[(t, nid)]:data_off[(t, nid)] + len(data)] = data
    return bytes(out), relocs


def write_syso(path, section, relocs):
    IMAGE_FILE_MACHINE_AMD64 = 0x8664
    IMAGE_REL_AMD64_ADDR32NB = 3
    n_sec = 1
    raw_ptr = 20 + 40 * n_sec
    reloc_ptr = raw_ptr + len(section)
    sym_ptr = reloc_ptr + 10 * len(relocs)
    header = struct.pack("<HHIIIHH", IMAGE_FILE_MACHINE_AMD64, n_sec, 0, sym_ptr, 1, 0, 0)
    sec = struct.pack("<8sIIIIIIHHI", b".rsrc", 0, 0, len(section), raw_ptr, reloc_ptr, 0,
                      len(relocs), 0, 0x40000040)
    rel = b"".join(struct.pack("<IIH", r, 0, IMAGE_REL_AMD64_ADDR32NB) for r in relocs)
    sym = struct.pack("<8sIhHBB", b".rsrc", 0, 1, 0, 3, 0)
    strtab = struct.pack("<I", 4)
    with open(path, "wb") as f:
        f.write(header + sec + section + rel + sym + strtab)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--version", default="0.0.0")
    ap.add_argument("--out", default="rsrc_windows_amd64.syso")
    ap.add_argument("--png", nargs=2, metavar=("FILE", "SIZE"), help="also write a PNG of the icon")
    ap.add_argument("--ico", help="also write a .ico file")
    a = ap.parse_args()

    imgs = icon_images()
    res = {RT_ICON: {}, RT_GROUP_ICON: {}, RT_VERSION: {}, RT_MANIFEST: {}}
    grp = struct.pack("<HHH", 0, 1, len(imgs))
    for i, (size, data) in enumerate(imgs, start=1):
        res[RT_ICON][i] = data
        grp += struct.pack("<BBBBHHIH", size % 256, size % 256, 0, 0, 1, 32, len(data), i)
    res[RT_GROUP_ICON][1] = grp
    res[RT_VERSION][1] = version_info(a.version)
    res[RT_MANIFEST][1] = MANIFEST
    section, relocs = build_rsrc(res)
    write_syso(a.out, section, relocs)
    print(f"wrote {a.out} ({len(section)} bytes of resources, version {a.version})")

    if a.png:
        size = int(a.png[1])
        f = max(1, size // 32)
        img = scale(draw_cube(32), f) if size % 32 == 0 else draw_cube(size)
        with open(a.png[0], "wb") as fh:
            fh.write(png_bytes(img))
    if a.ico:
        entries = b""
        blob = b""
        off = 6 + 16 * len(imgs)
        for size, data in imgs:
            entries += struct.pack("<BBBBHHII", size % 256, size % 256, 0, 0, 1, 32, len(data), off + len(blob))
            blob += data
        with open(a.ico, "wb") as fh:
            fh.write(struct.pack("<HHH", 0, 1, len(imgs)) + entries + blob)


if __name__ == "__main__":
    main()
