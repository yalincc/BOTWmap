# -*- coding: utf-8 -*-
"""检查底图分辨率与 ali213 瓦片配置"""
import os, re
from PIL import Image

for p in ['waypoint-data/BotW-Map.png', 'app/assets/map.webp']:
    try:
        im = Image.open(p)
        print(p, im.size, os.path.getsize(p) // 1024, 'KB')
    except Exception as e:
        print(p, 'ERR', e)

raw = open('source/ali213_raw.html', encoding='utf-8').read()
# 找瓦片 URL / tileLayer / maxZoom 等
patterns = [
    r'https?://[^"\'\s<>]*?tiles?[^"\'\s<>]*',
    r'tileLayer\([^)]{0,300}',
    r'maxZoom[^,;]{0,40}',
    r'new L\.Map\([^)]{0,200}',
    r'crss?[^;]{0,150}',
]
seen = set()
for pat in patterns:
    for m in re.finditer(pat, raw):
        s = m.group(0).strip()
        if s not in seen and len(s) > 5:
            seen.add(s)
            print(pat[:12], '=>', s[:220])
