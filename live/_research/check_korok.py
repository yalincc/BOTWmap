# -*- coding: utf-8 -*-
import struct, re
SAVE = r'C:/Users/Administrator/AppData/Roaming/Ryujinx/bis/user/save/0000000000000008/0/0/game_data.sav'

src = open(r'E:/WorkSpace/BOTWmap/live/_research/botw-unexplored-master/source/Data.cpp', encoding='utf-8').read()
block = src[src.index('Data::Korok Koroks[900]'):src.index('Data::KorokPath KorokPaths')]
kor = set()
for m in re.finditer(r'Data::Korok\((\d+), "([^"]+)", ([\d.-]+)f?, ([\d.-]+)f?, (\d+)\)', block):
    kor.add(int(m.group(1)))
print('lud99 korok hash 数:', len(kor))

d = open(SAVE, 'rb').read()
fl = {}
for i in range(0x0c, len(d) - 8, 8):
    h, v = struct.unpack_from('<II', d, i)
    fl.setdefault(h, []).append(v)

appear = sum(1 for h in kor if h in fl)
set_v = sum(1 for h in kor if h in fl and any(v != 0 for v in fl[h]))
print('lud99 hash 出现在存档:', appear, '置位:', set_v)

names = set()
for line in open(r'E:/WorkSpace/BOTWmap/live/_research/zelda-botw.hashes.csv', encoding='utf-8'):
    p = line.rstrip('\n').split(';')
    if len(p) >= 3:
        names.add(int(p[0]))
unknown = [h for h in fl if h not in names]
print('存档条目总数:', len(fl), '未收录于 hashes.csv:', len(unknown))
print('未收录样例:', [hex(h) for h in unknown[:10]])

# 未收录的条目里, 值非零的有多少? 可能包含未命名克洛格
unset_unknown = [h for h in unknown if any(v != 0 for v in fl[h])]
print('未收录且置位的条目数:', len(unset_unknown), [hex(h) for h in unset_unknown[:20]])
