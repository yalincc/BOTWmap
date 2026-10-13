/* ============================================================
 * 旷野之息互动地图 · 装备板块（V1.6.0）
 * ------------------------------------------------------------
 * 将「装备」并入探索面板（不加侧栏 Tab）：装备组 2 分类 =
 *   武器（90 件：单手剑/双手剑/长枪/弓/盾）/ 防具（31 件：头/衣/裤）
 * 收录范围（用户拍板）：只收【宝箱】与【任务奖励】来源的装备；
 *   商店购买 / 掉落等不收。当前数据源 = 宝箱库（chests.js 聚合），
 *   任务奖励（obtain kind:'quest'）后续填充。
 * 地图上画装备获取点（宝箱点位，Canvas 外部图层，经 map.js
 *   _extDraw / extHits 钩子），大师模式点位预留 hardPts 不画。
 * 点击装备点 / 搜索命中 → 装备卡片（类型 / 属性 / 获取宝箱列表 + 定位跳转）
 *
 * ★ 与地图引擎隔离：不侵入 map.js 业务逻辑，只注册绘制/命中回调。
 * ============================================================ */
(function () {
  'use strict';

  /* ---------- 数据 ---------- */
  var WEAPON_ITEMS = (typeof BOTW_WEAPONS !== 'undefined' && BOTW_WEAPONS.items) ? BOTW_WEAPONS.items : {};
  var ARMOR_ITEMS = (typeof BOTW_ARMORS !== 'undefined' && BOTW_ARMORS.items) ? BOTW_ARMORS.items : {};
  /* 装备大分类（用户拍板）：武器 / 盾牌 / 防具（上衣·裤子）。
     weapons.js 里 type='盾' 的 16 件从武器拆出，独立成「盾牌」分类。 */
  var CAT_NAME = { weapon: '武器', shield: '盾牌', armor: '防具' };
  var CAT_COLOR = { weapon: '#d9a066', shield: '#7fb2e0', armor: '#c084d9' };
  var CAT_ORDER = ['weapon', 'shield', 'armor'];

  /* 分类图标（TOTKmap 资源，已复制到本项目 assets/icons/；96x96 透明底彩色剪影，直接使用无需抠白） */
  var ICON_SRC = {
    weapon: 'assets/icons/eq-weapon.png',
    shield: 'assets/icons/eq-shield.png',
    armor: 'assets/icons/eq-armor.png'
  };
  var iconCache = {};   /* kind -> 已加载的 Image（透明底原图） */
  var iconReady = false;
  function loadIcons() {
    if (iconReady) return;
    iconReady = true;
    Object.keys(ICON_SRC).forEach(function (kind) {
      var img = new Image();
      img.onload = function () {
        iconCache[kind] = img;   /* 透明底原图，地图与分类行统一直接绘制 */
      };
      img.src = ICON_SRC[kind];
    });
    window.__eIconCache = iconCache; /* 调试钩子（验证图标缓存状态） */
  }
  /* 面板分类行图标：透明底 PNG 原图直接显示（无需抠白/dataURL 替换） */

  /* 展平装备列表：每件一个条目；type='盾' 归入盾牌分类 */
  var ITEMS = [];
  Object.keys(WEAPON_ITEMS).forEach(function (id) {
    var w = WEAPON_ITEMS[id];
    var kind = (w.type === '盾') ? 'shield' : 'weapon';
    ITEMS.push({
      id: id, kind: kind, name: w.name, en: w.en, type: w.type,
      atk: w.atk, def: (w.def != null ? w.def : null), durability: w.durability,
      hard: !!w.hard, obtain: w.obtain || []
    });
  });
  Object.keys(ARMOR_ITEMS).forEach(function (id) {
    var a = ARMOR_ITEMS[id];
    ITEMS.push({
      id: id, kind: 'armor', name: a.name, en: a.en, part: a.part,
      def: a.def, set: a.set, hard: !!a.hard, obtain: a.obtain || []
    });
  });

  var map = null;
  var ready = false;
  /* 勾选状态：{weapon:true, shield:true, armor:true} 默认全开（localStorage 记忆） */
  var K_LAYERS = 'botwmap.equipLayers.v1';
  var eLayers = { weapon: true, shield: true, armor: true };

  /* ---------- 完成态（V1.6.2 手动，localStorage；与宝箱完成态联动） ---------- */
  var K_EQDONE = 'botwmap.equipDone.v1';
  var equipDone = new Set();
  try { JSON.parse(localStorage.getItem(K_EQDONE) || '[]').forEach(function (k) { equipDone.add(k); }); } catch (e) {}
  function persistEqDone() {
    try { localStorage.setItem(K_EQDONE, JSON.stringify(Array.from(equipDone))); } catch (e) {}
  }
  var eqListeners = [];
  var EquipDone = {
    isDone: function (id) { return !!id && equipDone.has(id); },
    /* 标记/取消：标记完成时同步把该装备所有宝箱点（普通 + 大师）也存进「宝箱完成」 */
    toggle: function (it) {
      if (!it || !it.id) return null;
      var st;
      if (equipDone.has(it.id)) { equipDone.delete(it.id); st = { done: false }; }
      else { equipDone.add(it.id); st = { done: true }; }
      persistEqDone();
      var CP = window.ChestPanel;
      if (CP && CP.markPoints) {
        var all = [];
        (it.obtain || []).forEach(function (ob) {
          if (!ob || ob.kind !== 'chest') return;
          (ob.pts || []).forEach(function (p) { all.push(p); });
          (ob.hardPts || []).forEach(function (p) { all.push(p); });
        });
        CP.markPoints(all, st.done);
      }
      eqListeners.forEach(function (fn) { try { fn(); } catch (e) {} });
      return st;
    },
    onChange: function (fn) { eqListeners.push(fn); }
  };

  /* ---------- 面板注入 ---------- */
  function injectPanel() {
    var wrap = document.getElementById('layerList');
    if (!wrap) return;

    var grp = document.createElement('div');
    grp.className = 'lg-group';
    grp.dataset.group = '装备';
    var g = document.createElement('div');
    g.className = 'lg';
    var gLabel = document.createElement('span');
    gLabel.textContent = '装备';
    var gBtn = document.createElement('button');
    gBtn.type = 'button';
    gBtn.className = 'lg-btn';
    gBtn.dataset.group = '装备';
    gBtn.title = '一键勾选 / 取消本组全部装备分类';
    g.appendChild(gLabel); g.appendChild(gBtn);
    grp.appendChild(g);
    var lrows = document.createElement('div');
    lrows.className = 'lrows';
    grp.appendChild(lrows);

    CAT_ORDER.forEach(function (cat) {
      var n = 0;
      ITEMS.forEach(function (it) { if (it.kind === cat) n++; });
      var row = document.createElement('div');
      row.className = 'lrow' + (eLayers[cat] ? ' on' : '');
      row.dataset.ecat = cat;
      row.innerHTML =
        '<img class="eq-lrow-ic" data-kind="' + cat + '" src="' + ICON_SRC[cat] + '" alt="">' +
        '<span class="lname">' + CAT_NAME[cat] + '</span>' +
        '<span class="lprog">' + n + '</span>';
      row.title = '点击整行切换显示 / 隐藏（' + CAT_NAME[cat] + '获取点）';
      row.addEventListener('click', function () {
        eLayers[cat] = !eLayers[cat];
        row.classList.toggle('on', eLayers[cat]);
        try { localStorage.setItem(K_LAYERS, JSON.stringify(eLayers)); } catch (e) {}
        drawExt();
      });
      lrows.appendChild(row);
    });
    /* 插入到任务组之后（#layerList 顶部第 2 位：任务 → 装备 → 探索收集） */
    var first = wrap.firstChild;
    wrap.insertBefore(grp, first ? first.nextSibling : null);

    gBtn.addEventListener('click', function () {
      var allOn = CAT_ORDER.every(function (c) { return eLayers[c]; });
      CAT_ORDER.forEach(function (c) { eLayers[c] = !allOn; });
      grp.querySelectorAll('.lrow[data-ecat]').forEach(function (r) { r.classList.toggle('on', !allOn); });
      try { localStorage.setItem(K_LAYERS, JSON.stringify(eLayers)); } catch (e) {}
      drawExt();
    });
  }

  function drawExt() {
    if (!map || !ready) return;
    map.draw();
  }

  /* 装备获取点：武器层画武器宝箱点，防具层画防具宝箱点；完成态降透明 + 右上角绿勾 */
  function paint(ctx) {
    if (!map) return;
    window.__ePaints = (window.__ePaints || 0) + 1; /* 调试计数 */
    var scale = map.view.scale;
    /* V1.6.2：随缩放自适应（装备图标几乎无透明边距，画布取同视觉的 0.8 倍） */
    var size = Math.round(window.CardLayout.markerSize(scale) * 0.8);
    var on = {};
    CAT_ORDER.forEach(function (c) { if (eLayers[c]) on[c] = true; });
    if (!CAT_ORDER.some(function (c) { return on[c]; })) return;

    var pad = 60;
    var x0 = Math.max(0, (-pad - map.view.x) / scale), y0 = Math.max(0, (-pad - map.view.y) / scale);
    var x1 = Math.min(map.MW, (map.w + pad - map.view.x) / scale), y1 = Math.min(map.MH, (map.h + pad - map.view.y) / scale);

    for (var i = 0; i < ITEMS.length; i++) {
      var it = ITEMS[i];
      if (!on[it.kind]) continue;
      var color = CAT_COLOR[it.kind] || '#888';
      var done = EquipDone.isDone(it.id);
      for (var k = 0; k < it.obtain.length; k++) {
        var ob = it.obtain[k];
        if (!ob || ob.kind !== 'chest' || !ob.pts) continue;
        var pts = ob.pts;
        for (var j = 0; j < pts.length; j++) {
          var px = pts[j];
          if (px[0] < x0 || px[0] > x1 || px[1] < y0 || px[1] > y1) continue;
          var sx = map.view.x + px[0] * scale, sy = map.view.y + px[1] * scale;
          ctx.save();
          ctx.globalAlpha = done ? 0.65 : 1;   /* V1.6.2：完成态降透明 */
          /* 分类图标（武器=剑 / 盾牌=盾 / 防具=衣）；未就绪回退自绘 */
          var ic = iconCache[it.kind];
          if (ic) {
            try { ctx.drawImage(ic, sx - size / 2, sy - size / 2, size, size); }
            catch (e) { if (it.kind === 'shield') drawShield(ctx, sx, sy, size, color); else drawSword(ctx, sx, sy, size, color); }
          } else if (it.kind === 'shield') drawShield(ctx, sx, sy, size, color);
          else if (it.kind === 'armor') drawTunic(ctx, sx, sy, size, color);
          else drawSword(ctx, sx, sy, size, color);
          if (done) window.CardLayout.doneCheck(ctx, sx + size / 2 - 5, sy - size / 2 + 5, 7);
          ctx.restore();
        }
      }
    }
  }

  /* 命中：装备获取点（屏幕 12px 容差），返回 {type:'equip', equip} */
  function hit(mx, my) {
    if (!map) return null;
    var tol = (window.CardLayout.markerSize(map.view.scale) * 0.8 / 2 + 4) / map.view.scale;
    var best = null, bd = 1e18;
    for (var i = 0; i < ITEMS.length; i++) {
      var it = ITEMS[i];
      if (!eLayers[it.kind]) continue;
      for (var k = 0; k < it.obtain.length; k++) {
        var ob = it.obtain[k];
        if (!ob || ob.kind !== 'chest' || !ob.pts) continue;
        for (var j = 0; j < ob.pts.length; j++) {
          var p = ob.pts[j];
          var dx = p[0] - mx, dy = p[1] - my;
          if (Math.abs(dx) <= tol && Math.abs(dy) <= tol) {
            var d = dx * dx + dy * dy;
            if (d < bd) { bd = d; best = it; }
          }
        }
      }
    }
    return best ? { type: 'equip', equip: best, _r: best, _p: null } : null;
  }

  /* 定位：飞到装备首个获取点（或指定点） */
  function locate(it, idx) {
    if (!map || !it) return;
    var ob = it.obtain[0];
    if (!ob || !ob.pts || !ob.pts.length) return;
    var p = ob.pts[idx || 0];
    if (!p) return;
    locateByPoint(p);
  }

  /* 按像素坐标直接定位（供"获取来源"逐个点位跳转；坐标已是像素坐标无需变换） */
  function locateByPoint(p) {
    if (!map || !p) return;
    map.view.scale = Math.min(Math.max(map.view.scale, 1.2), map.maxScale);
    map.view.x = map.w / 2 - p[0] * map.view.scale;
    /* V1.6.1：手机端底部抽屉（55vh）会盖住下半屏，定位时把目标点推到上方 35% 处 */
    var vy = (window.CardLayout && window.CardLayout.isMobile()) ? map.h * 0.35 : map.h / 2;
    map.view.y = vy - p[1] * map.view.scale;
    map.draw();
    if (map.pulseAt) map.pulseAt(p[0], p[1]);   /* V1.6.1 调整g：定位脉冲光圈 */
    /* V1.6.1 调整i：跳转后按新目标点重算避让 */
    var card = document.getElementById('equipCard');
    if (card && !card.classList.contains('hidden') && map.m2s) {
      card.dataset.manual = '';
      var sp = map.m2s(p);
      map.layoutCard(card, sp[0], sp[1]);
    }
  }

  /* 剑形标记（武器）：剑尖朝上 + 护手 + 剑柄 */
  function drawSword(ctx, cx, cy, size, color) {
    var h = size / 2;
    ctx.strokeStyle = 'rgba(255,255,255,.9)';
    /* 剑身（尖朝上） */
    ctx.fillStyle = color;
    ctx.lineWidth = 1;
    ctx.beginPath();
    ctx.moveTo(cx - h * 0.30, cy + h * 0.45);
    ctx.lineTo(cx - h * 0.14, cy - h * 0.55);
    ctx.lineTo(cx, cy - h);
    ctx.lineTo(cx + h * 0.14, cy - h * 0.55);
    ctx.lineTo(cx + h * 0.30, cy + h * 0.45);
    ctx.closePath();
    ctx.fill();
    ctx.stroke();
    /* 护手（横线） */
    ctx.beginPath();
    ctx.moveTo(cx - h * 0.75, cy + h * 0.35);
    ctx.lineTo(cx + h * 0.75, cy + h * 0.35);
    ctx.stroke();
    /* 剑柄 */
    ctx.fillStyle = color;
    ctx.fillRect(cx - h * 0.16, cy + h * 0.38, h * 0.32, h * 0.34);
    ctx.strokeRect(cx - h * 0.16, cy + h * 0.38, h * 0.32, h * 0.34);
    /* 柄尾 */
    ctx.fillRect(cx - h * 0.24, cy + h * 0.72, h * 0.48, h * 0.1);
  }

  /* 盾形标记（防具）：上平 + 下尖盾形 + 中央十字纹章 */
  function drawShield(ctx, cx, cy, size, color) {
    var h = size / 2;
    ctx.fillStyle = color;
    ctx.strokeStyle = 'rgba(255,255,255,.9)';
    ctx.lineWidth = 1;
    ctx.beginPath();
    ctx.moveTo(cx - h * 0.55, cy - h * 0.55);
    ctx.lineTo(cx + h * 0.55, cy - h * 0.55);
    ctx.lineTo(cx + h * 0.55, cy + h * 0.12);
    ctx.quadraticCurveTo(cx + h * 0.55, cy + h * 0.62, cx, cy + h);
    ctx.quadraticCurveTo(cx - h * 0.55, cy + h * 0.62, cx - h * 0.55, cy + h * 0.12);
    ctx.closePath();
    ctx.fill();
    ctx.stroke();
    /* 中央十字纹章（竖十字） */
    ctx.strokeStyle = 'rgba(20,24,28,.85)';
    ctx.lineWidth = 1.2;
    ctx.beginPath();
    ctx.moveTo(cx, cy - h * 0.35);
    ctx.lineTo(cx, cy + h * 0.5);
    ctx.moveTo(cx - h * 0.32, cy - h * 0.02);
    ctx.lineTo(cx + h * 0.32, cy - h * 0.02);
    ctx.stroke();
  }

  /* 上衣形标记（防具 fallback）：矩形衣身 + V 领 + 下摆 */
  function drawTunic(ctx, cx, cy, size, color) {
    var h = size / 2;
    ctx.fillStyle = color;
    ctx.strokeStyle = 'rgba(255,255,255,.9)';
    ctx.lineWidth = 1;
    ctx.beginPath();
    ctx.moveTo(cx - h * 0.55, cy - h * 0.55);
    ctx.lineTo(cx + h * 0.55, cy - h * 0.55);
    ctx.lineTo(cx + h * 0.55, cy + h * 0.2);
    ctx.quadraticCurveTo(cx + h * 0.55, cy + h * 0.7, cx, cy + h);
    ctx.quadraticCurveTo(cx - h * 0.55, cy + h * 0.7, cx - h * 0.55, cy + h * 0.2);
    ctx.closePath();
    ctx.fill();
    ctx.stroke();
    /* V 领 */
    ctx.strokeStyle = 'rgba(20,24,28,.8)';
    ctx.lineWidth = 1.2;
    ctx.beginPath();
    ctx.moveTo(cx - h * 0.2, cy - h * 0.4);
    ctx.lineTo(cx, cy);
    ctx.lineTo(cx + h * 0.2, cy - h * 0.4);
    ctx.stroke();
  }

  /* ---------- 装备卡片 ---------- */
  function esc(s) {
    return String(s == null ? '' : s).replace(/[&<>"']/g, function (c) {
      return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c];
    });
  }

  /* 宝箱类型中文名（V1.6.2：坐标数字玩家无感，改用宝箱类型；数据 chests.js chestTypeCn） */
  function chestTypeCn(p) {
    var dict = (typeof BOTW_CHESTS !== 'undefined' && BOTW_CHESTS.chestTypeCn) ? BOTW_CHESTS.chestTypeCn : {};
    var t = p && p[2];
    return (t && dict[t]) ? dict[t] : '宝箱';
  }

  /* V1.6.1 调整5：hover 预览态机 hidden | preview | fixed（同 quest.js） */
  var pv = { mode: 'hidden', ref: null };
  var previewNow = false;

  function openCard(it) {
    var card = document.getElementById('equipCard');
    var body = document.getElementById('equipCardBody');
    if (!card || !body) return;
    card.dataset.manual = '';   /* V1.6.1 调整i：新卡 → 清除手动态，重新避让 */
    pv.mode = 'fixed'; pv.ref = it;   /* 点击路径默认固定态（onHoverExt 随后覆盖为 preview） */

    var kindCn = CAT_NAME[it.kind] || it.kind;
    var sub = (it.kind === 'weapon' || it.kind === 'shield') ? (it.type || '') : (it.part || '');
    var chipCls = 'tk-chip-' + (it.kind === 'weapon' ? 'main' : it.kind === 'shield' ? 'sub' : 'mini');
    var done = EquipDone.isDone(it.id);

    var h = '';
    /* 头部：分类徽章 + 大师模式徽章 + 关闭（V1.6.2：大师模式徽章从英文名后移到分类行） */
    h += '<div class="tk-grip">';
    h += '<span class="tk-chip ' + chipCls + '">' + esc(kindCn) + (sub ? ' · ' + esc(sub) : '') + '</span>';
    if (it.hard) h += '<span class="q-badge-hard" style="background:rgba(139,92,246,.16);border-color:#8a5cf6;color:#b18cff">大师模式</span>';
    h += '<span class="tk-grip-acts">' +
      '<button type="button" class="tk-close" id="eqCardClose" title="关闭">×</button></span>';
    h += '</div>';
    /* 标题行：装备名 + [复制]（V1.6.2：⧉ 图标改文字，紧跟名字）+ 完成绿勾 */
    h += '<div class="tk-title-row">' +
      '<h3 class="tk-name">' + esc(it.name) + '</h3>' +
      '<button type="button" class="tk-copy" id="eqCopy" title="复制装备名">[复制]</button>' +
      (done ? '<span class="tk-done-tick" title="已完成">✓</span>' : '') +
      '</div>';
    if (it.en) h += '<div class="qcard-en">' + esc(it.en) + '</div>';
    /* 属性（V1.6.2：盾显示 防御 + 耐久，不再误显攻击） */
    if (it.kind === 'weapon' || it.kind === 'shield') {
      if (sub) h += '<div class="tk-kv"><span class="tk-k">类型</span><span class="tk-v">' + esc(sub) + '</span></div>';
      if (it.kind === 'shield') {
        h += '<div class="tk-kv"><span class="tk-k">防御</span><span class="tk-v">' + (it.def != null ? it.def : '<span class="tk-pending">待补充</span>') + '</span></div>';
      } else {
        h += '<div class="tk-kv"><span class="tk-k">攻击</span><span class="tk-v">' + (it.atk != null ? it.atk : '<span class="tk-pending">待补充</span>') + '</span></div>';
      }
      h += '<div class="tk-kv"><span class="tk-k">耐久</span><span class="tk-v">' + (it.durability != null ? it.durability : '<span class="tk-pending">待补充</span>') + '</span></div>';
    } else {
      if (sub) h += '<div class="tk-kv"><span class="tk-k">部位</span><span class="tk-v">' + esc(sub) + '</span></div>';
      h += '<div class="tk-kv"><span class="tk-k">防御</span><span class="tk-v">' + (it.def != null ? it.def : '<span class="tk-pending">待补充</span>') + '</span></div>';
      h += '<div class="tk-kv"><span class="tk-k">套装</span><span class="tk-v">' + (it.set ? esc(it.set) : '<span class="tk-pending">待补充</span>') + '</span></div>';
    }

    /* 获取来源：普通模式宝箱（黄）/ 大师模式宝箱（紫），逐点可定位 + 点击高亮 */
    var normalPts = [], hardPts = [];
    it.obtain.forEach(function (ob) {
      if (!ob || ob.kind !== 'chest') return;
      (ob.pts || []).forEach(function (p) { normalPts.push(p); });
      (ob.hardPts || []).forEach(function (p) { hardPts.push(p); });
    });
    var allPts = normalPts.concat(hardPts);
    function ptsHtml(list, cls, base) {
      return '<div class="flow-pts">' + list.map(function (p, i) {
        return '<span class="fp ' + cls + '" data-eq-pt="' + (base + i) + '" title="点击定位到该宝箱（' + esc(chestTypeCn(p)) + '）">' +
          '<span class="num">' + (i + 1) + '</span><span class="txt">' + esc(chestTypeCn(p)) + '</span></span>';
      }).join('') + '</div>';
    }
    var obtainHtml = '';
    if (normalPts.length) obtainHtml += '<div class="eq-obtain-h">普通模式宝箱<span class="tk-src">' + normalPts.length + ' 处</span></div>' + ptsHtml(normalPts, 'fp-normal', 0);
    if (hardPts.length) obtainHtml += '<div class="eq-obtain-h fp-hard-h">大师模式宝箱<span class="tk-src">' + hardPts.length + ' 处</span></div>' + ptsHtml(hardPts, 'fp-hard', normalPts.length);
    if (!obtainHtml) obtainHtml = '<div class="tk-none">暂无宝箱获取点（任务奖励待补充）</div>';
    h += '<div class="tk-sec"><div class="tk-sec-t">获取来源</div><div class="tk-sec-b">' + obtainHtml + '</div></div>';

    h += '<div class="tk-actions">';
    if (allPts.length) h += '<button type="button" class="btn act" id="eqLocateFirst">定位</button>';
    h += '<button type="button" class="btn done' + (done ? ' is-done' : '') + '" id="eqDone">' + (done ? '取消完成' : '标记完成') + '</button>';
    h += '</div>';

    body.innerHTML = h;
    card.classList.remove('hidden');

    var closer = document.getElementById('eqCardClose');
    if (closer) closer.addEventListener('click', function (e) {
      e.stopPropagation();
      card.classList.add('hidden');
      pv.mode = 'hidden'; pv.ref = null; previewNow = false;
    });
    var cp = document.getElementById('eqCopy');
    if (cp) cp.addEventListener('click', function (e) {
      e.stopPropagation();
      if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(it.name).catch(function () {});
        if (typeof UI !== 'undefined' && UI.toast) UI.toast('已复制：' + it.name);
      }
    });
    var lf = document.getElementById('eqLocateFirst');
    if (lf) lf.addEventListener('click', function (e) {
      e.stopPropagation();
      if (allPts.length) locateByPoint(allPts[0]);
      positionCard();
    });
    /* 获取点：点击定位 + 高亮当前点（V1.6.2） */
    body.querySelectorAll('.fp[data-eq-pt]').forEach(function (btn) {
      btn.addEventListener('click', function (e) {
        e.stopPropagation();
        var p = allPts[+btn.dataset.eqPt];
        if (!p) return;
        body.querySelectorAll('.fp.is-cur').forEach(function (x) { x.classList.remove('is-cur'); });
        btn.classList.add('is-cur');
        locateByPoint(p);
        if (window.SpotMark) window.SpotMark.set(p);   /* V1.6.2：红点脉冲 */
        positionCard();
      });
    });
    /* 标记完成（V1.6.2）：同步把该装备所有宝箱点存进「宝箱完成」 */
    var dn = document.getElementById('eqDone');
    if (dn) dn.addEventListener('click', function (e) {
      e.stopPropagation();
      var st = EquipDone.toggle(it);
      openCard(it);
      if (map) map.draw();
      if (typeof UI !== 'undefined' && UI.toast) {
        UI.toast(st.done ? '已标记完成：' + it.name + '（含 ' + allPts.length + ' 处宝箱）' : '已取消完成：' + it.name);
      }
    });
    positionCard();
  }

  /* 卡片定位：与 quest 一致（点击点附近浮动，手机抽屉交给 CSS） */
  function positionCard() {
    var card = document.getElementById('equipCard');
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

  /* V1.6.1 调整5：hover 预览入口（map.js mousemove 仅变化时回调） */
  function onHoverExt(ext) {
    /* V1.6.1：PC 端悬停不再弹卡预览（改为地图左上角名称条），只有点击才弹卡 */
    if (window.innerWidth > 760) return;
    if (pv.mode === 'fixed') return;
    var mine = ext && ext.type === 'equip' ? ext.equip : null;
    if (mine) {
      if (pv.mode === 'preview' && pv.ref === mine) return;
      previewNow = true;
      openCard(mine);
      pv.mode = 'preview'; pv.ref = mine;
      previewNow = false;
    } else if (pv.mode !== 'hidden') {
      var card = document.getElementById('equipCard');
      if (card) card.classList.add('hidden');
      pv.mode = 'hidden'; pv.ref = null;
    }
  }

  /* V1.6.1 调整g：地图空白命中广播 → 关闭装备卡并复位 pv 态机 */
  window.addEventListener('botw:blankclose', function () {
    var card = document.getElementById('equipCard');
    if (card) card.classList.add('hidden');
    pv.mode = 'hidden'; pv.ref = null; previewNow = false;
  });

  /* ---------- 搜索（装备并入 ui.js 索引） ---------- */
  var equipSearch = null;
  function buildEquipSearch() {
    equipSearch = ITEMS.map(function (it) {
      var first = null;
      for (var k = 0; k < it.obtain.length; k++) {
        if (it.obtain[k] && it.obtain[k].pts && it.obtain[k].pts.length) { first = it.obtain[k].pts[0]; break; }
      }
      return {
        e: it, kind: it.kind, name: it.name, en: it.en,
        catCn: '装备·' + CAT_NAME[it.kind], pts: first,
        region: ''
      };
    });
    window.__equipSearch = equipSearch;
  }

  /* ---------- 对外 ---------- */
  window.EquipPanel = {
    isOn: function (cat) { return !!eLayers[cat]; },
    toggle: function (cat) { eLayers[cat] = !eLayers[cat]; drawExt(); },
    locate: locate,
    openCard: openCard,
    buildEquipSearch: buildEquipSearch,
    EquipDone: EquipDone
  };

  function init() {
    var boot = function () {
      map = window.__botwMap;
      if (!map) { setTimeout(boot, 60); return; }
      try {
        var d = JSON.parse(localStorage.getItem(K_LAYERS) || 'null');
        if (d) { Object.keys(d).forEach(function (k) { eLayers[k] = !!d[k]; }); }
      } catch (e) {}
      injectPanel();
      buildEquipSearch();
      loadIcons();        /* 预加载分类图标（武器剑/盾牌盾/防具衣，抠白缓存） */
      map._extDraw.push(paint);
      map.extHits.push(hit);
      if (map.extHoverHooks) map.extHoverHooks.push(onHoverExt);  /* V1.6.1 调整5：hover 预览 */
      var ec = document.getElementById('equipCard');
      if (window.initCardDrag && ec) window.initCardDrag(ec);     /* V1.6.1 调整i：卡片拖动 */
      EquipDone.onChange(function () { if (map) map.draw(); });   /* V1.6.2：完成态变化重绘地图 */
      /* UI 搜索索引并入装备（buildSearchIndex 读取 __equipSearch 后需重建） */
      if (window._rebuildSearch) window._rebuildSearch();
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
