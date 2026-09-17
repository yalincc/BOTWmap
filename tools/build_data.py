# -*- coding: utf-8 -*-
"""
Master build script v2: gamertw data + waypoint data -> app data (data.js)
Coordinate conversion (verified against 15 towers):
  gamertwX = -2*gameZ ; gamertwY = 2*gameX
  => gameX = gamertwY/2 ; gameZ = -gamertwX/2
  => map px (6000x5000): pxX = gameX/2 + 3000 ; pxY = gameZ/2 + 2500
"""
import json, re, os
from opencc import OpenCC

cc = OpenCC('t2s')
BASE = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))

# ---------- load ----------
T = json.load(open(os.path.join(BASE, 'source', 'gamertw_T_markers.json'), encoding='utf-8'))
H = json.load(open(os.path.join(BASE, 'source', 'gamertw_h_categories.json'), encoding='utf-8'))
DICT_TC_EN = json.load(open(os.path.join(BASE, 'source', 'gamertw_tc_en_dict.json'), encoding='utf-8'))
ALI = json.load(open(os.path.join(BASE, 'source', 'ali213_coords.json'), encoding='utf-8'))

# ---------- v2 additions: official CN shrine names (ali213), beast positions ----------
SHRINE_NAME_FIX = {
    'Shee Vaneer Shrine': '希贝·尼罗神庙',
    'Sha Gehma Shrine': '夏·格玛神庙',
    'Ya Naga Shrine': '亚·纳迦神庙',
    'Lanno Kooh Shrine': '拉诺·库席神庙',
    'Takama Shiri Shrine (DLC)': '塔克玛·西里神庙',
    'Noe Rajee Shrine (DLC)': '诺阿·拉基神庙',
    'Kihiro Moh Shrine (DLC)': '基席罗·莫阿神庙',
    'Kiah Toza Shrine (DLC)': '基阿·托扎神庙',
    'Shira Gomar Shrine (DLC)': '西拉·革玛神庙',
    'Keive Tala Shrine (DLC)': '基普·塔拉神庙',
    'Etsu Korima Shrine (DLC)': '艾簇·克里马神庙',
    'Rohta Chigah Shrine (DLC)': '罗塔·奇格尔神庙',
    'Yowaka Ita Shrine (DLC)': '耀瓦卡·伊塔神庙',
    'Ruvo Korbah Shrine (DLC)': '罗瓦·科巴神庙',
    'Kamia Omuna Shrine (DLC)': '卡米亚·欧姆纳神庙',
    'Rinu Honika Shrine (DLC)': '里努·霍尼卡神庙',
    'Sharo Lun Shrine (DLC)': '沙罗·伦恩神庙',
    'Sato Koda Shrine (DLC)': '沙托·科达神庙',
    'Mah Eliya Shrine (DLC)': '玛阿·艾丽娅神庙',
    'Kee Dafunia Shrine (DLC)': '凯亚·达芙妮亚神庙',
}
# 神兽本体坐标（游戏内停驻点，而非任务接取点）+ 区域/塔域人工指定
BEAST_POS_FIX = {
    'Divine Beast Vah Ruta':    (3646, -120, '拉聂尔', '拉聂尔之塔'),    # 东蓄水湖
    'Divine Beast Vah Rudania': (2562, -2968, '奥尔汀', '奥尔汀之塔'),  # 死亡火山口
    'Divine Beast Vah Naboris': (-2223, 2700, '格鲁德', '荒野之塔'),    # 眼镜岩南侧
    'Divine Beast Vah Medoh':   (-3852, -2289, '海布拉', '达邦挞之塔'),  # 飞行训练场上空
}

