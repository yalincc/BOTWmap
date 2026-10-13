/* ============================================================
 * 旷野之息互动地图 · 宝箱板块（V1.6.1 调整 i）
 * ------------------------------------------------------------
 * 宝箱为与任务/装备/探索收集平级的顶级大类（lg-group 标准形态，
 * 插在探索收集组之前），三条平级子行：普通宝箱 / DLC 宝箱 / 大师模式宝箱。
 *   - 普通宝箱 = items[].pts 第三元素不以 _Aoc/_Aoc_long/_Collabo 结尾（1167 点）
 *   - DLC 宝箱 = 第三元素以 _Aoc/_Aoc_long/_Collabo 结尾（27 点）
 *   - 大师模式宝箱 = items[].hardPts（118 点）
 * 数据 = data/chests.js（BOTW_CHESTS）；坐标均为像素坐标，直接画
 *   （禁止 px=(x+6000)*2 游戏坐标逆变换——会越界）。
 * 绘制/命中条件 = 三子开关独立（localStorage botwmap.chestLayers.v1={normal,dlc,hard}）。
 * 点击宝箱点 → #chestCard（内容物中文名 / DLC 归属徽章 / 类型 / 坐标 / 数量）。
 *
 * ★ 与地图引擎隔离：map.js 零改动；探索收集组的 treasure 行由本模块 DOM 手术移除。
 * ★ ui.js 的 ext 命中分发只认 quest/equip：本模块包装 map._onSelect 拦截
 *   {type:'chest'}（不能丢）。
 * ★ 存档同步（V1.6.3）：save_upload.js 反查 chest_save.js 命中已开宝箱 →
 *   ChestSync.loadSave 注入存档完成集；isDone = 存档 ∪ 手动标记。
 * ============================================================ */
