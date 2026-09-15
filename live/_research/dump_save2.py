# -*- coding: utf-8 -*-
"""解析 BOTW game_data.sav: 用 hashes.csv 做名字解析,统计神庙/克洛格/地点"""
import struct, os, zlib, json

SAVE_DIR = os.path.expandvars(r'%APPDATA%\Ryujinx\bis\user\save\0000000000000008')
HASHES_CSV = r'E:/WorkSpace/BOTWmap/live/_research/zelda-botw.hashes.csv'

def load_hashnames():
    m = {}
    with open(HASHES_CSV, encoding='utf-8') as f:
        for line in f:
            parts = line.rstrip('\n').split(';')
            if len(parts) >= 3:
                m[int(parts[0])] = parts[2]
    return m

def load_save(path):
    with open(path, 'rb') as f:
        data = f.read()
    ver, marker, unk = struct.unpack_from('<III', data, 0)
    flags = {}
    for i in range(0x0c, len(data) - 8, 8):
        h, v = struct.unpack_from('<II', data, i)
        flags.setdefault(h, []).append(v)
    return data, ver, flags

def main():
    names = load_hashnames()
    print(f"hashes.csv 条目: {len(names)}")
    saves = []
    for root, dirs, files in os.walk(SAVE_DIR):
        for fn in files:
            if fn == 'game_data.sav':
                p = os.path.join(root, fn)
                saves.append((p, os.path.getsize(p)))
    path = max(saves, key=lambda t: t[1])[0]
    data, ver, flags = load_save(path)
    print(f"解析: {path}  flags={len(flags)}")

    def v(h):
        return flags.get(h, [0])[0]

    # 名字 -> hash
    name2hash = {n: h for h, n in names.items()}

    # 神庙
    print("\n=== 神庙 ===")
    loc_d = {h: flags[h] for h in flags if names.get(h, '').startswith('Location_Dungeon')}
    clr_d = {h: flags[h] for h in flags if names.get(h, '').startswith('Clear_Dungeon')}
    set_loc = {h: x for h, x in loc_d.items() if x[0] != 0}
    set_clr = {h: x for h, x in clr_d.items() if x[0] != 0}
    print(f"Location_Dungeon: {len(loc_d)} 条, 置位 {len(set_loc)}")
    print(f"Clear_Dungeon:    {len(clr_d)} 条, 置位 {len(set_clr)}")
    # 找出置位的 Location_Dungeon 名字
    for h in sorted(set_loc)[:10]:
        print(f"  {names[h]} = {set_loc[h][0]}")
    print("...")
    for h in sorted(set_clr)[:5]:
        print(f"  {names[h]} = {set_clr[h][0]}")

    # 克洛格
    print("\n=== 克洛格 ===")
    kor = {h: flags[h] for h in flags if 'HiddenKorok' in names.get(h, '')}
    set_kor = {h: x for h, x in kor.items() if x[0] != 0}
    print(f"HiddenKorok: {len(kor)} 条, 置位 {len(set_kor)}")
    print("KorokSeedCounter:", v(name2hash.get('KorokSeedCounter', -1)))

    # 塔确认
    print("\n=== 塔(存档中的 MapTower) ===")
    tw = {h: flags[h][0] for h in flags if names.get(h, '').startswith('MapTower_') and 'Open' not in names[h] and 'Info' not in names[h] and 'Demo' not in names[h] and 'yagura' not in names[h]}
    for h in sorted(tw):
        print(f"  {names[h]} = {tw[h]}")

    # 玩家位置
    pos_h = name2hash.get('PLAYER_POSITION')
    if pos_h and pos_h in flags:
        p = data.find(struct.pack('<I', pos_h))
        f0 = struct.unpack_from('<f', data, p+4)[0]
        f1 = struct.unpack_from('<f', data, p+12)[0]
        f2 = struct.unpack_from('<f', data, p+20)[0]
        print(f"\nPLAYER_POSITION: ({f0:.1f}, {f1:.1f}, {f2:.1f}) -> px=({2*f0+12000:.1f}, {2*f2+10000:.1f})")

if __name__ == '__main__':
    main()
