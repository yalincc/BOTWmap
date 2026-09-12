# -*- coding: utf-8 -*-
"""并发抓取 ali213 zoom1-6 瓦片（内容区域范围按 zoom7 推导）"""
import os, time, urllib.request
from concurrent.futures import ThreadPoolExecutor, as_completed

OUT = r'E:\WorkSpace\BOTWmap\source\tiles'
URL = 'https://zelda.ali213.net/tiles/{z}_{x}_{y}.png'
HEADERS = {
    'Referer': 'http://zelda.ali213.net/',
    'User-Agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/120.0 Safari/537.36',
}
# 各 zoom 的内容像素范围（zoom7 为 [4384,28384]x[6384,26384]，每降一级除 2）
# zoom: (tx0, tx1, ty0, ty1)  含两端
ZOOM_RANGES = {
    6: (8, 55, 12, 51),
    5: (4, 27, 6, 25),
    4: (2, 13, 3, 12),
    3: (1, 6, 1, 6),
    2: (0, 3, 0, 3),
    1: (0, 1, 0, 1),
}

def fetch(zxy):
    z, x, y = zxy
    fp = os.path.join(OUT, '%d_%d_%d.png' % (z, x, y))
    if os.path.exists(fp) and os.path.getsize(fp) > 500:
        return (zxy, 'skip')
    for attempt in range(3):
        try:
            req = urllib.request.Request(URL.format(z=z, x=x, y=y), headers=HEADERS)
            with urllib.request.urlopen(req, timeout=20) as r:
                data = r.read()
            if len(data) < 500:
                raise IOError('too small %d' % len(data))
            with open(fp, 'wb') as f:
                f.write(data)
            return (zxy, 'ok')
        except Exception as e:
            if attempt == 2:
                return (zxy, 'FAIL:%s' % e)
            time.sleep(1.5 * (attempt + 1))
    return (zxy, 'FAIL')

tasks = []
for z, (tx0, tx1, ty0, ty1) in ZOOM_RANGES.items():
    for x in range(tx0, tx1 + 1):
        for y in range(ty0, ty1 + 1):
            tasks.append((z, x, y))
print('total zoom1-6 tiles:', len(tasks))

ok = skip = fail = 0
t0 = time.time()
with ThreadPoolExecutor(max_workers=14) as ex:
    futs = {ex.submit(fetch, t): t for t in tasks}
    done = 0
    for fut in as_completed(futs):
        _, st = fut.result()
        done += 1
        if st == 'ok': ok += 1
        elif st == 'skip': skip += 1
        else: fail += 1
        if done % 400 == 0 or done == len(tasks):
            print('[%d/%d] ok=%d skip=%d fail=%d %.0fs' % (done, len(tasks), ok, skip, fail, time.time() - t0))
print('DONE ok=%d skip=%d fail=%d' % (ok, skip, fail))