(function () {
  'use strict';

  /* ---------- 数据 ---------- */
  var CHESTS = (typeof BOTW_CHESTS !== 'undefined' && BOTW_CHESTS.items) ? BOTW_CHESTS.items : [];
  var CHEST_TYPE_CN = (typeof BOTW_CHESTS !== 'undefined' && BOTW_CHESTS.chestTypeCn) ? BOTW_CHESTS.chestTypeCn : {};

  /* contentCn 为 null 的中文名兜底（chests.js 数据缺口） */
  var CN_FALLBACK = {
    GameRomHorseReins_10: '古代缰绳',
    GameRomHorseSaddle_10: '古代马鞍'
  };

  /* 三子分组：过滤天然互斥（自检断言两两零重叠） */
  var normalList = [], dlcList = [], hardList = [];
  CHESTS.forEach(function (it) {
    (it.pts || []).forEach(function (p) {
      var t = p[2] || '';
      if (t.indexOf('_Aoc') >= 0 || t.indexOf('_Collabo') >= 0) dlcList.push({ item: it, pt: p, hard: false });
      else normalList.push({ item: it, pt: p, hard: false });
    });
    (it.hardPts || []).forEach(function (p) {
      hardList.push({ item: it, pt: p, hard: true });
    });
  });
  var SUB = { normal: normalList, dlc: dlcList, hard: hardList };
  var SUB_ORDER = ['normal', 'dlc', 'hard'];
  var SUB_NAME = { normal: '普通宝箱', dlc: 'DLC 宝箱', hard: '大师模式宝箱' };
  var SUB_COLOR = { normal: '#f4b942', dlc: '#8a5cf6', hard: '#c084d9' };

  /* 内容物 → DLC 归属（徽章依据）：
     DLC1 = 幻影171 / 魔吉拉172头 / 米多娜173头 / 汀空174 / 克洛格面具176头 / 传送标记器；
     DLC2 = 蓝色大虾175上 / 罗维奥177头 / 藏特178头 / 近卫兵179 / 幻影盖侬180 / 古代缰绳马鞍；
     联动 = 打捞员185；特典 = 它的衬衫170；红宝石/炸弹箭 → ''（通用 DLC，归属待确认） */
  function attrOf(content) {
    if (!content) return '';
    if (/^Armor_17[1234]/.test(content)) return 'DLC1';
    if (content === 'Armor_176_Head') return 'DLC1';
    if (content === 'Obj_WarpDLC') return 'DLC1';
    if (content === 'Armor_175_Upper') return 'DLC2';
    if (/^Armor_17[789]|^Armor_180|GameRomHorse(Reins|Saddle)_10/.test(content)) return 'DLC2';
    if (/^Armor_185_/.test(content)) return '联动';
    if (content === 'Armor_170_Upper') return '特典';
    return '';
  }

  function cnName(it) {
    return it.contentCn || CN_FALLBACK[it.content] || it.en || it.content;
  }

  var map = null;
  var ready = false;
  /* 子开关状态：{normal, dlc, hard} 默认全开（localStorage 记忆） */
  var K_LAYERS = 'botwmap.chestLayers.v1';
  var cLayers = { normal: true, dlc: true, hard: true };
  /* V1.6.2：三个子类图标（用户自制卡通图标，地图 + 侧栏统一） */
  var CHEST_ICON = {
    normal: 'assets/icons/chest_normal.png',
    dlc: 'assets/icons/chest_dlc.png',
    hard: 'assets/icons/chest_hard.png'
  };
  var chestImgs = {};
  SUB_ORDER.forEach(function (s) { var im = new Image(); im.src = CHEST_ICON[s]; chestImgs[s] = im; });

  /* ---------- 完成态（V1.6.2 手动，localStorage；以坐标为键，与装备完成态联动） ---------- */
  var K_CHDONE = 'botwmap.chestDone.v1';
  var chestDone = new Set();
  try { JSON.parse(localStorage.getItem(K_CHDONE) || '[]').forEach(function (k) { chestDone.add(k); }); } catch (e) {}
  function ptKey(p) { return Math.round(p[0]) + ',' + Math.round(p[1]); }
  function persistChDone() { try { localStorage.setItem(K_CHDONE, JSON.stringify(Array.from(chestDone))); } catch (e) {} }
  var chListeners = [];
  function notifyCh() { chListeners.forEach(function (fn) { try { fn(); } catch (e) {} }); }

  /* ---------- V1.6.3 存档同步完成集（save_upload.js 注入；与手动标记并存） ----------
   * 数据 = data/chest_save.js（1312 条 crc32(flagName)->像素点，romfs 权威派生），
   * loadSave 收到存档已开宝箱的像素点数组（存档权威，整集替换）；
   * isDone = 存档 ∪ 手动。localStorage 仅作刷新后展示缓存，权威每次随存档重注入。 */
  var K_SAVE_DONE = 'botwmap.chestSaveDone.v1';
  var saveDone = new Set();
  try { JSON.parse(localStorage.getItem(K_SAVE_DONE) || '[]').forEach(function (k) { saveDone.add(k); }); } catch (e) {}
  function persistSaveDone() { try { localStorage.setItem(K_SAVE_DONE, JSON.stringify(Array.from(saveDone))); } catch (e) {} }
  window.ChestSync = {
    loadSave: function (pts) {
      saveDone = new Set();
      (pts || []).forEach(function (p) { if (p) saveDone.add(ptKey(p)); });
      persistSaveDone(); notifyCh();
    }
  };

  /* 对外：装备完成态标记时会调 markPoints 同步宝箱完成 */
  window.ChestPanel = {
    isDone: function (p) { if (!p) return false; var kk = ptKey(p); return chestDone.has(kk) || saveDone.has(kk); },
    toggleDone: function (p) {
      if (!p) return null;
      var kk = ptKey(p), st;
      if (chestDone.has(kk)) { chestDone.delete(kk); st = { done: false }; }
      else { chestDone.add(kk); st = { done: true }; }
      persistChDone(); notifyCh();
      return st;
    },
    markPoints: function (list, on) {
      (list || []).forEach(function (p) {
        if (!p) return;
        var kk = ptKey(p);
        if (on) chestDone.add(kk); else chestDone.delete(kk);
      });
      persistChDone(); notifyCh();
    },
    onChange: function (fn) { chListeners.push(fn); }
  };

  /* ---------- 宝箱顶级大类注入（DOM 手术：ui.js buildLayerList 渲染完成后执行；
     从探索收集移除 treasure 行+旧子容器，新建「宝箱」顶级组插在探索收集之前） ---------- */
  function injectChestGroup() {
    var wrap = document.getElementById('layerList');
    if (!wrap) { setTimeout(injectChestGroup, 120); return; }
    if (wrap.querySelector('.lg-group[data-group="宝箱"]')) return;

    /* 1) 探索收集移除 treasure 行 + 旧 chest-subs（恢复 6 项） */
    var oldRow = wrap.querySelector('.lrow[data-cat="treasure"]');
    if (oldRow && oldRow.parentElement) oldRow.parentElement.removeChild(oldRow);
    var oldSubs = wrap.querySelector('.chest-subs');
    if (oldSubs && oldSubs.parentElement) oldSubs.parentElement.removeChild(oldSubs);

    /* 2) 「宝箱」顶级组：三条平级子行，无父行开关、无嵌套 */
    var grp = document.createElement('div');
    grp.className = 'lg-group';
    grp.dataset.group = '宝箱';
    var g = document.createElement('div');
    g.className = 'lg';
    var gLabel = document.createElement('span');
    gLabel.textContent = '宝箱';
    var gBtn = document.createElement('button');
    gBtn.type = 'button';
    gBtn.className = 'lg-btn';
    gBtn.dataset.group = '宝箱';
    gBtn.title = '一键勾选 / 清空全部宝箱';
    g.appendChild(gLabel); g.appendChild(gBtn);
    grp.appendChild(g);
    var lrows = document.createElement('div');
    lrows.className = 'lrows';
    grp.appendChild(lrows);

    SUB_ORDER.forEach(function (sub) {
      var n = SUB[sub].length;
      var r = document.createElement('div');
      r.className = 'lrow' + (cLayers[sub] ? ' on' : '');
      r.dataset.cat = 'chest_' + sub;
      /* V1.6.2：三子类统一用各自卡通图标 */
      var iconHtml = '<img class="lg-ic" src="' + CHEST_ICON[sub] + '" alt="" loading="lazy">';
      r.innerHTML =
        iconHtml +
        '<span class="lname">' + SUB_NAME[sub] + '</span>' +
        '<span class="lprog">' + n + '</span>';
      r.title = '点击切换显示 / 隐藏 ' + SUB_NAME[sub] + '（' + n + ' 点）';
      r.addEventListener('click', function () {
        cLayers[sub] = !cLayers[sub];
        r.classList.toggle('on', cLayers[sub]);
        try { localStorage.setItem(K_LAYERS, JSON.stringify(cLayers)); } catch (err) {}
        drawExt();
      });
      lrows.appendChild(r);
    });

    gBtn.addEventListener('click', function () {
      var allOn = SUB_ORDER.every(function (s) { return cLayers[s]; });
      SUB_ORDER.forEach(function (s) { cLayers[s] = !allOn; });
      lrows.querySelectorAll('.lrow[data-cat]').forEach(function (rowEl) { rowEl.classList.toggle('on', !allOn); });
      try { localStorage.setItem(K_LAYERS, JSON.stringify(cLayers)); } catch (err) {}
      drawExt();
    });

    /* 3) 插入到「探索收集」组之前（即装备组之后） */
    var anchor = wrap.querySelector('.lg-group[data-group="探索收集"]');
    if (anchor) wrap.insertBefore(grp, anchor);
    else wrap.appendChild(grp);
  }

  function drawExt() {
    if (!map || !ready) return;
    map.draw();
  }

  /* ---------- Canvas 绘制（V1.6.2：三子类图标，屏幕 22px，所有缩放统一） ---------- */
  function paint(ctx) {
    if (!map) return;
    var scale = map.view.scale;
    var size = window.CardLayout.markerSize(scale);   /* V1.6.2：随缩放自适应，最大 36px */
    var any = false;
    SUB_ORDER.forEach(function (sub) { if (cLayers[sub]) any = true; });
    if (!any) return;

    var pad = 60;
    var x0 = Math.max(0, (-pad - map.view.x) / scale), y0 = Math.max(0, (-pad - map.view.y) / scale);
    var x1 = Math.min(map.MW, (map.w + pad - map.view.x) / scale), y1 = Math.min(map.MH, (map.h + pad - map.view.y) / scale);

    SUB_ORDER.forEach(function (sub) {
      if (!cLayers[sub]) return;
      var list = SUB[sub], color = SUB_COLOR[sub];
      var img = chestImgs[sub];
      var hasImg = img && img.complete && img.naturalWidth;
      for (var i = 0; i < list.length; i++) {
        var entry = list[i];
        var px = entry.pt;
        if (px[0] < x0 || px[0] > x1 || px[1] < y0 || px[1] > y1) continue;
        var sx = map.view.x + px[0] * scale, sy = map.view.y + px[1] * scale;
        var isDone = window.ChestPanel.isDone(px);
        ctx.save();
        ctx.globalAlpha = isDone ? 0.65 : 1;   /* V1.6.2：完成态降透明 */
        if (hasImg) {
          ctx.drawImage(img, sx - size / 2, sy - size / 2, size, size);
        } else {
          /* 图标未加载兜底：子类色圆点 */
          ctx.beginPath();
          ctx.arc(sx, sy, size / 2 - 3, 0, Math.PI * 2);
          ctx.fillStyle = color;
          ctx.fill();
        }
        if (isDone) window.CardLayout.doneCheck(ctx, sx + size / 2 - 5, sy - size / 2 + 5, 7);
        ctx.restore();
      }
    });
  }

  /* 命中：宝箱点（屏幕 12px 容差），返回 {type:'chest', item, pt, hard} */
  function hit(mx, my) {
    if (!map) return null;
    var tol = (window.CardLayout.markerSize(map.view.scale) / 2 + 4) / map.view.scale;
    var best = null, bd = 1e18;
    SUB_ORDER.forEach(function (sub) {
      if (!cLayers[sub]) return;
      var list = SUB[sub];
      for (var i = 0; i < list.length; i++) {
        var entry = list[i];
        var p = entry.pt;
        var dx = p[0] - mx, dy = p[1] - my;
        if (Math.abs(dx) <= tol && Math.abs(dy) <= tol) {
          var d = dx * dx + dy * dy;
          if (d < bd) { bd = d; best = entry; }
        }
      }
    });
    return best ? { type: 'chest', item: best.item, pt: best.pt, hard: best.hard, _r: best.item, _p: best.pt } : null;
  }

  /* 定位：飞到该宝箱点 */
  function locate(p) {
    if (!map || !p) return;
    map.view.scale = Math.min(Math.max(map.view.scale, 1.2), map.maxScale);
    map.view.x = map.w / 2 - p[0] * map.view.scale;
    /* V1.6.1：手机端底部抽屉（55vh）会盖住下半屏，定位时把目标点推到上方 35% 处 */
    var vy = (window.CardLayout && window.CardLayout.isMobile()) ? map.h * 0.35 : map.h / 2;
    map.view.y = vy - p[1] * map.view.scale;
    map.draw();
    if (map.pulseAt) map.pulseAt(p[0], p[1]);   /* V1.6.1 调整g：定位脉冲光圈 */
    /* V1.6.1 调整i：跳转后按新目标点重算避让 */
    var card = document.getElementById('chestCard');
    if (card && !card.classList.contains('hidden') && map.m2s) {
      card.dataset.manual = '';
      var sp = map.m2s(p);
      map.layoutCard(card, sp[0], sp[1]);
    }
  }

  /* ---------- 宝箱卡片 ---------- */
  function esc(s) {
    return String(s == null ? '' : s).replace(/[&<>"']/g, function (c) {
      return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c];
    });
  }

  /* V1.6.1 调整5：hover 预览态机 hidden | preview | fixed（同 quest/equip） */
  var pv = { mode: 'hidden', ref: null };
  var previewNow = false;

  function openCard(entry) {
    var card = document.getElementById('chestCard');
    var body = document.getElementById('chestCardBody');
    if (!card || !body || !entry) return;
    card.dataset.manual = '';   /* V1.6.1 调整i：新卡 → 清除手动态，重新避让 */
    pv.mode = 'fixed'; pv.ref = entry;   /* 点击路径默认固定态（onHoverExt 随后覆盖为 preview） */
    var it = entry.item, p = entry.pt;
    lastOpen = Date.now();

    /* 互斥：开宝箱卡时关任务卡 / 装备卡 */
    var qc = document.getElementById('questCard');
    if (qc) qc.classList.add('hidden');
    var ec = document.getElementById('equipCard');
    if (ec) ec.classList.add('hidden');

    var attr = attrOf(it.content);
    var badgeHtml = '';
    if (entry.hard) {
      badgeHtml = '<span class="q-badge-hard" style="background:rgba(192,132,217,.16);border-color:#8a5aa8;color:#c084d9">大师模式·浮岛平台</span>';
    } else if (attr === 'DLC1') {
      badgeHtml = '<span class="q-badge-hard" style="background:rgba(139,92,246,.16);border-color:#8a5cf6;color:#b18cff">DLC1</span>';
    } else if (attr === 'DLC2') {
      badgeHtml = '<span class="q-badge-hard" style="background:rgba(216,178,92,.16);border-color:#c9a04e;color:#eac27e">DLC2</span>';
    } else if (attr === '联动') {
      badgeHtml = '<span class="q-badge-hard" style="background:rgba(126,200,169,.16);border-color:#4e9c7c;color:#7ec8a9">联动</span>';
    } else if (attr === '特典') {
      badgeHtml = '<span class="q-badge-hard" style="background:rgba(255,209,102,.16);border-color:#b8902e;color:#ffd166">特典</span>';
    } else if (/_Aoc|_Collabo/.test(p[2] || '')) {
      /* V1.6.2 修复：只有 DLC 宝箱点才可能显示「归属待确认」；普通宝箱一律不显示 */
      badgeHtml = '<span class="q-badge-hard" style="background:rgba(255,255,255,.1);border-color:#666;color:#aaa">DLC（归属待确认）</span>';
    }

    var done = window.ChestPanel.isDone(p);
    var h = '';
    h += '<div class="tk-grip">';
    h += '<span class="tk-chip ' + (entry.hard ? 'tk-chip-mini' : 'tk-chip-main') + '">' + (entry.hard ? '大师模式宝箱' : '宝箱') + '</span>';
    if (badgeHtml) h += badgeHtml;
    /* V1.6.2：去掉复制图标（宝箱只有 3 个名字，无需复制） */
    h += '<span class="tk-grip-acts">' +
      '<button type="button" class="tk-close" id="chCardClose" title="关闭">×</button></span>';
    h += '</div>';
    h += '<div class="tk-title-row"><h3 class="tk-name">' + esc(cnName(it)) + '</h3>' +
      (done ? '<span class="tk-done-tick" title="已完成">✓</span>' : '') + '</div>';
    if (it.en) h += '<div class="qcard-en">' + esc(it.en) + '</div>';
    h += '<div class="tk-kv"><span class="tk-k">宝箱类型</span><span class="tk-v">' +
      esc(CHEST_TYPE_CN[p[2]] || p[2] || '—') + '</span></div>';
    /* V1.6.2：去掉「坐标 X/Z 数字」行——游戏内并无此坐标，对玩家无意义（保留底部「定位」按钮） */
    var totalN = entry.hard ? (it.hardChestCount || 0) : (it.chestCount || 0);
    h += '<div class="tk-kv"><span class="tk-k">数量</span><span class="tk-v">' +
      (entry.hard ? '大师模式 ' + totalN + ' 处' : '普通 ' + totalN + ' 处') + '</span></div>';
    h += '<div class="tk-actions"><button type="button" class="btn act" id="chLocate">定位</button>' +
      '<button type="button" class="btn done' + (done ? ' is-done' : '') + '" id="chDone">' + (done ? '取消完成' : '标记完成') + '</button></div>';

    body.innerHTML = h;
    card.classList.remove('hidden');

    var closer = document.getElementById('chCardClose');
    if (closer) closer.addEventListener('click', function (e) {
      e.stopPropagation();
      card.classList.add('hidden');
      pv.mode = 'hidden'; pv.ref = null; previewNow = false;
    });
    /* 标记完成（V1.6.2）：宝箱级完成态，与装备完成态共享同一存储 */
    var dn = document.getElementById('chDone');
    if (dn) dn.addEventListener('click', function (e) {
      e.stopPropagation();
      var st = window.ChestPanel.toggleDone(p);
      openCard({ item: it, pt: p, hard: entry.hard });
      if (map) map.draw();
      if (typeof UI !== 'undefined' && UI.toast) UI.toast(st.done ? '已标记完成：' + cnName(it) : '已取消完成：' + cnName(it));
    });
    var lf = document.getElementById('chLocate');
    if (lf) lf.addEventListener('click', function (e) {
      e.stopPropagation();
      locate(p);
      if (window.SpotMark) window.SpotMark.set(p);   /* V1.6.2：红点脉冲 */
      positionCard();
    });
    positionCard();
  }

  /* 卡片定位：统一走 map.layoutCard（避让 + 手机抽屉交给 CSS） */
  function positionCard() {
    var card = document.getElementById('chestCard');
    if (!card || !map) return;
    if (card.dataset.manual === '1') return;   /* 手动拖过：保持手动态位 */
    /* 卡片此刻还是 hidden = 正要打开新卡 → 清掉上次定位锚点，改用地图点击点 */
    if (card.classList.contains('hidden') && window.CardLayout) window.CardLayout.clearAnchor();
    var sx, sy;
    if (previewNow && typeof map._hoverSX === 'number') { sx = map._hoverSX; sy = map._hoverSY; }
    /* ★ 优先复用最近一次定位的锚点（卡片内点坐标定位后，外层还会再调一次本函数，
       若此时退回 _lastClick 会把正确的避让结果覆盖掉） */
    else if (window.CardLayout && window.CardLayout.getAnchor()) { var a = window.CardLayout.getAnchor(); sx = a.sx; sy = a.sy; }
    else if (map._lastClick) { sx = map._lastClick.sx; sy = map._lastClick.sy; }
    else { sx = map.w / 2; sy = map.h * 0.45; }
    map.layoutCard(card, sx, sy);
  }

  function closeCard() {
    var card = document.getElementById('chestCard');
    if (card) card.classList.add('hidden');
    pv.mode = 'hidden'; pv.ref = null; previewNow = false;
  }

  /* V1.6.1 调整g：地图空白命中广播 → 关闭宝箱卡并复位 pv 态机 */
  window.addEventListener('botw:blankclose', closeCard);

  /* V1.6.1 调整5：hover 预览入口（map.js mousemove 仅变化时回调） */
  function onHoverExt(ext) {
    /* V1.6.1：PC 端悬停不再弹卡预览（改为地图左上角名称条），只有点击才弹卡 */
    if (window.innerWidth > 760) return;
    if (pv.mode === 'fixed') return;
    var mine = ext && ext.type === 'chest' ? { item: ext.item, pt: ext.pt, hard: ext.hard } : null;
    if (mine) {
      if (pv.mode === 'preview' && pv.ref && pv.ref.item === mine.item && pv.ref.pt === mine.pt) return;
      previewNow = true;
      openCard(mine);
      pv.mode = 'preview'; pv.ref = mine;
      previewNow = false;
    } else if (pv.mode !== 'hidden') {
      var card = document.getElementById('chestCard');
      if (card) card.classList.add('hidden');
      pv.mode = 'hidden'; pv.ref = null;
    }
  }

  var lastOpen = 0;
  document.addEventListener('click', function (e) {
    var card = document.getElementById('chestCard');
    if (!card || card.classList.contains('hidden')) return;
    var el = e.target;
    if (card.contains(el)) return;
    /* ★ V1.6.3：刚拖完卡片，这一次 click 不关卡（同 quest-card.js） */
    if (card._dragged) return;
    if (el && el.closest && el.closest('#chestCard')) return;
    if (map && map.canvas && map.canvas.contains(el) && Date.now() - lastOpen < 400) return;
    closeCard();
  });
  document.addEventListener('keydown', function (e) {
    if (e.key === 'Escape') closeCard();
  });

  /* ui.js 的 ext 命中分发只认 quest/equip：包装 map._onSelect 拦截 {type:'chest'} */
  function hookSelect() {
    if (!map || typeof map._onSelect !== 'function') { setTimeout(hookSelect, 120); return; }
    if (map._onSelect.__chestWrapped) return;
    var orig = map._onSelect;
    var wrapped = function (sel) {
      if (sel && sel.ext && sel.ext.type === 'chest') {
        openCard({ item: sel.ext.item, pt: sel.ext.pt, hard: sel.ext.hard });
        return;
      }
      orig.call(map, sel);
    };
    wrapped.__chestWrapped = true;
    map._onSelect = wrapped;
  }

  function init() {
    var boot = function () {
      map = window.__botwMap;
      if (!map) { setTimeout(boot, 60); return; }
      /* 恢复子开关：逐键合并补默认；旧格式 {dlc,hard} 缺 normal → 默认 true */
      try {
        var d = JSON.parse(localStorage.getItem(K_LAYERS) || 'null');
        if (d && typeof d === 'object') {
          Object.keys(cLayers).forEach(function (k) {
            cLayers[k] = (typeof d[k] === 'boolean') ? d[k] : cLayers[k];
          });
        }
      } catch (e) {}
      injectChestGroup();
      map._extDraw.push(paint);
      map.extHits.push(hit);
      if (map.extHoverHooks) map.extHoverHooks.push(onHoverExt);  /* V1.6.1 调整5：hover 预览 */
      var cc = document.getElementById('chestCard');
      if (window.initCardDrag && cc) window.initCardDrag(cc);     /* V1.6.1 调整i：卡片拖动 */
      window.ChestPanel.onChange(function () { if (map) map.draw(); });  /* V1.6.2：完成态变化重绘 */
      hookSelect();
      ready = true;
      map.draw();
    };
    if (document.readyState === 'loading') {
      document.addEventListener('DOMContentLoaded', boot, { once: true });
    } else {
      boot();
    }
  }
  init();
})();
