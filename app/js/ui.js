/* ============ 旷野之息互动地图 · UI 逻辑 ============ */
'use strict';

const UI = (() => {
  let map = null;
  let searchIndex = null;

  const $ = id => document.getElementById(id);

  function init(m) {
    map = m;
    buildLayerList();
    buildSearchIndex();
    wireTools();
    initStyle();
    map._onSelect = onSelect;
    map._onCoord = onCoord;
    map._onModeChange = onModeChange;
    map._onCustomChange = () => { saveCustom(); renderStats(); };
  }

  /* ---------- 图层列表 ---------- */
  function buildLayerList() {
    const wrap = $('layerList');
    wrap.innerHTML = '';
    const byKey = {};
    map.data.categories.forEach(c => byKey[c.key] = c);
    for (const [group, keys] of Object.entries(CAT_GROUP)) {
      const g = document.createElement('div');
      g.className = 'lg';
      const gLabel = document.createElement('span');
      gLabel.textContent = group;
      const gBtn = document.createElement('button');
      gBtn.type = 'button';
      gBtn.className = 'lg-btn';
      gBtn.dataset.group = group;
      gBtn.title = '一键勾选 / 取消本组全部图层';
      g.appendChild(gLabel);
      g.appendChild(gBtn);
      wrap.appendChild(g);
      for (const key of keys) {
        const cat = byKey[key];
        if (!cat) continue;
        const cfg = CAT_CFG[key] || {};
        const total = map.data.markers.filter(mk => mk.cat === key).length;
        const row = document.createElement('label');
        row.className = 'lrow';
        row.innerHTML = `
          <input type="checkbox" data-cat="${key}" ${map.enabled.has(key) ? 'checked' : ''}>
          <span class="dot" style="background:${cfg.color || '#888'}"></span>
          <span class="lname">${cat.cn}</span>
          <span class="lprog" id="prog_${key}"></span>
        `;
        row.addEventListener('change', () => {
          const cb = row.querySelector('input');
          if (cb.checked) map.enabled.add(key); else map.enabled.delete(key);
          saveLayers();
          updateLayerList();
          map.draw();
          renderStats();
        });
        wrap.appendChild(row);
      }
    }
    // 分组一键勾选 / 清空
    wrap.addEventListener('click', e => {
      const btn = e.target.closest('.lg-btn');
      if (!btn) return;
      const keys = CAT_GROUP[btn.dataset.group] || [];
      const allOn = keys.length > 0 && keys.every(k => map.enabled.has(k));
      keys.forEach(k => { if (allOn) map.enabled.delete(k); else map.enabled.add(k); });
      wrap.querySelectorAll('input[data-cat]').forEach(cb => {
        if (keys.includes(cb.dataset.cat)) cb.checked = !allOn;
      });
      saveLayers(); updateLayerList(); map.draw(); renderStats();
    });
    $('checkAll').addEventListener('click', () => setAllLayers(true));
    $('uncheckAll').addEventListener('click', () => setAllLayers(false));
    updateLayerList();
  }

  function setAllLayers(on) {
    if (on) map.data.categories.forEach(c => map.enabled.add(c.key));
    else map.enabled.clear();
    document.querySelectorAll('#layerList input').forEach(cb => cb.checked = on);
    saveLayers(); updateLayerList(); map.draw(); renderStats();
  }

  function updateLayerList() {
    let shown = 0;
    map.data.categories.forEach(c => { if (map.enabled.has(c.key)) shown++; });
    $('layerCount').textContent = shown + '/' + map.data.categories.length + ' 图层';
    // 分组按钮状态
    const wrap = $('layerList');
    for (const [group, keys] of Object.entries(CAT_GROUP)) {
      const btn = wrap.querySelector(`.lg-btn[data-group="${group}"]`);
      if (!btn) continue;
      const on = keys.filter(k => map.enabled.has(k)).length;
      btn.textContent = on === keys.length ? '清空本组' : (on === 0 ? '全选本组' : `全选（${on}/${keys.length}）`);
      btn.style.opacity = on === keys.length ? '1' : '0.75';
    }
    // 进度
    for (const cat of map.data.categories) {
      const el = $('prog_' + cat.key);
      if (!el) continue;
      const total = map.data.markers.filter(mk => mk.cat === cat.key).length;
      const done = map.data.markers.filter(mk => mk.cat === cat.key && map.doneSet.has(mk.id)).length;
      el.textContent = done ? `${done}/${total}` : '';
      el.style.color = done === total ? '#7dffa0' : 'var(--text-dim)';
    }
  }

  /* ---------- 统计面板 ---------- */
  function renderStats() {
    const total = map.data.markers.length;
    const done = map.doneSet.size;
    const focus = ['shrine', 'tower', 'beast', 'seed', 'memory', 'treasure'];
    let html = `<h4>收集进度</h4>
      <div class="stat-row"><span class="k">全部标注</span><span class="v">${done} / ${total}</span></div>
      <div class="stat-bar"><i style="width:${pct(done, total)}"></i></div>`;
    for (const key of focus) {
      const cat = map.data.categories.find(c => c.key === key);
      if (!cat) continue;
      const t = map.data.markers.filter(mk => mk.cat === key).length;
      const d = map.data.markers.filter(mk => mk.cat === key && map.doneSet.has(mk.id)).length;
      html += `<div class="stat-row"><span class="k">${cat.cn}</span><span class="v">${d} / ${t} ${pct(d, t, true)}</span></div>`;
      html += `<div class="stat-bar"><i style="width:${pct(d, t)}"></i></div>`;
    }
    $('statsPanel').innerHTML = html;
  }
  function pct(a, b, short) {
    if (!b) return short ? '' : '0%';
    return Math.round(a / b * 100) + '%';
  }

  /* ---------- 搜索 ---------- */
  function buildSearchIndex() {
    searchIndex = map.data.markers
      .filter(mk => mk.name && mk.name.length < 60)
      .map(mk => ({
        id: mk.id, cat: mk.cat, name: mk.name, en: mk.en,
        region: mk.region, tower: mk.tower, px: mk.px,
        catCn: (map.data.categories.find(c => c.key === mk.cat) || {}).cn || mk.cat,
      }));
    const input = $('searchInput');
    let timer = null;
    input.addEventListener('input', () => {
      clearTimeout(timer);
      timer = setTimeout(() => doSearch(input.value), 90);
    });
    input.addEventListener('focus', () => { if (input.value) doSearch(input.value); });
    document.addEventListener('click', e => {
      if (!e.target.closest('.search-wrap')) hideResults();
    });
  }

  function doSearch(q) {
    q = q.trim();
    if (!q) { hideResults(); return; }
    const ql = q.toLowerCase();
    const hits = [];
    for (const it of searchIndex) {
      if (hits.length >= 40) break;
      if (it.name.toLowerCase().includes(ql) || it.en.toLowerCase().includes(ql) ||
          (it.region && it.region.includes(q)) || (it.tower && it.tower.includes(q))) {
        hits.push(it);
      }
    }
    const box = $('searchResults');
    if (!hits.length) {
      box.innerHTML = '<div class="sr-item"><span class="sr-meta">无结果</span></div>';
    } else {
      box.innerHTML = hits.map(h => `
        <div class="sr-item ${map.doneSet.has(h.id) ? 'done' : ''}" data-id="${h.id}">
          <span class="sr-name">${esc(h.name)}</span>
          <span class="sr-meta">${h.catCn}${h.region ? ' · ' + esc(h.region) : ''}</span>
        </div>`).join('');
      box.querySelectorAll('.sr-item[data-id]').forEach(el => {
        el.addEventListener('click', () => {
          const mk = map.data.markers.find(x => x.id === el.dataset.id);
          if (mk) {
            // 先确定缩放，再计算视口偏移
            map.view.scale = Math.min(Math.max(map.view.scale, 1.2), map.maxScale);
            map.view.x = map.w / 2 - mk.px[0] * map.view.scale;
            map.view.y = map.h / 2 - mk.px[1] * map.view.scale;
            map.draw();
            map._selectMarker(mk);
          }
          hideResults();
          $('searchInput').blur();
        });
      });
    }
    box.classList.remove('hidden');
  }
  function hideResults() { $('searchResults').classList.add('hidden'); }
  function esc(s) { return String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;'); }

  /* ---------- 信息卡片 ---------- */
  let currentMarker = null;
  function onSelect(hit) {
    if (!hit) { currentMarker = null; hideInfo(); return; }
    if (hit.custom) {
      currentMarker = null;
      showCustomInfo(hit.custom);
      return;
    }
    currentMarker = hit.marker;
    renderCard();
  }

  function renderCard() {
    const mk = currentMarker;
    if (!mk) return;
    const cat = map.data.categories.find(c => c.key === mk.cat) || {};
    const cfg = CAT_CFG[mk.cat] || {};
    const [gx, gz] = map.gameCoord(mk.px);
    const done = map.doneSet.has(mk.id);
    $('cardCat').textContent = cat.cn || mk.cat;
    $('cardCat').style.borderColor = cfg.color;
    $('cardCat').style.color = cfg.color;
    $('cardName').textContent = mk.name;
    $('cardEn').textContent = mk.en || '';
    $('cardRegion').innerHTML = '区域<b>' + (mk.region || '—') + '</b>';
    $('cardTower').innerHTML = '塔域<b>' + (mk.tower || '—') + '</b>';
    $('cardCoord').innerHTML = '游戏坐标<b>X ' + gx + ' · Z ' + gz + '</b>';
    let extra = '';
    const ex = mk.extra || {};
    if (mk.cat === 'seed' && ex.korok_loc) extra += '<div class="row">位置提示<b>' + esc(ex.korok_loc) + '</b></div>';
    if (mk.cat === 'shrine') {
      if (ex.is_dlc) extra += '<div class="row">类型<b>英杰之诗 DLC 神庙</b></div>';
      if (ex.icon === 'shrine_resurrection.png') extra += '<div class="row">类型<b>复苏神庙</b></div>';
      if (ex.type) extra += '<div class="row">试炼<b>' + esc(ex.type) + '</b></div>';
      if (ex.treasure) extra += '<div class="row">宝藏<b>' + esc(ex.treasure) + '</b></div>';
      if (ex.quest) extra += '<div class="row">神庙任务<b>' + esc(ex.quest) + '</b></div>';
    }
    if (mk.cat === 'memory' && ex.memory_title_en) extra += '<div class="row">回忆<b>#' + ex.memory_no + ' · ' + esc(ex.memory_title_en) + '</b></div>';
    if (mk.cat === 'shrinequest' && ex.quest_en) extra += '<div class="row">任务<b>' + esc(ex.quest_en) + '</b></div>';
    if (mk.cat === 'objective' && ex.sub) extra += '<div class="row">目标<b>' + esc(ex.sub) + '</b></div>';
    if (mk.cat === 'beast') extra += '<div class="row">类型<b>神兽·讨伐</b></div>';
    $('cardExtra').innerHTML = extra;
    $('cardDone').classList.toggle('hidden', done);
    $('cardUndone').classList.toggle('hidden', !done);
    $('cardDelPin').classList.add('hidden');
    $('cardDone').onclick = () => { map.doneSet.add(mk.id); saveDone(); renderCard(); map.draw(); updateLayerList(); renderStats(); toast('已标记完成：' + mk.name); };
    $('cardUndone').onclick = () => { map.doneSet.delete(mk.id); saveDone(); renderCard(); map.draw(); updateLayerList(); renderStats(); toast('已取消完成：' + mk.name); };
    positionCard();
    $('infoCard').classList.remove('hidden');
  }

  function showCustomInfo(c) {
    const [gx, gz] = map.gameCoord(c.px);
    $('cardCat').textContent = '自定义标记';
    $('cardCat').style.borderColor = c.color;
    $('cardCat').style.color = c.color;
    $('cardName').textContent = c.name;
    $('cardEn').textContent = '';
    $('cardRegion').innerHTML = '区域<b>自定义</b>';
    $('cardTower').innerHTML = '坐标<b>X ' + gx + ' · Z ' + gz + '</b>';
    $('cardCoord').innerHTML = '';
    $('cardExtra').innerHTML = c.note ? '<div class="row">备注<b>' + esc(c.note) + '</b></div>' : '';
    $('cardDone').classList.add('hidden');
    $('cardUndone').classList.add('hidden');
    $('cardDelPin').classList.remove('hidden');
    $('cardDelPin').onclick = () => {
      map.customMarkers = map.customMarkers.filter(x => x.id !== c.id);
      saveCustom(); hideInfo(); map.draw();
      toast('已删除自定义标记');
    };
    positionCard();
    $('infoCard').classList.remove('hidden');
  }

  function positionCard() {
    const card = $('infoCard');
    card.style.left = 'auto'; card.style.right = 'auto';
    card.style.left = '14px';
    card.style.top = 'calc(100% - 14px)';
    card.style.top = 'auto';
    card.style.bottom = '14px';
    card.style.maxHeight = '70%';
    card.style.overflowY = 'auto';
  }

  function hideInfo() {
    $('infoCard').classList.add('hidden');
    $('cardDelPin').classList.add('hidden');
  }
  function showInfo() { /* refresh current card */ }

  /* ---------- 坐标读数 ---------- */
  function onCoord(game, mpx) {
    const inMap = mpx[0] >= 0 && mpx[1] >= 0 && mpx[0] <= 24000 && mpx[1] <= 20000;
    $('coordReadout').textContent = inMap
      ? `X: ${game[0]}  Z: ${game[1]}  (${Math.round(mpx[0])}, ${Math.round(mpx[1])})`
      : 'X: --  Z: --（地图外）';
  }

  /* ---------- 工具 ---------- */
  function wireTools() {
    $('btnReset').addEventListener('click', () => { map.fit(); map.draw(); toast('已回到全图'); });
    $('zoomIn').addEventListener('click', () => { map.zoomAt(map.w / 2, map.h / 2, 1.5); });
    $('zoomOut').addEventListener('click', () => { map.zoomAt(map.w / 2, map.h / 2, 1 / 1.5); });
    $('zoomFit').addEventListener('click', () => { map.fit(); map.draw(); });
    wirePinForm();
    $('btnMeasure').addEventListener('click', () => {
      if (map.mode === 'measure') { map.setMode('browse'); }
      else { map.setMode('measure'); }
    });
    $('btnPin').addEventListener('click', () => {
      if (map.mode === 'pin') map.setMode('browse');
      else map.setMode('pin');
    });
    $('btnShare').addEventListener('click', share);
    wireShare();
    $('cardClose').addEventListener('click', hideInfo);
    $('searchInput').addEventListener('keydown', e => {
      if (e.key === 'Escape') { hideResults(); $('searchInput').blur(); }
    });
  }

  /* ---------- 自定义标记表单 ---------- */
  let pinTarget = null, pinColor = '#ff5252';
  function wirePinForm() {
    document.querySelectorAll('#pinColors .sw').forEach(btn => {
      btn.addEventListener('click', () => {
        pinColor = btn.dataset.c;
        document.querySelectorAll('#pinColors .sw').forEach(b => b.classList.remove('sel'));
        btn.classList.add('sel');
      });
    });
    $('pinOk').addEventListener('click', () => {
      if (!pinTarget) return;
      const name = $('pinName').value.trim();
      const mk = map.addCustomMarker(name, pinColor, pinTarget);
      $('pinForm').classList.add('hidden');
      map.setMode('browse');
      pinTarget = null;
      toast('已添加标记：' + (name || '标记'));
      map._selectCustom(mk);
    });
    $('pinCancel').addEventListener('click', () => {
      $('pinForm').classList.add('hidden');
      pinTarget = null;
      map.setMode('browse');
    });
    $('pinName').addEventListener('keydown', e => {
      if (e.key === 'Enter') $('pinOk').click();
      if (e.key === 'Escape') $('pinCancel').click();
    });
    map._onPinClick = (sx, sy, mpx) => {
      pinTarget = mpx;
      pinColor = '#ff5252';
      document.querySelectorAll('#pinColors .sw').forEach((b, i) => b.classList.toggle('sel', i === 0));
      $('pinName').value = '';
      const form = $('pinForm');
      form.classList.remove('hidden');
      const r = map.canvas.getBoundingClientRect();
      form.style.left = Math.min(Math.max(sx - 90, 8), r.width - 200) + 'px';
      form.style.top = Math.min(Math.max(sy - 30, 8), r.height - 180) + 'px';
      $('pinName').focus();
    };
  }

  function onModeChange(mode) {
    $('btnMeasure').classList.toggle('active', mode === 'measure');
    $('btnPin').classList.toggle('active', mode === 'pin');
    const hint = $('modeHint');
    if (mode === 'measure') {
      hint.textContent = '测量模式：点击地图打点，双击结束，Esc 退出';
      hint.classList.remove('hidden');
    } else if (mode === 'pin') {
      hint.textContent = '标记模式：点击地图放置自定义标记，Esc 退出';
      hint.classList.remove('hidden');
    } else {
      hint.classList.add('hidden');
      $('btnMeasure').classList.remove('active');
      $('btnPin').classList.remove('active');
    }
  }

  /* ---------- 分享 ---------- */
  function share() {
    const layers = [...map.enabled].join(',');
    const v = {
      x: Math.round(map.view.x), y: Math.round(map.view.y),
      s: Math.round(map.view.scale * 100) / 100,
    };
    const hash = 'l=' + layers + '&x=' + v.x + '&y=' + v.y + '&s=' + v.s;
    location.hash = hash;
    const url = location.href.split('#')[0] + '#' + hash;
    $('shareUrl').value = url;
    $('shareBox').classList.remove('hidden');
    $('shareUrl').select();
  }
  function wireShare() {
    $('sbClose').addEventListener('click', () => $('shareBox').classList.add('hidden'));
    $('sbCopy').addEventListener('click', () => {
      const url = $('shareUrl').value;
      if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(url).then(() => {
          toast('分享链接已复制');
          $('shareBox').classList.add('hidden');
        });
      } else {
        $('shareUrl').select();
        document.execCommand('copy');
        toast('分享链接已复制');
        $('shareBox').classList.add('hidden');
      }
    });
    $('shareUrl').addEventListener('keydown', e => { if (e.key === 'Escape') $('sbClose').click(); });
  }

  function applyHash() {
    if (!location.hash) return;
    const params = new URLSearchParams(location.hash.slice(1));
    const l = params.get('l');
    if (l) {
      map.enabled.clear();
      l.split(',').forEach(k => { if (CAT_CFG[k]) map.enabled.add(k); });
      document.querySelectorAll('#layerList input').forEach(cb => cb.checked = map.enabled.has(cb.dataset.cat));
      updateLayerList(); renderStats();
    }
    const x = parseFloat(params.get('x')), y = parseFloat(params.get('y')), s = parseFloat(params.get('s'));
    if (!isNaN(x) && !isNaN(y) && !isNaN(s)) {
      map.view.x = x; map.view.y = y;
      map.view.scale = Math.min(Math.max(s, map.minScale), map.maxScale);
      map._viewApplied = true;
    }
    map.draw();
  }

  /* ---------- 持久化 ---------- */
  const K_DONE = 'botwmap.done.v1', K_CUSTOM = 'botwmap.custom.v1', K_LAYERS = 'botwmap.layers.v2';
  function loadState() {
    try {
      const d = JSON.parse(localStorage.getItem(K_DONE) || '[]');
      d.forEach(id => map.doneSet.add(id));
    } catch (e) {}
    try {
      map.customMarkers = JSON.parse(localStorage.getItem(K_CUSTOM) || '[]');
    } catch (e) { map.customMarkers = []; }
    const def = ['shrine', 'tower'];
    try {
      const l = JSON.parse(localStorage.getItem(K_LAYERS) || 'null');
      if (Array.isArray(l) && l.length) l.forEach(k => { if (CAT_CFG[k]) map.enabled.add(k); });
      else def.forEach(k => map.enabled.add(k));
    } catch (e) { def.forEach(k => map.enabled.add(k)); }
    // 同步复选框与图层计数
    document.querySelectorAll('#layerList input').forEach(cb => cb.checked = map.enabled.has(cb.dataset.cat));
    updateLayerList();
    renderStats();
  }
  function saveDone() {
    try { localStorage.setItem(K_DONE, JSON.stringify([...map.doneSet])); } catch (e) {}
  }
  function saveCustom() {
    try { localStorage.setItem(K_CUSTOM, JSON.stringify(map.customMarkers)); } catch (e) {}
  }
  function saveLayers() {
    try { localStorage.setItem(K_LAYERS, JSON.stringify([...map.enabled])); } catch (e) {}
  }

  /* ---------- 地名样式（顶栏⚙️设置弹层） ---------- */
  const K_STYLE = 'botwmap.style.v2'; // v2：新默认（米色底深褐字）+ 字号档位
  const DEFAULT_STYLE = { bg: [242, 232, 213], bgA: 0.6, tx: [92, 58, 30], txA: 0.92, fsz: 1, sc: [0, 0, 0], sw: 0 };
  const FONT_SIZES = { small: 0.85, medium: 1, large: 1.15 };
  function loadStyle() {
    try {
      const s = JSON.parse(localStorage.getItem(K_STYLE) || 'null');
      if (s && Array.isArray(s.bg) && Array.isArray(s.tx)) {
        map.regionStyle = Object.assign({}, DEFAULT_STYLE, s);
      }
    } catch (e) {}
  }
  function saveStyle() {
    try { localStorage.setItem(K_STYLE, JSON.stringify(map.regionStyle)); } catch (e) {}
  }
  function hexToRgb(hex) {
    const m = /^#?([0-9a-f]{6})$/i.exec((hex || '').trim());
    if (!m) return null;
    const n = parseInt(m[1], 16);
    return [(n >> 16) & 255, (n >> 8) & 255, n & 255];
  }
  function rgbToHex(rgb) {
    return '#' + rgb.map(v => (v & 255).toString(16).padStart(2, '0')).join('');
  }
  function initStyle() {
    loadStyle();
    const box = $('settingsBox');
    const openBox = () => { box.classList.remove('hidden'); syncStyle(); };
    const closeBox = () => box.classList.add('hidden');
    $('btnSettings').addEventListener('click', openBox);
    $('stClose').addEventListener('click', closeBox);
    window.addEventListener('keydown', e => {
      if (e.key === 'Escape' && !box.classList.contains('hidden')) closeBox();
    });

    const syncStyle = () => {
      const st = map.regionStyle;
      const setOpt = (sel, v) => {
        document.querySelectorAll(`#${sel} .st-opt`).forEach(b => b.classList.toggle('sel', b.dataset.v === v));
      };
      // 文字颜色
      const txHex = rgbToHex(st.tx);
      if (txHex === rgbToHex(DEFAULT_STYLE.tx)) setOpt('stTxOptions', 'default');
      else if (txHex === '#000000') setOpt('stTxOptions', '#000000');
      else if (txHex === '#ffffff') setOpt('stTxOptions', '#ffffff');
      else { setOpt('stTxOptions', '__custom'); $('stTxColor').value = txHex; }
      // 底色
      const bgHex = rgbToHex(st.bg);
      if (bgHex === rgbToHex(DEFAULT_STYLE.bg)) setOpt('stBgOptions', 'default');
      else if (bgHex === '#000000') setOpt('stBgOptions', '#000000');
      else if (bgHex === '#ffffff') setOpt('stBgOptions', '#ffffff');
      else { setOpt('stBgOptions', '__custom'); $('stBgColor').value = bgHex; }
      // 大小
      let sz = 'medium';
      for (const k in FONT_SIZES) if (Math.abs(FONT_SIZES[k] - (st.fsz || 1)) < 0.01) sz = k;
      setOpt('stSizeOptions', sz);
      // 透明度
      $('stTxAlpha').value = Math.round(st.txA * 100);
      $('stTxAlphaV').textContent = Math.round(st.txA * 100) + '%';
      $('stBgAlpha').value = Math.round(st.bgA * 100);
      $('stBgAlphaV').textContent = Math.round(st.bgA * 100) + '%';
      // 描边
      const scHex = st.sc ? rgbToHex(st.sc) : '';
      if (!st.sw || st.sw <= 0 || !scHex) setOpt('stStrokeOptions', 'none');
      else if (scHex === '#000000') setOpt('stStrokeOptions', '#000000');
      else if (scHex === '#ffffff') setOpt('stStrokeOptions', '#ffffff');
      else { setOpt('stStrokeOptions', '__custom'); $('stStrokeColor').value = scHex; }
      $('stStrokeWidth').value = st.sw || 0;
      $('stStrokeWidthV').textContent = (st.sw || 0) % 1 === 0 ? (st.sw || 0) : (st.sw || 0).toFixed(1);
    };
    function applyStyle(fn) {
      fn(map.regionStyle);
      saveStyle();
      map.draw();
      syncStyle();
    }
    // 文字颜色
    document.querySelectorAll('#stTxOptions .st-opt[data-v]').forEach(b => {
      if (b.dataset.v === 'default') b.addEventListener('click', () => applyStyle(st => { st.tx = DEFAULT_STYLE.tx.slice(); }));
      else b.addEventListener('click', () => applyStyle(st => { const c = hexToRgb(b.dataset.v); if (c) st.tx = c; }));
    });
    $('stTxColor').addEventListener('input', e => applyStyle(st => { const c = hexToRgb(e.target.value); if (c) st.tx = c; }));
    // 文字大小
    document.querySelectorAll('#stSizeOptions .st-opt').forEach(b => {
      b.addEventListener('click', () => applyStyle(st => { st.fsz = FONT_SIZES[b.dataset.v] || 1; }));
    });
    // 底色
    document.querySelectorAll('#stBgOptions .st-opt[data-v]').forEach(b => {
      if (b.dataset.v === 'default') b.addEventListener('click', () => applyStyle(st => { st.bg = DEFAULT_STYLE.bg.slice(); }));
      else b.addEventListener('click', () => applyStyle(st => { const c = hexToRgb(b.dataset.v); if (c) st.bg = c; }));
    });
    $('stBgColor').addEventListener('input', e => applyStyle(st => { const c = hexToRgb(e.target.value); if (c) st.bg = c; }));
    // 文字/底色透明度
    $('stTxAlpha').addEventListener('input', e => applyStyle(st => { st.txA = Math.min(1, Math.max(0, e.target.value / 100)); }));
    $('stBgAlpha').addEventListener('input', e => applyStyle(st => { st.bgA = Math.min(1, Math.max(0, e.target.value / 100)); }));
    // 描边颜色
    document.querySelectorAll('#stStrokeOptions .st-opt[data-v]').forEach(b => {
      if (b.dataset.v === 'none') b.addEventListener('click', () => applyStyle(st => { st.sw = 0; }));
      else b.addEventListener('click', () => applyStyle(st => { const c = hexToRgb(b.dataset.v); if (c) { st.sc = c; st.sw = st.sw || 2; } }));
    });
    $('stStrokeColor').addEventListener('input', e => applyStyle(st => { const c = hexToRgb(e.target.value); if (c) { st.sc = c; st.sw = st.sw || 2; } }));
    // 描边粗细
    $('stStrokeWidth').addEventListener('input', e => applyStyle(st => { st.sw = Math.max(0, Math.min(6, parseFloat(e.target.value) || 0)); }));
    // 恢复默认
    $('stReset').addEventListener('click', () => {
      map.regionStyle = Object.assign({}, DEFAULT_STYLE);
      saveStyle(); map.draw(); syncStyle();
      toast('地名样式已恢复默认');
    });
  }

  /* ---------- toast ---------- */
  let toastTimer = null;
  function toast(msg) {
    const t = $('toast');
    t.textContent = msg;
    t.classList.add('show');
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => t.classList.remove('show'), 2200);
  }

  return { init, loadState, applyHash, toast, renderStats, updateLayerList, hideInfo };
})();
