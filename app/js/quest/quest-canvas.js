/* ============================================================
 * 旷野之息互动地图 · 任务板块 · M3 · 画布绘制与命中
 * ------------------------------------------------------------
 * 任务点绘制（分类图标 + 完成态降透明 + 绿勾）、流程连线挂载、
 * 命中测试（供 map.js extHits）、定位飞行。
 * 对外：window.QuestCanvas
 *
 * ★ 与地图引擎隔离：本模块只往 map 的 _extDraw / extHits 注册回调，
 *   不改 map.js 任何业务逻辑。
 * ============================================================ */
(function (global) {
  'use strict';

  var D = global.QuestData;
  var ready = false;

  function M() { return global.__botwMap || null; }

  function draw() {
    var m = M();
    if (!m || !ready) return;
    m.draw();
  }

  /* V1.6.2：任务点画分类图标（随缩放自适应，最大 36px）；完成态降透明 + 右上角绿勾 */
  function paint(ctx) {
    var map = M();
    if (!map) return;
    if (global.QuestFlow) global.QuestFlow.paintLine(ctx);   /* 流程连线（画在任务图标之下） */
    global.__qPaints = (global.__qPaints || 0) + 1; /* 调试计数 */
    var scale = map.view.scale;
    var size = global.CardLayout.markerSize(scale);   /* V1.6.2：随缩放自适应，最大 36px */
    var on = {};
    D.CAT_ORDER.forEach(function (c) { if (D.qLayers[c]) on[c] = true; });
    /* 全部关闭时 on 为空，下方命中判断自然全 skip；不再早退，保证尾部 paintNodes 一定执行 */

    var pad = 60;
    var x0 = Math.max(0, (-pad - map.view.x) / scale), y0 = Math.max(0, (-pad - map.view.y) / scale);
    var x1 = Math.min(map.MW, (map.w + pad - map.view.x) / scale), y1 = Math.min(map.MH, (map.h + pad - map.view.y) / scale);

    for (var i = 0; i < D.QUESTS.length; i++) {
      var q = D.QUESTS[i];
      var ql = D.qLayerOf(q);
      if (!on[ql] || !q.pts || !q.pts.length) continue;
      var done = global.QuestDone.isDone(q.key);
      var img = D.ICONS[ql];
      var hasImg = img && img.complete && img.naturalWidth;
      for (var j = 0; j < q.pts.length; j++) {
        var px = q.pts[j];
        if (px[0] < x0 || px[0] > x1 || px[1] < y0 || px[1] > y1) continue;
        var sx = map.view.x + px[0] * scale, sy = map.view.y + px[1] * scale;
        ctx.save();
        ctx.globalAlpha = done ? 0.65 : 1;
        if (hasImg) {
          ctx.drawImage(img, sx - size / 2, sy - size / 2, size, size);
        } else {
          /* 图标未加载兜底：分类色圆点 */
          ctx.beginPath();
          ctx.arc(sx, sy, size / 2 - 3, 0, Math.PI * 2);
          ctx.fillStyle = D.CAT_COLOR[ql] || '#888';
          ctx.fill();
        }
        if (done) global.CardLayout.doneCheck(ctx, sx + size / 2 - 5, sy - size / 2 + 5, 7);
        ctx.restore();
      }
    }
    if (global.QuestFlow) global.QuestFlow.paintNodes(ctx);   /* 流程连线序号节点（画在图标之上） */
  }

  /* 命中：任务点（屏幕 12px 容差），返回 {type:'quest', quest} */
  function hit(mx, my) {
    var map = M();
    if (!map) return null;
    var tol = (global.CardLayout.markerSize(map.view.scale) / 2 + 4) / map.view.scale;
    var best = null, bd = 1e18;
    for (var i = 0; i < D.QUESTS.length; i++) {
      var q = D.QUESTS[i];
      var ql = D.qLayerOf(q);
      if (!D.qLayers[ql] || !q.pts || !q.pts.length) continue;
      for (var j = 0; j < q.pts.length; j++) {
        var p = q.pts[j];
        var dx = p[0] - mx, dy = p[1] - my;
        if (Math.abs(dx) <= tol && Math.abs(dy) <= tol) {
          var d = dx * dx + dy * dy;
          if (d < bd) { bd = d; best = q; }
        }
      }
    }
    return best ? { type: 'quest', quest: best, _r: best, _p: null } : null;
  }

  /* 定位：飞到任务点并选中 */
  function locate(q) {
    var map = M();
    if (!map || !q || !q.pts || !q.pts.length) return;
    var p = q.pts[0];
    map.view.scale = Math.min(Math.max(map.view.scale, 1.2), map.maxScale);
    map.view.x = map.w / 2 - p[0] * map.view.scale;
    /* V1.6.1：手机端底部抽屉（55vh）会盖住下半屏，定位时把目标点推到上方 35% 处 */
    var vy = (global.CardLayout && global.CardLayout.isMobile()) ? map.h * 0.35 : map.h / 2;
    map.view.y = vy - p[1] * map.view.scale;
    map.draw();
    if (map.pulseAt) map.pulseAt(p[0], p[1]);   /* V1.6.1 调整g：定位脉冲光圈 */
    /* V1.6.1 调整i：跳转后按新目标点重算避让（清除手动态，避免卡片挡住目标图标） */
    var card = document.getElementById('questCard');
    if (card && !card.classList.contains('hidden') && map.m2s) {
      card.dataset.manual = '';
      var sp = map.m2s(p);
      map.layoutCard(card, sp[0], sp[1]);
    }
  }

  /* 注册绘制 / 命中 / 悬停回调到地图引擎（由 quest.js 装配时调用） */
  function register() {
    var map = M();
    if (!map) return false;
    map._extDraw.push(paint);
    map.extHits.push(hit);
    ready = true;
    return true;
  }

  global.QuestCanvas = {
    draw: draw,
    paint: paint,
    hit: hit,
    locate: locate,
    register: register,
    isReady: function () { return ready; }
  };
})(window);
