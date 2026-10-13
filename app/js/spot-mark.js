/* ============================================================
 * 旷野之息互动地图 · 红点脉冲（V1.6.2 / G4）
 * ------------------------------------------------------------
 * 点「坐标 / 流程地点 / 获取点」后，在地图对应位置留一个脉冲红点，
 * 让用户知道刚才定位的是哪一个点；点地图空白即消失。
 *
 * 与金色光圈（map.pulseAt → CardLayout.ring，DOM 一次性脉冲）分工：
 *   - 光圈 = 定位瞬间的「到了」反馈（1.6s 消退）
 *   - 红点 = 持续标记「就是这里」，直到点空白
 * 只走 Canvas 绘制（跟随拖图/缩放天然正确），不注册进 marker 表。
 * ============================================================ */
(function () {
  'use strict';

  var map = null;
  var spot = null;          /* 当前红点像素坐标 [px, py] */
  var t0 = 0, raf = 0;
  var DUR = 1900;           /* 脉冲动画时长（ms）；之后停在静态红点，随每次重绘保留 */

  function paint(ctx) {
    if (!spot || !map) return;
    var s = map.view.scale;
    var x = map.view.x + spot[0] * s, y = map.view.y + spot[1] * s;
    var el = Date.now() - t0;
    ctx.save();
    if (el < DUR) {
      var ph = Math.min(1, (el % 950) / 950);
      ctx.beginPath();
      ctx.arc(x, y, 7 + 20 * ph, 0, Math.PI * 2);
      ctx.strokeStyle = 'rgba(255,70,70,' + (0.9 * (1 - ph)).toFixed(3) + ')';
      ctx.lineWidth = 2.5;
      ctx.stroke();
    }
    ctx.beginPath();
    ctx.arc(x, y, 5.5, 0, Math.PI * 2);
    ctx.fillStyle = '#ff4646';
    ctx.fill();
    ctx.lineWidth = 1.6;
    ctx.strokeStyle = 'rgba(255,255,255,.92)';
    ctx.stroke();
    ctx.restore();
  }

  function step() {
    raf = 0;
    if (!spot || !map) return;
    map.draw();
    if (Date.now() - t0 < DUR) raf = requestAnimationFrame(step);
  }

  /* 设置红点（传像素坐标 [px,py]） */
  function set(p) {
    if (!map || !p) return;
    spot = [p[0], p[1]];
    t0 = Date.now();
    if (!raf) raf = requestAnimationFrame(step);
    else map.draw();
  }
  function clear() {
    if (!spot) return;
    spot = null;
    if (raf) { cancelAnimationFrame(raf); raf = 0; }
    if (map) map.draw();
  }

  window.SpotMark = { set: set, clear: clear, isOn: function () { return !!spot; } };

  /* 点地图空白 → 红点消失（与卡片关闭同一广播） */
  window.addEventListener('botw:blankclose', clear);

  function init() {
    map = window.__botwMap;
    if (!map) { setTimeout(init, 60); return; }
    map._extDraw.push(paint);
  }
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', init);
  else init();
})();
