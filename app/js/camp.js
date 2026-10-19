/* ============================================================
 * 旷野之息互动地图 · 营地刷点图层（V1.6.4 / P4c）
 * ------------------------------------------------------------
 * 怪物营地(血月刷新) × 手持武器(=击杀掉落)外部图层，回答「去哪刷什么武器」。
 * 数据 = data/camp_weapons.js（CAMP_WEAPONS；hot 54 / all 555，hot ⊆ all 按 id）。
 *   - 精选（hot）默认开、全部默认关；全部 = all 去掉 hot 的 501 条（防重复画）
 *   - 一图标=一营地据点（刷点就近 200px 聚簇，不按武器拆图标）；点击出 #campCard
 *   - ★ 莱尼尔并入：actor Enemy_Lynel* 的 11 个营地不画图标（26/28 刷点
 *     与 data.js lynel 23 标记 <100px），点击 lynel 标记时在信息卡追加
 *     「营地掉落」段（信息增强，不重复画）
 *   - 收集态不做：血月重置、WeaponDrop 已证死表（见 handoff §三.2）；
 *     收集进度看装备图鉴 L2（IsGet_Weapon）
 *
 * ★ 与地图引擎隔离：map.js 零改动；照 chest.js 模式注册
 *   map._extDraw / map.extHits，并包装 map._onSelect 拦截 {type:'camp'}
 *   （包装链：camp(最外) → chest → ui.onSelect；标记 __campWrapped 防重入）。
 * ============================================================ */
