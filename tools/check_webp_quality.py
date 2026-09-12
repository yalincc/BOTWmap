# -*- coding: utf-8 -*-
"""验证瓦片 PNG→WebP(q82) 的画质损失"""
import io, os, statistics
from PIL import Image, ImageChops

os.chdir(r'E:\WorkSpace\BOTWmap')
for f in ['7_63_63.png', '4_7_7.png']:
    p = os.path.join('app', 'assets', 'tiles', f)
    im = Image.open(p).convert('RGB')
    buf = io.BytesIO()
    im.save(buf, 'WEBP', quality=82, method=6)
    webp = Image.open(io.BytesIO(buf.getvalue())).convert('RGB')
    diff = ImageChops.difference(im, webp)
    px = list(diff.getdata())
    maxd = max(max(r, g, b) for r, g, b in px)
    avg = statistics.mean((r + g + b) / 3 for r, g, b in px)
    pct99 = sorted((r + g + b) / 3 for r, g, b in px)[int(len(px) * 0.99)]
    print('%s: MAX diff=%d AVG=%.3f P99=%.1f (0-255尺度)' % (f, maxd, avg, pct99))
