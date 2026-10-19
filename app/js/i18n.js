/* ============================================================
 * BOTWmap 国际化引擎（i18n）
 * 零第三方依赖、轻量级原生实现、中/英双语切换与优雅 Fallback
 * ============================================================ */
'use strict';

const I18N = (() => {
  const STORAGE_KEY = 'botwmap.lang.v1';
  let curLang = 'zh';
  const listeners = [];

  function init() {
    try {
      const saved = localStorage.getItem(STORAGE_KEY);
      if (saved === 'en' || saved === 'zh') {
        curLang = saved;
      }
    } catch (e) {}
    if (document.documentElement) {
      document.documentElement.lang = curLang === 'en' ? 'en' : 'zh-CN';
    }
  }

  function getLang() {
    return curLang;
  }

  function t(key, fallback) {
    if (!key) return '';
    const dict = (window.BOTW_I18N && window.BOTW_I18N[curLang]) || {};
    if (dict[key] !== undefined && dict[key] !== null) return dict[key];
    const zhDict = (window.BOTW_I18N && window.BOTW_I18N.zh) || {};
    if (zhDict[key] !== undefined && zhDict[key] !== null) return zhDict[key];
    return fallback !== undefined ? fallback : key;
  }

  const ENEMY_EN = {
    '波克布林': 'Bokoblin', '蓝色波克布林': 'Blue Bokoblin', '黑色波克布林': 'Black Bokoblin',
    '白银波克布林': 'Silver Bokoblin', '黄金波克布林': 'Golden Bokoblin', '骷髅波克布林': 'Stalkoblin',
    '波克布林·守卫': 'Bokoblin (Guard)', '波克布林·守卫·树顶': 'Bokoblin (Treehouse Guard)',
    '波克布林·守卫·伏击': 'Bokoblin (Ambush Guard)', '蓝色波克布林·守卫': 'Blue Bokoblin (Guard)',
    '蓝色波克布林·守卫·树顶': 'Blue Bokoblin (Treehouse Guard)', '蓝色波克布林·守卫·伏击': 'Blue Bokoblin (Ambush Guard)',
    '莫力布林': 'Moblin', '蓝色莫力布林': 'Blue Moblin', '黑色莫力布林': 'Black Moblin',
    '白银莫力布林': 'Silver Moblin', '黄金莫力布林': 'Golden Moblin', '骷髅莫力布林': 'Stalmoblin',
    '黑色莫力布林·火山': 'Black Moblin (Volcano)',
    '蜥蜴战士': 'Lizalfos', '蓝色蜥蜴战士': 'Blue Lizalfos', '黑色蜥蜴战士': 'Black Lizalfos',
    '白银蜥蜴战士': 'Silver Lizalfos', '黄金蜥蜴战士': 'Golden Lizalfos', '骷髅蜥蜴战士': 'Stalizalfos',
    '喷火蜥蜴战士': 'Fire-Breath Lizalfos', '吐雹蜥蜴战士': 'Ice-Breath Lizalfos', '电麻蜥蜴战士': 'Electric Lizalfos',
    '蜥蜴战士·守卫': 'Lizalfos (Guard)', '蓝色蜥蜴战士·守卫': 'Blue Lizalfos (Guard)',
    '莱尼尔': 'Lynel', '蓝鬃莱尼尔': 'Blue-Maned Lynel', '白鬃莱尼尔': 'White-Maned Lynel',
    '白银莱尼尔': 'Silver Lynel', '黄金莱尼尔': 'Golden Lynel',
    '西诺克斯': 'Hinox', '蓝色西诺克斯': 'Blue Hinox', '黑色西诺克斯': 'Black Hinox',
    '骷髅西诺克斯': 'Stalnox', '骷髅西诺克斯·全天': 'Stalnox (All Day)',
    '岩石巨人': 'Stone Talus', '岩石巨人（稀有）': 'Stone Talus (Rare)', '岩石巨人（夜光）': 'Stone Talus (Luminous)',
    '冰岩巨人': 'Frost Talus', '熔岩巨人': 'Igneo Talus',
    '莫尔德拉吉克': 'Molduga', '王拉吉克': 'Molduking',
    '丘丘': 'Chuchu', '火丘丘': 'Fire Chuchu', '冰丘丘': 'Ice Chuchu', '电丘丘': 'Electric Chuchu',
    '火丘丘·蓝色': 'Fire Chuchu (Blue)', '火丘丘·黑色': 'Fire Chuchu (Black)',
    '冰丘丘·蓝色': 'Ice Chuchu (Blue)', '冰丘丘·黑色': 'Ice Chuchu (Black)',
    '电丘丘·蓝色': 'Electric Chuchu (Blue)', '电丘丘·黑色': 'Electric Chuchu (Black)',
    '丘丘·蓝色': 'Blue Chuchu', '丘丘·黑色': 'Black Chuchu',
    '蝙蝠': 'Keese', '火蝙蝠': 'Fire Keese', '冰蝙蝠': 'Ice Keese', '电蝙蝠': 'Electric Keese',
    '火蝙蝠·全天': 'Fire Keese (All Day)', '冰蝙蝠·全天': 'Ice Keese (All Day)', '电蝙蝠·全天': 'Electric Keese (All Day)', '蝙蝠·全天': 'Keese (All Day)',
    '岩石人': 'Stone Pebblit', '熔岩人': 'Igneo Pebblit', '冰岩人': 'Frost Pebblit',
    '水八爪怪': 'Water Octorok', '宝八爪怪': 'Treasure Octorok', '森八爪怪': 'Forest Octorok',
    '雪八爪怪': 'Snow Octorok', '岩八爪怪': 'Rock Octorok',
    '火焰长袍魔法师': 'Fire Wizzrobe', '陨石长袍魔法师': 'Meteo Wizzrobe',
    '冰雪长袍魔法师': 'Ice Wizzrobe', '冰冻长袍魔法师': 'Blizzrobe',
    '电击长袍魔法师': 'Electric Wizzrobe', '雷电长袍魔法师': 'Thunder Wizzrobe',
    '守护者（步行型）': 'Guardian Stalker', '守护者（飞行型）': 'Guardian Skywatcher',
    '守护者（炮台型）': 'Guardian Turret', '腐蚀守护者': 'Decayed Guardian',
    '小型守护者': 'Guardian Scout I', '小型守护者（初阶）': 'Guardian Scout II',
    '小型守护者（中阶）': 'Guardian Scout III', '小型守护者（终阶）': 'Guardian Scout IV',
    '依盖队 干部·据点': 'Yiga Blademaster (Hideout)', '依盖队 干部·据点·DLC': 'Yiga Blademaster (DLC)',
    '依盖队 成员·射手·据点': 'Yiga Footsoldier (Hideout)', '依盖队 成员·射手': 'Yiga Footsoldier',
    '可盖大人': 'Master Kohga', '费罗龙': 'Farosh', '奥尔龙': 'Dinraal', '聂尔龙': 'Naydra'
  };

  function nameOf(item) {
    if (!item) return '';
    if (curLang === 'en') {
      if (item.cat === 'seed' || (item.name && item.name.indexOf('克洛格') >= 0)) {
        var m = (item.name || '').match(/No\.\d+/i);
        return 'Korok Seed' + (m ? ' ' + m[0] : (item.id ? ' ' + item.id : ''));
      }
      if (item.en) return item.en;
      if (item.name && ENEMY_EN[item.name]) return ENEMY_EN[item.name];
      return item.hint_en || item.name_en || item.name || item.cn || '';
    }
    return item.name || item.cn || item.en || item.hint_en || '';
  }

  const REGION_EN = {
    '奥尔汀': 'Eldin',
    '奥尔汀地区': 'Eldin',
    '中央海拉鲁': 'Central Hyrule',
    '费罗尼': 'Faron',
    '费罗尼地区': 'Faron',
    '格鲁德': 'Gerudo',
    '格鲁德地区': 'Gerudo',
    '内卡鲁达': 'Necluda',
    '内卡鲁达地区': 'Necluda',
    '海布拉': 'Hebra',
    '海布拉地区': 'Hebra',
    '拉聂尔': 'Lanayru',
    '拉聂尔地区': 'Lanayru',
    '阿卡莱': 'Akkala',
    '阿卡莱地区': 'Akkala'
  };

  const TOWER_EN = {
    '阿卡莱之塔': 'Akkala Tower',
    '平原之塔': 'Central Tower',
    '双子山之塔': 'Dueling Peaks Tower',
    '奥尔汀之塔': 'Eldin Tower',
    '费罗尼之塔': 'Faron Tower',
    '格鲁德之塔': 'Gerudo Tower',
    '初始之塔': 'Great Plateau Tower',
    '哈特诺之塔': 'Hateno Tower',
    '海布拉之塔': 'Hebra Tower',
    '湖之塔': 'Lake Tower',
    '拉聂尔之塔': 'Lanayru Tower',
    '丘陵之塔': 'Ridgeland Tower',
    '达邦挞之塔': 'Tabantha Tower',
    '荒野之塔': 'Wasteland Tower',
    '森林之塔': 'Woodland Tower'
  };

  const ARMOR_SET_EN = {
    '海利亚套装': 'Hylian Set',
    '旷野之勇者套装': 'Of the Wild Set',
    '卓拉套装': 'Zora Set',
    '幻影套装': 'Phantom Set',
    '汀空套装': "Tingle's Set",
    '近卫兵套装': 'Royal Guard Set',
    '幻影盖侬套装': 'Phantom Ganon Set',
    '打捞员套装': 'Salvager Set'
  };

  const TRIAL_EN = {
    '力之试炼·低阶': 'A Minor Test of Strength',
    '力之试炼·中阶': 'A Modest Test of Strength',
    '力之试炼·高阶': 'A Major Test of Strength',
    '力量试炼': 'Test of Strength',
    '炸弹试炼': 'Bomb Trial',
    '时间静止试炼': 'Stasis Trial',
    '冰冻试炼': 'Cryonis Trial',
    '磁力试炼': 'Magnesis Trial'
  };

  function regionOf(str) {
    if (!str) return '';
    if (curLang === 'en') {
      if (REGION_EN[str]) return REGION_EN[str];
      if (window.BOTW_REGIONS) {
        const found = window.BOTW_REGIONS.find(r => r.n === str);
        if (found && found.en) return found.en;
      }
    }
    return str;
  }

  function towerOf(str) {
    if (!str) return '';
    if (curLang === 'en' && TOWER_EN[str]) return TOWER_EN[str];
    return str;
  }

  function armorSetOf(str) {
    if (!str) return '';
    if (curLang === 'en' && ARMOR_SET_EN[str]) return ARMOR_SET_EN[str];
    return str;
  }

  function trialOf(str, shrineEn) {
    if (!str) return '';
    if (curLang === 'en') {
      if (TRIAL_EN[str]) return TRIAL_EN[str];
      const baseName = (shrineEn || '').replace(/\s*Shrine$/i, '').trim();
      if (/的祝福$/.test(str)) {
        return baseName ? (baseName + "'s Blessing") : 'Blessing';
      }
      if (/机关$/.test(str)) {
        return baseName ? (baseName + ' Apparatus') : 'Apparatus';
      }
      if (/的教导$/.test(str)) {
        return baseName ? (baseName + "'s Teaching") : 'Teaching';
      }
      return 'Trial';
    }
    return str;
  }

  function monsterOf(str) {
    if (!str) return '';
    if (curLang === 'en') {
      const m = str.match(/^(.*?)( ×\d+)?$/);
      if (m) {
        const namePart = m[1].trim();
        const countPart = m[2] || '';
        if (ENEMY_EN[namePart]) {
          return ENEMY_EN[namePart] + countPart;
        }
      }
      if (ENEMY_EN[str]) return ENEMY_EN[str];
    }
    return str;
  }

  function descOf(item) {
    if (!item) return '';
    if (curLang === 'en') {
      return item.hint_en || item.desc_en || item.en || item.hint || item.hint_cn || item.desc || '';
    }
    return item.hint_cn || item.hint || item.desc || item.hint_en || '';
  }

  function updateDOM() {
    document.querySelectorAll('[data-i18n]').forEach(el => {
      const k = el.getAttribute('data-i18n');
      if (k) {
        const val = t(k, null);
        if (val !== null) el.textContent = val;
      }
    });
    document.querySelectorAll('[data-i18n-html]').forEach(el => {
      const k = el.getAttribute('data-i18n-html');
      if (k) {
        const val = t(k, null);
        if (val !== null) el.innerHTML = val;
      }
    });
    document.querySelectorAll('[data-i18n-title]').forEach(el => {
      const k = el.getAttribute('data-i18n-title');
      if (k) {
        const val = t(k, null);
        if (val !== null) el.setAttribute('title', val);
      }
    });
    document.querySelectorAll('[data-i18n-placeholder]').forEach(el => {
      const k = el.getAttribute('data-i18n-placeholder');
      if (k) {
        const val = t(k, null);
        if (val !== null) el.setAttribute('placeholder', val);
      }
    });
    document.querySelectorAll('[data-i18n-aria]').forEach(el => {
      const k = el.getAttribute('data-i18n-aria');
      if (k) {
        const val = t(k, null);
        if (val !== null) el.setAttribute('aria-label', val);
      }
    });
  }

  function onChange(fn) {
    if (typeof fn === 'function') listeners.push(fn);
  }

  function setLang(lang) {
    if (lang !== 'zh' && lang !== 'en') return;
    curLang = lang;
    try {
      localStorage.setItem(STORAGE_KEY, lang);
    } catch (e) {}
    if (document.documentElement) {
      document.documentElement.lang = curLang === 'en' ? 'en' : 'zh-CN';
    }
    updateDOM();
    listeners.forEach(fn => {
      try { fn(curLang); } catch (e) { console.error('i18n listener error:', e); }
    });
    if (window.UI && typeof window.UI.onLangChange === 'function') {
      window.UI.onLangChange(curLang);
    }
    if (window.__botwMap && typeof window.__botwMap.draw === 'function') {
      window.__botwMap.draw();
    }
  }

  init();

  return {
    getLang,
    setLang,
    t,
    nameOf,
    descOf,
    regionOf,
    towerOf,
    armorSetOf,
    trialOf,
    monsterOf,
    updateDOM,
    onChange
  };
})();

window.I18N = I18N;

