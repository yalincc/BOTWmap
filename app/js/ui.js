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
    // 记录最近一次点地图的位置（canvas 相对），信息卡以此为锚点放置
    map.canvas.addEventListener('click', e => {
      const r = map.canvas.getBoundingClientRect();
      map._lastClick = { sx: e.clientX - r.left, sy: e.clientY - r.top };
    });
    initCardDrag();
  }

  /* ---------- 克洛格类型配色与灯箱（v1.1.1，P3 第二标记复用同色表） ---------- */
  const KOROK_TYPE_COLORS = {
    lift_rock: '#e8b04a', magnesis: '#6fb9ff', balloon: '#ff9ad5',
    fairy_light: '#7dffa0', rock_circle: '#b18cff', shoot_target: '#ff5252',
    race: '#ffd166', offering: '#ff8c42', flower_trail: '#9be564',
    color_flower: '#4dd6c8', dive: '#6ad0ff', push_rock: '#c9a227',
    leaves: '#7cbf5a', ice: '#a8d8ff', other: '#9aa5a0'
  };
  window.KOROK_TYPE_COLORS = KOROK_TYPE_COLORS;
  const KOROK_ITEMS = (typeof BOTW_KOROKS !== 'undefined' && BOTW_KOROKS.items) ? BOTW_KOROKS.items : [];

  function openLightbox(src, alt) {
    const lb = $('imgLightbox'), img = $('lbImg'), meta = $('lbMeta');
    if (!lb || !img) return;
    img.onload = () => {
      if (meta) meta.textContent = '原图分辨率 ' + img.naturalWidth + ' × ' + img.naturalHeight + '（这就是源站图的最高分辨率）';
    };
    img.src = src; img.alt = alt || '';
    if (meta) meta.textContent = '';
    lb.classList.remove('hidden');
  }
  function closeLightbox() {
    const lb = $('imgLightbox'), img = $('lbImg'), meta = $('lbMeta');
    if (!lb) return;
    lb.classList.add('hidden');
    if (img) img.src = '';
    if (meta) meta.textContent = '';
  }

  /* ---------- 信息卡拖拽（按住头部移动） ---------- */
  function initCardDrag() {
    const card = $('infoCard');
    const head = card.querySelector('.card-head');
    if (!head) return;
    head.addEventListener('pointerdown', e => {
      if (window.innerWidth <= 760) return;             // 手机（≤760px）：底部抽屉布局，不拖拽
      if (e.target.closest('.card-close')) return;      // 关闭按钮不触发拖拽
      e.preventDefault();
      const r0 = card.getBoundingClientRect();
      const offX = e.clientX - r0.left, offY = e.clientY - r0.top;
      const move = ev => {
        const left = Math.max(8, Math.min(ev.clientX - offX, window.innerWidth - 60));
        const top = Math.max(8, Math.min(ev.clientY - offY, window.innerHeight - 50));
        card.style.left = left + 'px'; card.style.top = top + 'px';
        card.style.right = 'auto'; card.style.bottom = 'auto';
      };
      const up = () => {
        window.removeEventListener('pointermove', move);
        window.removeEventListener('pointerup', up);
      };
      window.addEventListener('pointermove', move);
      window.addEventListener('pointerup', up);
    });
  }

  /* ---------- 图层列表 ---------- */
  function buildLayerList() {
    const wrap = $('layerList');
    wrap.innerHTML = '';
    const byKey = {};
    map.data.categories.forEach(c => byKey[c.key] = c);
    for (const [group, keys] of Object.entries(CAT_GROUP)) {
      const grp = document.createElement('div');
      grp.className = 'lg-group';
      grp.dataset.group = group;
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
      grp.appendChild(g);
      const lrows = document.createElement('div');
      lrows.className = 'lrows';
      grp.appendChild(lrows);
      wrap.appendChild(grp);
      for (const key of keys) {
        const cat = byKey[key];
        if (!cat) continue;
        const cfg = CAT_CFG[key] || {};
        const total = map.data.markers.filter(mk => mk.cat === key).length;
        const row = document.createElement('label');
        row.className = 'lrow';
        const dispName = key === 'memory' ? '照片记忆（12+1）' : cat.cn;
/* 副标已移除 */
        row.innerHTML = `
          <input type="checkbox" data-cat="${key}" ${map.enabled.has(key) ? 'checked' : ''}>
          <span class="dot" style="background:${cfg.color || '#888'}"></span>
          <span class="lname">${dispName}</span>
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
        lrows.appendChild(row);
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
    buildMatLayerGroup();
    updateLayerList();
  }

  /* 材料追踪组（v1.1.0）：6 类分组 + 74 种材料单材料 checkbox（b 方案，按材料分别聚合） */
  function buildMatLayerGroup() {
    const wrap = $('layerList');
    if (!map.MATS) return;
    const grp = document.createElement('div');
    grp.className = 'lg-group';
    grp.dataset.group = '材料';
    const g = document.createElement('div');
    g.className = 'lg';
    const gLabel = document.createElement('span');
    gLabel.textContent = '材料 · 采集';
    const gBtn = document.createElement('button');
    gBtn.type = 'button';
    gBtn.className = 'lg-btn';
    gBtn.dataset.group = '材料';
    gBtn.title = '全选 / 清空所有材料';
    g.appendChild(gLabel);
    g.appendChild(gBtn);
    grp.appendChild(g);
    const lrows = document.createElement('div');
    lrows.className = 'lrows';
    grp.appendChild(lrows);
    wrap.appendChild(grp);
    // 手风琴：6 大类默认折叠，点大类展开/收起；一次只展开一个
    const openCatKey = { value: null };
    window._matOpenCat = openCatKey;  // 供搜索联动
    window._matSetOpenCat = (cat) => {
      openCatKey.value = cat;
      lrows.querySelectorAll('.mat-cat-body').forEach(b => {
        if (b.dataset.cat === '__fav__') return;  // 常用分组独立，不随大类折叠
        b.style.display = (b.dataset.cat === cat) ? '' : 'none';
      });
      lrows.querySelectorAll('.mat-cat-head').forEach(h => {
        h.querySelector('.mat-tri').textContent = (h.dataset.cat === cat) ? '▼' : '▶';
      });
    };
    for (const cat of MAT_GROUPS) {
      const list = map.MATS.materials.map((m, i) => [m, i]).filter(([m]) => m.cat === cat);
      if (!list.length) continue;
      const cfg = MAT_CAT_CFG[cat] || {};
      const totalCount = list.reduce((s, [m]) => s + m.count, 0);
      const catHead = document.createElement('div');
      catHead.className = 'lrow mat-cat-head';
      catHead.dataset.cat = cat;
      catHead.style.cursor = 'pointer';
      catHead.innerHTML = `
        <span class="mat-tri" style="width:12px;display:inline-block">▶</span>
        <span class="dot" style="background:${cfg.color || '#888'}"></span>
        <span class="lname" style="font-weight:600">${cat}</span>
        <span class="lprog">${list.length}种 / ${totalCount}点</span>
        <button type="button" class="mat-cat-btn" data-cat="${cat}" style="margin-left:6px;background:none;border:1px solid var(--line);color:var(--text-dim);font-size:11px;padding:1px 8px;border-radius:8px;cursor:pointer">全选</button>`;
      lrows.appendChild(catHead);
      const catBody = document.createElement('div');
      catBody.className = 'mat-cat-body';
      catBody.dataset.cat = cat;
      catBody.style.display = 'none';
      lrows.appendChild(catBody);
      // 点大类行（非全选按钮）切换展开
      catHead.addEventListener('click', (e) => {
        if (e.target.closest('.mat-cat-btn')) return;
        window._matSetOpenCat(openCatKey.value === cat ? null : cat);
      });
      for (const [m, id] of list) {
        const row = document.createElement('label');
        row.className = 'lrow mat-item';
        row.style.whiteSpace = 'nowrap';
        row.dataset.matid = id;
        row.dataset.cat = cat;
        row.innerHTML = `
          <input type="checkbox" data-matid="${id}" ${map.matIds.has(id) ? 'checked' : ''}>
          <span class="lname">${m.cn}</span>
          <span class="lprog">${m.count}</span>
          <span class="mat-fav" data-favid="${id}" title="收藏到常用" style="cursor:pointer;margin-left:auto;color:${favSet.has(id) ? '#ffd84d' : '#555'};font-size:12px;line-height:1">${favSet.has(id) ? '★' : '☆'}</span>`;
        row.querySelector('.mat-fav').addEventListener('click', (e) => {
          e.preventDefault(); e.stopPropagation();
          if (favSet.has(id)) favSet.delete(id); else favSet.add(id);
          saveFav();
          e.target.textContent = favSet.has(id) ? '★' : '☆';
          e.target.style.color = favSet.has(id) ? '#ffd84d' : '#555';
          refreshFavGroup();
        });
        row.addEventListener('change', () => {
          const cb = row.querySelector('input');
          const mid = +cb.dataset.matid;
          if (cb.checked) map.matIds.add(mid); else map.matIds.delete(mid);
          saveMatLayers(); updateLayerList(); map.draw();
        });
        catBody.appendChild(row);
      }
    }
    // 常用材料分组（星标置顶）
    const favGrp = document.createElement('div');
    favGrp.className = 'lrow';
    favGrp.style.cssText = 'margin-top:2px;color:#ffd84d;font-weight:600;';
    favGrp.innerHTML = '<span class="mat-tri" id="matFavTri" style="width:12px;display:inline-block">▼</span><span style="color:#ffd84d">★</span><span style="margin-left:4px">常用材料</span><span class="lprog" id="matFavCount" style="margin-left:6px;color:var(--text-dim);font-weight:400"></span><button type="button" id="matFavAll" style="margin-left:auto;background:none;border:1px solid var(--line);color:var(--text-dim);font-size:11px;padding:1px 8px;border-radius:8px;cursor:pointer">全选</button>';
    // 插到 lrows 最前：材料标题行下、第一个大类前
    const firstCatHead = lrows.querySelector('.mat-cat-head');
    lrows.insertBefore(favGrp, firstCatHead);
    const favBody = document.createElement('div');
    favBody.className = 'mat-cat-body';
    favBody.dataset.cat = '__fav__';
    favBody.style.display = 'none';
    lrows.insertBefore(favBody, firstCatHead);
    favGrp.style.cursor = 'pointer';
    favGrp.addEventListener('click', (e) => {
      if (e.target.closest('#matFavAll')) {
        // 全选常用材料
        const ids = [...favSet];
        const allOn = ids.length > 0 && ids.every(id => map.matIds.has(id));
        ids.forEach(id => { if (allOn) map.matIds.delete(id); else map.matIds.add(id); });
        saveMatLayers(); updateLayerList(); map.draw();
        // 同步常用区 checkbox
        favBody.querySelectorAll('input[data-matid]').forEach(cb => { cb.checked = !allOn; });
        e.stopPropagation();
        return;
      }
      const show = favBody.style.display === 'none';
      favBody.style.display = show ? '' : 'none';
      document.getElementById('matFavTri').textContent = show ? '▼' : '▶';
    });
    window._refreshFavGroup = refreshFavGroup;
    function refreshFavGroup() {
      favBody.innerHTML = '';
      document.getElementById('matFavCount').textContent = favSet.size ? '(' + favSet.size + ')' : '';
      favBody.style.display = favSet.size ? '' : 'none';
      // 标题始终显示，空时显示(0)提示用户去星标
      favGrp.style.display = '';
      document.getElementById('matFavTri').textContent = favSet.size ? '▼' : '▶';
      for (const id of favSet) {
        const m = map.MATS.materials[id];
        if (!m) continue;
        const row = document.createElement('label');
        row.className = 'lrow mat-item';
        row.style.whiteSpace = 'nowrap';
        row.innerHTML = `
          <input type="checkbox" data-matid="${id}" ${map.matIds.has(id) ? 'checked' : ''}>
          <span class="lname">${m.cn}</span>
          <span class="lprog">${m.count}</span>
          <span class="mat-fav" data-favid="${id}" style="cursor:pointer;margin-left:auto;color:#ffd84d;font-size:12px">★</span>`;
        row.querySelector('input').addEventListener('change', () => {
          const mid = +row.querySelector('input').dataset.matid;
          if (row.querySelector('input').checked) map.matIds.add(mid); else map.matIds.delete(mid);
          saveMatLayers(); updateLayerList(); map.draw();
        });
        row.querySelector('.mat-fav').addEventListener('click', (e) => {
          e.preventDefault(); e.stopPropagation();
          favSet.delete(id); saveFav(); refreshFavGroup();
          // 同步主列表里的 ☆ 显示
          const star = document.querySelector(`#layerList .mat-fav[data-favid="${id}"]`);
          if (star) { star.textContent = '☆'; star.style.color = '#555'; }
        });
        favBody.appendChild(row);
      }
    }
    refreshFavGroup();

    gBtn.addEventListener('click', () => {
      const all = map.matIds.size === map.MATS.materials.length;
      if (all) map.matIds.clear(); else map.MATS.materials.forEach((_, i) => map.matIds.add(i));
      lrows.querySelectorAll('input[data-matid]').forEach(cb => cb.checked = !all);
      saveMatLayers(); updateLayerList(); map.draw();
    });
    lrows.querySelectorAll('.mat-cat-btn').forEach(btn => {
      btn.addEventListener('click', () => {
        const cat = btn.dataset.cat;
        const ids = map.MATS.materials.map((m, i) => [m, i]).filter(([m]) => m.cat === cat).map(([, i]) => i);
        const on = ids.every(i => map.matIds.has(i));
        ids.forEach(i => { if (on) map.matIds.delete(i); else map.matIds.add(i); });
        lrows.querySelectorAll('input[data-matid]').forEach(cb => {
          const m = map.MATS.materials[+cb.dataset.matid];
          if (m.cat === cat) cb.checked = !on;
        });
        saveMatLayers(); updateLayerList(); map.draw();
      });
    });
  }

  const MAT_GROUPS = ['植物', '蘑菇', '水果', '昆虫', '鱼', '矿物'];

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
      // 回忆类别与统计面板同口径：有存档 counts 以存档为准，否则按标点完成
      const [done, total] = cat.key === 'memory' ? memoryDoneTotal() : catCount(cat.key);
      el.textContent = done ? `${done}/${total}` : '';
      el.style.color = done === total ? '#7dffa0' : 'var(--text-dim)';
    }
    // 材料组按钮 + 行统计
    if (map.MATS) {
      const mbtn = wrap.querySelector('.lg-btn[data-group="材料"]');
      if (mbtn) {
        const total = map.MATS.materials.length;
        const on = map.matIds.size;
        mbtn.textContent = on === total ? '清空本组' : (on === 0 ? '全选本组' : `全选（${on}/${total}）`);
        mbtn.style.opacity = on === total ? '1' : '0.75';
      }
      for (const cat of MAT_GROUPS) {
        const el = $('mprog_' + cat);
        if (!el) continue;
        const list = map.MATS.materials.filter(m => m.cat === cat);
        const n = list.length;
        const total = list.reduce((sum, m) => sum + m.count, 0);
        el.textContent = n ? `${n}种 · ${total}点` : '';
      }
    }
  }

  /* ---------- 统计面板 ---------- */
  // 复苏神庙：剧情地点，无存档 flag，不计入神庙收集统计
  function isResurrection(mk) {
    return mk.cat === 'shrine' && mk.extra && mk.extra.icon === 'shrine_resurrection.png';
  }
  // 某类别的 [已完成, 总数]（已完成 = 本地标记 ∪ 存档进度，与地图标点判定一致）
  function catCount(key) {
    let t = 0, d = 0;
    for (const mk of map.data.markers) {
      if (mk.cat !== key) continue;
      if (isResurrection(mk)) continue;
      t++;
      if (map._mkDone(mk)) d++;
    }
    return [d, t];
  }
  // 回忆分层统计：照片12（不含最终）/ 最终1（照片13）/ 主线自动5（英杰4+大师剑1）/ DLC EX5
  function memoryCounts() {
    const PHOTO_MEMNOS = [1, 3, 5, 7, 8, 9, 11, 12, 13, 14, 15, 16, 17];
    const r = { photo: [0, 0], final: [0, 0], auto: [0, 0], dlc: [0, 0] };
    for (const mk of map.data.markers) {
      if (mk.cat !== 'memory') continue;
      const ex = mk.extra || {};
      const tier = ex.tier || (ex.memory_no == null ? 'dlc'
        : (PHOTO_MEMNOS.indexOf(ex.memory_no) >= 0 ? 'photo' : 'auto'));
      const isFinalMem = tier === 'final' || (tier === 'photo' && ex.photoNo === 13);
      const bucket = isFinalMem ? r.final
                   : tier === 'photo' ? r.photo
                   : tier === 'auto' ? r.auto : r.dlc;
      bucket[1]++;
      if (map._mkDone(mk)) bucket[0]++;
    }
    return r;
  }
  // 回忆类别合计 [已完成, 总数]：与统计面板同口径（有存档 counts 以存档为准，否则按标点完成）
  function memoryDoneTotal() {
    // 图层进度口径：只统计 13 个照片回忆（照片12 + 最终1）；主线自动 / DLC 不标注、不计数
    const m = memoryCounts();
    const lc = map.live && map.live.counts;
    const savedOf = k => (lc && lc[k]) ? lc[k][0] : null;
    const photo = savedOf('memory_photo') != null ? savedOf('memory_photo') : m.photo[0];
    const final = savedOf('memory_final') != null ? savedOf('memory_final') : m.final[0];
    return [photo + final, m.photo[1] + m.final[1]];
  }
  function renderStats() {
    const focus = ['shrine', 'tower', 'beast', 'seed', 'treasure'];
    let html = `<h4>收集进度</h4>`;
    // 存档同步状态行：live 服务已载入存档进度 或 本会话上传过存档（离线且未上传时不显示）
    const lc = map.live && map.live.counts;
    if (lc || window.__saveUploaded) {
      html += `<div class="stat-sync">存档已同步 ✓</div>`;
    }
    for (const key of focus) {
      const cat = map.data.categories.find(c => c.key === key);
      if (!cat) continue;
      const [d, t] = catCount(key);
      if (!t) continue;
      html += `<div class="stat-row"><span class="k">${cat.cn}</span><span class="v">${d} / ${t} ${pct(d, t, true)}</span></div>`;
      html += `<div class="stat-bar"><i style="width:${pct(d, t)}"></i></div>`;
    }
    // 回忆分档 4 行：照片记忆 / 最终 / 主线自动 / DLC EX
    const m = memoryCounts();
    if (m.photo[1] || m.final[1] || m.auto[1] || m.dlc[1]) {
      // 显示口径：有存档 counts 以存档为准（live 服务 / 逐点 flag），否则用手动标记
      const savedOf = k => (lc && lc[k]) ? lc[k][0] : null;
      const photoDone = savedOf('memory_photo') != null ? savedOf('memory_photo') : m.photo[0];
      const finalDone = savedOf('memory_final') != null ? savedOf('memory_final') : m.final[0];
      const autoDone = savedOf('memory_auto') != null ? savedOf('memory_auto') : m.auto[0];
      const dlcDone = savedOf('memory_dlc') != null ? savedOf('memory_dlc') : m.dlc[0];
      html += `<div class="stat-row stat-mem"><span class="k">回忆 · 照片记忆</span><span class="v">${photoDone} / 12 ${pct(photoDone, 12, true)}</span></div>`;
      html += `<div class="stat-bar"><i style="width:${pct(photoDone, 12)}"></i></div>`;
      html += `<div class="stat-row stat-mem"><span class="k">回忆 · 最终</span><span class="v">${finalDone} / 1</span></div>`;
      html += `<div class="stat-bar"><i style="width:${pct(finalDone, 1)}"></i></div>`;
      html += `<div class="stat-row stat-mem"><span class="k">回忆 · 主线自动</span><span class="v">${autoDone} / 5 ${pct(autoDone, 5, true)}</span></div>`;
      html += `<div class="stat-bar"><i style="width:${pct(autoDone, 5)}"></i></div>`;
      html += `<div class="stat-row stat-mem"><span class="k">回忆 · DLC EX</span><span class="v">${dlcDone} / 5 ${pct(dlcDone, 5, true)}</span></div>`;
      html += `<div class="stat-bar"><i style="width:${pct(dlcDone, 5)}"></i></div>`;
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
        id: mk.id, mk: mk, cat: mk.cat, name: mk.name, en: mk.en,
        region: mk.region, tower: mk.tower, px: mk.px,
        catCn: (map.data.categories.find(c => c.key === mk.cat) || {}).cn || mk.cat,
      }));
    // 材料追踪（v1.1.0）：74 种材料并入搜索
    if (map.MATS) {
      map.MATS.materials.forEach(m => {
        searchIndex.push({ mat: m, cat: m.cat, name: m.cn, en: m.en, catCn: '材料·' + m.cat, px: null });
      });
    }
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
        <div class="sr-item ${h.mat ? '' : (map._mkDone(h.mk) ? 'done' : '')}" ${h.mat ? 'data-mat="' + h.mat.id + '"' : 'data-id="' + h.id + '"'}>
          <span class="sr-name">${esc(h.name)}</span>
          <span class="sr-meta">${h.catCn}${h.region ? ' · ' + esc(h.region) : ''}</span>
        </div>`).join('');
      // 材料命中：自动启用对应类目 → 飞到分布中心 → 选中材料（高亮全部点）
      box.querySelectorAll('.sr-item[data-mat]').forEach(el => {
        el.addEventListener('click', () => {
          const m = map.MATS.materials[+el.dataset.mat];
          if (m) {
            if (!map.matIds.has(m.id)) { map.matIds.add(m.id); saveMatLayers(); updateLayerList(); }
            // 手风琴联动：展开对应大类 + 滚到该条目
            if (window._matSetOpenCat) window._matSetOpenCat(m.cat);
            const targetRow = document.querySelector('#layerList .mat-item[data-matid="' + m.id + '"]');
            if (targetRow) targetRow.scrollIntoView({ block: 'center', behavior: 'smooth' });
            const pts = map.matPointsBy[m.id];
            if (pts && pts.length) {
              let ax = 0, ay = 0;
              const n = Math.min(pts.length, 2000);
              for (let i = 0; i < n; i++) { ax += pts[i][0]; ay += pts[i][1]; }
              ax /= n; ay /= n;
              map.view.scale = Math.min(Math.max(map.view.scale, 0.35), map.maxScale);
              map.view.x = map.w / 2 - ax * map.view.scale;
              map.view.y = map.h / 2 - ay * map.view.scale;
            }
            map.draw();
            map._selectMaterial(m.id);
          }
          hideResults();
          $('searchInput').blur();
        });
      });
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
    if (hit.material) {
      currentMarker = null;
      // 侧栏联动：展开对应大类 + 滚到该条目
      if (window._matSetOpenCat) window._matSetOpenCat(hit.material.cat);
      const targetRow = document.querySelector('#layerList .mat-item[data-matid="' + hit.material.id + '"]');
      if (targetRow) targetRow.scrollIntoView({ block: 'center', behavior: 'smooth' });
      showMaterialCard(hit.material, hit.matPx);
      return;
    }
    currentMarker = hit.marker;
    renderCard();
  }

  /* 材料详情卡（v1.1.0）：复用信息卡 DOM，隐藏收集类按钮 */
  function showMaterialCard(m, matPx) {
    const cfg = MAT_CAT_CFG[m.cat] || {};
    $('cardCat').textContent = m.cat;
    $('cardCat').style.borderColor = cfg.color;
    $('cardCat').style.color = cfg.color;
    $('cardName').textContent = m.cn;
    $('cardEn').textContent = m.en || '';
    $('cardRegion').innerHTML = '图鉴编号<b>' + esc(m.entry) + '</b>';
    $('cardTower').innerHTML = '全图点位<b>' + m.count + ' 个</b>';
    $('cardCoord').innerHTML = matPx
      ? '游戏坐标<b>X ' + map.gameCoord(matPx)[0] + ' · Z ' + map.gameCoord(matPx)[1] + '</b>'
      : '分布<b>' + (m.count ? '全图可采集' : '无固定刷点') + '</b>';
    let extra = '';
    if (m.count === 0) extra += '<div class="row">说明<b style="color:#e8b04a">无 MainField 固定刷点（动态生成 / 仅栖息地）</b></div>';
    extra += '<div class="row">图层<b>' + (map.matIds.has(m.id) ? '<span style="color:#7dffa0">已开启</span>' : '未开启（请勾选侧栏「材料」组）') + '</b></div>';
    $('cardExtra').innerHTML = extra;
    $('cardKorok').classList.add('hidden');
    $('cardKorok').innerHTML = '';
    $('cardDone').classList.add('hidden');
    $('cardUndone').classList.add('hidden');
    $('cardDelPin').classList.add('hidden');
    $('cardNav').classList.add('hidden');
    $('cardCollect').classList.add('hidden');
    positionCard();
    $('infoCard').classList.remove('hidden');
  }

  /* 克洛格详情块（简化版）：位置截图（点击放大灯箱）+ 英文解法原文 */
  function renderKorokBlock(mk) {
    const el = $('cardKorok');
    if (mk.cat !== 'seed') { el.classList.add('hidden'); el.innerHTML = ''; return; }
    const k = KOROK_ITEMS.find(x => x.id === mk.id);
    if (!k) { el.classList.add('hidden'); el.innerHTML = ''; return; }
    $('cardCat').textContent = '克洛格的果实 · No.' + k.no;
    $('cardCat').style.borderColor = '#ffd700';
    $('cardCat').style.color = '#ffd700';
    let html = '<div class="korok-shot-wrap"><img class="korok-shot" data-src="assets/korok/' + esc(k.img) + '" alt="克洛格 No.' + k.no + ' 位置截图"><span class="korok-zoom">查看原图</span></div>';
    const hint = k.hint_cn || k.hint_en || '';
    html += '<div class="row">解法<b>' + esc(hint) + '</b></div>';
    el.innerHTML = html;
    const img = el.querySelector('.korok-shot');
    if (img) {
      img.src = img.dataset.src;   // 打开卡片时才真正加载（懒加载，900 张不会一次全载）
      img.onclick = () => openLightbox(img.src, img.alt);
    }
    el.classList.remove('hidden');
  }

  function renderCard() {
    const mk = currentMarker;
    if (!mk) return;
    const cat = map.data.categories.find(c => c.key === mk.cat) || {};
    const cfg = CAT_CFG[mk.cat] || {};
    const [gx, gz] = map.gameCoord(mk.px);
    const done = map._mkDone(mk);
    const liveDone = map.liveDone && map.liveDone.has(mk.id);
    $('cardCat').textContent = cat.cn || mk.cat;
    $('cardCat').style.borderColor = cfg.color;
    $('cardCat').style.color = cfg.color;
    $('cardName').textContent = mk.name;
    $('cardEn').textContent = mk.en || '';
    $('cardRegion').innerHTML = '区域<b>' + (mk.region || '—') + '</b>';
    $('cardTower').innerHTML = '塔域<b>' + (mk.tower || '—') + '</b>';
    $('cardCoord').innerHTML = '游戏坐标<b>X ' + gx + ' · Z ' + gz + '</b>';
    renderKorokBlock(mk);
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
    if (mk.cat === 'memory' && ex.tier === 'final') extra += '<div class="row">类型<b>最终回忆：需找齐 12 张照片后回英帕处激活</b></div>';
    if (mk.cat === 'shrinequest' && ex.quest_en) extra += '<div class="row">任务<b>' + esc(ex.quest_en) + '</b></div>';
    if (mk.cat === 'objective' && ex.sub) extra += '<div class="row">目标<b>' + esc(ex.sub) + '</b></div>';
    if (mk.cat === 'beast') extra += '<div class="row">类型<b>神兽·讨伐</b></div>';
    // 存档同步状态（神庙 / 塔 / 克洛格 / 主线回忆 / 神兽 有存档 flag）
    if (mk.cat === 'shrine' || mk.cat === 'tower' || mk.cat === 'seed' || mk.cat === 'memory' || mk.cat === 'beast') {
      if (liveDone) extra += '<div class="row">存档<b style="color:#7dffa0">游戏内已收集 ✓</b></div>';
      else if (map.live && map.live.counts) extra += '<div class="row">存档<b style="color:#e8b04a">游戏内未收集</b></div>';
    }
    $('cardExtra').innerHTML = extra;
    $('cardDone').classList.toggle('hidden', done);
    $('cardUndone').classList.toggle('hidden', !done);
    $('cardDelPin').classList.add('hidden');
    // 导航按钮（live 层；离线时给出提示）
    $('cardNav').classList.remove('hidden');
    $('cardNav').onclick = () => LIVE.navigate(mk);
    // 收集按钮（仅回忆 / 克洛格 显示；live 层注入）
    syncCollectBtn();
    $('cardDone').onclick = () => {
      if (mk.cat === 'memory' && map.saveLoaded()) { toast('回忆以存档为准：游戏中收集后自动同步，无需手动标记'); return; }
      map.doneSet.add(mk.id); saveDone(); renderCard(); map.draw(); updateLayerList(); renderStats(); toast('已标记完成：' + mk.name);
    };
    $('cardUndone').onclick = () => {
      if (mk.cat === 'memory' && map.saveLoaded()) { toast('回忆以存档为准：请以游戏内收集状态为准'); return; }
      map.doneSet.delete(mk.id); saveDone(); renderCard(); map.draw(); updateLayerList(); renderStats(); toast('已取消完成：' + mk.name);
    };
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
    $('cardKorok').classList.add('hidden');
    $('cardKorok').innerHTML = '';
    $('cardDone').classList.add('hidden');
    $('cardUndone').classList.add('hidden');
    $('cardDelPin').classList.remove('hidden');
    $('cardNav').classList.add('hidden');
    $('cardCollect').classList.add('hidden');
    $('cardDelPin').onclick = () => {
      map.customMarkers = map.customMarkers.filter(x => x.id !== c.id);
      saveCustom(); hideInfo(); map.draw();
      toast('已删除自定义标记');
    };
    positionCard();
    $('infoCard').classList.remove('hidden');
  }

  /* ---------- 收集按钮（回忆 / 克洛格 连续导航） ---------- */
  function syncCollectBtn() {
    const mk = currentMarker;
    const isCollect = !!(mk && (mk.cat === 'memory' || mk.cat === 'seed'));
    $('cardCollect').classList.toggle('hidden', !isCollect);
    if (!isCollect) return;
    const btn = $('cardCollect');
    const active = LIVE.isModeActive(mk.cat);
    btn.classList.toggle('on', active);
    btn.textContent = active ? '收集中' : '收集';
    btn.disabled = !(map.live && map.live.online);
    btn.onclick = () => {
      if (active) { LIVE.stopCollectMode(); toast('已结束收集模式'); }
      else LIVE.startCollectMode(mk.cat, mk);
      syncCollectBtn();
    };
  }

  function positionCard() {
    const card = $('infoCard');
    const vw = window.innerWidth, vh = window.innerHeight;
    // 手机（≤760px）：底部抽屉布局，位置/高度交给 CSS（left/right/bottom 抽屉），不设 left/top
    if (vw <= 760) {
      card.style.left = ''; card.style.top = '';
      card.style.right = ''; card.style.bottom = '';
      card.style.maxHeight = ''; card.style.overflowY = '';
      return;
    }
    card.style.right = 'auto'; card.style.bottom = 'auto';
    card.style.maxHeight = '70%'; card.style.overflowY = 'auto';
    // 桌面侧边栏展开时避让（300px）；折叠或手机抽屉时不避让
    const sidebar = document.getElementById('sidebar');
    const guard = (sidebar && !sidebar.classList.contains('collapsed')) ? 312 : 12;
    // 锚点：最近一次点击地图的位置；无记录则用画布中心偏上
    const r = map.canvas.getBoundingClientRect();
    let ax = r.left + r.width / 2, ay = r.top + r.height * 0.45;
    if (map._lastClick) { ax = r.left + map._lastClick.sx; ay = r.top + map._lastClick.sy; }
    const cw = card.offsetWidth || 300;
    const ch = Math.min(card.offsetHeight || 320, vh * 0.7);
    let left = ax + 20;                                   // 锚点右侧
    let top = ay - ch / 2;                                // 纵向居中于锚点
    if (left + cw > vw - 10) left = ax - cw - 20;         // 放不下 → 锚点左侧
    left = Math.max(guard, Math.min(left, vw - cw - 10));
    top = Math.max(10, Math.min(top, vh - 90));
    card.style.left = left + 'px';
    card.style.top = top + 'px';
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
    wireCombos();
    $('cardClose').addEventListener('click', hideInfo);
    // 截图灯箱（v1.1.1）
    $('lbClose').addEventListener('click', closeLightbox);
    $('lbBack').addEventListener('click', closeLightbox);
    document.addEventListener('keydown', e => {
      if (e.key === 'Escape' && !$('imgLightbox').classList.contains('hidden')) closeLightbox();
    });
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

  /* ---------- 图层组合（保存当前勾选，一键切换） ---------- */
  const K_COMBOS = 'botwmap.combos.v1';
  let combos = [];
  function wireCombos() {
    $('comboSave').addEventListener('click', () => {
      $('comboInputWrap').classList.remove('hidden');
      $('comboName').value = '';
      $('comboName').focus();
    });
    $('comboOk').addEventListener('click', saveCombo);
    $('comboCancel').addEventListener('click', () => $('comboInputWrap').classList.add('hidden'));
    $('comboName').addEventListener('keydown', e => {
      if (e.key === 'Enter') saveCombo();
      if (e.key === 'Escape') $('comboCancel').click();
    });
  }
  function saveCombo() {
    const name = $('comboName').value.trim();
    if (!name) { toast('请输入组合名称'); $('comboName').focus(); return; }
    const keys = [...map.enabled];
    const idx = combos.findIndex(c => c.name === name);
    if (idx >= 0) combos[idx] = { name, keys };
    else combos.push({ name, keys });
    saveCombos();
    renderCombos();
    $('comboInputWrap').classList.add('hidden');
    toast('已保存组合：' + name + '（' + keys.length + ' 个图层）');
  }
  function saveCombos() {
    try { localStorage.setItem(K_COMBOS, JSON.stringify(combos)); } catch (e) {}
  }
  function loadCombos() {
    try {
      const c = JSON.parse(localStorage.getItem(K_COMBOS) || '[]');
      combos = Array.isArray(c) ? c : [];
    } catch (e) { combos = []; }
  }
  function renderCombos() {
    const box = $('comboList');
    if (!box) return;
    if (!combos.length) {
      box.innerHTML = '<div class="combo-empty">还没有组合，勾选图层后点「保存当前组合」</div>';
      return;
    }
    box.innerHTML = combos.map(c =>
      `<div class="combo-item" data-name="${esc(c.name)}">
         <span class="combo-name">${esc(c.name)}</span>
         <span class="combo-count">${c.keys.length} 图层</span>
         <button class="combo-del" title="删除组合">×</button>
       </div>`).join('');
    box.querySelectorAll('.combo-item').forEach(row => {
      row.addEventListener('click', e => {
        if (e.target.closest('.combo-del')) return;
        applyCombo(row.dataset.name);
      });
      row.querySelector('.combo-del').addEventListener('click', e => {
        e.stopPropagation();
        const n = row.dataset.name;
        combos = combos.filter(c => c.name !== n);
        saveCombos();
        renderCombos();
        toast('已删除组合：' + n);
      });
    });
  }
  function applyCombo(name) {
    const c = combos.find(x => x.name === name);
    if (!c) return;
    map.enabled.clear();
    c.keys.forEach(k => { if (CAT_CFG[k]) map.enabled.add(k); });
    document.querySelectorAll('#layerList input').forEach(cb => cb.checked = map.enabled.has(cb.dataset.cat));
    saveLayers(); updateLayerList(); map.draw(); renderStats();
    toast('已应用组合：' + name);
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
  const K_MAT_LAYERS = 'botwmap.matLayers.v1';
  const K_MAT_FAV = 'botwmap.matFav.v1';
  const favSet = new Set(JSON.parse(localStorage.getItem(K_MAT_FAV) || '[]'));
  function saveFav() { try { localStorage.setItem(K_MAT_FAV, JSON.stringify([...favSet])); } catch (e) {} }
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
    // 材料追踪图层恢复（v1.1.0，单材料 id 数组）
    try {
      const ml = JSON.parse(localStorage.getItem(K_MAT_LAYERS) || '[]');
      ml.forEach(i => { if (Number.isInteger(i) && map.MATS.materials[i]) map.matIds.add(i); });
    } catch (e) {}
    // 图层组合 + 地名显示开关
    loadCombos();
    map.showRegions = localStorage.getItem(K_REGION_SHOW) !== '0';
    // 克洛格轨迹开关（v1.1.4）
    map.showKorokPaths = localStorage.getItem(K_KOROK_PATHS) !== '0';
    // 同步复选框与图层计数
    document.querySelectorAll('#layerList input').forEach(cb => cb.checked = map.enabled.has(cb.dataset.cat));
    document.querySelectorAll('#layerList input[data-matid]').forEach(cb => cb.checked = map.matIds.has(+cb.dataset.matid));
    updateLayerList();
    renderCombos();
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
  function saveMatLayers() {
    try { localStorage.setItem(K_MAT_LAYERS, JSON.stringify([...map.matIds])); } catch (e) {}
  }

  /* ---------- 地名样式（顶栏⚙️设置弹层） ---------- */
  const K_STYLE = 'botwmap.style.v2'; // v2：新默认（米色底深褐字）+ 字号档位
  const K_REGION_SHOW = 'botwmap.regionShow.v1'; // 地名显示开关
  const K_KOROK_PATHS = 'botwmap.korokPaths.v1'; // 克洛格轨迹开关（v1.1.4）
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
      // 地名显示
      setOpt('stShowOptions', map.showRegions ? 'show' : 'hide');
      // 克洛格轨迹（v1.1.4）
      setOpt('stShowPaths', map.showKorokPaths ? 'show' : 'hide');
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
    // 地名显示开关
    document.querySelectorAll('#stShowOptions .st-opt').forEach(b => {
      b.addEventListener('click', () => {
        map.showRegions = b.dataset.v === 'show';
        try { localStorage.setItem(K_REGION_SHOW, map.showRegions ? '1' : '0'); } catch (e) {}
        map.draw();
        syncStyle();
      });
    });
    // 克洛格轨迹开关（v1.1.4）
    document.querySelectorAll('#stShowPaths .st-opt').forEach(b => {
      b.addEventListener('click', () => {
        map.showKorokPaths = b.dataset.v === 'show';
        try { localStorage.setItem(K_KOROK_PATHS, map.showKorokPaths ? '1' : '0'); } catch (e) {}
        map.draw();
        syncStyle();
      });
    });
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
      map.showRegions = true;
      map.showKorokPaths = true;
      try { localStorage.setItem(K_REGION_SHOW, '1'); } catch (e) {}
      try { localStorage.setItem(K_KOROK_PATHS, '1'); } catch (e) {}
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

  return { init, loadState, applyHash, toast, renderStats, updateLayerList, hideInfo, persistDone: saveDone, syncCollectBtn };
})();
