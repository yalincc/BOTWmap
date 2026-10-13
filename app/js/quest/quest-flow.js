/* ============================================================
 * 旷野之息互动地图 · 任务板块 · 流程连线（V1.6.2）
 * ------------------------------------------------------------
 * 玩法（对齐 TOTKmap task-flow，但引擎是 Canvas 非 Leaflet，故重写）：
 *   选中一个任务 → 把它「流程地点」按顺序连成虚线 + 序号节点；
 *   只在选中后显示，绝不全图铺满。
 *
 * 与 quest.js 的分工（零侵入，靠钩子通信）：
 *   - 线条画在「任务图标之前」（quest.js paint 开头调 paintLine）
 *   - 序号节点画在「任务图标之后」（quest.js paint 结尾调 paintNodes）
 *     这样节点不会被 28~36px 的图标盖住，线又不会盖住图标。
 *   - 想下线：删 index.html 里本模块的 script 标签即可。
 *
 * 不做的事（相对 TOTK 简化）：不跨图层分段（BOTW 只有地表一层）、
 *   不做飞行动画的「先拉远再放大」两段（单次 fit 足够）。
 * ============================================================ */
(function () {
  'use strict';

  var QUESTS = (typeof BOTW_QUESTS !== 'undefined' && BOTW_QUESTS.quests) ? BOTW_QUESTS.quests : [];
  /* 游戏世界坐标 -> 地图像素（与 build_quests_v2.py 同一口径） */
  function px(gx, gz) { return [gx * 2 + 12000, gz * 2 + 10000]; }

  var TYPE_COLOR = { take: '#7dd3a0', deliver: '#7dd3a0', battle: '#ff8a8a', npc: '#eac27e', other: '#9fc6ff' };
  var TYPE_CN = { take: '接取', deliver: '交付', battle: '战斗', npc: '关键 NPC', other: '其他' };

  var map = null, ready = false;
  var curKey = null;    /* 当前画着连线的任务 key */

  function questOf(k) {
    for (var i = 0; i < QUESTS.length; i++) if (QUESTS[i].key === k) return QUESTS[i];
    return null;
  }
  /* 流程点（像素坐标） */
  function pointsOf(q) {
    var out = [];
    (q && q.flowPts ? q.flowPts : []).forEach(function (f) {
      if (f.gx == null || f.gz == null) return;
      out.push({ p: px(f.gx, f.gz), type: f.type || 'other', note: f.note || '' });
    });
    return out;
  }

  /* ---------- 绘制：折线（画在任务图标之前） ---------- */
  function paintLine(ctx) {
    if (!ready || !curKey || !map) return;
    var q = questOf(curKey); if (!q) return;
    var pts = pointsOf(q); if (pts.length < 2) return;
    var s = map.view.scale, vx = map.view.x, vy = map.view.y;
    ctx.save();
    /* 深色描边打底 + 金色虚线，在任何底色上都看得清 */
    ctx.lineJoin = 'round';
    ctx.lineCap = 'round';
    ctx.beginPath();
    pts.forEach(function (it, i) {
      var x = vx + it.p[0] * s, y = vy + it.p[1] * s;
      if (i === 0) ctx.moveTo(x, y); else ctx.lineTo(x, y);
    });
    ctx.strokeStyle = 'rgba(10,14,18,.55)';
    ctx.lineWidth = 5;
    ctx.stroke();
    ctx.strokeStyle = '#eac27e';
    ctx.lineWidth = 2.5;
    ctx.setLineDash([9, 7]);
    ctx.stroke();
    ctx.setLineDash([]);
    ctx.restore();
  }

  /* ---------- 绘制：序号节点（画在任务图标之后，保证不被图标盖住） ---------- */
  function paintNodes(ctx) {
    if (!ready || !curKey || !map) return;
    var q = questOf(curKey); if (!q) return;
    var pts = pointsOf(q); if (!pts.length) return;
    var s = map.view.scale, vx = map.view.x, vy = map.view.y;
    var R = Math.max(9, Math.min(13, 11 * (0.6 + s)));   /* 略微随缩放，节点始终可读 */
    pts.forEach(function (it, i) {
      var x = vx + it.p[0] * s, y = vy + it.p[1] * s;
      if (x < -40 || y < -40 || x > map.w + 40 || y > map.h + 40) return;
      ctx.save();
      ctx.beginPath();
      ctx.arc(x, y, R, 0, Math.PI * 2);
      ctx.fillStyle = TYPE_COLOR[it.type] || '#eac27e';
      ctx.fill();
      ctx.lineWidth = 1.6;
      ctx.strokeStyle = 'rgba(16,20,24,.9)';
      ctx.stroke();
      ctx.fillStyle = '#14181c';
      ctx.font = '700 ' + Math.round(R * 1.15) + 'px "PingFang SC","Microsoft YaHei",sans-serif';
      ctx.textAlign = 'center';
      ctx.textBaseline = 'middle';
      ctx.fillText(String(i + 1), x, y + 1);
      ctx.restore();
    });
  }

  /* 可见地图区（扣掉左侧栏 + 打开的任务卡），避免流程点被 UI 盖住 */
  function availRect() {
    var L = 0, T = 0, R = map.w, B = map.h;
    var sb = document.getElementById('sidebar');
    if (sb) {
      var r = sb.getBoundingClientRect();
      if (r.width > 0 && r.left <= 4) L = Math.max(L, r.right);
    }
    var card = document.getElementById('questCard');
    if (card && !card.classList.contains('hidden')) {
      var cr = card.getBoundingClientRect();
      if (cr.top > map.h * 0.3 && cr.bottom > map.h * 0.72) B = Math.min(B, cr.top - 10);          /* 底部抽屉 */
      else if (cr.left > map.w * 0.5) R = Math.min(R, cr.left - 12);                              /* 卡片在右半 */
      else if (cr.right < map.w * 0.5) L = Math.max(L, cr.right + 12);                             /* 卡片在左半 */
    }
    if (R - L < 220) R = Math.min(map.w, L + 220);
    if (B - T < 170) B = Math.min(map.h, T + 170);
    return { L: L, T: T, R: R, B: B };
  }

  /* ---------- 视野自适应：一次 fit 把所有流程点框进「可见地图区」（之后不再动视野） ---------- */
  function frame(pts) {
    if (!map || !pts.length) return;
    var a = availRect();
    var xs = pts.map(function (it) { return it.p[0]; });
    var ys = pts.map(function (it) { return it.p[1]; });
    var minx = Math.min.apply(null, xs), maxx = Math.max.apply(null, xs);
    var miny = Math.min.apply(null, ys), maxy = Math.max.apply(null, ys);
    var PAD = 70;
    var availW = Math.max(120, (a.R - a.L) - PAD * 2);
    var availH = Math.max(120, (a.B - a.T) - PAD * 2);
    var spanX = Math.max(1, maxx - minx), spanY = Math.max(1, maxy - miny);
    var s = Math.min(availW / spanX, availH / spanY);
    /* 夹到引擎缩放范围；再夹一个「不过度放大」的上限，避免相邻两点怼到最大档 */
    s = Math.min(Math.max(s, map.minScale), map.maxScale);
    s = Math.min(s, Math.max(map.maxScale * 0.6, map.minScale * 8));
    map.view.scale = s;
    /* 让点集中心落在「可见地图区」中心，而不是整个画布中心 */
    var cx = (a.L + a.R) / 2, cy = (a.T + a.B) / 2;
    map.view.x = cx - (minx + maxx) / 2 * s;
    map.view.y = cy - (miny + maxy) / 2 * s;
    map.draw();
  }

  /* ---------- 对外 ---------- */
  function toast(msg) {
    if (typeof UI !== 'undefined' && UI.toast) UI.toast(msg);
  }
  function show(key, noFrame) {
    var q = questOf(key);
    if (!q) return false;
    var pts = pointsOf(q);
    if (pts.length < 2) {
      toast(pts.length === 1 ? '该任务只有 1 个流程点，无法连线' : '该任务没有流程点');
      return false;
    }
    curKey = key;
    if (!noFrame) frame(pts);
    else map.draw();
    return true;
  }
  function hide() {
    if (!curKey) return;
    curKey = null;
    if (map) map.draw();
  }
  function isOn(key) { return !!curKey && curKey === key; }
  function toggle(key) {
    if (isOn(key)) { hide(); toast('已关闭任务连线'); return false; }
    if (show(key)) toast('已显示任务连线（' + pointsOf(questOf(key)).length + ' 个流程点）');
    return true;
  }
  /* 当前连线任务被切走/数据变化时同步 */
  function refresh() { if (curKey && map) map.draw(); }

  window.QuestFlow = {
    paintLine: paintLine,
    paintNodes: paintNodes,
    show: show,
    hide: hide,
    toggle: toggle,
    isOn: isOn,
    refresh: refresh,
    typeCn: function (t) { return TYPE_CN[t] || t; }
  };

  /* ---------- 初始化 ---------- */
  function init() {
    map = window.__botwMap;
    if (!map) { setTimeout(init, 60); return; }
    ready = true;
  }
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', init);
  else init();
})();
