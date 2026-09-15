# -*- coding: utf-8 -*-
import struct, re
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

# 1) 克洛格 flag 多重出现分布
multi = {h: vals for h, vals in fl.items() if len(vals) > 1 and 'HiddenKorok' in names.get(h, '')}
print('克洛格 flag 多重出现:', len(multi))
for h, vals in list(multi.items())[:8]:
    print(' ', names[h][:60], vals)

# 2) 其他 Korok quest flag 的值
qflags = ['HiddenKorok_Complete', 'HiddenKorok_First', 'HiddenKorok_Number',
          'Npc_OldKorok_GetNuts', 'OldKorok_NatsCount', 'KorokNutsNum',
          'IsGet_Obj_ProofKorok', 'Npc_OldKorok_900First', 'HiddenKorok_Number']
for qn in qflags:
    h = next((hh for hh, n in names.items() if n == qn), None)
    if h and h in fl:
        print(f'{qn}: hash=0x{h:08x} values={fl[h]}')
    elif h:
        print(f'{qn}: hash=0x{h:08x} 不在存档')

# 3) 存档中所有置位且名字含 Korok 的(排除 900 种子)
print('\n置位的 Korok 相关非种子 flag:')
for h, vals in fl.items():
    if any(v != 0 for v in vals) and 'Korok' in names.get(h, '') and not names[h].startswith('MainField_Npc_HiddenKorok'):
        print(' ', names[h], vals[:2])
