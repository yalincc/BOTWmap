# -*- coding: utf-8 -*-
"""批量把 app/assets/tiles/*.png 转成 *.webp（q82），保留 PNG 直到验证通过"""
import os
from concurrent.futures import ProcessPoolExecutor
from PIL import Image

TILES = r'E:\WorkSpace\BOTWmap\app\assets\tiles'

def convert_one(path):
    try:
        webp = os.path.splitext(path)[0] + '.webp'
        if os.path.exists(webp) and os.path.getsize(webp) > 500:
            return (os.path.basename(path), 'skip', os.path.getsize(webp))
        im = Image.open(path)
        im.load()
        im.save(webp, 'WEBP', quality=82, method=4)
        return (os.path.basename(path), 'ok', os.path.getsize(webp))
    except Exception as e:
        return (os.path.basename(path), 'FAIL:%s' % e, 0)

def main():
    files = [os.path.join(TILES, f) for f in os.listdir(TILES) if f.endswith('.png')]
    print('total png:', len(files))
    ok = skip = fail = 0
    orig_bytes = webp_bytes = 0
    with ProcessPoolExecutor(max_workers=8) as ex:
        for i, (name, st, size) in enumerate(ex.map(convert_one, files), 1):
            if st == 'ok':
                ok += 1
                webp_bytes += size
                orig_bytes += os.path.getsize(os.path.join(TILES, name))
            elif st == 'skip':
                skip += 1
            else:
                fail += 1
                print('FAIL', name, st)
            if i % 1200 == 0 or i == len(files):
                print('[%d/%d] ok=%d skip=%d fail=%d' % (i, len(files), ok, skip, fail))
    print('DONE ok=%d skip=%d fail=%d' % (ok, skip, fail))
    if ok:
        print('PNG %.1fMB -> WEBP %.1fMB (%.0f%%)' % (orig_bytes / 1048576, webp_bytes / 1048576, webp_bytes * 100 / orig_bytes))

if __name__ == '__main__':
    main()
