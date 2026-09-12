# -*- coding: utf-8 -*-
"""提取 MapRegion_* 和 MapArea_* 地名（游戏坐标→逻辑像素），输出清单"""
import re, json

xml = open(r'E:\WorkSpace\BOTWmap\waypoint-data\Location.xml', encoding='utf-8').read()
items = re.findall(r'<value ShowLevel="(\d+)">\s*<MessageID type="string">([^<]+)</MessageID>\s*<Translate X="([^"]+)" Y="[^"]+" Z="([^"]+)" />\s*<Type>(\d+)</Type>', xml)

# 名称 → 首条记录（坐标/类型/等级）
by_id = {}
for show, mid, x, z, typ in items:
    mid = mid.strip()
    if mid not in by_id:
        by_id[mid] = {'show': int(show), 'x': float(x.rstrip('f')), 'z': float(z.rstrip('f')), 'type': int(typ)}

regions = {k: v for k, v in by_id.items() if k.startswith('MapRegion_')}
areas = {k: v for k, v in by_id.items() if k.startswith('MapArea_')}

def to_px(x, z):
    return [round(2 * x + 12000), round(2 * z + 10000)]

print('===== MapRegion_*（8 大区）=====')
for k in sorted(regions):
    v = regions[k]
    px = to_px(v['x'], v['z'])
    print(f"{k:28s} show={v['show']} type={v['type']} 坐标({v['x']:.0f},{v['z']:.0f}) -> px({px[0]},{px[1]})")

print()
print('===== MapArea_*（22 个地形区）=====')
for k in sorted(areas):
    v = areas[k]
    px = to_px(v['x'], v['z'])
    print(f"{k:32s} show={v['show']} type={v['type']} 坐标({v['x']:.0f},{v['z']:.0f}) -> px({px[0]},{px[1]})")
