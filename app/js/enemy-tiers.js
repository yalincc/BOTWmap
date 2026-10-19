/* ============================================================
 * BOTWmap v1.6.6 · 怪物层 tier 混装模块（§12 官方缩放接线）
 * ------------------------------------------------------------
 * 数据：data/enemy-tiers/（source/build_botwmap_tiers.py 生成，勿手改）
 *   BOTW_TIERS[0..3] = {categories, markers, camps:{hot,all}}
 *   BOTW_TIERS.quest / .hardmode = {categories, markers}
 *   BOTW_SCALING = {chains, killScores, tierNames, hardModeGold}
 * 职责：
 *   1. 敌人标记 per-chain 混装（tier 文件 index 对齐，同序同长）
 *   2. 营地层按 worldTier 混装（camp.js 的 CW 消费 window.BOTW_TIER_CAMPS）
 *   3. quest（DLC2 一击剑）/ hardmode（大师模式）开关叠加
 * 选档数据（window.__chainTier / __worldTier）由 save_upload.js 写入；
 * 无存档 = 全链 0 档 = tier-0 布点原貌（新档真实状态）。
 *
 * P2 阶段状态：**引而不接** —— 本模块只被 index.html 加载，apply() 不会被
 *   自动调用（P4 起由 save_upload 在存档就绪后显式调用），页面行为零变化。
 * 原则 D（异常可见）：apply() 失败一律 warn 出来，禁止 try-catch 吞异常。
 * 对外：window.EnemyTiers = { apply, setDlc, dlcState }
 * ============================================================ */
