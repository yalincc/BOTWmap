/* ============================================================
 * 旷野之息互动地图 · 任务板块（V1.6.1）
 * ------------------------------------------------------------
 * 将「任务」并入探索面板（不加侧栏 Tab）：任务组 4 分类 =
 *   主线挑战 / 支线任务 / 神庙挑战 / DLC 任务（V1.6.1 新增第 4 档）。
 * V1.6.1：dlc∈{'1','2'} 的任务（共 18 项）从本体三档拆到 DLC 档绘制；
 *   联动 side_148（EX 异度神剑2，pts=[]）保留在支线行；卡片加 DLC1/DLC2/联动 徽章；
 *   修复 questLayers.v1 只存不读（init 恢复并逐键补默认）。
 * 地图上画任务点（Canvas 外部图层，经 map.js 的 _extDraw / extHits 钩子），
 * 完成状态双源：
 *   - 存档（权威）：save_upload.js 解析任务完成标志后调用 QuestDone.loadSave
 *   - 手动：localStorage 标记，未映射到存档标志的任务以此兜底
 * 点击任务点 → 任务卡片（分类 / 名称 / 坐标 / 描述 / 步骤 / 奖励 / 完成按钮）
 *
 * ★ 与地图引擎隔离：不侵入 map.js 业务逻辑，只注册绘制/命中回调。
 * ============================================================ */