def parse_shrine_content(html):
    """从 ali213 ccontent 解析神庙信息：试炼名/要求/宝藏/神庙任务"""
    import html as _html
    info = {}
    if not html:
        return info
    m = re.search(r'<i>(.*?)</i>', html)
    if m:
        info['type'] = _html.unescape(m.group(1)).strip()
    for key, field in (('要求', 'req'), ('宝藏', 'treasure'), ('神庙任务', 'quest')):
        m = re.search(r'<b>%s[:：]?\s*</b>\s*(.*?)(?:<br\s*/?>|</p>|$)' % key, html, re.S)
        if m:
            val = _html.unescape(re.sub(r'<[^>]+>', '', m.group(1))).strip()
            if val and val not in ('无', 'None'):
                info[field] = val
    return info

# ali213 神庙坐标索引（gameX, gameZ -> info），容差匹配
ALI_SHRINE_POS = []
for p in ALI.get('2', []):
    gx = float(p['y']) / 2
    gz = -float(p['x']) / 2
    ALI_SHRINE_POS.append((gx, gz, parse_shrine_content(p.get('ccontent', ''))))

def find_ali_shrine(gx, gz):
    for ax, az, info in ALI_SHRINE_POS:
        if abs(ax - gx) < 1 and abs(az - gz) < 1:
            return info
    return None

CAT_META = {c['key']: c for c in H}
EN2TC = {}
for tc, en in DICT_TC_EN.items():
    EN2TC.setdefault(en, []).append(tc)

def tc2sc(s):
    s2 = cc.convert(s)
    OVER = {
        '希卡': '希卡', '神獸': '神兽', '神廟': '神庙', '驛站': '驿站', '回憶': '回忆',
        '石頭巨人': '岩石巨人', '獨眼巨人': '西诺克斯', '莫爾德拉吉克': '莫尔德拉吉克',
        '萊尼爾': '莱尼尔', '大妖精': '大精灵', '試煉': '试炼', '祝福': '祝福',
        '力之試煉': '力之试炼',
    }
    for k, v in OVER.items():
        s2 = s2.replace(k, v)
    s2 = s2.replace('‧', '·').replace('・', '·')
    return s2

def en2cn(en, default=None):
    tcs = EN2TC.get(en)
    if tcs:
        return tc2sc(tcs[0])
    return default

# ---------- waypoints ----------
js = open(os.path.join(BASE, 'waypoint-data', 'map_locations.js'), encoding='utf-8').read()
m = re.search(r'var locations = (\[.*?\]);', js, re.S)
waypoints = json.loads(re.sub(r',\s*\]', ']', m.group(1)))

def wp_px(wp):
    return wp['x'] / 2 + 3000, wp['y'] / 2 + 2500

WP_BY_EN = {}
for wp in waypoints:
    WP_BY_EN.setdefault(wp['display_name'], wp_px(wp))

# ---------- regions ----------
import xml.etree.ElementTree as ET
tree = ET.parse(os.path.join(BASE, 'waypoint-data', 'Location.xml'))
root = tree.getroot()
AREAS = []
for v in root.findall('./value'):
    mid = v.find('MessageID'); tr = v.find('Translate')
    if mid is None or tr is None:
        continue
    name = (mid.text or '').strip()
    if not (name.startswith('MapArea') or name.startswith('MapRegion')):
        continue
    gx = float(tr.attrib.get('X', '0').rstrip('f'))
    gz = float(tr.attrib.get('Z', '0').rstrip('f'))
    AREAS.append({'key': name, 'px': (gx / 2 + 3000, gz / 2 + 2500)})

REGION_CN = {
    'MapRegion_HyrulePrairie': '中央海拉鲁', 'MapRegion_Hebura': '海布拉', 'MapRegion_Eldin': '奥尔汀',
    'MapRegion_Firone': '费罗尼', 'MapRegion_Gerudo': '格鲁德', 'MapRegion_Hateru': '内卡鲁达',
    'MapRegion_Lanayru': '拉聂尔', 'MapRegion_Tamul': '阿卡莱',
}
REGIONS = [{'key': a['key'], 'cn': REGION_CN[a['key']], 'px': a['px']} for a in AREAS if a['key'] in REGION_CN]