(function () {
  'use strict';

  /* ---------- 数据 ---------- */
  var CW = (typeof window !== 'undefined' && window.BOTW_TIER_CAMPS && window.BOTW_TIER_CAMPS.all && window.BOTW_TIER_CAMPS.all.length)
    ? window.BOTW_TIER_CAMPS
    : ((typeof CAMP_WEAPONS !== 'undefined') ? CAMP_WEAPONS : null);
  var DEX = (typeof WEAPON_DEX !== 'undefined' && WEAPON_DEX.items) ? WEAPON_DEX.items : {};
  if (!CW || !CW.all || !CW.all.length) return;   /* 数据缺失静默退出（不阻塞站点） */

  var hotIds = {};
  (CW.hot || []).forEach(function (c) { hotIds[c.i] = true; });

  /* ---------- 刷点聚簇：就近 200px 归簇，一图标=一营地据点 ----------
   * 数据按 (actor,武器集) 跨区域聚合，实测 264 条营地刷点跨度 >400px（最大跨全图），
   * 单质心图标会落在无意义位置；聚簇后 hot 127 刷点 -> 112 图标、all 4014 -> 约 2485 图标
   * （站点常态 8687 点无压力）。点击定位跳簇内最近真实刷点。 */
  var CLUSTER_R = 200;
  function clusterLocs(loc) {
    var raw = [];
    (loc || []).forEach(function (p) {
      var best = null, bd = CLUSTER_R * CLUSTER_R;
      for (var i = 0; i < raw.length; i++) {
        var dx = raw[i].x - p[0], dy = raw[i].y - p[1];
        var d = dx * dx + dy * dy;
        if (d < bd) { bd = d; best = raw[i]; }
      }
      if (best) { best.sx += p[0]; best.sy += p[1]; best.n++; best.pts.push(p); }
      else raw.push({ x: p[0], y: p[1], sx: p[0], sy: p[1], n: 1, pts: [p] });
    });
    return raw.map(function (c) {
      var pt = [c.sx / c.n, c.sy / c.n];
      var near = c.pts[0], bd = 1e18;
      c.pts.forEach(function (p) {
        var d = (p[0] - pt[0]) * (p[0] - pt[0]) + (p[1] - pt[1]) * (p[1] - pt[1]);
        if (d < bd) { bd = d; near = p; }
      });
      return { pt: pt, loc: near, n: c.n };
    });
  }

  /* 绘制清单：hot 子层 = hot 营地聚簇；all 子层 = all 去掉 hot（互斥防重画） */
  var SUB = { hot: [], all: [] };
  (CW.hot || []).forEach(function (c) {
    if (isLynelCamp(c)) return;
    clusterLocs(c.loc).forEach(function (cl) {
      SUB.hot.push({ camp: c, pt: cl.pt, loc: cl.loc, cn: cl.n });
    });
  });
  CW.all.forEach(function (c) {
    if (isLynelCamp(c) || hotIds[c.i]) return;
    clusterLocs(c.loc).forEach(function (cl) {
      SUB.all.push({ camp: c, pt: cl.pt, loc: cl.loc, cn: cl.n });
    });
  });
  var SUB_NAME = { hot: '营地刷点·精选', all: '营地刷点·全部' };
  /* 营地图标（map_12 靶心；icon 底部锚在营地据点上，大小随缩放同宝箱） */
  var PIN_ICON = 'assets/icons/camp_target.png';
  var pinImg = new Image();
  pinImg.src = PIN_ICON;
  function cleanWeaponKey(k) {
    return (k || '').replace(/\+\d+$/, '').replace(/-+$/, '');
  }

  /* 悬停名称条文案：名字 + 掉落物（map.js _hoverLabel 对 camp 类型读 _name） */
  var _labelCache = {};
  function campLabel(c) {
    if (_labelCache[c.i]) return _labelCache[c.i];
    var ws = (c.w || []).slice(0, 3).map(function (k) {
      var key = cleanWeaponKey(k);
      var w = DEX[key];
      return (w && (w.name || w.en)) || '';
    }).filter(Boolean).join('/');
    var more = (c.w || []).length > 3 ? ' 等' + c.w.length + '件' : '';
    return (_labelCache[c.i] = (c.m || c.a || '怪物营地') + (ws ? ' · 掉 ' + ws + more : ''));
  }
  var SUB_COLOR = { hot: '#e0703a', all: '#7d8fa6' };   /* hot 橙红 / 普通灰蓝 */
  var SUB_ORDER = ['hot', 'all'];
  /* 导航序号表：全营地按 i 排序（卡片 ‹› 翻页 + #N 序号）；标记 id 按类分组（Boss 序号） */
  var CAMP_ORDER = CW.all.slice().sort(function (a, b) { return a.i - b.i; });
  var CAMP_IDX = {};
  CAMP_ORDER.forEach(function (c, i) { CAMP_IDX[c.i] = i; });

  /* V1.6.6 / P5：tier 混装数据重灌（EnemyTiers.apply 后调用）——重建预计算并重绘，条目对象引用由调用方替换 */
  function reload(data) {
    if (!data || !data.all || !data.all.length) return;
    CW = data;
    hotIds = {};
    (CW.hot || []).forEach(function (c) { hotIds[c.i] = true; });
    var nHot = [], nAll = [];
    _labelCache = {};
    (CW.hot || []).forEach(function (c) {
      if (isLynelCamp(c)) return;
      clusterLocs(c.loc).forEach(function (cl) {
        nHot.push({ camp: c, pt: cl.pt, loc: cl.loc, cn: cl.n });
      });
    });
    CW.all.forEach(function (c) {
      if (isLynelCamp(c) || hotIds[c.i]) return;
      clusterLocs(c.loc).forEach(function (cl) {
        nAll.push({ camp: c, pt: cl.pt, loc: cl.loc, cn: cl.n });
      });
    });
    SUB = { hot: nHot, all: nAll };
    CAMP_ORDER = CW.all.slice().sort(function (a, b) { return a.i - b.i; });
    CAMP_IDX = {};
    CAMP_ORDER.forEach(function (c, i) { CAMP_IDX[c.i] = i; });
    var rows = document.querySelectorAll('#layerList .lrow[data-ccat]');
    Array.prototype.forEach.call(rows, function (r) {
      var sub = r.dataset.ccat;
      var n = (sub === 'hot') ? (CW.hot || []).length : CW.all.length;
      var lp = r.querySelector('.lprog');
      if (lp) lp.textContent = n;
      r.title = '点击切换显示 / 隐藏 ' + SUB_NAME[sub] + '（' + n + ' 个营地）';
    });
    if (ready && map) map.draw();
  }
  window.CampLayer = { reload: reload };
  var MK_BY_CAT = {};
  if (typeof BOTW_DATA !== 'undefined' && BOTW_DATA.markers) {
    BOTW_DATA.markers.forEach(function (m) {
      (MK_BY_CAT[m.cat] = MK_BY_CAT[m.cat] || []).push(m.id);
    });
  }

  /* 莱尼尔营地（全量并入 lynel 标记，不画图标） */
  function isLynelCamp(c) { return !!(c.a && c.a.indexOf('Enemy_Lynel') === 0); }

  /* ---------- 图层开关（hot 默认开 / all 默认关，localStorage 记忆） ---------- */
  var K_LAYERS = 'botwmap.campLayers.v1';
  var cLayers = { hot: true, all: false };
  var map = null;
  var ready = false;

  function drawExt() { if (map && ready) map.draw(); }

  /* ---------- 侧栏：装备组内追加两条子行（DOM 手术，等 equip.js 注入完成） ---------- */
  function injectCampRows() {
    var grp = document.querySelector('#layerList .lg-group[data-group="装备"]');
    if (!grp) { setTimeout(injectCampRows, 120); return; }
    var lrows = grp.querySelector('.lrows');
    if (!lrows || grp.querySelector('.lrow[data-ccat]')) return;

    SUB_ORDER.forEach(function (sub) {
      var n = (sub === 'hot') ? (CW.hot || []).length : CW.all.length;  /* 计数显示营地总数（含莱尼尔并入的） */
      var r = document.createElement('div');
      r.className = 'lrow' + (cLayers[sub] ? ' on' : '');
      r.dataset.ccat = sub;
      r.innerHTML =
        '<img class="lg-ic" src="' + PIN_ICON + '" alt="" loading="lazy">' +
        '<span class="lname">' + SUB_NAME[sub] + '</span>' +
        '<span class="lprog">' + n + '</span>';
      r.title = '点击切换显示 / 隐藏 ' + SUB_NAME[sub] + '（' + n + ' 个营地）';
      r.addEventListener('click', function () {
        cLayers[sub] = !cLayers[sub];
        r.classList.toggle('on', cLayers[sub]);
        try { localStorage.setItem(K_LAYERS, JSON.stringify(cLayers)); } catch (e) {}
        drawExt();
      });
      lrows.appendChild(r);
    });
  }

  /* ---------- 绘制：一营地一图标（程序绘制圆标 + 白色剑形，无图片资产） ---------- */
  function paint(ctx) {
    if (!map) return;
    window.__cPaints = (window.__cPaints || 0) + 1;   /* 调试计数（verify 用） */
    var scale = map.view.scale;
    var any = false;
    SUB_ORDER.forEach(function (s) { if (cLayers[s]) any = true; });
    if (!any) return;

    var pad = 60;
    var x0 = Math.max(0, (-pad - map.view.x) / scale), y0 = Math.max(0, (-pad - map.view.y) / scale);
    var x1 = Math.min(map.MW, (map.w + pad - map.view.x) / scale), y1 = Math.min(map.MH, (map.h + pad - map.view.y) / scale);

    SUB_ORDER.forEach(function (sub) {
      if (!cLayers[sub]) return;
      var color = SUB_COLOR[sub];
      var size = window.CardLayout.markerSize(scale);
      var rad = size / 2 - 2;
      var list = SUB[sub];
      for (var i = 0; i < list.length; i++) {
        var p = list[i].pt;
        if (p[0] < x0 || p[0] > x1 || p[1] < y0 || p[1] > y1) continue;
        var sx = map.view.x + p[0] * scale, sy = map.view.y + p[1] * scale;
        ctx.save();
        if (pinImg.complete && pinImg.naturalWidth) {
          /* 图钉：针尖锚在据点位置，图标体在正上方（同宝箱尺寸随缩放） */
          ctx.drawImage(pinImg, sx - size / 2, sy - size, size, size);
        } else {
          /* 图标未加载兜底：色圆点 */
          ctx.beginPath();
          ctx.arc(sx, sy, rad, 0, Math.PI * 2);
          ctx.fillStyle = color;
          ctx.globalAlpha = 0.92;
          ctx.fill();
          ctx.globalAlpha = 1;
          ctx.lineWidth = 1.5;
          ctx.strokeStyle = 'rgba(255,255,255,.85)';
          ctx.stroke();
        }
        ctx.restore();
      }
    });
  }

  /* ---------- 命中：营地聚簇点（屏幕 12px 容差），返回 {type:'camp', camp, entry} ---------- */
  function hit(mx, my) {
    if (!map) return null;
    var size = window.CardLayout.markerSize(map.view.scale);
    var lift = (size / 2) / map.view.scale;   /* 图钉头部中心在锚点上方半个图标高 */
    var tol = (size / 2 + 4) / map.view.scale;
    var best = null, bd = 1e18;
    SUB_ORDER.forEach(function (sub) {
      if (!cLayers[sub]) return;
      var list = SUB[sub];
      for (var i = 0; i < list.length; i++) {
        var p = list[i].pt;
        var dx = p[0] - mx, dy = (p[1] - lift) - my;
        if (Math.abs(dx) <= tol && Math.abs(dy) <= tol) {
          var d = dx * dx + dy * dy;
          if (d < bd) { bd = d; best = list[i]; }
        }
      }
    });
    return best ? { type: 'camp', camp: best.camp, entry: best, _r: best.camp, _name: campLabel(best.camp), _p: best.loc } : null;
  }

  /* ---------- 定位：飞到营地首个刷点 ---------- */
  function locate(p) {
    if (!map || !p) return;
    map.view.scale = Math.min(Math.max(map.view.scale, 1.2), map.maxScale);
    map.view.x = map.w / 2 - p[0] * map.view.scale;
    var vy = (window.CardLayout && window.CardLayout.isMobile()) ? map.h * 0.35 : map.h / 2;
    map.view.y = vy - p[1] * map.view.scale;
    map.draw();
    if (map.pulseAt) map.pulseAt(p[0], p[1]);
    var card = document.getElementById('campCard');
    if (card && !card.classList.contains('hidden') && map.m2s) {
      card.dataset.manual = '';
      var sp = map.m2s(p);
      map.layoutCard(card, sp[0], sp[1]);
    }
  }

  /* ---------- 营地卡片 ---------- */
  function esc(s) {
    return String(s == null ? '' : s).replace(/[&<>"']/g, function (c) {
      return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c];
    });
  }

  function weaponRow(rawKey) {
    var key = cleanWeaponKey(rawKey);
    var w = DEX[key];
    var label = (w && (w.name || w.en)) || key;          /* dex 兜底链：name → en → key（9 条 name=null） */
    var meta = '';
    if (w) {
      if (w.type === '盾' && w.guard != null) meta = '防御 ' + w.guard;
      else if (w.atk != null) meta = '攻击 ' + w.atk;
    }
    var elem = (w && w.element && { '火': '#e8574a', '冰': '#5fb8e6', '雷': '#b48ce8' }[w.element]) || null;
    var elemHtml = elem
      ? '<span class="q-badge-hard" style="background:' + elem + '22;border-color:' + elem + ';color:' + elem + '">' + esc(w.element) + '</span>'
      : '';
    var stub = (w && w.stub) ? '<span class="q-badge-hard" style="background:rgba(255,255,255,.1);border-color:#666;color:#aaa">待补充</span>' : '';
    return '<div class="fp-camp cp-wlink" data-wid="' + esc(key) + '" role="button" tabindex="0">' +
      '<span class="txt">' + esc(label) + (meta ? ' <i class="cp-wmeta">' + esc(meta) + '</i>' : '') + '</span>' +
      elemHtml + stub + '</div>';
  }

  function openCard(camp, entry) {
    var anchor = (entry && entry.loc) || (camp.loc && camp.loc[0]) || null;
    var card = document.getElementById('campCard');
    var body = document.getElementById('campCardBody');
    if (!card || !body || !camp) return;
    card.dataset.manual = '';
    lastOpen = Date.now();

    /* 互斥：开营地卡时关 宝箱/装备/任务/信息 卡 */
    ['questCard', 'equipCard', 'chestCard', 'infoCard'].forEach(function (id) {
      var el = document.getElementById(id);
      if (el) el.classList.add('hidden');
    });

    var h = '';
    var order = CAMP_IDX[camp.i] != null ? CAMP_IDX[camp.i] : 0;
    h += '<div class="tk-grip">';
    h += '<span class="tk-chip tk-chip-main">怪物营地 #' + (order + 1) + '</span>';
    if (hotIds[camp.i]) h += '<span class="q-badge-hard" style="background:rgba(224,112,58,.16);border-color:#e0703a;color:#f09a68">精选刷点</span>';
    h += '<span class="tk-grip-acts">' +
      (order > 0 ? '<button type="button" class="tk-icon-btn tk-nav" id="cpPrev" title="上一个营地">‹</button>' : '') +
      (order < CAMP_ORDER.length - 1 ? '<button type="button" class="tk-icon-btn tk-nav" id="cpNext" title="下一个营地">›</button>' : '') +
      '<button type="button" class="tk-close" id="cpCardClose" title="关闭">×</button></span>';
    h += '</div>';
    h += '<div class="tk-title-row"><h3 class="tk-name">' + esc(camp.m || camp.a || '怪物营地') + '</h3>' +
      '<button type="button" class="tk-copy" id="cpCopy" title="复制 #序号 + 名称">[复制]</button></div>';
    if (camp.f && camp.f !== camp.m) h += '<div class="qcard-en">' + esc(camp.f) + '</div>';
    /* V1.6.5 修 Q3：t 字段与 m（标题怪物名）逐条相同（609/609），档位列纯重复——
       仅当将来数据出现 t≠m 才渲染；血月升档机制见卡内脚注 */
    if (camp.t && camp.t !== camp.m) h += '<div class="tk-kv"><span class="tk-k">档位</span><span class="tk-v">' + esc(camp.t) + '</span></div>';
    h += '<div class="tk-kv"><span class="tk-k">刷点</span><span class="tk-v">' + ((camp.loc || []).length) + ' 处</span></div>';
    /* V1.6.4 实测反馈：机制提示（法师游荡 / 依盖队据点剧情性 / 血月重抽） */
    var isWizzrobe = !!(camp.a && camp.a.indexOf('Wizzrobe') >= 0);
    var isYiga = !!(camp.a && camp.a.indexOf('Assassin') >= 0);
    if (isWizzrobe) h += '<div class="cp-foot">法师会游荡：找不到时在附近空域绕一圈找；被击杀后要等血月才会回来</div>';
    if (isYiga) h += '<div class="cp-foot">依盖队据点是剧情场景：主线击败可盖大人后不再刷怪，地图上保留仅为刷点参考</div>';
    h += '<div class="cp-foot">档位为静态布点初始档；血月后按击杀积分动态升档（黄金档为大师模式专属），手持武器也可能随血月按档位池重抽</div>';
    var ws = camp.w || [];
    h += '<div class="eq-camp-h">手持武器（= 击杀掉落）</div>';
    if (ws.length) {
      ws.forEach(function (k) { h += weaponRow(k); });
    } else {
      h += '<div class="cp-empty">不掉武器（该 Boss 击杀掉落素材）</div>';
    }
    h += '<div class="tk-actions"><button type="button" class="btn act" id="cpLocate">定位</button></div>';

    body.innerHTML = h;
    card.classList.remove('hidden');

    /* 武器清单直绑（除 document 级委托外的双保险：闭包内逐行 addEventListener） */
    body.querySelectorAll('.cp-wlink[data-wid]').forEach(function (el) {
      el.addEventListener('click', function (ev) {
        ev.stopPropagation();
        card.classList.add('hidden');
        var w = DEX[el.dataset.wid];
        if (w && window.EquipPanel && window.EquipPanel.openCard) window.EquipPanel.openCard(w);
      });
    });
    var cpCp = document.getElementById('cpCopy');
    if (cpCp) cpCp.addEventListener('click', function (e) {
      e.stopPropagation();
      var txt = '#' + (order + 1) + ' ' + (camp.m || camp.a || '怪物营地');
      if (navigator.clipboard && navigator.clipboard.writeText) { navigator.clipboard.writeText(txt); if (typeof UI !== 'undefined' && UI.toast) UI.toast('已复制：' + txt); }
    });
    var pv2 = document.getElementById('cpPrev');
    if (pv2) pv2.addEventListener('click', function (e) { e.stopPropagation(); var n = CAMP_ORDER[order - 1]; if (n) openCard(n, entry); });
    var nx2 = document.getElementById('cpNext');
    if (nx2) nx2.addEventListener('click', function (e) { e.stopPropagation(); var n = CAMP_ORDER[order + 1]; if (n) openCard(n, entry); });
    var closer = document.getElementById('cpCardClose');
    if (closer) closer.addEventListener('click', function (e) {
      e.stopPropagation();
      card.classList.add('hidden');
    });
    var lf = document.getElementById('cpLocate');
    if (lf) lf.addEventListener('click', function (e) {
      e.stopPropagation();
      var p = anchor;
      locate(p);
      if (window.SpotMark) window.SpotMark.set(p);
    });
    /* 卡片定位：统一走 map.layoutCard 避让 */
    if (map && map.m2s) {
      var p0 = anchor;
      if (p0) {
        var sp = map.m2s(p0);
        map.layoutCard(card, sp[0], sp[1]);
      }
    }
  }

  /* 武器清单点击 → 链装备卡片（事件委托：卡片每次重渲染，挂 document 一层） */
  document.addEventListener('click', function (e) {
    var t = e.target && e.target.closest ? e.target.closest('.cp-wlink') : null;
    if (!t || !t.dataset.wid) return;
    e.stopPropagation();
    var cc = document.getElementById('campCard');
    if (cc) cc.classList.add('hidden');
    var w = DEX[t.dataset.wid];
    if (w && window.EquipPanel && window.EquipPanel.openCard) window.EquipPanel.openCard(w);
  });

  /* ---------- 莱尼尔标记信息增强（营地不画图标，信息挂在 lynel 标记卡上） ---------- */
  /* V1.6.4：Boss 专属掉落段——四族标记；只匹配同族营地（修竞技场多点混杂：那个点位
     堆了几十种营地，按位置匹配会把全场武器都拉进来） */
  var ENH_CATS = ['lynel', 'hinox', 'talus', 'molduga'];
  var CAT_ACTOR = {
    lynel: /^Enemy_Lynel/,
    hinox: /^Enemy_Giant/,   /* 西诺克斯 romfs 内部名 = Enemy_Giant（IsDefeat_Enemy_Giant_* 印证）；写 Hinox 匹配 0 条 */
    talus: /^Enemy_Golem/,
    molduga: /^Enemy_(Molduga|Sandworm)/
  };
  function campsNearMarker(px, cat) {
    var re = CAT_ACTOR[cat] || null;
    var out = [];
    CW.all.forEach(function (c) {
      if (re && !(re.test(c.a || ''))) return;   /* 家族过滤：标记只认同族营地 */
      var hit = (c.loc || []).some(function (p) {
        var dx = p[0] - px[0], dy = p[1] - px[1];
        return dx * dx + dy * dy < 100 * 100;
      });
      if (hit) out.push(c);
    });
    return out;
  }

  /* V1.6.5：人工实测映射（上游布点缺失的点位）。Lynel23 = 大师模式/Mod 限定生成的人马
     （普通模式布点源无此条目），老大实测白鬃阶段掉兽神大剑+兽神弓（类型=双手剑+弓，升档类型不变） */
  var LYNEL_OVERRIDES = {
    Lynel23: { w: ['Weapon_Lsword_018', 'Weapon_Bow_032'], note: '该点位为大师模式/Mod 限定生成，上游无布点数据；组合来自游戏实测' }
  };

  function enhanceLynelCard(mk) {
    var camps = campsNearMarker(mk.px || [0, 0], mk.cat);
    var ov = (mk.cat === 'lynel' && LYNEL_OVERRIDES[mk.id]) || null;
    if (!camps.length && !ov) return;
    var ce = document.getElementById('cardExtra');
    if (!ce) return;
    /* V1.6.5 修 Q1：卡片 Title 段已有「#N 怪物名」，段标题不再重复拼一遍 */
    var h = '<div class="cp-lynel-h">营地掉落（手持武器 = 击杀掉落）</div>';
    /* V1.6.6 / P5：掉落段直读当前 tier 营地（camps 武器已按世界档位解析，无需前端推导）；
       Lynel23（mod 点位，上游无布点）走 OVERRIDES 人工实测映射 */
    if (ov) {
      if (ov.note) h += '<div class="cp-empty">' + esc(ov.note) + '</div>';
      (ov.w || []).forEach(function (k) { h += weaponRow(k); });
    } else {
      camps.forEach(function (c) {
        var label = (camps.length > 1 && c.m && c.m !== mk.name) ? c.m : '';
        if (label) h += '<div class="cp-lynel-row"><b>' + esc(label) + '</b>' +
          (c.t && c.t !== c.m ? '<span class="cp-lynel-tier">' + esc(c.t) + '</span>' : '') + '</div>';
        if ((c.w || []).length) {
          (c.w || []).forEach(function (k) { h += weaponRow(k); });
        } else {
          h += '<div class="cp-empty">不掉武器（该 Boss 击杀掉落素材）</div>';
        }
      });
    }
    ce.insertAdjacentHTML('beforeend', h);
    /* 防遮挡：若信息卡盖住了点击的标记，把卡水平推到标记另一侧 */
    try {
      var ic = document.getElementById('infoCard');
      if (ic && map) {
        var sp = map.m2s(mk.px);
        var mrect = map.canvas.getBoundingClientRect();
        var area = ic.parentElement.getBoundingClientRect();
        var r = ic.getBoundingClientRect();
        var mx = mrect.left - area.left + sp[0], my = mrect.top - area.top + sp[1];
        var cx = r.left - area.left;
        if (mx > cx && mx < cx + r.width && my > r.top - area.top && my < r.top - area.top + r.height) {
          var nx = (mx + r.width + 24 < map.w) ? (mx + 24) : (mx - r.width - 24);
          ic.style.left = Math.max(8, Math.round(nx)) + 'px';
        }
      }
    } catch (e) {}
    /* V1.6.4 防御：偶发信息卡落在容器左上角（定位锚失效），越界则扶正到地图中部 */
    var ic2 = document.getElementById('infoCard');
    if (ic2 && map) {
      var r2 = ic2.getBoundingClientRect();
      if (r2.left < 4 && r2.top < 4) {
        ic2.style.left = Math.round(map.w * 0.5) + 'px';
        ic2.style.top = Math.round(map.h * 0.35) + 'px';
      }
    }
  }

  /* ---------- map._onSelect 包装（最外层：camp → chest → ui） ---------- */
  function hookSelect() {
    if (!map || typeof map._onSelect !== 'function') { setTimeout(hookSelect, 120); return; }
    if (!map._onSelect.__chestWrapped) { setTimeout(hookSelect, 120); return; }  /* 等 chest 先包好，保证链序 */
    if (map._onSelect.__campWrapped) return;
    var orig = map._onSelect;
    var wrapped = function (sel) {
      if (sel && sel.ext && sel.ext.type === 'camp') { openCard(sel.ext.camp, sel.ext.entry); return; }
      if (sel && sel.marker && ENH_CATS.indexOf(sel.marker.cat) >= 0) {
        orig.call(map, sel);
        enhanceLynelCard(sel.marker);
        return;
      }
      orig.call(map, sel);
    };
    wrapped.__campWrapped = true;
    map._onSelect = wrapped;
  }

  /* ---------- 关闭流（同 chest：空白广播 / Esc / 点外部） ---------- */
  function closeCard() {
    var card = document.getElementById('campCard');
    if (card) card.classList.add('hidden');
  }
  window.addEventListener('botw:blankclose', closeCard);
  document.addEventListener('keydown', function (e) { if (e.key === 'Escape') closeCard(); });
  var lastOpen = 0;
  document.addEventListener('click', function (e) {
    var card = document.getElementById('campCard');
    if (!card || card.classList.contains('hidden')) return;
    var el = e.target;
    if (card.contains(el)) return;
    if (card._dragged) return;
    if (el && el.closest && el.closest('#campCard')) return;
    if (map && map.canvas && map.canvas.contains(el) && Date.now() - lastOpen < 400) return;
    closeCard();
  });

  /* ---------- 对外（verify / 调试钩子） ---------- */
  /* V1.6.6：全开 / 全关营地两子类（供图层面板「全选 / 清空」联动调用） */
  function setAllCamp(on) {
    SUB_ORDER.forEach(function (s) { cLayers[s] = !!on; });
    try { localStorage.setItem(K_LAYERS, JSON.stringify(cLayers)); } catch (e) {}
    var w = document.getElementById('layerList');
    if (w) w.querySelectorAll('.lrow[data-ccat]').forEach(function (r) { r.classList.toggle('on', !!on); });
    drawExt();
  }

  window.CampPanel = {
    isOn: function (sub) { return !!cLayers[sub]; },
    toggle: function (sub) { if (sub in cLayers) { cLayers[sub] = !cLayers[sub]; drawExt(); } },
    setAll: setAllCamp,
    openCard: openCard,
    locate: locate,
    lynelCamps: function () { return CW.all.filter(isLynelCamp).length; },
    campsNearMarker: campsNearMarker,
    reload: reload
  };

  /* ---------- init ---------- */
  function init() {
    var boot = function () {
      map = window.__botwMap;
      if (!map) { setTimeout(boot, 60); return; }
      try {
        var d = JSON.parse(localStorage.getItem(K_LAYERS) || 'null');
        if (d && typeof d === 'object') {
          Object.keys(cLayers).forEach(function (k) {
            cLayers[k] = (typeof d[k] === 'boolean') ? d[k] : cLayers[k];
          });
        }
      } catch (e) {}
      injectCampRows();
      map._extDraw.push(paint);
      map.extHits.push(hit);
      var cc = document.getElementById('campCard');
      if (window.initCardDrag && cc) window.initCardDrag(cc);
      hookSelect();
      ready = true;
      if (window.console && console.log) console.log('[BOTWmap] camp.js v3 ready（图层钩子已注册）');
      map.draw();
      /* V1.6.6 / P5 时序保障：boot 完成后兜底刷新 tier 营地数据（若 EnemyTiers 已就绪，触发一次 apply） */
      if (window.EnemyTiers && window.EnemyTiers.apply) {
        window.EnemyTiers.apply();
      } else if (window.BOTW_TIER_CAMPS && window.BOTW_TIER_CAMPS.all) {
        reload(window.BOTW_TIER_CAMPS);
      }
    };
    if (document.readyState === 'loading') {
      document.addEventListener('DOMContentLoaded', boot, { once: true });
    } else {
      boot();
    }
  }
  init();
})();
