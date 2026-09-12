# -*- coding: utf-8 -*-
"""客观对比：新版 14x 放大 vs 用户旧版截图的地图区域锐利度"""
from PIL import Image, ImageFilter

def sharpness(path, box_ratio):
    im = Image.open(path).convert('L')
    w, h = im.size
    x0 = int(w * box_ratio[0]); y0 = int(h * box_ratio[1])
    x1 = int(w * box_ratio[2]); y1 = int(h * box_ratio[3])
    crop = im.crop((x0, y0, x1, y1))
    lap = crop.filter(ImageFilter.Kernel((3, 3), [0, -1, 0, -1, 4, -1, 0, -1, 0], 1, 0))
    px = list(lap.getdata())
    n = len(px)
    var = sum((p - sum(px) / n) ** 2 for p in px) / n
    return round(var, 1)

# 旧版（用户截图 1680x1244，地图在右 55% 起）
old = sharpness(r'E:\WorkSpace\BOTWmap\source\user_compare_2.png', (0.35, 0.05, 0.98, 0.95))
# 新版（我的截图 849x1089，地图在右 40% 起）
new = sharpness(r'E:\WorkSpace\BOTWmap\source\user_view_14x.png', (0.05, 0.08, 0.95, 0.95))
print('旧版模糊截图 拉普拉斯方差:', old)
print('新版 14x 截图 拉普拉斯方差:', new)
print('提升倍数:', round(new / max(old, 0.1), 2))
