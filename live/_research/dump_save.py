# -*- coding: utf-8 -*-
"""解析 BOTW game_data.sav: 验证布局 + 输出关键 flag 统计"""
import struct, os, zlib, sys

SAVE_DIR = os.path.expandvars(r'%APPDATA%\Ryujinx\bis\user\save\0000000000000008')
FIXED = [
    (0x0bee9e46, 'MAP'), (0x23149bf8, 'RUPEES'), (0x73c29681, 'PLAYTIME'),
    (0x8a94e07a, 'KOROK_SEED_COUNTER'), (0xa40ba103, 'PLAYER_POSITION'),
    (0x982ba201, 'HORSE_POSITION'), (0x2906f327, 'MAX_HEARTS'), (0x3adff047, 'MAX_STAMINA'),
    (0x441b7231, 'DEFEATED_HINOX'), (0x54679940, 'DEFEATED_MOLDUGA'), (0x698266be, 'DEFEATED_TALUS'),
]

def load_save(path):
    with open(path, 'rb') as f:
        data = f.read()
    ver, marker, unk = struct.unpack_from('<III', data, 0)
    flags = {}
    for i in range(0x0c, len(data) - 8, 8):
        h, v = struct.unpack_from('<II', data, i)
        if h in flags:
            # 重复 hash: vector3f 场景的后续值会带 0 hash? 记录所有
            flags[h].append(v)
        else:
            flags[h] = [v]
    return data, ver, marker, flags

def find_saves():
    out = []
    for root, dirs, files in os.walk(SAVE_DIR):
        for fn in files:
            if fn == 'game_data.sav':
                p = os.path.join(root, fn)
                out.append((p, os.path.getsize(p)))
    return sorted(out)

def main():
    saves = find_saves()
    print("=== 找到的存档 ===")
    for p, sz in saves[:14]:
        rel = p.replace(SAVE_DIR, '').lstrip('\\/')
        print(f"{rel:40s} {sz}")
    if not saves:
        print("无存档!"); return
    # 用最大的/最新的(第一个有数据的)
    path = max(saves, key=lambda t: t[1])[0]
    print(f"\n=== 解析: {path} ===")
    data, ver, marker, flags = load_save(path)
    print(f"header: ver=0x{ver:x} marker=0x{marker:x} size={len(data)}")
    print(f"总 flag 条目: {len(flags)}")

    for h, name in FIXED:
        if h in flags:
            v = flags[h][0]
            if name == 'PLAYER_POSITION':
                # 找 hash 在文件中的位置, dump 周围字节
                pos = data.find(struct.pack('<I', h))
                raw = data[pos:pos+32]
                f0, f1, f2 = struct.unpack_from('<fff', raw, 4+8*0 if False else 4)[0], 0, 0
                # 手动读: f0 at +4, f1 at +12, f2 at +20
                f0 = struct.unpack_from('<f', data, pos+4)[0]
                f1 = struct.unpack_from('<f', data, pos+12)[0]
                f2 = struct.unpack_from('<f', data, pos+20)[0]
                print(f"{name:18s} hash=0x{h:08x} pos={pos} f0(X?)={f0:.2f} f1(alt?)={f1:.2f} f2(Z?)={f2:.2f}  -> px=({2*f0+12000:.1f},{2*f2+10000:.1f})")
                print(f"  raw bytes: {data[pos:pos+28].hex(' ')}")
            else:
                print(f"{name:18s} hash=0x{h:08x} value={v}")
        else:
            print(f"{name:18s} hash=0x{h:08x} 未找到")

    # 塔 flag 族
    print("\n=== MapTower flags ===")
    tower_ids = sorted(set(
        int(k[9:11]) for k in flags if k.startswith('MapTower_') and len(k) >= 11
    ) if False else [])
    # 用 hashes.csv 语义: MapTower_NN 是 name 而非 hash; 这里 hash 是 int。改用 crc32 检索
    def crc32n(s):
        return zlib.crc32(s.encode()) & 0xFFFFFFFF
    for i in range(16):
        base = crc32n(f'MapTower_{i:02d}')
        if base in flags:
            v = flags[base][0]
            op = crc32n(f'MapTower_{i:02d}_OpenCenterPos')
            osl = crc32n(f'MapTower_{i:02d}_OpenScaleLevel')
            opos = ''
            if op in flags:
                # vector3f
                p = data.find(struct.pack('<I', op))
                fx = struct.unpack_from('<f', data, p+4)[0]
                fz = struct.unpack_from('<f', data, p+20)[0]
                opos = f' OpenCenterPos=({fx:.1f},{fz:.1f})'
            oslv = flags.get(osl, ['-'])[0]
            print(f"MapTower_{i:02d}: value={v}{opos} OpenScaleLevel={oslv}")

    # 神庙: Location_Dungeon 与 Clear_Dungeon 计数
    print("\n=== 神庙 flags 计数 ===")
    loc_d = [k for k in flags if k.startswith('Location_Dungeon')]
    clr_d = [k for k in flags if k.startswith('Clear_Dungeon')]
    print(f"Location_Dungeon 条目: {len(loc_d)}")
    set_loc = [k for k in loc_d if flags[k][0] != 0]
    set_clr = [k for k in clr_d if flags[k][0] != 0]
    print(f"Location_Dungeon 已置位: {len(set_loc)}")
    print(f"Clear_Dungeon 条目: {len(clr_d)} 已置位: {len(set_clr)}")
    print("Clear_Dungeon 样例:", sorted(set_clr)[:5])

    # 克洛格
    print("\n=== 克洛格 flags ===")
    kor = [k for k in flags if 'HiddenKorok' in k]
    set_kor = [k for k in kor if flags[k][0] != 0]
    print(f"HiddenKorok 条目: {len(kor)} 已置位: {len(set_kor)}")
    print("KOROK_SEED_COUNTER 核对:", flags.get(crc32n('KorokSeedCounter'), ['-'])[0])

if __name__ == '__main__':
    main()
