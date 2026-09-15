# -*- coding: utf-8 -*-
"""详细检查: flag 多出现值、各槽位统计、克洛格置位值分布"""
import struct, os, zlib

SAVE = r'C:/Users/Administrator/AppData/Roaming/Ryujinx/bis/user/save/0000000000000008'
PLAYTIME = 0x73c29681
HIDDEN_KOROK_NUM = 0x8a94e07a
def load(p):
    d = open(p, 'rb').read()
    fl = {}
    for i in range(0x0c, len(d) - 8, 8):
        h, v = struct.unpack_from('<II', d, i)
        fl.setdefault(h, []).append(v)
    return d, fl

def hashes():
    m = {}
    with open(r'E:/WorkSpace/BOTWmap/live/_research/zelda-botw.hashes.csv', encoding='utf-8') as f:
        for line in f:
            parts = line.rstrip('\n').split(';')
            if len(parts) >= 3:
                m[int(parts[0])] = parts[2]
    return m

def main():
    names = hashes()
    # 1) Location_Dungeon003 的出现情况
    d, fl = load(SAVE + r'\0\0\game_data.sav')
    h3 = next((h for h, n in names.items() if n == 'Location_Dungeon003'), None)
    print('Location_Dungeon003 hash:', h3, 'values:', fl.get(h3))
    h7 = next((h for h, n in names.items() if n == 'Location_Dungeon007'), None)
    print('Location_Dungeon007 hash:', h7, 'values:', fl.get(h7))

    # 2) 克洛格置位值分布(any occurrence != 0)
    kor_hashes = [h for h, n in names.items() if n.startswith('MainField_Npc_HiddenKorok')]
    print('korok hash 总数:', len(kor_hashes))
    set_ids = []
    valdist = {}
    for h in kor_hashes:
        vals = fl.get(h, [0])
        nz = [v for v in vals if v != 0]
        if nz:
            set_ids.append(h)
            valdist[names[h][:20]] = nz[:3]
    print('置位克洛格:', len(set_ids), '值分布样例:', list(valdist.items())[:5])
    hkn = fl.get(HIDDEN_KOROK_NUM, ['-'])
    print('HiddenKorok_Number (0x8a94e07a):', hkn)

    # 3) 各槽位统计
    print('\n=== 槽位统计 ===')
    rows = []
    for root, dirs, files in os.walk(SAVE):
        for fn in sorted(files):
            if fn == 'game_data.sav':
                p = os.path.join(root, fn)
                dd, ff = load(p)
                play = ff.get(PLAYTIME, [0])[0]
                hkn = ff.get(HIDDEN_KOROK_NUM, [0])[0]
                nset = 0
                for h in kor_hashes:
                    if any(v != 0 for v in ff.get(h, [0])):
                        nset += 1
                rows.append((p.replace(SAVE, ''), play, hkn, nset))
    for r in sorted(rows, key=lambda x: -x[1]):
        print(f"{r[0]:20s} playtime={r[1]:8d} HiddenKorokNumber={r[2]:5d} setKorokFlags={r[3]}")

if __name__ == '__main__':
    main()
