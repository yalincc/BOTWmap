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
      /* 用户自己点了地图 → 锚点回到点击位置，不再复用上次定位锚点 */
      if (window.CardLayout) window.CardLayout.clearAnchor();
    });
    initCardDrag();
    initSideTabs();
    initStatsCollapse();
    initMatSearch();
  }

  /* ---------- 侧栏 Tab（v1.1.7）：探索/材料切换，CSS 显隐不重建 DOM，状态记忆 ---------- */
  function initSideTabs() {
    const sidebar = $('sidebar');
    const tabs = document.querySelectorAll('.side-tab');
    if (!sidebar || !tabs.length) return;
    const applyTab = tab => {
      sidebar.dataset.tab = tab;
      tabs.forEach(t => {
        const on = t.dataset.tab === tab;
        t.classList.toggle('active', on);
        t.setAttribute('aria-selected', on ? 'true' : 'false');
      });
      try { localStorage.setItem('botwmap.sideTab.v1', tab); } catch (e) {}
    };
    let saved = null;
    try { saved = localStorage.getItem('botwmap.sideTab.v1'); } catch (e) {}
    if (saved === 'explore' || saved === 'material') applyTab(saved);
    tabs.forEach(t => t.addEventListener('click', () => applyTab(t.dataset.tab)));
    window._applySideTab = applyTab;  // v1.2.0：探索搜索命中材料时联动切换
  }

  /* ---------- 探索度（v1.2.1 内嵌折叠面板）：点击展开/收起完整统计，面板自身滚动不截断 ---------- */
  function initStatsCollapse() {
    const toggle = $('statsToggle');
    const panel = $('statsPanel');
    if (!toggle || !panel) return;
    const KEY = 'botwmap.statsOpen.v1';
    let open = true;
    try { if (localStorage.getItem(KEY) === '0') open = false; } catch (e) {}
    const apply = () => {
      panel.classList.toggle('hidden', !open);
      toggle.textContent = (open ? '▾ ' : '▸ ') + '探索度';
      toggle.setAttribute('aria-expanded', open ? 'true' : 'false');
    };
    toggle.addEventListener('click', () => {
      open = !open;
      try { localStorage.setItem(KEY, open ? '1' : '0'); } catch (e) {}
      apply();
      if (open) renderStats();
    });
    apply();
    if (open) renderStats();
  }

  /* ---------- 材料 tab 搜索（v1.1.8）：仅搜 74 种材料，命中即展开大类+勾选+飞分布中心 ---------- */
  function initMatSearch() {
    const input = $('matSearchInput');
    const box = $('matSearchResults');
    if (!input || !box || !map.MATS) return;
    let timer = null;
    const doMatSearch = () => {
      const q = input.value.trim().toLowerCase();
      if (!q) { box.classList.add('hidden'); box.innerHTML = ''; return; }
      const hits = map.MATS.materials
        .filter(m => (m.cn || '').toLowerCase().includes(q) || (m.en || '').toLowerCase().includes(q))
        .slice(0, 40);
      if (!hits.length) {
        box.innerHTML = '<div class="sr-item"><span class="sr-meta">无结果</span></div>';
      } else {
        box.innerHTML = hits.map(m => `
          <div class="sr-item" data-mat="${m.id}">
            <span class="sr-name">${esc(m.cn)}</span>
            <span class="sr-meta">${m.cat} · ${m.count}点</span>
          </div>`).join('');
        box.querySelectorAll('.sr-item[data-mat]').forEach(el => {
          el.addEventListener('click', () => {
            const mm = map.MATS.materials[+el.dataset.mat];
            if (mm) {
              if (!map.matIds.has(mm.id)) { map.matIds.add(mm.id); saveMatLayers(); updateLayerList(); }
              if (window._matSetOpenCat) window._matSetOpenCat(mm.cat);
              const targetRow = document.querySelector('#layerList .mat-item[data-matid="' + mm.id + '"]');
              if (targetRow) { targetRow.scrollIntoView({ block: 'center', behavior: 'smooth' }); targetRow.classList.add('on'); }
              const pts = map.matPointsBy[mm.id];
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
              map._selectMaterial(mm.id);
            }
            box.classList.add('hidden'); box.innerHTML = '';
            input.blur();
          });
        });
      }
      box.classList.remove('hidden');
    };
    input.addEventListener('input', () => { clearTimeout(timer); timer = setTimeout(doMatSearch, 90); });
    input.addEventListener('focus', () => { if (input.value) doMatSearch(); });
    input.addEventListener('keydown', e => {
      if (e.key === 'Escape') { box.classList.add('hidden'); box.innerHTML = ''; input.blur(); }
    });
    document.addEventListener('click', e => {
      if (!e.target.closest('#matSearchWrap')) { box.classList.add('hidden'); box.innerHTML = ''; }
    });
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

  /* ---------- 信息卡拖拽（按住头部移动） ----------
     V1.6.3：坐标系归一改走 window.initCardDrag 统一内核（原来裸写视口坐标，
     而卡片定位原点在 #mapArea → 抓起来下移一个顶栏高度 56px）。 */
  function initCardDrag() {
    var card = $('infoCard');
    if (!card) return;
    var head = card.querySelector('.card-head');
    if (!head) return;
    window.initCardDrag(card, { handle: head });
  }

  /* V1.6.3 修复：卡片拖动统一内核（quest/equip/chest/info 四卡共用；手机底部抽屉不拖）。
     ★ 根因修复（V1.6.1 遗留）：原实现写 `X - offX - h.x`，而 toHost(0,0) 返回的是
       **负的**宿主原点 {x:-hr.left, y:-hr.top}，减两次等于加回去 → 按下瞬间卡片
       下移 2×56=112px，指针脱离卡片 → mouseup 落到 canvas → click 判定「点在外面」
       把卡片关掉，表现为「拖不动 / 拖完就没了」。
       现在直接 toHost(鼠标位置 - 抓取偏移)，由内核一次性归一，不再手工加减符号。
     ★ 拖动后写 card.dataset.manual='1'，模块 positionCard 见 manual 即跳过避让重算；
       下次 openCard / 定位跳转清除 manual 重新避让。
     ★ card._dragged：拖动刚结束的这一次 click 要被各卡的「点空白关闭」逻辑忽略。
     opts.handle：只有该元素能起拖（探索卡只认 .card-head）；不传 = 整卡可拖。 */
  window.initCardDrag = function (card, opts) {
    if (!card || card._dragInit) return;
    card._dragInit = true;
    opts = opts || {};
    var handle = opts.handle || card;
    /* 整卡可拖时：触屏只允许从 .tk-grip 抓手起拖，其余区域留给内容滚动
       （卡片 overflow-y:auto，加 touch-action:none 会连滚动一起废掉） */
    var touchGuard = !opts.handle;
    if (!handle) return;

    handle.addEventListener('pointerdown', function (e) {
      if (window.innerWidth <= 760) return;                  /* 手机：底部抽屉，不拖 */
      if (e.target.closest('.tk-close, .card-close, .btn, button, .co-link, a')) return;
      if (touchGuard && e.pointerType === 'touch' && !e.target.closest('.tk-grip')) return;
      /* ★ V1.6.4 修复（老大实测「装备卡所有位置点无法定位」根因）：
         原实现 pointerdown 立即 preventDefault + setPointerCapture，
         真实点击卡片内非按钮内容（获取点位行 / 营地武器链 / 流程点）时，
         click 被重定向到卡片本体，行监听器永远收不到（合成 el.click() 绕过
         指针系统，所以 verify 一直全绿——盲区）。
         改为「移动超 5px 才起拖」：按下只记起点；真拖起来再捕获+禁选中。 */
      var sx0 = e.clientX, sy0 = e.clientY, pid = e.pointerId, started = false;
      var offX = 0, offY = 0, r0 = null;

      var move = function (ev) {
        if (!started) {
          if (Math.abs(ev.clientX - sx0) + Math.abs(ev.clientY - sy0) < 5) return;
          started = true;
          r0 = card.getBoundingClientRect();
          offX = sx0 - r0.left; offY = sy0 - r0.top;   /* 用按下点算偏移，move 中途换算不抖 */
          try { card.setPointerCapture(pid); } catch (err) {}
          card.style.userSelect = 'none';
          card._dragged = true;
        }
        ev.preventDefault();
        /* ★ 宿主坐标归一：left/top 相对卡片定位容器 #mapArea，不是视口 */
        var p = window.CardLayout
          ? window.CardLayout.toHost(ev.clientX - offX, ev.clientY - offY)
          : { x: ev.clientX - offX, y: ev.clientY - offY, w: window.innerWidth, h: window.innerHeight };
        /* ★ 整卡不出容器：按卡片实际尺寸夹取（原实现用 w-60 / h-50，
           350px 宽的卡可以被拖到只剩 60px 露在可视区，等于「拖丢了」） */
        var cw = card.offsetWidth || 300, ch = card.offsetHeight || 320;
        var maxLeft = Math.max(8, p.w - cw - 8);
        var maxTop = Math.max(8, p.h - ch - 8);
        var left = Math.min(Math.max(8, p.x), maxLeft);
        var top = Math.min(Math.max(8, p.y), maxTop);
        card.style.left = left + 'px'; card.style.top = top + 'px';
        card.style.right = 'auto'; card.style.bottom = 'auto';
        card.dataset.manual = '1';
      };
      var up = function () {
        window.removeEventListener('pointermove', move);
        window.removeEventListener('pointerup', up);
        window.removeEventListener('pointercancel', up);
        if (started) {
          try { card.releasePointerCapture(pid); } catch (err) {}
          card.style.userSelect = '';
        }
        /* 延到本次 click 之后再清除，否则关闭逻辑还是会吃掉这一下 */
        setTimeout(function () { card._dragged = false; }, 0);
      };
      window.addEventListener('pointermove', move);
      window.addEventListener('pointerup', up);
      window.addEventListener('pointercancel', up);
    });
  };

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
        const row = document.createElement('div');
        row.className = 'lrow' + (map.enabled.has(key) ? ' on' : '');
        row.dataset.cat = key;
        const dispName = key === 'memory' ? '照片记忆' : cat.cn;  // v1.2.1：去掉(12+1)
        row.innerHTML = `
          ${cfg.icon ? `<img class="lg-ic" src="assets/icons/${cfg.icon}" alt="" loading="lazy">` : `<span class="dot" style="background:${cfg.color || '#888'}"></span>`}
          <span class="lname">${dispName}</span>
          <span class="lprog" id="prog_${key}"></span>
        `;
        row.title = '点击整行切换显示 / 隐藏';
        row.addEventListener('click', () => {
          if (map.enabled.has(key)) map.enabled.delete(key); else map.enabled.add(key);
          row.classList.toggle('on', map.enabled.has(key));
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
      wrap.querySelectorAll('.lrow[data-cat]').forEach(r => {
        if (keys.includes(r.dataset.cat)) r.classList.toggle('on', !allOn);
      });
      saveLayers(); updateLayerList(); map.draw(); renderStats();
    });
    $('checkAll').addEventListener('click', () => setAllLayers(true));
    $('uncheckAll').addEventListener('click', () => setAllLayers(false));
    buildMatLayerGroup();
    updateLayerList();
  }

  /* 材料追踪组（v1.1.0）：6 类分组 + 74 种材料单材料行（v1.2.0 起整行点击，带官方图标） */
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
    // 6 大类默认全部展开（v1.1.8：材料已独立成 tab，去掉手风琴互斥）；点大类行独立展开/收起
    window._matSetOpenCat = (cat) => {
      lrows.querySelectorAll('.mat-cat-body').forEach(b => {
        if (b.dataset.cat === cat) b.style.display = '';
      });
      lrows.querySelectorAll('.mat-cat-head').forEach(h => {
        const body = lrows.querySelector('.mat-cat-body[data-cat="' + h.dataset.cat + '"]');
        h.querySelector('.mat-tri').textContent = (body && body.style.display !== 'none') ? '▼' : '▶';
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
        <span class="mat-tri" style="width:12px;display:inline-block">▼</span>
        <span class="lname">${cat}</span>
        <span class="lprog">${list.length}种 / ${totalCount}点</span>
        <button type="button" class="mat-cat-btn" data-cat="${cat}" style="margin-left:6px;background:none;border:1px solid var(--line);color:var(--text-dim);font-size:11px;padding:1px 8px;border-radius:8px;cursor:pointer">全选</button>`;
      lrows.appendChild(catHead);
      const catBody = document.createElement('div');
      catBody.className = 'mat-cat-body';
      catBody.dataset.cat = cat;
      lrows.appendChild(catBody);
      // 点大类行（非全选按钮）独立展开/收起，不影响其他大类
      catHead.addEventListener('click', (e) => {
        if (e.target.closest('.mat-cat-btn')) return;
        const show = catBody.style.display === 'none';
        catBody.style.display = show ? '' : 'none';
        catHead.querySelector('.mat-tri').textContent = show ? '▼' : '▶';
      });
      for (const [m, id] of list) {
        const row = document.createElement('div');
        row.className = 'lrow mat-item' + (map.matIds.has(id) ? ' on' : '');
        row.style.whiteSpace = 'nowrap';
        row.dataset.matid = id;
        row.dataset.cat = cat;
        row.innerHTML = `
          <img class="lg-ic lg-ic-mat" src="assets/materials/${m.entry}.webp" alt="" loading="lazy">
          <span class="lname">${m.cn}</span>
          <span class="lprog">${m.count}</span>`;
        row.addEventListener('click', () => {
          const mid = +row.dataset.matid;
          if (map.matIds.has(mid)) map.matIds.delete(mid); else map.matIds.add(mid);
          row.classList.toggle('on', map.matIds.has(mid));
          saveMatLayers(); updateLayerList(); map.draw();
        });
        catBody.appendChild(row);
      }
    }
    gBtn.addEventListener('click', () => {
      const all = map.matIds.size === map.MATS.materials.length;
      if (all) map.matIds.clear(); else map.MATS.materials.forEach((_, i) => map.matIds.add(i));
      lrows.querySelectorAll('.mat-item').forEach(r => r.classList.toggle('on', !all));
      saveMatLayers(); updateLayerList(); map.draw();
    });
    lrows.querySelectorAll('.mat-cat-btn').forEach(btn => {
      btn.addEventListener('click', () => {
        const cat = btn.dataset.cat;
        const ids = map.MATS.materials.map((m, i) => [m, i]).filter(([m]) => m.cat === cat).map(([, i]) => i);
        const on = ids.every(i => map.matIds.has(i));
        ids.forEach(i => { if (on) map.matIds.delete(i); else map.matIds.add(i); });
        lrows.querySelectorAll('.mat-item').forEach(r => {
          const m = map.MATS.materials[+r.dataset.matid];
          if (m.cat === cat) r.classList.toggle('on', !on);
        });
        saveMatLayers(); updateLayerList(); map.draw();
      });
    });
  }

  const MAT_GROUPS = ['植物', '蘑菇', '水果', '昆虫', '鱼', '矿物'];

  function setAllLayers(on) {
    if (on) map.data.categories.forEach(c => map.enabled.add(c.key));
    else map.enabled.clear();
    document.querySelectorAll('#layerList .lrow[data-cat]').forEach(r => r.classList.toggle('on', on));
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
      // v1.2.1：只显示地图标注总数，完成度看探索度卡片（gamesir 风格）
      const [, total] = cat.key === 'memory' ? memoryDoneTotal() : catCount(cat.key);
      el.textContent = total;
      el.style.color = 'var(--text-dim)';
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
    const lc = map.live && map.live.counts;
    const synced = !!(lc || window.__saveCounts || window.__saveUploaded);
    let html = '';
    if (synced) html += `<div class="stat-sync">存档已同步 ✓</div>`;

    /* V1.6.4 改版：分组双列纯数字网格；排版对齐侧栏（行=村庄行 12px/数值 11px，
       组标题=设施地点标题样式，第二列加竖分割线） */
    const grid = rows => `<div style="display:grid;grid-template-columns:minmax(0,1fr) minmax(0,1fr);gap:4px 0">${rows}</div>`;
    const label = t => `<div style="font-size:13px;font-weight:500;color:rgba(255,255,255,.92);margin:8px 0 4px">${t}</div>`;
    const row = (n, d, t, col2) => `<div style="display:flex;justify-content:space-between;align-items:center;gap:6px;font-size:12px;line-height:1.6;` +
      (col2 ? 'border-left:1px solid rgba(255,255,255,.14);padding-left:10px;' : '') +
      `"><span style="color:rgba(255,255,255,.78);overflow:hidden;text-overflow:ellipsis;white-space:nowrap">${n}</span><span style="flex:none;font-size:11px;color:rgba(255,255,255,.85)">` + d + '/' + t + `</span></div>`;

    /* —— 探索（存档优先，本地标点兜底）—— */
    const exp = [];
    const scE = (window.__saveCounts && window.__saveCounts.explore) || {};
    [['shrine', '神庙', 'shrine'], ['tower', '希卡塔', 'tower'], ['beast', '神兽', 'beast']].forEach(([key, name, lck]) => {
      const saved = (lc && lc[lck]) ? lc[lck] : (scE[lck] != null ? [scE[lck], null] : null);
      const [d, t] = saved && saved[1] != null ? saved : catCount(key);
      if (t) exp.push([name, d, t]);
    });
    const kSaved = (lc && lc.korok) ? lc.korok : (scE.korok != null ? [scE.korok, null] : null);
    const [kd, kt] = kSaved && kSaved[1] != null ? kSaved : catCount('seed');
    if (kt) exp.push(['克洛格', kd, kt]);
    const m = memoryCounts();
    const mSaved = k => (lc && lc[k]) ? lc[k][0] : (scE[k] != null ? scE[k] : null);
    const photoDone = mSaved('memory_photo') != null ? mSaved('memory_photo') : m.photo[0];
    const finalDone = mSaved('memory_final') != null ? mSaved('memory_final') : m.final[0];
    if (m.photo[1] || photoDone) exp.push(['照片记忆', photoDone, 12]);
    if (m.final[1] || finalDone) exp.push(['最终回忆', finalDone, 1]);
    html += label('探索') + grid(exp.map((r, i) => row(r[0], r[1], r[2], i % 2 === 1)).join(''));

    /* —— 任务（4 类；QuestData/QuestDone 由任务板块提供）—— */
    const QD = window.QuestDone, QDD = window.QuestData;
    if (QD && QDD && QDD.QUESTS) {
      const qrows = [['main', '主线挑战'], ['side', '支线任务'], ['shrine', '神庙挑战'], ['dlc', 'DLC 任务']].map(([cat, name]) => {
        let t = 0, d = 0;
        QDD.QUESTS.forEach(q => { if (QDD.inRow(cat, q)) { t++; if (QD.isDone(q.key)) d++; } });
        return t ? [name, d, t] : null;
      }).filter(Boolean);
      if (qrows.length) html += label('任务') + grid(qrows.map((r, i) => row(r[0], r[1], r[2], i % 2 === 1)).join(''));
    }

    /* —— 收集 / 讨伐（随存档同步出现；localStorage 展示缓存兜底）—— */
    const sc = window.__saveCounts;
    if (sc) {
      const col = [];
      if (sc.chest != null) col.push(['宝箱', sc.chest, 1312]);
      if (sc.weapon != null) col.push(['武器获得', sc.weapon, sc.weaponTotal]);
      if (sc.pb != null) col.push(['图鉴·装备', sc.pb, sc.pbTotal]);
      if (sc.pbMat != null) col.push(['图鉴·材料', sc.pbMat, 112]);
      if (sc.pbEnemy != null) col.push(['图鉴·怪物', sc.pbEnemy, sc.pbEnemyTotal || 84]);
      html += label('收集') + grid(col.map(([n, d, t], i) => {
        if (d == null) return `<div style="display:flex;justify-content:space-between;font-size:12px;` + (i % 2 === 1 ? 'border-left:1px solid rgba(255,255,255,.14);padding-left:10px;' : '') + `"><span style="color:rgba(255,255,255,.35)">` + n + `</span><span style="color:rgba(255,255,255,.35)">待接</span></div>`;
        return row(n, d, t, i % 2 === 1);
      }).join(''));
      if (sc.enemy && sc.enemy.length) {
        html += label('讨伐') + grid(sc.enemy.map((e, i) => row(e.name, e.done, e.total, i % 2 === 1)).join(''));
      }
    }
    $('statsPanel').innerHTML = html;
  }
  function pct(a, b, short) {
    if (!b) return short ? '' : '0%';
    return Math.round(a / b * 100) + '%';
  }

  /* ---------- 搜索 ---------- */
  function buildSearchIndex() {
    const BOSS_CATS = ['lynel', 'hinox', 'talus', 'molduga'];
    searchIndex = map.data.markers
      .filter(mk => mk.name && mk.name.length < 60)
      .map(mk => {
        let searchName = mk.name;
        if (BOSS_CATS.indexOf(mk.cat) >= 0) {
          const sib = map.data.markers.filter(x => x.cat === mk.cat);
          const idx = sib.indexOf(mk);
          if (idx >= 0) searchName = '#' + (idx + 1) + ' ' + mk.name;
        }
        return {
          id: mk.id, mk: mk, cat: mk.cat, name: mk.name, searchName: searchName, en: mk.en,
          region: mk.region, tower: mk.tower, px: mk.px,
          catCn: (map.data.categories.find(c => c.key === mk.cat) || {}).cn || mk.cat,
        };
      });
    // 材料追踪（v1.1.0）：74 种材料并入搜索
    if (map.MATS) {
      map.MATS.materials.forEach(m => {
        searchIndex.push({ mat: m, cat: m.cat, name: m.cn, searchName: m.cn, en: m.en, catCn: '材料·' + m.cat, px: null });
      });
    }
    // 任务板块（V1.6.0）：147 个任务并入搜索（quest.js 提供 __questSearch）
    if (window.__questSearch) {
      window.__questSearch.forEach(t => {
        searchIndex.push({
          quest: t.q, cat: t.cat, name: t.name, searchName: t.searchName || t.name, en: t.en,
          catCn: t.catCn, px: (t.pts && t.pts[0]) || null, region: t.q.region || '',
        });
      });
    }
    // 装备板块（V1.6.0）：武器/防具并入搜索（equip.js 提供 __equipSearch）
    if (window.__equipSearch) {
      window.__equipSearch.forEach(t => {
        searchIndex.push({
          equip: t.e, cat: t.kind, name: t.name, searchName: t.searchName || t.name, en: t.en,
          catCn: t.catCn, px: t.pts || null, region: t.region || '',
        });
      });
    }
    // 供 quest.js 在移除旧任务标点后重建索引（防御性保留）
    window._rebuildSearch = buildSearchIndex;
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
    const cleanNum = ql.startsWith('#') ? ql.slice(1).trim() : null;
    const hits = [];
    for (const it of searchIndex) {
      if (hits.length >= 40) break;
      const nm = (it.name || '').toLowerCase();
      const snm = (it.searchName || '').toLowerCase();
      const en = (it.en || '').toLowerCase();
      let match = nm.includes(ql) || snm.includes(ql) || en.includes(ql) ||
          (it.region && it.region.includes(q)) || (it.tower && it.tower.includes(q));
      if (!match && cleanNum) {
        if (snm.includes('#' + cleanNum)) match = true;
      }
      if (match) hits.push(it);
    }
    const box = $('searchResults');
    if (!hits.length) {
      box.innerHTML = '<div class="sr-item"><span class="sr-meta">无结果</span></div>';
    } else {
      box.innerHTML = hits.map(h => `
        <div class="sr-item ${h.mat ? '' : h.quest ? '' : h.equip ? '' : (map._mkDone(h.mk) ? 'done' : '')}" ${h.mat ? 'data-mat="' + h.mat.id + '"' : h.quest ? 'data-quest="' + h.quest.key + '"' : h.equip ? 'data-equip="' + h.equip.id + '"' : 'data-id="' + h.id + '"'}>
          <span class="sr-name">${esc(h.searchName || h.name)}</span>
          <span class="sr-meta">${h.catCn}${h.region ? ' · ' + esc(h.region) : ''}</span>
        </div>`).join('');
      // 任务命中（V1.6.0）：打开任务卡 + 定位 + 确保对应分类勾选可见
      box.querySelectorAll('.sr-item[data-quest]').forEach(el => {
        el.addEventListener('click', () => {
          const t = window.__questSearch.find(x => x.q.key === el.dataset.quest);
          if (t && window.QuestPanel) {
            if (!window.QuestPanel.isOn(t.q.cat)) window.QuestPanel.toggle(t.q.cat);
            window.QuestPanel.locate(t.q);
            window.QuestPanel.openCard(t.q);
          }
          hideResults();
          $('searchInput').blur();
        });
      });
      // 装备命中（V1.6.0）：打开装备卡 + 定位 + 确保对应分类勾选可见
      box.querySelectorAll('.sr-item[data-equip]').forEach(el => {
        el.addEventListener('click', () => {
          const t = window.__equipSearch && window.__equipSearch.find(x => x.e.id === el.dataset.equip);
          if (t && window.EquipPanel) {
            if (!window.EquipPanel.isOn(t.kind)) window.EquipPanel.toggle(t.kind);
            window.EquipPanel.locate(t.e);
            window.EquipPanel.openCard(t.e);
          }
          hideResults();
          $('searchInput').blur();
        });
      });
      // 材料命中：自动启用对应类目 → 飞到分布中心 → 选中材料（高亮全部点）
      box.querySelectorAll('.sr-item[data-mat]').forEach(el => {
        el.addEventListener('click', () => {
          const m = map.MATS.materials[+el.dataset.mat];
          if (m) {
            if (window._applySideTab) window._applySideTab('material');  // v1.2.0：命中材料自动切到材料 tab
            if (!map.matIds.has(m.id)) { map.matIds.add(m.id); saveMatLayers(); updateLayerList(); }
            // 手风琴联动：展开对应大类 + 滚到该条目
            if (window._matSetOpenCat) window._matSetOpenCat(m.cat);
            const targetRow = document.querySelector('#layerList .mat-item[data-matid="' + m.id + '"]');
            if (targetRow) { targetRow.scrollIntoView({ block: 'center', behavior: 'smooth' }); targetRow.classList.add('on'); }
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
  let navMk = null;   // v1.4.0：当前卡片对应的导航目标（标记或材料点），用于导航/停止切换

  function onSelect(hit) {
    if (!hit) { currentMarker = null; navMk = null; hideInfo(); return; }
    // V1.6.0 外部图层命中：任务点 → 任务卡片；装备点 → 装备卡片（两卡互斥）
    if (hit.ext) {
      currentMarker = null;
      navMk = null;
      hideInfo();
      if (hit.ext.type === 'quest' && window.QuestPanel) {
        var eqCard = document.getElementById('equipCard');
        if (eqCard) eqCard.classList.add('hidden');
        window.QuestPanel.openCard(hit.ext.quest);
      }
      if (hit.ext.type === 'equip' && window.EquipPanel) {
        if (window.QuestPanel && window.QuestPanel.closeCard) window.QuestPanel.closeCard();
        window.EquipPanel.openCard(hit.ext.equip);
      }
      return;
    }
    if (hit.custom) {
      currentMarker = null;
      navMk = null;
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

  /* 基础行（区域/塔域/坐标）与材料专属块（图鉴图/meta/用途）互斥显隐（v1.4.0） */
  function showBaseRows() {
    $('cardRegion').classList.remove('hidden');
    $('cardTower').classList.remove('hidden');
    $('cardCoord').classList.remove('hidden');
  }
  function hideBaseRows() {
    $('cardRegion').classList.add('hidden');
    $('cardTower').classList.add('hidden');
    $('cardCoord').classList.add('hidden');
  }
  function hideMaterialOnly() {
    $('cardImg').classList.add('hidden');
    $('cardMeta').classList.add('hidden');
    $('cardUsage').classList.add('hidden');
  }

  /* 最近区域：取 BOTW_REGIONS（r1-r3 地名层级）中距该点最近者 */
  function nearestRegion(px) {
    if (!px || !map.regions || !map.regions.length) return '—';
    let best = null, bd = Infinity;
    for (const r of map.regions) {
      if (!r || !r.px || (r.lv && +r.lv > 3)) continue;
      const dx = r.px[0] - px[0], dy = r.px[1] - px[1];
      const d = dx * dx + dy * dy;
      if (d < bd) { bd = d; best = r; }
    }
    return best ? (best.n || '—') : '—';
  }
  /* 材料分布中心（无点击点时作导航/区域兜底） */
  function matCenter(pts) {
    let ax = 0, ay = 0;
    const n = Math.min(pts.length, 2000);
    for (let i = 0; i < n; i++) { ax += pts[i][0]; ay += pts[i][1]; }
    return [Math.round(ax / n), Math.round(ay / n)];
  }
  /* 点击点在全部点位中的序号（第 N / total 个位置） */
  function posLabel(m, pts, matPx) {
    if (!pts.length) return '无固定刷点';
    let best = 0, bd = Infinity;
    for (let i = 0; i < pts.length; i++) {
      const d = (pts[i][0] - matPx[0]) * (pts[i][0] - matPx[0]) + (pts[i][1] - matPx[1]) * (pts[i][1] - matPx[1]);
      if (d < bd) { bd = d; best = i; }
    }
    return '第 ' + (best + 1) + ' / ' + pts.length + ' 个位置';
  }
  /* 材料用途（v1.4.0 仿 TOTK）：类目标签 + 特例 */
  const MAT_UPGRADE_FISH = ['潜行鳟鱼', '铠甲鳟鱼', '大剑鳟鱼', '精力鲈鱼', '远昔骨舌鱼'];
  const MAT_SPECIAL_USAGE = {
    '精灵': '特殊道具（回复生命 / 护身）',
    '静谧公主': '特殊材料（任务 / 防具升级）',
    '毅力胡萝卜': '料理材料（补充精力圈）'
  };
  function matUsage(m) {
    const sp = MAT_SPECIAL_USAGE[m.cn];
    if (sp) return sp;
    if (m.cat === '昆虫') return '防具升级素材';
    if (m.cat === '矿物') return '强化材料（防具 / 珠宝）';
    if (m.cat === '鱼' && MAT_UPGRADE_FISH.indexOf(m.cn) >= 0) return '防具升级素材';
    return '料理材料';
  }

  /* 材料详情卡（v1.4.0 TOTK 风格）：图鉴图 + 区域/坐标/位置 meta + 用途 + 导航（可停止） */
  function showMaterialCard(m, matPx) {
    const cfg = MAT_CAT_CFG[m.cat] || {};
    $('cardCat').textContent = m.cat;
    $('cardCat').style.borderColor = cfg.color;
    $('cardCat').style.color = cfg.color;
    $('cardName').textContent = m.cn;
    $('cardEn').textContent = m.en || '';
    const headActs = $('cardHeadActs') || document.querySelector('#infoCard .card-head');
    if (headActs) headActs.querySelectorAll('.card-nav').forEach(el => el.remove());
    const tickEl = $('cardDoneTick');
    if (tickEl) tickEl.classList.add('hidden');
    const copyBtn = $('cardCopyBtn');
    if (copyBtn) copyBtn.onclick = (e) => { e.stopPropagation(); if (navigator.clipboard && navigator.clipboard.writeText) { navigator.clipboard.writeText(m.cn); toast('已复制：' + m.cn); } };
    // 图鉴图（webp；加载失败则隐藏，点击放大灯箱）
    const img = $('cardImg');
    img.src = 'assets/materials/' + m.entry + '.webp';
    img.alt = m.cn;
    img.onerror = () => img.classList.add('hidden');
    img.onclick = () => openLightbox(img.src, img.alt);
    img.classList.remove('hidden');
    // meta：区域 / 坐标 / 位置
    const pts = map.matPointsBy[m.id] || [];
    const navPoint = matPx || (pts.length ? matCenter(pts) : null);
    $('cardMetaRegion').textContent = matPx ? nearestRegion(matPx) : '全图分布';
    $('cardMetaCoord').textContent = matPx
      ? 'X ' + map.gameCoord(matPx)[0] + ' · Z ' + map.gameCoord(matPx)[1]
      : '—';
    $('cardMetaPos').textContent = matPx ? posLabel(m, pts, matPx) : (m.count ? '全图 ' + m.count + ' 个点位' : '无固定刷点');
    $('cardMeta').classList.remove('hidden');
    // 用途
    $('cardUsageVal').textContent = matUsage(m);
    $('cardUsage').classList.remove('hidden');
    hideBaseRows();
    let extra = '';
    extra += '<div class="row fd-row"><span class="fd-l">编号</span><span class="fd-v fd-num">' + esc(m.entry) + '</span></div>';
    if (m.count === 0) extra += '<div class="row fd-row"><span class="fd-l">说明</span><span class="fd-v" style="color:#e8b04a">无固定刷点（动态生成 / 仅栖息地）</span></div>';
    extra += '<div class="row fd-row"><span class="fd-l">图层</span><span class="fd-v">' + (map.matIds.has(m.id) ? '<span style="color:#7dffa0">已开启</span>' : '未开启（点击侧栏「材料」行开启）') + '</span></div>';
    $('cardExtra').innerHTML = extra;
    $('cardKorok').classList.add('hidden');
    $('cardKorok').innerHTML = '';
    $('cardDone').classList.add('hidden');
    $('cardUndone').classList.add('hidden');
    $('cardDelPin').classList.add('hidden');
    $('cardCollect').classList.add('hidden');
    // 导航（v1.4.0：材料点可导航；再次点击同一目标 = 停止导航）
    const navBtn = $('cardNav');
    if (navPoint) {
      navBtn.classList.remove('hidden');
      navMk = { px: navPoint, name: m.cn, en: m.en || '', cat: 'material' };
      navBtn.onclick = () => LIVE.toggleNav(navMk);
      refreshNavButton();
    } else {
      navBtn.classList.add('hidden');
      navMk = null;
    }
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
    html += '<div class="row fd-row"><span class="fd-l">解法</span><span class="fd-v fd-v-accent">' + esc(hint) + '</span></div>';
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
    /* V1.6.4：Boss 四族统一标题 #N + 名字（类内序号，方便陈述问题）+ head 加 ‹›/[复制] */
    const BOSS_CATS = ['lynel', 'hinox', 'talus', 'molduga'];
    const isBoss = BOSS_CATS.indexOf(mk.cat) >= 0;
    let bossIdx = -1, bossSib = null;
    if (isBoss) {
      bossSib = map.data.markers.filter(x => x.cat === mk.cat);
      bossIdx = bossSib.indexOf(mk);
      $('cardName').textContent = '#' + (bossIdx + 1) + ' ' + mk.name;
    } else {
      $('cardName').textContent = mk.name;
    }
    $('cardEn').textContent = mk.en || '';
    /* Boss head 控件：‹ › 翻同类 Boss（跳转视角并选卡） */
    const headActs = $('cardHeadActs') || document.querySelector('#infoCard .card-head');
    if (headActs) {
      headActs.querySelectorAll('.card-nav').forEach(el => el.remove());
      if (isBoss) {
        const mkSib = bossSib, mkIdx = bossIdx;
        const mkPrev = document.createElement('button');
        mkPrev.className = 'card-nav tk-icon-btn tk-nav'; mkPrev.textContent = '‹'; mkPrev.title = '上一个' + (cat.cn || '');
        mkPrev.onclick = (e) => { e.stopPropagation(); const t = mkSib[(mkIdx - 1 + mkSib.length) % mkSib.length]; map.view.x = map.w / 2 - t.px[0] * map.view.scale; map.view.y = map.h / 2 - t.px[1] * map.view.scale; map.draw(); map._onSelect({ marker: t }); };
        const mkNext = document.createElement('button');
        mkNext.className = 'card-nav tk-icon-btn tk-nav'; mkNext.textContent = '›'; mkNext.title = '下一个' + (cat.cn || '');
        mkNext.onclick = (e) => { e.stopPropagation(); const t = mkSib[(mkIdx + 1) % mkSib.length]; map.view.x = map.w / 2 - t.px[0] * map.view.scale; map.view.y = map.h / 2 - t.px[1] * map.view.scale; map.draw(); map._onSelect({ marker: t }); };
        headActs.insertBefore(mkPrev, $('cardClose'));
        headActs.insertBefore(mkNext, $('cardClose'));
      }
    }
    const copyBtn = $('cardCopyBtn');
    if (copyBtn) {
      copyBtn.onclick = (e) => {
        e.stopPropagation();
        const txt = (isBoss ? '#' + (bossIdx + 1) + ' ' : '') + mk.name;
        if (navigator.clipboard && navigator.clipboard.writeText) {
          navigator.clipboard.writeText(txt);
          toast('已复制：' + txt);
        }
      };
    }
    const tickEl = $('cardDoneTick');
    if (tickEl) {
      tickEl.classList.toggle('hidden', !done);
      tickEl.title = liveDone ? '已完成（来自存档）' : '已完成（手动标记）';
    }
    showBaseRows();
    hideMaterialOnly();
    $('cardRegion').innerHTML = '<span class="fd-l">区域</span><span class="fd-v">' + esc(mk.region || '—') + '</span>';
    $('cardTower').innerHTML = '<span class="fd-l">塔域</span><span class="fd-v">' + esc(mk.tower || '—') + '</span>';
    $('cardCoord').innerHTML = '<span class="fd-l">坐标</span><span class="fd-v fd-num">X ' + gx + ' · Z ' + gz + '</span>';
    renderKorokBlock(mk);
    let extra = '';
    const ex = mk.extra || {};
    if (mk.cat === 'seed' && ex.korok_loc) extra += '<div class="row fd-row"><span class="fd-l">位置</span><span class="fd-v">' + esc(ex.korok_loc) + '</span></div>';
    if (mk.cat === 'shrine') {
      if (ex.is_dlc) extra += '<div class="row fd-row"><span class="fd-l">类型</span><span class="fd-v fd-v-blue">英杰之诗 DLC 神庙</span></div>';
      if (ex.icon === 'shrine_resurrection.png') extra += '<div class="row fd-row"><span class="fd-l">类型</span><span class="fd-v fd-v-blue">复苏神庙</span></div>';
      if (ex.type) extra += '<div class="row fd-row"><span class="fd-l">试炼</span><span class="fd-v fd-v-accent">' + esc(ex.type) + '</span></div>';
      if (ex.treasure) extra += '<div class="row fd-row"><span class="fd-l">宝藏</span><span class="fd-v">' + esc(ex.treasure) + '</span></div>';
      /* V1.6.5 Q6：神庙卡「挑战」行 → 按神庙名反查神庙挑战（quest.name === 神庙名，42/42 全命中），
         点击打开挑战卡；data.js 的 ex.quest 是旧任务体系字段、与新任务名可能不一致
         （如 迪拉·玛神庙 vs「沙漠迷宫」），不作为关联键 */
      if (mk.cat === 'shrine') {
        let sq = null;
        (window.__questSearch || []).forEach(function (t) {
          if (!sq && t.q && t.q.cat === 'shrine' && t.q.name === mk.name) sq = t.q;
        });
        if (sq) extra += '<div class="row fd-row"><span class="fd-l">挑战</span><span class="fd-v"><span class="co-link" data-q-open="' + esc(sq.key) + '" title="点击打开对应神庙挑战卡">' + esc(sq.name) + '</span></span></div>';
      }
    }
    if (mk.cat === 'memory' && ex.memory_title_en) extra += '<div class="row fd-row"><span class="fd-l">回忆</span><span class="fd-v">#' + ex.memory_no + ' · ' + esc(ex.memory_title_en) + '</span></div>';
    if (mk.cat === 'memory' && ex.tier === 'final') extra += '<div class="row fd-row"><span class="fd-l">类型</span><span class="fd-v fd-v-accent">最终回忆：需找齐 12 张照片后回英帕处激活</span></div>';
    if (mk.cat === 'shrinequest' && ex.quest_en) extra += '<div class="row fd-row"><span class="fd-l">任务</span><span class="fd-v">' + esc(ex.quest_en) + '</span></div>';
    if (mk.cat === 'objective' && ex.sub) extra += '<div class="row fd-row"><span class="fd-l">目标</span><span class="fd-v">' + esc(ex.sub) + '</span></div>';
    if (mk.cat === 'beast') extra += '<div class="row fd-row"><span class="fd-l">类型</span><span class="fd-v fd-v-accent">神兽·讨伐</span></div>';
    // 存档同步状态（神庙 / 塔 / 克洛格 / 主线回忆 / 神兽 有存档 flag）
    if (mk.cat === 'shrine' || mk.cat === 'tower' || mk.cat === 'seed' || mk.cat === 'memory' || mk.cat === 'beast' ||
        mk.cat === 'hinox' || mk.cat === 'talus' || mk.cat === 'molduga') {   /* V1.6.4：讨伐三族显示存档态 */
      if (liveDone) extra += '<div class="row fd-row"><span class="fd-l">存档</span><span class="fd-v" style="color:#7dffa0">游戏内已收集 ✓</span></div>';
      else if (map.live && map.live.counts) extra += '<div class="row fd-row"><span class="fd-l">存档</span><span class="fd-v" style="color:#e8b04a">游戏内未收集</span></div>';
    }
    /* V1.6.5：人马讨伐 + 当前阶段（Q2 老大拍板拆两行；布点档在卡片标题，不重复） */
    if (mk.cat === 'lynel') {
      const lt = window.__lynelTierOf ? window.__lynelTierOf(mk.name) : null;
      if (lt) {
        extra += '<div class="row fd-row"><span class="fd-l">讨伐</span><span class="fd-v fd-num">已讨伐 ' + lt.total + ' 只</span></div>';
        extra += '<div class="row fd-row"><span class="fd-l">阶段</span><span class="fd-v fd-v-accent">' + esc(lt.name[lt.cur]) + (lt.upgraded ? '（血月已升档）' : '') + '</span></div>';
      }
    }
    $('cardExtra').innerHTML = extra;
    $('cardDone').classList.toggle('hidden', done);
    $('cardUndone').classList.toggle('hidden', !done);
    $('cardDelPin').classList.add('hidden');
    // 导航按钮（v1.4.0：同一目标再次点击 = 停止导航）
    $('cardNav').classList.remove('hidden');
    navMk = mk;
    $('cardNav').onclick = () => LIVE.toggleNav(mk);
    refreshNavButton();
    // 收集按钮（仅回忆 / 克洛格 显示；live 层注入）
    syncCollectBtn();
    $('cardDone').onclick = () => {
      if (mk.cat === 'memory' && map.saveLoaded()) { toast('回忆以存档为准：游戏中收集后自动同步，无需手动标记'); return; }
      map.doneSet.add(mk.id);
      if (map.saveLoaded()) map.liveDone.add(mk.id);
      saveDone(); renderCard(); map.draw(); updateLayerList(); renderStats(); toast('已标记完成：' + mk.name);
    };
    $('cardUndone').onclick = () => {
      if (mk.cat === 'memory' && map.saveLoaded()) { toast('回忆以存档为准：请以游戏内收集状态为准'); return; }
      map.doneSet.delete(mk.id);
      if (map.saveLoaded()) map.liveDone.delete(mk.id);
      saveDone(); renderCard(); map.draw(); updateLayerList(); renderStats(); toast('已取消完成：' + mk.name);
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
    const headActs = $('cardHeadActs') || document.querySelector('#infoCard .card-head');
    if (headActs) headActs.querySelectorAll('.card-nav').forEach(el => el.remove());
    const tickEl = $('cardDoneTick');
    if (tickEl) tickEl.classList.add('hidden');
    const copyBtn = $('cardCopyBtn');
    if (copyBtn) copyBtn.onclick = (e) => { e.stopPropagation(); if (navigator.clipboard && navigator.clipboard.writeText) { navigator.clipboard.writeText(c.name); toast('已复制：' + c.name); } };
    showBaseRows();
    hideMaterialOnly();
    $('cardRegion').innerHTML = '<span class="fd-l">区域</span><span class="fd-v">自定义</span>';
    $('cardTower').innerHTML = '<span class="fd-l">坐标</span><span class="fd-v fd-num">X ' + gx + ' · Z ' + gz + '</span>';
    $('cardCoord').innerHTML = '';
    $('cardExtra').innerHTML = c.note ? '<div class="row fd-row"><span class="fd-l">备注</span><span class="fd-v">' + esc(c.note) + '</span></div>' : '';
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

  /* 导航按钮文案刷新（v1.4.0）：当前卡片目标 == 进行中的导航目标 → 「停止导航」 */
  function refreshNavButton() {
    const btn = $('cardNav');
    if (!btn || btn.classList.contains('hidden')) return;
    const t = (typeof LIVE !== 'undefined' && LIVE.currentTarget) ? LIVE.currentTarget() : null;
    const cur = navMk && navMk.px && t && t.x === navMk.px[0] && t.y === navMk.px[1];
    btn.textContent = cur ? '停止导航' : '导航';
  }

  /* V1.6.1 卡片避让统一内核：实现迁至 js/card-layout.js（window.CardLayout）。
     锚点仍是最近一次点击地图位置（无记录用画布中心偏上），避让/夹取/多卡协同全交给内核。 */
  function positionCard() {
    const card = $('infoCard');
    if (!card || !map) return;
    /* 新打开（此刻仍是 hidden）→ 清除手动态重新避让；已显示的卡保持手动态位 */
    if (card.classList.contains('hidden')) {
      card.dataset.manual = '';
      /* 正要打开新卡 → 清掉上次定位锚点，改用地图点击点 */
      if (window.CardLayout) window.CardLayout.clearAnchor();
    }
    if (card.dataset.manual === '1') return;   /* 手动拖过：保持手动态位 */
    if (window.CardLayout) {
      const r = map.canvas.getBoundingClientRect();
      let sx = map.w / 2, sy = map.h * 0.45;
      const a = window.CardLayout.getAnchor();
      if (a) { sx = a.sx; sy = a.sy; }
      else if (map._lastClick) { sx = map._lastClick.sx; sy = map._lastClick.sy; }
      window.CardLayout.placeAll(r.left + sx, r.top + sy, { primary: 'infoCard' });
      return;
    }
    // 兜底（内核未加载时）：保持原行为
    const vw = window.innerWidth, vh = window.innerHeight;
    if (vw <= 760) { card.style.left = ''; card.style.top = ''; return; }
    card.style.right = 'auto'; card.style.bottom = 'auto';
    const sidebar = document.getElementById('sidebar');
    const guard = (sidebar && !sidebar.classList.contains('collapsed')) ? 312 : 12;
    const r = map.canvas.getBoundingClientRect();
    let ax = r.left + r.width / 2, ay = r.top + r.height * 0.45;
    if (map._lastClick) { ax = r.left + map._lastClick.sx; ay = r.top + map._lastClick.sy; }
    const cw = card.offsetWidth || 300;
    const ch = Math.min(card.offsetHeight || 320, vh * 0.7);
    let left = ax + 20, top = ay - ch / 2;
    if (left + cw > vw - 10) left = ax - cw - 20;
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

  /* ---------- 图层组合（v1.1.8 已移除：刷新易丢失、用处不大） ---------- */

  function applyHash() {
    if (!location.hash) return;
    const params = new URLSearchParams(location.hash.slice(1));
    const l = params.get('l');
    if (l) {
      map.enabled.clear();
      l.split(',').forEach(k => { if (CAT_CFG[k]) map.enabled.add(k); });
      document.querySelectorAll('#layerList .lrow[data-cat]').forEach(r => r.classList.toggle('on', map.enabled.has(r.dataset.cat)));
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
  function loadState() {
    try {
      const d = JSON.parse(localStorage.getItem(K_DONE) || '[]');
      d.forEach(id => map.doneSet.add(id));
    } catch (e) {}
    try {
      map.customMarkers = JSON.parse(localStorage.getItem(K_CUSTOM) || '[]');
    } catch (e) { map.customMarkers = []; }
    const def = ['shrine', 'tower', 'dragon'];  // V1.4.3b：龙特殊怪分类默认勾选（显示龙轨迹）
    try {
      const l = JSON.parse(localStorage.getItem(K_LAYERS) || 'null');
      if (Array.isArray(l) && l.length) {
        l.forEach(k => { if (CAT_CFG[k]) map.enabled.add(k); });
        // V1.4.3b 一次性迁移：老缓存无 dragon 时自动补上并保存（保证龙轨迹默认可见）
        if (CAT_CFG.dragon && !map.enabled.has('dragon')) {
          map.enabled.add('dragon');
          saveLayers();
        }
      } else def.forEach(k => map.enabled.add(k));
    } catch (e) { def.forEach(k => map.enabled.add(k)); }
    // 材料追踪图层恢复（v1.1.0，单材料 id 数组）
    try {
      const ml = JSON.parse(localStorage.getItem(K_MAT_LAYERS) || '[]');
      ml.forEach(i => { if (Number.isInteger(i) && map.MATS.materials[i]) map.matIds.add(i); });
    } catch (e) {}
    // 地名显示开关
    map.showRegions = localStorage.getItem(K_REGION_SHOW) !== '0';
    // 克洛格轨迹开关（v1.1.4）
    map.showKorokPaths = localStorage.getItem(K_KOROK_PATHS) !== '0';
    // 同步行状态与图层计数
    document.querySelectorAll('#layerList .lrow[data-cat]').forEach(r => r.classList.toggle('on', map.enabled.has(r.dataset.cat)));
    document.querySelectorAll('#layerList .mat-item').forEach(r => r.classList.toggle('on', map.matIds.has(+r.dataset.matid)));
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
  function saveMatLayers() {
    try { localStorage.setItem(K_MAT_LAYERS, JSON.stringify([...map.matIds])); } catch (e) {}
  }

  /* ---------- 地名样式（顶栏⚙️设置弹层） ---------- */
  const K_STYLE = 'botwmap.style.v3'; // v3：v1.3.1 新默认（无底金褐字深褐描边）；升级 key 强制丢弃老 v2 缓存
  const K_REGION_SHOW = 'botwmap.regionShow.v1'; // 地名显示开关
  const K_KOROK_PATHS = 'botwmap.korokPaths.v1'; // 克洛格轨迹开关（v1.1.4）
  const DEFAULT_STYLE = { bg: [0, 0, 0], bgA: 0, tx: [191, 174, 104], txA: 1, fsz: 1, sc: [63, 48, 16], sw: 3 };
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
    // V1.4.3b：龙轨迹开关移除（改由图层面板「龙」分类勾选控制）
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

  /* ---------- 人马档位推导（V1.6.5 Q2）：卡片行 + camp.js 掉落段共用 ---------- */
  /* 阈值锚点：老大存档（2026-10-14）总击杀 26（红6蓝4白6银10）实测白鬃 → 白银阈值 >26 暂定 30，待再校准 */
  window.__lynelTierOf = function (mkName) {
    const lc = (window.__saveCounts && window.__saveCounts.lynelCnt) || null;
    if (!lc) return null;
    const total = (lc.junior || 0) + (lc.middle || 0) + (lc.senior || 0) + (lc.dark || 0) + (lc.gold || 0);
    const name = { 1: '莱尼尔', 2: '蓝鬃莱尼尔', 3: '白鬃莱尼尔', 4: '白银莱尼尔' };
    const BASE = { '莱尼尔': 1, '蓝鬃莱尼尔': 2, '白鬃莱尼尔': 3, '白银莱尼尔': 4 };
    const ladder = total >= 30 ? 4 : total >= 10 ? 3 : total >= 4 ? 2 : 1;
    const base = BASE[mkName] || 1;
    const cur = Math.max(base, ladder);
    return { total: total, base: base, cur: cur, name: name, upgraded: cur > base };
  };

  /* ---------- 神庙卡「挑战」行 → 神庙挑战卡互跳（V1.6.5 Q6：data-q-open 直接放 quest.key） ---------- */
  const infoCardEl = document.getElementById('infoCard');
  if (infoCardEl) infoCardEl.addEventListener('click', function (e) {
    const el = e.target && e.target.closest ? e.target.closest('[data-q-open]') : null;
    if (!el || !el.dataset.qOpen || !window.QuestPanel) return;
    e.stopPropagation();   /* 防冒泡到 document「点空白关卡」：hideInfo 后 target 已不在卡内，会把刚开的任务卡关掉 */
    hideInfo();
    window.QuestPanel.openCard(el.dataset.qOpen);
    /* V1.6.5 Q6：定位到任务点 + 脉冲（与装备卡定位同款特效，老大要求双向都有） */
    let q = null;
    (window.__questSearch || []).forEach(function (t) { if (!q && t.q && t.q.key === el.dataset.qOpen) q = t.q; });
    if (q && q.pts && q.pts.length && window.QuestCanvas) {
      window.QuestCanvas.locate(q);
      const mp = window.__botwMap;
      if (mp && mp.pulseAt) mp.pulseAt(q.pts[0][0], q.pts[0][1]);
      if (window.SpotMark) window.SpotMark.set(q.pts[0]);
    }
  });

  /* ---------- toast ---------- */
  let toastTimer = null;
  function toast(msg, ms) {
    const t = $('toast');
    t.textContent = msg;
    t.classList.add('show');
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => t.classList.remove('show'), ms || 2200);
  }

  return { init, loadState, applyHash, toast, renderStats, updateLayerList, hideInfo, persistDone: saveDone, syncCollectBtn, refreshNavButton };
})();
