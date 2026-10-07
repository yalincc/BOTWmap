# -*- coding: utf-8 -*-
"""解析 XCI HFS0，读取 NCA 头（title id / version / build 信息），确认 BOTW 更新版本。"""
import struct, sys

def read_hfs0(f, off, end):
    f.seek(off)
    magic = f.read(4)
    if magic != b'HFS0':
        return None
    n, strsz = struct.unpack('<II', f.read(8))
    entries = []
    for i in range(n):
        raw = f.read(0x18)
        if len(raw) < 0x18:
            return None
        name, foff, fsize = struct.unpack('<16sII', raw)
        entries.append((name.rstrip(b'\x00').decode('utf-8', 'replace'),
                        off + 0x10 + n * 0x18 + strsz + foff, fsize))
    return entries

def main():
    xci = r'G:\YUZU\Switch Games\The Legend of Zelda Breath of the Wild [01007EF00011E000] [v1114112] (1G+1U+2D).xci'
    f = open(xci, 'rb')
    f.seek(0)
    head = f.read(0x20)
    print('XCI head magic:', head[:4])

    root = None
    for base in (0x0, 0x1000, 0x200):
        ents = read_hfs0(f, base, 0)
        if ents:
            print(f'== 根 HFS0 @ 0x{base:X} ==')
            for n, o, s in ents:
                print(f'  {n}: off=0x{o:X} size={s}')
            root = ents
            break
    if not root:
        print('!! 未找到根 HFS0')
        return

    for n, o, s in root:
        if n.lower() == 'secure':
            print(f'\n== secure 分区 @ 0x{o:X} (size={s}) ==')
            ents = read_hfs0(f, o, o + s)
            if not ents:
                print('  secure 不是 HFS0？')
                continue
            for n2, o2, s2 in ents:
                f.seek(o2)
                nca = f.read(0x400)
                if nca[:4] == b'NCA3':
                    ctype = nca[0x208]
                    ct = {0: 'Program', 1: 'Meta', 2: 'Control', 3: 'Manual', 4: 'Data', 5: 'PublicData'}.get(ctype, '?')
                    tid = struct.unpack('<Q', nca[0x210:0x218])[0]
                    ver = struct.unpack('<I', nca[0x238:0x23C])[0]
                    print(f'  {n2}: NCA3 type={ct} title_id=0x{tid:016X} version={ver} (0x{ver:X})')
                else:
                    print(f'  {n2}: off=0x{o2:X} size={s2} (非 NCA3, magic={nca[:4]!r})')
            break

    # 尝试列出 normal 分区（含 DLC 等）
    for n, o, s in root:
        if n.lower() == 'normal':
            print(f'\n== normal 分区 @ 0x{o:X} ==')
            ents = read_hfs0(f, o, o + s)
            if ents:
                for n2, o2, s2 in ents:
                    print(f'  {n2}: off=0x{o2:X} size={s2}')
            break
    f.close()

if __name__ == '__main__':
    main()
