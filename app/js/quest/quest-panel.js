/* ============================================================
 * 旷野之息互动地图 · 任务板块 · M3 · 侧栏面板
 * ------------------------------------------------------------
 * 往 #layerList 顶部注入「任务」组（4 分类行 + 一键全开/全关）：
 *   主线挑战 / 支线任务 / 神庙挑战 / DLC 任务
 * 完成态变化时刷新行计数；提供任务搜索索引给 ui.js。
 * 对外：window.QuestPanelUI
 * ============================================================ */
(function (global) {
  'use strict';

  var D = global.QuestData;
  if (!D) { console.warn('[任务板块] QuestData 未加载，面板不初始化'); return; }

  function injectPanel() {
    var wrap = document.getElementById('layerList');
    if (!wrap) return;
    /* 旧任务组（mainquest/shrinequest/sidequest/objective，buildLayerList 静态渲染）
       已被 quests.js 官方三档任务替代：先移除旧组再注入新组，避免双任务组并存 */
    var old = wrap.querySelector('.lg-group[data-group="任务"]');
    if (old) old.remove();

    var grp = document.createElement('div');
    grp.className = 'lg-group';
    grp.dataset.group = '任务';
    var g = document.createElement('div');
    g.className = 'lg';
    var gLabel = document.createElement('span');
    gLabel.textContent = '任务';
    var gBtn = document.createElement('button');
    gBtn.type = 'button';
    gBtn.className = 'lg-btn';
    gBtn.dataset.group = '任务';
    gBtn.title = '一键勾选 / 取消本组全部任务分类';
    g.appendChild(gLabel); g.appendChild(gBtn);
    grp.appendChild(g);
    var lrows = document.createElement('div');
    lrows.className = 'lrows';
    grp.appendChild(lrows);

    D.CAT_ORDER.forEach(function (cat) {
      var total = 0, done = 0;
      D.QUESTS.forEach(function (q) { if (D.inRow(cat, q)) { total++; if (global.QuestDone.isDone(q.key)) done++; } });
      var row = document.createElement('div');
      row.className = 'lrow' + (D.qLayers[cat] ? ' on' : '');
      row.dataset.qcat = cat;
      row.innerHTML =
        '<img class="lg-ic" src="' + D.CAT_ICON[cat] + '" alt="" loading="lazy">' +
        '<span class="lname">' + D.CAT_NAME[cat] + '</span>' +
        '<span class="lprog" id="qprog_' + cat + '">' + done + '/' + total + '</span>';
      row.title = '点击整行切换显示 / 隐藏';
      if (cat === 'dlc') {
        var g1 = 0, g2 = 0, gc = 0;
        D.QUESTS.forEach(function (q) { if (q.dlc === '1') g1++; else if (q.dlc === '2') g2++; else if (q.dlc) gc++; });
        row.title = 'DLC1 ' + g1 + ' 项 · DLC2 ' + g2 + ' 项 · 联动 ' + gc + ' 项（联动任务无地图点，宝箱见「探索收集」组）';
      }
      row.addEventListener('click', function () {
        D.toggleLayer(cat);
        row.classList.toggle('on', D.qLayers[cat]);
      });
      lrows.appendChild(row);
    });
    /* 插入到 #layerList 顶部（#layerList 是 flex:1 + overflow-y:auto 滚动容器，
       尾部 append 会被滚动区裁掉不可见；置顶保证打开侧栏即可看到任务组） */
    wrap.insertBefore(grp, wrap.firstChild);

    gBtn.addEventListener('click', function () {
      var allOn = D.isAllOn();
      D.setAll(!allOn);
      grp.querySelectorAll('.lrow[data-qcat]').forEach(function (r) { r.classList.toggle('on', !allOn); });
    });

    /* 完成态变化时刷新 4 行计数（卡片标记 / 载入存档后调用） */
    global._questRefreshRows = function () {
      D.CAT_ORDER.forEach(function (cat) {
        var el = document.getElementById('qprog_' + cat);
        if (!el) return;
        var total = 0, done = 0;
        D.QUESTS.forEach(function (q) { if (D.inRow(cat, q)) { total++; if (global.QuestDone.isDone(q.key)) done++; } });
        el.textContent = done + '/' + total;
      });
    };
  }

  /* 搜索索引（供 ui.js doSearch 调用） */
  function buildSearch() {
    global.__questSearch = D.QUESTS.map(function (q) {
      var seq = (q.no != null) ? q.no : (q.key || '');
      var searchName = (seq ? '#' + seq + ' ' : '') + q.name;
      return { q: q, name: q.name, seq: seq, searchName: searchName, en: q.en, catCn: '任务·' + (D.CAT_NAME[q.cat] || q.cat), pts: q.pts };
    });
  }

  global.QuestPanelUI = {
    injectPanel: injectPanel,
    buildSearch: buildSearch,
    refreshRows: function () { if (global._questRefreshRows) global._questRefreshRows(); }
  };
})(window);
