# -*- coding: utf-8 -*-
"""验证 ali213 DLC 神庙坐标与我数据的一致性"""
import json
from math import inf

ali = json.load(open('source/ali213_coords.json', encoding='utf-8'))
raw = open('app/js/data.js', encoding='utf-8').read().split('var BOTW_DATA = ', 1)[1].rstrip(';')
d = json.loads(raw)

DLC4 = ('希贝·尼罗神庙', '夏·格玛神庙', '亚·纳迦神庙', '拉诺·库席神庙')
for p in ali['2']:
    if p['ctitle'] in DLC4 or 'DLC' in p['ctitle']:
        agx = float(p['y']) / 2
        agz = -float(p['x']) / 2
        best, bd = None, inf
        for m in d['markers']:
            if m['cat'] != 'shrine':
                continue
            gx = (m['px'][0] - 3000) * 2
            gz = (m['px'][1] - 2500) * 2
            dd = (gx - agx) ** 2 + (gz - agz) ** 2
            if dd < bd:
                bd, best = dd, m
        mgx = round((best['px'][0] - 3000) * 2)
        mgz = round((best['px'][1] - 2500) * 2)
        print('%-18s ali(%6.0f,%6.0f) -> mine(%s | %s | %5d,%5d) dist=%.0f'
              % (p['ctitle'], agx, agz, best['name'], best['en'][:32], mgx, mgz, bd ** 0.5))
