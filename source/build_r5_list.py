# -*- coding: utf-8 -*-
"""生成《进入提示地点补充清单.csv》：官方 LocationPointer 缺失点 + 海拉鲁城堡本体
- r5（地图放到最大后显示）：城堡内部房间 11 条
- r4（按既有规则显示）：海拉鲁城堡 + 其他 39 条
数据源：Static.xml LocationPointer/LocationMarker + 692/704 中文名表
"""
import xml.etree.ElementTree as ET, json, re, math, csv
BASE = r'E:\WorkSpace\BOTWmap'
root = ET.parse(BASE + r'\waypoint-data\Static.xml').getroot()
regions = json.loads(re.search(r'const\s+BOTW_REGIONS\s*=\s*(\[.*?\])\s*;',
                    open(BASE + r'\app\js\regions.js', encoding='utf-8').read(), re.S).group(1))
pts = [(r['n'], r['px'][0], r['px'][1]) for r in regions]

# 中文名：692 表 + 704 表补充
msbt = {}
for row in csv.DictReader(open(BASE + r'\source\简体中文地名表.csv', encoding='utf-8-sig')):
    msbt[row['label'].strip()] = row['simplified_chinese'].strip()
extra = {}
s = open(BASE + r'\官方地名核对.md', encoding='utf-8').read()
st = s.index('## 二、官方地名全表'); en = s.index('## 附录 A')
for l in s[st:en].splitlines():
    if l.startswith('| '):
        p = [x.strip() for x in l.split('|')]
        if len(p) >= 3 and p[1]:
            extra[p[1]] = p[2]
def zh(lbl):
    return msbt.get(lbl) or extra.get(lbl) or ''

def label_of(v):
    sf = v.find('SaveFlag'); mids = v.findall('MessageID')
    return sf.text[9:] if sf is not None and sf.text and sf.text.startswith('Location_') else (mids[0].text if mids else '')

# 收集 LocationPointer 缺失点
missing = []
for v in root.find('LocationPointer').findall('value'):
    tr = v.find('Translate')
    lbl = label_of(v)
    if not lbl:
        continue  # 无名纯指针点排除
    x = float(tr.attrib['X'].rstrip('f')); z = float(tr.attrib['Z'].rstrip('f'))
    px, py = 2*x+12000, 2*z+10000
    best = min(pts, key=lambda p: math.hypot(p[1]-px, p[2]-py))
    if math.hypot(best[1]-px, best[2]-py) >= 10:
        missing.append({'label': lbl, 'px_x': px, 'px_y': py, 'src': 'LocationPointer'})

# 海拉鲁城堡本体（LocationMarker）
for v in root.find('LocationMarker').findall('value'):
    lbl = label_of(v)
    if lbl == 'HyruleCastle':
        tr = v.find('Translate')
        x = float(tr.attrib['X'].rstrip('f')); z = float(tr.attrib['Z'].rstrip('f'))
        missing.append({'label': lbl, 'px_x': 2*x+12000, 'px_y': 2*z+10000, 'src': 'LocationMarker'})

# 智慧之泉：label=WisdomFountain，中文名取自 WiseFountain
for m in missing:
    if m['label'] == 'WisdomFountain' and not zh(m['label']):
        m['zh_override'] = '智慧之泉'

# 城堡房间 11 条（r5）
CASTLE_ROOMS = {'HyruleCastle_Room_0','HyruleCastle_Room_1','HyruleCastle_Room_2','HyruleCastle_Room_3',
                'HyruleCastle_Room_5','HyruleCastle_Room_6','HyruleCastle_Room_7','HyruleCastle_Room_8',
                'HyruleCastle_Room_9','HyruleCastle_Room_10','HyruleCastle_Room_11'}

# 类别归类
def category(lbl, name):
    if lbl in CASTLE_ROOMS: return '城堡房间'
    if lbl == 'HyruleCastle': return '城堡'
    if lbl.endswith('Bridge') or '桥' in name: return '桥梁'
    if 'Spa' in lbl or 'Fountain' in lbl or '泉' in name: return '温泉/泉'
    if 'Entrance' in lbl or '入口' in name or lbl in ('HatenoGate','ShigonDam','LanayruWestEntrance'): return '入口/门'
    if 'Village' in lbl or '村' in name or 'Ruins' in lbl or '遗迹' in name or 'Legacy' in lbl or 'Park' in lbl or 'Garrison' in lbl: return '遗迹/村落'
    return '其他'

rows = []
for m in sorted(missing, key=lambda m: (0 if m['label'] in CASTLE_ROOMS else 1, m['label'])):
    name = m.get('zh_override') or zh(m['label']) or '(缺中文)'
    lv = 'r5' if m['label'] in CASTLE_ROOMS else 'r4'
    rows.append({
        '层级': lv, 'label': m['label'], '中文名': name,
        'game_x': round((m['px_x']-12000)/2, 1), 'game_z': round((m['px_y']-10000)/2, 1),
        'px_x': m['px_x'], 'px_y': m['px_y'], '类别': category(m['label'], name),
        '来源': m['src'], '备注': '官方中文名(704表)' if m.get('zh_override') else ('缺失中文名' if '(缺' in name else '')
    })

out = BASE + r'\source\进入提示地点补充清单.csv'
with open(out, 'w', encoding='utf-8-sig', newline='') as f:
    w = csv.DictWriter(f, fieldnames=['层级','label','中文名','game_x','game_z','px_x','px_y','类别','来源','备注'])
    w.writeheader()
    for r in rows:
        w.writerow(r)

from collections import Counter
print('总条数:', len(rows))
print('按层级:', dict(Counter(r['层级'] for r in rows)))
print('按类别:', dict(Counter(r['类别'] for r in rows)))
print('缺中文:', [r['label'] for r in rows if '缺' in r['备注']])
print('--- r5 城堡房间 ---')
for r in rows:
    if r['层级'] == 'r5':
        print(f'  {r["中文名"]:10s} {r["label"]:28s} ({r["px_x"]},{r["px_y"]})')
print('已写入:', out)
