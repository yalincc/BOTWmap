/* ============================================================
 * 旷野之息互动地图 · 任务板块（V1.6.2 · M3 模块化装配层）
 * ------------------------------------------------------------
 * 本文件只做「装配 + 对外门面」，具体实现按职责拆到 js/quest/ 下：
 *   quest-data.js   数据 / 分类元信息 / 图标 / 图层勾选
 *   quest-done.js   完成态（存档权威 + 手动兜底 双源）
 *   quest-canvas.js 绘制 / 命中 / 定位（注册到 map 引擎钩子）
 *   quest-flow.js   流程连线 + 序号节点
 *   quest-panel.js  侧栏「任务」组注入 / 计数刷新 / 搜索索引
 *   quest-card.js   任务卡片渲染与交互
 *
 * 将「任务」并入探索面板（不加侧栏 Tab）：任务组 4 分类 =
 *   主线挑战 / 支线任务 / 神庙挑战 / DLC 任务（V1.6.1 新增第 4 档）。
 *
 * ★ 与地图引擎隔离：不侵入 map.js 业务逻辑，只注册绘制/命中/悬停回调。
 * ★ 想彻底下线任务板块：删 index.html 里本文件 + js/quest/* 的 script 标签。
 * ============================================================ */
(function () {
  'use strict';

  var map = null;
  var ready = false;

  function init() {
    map = window.__botwMap || null;
    if (!map) { setTimeout(init, 300); return; }
    if (ready) return;

    var D = window.QuestData, QD = window.QuestDone, CV = window.QuestCanvas,
        QP = window.QuestPanelUI, QC = window.QuestCard;
    if (!D || !QD || !CV || !QP || !QC) {
      console.warn('[任务板块] 子模块未就绪，跳过初始化');
      return;
    }

    QD.init();            /* 载入 localStorage 手动完成标记 */
    D.restoreLayers();    /* V1.6.1：恢复图层勾选（旧版只存不读） */

    /* 旧任务标点（mainquest/shrinequest/sidequest/objective）已被 quests.js 替代：
       从 data.markers 移除，避免与新任务点双重显示 */
    var oldTask = { mainquest: 1, shrinequest: 1, sidequest: 1, objective: 1 };
    var removed = 0;
    map.data.markers = map.data.markers.filter(function (mk) {
      if (oldTask[mk.cat]) { removed++; return false; }
      return true;
    });
    if (removed) map._buildIndex();

    /* 注册绘制与命中（QuestCanvas），悬停预览（QuestCard） */
    CV.register();
    if (map.extHoverHooks) map.extHoverHooks.push(QC.onHoverExt);  /* V1.6.1 调整5：hover 预览 */
    var qc = document.getElementById('questCard');
    if (window.initCardDrag && qc) window.initCardDrag(qc);        /* V1.6.1 调整i：卡片拖动 */

    /* 任务组面板注入 + 搜索索引 */
    QP.injectPanel();
    QP.buildSearch();
    if (window._rebuildSearch) window._rebuildSearch();

    ready = true;
    /* 初始重绘：注册完成后立即画出任务点（否则地图停留在注册前的旧画面） */
    map.draw();
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', init);
  } else {
    init();
  }

  /* ---------- 导出（保持 V1.6.1 起的对外 API 不变） ---------- */
  window.QuestPanel = {
    QuestDone: window.QuestDone,
    openCard: function (q) { window.QuestCard.openCard(q); },
    closeCard: function () { window.QuestCard.closeCard(); },
    locate: function (q) { window.QuestCanvas.locate(q); },
    isOn: function (cat) { return window.QuestData.isOn(cat); },
    toggle: function (cat) {
      window.QuestData.toggleLayer(cat);
      var row = document.querySelector('#layerList .lrow[data-qcat="' + cat + '"]');
      if (row) row.classList.toggle('on', window.QuestData.qLayers[cat]);
    }
  };
})();
