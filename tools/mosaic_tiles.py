# -*- coding: utf-8 -*-
"""拼接 ali213 zoom7 瓦片 → 24000×20000 高清底图（地图内容区域），转 webp"""
import os
from PIL import Image
Image.MAX_IMAGE_PIXELS = None  # 高清底图 480M 像素，解除 PIL 防炸弹上限

SRC = r'E:\WorkSpace\BOTWmap\source\tiles_z7'
OUT_PNG = r'E:\WorkSpace\BOTWmap\source\map_hd_24000.png'
OUT_JPG = r'E:\WorkSpace\BOTWmap\app\assets\map.jpg'

TX0, TX1 = 17, 110   # 含 110
TY0, TY1 = 24, 103   # 含 103
NX = TX1 - TX0 + 1   # 94
NY = TY1 - TY0 + 1   # 80

# 内容区域相对拼接图左上角的偏移（瓦片坐标 → 内容 24000×20000）
# 内容 X∈[4384,28384], Y∈[6384,26384]；瓦片 17 起点 4352，瓦片 24 起点 6144
OFF_X = 4384 - TX0 * 256   # 32
OFF_Y = 6384 - TY0 * 256   # 48

W, H = 24064, 20480
print('mosaic size:', W, H, '-> crop at', OFF_X, OFF_Y, 'size 24000x20000')

full = Image.new('RGB', (W, H))
cnt = 0
for ty in range(TY0, TY1 + 1):
    for tx in range(TX0, TX1 + 1):
        fp = os.path.join(SRC, '7_%d_%d.png' % (tx, ty))
        if not os.path.exists(fp):
            raise SystemExit('MISSING ' + fp)
        t = Image.open(fp).convert('RGB')
        full.paste(t, ((tx - TX0) * 256, (ty - TY0) * 256))
        t.close()
        cnt += 1
print('pasted', cnt, 'tiles')

crop = full.crop((OFF_X, OFF_Y, OFF_X + 24000, OFF_Y + 20000))
print('cropped:', crop.size)
crop.save(OUT_PNG)
print('saved', OUT_PNG, os.path.getsize(OUT_PNG) // 1024 // 1024, 'MB')
# WebP 编码器单边限 16383px，24000 超限 → 用 JPEG（底图不透明，无损需求低）
crop.save(OUT_JPG, 'JPEG', quality=88, optimize=True, progressive=True)
print('saved', OUT_JPG, os.path.getsize(OUT_JPG) // 1024 // 1024, 'MB')