(function () {
  'use strict';

  /* ---------- 数据 ---------- */
  var QUESTS = (typeof BOTW_QUESTS !== 'undefined' && BOTW_QUESTS.quests) ? BOTW_QUESTS.quests : [];
  var CAT_NAME = { main: '主线挑战', side: '支线任务', shrine: '神庙挑战', dlc: 'DLC 任务' };
  /* V1.6.1：dlc 档用暗紫（与主线金/支线绿/神庙蓝区分） */
  var CAT_COLOR = { main: '#eac27e', side: '#7ec8a9', shrine: '#6fb3e0', dlc: '#c58fe0' };
  var CAT_ORDER = ['main', 'side', 'shrine', 'dlc'];

  var map = null;
  var ready = false;
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

  /* ---------- 完成态（存档 + 手动 双源） ---------- */
  var K_DONE = 'botwmap.questDone.v1';
  var manualSet = new Set();   // 手动标记（localStorage）
  var saveSet = new Set();     // 存档解析（save_upload 注入）
  var listeners = [];

  function loadDone() {
    try {
      var d = JSON.parse(localStorage.getItem(K_DONE) || '[]');
      d.forEach(function (k) { manualSet.add(k); });
    } catch (e) {}
  }
  function persistDone() {
    try { localStorage.setItem(K_DONE, JSON.stringify(Array.from(manualSet))); } catch (e) {}
  }
  var QuestDone = {
    isDone: function (key) { return !!key && (saveSet.has(key) || manualSet.has(key)); },
    /* 完成来源：save=存档（权威，不可取消） / manual=手动 / ''=未完成 */
    srcOf: function (key) {
      if (saveSet.has(key)) return 'save';
      if (manualSet.has(key)) return 'manual';
      return '';
    },
    toggle: function (key) {
      if (!key) return null;
      if (saveSet.has(key)) { /* 存档态不可手动取消 */ return { done: true, src: 'save' }; }
      var st;
      if (manualSet.has(key)) { manualSet.delete(key); st = { done: false, src: '' }; }
      else { manualSet.add(key); st = { done: true, src: 'manual' }; }
      persistDone();
      notify();
      return st;
    },
    /* 存档标志 → 任务完成集。flags: {flagName: true 表示已完成} */
    loadSave: function (flags) {
      saveSet = new Set();
      if (flags) {
        QUESTS.forEach(function (q) {
          if (q.flag && flags[q.flag]) saveSet.add(q.key);
        });
      }
      notify();
    },
    onChange: function (fn) { listeners.push(fn); },
    doneCount: function (cat) {
      var n = 0;
      QUESTS.forEach(function (q) { if ((!cat || q.cat === cat) && QuestDone.isDone(q.key)) n++; });
      return n;
    }
  };
  function notify() {
    listeners.forEach(function (fn) { try { fn(); } catch (e) {} });
  }

  /* ---------- 面板注入 ---------- */
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

    CAT_ORDER.forEach(function (cat) {
      var total = 0, done = 0;
      QUESTS.forEach(function (q) { if (inRow(cat, q)) { total++; if (QuestDone.isDone(q.key)) done++; } });
      var row = document.createElement('div');
      row.className = 'lrow' + (qLayers[cat] ? ' on' : '');
      row.dataset.qcat = cat;
      row.innerHTML =
        '<span class="dot" style="background:' + CAT_COLOR[cat] + '"></span>' +
        '<span class="lname">' + CAT_NAME[cat] + '</span>' +
        '<span class="lprog" id="qprog_' + cat + '">' + done + '/' + total + '</span>';
      row.title = '点击整行切换显示 / 隐藏';
      if (cat === 'dlc') {
        var g1 = 0, g2 = 0, gc = 0;
        QUESTS.forEach(function (q) { if (q.dlc === '1') g1++; else if (q.dlc === '2') g2++; else if (q.dlc) gc++; });
        row.title = 'DLC1 ' + g1 + ' 项 · DLC2 ' + g2 + ' 项 · 联动 ' + gc + ' 项（联动任务无地图点，宝箱见「探索收集」组）';
      }
      row.addEventListener('click', function () {
        qLayers[cat] = !qLayers[cat];
        row.classList.toggle('on', qLayers[cat]);
        try { localStorage.setItem(K_LAYERS, JSON.stringify(qLayers)); } catch (e) {}
        drawExt();
      });
      lrows.appendChild(row);
    });
    /* 插入到 #layerList 顶部（#layerList 是 flex:1 + overflow-y:auto 滚动容器，
       尾部 append 会被滚动区裁掉不可见；置顶保证打开侧栏即可看到任务组） */
    wrap.insertBefore(grp, wrap.firstChild);

    gBtn.addEventListener('click', function () {
      var allOn = CAT_ORDER.every(function (c) { return qLayers[c]; });
      CAT_ORDER.forEach(function (c) { qLayers[c] = !allOn; });
      grp.querySelectorAll('.lrow[data-qcat]').forEach(function (r) { r.classList.toggle('on', !allOn); });
      try { localStorage.setItem(K_LAYERS, JSON.stringify(qLayers)); } catch (e) {}
      drawExt();
    });

    /* 完成态变化时刷新 4 行计数（卡片标记 / 载入存档后调用） */
    window._questRefreshRows = function () {
      CAT_ORDER.forEach(function (cat) {
        var el = document.getElementById('qprog_' + cat);
        if (!el) return;
        var total = 0, done = 0;
        QUESTS.forEach(function (q) { if (inRow(cat, q)) { total++; if (QuestDone.isDone(q.key)) done++; } });
        el.textContent = done + '/' + total;
      });
    };
  }

  /* ---------- Canvas 绘制 ---------- */
  function drawExt() {
    if (!map || !ready) return;
    map.draw();
  }

  /* 任务点：方形徽标（分类色 + 白描边），完成态绿勾 + 降透明 */
  function paint(ctx) {
    if (!map) return;
    window.__qPaints = (window.__qPaints || 0) + 1; /* 调试计数 */
    var scale = map.view.scale;
    /* 全图/省区级：8px 色点；放大后：20px 方形徽标 */
    var dot = scale < 0.12;
    var size = dot ? 8 : 20;
    var on = {};
    CAT_ORDER.forEach(function (c) { if (qLayers[c]) on[c] = true; });
    if (!CAT_ORDER.some(function (c) { return on[c]; })) return;

    var pad = 60;
    var x0 = Math.max(0, (-pad - map.view.x) / scale), y0 = Math.max(0, (-pad - map.view.y) / scale);
    var x1 = Math.min(map.MW, (map.w + pad - map.view.x) / scale), y1 = Math.min(map.MH, (map.h + pad - map.view.y) / scale);

    for (var i = 0; i < QUESTS.length; i++) {
      var q = QUESTS[i];
      var ql = qLayerOf(q);
      if (!on[ql] || !q.pts || !q.pts.length) continue;
      var done = QuestDone.isDone(q.key);
      var alpha = done ? 0.42 : 0.95;
      for (var j = 0; j < q.pts.length; j++) {
        var px = q.pts[j];
        if (px[0] < x0 || px[0] > x1 || px[1] < y0 || px[1] > y1) continue;
        var sx = map.view.x + px[0] * scale, sy = map.view.y + px[1] * scale;
        ctx.save();
        ctx.globalAlpha = alpha;
        var color = CAT_COLOR[ql] || '#888';
        if (dot) {
          ctx.beginPath();
          ctx.arc(sx, sy, size / 2, 0, Math.PI * 2);
          ctx.fillStyle = color;
          ctx.fill();
          ctx.strokeStyle = 'rgba(255,255,255,.65)';
          ctx.lineWidth = 1;
          ctx.stroke();
        } else {
          var h = size / 2;
          ctx.fillStyle = color;
          ctx.strokeStyle = 'rgba(255,255,255,.92)';
          ctx.lineWidth = 1.5;
          ctx.beginPath();
          if (ctx.roundRect) {
            ctx.roundRect(sx - h, sy - h, size, size, 5);
          } else {
            ctx.rect(sx - h, sy - h, size, size);
          }
          ctx.fill();
          ctx.stroke();
          /* 分类首字（主线=主 / 支线=支 / 神庙=神 / DLC=EX），比纯色块更好辨认 */
          ctx.fillStyle = '#14181c';
          ctx.font = '700 11px "PingFang SC","Microsoft YaHei",sans-serif';
          ctx.textAlign = 'center';
          ctx.textBaseline = 'middle';
          ctx.fillText(ql === 'main' ? '主' : ql === 'shrine' ? '神' : ql === 'side' ? '支' : 'EX', sx, sy + 1);
          if (done) {
            /* 绿勾（右下角） */
            ctx.strokeStyle = '#7dffa0';
            ctx.lineWidth = 2;
            ctx.beginPath();
            ctx.moveTo(sx + 1, sy - 1);
            ctx.lineTo(sx + 5, sy + 3);
            ctx.lineTo(sx + 10, sy - 4);
            ctx.stroke();
          }
        }
        ctx.restore();
      }
    }
  }

  /* 命中：任务点（屏幕 12px 容差），返回 {type:'quest', quest} */
  function hit(mx, my) {
    if (!map) return null;
    var tol = 12 / map.view.scale;
    var best = null, bd = 1e18;
    for (var i = 0; i < QUESTS.length; i++) {
      var q = QUESTS[i];
      var ql = qLayerOf(q);
      if (!qLayers[ql] || !q.pts || !q.pts.length) continue;
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
    if (!map || !q || !q.pts || !q.pts.length) return;
    var p = q.pts[0];
    map.view.scale = Math.min(Math.max(map.view.scale, 1.2), map.maxScale);
    map.view.x = map.w / 2 - p[0] * map.view.scale;
    /* V1.6.1：手机端底部抽屉（55vh）会盖住下半屏，定位时把目标点推到上方 35% 处 */
    var vy = (window.CardLayout && window.CardLayout.isMobile()) ? map.h * 0.35 : map.h / 2;
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

  /* ---------- 任务卡片 ---------- */
  function esc(s) {
    return String(s == null ? '' : s).replace(/[&<>"']/g, function (c) {
      return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c];
    });
  }

  /* 关联任务链接（key 关联制）：reqs/unlockList 元素为 {key,name}，
     每个渲染成可点击的 co-link（data-q-key 跳转 openCard），多个用「、」分隔；
     空 / 缺失 → 显示「无」（tk-dash 样式） */
  function linkList(arr, tip) {
    if (!arr || !arr.length) return '<span class="tk-dash">无</span>';
    return arr.map(function (r) {
      return '<span class="co-link" data-q-key="' + esc(r.key) + '" title="' + tip + '">' + esc(r.name) + '</span>';
    }).join('、');
  }

  function openCard(q) {
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
    lastOpen = Date.now();
    pv.mode = 'fixed'; pv.ref = q;   /* 点击路径默认固定态（previewOpen 随后覆盖为 preview） */

    var done = QuestDone.isDone(q.key);
    var src = QuestDone.srcOf(q.key);
    var coord = q.pts && q.pts.length ? map.gameCoord(q.pts[0]) : null;
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

    var h = '';
    /* 头部：分类徽章 + 完成勾 + 上/下导航 + 关闭 */
    h += '<div class="tk-grip">';
    h += '<span class="tk-chip ' + chipCls + '">' + CAT_NAME[q.cat] + '</span>';
    /* V1.6.1：DLC 徽章（仿 q-badge-hard 胶囊；DLC1 紫 / DLC2 暗金 / 联动 青绿） */
    var dlg = dlcBadgeOf(q);
    if (dlg === 'DLC1') h += '<span class="q-badge-hard" style="background:rgba(139,92,246,.16);border-color:#8a5cf6;color:#b18cff">DLC1</span>';
    else if (dlg === 'DLC2') h += '<span class="q-badge-hard" style="background:rgba(216,178,92,.16);border-color:#c9a04e;color:#eac27e">DLC2</span>';
    else if (dlg === '联动') h += '<span class="q-badge-hard" style="background:rgba(126,200,169,.16);border-color:#4e9c7c;color:#7ec8a9">联动</span>';
    if (done) h += '<span class="tk-done-tick" title="' + (src === 'save' ? '已完成（来自存档）' : '已完成（手动标记）') + '">✓</span>';
    h += '<span class="tk-grip-acts">' +
      '<button type="button" class="tk-icon-btn tk-nav" id="qPrev" title="上一个任务">‹</button>' +
      '<button type="button" class="tk-icon-btn tk-nav" id="qNext" title="下一个任务">›</button>' +
      '<button type="button" class="tk-close" id="qcardClose" title="关闭">×</button></span>';
    h += '</div>';
    /* 标题行：#N 编号徽章 + 任务名 + 复制（对齐 TOTKmap） */
    h += '<div class="tk-title-row">' +
      '<span class="tk-no-badge" title="本档第 ' + seq + ' 个任务">#' + seq + '</span>' +
      '<h3 class="tk-name">' + esc(q.name) + '</h3>' +
      '<button type="button" class="tk-copy" id="qCopy" title="复制任务名">[复制]</button></div>';
    /* 关键信息：区域一行、位置一行（★ 英文名暂不显示，TOTKmap 卡片无英文行） */
    h += '<div class="tk-meta">';
    if (q.region) h += '<div class="tk-mrow"><span class="tk-mk">区域</span><span class="tk-mv">' + esc(q.region) + '</span></div>';
    if (coord) h += '<div class="tk-mrow"><span class="tk-mk">位置</span><span class="co-link" data-q-locate="1" title="点击定位到该坐标">X ' + coord[0] + ' · Z ' + coord[1] + '</span></div>';
    h += '</div>';
    /* 前置条件 / 后续解锁（key 关联制：渲染 q.reqs / q.unlockList，空 → 「无」；
       ★ 样式统一：同字号同色，仅链接语义区分；前置上方加分割线） */
    h += '<div class="tk-kv tk-kv-div"><span class="tk-k">前置条件</span><span class="tk-v">' +
      linkList(q.reqs, '点击查看前置任务') + '</span></div>';
    h += '<div class="tk-kv"><span class="tk-k">后续解锁</span><span class="tk-v">' +
      linkList(q.unlockList, '点击查看后续任务') + '</span></div>';
    /* 任务要点区（TOTKmap 结构：要点正文 + 接取/注意/奖励 子项）
       ★ 注意：q.note 为攻略注意事项，数据补齐后有则显示，无则不显示该行 */
    var tips = '';
    if (q.desc) tips += '<div class="tk-sec-b">' + esc(q.desc).replace(/\n/g, '<br>') + '</div>';
    if (q.npc) tips += '<div class="tk-kv"><span class="tk-k">接取</span><span class="tk-v">对话' + esc(q.npc) + '</span></div>';
    if (q.note) tips += '<div class="tk-kv"><span class="tk-k">注意</span><span class="tk-v">' + esc(q.note).replace(/\n/g, '<br>') + '</span></div>';
    if (q.reward) tips += '<div class="tk-kv"><span class="tk-k">奖励</span><span class="tk-v">' + esc(q.reward) + '</span></div>';
    if (tips) h += '<div class="tk-sec"><div class="tk-sec-t">任务要点</div>' + tips + '</div>';
    /* 流程地点（★ 流程步骤已按用户要求移除，只保留流程点；编号 ①②③ 对齐 TOTKmap 纯黄数字） */
    if (q.pts && q.pts.length) {
      var QNUM = ['①','②','③','④','⑤','⑥','⑦','⑧','⑨','⑩','⑪','⑫','⑬','⑭','⑮','⑯','⑰','⑱','⑲','⑳'];
      var fpts = q.pts.map(function (p, i) {
        var gc = map.gameCoord(p);
        return '<span class="fp" data-q-pt="' + i + '" title="点击定位到该地点"><span class="num">' + (QNUM[i] || (i + 1)) + '</span><span class="txt">X ' + gc[0] + ' · Z ' + gc[1] + '</span></span>';
      }).join('');
      h += '<div class="tk-sec"><div class="tk-sec-t">流程地点<span class="tk-src">' + q.pts.length + ' 处</span></div><div class="flow-pts">' + fpts + '</div></div>';
    }
    /* 完成状态来源 */
    h += '<div class="qcard-src">' + (src === 'save' ? '完成状态：存档自动同步（游戏内为准）' :
      src === 'manual' ? '完成状态：手动标记' : '完成状态：未标记（无存档标志，可手动标记）') + '</div>';
    /* 底部操作 */
    h += '<div class="tk-actions">';
    if (q.pts && q.pts.length) h += '<button type="button" class="btn act" id="qcardLocate">定位</button>';
    if (done && src === 'save') {
      h += '<button type="button" class="btn done is-done" disabled>已完成 ✓（存档）</button>';
    } else {
      h += '<button type="button" class="btn done' + (done ? ' is-done' : '') + '" id="qcardDone">' +
        (done ? '取消完成' : '标记完成') + '</button>';
    }
    h += '</div>';

    body.innerHTML = h;
    card.classList.remove('hidden');

    /* 定位 */
    var lc = document.getElementById('qcardLocate');
    if (lc) lc.addEventListener('click', function (e) {
      e.stopPropagation();
      locate(q);
      positionCard();
    });
    /* 坐标链接定位 */
    body.querySelectorAll('[data-q-locate]').forEach(function (el) {
      el.addEventListener('click', function (e) {
        e.preventDefault();
        e.stopPropagation();
        if (!q.pts || !q.pts.length) return;
        locate(q);
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
    /* 流程地点定位 */
    body.querySelectorAll('[data-q-pt]').forEach(function (el) {
      el.addEventListener('click', function (e) {
        e.preventDefault();
        e.stopPropagation();
        var p = q.pts[+el.dataset.qPt];
        if (!p) return;
        map.view.scale = Math.min(Math.max(map.view.scale, 1.2), map.maxScale);
        map.view.x = map.w / 2 - p[0] * map.view.scale;
        map.view.y = map.h / 2 - p[1] * map.view.scale;
        map.draw();
        positionCard();
      });
    });
    /* 标记完成 */
    var dn = document.getElementById('qcardDone');
    if (dn) dn.addEventListener('click', function (e) {
      e.stopPropagation();
      var st = QuestDone.toggle(q.key);
      openCard(q);
      if (window._questRefreshRows) window._questRefreshRows();
      if (map) map.draw();
      if (typeof UI !== 'undefined' && UI.toast) {
        UI.toast(st.done ? (st.src === 'save' ? '已完成（来自存档）' : '已标记完成：' + q.name) : '已取消完成：' + q.name);
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
      if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(q.name).catch(function () {});
        if (typeof UI !== 'undefined' && UI.toast) UI.toast('已复制：' + q.name);
      }
    });
    /* 上一个 / 下一个（本档内按 no 排序后 ±1，同 TOTKmap 任务卡） */
    function navBy(delta) {
      var i = seqList.indexOf(q);
      if (i < 0) return;
      var j = i + delta;
      if (j < 0 || j >= seqList.length) {
        if (typeof UI !== 'undefined' && UI.toast) UI.toast(delta < 0 ? '已是本档第一个任务' : '已是本档最后一个任务');
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
    var card = document.getElementById('questCard');
    if (!card || !map) return;
    if (card.dataset.manual === '1') return;   /* 手动拖过：保持手动态位 */
    /* 卡片此刻还是 hidden = 正要打开新卡 → 清掉上次定位锚点，改用地图点击点 */
    if (card.classList.contains('hidden') && window.CardLayout) window.CardLayout.clearAnchor();
    /* V1.6.1 调整g：统一走 map.layoutCard 避让定位；预览锚点优先 hover 坐标 */
    var sx, sy;
    if (previewNow && typeof map._hoverSX === 'number') { sx = map._hoverSX; sy = map._hoverSY; }
    /* ★ 优先复用最近一次定位的锚点（卡片内点坐标定位后，外层还会再调一次本函数，
       若此时退回 _lastClick 会把正确的避让结果覆盖掉） */
    else if (window.CardLayout && window.CardLayout.getAnchor()) { var a = window.CardLayout.getAnchor(); sx = a.sx; sy = a.sy; }
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
    var card = document.getElementById('questCard');
    if (!card || card.classList.contains('hidden')) return;
    var el = e.target;
    if (card.contains(el)) return;
    if (el && el.closest && el.closest('#questCard')) return;
    if (map && map.canvas && map.canvas.contains(el) && Date.now() - lastOpen < 400) return;
    closeCard();
  });
  document.addEventListener('keydown', function (e) {
    if (e.key === 'Escape') closeCard();
  });
  /* V1.6.1 调整g：地图空白命中广播 → 关闭任务卡并复位 pv 态机 */
  window.addEventListener('botw:blankclose', function () { closeCard(); });

  /* ---------- 搜索接入（供 ui.js doSearch 调用） ---------- */
  window.__questSearch = QUESTS.map(function (q) {
    return { q: q, name: q.name, en: q.en, catCn: '任务·' + (CAT_NAME[q.cat] || q.cat), pts: q.pts };
  });

  /* ---------- 初始化 ---------- */
  function init() {
    map = window.__botwMap || null;
    if (!map) { setTimeout(init, 300); return; }
    if (ready) return;

    loadDone();

    /* V1.6.1 修复：questLayers.v1 旧版只存不读。init 时从 localStorage 恢复，
       逐键合并并补默认（旧存档缺 dlc → 补 true；值非布尔回退默认；异常捕获） */
    try {
      var savedLayers = JSON.parse(localStorage.getItem(K_LAYERS) || 'null');
      if (savedLayers && typeof savedLayers === 'object') {
        Object.keys(qLayers).forEach(function (k) {
          qLayers[k] = (typeof savedLayers[k] === 'boolean') ? savedLayers[k] : qLayers[k];
        });
      }
    } catch (e) {}

    /* 旧任务标点（mainquest/shrinequest/sidequest/objective）已被 quests.js 替代：
       从 data.markers 移除，避免与新任务点双重显示 */
    var oldTask = { mainquest: 1, shrinequest: 1, sidequest: 1, objective: 1 };
    var removed = 0;
    map.data.markers = map.data.markers.filter(function (mk) {
      if (oldTask[mk.cat]) { removed++; return false; }
      return true;
    });
    if (removed) map._buildIndex();

    /* 注册绘制与命中 */
    map._extDraw.push(paint);
    map.extHits.push(hit);
    if (map.extHoverHooks) map.extHoverHooks.push(onHoverExt);  /* V1.6.1 调整5：hover 预览 */
    var qc = document.getElementById('questCard');
    if (window.initCardDrag && qc) window.initCardDrag(qc);    /* V1.6.1 调整i：卡片拖动 */

    /* 任务组面板注入（材料组之后） */
    injectPanel();

    /* UI 搜索索引需含任务：buildSearchIndex 已执行过则重建 */
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

  /* 导出 */
  window.QuestPanel = {
    QuestDone: QuestDone,
    openCard: openCard,
    closeCard: closeCard,
    locate: locate,
    isOn: function (cat) { return !!qLayers[cat]; },
    toggle: function (cat) {
      qLayers[cat] = !qLayers[cat];
      try { localStorage.setItem(K_LAYERS, JSON.stringify(qLayers)); } catch (e) {}
      var row = document.querySelector('#layerList .lrow[data-qcat="' + cat + '"]');
      if (row) row.classList.toggle('on', qLayers[cat]);
      drawExt();
    }
  };
})();