TOWER_PX = {}
for wp in waypoints:
    if 'Tower' in wp['display_name']:
        TOWER_PX[wp['display_name']] = wp_px(wp)

def nearest(px, lst):
    best, bd = None, 1e18
    for r in lst:
        d = (r['px'][0]-px[0])**2 + (r['px'][1]-px[1])**2
        if d < bd:
            bd, best = d, r
    return best

# ---------- name tables ----------
MAIN_QUEST_CN = {
    'Follow the Sheikah Slate': '跟随希卡石板',
    'The Isolated Plateau': '孤岛',
    'Destroy Ganon': '讨伐盖侬',
    'Seek Out Impa': '拜访英帕',
    'Free the Divine Beasts': '解放神兽',
    'Locked Mementos': '被封锁的记忆',
    'Captured Memories': '记忆写真',
    'Forbidden City Entry': '潜入禁忌之城',
    "The Hero's Sword": '英雄之剑',
    'Find the Fairy Fountain': '寻找大精灵之泉',
    "Reach Zora's Domain": '前往卓拉领地',
    'Divine Beast Vah Ruta': '神兽·瓦·露塔',
    'Divine Beast Vah Rudania': '神兽·瓦·鲁达尼亚',
    'Divine Beast Vah Naboris': '神兽·瓦·纳波利斯',
    'Divine Beast Vah Medoh': '神兽·瓦·梅德',
    "EX Champions' Ballad": 'EX 英杰们的诗篇',
    "EX Champion Urbosa's Song": 'EX 英杰乌尔波扎之歌',
    "EX Champion Daruk's Song": 'EX 英杰达尔克尔之歌',
    "EX Champion Mipha's Song": 'EX 英杰米法之歌',
    "EX Champion Revali's Song": 'EX 英杰力巴尔之歌',
}
BEAST_CN = {
    'Divine Beast Vah Ruta': '神兽·瓦·露塔',
    'Divine Beast Vah Rudania': '神兽·瓦·鲁达尼亚',
    'Divine Beast Vah Naboris': '神兽·瓦·纳波利斯',
    'Divine Beast Vah Medoh': '神兽·瓦·梅德',
}

