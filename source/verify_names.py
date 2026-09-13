# -*- coding: utf-8 -*-
# 用 LocationMarker.xmsbt（游戏内显示名）+ Location.xml（坐标）+ ali213（中文名）核对 r3 译名
import re, json

# 1) LocationMarker: label -> 显示名（英文）
marker = {}
txt = open(r'E:\WorkSpace\BOTWmap\source\LocationMarker.xmsbt', encoding='utf-8').read()
for m in re.finditer(r'<entry label="([^"]+)">\s*<text>([^<]*)</text>', txt):
    marker[m.group(1)] = m.group(2).strip()
print('marker entries:', len(marker))

# 2) Location.xml L4: label + 坐标
xml = open(r'E:\WorkSpace\BOTWmap\waypoint-data\Location.xml', encoding='utf-8').read()
l4 = []
for m in re.finditer(r'<value ShowLevel="(\d+)">\s*<MessageID type="string">([^<]+)</MessageID>\s*<Translate X="([^"]+)"[^>]*Z="([^"]+)"', xml):
    lv, mid, x, z = m.groups()
    if lv == '4':
        xf = float(x.replace('f', '')); zf = float(z.replace('f', ''))
        l4.append([mid, xf, zf])
print('L4 entries:', len(l4))

# 3) ali213 中文名+游戏坐标
adata = json.load(open(r'E:\WorkSpace\BOTWmap\source\ali213_landmarks.json', encoding='utf-8'))
acnv = []
for name, lat, lng in adata:
    gx = 63.97 * lng - 8188.2
    gz = -64.0 * lat - 8192.0
    acnv.append([name, gx, gz])

# 4) 对每个 L4：显示名 + 最近 ali213 中文名
def nearest(x, z):
    best = None; bd = 1e9
    for name, gx, gz in acnv:
        d = (gx - x) ** 2 + (gz - z) ** 2
        if d < bd:
            bd = d; best = name
    return best, bd ** 0.5

print('--- L4: label | 游戏显示名(EN) | ali213中文名(最近) | 距离 ---')
for mid, x, z in l4:
    en = marker.get(mid, '?')
    cn, dist = nearest(x, z)
    print(f'{mid} | {en} | {cn} | {dist:.1f}')
