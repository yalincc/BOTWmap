/* ============================================================
 * 旷野之息互动地图 · 任务板块 · M3 · 任务卡片
 * ------------------------------------------------------------
 * 卡片渲染 + 交互：分类徽章 / #N 编号 / 位置名 / 前置·后续 /
 * 任务要点 / 流程地点（位置名+类型色）/ 底部（定位·连线·标记完成）/
 * 上一下一导航 / 复制 / 拖动 / 空白与 Esc 关闭 / hover 预览态机。
 * 对外：window.QuestCard
 * ============================================================ */
(function (global) {
  'use strict';

  var D = global.QuestData;
  var QUESTS = D ? D.QUESTS : [];

  function M() { return global.__botwMap || null; }

  function esc(s) {
    return String(s == null ? '' : s).replace(/[&<>"']/g, function (c) {
      return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c];
    });
  }

  var REGION_EN = {
    '中央海拉鲁': 'Central Hyrule',
    '费罗尼': 'Faron',
    '内卡鲁达': 'Necluda',
    '格鲁德': 'Gerudo',
    '奥尔汀': 'Eldin',
    '拉聂尔': 'Lanayru',
    '海布拉': 'Hebra',
    '阿卡莱': 'Akkala',
    '塔邦挞': 'Tabantha',
    '达邦挞': 'Tabantha'
  };

  var EXTRA_LANDMARKS = {
    '女神像': 'Goddess Statue',
    '复苏神庙': 'Shrine of Resurrection',
    '卡拉卡拉集市': 'Kara Kara Bazaar',
    '一始村': 'Tarrey Town',
    '沃托里村': 'Lurelin Village',
    '鼓隆城': 'Goron City',
    '卓拉领地': "Zora's Domain",
    '军营': 'Barracks',
    '大精灵蒂拉': 'Great Fairy Tera',
    '南部采矿场': 'Southern Mine',
    '哈特诺古代研究所': 'Hateno Ancient Tech Lab',
    '阿卡莱古代研究所': 'Akkala Ancient Tech Lab',
    '格鲁德小镇': 'Gerudo Town',
    '卡卡利科村': 'Kakariko Village',
    '利特村': 'Rito Village',
    '哈特诺村': 'Hateno Village',
    '克洛格森林': 'Korok Forest'
  };

  var translateLocation = function (str) {
    if (!str) return '';
    if (EXTRA_LANDMARKS[str]) return EXTRA_LANDMARKS[str];
    if (global.I18N && global.I18N.regionOf) {
      var r = global.I18N.regionOf(str);
      if (r !== str) return r;
    }
    if (global.I18N && global.I18N.towerOf) {
      var t = global.I18N.towerOf(str);
      if (t !== str) return t;
    }
    var dataMarkers = (global.BOTW_DATA && global.BOTW_DATA.markers) || (global.map && global.map.data && global.map.data.markers);
    if (dataMarkers) {
      for (var mi = 0; mi < dataMarkers.length; mi++) {
        if (dataMarkers[mi].name === str && dataMarkers[mi].en) return dataMarkers[mi].en;
      }
    }
    return str;
  };

  var NPC_EN = {
    '奇异的声音': 'Strange Voice',
    '老人': 'Old Man',
    '海拉鲁国王': 'King Rhoam',
    '英帕': 'Impa',
    '本杰明': 'Benja',
    '德库树长老': 'Great Deku Tree',
    '皮冈': 'Pikango',
    '希多': 'Prince Sidon',
    '塞尔达': 'Zelda',
    '布尔多': 'Bludo',
    '卡昂': 'Kaneli',
    '璐菊': 'Riju',
    '帕雅': 'Paya',
    '古老的石版': 'Ancient Stone Slab',
    '利迦尼': 'Garini',
    '奥可芭': 'Magda',
    '贝盖': 'Bayge',
    '卡西': 'Kass',
    '卡西瓦': 'Kass',
    '可莱维亚': 'Clavia',
    '西默茨凯': 'Medda',
    '卡里尤': 'Calip',
    '鲁拉曲': 'Laruta',
    '诺比尔': 'Nobiro',
    '雪山的日志': 'Mountain Peak Log',
    '拉亦娜': 'Laine',
    '帕弗宇': 'Shabonne',
    '罗汀': 'Rotana',
    '帕可忒': 'Pokki',
    '果戈': 'Geggle',
    '阿兹基': 'Zooki',
    '泰奇奥': 'Tasho',
    '达秘达米': 'Damia',
    '桑伏': 'Mayro',
    '柳奈': 'Loone',
    '洁琳': 'Jerrin',
    '诺波坦': 'Nobotan',
    '奇尔': 'Kheel',
    '蓓拉': 'Bedoli',
    '布拉东': 'Bladon',
    '巴恺忒': 'Bayge',
    '桃虹': 'Molli',
    '多米达克': 'Domidak',
    '伯库林': 'Hestu',
    '弗撒兰': 'Rensa',
    '博嘉多': 'Cado',
    '塞罗拉': 'Rola',
    '珂珂娜': 'Koko',
    '拉兹莉': 'Lasli',
    '儒秀': 'Manny',
    '恶魔像': 'Horned Statue',
    '樱达': 'Bolson',
    '哈吉': 'Hagie',
    '纳卜': 'Neb',
    '万作': 'Manny',
    '托可优': 'Koyin',
    '普尔亚': 'Purah',
    '西蒙': 'Symin',
    '松达': 'Hudson',
    '普莉珂': 'Cottla',
    '菲内': 'Finley',
    '诺尔': 'Gruve',
    '福拉拉拖': 'Laflat',
    '彭特': 'Fronk',
    '托尔伏': 'Torfeau',
    '雷托冈': 'Ledo',
    '扶丘': 'Fronk',
    '伊琐': 'Izra',
    '基奥托': 'Jiahto',
    '伯腾萨': 'Bozai',
    '兹基': 'Zyle',
    '格玛纳': 'Sesami',
    '匹鲁艾': 'Pirou',
    '柒内玛': 'Guy',
    '艾夏': 'Isha',
    '依格莱塔': 'Greta',
    '梅尔埃娜': 'Malena',
    '陀鲁': 'Toren',
    '缇克尔': 'Dalia',
    '朗洁': 'Liana',
    '基丘乌': 'Kiana',
    '洛泽尔': 'Rozel',
    '萨巴卡亚': 'Sebasto',
    '西米纳': 'Cima',
    '思达吉': 'Chio',
    '卡拉塞': 'Kula',
    '匹康': 'Peeks',
    '奥拉科': 'Akrah',
    '夏媚': 'Shamae',
    '陀特茨': 'Toffa',
    '图洛陀': 'Trott',
    '伊莱扎': 'Eliza',
    '茱英': 'Jana',
    '霍司塔': 'Hoz',
    '司狄亚': 'Straia',
    '索艾': 'Blyn',
    '赖斯': 'Lester',
    '茱诺': 'Juney',
    '久戈': 'Jogo',
    '哈鲁丽': 'Ruli',
    '哈米拉': 'Amali',
    '基藏': 'Gesane',
    '帕莉塞': 'Parcy',
    '戈艾塔弗': 'Gotter',
    '麦萨奇': 'Kima',
    '呼戈': 'Fugo',
    '科普思': 'Kopops',
    '拉梅拉': 'Ramella',
    '一叶': 'Juannelle',
    '纳末敏': 'Walton',
    '修行者': 'Monk Maz Koshia',
    '传闻杂志V1': 'Super Rumor Mill V1',
    '传闻杂志V2': 'Super Rumor Mill V2',
    '传闻杂志V3': 'Super Rumor Mill V3',
    '传闻杂志V4': 'Super Rumor Mill V4',
    '米斯科的EX日志': "Misko's EX Journal",
    '米斯科的EX日志2': "Misko's EX Journal 2",
    '（联动任务，自动激活）': 'Auto-activated (Collab)'
  };

  var REWARD_EN = {
    '滑翔伞（Paraglider）': 'Paraglider',
    '卓拉铠甲（Zora Armor）': 'Zora Armor',
    '卓拉护胫（卓拉套装下装）': 'Zora Greaves',
    '大师剑（Master Sword）': 'Master Sword',
    '大师之剑永久觉醒（攻击力常驻提升）': 'Master Sword Awakened (Permanent Power Boost)',
    '英杰服（Champion\'s Tunic）': "Champion's Tunic",
    '摩托·零式（Master Cycle Zero）': 'Master Cycle Zero',
    '米法的祝福（英杰技能·自动回血）': "Mipha's Grace (Champion Ability)",
    '达尔克尔的守护（英杰技能·格挡反击）': "Daruk's Protection (Champion Ability)",
    '力巴尔的猛攻（英杰技能·原地起跳滑翔）': "Revali's Gale (Champion Ability)",
    '乌尔波扎的愤怒（英杰技能·范围落雷）': "Urbosa's Fury (Champion Ability)",
    '米法的祝福+（冷却时间缩短）': "Mipha's Grace + (Reduced Cooldown)",
    '达尔克尔的守护+（冷却时间缩短至 6 分钟）': "Daruk's Protection + (Reduced Cooldown)",
    '力巴尔的猛攻+（冷却时间缩短）': "Revali's Gale + (Reduced Cooldown)",
    '乌尔波扎的愤怒+（冷却时间缩短）': "Urbosa's Fury + (Reduced Cooldown)",
    '通关结局（封印灾厄盖侬，救出塞尔达）': 'Game Clear (Seal Calamity Ganon, Rescue Zelda)',
    '雷鸣头盔（雷电免疫）': 'Thunder Helm (Lightning Proof)',
    '橡胶头盔（绝缘头盔）': 'Rubber Helm (Shock Resistance)',
    '沙地靴（Sand Boots）': 'Sand Boots',
    '雪地靴': 'Snow Boots',
    '克洛格面具（Korok Mask）': 'Korok Mask',
    '魔吉拉面具（Majora\'s Mask）': "Majora's Mask",
    '米多娜头盔（Midna\'s Helmet）': "Midna's Helmet",
    '赞特头盔（Zant\'s Helmet）': "Zant's Helmet",
    '罗瓦头巾（Ravio\'s Hood）': "Ravio's Hood",
    '海洋龙虾衬衫（Island Lobster Shirt）': 'Island Lobster Shirt',
    '传送标记器（Travel Medallion）': 'Travel Medallion',
    '幻影头盔 + 幻影铠甲 + 幻影护腿': 'Phantom Helmet + Armor + Greaves',
    '汀之帽 + 汀之衫 + 汀之裤（汀套装）': "Tingle's Hood + Shirt + Tights",
    '幻影加农骷髅 + 幻影加农铠甲 + 幻影加农护腿': 'Phantom Ganon Skull + Armor + Greaves',
    '近卫兵之帽 + 近卫兵服 + 近卫兵靴': 'Royal Guard Cap + Uniform + Boots',
    '荒野之帽 + 荒野衬衣 + 荒野裤子（荒野套）': 'Cap of the Wild + Tunic of the Wild + Trousers of the Wild',
    '打捞员头盔 + 打捞员外衣 + 打捞员裤子（Salvager 套装）': 'Salvager Headwear + Vest + Trousers',
    '格鲁德套装（花费 600 卢比从维利亚处购买）': 'Gerudo Outfit (Buy from Vilia for 600 Rupees)',
    '林克的房子（可自由装修、挂武器）': "Link's House (Furnish & Weapon Displays)",
    '背包格扩容（武器/弓/盾袋，需克洛格种子交换）': 'Inventory Expansion (Weapon/Bow/Shield Stash via Korok Seeds)',
    '耐火铠甲（Flamebreaker Armor）': 'Flamebreaker Armor',
    '古代缰绳 + 古代马鞍': 'Ancient Bridle + Ancient Saddle',
    '王族缰绳 + 王族马鞍': 'Royal Bridle + Royal Saddle',
    '可在心之容器与精力容器之间互换的交易功能': 'Heart Container / Stamina Vessel Exchange Service',
    '可购买黄玉耳坠/蓝宝石环/红宝石环（星光记忆店）': 'Unlock Jewelry Shop (Topaz Earrings, Sapphire Circlet, Ruby Circlet)',
    '解锁大精灵泉（可强化装备）': 'Unlock Great Fairy Fountain (Armor Upgrades)',
    '解锁大精灵科特拉之泉（可强化防具）；皮冈提示一处回忆点位置': 'Unlock Great Fairy Cotera Fountain (Armor Upgrades); Memory Clue from Pikango',
    '解锁希卡之石地图（初始台地之塔升起，全图希卡之塔同步激活）': 'Unlock Sheikah Slate Map (Great Plateau Tower Activated)',
    '解锁格鲁德秘密俱乐部（可买夜光套装等）': 'Unlock Gerudo Secret Club (Radiant Set, etc.)',
    '解锁野莓/蜜瓜采摘地': 'Unlock Wildberry / Hydromelon Spots',
    '希卡之石恢复相机符文 + 海拉鲁图鉴': 'Restore Camera Rune + Hyrule Compendium',
    '希卡感应器+、远程炸弹+、静止器+': 'Sheikah Sensor+, Remote Bomb+, Stasis+',
    '新鲜牛奶×10': 'Fresh Milk ×10',
    '大松软菇×3': 'Big Hearty Truffle ×3',
    '硬生命松露×3（Hearty Truffle）': 'Hearty Truffle ×3',
    '精力鲈鱼×5（Staminoka Bass）': 'Staminoka Bass ×5',
    '速速莲实×5（Fleet-Lotus Seeds）': 'Fleet-Lotus Seeds ×5',
    '毅力胡萝卜': 'Endura Carrot',
    '星星碎片': 'Star Fragment',
    '远古箭×3': 'Ancient Arrow ×3',
    '铠甲草（Armoranth）': 'Armoranth',
    '钻头长枪（Drillshaft）': 'Drillshaft',
    '骑士之剑（Knight\'s Broadsword）': "Knight's Broadsword",
    '钻石': 'Diamond',
    '钻石×2（后续每10个夜光石换1钻石）': 'Diamond ×2 (Subsequent 10 Luminous Stones for 1 Diamond)',
    '钻石×3（并建成一始村）': 'Diamond ×3 (Complete Tarrey Town)',
    '岩石盐（岩盐）/ 琥珀等矿石': 'Rock Salt / Amber & Ores',
    '宝箱：蛋白石、琥珀、蓝宝石×2、夜光石、火焰剑等': 'Chests: Opal, Amber, Sapphire ×2, Luminous Stone, Flameblade, etc.',
    '蓝宝石×1、黄玉×2、雷电剑×1': 'Sapphire ×1, Topaz ×2, Thunderblade ×1',
    '硬肉酿南瓜（料理配方/成品）': 'Tough Meat-Stuffed Pumpkin (Recipe / Meal)',
    '精力蜂蜜苹果（料理配方/成品）': 'Energizing Honeyed Apple (Recipe / Meal)',
    '速效蔬菜奶油汤（料理配方/成品）': 'Hasty Veggie Cream Soup (Recipe / Meal)',
    '黄油烤苹果（料理配方/成品）': 'Hot Buttered Apple (Recipe / Meal)',
    '获得奥尔迪拉鳞片，开启茨茨阿·尼玛神庙': "Dinraal's Scale, Unlock Tutsuwa Nima Shrine",
    '获得费罗鳞片，开启希·库乔夫神庙': "Farosh's Scale, Unlock Shae Katha Shrine",
    '获得古代之球罗斯科，开启希·塔塔恩科神庙': 'Ancient Orb Roscoe, Unlock Shoqa Tatone Shrine',
    '净化那德鲁并获得其鳞片，开启吉塔·萨米神庙': "Purify Naydra & Obtain Scale, Unlock Jitan Sa'mi Shrine",
    '夺回全部装备，开启库古·齐德神庙': 'Reclaim All Equipment, Unlock Korgu Chideh Shrine',
    '夺回希卡族传家宝，开启拉纳·罗基神庙': 'Recover Sheikah Heirloom, Unlock Lakna Rokee Shrine',
    '寻回祭祀三叉戟，开启达·基基神庙': 'Recover Ceremonial Trident, Unlock Dagah Keek Shrine',
    '救活波基，开启密·苏神庙': 'Revive Pokki, Unlock Misae Suma Shrine',
    '兄弟团聚，开启克哈·拉玛神庙': 'Reunite Brothers, Unlock Kayra Mah Shrine',
    '护送小克洛格完成试炼，开启达·乔卡席神庙': 'Escort Oaki through Trial, Unlock Daag Chokah Shrine',
    '通过古特check挑战，开启古·阿齐托神庙': 'Pass Gut Check Challenge, Unlock Gorae Torr Shrine',
    '赢得大宝球（古代之球），开启拉丘·乌罗神庙': 'Win Ancient Orb, Unlock Raqa Zunzo Shrine',
    '逐步获红/紫/银卢比，最终金卢比+钻石': 'Progressive Rupees (Red/Purple/Silver), Final Gold Rupee + Diamond',
    '500 卢比': '500 Rupees',
    '金卢比': 'Gold Rupee (300 Rupees)',
    '金卢比（300卢比）': 'Gold Rupee (300 Rupees)',
    '银卢比': 'Silver Rupee (100 Rupees)',
    '银卢比 + 金卢比': 'Silver Rupee (100) + Gold Rupee (300)',
    '银卢比（100卢比）': 'Silver Rupee (100 Rupees)',
    '银卢比（100卢比）+ 红卢比（20卢比）': 'Silver Rupee (100) + Red Rupee (20)',
    '紫卢比': 'Purple Rupee (50 Rupees)',
    '紫卢比（50卢比）': 'Purple Rupee (50 Rupees)',
    '紫卢比（50卢比）+ 海鲜烩饭': 'Purple Rupee (50) + Seafood Paella',
    '红卢比（20卢比）': 'Red Rupee (20 Rupees)',
    '红卢比（20卢比）+ 银卢比（100卢比）': 'Red Rupee (20) + Silver Rupee (100)'
  };

  function translateReward(str) {
    if (!str) return '';
    if (REWARD_EN[str]) return REWARD_EN[str];
    var m = str.match(/^开启(.*?)神庙(.*)$/);
    if (m) {
      var sname = m[1].trim() + '神庙';
      var extra = m[2].trim();
      var enShrine = translateLocation(sname);
      var exText = extra === '（祝福神庙）' ? ' (Blessing)' : (extra ? ' ' + extra : '');
      return 'Unlock ' + enShrine + exText;
    }
    return str;
  }

  var curQuest = null;

  /* 关联任务链接（key 关联制）：reqs/unlockList 元素为 {key,name}，
     每个渲染成可点击的 co-link（data-q-key 跳转 openCard），多个用「、」分隔；
     空 / 缺失 → 显示「无」（tk-dash 样式） */
  function linkList(arr, tip) {
    var isEn = (global.I18N && global.I18N.getLang() === 'en');
    var noneText = isEn ? 'None' : '无';
    if (!arr || !arr.length) return '<span class="tk-dash">' + noneText + '</span>';
    var sep = isEn ? ', ' : '、';
    return arr.map(function (r) {
      var rName = r.name;
      if (isEn) {
        for (var i = 0; i < QUESTS.length; i++) {
          if (QUESTS[i].key === r.key && QUESTS[i].en) {
            rName = QUESTS[i].en;
            break;
          }
        }
      }
      return '<span class="co-link" data-q-key="' + esc(r.key) + '" title="' + tip + '">' + esc(rName) + '</span>';
    }).join(sep);
  }

  function openCard(q) {
    var map = M();
    var card = document.getElementById('questCard');
    var body = document.getElementById('questCardBody');
    if (!card || !body) return;
    card.dataset.manual = '';   /* V1.6.1 调整i：新卡 → 清除手动态，重新避让 */
    /* 兼容传入 key 字符串：按 key 在 QUESTS 中查找任务对象；找不到 toast 提示并退出 */
    if (typeof q === 'string') {
      var k = q, found = null;
      for (var i = 0; i < QUESTS.length; i++) { if (QUESTS[i].key === k) { found = QUESTS[i]; break; } }
      if (!found) {
        if (typeof UI !== 'undefined' && UI.toast) UI.toast('未找到任务：' + k);
        return;
      }
      q = found;
    }
    curQuest = q;
    lastOpen = Date.now();
    pv.mode = 'fixed'; pv.ref = q;   /* 点击路径默认固定态（previewOpen 随后覆盖为 preview） */

    var isEn = (global.I18N && global.I18N.getLang() === 'en');
    var done = global.QuestDone.isDone(q.key);
    var src = global.QuestDone.srcOf(q.key);
    /* V1.6.2：位置名（游戏内无「坐标」概念，不显示 X/Z）——
       取首个带 note 的流程点，**剥掉末尾动作词**（note 格式「地名·动作」；
       不能按第一个「·」切：神庙名如「拉丘·乌罗神庙」本身含「·」）；
       无 note（未补地名的任务）→ 不显示位置行，交由下方「流程地点」呈现 */
    var place = null;
    var PLACE_ACTS = ['接取', '交付', '对话', '战斗', '前往'];
    (q.flowPts || []).forEach(function (f) {
      if (place || !f.note) return;
      var n = String(f.note), idx = n.lastIndexOf('·');
      if (idx > 0 && PLACE_ACTS.indexOf(n.slice(idx + 1)) >= 0) n = n.slice(0, idx);
      place = { name: n.trim(), px: [f.gx * 2 + 12000, f.gz * 2 + 10000] };
    });
    /* V1.6.5 Q6（老大拍板）：神庙挑战「位置」统一为神庙本身——任务名与神庙标记名 42/42 全同，
       位置行显示神庙名，点击定位并打开神庙卡（挑战↔神庙互跳）；消除「有些是地名、有些是神庙」 */
    var shrineMk = null;
    if (q.cat === 'shrine' && window.__botwMap && window.__botwMap.data && window.__botwMap.data.markers) {
      var _mks = window.__botwMap.data.markers;
      for (var _i = 0; _i < _mks.length; _i++) {
        if (_mks[_i].cat === 'shrine' && _mks[_i].name === q.name) { shrineMk = _mks[_i]; break; }
      }
    }
    if (shrineMk) place = { name: shrineMk.name, px: shrineMk.px, mk: shrineMk };
    var chipCls = q.cat === 'main' ? 'tk-chip-main' : q.cat === 'side' ? 'tk-chip-imp' : 'tk-chip-sub';
    /* 同档序列：先按数据字段 no 升序；no 缺失时保持数组原序（稳定兜底，新旧数据兼容） */
    var sameCat = QUESTS.filter(function (x) { return x.cat === q.cat; });
    var seqList = sameCat.map(function (x, i) { return { q: x, idx: i }; })
      .sort(function (a, b) {
        if (a.q.no == null && b.q.no == null) return a.idx - b.idx;
        if (a.q.no == null) return 1;
        if (b.q.no == null) return -1;
        return a.q.no - b.q.no;
      })
      .map(function (r) { return r.q; });
    var seq = (q.no != null) ? q.no : sameCat.indexOf(q) + 1;   /* 档内编号（#N 徽章） */

    var catDisplay = D.catName ? D.catName(q.cat) : D.CAT_NAME[q.cat];

    var h = '';
    /* 头部：分类徽章 + 完成勾 + 上/下导航 + 关闭 */
    h += '<div class="tk-grip">';
    h += '<span class="tk-chip ' + chipCls + '">' + catDisplay + '</span>';
    /* V1.6.1：DLC 徽章（仿 q-badge-hard 胶囊；DLC1 紫 / DLC2 暗金 / 联动 青绿） */
    var dlg = D.dlcBadgeOf(q);
    var dlgText = dlg === '联动' ? (isEn ? 'Collab' : '联动') : dlg;
    if (dlg === 'DLC1') h += '<span class="q-badge-hard" style="background:rgba(139,92,246,.16);border-color:#8a5cf6;color:#b18cff">DLC1</span>';
    else if (dlg === 'DLC2') h += '<span class="q-badge-hard" style="background:rgba(216,178,92,.16);border-color:#c9a04e;color:#eac27e">DLC2</span>';
    else if (dlg === '联动') h += '<span class="q-badge-hard" style="background:rgba(126,200,169,.16);border-color:#4e9c7c;color:#7ec8a9">' + dlgText + '</span>';
    var doneTickTitle = src === 'save'
      ? (isEn ? 'Completed (from save)' : '已完成（来自存档）')
      : (isEn ? 'Completed (manual)' : '已完成（手动标记）');
    if (done) h += '<span class="tk-done-tick" title="' + doneTickTitle + '">✓</span>';
    h += '<span class="tk-grip-acts">' +
      '<button type="button" class="tk-icon-btn tk-nav" id="qPrev" title="' + (global.I18N ? global.I18N.t('card.prev', '上一个') : '上一个任务') + '">‹</button>' +
      '<button type="button" class="tk-icon-btn tk-nav" id="qNext" title="' + (global.I18N ? global.I18N.t('card.next', '下一个') : '下一个任务') + '">›</button>' +
      '<button type="button" class="tk-close" id="qcardClose" title="' + (global.I18N ? global.I18N.t('side.close', '关闭') : '关闭') + '">×</button></span>';
    h += '</div>';

    var mainTitle = isEn ? (q.en || q.name) : q.name;
    var subTitle = isEn ? (q.en ? q.name : '') : '';

    /* 标题行：#N 编号徽章 + 任务名 + 复制（对齐 TOTKmap） */
    h += '<div class="tk-title-row">' +
      '<span class="tk-no-badge" title="' + (isEn ? 'Quest #' + seq : ('本档第 ' + seq + ' 个任务')) + '">#' + seq + '</span>' +
      '<h3 class="tk-name">' + esc(mainTitle) + '</h3>' +
      '<button type="button" class="tk-copy" id="qCopy" title="' + (global.I18N ? global.I18N.t('card.copy.title', '复制任务名') : '复制任务名') + '">' + (global.I18N ? global.I18N.t('card.copy', '[复制]') : '[复制]') + '</button></div>';
    if (subTitle && subTitle !== mainTitle) {
      h += '<div class="qcard-en">' + esc(subTitle) + '</div>';
    }

    /* 关键信息：区域一行、位置一行 */
    var tRegion = global.I18N ? global.I18N.t('card.region', '区域') : '区域';
    var tPos = global.I18N ? global.I18N.t('card.pos', '位置') : '位置';
    h += '<div class="tk-meta">';
    if (q.region) {
      var regVal = isEn ? (REGION_EN[q.region] || q.region) : q.region;
      h += '<div class="tk-mrow"><span class="tk-mk">' + tRegion + '</span><span class="tk-mv">' + esc(regVal) + '</span></div>';
    }
    if (place) {
      var placeName = isEn
        ? ((place.mk && place.mk.en) ? place.mk.en : translateLocation(place.name))
        : place.name;
      var placeTip = place.mk ? (isEn ? 'Click to locate and open shrine' : '点击定位并打开该神庙') : (isEn ? 'Click to locate' : '点击定位到该地点');
      h += '<div class="tk-mrow"><span class="tk-mk">' + tPos + '</span><span class="co-link" data-q-locate="1" title="' + placeTip + '">' + esc(placeName) + '</span></div>';
    }
    h += '</div>';

    /* 前置 / 解锁（key 关联制：渲染 q.reqs / q.unlockList，空 → 「无」） */
    var tReqs = global.I18N ? global.I18N.t('quest.reqs', '前置') : '前置';
    var tUnlocks = global.I18N ? global.I18N.t('quest.unlocks', '解锁') : '解锁';
    var reqTip = isEn ? 'Click to view prerequisite quest' : '点击查看前置任务';
    var unlTip = isEn ? 'Click to view follow-up quest' : '点击查看后续任务';
    h += '<div class="tk-kv tk-kv-div"><span class="tk-k">' + tReqs + '</span><span class="tk-v">' +
      linkList(q.reqs, reqTip) + '</span></div>';
    h += '<div class="tk-kv"><span class="tk-k">' + tUnlocks + '</span><span class="tk-v">' +
      linkList(q.unlockList, unlTip) + '</span></div>';

    /* 任务要点区 */
    var tSummary = global.I18N ? global.I18N.t('quest.summary', '任务要点') : '任务要点';
    var tStart = global.I18N ? global.I18N.t('quest.start', '接取') : '接取';
    var tNote = global.I18N ? global.I18N.t('quest.note', '注意') : '注意';
    var tReward = global.I18N ? global.I18N.t('quest.reward', '奖励') : '奖励';
    var qEn = (window.BOTW_QUESTS_EN && window.BOTW_QUESTS_EN[q.key]) || {};
    var descText = isEn ? (qEn.desc || q.desc_en || q.desc) : q.desc;
    var noteText = isEn ? (qEn.note || q.note_en || q.note) : q.note;
    var tips = '';
    if (descText) tips += '<div class="tk-sec-b">' + esc(descText).replace(/\n/g, '<br>') + '</div>';
    if (q.npc) {
      var npcText = (isEn && NPC_EN[q.npc]) ? NPC_EN[q.npc] : q.npc;
      tips += '<div class="tk-kv"><span class="tk-k">' + tStart + '</span><span class="tk-v">' + (isEn ? 'Talk to ' : '对话') + esc(npcText) + '</span></div>';
    }
    if (noteText) tips += '<div class="tk-kv"><span class="tk-k">' + tNote + '</span><span class="tk-v">' + esc(noteText).replace(/\n/g, '<br>') + '</span></div>';
    if (q.reward) {
      var rewText = isEn ? translateReward(q.reward) : q.reward;
      tips += '<div class="tk-kv"><span class="tk-k">' + tReward + '</span><span class="tk-v">' + esc(rewText) + '</span></div>';
    }
    if (tips) h += '<div class="tk-sec"><div class="tk-sec-t">' + tSummary + '</div>' + tips + '</div>';

    /* 流程地点 */
    var flowList = (q.flowPts && q.flowPts.length)
      ? q.flowPts.map(function (f) { return { px: [f.gx * 2 + 12000, f.gz * 2 + 10000], type: f.type || 'other', note: f.note || '' }; })
      : (q.pts || []).map(function (p) { return { px: p, type: 'other', note: '' }; });
    if (flowList.length) {
      var QNUM = ['①','②','③','④','⑤','⑥','⑦','⑧','⑨','⑩','⑪','⑫','⑬','⑭','⑮','⑯','⑰','⑱','⑲','⑳'];
      var typeCn = (global.QuestFlow && global.QuestFlow.typeCn) ? global.QuestFlow.typeCn : function (t) { return t; };
      var TYPE_EN = { take: 'Start', deliver: 'Deliver', battle: 'Battle', npc: 'Key NPC', other: 'Other' };
      var ACT_EN = {
        '接取': 'Start',
        '交付': 'Turn In',
        '对话': 'Talk',
        '战斗': 'Battle',
        '调查': 'Investigate',
        '祭坛': 'Pedestal',
        '神庙': 'Shrine',
        '寻找': 'Search',
        '终点': 'Destination',
        '前往': 'Go To',
        '解密': 'Puzzle',
        '点火': 'Light Torch'
      };
      var translateFlowNote = function (note, type) {
        if (!note) return TYPE_EN[type] || type;
        var lastDot = note.lastIndexOf('·');
        if (lastDot > 0) {
          var loc = note.slice(0, lastDot);
          var act = note.slice(lastDot + 1);
          var locEn = translateLocation(loc);
          var actEn = ACT_EN[act] || act;
          return locEn + ' · ' + actEn;
        }
        if (ACT_EN[note]) return ACT_EN[note];
        return translateLocation(note);
      };
      var fpts = flowList.map(function (f, i) {
        var typeLabel = isEn ? (TYPE_EN[f.type] || f.type) : typeCn(f.type);
        var label = isEn ? translateFlowNote(f.note, f.type) : (f.note || typeLabel);
        var tipText = typeLabel + (isEn ? ' · Click to locate' : ' · 点击定位');
        return '<span class="fp fp-t-' + f.type + '" data-q-pt="' + i + '" title="' + tipText + '">' +
          '<span class="num">' + (QNUM[i] || (i + 1)) + '</span><span class="txt">' + esc(label) + '</span></span>';
      }).join('');
      var tFlowPts = global.I18N ? global.I18N.t('quest.flowPts', '流程地点') : '流程地点';
      var spotsUnit = isEn ? ' spots' : ' 处';
      h += '<div class="tk-sec"><div class="tk-sec-t">' + tFlowPts + '<span class="tk-src">' + flowList.length + spotsUnit + '</span></div><div class="flow-pts">' + fpts + '</div></div>';
    }

    /* 完成状态来源 */
    var srcText = src === 'save'
      ? (global.I18N ? global.I18N.t('quest.saveSync', '完成状态：存档自动同步（游戏内为准）') : '完成状态：存档自动同步（游戏内为准）')
      : src === 'manual'
        ? (global.I18N ? global.I18N.t('quest.manualMark', '完成状态：手动标记') : '完成状态：手动标记')
        : (global.I18N ? global.I18N.t('quest.unmarked', '完成状态：未标记（无存档标志，可手动标记）') : '完成状态：未标记（无存档标志，可手动标记）');
    h += '<div class="qcard-src">' + srcText + '</div>';

    /* 底部操作 */
    h += '<div class="tk-actions">';
    var tLocate = global.I18N ? global.I18N.t('card.locate', '定位') : '定位';
    if (q.pts && q.pts.length) {
      h += '<button type="button" class="btn act" id="qcardLocate">' + tLocate + '</button>';
    } else {
      var noLocTip = global.I18N ? global.I18N.t('quest.noPlace', '本任务在地图上无固定坐标，请用下方「流程地点」定位') : (q.noPlaceReason || '本任务在地图上无固定坐标，请用下方「流程地点」定位');
      h += '<button type="button" class="btn act is-off" id="qcardLocate" disabled title="' + esc(noLocTip) + '">' + tLocate + '</button>';
    }

    var flowOn = !!(global.QuestFlow && global.QuestFlow.isOn(q.key));
    var flowN = (global.QuestFlow && global.QuestFlow.pointsOf)
      ? global.QuestFlow.pointsOf(q).length
      : (q.flowPts ? q.flowPts.length : 0);
    if (flowN >= 2) {
      var flowBtnText = flowOn
        ? (global.I18N ? global.I18N.t('quest.flowClose', '关闭连线') : '关闭连线')
        : (global.I18N ? global.I18N.t('quest.flow', '任务连线') : '任务连线');
      h += '<button type="button" class="btn act tk-ghost' + (flowOn ? ' is-on' : '') + '" id="qcardFlow">' + flowBtnText + '</button>';
    }
    if (done && src === 'save') {
      h += '<button type="button" class="btn done is-done" disabled>' + (global.I18N ? global.I18N.t('quest.doneSave', '已完成 ✓（存档）') : '已完成 ✓（存档）') + '</button>';
    } else {
      var doneBtnText = done
        ? (global.I18N ? global.I18N.t('card.undone', '取消完成') : '取消完成')
        : (global.I18N ? global.I18N.t('card.done', '标记完成') : '标记完成');
      h += '<button type="button" class="btn done' + (done ? ' is-done' : '') + '" id="qcardDone">' + doneBtnText + '</button>';
    }
    h += '</div>';

    body.innerHTML = h;
    card.classList.remove('hidden');

    /* 定位 */
    var lc = document.getElementById('qcardLocate');
    if (lc) lc.addEventListener('click', function (e) {
      e.stopPropagation();
      global.QuestCanvas.locate(q);
      positionCard();
    });
    /* 位置名链接定位（定位到该地点 + 红点脉冲） */
    body.querySelectorAll('[data-q-locate]').forEach(function (el) {
      el.addEventListener('click', function (e) {
        e.preventDefault();
        e.stopPropagation();
        if (!place || !map) return;
        var p = place.px;
        map.view.scale = Math.min(Math.max(map.view.scale, 1.2), map.maxScale);
        map.view.x = map.w / 2 - p[0] * map.view.scale;
        map.view.y = map.h / 2 - p[1] * map.view.scale;
        map.draw();
        if (place.mk) {   /* V1.6.5 Q6：神庙挑战位置 → 关任务卡、定位并打开神庙卡（互跳闭环 + 脉冲特效） */
          if (global.QuestPanel && global.QuestPanel.closeCard) global.QuestPanel.closeCard();
          if (map._onSelect) map._onSelect({ marker: place.mk });
          if (map.pulseAt) map.pulseAt(p[0], p[1]);
          if (global.SpotMark) global.SpotMark.set(p);
        } else {
          if (map.pulseAt) map.pulseAt(p[0], p[1]);
          if (global.SpotMark) global.SpotMark.set(p);
        }
        positionCard();
      });
    });
    /* 前置 / 后续跳转（key 关联制：点击关联链接按 key 打开对应任务卡） */
    body.querySelectorAll('[data-q-key]').forEach(function (el) {
      el.addEventListener('click', function (e) {
        e.preventDefault();
        e.stopPropagation();
        if (el.dataset.qKey) openCard(el.dataset.qKey);
      });
    });
    /* 流程地点定位（V1.6.2：点击高亮 + 红点脉冲） */
    body.querySelectorAll('[data-q-pt]').forEach(function (el) {
      el.addEventListener('click', function (e) {
        e.preventDefault();
        e.stopPropagation();
        var f = flowList[+el.dataset.qPt];
        if (!f || !map) return;
        body.querySelectorAll('.fp.is-cur').forEach(function (x) { x.classList.remove('is-cur'); });
        el.classList.add('is-cur');
        var p = f.px;
        map.view.scale = Math.min(Math.max(map.view.scale, 1.2), map.maxScale);
        map.view.x = map.w / 2 - p[0] * map.view.scale;
        map.view.y = map.h / 2 - p[1] * map.view.scale;
        map.draw();
        if (map.pulseAt) map.pulseAt(p[0], p[1]);
        if (global.SpotMark) global.SpotMark.set(p);
        positionCard();
      });
    });
    /* 任务连线（V1.6.2）：开关流程连线并刷新按钮态 */
    var fb = document.getElementById('qcardFlow');
    if (fb) fb.addEventListener('click', function (e) {
      e.stopPropagation();
      if (!global.QuestFlow) return;
      global.QuestFlow.toggle(q.key);
      openCard(q);
    });
    /* 标记完成 */
    var dn = document.getElementById('qcardDone');
    if (dn) dn.addEventListener('click', function (e) {
      e.stopPropagation();
      var st = global.QuestDone.toggle(q.key);
      openCard(q);
      if (global._questRefreshRows) global._questRefreshRows();
      if (map) map.draw();
      if (typeof UI !== 'undefined' && UI.toast) {
        var toastTitle = isEn ? (q.en || q.name) : q.name;
        UI.toast(st.done
          ? (st.src === 'save' ? (isEn ? 'Completed (from save)' : '已完成（来自存档）') : (isEn ? 'Marked complete: ' + toastTitle : '已标记完成：' + q.name))
          : (isEn ? 'Marked incomplete: ' + toastTitle : '已取消完成：' + q.name));
      }
    });
    /* 关闭 */
    var cl = document.getElementById('qcardClose');
    if (cl) cl.addEventListener('click', function (e) {
      e.stopPropagation();
      card.classList.add('hidden');
    });
    /* 复制任务名 */
    var cp = document.getElementById('qCopy');
    if (cp) cp.addEventListener('click', function (e) {
      e.stopPropagation();
      var copyText = isEn ? (q.en || q.name) : q.name;
      if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(copyText).catch(function () {});
        if (typeof UI !== 'undefined' && UI.toast) UI.toast((isEn ? 'Copied: ' : '已复制：') + copyText);
      }
    });
    /* 上一个 / 下一个（本档内按 no 排序后 ±1，同 TOTKmap 任务卡） */
    function navBy(delta) {
      var i = seqList.indexOf(q);
      if (i < 0) return;
      var j = i + delta;
      if (j < 0 || j >= seqList.length) {
        if (typeof UI !== 'undefined' && UI.toast) UI.toast(delta < 0 ? (isEn ? 'First quest in group' : '已是本档第一个任务') : (isEn ? 'Last quest in group' : '已是本档最后一个任务'));
        return;
      }
      openCard(seqList[j]);
    }
    var qPrevBtn = document.getElementById('qPrev');
    if (qPrevBtn) qPrevBtn.addEventListener('click', function (e) { e.stopPropagation(); navBy(-1); });
    var nx = document.getElementById('qNext');
    if (nx) nx.addEventListener('click', function (e) { e.stopPropagation(); navBy(1); });
    positionCard();
  }

  function positionCard() {
    var map = M();
    var card = document.getElementById('questCard');
    if (!card || !map) return;
    if (card.dataset.manual === '1') return;   /* 手动拖过：保持手动态位 */
    /* 卡片此刻还是 hidden = 正要打开新卡 → 清掉上次定位锚点，改用地图点击点 */
    if (card.classList.contains('hidden') && global.CardLayout) global.CardLayout.clearAnchor();
    /* V1.6.1 调整g：统一走 map.layoutCard 避让定位；预览锚点优先 hover 坐标 */
    var sx, sy;
    if (previewNow && typeof map._hoverSX === 'number') { sx = map._hoverSX; sy = map._hoverSY; }
    /* ★ 优先复用最近一次定位的锚点（卡片内点坐标定位后，外层还会再调一次本函数，
       若此时退回 _lastClick 会把正确的避让结果覆盖掉） */
    else if (global.CardLayout && global.CardLayout.getAnchor()) { var a = global.CardLayout.getAnchor(); sx = a.sx; sy = a.sy; }
    else if (map._lastClick) { sx = map._lastClick.sx; sy = map._lastClick.sy; }
    else { sx = map.w / 2; sy = map.h * 0.45; }
    map.layoutCard(card, sx, sy);
  }

  /* V1.6.1 调整5：hover 预览态机 hidden | preview | fixed
     - hover 到任务点 → preview 显示（未固定，离开即隐藏）
     - 点击 → fixed（离开不隐藏，× / Esc / 点空白才关） */
  var pv = { mode: 'hidden', ref: null };
  var previewNow = false;   // positionCard 锚点：预览时跟随鼠标 hover 坐标

  function closeCard() {
    var card = document.getElementById('questCard');
    if (card) card.classList.add('hidden');
    pv.mode = 'hidden'; pv.ref = null; previewNow = false;
  }

  /* hover 预览入口（map.js mousemove 仅变化时回调） */
  function onHoverExt(ext) {
    /* V1.6.1：PC 端悬停不再弹卡预览（改为地图左上角名称条），只有点击才弹卡 */
    if (window.innerWidth > 760) return;
    if (pv.mode === 'fixed') return;           // 已固定：hover 不打扰
    var mine = ext && ext.type === 'quest' ? ext.quest : null;
    if (mine) {
      if (pv.mode === 'preview' && pv.ref === mine) return;
      previewNow = true;                        // positionCard 用 hover 坐标锚点
      openCard(mine);
      pv.mode = 'preview'; pv.ref = mine;
      previewNow = false;
    } else if (pv.mode !== 'hidden') {
      var card = document.getElementById('questCard');
      if (card) card.classList.add('hidden');
      pv.mode = 'hidden'; pv.ref = null;
    }
  }

  /* 点地图空白处关闭任务卡（与 #infoCard 行为一致）
     ★ 修复 V1.6.0 点击任务点不弹卡：canvas 的 click 先触发 openCard（显示），
       同一 click 冒泡到 document 又触发 closeCard 把它关掉。
       现在：若本次点击（<400ms）刚打开任务卡，说明点在任务点上 → 不关闭。 */
  var lastOpen = 0;
  document.addEventListener('click', function (e) {
    var map = M();
    var card = document.getElementById('questCard');
    if (!card || card.classList.contains('hidden')) return;
    var el = e.target;
    if (card.contains(el)) return;
    /* ★ V1.6.3：刚拖完卡片，这一次 click 不关卡（原实现只靠 400ms 阈值，
       拖久了就失效 → 卡片一松手就消失，表现为「拖不动」） */
    if (card._dragged) return;
    if (el && el.closest && el.closest('#questCard')) return;
    if (map && map.canvas && map.canvas.contains(el) && Date.now() - lastOpen < 400) return;
    closeCard();
  });
  document.addEventListener('keydown', function (e) {
    if (e.key === 'Escape') closeCard();
  });
  /* V1.6.1 调整g：地图空白命中广播 → 关闭任务卡并复位 pv 态机 */
  window.addEventListener('botw:blankclose', function () { closeCard(); });

  if (global.I18N && global.I18N.onChange) {
    global.I18N.onChange(function () {
      var card = document.getElementById('questCard');
      if (card && !card.classList.contains('hidden') && curQuest) {
        openCard(curQuest);
      }
    });
  }

  global.QuestCard = {
    openCard: openCard,
    closeCard: closeCard,
    positionCard: positionCard,
    onHoverExt: onHoverExt
  };
})(window);