def treasure_cn(en):
    """Translate treasure item names with suffix handling."""
    if not en:
        return '宝箱'
    suffix = ''
    base = en
    mm = re.match(r'^(.*?)(\s*\(([^)]+)\))?$', en)
    if mm and mm.group(3):
        base = mm.group(1)
        sf = mm.group(3)
        suffix = {'Master Mode': '（大师模式）', 'DLC': '（DLC）'}.get(sf, '（%s）' % sf)
    qty = ''
    mq = re.match(r'^(.*?)( x(\d+))?$', base)
    if mq and mq.group(3):
        base = mq.group(1)
        qty = '×%s' % mq.group(3)
    ITEMS = {
        'Boomerang': '回旋镖', 'Boulder Breaker': '碎岩巨剑', 'Cobble Crusher': '碎石锤',
        'Double Axe': '双手斧', 'Drillshaft': '钻头枪', 'Edge of Duality': '二重刃',
        'Eightfold Blade': '八方刀', 'Eightfold Longblade': '八方长刀', 'Feathered Edge': '羽刃',
        'Feathered Spear': '羽枪', 'Fishing Harpoon': '捕鱼叉', 'Flameblade': '火焰剑',
        'Flamespear': '火焰枪', "Forest Dweller's Bow": '森民之弓', "Forest Dweller's Shield": '森民之盾',
        "Forest Dweller's Spear": '森民之枪', "Forest Dweller's Sword": '森民之剑',
        'Frostblade': '冰雪剑', 'Gerudo Scimitar': '格鲁德弯刀', 'Gerudo Shield': '格鲁德盾',
        'Gerudo Spear': '格鲁德枪', 'Giant Boomerang': '巨大回旋镖', 'Golden Bow': '黄金弓',
        'Golden Claymore': '黄金大剑', 'Great Eagle Bow': '大鹫弓', 'Great Flameblade': '火焰大剑',
        'Great Frostblade': '冰雪大剑', 'Great Thunderblade': '雷电大剑', 'Hylian Shield': '海利亚盾',
        'Ice Rod': '冰杖', 'Iron Sledgehammer': '铁锤', 'Kite Shield': '风筝盾',
        "Knight's Bow": '骑士之弓', "Knight's Broadsword": '骑士之剑', "Knight's Claymore": '骑士大剑',
        "Knight's Halberd": '骑士长枪', "Knight's Shield": '骑士盾', 'Korok Leaf': '克洛格扇子',
        'Lightscale Trident': '光鳞之枪', 'Meteor Rod': '陨石杖', 'Moonlight Scimitar': '月光弯刀',
        'Phrenic Bow': '精准弓', 'Radiant Shield': '光辉盾', 'Royal Bow': '王族之弓',
        'Royal Broadsword': '王族之剑', 'Royal Claymore': '王族大剑', "Royal Guard's Bow": '近卫兵之弓',
        "Royal Guard's Claymore": '近卫兵大剑', "Royal Guard's Shield": '近卫兵盾',
        "Royal Guard's Spear": '近卫兵枪', "Royal Guard's Sword": '近卫兵剑',
        'Royal Halberd': '王族枪', 'Royal Shield': '王族盾', 'Savage Lynel Shield': '兽王盾',
        'Scimitar of the Seven': '七宝匕首', 'Serpentine Spear': '蛇形枪',
        "Shield of the Mind's Eye": '心眼之盾', 'Silver Bow': '白银弓', 'Silver Longsword': '白银剑',
        'Silver Shield': '白银盾', 'Silverscale Spear': '银鳞枪', "Soldier's Bow": '士兵之弓',
        "Soldier's Broadsword": '士兵之剑', "Soldier's Claymore": '士兵大剑',
        "Soldier's Shield": '士兵盾', "Soldier's Spear": '士兵枪', 'Spiked Boko Bow': '钉刺波克弓',
        'Stone Smasher': '碎岩锤', 'Swallow Bow': '燕子弓', 'Throwing Spear': '投枪',
        'Thunderblade': '雷电剑', 'Thunderstorm Rod': '雷暴杖', "Traveler's Bow": '旅人之弓',
        "Traveler's Sword": '旅人之剑', 'Warm Doublet': '耐寒服', 'Windcleaver': '斩风刀',
        'Zora Helm': '卓拉头盔', 'Zora Spear': '卓拉枪', 'Arrow': '箭', 'Bomb Arrow': '炸弹箭',
        'Fire Arrow': '火焰箭', 'Ice Arrow': '冰冻箭', 'Shock Arrow': '雷电箭', 'Ancient Arrow': '古代箭',
        'Red Rupee': '红卢比', 'Purple Rupee': '紫卢比', 'Silver Rupee': '银卢比', 'Gold Rupee': '金卢比',
        'Cap of the Wild': '荒野兜帽', 'Trousers of the Wild': '荒野裤子', 'Tunic of the Wild': '荒野服',
        'Diamond Circlet': '钻石头饰', 'Hylian Trousers': '海利亚裤子', 'Old Shirt': '旧衬衫',
        'Well-Worn Trousers': '磨破的裤子', 'Thunder Helm': '雷鸣头盔', 'Island Lobster Shirt': '岛国龙虾衬衫',
        'Salvager Headwear': '回收员头带', 'Salvager Trousers': '回收员裤子', 'Salvager Vest': '回收员上衣',
        'Korok Mask': '克洛格面具', "Majora's Mask": '魔吉拉面具', "Midna's Helmet": '米多娜头盔',
        'Phantom Armor': '幻影铠甲', 'Phantom Ganon Armor': '幻影盖侬铠甲',
        'Phantom Ganon Greaves': '幻影盖侬护胫', 'Phantom Ganon Skull': '幻影盖侬头盔',
        'Phantom Greaves': '幻影护胫', 'Phantom Helmet': '幻影头盔', "Ravio's Hood": '拉维奥兜帽',
        'Royal Guard Boots': '近卫兵靴子', 'Royal Guard Cap': '近卫兵帽子', 'Royal Guard Uniform': '近卫兵制服',
        "Tingle's Hood": '汀格尔兜帽', "Tingle's Shirt": '汀格尔上衣', "Tingle's Tights": '汀格尔紧身裤',
        "Zant's Helmet": '赞特头盔', 'Nintendo Switch Shirt': '任天堂Switch上衣',
        'Travel Medallion': '传送标记', "Hestu's Maracas": '海斯特的沙锤', "Dinraal's Scale": '奥尔龙的鳞片',
        "Naydra's Scale": '奈尔龙的鳞片', "Shard of Dinraal's Fang": '奥尔龙的尖角碎片',
        "Shard of Farosh's Fang": '费罗龙的尖角碎片', "Shard of Naydra's Fang": '奈尔龙的尖角碎片',
        'Demon Carver': '恶魔雕刻刀', 'Vicious Sickle': '残忍镰刀', 'Duplex Bow': '二连弓',
        'Blizzard Rod': '暴风雪杖', 'Lightning Rod': '雷电杖', 'Fire Rod': '火杖',
        'Falcon Bow': '隼弓', 'Thunderspear': '雷电枪', 'Frostspear': '冰雪枪',
        'Zora Sword': '卓拉剑', 'Emblazoned Shield': '纹章盾', "Fisherman's Shield": '渔夫盾',
        "Hunter's Shield": '猎人盾', 'Daybreaker': '破晓之剑',
        'Diamond': '钻石', 'Ruby': '红宝石', 'Sapphire': '蓝宝石', 'Topaz': '黄玉', 'Opal': '蛋白石',
        'Amber': '琥珀', 'Luminous Stone': '夜光石', 'Flint': '燧石',
        'Ancient Core': '古代核心', 'Giant Ancient Core': '古代巨大核心', 'Ancient Screw': '古代螺丝',
        'Ancient Spring': '古代弹簧', 'Ancient Gear': '古代齿轮', 'Ancient Shaft': '古代传动轴',
        'Star Fragment': '流星碎片', 'Hylian Rice': '海拉鲁米',
        'Mighty Bananas': '强力香蕉', 'Ancient Bridle': '古代马具', 'Ancient Saddle': '古代马鞍',
    }
    cn = ITEMS.get(base)
    if cn is None:
        return en
    return cn + qty + suffix

