# -*- coding: utf-8 -*-
"""验证拼接图与旧底图内容一致（取中央海拉鲁城堡区域对比）"""
from PIL import Image
Image.MAX_IMAGE_PIXELS = None

new = Image.open(r'E:\WorkSpace\BOTWmap\app\assets\map.jpg')
old = Image.open(r'E:\WorkSpace\BOTWmap\waypoint-data\BotW-Map.png')
print('new:', new.size, 'old:', old.size)

# 中央海拉鲁城堡 24000 系像素（游戏坐标 -254,-612 → px=(11492,8776)）
cx, cy = 11492, 8776
r = 256
new_crop = new.crop((cx - r, cy - r, cx + r, cy + r))
# 旧图同位置（÷4）：2873, 2194
ox, oy = (cx // 4), (cy // 4)
rr = 64
old_crop = old.crop((ox - rr, oy - rr, ox + rr, oy + rr)).resize((512, 512))
new_crop.save(r'E:\WorkSpace\BOTWmap\source\verify_new_castle.png')
old_crop.save(r'E:\WorkSpace\BOTWmap\source\verify_old_castle.png')

# 粗略相似度（缩略到 32x32 灰度差）
import math
a = new_crop.convert('L').resize((32, 32))
b = old_crop.convert('L').resize((32, 32))
pa, pb = list(a.getdata()), list(b.getdata())
diff = sum(abs(x - y) for x, y in zip(pa, pb)) / len(pa)
print('avg abs diff (0-255):', round(diff, 1))
print('saved verify crops')
