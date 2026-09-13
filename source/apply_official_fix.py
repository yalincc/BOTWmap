# -*- coding: utf-8 -*-
"""
应用官方坐标/名称修正到 app/js/regions.js
- 基底：当前工作区 regions.js（含上一轮 327 条译名修正，勿覆盖）
- 依据：source/官方修正清单.csv（build_official_fix.py 产出）
- 规则：官方有的以官方为准；官方未收录的不动（另清单单独列出）
- 主键：(lv, 现名, px_x, px_y)，必须在 regions.js 中唯一匹配，否则报错退出
- 输出：修改后的 regions.js（先备份 regions.js.bak2）
"""
import csv
import json
import re
import shutil
import sys
from collections import Counter

ROOT = r'E:\WorkSpace\BOTWmap'
REGIONS_JS = ROOT + r'\app\js\regions.js'
CSV_PATH = ROOT + r'\source\官方修正清单.csv'
BAK = REGIONS_JS + '.bak2'

# 读取 regions.js 的原始文本，用于精确替换（保持格式）
with open(REGIONS_JS, 'r', encoding='utf-8') as f:
    src = f.read()

# 用正则抓取数组内容（BOTW_REGIONS = [...]），并同时做 JS 语法校验
m = re.search(r'const\s+BOTW_REGIONS\s*=\s*(\[.*?\])\s*;', src, re.S)
if not m:
    sys.exit('ERROR: 未找到 BOTW_REGIONS 数组')
array_text = m.group(1)
regions = json.loads(array_text)
print('regions.js 现有条目数:', len(regions))

# 校验条目结构
for i, r in enumerate(regions):
    if not (set(r.keys()) == {'n', 'px', 'lv'} and len(r['px']) == 2):
        sys.exit(f'ERROR: 第 {i} 条结构异常: {r}')

# 建立主键索引
index = {}
for i, r in enumerate(regions):
    key = (r['lv'], r['n'], r['px'][0], r['px'][1])
    if key in index:
        sys.exit(f'ERROR: regions.js 内部主键重复: {key}')
    index[key] = i
print('主键索引条目:', len(index))

# 读取修正清单
rows = list(csv.DictReader(open(CSV_PATH, encoding='utf-8-sig')))
to_apply = [r for r in rows if r['动作'] not in ('无需改', '名字待定')]
print('待应用修正条数:', len(to_apply))

stats = Counter()
unmatched = []
for r in to_apply:
    lv, cur, cx, cy = r['lv'], r['现名'], int(r['px_x']), int(r['px_y'])
    key = (lv, cur, cx, cy)
    if key not in index:
        unmatched.append(r)
        continue
    i = index[key]
    entry = regions[i]
    action = r['动作']
    # 校验 CSV 与 regions.js 现有数据一致性
    if entry['n'] != cur or entry['px'][0] != cx or entry['px'][1] != cy:
        unmatched.append(r)
        continue
    if action in ('改名', '改名+改坐标'):
        entry['n'] = r['官方名']
    if action in ('改坐标', '改名+改坐标', '坐标改官方,名字待定'):
        entry['px'] = [int(r['官方px_x']), int(r['官方px_y'])]
    stats[action] += 1

if unmatched:
    print('--- 未匹配行（需人工处理）---')
    for r in unmatched:
        print(r['lv'], r['label'], r['现名'], r['px_x'], r['px_y'], '->', r['官方名'], r['官方px_x'], r['官方px_y'], r['动作'])
    sys.exit(f'ERROR: {len(unmatched)} 条无法匹配，未写入任何修改')

print('应用统计:', dict(stats))
print('实际修改条目数:', sum(stats.values()))

# 备份 + 写回（保持单行 JSON 输出格式与原文一致）
shutil.copy2(REGIONS_JS, BAK)
new_array = json.dumps(regions, ensure_ascii=False, separators=(',', ':'))
new_src = src[:m.start(1)] + new_array + src[m.end(1):]
with open(REGIONS_JS, 'w', encoding='utf-8', newline='') as f:
    f.write(new_src)
print('已写入:', REGIONS_JS)
print('备份:', BAK)
