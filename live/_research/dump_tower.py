# -*- coding: utf-8 -*-
import struct, zlib
SAVE = r'C:/Users/Administrator/AppData/Roaming/Ryujinx/bis/user/save/0000000000000008/0/0/game_data.sav'
HASHES = r'E:/WorkSpace/BOTWmap/live/_research/zelda-botw.hashes.csv'

names = {}
for line in open(HASHES, encoding='utf-8'):
    p = line.rstrip('\n').split(';')
    if len(p) >= 3:
        names[int(p[0])] = p[2]

d = open(SAVE, 'rb').read()
fl = {}
for i in range(0x0c, len(d) - 8, 8):
    h, v = struct.unpack_from('<II', d, i)
    fl.setdefault(h, []).append(v)

def crc(s):
    return zlib.crc32(s.encode()) & 0xFFFFFFFF

print('=== 存档中 Location_MapTower / MapTower flag ===')
for i in range(1, 16):
    lm = crc(f'Location_MapTower{i:02d}')
    mt = crc(f'MapTower_{i:02d}')
    lv = fl.get(lm, ['-'])
    mv = fl.get(mt, ['-'])
    nm = names.get(lm, '?')
    print(f'Tower{i:02d}: Location_MapTower({nm})={lv}  MapTower_{i:02d}={mv}')

print('\n=== hashes.csv 中 Location_MapTower 存在性 ===')
import re
found = [n for h, n in names.items() if n.startswith('Location_MapTower')]
print('Location_MapTower 名字数:', len(found), found[:5])

print('\n=== crc32 与 hashes.csv 对照(Location_MapTower01) ===')
h1 = crc('Location_MapTower01')
print('crc32(Location_MapTower01) =', h1, 'hashes.csv 名:', names.get(h1, '?'))

# 验证 crc32 映射方向: hashes.csv 中名字为 Location_MapTower07 的 hash
h7 = next((h for h, n in names.items() if n == 'Location_MapTower07'), None)
print('hashes.csv 中 Location_MapTower07 的 hash =', h7, 'crc32 算的 =', crc('Location_MapTower07'), '一致:', h7 == crc('Location_MapTower07'))
