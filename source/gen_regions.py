# -*- coding: utf-8 -*-
"""从 Location.xml 生成 app/js/regions.js（主要地名：8 大区 + 21 地形区，简体中文对照）"""
import re, json

xml = open(r'E:\WorkSpace\BOTWmap\waypoint-data\Location.xml', encoding='utf-8').read()
items = re.findall(r'<value ShowLevel="(\d+)">\s*<MessageID type="string">([^<]+)</MessageID>\s*<Translate X="([^"]+)" Y="[^"]+" Z="([^"]+)" />\s*<Type>(\d+)</Type>', xml)
by_id = {}
for show, mid, x, z, typ in items:
    mid = mid.strip()
    if mid not in by_id:
        by_id[mid] = {'x': float(x.rstrip('f')), 'z': float(z.rstrip('f'))}

def to_px(x, z):
    return [round(2 * x + 12000), round(2 * z + 10000)]

# 简体中文版对照（MapRegion=8大区，MapArea=主要地形区；CentralHyrule 与大区重复、LiveMountain 存疑，均剔除）
REGIONS = [
    ("MapRegion_Hebura",   "海布拉地区"),
    ("MapRegion_Eldin",    "奥尔汀地区"),
    ("MapRegion_Tamul",    "阿卡莱地区"),
    ("MapRegion_Lanayru",  "拉聂尔地区"),
    ("MapRegion_Hateru",   "哈特诺地区"),
    ("MapRegion_HyrulePrairie", "中央海拉鲁"),
    ("MapRegion_Firone",   "费罗尼地区"),
    ("MapRegion_Gerudo",   "格鲁德地区"),
]
AREAS = [
    ("MapArea_DeathMountain",      "死亡之山"),
    ("MapArea_EastHateru",         "东哈特诺"),
    ("MapArea_EldinCanyon",        "奥尔汀大峡谷"),
    ("MapArea_EldinMountains",     "奥尔汀山脉"),
    ("MapArea_FironeGrassland",    "费罗尼草原"),
    ("MapArea_FironeSea",          "费罗尼海"),
    ("MapArea_GerudoDesert",       "格鲁德沙漠"),
    ("MapArea_GerudoHighlands",    "格鲁德高地"),
    ("MapArea_HateruSea",          "哈特诺海"),
    ("MapArea_HeburaMountains",    "海布拉山"),
    ("MapArea_HyliaLake",          "海利亚湖"),
    ("MapArea_HyruleForest",       "海拉鲁大森林"),
    ("MapArea_HyruleHill",         "海拉鲁丘陵"),
    ("MapArea_LanayruSea",         "拉聂尔海"),
    ("MapArea_LanayruWaterSources","拉聂尔水源"),
    ("MapArea_LanayruWetlands",    "拉聂尔湿地"),
    ("MapArea_TabantaFrontier",    "塔邦达边境"),
    ("MapArea_TamulOutback",       "深远阿卡莱"),
    ("MapArea_TamulPlateau",       "阿卡莱高原"),
    ("MapArea_TamulSea",           "阿卡莱海"),
    ("MapArea_WestHateru",         "西哈特诺"),
]

out = []
for key, cn in REGIONS + AREAS:
    v = by_id[key]
    px = to_px(v['x'], v['z'])
    lv = 'region' if key in dict(REGIONS) else 'area'
    out.append({'n': cn, 'px': px, 'lv': lv})

js = "/* 主要地形/区域名（简体中文官方译名，来自游戏解包 Location 数据 + 中文版对照） */\n"
js += "const BOTW_REGIONS = " + json.dumps(out, ensure_ascii=False, indent=1) + ";\n"
open(r'E:\WorkSpace\BOTWmap\app\js\regions.js', 'w', encoding='utf-8').write(js)
print('生成 app/js/regions.js 共', len(out), '个地名')
for r in out:
    print(f"  [{r['lv']:6s}] {r['n']} px({r['px'][0]},{r['px'][1]})")
