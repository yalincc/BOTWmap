/* V1.6.1 卡片布局统一内核
 * 方案文档：BOTWmap-V1.6.1-卡片避让统一内核方案-进行中.md
 *
 * 解决的问题（原 map.js layoutCard / ui.js positionCard 两套重复实现）：
 *   R1 坐标系混用：视口坐标直接赋给 position:absolute 卡片（定位原点 #mapArea）→ 恒定下移 56px
 *   R2 夹取破坏避让：右侧→左侧→clamp 三步走，最后一步把卡片拉回压住目标，事后无校验
 *   R3 只避让单卡：四张卡共存时其他卡照样压目标
 *   R4 光圈在 canvas 层：被 DOM 卡片盖死
 *   R6 手机端一刀切抽屉，目标点落下半屏必被遮
 *
 * 用法：所有坐标一律传【视口坐标】，内核负责归一到卡片定位坐标系。
 */
(function () {
  'use strict';

  var PAD = 52;      /* 目标点安全半径 = 图标 + 光圈 + 余量 */
  var EDGE = 10;     /* 容器边距 */
  var GAP = 8;       /* 卡片与卡片之间的间隙 */
  var MIN_W = 240;   /* 卡片可被压缩到的最小宽度 */
  var MOBILE = 760;  /* 与 CSS @media 断点保持一致 */
  /* 多卡协同优先级：先挪的先占好位置，探索卡最大最容易挡视线 */
  var PRIORITY = ['infoCard', 'questCard', 'chestCard', 'equipCard'];

  function hostEl() { return document.getElementById('mapArea'); }
  function isMobile() { return window.innerWidth <= MOBILE; }
  function isVisible(card) { return !!card && !card.classList.contains('hidden'); }

  /* 侧栏展开时尽量不压住侧栏（侧栏宽 300 + 12 余量）；放不下时可放宽 */
  function guardLeft() {
    var sb = document.getElementById('sidebar');
    if (!sb) return EDGE + 2;
    return (!sb.classList.contains('collapsed')) ? 312 : EDGE + 2;
  }

  function rectOf(el) {
    var r = el.getBoundingClientRect();
    return { left: r.left, top: r.top, right: r.right, bottom: r.bottom, width: r.width, height: r.height };
  }
  function inflate(r, p) {
    return { left: r.left - p, top: r.top - p, right: r.right + p, bottom: r.bottom + p };
  }
  /* 矩形 vs 目标点安全区（相切算通过：间隙正好等于 PAD 时不算遮挡） */
  function hitsTarget(r, tx, ty) {
    return !(r.right <= tx - PAD || r.left >= tx + PAD || r.bottom <= ty - PAD || r.top >= ty + PAD);
  }
  function hitsRect(a, b) {
    return !(a.right <= b.left || a.left >= b.right || a.bottom <= b.top || a.top >= b.bottom);
  }
  function blockedBy(r, obstacles) {
    for (var i = 0; i < obstacles.length; i++) { if (hitsRect(r, inflate(obstacles[i], GAP))) return true; }
    return false;
  }

  /* 一次完整求解：给定卡片尺寸与左边界约束，找出不压目标、不压其他卡的位置。
     返回 {x,y,r}（x/y 为容器相对坐标，r 为视口矩形）或 null。 */
  function solve(hr, relX, relY, cw, ch, tx, ty, obstacles, guard) {
    var minX = guard, maxX = Math.max(guard, hr.width - cw - EDGE);
    var minY = EDGE, maxY = Math.max(EDGE, hr.height - ch - EDGE);
    var clampX = function (x) { return Math.max(minX, Math.min(x, maxX)); };
    var clampY = function (y) { return Math.max(minY, Math.min(y, maxY)); };
    var mk = function (x, y) {
      return { x: x, y: y, r: { left: hr.left + x, top: hr.top + y, right: hr.left + x + cw, bottom: hr.top + y + ch } };
    };

    /* 候选位（容器相对）：右 → 左 → 下 → 上 → 四个对角 */
    var cands = [
      [relX + PAD, relY - ch / 2], [relX - PAD - cw, relY - ch / 2],
      [relX - cw / 2, relY + PAD], [relX - cw / 2, relY - PAD - ch],
      [relX + PAD, relY + PAD], [relX + PAD, relY - PAD - ch],
      [relX - PAD - cw, relY + PAD], [relX - PAD - cw, relY - PAD - ch]
    ];
    var i, p, r;
    for (i = 0; i < cands.length; i++) {
      p = mk(clampX(cands[i][0]), clampY(cands[i][1]));
      /* 夹取之后才做重叠校验（原实现夹取完就收工，夹取会把卡片拉回压住目标） */
      if (hitsTarget(p.r, tx, ty)) continue;
      if (blockedBy(p.r, obstacles)) continue;
      return p;
    }

    /* 网格扫描：按「离目标由近及远」取第一个合法位（应对多卡挤压 + 边缘锚点） */
    var step = 30, grid = [];
    for (var gy = minY; gy <= maxY + 1; gy += step) {
      for (var gx = minX; gx <= maxX + 1; gx += step) {
        var ccx = hr.left + gx + cw / 2, ccy = hr.top + gy + ch / 2;
        grid.push({ x: gx, y: gy, d: (ccx - tx) * (ccx - tx) + (ccy - ty) * (ccy - ty) });
      }
    }
    grid.sort(function (a, b) { return a.d - b.d; });
    for (i = 0; i < grid.length; i++) {
      r = mk(grid[i].x, grid[i].y).r;
      if (hitsTarget(r, tx, ty)) continue;
      if (blockedBy(r, obstacles)) continue;
      return mk(grid[i].x, grid[i].y);
    }
    return null;
  }

  /* 单卡避让。
     card : 卡片 DOM（position:absolute，定位原点 #mapArea）
     tx,ty: 目标点【视口坐标】
     obstacles: 已定位卡片的视口矩形数组
     返回：卡片最终视口矩形（供后续卡避让） */
  function place(card, tx, ty, obstacles) {
    if (!card) return null;
    obstacles = obstacles || [];
    /* 手机端：底部抽屉布局交给 CSS，清空所有内联定位让 CSS 生效 */
    if (isMobile()) {
      card.style.left = ''; card.style.top = '';
      card.style.right = ''; card.style.bottom = '';
      card.style.maxHeight = ''; card.style.overflowY = '';
      return rectOf(card);
    }
    var host = hostEl();
    if (!host) return null;
    var hr = host.getBoundingClientRect();
    if (hr.height < 40) return rectOf(card);

    /* 先钉住高度上限再测量，保证 cw/ch 就是最终尺寸 */
    card.style.maxHeight = (hr.height - 20) + 'px';
    card.style.overflowY = 'auto';
    /* ★ R1 修复：视口坐标 → 容器相对坐标（原实现漏了这一步，导致恒定下移一个顶栏高度） */
    var relX = tx - hr.left, relY = ty - hr.top;

    card.style.width = '';
    var cw = card.offsetWidth || 300, ch = card.offsetHeight || 320;

    var best = solve(hr, relX, relY, cw, ch, tx, ty, obstacles, guardLeft());

    /* 降级一：放宽侧栏避让（卡片 z-index 高于侧栏，压侧栏优于压目标点） */
    if (!best) best = solve(hr, relX, relY, cw, ch, tx, ty, obstacles, EDGE + 2);

    /* 降级二：压缩卡片宽度（窄窗口横向物理放不下时的唯一出路） */
    if (!best) {
      var leftRoom = (tx - hr.left) - PAD - EDGE;
      var rightRoom = (hr.left + hr.width - EDGE) - (tx + PAD);
      var maxW = Math.max(leftRoom, rightRoom);
      if (cw > maxW && maxW >= MIN_W) {
        card.style.width = Math.floor(maxW) + 'px';
        cw = card.offsetWidth || cw; ch = card.offsetHeight || ch;
        best = solve(hr, relX, relY, cw, ch, tx, ty, obstacles, EDGE + 2);
        if (!best) card.style.width = '';
      }
    }

    /* 兜底：实在无处可放 → 压着也要压得最少（卡片中心离目标最远的夹取位）
       ★ 先恢复原始宽度再算，否则会按压缩后的 cw 定位、按原始宽渲染 → 右边缘越界 */
    if (!best) {
      card.style.width = '';
      cw = card.offsetWidth || cw; ch = card.offsetHeight || ch;
      var minX = EDGE + 2, maxX = Math.max(minX, hr.width - cw - EDGE);
      var minY = EDGE, maxY = Math.max(EDGE, hr.height - ch - EDGE);
      var opts = [[minX, minY], [maxX, minY], [minX, maxY], [maxX, maxY]];
      var bestD = -1;
      for (var k = 0; k < opts.length; k++) {
        var cx = hr.left + opts[k][0] + cw / 2, cy = hr.top + opts[k][1] + ch / 2;
        var d = (cx - tx) * (cx - tx) + (cy - ty) * (cy - ty);
        if (d > bestD) { bestD = d; best = { x: opts[k][0], y: opts[k][1], r: { left: hr.left + opts[k][0], top: hr.top + opts[k][1], right: hr.left + opts[k][0] + cw, bottom: hr.top + opts[k][1] + ch } }; }
      }
    }

    if (best) {
      card.style.left = best.x + 'px';
      card.style.top = best.y + 'px';
      card.style.right = 'auto';
      card.style.bottom = 'auto';
    }
    return best ? best.r : rectOf(card);
  }

  /* 多卡协同避让：所有可见卡依次挪开，先放的优先占好位，后放的避开目标点 + 已定位卡片。
     opts.primary = 卡片 id：本次定位的主卡排到最前（例如装备定位时先挪 equipCard）。 */
  function placeAll(tx, ty, opts) {
    opts = opts || {};
    var list = [], i, id, c;
    /* 用户手动拖过的卡不参与重排（manual 态），除非它就是本次定位的主卡 */
    var movable = function (el, cid) { return isVisible(el) && (el.dataset.manual !== '1' || cid === opts.primary); };
    if (opts.primary) {
      c = document.getElementById(opts.primary);
      if (movable(c, opts.primary)) list.push(c);
    }
    for (i = 0; i < PRIORITY.length; i++) {
      id = PRIORITY[i];
      if (id === opts.primary) continue;
      c = document.getElementById(id);
      if (movable(c, id)) list.push(c);
    }
    var placed = [];
    for (i = 0; i < list.length; i++) {
      var r = place(list[i], tx, ty, placed);
      if (r) placed.push(r);
    }
    return placed;
  }

  /* ---------- 当前定位锚点 ----------
     ★ 修复「点卡片内坐标后又被搬回遮挡位置」：
     卡片内点坐标的链路是 locate() → layoutCard(card, 新目标) → 外层 handler 又调 positionCard()。
     后者原本拿 map._lastClick（用户上次点地图的位置，与本次定位无关）当锚点重算，
     于是把刚算好的正确避让结果覆盖掉 —— 表现为「拖走也没用，点坐标又回到同一处遮挡」。
     现在 layoutCard 顺手记下本次锚点，positionCard 优先复用它，两边口径一致。 */
  var _anchor = null;
  function setAnchor(x, y) {
    _anchor = (x == null || y == null) ? null : { sx: x, sy: y };
  }
  function getAnchor() { return _anchor; }
  function clearAnchor() { _anchor = null; }

  /* ---------- 定位光圈（DOM 层，压过所有卡片） ---------- */
  var _ringEl = null, _ringT1 = 0, _ringT2 = 0;

  function clearRing() {
    if (_ringT1) { clearTimeout(_ringT1); _ringT1 = 0; }
    if (_ringT2) { clearTimeout(_ringT2); _ringT2 = 0; }
    if (_ringEl && _ringEl.parentNode) _ringEl.parentNode.removeChild(_ringEl);
    _ringEl = null;
  }

  /* tx,ty = 目标点【视口坐标】 */
  function ring(tx, ty, size) {
    var host = hostEl();
    if (!host) return;
    clearRing();
    var hr = host.getBoundingClientRect();
    var s = size || 46;
    var el = document.createElement('div');
    el.className = 'mk-locate-ring';
    el.style.width = s + 'px';
    el.style.height = s + 'px';
    /* ★ 必须在 append 之前算好 left/top：append 后元素进入动画态，再改定位会偏半个身位 */
    el.style.left = (tx - hr.left - s / 2) + 'px';
    el.style.top = (ty - hr.top - s / 2) + 'px';
    host.appendChild(el);
    _ringEl = el;
    _ringT1 = setTimeout(function () {
      if (!el.parentNode) return;
      el.classList.add('static');
      _ringT2 = setTimeout(function () { if (el.parentNode) el.parentNode.removeChild(el); if (_ringEl === el) _ringEl = null; }, 3000);
    }, 1600);
  }

  window.CardLayout = {
    PAD: PAD,
    PRIORITY: PRIORITY,
    place: place,
    placeAll: placeAll,
    ring: ring,
    clearRing: clearRing,
    setAnchor: setAnchor,
    getAnchor: getAnchor,
    clearAnchor: clearAnchor,
    isMobile: isMobile,
    /* 供拖动使用：把视口坐标转成卡片定位坐标系 */
    toHost: function (x, y) {
      var host = hostEl();
      if (!host) return { x: x, y: y, w: window.innerWidth, h: window.innerHeight };
      var hr = host.getBoundingClientRect();
      return { x: x - hr.left, y: y - hr.top, w: hr.width, h: hr.height };
    }
  };
})();
