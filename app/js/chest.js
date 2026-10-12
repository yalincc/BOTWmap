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
 * ★ 宝箱无存档逐项标志：本版只做图层与卡片，不做手动标记。
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
  /* V1.6.1 调整k：普通宝箱 marker 用原宝箱图标（DLC/大师模式保持色块不变） */
  var chestImg = new Image();
  chestImg.src = 'assets/icons/icons_14.png';

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
      /* 普通宝箱用原始宝箱图标 icons_14.png；DLC/大师模式沿用 dot 徽章 */
      var iconHtml = (sub === 'normal')
        ? '<img class="lg-ic" src="assets/icons/icons_14.png" alt="" loading="lazy">'
        : '<span class="dot" style="background:' + SUB_COLOR[sub] + '"></span>';
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

  /* ---------- Canvas 绘制（条件：三子开关独立，无父行） ---------- */
  function paint(ctx) {
    if (!map) return;
    var scale = map.view.scale;
    var dot = scale < 0.12;
    var size = dot ? 8 : 20;
    var any = false;
    SUB_ORDER.forEach(function (sub) { if (cLayers[sub]) any = true; });
    if (!any) return;

    var pad = 60;
    var x0 = Math.max(0, (-pad - map.view.x) / scale), y0 = Math.max(0, (-pad - map.view.y) / scale);
    var x1 = Math.min(map.MW, (map.w + pad - map.view.x) / scale), y1 = Math.min(map.MH, (map.h + pad - map.view.y) / scale);

    SUB_ORDER.forEach(function (sub) {
      if (!cLayers[sub]) return;
      var list = SUB[sub], color = SUB_COLOR[sub];
      for (var i = 0; i < list.length; i++) {
        var entry = list[i];
        var px = entry.pt;
        if (px[0] < x0 || px[0] > x1 || px[1] < y0 || px[1] > y1) continue;
        var sx = map.view.x + px[0] * scale, sy = map.view.y + px[1] * scale;
        ctx.save();
        if (dot) {
          ctx.beginPath();
          ctx.arc(sx, sy, size / 2, 0, Math.PI * 2);
          ctx.fillStyle = color;
          ctx.fill();
          ctx.strokeStyle = 'rgba(255,255,255,.6)';
          ctx.lineWidth = 1;
          ctx.stroke();
        } else if (sub === 'normal' && chestImg.complete && chestImg.naturalWidth) {
          /* 普通宝箱：icons_14.png 原图（屏幕尺寸 size，低缩放 dot 模式仍走上方圆点） */
          ctx.drawImage(chestImg, sx - size / 2, sy - size / 2, size, size);
        } else {
          var h = size / 2;
          ctx.fillStyle = color;
          ctx.strokeStyle = 'rgba(255,255,255,.92)';
          ctx.lineWidth = 1.5;
          ctx.beginPath();
          if (ctx.roundRect) ctx.roundRect(sx - h, sy - h, size, size, 5);
          else ctx.rect(sx - h, sy - h, size, size);
          ctx.fill();
          ctx.stroke();
          ctx.fillStyle = '#14181c';
          ctx.font = '700 11px "PingFang SC","Microsoft YaHei",sans-serif';
          ctx.textAlign = 'center';
          ctx.textBaseline = 'middle';
          ctx.fillText('箱', sx, sy + 1);
        }
        ctx.restore();
      }
    });
  }

  /* 命中：宝箱点（屏幕 12px 容差），返回 {type:'chest', item, pt, hard} */
  function hit(mx, my) {
    if (!map) return null;
    var tol = 12 / map.view.scale;
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
    map.view.y = map.h / 2 - p[1] * map.view.scale;
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

    var coord = map.gameCoord(p);
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
    } else if (!entry.hard) {
      badgeHtml = '<span class="q-badge-hard" style="background:rgba(255,255,255,.1);border-color:#666;color:#aaa">DLC（归属待确认）</span>';
    }

    var h = '';
    h += '<div class="tk-grip">';
    h += '<span class="tk-chip ' + (entry.hard ? 'tk-chip-mini' : 'tk-chip-main') + '">' + (entry.hard ? '大师模式宝箱' : '宝箱') + '</span>';
    if (badgeHtml) h += badgeHtml;
    h += '<span class="tk-grip-acts">' +
      '<button type="button" class="tk-icon-btn" id="chCopy" title="复制内容物名">⧉</button>' +
      '<button type="button" class="tk-close" id="chCardClose" title="关闭">×</button></span>';
    h += '</div>';
    h += '<div class="tk-title-row"><h3 class="tk-name">' + esc(cnName(it)) + '</h3></div>';
    if (it.en) h += '<div class="qcard-en">' + esc(it.en) + '</div>';
    h += '<div class="tk-kv"><span class="tk-k">宝箱类型</span><span class="tk-v">' +
      esc(CHEST_TYPE_CN[p[2]] || p[2] || '—') + '</span></div>';
    if (coord) h += '<div class="tk-kv"><span class="tk-k">坐标</span><span class="tk-v">' +
      '<span class="co-link" data-ch-locate="1" title="点击定位到该宝箱">X ' + coord[0] + ' · Z ' + coord[1] + '</span></span></div>';
    var totalN = entry.hard ? (it.hardChestCount || 0) : (it.chestCount || 0);
    h += '<div class="tk-kv"><span class="tk-k">数量</span><span class="tk-v">' +
      (entry.hard ? '大师模式 ' + totalN + ' 处' : '普通 ' + totalN + ' 处') + '</span></div>';
    h += '<div class="tk-actions"><button type="button" class="btn act" id="chLocate">定位</button></div>';

    body.innerHTML = h;
    card.classList.remove('hidden');

    var closer = document.getElementById('chCardClose');
    if (closer) closer.addEventListener('click', function (e) {
      e.stopPropagation();
      card.classList.add('hidden');
      pv.mode = 'hidden'; pv.ref = null; previewNow = false;
    });
    var cp = document.getElementById('chCopy');
    if (cp) cp.addEventListener('click', function (e) {
      e.stopPropagation();
      if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(cnName(it)).catch(function () {});
      }
    });
    body.querySelectorAll('[data-ch-locate]').forEach(function (el) {
      el.addEventListener('click', function (e) {
        e.preventDefault();
        e.stopPropagation();
        locate(p);
        positionCard();
      });
    });
    var lf = document.getElementById('chLocate');
    if (lf) lf.addEventListener('click', function (e) {
      e.stopPropagation();
      locate(p);
      positionCard();
    });
    positionCard();
  }

  /* 卡片定位：统一走 map.layoutCard（避让 + 手机抽屉交给 CSS） */
  function positionCard() {
    var card = document.getElementById('chestCard');
    if (!card || !map) return;
    if (card.dataset.manual === '1') return;   /* 手动拖过：保持手动态位 */
    var sx, sy;
    if (previewNow && typeof map._hoverSX === 'number') { sx = map._hoverSX; sy = map._hoverSY; }
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