# ---------- build markers ----------
def to_px(g):
    gx = g[1] / 2.0
    gz = -g[0] / 2.0
    return round(gx / 2 + 3000, 1), round(gz / 2 + 2500, 1)

markers_out = []
for layerDef in T:
    cat_en = layerDef['name']
    cat_key = next((c['key'] for c in H if c['english'] == cat_en), None)
    if cat_key is None:
        continue
    cn_cat = tc2sc(CAT_META[cat_key]['chineseTW'])
    for layer in layerDef.get('layers', []):
        icon_url = (layer.get('icon') or {}).get('url', '')
        for mk in layer.get('markers', []):
            name_en = mk.get('name') or ''
            mid = mk.get('id') or ''
            link = mk.get('link') or ''
            coords = mk.get('coords')
            if coords is None:
                continue
            extra = {}
            if cat_key == 'seed':
                pts = [to_px(c) for c in coords]
                px = pts[0]
                cn_name = '克洛格的果实 No.%s' % re.sub(r'\D', '', mid)
                loc_en = mk.get('loc', '')
                loc_cn = en2cn(loc_en, '')
                extra = {'korok_loc': loc_cn or loc_en, 'korok_path': pts}
            elif cat_key == 'shrine':
                # DLC shrine: name has "(DLC)" suffix
                is_dlc = 'DLC' in name_en or 'dlc' in icon_url
                lookup = re.sub(r'\s*\(DLC\)\s*$', '', name_en)
                cn_name = SHRINE_NAME_FIX.get(name_en, '')
                if not cn_name:
                    cn_name = en2cn(lookup, '')
                if not cn_name:
                    cn_name = lookup
                extra = {'is_dlc': is_dlc, 'icon': icon_url}
                # ali213 神庙信息（按坐标容差匹配）
                spx = to_px(coords)
                gx = spx[0] * 2 - 6000
                gz = spx[1] * 2 - 5000
                ali_info = find_ali_shrine(gx, gz)
                if ali_info:
                    extra.update(ali_info)
            elif cat_key == 'treasure':
                cn_name = treasure_cn(name_en)
                extra = {'icon': icon_url}
            elif cat_key == 'mainquest':
                cn_name = MAIN_QUEST_CN.get(name_en, en2cn(name_en, ''))
                if not cn_name:
                    cn_name = name_en
            elif cat_key == 'objective':
                cn_name = MAIN_QUEST_CN.get(name_en, '')
                if not cn_name:
                    cn_name = en2cn(name_en, '')
                if not cn_name:
                    cn_name = name_en
                # character sub-targets: "Divine Beast Vah Ruta Prince Sidon"
                if name_en.startswith('Divine Beast'):
                    extra['sub'] = name_en.replace('Divine Beast Vah Ruta', '').replace('Divine Beast Vah Rudania', '').replace('Divine Beast Vah Naboris', '').replace('Divine Beast Vah Medoh', '').strip()
            elif cat_key == 'shrinequest':
                # derive associated shrine from link: "ShrineName#QuestName"
                shrine_en = link.split('#')[0] if link else ''
                shrine_cn = en2cn(re.sub(r'\s*\(DLC\)\s*$', '', shrine_en), shrine_en)
                cn_name = '神庙挑战：%s' % shrine_cn if shrine_cn else '神庙挑战'
                extra = {'quest_en': name_en}
            elif cat_key == 'memory':
                # 回忆分层：照片(photo,可导航收集) / 主线自动(auto,英杰+大师剑) / DLC(dlc,英杰之诗)
                # 照片N↔回忆#N 固定映射(Impa 相册顺序 ≠ 游戏内回忆连续编号):
                #   照片1→#1 照片2→#3 照片3→#5 照片4→#7 照片5→#8 照片6→#9
                #   照片7→#11 照片8→#12 照片9→#13 照片10→#14 照片11→#15 照片12→#16 照片13→#17
                PHOTO_OF_MEMNO = {1: 1, 3: 2, 5: 3, 7: 4, 8: 5, 9: 6,
                                  11: 7, 12: 8, 13: 9, 14: 10, 15: 11, 16: 12, 17: 13}
                EX_NO_OF_TITLE = {
                    "Champion Revali's Song": 1, "Champion Daruk's Song": 2,
                    "Champion Urbosa's Song": 3, "Champion Mipha's Song": 4,
                    "The Champions' Ballad": 5,
                }
                mm = re.match(r'(?:EX )?Recovered Memory #(\d+) - (.+?)(?: \(Picture (\d+)\))?$', name_en)
                if mm:
                    no = int(mm.group(1))
                    title_en = mm.group(2)
                    is_ex = name_en.startswith('EX ')
                    if is_ex:
                        # DLC 英杰之诗回忆：与照片回忆共用编号空间会冲突，用 ex_no 标识，不占 memory_no
                        extra['tier'] = 'dlc'
                        extra['ex_no'] = EX_NO_OF_TITLE.get(title_en, no)
                        extra['nav'] = False
                    elif no in (2, 4, 6, 10):
                        # 英杰回忆（解放神兽后自动获得）
                        extra['tier'] = 'auto'
                        extra['kind'] = 'beast'
                        extra['nav'] = False
                    elif no == 18:
                        # 大师之剑回忆（拔剑后自动获得）
                        extra['tier'] = 'auto'
                        extra['kind'] = 'sword'
                        extra['nav'] = False
                    else:
                        # 照片回忆 13 个（含最终 #17 = 照片13），可导航收集
                        extra['tier'] = 'photo'
                        extra['photoNo'] = PHOTO_OF_MEMNO.get(no)
                        extra['nav'] = True
                    if not is_ex:
                        extra['memory_no'] = no
                    extra['memory_title_en'] = title_en
                cn_name = en2cn(name_en, '')
                if not cn_name:
                    cn_name = '回忆影片 #%s' % (extra.get('memory_no', mid))
            else:
                cn_name = en2cn(name_en, '')
                if not cn_name:
                    cn_name = name_en
                extra = {'icon': icon_url}
            px = to_px(coords) if cat_key != 'seed' else px
            region = nearest(px, REGIONS)
            tower_en = nearest(px, [{'px': v, 'key': k} for k, v in TOWER_PX.items()])
            tower_cn = en2cn(tower_en['key'], tower_en['key']) if tower_en else ''
            markers_out.append({
                'id': mid, 'cat': cat_key, 'name': cn_name, 'en': name_en,
                'px': px, 'link': link,
                'region': region['cn'] if region else '',
                'tower': tc2sc(tower_cn) if tower_cn else '',
                'extra': extra,
            })

