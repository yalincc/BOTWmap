# -*- coding: utf-8 -*-
# 重建 regions.js v2：
# r3 = L4 官方简体名（ali213 名称层匹配 + MANUAL 手工修正，坐标用 Location.xml）
# r4 = 旧 r4 - (与 r3 新名重复) + L8 去重（每 label 一点）
import re, json

# --- 输入 ---
marker = {}
txt = open(r'E:\WorkSpace\BOTWmap\source\LocationMarker.xmsbt', encoding='utf-8').read()
for m in re.finditer(r'<entry label="([^"]+)">\s*<text>([^<]*)</text>', txt):
    marker[m.group(1)] = m.group(2).strip()

xml = open(r'E:\WorkSpace\BOTWmap\waypoint-data\Location.xml', encoding='utf-8').read()
l4, l8 = [], []
for m in re.finditer(r'<value ShowLevel="(\d+)">\s*<MessageID type="string">([^<]+)</MessageID>\s*<Translate X="([^"]+)"[^>]*Z="([^"]+)"', xml):
    lv, mid, x, z = m.groups()
    if lv == '4':
        l4.append([mid, float(x.replace('f', '')), float(z.replace('f', ''))])
    elif lv == '8':
        l8.append([mid, float(x.replace('f', '')), float(z.replace('f', ''))])

adata = json.load(open(r'E:\WorkSpace\BOTWmap\source\ali213_landmarks.json', encoding='utf-8'))
acnv = [[name, 63.97 * lng - 8188.2, -64.0 * lat - 8192.0] for name, lat, lng in adata]

def nearest(lst, x, z, th=20):
    best = None; bd = 1e9
    for item in lst:
        d = (item[1] - x) ** 2 + (item[2] - z) ** 2
        if d < bd:
            bd = d; best = item
    return (best[0] if best else None), (bd ** 0.5 if best else 1e9)

# --- 手工修正表（label -> 简体中文名；ali213 错位/缺失/特殊修正，以 EN 显示名+官方简中为准）---
MANUAL = {
    # L4 缺失（ali213 无近匹配）
    'BottuBay': '挠曲海湾', 'EastEunpoHighlands': '东迪普兰荒地',
    'GasemaRiver': '路塔拉河', 'HigakkareBeach': '东阿卡莱海滩',
    'HyliaMt': '海利亚山', 'KakaomePlain': '伊尔奇平原',
    'KaturaRiver': '雷根西亚河', 'KirisasaPlateau': '拉奈鲁悬崖',
    'KukujaValley': '塔纳戈峡谷', 'TwinsMountain': '双子山',
    'GoanaValley': '五郎湾', 'HyruleEastTown': '东城堡镇',
    'HyruleWestTown': '西城堡镇', 'MapArea_HyliaLake': '海利亚湖',
    'HyliaRiver': '死亡之河', 'RonronRiver': '海利亚河',
    'RonronFarm': '牧场废墟', 'ShinikkyoForest': '巨人森林',
    'ShinyarkiPlateau': '拉乌鲁山腰', 'OngiForest': '艾普林森林',
    'MasazuRock': '马萨祖岩',  # zeldawiki 保留（显示名 Quarry Ruins 疑错配）
    # ali213 错位/语义不符 -> 按 EN 官方名修正
    'AkazaMt': '拉奈鲁山脉', 'AmimePlateau': '内特台地',
    'AzimetosPlateau': '米斯塔希暗礁', 'BuibuiTrees': '布比加森林',
    'BuramuPlateau': '波普拉山麓', 'ElegLake': '纳比湖',
    'FenaMt': '纳布鲁山', 'FloriaRiver': '花柔利亚河',
    'FlogPond': '霍珀池塘', 'GibururuMt': '露托山',
    'GimpoMt': '花柔利亚山', 'GoinaPlateau': '皮埃尔台地',
    'HamiyonPlain': '奥夫利平原', 'HatenoBay': '哈特诺湾',
    'HigashinoBay': '深背湾', 'HopeBridge': '许愿之岸',
    'KasuraMt': '科瓦什峰', 'KattoriPlateau': '法罗什丘陵',
    'KinshoiMt': '达夫尼斯山', 'KitakkarePlain': '北阿卡莱山谷',
    'KitanoBay': '哈特诺湾', 'KoshaIsland': '达夫迪岛',
    'MacusePeninsula': '里斯特半岛', 'MemeMt': '普洛伊穆斯山',
    'NorthHugeStone': '大断崖', 'OkuwaLake': '西拉湖',
    'PeridoBarrier': '英杰之门', 'SaiMt': '古斯塔夫山',
    'ShitanoPond': '佛利池塘', 'SouthGerudoRuins': '南部绿洲',
    'UchoPlateau': '梅布尔岭', 'WabiLake': '阿卡莱湖',
    'YamabiLake': '雅比湖', 'ZoraBridge': '卓拉大桥',
    'EastGerudo': '东部荒漠', 'EastShrine': '东修道院',
    'RoshiganLake': '南纳比湖', 'KakufusaPlain': '哨笛山',
    'TotenLake': '橡树之脐', 'TatsubaLake': '巴特雷亚湖',
    'IsakiCape': '索卡角', 'RosomaLake': '托托湖',
    'NaganizaHill': '坦托山',
    # 第二批补漏（r3 内同名错位修正）
    'KimarikaMt': '觉醒之峰', 'PaparaPlateau': '斯塔尔里高原',
    'ShimukaPlateau': '罗布列德崖', 'KattoriPlateau': '法罗什丘陵',
}

