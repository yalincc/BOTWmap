# -*- coding: utf-8 -*-
"""并发抓取 ali213 zoom7 瓦片（地图内容区域 tx 17-110, ty 24-103）"""
import os, time, urllib.request
from concurrent.futures import ThreadPoolExecutor, as_completed

OUT = r'E:\WorkSpace\BOTWmap\source\tiles_z7'
os.makedirs(OUT, exist_ok=True)
URL = 'https://zelda.ali213.net/tiles/7_{x}_{y}.png'
HEADERS = {
    'Referer': 'http://zelda.ali213.net/',
    'User-Agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/120.0 Safari/537.36',
}
TX = range(17, 111)   # 17..110
TY = range(24, 104)   # 24..103

def fetch(xy):
    x, y = xy
    fp = os.path.join(OUT, '7_%d_%d.png' % (x, y))
    if os.path.exists(fp) and os.path.getsize(fp) > 500:
        return (x, y, 'skip')
    for attempt in range(3):
        try:
            req = urllib.request.Request(URL.format(x=x, y=y), headers=HEADERS)
            with urllib.request.urlopen(req, timeout=20) as r:
                data = r.read()
            if len(data) < 500:
                raise IOError('too small %d' % len(data))
            with open(fp, 'wb') as f:
                f.write(data)
            return (x, y, 'ok')
        except Exception as e:
            if attempt == 2:
                return (x, y, 'FAIL: %s' % e)
            time.sleep(1.5 * (attempt + 1))
    return (x, y, 'FAIL')

tasks = [(x, y) for x in TX for y in TY]
total = len(tasks)
print('total tiles:', total)
ok = skip = fail = 0
t0 = time.time()
with ThreadPoolExecutor(max_workers=14) as ex:
    futs = {ex.submit(fetch, t): t for t in tasks}
    done = 0
    for fut in as_completed(futs):
        x, y, st = fut.result()
        done += 1
        if st == 'ok': ok += 1
        elif st == 'skip': skip += 1
        else: fail += 1
        if done % 500 == 0 or done == total:
            el = time.time() - t0
            print('[%d/%d] ok=%d skip=%d fail=%d  %.0fs' % (done, total, ok, skip, fail, el))
print('DONE ok=%d skip=%d fail=%d' % (ok, skip, fail))
