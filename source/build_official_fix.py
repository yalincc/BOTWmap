# -*- coding: utf-8 -*-
"""构建官方地名坐标总表 + 与 regions.js 比对，产出修正清单 / 未收录清单
规则：官方有的（名字+坐标）一律以官方为准；官方没有的不动，单独列出。
"""
import json
import re
import csv
import math
import xml.etree.ElementTree as ET

BASE = r'E:\WorkSpace\BOTWmap'
WAYPOINT = BASE + r'\waypoint-data'
SOURCE = BASE + r'\source'

# ---------- 1. 官方坐标源：Location.xml ----------
loc_root = ET.parse(WAYPOINT + r'\Location.xml').getroot()
official = {}  # label -> {coords: [(x,z,type,show)...], source: set}
for v in loc_root.findall('value'):
    mid = v.find('MessageID').text
    tr = v.find('Translate')
    typ = v.find('Type').text if v.find('Type') is not None else ''
    show = v.get('ShowLevel', '')
    entry = official.setdefault(mid, {'coords': [], 'source': set()})
    entry['coords'].append((float(tr.attrib['X'].rstrip('f')), float(tr.attrib['Z'].rstrip('f')), typ, show))
    entry['source'].add('Location')

# ---------- 2. 官方坐标源：Static.xml（LocationMarker / LocationPointer / Location_ SaveFlag） ----------
static_root = ET.parse(WAYPOINT + r'\Static.xml').getroot()
for child in static_root:
    for v in child.findall('value'):
        tr = v.find('Translate')
        if tr is None:
            continue
        x = float(tr.attrib['X'].rstrip('f'))
        z = float(tr.attrib['Z'].rstrip('f'))
        sf = v.find('SaveFlag')
        mids = v.findall('MessageID')
        label = None
        if sf is not None and sf.text and sf.text.startswith('Location_'):
            label = sf.text[len('Location_'):]
        elif mids:
            label = mids[0].text
        if label:
            entry = official.setdefault(label, {'coords': [], 'source': set()})
            entry['coords'].append((x, z, '', ''))
            entry['source'].add(child.tag)

# ---------- 3. 官方中文名：MSBT ----------
msbt = {}
with open(SOURCE + r'\简体中文地名表.csv', encoding='utf-8-sig') as f:
    for row in csv.DictReader(f):
        msbt[row['label'].strip()] = row['simplified_chinese'].strip()

# ---------- 4. ID 别名桥接（官方坐标在别的 label 名下） ----------
ALIAS = {
    'CastleTownMark': 'Location_HyruleCastleTown',
    'WiseFountain': 'Location_WisdomFountain',
    'TamurulHatago_02': 'Location_TamourHatago2',
    'MapRegion_HyrulePrairie': 'MapRegion_HyrulePrairie ',  # 尾随空格
    'LakeHylia': 'MapArea_HyliaLake',  # 海利亚湖 -> 官方区域坐标
}
for k, real in ALIAS.items():
    if real in official and k not in official:
        official[k] = official[real]

# ---------- 5. 官方坐标 -> px 换算 ----------
def to_px(x, z):
    return (2 * x + 12000, 2 * z + 10000)

def all_coords(label):
    entry = official.get(label)
    return entry['coords'] if entry else []

def nearest_coord(label, gx, gz):
    """取该 label 所有坐标中离项目点最近的一条"""
    best, bestd = None, 1e9
    for c in all_coords(label):
        d = math.hypot(gx - c[0], gz - c[1])
        if d < bestd:
            best, bestd = c, d
    return best, bestd

# ---------- 6. 解析 regions.js ----------
with open(BASE + r'\app\js\regions.js', encoding='utf-8') as f:
    text = f.read()
m = re.search(r'const BOTW_REGIONS = (\[.*?\]);', text, re.S)
regions = json.loads(m.group(1))
print('regions.js 条目数:', len(regions))

# ---------- 7. 双策略匹配 ----------
zh_to_labels = {}
for lbl, zh in msbt.items():
    zh_to_labels.setdefault(zh, []).append(lbl)

fix_rows = []      # 官方有 -> 修正清单（含无需改的？分开）
no_change = 0
untracked = []     # 官方没有 -> 未收录清单
matched = 0

# 官方坐标点列表（用于近邻）：(label, x, z) —— 每个坐标都是一条候选
near_points = []
for lbl, entry in official.items():
    for c in entry['coords']:
        near_points.append((lbl, c[0], c[1]))

