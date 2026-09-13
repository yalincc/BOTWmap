# -*- coding: utf-8 -*-
"""应用进入提示地点补充清单到 regions.js：r4 40 条 + r5 11 条"""
import json, re, csv, shutil, sys

BASE = r'E:\WorkSpace\BOTWmap'
REGIONS_JS = BASE + r'\app\js\regions.js'
CSV_PATH = BASE + r'\source\进入提示地点补充清单.csv'

src = open(REGIONS_JS, encoding='utf-8').read()
m = re.search(r'const\s+BOTW_REGIONS\s*=\s*(\[.*?\])\s*;', src, re.S)
regions = json.loads(m.group(1))
print('现有条目:', len(regions))

# 现有 px 集合（去重检查）
existing = {(r['lv'], r['px'][0], r['px'][1]) for r in regions}

rows = list(csv.DictReader(open(CSV_PATH, encoding='utf-8-sig')))
added = 0
for r in rows:
    lv = r['层级']
    name = r['中文名']
    px = (int(round(float(r['px_x']))), int(round(float(r['px_y']))))
    if (lv, px[0], px[1]) in existing:
        print(f'跳过(已存在): {lv} {name} {px}')
        continue
    regions.append({'n': name, 'px': list(px), 'lv': lv})
    existing.add((lv, px[0], px[1]))
    added += 1

print('新增:', added, '-> 总数:', len(regions))
if added != len(rows):
    sys.exit('ERROR: 新增数 != 清单数，中止')

shutil.copy2(REGIONS_JS, REGIONS_JS + '.bak4')
new_array = json.dumps(regions, ensure_ascii=False, separators=(',', ':'))
new_src = src[:m.start(1)] + new_array + src[m.end(1):]
open(REGIONS_JS, 'w', encoding='utf-8', newline='').write(new_src)
print('已写入，备份 bak4')

# 统计
from collections import Counter
print('层级分布:', dict(Counter(r['lv'] for r in regions)))
