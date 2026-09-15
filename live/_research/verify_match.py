# -*- coding: utf-8 -*-
"""验证 lud99 数据 ↔ BOTWmap 标点的匹配质量"""
import json, os, re

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(os.path.dirname(HERE))
koroks = json.load(open(os.path.join(HERE, 'koroks.json'), encoding='utf-8'))
shrines = json.load(open(os.path.join(HERE, 'shrines.json'), encoding='utf-8'))
dlc = json.load(open(os.path.join(HERE, 'dlc_shrines.json'), encoding='utf-8'))
towers = json.load(open(os.path.join(HERE, 'towers.json'), encoding='utf-8'))

src = open(os.path.join(ROOT, 'app', 'js', 'data.js'), encoding='utf-8').read()
m = re.search(r'BOTW_DATA = (\{.*\})', src, re.S)
d = json.loads(m.group(1))
markers = d['markers']

shrine_mk = [x for x in markers if x['cat'] == 'shrine']
tower_mk = [x for x in markers if x['cat'] == 'tower']
seed_mk = [x for x in markers if x['cat'] == 'seed']
print('markers: shrine=%d tower=%d seed=%d' % (len(shrine_mk), len(tower_mk), len(seed_mk)))

def px(x, y):
    return (2 * x + 12000, 2 * y + 10000)

# 1) 神庙: lud99 name ↔ marker en
all_sh = shrines + [{'hash': s['hash'], 'name': s.get('name', ''), 'x': s['x'], 'y': s['y']} for s in dlc]
by_en = {x.get('en', ''): x for x in shrine_mk}
named = 0
name_fail = []
for s in all_sh:
    if s['name'] in by_en:
        named += 1
    else:
        name_fail.append(s['name'])
print('\n神庙名字匹配: %d/136' % named)
print('未匹配名字:', name_fail[:10])
# 用坐标兜底检查未匹配的
for nm in name_fail:
    s = next(x for x in all_sh if x['name'] == nm)
    mx, my = px(s['x'], s['y'])
    best = min(shrine_mk, key=lambda k: ((k['px'][0] - mx) ** 2 + (k['px'][1] - my) ** 2) ** 0.5)
    dd = ((best['px'][0] - mx) ** 2 + (best['px'][1] - my) ** 2) ** 0.5
    print(f"  {nm!r} -> 最近 {best.get('en')!r} {best.get('name')!r} 距离 {dd:.1f}px({dd/2:.1f}单位)")

# 名字匹配的神庙坐标距离
dists = []
for s in all_sh:
    if s['name'] in by_en:
        mk = by_en[s['name']]
        mx, my = px(s['x'], s['y'])
        dists.append(((mk['px'][0] - mx) ** 2 + (mk['px'][1] - my) ** 2) ** 0.5)
dists.sort()
print('名字匹配的神庙 px 距离: min=%.1f med=%.1f max=%.1f' % (dists[0], dists[len(dists)//2], dists[-1]))

# 2) 克洛格: 坐标最近匹配
unmatched = []
dd_all = []
for k in koroks:
    mx, my = px(k['x'], k['y'])
    best = min(seed_mk, key=lambda s: ((s['px'][0] - mx) ** 2 + (s['px'][1] - my) ** 2) ** 0.5)
    dd = ((best['px'][0] - mx) ** 2 + (best['px'][1] - my) ** 2) ** 0.5
    dd_all.append(dd)
dd_all.sort()
print('\n克洛格坐标匹配(全部按最近): min=%.1f med=%.1f max=%.1f' % (dd_all[0], dd_all[len(dd_all)//2], dd_all[-1]))
print('>100px(50单位)的:', sum(1 for x in dd_all if x > 100))
print('>50px的:', sum(1 for x in dd_all if x > 50))

# 3) 塔: en 名匹配
by_en_t = {x.get('en', ''): x for x in tower_mk}
tm = 0
for t in towers:
    if t['en'] in by_en_t:
        tm += 1
    else:
        print('塔未匹配:', t['en'])
print('\n塔名字匹配: %d/15' % tm)
print('地图塔名:', [x.get('en') for x in tower_mk])
