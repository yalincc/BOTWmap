# -*- coding: utf-8 -*-
"""
应用 8 条"名字待定"的官方名修正（用户确认：待定按官方改）
依据：官方地名核对.md 704 表 + 游戏内截图（旷野之息地图1.jpg）
- TabantaSnow -> 塔邦挞大雪原   （截图 + 704 表）
- EastGerudo -> 东格鲁德        （截图 + 704 表）
- EunpoHighlands -> 埃文坡高地   （704 表）
- TrakaIsland -> 杜艾斯岛        （704 表）
- HyruleCastleTown -> 海拉鲁城邑遗迹（官方 CastleTownMark 译名）
- WeaponCureSpring_02/03/04 -> 大精灵之泉 ×3（截图确认 4 泉同名）
坐标均已与官方对齐，仅改名字。
"""
import json
import re
import shutil
import sys

ROOT = r'E:\WorkSpace\BOTWmap'
REGIONS_JS = ROOT + r'\app\js\regions.js'
BAK = REGIONS_JS + '.bak3'

with open(REGIONS_JS, 'r', encoding='utf-8') as f:
    src = f.read()

m = re.search(r'const\s+BOTW_REGIONS\s*=\s*(\[.*?\])\s*;', src, re.S)
if not m:
    sys.exit('ERROR: BOTW_REGIONS 未找到')
regions = json.loads(m.group(1))

# (lv, 现名, px_x, px_y) -> 新官方名
fixes = [
    ('r2', '塔邦达冻原', 8376, 4415, '塔邦挞大雪原'),
    ('r3', '东部荒漠', 6925, 17079, '东格鲁德'),
    ('r3', '迪普兰荒地', 12671, 2614, '埃文坡高地'),
    ('r3', '莱恩贝克岛', 15492, 10181, '杜艾斯岛'),
    ('r4', '海拉鲁城堡城镇遗址', 11490, 9388, '海拉鲁城邑遗迹'),
    ('r4', '大妖精泉', 20224, 7243, '大精灵之泉'),
    ('r4', '大妖精泉', 4921, 8505, '大精灵之泉'),
    ('r4', '大妖精泉', 2259, 17649, '大精灵之泉'),
]

# 建立主键索引并定位
index = {}
for i, r in enumerate(regions):
    key = (r['lv'], r['n'], r['px'][0], r['px'][1])
    if key in index:
        sys.exit(f'ERROR: 主键重复 {key}')
    index[key] = i

applied = 0
for lv, name, x, y, new_name in fixes:
    key = (lv, name, x, y)
    if key not in index:
        print(f'MISS: {lv} {name} ({x},{y})')
        continue
    i = index[key]
    old = regions[i]['n']
    regions[i]['n'] = new_name
    applied += 1
    print(f'OK: {lv} {old} ({x},{y}) -> {new_name}')

if applied != len(fixes):
    sys.exit(f'ERROR: 仅应用 {applied}/{len(fixes)}，中止')

# 统计改名后的重名组
names = {}
for r in regions:
    names.setdefault(r['n'], []).append(tuple(r['px']))
dup = {k: v for k, v in names.items() if len(v) > 1}
print('改名后重名组数:', len(dup))
for k, v in dup.items():
    print('  ', k, v)

shutil.copy2(REGIONS_JS, BAK)
new_array = json.dumps(regions, ensure_ascii=False, separators=(',', ':'))
new_src = src[:m.start(1)] + new_array + src[m.end(1):]
with open(REGIONS_JS, 'w', encoding='utf-8', newline='') as f:
    f.write(new_src)
print('已写入:', REGIONS_JS)
print('备份:', BAK)