(function (global) {
  'use strict';

  var K_DLC = 'botwmap.dlc.v1';
  var chainArr = null;    // chainArr[tier][species] = [marker...]（同序子序列）
  var campArr = null;     // campArr[tier][species] = [camp...]（camps.all 同序子序列）

  function dlcState() {
    try { return JSON.parse(global.localStorage.getItem(K_DLC) || '{}'); } catch (e) { return {}; }
  }
  function saveDlc(st) {
    try { global.localStorage.setItem(K_DLC, JSON.stringify(st)); } catch (e) {}
  }

  /* 换源范围：小怪 10 类 + dragon（幽灵怪所在层，enemies.js 退役）；
     boss 5 类（lynel/hinox/talus/molduga/guardian）保留 data.js 旧标记——
     讨伐绿勾 liveIds / #N 编号 / enemy_save 92 条匹配全部依赖旧 id 稳定，不动 */
  var SMALL_CATS = {
    bokoblin: 1,
    moblin: 1,
    moriblin: 1,
    lizalfos: 1,
    keese: 1,
    chuchu: 1,
    octorok: 1,
    airoctarock: 1,
    wizzrobe: 1,
    yiga: 1,
    assassin: 1,
    pebblit: 1,
    dragon: 1
  };

  /* 预建索引：各 tier 的每条链实例子序列（index 对齐的关键） */
  function buildIndex() {
    chainArr = {}; campArr = {};
    for (var n = 0; n < 4; n++) {
      chainArr[n] = {}; campArr[n] = {};
      var T = global.BOTW_TIERS && global.BOTW_TIERS[n];
      if (!T) continue;
      T.markers.forEach(function (m) {
        if (!m.chain) return;
        (chainArr[n][m.chain] = chainArr[n][m.chain] || []).push(m);
      });
      (T.camps && T.camps.all || []).forEach(function (c) {
        if (!c.species) return;
        (campArr[n][c.species] = campArr[n][c.species] || []).push(c);
      });
    }
  }

  function getCampChain(c) {
    if (!c) return null;
    if (c.species) return c.species;
    var a = c.a || '';
    if (/^Enemy_Bokoblin/.test(a)) return 'BokoblinSeries';
    if (/^Enemy_Moriblin/.test(a)) return 'MoriblinSeries';
    if (/^Enemy_Lynel/.test(a)) return 'LynelSeries';
    if (/^Enemy_Lizalfos/.test(a)) return 'LizalfosSeries';
    if (/^Enemy_Assassin/.test(a)) return 'AssassinSeries';
    return null;
  }

  function getChainLevel(chainTier, sp) {
    if (!chainTier || !sp) return 0;
    if (chainTier[sp] != null) return chainTier[sp];
    var shortName = sp.replace(/Series$/, '');
    if (chainTier[shortName] != null) return chainTier[shortName];
    if (chainTier[shortName.toLowerCase()] != null) return chainTier[shortName.toLowerCase()];
    var longName = shortName + 'Series';
    if (chainTier[longName] != null) return chainTier[longName];
    return 0;
  }

  /* 合成敌人标记：小怪层 per-chain 混装（tier 文件 index 对齐）；boss 类不在此层 */
  function composeMarkers(chainTierArg, dlcStateArg) {
    if (!chainArr) buildIndex();
    var chainTier = chainTierArg || global.__chainTier || {};
    var st = dlcStateArg || dlcState();
    var out = [];
    var cursor = {};
    if (!global.BOTW_TIERS || !global.BOTW_TIERS[0] || !global.BOTW_TIERS[0].markers) {
      return out;
    }
    global.BOTW_TIERS[0].markers.forEach(function (m) {
      if (!SMALL_CATS[m.cat]) return;   /* boss 类实例跳过（保留旧标记） */
      if (m.chain) {
        var sp = m.chain;
        var t = getChainLevel(chainTier, sp);
        if (t > 3) t = 3;
        if (t < 0) t = 0;
        var arr = (chainArr[t] && chainArr[t][sp]) || [];
        var k = cursor[sp] || 0;
        cursor[sp] = k + 1;
        out.push(arr[k] || m);
      } else {
        out.push(m);
      }
    });
    var isQuest = !!(st && (st.quest || st.dlc2));
    var questData = global.BOTW_TIERS && (global.BOTW_TIERS.quest || global.BOTW_TIER_QUEST);
    if (isQuest && questData && questData.markers) {
      out = out.concat(questData.markers);
    }
    var isHardmode = !!(st && (st.hardmode || st.master));
    var hardmodeData = global.BOTW_TIERS && (global.BOTW_TIERS.hardmode || global.BOTW_TIER_HARDMODE);
    if (isHardmode && hardmodeData && hardmodeData.markers) {
      out = out.concat(hardmodeData.markers.filter(function (m) { return SMALL_CATS[m.cat]; }));
    }
    return out;
  }

  /* 合成营地层（P5）：按营地主链档位取对应 tier 同序条目 */
  function composeCamps(chainTierArg) {
    var chainTier = chainTierArg || global.__chainTier || {};
    var defTier = (typeof chainTier === 'number') ? chainTier : (chainTier.worldTier != null ? chainTier.worldTier : (global.__worldTier != null ? global.__worldTier : 0));
    var hot = [], all = [];
    if (!global.BOTW_TIERS || !global.BOTW_TIERS[0] || !global.BOTW_TIERS[0].camps) return { hot: hot, all: all };
    var t0All = global.BOTW_TIERS[0].camps.all || [];
    for (var i = 0; i < t0All.length; i++) {
      var c0 = t0All[i];
      var sp = getCampChain(c0);
      var t = sp ? getChainLevel(chainTier, sp) : defTier;
      if (t > 3) t = 3;
      if (t < 0) t = 0;
      var pick = (t > 0 && global.BOTW_TIERS[t] && global.BOTW_TIERS[t].camps && global.BOTW_TIERS[t].camps.all && global.BOTW_TIERS[t].camps.all[i]) ? global.BOTW_TIERS[t].camps.all[i] : c0;
      all.push(pick);
      if (pick.hot) hot.push(pick);
    }
    return { hot: hot, all: all };
  }

  /* 应用：swap BOTW_DATA.markers 的小怪层条目 + 重建营地层视图 + 重绘 */
  var _applying = false;
  function apply(chainTierArg, dlcStateArg) {
    if (_applying) return false;
    _applying = true;
    if (chainTierArg !== undefined && chainTierArg !== null) {
      global.__chainTier = chainTierArg;
    }
    if (!global.BOTW_TIERS || !global.BOTW_TIERS[0]) {
      console.warn('[BOTWmap] EnemyTiers.apply 跳过：tier 数据件未就绪');
      _applying = false;
      return false;
    }
    if (!chainArr) buildIndex();
    var map = global.__botwMap;
    if (!map || !global.BOTW_DATA) {
      console.warn('[BOTWmap] EnemyTiers.apply 跳过：地图或数据未就绪');
      _applying = false;
      return false;
    }

    var st = (dlcStateArg !== undefined && dlcStateArg !== null) ? dlcStateArg : dlcState();

    // 过滤旧小怪标记（先前注入的 _tm 标记与旧 enemies.js 的小怪标记），保留 Boss 5 类及所有其它非怪标记
    global.BOTW_DATA.markers = global.BOTW_DATA.markers.filter(function (mk) {
      return !mk._tm && !SMALL_CATS[mk.cat];
    });

    var markers = composeMarkers(global.__chainTier, st);
    global.BOTW_DATA.markers.push.apply(global.BOTW_DATA.markers, markers);

    // P5：营地层换源与武器联动
    var camps = composeCamps(global.__chainTier);
    global.BOTW_TIER_CAMPS = camps;
    if (global.CampLayer && global.CampLayer.reload) {
      global.CampLayer.reload(camps);
    }

    if (map._buildIndex) map._buildIndex();
    if (global.UI && global.UI.updateLayerList) global.UI.updateLayerList();
    map.draw();
    _applying = false;
    return true;
  }

  function setDlc(key, on) {
    var st = dlcState();
    st[key] = on ? 1 : 0;
    if (key === 'quest') st.dlc2 = on ? 1 : 0;
    if (key === 'dlc2') st.quest = on ? 1 : 0;
    saveDlc(st);
    apply(global.__chainTier, st);
    return st;
  }

  /* ============================================================
   * P3：世界档位推算算法与侧栏徽标
   * 官方缩放积分算法标准（§12 口径）：
   *   0 ~ 285 分   = Lv1 (tier 0)
   *   286 ~ 941 分 = Lv2 (tier 1)
   *   942 ~ 1555 分= Lv3 (tier 2)
   *   1556+ 分     = Lv4 (tier 3)
   * ============================================================ */
  function calcTier(defeatDict) {
    var points = 0;
    if (typeof defeatDict === 'number') {
      points = defeatDict;
    } else if (defeatDict && typeof defeatDict.points === 'number') {
      points = defeatDict.points;
    } else if (defeatDict && typeof defeatDict === 'object') {
      var KS = (global.BOTW_SCALING && global.BOTW_SCALING.killScores) || [];
      KS.forEach(function (k) {
        var cnt = defeatDict[k.flag] || defeatDict[k.flag.replace(/^Defeated_/, '').replace(/_Num$/, '')] || 0;
        points += (k.point || 0) * cnt;
      });
    }

    var tier = 0;
    var level = 1;
    if (points >= 1556) {
      tier = 3;
      level = 4;
    } else if (points >= 942) {
      tier = 2;
      level = 3;
    } else if (points >= 286) {
      tier = 1;
      level = 2;
    } else {
      tier = 0;
      level = 1;
    }

    var chainTier = {};
    var chains = (global.BOTW_SCALING && global.BOTW_SCALING.chains) || [];
    chains.forEach(function (c) {
      var t = 0;
      (c.actors || []).forEach(function (a, i) {
        if (points >= (a.threshold || 0)) t = i;
      });
      if (t > 3) t = 3; // cap 3 (普通模式上限 Dark，Gold 为大师模式)
      chainTier[c.species] = t;
    });

    var res = {
      points: points,
      tier: tier,         // 0..3 (内部 tier 索引)
      worldTier: tier,    // 0..3 (同 tier)
      level: level,       // 1..4 (展示用 Lv1..Lv4)
      label: 'Lv' + level,
      chainTier: chainTier,
      valueOf: function () { return level; },
      toString: function () { return 'Lv' + level; }
    };
    return res;
  }

  function getBadgeEl() {
    var el = (typeof document !== 'undefined') && (document.getElementById('worldTierBadge') || document.getElementById('tierBadge'));
    if (!el && typeof document !== 'undefined') {
      var parent = document.querySelector('.save-upload');
      if (parent) {
        el = document.createElement('div');
        el.id = 'worldTierBadge';
        el.className = 'su-tier-badge hidden';
        parent.insertBefore(el, parent.querySelector('.su-tip') || null);
      }
    }
    return el;
  }

  function updateBadge(info) {
    var badge = getBadgeEl();
    if (!badge) return;
    if (!info) {
      badge.classList.add('hidden');
      badge.textContent = '';
      return;
    }
    var lvl = (info.level != null) ? info.level : ((info.tier != null) ? (info.tier + 1) : 1);
    var pts = (info.points != null) ? info.points : 0;
    badge.textContent = '世界档位: Lv' + lvl + ' (' + pts + '分)';
    badge.title = '世界档位根据存档击杀积分推算（官方缩放算法）\nLv1: 0~285分\nLv2: 286~941分\nLv3: 942~1555分\nLv4: 1556+分';
    badge.classList.remove('hidden');
  }

  function resetBadge() {
    updateBadge(null);
  }

  if (typeof document !== 'undefined') {
    var initBadgeOnReady = function () {
      if (global.__killPoints != null || (global.__saveCounts && global.__saveCounts.points != null)) {
        var pts = (global.__killPoints != null) ? global.__killPoints : global.__saveCounts.points;
        updateBadge(calcTier(pts));
      }
    };
    if (document.readyState === 'loading') {
      document.addEventListener('DOMContentLoaded', initBadgeOnReady);
    } else {
      initBadgeOnReady();
    }
  }

  var EnemyTiers = {
    apply: apply,
    composeMarkers: composeMarkers,
    composeCamps: composeCamps,
    setDlc: setDlc,
    dlcState: dlcState,
    calcTier: calcTier,
    updateBadge: updateBadge,
    resetBadge: resetBadge
  };
  global.EnemyTiers = EnemyTiers;
  if (typeof module !== 'undefined' && module.exports) {
    module.exports = EnemyTiers;
  }
})(typeof window !== 'undefined' ? window : global);