for r in regions:
    n = r['n']
    px = r['px']
    lv = r['lv']
    gx, gz = (px[0] - 12000) / 2.0, (px[1] - 10000) / 2.0

    # 策略 A：中文名匹配（唯一）
    label = None
    if n in zh_to_labels and len(zh_to_labels[n]) == 1:
        label = zh_to_labels[n][0]
    match_type = '名字' if label else None

    # 策略 B：坐标近邻（<15 游戏单位 ≈ 7.5px，兼容取整/不同源误差）
    if not label:
        best, bestd = None, 1e9
        for lbl, ox, oz in near_points:
            d = math.hypot(gx - ox, gz - oz)
            if d < bestd:
                bestd, best = d, lbl
        if bestd < 15:
            label = best
            match_type = '坐标'

    if not label:
        untracked.append({'lv': lv, 'n': n, 'px_x': px[0], 'px_y': px[1],
                          'note': '官方地名表无对应（r4 自拟细名或第三方名）'})
        continue

    matched += 1
    # 坐标：取该 label 所有标记中离项目点最近的一条（多坐标时）
    c, _ = nearest_coord(label, gx, gz)
    ox, oz = c[0], c[1]
    opx = to_px(ox, oz)
    dist_px = math.hypot(opx[0] - px[0], opx[1] - px[1])
    official_zh = msbt.get(label, '')   # 可能为空（官方文本缺失）

    # 判定动作（坐标差 >=1.5px 才算改坐标；名字差则改名）
    name_diff = official_zh and official_zh != n
    coord_diff = dist_px >= 1.5
    if not official_zh:
        action = '坐标改官方,名字待定' if coord_diff else '名字待定'
    elif name_diff and coord_diff:
        action = '改名+改坐标'
    elif name_diff:
        action = '改名'
    elif coord_diff:
        action = '改坐标'
    else:
        action = '无需改'
        no_change += 1

    fix_rows.append({
        'lv': lv, 'label': label, '现名': n, '官方名': official_zh,
        'px_x': px[0], 'px_y': px[1], '官方px_x': int(round(opx[0])), '官方px_y': int(round(opx[1])),
        '坐标差px': round(dist_px, 1), '匹配': match_type, '动作': action,
        '来源': '+'.join(sorted(official[label]['source'])),
    })

# ---------- 8. 输出 ----------
with open(SOURCE + r'\官方地名坐标总表.csv', 'w', encoding='utf-8-sig', newline='') as f:
    w = csv.writer(f)
    w.writerow(['label', '官方中文名', 'game_x', 'game_z', 'px_x', 'px_y', '来源', '坐标条数'])
    for lbl in sorted(official):
        cs = all_coords(lbl)
        if cs:
            # 总表取 ShowLevel=4 优先的第一条
            c0 = None
            for pref in ('4', '8', ''):
                for c in cs:
                    if c[3] == pref:
                        c0 = c
                        break
                if c0:
                    break
            if not c0:
                c0 = cs[0]
            px = to_px(c0[0], c0[1])
            w.writerow([lbl, msbt.get(lbl, ''), c0[0], c0[1], int(round(px[0])), int(round(px[1])),
                        '+'.join(sorted(official[lbl]['source'])), len(cs)])

with open(SOURCE + r'\官方修正清单.csv', 'w', encoding='utf-8-sig', newline='') as f:
    w = csv.DictWriter(f, fieldnames=list(fix_rows[0].keys()))
    w.writeheader()
    for row in fix_rows:
        w.writerow(row)

with open(SOURCE + r'\官方未收录清单.csv', 'w', encoding='utf-8-sig', newline='') as f:
    w = csv.DictWriter(f, fieldnames=['lv', 'n', 'px_x', 'px_y', 'note'])
    w.writeheader()
    for row in untracked:
        w.writerow(row)

# ---------- 9. 统计摘要 ----------
from collections import Counter
act = Counter(r['动作'] for r in fix_rows)
print('官方总表 label 数:', len(official))
print('匹配到官方: {}, 未收录: {}'.format(matched, len(untracked)))
print('动作分布:')
for k, v in act.most_common():
    print('  {}: {}'.format(k, v))
print()
print('== 未收录清单（{} 条，官方无对应，不动）=='.format(len(untracked)))
lv_c = Counter(r['lv'] for r in untracked)
print('  按层级:', dict(lv_c))
for r in untracked:
    print('  [{}] {} px=({},{})'.format(r['lv'], r['n'], r['px_x'], r['px_y']))
