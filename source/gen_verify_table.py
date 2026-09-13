# -*- coding: utf-8 -*-
# 生成 r3 译名核对表：r3现有名 vs 游戏英文显示名(LocationMarker) vs ali213中文名
# 以 Location.xml L4 坐标为锚点
import re, json

# 1) LocationMarker: label -> 显示名(EN)
marker = {}
txt = open(r'E:\WorkSpace\BOTWmap\source\LocationMarker.xmsbt', encoding='utf-8').read()
for m in re.finditer(r'<entry label="([^"]+)">\s*<text>([^<]*)</text>', txt):
    marker[m.group(1)] = m.group(2).strip()

# 2) Location.xml L4: label + 游戏坐标
xml = open(r'E:\WorkSpace\BOTWmap\waypoint-data\Location.xml', encoding='utf-8').read()
l4 = []
for m in re.finditer(r'<value ShowLevel="(\d+)">\s*<MessageID type="string">([^<]+)</MessageID>\s*<Translate X="([^"]+)"[^>]*Z="([^"]+)"', xml):
    lv, mid, x, z = m.groups()
    if lv == '4':
        l4.append([mid, float(x.replace('f', '')), float(z.replace('f', ''))])

# 3) regions.js 现有全部
regtxt = open(r'E:\WorkSpace\BOTWmap\app\js\regions.js', encoding='utf-8').read()
regs = json.loads(regtxt[regtxt.index('['):regtxt.rindex(']') + 1])
r3 = [[r['n'], (r['px'][0] - 12000) / 2, (r['px'][1] - 10000) / 2] for r in regs if r['lv'] == 'r3']
r4 = [[r['n'], (r['px'][0] - 12000) / 2, (r['px'][1] - 10000) / 2] for r in regs if r['lv'] == 'r4']

# 4) ali213 中文名 + 游戏坐标
adata = json.load(open(r'E:\WorkSpace\BOTWmap\source\ali213_landmarks.json', encoding='utf-8'))
acnv = [[name, 63.97 * lng - 8188.2, -64.0 * lat - 8192.0] for name, lat, lng in adata]

def nearest(lst, x, z):
    best = None; bd = 1e9
    for item in lst:
        d = (item[1] - x) ** 2 + (item[2] - z) ** 2
        if d < bd:
            bd = d; best = item
    return best, bd ** 0.5

print('=== r3 译名核对表（以 Location.xml L4 为锚） ===')
print('label | 英文显示名 | loc(x,z) | r3现有名(距) | ali213名(距) | r4名(距)')
for mid, x, z in l4:
    en = marker.get(mid, '?')
    r3n, d3 = nearest(r3, x, z)
    an, da = nearest(acnv, x, z)
    r4n, d4 = nearest(r4, x, z)
    d3s = f'{d3:.0f}' if d3 < 40 else f'{d3:.0f}!'
    das_ = f'{da:.0f}' if da < 40 else f'{da:.0f}!'
    d4s = f'{d4:.0f}' if d4 < 40 else f'{d4:.0f}!'
    print(f'{mid} | {en} | {round(x)},{round(z)} | {r3n[0]}({d3s}) | {an[0]}({das_}) | {r4n[0] if d4<30 else "-"}({d4s})')
