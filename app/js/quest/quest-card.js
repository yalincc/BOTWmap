/* ============================================================
 * 旷野之息互动地图 · 任务板块 · M3 · 任务卡片
 * ------------------------------------------------------------
 * 卡片渲染 + 交互：分类徽章 / #N 编号 / 位置名 / 前置·后续 /
 * 任务要点 / 流程地点（位置名+类型色）/ 底部（定位·连线·标记完成）/
 * 上一下一导航 / 复制 / 拖动 / 空白与 Esc 关闭 / hover 预览态机。
 * 对外：window.QuestCard
 * ============================================================ */
(function (global) {
  'use strict';

  var D = global.QuestData;
  var QUESTS = D ? D.QUESTS : [];

  function M() { return global.__botwMap || null; }

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
    var map = M();
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

    var done = global.QuestDone.isDone(q.key);
    var src = global.QuestDone.srcOf(q.key);
    /* V1.6.2：位置名（游戏内无「坐标」概念，不显示 X/Z）——
       取首个带 note 的流程点，**剥掉末尾动作词**（note 格式「地名·动作」；
       不能按第一个「·」切：神庙名如「拉丘·乌罗神庙」本身含「·」）；
       无 note（未补地名的任务）→ 不显示位置行，交由下方「流程地点」呈现 */
    var place = null;
    var PLACE_ACTS = ['接取', '交付', '对话', '战斗', '前往'];
    (q.flowPts || []).forEach(function (f) {
      if (place || !f.note) return;
      var n = String(f.note), idx = n.lastIndexOf('·');
      if (idx > 0 && PLACE_ACTS.indexOf(n.slice(idx + 1)) >= 0) n = n.slice(0, idx);
      place = { name: n.trim(), px: [f.gx * 2 + 12000, f.gz * 2 + 10000] };
    });
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
    h += '<span class="tk-chip ' + chipCls + '">' + D.CAT_NAME[q.cat] + '</span>';
    /* V1.6.1：DLC 徽章（仿 q-badge-hard 胶囊；DLC1 紫 / DLC2 暗金 / 联动 青绿） */
    var dlg = D.dlcBadgeOf(q);
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
    if (place) h += '<div class="tk-mrow"><span class="tk-mk">位置</span><span class="co-link" data-q-locate="1" title="点击定位到该地点">' + esc(place.name) + '</span></div>';
    h += '</div>';
    /* 前置 / 解锁（key 关联制：渲染 q.reqs / q.unlockList，空 → 「无」；
       ★ 样式统一：同字号同色，仅链接语义区分；前置上方加分割线
       ★ V1.6.5：label 缩短为 2 字，严格守住 44px 对齐基线（完整语义在 hover title）） */
    h += '<div class="tk-kv tk-kv-div"><span class="tk-k">前置</span><span class="tk-v">' +
      linkList(q.reqs, '点击查看前置任务') + '</span></div>';
    h += '<div class="tk-kv"><span class="tk-k">解锁</span><span class="tk-v">' +
      linkList(q.unlockList, '点击查看后续任务') + '</span></div>';
    /* 任务要点区（TOTKmap 结构：要点正文 + 接取/注意/奖励 子项）
       ★ 注意：q.note 为攻略注意事项，数据补齐后有则显示，无则不显示该行 */
    var tips = '';
    if (q.desc) tips += '<div class="tk-sec-b">' + esc(q.desc).replace(/\n/g, '<br>') + '</div>';
    if (q.npc) tips += '<div class="tk-kv"><span class="tk-k">接取</span><span class="tk-v">对话' + esc(q.npc) + '</span></div>';
    if (q.note) tips += '<div class="tk-kv"><span class="tk-k">注意</span><span class="tk-v">' + esc(q.note).replace(/\n/g, '<br>') + '</span></div>';
    if (q.reward) tips += '<div class="tk-kv"><span class="tk-k">奖励</span><span class="tk-v">' + esc(q.reward) + '</span></div>';
    if (tips) h += '<div class="tk-sec"><div class="tk-sec-t">任务要点</div>' + tips + '</div>';
    /* 流程地点（V1.6.2：优先用 flowPts —— 显示「位置名 + 类型」，不再显示玩家无感的游戏坐标） */
    var flowList = (q.flowPts && q.flowPts.length)
      ? q.flowPts.map(function (f) { return { px: [f.gx * 2 + 12000, f.gz * 2 + 10000], type: f.type || 'other', note: f.note || '' }; })
      : (q.pts || []).map(function (p) { return { px: p, type: 'other', note: '' }; });
    if (flowList.length) {
      var QNUM = ['①','②','③','④','⑤','⑥','⑦','⑧','⑨','⑩','⑪','⑫','⑬','⑭','⑮','⑯','⑰','⑱','⑲','⑳'];
      var typeCn = (global.QuestFlow && global.QuestFlow.typeCn) ? global.QuestFlow.typeCn : function (t) { return t; };
      var fpts = flowList.map(function (f, i) {
        var label = f.note || typeCn(f.type);
        return '<span class="fp fp-t-' + f.type + '" data-q-pt="' + i + '" title="' + typeCn(f.type) + ' · 点击定位">' +
          '<span class="num">' + (QNUM[i] || (i + 1)) + '</span><span class="txt">' + esc(label) + '</span></span>';
      }).join('');
      h += '<div class="tk-sec"><div class="tk-sec-t">流程地点<span class="tk-src">' + flowList.length + ' 处</span></div><div class="flow-pts">' + fpts + '</div></div>';
    }
    /* 完成状态来源 */
    h += '<div class="qcard-src">' + (src === 'save' ? '完成状态：存档自动同步（游戏内为准）' :
      src === 'manual' ? '完成状态：手动标记' : '完成状态：未标记（无存档标志，可手动标记）') + '</div>';
    /* 底部操作 */
    h += '<div class="tk-actions">';
    if (q.pts && q.pts.length) {
      h += '<button type="button" class="btn act" id="qcardLocate">定位</button>';
    } else {
      /* V1.6.3（TOTKmap 手册 §9.2）：无坐标时按钮【置灰 + title 说明】，不能隐藏
         —— 隐藏会让玩家以为功能缺失。locate() 在无 pts 时本来就会安全 return。 */
      h += '<button type="button" class="btn act is-off" id="qcardLocate" disabled title="' +
        esc(q.noPlaceReason || '本任务在地图上无固定坐标，请用下方「流程地点」定位') + '">定位</button>';
    }
    /* V1.6.3：连线按【去重后】的流程点坐标数判断（手册 §7：L1 = 去重后 ≥2 个不同坐标）。
       只看 flowPts 条数会给「两点同坐标」的任务（side_92/95/133）画出一条零长度线。 */
    var flowOn = !!(global.QuestFlow && global.QuestFlow.isOn(q.key));
    var flowN = (global.QuestFlow && global.QuestFlow.pointsOf)
      ? global.QuestFlow.pointsOf(q).length
      : (q.flowPts ? q.flowPts.length : 0);
    if (flowN >= 2) {
      h += '<button type="button" class="btn act tk-ghost' + (flowOn ? ' is-on' : '') + '" id="qcardFlow">' +
        (flowOn ? '关闭连线' : '任务连线') + '</button>';
    }
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
      global.QuestCanvas.locate(q);
      positionCard();
    });
    /* 位置名链接定位（定位到该地点 + 红点脉冲） */
    body.querySelectorAll('[data-q-locate]').forEach(function (el) {
      el.addEventListener('click', function (e) {
        e.preventDefault();
        e.stopPropagation();
        if (!place || !map) return;
        var p = place.px;
        map.view.scale = Math.min(Math.max(map.view.scale, 1.2), map.maxScale);
        map.view.x = map.w / 2 - p[0] * map.view.scale;
        map.view.y = map.h / 2 - p[1] * map.view.scale;
        map.draw();
        if (map.pulseAt) map.pulseAt(p[0], p[1]);
        if (global.SpotMark) global.SpotMark.set(p);
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
    /* 流程地点定位（V1.6.2：点击高亮 + 红点脉冲） */
    body.querySelectorAll('[data-q-pt]').forEach(function (el) {
      el.addEventListener('click', function (e) {
        e.preventDefault();
        e.stopPropagation();
        var f = flowList[+el.dataset.qPt];
        if (!f || !map) return;
        body.querySelectorAll('.fp.is-cur').forEach(function (x) { x.classList.remove('is-cur'); });
        el.classList.add('is-cur');
        var p = f.px;
        map.view.scale = Math.min(Math.max(map.view.scale, 1.2), map.maxScale);
        map.view.x = map.w / 2 - p[0] * map.view.scale;
        map.view.y = map.h / 2 - p[1] * map.view.scale;
        map.draw();
        if (map.pulseAt) map.pulseAt(p[0], p[1]);
        if (global.SpotMark) global.SpotMark.set(p);
        positionCard();
      });
    });
    /* 任务连线（V1.6.2）：开关流程连线并刷新按钮态 */
    var fb = document.getElementById('qcardFlow');
    if (fb) fb.addEventListener('click', function (e) {
      e.stopPropagation();
      if (!global.QuestFlow) return;
      global.QuestFlow.toggle(q.key);
      openCard(q);
    });
    /* 标记完成 */
    var dn = document.getElementById('qcardDone');
    if (dn) dn.addEventListener('click', function (e) {
      e.stopPropagation();
      var st = global.QuestDone.toggle(q.key);
      openCard(q);
      if (global._questRefreshRows) global._questRefreshRows();
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
    var map = M();
    var card = document.getElementById('questCard');
    if (!card || !map) return;
    if (card.dataset.manual === '1') return;   /* 手动拖过：保持手动态位 */
    /* 卡片此刻还是 hidden = 正要打开新卡 → 清掉上次定位锚点，改用地图点击点 */
    if (card.classList.contains('hidden') && global.CardLayout) global.CardLayout.clearAnchor();
    /* V1.6.1 调整g：统一走 map.layoutCard 避让定位；预览锚点优先 hover 坐标 */
    var sx, sy;
    if (previewNow && typeof map._hoverSX === 'number') { sx = map._hoverSX; sy = map._hoverSY; }
    /* ★ 优先复用最近一次定位的锚点（卡片内点坐标定位后，外层还会再调一次本函数，
       若此时退回 _lastClick 会把正确的避让结果覆盖掉） */
    else if (global.CardLayout && global.CardLayout.getAnchor()) { var a = global.CardLayout.getAnchor(); sx = a.sx; sy = a.sy; }
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
    var map = M();
    var card = document.getElementById('questCard');
    if (!card || card.classList.contains('hidden')) return;
    var el = e.target;
    if (card.contains(el)) return;
    /* ★ V1.6.3：刚拖完卡片，这一次 click 不关卡（原实现只靠 400ms 阈值，
       拖久了就失效 → 卡片一松手就消失，表现为「拖不动」） */
    if (card._dragged) return;
    if (el && el.closest && el.closest('#questCard')) return;
    if (map && map.canvas && map.canvas.contains(el) && Date.now() - lastOpen < 400) return;
    closeCard();
  });
  document.addEventListener('keydown', function (e) {
    if (e.key === 'Escape') closeCard();
  });
  /* V1.6.1 调整g：地图空白命中广播 → 关闭任务卡并复位 pv 态机 */
  window.addEventListener('botw:blankclose', function () { closeCard(); });

  global.QuestCard = {
    openCard: openCard,
    closeCard: closeCard,
    positionCard: positionCard,
    onHoverExt: onHoverExt
  };
})(window);