def cn_for(mid, x, z):
    if mid in MANUAL:
        return MANUAL[mid]
    an, d = nearest(acnv, x, z)
    if an:
        return an
    return marker.get(mid, mid)

# --- 旧 regions ---
regtxt = open(r'E:\WorkSpace\BOTWmap\app\js\regions.js', encoding='utf-8').read()
old = json.loads(regtxt[regtxt.index('['):regtxt.rindex(']') + 1])
r1 = [r for r in old if r['lv'] == 'r1']
r2 = [r for r in old if r['lv'] == 'r2']
old_r4 = [r for r in old if r['lv'] == 'r4']

# --- 新 r3（L4 全部，每 label 取第一个点）---
l4_name = {}
r3 = []
seen_label = set()
for mid, x, z in l4:
    if mid in seen_label:
        continue
    seen_label.add(mid)
    nm = cn_for(mid, x, z)
    px = [round(2 * x + 12000), round(2 * z + 10000)]
    r3.append({'n': nm, 'px': px, 'lv': 'r3'})
    l4_name.setdefault(mid, nm)
r3_names = set(r['n'] for r in r3)
print('新 r3(L4 label去重):', len(r3))

# --- 新 r4 = 旧 r4 去重(与 r1/r2/r3 任何名重复) + L8 去重 ---
l8_dedup = {}
for mid, x, z in l8:
    if mid not in l8_dedup:
        l8_dedup[mid] = [x, z]

r12_names = set(r['n'] for r in r1 + r2)
r4 = []
for r in old_r4:
    if r['n'] in r3_names or r['n'] in r12_names:
        continue
    # 坐标与 r3 任一近（同一位置的旧名残留）→ 删
    if any(abs(o['px'][0] - r['px'][0]) < 60 and abs(o['px'][1] - r['px'][1]) < 60 for o in r3):
        continue
    r4.append(r)
print('旧 r4 保留(非r1/r2/r3名且不重复):', len(r4))

for mid, (x, z) in l8_dedup.items():
    nm = MANUAL.get(mid) or l4_name.get(mid) or cn_for(mid, x, z) or marker.get(mid, mid)
    if nm in r3_names or nm in r12_names:
        continue  # r3/r2 已覆盖该名字
    px = [round(2 * x + 12000), round(2 * z + 10000)]
    dup = any(abs(o['px'][0] - px[0]) < 8 and abs(o['px'][1] - px[1]) < 8 and o['n'] == nm for o in r4)
    if not dup:
        r4.append({'n': nm, 'px': px, 'lv': 'r4'})
print('r4 最终:', len(r4))

# 终审去重：
# 1) r3 内同名只留第一个（同名错位）
r3_out = []
for item in r3:
    if not any(o['n'] == item['n'] for o in r3_out):
        r3_out.append(item)
r3 = r3_out
# 2) r4 内同名只留第一个（大妖精泉 4 个保留）
r4_out = []
for item in r4:
    if item['n'] == '大妖精泉':
        r4_out.append(item)
        continue
    if not any(o['n'] == item['n'] for o in r4_out):
        r4_out.append(item)
r4 = r4_out
print('r3 去重后:', len(r3), 'r4 去重后:', len(r4))

new = r1 + r2 + r3 + r4
print(f'最终: r1={len(r1)} r2={len(r2)} r3={len(r3)} r4={len(r4)} 总={len(new)}')

# --- diff 报告 ---
old_r3 = [r for r in old if r['lv'] == 'r3']
print('--- r3 改名清单 ---')
for o in old_r3:
    match = next((r for r in r3 if abs(r['px'][0] - o['px'][0]) < 2 and abs(r['px'][1] - o['px'][1]) < 2), None)
    if match and match['n'] != o['n']:
        print(f"{o['n']} -> {match['n']}")
print('--- r4 删除(与r3重复) ---')
for o in old_r4:
    if o['n'] in r3_names:
        print(f"- {o['n']}  ({o['px'][0]},{o['px'][1]})")
print('--- r4 新增(L8) ---')
for r in r4:
    if r['n'] in (MANUAL.get(mid) or '' for mid in l8_dedup) or any(abs(r['px'][0]-(2*x+12000))<2 and abs(r['px'][1]-(2*z+10000))<2 for mid,(x,z) in l8_dedup.items()):
        pass
for mid, (x, z) in l8_dedup.items():
    nm = MANUAL.get(mid) or cn_for(mid, x, z)
    print(f"+ {nm}  ({round(2*x+12000)},{round(2*z+10000)})  [L8:{mid}]")

# 写回
with open(r'E:\WorkSpace\BOTWmap\app\js\regions.js', 'w', encoding='utf-8') as f:
    f.write('// 地名数据：r1 大区(8) r2 地形区(24) r3 细地名(L4官方简体 367) r4 更细(L8+城堡内部+独有)\n')
    f.write('// 坐标: 逻辑坐标 px（24000x20000 画布）；译名以游戏简体中文版为准\n')
    f.write('const BOTW_REGIONS = ' + json.dumps(new, ensure_ascii=False) + ';\n')
print('已写入 regions.js')
