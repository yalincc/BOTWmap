/* ============================================================
 * 旷野之息互动地图 · 任务板块 · M3 · 数据层
 * ------------------------------------------------------------
 * 任务数据、分类元信息（名称/颜色/图标）、图层勾选状态（localStorage）。
 * 对外：window.QuestData
 *
 * ★ 与地图引擎隔离：本模块不触碰 map.js，只提供数据与状态；
 *   需要重绘时通过 window.QuestCanvas.draw() 间接调用（弱耦合）。
 * ============================================================ */
(function (global) {
  'use strict';

  /* ---------- 数据 ---------- */
  var QUESTS = (typeof BOTW_QUESTS !== 'undefined' && BOTW_QUESTS.quests) ? BOTW_QUESTS.quests : [];
  var CAT_NAME = { main: '主线挑战', side: '支线任务', shrine: '神庙挑战', dlc: 'DLC 任务' };
  /* V1.6.1：dlc 档用暗紫（与主线金/支线绿/神庙蓝区分） */
  var CAT_COLOR = { main: '#eac27e', side: '#7ec8a9', shrine: '#6fb3e0', dlc: '#c58fe0' };
  var CAT_ORDER = ['main', 'side', 'shrine', 'dlc'];
  /* V1.6.2：任务分类图标（用户自制卡通图标，地图 + 侧栏统一用） */
  var CAT_ICON = {
    main: 'assets/icons/quest_main.png',
    side: 'assets/icons/quest_side.png',
    shrine: 'assets/icons/quest_shrine.png',
    dlc: 'assets/icons/quest_dlc.png'
  };
  var ICONS = {};
  CAT_ORDER.forEach(function (c) {
    var im = new Image();
    im.src = CAT_ICON[c];
    ICONS[c] = im;
  });

  /* 勾选状态：{main, side, shrine, dlc} 默认全开（localStorage 记忆） */
  var K_LAYERS = 'botwmap.questLayers.v1';
  var qLayers = { main: true, side: true, shrine: true, dlc: true };

  /* 行归属规则（V1.6.1）：
     - 本体三档行 = cat 相同 且 dlc ∉ {'1','2'}（DLC 任务拆到第 4 档；联动 side_148 留支线行）；
     - DLC 行 = 任意 dlc 非空（'1' / '2' / 'collabo'，共 19 项）。
     计数直接在数据上算，不写死数字。 */
  function inRow(cat, q) {
    if (cat === 'dlc') return !!q.dlc;
    return q.cat === cat && q.dlc !== '1' && q.dlc !== '2';
  }
  /* DLC 卡片徽章分组：dlc='1'→DLC1、'2'→DLC2、'collabo'→联动 */
  function dlcBadgeOf(q) {
    if (q.dlc === '1') return 'DLC1';
    if (q.dlc === '2') return 'DLC2';
    if (q.dlc === 'collabo') return '联动';
    return '';
  }
  /* 绘制/命中归属：dlc∈{'1','2'} 的任务只在 DLC 层绘制，本体行不再画它们 */
  function qLayerOf(q) {
    return (q.dlc === '1' || q.dlc === '2') ? 'dlc' : q.cat;
  }

  /* ---------- 图层开关 ---------- */
  function persistLayers() {
    try { localStorage.setItem(K_LAYERS, JSON.stringify(qLayers)); } catch (e) {}
  }
  function redraw() {
    if (global.QuestCanvas) global.QuestCanvas.draw();
  }
  function isOn(cat) { return !!qLayers[cat]; }
  function toggleLayer(cat) {
    qLayers[cat] = !qLayers[cat];
    persistLayers();
    redraw();
  }
  function isAllOn() {
    return CAT_ORDER.every(function (c) { return qLayers[c]; });
  }
  /* 一键全开 / 全关（on=true 全开） */
  function setAll(on) {
    CAT_ORDER.forEach(function (c) { qLayers[c] = on; });
    persistLayers();
    redraw();
  }
  /* V1.6.1 修复：questLayers.v1 旧版只存不读。启动时恢复，逐键合并并补默认
     （旧存档缺 dlc → 补 true；值非布尔回退默认；异常捕获） */
  function restoreLayers() {
    try {
      var saved = JSON.parse(localStorage.getItem(K_LAYERS) || 'null');
      if (saved && typeof saved === 'object') {
        Object.keys(qLayers).forEach(function (k) {
          qLayers[k] = (typeof saved[k] === 'boolean') ? saved[k] : qLayers[k];
        });
      }
    } catch (e) {}
  }

  global.QuestData = {
    QUESTS: QUESTS,
    CAT_NAME: CAT_NAME,
    CAT_COLOR: CAT_COLOR,
    CAT_ORDER: CAT_ORDER,
    CAT_ICON: CAT_ICON,
    ICONS: ICONS,
    K_LAYERS: K_LAYERS,
    qLayers: qLayers,
    inRow: inRow,
    dlcBadgeOf: dlcBadgeOf,
    qLayerOf: qLayerOf,
    isOn: isOn,
    toggleLayer: toggleLayer,
    isAllOn: isAllOn,
    setAll: setAll,
    restoreLayers: restoreLayers
  };
})(window);
