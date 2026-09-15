# -*- coding: utf-8 -*-
"""从 lud99/botw-unexplored Data.cpp 提取克洛格/神庙/DLC神庙表为 JSON。

输出:
    _research/koroks.json    900 条 {hash, flag, x, y, id}
    _research/shrines.json   120 条 {hash, name, x, y}
    _research/dlc_shrines.json 16 条 {hash, x, y}
    _research/towers.json    15 条 {n, name_en}  (来自 waypoint LocationMarker.csv)
"""
import json, re, os

HERE = os.path.dirname(os.path.abspath(__file__))
SRC = os.path.join(HERE, 'botw-unexplored-master', 'source', 'Data.cpp')
WAYPOINT = r'E:/WorkSpace/BOTWmap/waypoint-data/LocationMarker.csv'

src = open(SRC, encoding='utf-8').read()

def block(start_marker, end_marker):
    i = src.index(start_marker)
    j = src.index(end_marker, i)
    return src[i:j]

# --- koroks: Data::Korok(hash, "flag", x, y, zeldaDungeonId) ---
kb = block('Data::Korok Koroks[900]', 'Data::KorokPath KorokPaths')
koroks = []
for m in re.finditer(r'Data::Korok\((\d+), "([^"]+)", ([\d.-]+)f?, ([\d.-]+)f?, (\d+)\)', kb):
    koroks.append({'hash': int(m.group(1)), 'flag': m.group(2),
                   'x': float(m.group(3)), 'y': float(m.group(4)), 'id': int(m.group(5))})
print('koroks:', len(koroks))

# --- shrines ---
sb = block('Data::Shrine Shrines[Data::ShrineCount]', 'Data::DLCShrine DLCShrines[DLCShrineCount]')
shrines = []
for m in re.finditer(r'Data::Shrine\((\d+), "([^"]+)", ([\d.-]+)f?, ([\d.-]+)f?\)', sb):
    shrines.append({'hash': int(m.group(1)), 'name': m.group(2),
                    'x': float(m.group(3)), 'y': float(m.group(4))})
print('shrines:', len(shrines))

# --- dlc shrines: 格式待查 ---
db = block('Data::DLCShrine DLCShrines[DLCShrineCount]', 'Data::Location Locations[187]')
dlc = []
for m in re.finditer(r'Data::DLCShrine\((\d+), "([^"]*)", ([\d.-]+)f?, ([\d.-]+)f?\)|Data::DLCShrine\((\d+), ([\d.-]+)f?, ([\d.-]+)f?\)', db):
    if m.group(1):
        dlc.append({'hash': int(m.group(1)), 'name': m.group(2),
                    'x': float(m.group(3)), 'y': float(m.group(4))})
    else:
        dlc.append({'hash': int(m.group(5)), 'name': '',
                    'x': float(m.group(6)), 'y': float(m.group(7))})
print('dlc shrines:', len(dlc))
print('dlc sample:', dlc[:2])

# --- towers from waypoint LocationMarker.csv ---
towers = []
for line in open(WAYPOINT, encoding='utf-8'):
    m = re.match(r'(Tower\d+),"(.+)"', line.strip())
    if m:
        towers.append({'n': int(m.group(1)[5:]), 'en': m.group(2)})
towers.sort(key=lambda t: t['n'])
print('towers:', len(towers))

for fn, obj in [('koroks.json', koroks), ('shrines.json', shrines),
                ('dlc_shrines.json', dlc), ('towers.json', towers)]:
    with open(os.path.join(HERE, fn), 'w', encoding='utf-8') as f:
        json.dump(obj, f, ensure_ascii=False, separators=(',', ':'))
    print('wrote', fn)