# move 4 divine beasts from mainquest to dedicated beast category
beast_keys = list(BEAST_CN.keys())
beast_markers = [m for m in markers_out if m['cat'] == 'mainquest' and m['en'] in beast_keys]
markers_out = [m for m in markers_out if not (m['cat'] == 'mainquest' and m['en'] in beast_keys)]
for bm in beast_markers:
    bm['cat'] = 'beast'
    bm['name'] = BEAST_CN[bm['en']]
    # 神兽本体坐标覆盖（游戏内停驻点）
    if bm['en'] in BEAST_POS_FIX:
        gx, gz, reg, twr = BEAST_POS_FIX[bm['en']][0], BEAST_POS_FIX[bm['en']][1], BEAST_POS_FIX[bm['en']][2], BEAST_POS_FIX[bm['en']][3]
        bm['px'] = (round(gx / 2 + 3000, 1), round(gz / 2 + 2500, 1))
        bm['region'] = reg
        bm['tower'] = twr
    markers_out.append(bm)

# ---------- stats ----------
from collections import Counter
cnt = Counter(m['cat'] for m in markers_out)
print('total:', len(markers_out))
print('by category:', dict(cnt))
translated = Counter()
for m in markers_out:
    if m['name'] != m['en'] and m['name']:
        translated[m['cat']] += 1
