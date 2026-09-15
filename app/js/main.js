/* ============ 旷野之息互动地图 · 入口 ============ */
'use strict';

(function () {
  const canvas = document.getElementById('mapCanvas');
  const sidebar = document.getElementById('sidebar');
  const sideCollapseBtn = document.getElementById('sideCollapse');
  const K_SIDE_COLLAPSE = 'botwmap.sidebarCollapsed.v1';

  // 桌面端侧栏折叠：初始化前先恢复状态（影响 canvas 宽度，必须先于 new BotwMap）
  let sideCollapsed = false;
  try { sideCollapsed = localStorage.getItem(K_SIDE_COLLAPSE) === '1'; } catch (e) {}
  function applySideCollapsed(c) {
    sidebar.classList.toggle('collapsed', c);
    sideCollapseBtn.textContent = c ? '展开 ›' : '‹ 收起';
    sideCollapseBtn.title = c ? '展开侧栏' : '收起侧栏';
  }
  applySideCollapsed(sideCollapsed);

  const map = new BotwMap(canvas, BOTW_DATA);

  UI.init(map);        // 先绑定 UI（buildLayerList 等）
  UI.loadState();      // 再恢复本地状态（完成/自定义/图层）
  UI.applyHash();      // URL 分享状态优先于本地图层状态

  UI.renderStats();
  UI.updateLayerList();

  // 视图变化时更新分享链接（轻量，不刷新 hash，避免抖动）
  map._onViewChange = () => {
    // 不做即时写回，用户点“分享”才生成链接
  };

  // 窗口尺寸变化
  let resizeTimer = null;
  window.addEventListener('resize', () => {
    clearTimeout(resizeTimer);
    resizeTimer = setTimeout(() => map._resize(), 120);
  });

  // 手机版侧栏抽屉：☰ 打开，× / 遮罩 / Esc 收起
  const sideMask = document.getElementById('sideMask');
  function setSidebar(open) {
    sidebar.classList.toggle('open', open);
    sideMask.classList.toggle('hidden', !open);
  }
  document.getElementById('btnSidebar').addEventListener('click', () => setSidebar(!sidebar.classList.contains('open')));
  document.getElementById('sideClose').addEventListener('click', () => setSidebar(false));
  sideMask.addEventListener('click', () => setSidebar(false));
  window.addEventListener('keydown', e => {
    if (e.key === 'Escape' && sidebar.classList.contains('open')) setSidebar(false);
  });
  // 桌面端（>760px）侧栏始终可见，收起态不生效
  window.addEventListener('resize', () => {
    if (window.innerWidth > 760) setSidebar(false);
  });

  // 桌面端折叠把手：收起后侧栏占 0 宽、地图占满；保留当前缩放中心（不 fit）
  function resizeCanvasKeepView() {
    const r = map.canvas.getBoundingClientRect();
    if (!r.width || !r.height) return;
    const oldCx = map.w / 2, oldCy = map.h / 2;
    const gx = (oldCx - map.view.x) / map.view.scale;
    const gy = (oldCy - map.view.y) / map.view.scale;
    map.w = r.width; map.h = r.height;
    map.canvas.width = Math.round(r.width * map.dpr);
    map.canvas.height = Math.round(r.height * map.dpr);
    map.ctx.setTransform(map.dpr, 0, 0, map.dpr, 0, 0);
    const s = Math.min(map.w / map.MW, map.h / map.MH);
    map.minScale = s * 0.85;
    map._regionScale = s;
    map.view.scale = Math.min(Math.max(map.view.scale, map.minScale), map.maxScale);
    map.view.x = map.w / 2 - gx * map.view.scale;
    map.view.y = map.h / 2 - gy * map.view.scale;
    map.draw();
  }
  let sideCollapseTimer = null;
  sideCollapseBtn.addEventListener('click', () => {
    sideCollapsed = !sideCollapsed;
    try { localStorage.setItem(K_SIDE_COLLAPSE, sideCollapsed ? '1' : '0'); } catch (e) {}
    applySideCollapsed(sideCollapsed);
    // 同步 resize：class 切换后 getBoundingClientRect 会强制 reflow，立即修正 canvas backing store，避免旧画面被拉伸一帧
    clearTimeout(sideCollapseTimer);
    resizeCanvasKeepView();
  });

  // 提示
  setTimeout(() => {
    UI.toast('点击标注查看详情 · 勾选图层可多选同显 · 完成度自动保存');
  }, 600);

  // URL 调试参数：?z=<放大倍数>&cx=<地图X归一化0-1>&cy=<地图Y归一化0-1>（仅带 z 时生效，便于分级验证）
  const sp = new URLSearchParams(location.search);
  const dz = parseFloat(sp.get('z'));
  if (dz && dz > 0) {
    setTimeout(() => {
      const f = map._regionScale || 0.05;
      const s = Math.min(Math.max(f * dz, map.minScale), map.maxScale);
      const cx = (sp.get('cx') ? parseFloat(sp.get('cx')) : 0.5) * map.MW;
      const cy = (sp.get('cy') ? parseFloat(sp.get('cy')) : 0.5) * map.MH;
      map.setView(map.w / 2 - cx * s, map.h / 2 - cy * s, s);
    }, 400);
  }

  // URL 调试参数：?dbg=1 显示右上角调试浮层（当前缩放级别 + 可见地名列表，供截图验证）
  if (sp.get('dbg')) {
    const el = document.createElement('div');
    el.id = 'dbgOut';
    el.style.cssText = 'position:fixed;right:8px;top:120px;z-index:999;background:rgba(0,0,0,.78);color:#0f0;font:11px/1.4 Consolas,monospace;padding:6px 8px;border-radius:6px;max-height:60vh;overflow:auto;pointer-events:none;white-space:pre;';
    document.body.appendChild(el);
    setInterval(() => {
      const m = window.__botwMap; if (!m) return;
      const f = m._regionScale || 0.02, g1 = f * 2, g2 = f * 4;
      const vis = m.regions.filter(r => {
        const lv = r.lv;
        if (lv === 'r1' && m.view.scale >= g1) return false;
        if (lv === 'r2' && (m.view.scale < g1 || m.view.scale >= g2)) return false;
        if (lv === 'r3' && m.view.scale < g2) return false;
        return true;
      });
      el.textContent = 'scale=' + m.view.scale.toFixed(3) + ' f=' + f.toFixed(4) + ' regs=' + m.regions.length + ' vis=' + vis.length + '\n' +
        vis.slice(0, 40).map(r => r.lv + ':' + r.n).join('\n');
    }, 800);
  }

  window.__botwMap = map; // 便于调试
})();
