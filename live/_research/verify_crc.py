# -*- coding: utf-8 -*-
"""验证 crc32 假设 + 检查塔数据 + 检查 BOTWmap 标点字段"""
import zlib, json, re

def crc32(s):
    return zlib.crc32(s.encode('utf-8')) & 0xFFFFFFFF

# 1) hashes.csv 格式
print("=== hashes.csv 格式 ===")
with open(r'E:/WorkSpace/BOTWmap/live/_research/zelda-botw.hashes.csv', encoding='utf-8') as f:
    for i, line in enumerate(f):
        if i < 3:
            print(repr(line.rstrip('\n')))
        else:
            break

# 2) 验证 crc32: 找一个已知 flag 名
print("\n=== crc32 验证 ===")
tests = ["Clear_Dungeon012", "Clear_Dungeon000", "MainField_Npc_HiddenKorokGround_3148789636",
         "MapTower_00", "OpenTower_00", "KorokSeedCounter"]
for t in tests:
    print(f"crc32({t!r}) = {crc32(t)}")

# 3) 对照 shrine 表: Misae Suma hash = 12786689, 检查 hashes.csv 中 12786689 是什么
print("\n=== hashes.csv 中查 12786689 与 2906973 ===")
with open(r'E:/WorkSpace/BOTWmap/live/_research/zelda-botw.hashes.csv', encoding='utf-8') as f:
    for line in f:
        parts = line.rstrip('\n').split(';')
        if len(parts) >= 3 and parts[0] in ('12786689', '2906973', '16873014'):
            print(line.rstrip('\n'))

# 4) 塔的 flag 名: 搜 hashes.csv 中 MapTower 相关
print("\n=== MapTower 哈希 ===")
seen = set()
with open(r'E:/WorkSpace/BOTWmap/live/_research/zelda-botw.hashes.csv', encoding='utf-8') as f:
    for line in f:
        parts = line.rstrip('\n').split(';')
        if len(parts) >= 3 and parts[2].startswith('MapTower_') and parts[2] not in seen:
            seen.add(parts[2])
            print(f"id={parts[0]} name={parts[2]} count={parts[1]}")

# 5) BOTWmap shrine/tower/seed 标点字段
print("\n=== BOTWmap shrine 标点示例 ===")
src = open(r'E:/WorkSpace/BOTWmap/app/js/data.js', encoding='utf-8').read()
m = re.search(r'BOTW_DATA = (\{.*\})', src, re.S)
d = json.loads(m.group(1))
shrines = [x for x in d['markers'] if x['cat'] == 'shrine']
towers = [x for x in d['markers'] if x['cat'] == 'tower']
seeds = [x for x in d['markers'] if x['cat'] == 'seed']
print(f"shrine={len(shrines)} tower={len(towers)} seed={len(seeds)}")
print(json.dumps(shrines[0], ensure_ascii=False))
print(json.dumps(towers[0], ensure_ascii=False))
print(json.dumps(seeds[0], ensure_ascii=False))