print('translated:', dict(translated))

# ---------- output ----------
cats = [{'key': c['key'], 'en': c['english'], 'cn': tc2sc(c['chineseTW'])} for c in H]
cats.append({'key': 'beast', 'en': 'Divine Beast', 'cn': '神兽'})
# 输出前统一放大 4 倍：底图为 ali213 zoom7 拼接的高清版 24000×20000（原 6000×5000）
for _m in markers_out:
    _m['px'] = (round(_m['px'][0] * 4, 1), round(_m['px'][1] * 4, 1))
out = {
    'meta': {'mapW': 24000, 'mapH': 20000, 'mapImg': 'assets/map.jpg',
             'source': 'data: gamertw.com + ali213.net + MrCheeze/botw-waypoint-map, 坐标已换算为游戏坐标',
             'version': '2.0'},
    'categories': cats,
    'markers': markers_out,
}
os.makedirs(os.path.join(BASE, 'app'), exist_ok=True)
with open(os.path.join(BASE, 'app', 'js', 'data.js'), 'w', encoding='utf-8') as f:
    f.write('var BOTW_DATA = ')
    json.dump(out, f, ensure_ascii=False, separators=(',', ':'))
    f.write(';')
print('saved app/js/data.js', os.path.getsize(os.path.join(BASE, 'app', 'js', 'data.js')), 'bytes')
