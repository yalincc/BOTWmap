# -*- coding: utf-8 -*-
# 解析 L4/L8 + LocationMarker EN + ali213，为每个地点生成建议简体名
import re, json

# 1) LocationMarker: label -> EN
marker = {}
txt = open(r'E:\WorkSpace\BOTWmap\source\LocationMarker.xmsbt', encoding='utf-8').read()
for m in re.finditer(r'<entry label="([^"]+)">\s*<text>([^<]*)</text>', txt):
    marker[m.group(1)] = m.group(2).strip()

# 2) Location.xml: L4/L8 + 坐标
xml = open(r'E:\WorkSpace\BOTWmap\waypoint-data\Location.xml', encoding='utf-8').read()
l4, l8 = [], []
for m in re.finditer(r'<value ShowLevel="(\d+)">\s*<MessageID type="string">([^<]+)</MessageID>\s*<Translate X="([^"]+)"[^>]*Z="([^"]+)"', xml):
    lv, mid, x, z = m.groups()
    if lv in ('4', '8'):
        (l4 if lv == '4' else l8).append([mid, float(x.replace('f', '')), float(z.replace('f', ''))])

# 3) ali213 中文名 + 游戏坐标
adata = json.load(open(r'E:\WorkSpace\BOTWmap\source\ali213_landmarks.json', encoding='utf-8'))
acnv = [[name, 63.97 * lng - 8188.2, -64.0 * lat - 8192.0] for name, lat, lng in adata]

# 4) zeldawiki 简中备用
zw = {}
try:
    zwtxt = json.load(open(r'E:\WorkSpace\BOTWmap\source\zw_full_map.json', encoding='utf-8'))
    for it in zwtxt:
        if isinstance(it, dict) and 'n' in it:
            zw[it.get('en', '').lower()] = it['n']
        elif isinstance(it, dict) and 'name' in it:
            zw[it.get('en', '').lower()] = it['name']
except Exception as e:
    print('zw load err', e)

def nearest(lst, x, z):
    best = None; bd = 1e9
    for item in lst:
        d = (item[1] - x) ** 2 + (item[2] - z) ** 2
        if d < bd:
            bd = d; best = item
    return best, bd ** 0.5

# 生成建议：每条 L4/L8
def suggest(entry):
    mid, x, z = entry
    en = marker.get(mid, mid)
    an, da = nearest(acnv, x, z)
    if da < 15:
        return an[0], da, 'ali213'
    enk = en.lower()
    if enk in zw:
        return zw[enk], da, 'zw'
    return None, da, 'missing'

out4 = [(e, suggest(e)) for e in l4]
out8 = [(e, suggest(e)) for e in l8]

# 统计
miss4 = [(e, s) for e, s in out4 if s[0] is None]
miss8 = [(e, s) for e, s in out8 if s[0] is None]
print(f'L4={len(l4)} 建议缺失={len(miss4)}; L8={len(l8)} 建议缺失={len(miss8)}')
print('--- L4 缺失清单 ---')
for e, s in miss4:
    print(f'{e[0]} | {marker.get(e[0],"")} | ({round(e[1])},{round(e[2])}) | ali最近={s[0]}')
print('--- L8 缺失清单 ---')
for e, s in miss8:
    print(f'{e[0]} | {marker.get(e[0],"")} | ({round(e[1])},{round(e[2])}) | ali最近={s[0]}')
